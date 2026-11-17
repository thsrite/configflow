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
                'query/header/cookie credentials. Agent capability belongs to the publicly seeded and heartbeating '
                'auth-agent, not an invented invalid token. Authentication-off successes remain a separate fixture audit.',
                tuple(required))
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
