"""Provider health schema is checked by actual shared-resource writes."""
from harness import Pair, json_body
from sources import Source
from cases.aggregations import AGG, options
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.subscriptions import SUB


def guarded(binary, recorder):
    for kind in ('subscriptions', 'aggregations'):
        for method in ('POST', 'PUT'):
            with Source(b'health shape validation must not contact sources') as source:
                opts = options()
                opts['shared']['subscriptions'][0]['url'] = source.url + '/feed'
                with Pair(binary, recorder, options=opts) as pair:
                    base = {**SUB, 'url': source.url + '/feed'} if kind == 'subscriptions' else dict(AGG)
                    if method == 'POST':
                        base.update(id='new-health', name='新增健康检查🧪')
                    route = method + ' /api/' + kind + ('/<sub_id>' if kind == 'subscriptions' else '/<agg_id>') if method == 'PUT' else method + ' /api/' + kind
                    path = '/api/' + kind + (('/sub-a' if kind == 'subscriptions' else '/agg-a') if method == 'PUT' else '')
                    vectors = [('H240', value, '健康检查必须是对象') for value in (None, [], 1, 'invalid', False)]
                    vectors += [('H241', value, '健康检查包含未知字段')
                                for value in ({'unknown': True}, {'enable': True, 'url': 'https://probe.example/', 'unknown': 1})]
                    for case, value, text in vectors:
                        pair.request(case, route, path, body=json_body({**base, 'health_check': value}),
                            expect=400, writes=True, check=message(text), interaction=SourceStable(pair, source),
                            variant=kind + '/' + method + '/' + repr(value))


GROUPS = {'provider-health-shared-write-guards': guarded}
