"""Read-only OxiDNS monitoring through an authenticated, registered Agent."""
from datetime import datetime, timezone
import hashlib
import ipaddress
import json
import math
import re
from urllib.parse import urlsplit, urlunsplit

import requests

from .version import compare_versions
from . import oxidns_metrics
from . import oxidns_publication_stats
from .oxidns_dashboard_state import DashboardStateStore, DashboardStateError, DashboardStateConflict

MIN_AGENT_VERSION = '1.7.0-go'
MIN_CLIENTS_AGENT_VERSION = '1.7.1-go'
MIN_DASHBOARD_AGENT_VERSION = '1.8.14-go'
CLIENT_SAMPLE_LIMIT = 10000
MAX_RESPONSE_BYTES = 5 * 1024 * 1024
MAX_INTEGER = 2 ** 53 - 1

# OxiDNS v1.6.1 query_recorder stores Rcode's Display text and matches it
# case-insensitively, without invoking Rcode::from_token for RFC mnemonics.
# Source: svenshi/oxidns v1.6.1 crates/proto/src/types.rs, Rcode::to_str.
RCODE_FILTER_TEXT = {
    'NOERROR': 'No Error',
    'FORMERR': 'Form Error',
    'SERVFAIL': 'Server Failure',
    'NXDOMAIN': 'Non-Existent Domain',
    'NOTIMP': 'Not Implemented',
    'REFUSED': 'Query Refused',
    'YXDOMAIN': 'Name should not exist',
    'YXRRSET': 'RR Set should not exist',
    'NXRRSET': 'RR Set does not exist',
    'NOTAUTH': 'Not authorized',
    'NOTZONE': 'Name not in zone',
    'BADVERS': 'Bad OPT Version',
    'BADSIG': 'TSIG Signature Failure',
    'BADKEY': 'Key not recognized',
    'BADTIME': 'Signature out of time window',
    'BADMODE': 'Bad TKEY mode',
    'BADNAME': 'Duplicate key name',
    'BADALG': 'Algorithm not supported',
    'BADTRUNC': 'Bad truncation',
    'BADCOOKIE': 'Bad server cookie',
}

MESSAGES = {
    'UPGRADE_REQUIRED': '请将此 Agent 升级到 1.7.0-go 或更新版本后使用 DNS 监控。',
    'API_NOT_CONFIGURED': 'OxiDNS 管理 API 未启用。请在 OxiDNS 设置的「API / 日志」中开启，保存后发布到此 Agent。',
    'RECORDER_NOT_CONFIGURED': '查询记录未启用，请在 OxiDNS 设置中开启查询记录并推送配置。',
    'TLS_UNSUPPORTED': '此监控连接暂不支持当前 OxiDNS API 的 TLS 配置。',
    'CORE_UNREACHABLE': '暂时无法连接 OxiDNS 管理 API，请检查核心运行状态。',
    'CORE_AUTH_FAILED': 'OxiDNS 管理 API 认证失败，请检查核心配置中的认证设置。',
    'AGENT_UNREACHABLE': '暂时无法连接 Agent，请检查网络和 Agent 运行状态。',
    'AGENT_AUTH_FAILED': 'Agent 认证失败，请检查注册状态。',
    'INVALID_RESPONSE': '监控数据格式异常，请检查 Agent 和 OxiDNS 版本。',
    'INVALID_QUERY': '查询条件无效，请检查筛选内容。',
    'RECORD_NOT_FOUND': '这条查询记录已不存在，可能已超过保留期限。',
    'DASHBOARD_STATE_ERROR': '无法读取或保存 DNS 面板统计起点，请检查数据目录并刷新面板确认状态。',
    'DASHBOARD_CHANGED': '设备或统计起点已改变，请重新载入 DNS 面板。',
    'SNAPSHOT_CHANGED': '采集期间核心或配置发生变化，请稍后刷新。',
    'CONFIG_UNSUPPORTED': '当前核心 API 配置无法用于监控，请检查监听地址与认证设置。',
}


class MonitorError(Exception):
    def __init__(self, code, status=502, message=None):
        self.code, self.status = code, status
        self.message = message or MESSAGES.get(code, MESSAGES['INVALID_RESPONSE'])
        super().__init__(self.message)


def _upgrade_required(min_version):
    feature = ('客户端 IP 选择与筛选' if min_version == MIN_CLIENTS_AGENT_VERSION else
               'DNS 排行与统计重置' if min_version == MIN_DASHBOARD_AGENT_VERSION else 'DNS 监控')
    return MonitorError('UPGRADE_REQUIRED', 409,
                        f'请将此 Agent 升级到 {min_version} 或更新版本后使用{feature}。')


def _agent(manager, agent_id, min_version=MIN_AGENT_VERSION):
    agent = manager.get_agent_by_id(agent_id)
    if not agent:
        raise MonitorError('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')
    if agent.get('service_type') != 'oxidns':
        raise MonitorError('WRONG_SERVICE', 400, 'DNS 监控仅适用于 OxiDNS Agent。')
    try:
        supported = compare_versions(agent.get('version', '0'), min_version) >= 0
    except (ValueError, TypeError, AttributeError):
        supported = False
    if not supported:
        raise _upgrade_required(min_version)
    return agent


def _base_url(agent):
    return _agent_origin(agent) + '/api/oxidns/'


def _agent_origin(agent):
    """Validated http://host:port of a registered Agent; never taken from request input."""
    host = agent.get('host', '')
    if not isinstance(host, str) or not host or any(c in host for c in '/?#@\\'):
        raise MonitorError('AGENT_UNREACHABLE')
    host = host.removeprefix('[').removesuffix(']')
    try:
        address = ipaddress.ip_address(host)
        host = '[' + str(address) + ']' if address.version == 6 else str(address)
    except ValueError:
        if not re.fullmatch(r'[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?', host):
            raise MonitorError('AGENT_UNREACHABLE')
    port = agent.get('port')
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise MonitorError('AGENT_UNREACHABLE')
    return f'http://{host}:{port}'


def _request(agent, endpoint, params=None, *, min_version=MIN_AGENT_VERSION):
    # Endpoints are chosen by the functions below, never taken from a URL argument.
    url = _base_url(agent) + endpoint
    try:
        headers = {'Authorization': 'Bearer ' + agent['token'], 'Accept': 'application/json'}
        if endpoint == 'queries':
            # New Agents use the core's SSE feed to enrich list rows. Old Agents
            # ignore this header and keep the existing detail-query fallback.
            headers['X-ConfigFlow-Query-Sources'] = '1'
        with requests.Session() as session:
            session.trust_env = False
            with session.get(url, params=params or {}, headers=headers,
                             timeout=(3, 12), allow_redirects=False, stream=True) as response:
                if response.status_code in (401, 403):
                    raise MonitorError('AGENT_AUTH_FAILED')
                if 300 <= response.status_code < 400:
                    raise MonitorError('INVALID_RESPONSE')
                chunks, size = [], 0
                for chunk in response.iter_content(64 * 1024):
                    size += len(chunk)
                    if size > MAX_RESPONSE_BYTES:
                        raise MonitorError('INVALID_RESPONSE')
                    chunks.append(chunk)
                try:
                    data = json.loads(b''.join(chunks))
                except (ValueError, UnicodeError):
                    if response.status_code == 404:
                        raise _upgrade_required(min_version) from None
                    raise MonitorError('INVALID_RESPONSE') from None
                if not isinstance(data, dict):
                    raise MonitorError('INVALID_RESPONSE')
                if response.status_code >= 400 or data.get('success') is False or data.get('ok') is False:
                    code = data.get('code')
                    if code not in MESSAGES:
                        code = 'UPGRADE_REQUIRED' if response.status_code == 404 else 'CORE_UNREACHABLE'
                    status = 404 if code == 'RECORD_NOT_FOUND' else 400 if code == 'INVALID_QUERY' else 409 if code in (
                        'UPGRADE_REQUIRED', 'API_NOT_CONFIGURED', 'RECORDER_NOT_CONFIGURED',
                        'TLS_UNSUPPORTED', 'CONFIG_UNSUPPORTED', 'SNAPSHOT_CHANGED') else 502
                    if code == 'UPGRADE_REQUIRED':
                        raise _upgrade_required(min_version)
                    raise MonitorError(code, status)
                return data
    except (requests.RequestException, KeyError, TypeError, ValueError):
        # Exceptions and upstream error bodies can contain credentials or query data.
        raise MonitorError('AGENT_UNREACHABLE') from None


def _number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    # Bound integers before math.isfinite converts them to float. JSON numbers
    # outside the frontend's exact integer range cannot be represented safely.
    if value < 0 or value > MAX_INTEGER:
        return None
    return value if math.isfinite(value) else None


def _text(value, limit=512):
    return value[:limit] if isinstance(value, str) else ''


def _dashboard_params(args=None):
    if args is None:
        return {}
    result = {}
    for key in args:
        values = args.getlist(key)
        if key != 'recorder' or len(values) != 1 or not _valid_recorder_tag(values[0]):
            raise MonitorError('INVALID_QUERY', 400)
        result[key] = values[0]
    return result


def _target(agent):
    # The digest detects registered target or token changes without storing secrets.
    identity = [agent.get(key) for key in ('id', 'host', 'port', 'token', 'service_type')]
    return hashlib.sha256(json.dumps(identity, separators=(',', ':')).encode()).hexdigest()


def _display_upstream(value):
    value = _text(value, 2048)
    if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
        return ''
    if '://' in value:
        try:
            parsed = urlsplit(value)
            host = parsed.hostname
            if not host:
                return '上游服务器'
            if ':' in host:
                host = '[' + host + ']'
            port = parsed.port
            return urlunsplit((parsed.scheme, host + (':' + str(port) if port is not None else ''),
                               parsed.path, '', ''))
        except ValueError:
            return '上游服务器'
    return value.split('?', 1)[0].rsplit('@', 1)[-1]


def _public_upstreams(rows, labels):
    addresses = {(item['plugin_tag'], item['upstream']): item['address'] for item in labels}
    return [{**row, 'id': hashlib.sha256(json.dumps([row.get('tag'), row.get('upstream')],
                                                   separators=(',', ':')).encode()).hexdigest(),
             'tag': _display_upstream(row.get('tag')),
             'upstream': _display_upstream(row.get('upstream')),
             'address': _display_upstream(addresses.get((row.get('tag'), row.get('upstream')), ''))}
            for row in rows]


def _overview_snapshot(agent, dashboard):
    data = _request(agent, 'overview', min_version=MIN_DASHBOARD_AGENT_VERSION if dashboard else MIN_AGENT_VERSION)
    health, capabilities, text = data.get('health'), data.get('capabilities'), data.get('metrics')
    if not isinstance(health, dict) or not isinstance(capabilities, dict) or not isinstance(text, str):
        raise MonitorError('INVALID_RESPONSE')
    recorders = capabilities.get('query_recorders', [])
    if not isinstance(recorders, list) or any(not isinstance(x, str) for x in recorders) or len(recorders) > 64:
        raise MonitorError('INVALID_RESPONSE')
    try:
        observations = oxidns_metrics.parse_metrics(text)
    except (ValueError, TypeError):
        raise MonitorError('INVALID_RESPONSE') from None
    core = {'version': _text(health.get('version'), 64), 'status': _text(health.get('status'), 32),
            'uptime_ms': _number(health.get('uptime_ms')), 'instance_id': _text(health.get('instance_id'), 128)}
    labels = data.get('upstream_labels', [])
    if not isinstance(labels, list) or len(labels) > CLIENT_SAMPLE_LIMIT:
        raise MonitorError('INVALID_RESPONSE')
    seen = set()
    for item in labels:
        if (not isinstance(item, dict) or any(not isinstance(item.get(key), str) or len(item[key]) > 16_384
                                             for key in ('plugin_tag', 'upstream', 'address'))):
            raise MonitorError('INVALID_RESPONSE')
        identity = (item['plugin_tag'], item['upstream'])
        if identity in seen:
            raise MonitorError('INVALID_RESPONSE')
        seen.add(identity)
    if dashboard and (any(not _valid_recorder_tag(tag) for tag in recorders)
            or not isinstance(health.get('instance_id'), str)
            or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,128}', health['instance_id'])
            or type(data.get('collected_at_ms')) is not int or not 0 <= data['collected_at_ms'] <= 253402300799999
            or not isinstance(data.get('source_revision'), str)
            or not re.fullmatch(r'[0-9a-f]{64}', data['source_revision'])
            or not isinstance(data.get('runtime_revision'), str)
            or not re.fullmatch(r'[0-9a-f]{64}', data['runtime_revision'])):
        raise MonitorError('INVALID_RESPONSE')
    return data, core, recorders, observations, labels


# Labels of the agent's blocked-ranking reason; older agents send none.
BLOCKED_REASONS = {'standard_adguard': '标准 AdGuard 拦截路径', 'rule_dns': '规则拦截（拒绝/黑洞）',
                   'standard_adguard_rule_dns': '标准 AdGuard 与规则拦截'}
# The agent ranks at most this many block matchers (oxRankingBlockLimit).
BLOCK_RANKING_MATCHER_LIMIT = 32


def _rankings_unavailable(code, **fields):
    reasons = {
        'upgrade_required': f'请将 Agent 升级到 {MIN_DASHBOARD_AGENT_VERSION} 或更新版本以查看排行并重置统计。',
        'recorder_not_configured': '未启用查询记录，无法计算域名和客户端排行。',
        'unsupported_configuration': '当前配置未包含可确认的拦截路径（标准 AdGuard 或规则「拒绝/黑洞」）。',
        'too_many_block_rules': (f'拦截规则分组超过 {BLOCK_RANKING_MATCHER_LIMIT} 个，暂不统计被拦截域名排行；'
                                 '请合并拒绝/黑洞规则后再查看。'),
        'snapshot_changed': '采集期间核心或配置发生变化，请稍后刷新。',
    }
    reason = reasons.get(code, MESSAGES.get(code.upper(), '暂时无法获取排行，请稍后刷新。'))
    return {'available': False, 'success': False, 'code': code, 'reason': reason,
            'sample_size': 0, 'rows': [], 'at_limit': False, **fields}


def _ranking_section(value, kind):
    if not isinstance(value, dict):
        raise MonitorError('INVALID_RESPONSE')
    sample, rows, at_limit = value.get('sample_size'), value.get('rows'), value.get('at_limit')
    if (type(sample) is not int or not 0 <= sample <= CLIENT_SAMPLE_LIMIT
            or not isinstance(rows, list) or len(rows) > 10
            or type(at_limit) is not bool or at_limit != (sample == CLIENT_SAMPLE_LIMIT)):
        raise MonitorError('INVALID_RESPONSE')
    result, seen, total = [], set(), 0
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get('key'), str):
            raise MonitorError('INVALID_RESPONSE')
        key, count = row['key'], row.get('count')
        if (not key or len(key) > 1024 or any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in key)
                or key in seen or type(count) is not int
                or not 0 < count <= sample * (1 if kind == 'clients' else 65535)):
            raise MonitorError('INVALID_RESPONSE')
        if kind == 'clients':
            try:
                if '%' in key:
                    raise ValueError
                ipaddress.ip_address(key)
            except ValueError:
                raise MonitorError('INVALID_RESPONSE') from None
        seen.add(key)
        total += count
        result.append({'key': key, 'count': count, 'share': count / sample})
    # A top-N list can omit the tail. Domain counts expand all questions in a
    # DNS packet, so their sum/share can legitimately exceed the record sample.
    if total > sample * (1 if kind == 'clients' else 65535):
        raise MonitorError('INVALID_RESPONSE')
    result.sort(key=lambda row: (-row['count'], row['key']))
    return {'sample_size': sample, 'rows': result, 'at_limit': at_limit}


def _dashboard_rankings(agent, params, snapshot, state, recorders):
    bounds = {'since_ms': state['started_at_ms'], 'until_ms': snapshot['collected_at_ms']}
    if params.get('recorder') and params['recorder'] not in recorders:
        raise MonitorError('INVALID_QUERY', 400)
    if not recorders:
        return _rankings_unavailable('recorder_not_configured', **bounds)
    data = _request(agent, 'rankings', {**params, **{key: str(value) for key, value in bounds.items()}},
                    min_version=MIN_DASHBOARD_AGENT_VERSION)
    recorder = data.get('recorder')
    if (data.get('success') is not True or not _valid_recorder_tag(recorder) or recorder not in recorders
            or params.get('recorder', recorder) != recorder
            or type(data.get('collected_at_ms')) is not int or not 0 <= data['collected_at_ms'] <= 253402300799999
            or data.get('sample_limit') != CLIENT_SAMPLE_LIMIT or type(data.get('sample_limit')) is not int
            or data.get('retention_days') is not None and (type(data['retention_days']) is not int
                                                         or not 0 <= data['retention_days'] <= 36500)):
        raise MonitorError('INVALID_RESPONSE')
    if (data.get('instance_id') != state['instance_id'] or data.get('source_revision') != state['source_revision']
            or data.get('runtime_revision') != state['runtime_revision']
            or data['collected_at_ms'] < snapshot['collected_at_ms']):
        raise MonitorError('SNAPSHOT_CHANGED', 409)
    blocked = data.get('blocked')
    if not isinstance(blocked, dict) or type(blocked.get('available')) is not bool:
        raise MonitorError('INVALID_RESPONSE')
    if blocked['available']:
        code = blocked.get('reason', 'standard_adguard')
        if not isinstance(code, str) or not re.fullmatch(r'[a-z_]{1,64}', code):
            raise MonitorError('INVALID_RESPONSE')
        blocked_result = {'available': True, 'code': code, 'reason': BLOCKED_REASONS.get(code, '拦截路径'),
                          **_ranking_section(blocked, 'domains')}
        if 'matcher_tag' in blocked:
            if not _valid_recorder_tag(blocked['matcher_tag']):
                raise MonitorError('INVALID_RESPONSE')
            blocked_result['matcher_tag'] = blocked['matcher_tag']
        if 'matcher_tags' in blocked:
            tags = blocked['matcher_tags']
            if (not isinstance(tags, list) or not 0 < len(tags) <= BLOCK_RANKING_MATCHER_LIMIT
                    or not all(_valid_recorder_tag(tag) for tag in tags)):
                raise MonitorError('INVALID_RESPONSE')
            blocked_result['matcher_tags'] = list(tags)
    else:
        reason = blocked.get('reason')
        if not isinstance(reason, str) or not re.fullmatch(r'[a-z_]{1,64}', reason):
            reason = 'unsupported_configuration'
        blocked_result = _rankings_unavailable(reason)
    return {'available': True, 'success': True, 'recorder': recorder, 'collected_at_ms': data['collected_at_ms'],
            'instance_id': data['instance_id'], 'source_revision': data['source_revision'],
            'runtime_revision': data['runtime_revision'],
            'retention_days': data.get('retention_days'), 'sample_limit': CLIENT_SAMPLE_LIMIT,
            **bounds, 'clients': _ranking_section(data.get('clients'), 'clients'),
            'domains': _ranking_section(data.get('domains'), 'domains'), 'blocked': blocked_result}


def _verify_target(manager, agent_id, expected):
    current = manager.get_agent_by_id(agent_id)
    if current is None or _target(current) != expected:
        raise MonitorError('DASHBOARD_CHANGED', 409)


def _dashboard_overview(manager, agent_id, params, *, manual=False):
    agent = _agent(manager, agent_id, MIN_DASHBOARD_AGENT_VERSION if manual else MIN_AGENT_VERSION)
    dashboard = compare_versions(agent.get('version', '0'), MIN_DASHBOARD_AGENT_VERSION) >= 0
    if not dashboard:
        _, core, recorders, observations, labels = _overview_snapshot(agent, False)
        summary = oxidns_metrics.summarize_observations(observations)
        summary['upstreams'] = _public_upstreams(summary.get('upstreams', []), labels)
        return {'success': True, 'agent': {key: agent.get(key) for key in (
                    'id', 'name', 'host', 'profile_id', 'version', 'status')},
                'collected_at': datetime.now(timezone.utc).isoformat(), 'core': core,
                'recorders': recorders, **summary, 'statistics': None,
                'rankings': _rankings_unavailable('upgrade_required')}
    store = DashboardStateStore(manager.repository.data_dir)
    try:
        previous = store.read(agent_id)
        snapshot, core, recorders, observations, labels = _overview_snapshot(agent, True)
        target = _target(agent)
        _verify_target(manager, agent_id, target)
        state = oxidns_publication_stats.advance(previous, snapshot, core, observations, target,
                                                manager.get_agent_by_id(agent_id), manual=manual)
        if snapshot['collected_at_ms'] < state['started_at_ms']:
            raise MonitorError('SNAPSHOT_CHANGED', 409)
        adjusted, partial = oxidns_publication_stats.totals(state, observations)
        summary = oxidns_metrics.summarize_observations(adjusted)
        summary['upstreams'] = _public_upstreams(summary.get('upstreams', []), labels)
        try:
            # A pushed configuration may remove the UI's selected recorder.
            # GET must return the new list so viewers can recover automatically;
            # a manual reset remains strict about its explicitly selected target.
            effective_params = params if manual or params.get('recorder') in recorders else {}
            rankings = _dashboard_rankings(agent, effective_params, snapshot, state, recorders)
        except MonitorError as error:
            if manual or error.code == 'INVALID_QUERY':
                raise
            rankings = _rankings_unavailable(error.code.lower())
        store.compare_and_set(agent_id, previous['generation'] if previous else None,
                              state if state != previous else None,
                              verify=lambda: _verify_target(manager, agent_id, target))
        return {'success': True, 'agent': {key: agent.get(key) for key in (
                    'id', 'name', 'host', 'profile_id', 'version', 'status')},
                'collected_at': datetime.fromtimestamp(snapshot['collected_at_ms'] / 1000, timezone.utc).isoformat(),
                'core': core, 'recorders': recorders, **summary,
                'statistics': {'generation': state['generation'], 'started_at_ms': state['started_at_ms'],
                               'reason': state['reason'], 'partial': partial,
                               **({'publication_count': state['publication_count']} if state.get('publication_count') else {})},
                'rankings': rankings}
    except oxidns_publication_stats.PublicationPending:
        raise MonitorError('SNAPSHOT_CHANGED', 409) from None
    except DashboardStateError:
        raise MonitorError('DASHBOARD_STATE_ERROR', 500) from None
    except DashboardStateConflict:
        raise MonitorError('DASHBOARD_CHANGED', 409) from None


def overview(manager, agent_id, args=None):
    return _dashboard_overview(manager, agent_id, _dashboard_params(args))


def reset_dashboard(manager, agent_id, body):
    if (not isinstance(body, dict) or set(body) - {'recorder'}
            or 'recorder' in body and not _valid_recorder_tag(body['recorder'])):
        raise MonitorError('INVALID_QUERY', 400)
    return _dashboard_overview(manager, agent_id, body, manual=True)



def query_params(args, detail=False):
    allowed = {'recorder'} if detail else {'recorder', 'limit', 'cursor', 'qname', 'qtype', 'client_ip',
                'rcode', 'status', 'since_ms', 'until_ms', 'matcher_tag'}
    result = {}
    for key in args:
        values = args.getlist(key)
        if key not in allowed or len(values) != 1:
            raise MonitorError('INVALID_QUERY', 400)
        value = values[0]
        if not isinstance(value, str) or len(value.encode()) > 1024 or any(ord(c) < 32 for c in value):
            raise MonitorError('INVALID_QUERY', 400)
        value = value.strip()
        if not value:
            continue
        if key in ('limit', 'since_ms', 'until_ms'):
            if not re.fullmatch(r'[0-9]{1,16}', value):
                raise MonitorError('INVALID_QUERY', 400)
            n = int(value)
            if n > MAX_INTEGER or key == 'limit' and not 1 <= n <= 100:
                raise MonitorError('INVALID_QUERY', 400)
            value = str(n)
        elif key == 'cursor':
            if not re.fullmatch(r'[0-9]{1,16}:[1-9][0-9]{0,15}', value) or any(int(x) > MAX_INTEGER for x in value.split(':')):
                raise MonitorError('INVALID_QUERY', 400)
        elif key == 'status' and value not in ('all', 'error', 'has_response', 'no_response'):
            raise MonitorError('INVALID_QUERY', 400)
        elif key in ('recorder', 'matcher_tag') and (len(value) > 256 or any(c in value for c in '/?#\\')):
            raise MonitorError('INVALID_QUERY', 400)
        elif key == 'client_ip':
            try:
                if '%' in value:
                    raise ValueError
                value = str(ipaddress.ip_address(value))
            except ValueError:
                raise MonitorError('INVALID_QUERY', 400) from None
        elif key == 'qtype':
            if not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', value):
                raise MonitorError('INVALID_QUERY', 400)
            value = value.upper()
        elif key == 'rcode':
            if not re.fullmatch(r'[A-Za-z0-9 _-]{1,64}', value):
                raise MonitorError('INVALID_QUERY', 400)
            value = RCODE_FILTER_TEXT.get(value.upper(), value)
        result[key] = value
    if 'since_ms' in result and 'until_ms' in result and int(result['since_ms']) > int(result['until_ms']):
        raise MonitorError('INVALID_QUERY', 400)
    if not detail:
        result.setdefault('limit', '50')
    return result


def _record(value, detail=False):
    if not isinstance(value, dict) or type(value.get('id')) is not int or not 0 < value['id'] <= MAX_INTEGER:
        raise MonitorError('INVALID_RESPONSE')
    fields = ('id', 'created_at_ms', 'elapsed_ms', 'client_ip', 'questions_json', 'error', 'has_response',
              'rcode', 'answer_count', 'authority_count', 'additional_count', 'answers_json',
              'authorities_json', 'additionals_json', 'signature_json', 'request_id', 'req_rd', 'req_cd',
              'req_ad', 'req_opcode', 'resp_aa', 'resp_tc', 'resp_ra', 'resp_ad', 'resp_cd')
    record = {k: value[k] for k in fields if k in value}
    for key in ('created_at_ms', 'elapsed_ms'):
        if _number(record.get(key)) is None:
            raise MonitorError('INVALID_RESPONSE')
    if not isinstance(record.get('questions_json'), list) or not isinstance(record.get('answers_json'), list):
        raise MonitorError('INVALID_RESPONSE')
    if detail:
        if not isinstance(value.get('steps'), list):
            raise MonitorError('INVALID_RESPONSE')
        record['steps'] = value['steps']
    source = value.get('resolution_source')
    if isinstance(source, str) and source in {
            'local', 'remote', 'fallback', 'pinned', 'cache', 'hosts', 'blocked', 'rejected',
            'unknown', 'unrecorded', 'error', 'no_response', 'ipv4_preferred'}:
        record['resolution_source'] = source
    return record


def queries(manager, agent_id, args):
    params = query_params(args)
    min_version = MIN_CLIENTS_AGENT_VERSION if params.get('client_ip') else MIN_AGENT_VERSION
    agent = _agent(manager, agent_id, min_version)
    data = _request(agent, 'queries', params, min_version=min_version)
    records = data.get('records')
    cursor = data.get('next_cursor')
    if data.get('ok') is not True or not isinstance(records, list) or len(records) > 100:
        raise MonitorError('INVALID_RESPONSE')
    if cursor is not None and (not isinstance(cursor, str) or not re.fullmatch(r'[0-9]{1,16}:[1-9][0-9]{0,15}', cursor)):
        raise MonitorError('INVALID_RESPONSE')
    return {'success': True, 'recorder': params.get('recorder', ''),
            'records': [_record(row) for row in records], 'next_cursor': cursor}


def query(manager, agent_id, record_id, args):
    params = query_params(args, detail=True)
    if not re.fullmatch(r'[1-9][0-9]{0,15}', record_id) or int(record_id) > MAX_INTEGER:
        raise MonitorError('INVALID_QUERY', 400)
    agent = _agent(manager, agent_id)
    data = _request(agent, 'queries/' + record_id, params)
    if data.get('ok') is not True:
        raise MonitorError('INVALID_RESPONSE')
    return {'success': True, 'recorder': params.get('recorder', ''), 'record': _record(data.get('record'), detail=True)}


def client_params(args):
    """Client choices are independent of the current record filters."""
    allowed = {'recorder', 'since_ms', 'until_ms', 'search'}
    result = {}
    for key in args:
        values = args.getlist(key)
        if key not in allowed or len(values) != 1 or not isinstance(values[0], str):
            raise MonitorError('INVALID_QUERY', 400)
        value = values[0]
        try:
            encoded = value.encode('utf-8')
        except UnicodeError:
            raise MonitorError('INVALID_QUERY', 400) from None
        if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
            raise MonitorError('INVALID_QUERY', 400)
        if key == 'search':
            if len(encoded) > 64:
                raise MonitorError('INVALID_QUERY', 400)
        elif key == 'recorder':
            if not _valid_recorder_tag(value):
                raise MonitorError('INVALID_QUERY', 400)
        elif not re.fullmatch(r'[0-9]{1,16}', value) or int(value) > MAX_INTEGER:
            raise MonitorError('INVALID_QUERY', 400)
        value = value.strip()
        if value:
            result[key] = value
    if 'since_ms' in result and 'until_ms' in result and int(result['since_ms']) > int(result['until_ms']):
        raise MonitorError('INVALID_QUERY', 400)
    return result


def _valid_recorder_tag(value):
    return isinstance(value, str) and 0 < len(value) <= 64 and re.fullmatch(
        r'[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*', value) is not None


def clients(manager, agent_id, args):
    params = client_params(args)
    agent = _agent(manager, agent_id, MIN_CLIENTS_AGENT_VERSION)
    data = _request(agent, 'clients', params, min_version=MIN_CLIENTS_AGENT_VERSION)
    recorder, rows, sample = data.get('recorder'), data.get('rows'), data.get('sample_size')
    if (data.get('ok') is not True or not _valid_recorder_tag(recorder)
            or params.get('recorder', recorder) != recorder
            or type(sample) is not int or not 0 <= sample <= CLIENT_SAMPLE_LIMIT
            or not isinstance(rows, list) or len(rows) > CLIENT_SAMPLE_LIMIT):
        raise MonitorError('INVALID_RESPONSE')
    result, seen, total = [], set(), 0
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get('key'), str):
            raise MonitorError('INVALID_RESPONSE')
        ip, count = row['key'], row.get('count')
        try:
            if '%' in ip:
                raise ValueError
            ipaddress.ip_address(ip)
        except ValueError:
            raise MonitorError('INVALID_RESPONSE') from None
        if ip in seen or type(count) is not int or not 0 < count <= sample:
            raise MonitorError('INVALID_RESPONSE')
        seen.add(ip)
        total += count
        result.append({'ip': ip, 'count': count})
    if total != sample:
        raise MonitorError('INVALID_RESPONSE')
    return {'success': True, 'recorder': recorder, 'clients': result,
            'sample_size': sample, 'sample_limited': sample == CLIENT_SAMPLE_LIMIT}
