"""O10 performance probe: Go server vs Flask on private copies of a data snapshot.

Runs inside run.sh's loopback-only namespace. Prints only aggregate numbers;
per-request detail, profiles and logs stay in the private output directory.
"""
import argparse
import collections
import datetime
import errno
import json
import os
import shutil
import signal
import socket
import sqlite3
import statistics
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

TICK = os.sysconf('SC_CLK_TCK')
PAGE = os.sysconf('SC_PAGE_SIZE')
ADMIN = ('o10-admin', 'o10-test-password', 'o10-test-only-shared-secret-0123456789abcdef')


def assert_no_egress():
    if [name for _, name in socket.if_nameindex()] != ['lo']:
        raise RuntimeError('network isolation missing')
    for path in ('/run/systemd/resolve/io.systemd.Resolve', '/run/nscd/socket', '/run/dbus/system_bus_socket',
                 '/run/docker.sock', '/var/run/docker.sock'):
        if os.path.exists(path):
            raise RuntimeError('host IPC socket visible in namespace')
    try:
        socket.getaddrinfo('example.com', 80)
    except socket.gaierror:
        pass
    else:
        raise RuntimeError('DNS resolution escapes the namespace')
    for family, address in ((socket.AF_INET, ('192.0.2.1', 80)), (socket.AF_INET6, ('2001:db8::1', 80))):
        with socket.socket(family, socket.SOCK_STREAM) as probe:
            probe.settimeout(2)
            try:
                probe.connect(address)
            except OSError as error:
                if error.errno not in (errno.ENETUNREACH, errno.EHOSTUNREACH, errno.EADDRNOTAVAIL):
                    raise RuntimeError('unexpected egress probe result') from None
            else:
                raise RuntimeError('egress connection succeeded')


def send(port, method, path, token='', body=None, timeout=300):
    headers = {'Host': '127.0.0.1:5001', 'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(f'http://127.0.0.1:{port}{path}',
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers=headers, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        response = opener.open(request, timeout=timeout)
    except urllib.error.HTTPError as error:
        response = error
    except OSError:
        return 0, b''
    with response:
        return response.code, response.read()


def tree(pid):
    """pid plus its descendants (Python may spawn helpers; Sub-Store is disabled)."""
    pids, frontier = [pid], [pid]
    while frontier:
        parent = frontier.pop()
        for task in Path(f'/proc/{parent}/task').glob('*'):
            try:
                children = (task / 'children').read_text().split()
            except OSError:
                children = []
            for child in children:
                pids.append(int(child))
                frontier.append(int(child))
    return pids


def proc_sample(pid):
    out = {'rss': 0, 'hwm': 0, 'threads': 0, 'fds': 0, 'ticks': 0, 'procs': 0, 'anon_huge': 0}
    for each in tree(pid):
        try:
            status = dict(line.split(':', 1) for line in Path(f'/proc/{each}/status').read_text().splitlines() if ':' in line)
            stat = Path(f'/proc/{each}/stat').read_text().rsplit(')', 1)[1].split()
            out['rss'] += int(status['VmRSS'].split()[0]) * 1024
            out['hwm'] += int(status['VmHWM'].split()[0]) * 1024
            out['threads'] += int(status['Threads'])
            out['fds'] += len(os.listdir(f'/proc/{each}/fd'))
            out['ticks'] += int(stat[11]) + int(stat[12])
            out['procs'] += 1
            for line in Path(f'/proc/{each}/smaps_rollup').read_text().splitlines():
                if line.startswith('AnonHugePages:'):
                    out['anon_huge'] += int(line.split()[1]) * 1024
        except (OSError, KeyError, ValueError):
            pass
    return out


class Server:
    def __init__(self, kind, args, data, port, outdir):
        self.kind, self.port, self.data = kind, port, data
        env = dict(os.environ, DATA_DIR=str(data), LOG_FILE=str(data / 'app.log'), LOG_LEVEL='INFO',
                   ADMIN_USERNAME=ADMIN[0], ADMIN_PASSWORD=ADMIN[1], JWT_SECRET_KEY=ADMIN[2],
                   CONFIGFLOW_BACKEND_HOST='127.0.0.1', CONFIGFLOW_BACKEND_PORT=str(port),
                   CONFIGFLOW_FRONTEND_DIR='', SUB_STORE_BUILTIN_DIR='/nonexistent', TZ='Asia/Shanghai')
        if kind == 'go':
            self.pprof = port + 1000
            env['O10_PPROF_ADDR'] = f'127.0.0.1:{self.pprof}'
            command = [args.go]
        else:
            command = [args.python, '-m', 'backend.startup']
        self.log = (outdir / f'{kind}.log').open('wb')
        self.started = time.monotonic()
        self.process = subprocess.Popen(command, env=env, cwd=args.root, stdout=self.log, stderr=self.log,
                                        start_new_session=True)
        self.token = ''

    def wait_ready(self):
        for _ in range(1800):
            if self.process.poll() is not None:
                raise RuntimeError(f'{self.kind} exited during startup')
            code, _ = send(self.port, 'GET', '/api/auth/status', timeout=5)
            if code == 200:
                self.ready = time.monotonic() - self.started
                code, body = send(self.port, 'POST', '/api/auth/login', body={'username': ADMIN[0], 'password': ADMIN[1]})
                if code != 200:
                    raise RuntimeError(f'{self.kind} login failed {code}')
                self.token = json.loads(body)['token']
                return
            time.sleep(0.1)
        raise RuntimeError(f'{self.kind} not ready')

    def sample(self):
        result = proc_sample(self.process.pid)
        if self.kind == 'go':
            result['go'] = self.memstats()
        return result

    def memstats(self, gc=False):
        code, body = send(self.pprof, 'GET', '/debug/o10/memstats' + ('?gc=1' if gc else ''), timeout=30)
        return json.loads(body) if code == 200 else {}

    def fetch(self, path, target, timeout=None):
        if timeout is None:
            seconds = path.split('seconds=')[1] if 'seconds=' in path else '0'
            timeout = int(seconds) + 300
        code, body = send(self.pprof, 'GET', path, timeout=timeout)
        if code == 200:
            target.write_bytes(body)

    def stop(self):
        if self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGTERM)
            try:
                self.process.wait(30)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait()
        self.log.close()


def document(data):
    connection = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
    result = {'system': {}, 'shared': {}, 'profiles': {}}
    with connection:
        for scope, owner, field, value in connection.execute('SELECT scope, owner, field, value FROM fields'):
            section = result['profiles'].setdefault(owner, {}) if scope == 'profile' else result.setdefault(scope, {})
            section[field] = json.loads(value)
    connection.close()
    return result


def heartbeat_body(agent):
    now = datetime.datetime.now(datetime.timezone.utc).isoformat()
    body = {'service_status': 'running', 'system_metrics': {
        'collected_at': now, 'cpu': {'usage_percent': 3.5, 'core_count': 4},
        'memory': {'total': 4 << 30, 'used': 1 << 30, 'available': 3 << 30, 'used_percent': 25.0},
        'disk': {'total': 32 << 30, 'used': 8 << 30, 'free': 24 << 30, 'used_percent': 25.0},
        'network': {'bytes_sent': 123456, 'bytes_recv': 654321, 'speed_sent': 100, 'speed_recv': 200}}}
    for field in ('version', 'config_version'):
        if isinstance(agent.get(field), str) and agent[field]:
            body[field] = agent[field]
    return body


def cases(doc):
    """(label, method, path, body, auth) — auth is 'jwt' or an Agent token."""
    out = []
    for pid in doc['profiles']:
        for engine in ('mihomo', 'surge', 'loon', 'mosdns', 'oxidns'):
            out.append((f'GET /api/config/<profile>/{engine}', 'GET', f'/api/config/{pid}/{engine}', None, 'jwt'))
        for agg in doc['shared'].get('subscription_aggregations', []):
            out.append(('GET /api/profiles/<profile>/aggregations/<agg>/provider', 'GET',
                        f'/api/profiles/{pid}/aggregations/{agg["id"]}/provider', None, 'jwt'))
    out.append(('GET /api/agents?summary=1', 'GET', '/api/agents?summary=1', None, 'jwt'))
    out.append(('GET /api/agents?history=0', 'GET', '/api/agents?history=0', None, 'jwt'))
    out.append(('GET /api/agents', 'GET', '/api/agents', None, 'jwt'))
    out.append(('GET /api/notifications', 'GET', '/api/notifications', None, 'jwt'))
    out.append(('GET /api/profiles', 'GET', '/api/profiles', None, 'jwt'))
    for agent in doc['system'].get('agents', []):
        out.append(('GET /api/agents/<id>', 'GET', f'/api/agents/{agent["id"]}', None, 'jwt'))
        out.append(('GET /api/agents/<id>/rule-sync', 'GET', f'/api/agents/{agent["id"]}/rule-sync', None, 'jwt'))
        if agent.get('token'):
            out.append(('POST /api/agents/<id>/heartbeat', 'POST', f'/api/agents/{agent["id"]}/heartbeat',
                        heartbeat_body(agent), agent['token']))
    return out


def router_cases(server, doc):
    """Router read routes for the default profile's routers (ids from GET /api/routers)."""
    out = [('GET /api/routers', 'GET', '/api/routers', None, 'jwt')]
    code, body = send(server.port, 'GET', '/api/routers', server.token)
    ids = [item.get('id') for item in json.loads(body).get('routers', [])] if code == 200 else []
    for rid in [i for i in ids if isinstance(i, str)]:
        for suffix in ('policy', 'policy-settings', 'rule-sync'):
            out.append((f'GET /api/routers/<id>/{suffix}', 'GET', f'/api/routers/{rid}/{suffix}', None, 'jwt'))
    for agent in doc['system'].get('agents', []):
        out.append(('GET /api/agents/<id>/upstream-routers', 'GET', f'/api/agents/{agent["id"]}/upstream-routers', None, 'jwt'))
    return out


def load(server, items, iterations, private):
    """Per case: 1 warm-up (first latency kept), then N timed calls."""
    rows = []
    for label, method, path, body, auth in items:
        token = server.token if auth == 'jwt' else auth
        t0 = time.monotonic()
        code, first = send(server.port, method, path, token, body)
        first_latency = time.monotonic() - t0
        time.sleep(0.5)
        before = server.sample()
        latencies, sizes, codes = [], [], collections.Counter()
        for _ in range(iterations):
            t0 = time.monotonic()
            code, reply = send(server.port, method, path, token, body)
            latencies.append(time.monotonic() - t0)
            sizes.append(len(reply))
            codes[code] += 1
        after = server.sample()
        row = {'label': label, 'codes': dict(codes), 'first_s': first_latency, 'median_s': statistics.median(latencies),
               'max_s': max(latencies), 'bytes': statistics.median(sizes),
               'cpu_ms_per_req': (after['ticks'] - before['ticks']) * 1000 / TICK / iterations,
               'rss_after': after['rss']}
        if server.kind == 'go':
            row['alloc_per_req'] = (after['go']['total_alloc'] - before['go']['total_alloc']) / iterations
            row['mallocs_per_req'] = (after['go']['mallocs'] - before['go']['mallocs']) / iterations
        rows.append(row)
    (private / f'load-{server.kind}.json').write_text(json.dumps(rows, indent=1))
    grouped = collections.defaultdict(list)
    for row in rows:
        grouped[row['label']].append(row)
    summary = {}
    for label, group in grouped.items():
        summary[label] = {key: (max(r[key] for r in group) if key in ('first_s', 'max_s', 'median_s', 'cpu_ms_per_req', 'alloc_per_req', 'mallocs_per_req', 'bytes')
                                else None) for key in group[0] if key not in ('label', 'codes', 'rss_after')}
        summary[label]['n'] = len(group)
        summary[label]['codes'] = sorted({c for r in group for c in r['codes']})
    return summary


def route_profiles(server, items, seconds, out):
    """Per label: CPU profile + allocs before/after while looping that label's requests."""
    grouped = collections.OrderedDict()
    for item in items:
        grouped.setdefault(item[0], []).append(item)
    summary = {}
    for index, (label, group) in enumerate(grouped.items()):
        slug = f'route{index:02d}'
        server.fetch('/debug/pprof/allocs', out / f'{slug}-allocs-before.pprof')
        before = server.sample()
        thread = threading.Thread(target=server.fetch, args=(f'/debug/pprof/profile?seconds={seconds}', out / f'{slug}-cpu.pprof'))
        thread.start()
        deadline, count, latencies = time.monotonic() + seconds, 0, []
        while time.monotonic() < deadline:
            _, method, path, body, auth = group[count % len(group)]
            t0 = time.monotonic()
            send(server.port, method, path, server.token if auth == 'jwt' else auth, body)
            latencies.append(time.monotonic() - t0)
            count += 1
        thread.join()
        after = server.sample()
        server.fetch('/debug/pprof/allocs', out / f'{slug}-allocs-after.pprof')
        summary[label] = {'slug': slug, 'requests': count, 'median_s': statistics.median(latencies),
                          'alloc_per_req': (after['go']['total_alloc'] - before['go']['total_alloc']) / count,
                          'mallocs_per_req': (after['go']['mallocs'] - before['go']['mallocs']) / count,
                          'cpu_ms_per_req': (after['ticks'] - before['ticks']) * 1000 / TICK / count}
    return summary


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('snapshot', type=Path)
    parser.add_argument('--go', required=True)
    parser.add_argument('--python', required=True)
    parser.add_argument('--root', required=True)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--idle-minutes', type=float, default=30)
    parser.add_argument('--sample-seconds', type=float, default=10)
    parser.add_argument('--iterations', type=int, default=10)
    parser.add_argument('--servers', default='go,python')
    parser.add_argument('--idle-only', action='store_true')
    parser.add_argument('--idle-profile-start', type=int, default=120,
                        help='Go only: seconds into the idle window at which the idle CPU profile starts')
    parser.add_argument('--only', default='', help='comma list of label substrings to keep (load phase)')
    parser.add_argument('--route-profile-seconds', type=int, default=0,
                        help='Go only: per route label, loop requests for N s under a CPU profile + allocs diff')
    args = parser.parse_args()
    assert_no_egress()
    out = args.out
    out.mkdir(parents=True, exist_ok=True)
    os.chmod(out, 0o700)
    kinds = args.servers.split(',')
    servers = {}
    with tempfile.TemporaryDirectory(prefix='copies-', dir=out) as temporary:
        try:
            for index, kind in enumerate(kinds):
                data = Path(temporary) / kind
                shutil.copytree(args.snapshot, data, symlinks=False)
                servers[kind] = Server(kind, args, data, 15101 + index, out)
            for server in servers.values():
                server.wait_ready()
            report = {'startup': {}, 'idle': {}, 'load': {}, 'after_load': {}}
            for kind, server in servers.items():
                sample = server.sample()
                report['startup'][kind] = {'ready_s': round(server.ready, 2), **sample}
            # Idle: sample everything; Go also gets a CPU profile over the middle of the window.
            idle_seconds = args.idle_minutes * 60
            series = {kind: [] for kind in servers}
            profile_thread = None
            if 'go' in servers and idle_seconds >= 600:
                go = servers['go']
                profile_thread = threading.Thread(target=go.fetch, args=(
                    f'/debug/pprof/profile?seconds={int(idle_seconds - args.idle_profile_start - 180)}', out / 'go-idle-cpu.pprof'), daemon=True)
            start = time.monotonic()
            started_profile = False
            while time.monotonic() - start < idle_seconds:
                for kind, server in servers.items():
                    sample = server.sample()
                    sample['t'] = round(time.monotonic() - start, 1)
                    series[kind].append(sample)
                if profile_thread and not started_profile and time.monotonic() - start >= args.idle_profile_start:
                    servers['go'].fetch('/debug/pprof/allocs', out / 'go-idle-start-allocs.pprof')
                    profile_thread.start()
                    started_profile = True
                time.sleep(args.sample_seconds)
            if profile_thread and started_profile:
                profile_thread.join()
            (out / 'idle-series.json').write_text(json.dumps(series))
            for kind, rows in series.items():
                first, last = rows[0], rows[-1]
                span = last['t'] - first['t']
                # Per-minute CPU to show the peak.
                per_window = []
                for a, b in zip(rows, rows[1:]):
                    if b['t'] > a['t']:
                        per_window.append((b['ticks'] - a['ticks']) / TICK / (b['t'] - a['t']) * 100)
                report['idle'][kind] = {
                    'minutes': round(span / 60, 1), 'cpu_pct_avg': round((last['ticks'] - first['ticks']) / TICK / span * 100, 3),
                    'cpu_pct_peak_window': round(max(per_window), 2), 'rss_start': first['rss'], 'rss_end': last['rss'],
                    'rss_max': max(r['rss'] for r in rows), 'anon_huge_end': last.get('anon_huge'), 'threads_end': last['threads'], 'fds_start': first['fds'],
                    'fds_end': last['fds'], 'fds_max': max(r['fds'] for r in rows), 'procs': last['procs']}
                if kind == 'go':
                    report['idle'][kind]['goroutines'] = [rows[0]['go'].get('goroutines'), last['go'].get('goroutines'),
                                                          max(r['go'].get('goroutines', 0) for r in rows)]
                    report['idle'][kind]['heap_alloc_end'] = last['go'].get('heap_alloc')
                    report['idle'][kind]['alloc_rate_per_min'] = (last['go']['total_alloc'] - first['go']['total_alloc']) / (span / 60)
                    report['idle'][kind]['gc_count'] = last['go']['num_gc'] - first['go']['num_gc']
            if 'go' in servers:
                go = servers['go']
                report['idle']['go']['memstats_after_gc'] = go.memstats(gc=True)
                go.fetch('/debug/pprof/heap?gc=1', out / 'go-idle-heap.pprof')
                go.fetch('/debug/pprof/goroutine?debug=1', out / 'go-idle-goroutines.txt')
                go.fetch('/debug/pprof/allocs', out / 'go-before-load-allocs.pprof')
            if args.idle_only:
                (out / 'report.json').write_text(json.dumps(report, indent=1))
                print(json.dumps(report, indent=1))
                return
            doc = document(next(iter(servers.values())).data)
            items = cases(doc) + router_cases(next(iter(servers.values())), doc)
            if args.only:
                keep = args.only.split(',')
                items = [item for item in items if any(k in item[0] for k in keep)]
            for kind, server in servers.items():
                thread = None
                if kind == 'go':
                    thread = threading.Thread(target=server.fetch, args=('/debug/pprof/profile?seconds=120', out / 'go-load-cpu.pprof'), daemon=True)
                    thread.start()
                report['load'][kind] = load(server, items, args.iterations, out)
                if thread:
                    thread.join()
                    server.fetch('/debug/pprof/allocs', out / 'go-after-load-allocs.pprof')
                    server.fetch('/debug/pprof/goroutine?debug=1', out / 'go-load-goroutines.txt')
                report['after_load'][kind] = server.sample()
            if 'go' in servers and args.route_profile_seconds > 0:
                report['route_profiles'] = route_profiles(servers['go'], items, args.route_profile_seconds, out)
            time.sleep(120)
            for kind, server in servers.items():
                report['after_load'][kind + '_settled_120s'] = server.sample()
            if 'go' in servers:
                report['after_load']['go_memstats_after_gc'] = servers['go'].memstats(gc=True)
                servers['go'].fetch('/debug/pprof/heap?gc=1', out / 'go-after-load-heap.pprof')
            (out / 'report.json').write_text(json.dumps(report, indent=1))
            print(json.dumps(report, indent=1))
        finally:
            for server in servers.values():
                server.stop()


if __name__ == '__main__':
    main()
