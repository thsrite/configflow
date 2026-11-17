"""Observable storage and foreign-object invariants, not business emulation."""
from compare import differences


def fields(snapshot, scope, owner):
    table = snapshot['database']['fields']
    rows = [dict(zip(table['columns'], row)) for row in table['rows']]
    return {row['field']: row['value'] for row in rows if row['scope'] == scope and row['owner'] == owner}


def profile_write(before, after, profile):
    original, current = fields(before, 'profile', profile), fields(after, 'profile', profile)
    if type(current['_revision']) is not int or current['_revision'] != original['_revision'] + 1:
        raise AssertionError('profile write must increment revision exactly once')
    # The outer physical revision of a rewritten field is also compared by the
    # differential; these assertions guard unrelated owners even in Python-only
    # runs while the Go HTTP layer is still unimplemented.
    for name in ('fields', 'sections'):
        columns = before['database'][name]['columns']
        scope_at, owner_at = columns.index('scope'), columns.index('owner')
        def foreign(snapshot):
            return [row for row in snapshot['database'][name]['rows']
                    if row[scope_at] != 'profile' or row[owner_at] != profile]
        changed = differences(foreign(before), foreign(after))
        if changed:
            raise AssertionError('profile write changed foreign storage: ' + repr(changed[:3]))


def configuration_only(before, after):
    # Device reads are permitted for probe, but no table or Agent state may
    # change when saving drafts or editing stored connection settings.
    for key in ('ros', 'ikuai', 'agent'):
        changed = differences(before['fake'][key], after['fake'][key])
        if changed:
            raise AssertionError('draft write changed device state: ' + repr(changed[:3]))
