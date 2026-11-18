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
    disposition: str = 'source-unreachable'


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
    proofs = {
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
    for ident, replacement, code, number in (('R001-F08', 'R001-F14/explicit', 404, 0),
                                            ('R001-F09', 'R001-F13/explicit', 400, 2)):
        controls = (('guards', replacement, code),
                    ('runtime-reads', 'R001-F91/global-' + str(number), 200),
                    ('runtime-reads', 'R001-F91/global-' + str(number + 1), 200),
                    ('upstream-linked', 'R001/linked-header-does-not-scope', 200))
        proofs[ident] = SourceProof(('server/parity/difftest-routers.md:53',
                                    'backend/routes/upstream_router_pending.py:11',
                                    'backend/routes/__init__.py:15', 'backend/routers/pending.py:165'),
            'O6 approved-with-additions explicitly strikes out this original global profile guard '
            'in the R001 matrix row: agents blueprint ignores header/profile_id; only explicit '
            '?profile= is validated by the authenticated handler. This is a REVIEW SUPERSESSION, '
            'not a claimed executed404/400 and not a new design exemption. Controls require the '
            'replacement explicit-profile refusal, both ignored selectors for missing/invalid '
            'profile values and a truly linked alpha/beta router result with an invalid header.',
            controls, 'review-superseded')
    version_controls = tuple(('forwarding-version-' + label, route + '/forwarding-guard-version-' + label,
                               200 if route == 'R025' else 409)
                              for label in ('null', 'false', 'number', 'array', 'object', 'empty', 'text',
                                            'no-patch', 'four-parts', 'prefix', 'suffix', 'extra-suffix',
                                            'leading-space', 'trailing-space', 'newline', 'fullwidth', 'negative',
                                            'old', 'old-major', 'old-minor', 'digit-limit')
                              for route in ('R025', 'R026'))
    version_controls += tuple(('forwarding-version-accepted-' + label, 'R026/forwarding-version-accepted-' + label, 200)
                              for label in ('aio-1.8.40', 'standalone-1.8.41', 'suffixless', 'leading-zeroes',
                                            'new-major', 'new-minor'))
    for ident in ('H0002', 'H0003'):
        proofs[ident] = SourceProof(('backend/agents/linux_gateway.py:54', 'backend/agents/linux_gateway.py:18',
                                    'backend/agents/version.py:548'),
            '_at_least returns before its try unless JSON version is a built-in str matching exactly '
            'three ASCII decimal components plus optional -go. Every production minimum is a valid '
            'literal string. compare_versions splits only these strings, so no TypeError or '
            'AttributeError is possible; its integer conversion catches ValueError internally, '
            'including oversized digit strings, before returning an integer. Thus the outer '
            'ValueError/TypeError/AttributeError catch/False at H0002/H0003 cannot be driven by '
            'normal public inputs. HTTP controls cover all pre-guard types/forms, older/minimum/'
            'newer/leading-zero/suffixless success, and a5000-digit zero-major refusal. That last '
            'control does not claim an inner exception was externally observed. No helper/exception '
            'or Python comparison output is simulated.', version_controls)
    # The abstract base defaults are not a third production device adapter.
    # All application construction funnels through the fixed public map, and
    # both concrete classes override each method without calling super().
    base_controls = {
        'H0128': (('probe-guard-controls', 'R017/routeros/guard-version-minimum', 200),
                  ('probe-guard-controls', 'R017/ikuai/guard-version-minimum', 200)),
        'H0129': (('release-ros', 'R012/fake-ip-preview-routeros', 200),
                  ('release-ik-plain', 'R012/fake-ip-preview-ikuai', 200)),
        'H0130': (('dhcp-evidence-guards', 'R018/dhcp-evidence-healthy', 200),
                  ('dhcp-ikuai-plain', 'R018/iKuai-dhcp-plain-mask-synced', 200)),
        'H0131': (('release-ros', 'R011/fake-ip-publish-routeros', 200),
                  ('release-ik-plain', 'R011/fake-ip-publish-ikuai', 200)),
    }
    for ident, method in (('H0128', 'probe'), ('H0129', 'read_state'),
                           ('H0130', 'read_dhcp_dns'), ('H0131', 'apply')):
        proofs[ident] = SourceProof(('backend/routers/base.py:272', 'backend/routes/routers.py:29',
                                    'backend/routes/routers.py:79', 'backend/common/client_discovery.py:28',
                                    'backend/routers/pending.py:121', 'backend/routers/rule_sync.py:300',
                                    'backend/routers/routeros.py:69', 'backend/routers/ikuai.py:184'),
            'The only production construction path is routes.routers._driver (also imported by '
            'discovery, pending and rule_sync), whose fixed map contains only RouterOSDriver and '
            'IKuaiDriver. Both concrete classes define ' + method + ' and neither delegates this '
            'method to RouterDriver or super(). No production code instantiates RouterDriver, '
            'replaces the map, changes these method attributes or explicitly calls the base method. '
            'Unknown/corrupt stored vendor names cannot instantiate the abstract base: guarded '
            'paths refuse, and unguarded map lookup raises before any base dispatch. Device HTTP '
            'errors still execute the concrete adapter and cannot select its inherited default. '
            'Thus this defensive base error is source-unreachable through current normal public '
            'inputs. Named real HTTP controls exercise the corresponding concrete operation on '
            'both vendors; they do not execute/tag this H branch. Future adapters require reproof.',
            base_controls[ident])
    return proofs


def evaluate(results):
    observed = {(row.get('world'), row['case']): row for row in results}
    verdicts = {}
    for ident, proof in declared().items():
        if (not proof.sources or not proof.rationale or not proof.controls
                or proof.disposition not in ('source-unreachable', 'review-superseded')):
            raise ValueError('source reachability proof needs sources, rationale and real public controls')
        missing = [world + ':' + case for world, case, status in proof.controls
                   if observed.get((world, case), {}).get('python', {}).get('status') != status]
        pending = 'source-proof-controls-pending' if proof.disposition == 'source-unreachable' else 'review-proof-controls-pending'
        verdicts[ident] = {'audit': pending if missing else proof.disposition,
                          'execution_claim': False, 'sources': list(proof.sources), 'rationale': proof.rationale,
                          'required_controls': [world + ':' + case for world, case, _ in proof.controls],
                          'missing_controls': missing}
    return verdicts
