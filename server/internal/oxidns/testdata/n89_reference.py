"""Real Python business oracle with native-reader input projected by Go's facade.

No Python loader or business function is patched. All functions run normally.
"""
import json
import sys
from c22_record import conv
from backend.common import oxidns_settings as st, oxidns_native as nv
from backend.common import oxidns_common as cm, oxidns_common_routing as rt
from backend.common import oxidns_editor as ed, oxidns_references as rf
from backend.converters import oxidns as cv


def call(name, a):
    if name == 'normalize':
        return st.normalize_oxidns_settings(a[0], validate_native_arguments=a[1] if len(a) > 1 else True)
    if name == 'native_document':
        sources = {} if len(a) > 2 and a[2] else None
        document = nv.native_document(a[0], validate_arguments=a[1] if len(a) > 1 else True, sources=sources)
        return {'document': document, 'sources': sources}
    if name == 'apply_routing':
        notices = a[6] if len(a) > 6 else None
        document, settings = rt.apply_routing(*a[:5], before_config=a[5] if len(a) > 5 else None, notices=notices)
        return [document, settings, notices]
    functions = {
        'load_native_yaml': nv.load_native_yaml,
        'generate': cv.generate_oxidns_config,
        'downloads': cv.get_oxidns_ruleset_downloads,
        'custom_files': cv.get_oxidns_custom_files,
        'unavailable_rulesets': cv.unavailable_rulesets,
        'resource_references': nv.resource_references,
        'validate_resource_files': nv.validate_resource_files,
        'read_common_settings': cm.read_common_settings,
        'apply_common_settings': cm.apply_common_settings,
        'read_routing': rt.read_routing,
        'resource_downloads': rt.resource_downloads,
        'prepare_editor': ed.prepare_editor,
    }
    return functions[name](*a)


for line in sys.stdin:
    request = json.loads(line)
    args = request['args']
    answer = {'fn': request['fn'], 'args': args}
    try:
        answer['out'] = conv(call(request['fn'], args))
    except Exception as error:
        answer['err'] = [type(error).__name__, str(error)]
        for field in ('issues', 'references'):
            if hasattr(error, field):
                answer[field] = conv(getattr(error, field))
    answer['args_after'] = conv(args)
    print(json.dumps(answer, ensure_ascii=False, sort_keys=True), flush=True)
