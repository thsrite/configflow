"""Mihomo 流量记录：上报校验、内存累加与按监控历史间隔保存、合并旧数据、查询与开关。"""
import sqlite3
import time
from pathlib import Path

import pytest

from backend.agents import metrics_flush
from backend.agents import traffic_history as module
from backend.agents.traffic_history import DAY, HOUR, OTHER_TARGET, UNATTRIBUTED, TrafficHistory, clean_report
from backend.common import config as config_module
from backend.test_qa_integrity import make_app

TOKEN = 'agent-token-0123456789abcdef'
NOW = 1_793_500_000  # 2026-11-01 某时，非整点
THIS_HOUR = NOW // HOUR * HOUR


def usage(hour=THIS_HOUR, source='10.0.0.2', group='Proxy', outbound='HK', up=1, down=10, conns=1):
    return {'hour': hour, 'source': source, 'group': group, 'outbound': outbound, 'up': up, 'down': down, 'conns': conns}


def target(hour=THIS_HOUR, source='10.0.0.2', domain='www.example.com', outbound='HK', up=1, down=10, conns=1):
    return {'hour': hour, 'source': source, 'domain': domain, 'outbound': outbound, 'up': up, 'down': down, 'conns': conns}


def report(usage_items=(), target_items=()):
    return {'schema': 1, 'usage': list(usage_items), 'domains': list(target_items), 'dropped': 0}


def test_clean_report_merges_targets_by_main_domain_and_rejects_bad_items():
    cleaned = clean_report(report([usage(), usage(source='', group='x', outbound='y')],
                                  [target(domain='a.b.example.com.cn'), target(domain='203.0.113.9'), target(domain='')]), now=NOW)
    # 未归属的一行不带策略组和出口
    assert cleaned['usage'][1][:4] == (THIS_HOUR, UNATTRIBUTED, '', '')
    assert [row[2:4] for row in cleaned['targets']] == [('example.com.cn', 'HK'), ('203.0.113.9', 'HK'), (OTHER_TARGET, 'HK')]
    for bad in (usage(hour=THIS_HOUR + 1), usage(up=-1), usage(down='1'), usage(source='x' * 65), None):
        assert clean_report(report([bad]), now=NOW) is None
    missing_outbound = target()
    del missing_outbound['outbound']
    assert clean_report(report([], [missing_outbound]), now=NOW) is None
    assert clean_report({'schema': 2, 'usage': [], 'domains': []}, now=NOW) is None
    # 积压超过 2 天的整点数据直接丢弃，其余照收
    stale = clean_report(report([usage(hour=THIS_HOUR - 3 * DAY), usage()]), now=NOW)
    assert len(stale['usage']) == 1


def test_reports_stay_in_memory_until_flush_and_queries_include_pending(tmp_path):
    history = TrafficHistory(str(tmp_path))
    history.add_report('a1', clean_report(report(
        [usage(down=100), usage(group='Media', outbound='DIRECT', down=40), usage(source='10.0.0.3', group='DIRECT', outbound='DIRECT', down=50),
         usage(source='', down=9)],
        [target(down=100), target(outbound='DIRECT', down=40)]), now=NOW))
    assert not (tmp_path / module.FILENAME).exists()
    by_source = history.query('a1', '24h', 'source', now=NOW)
    assert [(row['key'], row['down']) for row in by_source['rows']] == [('10.0.0.2', 140), ('10.0.0.3', 50), (UNATTRIBUTED, 9)]
    # 每行按出口节点拆开，未归属没有明细；出口节点总量不含未归属
    assert [(part['key'], part['down']) for part in by_source['rows'][0]['parts']] == [('HK', 100), ('DIRECT', 40)]
    assert by_source['rows'][2]['parts'] == []
    assert [(row['key'], row['down']) for row in by_source['outbounds']] == [('HK', 100), ('DIRECT', 90)]
    assert by_source['total'] == {'up': 4, 'down': 199, 'conns': 4}
    # 按出站节点汇总时按设备拆开
    by_outbound = history.query('a1', '24h', 'outbound', now=NOW)
    direct = next(row for row in by_outbound['rows'] if row['key'] == 'DIRECT')
    assert [(part['key'], part['down']) for part in direct['parts']] == [('10.0.0.3', 50), ('10.0.0.2', 40)]
    destination = history.query('a1', '24h', 'destination', now=NOW)
    assert [(part['key'], part['down']) for part in destination['rows'][0]['parts']] == [('HK', 100), ('DIRECT', 40)]
    assert history.flush(now=NOW) == {'rows': 6, 'compacted': 0, 'pruned': 0}
    assert history.pending_count() == 0
    # 保存后再收到同一小时的数据，累加而不是覆盖
    history.add_report('a1', clean_report(report([usage(down=1)]), now=NOW))
    history.flush(now=NOW)
    rows = sqlite3.connect(tmp_path / module.FILENAME).execute(
        "SELECT down FROM usage WHERE source = '10.0.0.2' AND outbound = 'HK'").fetchall()
    assert rows == [(101,)]
    group = history.query('a1', '24h', 'group', now=NOW)['rows'][0]
    assert (group['key'], group['down'], group['conns']) == ('Proxy', 101, 2)
    destination = history.query('a1', '24h', 'destination', now=NOW)
    assert [(row['key'], row['down']) for row in destination['rows']] == [('example.com', 140), (UNATTRIBUTED, 9)]
    assert len(destination['trend']) == 25 and destination['trend'][-1]['down'] == 200


def test_failed_flush_keeps_the_data_for_the_next_run(tmp_path, monkeypatch):
    history = TrafficHistory(str(tmp_path / 'metrics'))
    history.add_report('a1', clean_report(report([usage(down=7)]), now=NOW))

    def broken(self):
        raise sqlite3.OperationalError('disk I/O error')
    monkeypatch.setattr(TrafficHistory, '_connect', broken)
    with pytest.raises(sqlite3.OperationalError):
        history.flush(now=NOW)
    history.add_report('a1', clean_report(report([usage(down=3)]), now=NOW))
    monkeypatch.undo()
    history.flush(now=NOW)
    assert history.query('a1', '24h', 'source', now=NOW)['total']['down'] == 10


def test_old_hours_are_merged_into_days_and_expired_days_are_deleted(tmp_path):
    history = TrafficHistory(str(tmp_path))
    today = NOW // DAY * DAY
    old = today - 10 * DAY + 5 * HOUR
    cleaned = {
        'usage': [(old, '10.0.0.2', 'Proxy', 'HK', 1, 10, 1), (old + HOUR, '10.0.0.2', 'Proxy', 'HK', 1, 20, 1),
                  (today - 200 * DAY, '10.0.0.2', 'Proxy', 'HK', 1, 1, 1)],
        # 同一目标走两个出口时按合计排名；被挤出的目标按出口并入「其他」
        'targets': [(old, '10.0.0.2', f'site{i}.example', 'HK', 0, 1000 - i, 1) for i in range(module.TARGET_DAILY_TOP + 2)]
        + [(old + HOUR, '10.0.0.2', 'site101.example', 'DIRECT', 0, 1, 1)],
    }
    history.add_report('a1', cleaned)
    result = history.flush(retention=90, now=NOW)
    assert result['pruned'] == 1
    db = sqlite3.connect(tmp_path / module.FILENAME)
    assert db.execute('SELECT bucket, span, down FROM usage').fetchall() == [(today - 10 * DAY, DAY, 30)]
    # 每台设备每天只留流量最大的若干个目标，其余并入「其他」
    targets = {(target, outbound): down for target, outbound, down in db.execute('SELECT target, outbound, down FROM targets')}
    assert len(targets) == module.TARGET_DAILY_TOP + 2
    assert targets[(OTHER_TARGET, 'HK')] == (1000 - module.TARGET_DAILY_TOP) + (1000 - module.TARGET_DAILY_TOP - 1)
    assert targets[(OTHER_TARGET, 'DIRECT')] == 1
    assert history.query('a1', '30d', 'source', now=NOW)['total']['down'] == 30


def test_delete_and_retain_remove_agents(tmp_path):
    history = TrafficHistory(str(tmp_path))
    for agent in ('a1', 'a2', 'a3'):
        history.add_report(agent, clean_report(report([usage()]), now=NOW))
    history.flush(now=NOW)
    history.add_report('a1', clean_report(report([usage()]), now=NOW))
    history.delete('a1')
    assert history.query('a1', '24h', 'source', now=NOW)['rows'] == []
    assert history.retain_agents(['a2']) == 1
    assert history.query('a3', '24h', 'source', now=NOW)['rows'] == []
    assert history.query('a2', '24h', 'source', now=NOW)['total']['down'] == 10


@pytest.fixture
def env(tmp_path, monkeypatch):
    app, repo = make_app(tmp_path / 'config')
    repo.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'gw', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': TOKEN, 'host': 'h', 'port': 8080,
         'version': '1.8.51-go'},
        {'id': 'dns', 'name': 'DNS', 'service_type': 'oxidns', 'profile_id': 'default', 'token': TOKEN + 'x', 'host': 'h', 'port': 8080}]))
    from backend.common import agent_manager as singleton
    monkeypatch.setattr(singleton, '_agent_manager', None)
    monkeypatch.setattr(metrics_flush, '_started_at', None)
    yield app.test_client(), repo, singleton.get_agent_manager()
    config_module.reset_config_context()


def auth(token=TOKEN):
    return {'Authorization': f'Bearer {token}'}


def test_switch_reports_and_heartbeat(env):
    client, repo, manager = env
    url = '/api/agents/gw/traffic-history'
    body = report([usage(hour=int(time.time()) // HOUR * HOUR)])
    assert client.post(url, json=body).status_code == 401
    assert client.post(url, json=body, headers=auth()).status_code == 409
    assert client.post('/api/agents/gw/heartbeat', json={}, headers=auth()).get_json()['traffic_history_enabled'] is False

    response = client.put(url, json={'enabled': True})
    assert response.status_code == 200 and response.get_json()['data']['enabled'] is True
    since = repo.get_system()['agents'][0]['traffic_history_since']
    assert client.post('/api/agents/gw/heartbeat', json={}, headers=auth()).get_json()['traffic_history_enabled'] is True
    assert client.post(url, json=body, headers=auth()).status_code == 200
    assert client.post(url, json={'schema': 1, 'usage': 'x', 'domains': []}, headers=auth()).status_code == 400

    data = client.get(url + '?range=24h&by=source').get_json()['data']
    assert data['enabled'] is True and data['since'] == since and data['supported'] is True
    assert data['retention_days'] == 90 and data['rows'][0]['key'] == '10.0.0.2'
    assert client.get(url + '?range=1y').status_code == 400
    assert client.put('/api/agents/dns/traffic-history', json={'enabled': True}).status_code == 400

    # 再次开启不改开始时间
    assert client.put(url, json={'enabled': True}).status_code == 200
    assert repo.get_system()['agents'][0]['traffic_history_since'] == since

    # 关闭并删除数据
    assert client.put(url, json={'enabled': False, 'clear': True}).status_code == 200
    assert manager.traffic_history.query('gw', '24h', 'source')['rows'] == []
    assert client.post(url, json=body, headers=auth()).status_code == 409


def test_old_agent_is_reported_unsupported_and_retention_is_validated(env):
    client, repo, _ = env
    repo.update_system_transaction(lambda system: system['agents'][0].update(version='1.8.50-go'))
    assert client.get('/api/agents/gw/traffic-history').get_json()['data']['supported'] is False
    for value in (7, '90', None):
        assert client.put('/api/agents/traffic-history/retention', json={'days': value}).status_code == 400
    assert client.put('/api/agents/traffic-history/retention', json={'days': 180}).status_code == 200
    assert client.get('/api/agents/gw/traffic-history').get_json()['data']['retention_days'] == 180


def test_flush_task_saves_traffic_history_and_drops_deleted_agents(env):
    client, repo, manager = env
    client.put('/api/agents/gw/traffic-history', json={'enabled': True})
    client.post('/api/agents/gw/traffic-history', headers=auth(),
                json=report([usage(hour=int(time.time()) // HOUR * HOUR)]))
    result = metrics_flush.run('manual')
    assert result['traffic_rows'] == 1
    path = Path(manager.traffic_history.path)
    assert path.exists() and manager.traffic_history.pending_count() == 0
    # 导入或恢复配置后不存在的 Agent，下次保存时删除其数据
    repo.update_system_transaction(lambda system: system.update(agents=[]))
    metrics_flush.run('manual')
    assert sqlite3.connect(path).execute('SELECT COUNT(*) FROM usage').fetchone() == (0,)


def test_deleting_an_agent_deletes_its_traffic_history(env):
    client, _, manager = env
    client.put('/api/agents/gw/traffic-history', json={'enabled': True})
    client.post('/api/agents/gw/traffic-history', headers=auth(),
                json=report([usage(hour=int(time.time()) // HOUR * HOUR)]))
    manager.delete_agent('gw')
    assert manager.traffic_history.pending_count() == 0
