"""G10: native includes, original attachments and runtime/package isolation."""
import base64

import yaml

from artifacts import Observer
from assets import Assets
from downloads import capture_headers, validate_headers
from harness import Pair, DesignError, json_body
from cases.generator_shapes import project
from cases.subscriptions import require


def attachment(path, body, encoding='text'):
    return {'path': path, 'encoding': encoding,
            'content': base64.b64encode(body).decode() if encoding == 'base64' else body.decode()}


def fixture(shape):
    body = '# 原始🧪\r\ndomain:example.com\r\n  full:keep.example  \r\n'.encode()
    document = {'runtime': {'worker_threads': 3}, 'plugins': [
        {'tag': 'native_asset', 'type': 'domain_set', 'args': {'files': ['./rules/native.txt']}}]}
    expected = {'rules/native.txt': body}
    encoding = 'base64' if shape == 'native-base64' else 'text'
    files = [attachment('rules/native.txt', body, encoding)]
    if shape == 'native-includes':
        document['include'] = ['./plugins/first.yaml']
        document['plugins'] = [{'tag': 'last', 'type': 'debug_print'}]
        expected = {'plugins/first.yaml': b'include: [nested/second.yaml]\nruntime: {worker_threads: 99}\nplugins: [{tag: middle, type: debug_print}]\n',
                    'plugins/nested/second.yaml': b'include: [../../common.yaml]\nplugins: [{tag: first, type: debug_print}]\n',
                    'common.yaml': b'plugins: [{tag: shared, type: debug_print}]\n'}
        files = [attachment(path, value) for path, value in expected.items()]
    elif shape == 'native-resource-closure':
        document['api'] = {'http': {'listen': '127.0.0.1:9199',
            'ssl': {'cert': './tls/server.crt', 'key': './tls/server.key', 'client_ca': './tls/ca.pem'},
            'webui': {'root': './webui', 'index': 'index.html'}}}
        # Synthetic opaque resource bytes test packaging, not a TLS handshake.
        expected.update({'tls/server.crt': b'SYNTHETIC CERT\r\n', 'tls/server.key': b'SYNTHETIC PRIVATE KEY\r\n',
                         'tls/ca.pem': b'SYNTHETIC CA\n', 'webui/index.html': '<html>中文🧪</html>\r\n'.encode(),
                         'data/opaque.bin': b'\x00\xff\x01\x80'})
        files = [attachment(path, value, 'base64') for path, value in expected.items()]
    elif shape == 'native-runtime':
        document['plugins'].extend([
            {'tag': 'cache', 'type': 'cache', 'args': {'size': 1024, 'dump_file': './state/cache.dump'}},
            {'tag': 'queries', 'type': 'query_recorder', 'args': {'path': './state/queries.db'}},
            {'tag': 'learned', 'type': 'dynamic_domain_set', 'args': {'path': './state/learned.txt'}}])
    mode = 'template' if shape == 'template-text' else 'native'
    settings = {'config_mode': mode, 'additional_files': files,
                'custom_config' if mode == 'template' else 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
    return settings, expected


def matrix(binary, recorder):
    for shape in ('native-text', 'native-base64', 'template-text', 'native-includes', 'native-resource-closure', 'native-runtime'):
        settings, expected = fixture(shape)
        for scoped in (False, True):
            prefix = '/api/profiles/default/generate' if scoped else '/api/generate'
            template = '/api/profiles/<profile_id>/generate' if scoped else '/api/generate'
            with Pair(binary, recorder, options={'profile': {'oxidns': settings}}) as pair:
                runtime = {'cache.dump': b'synthetic-live-cache\x00', 'queries.db': b'synthetic-live-query-state',
                           'learned.txt': b'domain:learned.example\n'}
                for directory in pair.dirs:
                    folder = directory / 'profiles/default/state'
                    folder.mkdir(parents=True, exist_ok=True)
                    for name, value in runtime.items():
                        (folder / name).write_bytes(value)
                def check(reply):
                    config = yaml.safe_load(reply.json()['content'])
                    if shape == 'native-includes':
                        require([item['tag'] for item in config['plugins']] == ['shared', 'first', 'middle', 'last']
                                and 'include' not in config and config['runtime']['worker_threads'] == 3,
                                'includes expand in device order, parent paths stay inside managed root, and included globals are ignored')
                    for directory in pair.dirs[:1]:
                        require(all((directory / 'profiles/default/state' / name).read_bytes() == value for name, value in runtime.items()),
                                'generation never mutates live runtime files')
                pair.request('G10', 'POST ' + template + '/oxidns/preview', prefix + '/oxidns/preview', body='{}',
                             expect=200, writes=True, projection=project('oxidns'), check=check, variant=shape + '/' + str(scoped) + '/preview')
                assets = Assets(expected)
                def archive_check(reply):
                    assets.reference(reply)
                    errors = validate_headers(reply, pair.policy.start, pair.policy.end)
                    if errors:
                        raise DesignError('Python native archive headers: ' + repr(errors))
                def archive_capture(replies, policy):
                    return assets.capture(replies, policy) + capture_headers(replies, policy)
                pair.request('G10', 'POST ' + template + '/oxidns', prefix + '/oxidns', body='{}', expect=200,
                             writes=True, check=archive_check, capture=archive_capture, filename='oxidns-config.zip',
                             variant=shape + '/' + str(scoped) + '/zip')
                observer = Observer(pair)
                bundle_assets = Assets(expected, observer)
                pair.request('G10', 'POST ' + template + '/<service>/artifact', prefix + '/oxidns/artifact', body='{}',
                             expect=200, writes=True, files=False, check=bundle_assets.reference,
                             capture=bundle_assets.capture, projection=observer.public, variant=shape + '/' + str(scoped) + '/artifact')


def guards(binary, recorder):
    for shape in ('missing-resource', 'runtime-overlap'):
        settings, _ = fixture('native-text')
        if shape == 'missing-resource':
            settings['additional_files'] = []
        else:
            settings['native_config'] = 'plugins: [{tag: cache, type: cache, args: {size: 1024, dump_file: cache.dump}}]\n'
            settings['additional_files'] = [attachment('cache.dump', b'synthetic live state')]
        with Pair(binary, recorder, options={'profile': {'oxidns': settings}}) as pair:
            pair.request('G10', 'POST /api/generate/oxidns/preview', body='{}', expect=200, writes=True,
                         projection=project('oxidns'), variant=shape + '/preview-is-not-complete-package')
            expected = '未打包的资源文件' if shape == 'missing-resource' else '运行数据不能与附加文件重合'
            pair.request('G10', 'POST /api/generate/oxidns', body='{}', expect=400, writes=True,
                         check=lambda r: require(expected in r.json()['message'], 'complete package rejects missing/read-write-overlapping resources'),
                         variant=shape + '/reject-package')
    for path in ('state/cache.dump', 'state/queries.db', 'state/learned.txt'):
        with Pair(binary, recorder) as pair:
            settings = {'config_mode': 'native', 'native_config': 'plugins: []\n',
                        'additional_files': [attachment(path, b'synthetic state') ]}
            pair.request('G10', 'POST /api/oxidns/render', body=json_body({'settings': settings}), expect=400, writes=True,
                         check=lambda r: require('state/ 运行数据' in r.json()['message'], 'mutable state can never be declared as deployable attachment'),
                         variant='state-file-rejected/' + path)


GROUPS = {'oxidns-managed-assets': matrix, 'oxidns-asset-guards': guards}
