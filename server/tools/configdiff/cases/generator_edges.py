"""Generator failures and offline cache use at the public HTTP boundary."""
import io
import os
import re
import zipfile

from compare import differences
from downloads import capture_headers, validate_headers
from harness import Pair
from inventory import routes
from sources import Source
from subscriptions import Exchange
from cases.generate import FILENAMES
from cases.profile_generators import GeneratedOnly
from cases.rule_io import write_cache
from cases.rule_library import DOMAIN
from cases.setup import Stable
from cases.subscriptions import require


def generators():
    for route, rid in routes().items():
        match = re.fullmatch(r'POST /api/(?:profiles/<profile_id>/)?generate/(mihomo|surge|loon|mosdns|oxidns)(/preview)?', route)
        if match:
            yield rid, route, route.split(' ', 1)[1].replace('<profile_id>', 'default'), match[1], bool(match[2])


class ReadonlyOutput(Stable):
    def __init__(self, pair):
        super().__init__(pair)
        self.observed = [None, None]

    def before(self, index):
        self.folder = self.pair.dirs[index] / 'profiles/default/generated'
        self.folder.mkdir(parents=True, exist_ok=True)
        self.folder.chmod(0o500)
        self.active = not os.access(self.folder, os.W_OK)
        if index == 0:
            require(self.active, 'ordinary process genuinely cannot write into generated directory')
        super().before(index)

    def after(self, index, reply):
        try:
            retained = self.folder.is_dir() and self.folder.stat().st_mode & 0o777 == 0o500
        except OSError:
            retained = False
        finally:
            if self.folder.exists():
                self.folder.chmod(0o700)
        super().after(index, reply)
        self.observed[index] = {'real_fault_active': self.active, 'readonly_mode_retained': retained}
        if index == 0:
            require(all(self.observed[0].values()), 'failed generated write leaves real directory permissions intact')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'generator.write-permission', policy)


def failures(binary, recorder):
    invalid_policy = {'profile': {'rule_configs': [{'id': 'missing-policy-rule', 'itemType': 'rule', 'rule_type': 'DOMAIN',
                       'value': 'example.com', 'policy': 'synthetic-missing-policy', 'enabled': True}]}}
    with Pair(binary, recorder, options=invalid_policy) as pair:
        for rid, route, path, service, preview in generators():
            if service not in ('mihomo', 'surge', 'loon'):
                continue
            pair.request(rid + '-F08', route, path, body='{}', expect=400, writes=True,
                         interaction=Stable(pair), variant='actual-enabled-rule-missing-strategy',
                         check=lambda reply: require(reply.json()['message'] ==
                             "规则 missing-policy-rule 引用了不存在或未启用的策略 'synthetic-missing-policy'，请修改策略引用",
                             'converter reports the exact missing strategy and offending enabled rule'))
    with Pair(binary, recorder) as pair:
        for rid, route, path, service, preview in generators():
            if not preview and service not in ('oxidns', 'mosdns'):
                continue
            pair.policy.internal_errors['body.message'] = ''
            if service == 'oxidns' and not preview:
                pair.policy.internal_errors['body.message'] = '生成 OxiDNS 配置失败：'
            if service == 'mosdns' and not preview:
                pair.policy.internal_errors['body.message'] = '生成 MosDNS 配置失败：'
            case = 'F08' if service == 'mosdns' and preview else 'F09'
            pair.request(rid + '-' + case, route, path, body='[1]', expect=500, writes=True,
                         interaction=Stable(pair), variant='non-object-generator-input')
    for rid, route, path, service, preview in generators():
        if service not in ('mihomo', 'surge', 'loon') or preview:
            continue
        with Pair(binary, recorder) as pair:
            pair.policy.internal_errors['body.message'] = ''
            try:
                pair.request(rid + '-F11', route, path, body='{}', expect=500, writes=True,
                             interaction=ReadonlyOutput(pair), variant='real-generated-directory-EACCES')
            finally:
                for directory in pair.dirs:
                    folder = directory / 'profiles/default/generated'
                    if folder.exists():
                        folder.chmod(0o700)
    with Pair(binary, recorder, options={'profile': {'oxidns': {'config_mode': 'native', 'native_config': 'plugins: [\n'}}}) as pair:
        for rid, route, path, service, preview in generators():
            if service == 'oxidns':
                pair.request(rid + '-F08', route, path, body='{}', expect=400, writes=True,
                             interaction=Stable(pair), variant='actual-invalid-native-YAML')


def cached(binary, recorder):
    cached_bytes = b'+.offline-cache.example\n'
    with Source(b'must never be read') as source:
        source.status = 503
        declaration = {**DOMAIN, 'source_type': 'url', 'url': source.url + '/unavailable-rules'}
        declaration.pop('content')
        profile = {'rule_configs': [{'id': 'offline-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True}],
                   'mosdns': {'direct_rulesets': ['offline-rule']}, 'oxidns': {'direct_rulesets': ['offline-rule']}}
        options = {'shared': {'rule_library': [declaration]}, 'profile': profile}
        for rid, route, path, service, preview in generators():
            if preview:
                continue
            with Pair(binary, recorder, options=options) as pair:
                write_cache(pair, declaration, cached_bytes)
                name = {'mihomo': 'config.yaml', 'surge': 'config.conf', 'loon': 'loon.lcf'}.get(service, '')
                def check(reply):
                    require(not validate_headers(reply, pair.policy.start, pair.policy.end), 'normal attachment header contract remains intact offline')
                    if service in ('mosdns', 'oxidns'):
                        with zipfile.ZipFile(io.BytesIO(reply.body)) as archive:
                            require(any(b'offline-cache.example' in archive.read(member) for member in archive.namelist()
                                        if member != 'config.yaml' and not member.endswith('/')),
                                    'complete DNS archive includes actual cached converted rule bytes')
                pair.request(rid + '-S04', route, path, body='{}', expect=200, writes=True,
                             interaction=GeneratedOnly(pair, source, 'default', name), capture=capture_headers,
                             filename=FILENAMES[service], check=check, variant='real-valid-cache-with-offline-503-source')


GROUPS = {'generator-real-failures': failures, 'generator-offline-valid-cache': cached}
