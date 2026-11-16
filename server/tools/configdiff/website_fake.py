"""Expected page-identification Agent calls and synthetic raw page markers."""
from region_fake import protocol
from backend.utils.website_identity import PAGE_MARKERS
from seed import AGENT


def collected_host(domain):
    # Offline PSL keeps unknown .example hosts whole; recognized example.com/
    # org roots have an observed API subdomain to exercise three candidates.
    return domain if domain.endswith('.example') else 'api.' + domain


def calls(domains, proxy='代理🧪'):
    return [{'method': 'POST', 'path': '/api/region-check', 'authorization': 'Bearer ' + AGENT,
             'content_type': 'application/json', 'body': {'target': proxy, 'timeout_ms': 8000,
                 'requests': [{'id': 'website-' + str(i), 'url': 'https://' + host + '/', 'markers': PAGE_MARKERS}
                              for i, host in enumerate(dict.fromkeys((domain, 'www.' + domain, collected_host(domain))))]}}
            for domain in domains]


def result(request, index, scenario):
    if scenario == 'rpc404' and index == 0:
        return 404, {'success': False}
    rows = []
    for entry in (request.get('body') or {}).get('requests', []):
        markers = {}
        if scenario == 'challenge':
            markers = {'challenge': '人机验证'}
        elif scenario == 'empty':
            pass
        elif entry['id'] == 'website-1':
            if scenario == 'title':
                markers = {'title': '登录 | 合成 &amp; 网站🧪 - Powered by NexusPHP'}
            elif scenario == 'long':
                markers = {'title': '中🧪' * 120}
            else:
                markers = {'title': 'fallback title', 'site': '<meta property="og:site_name" content="合成 &amp; 站点🧪">',
                           'description': '<meta name="description" content="说明 &amp; 特殊 &quot;字符&quot;">'}
        rows.append({'id': entry['id'], 'status': 200, 'final_url': 'https://redirect.example/private?token=synthetic-private',
                     'redirects': [entry['url']], 'markers': markers})
    return 200, {'success': True, 'results': rows}
