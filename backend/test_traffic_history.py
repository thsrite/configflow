"""Mihomo 流量记录：上报校验、内存累加与按监控历史间隔保存、合并旧数据、查询与开关。"""
import sqlite3
import time
from pathlib import Path

import pytest

from backend.agents import metrics_flush
from backend.agents import traffic_history as module
from backend.agents.traffic_history import CHAIN_SEPARATOR, DAY, HOUR, OTHER_TARGET, UNATTRIBUTED, TrafficHistory, clean_report
from backend.common import config as config_module
from backend.test_qa_integrity import make_app

TOKEN = 'agent-token-0123456789abcdef'
NOW = 1_793_500_000  # 2026-11-01 某时，非整点
THIS_HOUR = NOW // HOUR * HOUR
AI = ['人工智能', '美国自建', 'IEPL专线']
DIRECT = ['全球直连', 'DIRECT']


def joined(layers):
    return CHAIN_SEPARATOR.join(layers)


def usage(hour=THIS_HOUR, source='10.0.0.2', chain=AI, up=1, down=10, conns=1):
    return {'hour': hour, 'source': source, 'chain': list(chain), 'up': up, 'down': down, 'conns': conns}


def target(hour=THIS_HOUR, source='10.0.0.2', domain='www.example.com', chain=AI, up=1, down=10, conns=1):
    return {'hour': hour, 'source': source, 'domain': domain, 'chain': list(chain), 'up': up, 'down': down, 'conns': conns}


def report(usage_items=(), target_items=(), schema=2):
    return {'schema': schema, 'usage': list(usage_items), 'domains': list(target_items), 'dropped': 0}


def test_clean_report_keeps_full_chains_and_rejects_bad_items():
    cleaned = clean_report(report([usage(), usage(source='', chain=['x'])],
                                  [target(domain='a.b.example.com.cn'), target(domain='203.0.113.9', chain=DIRECT), target(domain='')]), now=NOW)
    assert cleaned['usage'][0][:3] == (THIS_HOUR, '10.0.0.2', joined(AI))
    # 未归属的一行没有链路
    assert cleaned['usage'][1][:3] == (THIS_HOUR, UNATTRIBUTED, '')
    assert [row[2:4] for row in cleaned['targets']] == [
        ('example.com.cn', joined(AI)), ('203.0.113.9', joined(DIRECT)), (OTHER_TARGET, joined(AI))]
    for bad in (usage(hour=THIS_HOUR + 1), usage(up=-1), usage(down='1'), usage(source='x' * 65), {**usage(), 'chain': 'Proxy'},
                usage(chain=['']), usage(chain=['a' + CHAIN_SEPARATOR + 'b']), usage(chain=['x'] * 9), None):
        assert clean_report(report([bad]), now=NOW) is None
    assert clean_report({'schema': 3, 'usage': [], 'domains': []}, now=NOW) is None
    # 积压超过 2 天的整点数据直接丢弃，其余照收
    stale = clean_report(report([usage(hour=THIS_HOUR - 3 * DAY), usage()]), now=NOW)
    assert len(stale['usage']) == 1


def test_reports_from_1_8_51_agents_become_two_layer_chains():
    old_usage = [{'hour': THIS_HOUR, 'source': '10.0.0.2', 'group': 'Proxy', 'outbound': 'HK', 'up': 1, 'down': 2, 'conns': 1},
                 {'hour': THIS_HOUR, 'source': '10.0.0.2', 'group': 'DIRECT', 'outbound': 'DIRECT', 'up': 1, 'down': 2, 'conns': 1}]
    old_targets = [{'hour': THIS_HOUR, 'source': '10.0.0.2', 'domain': 'a.example.com', 'outbound': 'HK', 'up': 1, 'down': 2, 'conns': 1}]
    cleaned = clean_report(report(old_usage, old_targets, schema=1), now=NOW)
    assert [row[2] for row in cleaned['usage']] == [joined(['Proxy', 'HK']), 'DIRECT']
    assert cleaned['targets'][0][3] == 'HK'


def test_reports_stay_in_memory_until_flush_and_queries_include_pending(tmp_path):
    history = TrafficHistory(str(tmp_path))
    history.add_report('a1', clean_report(report(
        [usage(down=100), usage(chain=DIRECT, down=40), usage(source='10.0.0.3', chain=DIRECT, down=50), usage(source='', down=9)],
        [target(down=100), target(chain=DIRECT, down=40)]), now=NOW))
    assert not (tmp_path / module.FILENAME).exists()
    by_source = history.query('a1', '24h', 'source', now=NOW)
    assert [(row['key'], row['down']) for row in by_source['rows']] == [('10.0.0.2', 140), ('10.0.0.3', 50), (UNATTRIBUTED, 9)]
    # 设备按完整链路拆开，未归属没有明细；出口节点总量不含未归属
    assert [(part['key'], part['down']) for part in by_source['rows'][0]['parts']] == [(joined(AI), 100), (joined(DIRECT), 40)]
    assert by_source['rows'][0]['part_count'] == 2 and by_source['rows'][2]['parts'] == []
    assert [(row['key'], row['down']) for row in by_source['outbounds']] == [('IEPL专线', 100), ('DIRECT', 90)]
    assert by_source['total'] == {'up': 4, 'down': 199, 'conns': 4}
    # 按链路、出站节点汇总时按设备拆开
    by_chain = history.query('a1', '24h', 'chain', now=NOW)
    direct = next(row for row in by_chain['rows'] if row['key'] == joined(DIRECT))
    assert [(part['key'], part['down']) for part in direct['parts']] == [('10.0.0.3', 50), ('10.0.0.2', 40)]
    by_outbound = history.query('a1', '24h', 'outbound', now=NOW)
    assert [(row['key'], row['down']) for row in by_outbound['rows']] == [('IEPL专线', 100), ('DIRECT', 90), (UNATTRIBUTED, 9)]
    # 策略组是链路的第一层，按链路拆开
    by_group = history.query('a1', '24h', 'group', now=NOW)
    assert [(row['key'], [part['key'] for part in row['parts']]) for row in by_group['rows'][:2]] == [
        ('人工智能', [joined(AI)]), ('全球直连', [joined(DIRECT)])]
    destination = history.query('a1', '24h', 'destination', now=NOW)
    assert [(part['key'], part['down']) for part in destination['rows'][0]['parts']] == [(joined(AI), 100), (joined(DIRECT), 40)]
    assert history.flush(now=NOW) == {'rows': 6, 'compacted': 0, 'pruned': 0}
    assert history.pending_count() == 0
    # 保存后再收到同一小时的数据，累加而不是覆盖
    history.add_report('a1', clean_report(report([usage(down=1)]), now=NOW))
    history.flush(now=NOW)
    rows = sqlite3.connect(tmp_path / module.FILENAME).execute(
        "SELECT down FROM traffic_usage WHERE source = '10.0.0.2' AND chain = ?", (joined(AI),)).fetchall()
    assert rows == [(101,)]
    destination = history.query('a1', '24h', 'destination', now=NOW)
    assert [(row['key'], row['down']) for row in destination['rows']] == [('example.com', 140), (UNATTRIBUTED, 9)]
    assert len(destination['trend']) == 25 and destination['trend'][-1]['down'] == 200


def test_tables_from_1_8_51_are_migrated_to_chains(tmp_path):
    db = sqlite3.connect(tmp_path / module.FILENAME)
    db.execute('CREATE TABLE usage (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, policy_group TEXT, outbound TEXT,'
               ' up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, policy_group, outbound))')
    db.execute('CREATE TABLE targets (agent_id TEXT, bucket INTEGER, span INTEGER, source TEXT, target TEXT, outbound TEXT,'
               ' up INTEGER, down INTEGER, conns INTEGER, PRIMARY KEY (agent_id, bucket, span, source, target, outbound))')
    db.executemany('INSERT INTO usage VALUES (?, ?, 3600, ?, ?, ?, ?, ?, ?)', [
        ('a1', THIS_HOUR, '10.0.0.2', 'Proxy', 'HK', 1, 10, 1), ('a1', THIS_HOUR, '10.0.0.2', 'DIRECT', 'DIRECT', 1, 5, 1),
        ('a1', THIS_HOUR, '', '', '', 0, 3, 0)])
    db.execute("INSERT INTO targets VALUES ('a1', ?, 3600, '10.0.0.2', 'example.com', 'HK', 1, 10, 1)", (THIS_HOUR,))
    db.commit()
    db.close()
    history = TrafficHistory(str(tmp_path))
    by_source = history.query('a1', '24h', 'source', now=NOW)
    assert [(part['key'], part['down']) for part in by_source['rows'][0]['parts']] == [(joined(['Proxy', 'HK']), 10), ('DIRECT', 5)]
    assert by_source['total']['down'] == 18
    assert history.query('a1', '24h', 'destination', now=NOW)['rows'][0]['parts'][0]['key'] == 'HK'
    tables = {row[0] for row in sqlite3.connect(tmp_path / module.FILENAME).execute("SELECT name FROM sqlite_master WHERE type = 'table'")}
    assert tables == {'traffic_usage', 'traffic_targets'}


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
    ai, direct = joined(AI), joined(DIRECT)
    cleaned = {
        'usage': [(old, '10.0.0.2', ai, 1, 10, 1), (old + HOUR, '10.0.0.2', ai, 1, 20, 1), (today - 200 * DAY, '10.0.0.2', ai, 1, 1, 1)],
        # 同一目标走两条链路时按合计排名；被挤出的目标按链路并入「其他」
        'targets': [(old, '10.0.0.2', f'site{i}.example', ai, 0, 1000 - i, 1) for i in range(module.TARGET_DAILY_TOP + 2)]
        + [(old + HOUR, '10.0.0.2', 'site101.example', direct, 0, 1, 1)],
    }
    history.add_report('a1', cleaned)
    result = history.flush(retention=90, now=NOW)
    assert result['pruned'] == 1
    db = sqlite3.connect(tmp_path / module.FILENAME)
    assert db.execute('SELECT bucket, span, down FROM traffic_usage').fetchall() == [(today - 10 * DAY, DAY, 30)]
    targets = {(target, chain): down for target, chain, down in db.execute('SELECT target, chain, down FROM traffic_targets')}
    assert len(targets) == module.TARGET_DAILY_TOP + 2
    assert targets[(OTHER_TARGET, ai)] == (1000 - module.TARGET_DAILY_TOP) + (1000 - module.TARGET_DAILY_TOP - 1)
    assert targets[(OTHER_TARGET, direct)] == 1
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
    # 1.8.51-go 只有两层链路，提示更新到能上报完整链路的版本
    assert data['full_chain'] is False and data['full_chain_version'] == '1.8.52-go'
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
    assert sqlite3.connect(path).execute('SELECT COUNT(*) FROM traffic_usage').fetchone() == (0,)


def test_deleting_an_agent_deletes_its_traffic_history(env):
    client, _, manager = env
    client.put('/api/agents/gw/traffic-history', json={'enabled': True})
    client.post('/api/agents/gw/traffic-history', headers=auth(),
                json=report([usage(hour=int(time.time()) // HOUR * HOUR)]))
    manager.delete_agent('gw')
    assert manager.traffic_history.pending_count() == 0


def test_agents_can_report_when_panel_login_is_enabled(tmp_path, monkeypatch):
    """开启登录时，Agent 用自己的令牌上报不能被全局登录检查拦下；网页端的查询仍然要登录。"""
    from flask import Flask
    from backend.common import auth as auth_module
    from backend.routes import register_blueprints
    from backend.routes.auth import setup_before_request
    from backend.common.config_repository import ProfileRepository
    from backend.common import agent_manager as singleton
    monkeypatch.setattr(auth_module, 'ADMIN_USERNAME', 'admin')
    monkeypatch.setattr(auth_module, 'ADMIN_PASSWORD', 'admin-password')
    repository = ProfileRepository(tmp_path / 'config')
    config_module.set_repository(repository)
    repository.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'gw', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': TOKEN, 'host': 'h', 'port': 8080,
         'version': '1.8.52-go', 'traffic_history_enabled': True}]))
    monkeypatch.setattr(singleton, '_agent_manager', None)
    app = Flask(__name__)
    register_blueprints(app)
    setup_before_request(app)
    client = app.test_client()
    url = '/api/agents/gw/traffic-history'
    body = report([usage(hour=int(time.time()) // HOUR * HOUR)])
    try:
        assert client.post(url, json=body, headers=auth()).status_code == 200
        assert client.post(url, json=body, headers=auth('wrong-token')).status_code == 401
        # 查询、开关和清空是网页端接口：Agent 令牌不行，要网页登录
        assert client.get(url, headers=auth()).status_code == 401
        admin = auth(auth_module.generate_token('admin'))
        data = client.get(url + '?range=24h&by=source', headers=admin).get_json()['data']
        assert data['total']['down'] == 10
    finally:
        config_module.reset_config_context()


def test_every_agent_token_route_bypasses_the_panel_login():
    """用 Agent 令牌鉴权的接口都要在全局登录检查的放行名单里，否则开启登录后 Agent 的请求全被 401。"""
    import re
    root = Path(__file__).resolve().parent
    agents_source = (root / 'routes' / 'agents.py').read_text(encoding='utf-8')
    auth_source = (root / 'routes' / 'auth.py').read_text(encoding='utf-8')
    handlers = re.findall(r"@bp\.route\([^\n]*\)\n(?:@[^\n]*\n)*def (\w+)\([^)]*\):\n(?:(?!\ndef ).)*?_authenticated_agent\(",
                          agents_source, re.S)
    assert 'agent_traffic_history_report' in handlers and 'agent_heartbeat' in handlers
    missing = [name for name in handlers if f"'agents.{name}'" not in auth_source]
    assert missing == []
