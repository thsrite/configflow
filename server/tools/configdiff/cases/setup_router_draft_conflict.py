"""A retained private draft must not replace an independently imported router."""
import copy
import re

from compare import database, differences, files
from harness import json_body
from cases.config_backup import message
from cases.helpers import profile_capture
from cases.setup import ACTION, PATH, PID, SID, call, row, session
from cases.setup_completion import prepared, publication, StatusExchange, IDS, INSTALL, INSTALL_NAME
from cases.setup_lifecycle import ROUTER, capture_draft
from cases.subscriptions import require


def preserved_errors(before, after):
    """Check installation may save only its four documented session fields."""
    result = differences(before['database'], after['database'], 'draft-conflict.SQL')
    key = 'setup-sessions/' + SID + '.json'
    expected = copy.deepcopy(before['files'])
    if key not in expected or key not in after['files']:
        return result + differences(before['files'], after['files'], 'draft-conflict.files')
    allowed = {'installation', 'config_revision', 'route_instructions', 'updated_at'}
    old, new = expected[key]['content'], after['files'][key]['content']
    for field in allowed:
        if field in new:
            old[field] = copy.deepcopy(new[field])
        else:
            old.pop(field, None)
    return result + differences(expected, after['files'], 'draft-conflict.files')


class DraftConflict(StatusExchange):
    def before(self, index):
        super().before(index)
        self.before_values = {'database': database(self.pair.dirs[index]), 'files': files(self.pair.dirs[index])}

    def after(self, index, reply):
        super().after(index, reply)
        current = {'database': database(self.pair.dirs[index]), 'files': files(self.pair.dirs[index])}
        self.proofs[index] = [vars(item) for item in preserved_errors(self.before_values, current)]
        if index == 0:
            require(not self.proofs[0], 'draft attach conflict preserves all SQL and every unrelated session/file field')
            saved = session(self.pair)
            install = saved.get('installation', {})
            require(install.get('id') == INSTALL and bool(re.fullmatch('[0-9a-f]{64}', install.get('fingerprint', '')))
                and install.get('mihomo') == {'id': IDS[0], 'name': INSTALL_NAME + '-mihomo', 'host': '127.0.0.1'}
                and install.get('oxidns') == {'id': IDS[1], 'name': INSTALL_NAME + '-oxidns', 'host': '127.0.0.1'}
                and saved['step'] == saved['max_step'] == 3 and saved['prepared'] is True
                and 'router_draft' in saved and 'router_id' not in saved,
                'failed attachment saves checked installation but leaves private draft pending at installation step')

    def __init__(self, pair, agents):
        super().__init__(pair, agents)
        self.proofs = [None, None]

    def compare(self, policy):
        return super().compare(policy) + differences(*self.proofs, 'draft-conflict.preservation-proof', policy)


def conflict(binary, recorder):
    with prepared(binary, recorder, 'router', 'existing-draft-conflict') as (pair, agents, outbound):
        call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': {
            **ROUTER, 'endpoint': 'http://127.0.0.1:' + str(agents[1].port)}},
            capture=capture_draft(pair), variant='conflict/create-private-draft')
        drafts = [session(pair)['router_draft']]
        if (pair.dirs[1] / ('setup-sessions/' + SID + '.json')).exists():
            drafts.append(session(pair, index=1)['router_draft'])
        else:
            require(bool(pair.unimplemented_mutations), 'missing Go session is solely an unwired setup dependency')
            drafts.append(copy.deepcopy(drafts[0]))  # This request is not sent to an unwired dependent server.
        bodies = [json_body({'routers': {'items': [{**draft, 'name': '其他页面保留的记录'}]}}) for draft in drafts]
        pair.request('R137-S01', 'POST /api/profiles/<profile_id>/import', '/api/profiles/' + PID + '/import',
            bodies=bodies, expect=200, writes=True, capture=profile_capture(pair), variant='conflict/import-same-ID-different-router')
        publication(pair, agents)
        baseline = copy.deepcopy(row(pair)['routers'])
        call(pair, 'H1055', ACTION, PATH + '/check', body={}, expect=409,
            check=message('主路由记录已在其他页面修改，请检查路由器设置'), interaction=DraftConflict(pair, agents),
            variant='private-draft-cannot-overwrite-publicly-imported-router')
        require(row(pair)['routers'] == baseline, 'conflicting existing router remains complete and unchanged')


GROUPS = {'setup-real-existing-router-draft-conflict': conflict}
