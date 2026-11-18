"""Untrusted nested iKuai version evidence, exercised over actual public HTTP."""
import copy

from cases import Case
from compare import differences, timestamp
from invariants import fields, profile_write
from probes import probe


def nested(value):
    return {'action': 'fault', 'match': {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': 'sysstat'},
            'status': 200, 'body': {'Result': 10000, 'code': 0, 'results': {'verinfo': value}}}


def cases():
    # False-like malformed containers reach the ordinary handled version guard;
    # truthy containers/scalars instead reach an unchecked .get in real Python.
    for label, value in (('null', None), ('false', False), ('zero', 0), ('empty-text', ''),
                         ('empty-list', []), ('empty-object', {})):
        yield probe('nested-empty-' + label, 'ikuai', (nested(value),),
                    message='只支持 iKuai 4.x，当前版本 未知', entries=('X12',))
    for label, value, kind in (('true', True, 'bool'), ('integer', 1, 'int'), ('number', 1.5, 'float'),
                               ('text', '4.0.314', 'str'), ('list', [{'version': '4.0.314'}], 'list')):
        yield Case('R017/ikuai/nested-verinfo-' + label, 'R017', 'POST', '/api/routers/r-ik/probe', 500,
                   ('R017-F12', 'X12'), prepare=(nested(value),), unchanged=False, effect='device-read',
                   html_error=True, python_bug='IKUAI_PROBE_NESTED_SCHEMA_CRASH',
                   console_patterns=(r'backend/routers/ikuai\.py',
                                     "AttributeError: '" + kind + "' object has no attribute 'get'"))
        # A peer error must not poison the live process/session or prevent a
        # subsequent valid probe from recording its own successful metadata.
        yield probe('nested-recovery-' + label, 'ikuai', ok=True,
                    checks={'probe/version': '4.0.314'}, entries=('X12',))


def only_failed_probe_metadata(before, after, profile, router_id, window):
    """Allow a safe Go fix to persist its own failed probe, never other settings.

    This is a boundary invariant, not a Python save implementation. Physical
    differences are still retained for O12 review, and never auto-passed.
    """
    if not differences(before['database'], after['database']):
        return True
    try:
        profile_write(before, after, profile)
        if before['database'].keys() != after['database'].keys():
            return False
        for table in before['database'].keys() - {'fields', 'sections', 'meta'}:
            if differences(before['database'][table], after['database'][table]):
                return False
        def meta(snapshot):
            table = snapshot['database']['meta']
            if table['columns'] != ['key', 'value'] or any(len(row) != 2 for row in table['rows']):
                raise ValueError('invalid metadata schema')
            values = dict(table['rows'])
            if len(values) != len(table['rows']):
                raise ValueError('duplicate metadata key')
            return values
        prior_meta, next_meta = meta(before), meta(after)
        generation, previous_generation = next_meta.pop('generation'), prior_meta.pop('generation')
        commit, previous_commit = next_meta.pop('commit_id'), prior_meta.pop('commit_id')
        if (type(generation) is not int or type(previous_generation) is not int or generation != previous_generation + 1
                or not isinstance(commit, str) or not commit or commit == previous_commit
                or differences(prior_meta, next_meta)):
            return False
        previous = copy.deepcopy(fields(before, 'profile', profile))
        current = copy.deepcopy(fields(after, 'profile', profile))
        old_items, new_items = previous['routers']['items'], current['routers']['items']
        if (not isinstance(old_items, list) or not isinstance(new_items, list)
                or any(not isinstance(item, dict) for item in old_items + new_items)):
            return False
        old = [item for item in old_items if item.get('id') == router_id]
        new = [item for item in new_items if item.get('id') == router_id]
        if len(old) != 1 or len(new) != 1:
            return False
        probe = new[0].pop('last_probe')
        old[0].pop('last_probe', None)
        previous.pop('_revision')
        current.pop('_revision')
        if (differences(previous, current) or not isinstance(probe, dict) or probe.get('ok') is not False
                or not isinstance(probe.get('message'), str) or not probe['message'].strip()):
            return False
        timestamp(probe['at'], window[0] - 1, window[1] + 1)
        return True
    except (KeyError, IndexError, TypeError, ValueError, AssertionError, OverflowError):
        return False
