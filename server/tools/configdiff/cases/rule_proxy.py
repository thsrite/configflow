"""MosDNS rule-proxy: credential roles, conversion, declared caches and inline scope."""
import copy
from urllib.parse import urlencode

import jwt

from compare import database
from harness import Pair, USERNAME, SECRET
from seed import CURRENT, RETIRED, PULL
from sources import Source
from subscriptions import Exchange, capture_notice
from task_lifecycle import configure
from cases.config_backup import message
from cases.rule_library import DOMAIN
from cases.rule_io import write_cache
from cases.subscriptions import put_file, require

ROUTES = [('R087', 'GET /api/mosdns/rule-proxy', '/api/mosdns/rule-proxy'),
          ('R138', 'GET /api/profiles/<profile_id>/mosdns/rule-proxy', '/api/profiles/default/mosdns/rule-proxy')]
INLINE = {**DOMAIN, 'id': 'proxy-inline', 'name': '手写规则🧪', 'content': 'DOMAIN,inline.example\n', 'behavior': 'classical'}


def options(library=None, origin=None):
    library = copy.deepcopy(library or [])
    value = {'shared': {'rule_library': library}, 'profile': {'rule_configs': [
        {'id': 'proxy-ref-' + lib['id'], 'itemType': 'ruleset', 'library_rule_id': lib['id'], 'policy': 'DIRECT', 'enabled': True}
        for lib in library]}}
    if origin:
        value['system_config'] = {'server_domain': origin}
    return value


def request(pair, rid, route, base, suffix, query, *, auth='admin', expect=200, source=None, calls=0, body=None, check=None, variant='', capture=None):
    before = database(pair.dirs[0])
    def reference(reply):
        require(database(pair.dirs[0]) == before, 'rule proxy never commits configuration, including on cache refresh/failure')
        if body is not None:
            require(reply.body == body, 'exact DNS text framing, selected part and conversion output')
        if check:
            check(reply)
    return pair.request(rid + '-' + suffix, route, base + ('?' + urlencode(query) if query else ''), auth=auth,
                        expect=expect, writes=True, check=reference, interaction=Exchange(pair, source, expected=calls) if source else None,
                        raw_body=expect == 200, variant=variant, capture=capture)


def credentials(binary, recorder):
    for rid, route, base in ROUTES:
        for mode in ('login', 'token', 'open'):
            with Source(b'example.com\n') as source, Pair(binary, recorder, mode=mode) as pair:
                url = source.url + '/rules'
                query = {'url': url}
                cases = [('F04', '', {}), ('F02', CURRENT, {}), ('F06', CURRENT, {}),
                         ('F03', '', {'token': RETIRED}), ('F05', '', {'token': RETIRED}),
                         ('F03', RETIRED, {'token': CURRENT}),
                         ('F02', 'x' * 8193, {}), ('F05', '', {'token': 'x' * 8193})]
                if mode == 'open':
                    cases.append(('F01', '', {}))
                for index, (case, bearer, extra) in enumerate(cases):
                    request(pair, rid, route, base, case, {**query, **extra}, auth=bearer, expect=401, source=source,
                            check=message('Unauthorized'), variant=mode + '/credential-role/' + str(index))
                token = jwt.encode({'username': USERNAME}, SECRET, algorithm='HS256')
                for case, bearer, extra, label in [('S01', token, {}, 'signed-JWT'), ('S02', '', {'token': CURRENT}, 'query-internal'),
                        ('S01', token, {'token': RETIRED}, 'valid-JWT-precedes-retired-query')]:
                    request(pair, rid, route, base, case, {**query, **extra}, auth=bearer, source=source, calls=1,
                            body=b'full:example.com', variant=mode + '/' + label)
        with Source(b'example.com\n') as source, Pair(binary, recorder, options={'config_token': PULL}) as pair:
            request(pair, rid, route, base, 'S03', {'url': source.url + '/rules', 'token': PULL}, auth='', source=source, calls=1,
                    body=b'full:example.com', variant='query-public-pull-token')


def conversions(binary, recorder):
    classical = b'DOMAIN,exact.example,DIRECT\nDOMAIN-SUFFIX,suffix.example,no-resolve\nDOMAIN-REGEX,^a,b[.]example$,DIRECT\nIP-CIDR,192.0.2.0/24,no-resolve\n'
    domains = b'full:exact.example\ndomain:suffix.example\nregexp:^a,b[.]example$,DIRECT'
    forms = [('classical', classical, domains, b'192.0.2.0/24', domains),
             ('yaml', b'payload:\n  - DOMAIN,exact.example\n  - IP-CIDR,192.0.2.0/24\n', b'full:exact.example', b'192.0.2.0/24', b'full:exact.example'),
             ('list', b'example.com\n+.example.org\n192.0.2.1\n', b'full:example.com\ndomain:example.org', b'192.0.2.1', b'full:example.com\ndomain:example.org\n192.0.2.1'),
             ('native', b'# keep\nfull:exact.example\nip:192.0.2.0/24\n', b'full:exact.example', b'192.0.2.0/24', b'# keep\nfull:exact.example\nip:192.0.2.0/24\n')]
    for rid, route, base in ROUTES:
        with Source() as source, Pair(binary, recorder) as pair:
            for shape, raw, domain, ip, mixed in forms:
                source.body = raw
                for case, part, expected in [('S07', 'domain', domain), ('S08', 'ip', ip), ('S09', None, mixed)]:
                    query = {'url': source.url + '/rules'}
                    if part:
                        query['part'] = part
                    request(pair, rid, route, base, case, query, source=source, calls=1, body=expected, variant=shape + '/' + str(part))
                request(pair, rid, route, base, 'S06', {'url': source.url + '/rules'}, source=source, calls=1, body=mixed, variant=shape)
            for case, raw, expected, label in [('B01', '\ufeff中国.example\n+.例子.example\n'.encode(), 'full:中国.example\ndomain:例子.example'.encode(), 'BOM-Unicode'),
                    ('B04', b'\xffexample.com\n', b'', 'undeclared-replacement-character')]:
                source.body = raw
                request(pair, rid, route, base, case, {'url': source.url + '/rules'}, source=source, calls=1, body=expected, variant=label)
            source.body = b'full:only.example\n'
            request(pair, rid, route, base, 'S08', {'url': source.url + '/rules', 'part': 'ip'}, source=source, calls=1,
                    body=b'# no ip rules', variant='empty-filtered-ip-marker')


def local_and_cache(binary, recorder):
    for rid, route, base in ROUTES:
        with Source(b'network.example\n') as source:
            opts = options([INLINE], source.url)
            with Pair(binary, recorder, options=opts) as pair:
                local = source.url + '/api/profiles/default/rule-library/content/' + INLINE['id']
                request(pair, rid, route, base, 'S04', {'url': local}, source=source, body=b'full:inline.example', variant='inline-origin')
                request(pair, rid, route, base, 'S04', {'url': '/api/profiles/default/rule-library/content/' + INLINE['id']},
                        source=source, body=b'full:inline.example', variant='inline-relative')
                request(pair, rid, route, base, 'B03', {'url': '/api/profiles/other/rule-library/content/' + INLINE['id']},
                        source=source, expect=400, check=message('Invalid remote URL'), variant='cross-profile-relative-is-not-inline')
                request(pair, rid, route, base, 'B91', {'url': '/api/profiles/default/rule-library/content/' + INLINE['id'] + '?x=1'},
                        source=source, expect=400, check=message('Invalid remote URL'), variant='query-prevents-inline-relative-url')
                request(pair, rid, route, base, 'B91', {'url': local + '?x=1'}, source=source,
                        body=b'full:inline.example', variant='absolute-query-dispatches-internal-content-endpoint')
            opts = options([{**INLINE, 'enabled': False}], source.url)
            opts['profile']['rule_configs'][0]['library_enabled'] = False
            with Pair(binary, recorder, options=opts) as pair:
                request(pair, rid, route, base, 'B91', {'url': '/api/profiles/default/rule-library/content/' + INLINE['id']},
                        source=source, expect=400, check=message('Invalid remote URL'), variant='disabled-library-reference')
        for stale in (False, True):
            with Source(b'DOMAIN,new.example\n') as source:
                declaration = {**INLINE, 'id': 'proxy-remote', 'name': '远端规则🧪', 'source_type': 'url', 'content': '',
                               'url': source.url + '/rules', 'format': 'text', 'update_interval': 0}
                with Pair(binary, recorder, options=options([declaration])) as pair:
                    write_cache(pair, declaration, b'DOMAIN,old.example\n')
                    if stale:
                        put_file(pair, 'task-status.json', {'rule_source:proxy-remote': {'last_status': 'failed', 'last_error': '合成上次更新失败'}})
                    def check(reply):
                        require(reply.headers.get('x-configflow-rule-cache') == ('stale' if stale else None), 'only a failed scheduled refresh marks a valid cache stale')
                    request(pair, rid, route, base, 'S90' if stale else 'S05', {'url': declaration['url'], 'source_id': declaration['id']},
                            source=source, body=b'full:old.example', check=check, variant='stale-cache' if stale else 'valid-cache')
        with Source(b'DOMAIN,fresh.example\n') as source:
            declaration = {**INLINE, 'id': 'proxy-first', 'name': '初次规则', 'source_type': 'url', 'content': '',
                           'url': source.url + '/rules', 'format': 'text', 'update_interval': 0}
            with Pair(binary, recorder, options=options([declaration])) as pair:
                configure(pair, 'rule_source:proxy-first')
                request(pair, rid, route, base, 'S05', {'url': declaration['url'], 'source_id': declaration['id']}, source=source, calls=1,
                        body=b'full:fresh.example', variant='first-fetch-writes-validated-cache-and-receipt',
                        capture=capture_notice(pair, 'rule_source:proxy-first'))
        with Source(b'synthetic upstream failure') as source:
            source.status = 503
            declaration = {**INLINE, 'id': 'proxy-no-cache', 'source_type': 'url', 'content': '',
                           'url': source.url + '/rules', 'format': 'text', 'update_interval': 0}
            with Pair(binary, recorder, options=options([declaration])) as pair:
                configure(pair, 'rule_source:proxy-no-cache')
                request(pair, rid, route, base, 'S90', {'url': declaration['url'], 'source_id': declaration['id']}, source=source, calls=1,
                        expect=502, check=lambda r: require(r.json()['code'] == 'RULE_SOURCE_UNAVAILABLE'
                            and r.headers.get('cache-control') == 'no-store' and 'x-configflow-rule-cache' not in r.headers,
                            'failed first refresh has no stale snapshot to serve'), variant='declared-no-cache-download-failure',
                        capture=capture_notice(pair, 'rule_source:proxy-no-cache'))


GROUPS = {'rule-proxy-credentials': credentials, 'rule-proxy-conversions': conversions, 'rule-proxy-local-cache': local_and_cache}


def guards(binary, recorder):
    for rid, route, base in ROUTES:
        with Source(b'example.com\n') as source, Pair(binary, recorder) as pair:
            request(pair, rid, route, base, 'F07', {}, expect=400, source=source, check=message('URL parameter is required'))
            for part in ('all', 'DOMAIN', ' ', 'false'):
                request(pair, rid, route, base, 'F08', {'url': source.url + '/rules', 'part': part}, expect=400,
                        source=source, check=message('Invalid part parameter'), variant=repr(part))
            invalid = [('file:///synthetic', 'H111'), ('http:///missing', 'H111'), ('http://127.0.0.1:0/rules', 'H112'),
                       ('http://127.0.0.1:65536/rules', 'H114'), ('http://host name/rules', 'H113'),
                       ('http://127.0.0.1\\other/rules', 'H113'), ('http://[::1', 'H114'),
                       ('http://127.0.0.1/rules\x00', 'H110')]
            for value, helper in invalid:
                request(pair, rid, route, base, 'F09', {'url': value}, expect=400, source=source,
                        check=message('Invalid remote URL'), variant=repr(value))
                if rid == 'R087':
                    pair.request(helper, route, base + '?' + urlencode({'url': value}), expect=400, writes=True,
                                 interaction=Exchange(pair, source, expected=0), check=message('Invalid remote URL'), variant=repr(value))
        for status in (403, 404, 503):
            with Source(b'synthetic upstream failure') as source, Pair(binary, recorder) as pair:
                source.status = status
                request(pair, rid, route, base, 'F10', {'url': source.url + '/rules'}, expect=502, source=source, calls=1,
                        check=lambda r: require(r.json()['code'] == 'RULE_SOURCE_UNAVAILABLE' and r.headers.get('cache-control') == 'no-store',
                                                'failed live download never silently returns an old device snapshot'), variant=str(status))
        with Source(b'') as source, Pair(binary, recorder) as pair:
            source.status, source.headers = 302, {'Location': source.url + '/loop'}
            request(pair, rid, route, base, 'F11', {'url': source.url + '/loop'}, expect=400, source=source, calls=4,
                    check=message('Invalid remote URL'), variant='redirect-loop-is-url-validation-error')


def declarations(binary, recorder):
    for rid, route, base in ROUTES:
        with Source(b'example.com\n') as source:
            remote = {**INLINE, 'id': 'proxy-remote', 'name': '远端', 'source_type': 'url', 'content': '',
                      'url': source.url + '/rules', 'behavior': 'domain', 'format': 'text'}
            with Pair(binary, recorder, options=options([remote, INLINE])) as pair:
                for source_id in (remote['id'], 'proxy-ref-' + remote['id']):
                    write_cache(pair, remote, b'example.com\n')
                    request(pair, rid, route, base, 'B90', {'url': remote['url'], 'source_id': source_id}, source=source,
                            body=b'full:example.com', variant=source_id)
                vectors = [('missing', remote['url'], '规则来源已不存在，请重新生成并发布配置。', 'H106'),
                           (INLINE['id'], remote['url'], '规则来源已改为手写内容，请重新生成并发布配置。', 'H107'),
                           (remote['id'], source.url + '/different', '规则订阅地址已变更，请重新生成并发布配置。', 'H108')]
                for source_id, url, text, helper in vectors:
                    request(pair, rid, route, base, 'B90', {'url': url, 'source_id': source_id}, source=source,
                            expect=422, check=message(text), variant=source_id + '/' + url.rsplit('/', 1)[-1])
                    if rid == 'R087':
                        pair.request(helper, route, base + '?' + urlencode({'url': url, 'source_id': source_id}), expect=422, writes=True,
                                     interaction=Exchange(pair, source, expected=0), check=message(text))
            ambiguous = [remote, {**remote, 'id': 'proxy-different', 'name': '同URL不同声明', 'behavior': 'classical'}]
            with Pair(binary, recorder, options=options(ambiguous)) as pair:
                text = '同一订阅地址对应多个不同规则类型或格式，无法确定当前来源；请重新生成并发布配置。'
                request(pair, rid, route, base, 'B90', {'url': remote['url']}, source=source, expect=422,
                        check=message(text), variant='ambiguous-without-source-id')
                if rid == 'R087':
                    pair.request('H109', route, base + '?' + urlencode({'url': remote['url']}), expect=422, writes=True,
                                 interaction=Exchange(pair, source, expected=0), check=message(text))
        for case, rule_format, raw in [('F12', 'mrs', b'not fetched'), ('F13', 'text', b'DOMAIN,example.com\n'),
                                     ('B04', 'text', b'\xffinvalid-UTF8\n')]:
            with Source(raw) as source:
                remote = {**INLINE, 'id': 'proxy-invalid', 'name': '声明无效内容', 'source_type': 'url', 'content': '',
                          'url': source.url + '/rules', 'behavior': 'domain', 'format': rule_format}
                with Pair(binary, recorder, options=options([remote])) as pair:
                    configure(pair, 'rule_source:proxy-invalid')
                    request(pair, rid, route, base, case, {'url': remote['url'], 'source_id': remote['id']}, expect=422, source=source,
                            calls=0 if rule_format == 'mrs' else 1,
                            check=lambda r: require(r.json()['code'] == 'RULE_CONTENT_INVALID', 'declared source bytes fail their declared type before any invalid cache is installed'),
                            variant=rule_format + '/' + case, capture=capture_notice(pair, 'rule_source:proxy-invalid'))


GROUPS.update({'rule-proxy-guards': guards, 'rule-proxy-declarations': declarations})


def size_limits(binary, recorder):
    maximum = 5 * 1024 * 1024
    tail = b'\nexample.com\n'
    for rid, route, base in ROUTES:
        for case, scenario in [('B02', 'exact-boundary'), ('F14', 'declared-too-large'), ('F15', 'stream-too-large')]:
            with Source() as source, Pair(binary, recorder) as pair:
                source.body = b'#' + b'x' * (maximum - 1 - len(tail)) + tail
                if scenario == 'declared-too-large':
                    source.body = b'short synthetic response'
                    source.headers['Content-Length'] = str(maximum + 1)
                if scenario == 'stream-too-large':
                    source.body += b'x'
                    source.omit_content_length = True
                request(pair, rid, route, base, case, {'url': source.url + '/rules'}, source=source, calls=1,
                        expect=200 if case == 'B02' else 400, body=b'full:example.com' if case == 'B02' else None,
                        check=None if case == 'B02' else message('Invalid remote URL'), variant=scenario)


GROUPS['rule-proxy-size-limits'] = size_limits


def proxy_transport(binary, recorder):
    original = 'https://raw.githubusercontent.com/synthetic/rules/main/domains.list'
    rewritten = 'http://mirror.example/cache/' + original
    for rid, route, base in ROUTES:
        with Source(b'example.com\n') as source:
            source.record_wire = True
            class ProxyExchange(Exchange):
                def after(self, index, reply):
                    super().after(index, reply)
                    if index == 0:
                        connect, row = self.transcripts[0]
                        require(connect['method'] == 'CONNECT' and connect['path'] == 'raw.githubusercontent.com:443'
                            and connect['authorization'] == connect['proxy_authorization'] == '',
                            'original HTTPS origin is attempted through the fake proxy and explicitly refused before the mirror fallback')
                        require(row['request_line'] == 'GET ' + rewritten + ' HTTP/1.1' and row['host'] == 'mirror.example',
                                'forward proxy receives rewritten absolute URI, without resolving mirror or GitHub externally')
                        require(row['authorization'] == row['proxy_authorization'] == '', 'no panel JWT or internal capability leaks to upstream')
            opts = {'system_config': {'github_proxy_domain': 'http://mirror.example/cache', 'rule_fetch_proxy': source.url}}
            with Pair(binary, recorder, options=opts) as pair:
                before = database(pair.dirs[0])
                pair.request(rid + '-S91', route, base + '?' + urlencode({'url': original}), expect=200, writes=True, raw_body=True,
                             interaction=ProxyExchange(pair, source, expected=2),
                             check=lambda r: require(r.body == b'full:example.com' and database(pair.dirs[0]) == before,
                                                     'proxy conversion returns intended data without configuration commit'))


def processing_failure(binary, recorder):
    for rid, route, base in ROUTES:
        with Source(b'example.com\n') as source:
            remote = {**INLINE, 'id': 'proxy-storage-failure', 'source_type': 'url', 'content': '',
                      'url': source.url + '/rules', 'behavior': 'domain', 'format': 'text'}
            with Pair(binary, recorder, options=options([remote])) as pair:
                # A real filesystem collision, not a monkeypatch of either implementation.
                # Valid bytes are downloaded, but a directory cannot be atomically replaced
                # by the task receipt file; the public route must return its generic 500.
                for directory in pair.dirs:
                    path = directory / 'task-status.json'
                    if path.exists():
                        path.unlink()
                    path.mkdir()
                request(pair, rid, route, base, 'F16', {'url': remote['url'], 'source_id': remote['id']}, expect=500,
                        source=source, calls=1, check=message('Failed to process rule proxy request'), variant='receipt-path-is-directory')


GROUPS.update({'rule-proxy-transport': proxy_transport, 'rule-proxy-processing-failure': processing_failure})


def no_backup_errors(snapshot):
    from compare import differences
    return differences([], snapshot['requests'], 'rule-proxy.webdav.requests') + differences({}, snapshot['objects'], 'rule-proxy.webdav.objects')


def no_automatic_backup(binary, recorder):
    import time
    from fakes import WebDAV
    for rid, route, base in ROUTES:
        with WebDAV() as dav:
            opts = options([INLINE])
            opts['backup'] = {'auto_backup': True, 'webdav_url': dav.url, 'webdav_username': dav.username,
                              'webdav_password': dav.password, 'webdav_path': '/backup/'}
            with Pair(binary, recorder, options=opts, allow_backup_fixture=True) as pair:
                # Isolate the request from any prior startup action, then observe
                # six seconds after each request (source debounce is one second).
                time.sleep(6)
                class NoBackup:
                    def __init__(self):
                        self.observations = []
                    def before(self, index):
                        dav.reset()
                    def after(self, index, reply):
                        time.sleep(6)
                        snapshot = dav.snapshot()
                        self.observations.append(snapshot)
                        if index == 0:
                            require(not no_backup_errors(snapshot), 'inline rule-proxy does not schedule an automatic DAV backup')
                    def compare(self, policy):
                        return no_backup_errors(self.observations[1])
                before = database(pair.dirs[0])
                pair.request(rid + '-S04', route, base + '?' + urlencode({'url': '/api/profiles/default/rule-library/content/' + INLINE['id']}),
                             expect=200, writes=True, raw_body=True, variant='auto-backup-enabled-no-event', interaction=NoBackup(),
                             check=lambda r: require(r.body == b'full:inline.example' and database(pair.dirs[0]) == before,
                                                     'rule-proxy reads inline bytes without committing any SQL row'))


GROUPS['rule-proxy-no-backup'] = no_automatic_backup
