"""域名发现 P1：主动探测结果的判定、置信度与存储。

探测由 Agent 调用本机 Mihomo 的延迟测试完成，分别经直连（DIRECT）与代理策略访问目标；
这里只做纯判定和持久化，不发网络请求。
"""

import copy
import json
import os
import statistics
import threading
from datetime import datetime, timedelta
from typing import Any, Dict, List, Optional

PROBE_RETENTION_DAYS = 30
MAX_PROBED_HOSTS = 5000
MAX_CONFIDENCE = 99

# 每批都带上对照目标：直连对照不通说明本机网络有问题，代理对照不通说明代理节点有问题，
# 此时不能把单个域名的失败归咎于「被墙」或「只能直连」
CONTROL_TARGETS = {
    'direct': {'host': 'www.baidu.com', 'url': 'https://www.baidu.com/'},
    'proxy': {'host': 'www.gstatic.com', 'url': 'https://www.gstatic.com/generate_204'},
}

VERDICT_LABELS = {
    'needs_proxy': '直连不通，代理可达',
    'direct_only': '代理不通，直连可达',
    'both_ok': '直连与代理都可达',
    'unreachable': '直连与代理都不通',
    'proxy_down': '代理节点不可用，无法判断',
    'direct_down': '本机直连网络异常，无法判断',
    'flaky': '结果不稳定',
    'incomplete': '探测不完整',
}

_LOCK = threading.Lock()


def probe_url(host: str, port: Optional[int]) -> str:
    """按流量里最常见的端口选择探测地址。"""
    if port == 80:
        return f'http://{host}/'
    if not port or port == 443:
        return f'https://{host}/'
    return f'https://{host}:{port}/'


def _rate(stats: Optional[Dict[str, Any]]) -> Optional[float]:
    if not stats or not stats.get('total'):
        return None
    return stats.get('ok', 0) / stats['total']


def _median(stats: Dict[str, Any]) -> Optional[float]:
    delays = stats.get('delays') or []
    return statistics.median(delays) if delays else None


def path_healthy(stats: Optional[Dict[str, Any]]) -> bool:
    rate = _rate(stats)
    return rate is not None and rate >= 0.5


def judge(direct: Optional[Dict[str, Any]], proxy: Optional[Dict[str, Any]], *,
          passive_failing: bool = False, direct_healthy: bool = True,
          proxy_healthy: bool = True) -> Dict[str, Any]:
    """根据直连 / 代理两条路径的探测统计给出判定、建议目标和置信度。"""
    direct_rate, proxy_rate = _rate(direct), _rate(proxy)
    reasons: List[str] = []

    def result(verdict, target=None, confidence=0):
        return {
            'verdict': verdict,
            'target': target,
            'confidence': min(confidence, MAX_CONFIDENCE) if target else 0,
            'reasons': reasons,
        }

    if proxy and proxy.get('errors', {}).get('no_path'):
        reasons.append('代理策略不在当前 Mihomo 配置中，部署配置后再探测')
        return result('incomplete')
    if direct_rate is None or proxy_rate is None:
        reasons.append('有一条路径没有完成探测')
        return result('incomplete')

    reasons.append(f"直连 {direct['ok']}/{direct['total']}，代理 {proxy['ok']}/{proxy['total']}")

    if direct_rate == 0 and proxy_rate >= 0.5:
        if not direct_healthy:
            reasons.append('直连对照目标也不通')
            return result('direct_down')
        confidence = 70
        if proxy_rate == 1 and direct['total'] >= 2:
            confidence += 15
            reasons.append('多次探测结果一致')
        if passive_failing:
            confidence += 15
            reasons.append('真实流量中直连也大量失败')
        return result('needs_proxy', 'proxy', confidence)

    if direct_rate >= 0.5 and proxy_rate == 0:
        if not proxy_healthy:
            reasons.append('代理对照目标也不通')
            return result('proxy_down')
        confidence = 70
        if direct_rate == 1 and proxy['total'] >= 2:
            confidence += 15
            reasons.append('多次探测结果一致')
        return result('direct_only', 'direct', confidence)

    if direct_rate >= 0.5 and proxy_rate >= 0.5:
        direct_delay, proxy_delay = _median(direct), _median(proxy)
        if direct_delay is not None and proxy_delay is not None:
            reasons.append(f'延迟：直连 {round(direct_delay)} ms，代理 {round(proxy_delay)} ms')
            if direct_delay <= proxy_delay * 1.5:
                return result('both_ok', 'direct', 60)
            return result('both_ok', 'proxy', 50)
        return result('both_ok')

    if direct_rate == 0 and proxy_rate == 0:
        if not proxy_healthy:
            reasons.append('代理对照目标也不通')
            return result('proxy_down')
        if not direct_healthy:
            reasons.append('直连对照目标也不通')
            return result('direct_down')
        return result('unreachable')

    return result('flaky')


class ProbeStore:
    """按 Agent 保存每个 host 最近一次的探测结果。"""

    def __init__(self, data_dir):
        self.data_dir = str(data_dir)

    def _path(self, agent_id):
        safe_id = str(agent_id).replace('/', '_').replace('\\', '_')
        return os.path.join(self.data_dir, f'{safe_id}.json')

    def _read(self, agent_id):
        try:
            with open(self._path(agent_id), encoding='utf-8') as handle:
                data = json.load(handle)
        except (OSError, ValueError):
            return {'hosts': {}}
        if not isinstance(data, dict) or not isinstance(data.get('hosts'), dict):
            return {'hosts': {}}
        return data

    @staticmethod
    def _prune(data, now):
        oldest = (now - timedelta(days=PROBE_RETENTION_DAYS)).isoformat(timespec='seconds')
        hosts = data['hosts']
        for host in [host for host, entry in hosts.items() if entry.get('checked_at', '') < oldest]:
            del hosts[host]
        if len(hosts) > MAX_PROBED_HOSTS:
            ranked = sorted(hosts, key=lambda host: hosts[host].get('checked_at', ''), reverse=True)
            for host in ranked[MAX_PROBED_HOSTS:]:
                del hosts[host]

    def save(self, agent_id, results: Dict[str, Dict[str, Any]], now: Optional[datetime] = None):
        now = now or datetime.now()
        with _LOCK:
            data = self._read(agent_id)
            data['hosts'].update(copy.deepcopy(results))
            self._prune(data, now)
            os.makedirs(self.data_dir, exist_ok=True)
            path = self._path(agent_id)
            temp_path = f'{path}.tmp'
            with open(temp_path, 'w', encoding='utf-8') as handle:
                json.dump(data, handle, ensure_ascii=False, separators=(',', ':'))
            os.replace(temp_path, path)

    def load(self, agent_id, now: Optional[datetime] = None):
        with _LOCK:
            data = self._read(agent_id)
        self._prune(data, now or datetime.now())
        return data['hosts']

    def delete(self, agent_id):
        with _LOCK:
            try:
                os.remove(self._path(agent_id))
            except FileNotFoundError:
                pass


def get_probe_store():
    from backend.common.config import get_repository
    return ProbeStore(os.path.join(str(get_repository().data_dir), 'probes'))


def latest_probes(agent_ids: List[str], store: Optional[ProbeStore] = None) -> Dict[str, Dict[str, Any]]:
    """合并多个 Agent 的探测结果，每个 host 取最新一次。"""
    store = store or get_probe_store()
    merged: Dict[str, Dict[str, Any]] = {}
    for agent_id in agent_ids:
        for host, entry in store.load(agent_id).items():
            if host not in merged or entry.get('checked_at', '') > merged[host].get('checked_at', ''):
                merged[host] = {**entry, 'agent_id': agent_id}
    return merged


def domain_suggestion(hosts: List[Dict[str, Any]]) -> Optional[Dict[str, Any]]:
    """主域的建议：取各子域里置信度最高的可采纳建议；都没有时返回最新一次的判定。"""
    probed = [host['probe'] for host in hosts if host.get('probe')]
    if not probed:
        return None
    actionable = [probe for probe in probed if probe.get('target')]
    best = max(actionable, key=lambda probe: probe['confidence']) if actionable else \
        max(probed, key=lambda probe: probe.get('checked_at', ''))
    return {key: best.get(key) for key in ('host', 'verdict', 'target', 'confidence', 'reasons', 'checked_at')}
