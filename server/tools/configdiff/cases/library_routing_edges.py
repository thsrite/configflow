"""Non-strict native rebuild warnings and real invalid HEAD address handling."""

from compare import database
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from cases.rule_library import DOMAIN, library
from cases.rules import options
from cases.helpers import deny_writes, exception_policy
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require


def native(binary, recorder):
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: []\n'}
    with Pair(binary, recorder, options=opts) as pair:
        def warning(reply):
            fields = database(pair.dirs[0])['fields']
            require(library(pair)[0]['enabled'] is False, 'non-strict native rebuild still commits resource disablement')
            require(fields['profile/default/oxidns']['value']['native_config'] == 'plugins: []\n',
                    'unsupported handwritten native YAML remains byte-identical')
            values = reply.json()['warnings']
            require(len(values) == 1 and values[0]['profile'] == 'default' and values[0]['message'],
                    'non-strict native rebuild reports a real profile-qualified warning rather than failing silently')
        pair.request('R167-F15', 'PUT /api/rule-library/<rule_id>', '/api/rule-library/lib-a',
                     body=json_body({**DOMAIN, 'enabled': False}), expect=200, writes=True,
                     check=warning, variant='source-correction-non-strict-native-rebuild-warns-and-commits')


def native_two_sources(binary, recorder):
    # Retain a second real ruleset download. Disabling the last one produces
    # an empty download plugin rejected by Python's schema, a warning branch
    # rather than evidence of a successful native rebuild.
    opts = options()
    opts['shared']['rule_library'].append({**DOMAIN, 'id': 'lib-b', 'name': '保留来源', 'content': '+.kept.example\n'})
    opts['profile']['rule_configs'].append({'id': 'rs-b', 'itemType': 'ruleset', 'library_rule_id': 'lib-b',
                                         'policy': 'DIRECT', 'enabled': True})
    opts['profile']['oxidns'] = {'direct_rulesets': ['rs-a', 'rs-b']}
    opts['x01_native'] = True
    with Pair(binary, recorder, options=opts) as pair:
        previous = database(pair.dirs[0])['fields']['profile/default/oxidns']['value']['native_config']
        compositions = database(pair.dirs[0])['fields']['profile/default/rule_configs']['value']
        for enabled in (False, True):
            def check(reply):
                nonlocal previous
                state = database(pair.dirs[0])['fields']
                current = state['profile/default/oxidns']['value']['native_config']
                require(current != previous and reply.json()['warnings'] == [],
                        'two-source fixture actually rebuilds native providers without a schema warning: ' + repr(reply.json()))
                require(library(pair) == [{**DOMAIN, 'enabled': enabled}, opts['shared']['rule_library'][1]]
                    and state['profile/default/rule_configs']['value'] == compositions,
                    'only selected shared enablement changes; second source and all composition declarations survive')
                previous = current
            pair.request('R167-S04', 'PUT /api/rule-library/<rule_id>', '/api/rule-library/lib-a',
                         body=json_body({**DOMAIN, 'enabled': enabled}), expect=200, writes=True,
                         check=check, variant='two-real-native-downloads-selected-source-enabled-' + str(enabled))


def invalid_head(binary, recorder):
    bad = {**DOMAIN, 'source_type': 'url', 'url': 'ftp://synthetic.invalid/rules'}
    bad.pop('content')
    with Source(b'unused') as source, Pair(binary, recorder, options={'shared': {'rule_library': [bad]}}) as pair:
        def check(reply):
            require(reply.json() == {'success': True, 'results': [{'id': bad['id'], 'name': bad['name'],
                        'url': bad['url'], 'available': False, 'error': 'exception_type=ValueError'}],
                        'failed_count': 1, 'total_count': 1, 'skipped_count': 0},
                    'invalid HEAD scheme becomes a precise per-item failure without echoing exception URLs: ' + repr(reply.json()))
            require(library(pair) == [{**bad, 'enabled': False}], 'only failing source enablement is changed')
        pair.request('R176-F08', 'POST /api/rule-library/test', body='{}', expect=200, writes=True,
                     interaction=Exchange(pair, source, expected=0), check=check,
                     variant='actual-invalid-scheme-HEAD-exception')


def failed_save(binary, recorder):
    with Pair(binary, recorder, options={'shared': {'rule_library': [DOMAIN]}}) as pair:
        with deny_writes(pair):
            pair.request('R176-F10', 'POST /api/rule-library/test', body='{}', expect=500, writes=True,
                         interaction=Stable(pair), policy=exception_policy(pair),
                         check=message('exception_type=ConfigStorageError'),
                         variant='real-SQLite-trigger-rejects-bulk-enabled-save')



GROUPS = {'rule-library-native-warning-commit': native, 'rule-library-real-HEAD-exception': invalid_head,
          'rule-library-bulk-enabled-save-failure': failed_save,
          'rule-library-two-source-native-enable-rebuild': native_two_sources}
