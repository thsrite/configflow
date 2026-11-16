"""R074–R076 fixed bytes, parser boundaries, fallback and OS failures."""
from pathlib import Path
import os
from urllib.parse import quote

from compare import Difference, differences
from harness import Pair, DesignError


def bind_path(pair):
    pair.policy.bind(*[str(d / 'app.log') for d in pair.dirs],
                     r'/tmp/configdiff-[^/]+/(python|go)/app\.log', 'fixed-log-path')


def fixed_logs(binary, recorder):
    lines = [f'2026-01-01 00:00:00,000 - synthetic - INFO - line {i:05d}\n' for i in range(10005)]
    lines[-3:] = ['2026-01-01 00:00:00,000 - synthetic - WARNING - Synthetic 中文🧪\n',
                  '2026-01-01 00:00:00,000 - synthetic - ERROR - Special \\ " ,\n',
                  '2026-01-01 00:00:00,000 - synthetic - INFO - last\n']
    content = ''.join(lines).encode() + b'invalid UTF8 \xff ignored\n'
    for tz in ('UTC', 'Asia/Shanghai'):
        with Pair(binary, recorder, tz=tz, options={'isolated_cwd': True}) as pair:
            bind_path(pair)
            for directory in pair.dirs:
                (directory / 'app.log').write_bytes(content)
                os.utime(directory / 'app.log', (1767225600, 1767225600))
            pair.policy.times.add('body.created')
            pair.request('R075-S01', 'GET /api/logs/info', expect=200, variant=tz)
            pair.request('R075-B90', 'GET /api/logs/info', expect=200, variant=tz)
            for case, query in [('S01', ''), ('S02', '?search=synthetic%20%E4%B8%AD%E6%96%87'),
                                 ('S03', '?level=warning'), ('B01', '?lines=0'), ('B02', '?lines=-1'),
                                 ('B03', '?lines=10001'), ('B04', '?lines=abc'), ('B05', '?lines=3')]:
                pair.request('R076-' + case, 'GET /api/logs/tail', '/api/logs/tail' + query,
                             expect=200, variant=tz)
            for raw in (' 5', '+5', '5_0', '٥', '99999999999999999999'):
                pair.request('R076-B90', 'GET /api/logs/tail', '/api/logs/tail?lines=' + quote(raw, safe=''),
                             expect=200, variant=tz + '/' + raw)
            error = pair.policy.internal_errors
            error['body.error'] = ''
            pair.request('R076-F08', 'GET /api/logs/tail', '/api/logs/tail?level=%5B', expect=500, variant=tz)
            del error['body.error']
            def empty(_replies, policy):
                actual = [(d / 'app.log').read_bytes() for d in pair.dirs]
                if actual[0]:
                    raise DesignError('Python did not clear fixed log bytes')
                return differences(*actual, path='log-file.bytes', policy=policy)
            pair.request('R074-S01', 'POST /api/logs/clear', expect=200, variant=tz, writes=True, capture=empty)
            pair.request('R074-B01', 'POST /api/logs/clear', expect=200, variant=tz, writes=True, capture=empty)
            pair.policy.times.add('body.modified')
            pair.request('R075-B01', 'GET /api/logs/info', expect=200, variant=tz)
            for directory in pair.dirs:
                (directory / 'app.log').unlink()
            pair.request('R075-S02', 'GET /api/logs/info', expect=200, variant=tz,
                         check=lambda r: require(r.json().get('exists') is False, 'missing log should not pick uncontrolled fallback'))
            # Missing-file messages contain a validated per-process absolute path.
            messages = ['日志文件不存在: ' + str(d / 'app.log') for d in pair.dirs]
            pair.policy.bind(*messages, r'日志文件不存在: /tmp/configdiff-[^/]+/(python|go)/app\.log', 'missing-log-message')
            pair.request('R076-S04', 'GET /api/logs/tail', expect=200, variant=tz)
            pair.request('R074-S02', 'POST /api/logs/clear', expect=200, variant=tz, writes=True)
            for directory in pair.dirs:
                (directory / 'app.log').mkdir()
            pair.policy.internal_errors['body.error'] = ''
            pair.request('R076-F09', 'GET /api/logs/tail', expect=500, variant=tz, writes=True)
            pair.request('R074-F08', 'POST /api/logs/clear', expect=500, variant=tz, writes=True)


def require(value, message):
    if not value:
        raise DesignError(message)


def fallback(binary, recorder):
    with Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        for directory in pair.dirs:
            (directory / 'app.log').unlink()
        fallback = pair.root / 'app.log'
        fallback.write_text('synthetic cwd fallback\n')
        pair.policy.times.update({'body.modified', 'body.created'})
        pair.request('R075-B92', 'GET /api/logs/info', expect=200)
        pair.request('R076-B92', 'GET /api/logs/tail', expect=200)
        pair.request('R074-B92', 'POST /api/logs/clear', expect=200)
        if fallback.read_bytes():
            raise DesignError('CWD fallback clear did not target the observed file')


GROUPS = {'logs-fixed': fixed_logs, 'logs-fallback': fallback}
