"""Non-YAML conversion and fallback failures use real HTTP, not thrown hooks."""
import json

from compare import database
from harness import Pair, json_body
from sources import Source
from substore import SubStore, Exchange
from subscriptions import configure, capture_notice
from cases.subscription_fetch import pair_options
from cases.subscriptions import require


def failures(binary, recorder):
    for fragment in (False, True):
        upstream = b'<html>synthetic non-YAML upstream</html>'
        with Source(upstream) as source, SubStore(b'<html>synthetic non-YAML conversion</html>') as store:
            url = source.url + '/feed' + ('#synthetic-options' if fragment else '')
            options = pair_options(url)
            options['environment'] = {'SUB_STORE_URL': store.url}
            with Pair(binary, recorder, options=options) as pair:
                configure(pair, url)
                baseline = database(pair.dirs[0])
                exchange = Exchange(source, store, url, None if fragment else upstream.decode(),
                    ['POST', 'GET', 'DELETE', 'source'] if fragment else ['source', 'POST', 'GET', 'DELETE'])
                def check(reply):
                    value = reply.json()
                    require(value.get('success') is False and value.get('message', '').startswith('从订阅URL获取失败: request_failed (')
                        and value['message'].endswith('，且本地无缓存数据'), 'conversion and direct fallback fail with the exact public failure envelope')
                    require(database(pair.dirs[0]) == baseline, 'failed conversion never imports any SQL row')
                    require(not (pair.dirs[0] / 'shared/subscribes/sub-a.json').exists(), 'failed conversion never manufactures node cache')
                    task = json.loads((pair.dirs[0] / 'task-status.json').read_text())['subscription:sub-a']
                    require(task['last_status'] == 'failed' and task['cache_retained'] is False
                        and task['last_error'] == ('更新失败（Exception）' if fragment else '已拉取的订阅正文无法转换为目标 YAML'),
                        'failure receipt reflects the actual exhausted direct and conversion branches')
                pair.request('H1074' if fragment else 'H1073', 'POST /api/subscriptions/<sub_id>/fetch',
                    '/api/subscriptions/sub-a/fetch', body=json_body({'preview': True}), expect=500, writes=True,
                    interaction=exchange, capture=capture_notice(pair), check=check,
                    variant='fragment-both-non-YAML' if fragment else 'already-downloaded-body-never-redownloaded')
                pair.recorder.results[-1]['helper_cases'] = ['H1072'] + (['H1070'] if fragment else [])


GROUPS = {'subscription-real-non-YAML-conversion-and-direct-failures': failures}
