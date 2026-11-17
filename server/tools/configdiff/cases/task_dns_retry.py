"""DNS retry admission persists intent while awaiting the next source publish."""

from compare import database, timestamp
from harness import Pair, json_body
from seed import AGENT
from sources import Source
from subscriptions import Exchange
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require

TEXT = '已安排重试，将重新确认 Mihomo 当前 DNS 地址'


def retry(binary, recorder):
    with Source(b'retry waits for a successful source deployment') as device:
        common = {'profile_id': 'default', 'host': '127.0.0.1', 'port': device.server.server_port,
                  'enabled': True, 'version': '1.8.60-go', 'deployment_method': 'shell'}
        agents = [{**common, 'id': 'source-retry', 'name': '合成来源', 'token': AGENT, 'service_type': 'mihomo'},
                  {**common, 'id': 'target-retry', 'name': '合成目标', 'token': AGENT + '-target', 'service_type': 'oxidns',
                   'dns_follow': {'id': 'synthetic-binding', 'source_agent_id': 'source-retry',
                       'source_profile_id': 'default', 'target_profile_id': 'default', 'upstream_tag': 'forward_local',
                       'upstream_index': 0, 'connection': 'network', 'address': '192.0.2.20'},
                   'dns_follow_state': {'status': 'failed', 'message': '合成旧失败', 'attempt': 2,
                       'source_deployment_id': 'synthetic-old-deployment', 'events': []},
                   'dns_follow_job': {'synthetic': 'old-preparation'}}]
        with Pair(binary, recorder, options={'agents': agents}) as pair:
            before = database(pair.dirs[0])
            root = 'db.fields.system//agents.value[1].dns_follow_state.'
            pair.policy.times.update({root + 'updated_at', root + 'events[0].at'})
            def check(reply):
                after = database(pair.dirs[0])
                saved = after['fields']['system//agents']['value']
                state = saved[1]['dns_follow_state']
                require('dns_follow_job' not in saved[1] and saved[0] == before['fields']['system//agents']['value'][0],
                        'retry clears only the old target job and leaves source device exact')
                expected = {'status': 'waiting', 'source_deployment_id': 'synthetic-old-deployment',
                    'retry_requested': True, 'attempt': 3, 'message': TEXT,
                    'updated_at': state.get('updated_at'), 'events': state.get('events')}
                require(state == expected and timestamp(state['updated_at'], pair.policy),
                        'retry persists precisely one incremented attempt and a current waiting timestamp')
                require(len(state['events']) == 1 and state['events'][0] ==
                    {'status': 'waiting', 'message': TEXT, 'at': state['events'][0].get('at')}
                    and timestamp(state['events'][0]['at'], pair.policy), 'retry records one actual waiting event')
                foreign = lambda d: {k: v for k, v in d['fields'].items() if k not in ('system//agents', 'system//_revision')}
                require(foreign(before) == foreign(after) and before['sections'] == after['sections'],
                        'retry does not touch any profile, shared resource or unrelated system field')
                require(after['meta']['generation'] == before['meta']['generation'] + 2,
                        'reset and waiting event are two durable source transactions')
            pair.request('R227-S06', 'POST /api/tasks/run', body=json_body({'key': 'dns_follow:target-retry'}),
                         expect=202, writes=True, runtime_writes=True, check=check,
                         interaction=Exchange(pair, device, expected=0),
                         variant='real-persisted-retry-intent-waits-for-next-source-publication')
            pair.request('R227-F15', 'POST /api/tasks/run', body=json_body({'key': 'dns_follow:target-retry'}),
                         expect=409, writes=True, interaction=Stable(pair),
                         check=message('只有已确认失败的 DNS 联动可以重试；执行中的任务会继续确认结果'),
                         variant='repeated-retry-while-waiting-keeps-existing-intent')


GROUPS = {'tasks-DNS-follow-retry-intent': retry}
