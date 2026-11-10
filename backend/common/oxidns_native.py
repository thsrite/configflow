"""Bounded native OxiDNS documents and explicitly managed deployment assets.

Native configuration is deliberately not projected through the convenience
template. Unknown core fields remain intact, and the installed core performs
the final semantic validation during an Agent transaction.
"""
import base64
import binascii
import math
import posixpath
from pathlib import PurePosixPath
import re

import yaml

from backend.common.oxidns_settings import OxidnsSettingsError, safe_relative_path
from backend.utils import fast_yaml

MAX_NATIVE_BYTES = 2 * 1024 * 1024
MAX_ADDITIONAL_FILES = 256
MAX_ADDITIONAL_FILE_BYTES = 16 * 1024 * 1024
MAX_ADDITIONAL_TOTAL_BYTES = 64 * 1024 * 1024
TAG = re.compile(r'^[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9_-]*[A-Za-z0-9])?)*$')


class NativeLoader(yaml.SafeLoader):
    """Do not silently reinterpret YAML 1.2 strings such as on/off or dates."""
    yaml_implicit_resolvers = {
        key: [(tag, rx) for tag, rx in values if tag not in (
            'tag:yaml.org,2002:bool', 'tag:yaml.org,2002:timestamp',
            'tag:yaml.org,2002:int', 'tag:yaml.org,2002:float')]
        for key, values in yaml.SafeLoader.yaml_implicit_resolvers.items()
    }

    def construct_mapping(self, node, deep=False):
        result = {}
        for key_node, value_node in node.value:
            key = self.construct_object(key_node, deep=deep)
            if not isinstance(key, str) or key in ('__proto__', 'prototype', 'constructor'):
                raise OxidnsSettingsError('OxiDNS YAML 字段名必须为安全文本')
            if key in result:
                raise OxidnsSettingsError('OxiDNS YAML 包含重复字段')
            result[key] = self.construct_object(value_node, deep=deep)
        return result


NativeLoader.add_implicit_resolver('tag:yaml.org,2002:bool', re.compile(r'^(?:true|false|True|False|TRUE|FALSE)$'), list('tTfF'))
NativeLoader.add_implicit_resolver('tag:yaml.org,2002:int', re.compile(r'^[-+]?(?:[0-9][0-9_]*|0o[0-7_]+|0x[0-9a-fA-F_]+|0b[01_]+)$'), list('-+0123456789'))
NativeLoader.add_implicit_resolver('tag:yaml.org,2002:float', re.compile(r'^[-+]?(?:(?:[0-9][0-9_]*\.[0-9_]*|\.[0-9_]+)(?:[eE][-+]?[0-9]+)?|[0-9][0-9_]*[eE][-+]?[0-9]+|\.(?:inf|Inf|INF|nan|NaN|NAN))$'), list('-+0123456789.'))
NativeLoader.add_constructor('tag:yaml.org,2002:int', lambda loader, node: int(
    loader.construct_scalar(node).replace('_', ''),
    0 if loader.construct_scalar(node).lstrip('-+').startswith(('0x', '0o', '0b')) else 10))


if fast_yaml.LIBYAML_SAFE_LOADER is not None:
    class _LibyamlNativeLoader(fast_yaml.LIBYAML_SAFE_LOADER):
        """NativeLoader's rules on the libyaml parser; see fast_yaml for when it is used."""
        yaml_implicit_resolvers = {key: list(values) for key, values in NativeLoader.yaml_implicit_resolvers.items()}
        yaml_constructors = dict(NativeLoader.yaml_constructors)
        construct_mapping = NativeLoader.construct_mapping
else:
    _LibyamlNativeLoader = None


def load_native_yaml(text, label='完整配置'):
    if not isinstance(text, str) or len(text.encode('utf-8')) > MAX_NATIVE_BYTES:
        raise OxidnsSettingsError(f'OxiDNS {label}必须为不超过 2 MiB 的 YAML 文本')
    try:
        value = fast_yaml.load(text, NativeLoader, _LibyamlNativeLoader)
    except OxidnsSettingsError:
        raise
    except (yaml.YAMLError, RecursionError, ValueError):
        raise OxidnsSettingsError(f'OxiDNS {label} YAML 语法无效') from None
    count = 0
    active = set()

    def visit(item, depth=0):
        nonlocal count
        count += 1
        if count > 50000 or depth > 32:
            raise OxidnsSettingsError('OxiDNS YAML 超出结构大小限制')
        if isinstance(item, (dict, list)):
            if id(item) in active:
                raise OxidnsSettingsError('OxiDNS YAML 不支持循环别名')
            active.add(id(item))
            for child in (item.values() if isinstance(item, dict) else item):
                visit(child, depth + 1)
            active.remove(id(item))
        elif not (item is None or isinstance(item, (str, int, float, bool))):
            raise OxidnsSettingsError('OxiDNS YAML 只支持普通对象、数组和标量')
        elif isinstance(item, float) and not math.isfinite(item):
            raise OxidnsSettingsError('OxiDNS YAML 数字必须为有限值')

    visit(value)
    if not isinstance(value, dict):
        raise OxidnsSettingsError(f'OxiDNS {label}顶层必须为对象')
    return value


def managed_files(value):
    """Validate settings metadata and return byte-exact materializer inputs."""
    if not isinstance(value, list) or len(value) > MAX_ADDITIONAL_FILES:
        raise OxidnsSettingsError('附加文件必须为数组，最多 256 个文件')
    result, seen, total = [], set(), 0
    for item in value:
        if not isinstance(item, dict):
            raise OxidnsSettingsError('附加文件条目必须为对象')
        path = safe_relative_path(item.get('path'), '附加文件路径')
        if path in ('config.yaml', 'manifest.json', 'state') or path.startswith(('state/', '.configflow')):
            raise OxidnsSettingsError('附加文件不能覆盖主配置、部署元数据或 state/ 运行数据')
        if path in seen:
            raise OxidnsSettingsError('附加文件路径重复')
        if any(path.startswith(previous + '/') or previous.startswith(path + '/') for previous in seen):
            raise OxidnsSettingsError('附加文件路径存在文件与目录冲突')
        seen.add(path)
        content, encoding = item.get('content'), item.get('encoding', 'text')
        if not isinstance(content, str) or encoding not in ('text', 'base64'):
            raise OxidnsSettingsError('附加文件内容必须为文本，编码为 text 或 base64')
        if len(content) > MAX_ADDITIONAL_FILE_BYTES * 2:
            raise OxidnsSettingsError('单个附加文件不能超过 16 MiB')
        try:
            data = content.encode('utf-8') if encoding == 'text' else base64.b64decode(content, validate=True)
        except (binascii.Error, ValueError):
            raise OxidnsSettingsError('附加文件 Base64 内容无效') from None
        if len(data) > MAX_ADDITIONAL_FILE_BYTES:
            raise OxidnsSettingsError('单个附加文件不能超过 16 MiB')
        total += len(data)
        if total > MAX_ADDITIONAL_TOTAL_BYTES:
            raise OxidnsSettingsError('附加文件总大小不能超过 64 MiB')
        result.append({'path': path, 'content': data, 'native': True})
    return result


def native_document(settings, *, validate_arguments=True, sources=None):
    """Mirror OxiDNS include semantics: included plugins first, globals ignored."""
    assets = {item['path']: item['content'] for item in managed_files(settings.get('additional_files', []))}
    count = 0
    origins = []
    included_globals = []

    def invalid(message, source='config.yaml', path=None):
        error = OxidnsSettingsError(message)
        error.issues = [{'path': path or [], 'source': source, 'message': message,
                         'severity': 'error', 'code': 'structure'}]
        return error

    def expand(document, name='config.yaml', parents=()):
        nonlocal count
        count += 1
        if len(parents) > 8 or count > 512:
            raise invalid('OxiDNS include 超过 8 层或文件数量限制', name, ['include'])
        includes = document.pop('include', [])
        if not isinstance(includes, list) or any(not isinstance(item, str) for item in includes):
            raise invalid('OxiDNS include 必须为文件路径数组', name, ['include'])
        plugins = document.get('plugins', [])
        if not isinstance(plugins, list):
            raise invalid('OxiDNS plugins 必须为数组', name, ['plugins'])
        result = []
        for include_index, reference in enumerate(includes):
            if (not reference or reference.startswith('/') or '${' in reference
                    or '\\' in reference or any(ord(c) < 32 for c in reference)):
                raise invalid('include 必须引用附加文件中的相对路径，不能使用绝对路径或环境变量', name, ['include', include_index])
            # OxiDNS resolves each include relative to its declaring document.
            # Parent segments inside the managed root are legitimate.
            path = safe_relative_path(posixpath.normpath(posixpath.join(posixpath.dirname(name), reference)), 'include 路径')
            if path in parents or path == name:
                raise invalid('OxiDNS include 存在循环引用', name, ['include', include_index])
            if path not in assets:
                raise invalid('OxiDNS include 文件未在附加文件中提供', name, ['include', include_index])
            try:
                included = load_native_yaml(assets[path].decode('utf-8'), 'include 文件')
            except UnicodeDecodeError:
                raise invalid('OxiDNS include 文件必须为 UTF-8 YAML', path) from None
            except OxidnsSettingsError as exc:
                raise invalid(str(exc), path) from None
            result.extend(expand(included, path, (*parents, name))['plugins'])
        if name != 'config.yaml':
            included_globals.append((name, {key: value for key, value in document.items() if key != 'plugins'}))
        origins.extend({'source': name, 'index': index} for index in range(len(plugins)))
        document['plugins'] = result + plugins
        return document

    document = expand(load_native_yaml(settings.get('native_config', '')))
    metadata = {'plugins': origins, 'includes': included_globals}
    if sources is not None:
        sources.update(metadata)
    if validate_arguments:
        from backend.common.oxidns_validation import validate_expanded_document, OxidnsValidationError
        issues = validate_expanded_document(document, metadata)
        if any(item['severity'] == 'error' for item in issues):
            raise OxidnsValidationError(issues)
    return document


def resource_references(document):
    """Return read-only local resources, never mutable cache/log/database paths."""
    references = []

    def add(value):
        if not isinstance(value, str) or not value:
            raise OxidnsSettingsError('OxiDNS 文件引用必须为非空文本')
        references.append(value)

    def files(args):
        if 'files' in args:
            if not isinstance(args['files'], list):
                raise OxidnsSettingsError('OxiDNS 文件引用必须为列表')
            for path in args['files']:
                add(path)

    for plugin in document.get('plugins', []):
        args = plugin.get('args') or {}
        if not isinstance(args, dict):
            continue
        files(args)
        for key in ('file', 'cert', 'key'):
            if args.get(key):
                add(args[key])
        for key in ('persistent', 'tls'):
            nested = args.get(key) or {}
            if isinstance(nested, dict):
                files(nested)
                if nested.get('ca'):
                    add(nested['ca'])
    api = document.get('api') or {}
    http = api.get('http') or {} if isinstance(api, dict) else {}
    if isinstance(http, dict):
        ssl = http.get('ssl') or {}
        if isinstance(ssl, dict):
            for key in ('cert', 'key', 'client_ca'):
                if ssl.get(key):
                    add(ssl[key])
        webui = http.get('webui') or {}
        if isinstance(webui, dict) and webui.get('root'):
            root, index = webui['root'], webui.get('index', 'index.html')
            if not isinstance(root, str) or not isinstance(index, str):
                raise OxidnsSettingsError('OxiDNS WebUI 路径必须为文本')
            add(str(PurePosixPath(root) / index))
    return references


def relative_resource(value):
    """Absolute/env resources are provisioned on the target, not managed here."""
    if isinstance(value, str) and (value.startswith('/') or '${' in value):
        return None
    return safe_relative_path(value, '资源文件路径')


def validate_resource_files(document, paths):
    normalized = {safe_relative_path(path) for path in paths}
    for reference in resource_references(document):
        path = relative_resource(reference)
        if path is not None and path not in normalized:
            raise OxidnsSettingsError('OxiDNS 配置引用了未打包的资源文件：' + path)
    # Runtime outputs must never be overwritten or rolled back with config.
    for plugin in document.get('plugins', []):
        args = plugin.get('args') or {}
        if not isinstance(args, dict):
            continue
        key = {'cache': 'dump_file', 'query_recorder': 'path', 'dynamic_domain_set': 'path'}.get(plugin.get('type'))
        if key and args.get(key):
            path = relative_resource(args[key])
            if path in normalized:
                raise OxidnsSettingsError('OxiDNS 运行数据不能与附加文件重合')
