"""域名发现：Agent 上报的 mihomo 流量汇总的校验与存储。

每个 Agent 一个 JSON 文件，按天分桶，只保存域名维度的统计数，
不保存 URL 路径、客户端 IP 或进程信息。
"""

import copy
import json
import os
import threading
from datetime import datetime, timedelta
from typing import Any, Dict, Optional

from backend.utils.rule_matcher import is_valid_domain, is_valid_ip

REPORT_SCHEMA = 1
REPORT_MAX_CONTENT_LENGTH = 256 * 1024
REPORT_MAX_ITEMS = 2000
RETENTION_DAYS = 7
MAX_HOSTS_PER_DAY = 5000

REPORT_STATUSES = frozenset({'running', 'no_controller', 'auth_failed', 'unreachable'})
OUTLETS = frozenset({'direct', 'proxy', 'reject'})
FAIL_KINDS = frozenset({'timeout', 'reset', 'refused', 'eof', 'dns', 'other'})

_REPORT_FIELDS = frozenset({
    'schema', 'status', 'mihomo_version', 'window_start', 'window_end',
    'ip_only_conns', 'dropped', 'items',
})
_ITEM_FIELDS = frozenset({
    'host', 'port', 'network', 'rule', 'rule_payload', 'policy', 'outlet',
    'conns', 'fails', 'fail_kinds', 'zero_dl', 'up', 'down',
})
_COUNT_FIELDS = ('conns', 'fails', 'zero_dl', 'up', 'down')
_MAX_COUNT = 2 ** 53
_ROUTE_SEPARATOR = '\t'
# 存储实例按请求创建，锁必须是模块级的才能串行化同一进程内的读改写
_LOCK = threading.Lock()


def _bounded_text(value, max_length, *, required=False):
    if value is None:
        return not required
    if not isinstance(value, str) or len(value) > max_length:
        return False
    if required and not value:
        return False
    return not any(ord(char) < 32 or ord(char) == 127 for char in value)


def _count(value):
    return isinstance(value, int) and not isinstance(value, bool) and 0 <= value <= _MAX_COUNT


def _valid_item(item):
    if not isinstance(item, dict) or set(item) - _ITEM_FIELDS:
        return False
    host = item.get('host')
    if not isinstance(host, str) or host != host.lower() or is_valid_ip(host) or not is_valid_domain(host):
        return False
    port = item.get('port', 0)
    if not isinstance(port, int) or isinstance(port, bool) or not 0 <= port <= 65535:
        return False
    if item.get('network', 'tcp') not in ('tcp', 'udp'):
        return False
    if item.get('outlet') not in OUTLETS:
        return False
    # 全局 / 直连模式、或经指定了 proxy 的入站时，Mihomo 不走规则匹配，rule 为空
    if not (_bounded_text(item.get('rule'), 64)
            and _bounded_text(item.get('rule_payload'), 256)
            and _bounded_text(item.get('policy'), 128, required=True)):
        return False
    if any(not _count(item.get(field, 0)) for field in _COUNT_FIELDS):
        return False
    fail_kinds = item.get('fail_kinds', {})
    if not isinstance(fail_kinds, dict) or set(fail_kinds) - FAIL_KINDS:
        return False
    return all(_count(value) for value in fail_kinds.values())


def clean_report(payload) -> Optional[Dict[str, Any]]:
    """校验 Agent 上报体。

    顶层字段越界时整体拒收（返回 None）；单条明细越界只丢弃这一条并计入 dropped，
    避免一条异常连接（如超长的逻辑规则）让整个 5 分钟窗口的数据都丢掉。
    """
    if not isinstance(payload, dict) or set(payload) - _REPORT_FIELDS:
        return None
    if payload.get('schema') != REPORT_SCHEMA or payload.get('status') not in REPORT_STATUSES:
        return None
    for field in ('mihomo_version', 'window_start', 'window_end'):
        if not _bounded_text(payload.get(field), 64):
            return None
    for field in ('ip_only_conns', 'dropped'):
        if not _count(payload.get(field, 0)):
            return None
    items = payload.get('items', [])
    if not isinstance(items, list) or len(items) > REPORT_MAX_ITEMS:
        return None
    valid = [item for item in items if _valid_item(item)]
    return {**payload, 'items': valid, 'dropped': payload.get('dropped', 0) + len(items) - len(valid)}


def route_key(rule, rule_payload, outlet, policy):
    return _ROUTE_SEPARATOR.join((rule, rule_payload or '', outlet, policy))


def split_route_key(key):
    rule, rule_payload, outlet, policy = key.split(_ROUTE_SEPARATOR, 3)
    return {'rule': rule, 'rule_payload': rule_payload, 'outlet': outlet, 'policy': policy}


def _empty_stats():
    return {'conns': 0, 'fails': 0, 'fail_kinds': {}, 'zero_dl': 0, 'up': 0, 'down': 0}


def _host_weight(host_entry):
    return sum(stats['conns'] + stats['fails'] for stats in host_entry['by_route'].values())


class TrafficStore:
    """按 Agent 保存域名发现数据，按天分桶并自动过期。"""

    def __init__(self, data_dir):
        self.data_dir = str(data_dir)

    def _path(self, agent_id):
        safe_id = str(agent_id).replace('/', '_').replace('\\', '_')
        return os.path.join(self.data_dir, f'{safe_id}.json')

    def _read(self, agent_id):
        try:
            with open(self._path(agent_id), encoding='utf-8') as handle:
                data = json.load(handle)
        except FileNotFoundError:
            return {'status': None, 'days': {}}
        except (OSError, ValueError):
            # 损坏的统计文件不影响主流程，重新累计即可
            return {'status': None, 'days': {}}
        if not isinstance(data, dict) or not isinstance(data.get('days'), dict):
            return {'status': None, 'days': {}}
        return data

    def _write(self, agent_id, data):
        os.makedirs(self.data_dir, exist_ok=True)
        path = self._path(agent_id)
        temp_path = f'{path}.tmp'
        with open(temp_path, 'w', encoding='utf-8') as handle:
            json.dump(data, handle, ensure_ascii=False, separators=(',', ':'))
        os.replace(temp_path, path)

    @staticmethod
    def _prune(data, now):
        oldest = (now - timedelta(days=RETENTION_DAYS - 1)).date().isoformat()
        for day in [day for day in data['days'] if day < oldest]:
            del data['days'][day]

    def add_report(self, agent_id, report: Dict[str, Any], now: Optional[datetime] = None):
        """合并一份已校验的上报，返回保存后的状态。"""
        now = now or datetime.now()
        timestamp = now.isoformat(timespec='seconds')
        with _LOCK:
            data = self._read(agent_id)
            previous = data.get('status') or {}
            data['status'] = {
                'value': report['status'],
                'at': timestamp,
                # 连不上 Mihomo 时拿不到版本，沿用上次的值
                'mihomo_version': report.get('mihomo_version') or previous.get('mihomo_version', ''),
            }
            day = data['days'].setdefault(now.date().isoformat(), {'ip_only_conns': 0, 'dropped': 0, 'hosts': {}})
            day['ip_only_conns'] += report.get('ip_only_conns', 0)
            day['dropped'] += report.get('dropped', 0)
            hosts = day['hosts']
            for item in report.get('items', []):
                entry = hosts.setdefault(item['host'], {'first_seen': timestamp, 'last_seen': timestamp, 'by_route': {}})
                entry['last_seen'] = timestamp
                # 端口用于主动探测时选择 http / https，按连接与失败次数计权
                ports = entry.setdefault('ports', {})
                port_key = str(item.get('port', 0))
                ports[port_key] = ports.get(port_key, 0) + item.get('conns', 0) + item.get('fails', 0)
                key = route_key(item['rule'], item.get('rule_payload'), item['outlet'], item['policy'])
                stats = entry['by_route'].setdefault(key, _empty_stats())
                for field in _COUNT_FIELDS:
                    stats[field] += item.get(field, 0)
                for kind, value in item.get('fail_kinds', {}).items():
                    stats['fail_kinds'][kind] = stats['fail_kinds'].get(kind, 0) + value
            if len(hosts) > MAX_HOSTS_PER_DAY:
                ranked = sorted(hosts, key=lambda host: _host_weight(hosts[host]), reverse=True)
                for host in ranked[MAX_HOSTS_PER_DAY:]:
                    del hosts[host]
                    day['dropped'] += 1
            self._prune(data, now)
            self._write(agent_id, data)
            return copy.deepcopy(data['status'])

    def load(self, agent_id, now: Optional[datetime] = None):
        with _LOCK:
            data = self._read(agent_id)
        self._prune(data, now or datetime.now())
        return data

    def delete(self, agent_id):
        with _LOCK:
            try:
                os.remove(self._path(agent_id))
            except FileNotFoundError:
                pass


def get_traffic_store():
    from backend.common.config import get_repository
    return TrafficStore(os.path.join(str(get_repository().data_dir), 'traffic'))
