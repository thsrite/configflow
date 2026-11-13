"""Exercise the opt-in core installer against a fully isolated fake host."""
from copy import deepcopy
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tarfile

import pytest

from backend.agents import gateway_install_script as installer
from backend.agents.go_install_script import AGENT_INSTALL_DEPENDENCIES, generate_go_aio_install_script
from backend.test_router_split_regression import without_reviewed_installer_deltas


def generate(**kwargs):
    params = dict(server_url='https://cf.test', agent_name='gateway', agent_ip='192.168.8.2',
                  agent_checksums={'linux-amd64': 'a' * 64, 'linux-arm64': 'b' * 64},
                  installation_id='install-session-0123456789', enrollment_url='https://cf.test/api/enroll',
                  enrollment_token='session-token-01234567890123456789')
    params.update(kwargs)
    return installer.generate_gateway_install_script(**params)


@pytest.mark.parametrize('params', [
    {'agent_name': 'name;touch /tmp/bad'}, {'agent_ip': '127.0.0.1'}, {'agent_ip': 'bad'},
    {'server_url': 'file:///etc/passwd'}, {'server_url': 'https://user:pass@cf.test'},
    {'enrollment_url': 'https://other.test/enroll'}, {'enrollment_token': 'x\nfoo'},
    {'installation_id': 'bad'}, {'agent_checksums': {}}, {'agent_checksums': {'linux-amd64': 'invalid'}},
    {'agent_checksums': {'linux-armv7': 'a' * 64}}, {'mihomo_agent_port': 8082},
])
def test_invalid_input_rejected(params):
    with pytest.raises(ValueError):
        generate(**params)


def test_opt_in_does_not_change_ordinary_aio_and_sh_syntax(tmp_path):
    ordinary = generate_go_aio_install_script('https://cf.test', 'ordinary')
    assert 'read -r choice < /dev/tty' in ordinary
    # The report helper recognizes secret names; ordinary config has no enrollment key.
    assert '"installation_token":' not in ordinary
    assert 'configflow-gateway' not in ordinary
    assert '-prepare-tun' in ordinary
    assert 'CONFIGFLOW_FORWARDING_OWNER' not in ordinary
    script = generate()
    assert 'read -r choice < /dev/tty' not in script
    assert '"installation_id": "install-session-0123456789"' in script
    assert '"installation_token": "session-token-01234567890123456789"' in script
    path = tmp_path / 'script.sh'
    path.write_text(script)
    result = subprocess.run(['sh', '-n', path], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    assert 'releases/latest' not in script
    # Only the gateway's own apply changes forwarding; its environment check says so.
    assert script.count('CONFIGFLOW_FORWARDING_OWNER=gateway sh "$GATEWAY_STAGE/install-agents.sh"') == 1
    assert 'sysctl -w' not in script
    assert 'enable-ipv4-forwarding.sh' not in script
    assert '99-configflow-aio-forward.conf' not in script
    assert '-prepare-tun' not in script
    assert 'nft add' not in script
    assert 'curl | sh' not in script


def test_hashes_exact_served_agent_files(tmp_path, monkeypatch):
    monkeypatch.setenv('AGENTS_STATIC_DIR', str(tmp_path))
    (tmp_path / 'configflow-agent-linux-amd64').write_bytes(b'new binary')
    assert installer.local_agent_checksums() == {'linux-amd64': hashlib.sha256(b'new binary').hexdigest()}


def host(tmp_path, monkeypatch, manager='systemd', architecture='x86_64', full_tun=False):
    events = tmp_path / 'events'
    assets = tmp_path / 'assets'
    commands = tmp_path / 'commands'
    assets.mkdir()
    commands.mkdir()
    # The fake commands use only stdlib; skip site initialization per process.
    shebang = '#!' + sys.executable + ' -S\n'
    core = shebang + '''import os,sys
from pathlib import Path
kind=Path(sys.argv[0]).name
args=sys.argv[1:]
if '-v' in args:
 version=os.environ.get('EXISTING_MIHOMO_VERSION','v1.19.32') if '/usr/local/bin/' in sys.argv[0] else 'v1.19.32'
 print('Mihomo Meta '+version+' linux amd64'); raise SystemExit()
if '--version' in args: print('oxidns 1.6.1'); raise SystemExit()
with open(os.environ['EVENTS'],'a') as f: f.write('core-check '+kind+' '+' '.join(args)+'\\n')
if os.environ.get('FAIL_CHECK') == kind: raise SystemExit(41)
if '-f' in args: p=args[args.index('-f')+1]
else: p=args[args.index('-c')+1]
assert Path(p).is_file()
'''
    agent = shebang + '''import json,os,sys
args=sys.argv[1:]
if args == ['-h']: raise SystemExit()
if args == ['-version']: print(os.environ.get('AGENT_VERSION','1.8.25-go')); raise SystemExit()
cfg=json.load(open(args[args.index('-config')+1]))
with open(os.environ['EVENTS'],'a') as f: f.write('agent-check '+cfg['service_type']+' '+args[-1]+'\\n')
if '-prepare-tun' in args:
 with open(os.environ['EVENTS'],'a') as f: f.write('prepare-tun:'+cfg['service_type']+' '+' '.join(args[2:])+'\\n')
 if os.environ.get('FULL_TUN') != 'yes': raise SystemExit('Gateway installation must not prepare TUN')
 raise SystemExit()
if cfg['service_type'] == os.environ.get('FAIL_AGENT_CHECK'): raise SystemExit(42)
'''
    raw_agent = agent.encode()
    (assets / 'agent').write_bytes(raw_agent)
    (assets / 'mihomo.gz').write_bytes(gzip.compress(core.encode(), mtime=0))
    with tarfile.open(assets / 'oxidns.tar.gz', 'w:gz') as archive:
        member = tarfile.TarInfo('oxidns')
        member.size = len(core.encode())
        member.mode = 0o755
        archive.addfile(member, io.BytesIO(core.encode()))
    releases = deepcopy(installer.CORE_RELEASES)
    for service, file in [('mihomo','mihomo.gz'),('oxidns','oxidns.tar.gz')]:
        for arch in ('linux-amd64','linux-arm64'):
            releases[service][arch]['sha256'] = hashlib.sha256((assets / file).read_bytes()).hexdigest()
    monkeypatch.setattr(installer, 'CORE_RELEASES', releases)
    hashes = {arch: hashlib.sha256(raw_agent).hexdigest() for arch in ('linux-amd64','linux-arm64')}
    source = generate(agent_checksums=hashes, full_tun=full_tun)
    # No command sees a real host path: all writable paths, logs, proc reads,
    # service definitions and Agent/core roots are confined to this temp root.
    replacements = [('/etc/', str(tmp_path / 'etc') + '/'), ('/etc ', str(tmp_path / 'etc') + ' '),
                    ('/usr/local', str(tmp_path / 'usr/local')), ('/opt/', str(tmp_path / 'opt') + '/'),
                    ('/opt ', str(tmp_path / 'opt') + ' '), ('/run/', str(tmp_path / 'run') + '/'),
                    ('/run;', str(tmp_path / 'run') + ';'), ('/var/log', str(tmp_path / 'logs')),
                    ('/proc/[0-9]*/status', str(tmp_path / 'agent-process')),
                    ('/dev/net/tun', '/dev/null'), ('/proc/sys/net/ipv4/ip_forward', str(tmp_path / 'ip_forward')),
                    # Absent unless a test answers a prompt; then it holds the typed answer.
                    ('/dev/tty', str(tmp_path / 'tty'))]
    for old,new in replacements:
        source=source.replace(old,new)
    if sys.platform == 'darwin':
        source=source.replace('sed -i ', "sed -i '' ")
    # systemd is detected by its runtime directory, which wins over OpenRC tools.
    for directory in ['etc','usr/local/bin','opt','run/systemd/system' if manager=='systemd' else 'run','logs']:
        (tmp_path / directory).mkdir(parents=True,exist_ok=True)
    (tmp_path / 'ip_forward').write_text('0\n')
    (tmp_path / 'agent-process').write_text('PPid:\tmissing\n')
    (tmp_path / 'active').write_text('')
    # Timezone data is looked up through TZDIR, never the host's zoneinfo.
    (tmp_path / 'zoneinfo/Asia').mkdir(parents=True)
    (tmp_path / 'zoneinfo/Asia/Shanghai').write_text('TZif')
    manager_code = shebang + '''import json,os,sys
from pathlib import Path
root=Path(os.environ['FAKE_ROOT']); name=Path(sys.argv[0]).name; args=sys.argv[1:]
with open(os.environ['EVENTS'],'a') as f: f.write(name+' '+' '.join(args)+'\\n')
active=set((root/'active').read_text().split())
service=args[1] if name=='systemctl' and len(args)>1 else (args[0] if args else '')
if service.endswith('.service'): service=service[:-8]
if name=='systemctl' and args[0]=='show':
 if '--property=LoadState' in args: print('loaded' if (root/'etc/systemd/system'/f'{service}.service').exists() else 'not-found')
 else:
  text=(root/'etc/systemd/system'/f'{service}.service').read_text()
  command=[l[10:] for l in text.splitlines() if l.startswith('ExecStart=')][0]
  print('{ path='+command.split()[0]+' ; argv[]='+command+' ; }')
 raise SystemExit()
if (name=='systemctl' and args[0]=='is-active'):
 service=args[-1]; raise SystemExit(0 if service in active else 3)
action=args[0] if name=='systemctl' else (args[1] if len(args)>1 else '')
if name=='rc-service' and action=='status': raise SystemExit(0 if service in active else 3)
if action=='start':
 if os.environ.get('FAIL_START')==service: raise SystemExit(43)
 active.add(service); (root/'active').write_text(' '.join(sorted(active)))
 if service.startswith('configflow-agent-'):
  kind=service.removeprefix('configflow-agent-'); p=root/'opt/configflow-agent'/kind/'config.json'
  cfg=json.loads(p.read_text()); cfg.update(agent_id='registered-'+kind,token='agent-token-'+kind)
  p.write_text(json.dumps(cfg,indent=2))
if action=='stop':
 active.discard(service); (root/'active').write_text(' '.join(sorted(active)))
if name=='systemctl' and action=='restart' and service=='systemd-resolved' and (root/'etc/systemd/resolved.conf.d/configflow.conf').exists():
 (root/'resolved-stub-off').write_text('')
'''
    curl = shebang + '''import json,os,shutil,sys
from pathlib import Path
args=sys.argv[1:]; url=args[-1]; out=Path(args[args.index('-o')+1]); assets=Path(os.environ['ASSETS'])
with open(os.environ['EVENTS'],'a') as f: f.write('download '+url+'\\n')
if '--data-binary' in args:
 body=Path(args[args.index('--data-binary')+1][1:]); headers=Path(args[args.index('-H')+1][1:])
 data=json.loads(body.read_text()); assert data['agents']['mihomo']['token']=='agent-token-mihomo'; assert data['agents']['oxidns']['id']=='registered-oxidns'
 assert headers.read_text().startswith('Authorization: Bearer session-token-')
 out.write_text('{"success":true}'); raise SystemExit()
if os.environ.get('FAIL_GITHUB') == 'yes' and 'github.com/' in url: raise SystemExit(45)
if 'mihomo-linux-' in url: src=assets/'mihomo.gz'
elif 'oxidns-' in url: src=assets/'oxidns.tar.gz'
else: src=assets/'agent'
if os.environ.get('FAIL_DOWNLOAD')==src.name: raise SystemExit(44)
shutil.copyfile(src,out)
if os.environ.get('CORRUPT')==src.name: out.write_bytes(b'bad checksum')
'''
    mocks={'id':'#!/bin/sh\necho 0\n','uname':f'#!/bin/sh\nif [ "$1" = -s ]; then echo Linux; else echo {architecture}; fi\n',
           'curl':curl,'ip':'#!/bin/sh\nexit 0\n','nft':'#!/bin/sh\n[ "${FAIL_NFT:-}" != yes ]\n',
           'ss':'#!/bin/sh\ncase "${BUSY_OWNER:-}" in *systemd-resolve*) [ -e "$FAKE_ROOT/resolved-stub-off" ] && exit 0 ;; esac\nif [ -n "${BUSY_PORT:-}" ]; then echo "udp UNCONN 0 0 ${BUSY_ADDR:-0.0.0.0}:$BUSY_PORT 0.0.0.0:* ${BUSY_OWNER:-}"; fi\n',
           'sysctl':(shebang + '''import os,sys
args=sys.argv[1:]
with open(os.environ['EVENTS'],'a') as f: f.write('sysctl '+' '.join(args)+'\\n')
assert args[:2]==['-q','-w'] and args[2] in ('net.ipv4.ip_forward=0','net.ipv4.ip_forward=1'), args
open(os.environ['FAKE_ROOT']+'/ip_forward','w').write(args[2][-1]+'\\n')
''' if full_tun else '#!/bin/sh\nprintf "forbidden-sysctl %s\\n" "$*" >> "$EVENTS"\nexit 98\n'),
           'modprobe':'#!/bin/sh\nprintf "forbidden-modprobe %s\\n" "$*" >> "$EVENTS"\nexit 97\n',
           'mknod':'#!/bin/sh\nprintf "forbidden-mknod %s\\n" "$*" >> "$EVENTS"\nexit 97\n',
           'sleep':'#!/bin/sh\nexit 0\n'}
    mocks.update({'systemctl':manager_code} if manager=='systemd' else {'rc-service':manager_code,'rc-update':manager_code})
    for name,body in mocks.items():
        p=commands/name;p.write_text(body);p.chmod(0o755)
    script=tmp_path/'install.sh';script.write_text(source)
    environment=dict(os.environ,PATH=str(commands)+os.pathsep+os.environ['PATH'],FAKE_ROOT=str(tmp_path),EVENTS=str(events),ASSETS=str(assets),
                     TZDIR=str(tmp_path/'zoneinfo'))
    if full_tun:
        environment.update(FULL_TUN='yes', AGENT_VERSION='1.8.40-go')
    def run(**overrides):
        return subprocess.run(['sh',str(script)],env=dict(environment,**overrides),capture_output=True,text=True,timeout=30)
    return run,events,source


@pytest.mark.parametrize('manager,architecture',[('systemd','x86_64'),('openrc','aarch64')])
def test_new_install_validates_before_mutation_registers_and_never_applies_forwarding(tmp_path,monkeypatch,manager,architecture):
    run,events,source=host(tmp_path,monkeypatch,manager,architecture)
    result=run()
    assert result.returncode==0,result.stdout+result.stderr
    log=events.read_text()
    assert log.index('core-check mihomo')<log.index('core-check oxidns')<log.index('enable' if manager=='systemd' else 'rc-update -u')
    assert (tmp_path/'ip_forward').read_text()=='0\n'
    assert 'forbidden-sysctl' not in log
    assert 'prepare-tun:' not in log
    assert 'forbidden-modprobe' not in log and 'forbidden-mknod' not in log
    assert not (tmp_path/'etc/sysctl.d/99-configflow-aio-forward.conf').exists()
    assert not (tmp_path/'opt/configflow-agent/mihomo/enable-ipv4-forwarding.sh').exists()
    assert 'enable: false' in (tmp_path/'etc/mihomo/config.yaml').read_text()
    assert '127.0.0.1:53' in (tmp_path/'etc/oxidns/config.yaml').read_text()
    assert '0.0.0.0:53' not in (tmp_path/'etc/oxidns/config.yaml').read_text()
    assert log.count('configflow-agent-linux-')==1
    assert 'session-token' not in result.stdout+result.stderr
    assert 'agent-token' not in result.stdout+result.stderr
    for kind in ('mihomo','oxidns'):
        cfg=json.loads((tmp_path/f'opt/configflow-agent/{kind}/config.json').read_text())
        assert cfg['installation_id']=='install-session-0123456789'
        assert cfg['service_binary']==str(tmp_path/f'usr/local/bin/{kind}')
    assert not (tmp_path/'run/configflow-gateway-install.lock').exists()


def test_missing_timezone_data_installed_before_cores(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch,'openrc')
    apk=tmp_path/'commands/apk'
    apk.write_text('#!/bin/sh\nprintf "apk %s\\n" "$*" >> "$EVENTS"\n'
                   'mkdir -p "$TZDIR/Asia" && printf TZif > "$TZDIR/Asia/Shanghai"\n')
    apk.chmod(0o755)
    empty=tmp_path/'empty-zoneinfo'
    result=run(TZDIR=str(empty))
    assert result.returncode==0,result.stdout+result.stderr
    log=events.read_text()
    assert log.count('apk add --no-cache tzdata')==1
    assert log.index('apk add --no-cache tzdata')<log.index('rc-update -u')
    assert (empty/'Asia/Shanghai').is_file()


def test_timezone_data_install_failure_stops_before_mutation(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch,'openrc')
    apk=tmp_path/'commands/apk'
    apk.write_text('#!/bin/sh\nexit 1\n')
    apk.chmod(0o755)
    result=run(TZDIR=str(tmp_path/'empty-zoneinfo'))
    assert result.returncode!=0
    assert 'tzdata' in result.stdout+result.stderr
    assert not (tmp_path/'etc/oxidns').exists()


PACKAGE_MANAGERS = ('apk', 'apt-get', 'dnf', 'yum', 'pacman', 'zypper')


def without_host_tools(tmp_path, hidden):
    """PATH of the fake commands plus host tools, minus the hidden ones and every package manager."""
    shadow = tmp_path / 'host-tools'
    shadow.mkdir()
    for directory in os.environ['PATH'].split(os.pathsep):
        if not os.path.isdir(directory):
            continue
        for entry in os.scandir(directory):
            target = shadow / entry.name
            if entry.name in hidden or entry.name in PACKAGE_MANAGERS or target.exists() or target.is_symlink():
                continue
            if entry.is_file() and os.access(entry.path, os.X_OK):
                target.symlink_to(entry.path)
    return str(tmp_path / 'commands') + os.pathsep + str(shadow)


def gateway_package_manager(tmp_path, name, fail=False):
    """Mock manager: logs the transaction, then provides the tools of the named packages."""
    provides = {'nftables': ['nft'], 'iproute2': ['ss', 'ip'], 'iproute': ['ss', 'ip']}
    manager = tmp_path / 'commands' / name
    manager.write_text('#!' + sys.executable + """ -S
import os,sys
from pathlib import Path
with open(os.environ['EVENTS'],'a') as f: f.write(Path(sys.argv[0]).name+' '+' '.join(sys.argv[1:])+'\\n')
if sys.argv[1:]==['update']: raise SystemExit()
if %r: raise SystemExit(1)
pending=Path(os.environ['FAKE_ROOT'])/'pending'
for package,tools in %r.items():
    if package in sys.argv[1:]:
        for tool in tools:
            if (pending/tool).exists(): (pending/tool).rename(Path(os.environ['FAKE_ROOT'])/'commands'/tool)
""" % (fail, provides))
    manager.chmod(0o755)


def all_output(result):
    """Terminal output plus the full installer log it points to."""
    text = result.stdout + result.stderr
    for line in text.splitlines():
        if line.startswith('完整日志：') and Path(line.removeprefix('完整日志：')).is_file():
            text += Path(line.removeprefix('完整日志：')).read_text()
    return text


def hide_gateway_tools(tmp_path, tools):
    pending = tmp_path / 'pending'
    pending.mkdir()
    for tool in tools:
        (tmp_path / 'commands' / tool).rename(pending / tool)
    return without_host_tools(tmp_path, set(tools))


@pytest.mark.parametrize('manager,package_manager,transaction', [
    ('openrc', 'apk', 'apk add --no-cache nftables iproute2'),
    ('systemd', 'apt-get', 'apt-get install -y --no-install-recommends nftables iproute2'),
    ('systemd', 'pacman', 'pacman -S --needed --noconfirm nftables iproute2'),
    ('systemd', 'dnf', 'dnf install -y nftables iproute'),
])
def test_missing_gateway_tools_installed_and_rechecked_before_any_change(tmp_path, monkeypatch, manager, package_manager, transaction):
    # Native Alpine ships BusyBox without nft and ss; the installer adds them.
    run, events, _ = host(tmp_path, monkeypatch, manager)
    path = hide_gateway_tools(tmp_path, ('nft', 'ss'))
    gateway_package_manager(tmp_path, package_manager)
    result = run(PATH=path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert '缺少工具：nft ss' in all_output(result)
    log = events.read_text()
    assert log.count(transaction) == 1
    first_change = 'rc-update -u' if manager == 'openrc' else 'systemctl enable'
    assert log.index(transaction) < log.index('core-check mihomo') < log.index(first_change)
    # Rerunning with the tools present makes no package transaction.
    events.write_text('')
    assert run(PATH=path).returncode == 0
    assert package_manager + ' ' not in events.read_text()


def test_busybox_ip_without_json_installs_iproute2(tmp_path, monkeypatch):
    run, events, _ = host(tmp_path, monkeypatch, 'openrc')
    path = hide_gateway_tools(tmp_path, ('ip',))
    # BusyBox ip exists but rejects -j, which the gateway Agent reads.
    busybox_ip = tmp_path / 'commands/ip'
    busybox_ip.write_text('#!/bin/sh\n[ "$1" != -j ]\n')
    busybox_ip.chmod(0o755)
    (tmp_path / 'pending/ip').write_text('#!/bin/sh\nexit 0\n')
    gateway_package_manager(tmp_path, 'apk')
    result = run(PATH=path)
    assert result.returncode == 0, result.stdout + result.stderr
    assert events.read_text().count('apk add --no-cache iproute2') == 1


@pytest.mark.parametrize('package_manager,command', [
    ('apk', 'apk add --no-cache nftables iproute2'),
    ('apt-get', 'apt-get update && apt-get install -y --no-install-recommends nftables iproute2'),
])
def test_failed_gateway_tool_install_stops_with_copyable_command(tmp_path, monkeypatch, package_manager, command):
    run, events, _ = host(tmp_path, monkeypatch, 'openrc')
    path = hide_gateway_tools(tmp_path, ('nft', 'ss'))
    gateway_package_manager(tmp_path, package_manager, fail=True)
    result = run(PATH=path)
    assert result.returncode != 0
    assert '可在 root 下手动执行：' + command in all_output(result)
    assert not (tmp_path / 'etc/mihomo').exists() and not (tmp_path / 'etc/oxidns').exists()
    assert not (tmp_path / 'opt/configflow-agent').exists()
    assert 'download ' not in events.read_text()


def test_gateway_without_package_manager_names_the_packages(tmp_path, monkeypatch):
    run, events, _ = host(tmp_path, monkeypatch, 'openrc')
    path = hide_gateway_tools(tmp_path, ('nft',))
    result = run(PATH=path)
    assert result.returncode != 0
    output = all_output(result)
    assert '未找到支持的包管理器' in output and '请手动安装 nftables 后重新运行' in output
    assert not (tmp_path / 'etc/mihomo').exists()


@pytest.mark.parametrize('failure',[{'CORRUPT':'oxidns.tar.gz'},{'FAIL_DOWNLOAD':'agent'}, {'FAIL_CHECK':'oxidns'},
                                  {'AGENT_VERSION':'1.8.24-go'}, {'AGENT_VERSION':'invalid'},
                                  {'AGENT_VERSION':'1.8.25-beta'}, {'BUSY_PORT':'53'}, {'FAIL_NFT':'yes'}])
def test_preflight_download_and_validation_failures_leave_host_untouched(tmp_path,monkeypatch,failure):
    run,events,source=host(tmp_path,monkeypatch)
    result=run(**failure)
    assert result.returncode!=0
    assert not (tmp_path/'etc/mihomo').exists()
    assert not (tmp_path/'etc/oxidns').exists()
    assert not (tmp_path/'opt/configflow-agent').exists()
    assert (tmp_path/'ip_forward').read_text()=='0\n'


RESOLVED_OWNER='users:(("systemd-resolve",pid=367,fd=18))'


def resolved_host(tmp_path,monkeypatch,answer=None):
    run,events,source=host(tmp_path,monkeypatch)
    (tmp_path/'active').write_text('systemd-resolved')
    if answer is not None:
        (tmp_path/'tty').write_text(answer+'\n')
    return run,events


def test_systemd_resolved_stub_without_terminal_names_the_fix(tmp_path,monkeypatch):
    run,events=resolved_host(tmp_path,monkeypatch)
    result=run(BUSY_PORT='53',BUSY_ADDR='127.0.0.53%lo',BUSY_OWNER=RESOLVED_OWNER)
    assert result.returncode!=0
    output=result.stdout+result.stderr
    assert 'systemd-resolved' in output and '没有可交互的终端' in output and 'DNSStubListener=no' in output
    assert not (tmp_path/'etc/systemd/resolved.conf.d').exists()
    assert not (tmp_path/'etc/oxidns').exists()
    assert 'restart systemd-resolved' not in events.read_text()


def test_systemd_resolved_stub_fixed_after_user_confirms(tmp_path,monkeypatch):
    run,events=resolved_host(tmp_path,monkeypatch,answer='Y')
    result=run(BUSY_PORT='53',BUSY_ADDR='127.0.0.53%lo',BUSY_OWNER=RESOLVED_OWNER)
    assert result.returncode==0,result.stdout+result.stderr
    assert '[y/N]' in (tmp_path/'tty').read_text()
    assert (tmp_path/'etc/systemd/resolved.conf.d/configflow.conf').read_text().endswith('DNSStubListener=no\n')
    assert 'systemctl restart systemd-resolved' in events.read_text()
    assert (tmp_path/'etc/oxidns').exists()


@pytest.mark.parametrize('answer',['','n','no'])
def test_systemd_resolved_stub_left_alone_unless_user_types_y(tmp_path,monkeypatch,answer):
    run,events=resolved_host(tmp_path,monkeypatch,answer=answer)
    result=run(BUSY_PORT='53',BUSY_ADDR='127.0.0.53%lo',BUSY_OWNER=RESOLVED_OWNER)
    assert result.returncode!=0
    assert '已取消自动修复' in result.stdout+result.stderr
    assert not (tmp_path/'etc/systemd/resolved.conf.d').exists()
    assert not (tmp_path/'etc/oxidns').exists()


def test_other_dns_owner_is_never_offered_the_resolved_fix(tmp_path,monkeypatch):
    run,events=resolved_host(tmp_path,monkeypatch,answer='Y')
    result=run(BUSY_PORT='53',BUSY_OWNER='users:(("dnsmasq",pid=9,fd=4))')
    output=result.stdout+result.stderr
    assert result.returncode!=0 and 'DNSStubListener' not in output and '已被占用' in output
    assert '[y/N]' not in (tmp_path/'tty').read_text()


def test_fresh_cores_are_not_reported_as_existing_but_reruns_are(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch)
    fresh=run()
    assert fresh.returncode==0,fresh.stdout+fresh.stderr
    output=fresh.stdout+fresh.stderr
    assert '已有核心配置已保留' not in output and '不安装核心' not in output
    assert '为上一步准备好的 Mihomo 和 OxiDNS 安装 Agent' in output
    rerun=run(FAIL_GITHUB='yes')
    assert rerun.returncode==0,rerun.stdout+rerun.stderr
    assert '已有核心配置已保留' in rerun.stdout+rerun.stderr


def test_rerun_preserves_published_configs_and_existing_agent_identity(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch)
    assert run().returncode==0
    for kind in ('mihomo','oxidns'):
        (tmp_path/f'etc/{kind}/config.yaml').write_text('# user published configuration\n')
    events.write_text('')
    result=run(FAIL_GITHUB='yes')
    assert result.returncode==0,result.stdout+result.stderr
    assert 'download https://github.com/' not in events.read_text()
    for kind in ('mihomo','oxidns'):
        assert (tmp_path/f'etc/{kind}/config.yaml').read_text()=='# user published configuration\n'
        cfg=json.loads((tmp_path/f'opt/configflow-agent/{kind}/config.json').read_text())
        assert cfg['agent_id']=='registered-'+kind


def test_openrc_rerun_upgrades_earlier_core_script_to_wait_for_exit(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch,'openrc')
    assert run().returncode==0
    retry='retry="TERM/30/KILL/5"\n'
    for kind in ('mihomo','oxidns'):
        unit=tmp_path/f'etc/init.d/{kind}'
        assert retry in unit.read_text()
        # Scripts written before retry existed, otherwise identical.
        unit.write_text(unit.read_text().replace(retry,''))
    result=run(FAIL_GITHUB='yes')
    assert result.returncode==0,result.stdout+result.stderr
    for kind in ('mihomo','oxidns'):
        unit=tmp_path/f'etc/init.d/{kind}'
        assert unit.read_text().count(retry)==1
        assert os.stat(unit).st_mode & 0o777==0o755


def test_openrc_rerun_still_refuses_other_core_script_edits(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch,'openrc')
    assert run().returncode==0
    unit=tmp_path/'etc/init.d/oxidns'
    edited=unit.read_text().replace('retry="TERM/30/KILL/5"\n','').replace('need net','need net localmount')
    unit.write_text(edited)
    result=run(FAIL_GITHUB='yes')
    assert result.returncode!=0
    assert '已被修改' in result.stdout+result.stderr
    assert unit.read_text()==edited


def test_partial_start_failure_can_resume_without_rewriting_configs(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch)
    failed=run(FAIL_START='oxidns')
    assert failed.returncode!=0
    before=(tmp_path/'etc/mihomo/config.yaml').read_bytes()
    assert '重新运行同一命令' in failed.stderr
    result=run()
    assert result.returncode==0,result.stdout+result.stderr
    assert (tmp_path/'etc/mihomo/config.yaml').read_bytes()==before


def test_partial_foreign_layout_is_rejected_without_overwrite(tmp_path,monkeypatch):
    run,events,source=host(tmp_path,monkeypatch)
    path=tmp_path/'etc/mihomo/config.yaml';path.parent.mkdir();path.write_text('external config')
    result=run()
    assert result.returncode!=0
    assert path.read_text()=='external config'
    assert not (tmp_path/'etc/oxidns').exists()
    assert not (path.parent/'.configflow-gateway-core-v1').exists()


@pytest.mark.parametrize('manager',['systemd','openrc'])
def test_complete_existing_layout_attaches_without_mutating_cores(tmp_path,monkeypatch,manager):
    run,events,source=host(tmp_path,monkeypatch,manager)
    assert run().returncode==0
    snapshots={}
    for kind in ('mihomo','oxidns'):
        (tmp_path/f'etc/{kind}/.configflow-gateway-core-v1').unlink()
        path=tmp_path/f'etc/{kind}/config.yaml';path.write_text('# existing external config\n')
        snapshots[path]=path.read_bytes()
        unit=tmp_path/(f'etc/systemd/system/{kind}.service' if manager=='systemd' else f'etc/init.d/{kind}')
        snapshots[unit]=unit.read_bytes()
        binary=tmp_path/f'usr/local/bin/{kind}'
        snapshots[binary]=binary.read_bytes()
    events.write_text('')
    result=run(FAIL_GITHUB='yes')
    assert result.returncode==0,result.stdout+result.stderr
    assert '保留并接入已有' in result.stdout
    for path,data in snapshots.items(): assert path.read_bytes()==data
    log=events.read_text()
    assert 'download https://github.com/' not in log
    assert log.count('configflow-agent-linux-')==1
    assert 'systemctl start mihomo' not in log
    assert 'rc-service oxidns start' not in log


@pytest.mark.parametrize('existing', ['mihomo', 'oxidns'])
def test_one_external_core_is_preserved_while_only_missing_peer_is_installed(tmp_path, monkeypatch, existing):
    run, events, source = host(tmp_path, monkeypatch)
    initial = run()
    assert initial.returncode == 0, initial.stdout + initial.stderr
    missing = 'oxidns' if existing == 'mihomo' else 'mihomo'
    (tmp_path / f'etc/{existing}/.configflow-gateway-core-v1').unlink()
    paths = [tmp_path / f'etc/{existing}/config.yaml', tmp_path / f'usr/local/bin/{existing}',
             tmp_path / f'etc/systemd/system/{existing}.service']
    original = {path: (path.read_bytes(), path.stat().st_mtime_ns, path.stat().st_mode) for path in paths}
    shutil.rmtree(tmp_path / f'etc/{missing}')
    (tmp_path / f'usr/local/bin/{missing}').unlink()
    (tmp_path / f'etc/systemd/system/{missing}.service').unlink()
    active = tmp_path / 'active'
    active.write_text(' '.join(service for service in active.read_text().split() if service != missing))
    events.write_text('')
    result = run()
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    downloads = [line for line in log.splitlines() if line.startswith('download https://github.com/')]
    assert len(downloads) == 1
    assert ('mihomo-linux-' if missing == 'mihomo' else 'oxidns-') in downloads[0]
    for path, expected in original.items():
        assert (path.read_bytes(), path.stat().st_mtime_ns, path.stat().st_mode) == expected
    assert f'systemctl start {existing}' not in log
    assert (tmp_path / f'etc/{missing}/.configflow-gateway-core-v1').is_file()
    assert (tmp_path / 'ip_forward').read_text() == '0\n'


@pytest.mark.parametrize('kind', ['mihomo', 'oxidns'])
def test_owned_missing_binary_downloads_only_that_core_and_checks_preserved_config(tmp_path, monkeypatch, kind):
    run, events, source = host(tmp_path, monkeypatch)
    initial = run()
    assert initial.returncode == 0, initial.stdout + initial.stderr
    binary = tmp_path / f'usr/local/bin/{kind}'
    binary.unlink()
    config = tmp_path / f'etc/{kind}/config.yaml'
    config.write_text('# retained published config\n')
    events.write_text('')
    result = run()
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    downloads = [line for line in log.splitlines() if line.startswith('download https://github.com/')]
    assert len(downloads) == 1
    assert ('mihomo-linux-' if kind == 'mihomo' else 'oxidns-') in downloads[0]
    assert config.read_text() == '# retained published config\n'
    assert binary.is_file()
    checks = [line for line in log.splitlines() if line.startswith('core-check ' + kind + ' ')]
    assert len(checks) == 1 and str(config) in checks[0]


@pytest.mark.parametrize('kind', ['mihomo', 'oxidns'])
def test_owned_missing_config_checks_bootstrap_with_existing_binary_without_github(tmp_path, monkeypatch, kind):
    run, events, source = host(tmp_path, monkeypatch)
    initial = run()
    assert initial.returncode == 0, initial.stdout + initial.stderr
    config = tmp_path / f'etc/{kind}/config.yaml'
    config.unlink()
    binary = tmp_path / f'usr/local/bin/{kind}'
    original = binary.read_bytes()
    events.write_text('')
    result = run(FAIL_GITHUB='yes')
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    assert 'download https://github.com/' not in log
    assert binary.read_bytes() == original
    assert config.is_file()
    checks = [line for line in log.splitlines() if line.startswith('core-check ' + kind + ' ')]
    assert len(checks) == 1 and f'/{kind}.yaml' in checks[0] and str(config) not in checks[0]


@pytest.mark.parametrize('kind', ['mihomo', 'oxidns'])
def test_partial_recovery_native_check_failure_preserves_missing_core(tmp_path, monkeypatch, kind):
    run, events, source = host(tmp_path, monkeypatch)
    initial = run()
    assert initial.returncode == 0, initial.stdout + initial.stderr
    binary = tmp_path / f'usr/local/bin/{kind}'
    binary.unlink()
    config = tmp_path / f'etc/{kind}/config.yaml'
    original = config.read_bytes()
    events.write_text('')
    result = run(FAIL_CHECK=kind)
    assert result.returncode != 0
    assert not binary.exists()
    assert config.read_bytes() == original
    assert 'systemctl enable' not in events.read_text()


@pytest.mark.parametrize('version',['v1.19.32-alpha','v1.19.32-custom','v1.18.9','alpha'])
def test_existing_mihomo_must_be_supported_stable_release_before_any_install_changes(tmp_path,monkeypatch,version):
    run,events,source=host(tmp_path,monkeypatch)
    initial=run()
    assert initial.returncode==0,initial.stdout+initial.stderr
    before={kind:(tmp_path/f'etc/{kind}/config.yaml').read_bytes() for kind in ('mihomo','oxidns')}
    events.write_text('')
    result=run(EXISTING_MIHOMO_VERSION=version)
    assert result.returncode!=0
    assert '稳定版' in result.stderr
    assert 'systemctl enable' not in events.read_text()
    for kind,content in before.items(): assert (tmp_path/f'etc/{kind}/config.yaml').read_bytes()==content


BASELINES = Path(__file__).parent / 'testdata'


# The gateway installer now installs its missing tools with the shared helper
# (as ordinary native installs do) instead of failing on the first one.
GATEWAY_OLD_NEED = ('need() { command -v "$1" >/dev/null 2>&1 || fail "缺少命令 $1。Debian/Ubuntu: apt-get update && '
                    'apt-get install -y ca-certificates curl iproute2 nftables tar gzip coreutils；Alpine: apk add '
                    'ca-certificates curl iproute2 nftables tar gzip coreutils。安装依赖后重跑。"; }\n')
GATEWAY_DEPENDENCY_CALL = ('# Install missing tools with the system package manager, as ordinary native\n'
                           '# installs do (Alpine lacks nft and ss). The gateway Agent reads `ip -j`, which\n'
                           '# BusyBox ip lacks; OxiDNS cron plugins need the timezone database.\n'
                           'agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo\n')


def with_gateway_dependency_delta(expected):
    assert expected.count(GATEWAY_OLD_NEED) == 1
    helper = without_reviewed_installer_deltas(AGENT_INSTALL_DEPENDENCIES)
    expected = expected.replace(GATEWAY_OLD_NEED, helper, 1)
    start = expected.index('for dependency in ip nft ss tar gzip')
    end = expected.index('[ -c /dev/net/tun ]')
    assert 'apk add --no-cache tzdata' in expected[start:end]
    return expected[:start] + GATEWAY_DEPENDENCY_CALL + expected[end:]


def test_selective_gateway_script_is_byte_identical_to_the_previous_release():
    # Captured from 0ef9e89 with exactly these inputs.
    expected = with_gateway_dependency_delta(
        (BASELINES / 'gateway_install_selective_0ef9e89.sh').read_text(encoding='utf-8'))
    assert without_reviewed_installer_deltas(generate()) == expected
    assert without_reviewed_installer_deltas(generate(full_tun=False)) == expected
    assert 'CONFIGFLOW_INSTALLER_FORWARDING' not in expected


# The ordinary AIO persists IPv4 forwarding itself. Its environment check is
# told so (no manual-repair warnings remain to point at), and the summary
# repeats a forward-filter warning; nothing else moves.
ORDINARY_AIO_FORWARDING_DELTAS = (
    ('cf_step "检查运行环境"\nif cf_environment_check "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json"',
     'cf_step "检查运行环境"\nCONFIGFLOW_INSTALLER_FORWARDING=persist\nexport CONFIGFLOW_INSTALLER_FORWARDING\n'
     'if cf_environment_check "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json"'),
    ('fi\ncf_step_done "运行环境检查通过"\n',
     'fi\nunset CONFIGFLOW_INSTALLER_FORWARDING\ncf_step_done "运行环境检查通过"\n'),
    ('    cf_note forwarding "IPv4 转发已设置持久化，尚未实际重启验证；其他配置或服务覆盖风险及原检查结果见详细日志。"\n',
     '    cf_note forwarding "IPv4 转发已设置持久化，尚未实际重启验证；其他配置或服务仍可能在重启后把它改回 0，'
     '重启后可执行 sysctl net.ipv4.ip_forward 确认仍为 1。"\n'),
    ('确认仍为 1。"\n    cf_fact ipv4_forwarding ',
     '确认仍为 1。"\n'
     '    # The environment check only warns about these; repeat them in the final\n'
     '    # summary because neither stops the install.\n'
     '    if [ -n "${CONFIGFLOW_LOG_FILE:-}" ] && [ -f "$CONFIGFLOW_LOG_FILE" ]; then\n'
     "        if grep -q '^\\[提醒\\] IPv4 转发冲突：' \"$CONFIGFLOW_LOG_FILE\" 2>/dev/null; then\n"
     '            cf_note forwarding_conflict "IPv4 转发冲突：其他 sysctl 设置会在重启后把 net.ipv4.ip_forward 改回 0；'
     '处理方法见上方「IPv4 转发冲突」或完整日志。"\n'
     '        fi\n'
     "        if grep -q '^\\[提醒\\] 转发过滤：' \"$CONFIGFLOW_LOG_FILE\" 2>/dev/null; then\n"
     '            cf_note forward_filter "转发过滤：这台 Linux 的防火墙可能拦截经过 Mihomo TUN 的流量，安装不受影响；'
     '影响范围和放行命令见上方「转发过滤」或完整日志。"\n'
     '        fi\n'
     '    fi\n'
     '    cf_fact ipv4_forwarding '),
    # OpenRC's sysctl service may be in no runlevel (PVE's Alpine LXC template):
    # the persisted forwarding is then added to boot, or a manual step noted.
    ('IPV4_TEMP=""\n\nipv4_failure() {',
     'IPV4_TEMP=""\nIPV4_BOOT_RESULT=""\n\nipv4_failure() {'),
    ('    [ "$(cat /proc/sys/net/ipv4/ip_forward)" = 1 ] || ipv4_failure "最终 IPv4 转发状态不是 1"\n' + '}\n\nsummarize_ipv4_forwarding() {',
     '    [ "$(cat /proc/sys/net/ipv4/ip_forward)" = 1 ] || ipv4_failure "最终 IPv4 转发状态不是 1"\n'
     '    ensure_ipv4_sysctl_boot\n'
     '}\n'
     '\n'
     'ipv4_sysctl_boot_state() {\n'
     "    # Matches the Agent's SysctlBoot: only OpenRC can leave sysctl.d unapplied.\n"
     '    [ "$INIT_SYSTEM" = openrc ] || { echo na; return 0; }\n'
     '    [ -f /etc/init.d/sysctl ] || { echo unavailable; return 0; }\n'
     '    for ipv4_level in sysinit boot default; do\n'
     '        [ -e "/etc/runlevels/$ipv4_level/sysctl" ] && { echo enabled; return 0; }\n'
     '    done\n'
     '    echo disabled\n'
     '}\n'
     '\n'
     'ensure_ipv4_sysctl_boot() {\n'
     "    # PVE's Alpine LXC template has the OpenRC sysctl service in no runlevel, so\n"
     '    # sysctl.d would never apply at boot and forwarding would rely on the Agent\n'
     '    # alone. Forwarding is already on, so a failure only asks for a manual step.\n'
     '    case "$(ipv4_sysctl_boot_state)" in\n'
     '        disabled)\n'
     '            if rc-update add sysctl boot >/dev/null 2>&1 && [ "$(ipv4_sysctl_boot_state)" = enabled ]; then\n'
     '                IPV4_BOOT_RESULT=added\n'
     '            else\n'
     '                IPV4_BOOT_RESULT=failed\n'
     '            fi\n'
     '            ;;\n'
     '        unavailable) IPV4_BOOT_RESULT=unavailable ;;\n'
     '    esac\n'
     '}\n'
     '\n'
     'summarize_ipv4_forwarding() {'),
    ('确认仍为 1。"\n    # The environment check only warns',
     '确认仍为 1。"\n'
     '    case "${IPV4_BOOT_RESULT:-}" in\n'
     '        added) cf_fact ipv4_forwarding_boot "OpenRC 的 sysctl 服务原本不在开机运行级别，已执行 rc-update add sysctl boot，开机时会应用 $IPV4_CONFIG。" ;;\n'
     '        failed) cf_note forwarding_boot "无法自动把 OpenRC 的 sysctl 服务加入开机运行级别，开机时不会应用 $IPV4_CONFIG（目前只靠 Mihomo Agent 启动前检查补设）。请执行：rc-update add sysctl boot" ;;\n'
     '        unavailable) cf_note forwarding_boot "系统没有 OpenRC 的 sysctl 服务，开机时不会应用 $IPV4_CONFIG（目前只靠 Mihomo Agent 启动前检查补设）。请执行：mkdir -p /etc/local.d && printf \'#!/bin/sh\\\\nsysctl -w net.ipv4.ip_forward=1\\\\n\' > /etc/local.d/configflow-ip-forward.start && chmod 755 /etc/local.d/configflow-ip-forward.start && rc-update add local default" ;;\n'
     '    esac\n'
     '    # The environment check only warns'),
)


def test_ordinary_aio_script_is_the_previous_release_plus_intentional_forwarding_context_delta():
    expected = (BASELINES / 'go_aio_install_0ef9e89.sh').read_text(encoding='utf-8')
    for old, new in ORDINARY_AIO_FORWARDING_DELTAS:
        assert expected.count(old) == 1, old
        expected = expected.replace(old, new, 1)
    script = generate_go_aio_install_script('https://cf.test', 'ordinary', agent_ip='192.168.8.3',
                                            binary_download_url='https://cf.test/api/agents/download')
    assert without_reviewed_installer_deltas(script) == expected
    assert script.count('export CONFIGFLOW_INSTALLER_FORWARDING\n') == 1
    assert script.count('unset CONFIGFLOW_INSTALLER_FORWARDING\n') == 1


@pytest.mark.parametrize('kwargs', [
    {'tun_prepare_assume_enabled': True},
    {'tun_prepare_assume_enabled': True, 'manage_tun': False, 'non_interactive': True,
     'verified_agent_path': '$GATEWAY_STAGE/configflow-agent'},
    {'tun_prepare_assume_enabled': 1},
])
def test_assumed_tun_preparation_is_only_for_the_gateway_installer(kwargs):
    with pytest.raises(ValueError):
        generate_go_aio_install_script('https://cf.test', 'ordinary', **kwargs)


def test_full_tun_script_persists_forwarding_and_prepares_tun(tmp_path):
    script = generate(full_tun=True)
    for expected in ('prepare_ipv4_forwarding', 'install_ipv4_forwarding_hook', 'persist_ipv4_forwarding',
                     '-prepare-tun -assume-tun-enabled;', '$3>=40', 'Agent 1.8.40-go 或更新',
                     'if [ -e /opt/configflow-agent/mihomo/linux-gateway.json ] || [ -L /opt/configflow-agent/mihomo/linux-gateway.json ]',
                     '# ConfigFlow Linux gateway installation. Full-TUN router split',
                     '请回到 ConfigFlow 安装向导，继续发布 Mihomo、发布 OxiDNS，再检查转发与 TUN。',
                     # Shared with the selective installer: timezone data and the Agent's -version check.
                     'agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo',
                     '"$GATEWAY_STAGE/configflow-agent" -version >/dev/null',
                     'agent_ensure_dependencies zoneinfo sysctl cmp ln'):
        assert expected in script, expected
    assert 'CONFIGFLOW_FORWARDING_OWNER' not in script
    assert '$3>=25' not in script
    # The inner AIO persists forwarding, so its environment check is told so.
    assert script.count('CONFIGFLOW_INSTALLER_FORWARDING=persist\nexport CONFIGFLOW_INSTALLER_FORWARDING\nif cf_environment_check "$BINARY_TMP"') == 1
    assert script.count('unset CONFIGFLOW_INSTALLER_FORWARDING\n') == 1
    path = tmp_path / 'script.sh'
    path.write_text(script)
    result = subprocess.run(['sh', '-n', path], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    with pytest.raises(ValueError):
        generate(full_tun='yes')


@pytest.mark.parametrize('manager', ['systemd', 'openrc'])
def test_full_tun_install_persists_forwarding_and_prepares_assumed_tun(tmp_path, monkeypatch, manager):
    run, events, _ = host(tmp_path, monkeypatch, manager, full_tun=True)
    result = run()
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    assert (tmp_path / 'ip_forward').read_text() == '1\n'
    assert (tmp_path / 'etc/sysctl.d/99-configflow-aio-forward.conf').read_text() == \
        '# Managed by ConfigFlow Shell AIO IPv4 forwarding v1\nnet.ipv4.ip_forward=1\n'
    hook = tmp_path / 'opt/configflow-agent/mihomo/enable-ipv4-forwarding.sh'
    assert stat.S_IMODE(hook.stat().st_mode) == 0o755
    unit = (tmp_path / ('etc/systemd/system/configflow-agent-mihomo.service' if manager == 'systemd'
                        else 'etc/init.d/configflow-agent-mihomo')).read_text()
    assert f'/bin/sh {hook}' in unit
    assert 'prepare-tun:mihomo -prepare-tun -assume-tun-enabled' in log
    # The bootstrap keeps TUN off until the published configuration takes over.
    assert 'enable: false' in (tmp_path / 'etc/mihomo/config.yaml').read_text()
    assert not (tmp_path / 'opt/configflow-agent/mihomo/linux-gateway.json').exists()


def test_full_tun_install_requires_the_forwarding_agent(tmp_path, monkeypatch):
    run, _, _ = host(tmp_path, monkeypatch, full_tun=True)
    result = run(AGENT_VERSION='1.8.39-go')
    assert result.returncode != 0
    assert '1.8.40-go' in result.stdout + result.stderr
    assert not (tmp_path / 'etc/mihomo').exists()
    assert (tmp_path / 'ip_forward').read_text() == '0\n'


def test_full_tun_install_refuses_a_host_still_on_the_gateway_takeover(tmp_path, monkeypatch):
    run, events, _ = host(tmp_path, monkeypatch, full_tun=True)
    record = tmp_path / 'opt/configflow-agent/mihomo/linux-gateway.json'
    record.parent.mkdir(parents=True)
    record.write_text('{}')
    result = run()
    assert result.returncode != 0
    assert '中撤销接管' in result.stdout + result.stderr
    assert (tmp_path / 'ip_forward').read_text() == '0\n'
    assert not (tmp_path / 'etc/mihomo').exists()
    assert not events.exists() or 'sysctl' not in events.read_text()
