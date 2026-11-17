"""Source-audited H labels for already enumerated public R requests.

One request can exercise both a route recipe and its named Python helper
branch. Labels do not manufacture another request or mark a comparison passed.
Unreachable/earlier-guard branches are deliberately absent.
"""

LINKS = {
    'R206-F20': ('POST /api/settings/web-port', ('H1112',)),
    'R016-F14': ('POST /api/backup/now', ('H030',)),
    'H497': ('POST /api/generate/oxidns', ('H023',)),
    'H498': ('POST /api/generate/oxidns', ('H023',)),
    'R180-F10': ('DELETE /api/rule-sets/<rule_set_id>', ('H374',)),
    'R184-F09': ('DELETE /api/rules/<rule_id>', ('H374',)),
    'R028-F10': ('POST /api/config/import', ('H845',)),
    'R028-F12': ('POST /api/config/import', ('H848',)),
    'R157-F19': ('POST /api/proxy-groups', ('H917',)),
    'R149-F19': ('POST /api/profiles/<profile_id>/proxy-groups', ('H917',)),
    'R159-F18': ('PUT /api/proxy-groups/<group_id>', ('H917',)),
    'R185-F20': ('PUT /api/rules/<rule_id>', ('H917',)),
    'R008-F15': ('GET /api/aggregations/<agg_id>/provider', ('D8',)),
    'R220-F18': ('GET /api/subscriptions/<sub_id>/proxies', ('D8',)),
    'R222-F13': ('GET /api/subscriptions/proxies', ('D8',)),
    'R189-F12': ('PUT /api/rules/dns', ('H909',)),
    'R189-F13': ('PUT /api/rules/dns', ('H912',)),
    'R189-F15': ('PUT /api/rules/dns', ('H913',)),
    'R185-F19': ('PUT /api/rules/<rule_id>', ('H913',)),
    'R181-F11': ('PUT /api/rule-sets/<rule_set_id>', ('H913',)),
    'R068-F08': ('POST /api/generate/mosdns', ('H014', 'H015')),
    'R131-F08': ('POST /api/profiles/<profile_id>/generate/mosdns', ('H014', 'H015')),
    'R062-B90': ('POST /api/generate/<service>/artifact', ('X01',)),
    'R126-B90': ('POST /api/profiles/<profile_id>/generate/<service>/artifact', ('X01',)),
    'R062-S90': ('POST /api/generate/<service>/artifact', ('G16',)),
    'R126-S90': ('POST /api/profiles/<profile_id>/generate/<service>/artifact', ('G16',)),
    'R062-F10': ('POST /api/generate/<service>/artifact', ('H025',)),
    'R126-F10': ('POST /api/profiles/<profile_id>/generate/<service>/artifact', ('H025',)),
    'R063-F09': ('GET /api/generate/artifacts/<preview_id>/<action>', ('H026',)),
    'R063-F11': ('GET /api/generate/artifacts/<preview_id>/<action>', ('H027', 'H029')),
    'R063-F12': ('GET /api/generate/artifacts/<preview_id>/<action>', ('H029',)),
    'R063-F13': ('GET /api/generate/artifacts/<preview_id>/<action>', ('H028', 'H029')),
    'R063-F14': ('GET /api/generate/artifacts/<preview_id>/<action>', ('H027', 'H029')),
    'R098-F12': ('POST /api/nodes/latency', ('H737',)),
    'R098-F17': ('POST /api/nodes/latency', ('H741',)),
    **{case: ('POST /api/rule-library/check-content', ('H149',)) for case in ('H146', 'H147', 'H148')},
    'H116': ('POST /api/settings/rule-fetch-proxy', ('H117',)),
    'R087-F11': ('GET /api/mosdns/rule-proxy', ('H121',)),
    'R138-F11': ('GET /api/profiles/<profile_id>/mosdns/rule-proxy', ('H121',)),
    **{case: ('POST /api/settings/web-port', ('H1088',)) for case in ('R206-F09', 'R206-F10', 'R206-F11', 'R206-F12')},
    'R206-F13': ('POST /api/settings/web-port', ('H1104',)),
    'R206-F16': ('POST /api/settings/web-port', ('H1109',)),
    'R206-F14': ('POST /api/settings/web-port', ('H1105',)),
    'R093-F10': ('DELETE /api/nodes/<node_id>', ('H796',)),
    'R093-F11': ('DELETE /api/nodes/<node_id>', ('H796',)),
    'R095-F14': ('POST /api/nodes/delete', ('H799',)),
    'R200-F09': ('POST /api/settings/dependencies/sub-store/update', ('H041',)),
    'R200-F10': ('POST /api/settings/dependencies/sub-store/update', ('H040',)),
    **{case: ('POST /api/generate/mosdns', ('H012',)) for case in ('H003', 'H004', 'H005', 'H006', 'H007', 'H008')},
    'H014': ('POST /api/generate/mosdns', ('H015',)),
    'R123-F08': ('POST /api/profiles/<profile_id>/clone', ('H770',)),
    'R116-F09': ('POST /api/profiles', ('H771',)),
    'R123-F09': ('POST /api/profiles/<profile_id>/clone', ('H771',)),
    'R116-F11': ('POST /api/profiles', ('H772',)),
    'R123-F11': ('POST /api/profiles/<profile_id>/clone', ('H772',)),
    'R155-F17': ('POST /api/profiles/import-client', ('H775',)),
    'R119-F08': ('PUT /api/profiles/<profile_id>', ('H777',)),
    'R119-F09': ('PUT /api/profiles/<profile_id>', ('H778',)),
    'R119-F10': ('PUT /api/profiles/<profile_id>', ('H778',)),
    'R119-F11': ('PUT /api/profiles/<profile_id>', ('H778',)),
    'R117-F08': ('DELETE /api/profiles/<profile_id>', ('H779',)),
    'R117-F09': ('DELETE /api/profiles/<profile_id>', ('H780',)),
    'R117-F10': ('DELETE /api/profiles/<profile_id>', ('H781',)),
    'R117-B01': ('DELETE /api/profiles/<profile_id>', ('H781',)),
    'R117-F11': ('DELETE /api/profiles/<profile_id>', ('H782',)),
    'R095-F12': ('POST /api/nodes/delete', ('H797',)),
    'R095-F13': ('POST /api/nodes/delete', ('H798',)),
    'R137-F09': ('POST /api/profiles/<profile_id>/import', ('H806',)),
    'R137-F10': ('POST /api/profiles/<profile_id>/import', ('H807',)),
    'R137-F11': ('POST /api/profiles/<profile_id>/import', ('H808',)),
    'R118-F09': ('GET /api/profiles/<profile_id>', ('H809',)),
}

VARIANT_LINKS = {
    (case, variant, route): labels
    for prefix, route in [('R087', 'GET /api/mosdns/rule-proxy'),
                          ('R138', 'GET /api/profiles/<profile_id>/mosdns/rule-proxy')]
    for case, variant, labels in [(prefix + '-F13', 'text/F13', ('H124', 'H145')),
                                 (prefix + '-B04', 'text/B04', ('H143',))]
}
VARIANT_LINKS.update({
    ('R016-F15', 'real-MKCOL-403-before-PUT', 'POST /api/backup/now'): ('H031',),
})
VARIANT_LINKS.update({
    ('G10', variant, 'POST /api/generate/oxidns'): labels
    for variant, labels in [('missing-resource/reject-package', ('H500', 'H023')),
                            ('runtime-overlap/reject-package', ('H501', 'H023'))]
})
VARIANT_LINKS.update({
    ('G13', age + '/' + selection + '/' + service + '/sub-a', 'GET /api/subscriptions/<sub_id>/proxies'): (helper,)
    for age in ('fresh', 'expired', 'interval-zero') for selection in ('filtered', 'all')
    for service, helper in [('surge', 'H175'), ('loon', 'H170')]
})
VARIANT_LINKS.update({
    (rid + '-' + case, variant, route): (helper,)
    for rid, route in [('R109', 'POST /api/oxidns/native-settings/apply'),
                       ('R142', 'POST /api/profiles/<profile_id>/oxidns/native-settings/apply')]
    for case, variants, helper in [
        ('F12', ('None', '[]', '1'), 'H453'), ('F13', ('[]', "{'lazy_cache': 1}"), 'H454'),
        ('F14', ("'lazy_cache'", '[1]', "['lazy_cache', 'lazy_cache']"), 'H455'),
        ('F14', ("['unknown']",), 'H456'), ('F15', ('',), 'H457'),
        ('B01', ('中文', 'emoji🧪'), 'H459'), ('F16', ('',), 'H461'), ('F18', ('',), 'H465')]
    for variant in variants
})

ROUTE_LINKS = {(case, route): ('H423',)
    for case in ('H416', 'H417', 'H418', 'H419', 'H420', 'H421', 'H422')
    for route in ('POST /api/oxidns/editor', 'POST /api/profiles/<profile_id>/oxidns/editor')}

VARIANT_LINKS.update({
    (rid + '-F15', variant, route): (helper,)
    for rid, route in [('R214', 'POST /api/subscriptions'), ('R217', 'PUT /api/subscriptions/<sub_id>')]
    for helper, variants in [('H1082', ('1', 'True', '[]', '{}')), ('H1083', ('regex-syntax',))]
    for variant in variants
})
VARIANT_LINKS.update({
    (rid + '-F16', variant, route): ('H1084',)
    for rid, route in [('R214', 'POST /api/subscriptions'), ('R217', 'PUT /api/subscriptions/<sub_id>')]
    for variant in ('-1', '59', '604801', '2592000', 'True', '60.0', "'60'", 'None')
})
VARIANT_LINKS.update({
    (rid + '-S04', 'real-403-then-last-valid-node-cache', route): ('H1071',)
    for rid, route in [('R220', 'GET /api/subscriptions/<sub_id>/proxies'),
                       ('R154', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies')]
})


def attach(results):
    for result in results:
        route, labels = LINKS.get(result['case'], ('', ()))
        labels = labels if result['route'] == route else ()
        labels += VARIANT_LINKS.get((result['case'], result.get('variant', ''), result['route']), ())
        labels += ROUTE_LINKS.get((result['case'], result['route']), ())
        if labels:
            result['helper_cases'] = sorted(set(labels))
    return results


def auxiliary_cases(results):
    return ({item['case'] for item in results if not item['case'].startswith('R')}
            | {case for item in results for case in item.get('helper_cases', [])})
