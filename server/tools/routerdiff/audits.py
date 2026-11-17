"""Explicit variant audits, separate from inventory parsing and recipe execution.

An entry has no completed audit unless it appears here with source evidence and
every named required variant completed its real Python HTTP assertions. A report
with only another world's similarly named recipe never satisfies a requirement.
This is test coverage, not a claim that an unregistered Go route was compared.
"""
from dataclasses import dataclass

from inventory import load


@dataclass(frozen=True)
class Audit:
    sources: tuple
    rationale: str
    requirements: tuple


def declared():
    routes, _ = load()
    audits = {}
    variants = ('missing header', 'wrong signature', 'expired JWT', 'empty claims',
                'double space Bearer', '8193-byte credential', 'valid Agent capability')
    labels = ('config-token', 'download-token', 'agent-token', 'admin-jwt-query', 'device-password', 'config-cookie')
    for route in routes:
        for number, description in enumerate(variants, 1):
            ident = route + '-F' + format(number, '02')
            required = [('guards', ident + '/A' + str(number), 401)]
            if number == 1:
                required += [('guards', route + '/no-admin-header-' + label, 401) for label in labels]
            audits[ident] = Audit(
                ('backend/routes/auth.py:35', 'backend/common/auth.py:23', 'server/parity/difftest-routers.md:20'),
                'A' + str(number) + ': ' + description + '; valid alpha/default profile, authentication enabled. '
                'Every refusal checks full DB/files/device state unchanged. A1 also includes all six alternative '
                'query/header/cookie credentials. Agent capability belongs to the publicly seeded auth-agent, '
                'not an invented invalid token; its heartbeat acceptance is separately tested over real public HTTP. '
                'The auth world itself makes no preparatory mutation. Authentication-off successes remain a separate fixture audit.',
                tuple(required))
    profile_sources = ('backend/common/profile_context.py:53', 'backend/common/profile_context.py:93',
                       'backend/routes/auth.py:35', 'server/parity/difftest-routers.md:222')
    for route in routes:
        if route == 'R001':
            continue
        for suffix, status in (('F08', 404), ('F09', 400)):
            ident = route + '-' + suffix
            audits[ident] = Audit(profile_sources,
                'Authenticated invalid/missing profile, each of query profile, query profile_id and header separately. '
                'No handler/device/DB write. Combined selector precedence is a different X02 audit, still pending.',
                tuple(('guards', ident + '/' + selector, status) for selector in ('profile', 'profile_id', 'header')))
        ident = route + '-F90'
        audits[ident] = Audit(profile_sources,
            'Profile middleware rejects before authentication: absent bearer with each invalid/missing selector. '
            'All six concrete combinations and unchanged full state are required.',
            tuple(('guards', ident + '/' + suffix + '/' + selector, status)
                  for suffix, status in (('F08', 404), ('F09', 400)) for selector in ('profile', 'profile_id', 'header')))
    global_sources = (*profile_sources, 'backend/routes/upstream_router_pending.py:20', 'backend/routers/pending.py:165')
    for ident, case, status in (
        ('R001-F13', 'R001-F13/explicit', 400), ('R001-F14', 'R001-F14/explicit', 404),
        ('R001-F90', 'R001-F90/no-bearer-missing-profile', 401),
        ('R001-B90', 'R001-B90/empty-explicit-profile', 400),
    ):
        audits[ident] = Audit(global_sources,
            'Global agents blueprint ignores request profile selectors before auth; only explicit query profile '
            'is validated inside the authenticated upstream handler. Original R001-F08/F09 stay unclosed, '
            'pending their explicit O6 supersession audit; no invalid global-guard assumption is reused.',
            (('guards', case, status),))
    audits['R001-F91'] = Audit(global_sources,
        'Header and profile_id cannot select scope on a global agents route. Two empty-linked variants plus '
        'a genuinely linked alpha/beta case with invalid header prove it; all semantic response headers remain compared.',
        (('runtime-reads', 'R001-F91/global-0', 200), ('runtime-reads', 'R001-F91/global-1', 200),
         ('upstream-linked', 'R001/linked-header-does-not-scope', 200)))
    # Explicit reviewed input positions, not "whatever cases currently claim H".
    # The integrity test fails if a later input insertion renumbers these cases.
    def settings_inputs(groups, field='dns_guard', reset=False, ikuai=True):
        required = []
        for route in ('R008', 'R010'):
            for start, count in groups:
                for offset in range(count):
                    label = str(start + offset) + '-' + field + '-' + str(0 if reset else offset)
                    required.append(('guards', route + '/' + label, 400))
                    if ikuai:
                        required.append(('guards', route + '/iKuai-shared-' + label, 400))
        return tuple(required)
    def vendor_inputs(label):
        return tuple(('guards', case, 400) for case in (
            'R008/' + label, 'R010/' + label, 'R010/' + label + '-existing-iKuai'))
    settings = {
        'H0154': settings_inputs(((67, 5),)),
        'H0155': settings_inputs(((72, 6), (78, 6), (84, 6))),
        'H0156': settings_inputs(((90, 10),)),
        'H0157': settings_inputs(((100, 7),), reset=True),
        'H0158': settings_inputs(((100, 7),), reset=True),
        'H0159': settings_inputs(((107, 1),)),
        'H0160': vendor_inputs('iKuai-cannot-enable-DNS-guard'),
        'H0161': settings_inputs(((108, 1),), ikuai=False),
        'H0491': settings_inputs(((109, 8),), field='ipv6_pd_guard'),
        'H0492': vendor_inputs('iKuai-cannot-enable-PD-guard'),
    }
    settings['H1243'] = settings['H1244'] = tuple(sorted(set(
        required for ident in ('H0154', 'H0155', 'H0156', 'H0157', 'H0158', 'H0159', 'H0160', 'H0161')
        for required in settings[ident])))
    settings['H1246'] = settings['H1247'] = tuple(sorted(set(settings['H0491'] + settings['H0492'])))
    for ident, required in settings.items():
        audits[ident] = Audit(
            ('backend/routers/dns_guard.py:48', 'backend/routers/ipv6_pd_guard.py:116', 'backend/routers/store.py:103'),
            'Only public create/update enter these settings validators. The original H route-scope column omitted '
            'R008/R010; it is not proof that report/plan reaches the guard. Shape, all three boolean fields, '
            'non-string bypass members, empty/whitespace/CJK/fullwidth/IPv6/bad-prefix strings, pre-dedup65 cap '
            'and both vendors are explicit. Missing-DNS rejection is RouterOS-only: iKuai stops at the preceding '
            'unsupported-vendor guard. Vendor tests cover creation, type-changing edit and existing iKuai edit. '
            'The parent catch/rethrow entries require every callee error family, not merely one400.',
            required)
    return audits


def evaluate(results):
    declarations = declared()
    completed = {(row.get('world'), row['case']): row for row in results}
    verdicts = {}
    for ident, audit in declarations.items():
        missing = []
        for world, case, status in audit.requirements:
            row = completed.get((world, case))
            if (row is None or row.get('python', {}).get('status') != status
                    or ident not in row.get('entries', ())):
                missing.append(world + ':' + case)
        verdicts[ident] = {'audit': 'variants-pending' if missing else 'variants-audited',
                          'sources': list(audit.sources), 'rationale': audit.rationale,
                          'required_variants': [world + ':' + case for world, case, _ in audit.requirements],
                          'missing_variants': missing}
    return verdicts
