"""Source proofs for guards that normal public Python cannot reach.

A proof is distinct from an executed variant. Named public controls must still
complete successfully; their requests do not acquire the unreachable H entry as
an execution tag. Never generate an exception by replacing a product helper.
"""
from dataclasses import dataclass


@dataclass(frozen=True)
class SourceProof:
    sources: tuple
    rationale: str
    controls: tuple


def declared():
    sources = ('backend/common/client_fixed_ip.py:41', 'backend/common/client_fixed_ip.py:102',
               'backend/common/client_fixed_ip.py:109', 'backend/common/client_fixed_ip.py:205',
               'backend/common/client_fixed_ip.py:265', 'backend/common/client_discovery.py:28',
               'backend/common/client_discovery.py:32', 'backend/common/client_discovery.py:93',
               'backend/routes/routers.py:29', 'backend/routes/routers.py:79',
               'backend/routers/routeros.py:70', 'backend/routers/ikuai.py:185')
    rejected = tuple(('fixed-source-router-' + label, 'R006/source-router-' + label + '-' + operation, 409)
                     for label in ('unknown', 'case', 'empty', 'null', 'false', 'true', 'number', 'array', 'object', 'missing')
                     for operation in ('add', 'delete'))
    rejected += tuple(('fixed-source-missing-router', 'R006/source-missing-' + ident + '-' + operation, 409)
                      for ident in ('missing', 'r-ROS', ' r-ros ') for operation in ('add', 'delete'))
    accepted = tuple(('fixed-source-unseen', 'R006/source-seen-' + vendor, 200) for vendor in ('routeros', 'ikuai'))
    discovery = (('discovery', 'R004/dual-vendor-first', 200), ('discovery-failures', 'R004/ros-enrich', 200))
    dispatch = ('_router rejects absent records and any type outside routeros/ikuai before _open; '
                'the real public tests include every stored legacy JSON type and both operations. '
                'open_driver uses the fixed DRIVERS map whose only concrete classes have '
                'supports_write=True and constant vendor routeros/ikuai. There are no production '
                'assignments replacing this map/property. Real device permission failures raise at '
                'HTTP and cannot change this class capability flag. ')
    return {
        'H0034': SourceProof(sources, dispatch +
            'All three adapter call sites are guarded: _plan gets _router-approved type; apply '
            'gets _router-approved type again; enrich receives one of those same drivers after '
            'discovery filters types to the same two READERS. No request/fake reply can supply '
            'a third adapter vendor. Changing a unit-test driver/map is outside public Step B.',
            rejected + accepted + discovery),
        'H0041': SourceProof(sources, dispatch +
            '_plan supports_write=False is defensive for a future driver, unreachable for every '
            'driver constructible by current production _open. This proof is not a passed execution '
            'of the error branch and does not waive actual write-refusal/permission scenarios.',
            rejected + accepted),
        'H0061': SourceProof(sources, dispatch +
            'Discovery filters stored types to READERS before _read, then open_driver constructs '
            'the same concrete class; enrich cannot receive supports_write=False. Real enrichment '
            'read failures still execute H0062, separately, as demonstrated by its public control.',
            accepted + discovery),
    }


def evaluate(results):
    observed = {(row.get('world'), row['case']): row for row in results}
    verdicts = {}
    for ident, proof in declared().items():
        if not proof.sources or not proof.rationale or not proof.controls:
            raise ValueError('source reachability proof needs sources, rationale and real public controls')
        missing = [world + ':' + case for world, case, status in proof.controls
                   if observed.get((world, case), {}).get('python', {}).get('status') != status]
        verdicts[ident] = {'audit': 'source-proof-controls-pending' if missing else 'source-unreachable',
                          'execution_claim': False, 'sources': list(proof.sources), 'rationale': proof.rationale,
                          'required_controls': [world + ':' + case for world, case, _ in proof.controls],
                          'missing_controls': missing}
    return verdicts
