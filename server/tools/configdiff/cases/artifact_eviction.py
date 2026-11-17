"""Exercise the real 24-preview count bound without changing its timer."""
import time

from artifacts import Observer
from harness import Pair
from cases.artifact_frozen import Frozen, READ
from cases.setup import Stable
from cases.subscriptions import require


def count_bound(binary, recorder):
    for scoped in (False, True):
        with Pair(binary, recorder) as pair:
            route = ('POST /api/profiles/<profile_id>/generate/<service>/artifact' if scoped
                     else 'POST /api/generate/<service>/artifact')
            path = '/api/profiles/default/generate/mihomo/artifact' if scoped else '/api/generate/mihomo/artifact'
            retained, first_ids, last_ids, last_bytes = [], None, None, None
            for number in range(25):
                observer = Observer(pair)
                def check(reply):
                    observer.reference(reply)
                    retained.append(reply.json()['id'])
                    root = pair.dirs[0] / '.generated-previews'
                    actual = {folder.name for folder in root.iterdir() if folder.is_dir()}
                    require(actual == set(retained[-24:]), 'private root retains exactly the newest 24 actual preview IDs')
                replies = pair.request('R126-B01' if scoped else 'R062-B01', route, path, body='{}', expect=200,
                    writes=True, files=False, check=check, capture=observer.capture, projection=observer.public,
                    variant=('scoped' if scoped else 'default') + '/real-preview-' + format(number + 1, '02'))
                ids = [reply.json()['id'] if reply.status else '0' * 32 for reply in replies]
                if first_ids is None:
                    first_ids = ids
                last_ids = ids
                last_bytes = [checked['raw']['files/config.yaml'] if checked else b'' for checked in observer.checked]
                # Separate real filesystem mtimes; never alter the production
                # TTL or let a coarse mtime tie decide the "oldest" fixture.
                time.sleep(0.02)
            label = 'scoped' if scoped else 'default'
            pair.request('R063-F10', READ, paths=['/api/generate/artifacts/' + key + '/download' for key in first_ids],
                expect=409, writes=True, interaction=Stable(pair), variant=label + '/oldest-actually-evicted-not-rerendered',
                check=lambda reply: require(reply.json() == {'success': False, 'code': 'preview_changed',
                    'message': '生成结果已过期或不可用，请重新生成后推送。'}, 'oldest missing artifact returns exact explicit regeneration conflict'))
            pair.request('R063-S02', READ, paths=['/api/generate/artifacts/' + key + '/file?path=config.yaml' for key in last_ids],
                expect=200, writes=True, interaction=Frozen(pair, last_bytes), filename='config.yaml',
                variant=label + '/newest-retained-original-member')


GROUPS = {'artifact-real-count-eviction': count_bound}
