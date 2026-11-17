"""X07 real 10 MiB/five-backup rotation driven only by public writes."""
from pathlib import Path
import tempfile

from compare import database, differences
from harness import Pair, json_body
from cases.pull_logging import events
from cases.subscriptions import require

MARKER = 'Updated server domain to '


def rotation_snapshot(directory):
    """Retain exact archive names, modes, event order, levels and messages."""
    return {path.name: {'mode': path.stat().st_mode & 0o777,
                       'events': events(path.read_bytes(), MARKER),
                       'unrecognised_lines': sum(MARKER not in line for line in path.read_text().splitlines())}
            for path in sorted(Path(directory).glob('app.log*')) if path.is_file()}


def rotation_errors(observed, domains):
    expected_names = {'app.log', *('app.log.' + str(i) for i in range(1, 6))}
    expected = {name: {'mode': observed.get(name, {}).get('mode'),
        'events': [{'level': 'INFO', 'message': MARKER + domains[-1 if name == 'app.log' else -1 - int(name.split('.')[-1])]}],
        'unrecognised_lines': 0} for name in expected_names}
    return differences(expected, observed, 'log.rotation.exact-retained-events')


def rotation(binary, recorder):
    # Seven >5 MiB records cannot share a 10 MiB segment. The seventh must
    # discard the oldest record and retain exactly the newest six segments.
    # No external prefill, replacement logger, shortened limit or forced roll.
    disk = Path.home() / '.cache/configdiff-large'
    disk.mkdir(parents=True, exist_ok=True, mode=0o700)
    domains = ['http://rotation-' + str(i) + '.example/' + 'x' * (6 * 1024 * 1024) for i in range(7)]
    with tempfile.TemporaryDirectory(prefix='log-rotation-', dir=disk) as parent, Pair(binary, recorder,
            options={'temporary_root': parent}) as pair:
        for i, value in enumerate(domains):
            def saved(reply):
                require(reply.json() == {'success': True, 'server_domain': value}
                    and database(pair.dirs[0])['fields']['system//system_config']['value']['server_domain'] == value,
                    'actual public setting and durable SQL preserve the complete rotation-driving value')
            pair.request('X07', 'POST /api/server-domain', body=json_body({'new_domain': value}),
                expect=200, writes=True, check=saved, variant='real-rotation-public-write/' + str(i), timeout=60)
        observed = [rotation_snapshot(directory) for directory in pair.dirs]
        source_errors = rotation_errors(observed[0], domains)
        require(not source_errors, 'unchanged Python 10 MiB/five-backup logger really evicts exactly the oldest event: '
            + repr([(item.path, item.kind) for item in source_errors[:5]]))
        available = pair.route_available('POST /api/server-domain') and not pair.unimplemented_mutations
        diffs = differences(*observed, 'log.rotation.full-event-segments') if available else []
        recorder.record('X07', 'real-10MiB-five-backup-rotation-after-seven-large-public-writes',
            'POST /api/server-domain', 'X07', 'go-unimplemented-dependency' if not available else 'different' if diffs else 'compared', diffs,
            python_status=200, go_status=200 if available else None, segment_limit=10 * 1024 * 1024, backups=5,
            source_segment_oracle=True)


GROUPS = {'logs-real-public-write-driven-10MiB-five-backup-rotation': rotation}
