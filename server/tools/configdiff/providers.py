"""Validate provider comment metadata and download names without losing YAML data."""
from datetime import datetime
import re
import time
from zoneinfo import ZoneInfo

from compare import Difference, differences, timestamp
from harness import DesignError
from subscriptions import capture_ids


def comments(raw, policy):
    lines = raw.decode('utf-8').splitlines()
    result, generated = [], 0
    for line in lines:
        if not line.startswith('#'):
            continue
        if line.startswith('# Generated: '):
            value = line.removeprefix('# Generated: ')
            # The Python format is local wall-clock ISO followed by literal Z.
            if not value.endswith('Z') or not timestamp(value[:-1], policy):
                raise ValueError('Generated comment is not current local-time ISO + literal Z')
            result.append('# Generated: ' + re.sub(r'\d', '0', value))
            generated += 1
        else:
            result.append(line)
    if generated != 1:
        raise ValueError('expected exactly one Generated comment')
    return result


def filename(reply, policy):
    value = reply.headers.get('content-disposition', '')
    match = re.fullmatch(r'inline; filename="proxies_sub-a_([0-9]{8}_[0-9]{6})\.yaml"', value)
    if not match:
        raise ValueError('provider filename shape or subscription identity')
    stamp = datetime.strptime(match[1], '%Y%m%d_%H%M%S').replace(tzinfo=ZoneInfo(policy.tz)).timestamp()
    if not policy.start - 2 <= stamp <= policy.end + 2:
        raise ValueError('provider filename timestamp window')
    return value


class Metadata:
    def __init__(self, pair, *, profile='default', method='GET', yaml=True, served=True):
        self.pair, self.profile, self.method, self.yaml, self.served = pair, profile, method, yaml, served

    def reference(self, reply):
        self.pair.policy.end = time.time()
        if not self.yaml:
            return
        try:
            filename(reply, self.pair.policy)
            if self.method != 'HEAD':
                comments(reply.body, self.pair.policy)
            if self.served:
                path = self.pair.dirs[0] / f'profiles/{self.profile}/providers/served/subscription-sub-a.yaml'
                comments(path.read_bytes(), self.pair.policy)
        except (ValueError, OSError) as error:
            raise DesignError('Python provider metadata fixture: ' + str(error)) from error

    def capture(self, replies, policy):
        out = capture_ids(self.pair)(replies, policy)
        if not self.yaml:
            return out
        names, body_comments = [], []
        for index, reply in enumerate(replies):
            if reply.status != 200:
                names.append(None)
                body_comments.append(None)
                continue
            try:
                names.append(filename(reply, policy))
                body_comments.append(comments(reply.body, policy) if self.method != 'HEAD' else [])
            except ValueError as error:
                if index == 0:
                    raise DesignError('Python provider metadata changed: ' + str(error)) from error
                if len(names) <= index:
                    names.append(None)
                observed = [line for line in reply.body.decode('utf-8', errors='replace').splitlines() if line.startswith('#')]
                if len(body_comments) <= index:
                    body_comments.append(observed)
                out.append(Difference('provider.metadata', 'invalid-Go-metadata', '<valid>',
                                      {'error': str(error), 'comments': observed}))
        if all(isinstance(value, str) for value in names):
            policy.bind(*names, r'inline; filename="proxies_sub-a_[0-9]{8}_[0-9]{6}\.yaml"', 'provider-filename')
        out += differences(*body_comments, 'provider.comments', policy)
        if self.served:
            for suffix in ('', '.bak'):
                paths = [directory / f'profiles/{self.profile}/providers/served/subscription-sub-a.yaml{suffix}' for directory in self.pair.dirs]
                if not any(path.exists() for path in paths):
                    continue
                metadata = []
                for index, path in enumerate(paths):
                    if index == 0 and suffix == '.bak' and not path.exists():
                        metadata.append(None)
                        continue  # An extra Go backup is a difference, not a bad Python fixture.
                    try:
                        metadata.append(comments(path.read_bytes(), policy))
                    except (OSError, ValueError) as error:
                        if index == 0:
                            raise DesignError('Python served provider lost metadata') from error
                        observed = [line for line in path.read_text(errors='replace').splitlines() if line.startswith('#')] if path.exists() else None
                        metadata.append({'invalid': str(error), 'comments': observed})
                out += differences(*metadata, 'provider.served-file' + suffix + '.comments', policy)
        return out
