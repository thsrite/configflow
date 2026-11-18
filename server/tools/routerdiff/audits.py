"""Explicit variant audits, separate from inventory parsing and recipe execution.

An entry has no completed audit unless it appears here with source evidence and
every named required variant completed its real Python HTTP assertions. A report
with only another world's similarly named recipe never satisfies a requirement.
This is test coverage, not a claim that an unregistered Go route was compared.
"""
from dataclasses import dataclass

from inventory import load
import reachability


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
            'is validated inside the authenticated upstream handler. Original R001-F08/F09 have a '
            'separate explicit O6 supersession proof; no invalid global-guard assumption is reused.',
            (('guards', case, status),))
    audits['R001-F91'] = Audit(global_sources,
        'Header and profile_id cannot select scope on a global agents route. Four empty-linked variants plus '
        'a genuinely linked alpha/beta case with invalid header prove it; all semantic response headers remain compared.',
        (('runtime-reads', 'R001-F91/global-0', 200), ('runtime-reads', 'R001-F91/global-1', 200),
         ('runtime-reads', 'R001-F91/global-2', 200), ('runtime-reads', 'R001-F91/global-3', 200),
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
    # Client guard evidence uses exact source-reviewed values rather than
    # collecting every recipe that happens to claim a helper identifier.
    invalid_ranges = {
        'H0071': (None, 0, False, [], {}),
        'H0072': ('', 'bad', '10.44.0.1/1/2', '10.44.0.1%lo', '地址', '１０.４４.０.１'),
        'H0073': ('10.44.0.1/', '10.44.0.1/-1', '10.44.0.1/+1', '10.44.0.1/3a',
                  '10.44.0.1/0001', '10.44.0.1/ 24', '10.44.0.1/２４'),
        'H0074': ('10.44.0.1/33', 'fd42:cf::1/129'),
        'H0075': ('::ffff:10.44.0.1/95',),
        'H0076': ('224.0.0.1', '224.0.0.1/24', 'ff02::1', 'ff02::1/64'),
        'H0077': ('0.0.0.0', '0.0.0.0/32', '::', '::/128'),
    }
    client_requirements = {
        ident: tuple(('guards', 'R003/ip-' + repr(value), 400) for value in values)
        for ident, values in invalid_ranges.items()}
    translated = tuple(requirement for required in client_requirements.values() for requirement in required)
    translated += tuple(('guards', 'R003/ip-' + repr(value), 400)
                        for value in ('10.44.0.999', '1:2:3:4:5:6:7:8:9'))
    client_requirements['H0078'] = client_requirements['H0079'] = translated
    names = [None, 0, False, [], {}, 'x' * 81, '行🧪' * 40 + '尾', ' ' * 81]
    names += ['control' + chr(number) for number in range(32)] + ['del\x7f']
    client_requirements['H0068'] = tuple(('guards', 'R003/' + field + '-' + repr(value), 400)
        for field in ('name', 'previous_name') for value in names)
    client_requirements['H0023'] = tuple(('guards', 'R003/' + field + '-' + repr(value), 400)
        for field in ('mode', 'previous_mode') for value in (None, 0, False, [], {}, 'SMART', 'proxy'))
    client_requirements['H0024'] = (('guards', 'R003/previous_name-stale', 409),
                                   ('guards', 'R003/previous_mode-stale', 409))
    for ident, required in client_requirements.items():
        audits[ident] = Audit(
            ('backend/common/client_devices.py:28', 'backend/common/device_notes.py:18',
             'backend/common/oxidns_device_policy.py:22', 'backend/routes/client_devices.py:55'),
            'R003 public draft update reaches these helpers before transaction writes. Each compound type/length/'
            'character condition, both name fields, both mode fields, IPv4/IPv6 prefix limits, mapped-prefix lower '
            'bound, host/network multicast, unspecified hosts and actual ipaddress parse errors are required. '
            'The outer ValueError translation requires every family. Stale name and mode are separate409 variants. '
            'Name length is checked before trim; all32 C0 controls and DEL are individually rejected. '
            'Canonical accepted host/network/mapped forms and Unicode80 limits are separate positive controls; '
            'these guard audits do not claim policy graph rewriting, device I/O or all client route coverage.',
            required)
    policy_variants = {
        'H0080': tuple('shape-' + label for label in ('null', 'false', 'number', 'string', 'object', '513')),
        'H0081': tuple('row-' + label for label in ('null', 'false', 'number', 'string', 'array', 'extra-key')),
        'H0082': tuple('duplicate-' + label for label in ('host-prefix', 'mapped-host', 'ipv6-compression', 'network-mask')),
        'H0083': tuple('name-' + label for label in ('null', 'false', 'number', 'array', 'object',
                                                   'ascii81', 'unicode81', 'space81'))
                  + tuple('name-control' + str(number) for number in range(32)),
        'H0084': tuple('mode-' + label for label in ('null', 'false', 'number', 'array', 'object',
                                                   'empty', 'upper', 'proxy', 'missing')),
    }
    for ident, labels in policy_variants.items():
        required = tuple(('stored-policy-' + label, route + '/stored-policy-' + label, 400)
                         for label in labels for route in ('R002', 'R003', 'R014'))
        if ident == 'H0080':
            required += (('stored-policy-capacity', 'R003/policy-513-growth-refused-atomically', 400),)
        audits[ident] = Audit(
            ('backend/common/oxidns_device_policy.py:54', 'backend/common/device_notes.py:36',
             'backend/common/client_devices.py:18', 'backend/routers/policy_workspace.py:18',
             'backend/common/config_repository.py:2577'),
            'Real public import permits these invalid legacy policy rows (unlike invalid device_notes). '
            'R002 read, R003 draft write and R014 workspace each reach the guard and return its exact400; '
            'full state remains unchanged, no device I/O. All type/length/member/duplicate/name/mode '
            'families require explicit variants, including every C0 control, canonical mapped/network '
            'duplicates and missing mode. DEL is accepted and removed only for legacy policy names, '
            'with a separate successful read/rename fixture; it is not confused with normalize_name. '
            '512 rows and80 Unicode characters are separate positive controls; H0080 additionally '
            'requires a real write growing512 valid rows to513, with no partial note/policy commit. These helper audits do '
            'not claim every policy graph mutation or all public router call sites.',
            required)
    client_successes = {
        'S01': (('client-drafts', 'R003-S01/0'), ('notes-capacity', 'R003/notes-2048-freed-slot-accepted')),
        'S02': (('client-drafts', 'R003-S02/1'), ('client-drafts', 'R003-S02/3'),
                ('client-mode-notes', 'R003/mode-note-smart-with-name')),
        'S03': (('client-drafts', 'R003-S03/2'), ('client-mode-notes', 'R003/mode-note-create-direct')),
        'S04': (('client-drafts', 'R003-S04/4'),),
        'S05': (('client-drafts', 'R003-S05/6'), ('client-mode-notes', 'R003/mode-note-clear-mode-retains-same-name')),
        'S06': (('client-drafts', 'R003-S06/5'), ('client-mode-notes', 'R003/mode-note-empty-note-hides-legacy-name')),
        'S07': (('client-drafts', 'R003-S07/8'), ('client-canonical-names', 'R003/remove-note-with-normalized-previous'),
                ('client-mode-notes', 'R003/mode-note-clear-policy-removes-hidden-name'),
                ('notes-keep-reservation', 'R003/note-remove-forgets-binding-keeps-reservation')),
        'S08': (('client-drafts', 'R003-S08/7'), ('client-mode-notes', 'R003/mode-note-rename-preserves-native-and-legacy-name'),
                ('stored-policy-legacy', 'R003/stored-policy-legacy-rename-keeps-raw-rows'),
                ('notes-capacity', 'R003/notes-2048-replace-existing'),
                ('notes-keep-reservation', 'R003/note-rename-preserves-real-binding')),
    }
    for suffix, required in client_successes.items():
        audits['R003-' + suffix] = Audit(
            ('backend/routes/client_devices.py:55', 'backend/common/client_devices.py:39',
             'backend/common/device_notes.py:52'),
            'Actual public row transitions with own previous values: new smart/direct and transitions, '
            'name-only vs policy changes, legacy-name hiding/cleanup and Unicode/mapped address removal. '
            'S07 additionally requires a genuinely previewed/applied DHCP reservation: local binding is '
            'forgotten while the complete device state survives with zero device requests. S08 requires '
            'full native/settings/binding retention for normal direct policies, canonical legacy rows, '
            'the2048-note boundary and an actual DHCP binding. R015 stale names_revision vs modern '
            'editor success is tested separately, not inferred from the devices response.',
            tuple((world, name, 200) for world, name in required))
    store_requirements = {
        'H1234': tuple(('guards', route + '-' + suffix + '/' + prefix + variant, 400)
            for route, suffix, prefix in (('R008', 'F10', ''), ('R010', 'F13', ''), ('R010', 'F13', 'iKuai-'))
            for variant in ('none', 'null', 'array', 'text', 'bool', 'number', 'malformed', 'wrong-content-type')),
        'H1235': settings_inputs(((21, 6),), field='manage_return_routing'),
        'H1236': settings_inputs(((27, 6),), field='auto_rule_sync') + tuple(('guards', case, 400) for case in (
            'R008/66-ikuai-auto', 'R010/66-ikuai-auto', 'R010/iKuai-shared-66-ikuai-auto')),
        'H1237': settings_inputs(((33, 13),), field='rule_sync_interval'),
        'H1238': settings_inputs(((46, 9),), field='ip_rule_sets'),
        'H1239': settings_inputs(((55, 9),), field='ros_policy_rule_sets'),
        'H1240': tuple(('guards', case, 400) for case in (
            'R008/65-ikuai-policy', 'R010/65-ikuai-policy', 'R010/iKuai-shared-65-ikuai-policy'))
            + vendor_inputs('iKuai-overlap-stops-at-vendor-check'),
        'H1241': (('guards', 'R008/64-overlap', 400), ('guards', 'R010/64-overlap', 400)),
        'H1251': settings_inputs(((0, 5),), field='name'),
        'H1252': settings_inputs(((5, 6),), field='type', ikuai=False)
            + tuple(('guards', 'R010/iKuai-shared-' + str(5 + index) + '-type-' + str(index), 400)
                    for index in range(6)),
        'H1253': settings_inputs(((11, 5),), field='endpoint'),
        'H1254': settings_inputs(((16, 5),), field='username'),
        'H1255': tuple(('guards', 'R008/password-' + str(index), 400) for index in range(6)),
    }
    for ident in ('H1235', 'H1236'):
        store_requirements[ident] += tuple(('router-guard-normalization', 'R010/native-boolean-flags-' + rid, 200)
                                           for rid in ('r-ros', 'r-ik'))
    store_requirements['H1237'] += tuple(('router-guard-normalization', 'R010/interval-enum-' + rid + '-' + str(value), 200)
        for rid in ('r-ros', 'r-ik') for value in (15, 30, 60, 180, 360, 720, 1440))
    store_requirements['H1255'] += tuple(('router-guard-normalization', 'R010/ignored-password-' + rid + '-' + str(number), 200)
        for rid in ('r-ros', 'r-ik') for number in range(6))
    for vendor in ('routeros', 'ikuai'):
        for form in ('empty', 'missing'):
            rid = 'legacy-' + vendor + '-' + form
            store_requirements['H1255'] += tuple(('legacy-credentials', 'R010/legacy-no-password-' + rid + '-' + label, 400)
                                                for label in ('rename', 'null', 'empty'))
            store_requirements['H1255'] += (('legacy-credentials', 'R010/legacy-credential-repaired-' + rid, 200),)
    for field, update, delete in (
        ('ros_policy_state', 'H1242', 'H1289'), ('ipv6_fakeip_state', 'H1249', 'H1290'),
        ('ipv6_routes_state', 'H1250', 'H1291'), ('dns_guard_state', 'H1245', 'H1292'),
        ('ipv6_pd_guard_state', 'H1248', 'H1293')):
        store_requirements[update] = tuple(('stored', 'R010/' + field + '/' + label, 400) for label in ('type', 'endpoint'))
        store_requirements[update] += (('stored', 'R010-B01/preserve-' + field, 200),)
        store_requirements[update] += tuple(('empty-manifests', 'R010/empty-' + field + '-' + label, 200)
                                             for label in ('endpoint', 'type'))
        store_requirements[delete] = (('stored', 'R009/' + field, 409),
                                      ('empty-manifests', 'R009/empty-' + field + '-delete', 200))
    for ident, required in store_requirements.items():
        audits[ident] = Audit(
            ('backend/routers/store.py:54', 'backend/routes/routers.py:69', 'backend/routes/routers.py:157'),
            'Public create/update and independent stored credential/manifest fixtures. Non-object inputs '
            'include silent JSON failures on both stored vendors. Each boolean/type/member loop is explicit; '
            'intervals include all seven legal integers and thirteen illegal forms. iKuai policy refusal '
            'precedes overlap, so only RouterOS can reach H1241. Truthy manifests reject both connection '
            'field changes but ignore injected manifest/id changes; genuinely empty manifests permit both '
            'changes and deletion. Missing/empty legacy credentials load, reject incomplete edits, and can '
            'be repaired without losing unknown stored fields. These are normalizer/ownership guards, '
            'not proof that a selected resource exists or any publication succeeds.', required)
    cache_requirements = [('discovery', name, 200) for name in (
        'R004/dual-vendor-first', 'R004/dual-vendor-cached', 'R004/refresh-literal-true-is-cache',
        'R003/discovery-rename', 'R004/cached-renamed')]
    cache_requirements += [('discovery-failures', 'R004/' + label, 200) for label in (
        'outage-prior-good', 'failed-refresh-keeps-good', 'required-arp', 'required-terminal', 'both-required')]
    cache_requirements += [('discovery-signature', name, 200) for name in (
        'R004/cache-signature-initial', 'R010/cache-signature-edit-password', 'R004/cache-signature-invalidated-password',
        'R010/cache-signature-edit-restore', 'R004/cache-signature-invalidated-restore',
        'R010/cache-signature-edit-tls', 'R004/cache-signature-invalidated-tls',
        'R010/cache-signature-noop-revision-only', 'R004/cache-signature-noop-keeps-good')]
    lru = ['alpha-first', 'beta-first'] + ['fill-' + str(number) for number in range(126)]
    lru += ['touch-alpha-at128', 'insert129-evicts-beta', 'alpha-recent-survives', 'beta-least-recent-rereads',
            'cache001-survives', 'cache000-evicted-by-beta-rereads']
    cache_requirements += [('discovery-lru', 'R004/cache-lru-' + label, 200) for label in lru]
    cache_requirements += [('discovery-coalescing', 'R004/cache-coalesce-' + label, 200) for label in (
        'owner', 'waiter-same-snapshot', 'subsequent-explicit-refresh')]
    cache_requirements += [('discovery-staleness', 'R004/stale-cache-' + label, 200) for label in (
        'first', '61s-only-marks-stale', 'true-does-not-refresh', 'forced-refresh-clears-stale', 'process-restart-reads-again')]
    audits['T03'] = Audit(
        ('backend/common/client_discovery_cache.py:34', 'backend/common/client_discovery.py:89',
         'backend/routes/client_devices.py:81', 'server/parity/difftest-routers.md:170'),
        'Every T03 listed variant is explicit: regular/cache/refresh1/true, fresh all-failure retaining good '
        'snapshot with refresh_error, partial replacement and unknown missing saved hosts,129 real profiles '
        'testing128-capacity LRU with recency touch, actual simultaneous TCP refreshes gated on a device '
        'read, exact same own-side snapshot, real61s age and restart. Credential/TLS changes invalidate '
        'without forced refresh; same router fields with a new profile revision retain the snapshot. '
        'Router credentials are not overridden in the fake: a wrong new password really returns401, '
        'which becomes a fresh failed snapshot, then restoration reads again. Name changes join current '
        'saved notes without re-reading devices. This does not close H0030/H0031 OS-resource failures '
        'or claim physical device presence/Go parity while the public route is absent.', tuple(cache_requirements))
    native_main = (
        'include', 'missing-main', 'missing-tag', 'duplicate-tag', 'main-type', 'main-args-object',
        'main-args-empty', 'main-step-scalar', 'main-exec-missing', 'main-exec-number', 'main-last-wrong',
        'main-last-conditional', 'main-last-extra', 'missing-local', 'local-type', 'local-args-array',
        'local-upstreams-missing', 'local-upstreams-empty')
    native_listeners = ['listener-none']
    native_listeners += ['listener-args-' + label for label in ('null', 'array', 'string', 'bool', 'number')]
    native_listeners += ['listener-entry-' + label for label in ('missing', 'empty', 'number', 'array')]
    native_listeners += ['listener-http-' + label for label in ('null', 'empty', 'object', 'string', 'bool',
                                                              'number', 'scalar-member', 'missing-exec', 'empty-exec', 'number-exec')]
    native_listeners += ['listener-no-direct-main']
    native_listeners += ['listener-side-port-' + label for label in ('unset', 'env', 'zero', 'overflow', 'invalid', 'same')]
    native_listeners += ['listener-side-reach-missing', 'listener-side-reach-dynamic', 'listener-side-indirect-main',
                         'listener-custom-main-port', 'listener-inner-dynamic']
    def graph_requirements(labels):
        return tuple(('native-policy-' + label, route + '/native-policy-' + middle + label, status)
            for label in labels for route, middle, status in (('R002', '', 200), ('R003', 'mode-refused-', 400)))
    native_required = {
        'H0085': (('native-policy-valid', 'R002/native-policy-recognized-main', 200),
                  ('native-policy-valid', 'R003/native-policy-valid-direct', 200),
                  ('stored-policy-capacity', 'R002/stored-policy-512-accepted', 200)),
        'H0086': graph_requirements((*native_main, *native_listeners)),
        'H0087': graph_requirements(('listener-side-port-env',)),
        'H0088': graph_requirements(tuple('listener-args-' + label for label in ('null', 'array', 'string', 'bool', 'number'))),
        'H0089': graph_requirements(tuple('listener-http-' + label for label in
                                         ('null', 'empty', 'object', 'string', 'bool', 'number', 'scalar-member'))),
    }
    for ident, required in native_required.items():
        audits[ident] = Audit(
            ('backend/common/oxidns_device_policy.py:80', 'backend/common/oxidns_device_policy.py:102',
             'backend/common/oxidns_device_policy.py:106', 'backend/common/oxidns_device_policy.py:145',
             'backend/common/oxidns_device_policy.py:266'),
            'Literal public-import native graphs, exact200 readonly diagnostics then400 atomic mode refusal, '
            'and separate successful name-only/full-native retention/workspace reads. Every _refuse caller '
            'family in main/listener inspection is enumerated: include/tag/main/forward, entry shapes, '
            'client53 and custom main5353 ports, unresolved direct/inner references and indirect main. '
            'H0087 non-string operand cannot be reached here: all _listen_port call sites receive the '
            'listen string normalized by inspect_entries; the environment-placeholder operand is reached. '
            'H0085 covers zero and512 smart-only policies plus a successful direct insertion whose actual '
            'stored native text must contain both managed plugins, client matcher and hook. This does '
            'not close ownership/legacy-node-test/read_policies parent families or the entire route.', required)
    ownership = {
        'H0096': ('missing-policy', 'missing-direct', 'changed-policy', 'changed-direct',
                   'missing-hook', 'duplicate-hook', 'conditional-hook'),
        'H0097': ('hook-before-builtin', 'hook-after-custom-step'),
        'H0098': ('policy-only', 'direct-only', 'hook-only', 'all-unrecorded'),
        'H0099': ('goto-policy-without-hook', 'goto-direct-without-owned', 'recorded-direct-extra-call'),
    }
    for ident, labels in ownership.items():
        required = tuple(('native-owned-' + label, route + '/native-owned-' + middle + label, status)
            for label in labels for route, middle, status in (
                ('R002', '', 200), ('R003', 'mode-refused-', 400), ('R014', 'readonly-', 200)))
        if ident == 'H0098':
            required += (('native-policy-valid', 'R002/native-policy-recognized-main', 200),)
        else:
            required += (('native-owned-valid', 'R003/native-owned-direct-to-smart-withdraws-plugins', 200),)
            if ident != 'H0099':
                required += (('native-owned-valid', 'R002/native-owned-valid-recorded', 200),)
        audits[ident] = Audit(
            ('backend/common/oxidns_device_policy.py:314', 'backend/common/oxidns_common_routing.py:74',
             'backend/routes/client_devices.py:55'),
            'Literal previous direct policy plus its managed plugins/hook, supplied through public import. '
            'Missing/changed plugin and missing/duplicate/conditional hook instantiate each ownership guard; '
            'hook too early/late is tested against recognized built-in and custom main steps. Unrecorded '
            'policy/direct/hook/both conflict independently; extra goto/jump references are rejected. '
            'Every negative reads safely, atomically refuses mode change, allows note-only editing with '
            'complete native retention, and returns the same workspace reason. Positive old ownership '
            'reads editable and changing direct to smart removes both markers from actual stored native '
            'text. No generic _refuse attribution: these four errors are direct ownership exceptions. '
            'This does not close node-test listener classification or all native graph parents.', required)
    side_variants = {
        'H0090': ('invalid-dns-context-members', 'custom-dns-context-members'),
        'H0091': ('isolated-side', 'different-host', 'different-port', 'default-node-port-is53',
                   'wildcard-no-host', 'ipv6-wildcard-no-host'),
        'H0092': ('http-is-not-legacy', 'multiple-http-entries', 'tls-is-not-legacy'),
        'H0093': ('entry-not-sequence',),
        'H0094': ('nested-sequence', 'nested-fallback'),
        'H0095': ('outside-plugin-reference', 'foreign-plugin-reference'),
    }
    legacy_controls = ('legacy-direct-host', 'legacy-wildcard-agent-host', 'legacy-ipv6-wildcard-agent-host',
                       'legacy-empty-wildcard-agent-host', 'udp-tcp-group-legacy')
    for ident, labels in side_variants.items():
        if ident != 'H0090':
            labels = (*labels, *legacy_controls)
        required = tuple(('native-side-' + label, route + '/native-side-' + middle + label, 200)
            for label in labels for route, middle in (('R002', ''), ('R003', 'direct-')))
        audits[ident] = Audit(
            ('backend/common/oxidns_device_policy.py:198', 'backend/common/oxidns_device_policy.py:231',
             'backend/common/oxidns_references.py:27'),
            'Real native side listeners, public system node-test DNS settings and an optional disabled '
            'profile-bound OxiDNS registry record; no helper monkeypatch. Literal bad context members '
            'are skipped in both legacy/custom DNS lists. Empty/different/default53 address contexts, '
            'wildcard host without a known Agent, HTTP multiple entries/non-UDP-TCP types, non-sequence '
            'entry, nested sequence/fallback and external owners independently keep side_entries visible. '
            'Exact/direct and all three wildcard forms with known Agent hosts and grouped UDP/TCP '
            'listeners are recognized as isolated legacy node-test chains. Applying direct policies '
            'keeps side upstream/opaque settings; note-only editing keeps complete native bytes. '
            'Neutral main tags reach these guards; the udp tag/udp:// false reference is a separately '
            'recorded Python bug, never hidden by changing a device fake.', required)
    invalid_labels = ['mode']
    invalid_labels += ['native-type-' + label for label in ('null', 'bool', 'number', 'array', 'object')]
    invalid_labels += ['yaml-' + label for label in ('empty', 'array', 'null', 'scalar', 'syntax', 'duplicate',
                                                    'key-number', 'key-forbidden', 'loop', 'nonfinite', 'binary')]
    invalid_labels += ['files-shape-' + label for label in ('null', 'bool', 'number', 'object', 'string')]
    invalid_labels += ['files-257']
    invalid_labels += ['file-row-' + label for label in ('null', 'array', 'number', 'string')]
    invalid_labels += ['file-path-' + label for label in ('null', 'empty', 'number', 'array', 'object', 'backslash',
        'control', 'absolute', 'parent', 'empty-part', 'dot-part', 'parent-part')]
    invalid_labels += ['file-reserved-' + label for label in ('config', 'manifest', 'state', 'state-child', 'hidden', 'hidden-child')]
    invalid_labels += ['file-' + label for label in ('duplicate', 'parent-first', 'child-first')]
    invalid_labels += ['file-content-' + label for label in ('null', 'number', 'array', 'object', 'encoding')]
    invalid_labels += ['file-base64']
    families = [('native-policy', (*native_main, *native_listeners)),
                ('native-owned', tuple(label for labels in ownership.values() for label in labels)),
                ('native-invalid', invalid_labels)]
    readonly = tuple((prefix + '-' + label, route + '/' + prefix + '-' + middle + label, 200)
        for prefix, labels in families for label in labels
        for route, middle in (('R002', ''), ('R014', 'readonly-')))
    ordinary = (('native-policy-valid', 'R002/native-policy-recognized-main', 200),
                ('native-owned-valid', 'R002/native-owned-valid-recorded', 200))
    parent_sources = ('backend/common/oxidns_device_policy.py:357', 'backend/common/oxidns_editor.py:107',
                      'backend/common/oxidns_settings.py:116', 'backend/common/oxidns_native.py:65')
    for ident in ('H0100', 'H0101'):
        audits[ident] = Audit(parent_sources,
            'Actual read_policies exception/readonly return across native settings/YAML/attachment normalization, '
            'all main/listener refusals and all managed ownership families, through both client and workspace '
            'GETs. Covers schema/type/duplicate/unsafe keys/cycles/nonfinite/binary, metadata count/paths/'
            'reserved/duplicate/file-directory/content/base64 errors without writing files. Positive empty '
            'and old-owned native chains remain editable. Does not claim every callee outside the routers '
            'file inventory or physical DNS core validation; malformed raw policy rows fail before this try '
            'and are separately covered by H0080-H0084/API400, not falsely counted as readonly200.', readonly + ordinary)
    audits['R002-F12'] = Audit(parent_sources,
        'All120 negative native graph/settings worlds return200 editable=false with exact diagnostic '
        'and unchanged complete state; callers can still rename notes without rewriting native data. '
        'The valid udp:// reference collision is a separate Python bug, not a legitimate rejection.',
        tuple(requirement for requirement in readonly if requirement[1].startswith('R002/')))
    audits['R003-F15'] = Audit(parent_sources,
        'All120 negative native graph/settings worlds reject a valid selected-profile, fresh-previous '
        'mode change with exact400 and complete state unchanged; name-only edits remain usable and '
        'retain native settings. Existing direct ownership cases change toward smart, others toward direct.',
        tuple((prefix + '-' + label, 'R003/' + prefix + '-mode-refused-' + label, 400)
              for prefix, labels in families for label in labels))
    split_sources = ('backend/common/router_split.py:46', 'backend/common/router_split.py:143',
                     'backend/common/router_split.py:162', 'backend/routes/generate.py:preview_mihomo',
                     'backend/routes/agents.py:_prepare_transactional_content')
    invalid_markers = ['null', 'bool', 'array', 'empty', 'extra', 'version-bool', 'version-float',
                       'version-string', 'takeover', 'origin', 'version-zero', 'missing-key']
    invalid_markers += ['ids-' + label for label in ('null', 'bool', 'object', 'string', '1025',
        'empty', 'null-row', 'bool-row', 'number-row', 'array-row', 'object-row', '257')]
    invalid_markers += ['control-' + str(number) for number in (*range(32), 127)]
    marker_guards = tuple(('split-marker-' + label, 'G23/config-marker-' + label, 500)
                          for label in invalid_markers)
    valid_markers = tuple(('split-valid-' + label, 'G23/config-valid-' + label, 200)
                         for label in ('empty', '1024-duplicate', 'unicode256'))
    audits['H0110'] = Audit(split_sources,
        'Actual public config preview rejects invalid marker shape/keys/exact-int version/takeover/origin; '
        'IDs cover list type, cap1025, row types, empty text,257 Unicode characters, each C0 and DEL. '
        'Empty IDs,1024 duplicates and256 Unicode characters are real public200 controls. The preview '
        'wrapper translates RouterSplitError to500; no exception or generated output is emulated. '
        'External routes have their own manifest oracle and do not count among26 routers routes.',
        marker_guards + valid_markers)
    audits['H0111'] = Audit(split_sources,
        'Literal enabled=true on a valid full_tun marker refuses public config preview with exact500 '
        'message. Three valid-marker gateway-disabled previews succeed; no Agent/device I/O or storage write.',
        (('split-gateway-conflict', 'G23/config-full-tun-gateway-conflict', 500),) + valid_markers)
    missing_tun = ['missing-tun'] + ['tun-' + label for label in ('null', 'bool', 'array', 'number', 'string')]
    missing_tun += [key + '-' + label for key in ('enable', 'auto-route', 'auto-redirect')
                   for label in ('missing', 'false', 'one', 'string')]
    missing_tun += ['hijack-' + label for label in ('null', 'string', 'object', 'empty', 'tcp-only', 'wrong-case')]
    missing_tun += ['old-route-' + key for key in ('route-address', 'inet4-route-address', 'inet6-route-address',
                                                 'route-address-set', 'route-exclude-address-set')]
    missing_tun += ['gateway-marker']
    audits['H0112'] = Audit(split_sources,
        'Actual Agent final generation, not preview, reaches require_full_tun_generated. Real Docker Agent '
        'binding/version avoids unrelated live forwarding preconditions. Missing/nonobject tun, each required '
        'literal-true flag, hijack list/any:53, all five removed route keys (even empty) and old gateway marker '
        'refuse400 with exact missing-TUN message. Complete state unchanged and zero Agent requests. Root '
        'nonobject YAML fails earlier in converter validation and is not falsely claimed at this guard.',
        tuple(('split-agent-' + label, 'G23/agent-full-tun-' + label, 400) for label in missing_tun))
    filter_keys = ('include-interface', 'exclude-interface', 'include-uid', 'include-uid-range',
                   'exclude-uid', 'exclude-uid-range', 'include-mac-address', 'exclude-mac-address',
                   'include-android-user', 'include-package', 'exclude-package', 'all')
    audits['H0113'] = Audit(split_sources,
        'Each of eleven TUN filter keys and the combined ordered diagnostic refuse actual Agent final '
        'generation with400. The valid full-TUN shape reaches this guard rather than the preceding missing '
        'shape check. No mocked Python exceptions, Agent delivery or generated-preview record is used.',
        tuple(('split-agent-filter-' + key, 'G23/agent-full-tun-filtered-' + key, 400) for key in filter_keys))
    base_sources = ('backend/common/router_split.py:95', 'backend/common/router_split.py:118',
                    'backend/common/gateway_onboarding.py:500', 'backend/routes/gateway_onboarding.py:gateway_onboarding')
    accepted_bases = tuple(('split-onboarding-accepted-' + label, 'G23/onboarding-accepted-' + label, 200)
                           for label in ('base-empty', 'tun-null', 'tun-custom'))
    base_guards = {
        'H0102': tuple('tun-' + label for label in ('bool', 'number', 'string', 'array')),
        'H0103': tuple('filter-' + key for key in filter_keys if key != 'all'),
        'H0104': tuple('base-type-' + label for label in ('null', 'bool', 'number', 'array', 'object')),
        'H0105': tuple('base-yaml-' + label for label in ('syntax', 'recursive-depth', 'unsafe-tag')),
        'H0106': tuple('base-yaml-' + label for label in ('syntax', 'recursive-depth', 'unsafe-tag')),
        'H0107': tuple('base-root-' + label for label in ('null', 'bool', 'number', 'array', 'string')),
    }
    for ident, labels in base_guards.items():
        required = tuple(('split-onboarding-' + label, 'G23/onboarding-' + label, 200) for label in labels)
        if ident in ('H0102', 'H0104', 'H0107'):
            required += accepted_bases
        audits[ident] = Audit(base_sources,
            'Only the actual public gateway-onboarding preview invokes full_tun_base_yaml here. Its200 '
            'report has ready=false and the exact single blocker; it is not an accepted install. Nonobject '
            'tun, all eleven truthy filters, nontext base, actual YAML syntax/constructor/RecursionError '
            'and nonobject YAML roots are separate inputs. Empty base, null tun and custom stack/mtu '
            'advance through rendering and both service validations to the later no-proxy prerequisite. '
            'The preview does not persist candidates or contact devices. No private Python call is used '
            'as the expected result and no recursion error is simulated.', required)
    state_sources = ('backend/common/router_split.py:143', 'backend/common/router_split.py:207',
                     'backend/common/router_split.py:263', 'backend/routes/linux_gateway.py:71')
    malformed_base = tuple('yaml-' + label for label in ('syntax', 'recursive-depth', 'unsafe-tag'))
    state_guards = {
        'H0108': tuple('root-' + label for label in ('null', 'bool', 'number', 'array', 'string'))
                  + ('old-gateway-marker',) + malformed_base,
        'H0109': ('missing-tun',) + tuple('tun-' + label for label in ('null', 'bool', 'number', 'array', 'string')),
        'H0114': malformed_base,
        'H0115': malformed_base,
    }
    for ident, labels in state_guards.items():
        audits[ident] = Audit(state_sources,
            'Real public Linux gateway editor GET diagnoses valid-marker base YAML without generating '
            'or rewriting it. Root shape/old marker and missing/nonobject tun produce tun_missing; actual '
            'syntax, unsafe-constructor and recursion errors are caught by _base_document and also '
            'produce tun_missing. Complete full-TUN control has no issues. Eleven isolated filter '
            'diagnostics are additional public controls, not used to inflate these requirement sets. '
            'Full response/schema, no-store and complete before/after storage/device equality are retained.',
            tuple(('split-state-' + label, 'G23/editor-state-' + label, 200)
                  for label in (*labels, 'valid-full-tun')))
    forwarding_sources = ('backend/agents/linux_gateway.py:53', 'backend/agents/linux_gateway.py:335',
                          'backend/agents/linux_gateway.py:654', 'backend/routes/routers.py:130')
    bad_versions = ('null', 'false', 'number', 'array', 'object', 'empty', 'text', 'no-patch', 'four-parts',
                    'prefix', 'suffix', 'extra-suffix', 'leading-space', 'trailing-space', 'newline', 'fullwidth', 'negative')
    old_versions = ('old', 'old-major', 'old-minor')
    accepted_versions = ('aio-1.8.40', 'standalone-1.8.41', 'suffixless', 'leading-zeroes', 'new-major', 'new-minor')
    version_ok = tuple(('forwarding-version-accepted-' + label, 'R026/forwarding-version-accepted-' + label, 200)
                       for label in accepted_versions)
    version_bad = tuple(('forwarding-version-' + label, route + '/forwarding-guard-version-' + label, status)
                        for label in bad_versions for route, status in (('R025', 200), ('R026', 409)))
    audits['H0001'] = Audit(forwarding_sources,
        'Real imported registry values plus an authenticated empty heartbeat reach the version gate online. '
        'Five nonstring JSON types, empty/text/part count/prefix/suffix/space/newline/fullwidth/negative '
        'forms refuse without any Agent request on both GET/POST. Native/public valid version controls '
        'include exact forwarding/standalone boundaries, suffixless, leading zeroes and newer major/minor. '
        'Does not claim the later exception catch: H0002/H0003 remain separate reachability work.',
        version_bad + version_ok)
    layout_bad = tuple(('forwarding-layout-' + label, 'R026/forwarding-layout-' + label, 409)
                       for label in ('missing-status', 'unsupported-layout'))
    audits['H0004'] = Audit(forwarding_sources,
        'Old1.8.40 actual status read has either no forwarding object or supported=false on a supported '
        'platform: exact standalone409 and zero forwarding POST. AIO1.8.40 reads once then persists; '
        '1.8.41/newer short-circuits the status read and really persists, proving both guard operands.',
        layout_bad + version_ok)
    basic_guards = (('H0018', 'missing', 404), ('H0019', 'wrong-service', 400),
                    ('H0020', 'docker', 409), ('H0021', 'offline', 409))
    for ident, label, status in basic_guards:
        audits[ident] = Audit(forwarding_sources,
            'Current-profile router link authorizes the named Agent before the real persist precheck. '
            'Registry absence, valid OxiDNS service, Docker and genuinely offline/no-heartbeat cases '
            'return their exact status/code/message and preserve all state with zero Agent requests. '
            'The corresponding GET has a different precheck and is tracked under R025-F10, not '
            'mislabelled as reaching this persist helper.',
            (('forwarding-guard-' + label, 'R026/forwarding-guard-' + label, status),))
    version_post_bad = tuple(item for item in version_bad if item[1].startswith('R026/'))
    version_old = tuple(('forwarding-version-' + label, 'R026/forwarding-guard-version-' + label, 409)
                        for label in old_versions)
    audits['H0022'] = Audit(forwarding_sources,
        'Actual persist rejects all malformed versions plus older major/minor/patch versions after '
        'a real fresh heartbeat. Six actual persisted successes bind the numerical/format boundaries; '
        'GET responses are not falsely claimed as executing persist_gateway_forwarding.',
        version_post_bad + version_old + version_ok)
    audits['R025-F10'] = Audit(forwarding_sources,
        'Each missing/service/Docker/offline/version precheck is an ordinary200 entry issue with exact '
        'message, cannot persist and no Agent I/O. Never turn one entry issue into a handler failure.',
        tuple(('forwarding-guard-' + label, 'R025/forwarding-guard-' + label, 200) for _, label, _ in basic_guards)
        + tuple(('forwarding-version-' + label, 'R025/forwarding-guard-version-' + label, 200)
                for label in (*bad_versions, *old_versions)))
    audits['R026-F12'] = Audit(forwarding_sources,
        'All persist preconditions through actual HTTP: registry absence/service/Docker/offline, every '
        'malformed/old version and both old-layout refusals. The current-profile link is present and '
        'authentication/body guards are valid, so an earlier authorization failure cannot satisfy this.',
        tuple(('forwarding-guard-' + label, 'R026/forwarding-guard-' + label, status) for _, label, status in basic_guards)
        + version_post_bad + version_old + layout_bad)
    transport_sources = ('backend/agents/linux_gateway.py:380', 'backend/agents/linux_gateway.py:535',
                         'backend/agents/linux_gateway.py:638', 'backend/routes/routers.py:130')
    def errors(route, labels):
        return tuple(('forwarding-failures', route + '/agent-' + label, 200 if route == 'R025' else status)
                     for label, status in labels)
    size_read = tuple(('forwarding-failures', 'R025/response-byte-limit-' + str(length), 200)
                      for length in (524288, 524289))
    size_post = tuple(('forwarding-failures', 'R026/response-byte-limit-' + str(length) + '-' + str(applied), status)
                      for length, applied, status in ((524288, True, 200), (524289, False, 502), (524289, True, 502)))
    size_errors = size_read[1:] + size_post[1:]
    for ident, required in (('H0005', size_read + size_post), ('H0006', size_errors), ('H0007', size_errors)):
        audits[ident] = Audit(transport_sources,
            'Actual streamed Agent JSON at524288 bytes is accepted;524289 raises the bridge MonitorError. '
            'GET and POST limits are checked; overlarge forwarding reply includes not-applied and '
            'really-persisted variants, one POST only and subsequent public GET confirms actual state. '
            'The inner MonitorError rethrow is exercised by real streamed oversize responses.', required)
    transport_errors = (('forwarding-failures', 'R025/agent-lost-response', 200),
        ('forwarding-uncertain', 'R026/lost-reply-not-applied', 502),
        ('forwarding-uncertain', 'R026/lost-reply-applied', 502),
        ('forwarding-uncertain', 'R025-26/real-timeout-status', 200),
        ('forwarding-uncertain', 'R025-26/real-timeout-forwarding', 502))
    for ident in ('H0008', 'H0009'):
        audits[ident] = Audit(transport_sources,
            'Real disconnected TCP replies and45-second requests timeouts on status and forwarding. '
            'Write uncertainty is tested before/after actual persistence and confirmed with own public '
            'GET; no automatic repeat POST. The apply/remove texts belong to other Agent endpoints, '
            'outside the matrix K01 forwarding-only scope; no synthetic Python exception is raised.', transport_errors)
    missing_api = errors('R025', (('missing-api', 200),)) + errors('R026', (('missing-api', 409),))
    auth_errors = errors('R025', tuple(('auth-' + str(code), 200) for code in (401, 403)))
    auth_errors += errors('R026', tuple(('auth-' + str(code), 502) for code in (401, 403)))
    audits['H0010'] = Audit(transport_sources, 'Real404 Agent API response, GET entry upgrade and POST409 with operation-specific minimum.', missing_api)
    audits['H0011'] = Audit(transport_sources, 'Both real401 and403 Agent replies map to exact authentication errors; all other healthy entries survive.', auth_errors)
    parse_labels = ('invalid-json', 'invalid-utf8', *(('redirect-' + str(code)) for code in (301, 302, 303, 307, 308)))
    parse_errors = errors('R025', tuple((label, 200) for label in parse_labels))
    parse_errors += errors('R026', tuple((label, 502) for label in parse_labels))
    for ident in ('H0012', 'H0013'):
        audits[ident] = Audit(transport_sources,
            'Actual malformed JSON, undecodable UTF-8 bytes and five real redirect statuses with HTML. '
            'Redirect Location points only to the isolated Agent fake; explicit zero requests to that '
            'path proves no following. Real json.loads errors are caught, not mocked.', parse_errors)
    object_errors = errors('R025', tuple(('not-object-' + str(index), 200) for index in range(5)))
    object_errors += errors('R026', tuple(('shape-' + str(index), 502) for index in range(5)))
    audits['H0014'] = Audit(transport_sources, 'All nonobject JSON root types from real Agent HTTP: array/null/bool/number/string, both operations.', object_errors)
    message_labels = ('absent', 'nontext-number', 'nontext-true', 'nontext-array', 'nontext-object',
                      'fallback-null', 'fallback-false', 'fallback-empty', 'fallback-array', 'fallback-object',
                      'priority', 'unicode-limit')
    http_read = tuple(('http-' + str(code), 200) for code in (400, 409, 422, 500, 502, 503))
    http_post = tuple(('http-' + str(code), code if code in (400, 409, 422) else 502) for code in (400, 409, 422, 500, 503))
    message_read = tuple(('message-' + label, 200) for label in message_labels)
    message_post = tuple(('message-' + label, 502) for label in message_labels)
    http_errors = errors('R025', http_read + (('unicode-error-limit', 200),) + message_read)
    http_errors += errors('R026', http_post + message_post)
    audits['H0015'] = Audit(transport_sources,
        'HTTP error status mapping400/409/422 vs502, absent/nontext message fallback, falsy message '
        'falling back to error, message priority and1000-Unicode-codepoint clipping; actual Agent HTTP '
        'bodies are processed by Python. Error text is exact and healthy second gateway survives.', http_errors)
    filter_required = tuple(('forwarding-failures', 'R025/filter-dropped-' + str(index), 200) for index in range(11))
    filter_required += tuple(('forwarding-failures', 'R025/filter-state-' + state, 200)
                             for state in ('ok', 'may_drop', 'unknown', 'not_applicable'))
    filter_required += tuple(('forwarding-failures', 'R025/' + label, 200)
                             for label in ('filter-safe-display-shape', 'filter-exact-types'))
    filter_required += tuple(('forwarding-failures', 'R025/filter-bug-' + label, 500)
                             for label in ('state-array', 'state-object', 'source-kind-array', 'source-kind-object',
                                           'remedy-source-array', 'remedy-source-object'))
    audits['H0016'] = Audit(transport_sources,
        'Outer nonobject/invalid state types, every allowed state, explicit safe output caps/type/command '
        'filter controls. Unhashable state/source-kind/remedy-source arrays AND objects are real scoped '
        'traceback-confirmed Python bugs; matching a native crash never passes parity. This audit does '
        'not claim real host firewall semantics, which remain L5.', filter_required)
    read_error_requirements = (errors('R025', http_read + (('unicode-error-limit', 200),) + message_read)
        + errors('R025', tuple((label, 200) for label in parse_labels))
        + errors('R025', tuple(('not-object-' + str(index), 200) for index in range(5)))
        + errors('R025', (('auth-401', 200), ('auth-403', 200), ('missing-api', 200), ('lost-response', 200)))
        + size_read[1:] + transport_errors[3:4])
    for ident in ('H0017', 'R025-F11'):
        audits[ident] = Audit(transport_sources,
            'All public read error families, including streamed size and real timeout, retain healthy '
            'gateway entries and turn the affected entry into exact error/upgrade state. Four forwarding '
            'object layout issues and unhashable sanitizer crashes are separate branches, not falsely '
            'labelled as caught MonitorError.', read_error_requirements)
    post_error_requirements = errors('R026', http_post + message_post)
    post_error_requirements += errors('R026', tuple((label, 502) for label in parse_labels))
    post_error_requirements += errors('R026', tuple(('shape-' + str(index), 502) for index in range(5)))
    post_error_requirements += errors('R026', (('auth-401', 502), ('auth-403', 502), ('missing-api', 409)))
    audits['R026-F13'] = Audit(transport_sources,
        'Every actual forwarding device-response failure family plus exact byte-boundary control, '
        'real45-second timeout and both lost-reply outcomes. Full device/DB/file state and one POST '
        'only are compared; no proxy follows a redirect and no generic500 is accepted.',
        post_error_requirements + size_post + transport_errors[1:3] + transport_errors[4:5])
    lock_required = []
    for vendor in ('routeros', 'ikuai'):
        labels = ('R009', 'R010', 'R011', 'R015', 'R017', 'R020', 'R024')
        if vendor == 'routeros':
            labels += ('shared-connection',)
        for label in labels:
            route = 'R010' if label == 'shared-connection' else label
            world = 'job-lock-' + vendor + '-' + label
            lock_required += [(world, route + '/busy-before-body-' + vendor + '-' + label, 409),
                              (world, 'R017/gated-probe-finish-' + vendor + '-' + label,
                               200 if vendor == 'routeros' else 502)]
    audits['H1278'] = Audit(
        ('backend/routes/routers.py:37', 'backend/routes/routers.py:52', 'backend/routers/rule_sync_store.py:52'),
        'All seven actual decorator callers/methods for both vendors plus the shared physical connection '
        'across alpha/beta. A real public probe reaches its device gate before each malformed-body409; '
        'then the same pending probe is released/joined and saves exactly one last_probe revision. '
        'Each isolated world needs only its target and the holding probe, never all sibling routes. '
        'Readonly plan/report do not enter this decorator despite the original H scope shorthand. '
        'This proves the HTTP nonacquired branch with real in-process contention, not separate-process '
        'flock or scheduler/crash recovery, which remain T25 and their own helper entries.', tuple(lock_required))
    client_sources = ('backend/routes/client_devices.py:39', 'backend/routes/client_devices.py:55',
                      'backend/common/client_devices.py:28', 'backend/common/client_devices.py:39')
    agent_pin_bad = tuple(('agent-scope', route + '/invalid-agent-' + agent, 404)
                          for route in ('R002', 'R004') for agent in ('missing-agent', '', 'm-a'))
    agent_pin_ok = tuple(('agent-scope', 'R002-S03/' + agent, 200) for agent in ('o-a', 'o-b'))
    discovery_pin_ok = (('agent-scope', 'R004-S04/pinned-empty-profile', 200),)
    query_writes = tuple(('client-agent-query-writes', 'R003/PUT-ignores-agent-' + agent, 200)
                         for agent in ('o-b', 'missing-agent'))
    audits['H1256'] = Audit(client_sources,
        'Actual agent pin guard is reachable on R002 GET and R004 GET/HEAD, not every R002–R006 method '
        'in the original shorthand. Missing/empty/wrong-service IDs, alpha/beta OxiDNS successes, '
        'unsupported beta discovery, distinct HEAD behaviour (R002 ignores pin, R004 keeps it), and '
        'PUT ignores both a valid foreign-profile Agent and unknown Agent. Writes retain DNS/bindings '
        'and foreign profiles, no device I/O. Missing legacy bindings are a separate loading barrier.',
        agent_pin_bad + agent_pin_ok + discovery_pin_ok
        + (('agent-scope', 'R002-B92/head-ignores-missing-agent', 200),
           ('agent-scope', 'R004/head-retains-agent-pin-invalid', 404)) + query_writes)
    good_draft = (('client-drafts', 'R003-S01/0', 200),)
    draft_shape = tuple(('guards', 'R003-F10/' + label, 400)
                        for label in ('none', 'null', 'array', 'text', 'bool', 'number', 'malformed', 'wrong-content-type'))
    draft_shape += tuple(('guards', 'R003/missing-' + key, 400) for key in
                         ('profile_id', 'ip', 'name', 'mode', 'previous_name', 'previous_mode', 'base_url'))
    draft_shape += (('guards', 'R003/extra', 400),)
    draft_shape += tuple(('guards', 'R003/base_url-' + repr(value), 400) for value in (None, 0, False, [], {}))
    bad_profiles = tuple(('guards', 'R003/profile-' + repr(value), 409)
                         for value in ('beta', '', None, 0, False, True, [], {}, 1.5, ' alpha ', 'ALPHA'))
    stale_drafts = (('guards', 'R003/previous_name-stale', 409), ('guards', 'R003/previous_mode-stale', 409))
    audits['H1257'] = Audit(client_sources,
        'Eight real body parsing/root-type failures, each missing key, extra key and five nonstring '
        'base_url values reach the wrapper400 with exact message and full state unchanged. The actual '
        'valid fresh-note write is the successful shape control.', draft_shape + good_draft)
    audits['H1258'] = Audit(client_sources,
        'Valid exact object with wrong body profile: foreign/empty/case/whitespace and every JSON '
        'type including unhashable array/object. Exact409 and unchanged state, fresh alpha write200 '
        'control; no inferred type-validation error or accidental parsing failure.', bad_profiles + good_draft)
    for ident in ('H1259', 'H1260'):
        audits[ident] = Audit(client_sources,
            'Real stale previous name and mode are the two DeviceChanged operands inside the transaction; '
            'both409 exact message/full state unchanged. A fresh empty row actually commits200. '
            'OxidnsSettingsError translation is a separate unfinished parent audit, not inferred here.',
            stale_drafts + good_draft)
    normalizer_failures = tuple(sorted(set(requirement for ident in ('H0068', 'H0023', 'H0078')
                                          for requirement in client_requirements[ident])))
    for ident, required in (('R003-F10', draft_shape), ('R003-F11', bad_profiles),
                            ('R003-F12', normalizer_failures), ('R003-F13', stale_drafts[:1]),
                            ('R003-F14', stale_drafts[1:])):
        audits[ident] = Audit(client_sources,
            'Explicit endpoint-specific parsing/profile/field/stale-row prerequisites. Normalizer '
            'variants reuse the source-reviewed H0068/H0023/H0078 exact lists (127 typed/error inputs), '
            'not whatever recipes happen to claim the parent ID. No rejected request changes any state.', required)
    audits['R002-S03'] = Audit(client_sources, 'Both real alpha/beta OxiDNS Agent pins, with response header still reflecting the selected request profile.', agent_pin_ok)
    audits['R004-S04'] = Audit(client_sources, 'Actual beta-pinned discovery with no routers returns unsupported/empty data and real timestamp, selected header alpha.', discovery_pin_ok)
    audits['R003-B06'] = Audit(client_sources, 'Valid foreign-profile and missing Agent queries cannot change PUT scope; both write alpha notes only and retain all DNS/binding/foreign state.', query_writes)
    raw_policy_errors = tuple(('stored-policy-' + label, 'R003/stored-policy-' + label, 400)
                              for labels in policy_variants.values() for label in labels)
    raw_ip_labels = ('missing',) + tuple(ident + '-' + str(index) for ident, values in invalid_ranges.items()
                                        for index in range(len(values))) + ('H0078-0', 'H0078-1')
    raw_ip_errors = tuple(('stored-policy-ip-' + label, 'R003/stored-policy-ip-' + label, 400)
                          for label in raw_ip_labels)
    growth_errors = (('notes-capacity', 'R003/notes-2049-refused-atomically', 400),
                     ('notes-capacity', 'R003/notes-2049-policy-change-rollback', 400),
                     ('stored-policy-capacity', 'R003/policy-513-growth-refused-atomically', 400))
    settings_errors = normalizer_failures + raw_policy_errors + raw_ip_errors
    settings_errors += audits['R003-F15'].requirements + growth_errors + good_draft
    for ident in ('H1261', 'H1262'):
        audits[ident] = Audit(client_sources + ('backend/common/oxidns_device_policy.py:54',
                                               'backend/common/device_notes.py:18'),
            'All routers-inventory OxidnsSettingsError families at R003:127 request normalizer inputs, '
            '65 raw policy shape/name/mode/duplicate cases,32 bad stored-IP/missing-field contexts, '
            '120 native graph/settings/ownership refusals, actual512→513 policy growth, notes2048→2049 '
            'with and without a simultaneous DNS policy rewrite, and a fresh success control. All '
            'refusals compare complete DB/files/devices unchanged; the latter note failure occurs '
            'after preparing the policy and must roll both changes back. Invalid stored device_notes '
            'cannot pass public import/loading (separately proven), so not forged into this HTTP '
            'catch. Does not claim all shared C22 normalizer internals outside the routers file inventory '
            'or repository/transaction errors, which have separate parent entries.', settings_errors)
    fixed_sources = ('backend/common/client_fixed_ip.py:68', 'backend/routes/client_devices.py:105',
                     'backend/routes/client_devices.py:129')
    root_labels = ('none', 'null', 'array', 'text', 'bool', 'number', 'malformed', 'wrong-content-type')
    preview_shape = tuple(('guards', 'R006-F10/' + label, 400) for label in root_labels)
    preview_shape += tuple(('guards', 'R006/missing-' + field, 400) for field in ('profile_id', 'operation', 'devices'))
    preview_shape += (('guards', 'R006/extra', 400),)
    preview_operation = tuple(('guards', 'R006/operation-' + repr(value), 400)
                              for value in ('ADD', 'unknown', '', None, 1, False, [], {}))
    preview_devices = tuple(('guards', 'R006/devices-' + label, 400) for label in
                            ('null', 'number', 'bool', 'empty-string', 'string', 'empty-object', 'object', 'empty', '65'))
    preview_rows = tuple(('guards', 'R006/row-' + label, 400)
                         for label in ('null', 'number', 'bool', 'string', 'array', 'empty-object', 'extra'))
    preview_rows += tuple(('guards', 'R006/row-missing-' + field, 400) for field in ('ip', 'mac', 'router_id', 'name'))
    scope_values = ('beta', '', None, 0, False, True, [], {}, 1.5, ' alpha ', 'ALPHA')
    preview_profile = tuple(('guards', 'R006/profile-' + repr(value), 409) for value in scope_values)
    apply_shape = tuple(('guards', 'R005-F10/' + label, 400) for label in root_labels)
    apply_shape += tuple(('guards', 'R005/missing-' + field, 400) for field in ('profile_id', 'token'))
    apply_shape += (('guards', 'R005/extra', 400),)
    apply_profile = tuple(('guards', 'R005/profile-' + repr(value), 409) for value in scope_values)
    preview_positive = (('fixed-ip', 'R006/fixed-add-dynamic', 200),
                        ('fixed-ip-64', 'R006/mixed-64-add', 200), ('fixed-ip-64', 'R006/mixed-64-delete', 200))
    audits['H0036'] = Audit(fixed_sources,
        'Exact outer object, allowed operation and1..64 device list: all JSON roots, missing/extra '
        'members, malformed/content-type cases, all operation types and list types including NONEMPTY '
        'wrong string/map (not hidden by a simultaneous empty-list check), zero/65. Actual one-row '
        'and64-row add/delete previews succeed with real device reads and signed reference payloads.',
        preview_shape + preview_operation + preview_devices + preview_positive)
    audits['H0037'] = Audit(fixed_sources,
        'Each wrong row type, empty/extra object and every missing IP/MAC/router/name key. Actual '
        'one-row and64-row add/delete are schema controls. Field-value/MAC/IP/name normalization '
        'is separate; this audit does not pretend those helpers are covered by an outer-shape400.',
        preview_rows + preview_positive)
    for ident, required in (('H1263', preview_profile), ('H1270', apply_shape), ('H1271', apply_profile),
                            ('R005-F10', apply_shape), ('R005-F11', apply_profile),
                            ('R006-F10', preview_shape), ('R006-F11', preview_operation),
                            ('R006-F12', preview_devices), ('R006-F13', preview_rows), ('R006-F16', preview_profile)):
        audits[ident] = Audit(fixed_sources,
            'Actual endpoint-specific shape/profile/operation/list/row guard with exact message and '
            'complete state unchanged. Profile mismatch uses eleven wrong values of every JSON type '
            'plus case/whitespace and a valid row/token-shaped object, before device I/O or apply '
            'cache invalidation; no unknown signed-token value is substituted from Python into Go.', required)
    normalization_sources = ('backend/common/client_fixed_ip.py:68',
                             'backend/routers/lan_clients.py:64', 'backend/routers/lan_clients.py:72')
    normal_ip = ('null', 'false', 'true', 'zero', 'integer', 'fraction', 'array', 'object', 'empty',
                 'blank', 'garbage', 'ipv6', 'mapped-v6', 'cidr', 'leading-zero', 'unicode-digit',
                 'octet-overflow', 'extra-octet', 'sign', 'zone', 'newline-inside')
    normal_mac = ('null', 'false', 'true', 'zero', 'integer', 'fraction', 'array', 'object', 'empty',
                  'blank', 'five', 'seven', 'dot', 'no-separator', 'long-octet', 'empty-octet',
                  'non-hex', 'bad-hex-prefix')
    normal_router = ('null', 'false', 'true', 'zero', 'integer', 'fraction', 'array', 'object', 'empty')
    normal_good = ('canonical', 'hyphen', 'short-octets', 'mixed-separators', 'outer-whitespace',
                   'empty-name', 'blank-name', 'ascii80', 'unicode80', 'lowercase', 'mixed-case',
                   'same-MAC-different-router')
    def normalized_case(label, status=400):
        return ('fixed-ip-normalization', 'R006/normalize-' + label, status)
    bad_fields = tuple(normalized_case(field + '-' + label)
                       for field, labels in (('ip', normal_ip), ('mac', normal_mac), ('router_id', normal_router))
                       for label in labels)
    duplicate_ips = tuple(normalized_case('duplicate-' + label)
                          for label in ('ip-same-router', 'ip-foreign-router', 'ip-trimmed'))
    duplicate_macs = tuple(normalized_case('duplicate-' + label)
                           for label in ('mac-hyphen', 'mac-short-octets', 'mac-trimmed'))
    normal_controls = tuple(normalized_case('accepted-' + label, 200) for label in normal_good)
    for ident in ('H0038', 'R006-F14'):
        audits[ident] = Audit(normalization_sources,
            'Every JSON type for IP/MAC/router ID, blank/nonempty wrong containers, IPv6/mapped IPv6, '
            'CIDR, IPv4 leading zero/Unicode/overflow/extra octet/sign/zone/internal newline; MAC '
            'segment count/length/notation and actual int-hex failures. Duplicate IP on same/different '
            'routers and after trimming. All97 invalid-field/name/duplicate vectors are real HTTP '
            'requests; this guard specifically requires48 field errors and3 IP duplicates. Twelve '
            'device-backed positive previews include trim/hyphen/short/mixed MACs and same canonical '
            'MAC on TWO routers. No private normalizer invocation or production data.',
            bad_fields + duplicate_ips + normal_controls if ident == 'H0038' else bad_fields + duplicate_ips)
    for ident in ('H0039', 'R006-F15'):
        required = (('guards', 'R006/same-router-mac', 400),) + duplicate_macs
        audits[ident] = Audit(normalization_sources,
            'Canonical duplicate MAC, hyphen form, short octets and outer whitespace on distinct IPs '
            'of the SAME router all400 with the later MAC-specific message. Positive control uses '
            'the same MAC on different vendors, both with actual sightings; the request order is '
            'iKuai then RouterOS and actions must retain that order. No duplicate-IP predecessor.',
            required + normal_controls if ident == 'H0039' else required)
    for ident, required, detail in (
        ('H0592', tuple(normalized_case('ip-' + label) for label in normal_ip),
         'Actual IPv4Address ValueError becomes empty IP, then the public preview returns exact400.'),
        ('H0593', tuple(normalized_case('mac-' + label) for label in ('non-hex', 'bad-hex-prefix')),
         'All six segments have accepted lengths; invalid hex reaches the int conversion catch, not the earlier count/length guard.'),
        ('R006-B01', tuple(normalized_case('accepted-' + label, 200)
                          for label in normal_good[:5] + ('lowercase', 'mixed-case')),
         'Canonical, hyphen, short octets, mixed separators, trimmed IP/MAC and actual AF hex letter case reach seen-device planning.'),
        ('R006-B02', duplicate_ips,
         'Duplicate canonical IP is refused independently of router ID; outer whitespace cannot bypass it.'),
        ('R006-B03', tuple(normalized_case('accepted-' + label, 200) for label in ('empty-name', 'blank-name')),
         'Empty and all-whitespace names are legitimate PREVIEW inputs; saved-row requirement is an apply guard.')):
        audits[ident] = Audit(normalization_sources, detail +
            ' Checks full DB/files/devices and foreign rows; only device reads permitted for successful previews. '
            'This is reference boundary evidence; an absent native route remains Go-unimplemented.', required)
    setup_all = tuple(('fixed-setup-' + state, 'R006/setup-' + state, 409 if state == 'active' else 200)
                      for state in ('active', 'completed', 'abandoned', 'foreign'))
    setup_reopen = (('fixed-setup-reopened', 'R006/setup-before-reopen', 200),
                    ('fixed-setup-reopened', 'R005/setup-reopened-valid-token', 409))
    for ident, required in (('H0035', setup_all + setup_reopen), ('R006-F17', setup_all),
                            ('R005-F14', setup_reopen)):
        audits[ident] = Audit(('backend/common/client_fixed_ip.py:61',
                              'backend/common/setup_flow.py:270', 'backend/routes/setup.py:72'),
            'Real DATA_DIR session JSON, canonical IDs/profile,0600 file/0700 directory. Active '
            'selected setup blocks preview before device I/O. Completed/abandoned stored flags and '
            'a foreign active session permit real device-backed preview. The apply case first '
            'obtains its OWN valid preview while abandoned, then public PATCH reopens the same '
            'session (explicit schema, no invented success property) and apply409 with no device '
            'I/O or mutation. Actual public setup create/abandon proves the base file contract; '
            'all seeded states pass the real public session reader unchanged. The completed fixture '
            'tests an archived reader/guard flag, not the config domain installation/finish workflow. '
            'No monkeypatch of profile_sessions or manually signed apply token.', required)
    source_guards = tuple(('fixed-source-router-' + label, 'R006/source-router-' + label + '-' + operation, 409)
                          for label in ('unknown', 'case', 'empty', 'null', 'false', 'true', 'number', 'array', 'object', 'missing')
                          for operation in ('add', 'delete'))
    source_guards += tuple(('fixed-source-missing-router', 'R006/source-missing-' + ident + '-' + operation, 409)
                           for ident in ('missing', 'r-ROS', ' r-ros ') for operation in ('add', 'delete'))
    unseen_guards = tuple(('fixed-source-unseen', 'R006/source-unseen-' + vendor + '-' + label, 409)
                          for vendor in ('routeros', 'ikuai') for label in ('mac', 'ip', 'empty'))
    unseen_guards += (('fixed-ip-guards', 'R006/wrong-vendor-source', 409),)
    seen_controls = tuple(('fixed-source-unseen', 'R006/source-seen-' + vendor, 200) for vendor in ('routeros', 'ikuai'))
    binding_guards = tuple(('fixed-source-binding-' + vendor + '-' + field,
                            'R006/source-binding-' + vendor + '-' + field, 409)
                           for vendor in ('routeros', 'ikuai') for field in ('router_id', 'mac'))
    binding_guards += tuple(('fixed-ip-guards', 'R006/delete-foreign-' + vendor, 409) for vendor in ('ros', 'ik'))
    recovered_controls = tuple(('fixed-source-recover-' + vendor, route + '/source-recover-owned-' + vendor, 200)
                               for vendor in ('routeros', 'ikuai') for route in ('R006', 'R005'))
    fixed_source_refs = ('backend/common/client_fixed_ip.py:91', 'backend/common/client_fixed_ip.py:102',
                         'backend/common/client_fixed_ip.py:109', 'backend/routers/store.py:18')
    for ident, required, detail in (
        ('H0040', source_guards, 'Both add/delete, absent/case/space router ID and every stored legacy type/missing field; exact409 before any driver I/O.'),
        ('R006-F18', source_guards, 'Repository accepts legacy router dictionaries; preview diagnoses missing/unsupported vendor before selecting a driver.'),
        ('H0042', unseen_guards + seen_controls + (('fixed-ip-stale', 'R005/stale-source', 409),),
         'Both vendors: mismatched MAC, mismatched IP, empty sightings, wrong vendor source, real positive sightings and a valid token whose source changes before apply.'),
        ('R006-F19', unseen_guards, 'Real source IP/MAC/empty-table checks on both vendors, including a MAC borrowed from a different router.'),
        ('H0043', binding_guards + recovered_controls,
         'All three OR operands: missing local metadata and foreign device marker; stored wrong router; stored wrong MAC. Both vendors recover an absent local binding from actual owned device state and delete only that row.'),
        ('R006-F22', binding_guards, 'Both vendors refuse foreign reservations, binding router mismatch and binding MAC mismatch; no write and no guessing ownership.'),
        ('R005-S07', recovered_controls, 'Actual preview/apply on each vendor with empty local binding map, persistent cf-client ownership, unchanged saved names/DNS/router and retained foreign reservation.'),
        ('R006-S04', tuple(('fixed-ip', 'R006/fixed-delete-' + label, 200) for label in ('dynamic', 'new', 'ikuai'))
         + tuple(item for item in recovered_controls if item[1].startswith('R006/')),
         'Both vendors delete tracked owned reservations; RouterOS covers converted/new forms, plus both missing-local-binding recovery previews.')):
        audits[ident] = Audit(fixed_source_refs, detail +
            ' Public Python process, real repository and stateful loopback peers only. Every write '
            'compares complete storage/files/device tables; fixtures never replace a driver or Python helper.', required)
    probe_edits = []
    for kind, labels in (('clear', ('type', 'endpoint', 'username', 'password', 'verify-tls')),
                          ('retain', ('name', 'same-type', 'same-username', 'same-endpoint', 'same-tls', 'noop',
                                      'password-empty', 'password-null', 'password-false', 'password-zero',
                                      'password-array', 'password-object'))):
        for label in labels:
            world = 'probe-edit-' + kind + '-' + label
            probe_edits += [(world, 'R017/routeros/edit-' + kind + '-' + label, 200),
                            (world, 'R010/probe-' + kind + '-' + label, 200)]
    audits['R010-B03'] = Audit(('backend/routes/routers.py:193', 'backend/routers/store.py:65'),
        'Each edit starts from its OWN actual successful public probe. All five connection members '
        'type/endpoint/username/password/verify_tls independently remove stored last_probe (not '
        'merely set null) and return public null. Name edit, same type, normalized same username/'
        'endpoint/TLS, empty no-op and six ignored empty/non-string passwords preserve the exact '
        'own response clock in DB and HTTP. Every edit commits one profile revision, keeps unrelated '
        'DNS/names/bindings and performs zero device I/O. Probe clock comparison is owned/content '
        'bound, never a last_probe field mask.', tuple(probe_edits))
    def probe_result(vendor, label, status=200, world='probe-errors'):
        return (world, 'R017/' + vendor + '/' + label, status)
    ros_good = tuple(probe_result('routeros', 'guard-version-' + label, world='probe-guard-controls')
                     for label in ('minimum', 'unicode', 'major', 'zeroes', 'rc'))
    ik_good = tuple(probe_result('ikuai', 'guard-version-' + label, world='probe-guard-controls')
                    for label in ('minimum', 'unicode'))
    ros_caps = tuple(probe_result('routeros', 'capability-' + cap + '-' + label)
                     for cap in ('routes', 'nat_bypass', 'address_sets')
                     for label in ('refused', 'missing', 'unauthenticated', 'server-error', 'not-json',
                                   'closed', 'bad-shape', 'bad-row', 'timeout'))
    late_caps = tuple(probe_result('routeros', 'guard-late-row-' + cap, world='probe-guard-controls')
                      for cap in ('routes', 'nat_bypass', 'address_sets'))
    ik_caps = tuple(probe_result('ikuai', 'capability-' + func + suffix)
                    for func in ('static_rt', 'nat_rule', 'route_object', 'stream_ipport')
                    for suffix in ('', '-http', '-not-json', '-closed', '-expired-twice', '-timeout'))
    bad_version_names = ('null', 'false', 'true', 'integer', 'number', 'empty-array', 'array', 'empty-object', 'object')
    bad_versions = {vendor: tuple(probe_result(vendor, 'guard-invalid-version-' + label, 502, 'probe-guard-controls')
                                  for label in bad_version_names) for vendor in ('routeros', 'ikuai')}
    ros_versions = tuple(probe_result('routeros', 'old-version-' + str(index), 502) for index in range(8))
    ros_versions += (probe_result('routeros', '204-is-empty-resource', 502),) + bad_versions['routeros']
    ik_versions = tuple(probe_result('ikuai', 'old-version-' + str(index), 502) for index in range(4))
    ik_versions += bad_versions['ikuai']
    malformed_lists = tuple(probe_result('routeros', 'capability-' + cap + '-' + label)
                            for cap in ('routes', 'nat_bypass', 'address_sets') for label in ('bad-shape', 'bad-row'))
    probe_sources = ('backend/routers/routeros.py:125', 'backend/routers/routeros.py:162',
                     'backend/routers/ikuai.py:299', 'backend/routes/routers.py:208')
    for ident, required, detail in (
        ('H0835', malformed_lists + late_caps + ros_good[:1],
         'Each capability list: wrong root and non-object row, including a later null after a valid dict. Empty lists are a genuine successful read-access control; errors omit only that capability.'),
        ('H0841', tuple(probe_result('routeros', 'resource-shape-' + str(index), 502) for index in range(8)) + ros_good,
         'Eight non-object system roots cover all JSON kinds, empty/nonempty list and both booleans, plus five valid resource dictionaries. Malformed resource is a failed persisted probe, not capability-only downgrade.'),
        ('H0842', ros_versions + ros_good,
         'No-match versus below-minimum operands, missing/empty/zero/version forms,204 empty resource and every wrong JSON field kind. Valid minimum/current/new major/zero padding/rc preserve real version/model and full capabilities.'),
        ('H0843', ros_caps + late_caps + ros_good,
         'All three capability paths:403/404/401/500, non-JSON, disconnect, two list-shape guards and actual10-second read timeout. Late bad rows prove complete iteration. Every failure remains HTTP200 with only that capability missing and a successful stored probe.'),
        ('H0241', ik_versions + ik_good,
         'iKuai version empty/old/other major and every JSON value kind refuse502; actual4.0.0 and Unicode current-version/model controls succeed with all four capabilities.'),
        ('H0242', ik_caps + ik_good,
         'All four iKuai capabilities: permission refusal,503, non-JSON, disconnect,1008 replies retrying the same request once and real10-second timeout. Each keeps probe.ok=true and only omits its capability.1008 fault replies do not claim firmware session acceptance.'),
        ('R017-S01', ros_good, 'Five real RouterOS resource/version/model controls with all three readable capabilities and exact owned persisted probe clock.'),
        ('R017-S02', ik_good + (probe_result('ikuai', 'session-relogin-once'),),
         'Actual iKuai4.x sysstat/current/minimum/Unicode model and all four readable capabilities, including real HTTP single re-login recovery.'),
        ('R017-S03', ros_caps + late_caps + ik_caps,
         'Both vendors and every capability path downgrade a child failure without losing healthy capabilities or failing the overall probe.'),
        ('R017-F13', ros_caps + late_caps + ik_caps,
         'Read/refusal/shape/transport/timeouts and bounded session retry at child capability scope, distinct from fatal system-information errors.'),
        ('R017-B04', ros_good + ik_good, 'Both version and model retain Chinese/emoji and real firmware version suffixes through HTTP and storage.')):
        audits[ident] = Audit(probe_sources, detail +
            ' Every probe writes one profile revision even on fatal502, reads only device state, '
            'keeps foreign storage and unrelated fake tables, and validates stored clock against its '
            'own actual response. Fakes prove protocol only; firmware permissions/acceptance remain L5.', required)
    ros_fatal = tuple(probe_result('routeros', 'http-' + str(status), 502) for status in (401, 403, 404))
    ros_fatal += tuple(probe_result('routeros', 'http-detail-' + str(status), 502) for status in (300, 400, 405, 429, 500, 502, 503))
    ros_fatal += tuple(probe_result('routeros', label, 502) for label in
                       ('permissions-detail', 'not-json-200', 'not-json-500', 'connection-closed',
                        'device-password-echo-is-not-global-token', 'global-token-whole-string-redacted', 'real-read-timeout'))
    ros_fatal += tuple(probe_result('routeros', 'resource-shape-' + str(index), 502) for index in range(8)) + ros_versions
    ik_fatal = tuple(probe_result('ikuai', 'http-' + str(status), 502)
                     for status in (401, 403, 404, 405, 408, 429, 502, 503, 504, 418, 500))
    ik_fatal += tuple(probe_result('ikuai', 'redirect-' + str(status) + '-' + label, 502)
                      for status in (301, 302, 303, 307, 308) for label in ('relative', 'other-origin'))
    ik_fatal += tuple(probe_result('ikuai', 'not-json-' + str(index), 502) for index in range(4))
    ik_fatal += tuple(probe_result('ikuai', 'login-shape-' + str(index), 502) for index in range(5))
    ik_fatal += tuple(probe_result('ikuai', label, 502) for label in
                      ('result-success-code-refusal', 'login-missing-code-old-result-refused', 'old-call-Data',
                       'old-call-Result', 'session-retry-still-expired', 'connection-closed', 'real-read-timeout'))
    ik_fatal += tuple(probe_result('ikuai', 'call-code-' + str(index), 502) for index in range(4)) + ik_versions
    tls_controls = tuple(probe_result(vendor, 'self-signed-' + label, status, 'probe-tls')
                         for vendor in ('routeros', 'ikuai')
                         for label, status in (('verified-0', 502), ('unverified-1', 200), ('verified-2', 502)))
    audits['H1296'] = Audit(probe_sources,
        'Every currently enumerated fatal RouterError family: system/auth/HTTP/redirect/non-JSON/root/'
        'version/session/connection/read timeout plus both verified/unverified TLS states and valid '
        'both-vendor controls. API502 still persists probe.ok=false/message/own fresh clock and bumps '
        'one profile revision, with no device write. Explicit stored-vs-HTTP secret redaction checks '
        'remain separate (do not mask messages). Child capability errors stay successful and have '
        'their own loop audits. Does not claim database write failures, unhandled non-RouterError '
        'crashes, real permission acceptance or all malformed nested peer schemas.',
        ros_fatal + ik_fatal + tls_controls + ros_good + ik_good)
    # DHCP evidence audits are enumerated independently of recipe tags. All
    # three public boundaries must observe each blocking vector; a plan alone
    # never proves confirmation refuses to write.
    def dhcp_rows(labels, healthy=()):
        result = []
        for label in labels:
            result += [('dhcp-evidence-guards', route + '/dhcp-evidence-' + label, status)
                       for route, status in (('R018', 200), ('R012', 200), ('R011', 409))]
        for label in healthy:
            result += [('dhcp-evidence-guards', route + '/dhcp-evidence-' + label, 200)
                       for route in ('R018', 'R012')]
        return tuple(result)
    dhcp_specs = {
        'H0849': (tuple('servers-' + label for label in ('missing', 'disabled-bool', 'disabled-upper')), ('healthy',),
                  'No active servers: empty table and boolean/text disabled. Healthy enabled LAN is the positive control.'),
        'H0850': (tuple('server-' + label for label in ('missing-name', 'empty-name', 'null-name', 'no-name-or-id', 'invalid-bool', 'invalid-text')), ('healthy',),
                  'Both name-missing and invalid disjuncts, id and unknown display fallbacks, boolean/text invalid and valid name.'),
        'H0851': (tuple('radius-' + str(index) for index in range(5)), tuple('radius-accepted-' + label for label in ('no', 'false', 'accounting', 'upper')),
                  'Unknown/true/numeric/null RADIUS evidence refuses; no/false/accounting and case normalization remain usable.'),
        'H0852': (('option6-layer-0',), ('healthy',), 'Server option6 overrides network DNS; ordinary server without overrides permits preview.'),
        'H0853': (('option6-layer-3',), ('healthy', 'matcher-ignored-disabled', 'matcher-ignored-disabled-server'),
                  'Active matcher option set blocks globally; disabled matcher and matcher attached to disabled server are ignored.'),
        'H0854': (tuple('network-id-' + label for label in ('empty', 'null', 'zero', 'missing', 'duplicate')), ('healthy',),
                  'Missing/falsy and duplicate IDs refuse before guessing target identity; valid unique ID remains usable.'),
        'H0855': (tuple('network-address-' + str(index) for index in range(7)), ('healthy', 'network-address-accepted-host-bits', 'network-address-accepted-netmask'),
                  'All invalid network value kinds and IPv6 refuse; host bits and dotted mask canonicalize to the actual LAN.'),
        'H0856': (tuple('network-address-' + str(index) for index in range(7)), ('healthy', 'network-address-accepted-host-bits', 'network-address-accepted-netmask'),
                  'The raised network-address error reaches report unknown and exact plan/apply refusal.'),
        'H0857': (tuple('network-' + member + '-' + kind for member in ('dynamic', 'invalid') for kind in ('bool', 'text')), ('healthy', 'network-flags-false'),
                  'Each dynamic/invalid operand independently blocks, using real bool and RouterOS text flags; explicit false flags permit preview.'),
        'H0858': (('option6-layer-1',), ('healthy',), 'Network-scoped custom option6 cannot be overwritten by changing only dns-server.'),
        'H0859': (tuple('lease-radius-' + label for label in ('invalid', 'null', 'empty', 'ipv6', 'pool')), ('lease-ignored-outside', 'lease-ignored-disabled', 'lease-ignored-disabled-server', 'lease-ignored-ordinary-dynamic'),
                  'Invalid/pool/null/IPv6 lease scope is conservative; known outside, disabled lease/server and ordinary dynamic lease do not block this LAN.'),
        'H0860': (tuple('lease-radius-' + label for label in ('invalid', 'null', 'empty', 'ipv6', 'pool', 'unspecified', 'inside')), ('healthy', 'lease-ignored-outside', 'lease-ignored-disabled', 'lease-ignored-disabled-server', 'lease-ignored-ordinary-dynamic'),
                  'Scoped and unknown RADIUS lease evidence blocks; every explicit exclusion remains usable.'),
        'H0861': (('option6-layer-2',), ('healthy', 'lease-ignored-outside', 'lease-ignored-disabled', 'lease-ignored-disabled-server', 'lease-ignored-ordinary-dynamic'),
                  'Lease custom option6 blocks and unselected/disabled/ordinary leases do not introduce a false network override.'),
        'H0862': (tuple('dns-invalid-' + str(index) for index in range(4)), tuple('dns-accepted-' + label for label in ('list', 'csv', 'filtered-list')),
                  'Bad IPv4 DNS in strings/lists and IPv6 refuse; actual string/list parsing retains valid IPv4 DNS and ignores empty/none elements.'),
        'H0863': (tuple('dns-invalid-' + str(index) for index in range(4)), tuple('dns-accepted-' + label for label in ('list', 'csv', 'filtered-list')),
                  'DNS-address exception reaches all three public boundaries, distinct from names-container shape refusal.'),
        'H0904': (tuple('names-' + str(position) + '-' + kind for position in range(9) for kind in ('null', 'bool', 'number', 'object', 'mixed-list')),
                  tuple('names-accepted-' + str(position) + '-' + kind for position in range(9) for kind in ('empty-list', 'ignored-names')),
                  'Every real names call position: server/network/lease direct and set references, matcher set, nested set options and DNS. Wrong JSON types and late mixed row refuse; empty/list-string filters succeed.'),
        'H0905': (tuple('dns-none-' + str(index) for index in range(7)), tuple('dns-none-accepted-' + label for label in ('bool-true', 'bool-false', 'true', 'false', 'yes', 'no')),
                  'Both accepted boolean values and four exact firmware strings; null/numbers/case/unknown/container values fail closed.'),
        'H0906': (tuple('set-' + label for label in ('missing', 'duplicate', 'no-options')), tuple('code-accepted-' + label for label in ('one', 'maximum', 'number-string', 'vendor')),
                  'Missing/multiple set matches and missing options separately block; valid lookup by .id with string-list options succeeds.'),
        'H0907': (('option-missing', 'option-duplicate'), tuple('code-accepted-' + label for label in ('one', 'maximum', 'number-string', 'vendor')),
                  'Zero/multiple option matches block; unique .id reference and repeated names are resolved without guessing.'),
        'H0908': (('code-oversized',), ('code-accepted-number-string',),
                  'Literal5000-digit decimal triggers real Python integer conversion limit inside this catch; ordinary decimal code succeeds. No exception helper is simulated.'),
        'H0909': (('option6-layer-0', 'option6-layer-1', 'option6-layer-2', 'option6-layer-3', 'code-six', 'code-padded-six'), tuple('code-accepted-' + label for label in ('one', 'maximum', 'number-string', 'vendor')),
                  'Option6 integer at all four layers plus decimal and padded-decimal strings; non-DNS standard/vendor-specific codes remain usable.'),
        'H0910': (tuple('code-' + label for label in ('null', 'false', 'true', 'zero', 'negative', 'too-large', 'float', 'array', 'object', 'whitespace', 'sign', 'unknown', 'oversized')),
                  tuple('code-accepted-' + label for label in ('one', 'maximum', 'number-string', 'vendor')),
                  'Unknown JSON types, bool-not-int, out-of-range and non-decimal/oversized strings; exact1/254 bounds, numeric string and vendor-specific43 controls.'),
    }
    for ident, (blocked, healthy, rationale) in dhcp_specs.items():
        audits[ident] = Audit(('backend/routers/routeros.py:295', 'backend/routers/routeros.py:760',
                              'backend/routers/dhcp_dns.py:107', 'backend/routes/routers.py:304'),
            rationale + ' Real HTTP report200 preserves unknown diagnostics, plan200 exposes exact blockers, '
            'confirmation409 refuses before fingerprint/device writes. Healthy controls have pending DNS and no '
            'plan blockers. All boundaries compare full storage/device state; only loopback synthetic evidence. '
            'Does not claim DHCP firmware acceptance or successful DNS write/readback.', dhcp_rows(blocked, healthy))
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
    proofs = reachability.evaluate(results)
    if verdicts.keys() & proofs.keys():
        raise ValueError('a source-unreachable guard cannot also claim executed variant coverage')
    verdicts.update(proofs)
    return verdicts
