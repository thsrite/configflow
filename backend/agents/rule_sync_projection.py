"""Project opted-in rule changes onto a successful deployment's source snapshot.

The source snapshot and archive must come from the same successful deployment.
Neither function mutates its inputs. No network requests or repository writes are
made here. Settings are ``{'library': bool, 'rules': bool}``; omitted scopes are off.

Per-rule OxiDNS DNS choices follow their rule's scope, except that a rule newly
pinned to an upstream the published configuration does not have keeps its
published choice: DNS drafts are never published automatically.
"""
from copy import deepcopy
import hashlib
import io
import json
import re
import tarfile
from urllib.parse import parse_qs, urlsplit

import yaml

from backend.common.oxidns_settings import OxidnsSettingsError
from backend.common.rule_dns import (
    BLACKHOLE_PLUGIN, BLACKHOLE_TAG, UPSTREAM_PREFIX, entry_key, hydrate_ruleset, normalize_action,
    parse_exec, pinned_action, upstream_catalog,
)
from .deployment_bundle import (
    DeploymentPreparationError, _local_rule, _pack, deployment_content_fingerprint, relative_path,
)


class RuleSyncProjectionError(ValueError):
    pass


_SELECTORS = ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules')
_RULE_DNS = ('rule_actions', 'rule_dns_follow', 'rule_dns_parked')
_GROUP_TAG = re.compile(r'rules_(?:domain|ip)_\d+(?:_\d+)*\Z')
# Owned rule DNS helpers: rebuilt with the rule groups, never hand-written plugins.
_HELPER_TAG = re.compile(rf'{BLACKHOLE_TAG}|cf_dns_(?:up|fwd|cache)_([0-9a-f]{{12}})')
PINNED_WITHHELD = '指定上游需手动发布'


def _scope(item):
    return 'library' if item.get('itemType') == 'ruleset' else 'rules' if item.get('itemType') == 'rule' else None


def _key_scope(key):
    return 'library' if key.endswith('rulesets') else 'rules'


def _scopes(*configs):
    """Scope of every rule id the configs know, so removed rules also follow their scope."""
    result = {}
    for config in configs:
        for item in config.get('rule_configs', []):
            if isinstance(item, dict) and isinstance(item.get('id'), str):
                result.setdefault(item['id'], _scope(item))
    return result


def _pinned_key(action):
    try:
        action = normalize_action(action, parked=True)
    except OxidnsSettingsError:
        return None
    return entry_key(action['upstream'], action['source']) if action['action'] == 'upstream' else None


def published_upstreams(source):
    """Pinned upstream keys a published source can already resolve: its upstream
    lists and the upstreams its rules are already pinned to."""
    settings = source.get('oxidns') or {}
    try:
        keys = {entry['key'] for entry in upstream_catalog(settings)}
    except OxidnsSettingsError:
        keys = set()
    actions = settings.get('rule_actions')
    keys.update(key for key in map(_pinned_key, actions.values() if isinstance(actions, dict) else ()) if key)
    return keys


def _rule_dns(previous, desired, scoped, published, withheld):
    """Selections with opted-in rules taken from desired.

    Both map the selector lists and rule DNS fields they hold. A rule newly
    pinned to an upstream outside published keeps all its previous choices and
    is added to withheld.
    """
    def field(source, key, kind):
        value = source.get(key)
        return value if isinstance(value, kind) else kind()
    held = set()
    for key in ('rule_actions', 'rule_dns_parked'):
        old = field(previous, key, dict)
        for identity, action in field(desired, key, dict).items():
            pinned = _pinned_key(action) if scoped(identity) and action != old.get(identity) else None
            if pinned is not None and pinned not in published:
                held.add(identity)
    withheld.update(held)
    take = lambda identity: scoped(identity) and identity not in held
    result = {}
    for key in _SELECTORS:
        if key in previous or key in desired:
            chosen = [identity for identity in field(desired, key, list) if identity not in held]
            result[key] = chosen + [identity for identity in field(previous, key, list)
                                    if identity in held and identity not in chosen]
    for key in ('rule_actions', 'rule_dns_parked'):
        if key in previous or key in desired:
            result[key] = {identity: deepcopy(action) for identity, action in field(previous, key, dict).items()
                           if not take(identity)}
            result[key].update((identity, deepcopy(action)) for identity, action in field(desired, key, dict).items()
                               if take(identity))
    if 'rule_dns_follow' in previous or 'rule_dns_follow' in desired:
        follow = [identity for identity in field(previous, 'rule_dns_follow', list) if not take(identity)]
        follow.extend(identity for identity in field(desired, 'rule_dns_follow', list)
                      if take(identity) and identity not in follow)
        result['rule_dns_follow'] = follow
    return result


def _disjoint_follow(follow, actions, parked):
    # Follow only records intent; a rule with an explicit or parked choice does not follow.
    return [identity for identity in follow if identity not in actions and identity not in parked]


def _merge_items(previous, desired, settings):
    """Refill opted-in slots in desired order; inactive items remain anchors.

    Overflow follows the last selected slot. If the old list had no selected
    slot, new entries precede the anchored list, matching the rule editor.
    """
    selected = lambda item: bool(settings.get(_scope(item), False))
    replacements = [deepcopy(item) for item in desired if selected(item)]
    if settings.get('library') and settings.get('rules'):
        return replacements + [deepcopy(item) for item in previous if _scope(item) is None]
    slots = [index for index, item in enumerate(previous) if selected(item)]
    if not slots:
        return replacements + deepcopy(previous)
    output, offset = [], 0
    for index, item in enumerate(previous):
        if selected(item):
            if offset < len(replacements):
                output.append(replacements[offset])
                offset += 1
            if index == slots[-1]:
                output.extend(replacements[offset:])
        else:
            output.append(deepcopy(item))
    return output


def _hydrate(config):
    library = {entry['id']: entry for entry in config.get('rule_library', [])}
    for item in config.get('rule_configs', []):
        if item.get('itemType') != 'ruleset' or not item.get('library_rule_id'):
            continue
        source = library.get(item['library_rule_id'])
        if source is None:
            raise RuleSyncProjectionError('规则集引用的共享来源不存在，请先修复规则引用。')
        hydrate_ruleset(item, source, config.get('profile_id'))
    return config


def _native_projection(base, current, projected, settings, published, withheld):
    """Edit only a recognizable native routing graph, preserving its surroundings."""
    from backend.common.oxidns_native import load_native_yaml
    from backend.common.oxidns_common_routing import _analyze, _references, apply_routing

    original = base.get('oxidns') or {}
    document = load_native_yaml(original.get('native_config', ''))
    before = _analyze(document, original, base)
    keys = [key for key in _SELECTORS if settings.get(_key_scope(key))]
    if not keys:
        return
    # Content-only updates can use existing file references even when a custom
    # routing graph cannot be uniquely associated with editor rows.
    source_before = _source_rule_state(base, include_content=False)
    source_after = _source_rule_state(projected, include_content=False)
    current_oxi = current.get('oxidns') or {}
    same_document = current_oxi.get('native_config') == original.get('native_config')
    if source_before == source_after and same_document:
        return
    if any(not before['capabilities'][key]['editable'] for key in keys):
        raise RuleSyncProjectionError('OxiDNS 原生分流包含无法识别的自定义结构，无法自动应用规则结构；请手动检查并发布。')
    # Native source selectors are a retained resource catalog, not routing truth.
    # If YAML did not change, retain the deployed graph's selections.
    desired = before['values']
    if current_oxi.get('config_mode') == 'native' and not same_document:
        desired_document = load_native_yaml(current_oxi.get('native_config', ''))
        state = _analyze(desired_document, current_oxi, current)
        if any(not state['capabilities'][key]['editable'] for key in keys):
            raise RuleSyncProjectionError('OxiDNS 草稿分流无法唯一对应当前规则，请手动检查并发布。')
        desired = state['values']
    elif current_oxi.get('config_mode') != 'native':
        actions = current_oxi.get('rule_actions') if isinstance(current_oxi.get('rule_actions'), dict) else {}
        # A template's actions win over its union lists.
        desired = {**{key: [identity for identity in current_oxi.get(key, []) if identity not in actions]
                      for key in _SELECTORS}, 'rule_actions': actions}
    desired = {**desired, 'rule_dns_follow': current_oxi.get('rule_dns_follow') or []}
    scopes = _scopes(projected, current, base)
    values = _rule_dns(before['values'], desired, lambda identity: settings.get(scopes.get(identity), False),
                       published, withheld)
    known = _scopes(projected)
    for key in _SELECTORS:
        values[key] = [identity for identity in values[key] if known.get(identity) == _key_scope(key)]
    values['rule_actions'] = {identity: action for identity, action in values['rule_actions'].items()
                              if identity in known}
    values['rule_dns_follow'] = [identity for identity in _disjoint_follow(
        values['rule_dns_follow'], values['rule_actions'], projected['oxidns'].get('rule_dns_parked') or {})
        if identity in known]
    old_groups = [group for entries in before['groups'].values() for group in entries]
    old_tags = {group['tag'] for group in old_groups}
    if any(set(group['provider']) - {'tag', 'type', 'args'} for group in old_groups):
        raise RuleSyncProjectionError('OxiDNS 规则 Provider 包含额外原生参数，请手动检查并发布。')
    for tag in old_tags:
        if (_references({key: value for key, value in document.items() if key != 'plugins'}, tag)
                or _references(before['values'].get('custom_matches', []), tag)
                or any(_references(plugin, tag) for plugin in document['plugins']
                       if plugin.get('tag') not in old_tags | {'sequence_main', 'rules_reload'})):
            raise RuleSyncProjectionError('OxiDNS 规则 Provider 被自定义插件共用，无法安全自动替换；请手动发布。')
    # Only opted-in keys change; values such as an unset aggregate are not editable.
    changes = {**{key: values[key] for key in keys},
               'rule_actions': values['rule_actions'], 'rule_dns_follow': values['rule_dns_follow']}
    try:
        result, result_settings = apply_routing(document, projected['oxidns'], projected, changes, before_config=base)
    except OxidnsSettingsError as error:
        raise RuleSyncProjectionError(f'OxiDNS 规则分流无法自动更新：{str(error).rstrip("。")}；请手动检查并发布。') from None
    # Withdrawing the last IP-only subscription can also withdraw the whole
    # update service. Rule-only publication cannot change that lifecycle, and
    # an empty download/reload plugin is not a runnable intermediate document.
    for plugin in result.get('plugins', []):
        empty_download = (plugin.get('tag') == 'rules_download' and plugin.get('type') == 'download'
                          and isinstance(plugin.get('args'), dict) and plugin['args'].get('downloads') == [])
        empty_reload = (plugin.get('tag') == 'rules_reload' and plugin.get('type') == 'reload_provider'
                        and plugin.get('args') == [])
        if empty_download or empty_reload:
            raise RuleSyncProjectionError(
                '本次规则变更需要停用 OxiDNS 规则下载或重载服务，无法单独自动同步；请手动完整发布。')
    result_settings['native_config'] = yaml.safe_dump(result, allow_unicode=True, sort_keys=False)
    projected['oxidns'] = result_settings


def project_rule_changes(base, current, settings, *, service_type=None, withheld=None):
    """Return the deployed source with only opted-in rule scopes advanced.

    withheld, when a set, receives the ids of rules whose newly pinned upstream
    must be published manually.
    """
    result = deepcopy(base)
    withheld = set() if withheld is None else withheld
    if not settings.get('library') and not settings.get('rules'):
        return result
    if settings.get('library'):
        result['rule_library'] = deepcopy(current.get('rule_library', []))
    result['rule_configs'] = _merge_items(base.get('rule_configs', []), current.get('rule_configs', []), settings)
    _hydrate(result)
    previous, desired = base.get('oxidns') or {}, current.get('oxidns') or {}
    published = published_upstreams(base)
    scopes = _scopes(result, current, base)
    values = _rule_dns({key: previous[key] for key in (*_SELECTORS, *_RULE_DNS) if key in previous},
                       {key: desired[key] for key in (*_SELECTORS, *_RULE_DNS) if key in desired},
                       lambda identity: settings.get(scopes.get(identity), False), published, withheld)
    oxidns = result.setdefault('oxidns', {})
    for key in _SELECTORS:
        if settings.get(_key_scope(key)):
            oxidns[key] = values.get(key, [])
    known = _scopes(result)
    for key in _RULE_DNS:
        if key in values:
            value = values[key]
            oxidns[key] = ({identity: action for identity, action in value.items() if identity in known}
                           if isinstance(value, dict) else [identity for identity in value if identity in known])
    if 'rule_dns_follow' in oxidns:
        oxidns['rule_dns_follow'] = _disjoint_follow(oxidns['rule_dns_follow'], oxidns.get('rule_actions') or {},
                                                     oxidns.get('rule_dns_parked') or {})
    if service_type != 'mihomo' and previous.get('config_mode') == 'native':
        _native_projection(base, current, result, settings, published, withheld)
    return result


def _source_rule_state(config, *, include_content=True):
    state = []
    for item in config.get('rule_configs', []):
        if not item.get('enabled', True) or not item.get('library_enabled', True):
            continue
        if _scope(item) == 'library':
            keys = ('id', 'library_rule_id', 'name', 'url', 'behavior', 'source_type', 'format', 'policy', 'no_resolve')
            if include_content:
                keys += ('content',)
        elif _scope(item) == 'rules':
            keys = ('rule_type', 'value', 'policy', 'no_resolve', 'udp_fallback')
        else:
            continue
        state.append({key: deepcopy(item[key]) for key in keys if key in item})
    return state


def _rule_state(config, service_type, *, include_content):
    config = _hydrate(deepcopy(config))
    if service_type == 'mihomo':
        return _source_rule_state(config, include_content=include_content)
    if service_type != 'oxidns':
        raise RuleSyncProjectionError('规则自动同步仅支持 Mihomo 和 OxiDNS。')
    from backend.converters.oxidns import generate_oxidns_config, _rule_path, _ruleset_parts
    from backend.common.oxidns_native import resource_references, relative_resource
    document = yaml.safe_load(generate_oxidns_config(config))
    referenced = {relative_resource(path) for path in resource_references(document)}
    sources = []
    for item in config.get('rule_configs', []):
        if item.get('itemType') != 'ruleset' or not item.get('enabled', True) or not item.get('library_enabled', True):
            continue
        paths = [relative_resource(_rule_path(item, part)) for part in _ruleset_parts(item.get('behavior', 'domain'))]
        paths = [path for path in paths if path in referenced]
        if not paths:
            continue
        source = {key: deepcopy(item[key]) for key in ('source_type', 'url', 'behavior') if key in item}
        if include_content and item.get('source_type') == 'content':
            source['content'] = item.get('content', '')
        sources.append({'paths': paths, **source})
    return {'document': document, 'sources': sources}


def rule_signature(config, service_type, *, include_content=True):
    """Hash effective consumers, excluding unused library entries and metadata."""
    state = _rule_state(config, service_type, include_content=include_content)
    return hashlib.sha256(json.dumps(state, sort_keys=True, ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()


def rule_change_kind(base, current, service_type, settings):
    """Classify the isolated projection as none, source-content, or structure."""
    projected = project_rule_changes(base, current, settings, service_type=service_type)
    if rule_signature(base, service_type) == rule_signature(projected, service_type):
        return 'none'
    if rule_signature(base, service_type, include_content=False) == rule_signature(projected, service_type, include_content=False):
        return 'content'
    return 'structure'


def rule_source_fingerprint(url, config, base_url, *, depth=0):
    """Describe what a rule URL serves in ``config`` without fetching it.

    Mirrors the resolution of ``materialize_rule``: an external URL stands for
    itself, local callbacks resolve to the rule library entry they serve. Returns
    None when the source cannot be identified, so callers refresh the file.
    """
    from backend.utils.rule_fetch import is_internal_rule_url
    if depth > 3 or not isinstance(url, str) or not url:
        return None
    parsed = urlsplit(url)
    settings = {**config.get('system_config', {}),
                'server_domain': config.get('system_config', {}).get('server_domain') or base_url}
    if parsed.scheme or parsed.netloc:
        if not is_internal_rule_url(url, {'system_config': settings}):
            return ['url', url]
    if parsed.path.endswith(('/mosdns/rule-proxy', '/oxidns/rule-proxy')):
        inner = rule_source_fingerprint(parse_qs(parsed.query).get('url', [''])[0], config, base_url, depth=depth + 1)
        return inner and ['proxy', inner]
    try:
        rule = _local_rule(url, config, base_url)
    except DeploymentPreparationError:
        return None
    # Gateway and other generated callbacks depend on more than one entry.
    if rule is None:
        return None
    shape = [rule.get('format'), rule.get('behavior')]
    if rule.get('source_type') == 'content':
        return ['content', *shape, hashlib.sha256(str(rule.get('content', '')).encode()).hexdigest()]
    source = rule.get('url', '')
    inner = None if source == url else rule_source_fingerprint(source, config, base_url, depth=depth + 1)
    return inner and ['library', *shape, inner]


def unchanged_rule_paths(old_entries, new_entries, base, current, base_url):
    """Rule files whose download URL and served source are identical.

    Entries are ``{'url', 'local_path'}`` download descriptors generated from
    ``base`` and ``current`` respectively. A file absent from either side, or
    with an unidentifiable source, is never reported as unchanged.
    """
    old = {}
    for item in old_entries:
        old.setdefault(relative_path(item['local_path']), set()).add(item.get('url'))
    result = set()
    for item in new_entries:
        path = relative_path(item['local_path'])
        if old.get(path) != {item.get('url')}:
            continue
        before = rule_source_fingerprint(item.get('url'), base, base_url)
        if before is not None and before == rule_source_fingerprint(item.get('url'), current, base_url):
            result.add(path)
    duplicates = [relative_path(item['local_path']) for item in new_entries]
    return {path for path in result if duplicates.count(path) == 1}


def mihomo_rule_entries(document):
    """HTTP rule-provider download descriptors of a Mihomo main document."""
    return [{'name': name, 'url': provider['url'], 'local_path': provider['path']}
            for name, provider in ((document or {}).get('rule-providers') or {}).items()
            if isinstance(provider, dict) and provider.get('type') == 'http'
            and isinstance(provider.get('url'), str) and isinstance(provider.get('path'), str)]


def read_bundle_files(bundle):
    """Read and verify a trusted stored deployment; return path -> (bytes, role)."""
    deployment_content_fingerprint(bundle.manifest)
    if hashlib.sha256(bundle.archive).hexdigest() != bundle.sha256:
        raise DeploymentPreparationError('Stored rule-sync deployment digest mismatch')
    result = {}
    with tarfile.open(fileobj=io.BytesIO(bundle.archive), mode='r:gz') as archive:
        for entry in bundle.manifest['files']:
            member = archive.getmember('files/' + entry['path'])
            if not member.isfile() or member.size != entry['size']:
                raise DeploymentPreparationError('Stored rule-sync deployment file mismatch')
            content = archive.extractfile(member).read()
            if hashlib.sha256(content).hexdigest() != entry['sha256']:
                raise DeploymentPreparationError('Stored rule-sync deployment file digest mismatch')
            result[entry['path']] = (content, entry['role'])
    return result


def _sequence_skeleton(plugin):
    if not isinstance(plugin, dict) or plugin.get('type') != 'sequence' or not isinstance(plugin.get('args'), list):
        raise RuleSyncProjectionError('OxiDNS 主执行链无法安全隔离规则更新。')
    args = plugin['args']
    removable, ip_indices = set(), []
    for index, entry in enumerate(args):
        matches = entry.get('matches') if isinstance(entry, dict) else None
        if isinstance(matches, list) and len(matches) == 1 and isinstance(matches[0], str):
            match = re.fullmatch(r'(qname|resp_ip) \$(rules_(?:domain|ip)_\d+(?:_\d+)*)', matches[0])
            if match and set(entry) == {'matches', 'exec'}:
                # Any rule DNS route: list gotos, rejections and owned helpers.
                # Helper contents are checked separately, so no plugins are needed here.
                if parse_exec(entry['exec'], 'domain' if match[1] == 'qname' else 'ip', {}) is not None:
                    removable.add(index)
                    if match[1] == 'resp_ip':
                        ip_indices.append(index)
    if ip_indices:
        start, end = min(ip_indices), max(ip_indices)
        if (start == 0 or args[start - 1] != {'exec': 'jump local_probe'}
                or ip_indices != list(range(start, end + 1)) or end + 1 >= len(args)
                or args[end + 1] not in ({'exec': 'drop_resp'}, {'matches': ['has_resp'], 'exec': 'goto default_direct_ip'})):
            raise RuleSyncProjectionError('OxiDNS IP 分流区包含自定义结构，无法自动隔离。')
        removable.update((start - 1, end + 1))
    return {**deepcopy(plugin), 'args': [deepcopy(entry) for index, entry in enumerate(args) if index not in removable]}


def _check_helpers(previous, left, right):
    """Changed rule DNS helpers must be canonical and pin only upstreams the
    deployed configuration already has, whatever the candidate's source said."""
    try:
        published = {entry['key'] for entry in upstream_catalog({'config_mode': 'native'}, previous)}
    except OxidnsSettingsError:
        published = set()
    published.update(match[1] for match in map(_HELPER_TAG.fullmatch, left)
                     if match and match[0].startswith(UPSTREAM_PREFIX) and _canonical_chain(match[1], left))
    for tag, plugin in right.items():
        match = _HELPER_TAG.fullmatch(tag)
        if not match or left.get(tag) == plugin:
            continue
        if tag == BLACKHOLE_TAG:
            if plugin != BLACKHOLE_PLUGIN or type(plugin['args']['short_circuit']) is not bool:
                raise RuleSyncProjectionError('OxiDNS 候选配置的黑洞插件不是规则生成的格式。')
        elif not (_canonical_chain(match[1], right) and match[1] in published and (
                tag.startswith(UPSTREAM_PREFIX) or {'exec': f'${tag}'} in right[UPSTREAM_PREFIX + match[1]]['args'])):
            raise RuleSyncProjectionError('OxiDNS 候选配置包含未发布的指定上游，请手动发布。')


def _canonical_chain(key, plugins):
    action = pinned_action(key, plugins)
    return action is not None and entry_key(action['upstream'], action['source']) == key


def _isolate_oxidns_document(previous, candidate):
    left = {plugin['tag']: plugin for plugin in previous.get('plugins', [])}
    right = {plugin['tag']: plugin for plugin in candidate.get('plugins', [])}
    if len(left) != len(previous.get('plugins', [])) or len(right) != len(candidate.get('plugins', [])):
        raise RuleSyncProjectionError('OxiDNS 插件标签重复，无法隔离规则更新。')
    allowed = lambda tag: bool(_GROUP_TAG.fullmatch(tag) or _HELPER_TAG.fullmatch(tag)) or \
        tag in ('sequence_main', 'rules_download', 'rules_reload')
    _check_helpers(previous, left, right)
    if _sequence_skeleton(left.get('sequence_main')) != _sequence_skeleton(right.get('sequence_main')):
        raise RuleSyncProjectionError('OxiDNS 候选配置包含规则范围之外的执行链变化。')
    for tag in set(left) | set(right):
        if allowed(tag):
            continue
        if left.get(tag) != right.get(tag):
            raise RuleSyncProjectionError('OxiDNS 候选配置包含规则范围之外的插件变化。')
    for tag in ('rules_download', 'rules_reload'):
        old, new = left.get(tag), right.get(tag)
        if (old is None) != (new is None):
            raise RuleSyncProjectionError('自动同步不能启用或关闭 OxiDNS 下载和重载服务。')
        if old is not None:
            ignored = ('downloads',) if tag == 'rules_download' else ()
            old_meta, new_meta = deepcopy(old), deepcopy(new)
            if ignored:
                for value in (old_meta, new_meta):
                    value.get('args', {}).pop('downloads', None)
            else:
                old_meta.pop('args', None)
                new_meta.pop('args', None)
            if old_meta != new_meta:
                raise RuleSyncProjectionError('OxiDNS 候选配置改变了下载和重载服务参数。')
    result = deepcopy(previous)
    # Non-rule plugin order is unchanged by normal generation. Reject a reordered
    # candidate rather than change lifecycle ordering of custom plugins.
    if [tag for tag in left if not allowed(tag)] != [tag for tag in right if not allowed(tag)]:
        raise RuleSyncProjectionError('OxiDNS 候选配置改变了非规则插件顺序。')
    result['plugins'] = [deepcopy(plugin if allowed(plugin['tag']) else left[plugin['tag']])
                         for plugin in candidate.get('plugins', [])]
    result_plugins = {plugin['tag']: plugin for plugin in result['plugins']}
    def managed_paths(plugins):
        return {relative_path(path) for tag, plugin in plugins.items() if _GROUP_TAG.fullmatch(tag)
                for path in (plugin.get('args') or {}).get('files', [])}
    old_paths, new_paths = managed_paths(left), managed_paths(right)
    from backend.common.oxidns_native import resource_references, relative_resource
    other_paths = {relative_resource(path) for path in resource_references({'plugins': [
        plugin for tag, plugin in left.items() if not _GROUP_TAG.fullmatch(tag)]})}
    if (old_paths | new_paths) & other_paths:
        raise RuleSyncProjectionError('OxiDNS 规则文件被其他插件共用，无法单独自动更新。')
    if 'rules_download' in result_plugins:
        def location(entry):
            if not isinstance(entry, dict):
                raise RuleSyncProjectionError('OxiDNS 规则下载条目格式无效。')
            return relative_path(str(entry.get('dir', '')).rstrip('/') + '/' + str(entry.get('filename', '')))
        candidate_downloads = right['rules_download']['args']['downloads']
        managed = [deepcopy(entry) for entry in candidate_downloads if location(entry) in new_paths]
        unrelated = [deepcopy(entry) for entry in left['rules_download']['args']['downloads'] if location(entry) not in old_paths]
        result_plugins['rules_download']['args']['downloads'] = managed + unrelated
        managed_refs = [value for value in right['rules_reload']['args']
                        if isinstance(value, str) and _GROUP_TAG.fullmatch(value.removeprefix('$'))]
        other_refs = [value for value in left['rules_reload']['args']
                      if not isinstance(value, str) or not _GROUP_TAG.fullmatch(value.removeprefix('$'))]
        result_plugins['rules_reload']['args'] = deepcopy(managed_refs + other_refs)
    return result


def isolate_rule_bundle(baseline_bundle, candidate_bundle, service_type):
    """Keep deployed non-rule configuration/files byte-for-byte in a candidate.

    Candidate sources must already be projected with ``project_rule_changes``.
    This second boundary also protects against regenerated/fetched node data.
    """
    for field in ('agent_id', 'profile_id', 'service_type'):
        if baseline_bundle.manifest.get(field) != candidate_bundle.manifest.get(field):
            raise RuleSyncProjectionError('规则同步的设备或配置空间已经改变。')
    if candidate_bundle.manifest.get('service_type') != service_type:
        raise RuleSyncProjectionError('规则同步服务类型不匹配。')
    baseline, candidate = read_bundle_files(baseline_bundle), read_bundle_files(candidate_bundle)
    old_path, new_path = baseline_bundle.manifest['config_path'], candidate_bundle.manifest['config_path']
    previous = yaml.safe_load(baseline[old_path][0])
    proposed = yaml.safe_load(candidate[new_path][0])
    if service_type == 'mihomo':
        document = deepcopy(previous)
        for key in ('rules', 'rule-providers'):
            if key in proposed:
                document[key] = deepcopy(proposed[key])
            else:
                document.pop(key, None)
        paths = {relative_path(provider['path']) for provider in document.get('rule-providers', {}).values()
                 if isinstance(provider, dict) and provider.get('path')}
        old_paths = {relative_path(provider['path']) for provider in previous.get('rule-providers', {}).values()
                     if isinstance(provider, dict) and provider.get('path')}
    elif service_type == 'oxidns':
        document = _isolate_oxidns_document(previous, proposed)
        def paths_for(value):
            return {relative_path(path) for plugin in value.get('plugins', []) if _GROUP_TAG.fullmatch(plugin['tag'])
                    for path in (plugin.get('args') or {}).get('files', [])}
        paths, old_paths = paths_for(document), paths_for(previous)
    else:
        raise RuleSyncProjectionError('规则自动同步仅支持 Mihomo 和 OxiDNS。')
    files = {path: value for path, value in baseline.items() if path != old_path and path not in old_paths}
    for path in paths:
        if path in files:
            raise RuleSyncProjectionError('规则文件与非规则文件冲突，无法自动隔离。')
        value = candidate.get(path)
        if value is None or value[1] != 'rule':
            raise RuleSyncProjectionError('候选规则文件缺失或角色不符。')
        files[path] = value
    content = yaml.safe_dump(document, allow_unicode=True, sort_keys=False).encode('utf-8')
    files[old_path] = (content, 'config')
    manifest = deepcopy(candidate_bundle.manifest)
    manifest['config_path'] = old_path
    manifest['files'] = [{'path': path, 'role': role, 'size': len(value), 'sha256': hashlib.sha256(value).hexdigest()}
                         for path, (value, role) in sorted(files.items())]
    deployment_content_fingerprint(manifest)
    return _pack(manifest, files, hashlib.sha256(content).hexdigest()[:8])
