"""Materialize complete MosDNS downloads without HTTP calls back to ConfigFlow."""
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from copy import deepcopy
import io
import logging
from pathlib import PurePosixPath
from urllib.parse import parse_qs, unquote, urlsplit
import zipfile

import requests
import yaml

from backend.converters.mosdns import RULE_PARTS, convert_rule_content_for_mosdns
from backend.utils.url_utils import safe_exception_details

logger = logging.getLogger(__name__)
MAX_WORKERS = 8
MAX_FILES = 4096
MAX_TOTAL_BYTES = 256 * 1024 * 1024


class MosdnsArchiveError(ValueError):
    """A safe, user-facing error that contains no source URL or credentials."""


def _path(value):
    if not isinstance(value, str) or not value or '\\' in value or any(ord(c) < 32 for c in value):
        raise MosdnsArchiveError('规则文件路径无效')
    while value.startswith('./'):
        value = value[2:]
    path = PurePosixPath(value)
    if path.is_absolute() or any(part in ('', '.', '..') for part in value.split('/')):
        raise MosdnsArchiveError('规则文件必须使用安全的相对路径')
    return path.as_posix()


def _label(item):
    name = str(item.get('name') or '未命名规则')
    # Names normally contain only a label, but do not render pasted URLs or
    # credential expressions into errors/logs either.
    if '://' in name or any(mark in name.lower() for mark in ('token=', 'password=', 'secret=')):
        return '未命名规则'
    return ''.join(c for c in name if ord(c) >= 32 and ord(c) != 127)[:160]


def _local_path(url, config, base_url):
    """Return a local API suffix only for this captured profile and origin."""
    parsed = urlsplit(url)
    bases = [config.get('system_config', {}).get('server_domain', ''), base_url]
    paths = []
    if not (parsed.scheme or parsed.netloc):
        paths.append(unquote(parsed.path))
    else:
        origin = (parsed.scheme, parsed.hostname, parsed.port or (443 if parsed.scheme == 'https' else 80))
        for base in bases:
            if not base:
                continue
            candidate = urlsplit(base)
            expected = (candidate.scheme, candidate.hostname,
                        candidate.port or (443 if candidate.scheme == 'https' else 80))
            prefix = candidate.path.rstrip('/')
            if origin == expected and parsed.path.startswith(prefix + '/'):
                paths.append(unquote(parsed.path[len(prefix):]))
    profile_id = config.get('profile_id', 'default')
    for path in paths:
        if path.startswith('/api/profiles/'):
            prefix = f'/api/profiles/{profile_id}/'
            if not path.startswith(prefix):
                raise MosdnsArchiveError('规则回调引用了其他配置')
            return '/' + path[len(prefix):]
        if path.startswith('/api/'):
            return path[4:]
        if path.startswith(('/mosdns/', '/rules/local/', '/rule-library/content/')):
            return path
    return None


def _source(url, config, base_url, depth=0):
    """Resolve callbacks to inline text or a remote source plus conversions."""
    if depth > 3:
        raise MosdnsArchiveError('规则回调存在循环引用')
    if not isinstance(url, str) or not url:
        raise MosdnsArchiveError('规则缺少下载地址')
    local = _local_path(url, config, base_url)
    if local is None:
        return ('remote', url), ()
    if local == '/mosdns/rule-proxy':
        query = parse_qs(urlsplit(url).query)
        original = query.get('url', [''])[0]
        part = query.get('part', [None])[0] or None
        if part is not None and part not in RULE_PARTS:
            raise MosdnsArchiveError('规则转换类型无效')
        key, conversions = _source(original, config, base_url, depth + 1)
        return key, conversions + (part,)
    library_id = local.removeprefix('/rule-library/content/') if local.startswith('/rule-library/content/') else None
    name = local.removeprefix('/rules/local/') if local.startswith('/rules/local/') else None
    if library_id is None and name is None:
        raise MosdnsArchiveError('无法识别规则回调')
    selected = {rule.get('library_rule_id') for rule in config.get('rule_configs', [])
                if rule.get('enabled', True) and rule.get('library_enabled', True)}
    for rule in config.get('rule_library', []):
        if rule.get('id') not in selected:
            continue
        if (library_id is not None and rule.get('id') == library_id) or (name is not None and rule.get('name') == name):
            if rule.get('source_type') == 'content':
                return ('inline', rule.get('content', '')), ()
            return _source(rule.get('url', ''), config, base_url, depth + 1)
    raise MosdnsArchiveError('规则不在当前配置快照中')


def build_mosdns_zip(config, yaml_content, downloads, custom_files, *, base_url=''):
    """Fetch each original URL once, convert each part, then create the ZIP."""
    # Workers must never select a default profile or read a changed setting.
    config = deepcopy(config)
    files = {}
    total = 0

    def add(path, content):
        nonlocal total
        path = _path(path)
        if not isinstance(content, (str, bytes)):
            raise MosdnsArchiveError('规则文件缺少内容')
        value = content.encode('utf-8') if isinstance(content, str) else content
        if path in files:
            # Separate routing entries may consume the same library file.
            if path != 'config.yaml' and files[path] == value:
                return
            raise MosdnsArchiveError('生成配置包含内容冲突的文件路径')
        total += len(value)
        if len(files) >= MAX_FILES or total > MAX_TOTAL_BYTES:
            raise MosdnsArchiveError('生成配置超过打包大小限制')
        files[path] = value

    add('config.yaml', yaml_content)
    for item in custom_files:
        add(item.get('path'), item.get('content'))

    sources = {}
    file_order = dict.fromkeys(files)
    for item in downloads:
        try:
            path = _path(item.get('local_path'))
            key, conversions = _source(item.get('url'), config, base_url)
        except Exception as exc:
            detail = str(exc) if isinstance(exc, MosdnsArchiveError) else safe_exception_details(exc)
            raise MosdnsArchiveError(f'规则“{_label(item)}”准备失败：{detail}') from None
        file_order.setdefault(path, None)
        if len(file_order) > MAX_FILES:
            raise MosdnsArchiveError('生成配置超过打包文件数量限制')
        sources.setdefault(key, []).append((item, path, conversions))

    def fetch(source):
        # Reuse rule-proxy's bounded transport and cache fallback. The explicit
        # snapshot keeps workers independent of Flask's request context.
        from backend.routes.mosdns import _fetch_remote_content, _load_cached_rule_content_for_url
        kind, value = source
        if kind == 'inline':
            return value
        try:
            try:
                return _fetch_remote_content(value, config_data=config)
            except requests.RequestException as exc:
                cached = _load_cached_rule_content_for_url(value, config_data=config)
                if not cached:
                    raise
                logger.warning('MosDNS ZIP 规则“%s”使用本地缓存：%s',
                               _label(sources[source][0][0]), safe_exception_details(exc))
                return cached
        except Exception as exc:
            raise MosdnsArchiveError(
                f'规则“{_label(sources[source][0][0])}”下载失败：{safe_exception_details(exc)}'
            ) from None

    pool = ThreadPoolExecutor(max_workers=MAX_WORKERS)
    remaining = iter(sources)
    pending = {}

    def schedule():
        while len(pending) < MAX_WORKERS:
            source = next(remaining, None)
            if source is None:
                break
            pending[pool.submit(fetch, source)] = source

    try:
        schedule()
        while pending:
            completed, _ = wait(pending, return_when=FIRST_COMPLETED)
            for future in completed:
                source = pending.pop(future)
                original = future.result()
                for item, path, conversions in sources[source]:
                    try:
                        content = original
                        for part in conversions:
                            content = convert_rule_content_for_mosdns(content, part)
                        add(path, content)
                    except Exception as exc:
                        detail = str(exc) if isinstance(exc, MosdnsArchiveError) else safe_exception_details(exc)
                        raise MosdnsArchiveError(f'规则“{_label(item)}”转换失败：{detail}') from None
            # Bound retained response bodies as well as concurrent sockets.
            # Converted files are checked against the budget before more work.
            del completed, future, original, content
            schedule()
    except Exception:
        # Do not wait for every queued timeout after the package already failed.
        # At most MAX_WORKERS requests remain in flight with bounded timeouts.
        pool.shutdown(wait=False, cancel_futures=True)
        raise
    else:
        pool.shutdown(wait=True)

    document = yaml.safe_load(yaml_content)
    if not isinstance(document, dict) or not isinstance(document.get('plugins', []), list):
        raise MosdnsArchiveError('MosDNS 配置格式无效')
    for plugin in document.get('plugins', []):
        args = plugin.get('args') or {}
        if not isinstance(args, dict):
            continue
        references = args.get('files', [])
        if not isinstance(references, list):
            raise MosdnsArchiveError('MosDNS 文件引用必须为列表')
        if any(_path(path) not in files for path in references):
            raise MosdnsArchiveError('MosDNS 配置引用了未打包的规则文件')

    result = io.BytesIO()
    with zipfile.ZipFile(result, 'w', zipfile.ZIP_DEFLATED) as archive:
        for path in file_order:
            archive.writestr(path, files[path])
    result.seek(0)
    return result
