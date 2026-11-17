"""Retained malformed text settings exercise live configuration pull failures."""
from harness import Pair
from sources import Source
from cases.config_backup import message
from cases.pull_routes import PULL_ROUTES, pull_path, SourceStable


def malformed_text(binary, recorder):
    for service in ('loon', 'surge', 'mosdns'):
        # The real Python importer accepts this retained nested value. Public
        # text-setting writes are tested separately and must not be patched.
        with Source(b'bad local settings fail without upstream access') as source, Pair(binary, recorder,
                options={'profile': {service: {'custom_config': 1}}}) as pair:
            pair.policy.internal_errors['body.message'] = ''
            for route, rid in PULL_ROUTES:
                if '/' + service not in route:
                    continue
                pair.request(rid + '-F10', route, pull_path(route), expect=500, writes=True,
                    interaction=SourceStable(pair, source), check=message("'int' object has no attribute 'strip'"),
                    variant='real-imported-non-text-' + service + '-custom-config')


GROUPS = {'config-pull-retained-non-text-settings-failure': malformed_text}
