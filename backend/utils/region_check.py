"""域名发现 P2：区域限制检测的服务定义、判定与目标选择。

Agent 只负责「经指定节点取页面并匹配标记」，判定全部在这里：检测方式变化时只需升级服务端。
判定原则是宁可 unknown 也不猜，每个结论都附带可读的依据。
"""

import os
import re
import threading
from datetime import datetime, timedelta
from typing import Any, Callable, Dict, List, Optional
from urllib.parse import urlsplit

from backend.utils.json_store import JsonFileStore

REGION_RETENTION_DAYS = 30
MAX_GROUP_TARGETS = 4
MAX_REGION_TARGETS = 8
MAX_DOMAINS_PER_CHECK = 10
CHECK_TIMEOUT_MS = 8000

EXIT_REQUEST = {
    'id': 'exit',
    'url': 'https://www.cloudflare.com/cdn-cgi/trace',
    'markers': {'loc': r'loc=([A-Z]{2})', 'ip': r'ip=([0-9a-fA-F:.]+)'},
}

GROUP_TYPES = {'Selector', 'URLTest', 'Fallback', 'LoadBalance', 'Relay', 'Smart'}
NON_NODE_TYPES = GROUP_TYPES | {'Direct', 'Reject', 'RejectDrop', 'Pass', 'PassRule', 'Compatible', 'Dns'}
EXCLUDED_TARGETS = {'GLOBAL', 'ConfigFlow-Region-Probe'}

# 与前端 lib/regions.ts 的规则一致，并补充常见地区；拉丁缩写两侧不能是字母
REGION_RULES = [
    ('HK', '香港', r'港|(?<![A-Za-z])HK(?![A-Za-z])|Hong\s?Kong'),
    ('TW', '台湾', r'台湾|台北|(?<![A-Za-z])TW(?![A-Za-z])|Taiwan'),
    ('JP', '日本', r'日本|东京|大阪|(?<![A-Za-z])JP(?![A-Za-z])|Japan|Tokyo'),
    ('SG', '新加坡', r'新加坡|狮城|(?<![A-Za-z])SG(?![A-Za-z])|Singapore'),
    ('US', '美国', r'美国|洛杉矶|硅谷|(?<![A-Za-z])US(?![A-Za-z])|United\s?States|America'),
    ('KR', '韩国', r'韩国|首尔|(?<![A-Za-z])KR(?![A-Za-z])|Korea'),
    ('GB', '英国', r'英国|伦敦|(?<![A-Za-z])(UK|GB)(?![A-Za-z])|Britain|London'),
    ('DE', '德国', r'德国|法兰克福|(?<![A-Za-z])DE(?![A-Za-z])|Germany'),
    ('CA', '加拿大', r'加拿大|(?<![A-Za-z])CA(?![A-Za-z])|Canada'),
    ('AU', '澳大利亚', r'澳大利亚|澳洲|悉尼|(?<![A-Za-z])AU(?![A-Za-z])|Australia'),
    ('FR', '法国', r'法国|巴黎|(?<![A-Za-z])FR(?![A-Za-z])|France'),
    ('NL', '荷兰', r'荷兰|阿姆斯特丹|(?<![A-Za-z])NL(?![A-Za-z])|Netherlands'),
    ('IN', '印度', r'印度|(?<![A-Za-z])IN(?![A-Za-z])|India'),
    ('MY', '马来西亚', r'马来|(?<![A-Za-z])MY(?![A-Za-z])|Malaysia'),
    ('TH', '泰国', r'泰国|(?<![A-Za-z])TH(?![A-Za-z])|Thailand'),
    ('VN', '越南', r'越南|(?<![A-Za-z])VN(?![A-Za-z])|Vietnam'),
    ('PH', '菲律宾', r'菲律宾|(?<![A-Za-z])PH(?![A-Za-z])|Philippines'),
    ('TR', '土耳其', r'土耳其|(?<![A-Za-z])TR(?![A-Za-z])|Turkey|Türkiye'),
    ('AR', '阿根廷', r'阿根廷|(?<![A-Za-z])AR(?![A-Za-z])|Argentina'),
    ('BR', '巴西', r'巴西|(?<![A-Za-z])BR(?![A-Za-z])|Brazil'),
]
_REGION_PATTERNS = [(code, name, re.compile(pattern, re.IGNORECASE)) for code, name, pattern in REGION_RULES]


def region_of_name(name: str) -> Optional[str]:
    for code, _, pattern in _REGION_PATTERNS:
        if pattern.search(name or ''):
            return code
    return None


def region_label(code: Optional[str]) -> str:
    return next((name for rule_code, name, _ in REGION_RULES if rule_code == code), code or '未知')


# ---------- 结果构造 ----------

def _result(status, *, region=None, reason='', evidence=None):
    return {'status': status, 'region': region, 'reason': reason, 'evidence': evidence or []}


def _evidence(result: Optional[Dict[str, Any]]) -> str:
    if not result:
        return '没有结果'
    if result.get('error'):
        return f"{result['id']}：{result['error']}"
    text = f"{result['id']}：HTTP {result.get('status')}"
    if result.get('final_url') and result.get('redirects'):
        text += f" → {result['final_url']}"
    if result.get('markers'):
        text += '，命中 ' + '、'.join(sorted(result['markers']))
    return text


def _marker(result, name):
    return (result or {}).get('markers', {}).get(name)


def _status(result):
    """出错的请求没有可信的状态码。"""
    if not result or result.get('error'):
        return None
    return result.get('status')


# ---------- 服务 ----------

def _evaluate_openai(results):
    compliance, ios, trace = results.get('openai-compliance'), results.get('openai-ios'), results.get('openai-trace')
    evidence = [_evidence(compliance), _evidence(ios)]
    region = _marker(trace, 'loc')
    if _marker(compliance, 'unsupported') or _marker(ios, 'unsupported'):
        return _result('blocked', region=region, reason='所在地区不受支持', evidence=evidence)
    if _marker(ios, 'vpn'):
        return _result('blocked', region=region, reason='被识别为代理 / VPN', evidence=evidence)
    if _status(compliance) == 200 and _marker(compliance, 'consent') is not None:
        reason = 'App 端可能受限（被识别为数据中心 IP）' if _marker(ios, 'dc') else ''
        return _result('available', region=region, reason=reason, evidence=evidence)
    return _result('unknown', region=region, reason='响应不符合已知模式', evidence=evidence)


def _evaluate_claude(results):
    web, trace = results.get('claude-web'), results.get('claude-trace')
    evidence = [_evidence(web)]
    region = _marker(trace, 'loc')
    if not web or web.get('error'):
        return _result('unknown', region=region, reason='请求失败', evidence=evidence)
    urls = [web.get('final_url') or '', *web.get('redirects', [])]
    if any('app-unavailable-in-region' in url for url in urls):
        return _result('blocked', region=region, reason='跳转到「所在地区不可用」页面', evidence=evidence)
    if _status(web) == 200:
        return _result('available', region=region, evidence=evidence)
    if _status(web) == 403:
        return _result('unknown', region=region, reason='Cloudflare 质询，无法判断', evidence=evidence)
    return _result('unknown', region=region, reason='响应不符合已知模式', evidence=evidence)


_NETFLIX_REGION = re.compile(r'^/([a-z]{2})(?:-[a-z]{2})?/title/')


def _netflix_region(result):
    if not result or not result.get('final_url'):
        return None
    path = urlsplit(result['final_url']).path
    match = _NETFLIX_REGION.match(path)
    if match:
        return match.group(1).upper()
    return 'US' if path.startswith('/title/') and _status(result) == 200 else None


def _evaluate_netflix(results):
    licensed = [results.get('netflix-licensed-1'), results.get('netflix-licensed-2')]
    original = results.get('netflix-original')
    evidence = [_evidence(item) for item in (*licensed, original)]
    region = next((_netflix_region(item) for item in (*licensed, original) if _netflix_region(item)), None)
    statuses = [_status(item) for item in (*licensed, original) if _status(item) is not None]
    if any(_status(item) == 200 for item in licensed):
        return _result('available', region=region, reason='完整解锁', evidence=evidence)
    if _status(original) == 200:
        return _result('partial', region=region, reason='仅自制剧', evidence=evidence)
    if statuses and all(status == 403 for status in statuses):
        return _result('blocked', region=region, reason='Netflix 拒绝访问（403）', evidence=evidence)
    if statuses and all(status == 404 for status in statuses):
        return _result('blocked', region=region, reason='所在地区没有可播放的内容', evidence=evidence)
    return _result('unknown', region=region, reason='响应不符合已知模式', evidence=evidence)


def _evaluate_youtube(results):
    page = results.get('youtube-premium')
    evidence = [_evidence(page)]
    region = _marker(page, 'gl')
    if _marker(page, 'blocked'):
        return _result('blocked', region=region, reason='Premium 在所在地区不可用', evidence=evidence)
    if region == 'CN':
        return _result('blocked', region=region, reason='识别为中国大陆', evidence=evidence)
    if region:
        return _result('available', region=region, evidence=evidence)
    return _result('unknown', reason='页面里没有地区信息', evidence=evidence)


SERVICES: List[Dict[str, Any]] = [
    {
        'id': 'openai', 'name': 'ChatGPT',
        'domains': ['openai.com', 'chatgpt.com', 'oaistatic.com', 'oaiusercontent.com', 'sora.com'],
        'requests': [
            {'id': 'openai-compliance', 'url': 'https://api.openai.com/compliance/cookie_requirements',
             'headers': {'Authorization': 'Bearer null'},
             'markers': {'unsupported': 'unsupported_country', 'consent': 'cookie_consent_required'}},
            {'id': 'openai-ios', 'url': 'https://ios.chat.openai.com/',
             'markers': {'unsupported': 'unsupported_country', 'vpn': 'VPN', 'dc': r'"type"\s*:\s*"dc"'}},
            {'id': 'openai-trace', 'url': 'https://chatgpt.com/cdn-cgi/trace', 'markers': {'loc': r'loc=([A-Z]{2})'}},
        ],
        'evaluate': _evaluate_openai,
    },
    {
        'id': 'claude', 'name': 'Claude',
        'domains': ['claude.ai', 'claude.com', 'anthropic.com'],
        'requests': [
            {'id': 'claude-web', 'url': 'https://claude.ai/'},
            {'id': 'claude-trace', 'url': 'https://claude.ai/cdn-cgi/trace', 'markers': {'loc': r'loc=([A-Z]{2})'}},
        ],
        'evaluate': _evaluate_claude,
    },
    {
        'id': 'netflix', 'name': 'Netflix',
        'domains': ['netflix.com', 'netflix.net', 'nflxext.com', 'nflximg.com', 'nflximg.net', 'nflxso.net',
                    'nflxvideo.net'],
        'requests': [
            # 非自制剧能播说明完整解锁；都不行时再看自制剧
            {'id': 'netflix-licensed-1', 'url': 'https://www.netflix.com/title/81280792'},
            {'id': 'netflix-licensed-2', 'url': 'https://www.netflix.com/title/70143836'},
            {'id': 'netflix-original', 'url': 'https://www.netflix.com/title/80018499'},
        ],
        'evaluate': _evaluate_netflix,
    },
    {
        'id': 'youtube', 'name': 'YouTube Premium',
        'domains': ['youtube.com', 'googlevideo.com', 'ytimg.com', 'youtu.be', 'youtube-nocookie.com'],
        'requests': [
            {'id': 'youtube-premium', 'url': 'https://www.youtube.com/premium',
             'headers': {'Accept-Language': 'en'},
             'markers': {'blocked': 'Premium is not available in your country',
                         'gl': r'"INNERTUBE_CONTEXT_GL"\s*:\s*"([A-Z]{2})"'}},
        ],
        'evaluate': _evaluate_youtube,
    },
]
SERVICES_BY_ID = {service['id']: service for service in SERVICES}


def service_requests() -> List[Dict[str, Any]]:
    return [EXIT_REQUEST] + [request for service in SERVICES for request in service['requests']]


def evaluate_services(results: List[Dict[str, Any]]) -> Dict[str, Dict[str, Any]]:
    by_id = {result['id']: result for result in results}
    return {service['id']: service['evaluate'](by_id) for service in SERVICES}


def exit_info(results: List[Dict[str, Any]]) -> Dict[str, Any]:
    exit_result = next((result for result in results if result['id'] == 'exit'), None)
    return {
        'loc': _marker(exit_result, 'loc'),
        'ip': _marker(exit_result, 'ip'),
        'error': (exit_result or {}).get('error') or None,
    }


def public_services():
    return [{'id': service['id'], 'name': service['name'], 'domains': service['domains']} for service in SERVICES]


# ---------- 任意域名 ----------

BLOCK_PATTERN = (
    r"(?i)(not|isn't|is not) available in your (country|region|location)"
    r"|unavailable in your (country|region|location)"
    r"|not available in (this|the) (country|region)"
    r"|地区不可用|所在的?地区|所在国家"
)
_REGION_URL_HINT = re.compile(r'unavailable|region|country|geo-?block', re.IGNORECASE)


def domain_request(request_id: str, url: str) -> Dict[str, Any]:
    return {'id': request_id, 'url': url, 'markers': {'block': BLOCK_PATTERN}}


def classify_domain_result(result: Optional[Dict[str, Any]], original_url: str) -> Dict[str, Any]:
    """单个目标访问域名的结果：ok / restricted / denied / error。"""
    if not result or result.get('error'):
        return {'state': 'error', 'detail': (result or {}).get('error') or '没有结果', 'status': None,
                'final_url': None}
    status = result.get('status')
    final_url = result.get('final_url') or ''
    detail = f'HTTP {status}'
    moved_to_region_page = bool(final_url and _REGION_URL_HINT.search(urlsplit(final_url).path)
                                and not _REGION_URL_HINT.search(urlsplit(original_url).path))
    if status == 451:
        state, detail = 'restricted', 'HTTP 451（因法律原因不可用）'
    elif _marker(result, 'block') and status in (200, 403):
        state, detail = 'restricted', f"HTTP {status}，页面提示：{_marker(result, 'block')}"
    elif moved_to_region_page:
        state, detail = 'restricted', f'跳转到 {final_url}'
    elif status == 403:
        state = 'denied'
    elif status and status < 500:
        state = 'ok'
    else:
        state = 'error'
    return {'state': state, 'detail': detail, 'status': status, 'final_url': final_url}


def summarize_domain(per_target: Dict[str, Dict[str, Any]]) -> Dict[str, Any]:
    states = [item['state'] for item in per_target.values()]
    restricted = [name for name, item in per_target.items() if item['state'] == 'restricted']
    usable = [name for name, item in per_target.items() if item['state'] == 'ok']
    if restricted and usable:
        verdict = 'suspected'
    elif restricted and len(restricted) == len(states):
        verdict = 'all_restricted'
    elif usable and not restricted:
        verdict = 'no_difference'
    else:
        verdict = 'unreachable'
    return {'verdict': verdict, 'restricted': restricted, 'usable': usable}


# ---------- 目标选择 ----------

def pick_targets(targets: List[Dict[str, Any]], preferred_group: Optional[str] = None) -> List[Dict[str, Any]]:
    """直连 + 最多 4 个策略组（当前路径）+ 每个地区一个存活节点（最多 8 个地区）。"""
    picked = []
    names = {target['name'] for target in targets}
    if 'DIRECT' in names:
        picked.append({'name': 'DIRECT', 'kind': 'direct', 'region_hint': None})
    groups = [target for target in targets if target.get('type') in GROUP_TYPES and target['name'] not in EXCLUDED_TARGETS]
    groups.sort(key=lambda target: target['name'] != preferred_group)
    picked += [{'name': target['name'], 'kind': 'group', 'region_hint': region_of_name(target['name'])}
               for target in groups[:MAX_GROUP_TARGETS]]
    order = [code for code, _, _ in REGION_RULES]
    by_region: Dict[str, Dict[str, Any]] = {}
    for target in targets:
        if target.get('type') in NON_NODE_TYPES or not target.get('alive', True):
            continue
        code = region_of_name(target['name'])
        if code and code not in by_region:
            by_region[code] = target
    for code in sorted(by_region, key=order.index)[:MAX_REGION_TARGETS]:
        picked.append({'name': by_region[code]['name'], 'kind': 'node', 'region_hint': code})
    return picked


# ---------- 存储 ----------

_LOCK = threading.Lock()


class RegionStore(JsonFileStore):
    """按 Agent 保存最近一次服务检测矩阵和各域名的地区差异。"""

    def _read(self, agent_id):
        data = self._read_json(agent_id) or {}
        data.setdefault('services', None)
        if not isinstance(data.get('domains'), dict):
            data['domains'] = {}
        return data

    def update(self, agent_id, mutate: Callable[[Dict[str, Any]], None], now: Optional[datetime] = None):
        now = now or datetime.now()
        with _LOCK:
            data = self._read(agent_id)
            mutate(data)
            oldest = (now - timedelta(days=REGION_RETENTION_DAYS)).isoformat(timespec='seconds')
            data['domains'] = {host: entry for host, entry in data['domains'].items()
                               if entry.get('checked_at', '') >= oldest}
            self._write_json(agent_id, data)

    def load(self, agent_id):
        with _LOCK:
            return self._read(agent_id)

    def delete(self, agent_id):
        with _LOCK:
            self._remove(agent_id)


def get_region_store():
    from backend.common.config import get_repository
    return RegionStore(os.path.join(str(get_repository().data_dir), 'regions'))
