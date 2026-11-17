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


def filename(reply, policy, subscription_id='sub-a'):
    value = reply.headers.get('content-disposition', '')
    match = re.fullmatch(r'inline; filename="proxies_' + re.escape(subscription_id) + r'_([0-9]{8}_[0-9]{6})\.yaml"', value)
    if not match:
        raise ValueError('provider filename shape or subscription identity')
    stamp = datetime.strptime(match[1], '%Y%m%d_%H%M%S').replace(tzinfo=ZoneInfo(policy.tz)).timestamp()
    if not policy.start - 2 <= stamp <= policy.end + 2:
        raise ValueError('provider filename timestamp window')
    return value


def failure_comments(rows, status):
    """O6 permits this proven native English diagnostic suffix to differ."""
    original = '# Source: from local cache (fetch error: request_failed (exception_type=SubscriptionFetchError))'
    suffixed = original[:-2] + ' status=' + str(status) + '))'
    return [original if line == suffixed else line for line in rows]


class Metadata:
    def __init__(self, pair, *, profile='default', method='GET', yaml=True, served=True, subscription_id='sub-a', fetch_error_status=None):
        self.pair, self.profile, self.method, self.yaml, self.served = pair, profile, method, yaml, served
        self.subscription_id = subscription_id
        self.fetch_error_status = fetch_error_status

    def comments(self, raw, policy):
        rows = comments(raw, policy)
        return failure_comments(rows, self.fetch_error_status) if self.fetch_error_status is not None else rows

    def reference(self, reply):
        self.pair.policy.end = time.time()
        if not self.yaml:
            return
        try:
            filename(reply, self.pair.policy, self.subscription_id)
            if self.method != 'HEAD':
                comments(reply.body, self.pair.policy)
            if self.served:
                path = self.pair.dirs[0] / f'profiles/{self.profile}/providers/served/subscription-{self.subscription_id}.yaml'
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
                names.append(filename(reply, policy, self.subscription_id))
                body_comments.append(self.comments(reply.body, policy) if self.method != 'HEAD' else [])
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
            policy.bind(*names, r'inline; filename="proxies_' + re.escape(self.subscription_id) + r'_[0-9]{8}_[0-9]{6}\.yaml"', 'provider-filename')
        out += differences(*body_comments, 'provider.comments', policy)
        if self.served:
            for suffix in ('', '.bak'):
                paths = [directory / f'profiles/{self.profile}/providers/served/subscription-{self.subscription_id}.yaml{suffix}' for directory in self.pair.dirs]
                if not any(path.exists() for path in paths):
                    continue
                metadata = []
                for index, path in enumerate(paths):
                    if index == 0 and suffix == '.bak' and not path.exists():
                        metadata.append(None)
                        continue  # An extra Go backup is a difference, not a bad Python fixture.
                    try:
                        metadata.append(self.comments(path.read_bytes(), policy))
                    except (OSError, ValueError) as error:
                        if index == 0:
                            raise DesignError('Python served provider lost metadata') from error
                        observed = [line for line in path.read_text(errors='replace').splitlines() if line.startswith('#')] if path.exists() else None
                        metadata.append({'invalid': str(error), 'comments': observed})
                out += differences(*metadata, 'provider.served-file' + suffix + '.comments', policy)
        return out


class MergedMetadata:
    def __init__(self, pair, details, *, method='GET'):
        self.pair, self.details, self.method = pair, details, method

    def parse(self, raw, policy):
        result = comments(raw, policy)
        for name, count in self.details:
            prefix = f'#   - {name}: {count} proxies (updated: '
            found = [index for index, line in enumerate(result) if line.startswith(prefix) and line.endswith(')')]
            if len(found) != 1:
                raise ValueError('merged subscription detail name/count missing or duplicated')
            index = found[0]
            value = result[index][len(prefix):-1]
            if not timestamp(value, policy):
                raise ValueError('merged detail update is not current local wall time')
            result[index] = prefix + re.sub(r'\d', '0', value) + ')'
        if '# Subscriptions: ' + str(len(self.details)) not in result:
            raise ValueError('merged subscription count comment')
        if '# Total Proxies: ' + str(sum(count for _, count in self.details)) not in result:
            raise ValueError('merged proxy count comment')
        return result

    def name(self, reply, policy):
        value = reply.headers.get('content-disposition', '')
        match = re.fullmatch(r'inline; filename="proxies_([0-9]{8}_[0-9]{6})\.yaml"', value)
        if not match:
            raise ValueError('merged provider filename shape')
        stamp = datetime.strptime(match[1], '%Y%m%d_%H%M%S').replace(tzinfo=ZoneInfo(policy.tz)).timestamp()
        if not policy.start - 2 <= stamp <= policy.end + 2:
            raise ValueError('merged filename time window')
        return value

    def reference(self, reply):
        self.pair.policy.end = time.time()
        try:
            self.name(reply, self.pair.policy)
            if self.method != 'HEAD':
                self.parse(reply.body, self.pair.policy)
        except ValueError as error:
            raise DesignError('Python merged metadata fixture: ' + str(error)) from error

    def capture(self, replies, policy):
        out, names, metadata = capture_ids(self.pair)(replies, policy), [], []
        for index, reply in enumerate(replies):
            if reply.status != 200:
                names.append(None)
                metadata.append(None)
                continue
            try:
                names.append(self.name(reply, policy))
                metadata.append(self.parse(reply.body, policy) if self.method != 'HEAD' else [])
            except ValueError as error:
                if index == 0:
                    raise DesignError('Python merged metadata changed: ' + str(error)) from error
                if len(names) <= index:
                    names.append(None)
                observed = [line for line in reply.body.decode('utf-8', errors='replace').splitlines() if line.startswith('#')]
                metadata.append(observed)
                out.append(Difference('merged.metadata', 'invalid-Go-metadata', '<valid>', {'error': str(error), 'comments': observed}))
        if all(isinstance(value, str) for value in names):
            policy.bind(*names, r'inline; filename="proxies_[0-9]{8}_[0-9]{6}\.yaml"', 'merged-provider-filename')
        return out + differences(*metadata, 'merged.comments', policy)
