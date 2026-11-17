"""Source-audited H labels for already enumerated public R requests.

One request can exercise both a route recipe and its named Python helper
branch. Labels do not manufacture another request or mark a comparison passed.
Unreachable/earlier-guard branches are deliberately absent.
"""

LINKS = {
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


def attach(results):
    for result in results:
        route, labels = LINKS.get(result['case'], ('', ()))
        if result['route'] == route:
            result['helper_cases'] = list(labels)
    return results


def auxiliary_cases(results):
    return ({item['case'] for item in results if not item['case'].startswith('R')}
            | {case for item in results for case in item.get('helper_cases', [])})
