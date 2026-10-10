"""域名发现：把各 Agent 的流量汇总合并成按主域分组的待处理列表。"""

from datetime import datetime, timedelta
from functools import lru_cache
from typing import Any, Callable, Dict, Iterable, List, Optional, Tuple

from backend.agents.traffic_store import split_route_key

# mihomo 兜底规则在连接与日志里的名字
MATCH_RULE = 'Match'
# 直连失败判定：失败占比与最少失败次数
FAILING_RATIO = 0.5
FAILING_MIN_FAILS = 3

VIEWS = ('uncovered', 'failing', 'all', 'ignored')
TARGETS = ('direct', 'proxy')
APPLY_RULE_TYPES = ('DOMAIN-SUFFIX', 'DOMAIN')


@lru_cache(maxsize=1)
def _extractor():
    import tldextract
    # 只用包内自带的公共后缀列表，不联网、不写缓存
    return tldextract.TLDExtract(suffix_list_urls=(), cache_dir=None)


def registrable_domain(host: str) -> str:
    """a.b.example.co.uk → example.co.uk；无法识别后缀时原样返回。"""
    result = _extractor()(host)
    if result.domain and result.suffix:
        return f'{result.domain}.{result.suffix}'
    return host


def _merge_stats(target: Dict[str, Any], stats: Dict[str, Any]):
    for field in ('conns', 'fails', 'zero_dl', 'up', 'down'):
        target[field] = target.get(field, 0) + stats.get(field, 0)
    kinds = target.setdefault('fail_kinds', {})
    for kind, value in stats.get('fail_kinds', {}).items():
        kinds[kind] = kinds.get(kind, 0) + value


def is_failing(direct_conns: int, direct_fails: int) -> bool:
    """失败的拨号不会出现在连接列表里，所以占比按「成功 + 失败」计算。"""
    attempts = direct_conns + direct_fails
    return direct_fails >= FAILING_MIN_FAILS and attempts > 0 and direct_fails / attempts >= FAILING_RATIO


def is_ignored(domain: str, host: str, ignored: Iterable[str]) -> bool:
    for value in ignored:
        if host == value or host.endswith('.' + value) or domain == value:
            return True
    return False


def summarize(agent_reports: List[Tuple[Dict[str, Any], Dict[str, Any]]], *, days: int,
              ignored: Iterable[str] = (), match_rule: Optional[Callable[[str], Optional[dict]]] = None,
              view: str = 'uncovered', agent_id: Optional[str] = None,
              now: Optional[datetime] = None) -> Dict[str, Any]:
    """合并 (agent, store_data) 列表，返回按主域分组并归类后的结果。"""
    now = now or datetime.now()
    oldest = (now - timedelta(days=max(days, 1) - 1)).date().isoformat()
    ignored = [value.lower() for value in ignored]
    domains: Dict[str, Dict[str, Any]] = {}
    agents = []
    ip_only = 0
    host_conns = 0

    for agent, data in agent_reports:
        status = data.get('status') or {}
        agents.append({
            'id': agent['id'],
            'name': agent.get('name', ''),
            'status': status.get('value') or 'no_data',
            'last_report': status.get('at'),
            'mihomo_version': status.get('mihomo_version', ''),
        })
        if agent_id and agent['id'] != agent_id:
            continue
        for day, bucket in data.get('days', {}).items():
            if day < oldest:
                continue
            ip_only += bucket.get('ip_only_conns', 0)
            for host, entry in bucket.get('hosts', {}).items():
                domain = registrable_domain(host)
                group = domains.setdefault(domain, {'domain': domain, 'hosts': {}, 'routes': {}, 'agents': set()})
                group['agents'].add(agent.get('name') or agent['id'])
                host_item = group['hosts'].setdefault(host, {
                    'host': host, 'first_seen': entry['first_seen'], 'last_seen': entry['last_seen'],
                    'uncovered': False, 'direct_conns': 0, 'direct_fails': 0, 'ports': {}, 'agent_ids': [],
                })
                if agent['id'] not in host_item['agent_ids']:
                    host_item['agent_ids'].append(agent['id'])
                for port, weight in entry.get('ports', {}).items():
                    host_item['ports'][port] = host_item['ports'].get(port, 0) + weight
                host_item['first_seen'] = min(host_item['first_seen'], entry['first_seen'])
                host_item['last_seen'] = max(host_item['last_seen'], entry['last_seen'])
                for key, stats in entry.get('by_route', {}).items():
                    route = split_route_key(key)
                    _merge_stats(host_item, stats)
                    _merge_stats(group['routes'].setdefault(key, route), stats)
                    host_conns += stats.get('conns', 0)
                    if route['rule'] == MATCH_RULE:
                        host_item['uncovered'] = True
                    if route['outlet'] == 'direct':
                        host_item['direct_conns'] += stats.get('conns', 0)
                        host_item['direct_fails'] += stats.get('fails', 0)

    items = []
    for domain, group in domains.items():
        hosts = sorted(group['hosts'].values(), key=lambda item: (item.get('fails', 0), item.get('conns', 0)), reverse=True)
        totals: Dict[str, Any] = {}
        for host_item in hosts:
            ports = host_item.pop('ports')
            host_item['port'] = int(max(ports, key=ports.get)) if ports else 443
            _merge_stats(totals, host_item)
            host_item['failing'] = is_failing(host_item['direct_conns'], host_item['direct_fails'])
            host_item['ignored'] = is_ignored(domain, host_item['host'], ignored)
            host_item['pending_rule'] = None
        uncovered_hosts = [item for item in hosts if item['uncovered'] and not item['ignored']]
        # 只对仍落到兜底规则的子域查当前规则：全部已被规则覆盖说明规则加了但还没部署
        if match_rule and uncovered_hosts:
            for host_item in uncovered_hosts:
                host_item['pending_rule'] = match_rule(host_item['host'])
        tags = []
        if uncovered_hosts:
            if all(item['pending_rule'] for item in uncovered_hosts):
                tags.append('pending')
            else:
                tags.append('uncovered')
        if any(item['failing'] and not item['ignored'] for item in hosts):
            tags.append('failing')
        if all(item['ignored'] for item in hosts):
            tags = ['ignored']
        pending = next((item['pending_rule'] for item in uncovered_hosts if item['pending_rule']), None)
        items.append({
            'domain': domain,
            'tags': tags,
            'conns': totals.get('conns', 0),
            'fails': totals.get('fails', 0),
            'fail_kinds': totals.get('fail_kinds', {}),
            'zero_dl': totals.get('zero_dl', 0),
            'up': totals.get('up', 0),
            'down': totals.get('down', 0),
            'routes': sorted(group['routes'].values(), key=lambda route: route.get('conns', 0) + route.get('fails', 0), reverse=True),
            'hosts': hosts,
            'agents': sorted(group['agents']),
            'first_seen': min(item['first_seen'] for item in hosts),
            'last_seen': max(item['last_seen'] for item in hosts),
            'pending_rule': pending,
        })

    if view == 'ignored':
        items = [item for item in items if 'ignored' in item['tags']]
    elif view == 'all':
        items = [item for item in items if 'ignored' not in item['tags']]
    elif view == 'failing':
        items = [item for item in items if 'failing' in item['tags']]
    else:
        items = [item for item in items if 'uncovered' in item['tags'] or 'pending' in item['tags']]
    items.sort(key=lambda item: ('failing' in item['tags'], item['fails'], item['conns']), reverse=True)

    total = host_conns + ip_only
    return {
        'agents': agents,
        'sniff_coverage': round(host_conns / total, 4) if total else None,
        'items': items,
    }


def format_ruleset_line(behavior: str, rule_type: str, value: str) -> str:
    """把一条域名规则写成目标规则集所用的格式。"""
    if behavior == 'classical':
        return f'{rule_type},{value}'
    return f'+.{value}' if rule_type == 'DOMAIN-SUFFIX' else value
