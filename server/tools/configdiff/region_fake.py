"""Synthetic region Agent target catalog and raw HTTP page observations."""
import sys

from harness import ROOT
from seed import AGENT

# Request descriptors are part of the Python oracle, not Go implementation.
# No store or application globals are initialized by this definitions module.
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))
from backend.utils import region_check as protocol

AVAILABLE = [{'name': 'DIRECT', 'type': 'Direct'}, {'name': '代理🧪', 'type': 'Selector'},
             {'name': 'GLOBAL', 'type': 'Selector'}, {'name': 'ConfigFlow-Region-Probe', 'type': 'Selector'},
             {'name': '香港节点🧪', 'type': 'Socks5', 'alive': True},
             {'name': '香港备用', 'type': 'Trojan', 'alive': True},
             {'name': '日本离线', 'type': 'Socks5', 'alive': False}, {'name': '无地区', 'type': 'Socks5'}]
PICKED = ['DIRECT', '代理🧪', '香港节点🧪']


def target_call():
    return {'method': 'GET', 'path': '/api/region-check/targets', 'authorization': 'Bearer ' + AGENT,
            'content_type': '', 'body': None}


def region_calls(domains, services=True):
    requests = (protocol.service_requests() if services else [protocol.EXIT_REQUEST]) + [
        protocol.domain_request('domain-' + str(i), 'https://' + domain + '/') for i, domain in enumerate(domains)]
    return [target_call()] + [{'method': 'POST', 'path': '/api/region-check',
        'authorization': 'Bearer ' + AGENT, 'content_type': 'application/json',
        'body': {'target': target, 'timeout_ms': 8000, 'requests': requests[start:start + 12]}}
        for target in PICKED for start in range(0, len(requests), 12)]


def result(request, index, scenario='success'):
    if request['method'] == 'GET':
        return 200, {'success': True, 'targets': AVAILABLE}
    payload = request.get('body') or {}
    target = payload.get('target')
    if scenario == 'partial' and target == '代理🧪':
        return 500, {'success': False, 'message': '合成区域节点失败🧪'}
    rows = []
    for item in payload.get('requests', []):
        row = {'id': item['id'], 'status': 200, 'final_url': item['url'], 'redirects': [], 'markers': {}}
        if item['id'] == 'exit':
            row['markers'] = {'loc': 'CN' if target == 'DIRECT' else 'HK', 'ip': '192.0.2.12'}
        elif item['id'].endswith('trace'):
            row['markers'] = {'loc': 'HK'}
        elif item['id'] == 'openai-compliance':
            row['markers'] = {'consent': 'cookie_consent_required'}
        elif item['id'] == 'youtube-premium':
            row['markers'] = {'gl': 'HK'}
        elif item['id'].startswith('domain-'):
            if target == 'DIRECT':
                row['status'] = 451
            elif target == '香港节点🧪':
                row['error'] = '合成目标超时'
        rows.append(row)
    return 200, {'success': True, 'results': rows}
