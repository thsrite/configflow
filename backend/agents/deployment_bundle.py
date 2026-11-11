"""Build self-contained, byte-preserving Agent deployment archives.

All callbacks are resolved from the captured profile, never from a second HTTP
request to ConfigFlow. A missing resource aborts publication instead of leaving
an Agent to download a partial configuration.
"""
from collections import OrderedDict
from concurrent.futures import Future, ThreadPoolExecutor, as_completed
from copy import deepcopy
import hashlib
import gzip
import io
import ipaddress
import json
from pathlib import PurePosixPath
import re
import tarfile
from threading import Lock
from urllib.parse import parse_qs, unquote, urlsplit
import uuid

import requests
import yaml

from backend.agents.diagnostics import failure_text
from backend.agents.deployment_locations import DeploymentPreparationError, preparation_context, source_error
from backend.converters.common import ruleset_format
from backend.converters.mosdns import RULE_PARTS, domain_pattern_for_mosdns, empty_rule_part, parse_clash_rule_value
from backend.utils.rule_fetch import (
    RuleFetchError, github_download_candidates, is_github_url, is_internal_rule_url, request_rule,
)
from backend.utils.logger import get_logger
from backend.utils.ruleset_content import (
    ResultCache, RemoteRuleValidationError, _has_yaml_payload, content_digest, normalize_mihomo_ruleset, yaml_safe_load,
)
from backend.utils.remote_rule_sources import resolve_remote_rule_source

logger = get_logger(__name__)

MAX_FILE_BYTES = 64 * 1024 * 1024
MAX_TOTAL_BYTES = 256 * 1024 * 1024
MAX_FILES = 4096
MAX_RAW_CACHE_BYTES = 16 * 1024 * 1024
MAX_RAW_CACHE_ENTRIES = 128


class RuleDownloadError(DeploymentPreparationError):
    """A rule source could not be fetched before contacting the Agent.

    remedy is set when the panel's GitHub mirror or download proxy may fix it.
    """

    def __init__(self, message, *, remedy=None):
        super().__init__(message)
        self.remedy = remedy


def _download_remedy(url, exc, config):
    # HTTP status errors and internal callbacks are not network reachability
    # problems; another route would return the same answer.
    last = exc.last_error if isinstance(exc, RuleFetchError) else exc
    if not isinstance(last, requests.RequestException) or isinstance(last, requests.exceptions.HTTPError):
        return None
    if is_internal_rule_url(url, config):
        return None
    source = github_download_candidates(url, config)[0][1]
    return {'kind': 'rule_download', 'github': is_github_url(source)}


def _download_error_message(exc):
    # Requests exceptions can contain rule tokens or proxy credentials.
    if isinstance(exc, RuleFetchError):
        return '原始地址及 GitHub 代理地址均下载失败；' + _download_error_message(exc.last_error)
    if isinstance(exc, requests.exceptions.Timeout):
        return '下载超时，请检查网络连接或稍后重试'
    if isinstance(exc, requests.exceptions.ProxyError):
        return '代理连接失败，请检查规则下载代理是否可用'
    if isinstance(exc, requests.exceptions.SSLError):
        return '无法建立 HTTPS 安全连接，请检查规则源及其证书'
    if isinstance(exc, requests.exceptions.ConnectionError):
        return '连接失败，请检查规则下载地址及网络连接'
    if isinstance(exc, requests.exceptions.HTTPError) and exc.response is not None:
        return f'下载服务器返回错误（HTTP {exc.response.status_code}），请检查规则下载地址或稍后重试'
    return '下载失败，请检查规则下载地址及网络连接'


_packing = Lock()


class DeploymentBundle:
    """A delivery: its manifest, and the gzip archive a device receives.

    A bundle made from its files (``from_files``) packs the archive on first
    use. Comparing content needs the manifest alone, and listing changes reads
    a few files; compressing every file for those was a large part of each
    comparison. The archive bytes do not depend on when they are packed.
    """

    def __init__(self, manifest, archive, sha256, config_version, comparison_config=None):
        self.manifest = manifest
        self.config_version = config_version
        self.comparison_config = comparison_config
        self._archive, self._sha256, self._files = archive, sha256, None

    @classmethod
    def from_files(cls, manifest, files, config_version, comparison_config=None):
        """files: {path: (bytes, role)}, as the manifest lists them."""
        bundle = cls(manifest, None, None, config_version, comparison_config)
        bundle._files = files
        return bundle

    def _packed(self):
        with _packing:
            if self._archive is None:
                self._archive = _archive_bytes(self.manifest, self._files)
                self._sha256 = hashlib.sha256(self._archive).hexdigest()
                self._files = None  # the archive holds them now
            return self._archive

    @property
    def archive(self):
        return self._archive if self._archive is not None else self._packed()

    @property
    def sha256(self):
        if self._sha256 is None:
            self._packed()
        return self._sha256

    def files(self, paths=None):
        """{path: bytes} of the listed manifest paths (all of them by default)."""
        wanted = [item['path'] for item in self.manifest['files']] if paths is None else list(paths)
        with _packing:
            unpacked = self._files
        if unpacked is not None:
            return {path: unpacked[path][0] for path in wanted if path in unpacked}
        return _archive_files(self._archive, wanted)

    def _fields(self):
        return self.manifest, self.archive, self.sha256, self.config_version, self.comparison_config

    def __eq__(self, other):
        return self._fields() == other._fields() if isinstance(other, DeploymentBundle) else NotImplemented

    __hash__ = None

    def __repr__(self):
        return (f'DeploymentBundle(files={len(self.manifest.get("files", []))}, config_version={self.config_version!r}, '
                f'packed={self._archive is not None})')


def _archive_files(archive, paths):
    """Selected members of a packed bundle, in one pass over the compressed stream."""
    wanted = {'files/' + path: path for path in paths}
    found = {}
    if not wanted:
        return found
    with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tar:
        for member in tar:
            path = wanted.get(member.name)
            if path is not None and member.isfile():
                found[path] = tar.extractfile(member).read()
                if len(found) == len(wanted):
                    break
    return found


def relative_path(value):
    if not isinstance(value, str) or not value or '\\' in value or any(ord(c) < 32 for c in value):
        raise DeploymentPreparationError('Invalid deployment file path')
    while value.startswith('./'):
        value = value[2:]
    path = PurePosixPath(value)
    if path.is_absolute() or not path.parts or any(p in ('..', '.', '') for p in value.split('/')):
        raise DeploymentPreparationError('Deployment files must use safe relative paths')
    if value.startswith('.configflow') or value == 'manifest.json':
        raise DeploymentPreparationError('Reserved deployment file path')
    return path.as_posix()


def deployment_content_digests(manifest):
    """Per-file digests behind deployment_content_fingerprint, to explain drift.

    Keys match the fingerprint's canonical paths: the main file is
    ``config.yaml`` whatever its installed name.
    """
    deployment_content_fingerprint(manifest)  # same validation as the fingerprint
    return {('config.yaml' if item['role'] == 'config' else item['path']): item['sha256'].lower()
            for item in manifest['files']}


def deployment_content_fingerprint(manifest):
    """Identify all materialized delivery bytes, independently of a transaction.

    The manifest must describe a complete validated bundle. This function does
    not fetch resources or inspect their contents: its digests come from the
    materialization step. An Agent's installed main filename is presentation
    metadata; every other managed file path remains part of the identity.
    """
    if (not isinstance(manifest, dict)
            or type(manifest.get('protocol_version')) is not int
            or manifest['protocol_version'] != 1
            or manifest.get('service_type') not in ('mihomo', 'mosdns', 'oxidns')):
        raise DeploymentPreparationError('Invalid deployment content manifest')

    def manifest_path(value):
        path = relative_path(value)
        if path != value or '\x7f' in path:
            raise DeploymentPreparationError('Manifest paths must be canonical relative paths')
        return path

    config_path = manifest_path(manifest.get('config_path'))
    if '/' in config_path:
        raise DeploymentPreparationError('Agent configuration filename must be a basename')
    entries = manifest.get('files')
    if not isinstance(entries, list) or not entries or len(entries) > MAX_FILES:
        raise DeploymentPreparationError('Manifest must contain a complete deployment file list')

    files, paths, canonical_paths = [], set(), set()
    config_count, total_size = 0, 0
    for item in entries:
        if not isinstance(item, dict):
            raise DeploymentPreparationError('Invalid deployment manifest file')
        path = manifest_path(item.get('path'))
        role, size, digest = item.get('role'), item.get('size'), item.get('sha256')
        if (not isinstance(role, str) or role not in ('config', 'provider', 'rule', 'custom')
                or type(size) is not int or not 0 <= size <= MAX_FILE_BYTES
                or not isinstance(digest, str) or not re.fullmatch(r'[0-9a-fA-F]{64}', digest)):
            raise DeploymentPreparationError('Invalid deployment manifest file metadata')
        if path in paths:
            raise DeploymentPreparationError('Duplicate deployment manifest path')
        paths.add(path)
        if role == 'config':
            config_count += 1
            if path != config_path or size == 0:
                raise DeploymentPreparationError('Invalid main configuration manifest entry')
            path = 'config.yaml'
        elif path == config_path:
            raise DeploymentPreparationError('Main configuration entry must have config role')
        if path in canonical_paths:
            raise DeploymentPreparationError('Conflicting canonical deployment paths')
        canonical_paths.add(path)
        total_size += size
        if total_size > MAX_TOTAL_BYTES:
            raise DeploymentPreparationError('Deployment exceeds size limit')
        files.append({'path': path, 'size': size, 'sha256': digest.lower(), 'role': role})
    if config_count != 1:
        raise DeploymentPreparationError('Manifest must contain exactly one main configuration')

    content = {'version': 1, 'service_type': manifest['service_type'],
               'files': sorted(files, key=lambda item: item['path'])}
    encoded = json.dumps(content, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode('utf-8')
    return 'v1:' + hashlib.sha256(encoded).hexdigest()


def _local_rule(url, config, base_url, *, rules_local_only=False):
    """Return the snapshot's library entry only for a recognized local URL."""
    from backend.common.profile_context import profile_api_path
    parsed = urlsplit(url)
    settings = deepcopy(config.get('system_config', {}))
    settings['server_domain'] = settings.get('server_domain') or base_url
    if (parsed.scheme or parsed.netloc) and not is_internal_rule_url(url, {'system_config': settings}):
        return None
    prefix = urlsplit(settings['server_domain']).path.rstrip('/')
    path = unquote(parsed.path)
    for rule in config.get('rule_library', []):
        paths = [profile_api_path(config, '/rules/local/' + rule.get('name', '')),
                 profile_api_path(config, '/rule-library/content/' + rule.get('id', ''))]
        if path in (paths[0], prefix + paths[0]):
            return rule
        if path in (paths[1], prefix + paths[1]):
            return None if rules_local_only else rule
    if '/api/' in path or path.startswith('/rules/local/') or path.startswith('/rule-library/content/'):
        raise DeploymentPreparationError('Rule callback is not part of this profile snapshot')
    return None


def _fetch_bytes(url, config):
    if not url:
        raise DeploymentPreparationError('Required rule has no source URL')
    response = None
    try:
        response = request_rule(url, timeout=(10, 60), config_data=config, stream=True, max_bytes=MAX_FILE_BYTES)
        response.raise_for_status()
        value = bytearray()
        for chunk in response.iter_content(chunk_size=64 * 1024):
            value.extend(chunk)
            if len(value) > MAX_FILE_BYTES:
                raise DeploymentPreparationError('Rule exceeds deployment file size limit')
        return bytes(value)
    except DeploymentPreparationError:
        raise
    except Exception as exc:
        details = failure_text(f'{type(exc).__name__}: {exc}')
        raise RuleDownloadError(f'{_download_error_message(exc)}\n原始错误：{details}',
                                remedy=_download_remedy(url, exc, config)) from None
    finally:
        if response is not None:
            response.close()


def materialize_rule(url, config, base_url, *, depth=0, raw_mosdns=False, fetch_raw=None,
                     source_id=None, declared_rule=None, remote_sources=None):
    if depth > 3:
        raise DeploymentPreparationError('Recursive rule callback')
    parsed = urlsplit(url)
    settings = {**config.get('system_config', {}),
                'server_domain': config.get('system_config', {}).get('server_domain') or base_url}
    internal = not (parsed.scheme or parsed.netloc) or is_internal_rule_url(url, {'system_config': settings})
    if internal and parsed.path.endswith(('/mosdns/rule-proxy', '/oxidns/rule-proxy')):
        query = parse_qs(parsed.query)
        original = query.get('url', [''])[0]
        if not original:
            raise DeploymentPreparationError('Rule conversion callback has no source')
        value = materialize_rule(original, config, base_url, depth=depth + 1,
                                 raw_mosdns=raw_mosdns, fetch_raw=fetch_raw,
                                 source_id=query.get('source_id', [None])[0], declared_rule=declared_rule,
                                 remote_sources=remote_sources)
        # The final consuming plugin chooses domain/IP conversion for bundles.
        return value if raw_mosdns else convert_mosdns_rules(value)
    # Gateway callbacks must use this publication's immutable snapshot, never a
    # second HTTP request that could observe another draft/profile revision.
    if internal:
        from backend.common.linux_gateway import (
            LinuxGatewayError, gateway_provider_yaml, gateway_source_yaml, provider_path,
        )
        prefix = urlsplit(settings['server_domain']).path.rstrip('/')
        path = unquote(parsed.path)
        selected = config.get('mihomo', {}).get('linux_gateway', {}).get('rule_set_ids', [])
        for rule_id in selected:
            for source in (False, True):
                expected = unquote(provider_path(config, rule_id, source=source))
                if path not in (expected, prefix + expected):
                    continue
                def load_source(item, library):
                    rule_source = library or item
                    content = materialize_rule(rule_source.get('url', ''), config, base_url,
                                               depth=depth + 1, fetch_raw=fetch_raw, remote_sources=remote_sources)
                    try:
                        return content.decode('utf-8-sig')
                    except UnicodeError:
                        raise DeploymentPreparationError('Gateway rules must be UTF-8 text') from None
                try:
                    render = gateway_source_yaml if source else gateway_provider_yaml
                    return render(config, rule_id, content_loader=load_source).encode('utf-8')
                except LinuxGatewayError as exc:
                    raise DeploymentPreparationError(str(exc)) from None
    rule = _local_rule(url, config, base_url)
    if rule is not None:
        if rule.get('source_type') == 'content':
            return rule.get('content', '').encode('utf-8')
        source = rule.get('url', '')
        if source == url:
            raise DeploymentPreparationError('Rule callback refers to itself')
        source_id = rule.get('id')
        return materialize_rule(source, config, base_url, depth=depth + 1,
                                raw_mosdns=raw_mosdns, fetch_raw=fetch_raw, source_id=source_id,
                                declared_rule=declared_rule, remote_sources=remote_sources)
    if remote_sources is not None:
        remote_sources.add(url)
    try:
        declared = resolve_remote_rule_source(url, config, source_id)
        if declared is None and declared_rule is not None:
            declared = {**declared_rule, 'source_type': 'url', 'url': url}
        if declared is not None and declared.get('source_type', 'url') == 'url':
            from backend.utils.scheduled_refresh import read_rule_source
            value, error = read_rule_source(
                declared, config, fetch=fetch_raw or (lambda source_url: _fetch_bytes(source_url, config)))
            if error is not None:
                logger.warning('规则集「%s」更新失败，使用上一次有效缓存：%s',
                               declared.get('name', ''), failure_text(str(error)))
            return value
    except RemoteRuleValidationError as exc:
        raise DeploymentPreparationError(str(exc)) from None
    return fetch_raw(url) if fetch_raw is not None else _fetch_bytes(url, config)


def _empty_rule_text(url):
    """Bytes a device receives for an empty part when it refreshes this URL.

    Native rule updates store rule-proxy's response; a publication must carry
    the same bytes, or the content baseline differs after every refresh.
    """
    parsed = urlsplit(url or '')
    if not parsed.path.endswith(('/mosdns/rule-proxy', '/oxidns/rule-proxy')):
        return b''
    part = parse_qs(parsed.query).get('part', [None])[0]
    return empty_rule_part(part).encode('utf-8') if part in RULE_PARTS else b''


_converted = ResultCache(256, 16 * 1024 * 1024)


def convert_mosdns_rules(content, rule_type='domain_set', *, mixed=False):
    """Convert text for the final MosDNS domain_set or ip_set consumer.

    mixed marks the IP part of a classical source: its domain entries belong to
    the domain part and are skipped, as rule-proxy does for device updates.
    """
    if rule_type not in ('domain_set', 'ip_set'):
        raise DeploymentPreparationError('Unsupported MosDNS rule plugin type')
    # A pure function of its arguments that every publication and Agent
    # repeats for the same sources. Failures are not cached.
    key = (content_digest(content), rule_type, mixed)
    cached = _converted.get(key) if key[0] else None
    if cached is not None:
        return cached
    converted = _convert_mosdns_rules(content, rule_type, mixed)
    if key[0]:
        _converted.put(key, converted, len(converted))
    return converted


def _convert_mosdns_rules(content, rule_type, mixed):
    try:
        text = content.decode('utf-8-sig')
    except UnicodeError:
        raise DeploymentPreparationError('MosDNS requires text rules; binary MRS is only supported by Mihomo') from None
    if '\x00' in text:
        raise DeploymentPreparationError('MosDNS rule source is binary')
    lines = text.splitlines()
    yaml_payload = bool(_has_yaml_payload(text))
    if yaml_payload:
        try:
            document = yaml_safe_load(text)
            lines = document['payload']
            if not isinstance(lines, list):
                raise ValueError
        except Exception as exc:
            raise source_error('Invalid rule YAML payload', text, cause=exc,
                               payload_index=-1 if not isinstance(exc, yaml.YAMLError) else None,
                               source_label='转换前的规则内容') from None
        invalid = next((index for index, line in enumerate(lines) if not isinstance(line, str)), None)
        if invalid is not None:
            raise source_error('Invalid rule YAML payload', text, payload_index=invalid,
                               source_label='转换前的规则内容')
    converted = []
    mapping = {'DOMAIN': 'full', 'DOMAIN-SUFFIX': 'domain', 'DOMAIN-KEYWORD': 'keyword', 'DOMAIN-REGEX': 'regexp'}

    def invalid_line(message, index):
        return source_error(message, text, line=None if yaml_payload else index + 1,
                            payload_index=index if yaml_payload else None, source_label='转换前的规则内容')

    for index, line in enumerate(lines):
        line = line.strip()
        if not line or line.startswith(('#', '//')):
            continue
        if rule_type == 'ip_set':
            if re.match(r'^(domain|full|keyword|regexp):', line):
                continue
            value = line[3:].strip() if line.startswith('ip:') else line
            if ',' in line:
                kind, value = parse_clash_rule_value(line)
                if kind not in ('IP-CIDR', 'IP-CIDR6'):
                    continue
            try:
                ipaddress.ip_network(value, strict=False)
            except ValueError:
                if mixed and ',' not in line and not line.startswith('ip:'):
                    continue
                raise invalid_line('Invalid IP network in MosDNS ip_set rule source', index) from None
            converted.append(value)
            continue
        if line.startswith('ip:'):
            continue
        if re.match(r'^(domain|full|keyword|regexp):', line):
            converted.append(line)
        elif ',' in line:
            # Classical provider regex commas are payload; ordinary rules
            # take only their first value field, ignoring subsequent params.
            kind, value = parse_clash_rule_value(line)
            if kind in mapping:
                converted.append(f'{mapping[kind]}:{value}')
            # Domain consumers omit IP/process rules, as rule-proxy did for
            # Clash input. In particular, a bare IPv6 CIDR cannot load in domain_set.
        else:
            try:
                ipaddress.ip_network(line, strict=False)
                # Raw address/CIDR lists must not reach a domain_set either.
            except ValueError:
                expression = domain_pattern_for_mosdns(line)
                if not expression:
                    raise invalid_line('Unrecognized MosDNS rule source content', index) from None
                converted.append(expression)
    return '\n'.join(converted).encode('utf-8')


def render_deployment_config(main, service, *, local_dns_watchdog=True):
    """Render the delivered main file without materializing remote resources."""
    if local_dns_watchdog and service in ('mosdns', 'oxidns'):
        from backend.common.dns_watchdog import ensure_local_dns_watchdog
        try:
            main = ensure_local_dns_watchdog(main)
        except ValueError as exc:
            raise preparation_context(DeploymentPreparationError(str(exc)),
                                      name=service, path='config.yaml', kind='配置') from None
    return yaml.safe_dump(main, allow_unicode=True, sort_keys=False).encode('utf-8')


def build_deployment_bundle(agent, config, config_content, *, provider_downloads=(),
                            ruleset_downloads=(), custom_files=(), base_url='', deployment_id=None,
                            progress=None, detail_progress=None, phase_progress=None, pinned_files=(),
                            local_dns_watchdog=True):
    """Pack the delivered graph, local DNS probe and all managed references."""
    config = deepcopy(config)
    service = agent.get('service_type', 'mihomo')
    if service not in ('mihomo', 'mosdns', 'oxidns'):
        raise DeploymentPreparationError('Unsupported transactional deployment service')
    try:
        main = yaml.safe_load(config_content)
    except yaml.YAMLError as exc:
        raise preparation_context(source_error('生成配置的 YAML 格式不正确', config_content, cause=exc),
                                  name=service, path='config.yaml', kind='配置') from None
    if not isinstance(main, dict):
        raise DeploymentPreparationError('Generated configuration must be a YAML mapping')
    if service == 'oxidns' and main.get('include'):
        raise DeploymentPreparationError('OxiDNS deployment must inline included plugin definitions')
    rule_types, rule_names = {}, {}
    if service in ('mosdns', 'oxidns'):
        for plugin in main.get('plugins', []):
            rule_type = plugin.get('type')
            if rule_type not in ('domain_set', 'ip_set'):
                continue
            args = plugin.get('args') or {}
            if not isinstance(args, dict) or not isinstance(args.get('files', []), list):
                raise DeploymentPreparationError('MosDNS file references must be a list')
            for reference in args.get('files', []):
                if service == 'oxidns':
                    from backend.common.oxidns_native import relative_resource
                    if relative_resource(reference) is None:
                        continue
                path = relative_path(reference)
                if path in rule_types and rule_types[path] != rule_type:
                    raise DeploymentPreparationError('MosDNS file is used by both domain_set and ip_set: ' + path)
                rule_types[path] = rule_type
                rule_names[path] = plugin.get('tag', '')
    files = {}
    file_names = {}

    def add_content(path, value, role, *, native=False, mixed=False, empty=b''):
        path = relative_path(path)
        if service == 'oxidns' and (path == 'state' or path.startswith('state/')):
            raise DeploymentPreparationError('OxiDNS runtime state cannot be managed by a deployment')
        value = value.encode('utf-8') if isinstance(value, str) else value
        if not isinstance(value, bytes):
            raise DeploymentPreparationError('Missing deployment file content')
        # OxiDNS attachments already contain native provider syntax. In
        # particular, a bare domain is a suffix rule, not MosDNS's full: rule.
        # Only downloaded/shared rule sources go through the rule converter.
        if path in rule_types and not (service == 'oxidns' and native):
            value = convert_mosdns_rules(value, rule_types[path], mixed=mixed) or empty
        if len(value) > MAX_FILE_BYTES:
            raise DeploymentPreparationError('Deployment file exceeds size limit')
        if path in files:
            raise DeploymentPreparationError('Duplicate deployment path: ' + path)
        if len(files) >= MAX_FILES or sum(len(item[0]) for item in files.values()) + len(value) > MAX_TOTAL_BYTES:
            raise DeploymentPreparationError('Deployment exceeds size limit')
        files[path] = (value, role)
        return path

    def add(path, value, role, *, native=False, mixed=False, name='', empty=b''):
        kind = '订阅' if role == 'provider' else '配置' if role == 'config' else '规则集' if role == 'rule' else '附加文件'
        name = name or rule_names.get(str(path).removeprefix('./'), '')
        try:
            target = add_content(path, value, role, native=native, mixed=mixed, empty=empty)
        except DeploymentPreparationError as exc:
            raise preparation_context(exc, name=name, path=path, kind=kind) from None
        if name:
            file_names[target] = failure_text(str(name), limit=512)
        return target

    for item in provider_downloads:
        add(item.get('local_path'), item.get('content'), 'provider', name=item.get('name', ''))
    for item in custom_files:
        add(item.get('path'), item.get('content'), 'custom', native=item.get('native', False), name=item.get('name', ''))

    # Trusted bytes from the last confirmed deployment keep disabled rule
    # scopes and unrelated resources unchanged during an automatic rule edit.
    pinned_paths = set()
    for item in pinned_files:
        if item.get('role') not in ('rule', 'custom', 'provider'):
            raise DeploymentPreparationError('Invalid pinned deployment file role')
        pinned_paths.add(add(item['path'], item['content'], item['role'], native=True))

    if service == 'mihomo' and config.get('mihomo', {}).get('linux_gateway', {}).get('enabled'):
        from backend.common.linux_gateway import _routing_mark_path
        for path, (content, role) in files.items():
            if role != 'provider':
                continue
            try:
                provider = yaml.safe_load(content)
            except (yaml.YAMLError, UnicodeError, RecursionError):
                raise DeploymentPreparationError('Linux 网关无法检查订阅节点，请先修复 Provider YAML') from None
            if _routing_mark_path(provider):
                raise DeploymentPreparationError('Linux 网关与订阅节点中的 routing-mark 冲突，请移除后重新发布')

    # Use the final Mihomo configuration rather than reconstructing paths from
    # source suffixes; custom configuration can change provider format/path.
    downloads = list(ruleset_downloads)
    if service == 'mihomo' and 'rule-providers' in main:
        downloads = []
        for name, definition in main['rule-providers'].items():
            if definition.get('type') == 'inline':
                definition.pop('path', None)
                continue
            if not definition.get('path'):
                raise DeploymentPreparationError(f'Rule provider {name} has no bundled path')
            path = relative_path(definition['path'])
            if path in files:
                continue
            if not definition.get('url'):
                raise DeploymentPreparationError(f'Rule provider {name} references an unbundled local file; use a managed rule source')
            downloads.append({'name': name, 'url': definition['url'], 'local_path': path,
                              'behavior': definition.get('behavior'),
                              'format': definition.get('format', 'yaml')})
    downloads = [item for item in downloads if relative_path(item['local_path']) not in pinned_paths]
    # Share only original response bytes within this captured deployment. A
    # classical source may feed both domain and IP files through different
    # local callbacks; conversion and per-file/total limits still run for each
    # destination. Bound retained originals too: a large comment-only source
    # can become a very small converted file. Never reuse these responses
    # across separate publications.
    raw_downloads = OrderedDict()
    pending_downloads = {}
    cached_bytes = 0
    download_lock = Lock()

    def fetch_raw(url):
        nonlocal cached_bytes
        with download_lock:
            cached = raw_downloads.get(url)
            if cached is not None:
                raw_downloads.move_to_end(url)
                return cached
            future = pending_downloads.get(url)
            owner = future is None
            if owner:
                future = pending_downloads[url] = Future()
        if not owner:
            return future.result()
        # The owning worker fetches directly, without scheduling a second
        # pool task or holding the lock while other consumers wait.
        try:
            value = _fetch_bytes(url, config)
            with download_lock:
                if len(value) <= MAX_RAW_CACHE_BYTES and MAX_RAW_CACHE_ENTRIES > 0:
                    while raw_downloads and (cached_bytes + len(value) > MAX_RAW_CACHE_BYTES
                                             or len(raw_downloads) >= MAX_RAW_CACHE_ENTRIES):
                        _, evicted = raw_downloads.popitem(last=False)
                        cached_bytes -= len(evicted)
                    raw_downloads[url] = value
                    cached_bytes += len(value)
            future.set_result(value)
            return value
        except BaseException as exc:
            future.set_exception(exc)
            raise
        finally:
            with download_lock:
                pending_downloads.pop(url, None)

    def download(item):
        if detail_progress is not None:
            detail_progress(None, len(downloads), item.get('name') or '未命名', 'preparing')
        try:
            remote_sources = set()
            value = materialize_rule(item.get('url', ''), config, base_url,
                                     raw_mosdns=relative_path(item['local_path']) in rule_types,
                                     fetch_raw=fetch_raw,
                                     declared_rule=item if item.get('behavior') is not None else None,
                                     remote_sources=remote_sources)
            # Snapshot callbacks bypass /rules/local, so apply the same repair
            # for the final Mihomo consumer without changing shared raw bytes.
            if (service == 'mihomo'
                    and parse_qs(urlsplit(item.get('url', '')).query).get('target', [''])[0] == 'mihomo'
                    and (local_rule := _local_rule(item['url'], config, base_url, rules_local_only=True)) is not None
                    and local_rule.get('behavior', 'domain') == item.get('behavior', 'domain')):
                try:
                    text = value.decode('utf-8')
                except UnicodeDecodeError:
                    pass
                else:
                    normalized, warnings = normalize_mihomo_ruleset(
                        text, local_rule.get('behavior', 'domain'), ruleset_format(local_rule, config))
                    value = normalized.encode('utf-8')
                    if warnings:
                        logger.warning('Mihomo provider %s: %s unconverted lines (first 10: %s)',
                                       item.get('name', ''), len(warnings),
                                       ', '.join(str(warning.get('line')) for warning in warnings[:10]))
            if service == 'mihomo' and remote_sources and item.get('behavior') is not None:
                # Custom provider declarations can differ from the library.
                # Check remote bytes against their final consumer too; inline
                # sources retain their existing save/delivery warning policy.
                from backend.utils.ruleset_content import validate_remote_rule_content
                validate_remote_rule_content(value, item, config_data={}, rule_format=item.get('format'))
        except RemoteRuleValidationError as exc:
            raise preparation_context(DeploymentPreparationError(str(exc)),
                                      name=item.get('name', ''), path=item.get('local_path', '')) from None
        except RuleDownloadError as exc:
            name = item.get('name') or '未命名'
            explanation, _, details = str(exc).partition('\n')
            raise RuleDownloadError(
                f'无法下载「{name}」规则集：{explanation}。本次发布已停止，设备仍使用原配置。'
                + f'\n文件：{failure_text(str(item.get("local_path", "")))}'
                + '\n修复：更换该规则的下载地址，或检查「GitHub 代理域名」和「系统设置 → 规则下载代理」。'
                + '\n也可手动下载后导入规则库的内容型规则，再重新发布。'
                + (f'\n{details}' if details else ''), remedy=exc.remedy) from None
        except DeploymentPreparationError as exc:
            raise preparation_context(exc, name=item.get('name', ''), path=item.get('local_path', '')) from None
        return item, value
    if progress is not None:
        progress(0, len(downloads))
    try:
        with ThreadPoolExecutor(max_workers=8) as pool:
            futures = (pool.submit(download, item) for item in downloads)
            for done, future in enumerate(as_completed(futures), 1):
                item, value = future.result()
                add(item['local_path'], value, 'rule', name=item.get('name', ''),
                    mixed=item.get('behavior') == 'classical', empty=_empty_rule_text(item.get('url', '')))
                if detail_progress is not None:
                    detail_progress(done, len(downloads), item.get('name') or '未命名', 'prepared')
                elif progress is not None:
                    progress(done, len(downloads))
    finally:
        raw_downloads.clear()
        pending_downloads.clear()

    if service == 'mihomo':
        for section in ('proxy-providers', 'rule-providers'):
            for name, provider in (main.get(section) or {}).items():
                if 'path' not in provider:
                    if provider.get('type') == 'inline':
                        continue
                    raise DeploymentPreparationError(f'Provider {name} has no bundled path')
                path = relative_path(provider['path'])
                if path not in files:
                    raise DeploymentPreparationError(f'Provider {name} has no bundled file')
                provider['path'] = './' + path
    elif service == 'oxidns':
        from backend.common.oxidns_native import validate_resource_files
        from backend.common.oxidns_settings import OxidnsSettingsError
        from backend.converters.oxidns import unavailable_rulesets
        try:
            validate_resource_files(main, files)
        except OxidnsSettingsError as exc:
            missing = _unbundled_references(main, files)
            hints = unavailable_rulesets(config, missing)
            if hints:
                raise DeploymentPreparationError('OxiDNS 还在使用不可用的规则集，发布已停止，设备配置没有改动。'
                                                 + ''.join(f'\n· {hint}' for hint in hints)) from None
            if missing:
                raise DeploymentPreparationError(
                    f'OxiDNS 配置用到了文件 {missing[0]}，但附加文件里没有它，发布已停止，设备配置没有改动。'
                    '\n请在 OxiDNS 设置的「附加文件」中添加它，或在 YAML 中删除这个引用。') from None
            raise DeploymentPreparationError(str(exc)) from None
    else:
        for plugin in main.get('plugins', []):
            args = plugin.get('args') or {}
            if not isinstance(args, dict):
                continue
            paths = args.get('files', [])
            if not isinstance(paths, list):
                raise DeploymentPreparationError('MosDNS file references must be a list')
            for index, value in enumerate(paths):
                path = relative_path(value)
                if path not in files:
                    raise DeploymentPreparationError('MosDNS references an unbundled file: ' + path)
                paths[index] = './' + path
    config_bytes = render_deployment_config(main, service, local_dns_watchdog=local_dns_watchdog)
    add('config.yaml', config_bytes, 'config')
    from backend.common.config import get_default_profile_id
    manifest = {
        'protocol_version': 1, 'deployment_id': deployment_id or uuid.uuid4().hex,
        'agent_id': agent['id'], 'profile_id': config.get('profile_id') or agent.get('profile_id') or get_default_profile_id(),
        'service_type': service, 'config_path': 'config.yaml',
        'files': [{'path': path, 'size': len(value), 'sha256': hashlib.sha256(value).hexdigest(), 'role': role,
                   **({'name': file_names[path]} if path in file_names else {})}
                  for path, (value, role) in sorted(files.items())],
    }
    if phase_progress is not None:
        phase_progress(f'正在打包发布文件（{len(files)} 个文件）')
    return _pack(manifest, files, hashlib.sha256(config_bytes).hexdigest()[:8])


def _unbundled_references(main, files):
    """Relative OxiDNS resource paths the YAML references but the bundle lacks."""
    from backend.common.oxidns_native import relative_resource, resource_references
    from backend.common.oxidns_settings import safe_relative_path
    bundled = {safe_relative_path(path) for path in files}
    return sorted({path for path in map(relative_resource, resource_references(main))
                   if path is not None and path not in bundled})


def _pack(manifest, files, config_version, *, comparison_config=None):
    return DeploymentBundle.from_files(manifest, dict(files), config_version, comparison_config)


def _archive_bytes(manifest, files):
    archive = io.BytesIO()
    with gzip.GzipFile(fileobj=archive, mode='wb', mtime=0) as compressed, \
            tarfile.open(fileobj=compressed, mode='w', format=tarfile.PAX_FORMAT) as tar:
        members = [('manifest.json', json.dumps(manifest, ensure_ascii=False).encode('utf-8'))]
        members.extend(('files/' + path, value) for path, (value, _) in sorted(files.items()))
        for path, value in members:
            member = tarfile.TarInfo(path)
            member.size = len(value)
            member.mode = 0o600
            tar.addfile(member, io.BytesIO(value))
    return archive.getvalue()


def retarget_config_path(bundle, config_path):
    """Honor the target's installed config filename without changing config bytes."""
    path = relative_path(config_path)
    if '/' in path:
        raise DeploymentPreparationError('Agent configuration filename must be a basename')
    current = bundle.manifest['config_path']
    if path == current:
        return bundle
    manifest = deepcopy(bundle.manifest)
    if any(item['path'] == path for item in manifest['files']):
        raise DeploymentPreparationError('Agent configuration filename collides with a managed file')
    manifest['config_path'] = path
    files = {}
    content = bundle.files()
    for item in manifest['files']:
        old_path = item['path']
        if old_path == current:
            item['path'] = path
        files[item['path']] = (content[old_path], item['role'])
    comparison = deepcopy(bundle.comparison_config)
    if comparison:
        comparison['path'] = path
    return _pack(manifest, files, bundle.config_version, comparison_config=comparison)
