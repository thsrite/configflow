"""Common controls are a projection of the current native draft, never a template.

Only the requested fields are patched. Ambiguous/custom execution paths remain
editable in the complete editor and are explicitly unavailable in this projection.
"""
from copy import deepcopy
import re

import yaml

from backend.common.oxidns_editor import prepare_editor, _same
from backend.common.oxidns_native import load_native_yaml, native_document, relative_resource, resource_references
from backend.common.oxidns_settings import (
    BLOCK_QTYPES_EXEC, BLOCK_QTYPES_EXECS, get_default_oxidns_settings, normalize_oxidns_settings, OxidnsSettingsError,
)
from backend.common.oxidns_references import reference_locations, reference_reason

_MISSING = object()
_DEFAULTS = get_default_oxidns_settings()
_FEATURES = ('query_recorder', 'ip_selector', 'ros_address_list', 'ros_route')
_WRAPPERS = ('china_dns', 'direct_ip', 'default_direct_ip')
# Features that pinned upstream chains of rule DNS copy from china_dns / remote_dns.
_CHAIN_WRAPPERS = ('ip_selector', 'ros_address_list', 'ros_route')
_CHAIN_FIELDS = {'cache_enabled', 'cache_lazy_ttl', 'remote_ttl', *(f'{name}.enabled' for name in _CHAIN_WRAPPERS)}
_TAG_TYPES = {'lazy_cache': 'cache', 'local_probe_cache': 'cache',
              'forward_local': 'forward', 'forward_remote': 'forward', 'forward_fallback': 'forward',
              'fallback': 'fallback', 'dynamic_domains': 'dynamic_domain_set',
              'learn_classified_domain': 'learn_domain', 'adguard_rules': 'adguard_rule',
              'adguard_block': 'black_hole', 'custom_hosts': 'hosts',
              'server_udp': 'udp_server', 'server_tcp': 'tcp_server',
              'rules_download': 'download', 'rules_reload': 'reload_provider',
              'rules_cron': 'cron', **{name: name for name in _FEATURES}}
_DIRECT = {
    'worker_threads': (None, ('runtime', 'worker_threads')),
    'log.level': (None, ('log', 'level')), 'log.file': (None, ('log', 'file')),
    'log.rotation': (None, ('log', 'rotation', 'type')),
    'log.max_files': (None, ('log', 'rotation', 'max_files')),
    'cache_size': ('lazy_cache', ('size',)), 'cache_lazy_ttl': ('lazy_cache', ('lazy_cache_ttl',)),
    'cache_dump_file': ('lazy_cache', ('dump_file',)), 'cache_dump_interval': ('lazy_cache', ('dump_interval',)),
    'probe_cache_size': ('local_probe_cache', ('size',)),
    'fallback_threshold': ('fallback', ('threshold',)), 'always_standby': ('fallback', ('always_standby',)),
    **{f'{name}_concurrent': (f'forward_{name}', ('concurrent',)) for name in ('local', 'remote', 'fallback')},
}


def _leaves(value, prefix=''):
    for key, item in value.items():
        path = f'{prefix}.{key}' if prefix else key
        if isinstance(item, dict):
            yield from _leaves(item, path)
        else:
            yield path, item


for _feature in _FEATURES:
    for _field, _value in _leaves(_DEFAULTS[_feature]['args']):
        _DIRECT[f'{_feature}.args.{_field}'] = (_feature, tuple(_field.split('.')))
for _feature, _tag in (('args', 'dynamic_domains'), ('learn_args', 'learn_classified_domain')):
    for _field, _value in _leaves(_DEFAULTS['dynamic_domain_set'][_feature]):
        _DIRECT[f'dynamic_domain_set.{_feature}.{_field}'] = (_tag, tuple(_field.split('.')))
for _feature in ('ros_address_list', 'ros_route'):
    for _field in ('tls', 'tls.server_name', 'tls.ca', 'tls.insecure'):
        _DIRECT[f'{_feature}.args.{_field}'] = (_feature, tuple(_field.split('.')))
for _field in ('timeout', 'outbound', 'socks5'):
    _DIRECT[f'rule_update.args.{_field}'] = ('rules_download', (_field,))

_API = {'api.listen': ('listen',), 'api.username': ('auth', 'username'),
        'api.password': ('auth', 'password'), 'api.webui_root': ('webui', 'root'),
        'api.ssl.cert': ('ssl', 'cert'), 'api.ssl.key': ('ssl', 'key')}
_SPECIAL = {'listen', 'tcp_enabled', 'prefer_ipv4', 'metrics_enabled', 'block_qtypes', 'remote_ttl',
            'local_dns', 'remote_dns', 'fallback_dns', 'cache_enabled', 'cache_dump_enabled',
            'probe_cache_enabled', 'api.enabled', 'custom_hosts',
            'dynamic_domain_set.enabled', 'dynamic_domain_set.route', 'dynamic_domain_set.learn',
            'adguard.enabled', 'adguard.content', 'adguard.urls', 'adguard.mode',
            'rule_update.enabled', 'rule_update.args.schedule', 'rule_update.args.timezone',
            *(f'{name}.enabled' for name in _FEATURES)}
COMMON_FIELDS = set(_DIRECT) | set(_API) | _SPECIAL


def _get(obj, path):
    for key in path:
        if not isinstance(obj, dict):
            return _MISSING
        obj = obj.get(key, _MISSING)
    return obj


def _put(obj, path, value):
    for key in path[:-1]:
        child = obj.get(key, _MISSING)
        if child is _MISSING or child is None:
            if value is None:
                return
            obj[key] = {}
        elif not isinstance(child, dict):
            raise OxidnsSettingsError('此参数包含自定义值类型，请在完整参数中编辑')
        obj = obj[key]
    if value is None:
        obj.pop(path[-1], None)
    else:
        obj[path[-1]] = deepcopy(value)


def _value(obj, field, value):
    """Projection assignment preserves null, which means an omitted native value."""
    path = field.split('.')
    for key in path[:-1]:
        if not isinstance(obj.get(key), dict):
            obj[key] = {}
        obj = obj[key]
    obj[path[-1]] = deepcopy(value)


def _matches(item):
    value = item.get('matches', []) if isinstance(item, dict) else []
    return [value] if isinstance(value, str) else value if isinstance(value, list) else []


def _reference_count(value, tag):
    if isinstance(value, dict):
        return sum(_reference_count(item, tag) for item in value.values())
    if isinstance(value, list):
        return sum(_reference_count(item, tag) for item in value)
    return int(isinstance(value, str) and value in (f'${tag}', f'jump {tag}', f'goto {tag}'))


def _tag_mentions(value, tag):
    if isinstance(value, dict):
        return sum(_tag_mentions(item, tag) for item in value.values())
    if isinstance(value, list):
        return sum(_tag_mentions(item, tag) for item in value)
    return int(isinstance(value, str) and (re.search(r'(?<!\S)\$' + re.escape(tag) + r'(?!\S)', value) is not None or value in (f'jump {tag}', f'goto {tag}')))


def _safe_parent(obj, path):
    for key in path[:-1]:
        if obj is _MISSING or obj is None:
            return True
        if not isinstance(obj, dict):
            return False
        obj = obj.get(key, _MISSING)
    return obj is _MISSING or obj is None or isinstance(obj, dict)


def _cap(editable=True, reason=None, target=None, absent=False):
    return {'editable': editable, **({'reason': reason} if reason else {}),
            **({'target': target} if target else {}), **({'absent': True} if absent else {})}


def _neutral(value):
    if isinstance(value, dict):
        return {key: _neutral(item) for key, item in value.items()}
    if isinstance(value, list):
        return []
    return False if isinstance(value, bool) else '' if isinstance(value, str) else None


def _owned_chains(document):
    from backend.common.oxidns_common_routing import owned_chains
    return set(owned_chains(document))


class _Context:
    def __init__(self, document, settings):
        self.doc, self.settings = document, settings
        self.plugins = document.get('plugins', [])
        if not isinstance(self.plugins, list) or any(not isinstance(item, dict) or not isinstance(item.get('tag'), str) for item in self.plugins):
            raise OxidnsSettingsError('插件结构尚未完成，请在完整参数或 YAML 中继续编辑')
        self.by_tag = {item.get('tag'): item for item in self.plugins if isinstance(item, dict)}
        self.parked = settings.get('_common_disabled', {})
        if not isinstance(self.parked, dict):
            self.parked = {}
        self.included_tags = set()
        if document.get('include'):
            expanded = native_document(settings, validate_arguments=False)
            self.included_tags = {item['tag'] for item in expanded.get('plugins', [])} - set(self.by_tag)

    def plugin(self, tag):
        return self.by_tag.get(tag)

    def alias_capability(self, tag):
        aliases = self.parked.get('renamed_plugins', {})
        alias = aliases.get(tag) if isinstance(aliases, dict) else None
        if isinstance(alias, str) and alias != tag and self.plugin(alias):
            return _cap(False, f'受管插件已重命名为「{alias}」，请在完整参数中编辑自定义配置', alias)
        return None

    def allowed(self, tag, kind=None):
        alias = self.alias_capability(tag)
        if alias:
            return alias
        if tag in self.included_tags:
            return _cap(False, '此插件来自 include 文件，请在完整参数或附加文件中编辑', tag)
        plugin = self.plugin(tag)
        kind = kind or _TAG_TYPES.get(tag, 'sequence')
        if plugin and plugin.get('type') != kind:
            return _cap(False, '同名插件使用了其他类型，请在完整参数中编辑', tag)
        if plugin and plugin.get('args') is not None and not isinstance(plugin.get('args'), dict):
            return _cap(False, '插件参数使用了自定义值类型，请在完整参数中编辑', tag)
        return _cap(target=tag, absent=not plugin)

    def args(self, tag):
        plugin = self.plugin(tag)
        return ({} if plugin.get('args') is None else plugin['args']) if plugin else {}

    def stored_args(self, tag):
        alias = self.alias_capability(tag)
        if alias and self.plugin(tag) is None:
            return self.args(alias['target'])
        plugin = self.plugin(tag) or self.parked.get(tag)
        if isinstance(plugin, dict) and plugin.get('tag') == tag and plugin.get('type') == _TAG_TYPES.get(tag, 'sequence'):
            return plugin.get('args') or {}
        return self.args(tag)

    def ensure(self, tag, args=None, kind=None):
        kind = kind or _TAG_TYPES.get(tag, 'sequence')
        plugin = self.plugin(tag)
        parked = self.parked.get(tag)
        if plugin is None and isinstance(parked, dict) and parked.get('tag') == tag and parked.get('type') == kind:
            plugin = deepcopy(parked)
            self.doc.setdefault('plugins', []).append(plugin)
            self.by_tag[tag] = plugin
        if plugin is None:
            plugin = {'tag': tag, 'type': kind, 'args': deepcopy({} if args is None else args)}
            self.doc.setdefault('plugins', []).append(plugin)
            self.by_tag[tag] = plugin
        elif plugin.get('type') != kind:
            raise OxidnsSettingsError(f'插件「{tag}」已使用其他类型')
        if plugin.get('args') is None:
            plugin['args'] = deepcopy({} if args is None else args)
        return plugin

    def sequence(self, tag):
        plugin = self.plugin(tag)
        return plugin.get('args') if plugin and plugin.get('type') == 'sequence' and isinstance(plugin.get('args'), list) else None

    def park(self, name, value):
        self.settings.setdefault('_common_disabled', {})[name] = deepcopy(value)
        self.parked = self.settings['_common_disabled']

    def park_plugin(self, tag):
        plugin = self.plugin(tag)
        if plugin is not None:
            self.park(tag, plugin)
            self.doc['plugins'].remove(plugin)
            self.by_tag.pop(tag)

    def resource_paths(self):
        document = self.doc
        if document.get('include'):
            document = native_document({**self.settings, 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}, validate_arguments=False)
        return {relative_resource(path) for path in resource_references(document)}

    def call_state(self, tag, chains):
        capability = self.allowed(tag)
        if not capability['editable']:
            alias = self.alias_capability(tag)
            return bool(alias and reference_locations(self.doc, [alias['target']], dynamic=False)), capability
        if self.doc.get('include'):
            return False, _cap(False, '配置包含外部执行链，请在完整参数中调整功能调用', tag)
        expected = {'exec': f'${tag}'}
        counts = {chain: 0 for chain in chains}
        custom = False
        any_call = False
        # Pinned upstream chains of rule DNS copy china_dns's wrappers and are
        # rebuilt with it, so their calls are neither custom nor a feature switch.
        owned = _owned_chains(self.doc) if tag in _CHAIN_WRAPPERS else set()
        for plugin in self.plugins:
            if not isinstance(plugin.get('args'), list) or plugin.get('tag') in owned:
                continue
            for item in plugin['args']:
                action = item.get('exec') if isinstance(item, dict) else None
                if action in (f'${tag}', f'jump {tag}', f'goto {tag}'):
                    any_call = True
                    if plugin.get('tag') in counts and item == expected:
                        counts[plugin['tag']] += 1
                    else:
                        custom = True
        remaining = deepcopy(self.doc)
        for plugin in remaining.get('plugins', []):
            if (plugin.get('tag') in counts or plugin.get('tag') in owned) and isinstance(plugin.get('args'), list):
                plugin['args'] = [item for item in plugin['args'] if item != expected]
        references = reference_locations(remaining, [tag])
        if references:
            return any_call or any(item['tag'] == tag for item in references), _cap(False, reference_reason(references), tag)
        if custom or any(count > 1 for count in counts.values()) or (any_call and not all(count == 1 for count in counts.values())):
            return any_call, _cap(False, '功能使用了自定义或部分接入的执行链，请在完整参数中调整调用', tag)
        if any(self.sequence(chain) is None for chain in chains):
            return any_call, _cap(False, '当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能', tag)
        return any_call, capability

    def toggle_call(self, tag, chains, enabled, default_args):
        if enabled:
            self.ensure(tag, default_args)
        saved = self.parked.get('feature_calls', {})
        saved = deepcopy(saved) if isinstance(saved, dict) else {}
        for chain in chains:
            items = self.sequence(chain)
            expected = {'exec': f'${tag}'}
            position = saved.get(chain, {})
            position = position if isinstance(position, dict) else {}
            original, disabled = position.get('items', []), position.get('disabled', [])
            original = original if isinstance(original, list) else []
            disabled = disabled if isinstance(disabled, list) else []
            if enabled and expected not in items:
                if tag in disabled and expected in original:
                    following = original[original.index(expected) + 1:]
                    index = next((items.index(item) for item in following if item in items), len(items))
                elif chain == 'sequence_main':
                    index = next((i for i, item in enumerate(items) if item.get('exec') not in ('metrics_collector metrics', '$query_recorder')), len(items))
                elif tag == 'lazy_cache':
                    index = next((i for i, item in enumerate(items) if item.get('exec') == '$forward_local'), 0)
                else:
                    index = 0
                items.insert(index, expected)
                if tag in disabled:
                    disabled.remove(tag)
                if not disabled:
                    saved.pop(chain, None)
            elif not enabled:
                if expected in items:
                    if not disabled:
                        position = {'items': deepcopy(items), 'disabled': []}
                        saved[chain] = position
                    if tag not in position['disabled']:
                        position['disabled'].append(tag)
                items[:] = [item for item in items if item != expected]
        if saved or 'feature_calls' in self.parked:
            self.park('feature_calls', saved)


_FALLBACK_PLUGINS = ('forward_fallback', 'fallback_dns', 'fallback')
_FALLBACK_FIELDS = {'fallback_dns', 'fallback_concurrent',
                    'fallback_threshold', 'always_standby'}
_FALLBACK_ON = [{'exec': '$fallback'}, {'exec': 'accept'}]
_FALLBACK_OFF = [{'exec': 'goto remote_dns'}]
_FALLBACK_SECONDARY = [{'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]
_FALLBACK_REASON = '代理回退使用了自定义执行链、额外引用或 include，请在完整参数中调整，不能自动切换'
_FALLBACK_DYNAMIC_REFERENCE_REASON = '执行链包含未展开的环境变量引用，请在完整参数中确认引用后调整备用 DNS'


def _fallback_has_dynamic_references(value):
    """Environment expansion can hide references from the literal tag scanner.

    Restrict this guard to reference-bearing fields and positional argument
    strings: credentials and forward upstream addresses may still use env vars.
    """
    return bool(reference_locations(value))


def _fallback_state(ctx):
    """Recognize only the owned primary/secondary graph before moving plugins.

    Bare references (for example another fallback's secondary or a server's
    entry) matter as much as $tag calls. The conservative reference scanner also
    protects future/custom plugin arguments that the schema does not yet know.
    """
    from backend.common.oxidns_common_routing import _references
    calls = ctx.sequence('proxy_dns_seq')
    enabled = _same(calls, _FALLBACK_ON)
    for tag in (*_FALLBACK_PLUGINS, 'proxy_dns_seq', 'remote_dns'):
        alias = ctx.alias_capability(tag)
        if alias:
            return enabled, alias
    denied = _cap(False, _FALLBACK_REASON, 'proxy_dns_seq')
    if _fallback_has_dynamic_references(ctx.doc):
        return enabled, _cap(False, _FALLBACK_DYNAMIC_REFERENCE_REASON, 'proxy_dns_seq')
    if (ctx.doc.get('include') or ctx.sequence('remote_dns') is None or
            not any(_same(calls, expected) for expected in
                    (_FALLBACK_ON, _FALLBACK_OFF, [{'exec': '$remote_dns'}, {'exec': 'accept'}]))):
        return enabled, denied
    if any(sum(item.get('tag') == tag for item in ctx.plugins) > 1
           for tag in (*_FALLBACK_PLUGINS, 'proxy_dns_seq', 'remote_dns')):
        return enabled, denied
    kinds = {'forward_fallback': 'forward', 'fallback_dns': 'sequence', 'fallback': 'fallback'}
    for tag in _FALLBACK_PLUGINS:
        plugin = ctx.plugin(tag)
        if enabled and plugin is None:
            return enabled, denied
        plugin = plugin or ctx.parked.get(tag)
        if plugin is None:
            continue
        if not isinstance(plugin, dict) or plugin.get('tag') != tag or plugin.get('type') != kinds[tag]:
            return enabled, denied
        args = plugin.get('args')
        if tag == 'fallback_dns':
            if not _same(args, _FALLBACK_SECONDARY):
                return enabled, denied
        elif not isinstance(args, dict):
            return enabled, denied
        elif tag == 'fallback' and (args.get('primary') != 'remote_dns' or args.get('secondary') != 'fallback_dns'):
            return enabled, denied
        elif tag == 'forward_fallback' and not isinstance(args.get('upstreams', []), list):
            return enabled, denied
    # Remove only the known owned edges, then reject any remaining reference to
    # a plugin that disabling would park. Include top-level extension fields.
    remaining = deepcopy(ctx.doc)
    for plugin in remaining.get('plugins', []):
        tag = plugin.pop('tag', None)
        plugin.pop('type', None)
        if tag == 'proxy_dns_seq':
            plugin.pop('args', None)
        elif tag == 'fallback_dns':
            plugin.pop('args', None)
        elif tag == 'fallback' and isinstance(plugin.get('args'), dict):
            plugin['args'].pop('primary', None)
            plugin['args'].pop('secondary', None)
    if any(_references(remaining, tag) for tag in _FALLBACK_PLUGINS):
        return enabled, denied
    return enabled, _cap(target='proxy_dns_seq')


def _prepare_fallback(ctx, changes):
    if not set(changes) & _FALLBACK_FIELDS:
        return None
    enabled, capability = _fallback_state(ctx)
    if not capability['editable']:
        raise OxidnsSettingsError(_FALLBACK_REASON)
    requested = enabled
    if 'fallback_dns' in changes:
        try:
            value = changes['fallback_dns']
            upstreams = yaml.safe_load(value) if value and value.strip() else []
        except yaml.YAMLError:
            raise OxidnsSettingsError('DNS 上游 YAML 语法无效') from None
        upstreams = [] if upstreams is None else upstreams
        if not isinstance(upstreams, list) or any(not isinstance(item, dict) for item in upstreams):
            raise OxidnsSettingsError('DNS 上游必须为参数对象数组')
        requested = bool(upstreams)
    # Restore before parameter patches, then park again if disabled. This keeps
    # edits to disabled options without accidentally activating an empty forward.
    ctx.ensure('forward_fallback', {'upstreams': [], 'concurrent': 1})
    ctx.ensure('fallback_dns', _FALLBACK_SECONDARY)
    ctx.ensure('fallback', {'primary': 'remote_dns', 'secondary': 'fallback_dns',
                            'threshold': 500, 'always_standby': False})
    return requested


def _finish_fallback(ctx, enabled):
    if enabled is None:
        return
    upstreams = ctx.args('forward_fallback').get('upstreams', [])
    if enabled and (not isinstance(upstreams, list) or not upstreams or any(
            not isinstance(item, dict) or not isinstance(item.get('addr'), str) or not item['addr'].strip()
            for item in upstreams)):
        raise OxidnsSettingsError('启用代理回退前请先填写备用 DNS 地址')
    ctx.plugin('proxy_dns_seq')['args'] = deepcopy(_FALLBACK_ON if enabled else _FALLBACK_OFF)
    if not enabled:
        for tag in _FALLBACK_PLUGINS:
            ctx.park_plugin(tag)


def _direct_read(ctx, values, capabilities):
    for field, (tag, path) in _DIRECT.items():
        holder = ctx.doc if tag is None else ctx.stored_args(tag)
        cap = _cap(target='.'.join(path)) if tag is None else ctx.allowed(tag)
        value = _get(holder, path)
        if not _safe_parent(holder, path):
            cap = _cap(False, '父参数使用了自定义值类型，请在完整参数中编辑', cap.get('target'))
        _value(values, field, None if value is _MISSING else value)
        # A missing field is represented explicitly, rather than inheriting the old template.
        if value is _MISSING:
            holder_value = values
            for key in field.split('.')[:-1]:
                holder_value = holder_value.setdefault(key, {})
            holder_value[field.split('.')[-1]] = None
            cap['absent'] = True
        capabilities[field] = cap


def _main_control(ctx, field):
    sequence = ctx.sequence('sequence_main')
    if sequence is None or ctx.doc.get('include'):
        return None, _cap(False, '当前没有可识别的主处理链，请在完整参数中编辑', 'sequence_main'), None
    if field == 'block_qtypes':
        possible = [(index, item) for index, item in enumerate(sequence) if isinstance(item, dict)
                    and item.get('exec') in BLOCK_QTYPES_EXECS and any(str(match).startswith('qtype ') for match in _matches(item))]
        if not possible:
            return [], _cap(target='sequence_main'), None
        if len(possible) == 1:
            index, item = possible[0]
            matches = item.get('matches')
            if set(item) == {'matches', 'exec'} and len(matches) == 1 and re.fullmatch(r'qtype(?: [0-9]+)+', matches[0]):
                return [int(value) for value in matches[0].split()[1:]], _cap(target='sequence_main'), index
    else:
        command = 'prefer_ipv4' if field == 'prefer_ipv4' else 'metrics_collector metrics'
        prefix = 'prefer_ipv4' if field == 'prefer_ipv4' else 'metrics_collector'
        possible = [(index, item) for index, item in enumerate(sequence) if isinstance(item, dict) and str(item.get('exec', '')).startswith(prefix)]
        if not possible:
            return False, _cap(target='sequence_main'), None
        if len(possible) == 1 and possible[0][1] == {'exec': command}:
            return True, _cap(target='sequence_main'), possible[0][0]
    return None, _cap(False, '当前使用了条件化、重复或自定义指令，请在完整参数中编辑', 'sequence_main'), None


def _api_holder(ctx, restore=False):
    api = ctx.doc.get('api', {})
    if not isinstance(api, dict):
        return api, False
    current = api.get('http')
    return (current if current is not None else ctx.parked.get('api_http', {}) if restore else {}), current is not None


def _dynamic_state(ctx):
    items = ctx.sequence('sequence_main')
    if items is None or ctx.doc.get('include'):
        return False, None, _cap(False, '当前没有可识别的主处理链，请在完整参数中编辑', 'dynamic_domains')
    if any(plugin.get('type') == 'learn_domain' and plugin.get('tag') != 'learn_classified_domain'
           and _get(plugin, ('args', 'provider')) == 'dynamic_domains' for plugin in ctx.plugins):
        return True, None, _cap(False, '动态域名集还被自定义学习器引用，请在完整参数中调整', 'dynamic_domains')
    matches = [item for item in items if isinstance(item, dict) and any('$dynamic_domains' in str(match) for match in _matches(item))]
    remaining = deepcopy(ctx.doc)
    for plugin in remaining.get('plugins', []):
        if plugin.get('tag') == 'sequence_main' and isinstance(plugin.get('args'), list):
            plugin['args'] = [item for item in plugin['args'] if item not in matches]
        if plugin.get('tag') == 'learn_classified_domain' and plugin.get('type') == 'learn_domain' and isinstance(plugin.get('args'), dict):
            plugin['args'].pop('provider', None)
    extra = reference_locations(remaining, ['dynamic_domains'])
    if extra:
        return bool(matches) or any(item['tag'] == 'dynamic_domains' for item in extra), None, _cap(False, reference_reason(extra), 'dynamic_domains')
    references = sum(_tag_mentions(plugin.get('args'), 'dynamic_domains') for plugin in ctx.plugins)
    if references != len(matches):
        return bool(references), None, _cap(False, '动态域名集还被其他执行链引用，请在完整参数中调整', 'dynamic_domains')
    if not matches:
        return False, None, ctx.allowed('dynamic_domains')
    if len(matches) == 1 and matches[0] in ({'matches': ['qname $dynamic_domains'], 'exec': 'goto china_dns'}, {'matches': ['qname $dynamic_domains'], 'exec': 'goto proxy_dns_seq'}):
        return True, 'direct' if matches[0]['exec'] == 'goto china_dns' else 'proxy', ctx.allowed('dynamic_domains')
    return True, None, _cap(False, '动态域名使用了自定义匹配，请在完整参数中编辑', 'dynamic_domains')


def _cron_job(ctx):
    alias = next((cap for tag in ('rules_cron', 'rules_download', 'rules_reload', 'rules_refresh')
                  if (cap := ctx.alias_capability(tag))), None)
    args = ctx.args('rules_cron')
    jobs = args.get('jobs', []) if isinstance(args, dict) else None
    if not isinstance(jobs, list):
        return None, _cap(False, '定时任务使用了自定义值类型，请在完整参数中编辑', 'rules_cron')
    matching = [job for job in jobs if isinstance(job, dict) and (job.get('name') == 'refresh_rules' or '$rules_refresh' in (job.get('executors') if isinstance(job.get('executors'), list) else []))]
    if not matching:
        return None, alias or ctx.allowed('rules_cron')
    if len(matching) != 1 or matching[0].get('name') != 'refresh_rules' or matching[0].get('executors') != ['$rules_refresh']:
        return None, _cap(False, '规则更新任务使用了自定义执行器，请在完整参数中编辑', 'rules_cron')
    return matching[0], alias or ctx.allowed('rules_cron')


def _resources(ctx):
    from backend.converters.oxidns import _tag
    source = ctx.settings.get('adguard')
    source = source if isinstance(source, dict) else {}
    args = ctx.stored_args('adguard_rules')
    files = args.get('files', []) if isinstance(args, dict) else None
    urls = source.get('urls', [])
    urls = urls if isinstance(urls, list) else []
    known = {f'rules/{_tag("adguard", url)}.txt': url for url in urls if isinstance(url, str)}
    if not isinstance(files, list) or any(not isinstance(path, str) for path in files):
        return [], False, _cap(False, '规则文件使用了自定义格式，请在完整参数中编辑', 'adguard_rules')
    paths = [relative_resource(path) for path in files]
    unknown = [path for path in paths if path != 'rules/adguard-custom.txt' and path not in known]
    cap = ctx.allowed('adguard_rules')
    if unknown:
        cap = _cap(False, '存在无法映射到订阅地址的自定义规则文件，请在完整参数中编辑', 'adguard_rules')
    return [known[path] for path in paths if path in known], 'rules/adguard-custom.txt' in paths, cap


def _adguard_state(ctx):
    call = {'matches': ['question $adguard_rules'], 'exec': 'goto adguard_reject'}
    main = ctx.sequence('sequence_main')
    enabled = call in (main or [])
    for tag in ('adguard_rules', 'adguard_reject', 'adguard_block'):
        alias = ctx.alias_capability(tag)
        if alias:
            return bool(reference_locations(ctx.doc, [alias['target']], dynamic=False)), alias
    denied = _cap(False, '自定义过滤链请在完整参数中调整', 'adguard_rules')
    reject = ctx.plugin('adguard_reject')
    if (main is None or ctx.doc.get('include') or
            (reject and (reject.get('type') != 'sequence' or reject.get('args') != [{'exec': '$adguard_block'}, {'exec': 'accept'}])) or
            not ctx.allowed('adguard_block')['editable'] or not ctx.allowed('adguard_rules')['editable']):
        return enabled, denied
    if (main or []).count(call) > 1:
        return enabled, denied
    remaining = deepcopy(ctx.doc)
    for item in remaining.get('plugins', []):
        if item.get('tag') == 'sequence_main':
            item['args'] = [entry for entry in item['args'] if entry != call]
        elif item.get('tag') == 'adguard_reject':
            item.pop('args', None)
        elif item.get('tag') == 'rules_reload' and item.get('type') == 'reload_provider' and isinstance(item.get('args'), list):
            item['args'] = [entry for entry in item['args'] if entry != '$adguard_rules']
    refs = reference_locations(remaining, ['adguard_rules', 'adguard_reject', 'adguard_block'])
    if refs:
        return enabled or any(item['tag'] is not None for item in refs), _cap(False, reference_reason(refs), 'adguard_rules')
    return enabled, _cap(target='adguard_rules')


def _read(document, settings, config):
    ctx = _Context(document, settings)
    values = _neutral(_DEFAULTS)
    # Parked rule DNS choices are routing bookkeeping, not a common control.
    for field in ('config_mode', 'native_config', 'additional_files', 'custom_config', 'rule_dns_parked'):
        values.pop(field, None)
    capabilities = {}
    _direct_read(ctx, values, capabilities)
    for feature in ('ros_address_list', 'ros_route'):
        if _get(ctx.stored_args(feature), ('tls',)) in (_MISSING, None):
            values[feature]['args']['tls'] = None
    for name in ('local', 'remote', 'fallback'):
        tag = f'forward_{name}'
        upstreams = _get(ctx.stored_args(tag) if name == 'fallback' else ctx.args(tag), ('upstreams',))
        cap = ctx.allowed(tag)
        if upstreams is not _MISSING and not isinstance(upstreams, list):
            cap = _cap(False, '上游使用了自定义值类型，请在完整参数中编辑', tag)
        values[f'{name}_dns'] = '' if upstreams is _MISSING else yaml.safe_dump(upstreams, allow_unicode=True, sort_keys=False)
        capabilities[f'{name}_dns'] = cap
    fallback_enabled, fallback_capability = _fallback_state(ctx)
    if not fallback_enabled and fallback_capability['editable']:
        values['fallback_dns'] = ''
    if not fallback_capability['editable']:
        for field in _FALLBACK_FIELDS:
            capabilities[field] = fallback_capability
    udp = ctx.args('server_udp')
    tcp = ctx.plugin('server_tcp')
    values['listen'] = udp.get('listen') if isinstance(udp, dict) else None
    capabilities['listen'] = ctx.allowed('server_udp')
    values['tcp_enabled'] = tcp is not None
    capabilities['tcp_enabled'] = ctx.allowed('server_tcp')
    parked_tcp = ctx.parked.get('server_tcp', {})
    restorable_tcp = (isinstance(parked_tcp, dict) and parked_tcp.get('type') == 'tcp_server'
                      and isinstance(parked_tcp.get('args'), dict) and parked_tcp['args'].get('listen') and parked_tcp['args'].get('entry'))
    if tcp is None and not restorable_tcp and (not ctx.plugin('server_udp') or not ctx.allowed('server_udp')['editable'] or
                                             not isinstance(udp, dict) or not udp.get('listen') or not udp.get('entry')):
        capabilities['tcp_enabled'] = _cap(False, '缺少可复用的 UDP 监听地址和处理入口，请在监听服务中配置 TCP', 'server_tcp')
    if tcp and ctx.plugin('server_udp') and (not isinstance(udp, dict) or not isinstance(tcp.get('args'), dict) or tcp['args'].get('listen') != values['listen'] or tcp['args'].get('entry') != udp.get('entry')):
        capabilities['listen'] = capabilities['tcp_enabled'] = _cap(False, 'UDP 与 TCP 使用独立配置，请在完整参数或监听服务中分别编辑', 'server_tcp')
    for field in ('prefer_ipv4', 'metrics_enabled', 'block_qtypes'):
        values[field], capabilities[field], _ = _main_control(ctx, field)
    for field, tag, chains in [('cache_enabled', 'lazy_cache', ('china_dns',)), ('probe_cache_enabled', 'local_probe_cache', ('local_probe',)),
                               *((f'{name}.enabled', name, ('sequence_main',) if name == 'query_recorder' else _WRAPPERS) for name in _FEATURES)]:
        enabled, cap = ctx.call_state(tag, chains)
        _put(values, field.split('.'), enabled)
        capabilities[field] = cap
    cache = ctx.stored_args('lazy_cache')
    values['cache_dump_enabled'] = bool(isinstance(cache, dict) and cache.get('dump_file'))
    capabilities['cache_dump_enabled'] = ctx.allowed('lazy_cache')
    if not values['cache_dump_enabled'] and isinstance(cache, dict):
        # Disabled options remain editable drafts without leaking into runtime YAML.
        for field in ('cache_dump_file', 'cache_dump_interval'):
            if field in ctx.parked and field.removeprefix('cache_') not in cache:
                values[field] = deepcopy(ctx.parked[field])
    remote = ctx.sequence('remote_dns')
    ttl_items = [(index, item) for index, item in enumerate(remote or []) if isinstance(item, dict) and str(item.get('exec', '')).startswith('ttl ')]
    values['remote_ttl'] = 0
    capabilities['remote_ttl'] = _cap(target='remote_dns')
    if remote is None or len(ttl_items) > 1 or (ttl_items and (set(ttl_items[0][1]) != {'exec'} or not re.fullmatch(r'ttl [0-9]+', ttl_items[0][1]['exec']))):
        values['remote_ttl'] = None
        capabilities['remote_ttl'] = _cap(False, '代理响应 TTL 使用了自定义执行链，请在完整参数中编辑', 'remote_dns')
    elif ttl_items:
        values['remote_ttl'] = int(ttl_items[0][1]['exec'].split()[1])
    api, enabled = _api_holder(ctx)
    values['api']['enabled'] = enabled
    capabilities['api.enabled'] = _cap(isinstance(api, dict), None if isinstance(api, dict) else '管理 API 使用原生简写，请在完整参数中编辑', 'api.http')
    for field, path in _API.items():
        value = _get(api, path)
        editable = enabled and isinstance(api, dict) and _safe_parent(api, path)
        if path[0] == 'auth' and isinstance(api, dict) and isinstance(api.get('auth'), dict) and api['auth'].get('type', 'basic') != 'basic':
            editable = False
        _value(values, field, '' if value is _MISSING else value)
        capabilities[field] = _cap(editable, None if editable else ('请先启用 API / WebUI，再编辑其参数' if not enabled else '管理 API 使用自定义认证或原生值类型，请在完整参数中编辑'), 'api.http', value is _MISSING)
    enabled, route, cap = _dynamic_state(ctx)
    values['dynamic_domain_set'].update(enabled=enabled, route=route)
    capabilities['dynamic_domain_set.enabled'] = capabilities['dynamic_domain_set.route'] = cap
    learn_plugin = ctx.plugin('learn_classified_domain')
    learn_route = 'direct_ip' if route == 'direct' else 'ip_requery_proxy'
    learning, learning_cap = ctx.call_state('learn_classified_domain', (learn_route,))
    if learn_plugin and _get(learn_plugin, ('args', 'provider')) != 'dynamic_domains':
        learning_cap = _cap(False, '学习器引用了其他域名集，请在完整参数中编辑', 'learn_classified_domain')
    values['dynamic_domain_set']['learn'] = learning
    capabilities['dynamic_domain_set.learn'] = learning_cap
    if cap['editable'] and sum(_reference_count(plugin.get('args'), 'learn_classified_domain') for plugin in ctx.plugins) and not learning_cap['editable']:
        capabilities['dynamic_domain_set.enabled'] = capabilities['dynamic_domain_set.route'] = learning_cap
    job, cap = _cron_job(ctx)
    values['rule_update']['enabled'] = job is not None
    capabilities['rule_update.enabled'] = cap
    for key in ('schedule', 'timezone'):
        value = (job or {}).get('schedule') if key == 'schedule' else _get(ctx.args('rules_cron'), ('timezone',))
        values['rule_update']['args'][key] = None if value is _MISSING else value
        capabilities[f'rule_update.args.{key}'] = cap
    urls, has_content, resources_cap = _resources(ctx)
    source = settings.get('adguard') if isinstance(settings.get('adguard'), dict) else {}
    values['adguard'].update(urls=urls, content=source.get('content', '') if has_content else '', mode=_get(ctx.stored_args('adguard_block'), ('mode',)))
    if values['adguard']['mode'] is _MISSING:
        values['adguard']['mode'] = None
    for field in ('adguard.urls', 'adguard.content'):
        capabilities[field] = resources_cap
    capabilities['adguard.mode'] = ctx.allowed('adguard_block')
    values['adguard']['enabled'], capabilities['adguard.enabled'] = _adguard_state(ctx)
    hosts = ctx.args('custom_hosts')
    files = hosts.get('files', []) if isinstance(hosts, dict) else None
    paths = files if isinstance(files, list) else []
    values['custom_hosts'] = settings.get('custom_hosts', '') if './rules/custom_hosts.txt' in paths or 'rules/custom_hosts.txt' in paths else ''
    _, capabilities['custom_hosts'] = ctx.call_state('custom_hosts', ('sequence_main',))
    if ctx.sequence('sequence_main') is None or not isinstance(files, list) or (paths and paths not in (['./rules/custom_hosts.txt'], ['rules/custom_hosts.txt'])):
        capabilities['custom_hosts'] = _cap(False, 'Hosts 使用自定义文件或处理链，请在完整参数中编辑', 'custom_hosts')
    try:
        from backend.common.oxidns_common_routing import read_routing
    except ImportError:
        read_routing = None
    if read_routing:
        routing = read_routing(document, settings, config)
        values.update(routing['values'])
        capabilities.update(routing['capabilities'])
    from backend.common.oxidns_match_targets import read_match_targets
    return {'values': values, 'capabilities': capabilities, **read_match_targets(document, settings)}


def read_common_settings(settings, config, base_url=''):
    prepared = prepare_editor(settings, config, base_url)
    return _read(load_native_yaml(prepared['native_config']), prepared, config)


def _validate_value(field, value):
    if value is None:
        return
    example = _get(_DEFAULTS, field.split('.'))
    if field.endswith('.tls'):
        if not isinstance(value, dict):
            raise OxidnsSettingsError(f'{field} 必须是 TLS 参数对象或 null')
    elif field.endswith('.tls.insecure') or isinstance(example, bool):
        if type(value) is not bool:
            raise OxidnsSettingsError(f'{field} 必须为布尔值')
    elif isinstance(example, int):
        if type(value) is not int:
            raise OxidnsSettingsError(f'{field} 必须为整数')
    elif isinstance(example, list):
        if not isinstance(value, list):
            raise OxidnsSettingsError(f'{field} 必须为数组')
        if field == 'block_qtypes' and any(type(item) is not int for item in value):
            raise OxidnsSettingsError('屏蔽查询类型必须为整数数组')
    elif isinstance(example, str) or '.tls.' in field or field in ('api.ssl.cert', 'api.ssl.key'):
        if not isinstance(value, str):
            raise OxidnsSettingsError(f'{field} 必须为文本')


def _default_args(tag):
    if tag in _FEATURES:
        return deepcopy(_DEFAULTS[tag]['args'])
    if tag == 'lazy_cache':
        return {'size': 10240, 'lazy_cache_ttl': 3600, 'short_circuit': True}
    if tag == 'local_probe_cache':
        return {'size': 2048, 'short_circuit': False}
    if tag == 'dynamic_domains':
        return deepcopy(_DEFAULTS['dynamic_domain_set']['args'])
    if tag == 'learn_classified_domain':
        return {**deepcopy(_DEFAULTS['dynamic_domain_set']['learn_args']), 'provider': 'dynamic_domains'}
    return {}


def _patch_direct(ctx, field, value):
    tag, path = _DIRECT[field]
    holder = ctx.doc if tag is None else ctx.ensure(tag, _default_args(tag))['args']
    if field == 'cache_lazy_ttl' and value == 0:
        value = None
    if field in ('log.file', 'rule_update.args.timeout', 'rule_update.args.outbound', 'rule_update.args.socks5') and value == '':
        value = None
    if field == 'log.max_files' and value is not None and _get(holder, ('log', 'rotation', 'type')) in (_MISSING, None):
        _put(holder, ('log', 'rotation', 'type'), 'never')
    _put(holder, path, value)
    if field == 'log.rotation' and value is None:
        rotation = _get(holder, ('log', 'rotation'))
        if isinstance(rotation, dict):
            if rotation:
                rotation['type'] = 'never'
            else:
                holder['log'].pop('rotation')


def _patch_api(ctx, field, value):
    current, enabled = _api_holder(ctx, restore=True)
    if field == 'api.enabled':
        if value:
            ctx.doc.setdefault('api', {})['http'] = deepcopy(current) if current else {'listen': _DEFAULTS['api']['listen']}
        elif enabled:
            ctx.park('api_http', current)
            ctx.doc['api'].pop('http', None)
            if not ctx.doc['api']:
                ctx.doc.pop('api')
        return
    if enabled:
        holder = ctx.doc['api']['http']
    else:
        ctx.settings.setdefault('_common_disabled', {}).setdefault('api_http', deepcopy(current))
        holder = ctx.settings['_common_disabled']['api_http']
    path = _API[field]
    if path[0] in ('auth', 'webui') and value not in ('', None) and path[0] not in holder:
        saved = ctx.parked.get('api_' + path[0])
        if isinstance(saved, dict):
            holder[path[0]] = deepcopy(saved)
    _put(holder, path, None if value == '' and field not in ('api.listen',) else value)
    if path[0] == 'auth' and isinstance(holder.get('auth'), dict):
        if holder['auth']:
            holder['auth'].setdefault('type', 'basic')
        if not holder['auth'].get('username') and not holder['auth'].get('password'):
            ctx.park('api_auth', holder['auth'])
            holder.pop('auth')
    if path[0] == 'webui' and holder.get('webui', {}).get('root'):
        holder['webui'].setdefault('index', 'index.html')
    elif path[0] == 'webui' and isinstance(holder.get('webui'), dict):
        ctx.park('api_webui', holder.pop('webui'))


def _patch_cache_dump(ctx, changes):
    if not any(field in changes for field in ('cache_dump_enabled', 'cache_dump_file', 'cache_dump_interval')):
        return
    args = ctx.ensure('lazy_cache', _default_args('lazy_cache'))['args']
    enabled = changes.get('cache_dump_enabled', bool(args.get('dump_file')))
    if args.get('dump_file'):
        ctx.park('cache_dump_file', args['dump_file'])
    if args.get('dump_file') or 'dump_interval' in args:
        # None also preserves an explicitly omitted interval (the core default).
        ctx.park('cache_dump_interval', args.get('dump_interval'))
    for field in ('cache_dump_file', 'cache_dump_interval'):
        if field in changes:
            ctx.park(field, changes[field])
    if enabled:
        path = changes.get('cache_dump_file', args.get('dump_file') or ctx.parked.get('cache_dump_file') or _DEFAULTS['cache_dump_file'])
        enabled = bool(isinstance(path, str) and path.strip())
        if enabled:
            args['dump_file'] = path
            interval = ctx.parked.get('cache_dump_interval', _DEFAULTS['cache_dump_interval'])
            _put(args, ('dump_interval',), interval)
    if not enabled:
        args.pop('dump_file', None)
        args.pop('dump_interval', None)


def _patch_tls(ctx, changes):
    for feature in ('ros_address_list', 'ros_route'):
        field = f'{feature}.args.tls'
        if not any(key == field or key.startswith(field + '.') for key in changes):
            continue
        args = ctx.ensure(feature, _default_args(feature))['args']
        current = args.get('tls')
        enabled = isinstance(changes[field], dict) if field in changes else isinstance(current, dict)
        saved = current if isinstance(current, dict) else ctx.parked.get(field, {})
        value = deepcopy(saved) if isinstance(saved, dict) else {}
        if isinstance(changes.get(field), dict):
            value.update(deepcopy(changes[field]))
        for key, item in changes.items():
            if key.startswith(field + '.'):
                _put(value, key[len(field) + 1:].split('.'), item)
        ctx.park(field, value)
        _put(args, ('tls',), value if enabled else None)


def _patch_main(ctx, field, value):
    _, _, index = _main_control(ctx, field)
    items = ctx.sequence('sequence_main')
    replacement = None
    if value:
        replacement = {'matches': ['qtype ' + ' '.join(map(str, value))], 'exec': BLOCK_QTYPES_EXEC} if field == 'block_qtypes' else {'exec': 'prefer_ipv4' if field == 'prefer_ipv4' else 'metrics_collector metrics'}
    if index is not None:
        if replacement:
            items[index] = replacement
        else:
            items.pop(index)
    elif replacement:
        prefix = ('metrics_collector metrics', '$query_recorder', 'prefer_ipv4')
        index = 0 if field == 'metrics_enabled' else next((i for i, item in enumerate(items) if item.get('exec') not in prefix), len(items))
        items.insert(index, replacement)


def _patch_dynamic(ctx, changes):
    enabled, route, _ = _dynamic_state(ctx)
    parked = ctx.parked.get('dynamic_domain_set_state', {})
    parked = parked if isinstance(parked, dict) else {}
    next_enabled = changes.get('dynamic_domain_set.enabled', enabled)
    next_route = changes.get('dynamic_domain_set.route', route or parked.get('route', 'direct'))
    if next_route not in ('direct', 'proxy'):
        raise OxidnsSettingsError('动态域名路由必须为 direct 或 proxy')
    old_chains = [name for name in ('direct_ip', 'ip_requery_proxy') if {'exec': '$learn_classified_domain'} in (ctx.sequence(name) or [])]
    learn = changes.get('dynamic_domain_set.learn', bool(old_chains) if enabled else parked.get('learn', False))
    if any(field in changes for field in ('dynamic_domain_set.enabled', 'dynamic_domain_set.route', 'dynamic_domain_set.learn')):
        ctx.park('dynamic_domain_set_state', {'route': next_route, 'learn': learn})
    if next_enabled:
        ctx.ensure('dynamic_domains', _default_args('dynamic_domains'))
    if any(field in changes for field in ('dynamic_domain_set.enabled', 'dynamic_domain_set.route')):
        main = ctx.sequence('sequence_main')
        previous = next((index for index, item in enumerate(main) if item.get('matches') == ['qname $dynamic_domains']), None)
        if previous is not None:
            main.pop(previous)
        if next_enabled:
            index = previous if previous is not None else next((i for i, item in enumerate(main) if item.get('exec') == 'jump local_probe'), max(0, len(main) - 1))
            main.insert(index, {'matches': ['qname $dynamic_domains'], 'exec': 'goto china_dns' if next_route == 'direct' else 'goto proxy_dns_seq'})
    if any(field in changes for field in ('dynamic_domain_set.enabled', 'dynamic_domain_set.route', 'dynamic_domain_set.learn')):
        for chain in old_chains:
            ctx.sequence(chain)[:] = [item for item in ctx.sequence(chain) if item != {'exec': '$learn_classified_domain'}]
        if next_enabled and learn:
            chain = ctx.sequence('direct_ip' if next_route == 'direct' else 'ip_requery_proxy')
            if chain is None:
                raise OxidnsSettingsError('缺少动态学习的目标执行链，请在完整参数中配置')
            ctx.ensure('learn_classified_domain', _default_args('learn_classified_domain'))
            chain.insert(max(0, len(chain) - 1), {'exec': '$learn_classified_domain'})


def _park_disabled_features(ctx, changes):
    controls = [('cache_', 'lazy_cache', ('china_dns',)), ('probe_cache_', 'local_probe_cache', ('local_probe',)),
                *((name + '.', name, ('sequence_main',) if name == 'query_recorder' else _WRAPPERS) for name in _FEATURES)]
    for prefix, tag, chains in controls:
        if any(field.startswith(prefix) for field in changes):
            enabled, cap = ctx.call_state(tag, chains)
            if not enabled and cap['editable']:
                ctx.park_plugin(tag)
    if any(field.startswith('dynamic_domain_set.') for field in changes):
        _, _, cap = _dynamic_state(ctx)
        if not cap['editable']:
            return
        if not reference_locations(ctx.doc, ['learn_classified_domain']):
            ctx.park_plugin('learn_classified_domain')
        enabled, _, cap = _dynamic_state(ctx)
        if not enabled and cap['editable']:
            ctx.park_plugin('dynamic_domains')


def _patch_rule_update(ctx, changes, config, base_url):
    if ctx.plugin('rules_cron') is None and (changes.get('rule_update.enabled') is True or 'rule_update.args.timezone' in changes):
        parked = ctx.parked.get('rules_cron')
        if (isinstance(parked, dict) and parked.get('tag') == 'rules_cron' and parked.get('type') == 'cron'
                and isinstance(parked.get('args'), dict) and parked['args'].get('jobs') == []):
            restored = deepcopy(parked)
            ctx.doc.setdefault('plugins', []).append(restored)
            ctx.by_tag['rules_cron'] = restored
    job, _ = _cron_job(ctx)
    if changes.get('rule_update.enabled') is True and job is None:
        source = deepcopy(config)
        source['oxidns'] = {**ctx.settings, 'native_config': yaml.safe_dump(ctx.doc, allow_unicode=True, sort_keys=False)}
        # Drafts can contain unfinished unrelated listener fields. Download metadata
        # is assembled without asking the full config generator to validate them.
        from backend.common.oxidns_common_routing import resource_downloads
        downloads = resource_downloads(ctx.doc, source['oxidns'], config, base_url)
        if not downloads:
            raise OxidnsSettingsError('规则定时更新需要当前配置引用至少一个可下载订阅')
        defaults = _DEFAULTS['rule_update']['args']
        downloader = ctx.ensure('rules_download', {'timeout': defaults['timeout'], 'downloads': downloads})
        if not isinstance(downloader.get('args'), dict):
            raise OxidnsSettingsError('下载插件使用自定义参数，请在完整参数中编辑')
        downloader['args'].setdefault('downloads', downloads)
        providers = [f'${item["tag"]}' for item in ctx.doc.get('plugins', []) if item.get('type') in ('domain_set', 'ip_set', 'adguard_rule') and isinstance(item.get('args'), dict) and item['args'].get('files')]
        ctx.ensure('rules_reload', providers, 'reload_provider')
        ctx.ensure('rules_refresh', [{'exec': '$rules_download'}, {'exec': '$rules_reload'}], 'sequence')
        cron = ctx.ensure('rules_cron', {'timezone': defaults['timezone'], 'jobs': []})
        job = deepcopy(ctx.parked.get('rules_cron_job', {'name': 'refresh_rules', 'schedule': defaults['schedule'], 'executors': ['$rules_refresh']}))
        cron['args'].setdefault('jobs', []).append(job)
    if changes.get('rule_update.enabled') is False and job is not None:
        ctx.park('rules_cron_job', job)
        ctx.args('rules_cron')['jobs'].remove(job)
        job = None
    if 'rule_update.args.schedule' in changes:
        if job is not None:
            job['schedule'] = changes['rule_update.args.schedule']
        else:
            holder = ctx.settings.setdefault('_common_disabled', {}).setdefault('rules_cron_job', {'name': 'refresh_rules', 'executors': ['$rules_refresh']})
            holder['schedule'] = changes['rule_update.args.schedule']
    if 'rule_update.args.timezone' in changes:
        _put(ctx.ensure('rules_cron', {'jobs': []})['args'], ('timezone',), changes['rule_update.args.timezone'])
    # OxiDNS requires at least one cron job. Keep disabled settings outside the
    # runnable document, but leave any unrelated jobs and their plugin intact.
    cron = ctx.plugin('rules_cron')
    if cron and cron.get('type') == 'cron' and isinstance(cron.get('args'), dict) and cron['args'].get('jobs') == []:
        ctx.park('rules_cron', cron)
        ctx.doc['plugins'].remove(cron)
        ctx.by_tag.pop('rules_cron')


def _patch_resources(ctx, changes, config, base_url):
    from backend.converters.oxidns import _tag, _hosts_file_content
    if 'custom_hosts' in changes:
        # A direct resource edit supersedes content parked by native deletion.
        # Re-adding the feature must not revive a mapping the user withdrew.
        ctx.parked.pop('native_hosts_source', None)
        content = changes['custom_hosts'] or ''
        previous_content = ctx.settings.get('custom_hosts', '')
        if content.strip():
            _hosts_file_content(content)
            ctx.ensure('custom_hosts', {'files': ['./rules/custom_hosts.txt']})['args']['files'] = ['./rules/custom_hosts.txt']
            main = ctx.sequence('sequence_main')
            if {'exec': '$custom_hosts'} not in main:
                index = next((i for i, item in enumerate(main) if item.get('exec') not in ('metrics_collector metrics', '$query_recorder', 'prefer_ipv4') and not str(item.get('matches', [''])[0] if item.get('matches') else '').startswith('qtype ')), 0)
                main[index:index] = [{'exec': '$custom_hosts'}, {'matches': ['has_resp'], 'exec': 'accept'}]
        else:
            if ctx.plugin('custom_hosts'):
                args = ctx.args('custom_hosts')
                args['files'] = []
                # The common field owns the managed file, not native inline rules.
                if args.get('entries') in (None, []):
                    if args.get('entries') is None:
                        args.pop('entries', None)
                    main = ctx.sequence('sequence_main')
                    index = next((i for i, item in enumerate(main) if item == {'exec': '$custom_hosts'}), None)
                    if index is not None:
                        main.pop(index)
                        if index < len(main) and main[index] == {'matches': ['has_resp'], 'exec': 'accept'}:
                            main.pop(index)
                    if not reference_locations(ctx.doc, ['custom_hosts']):
                        ctx.park_plugin('custom_hosts')
        referenced = ctx.resource_paths()
        ctx.settings['custom_hosts'] = previous_content if not content.strip() and 'rules/custom_hosts.txt' in referenced else content
    if not any(field.startswith('adguard.') for field in changes):
        return
    urls, has_content, _ = _resources(ctx)
    resource = ctx.settings.setdefault('adguard', {})
    if not isinstance(resource, dict):
        ctx.settings['adguard'] = resource = {}
    old_resource = deepcopy(resource)
    urls = changes.get('adguard.urls', urls)
    content = changes.get('adguard.content', resource.get('content', '') if has_content else '') or ''
    if changes.get('adguard.enabled') and isinstance(ctx.parked.get('adguard_rules'), dict):
        ctx.ensure('adguard_rules')
    if any(not isinstance(url, str) or not re.match(r'^https?://', url) for url in urls):
        raise OxidnsSettingsError('AdGuard 订阅必须为 HTTP(S) URL')
    if any(field in changes for field in ('adguard.urls', 'adguard.content')) or (changes.get('adguard.enabled') and not ctx.plugin('adguard_rules')):
        provider = ctx.ensure('adguard_rules', {'files': []})
        provider['args']['files'] = (['./rules/adguard-custom.txt'] if content.strip() else []) + [f'./rules/{_tag("adguard", url)}.txt' for url in urls]
        resource.update(urls=urls, content=content, enabled=True)
        # A separate manually wired provider may still consume an old source.
        # Keep its source bytes/URL metadata while the common card reflects only
        # the files attached to adguard_rules.
        referenced = ctx.resource_paths()
        for previous_url in old_resource.get('urls', []):
            if isinstance(previous_url, str) and f'rules/{_tag("adguard", previous_url)}.txt' in referenced and previous_url not in resource['urls']:
                resource['urls'].append(previous_url)
        if not content.strip() and 'rules/adguard-custom.txt' in referenced:
            resource['content'] = old_resource.get('content', '')
    if 'adguard.mode' in changes:
        if changes['adguard.mode'] not in ('nxdomain', 'nodata', 'null', 'refused'):
            raise OxidnsSettingsError('AdGuard 响应模式无效')
        ctx.ensure('adguard_block', {'mode': 'nxdomain'})['args']['mode'] = changes['adguard.mode']
    if 'adguard.enabled' in changes:
        main = ctx.sequence('sequence_main')
        call = {'matches': ['question $adguard_rules'], 'exec': 'goto adguard_reject'}
        if changes['adguard.enabled']:
            ctx.ensure('adguard_block', {'mode': 'nxdomain'})
            ctx.ensure('adguard_reject', [{'exec': '$adguard_block'}, {'exec': 'accept'}], 'sequence')
            if call not in main:
                index = next((i for i, item in enumerate(main) if str(item.get('exec', '')).startswith(('goto ', 'jump '))), max(0, len(main) - 1))
                main.insert(index, call)
        else:
            main[:] = [item for item in main if item != call]
    active, capability = _adguard_state(ctx)
    if not active and capability['editable']:
        for tag in ('adguard_rules', 'adguard_reject', 'adguard_block'):
            ctx.park_plugin(tag)
        reload = ctx.plugin('rules_reload')
        if reload and reload.get('type') == 'reload_provider' and isinstance(reload.get('args'), list):
            reload['args'] = [entry for entry in reload['args'] if entry != '$adguard_rules']
    if any(field in changes for field in ('adguard.urls', 'adguard.content', 'adguard.enabled')) and ctx.plugin('rules_download'):
        from backend.common.oxidns_common_routing import resource_downloads, replace_download_entries
        downloader = ctx.plugin('rules_download')
        reload = ctx.plugin('rules_reload')
        if downloader.get('type') != 'download' or not isinstance(downloader.get('args'), dict) or not isinstance(downloader['args'].get('downloads'), list) or (reload and (reload.get('type') != 'reload_provider' or not isinstance(reload.get('args'), list))):
            raise OxidnsSettingsError('规则更新使用自定义下载/重载结构，请在完整参数中调整')
        known = {f'rules/{_tag("adguard", url)}.txt' for url in [*old_resource.get('urls', []), *resource.get('urls', [])] if isinstance(url, str)}
        def local_path(entry):
            return (entry['dir'].rstrip('/') + '/' + entry['filename']).removeprefix('./') if isinstance(entry, dict) and isinstance(entry.get('dir'), str) and isinstance(entry.get('filename'), str) else None
        desired = {local_path(entry): entry for entry in resource_downloads(ctx.doc, ctx.settings, config, base_url) if local_path(entry) in known}
        output = []
        for entry in downloader['args']['downloads']:
            location = local_path(entry)
            if location not in known:
                output.append(entry)
            elif location in desired:
                output.append(entry)
                desired.pop(location)
        replace_download_entries(downloader, output + list(desired.values()))
        if reload and ctx.plugin('adguard_rules') and '$adguard_rules' not in reload['args']:
            reload['args'].append('$adguard_rules')


def apply_common_settings(settings, changes, config, base_url=''):
    if not isinstance(changes, dict) or not changes:
        raise OxidnsSettingsError('changes 必须为非空参数对象')
    from backend.common.oxidns_common_routing import ROUTING_FIELDS, apply_routing
    unknown = set(changes) - COMMON_FIELDS - ROUTING_FIELDS
    if unknown:
        raise OxidnsSettingsError('不支持的常用参数：' + ', '.join(sorted(unknown)))
    current = prepare_editor(settings, config, base_url)
    document = load_native_yaml(current['native_config'])
    projection = _read(document, current, config)
    for field, value in changes.items():
        capability = projection['capabilities'].get(field, {})
        enabling_api = field in _API and changes.get('api.enabled') is True and projection['capabilities']['api.enabled']['editable']
        if not capability.get('editable') and not enabling_api:
            raise OxidnsSettingsError(f'{field}：' + capability.get('reason', '请在完整参数中编辑'))
        _validate_value(field, value)
    before = deepcopy(document)
    routing_changes = {field: value for field, value in changes.items() if field in ROUTING_FIELDS}
    if routing_changes:
        document, current = apply_routing(document, current, config, routing_changes, base_url)
    ctx = _Context(document, current)
    fallback_enabled = _prepare_fallback(ctx, changes)
    if 'api.enabled' in changes:
        _patch_api(ctx, 'api.enabled', changes['api.enabled'])
    # Enable first so simultaneous argument edits modify the newly created node.
    for field, tag, chains in [('cache_enabled', 'lazy_cache', ('china_dns',)), ('probe_cache_enabled', 'local_probe_cache', ('local_probe',)),
                               *((f'{name}.enabled', name, ('sequence_main',) if name == 'query_recorder' else _WRAPPERS) for name in _FEATURES)]:
        if field in changes:
            ctx.toggle_call(tag, chains, changes[field], _default_args(tag))
    if any(field.startswith('dynamic_domain_set.') for field in changes):
        _patch_dynamic(ctx, changes)
    _patch_resources(ctx, changes, config, base_url)
    from backend.common.oxidns_common_updates import prepare_rule_update
    prepare_rule_update(ctx, changes, config, base_url)
    if any(field.startswith('rule_update.') for field in changes):
        _patch_rule_update(ctx, changes, config, base_url)
    _patch_cache_dump(ctx, changes)
    _patch_tls(ctx, changes)
    for field, value in changes.items():
        if field in ('cache_dump_enabled', 'cache_dump_file', 'cache_dump_interval') or any(
                field == f'{name}.args.tls' or field.startswith(f'{name}.args.tls.') for name in ('ros_address_list', 'ros_route')):
            continue
        if field in _DIRECT:
            _patch_direct(ctx, field, value)
        elif field in _API:
            _patch_api(ctx, field, value)
        elif field in ('prefer_ipv4', 'metrics_enabled', 'block_qtypes'):
            _patch_main(ctx, field, value)
        elif field in ('local_dns', 'remote_dns', 'fallback_dns'):
            try:
                upstreams = yaml.safe_load(value) if value and value.strip() else []
            except yaml.YAMLError:
                raise OxidnsSettingsError('DNS 上游 YAML 语法无效') from None
            if field == 'fallback_dns' and upstreams is None:
                upstreams = []
            if not isinstance(upstreams, list) or any(not isinstance(item, dict) for item in upstreams):
                raise OxidnsSettingsError('DNS 上游必须为参数对象数组')
            ctx.ensure('forward_' + field.split('_')[0])['args']['upstreams'] = upstreams
        elif field == 'listen':
            ctx.ensure('server_udp', {'entry': 'sequence_main'})['args']['listen'] = value
            if ctx.plugin('server_tcp'):
                ctx.args('server_tcp')['listen'] = value
        elif field == 'tcp_enabled':
            if value:
                args = deepcopy(ctx.args('server_udp'))
                parked = ctx.parked.get('server_tcp')
                if not ctx.plugin('server_tcp') and isinstance(parked, dict) and parked.get('type') == 'tcp_server':
                    restored = deepcopy(parked)
                    restored['args'].update({key: args[key] for key in ('listen', 'entry') if key in args})
                    ctx.doc.setdefault('plugins', []).append(restored)
                    ctx.by_tag['server_tcp'] = restored
                else:
                    ctx.ensure('server_tcp', args)
            elif ctx.plugin('server_tcp'):
                ctx.park('server_tcp', ctx.plugin('server_tcp'))
                ctx.doc['plugins'].remove(ctx.plugin('server_tcp'))
                ctx.by_tag.pop('server_tcp')
        elif field == 'remote_ttl':
            items = ctx.sequence('remote_dns')
            index = next((i for i, item in enumerate(items) if str(item.get('exec', '')).startswith('ttl ')), None)
            if index is not None:
                if value:
                    items[index] = {'exec': f'ttl {value}'}
                else:
                    items.pop(index)
            elif value:
                items.append({'exec': f'ttl {value}'})
    if _CHAIN_FIELDS & set(changes):
        from backend.common.oxidns_common_routing import refresh_rule_helpers
        refresh_rule_helpers(ctx.doc)
        ctx.by_tag = {item.get('tag'): item for item in ctx.doc['plugins'] if isinstance(item, dict)}
    _finish_fallback(ctx, fallback_enabled)
    _park_disabled_features(ctx, changes)
    from backend.common.oxidns_common_updates import finish_rule_updates
    finish_rule_updates(ctx, changes, config, base_url)
    if not _same(document, before):
        current['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    normalize_oxidns_settings(current, validate_native_arguments=False)
    return {'settings': current, **_read(document, current, config)}
