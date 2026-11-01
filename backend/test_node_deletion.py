"""Reviewed node deletion cleans shared/profile references in one commit."""
import copy
from unittest.mock import Mock

import pytest

from backend.test_dialer_proxy import node, seed_nodes, chain_group
from backend.test_qa_integrity import make_app


def fixture(tmp_path):
    app, repo = make_app(tmp_path)
    seed_nodes(repo, [node('delete', 'Remove', params={'password': 'never-in-preview'}), node('keep', 'Keep')],
               aggregations=[{'id': 'agg', 'name': 'Shared', 'nodes': ['delete', 'keep'], 'subscriptions': []}],
               groups=[{'id': 'g', 'name': 'Group', 'type': 'select', 'manual_nodes': ['delete', 'keep', 'DIRECT'],
                        'aggregations': ['agg'], 'proxies_order': [{'type': 'node', 'id': 'delete'},
                        {'type': 'node', 'id': 'keep'}, {'type': 'node', 'id': 'DIRECT'}]}])
    repo.create_profile({'id': 'other', 'name': 'Other'})
    repo.update_profile_transaction('other', lambda p: p.update(proxy_groups=[
        {'id': 'legacy', 'name': 'Legacy', 'type': 'select', 'source': 'node', 'proxies': ['delete', 'keep']},
        {'id': 'indirect', 'name': 'Indirect', 'type': 'select', 'aggregations': ['agg']},
    ]))
    repo.create_profile({'id': 'untouched', 'name': 'Untouched'})
    return app.test_client(), repo


def preview(client, ids=None):
    response = client.post('/api/nodes/delete-preview', json={'ids': ids or ['delete']})
    assert response.status_code == 200, response.get_json()
    return response.get_json()


def delete(client, plan):
    return client.post('/api/nodes/delete', json={
        'ids': [node['id'] for node in plan['nodes']], 'confirmation': plan['confirmation'],
    })


def test_preview_is_read_only_and_confirmed_delete_cleans_all_spaces_once(tmp_path, monkeypatch):
    client, repo = fixture(tmp_path)
    before = repo.export_all()
    fingerprint = repo._store.fingerprint()
    plan = preview(client)
    assert repo._store.fingerprint() == fingerprint
    assert 'never-in-preview' not in str(plan)
    assert {(u['kind'], u['id']) for u in plan['usages']} == {
        ('aggregation', 'agg'), ('profile', 'default'), ('profile', 'other'),
    }
    assert {(g['profile_id'], g['id']) for g in plan['groups']} == {('default', 'g'), ('other', 'legacy')}
    assert plan['blockers'] == []
    # Existing API clients cannot accidentally opt into cascading removal.
    assert client.delete('/api/nodes/delete').status_code == 409
    commit = Mock(wraps=repo._store.commit)
    monkeypatch.setattr(repo._store, 'commit', commit)
    response = delete(client, plan)
    assert response.status_code == 200, response.get_json()
    assert response.get_json()['deleted_ids'] == ['delete']
    commit.assert_called_once()
    after = repo.export_all()
    assert [n['id'] for n in after['shared']['nodes']] == ['keep']
    assert after['shared']['subscription_aggregations'][0]['nodes'] == ['keep']
    group = after['profiles']['default']['proxy_groups'][0]
    assert group['manual_nodes'] == ['keep', 'DIRECT']
    assert group['proxies_order'] == [{'type': 'node', 'id': 'keep'}, {'type': 'node', 'id': 'DIRECT'}]
    assert group['aggregations'] == ['agg']
    assert after['profiles']['other']['proxy_groups'][0]['proxies'] == ['keep']
    assert after['profiles']['other']['proxy_groups'][1] == before['profiles']['other']['proxy_groups'][1]
    assert after['profiles']['untouched'] == before['profiles']['untouched']
    assert after['shared']['_revision'] == before['shared']['_revision'] + 1
    for identity in ('default', 'other'):
        assert after['profiles'][identity]['_revision'] == before['profiles'][identity]['_revision'] + 1


@pytest.mark.parametrize('change', ['new_reference', 'node_changed', 'aggregation_changed'])
def test_confirmation_cannot_remove_changed_nodes_or_unreviewed_references(tmp_path, change):
    client, repo = fixture(tmp_path)
    plan = preview(client)
    if change == 'new_reference':
        repo.update_profile_transaction('untouched', lambda p: p['proxy_groups'].append(
            {'id': 'new', 'name': 'New', 'type': 'select', 'manual_nodes': ['delete']}))
    elif change == 'node_changed':
        repo.update_shared_transaction(lambda shared: shared['nodes'][0].update(name='Changed'))
    else:
        repo.update_shared_transaction(lambda shared: shared['subscription_aggregations'][0].update(nodes=['delete']))
    before = repo._store.fingerprint()
    response = delete(client, plan)
    assert response.status_code == 409
    assert response.get_json()['code'] == 'node_deletion_changed'
    assert repo._store.fingerprint() == before


def test_unrelated_edit_is_preserved_without_invalidating_confirmation(tmp_path):
    client, repo = fixture(tmp_path)
    plan = preview(client)
    repo.update_profile('other', {'description': 'Concurrent unrelated change'})
    assert delete(client, plan).status_code == 200
    assert repo.get_profile('other')['description'] == 'Concurrent unrelated change'


@pytest.mark.parametrize('dependency', ['chain', 'dialer', 'rule', 'chain_group'])
def test_protected_dependency_blocks_entire_batch_without_detaching(tmp_path, dependency):
    client, repo = fixture(tmp_path)
    if dependency == 'chain':
        repo.update_profile_transaction('other', lambda p: p['proxy_groups'].append(
            chain_group('keep', {'type': 'node', 'id': 'delete'})))
    elif dependency == 'chain_group':
        repo.update_profile_transaction('other', lambda p: p['proxy_groups'].extend([
            {'id': 'entry', 'name': 'Entry', 'type': 'select', 'manual_nodes': ['delete']},
            chain_group('keep', {'type': 'group', 'id': 'entry'}),
        ]))
    elif dependency == 'dialer':
        repo.update_shared_transaction(lambda s: s['nodes'].append(node('dependent', params={'dialer-proxy': 'Remove'})))
    else:
        repo.update_profile_transaction('other', lambda p: p['rule_configs'].append(
            {'id': 'rule', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'Remove'}))
    # An unrelated node in the same batch must also survive a blocked deletion.
    repo.update_shared_transaction(lambda s: s['nodes'].append(node('unused')))
    plan = preview(client, ['delete', 'unused'])
    assert plan['blockers']
    before = repo._store.fingerprint()
    assert delete(client, plan).status_code == 409
    assert repo._store.fingerprint() == before


def test_storage_failure_rolls_back_references_and_nodes(tmp_path, monkeypatch):
    client, repo = fixture(tmp_path)
    plan = preview(client)
    before = copy.deepcopy(repo.export_all())
    monkeypatch.setattr(repo._store, 'commit', Mock(side_effect=OSError('disk full')))
    assert delete(client, plan).status_code == 500
    assert repo.export_all() == before


def test_batch_deletes_all_selected_nodes_and_warns_about_empty_sources(tmp_path):
    client, repo = fixture(tmp_path)
    plan = preview(client, ['delete', 'keep'])
    assert '聚合「Shared」' in plan['empty_sources']
    assert any('Legacy' in source for source in plan['empty_sources'])
    assert delete(client, plan).status_code == 200
    assert repo.get_shared()['nodes'] == []
    assert repo.get_shared()['subscription_aggregations'][0]['nodes'] == []
    assert repo.get_profile('default')['proxy_groups'][0]['manual_nodes'] == ['DIRECT']


@pytest.mark.parametrize('body', [None, [], {}, {'ids': []}, {'ids': 'delete'}, {'ids': [None]}])
def test_invalid_preview_request_does_not_write(tmp_path, body):
    client, repo = fixture(tmp_path)
    before = repo._store.fingerprint()
    assert client.post('/api/nodes/delete-preview', json=body).status_code == 400
    assert repo._store.fingerprint() == before


@pytest.mark.parametrize('confirmation', [None, '', True, '确认', 'a' * 64])
def test_missing_or_invalid_confirmation_never_writes(tmp_path, confirmation):
    client, repo = fixture(tmp_path)
    before = repo._store.fingerprint()
    response = client.post('/api/nodes/delete', json={'ids': ['delete'], 'confirmation': confirmation})
    assert response.status_code in (400, 409)
    assert repo._store.fingerprint() == before


def test_missing_node_and_duplicate_ids(tmp_path):
    client, repo = fixture(tmp_path)
    response = client.post('/api/nodes/delete-preview', json={'ids': ['gone']})
    assert response.status_code == 404
    assert '已不存在' in response.get_json()['message']
    plan = preview(client, ['delete', 'delete'])
    assert len(plan['nodes']) == 1
    assert delete(client, plan).status_code == 200
