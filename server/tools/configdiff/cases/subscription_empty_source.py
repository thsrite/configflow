"""Actual empty node downloads preserve old cache and record failure receipts."""
import copy
import hashlib
import json

from compare import database, files, timestamp
from harness import Pair, json_body
from sources import Source
from subscriptions import configure, Exchange, capture_notice
from cases.subscription_fetch import pair_options
from cases.subscriptions import CACHE_NODES, STAMP, put_file, require


def empty(binary, recorder):
    for previous in (False, True):
        with Source(b'proxies: []\n') as source, Pair(binary, recorder, options=pair_options(source.url + '/empty')) as pair:
            configure(pair, source.url + '/empty')
            if previous:
                put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                    'count': len(CACHE_NODES), 'nodes': copy.deepcopy(CACHE_NODES), 'metadata': {'url': source.url + '/empty'}})
                for field in ('updated_at', 'metadata.fetched_at'):
                    pair.policy.times.discard('files.shared/subscribes/sub-a.json.content.' + field)
                pair.policy.times.discard('body.cached_updated_at')
                pair.policy.times.discard('files.task-status.json.content.subscription:sub-a.cache_updated_at')
            before_db = database(pair.dirs[0])
            cache_path = pair.dirs[0] / 'shared/subscribes/sub-a.json'
            before_cache = cache_path.read_bytes() if previous else None
            def check(reply):
                task = json.loads((pair.dirs[0] / 'task-status.json').read_text())['subscription:sub-a']
                require(task.get('last_status') == 'failed' and task.get('last_error') == '订阅没有返回任何节点，已保留原有缓存'
                    and task.get('cache_retained') is previous, 'empty source reaches the actual download-empty guard and failed task receipt')
                require(database(pair.dirs[0]) == before_db, 'empty upstream never imports or changes any SQL field')
                require((cache_path.read_bytes() if cache_path.exists() else None) == before_cache,
                    'empty downloaded source preserves complete raw old node cache or its original absence')
                if previous:
                    require(reply.json()['nodes'] == CACHE_NODES and reply.json()['from_cache'] is True,
                        'manual preview returns exactly the old nodes after empty source rejection')
                transport = 'shared/subscription-downloads/' + hashlib.sha256((source.url + '/empty').encode()).hexdigest() + '.json'
                record = files(pair.dirs[0])[transport]
                require(record['mode'] == 0o600 and record['content']['text'] == 'proxies: []\n'
                    and timestamp(record['content']['fetched_at'], pair.policy), 'transport cache retains actual empty raw response')
            pair.request('H1085', 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                body=json_body({'preview': True}), expect=200 if previous else 500, writes=True,
                interaction=Exchange(pair, source), check=check, capture=capture_notice(pair), variant='old-cache' if previous else 'no-cache')


GROUPS = {'subscription-real-empty-download-keeps-cache': empty}
