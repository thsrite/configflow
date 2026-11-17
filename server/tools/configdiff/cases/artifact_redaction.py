"""X01 artifact content exception with a capability in non-content metadata."""
import copy
from urllib.parse import quote, quote_plus

from artifacts import Observer, ArtifactError, require
from harness import Pair
from seed import CURRENT
from cases.rule_library import DOMAIN


class RedactedPath(Observer):
    """Validate one exact redacted path, keeping all other metadata strict."""
    def __init__(self, pair, path):
        super().__init__(pair)
        self.path = path

    def public(self, value, index):
        if self.checked[index] is None:
            return value
        try:
            manifest = self.checked[index]['record']['bundle_meta']['manifest']
            expected = copy.deepcopy(manifest['files'])
            matches = [i for i, item in enumerate(expected) if item['path'] == self.path]
            require(len(matches) == 1, 'one real capability-named attachment must be in the archive')
            offset = matches[0]
            expected[offset]['path'] = '[REDACTED]'
            require(value['files'] == expected, 'only the secret-bearing public path is redacted; digests, sizes and siblings remain exact')
            restored = copy.deepcopy(value)
            restored['files'][offset]['path'] = self.path
            result = super().public(restored, index)
            result['files'][offset]['path'] = '[REDACTED]'
            return result
        except (ArtifactError, KeyError, TypeError) as error:
            if index == 0:
                raise
            # Keep the incorrect Go response observable instead of aborting a
            # real comparison as a fixture error.
            return {'invalid_public_artifact': str(error), 'actual': value}


def matrix(binary, recorder):
    for label, encode in [('raw', lambda value: value), ('percent', lambda value: quote(value, safe='')),
                          ('double-percent', lambda value: quote(quote(value, safe=''), safe='')), ('plus', quote_plus)]:
        path = 'rules/' + encode(CURRENT) + '.txt'
        options = {'x01_native': True, 'shared': {'rule_library': [DOMAIN]}, 'profile': {
            'rule_configs': [{'id': 'redaction-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True}],
            'oxidns': {'direct_rulesets': ['redaction-rule'],
                       'additional_files': [{'path': path, 'content': '+.redaction.example\n', 'encoding': 'text'}]}}}
        for rid, template, url in [('R062', 'POST /api/generate/<service>/artifact', '/api/generate/oxidns/artifact'),
                ('R126', 'POST /api/profiles/<profile_id>/generate/<service>/artifact', '/api/profiles/default/generate/oxidns/artifact')]:
            with Pair(binary, recorder, options=options) as pair:
                observer = RedactedPath(pair, path)
                def check(reply):
                    observer.reference(reply, secret=CURRENT)
                    content = reply.json()['content']
                    require('/mosdns/rule-proxy?' in content and quote(CURRENT, safe='') in content,
                            'content preserves a real generated authenticated rule callback URL')
                    require(observer.checked[0]['raw']['files/' + path] == b'+.redaction.example\n',
                            'the private bundle keeps the full original secret-bearing path and attachment bytes')
                pair.request(rid + '-B90', template, url, body='{}', expect=200, writes=True, files=False,
                             check=check, capture=observer.capture, projection=observer.public,
                             variant='X01-content-exempt-metadata-redacted/' + label)


GROUPS = {'artifact-redaction-metadata-boundary': matrix}
