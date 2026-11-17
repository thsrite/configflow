"""Real extra-file previews, TTL, profile selection and reads after live edits."""
from urllib.parse import quote, urlencode

import yaml

from artifacts import Observer
from compare import database, differences
from harness import Pair, json_body
from oxidns import check as etag_check, capture as etag_capture
from cases.helpers import profile_policy
from cases.setup import Stable
from cases.subscriptions import require

EXTRA_PATH = 'rules/中文🧪.txt'
EXTRA = b'+.frozen.example\n'
READ = 'GET /api/generate/artifacts/<preview_id>/<action>'


def settings(worker=2):
    document = {'runtime': {'worker_threads': worker}, 'plugins': [
        {'tag': 'frozen_domains', 'type': 'domain_set', 'args': {'files': ['./' + EXTRA_PATH]}}]}
    return {'config_mode': 'native', 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False),
            'additional_files': [{'path': EXTRA_PATH, 'content': EXTRA.decode(), 'encoding': 'text'}]}


class Frozen(Stable):
    def __init__(self, pair, expected):
        super().__init__(pair)
        self.expected, self.observed = expected, [None, None]

    def after(self, index, reply):
        super().after(index, reply)
        self.observed[index] = {'own_frozen_bytes': reply.body == self.expected[index]}
        if index == 0:
            require(self.observed[0]['own_frozen_bytes'], 'file comes from the original validated bundle, even after live settings changed')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'artifact.frozen-read', policy)


def lifecycle(binary, recorder):
    options = {'profile': {'oxidns': settings(2)}, 'other_profile': {'oxidns': settings(5)}}
    with Pair(binary, recorder, options=options) as pair:
        pair.policy = profile_policy(pair)
        first_ids, frozen = None, None
        recipes = [('R062-B02', '/api/generate/oxidns/artifact', {}, None, 'default', 'TTL-3600'),
                   ('R062-B03', '/api/generate/oxidns/artifact', {}, None, 'default', 'unicode-extra-file'),
                   ('R062-B05', '/api/generate/oxidns/artifact', {'profile': 'default', 'profile_id': 'other'}, 'other', 'default', 'P4'),
                   ('R062-B06', '/api/generate/oxidns/artifact', {}, 'other', 'other', 'P5'),
                   ('R062-B07', '/api/generate/oxidns/artifact', {}, None, 'default', 'P6'),
                   ('R126-B02', '/api/profiles/default/generate/oxidns/artifact', {}, None, 'default', 'TTL-3600'),
                   ('R126-B03', '/api/profiles/default/generate/oxidns/artifact', {}, None, 'default', 'unicode-extra-file'),
                   ('R126-B04', '/api/profiles/default/generate/oxidns/artifact', {'profile': 'other'}, 'other', 'default', 'P3-URL-wins')]
        for case, path, query, header, owner, variant in recipes:
            route = ('POST /api/profiles/<profile_id>/generate/<service>/artifact' if case.startswith('R126')
                     else 'POST /api/generate/<service>/artifact')
            observer = Observer(pair)
            def check(reply):
                observer.reference(reply)
                value = reply.json()
                require(value['profile_id'] == owner and reply.headers.get('x-configflow-profile') == owner,
                        'stored/public artifact belongs to exactly the selected profile')
                raw = observer.checked[0]['raw']
                document = yaml.safe_load(raw['files/config.yaml'])
                require(document['runtime']['worker_threads'] == (2 if owner == 'default' else 5)
                    and raw['files/' + EXTRA_PATH] == EXTRA
                    and EXTRA_PATH in [item['path'] for item in value['files']],
                    'selected profile marker and complete Unicode attachment are really in archive and public manifest')
            replies = pair.request(case, route, path + ('?' + urlencode(query) if query else ''), body='{}',
                headers={'X-ConfigFlow-Profile': header}, expect=200, writes=True, files=False,
                check=check, capture=observer.capture, projection=observer.public, variant=variant + '/real-extra-file')
            if first_ids is None:
                first_ids = [reply.json()['id'] if reply.status else '0' * 32 for reply in replies]
                frozen = [checked['raw']['files/config.yaml'] if checked else b'' for checked in observer.checked]
        extra = [EXTRA, EXTRA]
        paths = ['/api/generate/artifacts/' + key + '/file?path=' + quote(EXTRA_PATH, safe='') for key in first_ids]
        pair.request('R063-S03', READ, paths=paths, expect=200, writes=True,
            interaction=Frozen(pair, extra), raw_body=True, variant='Unicode-extra-file-original-bytes')
        changed = {'config_mode': 'native', 'native_config': 'runtime:\n  worker_threads: 9\nplugins:\n- tag: changed\n  type: cache\n  args: {size: 99}\n'}
        def edited(reply):
            etag_check(pair)(reply)
            saved = database(pair.dirs[0])['fields']['profile/default/oxidns']['value']
            require(saved['native_config'] == changed['native_config'], 'successful public edit commits a different live config')
        pair.request('R112-S01', 'POST /api/oxidns/settings', body=json_body(changed), expect=200, writes=True,
            capture=etag_capture(pair), check=edited, variant='live-edit-after-frozen-extra-file-preview')
        for case, query, header, owner, variant in [('R063-B02', {}, None, 'default', 'after-live-settings-edit'),
                ('R063-B04', {'profile': 'default', 'profile_id': 'other'}, 'other', 'default', 'P4'),
                ('R063-B05', {}, 'other', 'other', 'P5-global-manual-preview'),
                ('R063-B06', {}, None, 'default', 'P6')]:
            suffix = urlencode({'path': 'config.yaml', **query})
            paths = ['/api/generate/artifacts/' + key + '/file?' + suffix for key in first_ids]
            pair.request(case, READ, paths=paths, expect=200, writes=True, interaction=Frozen(pair, frozen),
                headers={'X-ConfigFlow-Profile': header}, filename='config.yaml', variant=variant + '/unchanged-old-member',
                check=lambda reply: require(reply.headers.get('x-configflow-profile') == owner,
                    'context selection stays visible while authorized manual artifact still reads its frozen global record'))


GROUPS = {'artifact-extra-frozen-profile-lifecycle': lifecycle}
