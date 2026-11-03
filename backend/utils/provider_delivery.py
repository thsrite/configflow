"""Request-local provider validation before publishing node caches or artifacts."""
from dataclasses import dataclass
import json

from backend.utils.dialer_references import DialerReferenceError, validate_emitted


def _provider_context(exc, *, name='', path='', kind='订阅'):
    """Keep validation exceptions outside the transport-fallback boundary."""
    from backend.agents.deployment_locations import preparation_context
    context = (kind, name, path)
    contexts = getattr(exc, '_provider_contexts', ())
    if not (name or path) or context in contexts:
        wrapped = type(exc)(str(exc))
        wrapped._provider_contexts = contexts
    else:
        wrapped = type(exc)(str(preparation_context(exc, name=name, path=path, kind=kind)))
        wrapped._provider_contexts = (*contexts, context)
    return wrapped


def _shape_error(message, *field):
    label = ''.join(f'[{item}]' if isinstance(item, int) else ('.' if index else '') + item
                    for index, item in enumerate(field))
    exc = DialerReferenceError(f'{message}\n字段：{label}')
    exc.provider_field = field
    return exc


def _provider_source_error(exc, text, source_label):
    """Resolve a known field only on failure; never guess a node's line number."""
    import yaml
    from backend.agents.deployment_locations import source_error
    from backend.agents.diagnostics import failure_text
    mark = getattr(exc, 'problem_mark', None)
    field = getattr(exc, 'provider_field', ())
    if field:
        loader = yaml.SafeLoader(text)
        try:
            node = loader.get_single_node()
            for item in field:
                if isinstance(node, yaml.MappingNode) and isinstance(item, str):
                    loader.flatten_mapping(node)
                    node = next((value for key, value in reversed(node.value) if key.value == item), None)
                elif isinstance(node, yaml.SequenceNode) and isinstance(item, int) and item < len(node.value):
                    node = node.value[item]
                else:
                    node = None
                if node is None:
                    break
            mark = node.start_mark if node is not None else None
        except yaml.YAMLError:
            pass
        finally:
            loader.dispose()
    if isinstance(exc, yaml.YAMLError):
        message, diagnostic = 'Provider YAML 内容无效', exc
    else:
        message, diagnostic = str(exc), None
    detail = str(source_error(message, text,
                line=mark.line + 1 if mark is not None else None,
                column=mark.column + 1 if mark is not None else None, cause=diagnostic,
                source_label=source_label))
    return DialerReferenceError(failure_text(detail))


@dataclass(frozen=True)
class DeliverySnapshot:
    profile_id: str
    main_json: str
    chain_names: frozenset = frozenset()

    @classmethod
    def capture(cls, profile_id, main, *, profile=None):
        names = frozenset(g['name'] for g in (profile or {}).get('proxy_groups', [])
                          if g.get('type') == 'chain' and g.get('enabled', True))
        return cls(profile_id, json.dumps(main), names)

    @property
    def has_chains(self):
        main = json.loads(self.main_json)
        return bool(self.chain_names) or any(p.get('dialer-proxy') is not None for p in main.get('proxies', [])) or any(
            p.get('override', {}).get('dialer-proxy') for p in main.get('proxy-providers', {}).values())

    def adapt(self, sub, proxies, *, chains=True):
        return adapt_delivered(self, sub, proxies, chains=chains)

    def validate(self, proxies, *, force_chains=False, provider_name=None):
        validate_proxy_shapes(proxies)
        main = json.loads(self.main_json)
        # Suppress only cross-artifact overlap, never duplicates inside a provider.
        names = {p['name'] for p in proxies}
        if names.intersection(self.chain_names):
            raise DialerReferenceError('Provider 节点名称与代理链出口冲突')
        # Cross-artifact overlap can remove the last visible main dialer. Strict
        # activation belongs to the immutable original snapshot, not that filtered
        # graph; an explicit False from a caller must never deactivate it.
        if provider_name is not None and provider_name in main.get('proxy-providers', {}):
            definitions = main.get('proxy-providers', {})
            source = definitions.get(provider_name, {})
            payloads = {name: proxies for name, definition in definitions.items()
                        if name == provider_name or (source.get('url') and definition.get('url') == source['url'])}
            validate_emitted(main, force_chains=self.has_chains or force_chains, provider_proxies=payloads)
        else:
            validate_emitted({**main,
                'proxies': [p for p in main.get('proxies', []) if p['name'] not in names] + proxies,
            }, force_chains=self.has_chains or force_chains)


def adapt_delivered(snapshot, sub, proxies, *, chains=True):
    """A subscription's own chained nodes as delivered with the snapshot's main configuration."""
    main_json = getattr(snapshot, 'main_json', None)
    if main_json is None or not sub.get('id'):
        return proxies
    from backend.utils.subscription_dialers import adapt_subscription_proxies, dialer_record
    return adapt_subscription_proxies(sub.get('name', ''), proxies, json.loads(main_json), dialer_record(sub),
                                      chains=chains)


def validate_proxy_shapes(proxies):
    if not isinstance(proxies, list):
        raise _shape_error('Provider proxies 必须是数组', 'proxies')
    for index, proxy in enumerate(proxies):
        if (not isinstance(proxy, dict) or not isinstance(proxy.get('name'), str)
                or not proxy['name'].strip()):
            field = ('proxies', index, 'name') if isinstance(proxy, dict) and 'name' in proxy else ('proxies', index)
            raise _shape_error('Provider 代理必须是含非空 name 的对象', *field)
        if 'proxies' in proxy and (not isinstance(proxy['proxies'], list) or
                any(not isinstance(name, str) or not name.strip() for name in proxy['proxies'])):
            raise _shape_error('Provider proxies 引用必须是字符串数组', 'proxies', index, 'proxies')
        if 'type' in proxy and not isinstance(proxy['type'], str):
            raise _shape_error('Provider type 必须是字符串', 'proxies', index, 'type')


def _load_provider_proxies(text, *, name='', path='', kind='订阅', source_label='本次准备的文件内容'):
    import yaml
    try:
        data = yaml.safe_load(text)
    except yaml.YAMLError as exc:
        # Content errors must propagate through the transport-fallback boundary.
        raise _provider_context(_provider_source_error(exc, text, source_label), name=name, path=path, kind=kind) from exc
    try:
        if not isinstance(data, dict):
            raise DialerReferenceError('Provider 必须是 YAML 对象\n字段：文档根节点')
        validate_proxy_shapes(data.get('proxies', []))
    except DialerReferenceError as exc:
        raise _provider_context(_provider_source_error(exc, text, source_label), name=name, path=path, kind=kind) from exc
    return data.get('proxies', [])


def parse_provider_proxies(text, *, name='', path=''):
    """Reject malformed metadata before legacy field normalization can mask it."""
    from backend.utils.sub_store_client import parse_proxies_from_yaml
    _load_provider_proxies(text, name=name, path=path, source_label='本次订阅返回的内容')
    return parse_proxies_from_yaml(text)


def prepare_subscription_provider(sub, config, snapshot, *, fetch=None,
                                  allow_transport_fallback=False, source_path='', source_prepared=False):
    """Prepare without publishing node caches, retaining its name on failure.

    source_prepared means fetch already resolves the bundle's captured source
    under its refresh policy; do not replace that snapshot with another cache.
    """
    from backend.utils.strategy_references import StrategyReferenceError
    try:
        return _prepare_subscription_provider(sub, config, snapshot, fetch=fetch,
                    allow_transport_fallback=allow_transport_fallback, source_path=source_path,
                    source_prepared=source_prepared)
    except StrategyReferenceError as exc:
        raise _provider_context(exc, name=sub.get('name', ''), path=source_path) from exc


def _prepare_subscription_provider(sub, config, snapshot, *, fetch=None,
                                   allow_transport_fallback=False, source_path='', source_prepared=False):
    """Return validated output and staged node-cache updates for the caller."""
    import uuid
    import yaml
    from backend.utils.sub_store_client import (get_subscription_proxies_yaml,
                                               parse_proxies_from_yaml, proxies_to_nodes)
    from backend.utils.subscription_cache import load_subscription_cache
    from backend.utils.strategy_references import StrategyReferenceError
    from backend.utils.dialer_references import validate_shapes
    from backend.converters.mihomo import convert_node_to_mihomo, IndentDumper
    from backend.utils.subscription_filter import filter_subscription_items
    from backend.utils.subscription_refresh import fetch_subscription_source, source_cache_metadata

    fetch = fetch or get_subscription_proxies_yaml
    update = None
    proxies = None
    transport_error = None
    if sub.get('url'):
        try:
            result = fetch(sub['id'], sub['url']) if source_prepared else fetch_subscription_source(sub, fetch=fetch)
            text, _source = result
            raw_proxies = parse_provider_proxies(text, name=sub.get('name', ''), path=source_path)
            proxies = adapt_delivered(snapshot, sub, filter_subscription_items(sub, raw_proxies))
            snapshot.validate(proxies, provider_name=sub['name'])
            if raw_proxies and _source not in ('cache', 'prepared_yaml'):
                nodes = proxies_to_nodes(raw_proxies)
                for node in nodes:
                    node['subscription_id'] = sub['id']
                    node['subscription_name'] = sub['name']
                    node.setdefault('id', f"node_{uuid.uuid4().hex[:8]}")
                update = (sub['id'], nodes, source_cache_metadata(sub, result))
        except StrategyReferenceError:
            raise
        except Exception as exc:
            transport_error = exc
            proxies = None
    if proxies is None:
        cache = load_subscription_cache(sub['id'])
        if not cache or not cache.get('nodes'):
            if allow_transport_fallback and not snapshot.has_chains:
                return {'content': '', 'cache_updates': []}
            from backend.agents.diagnostics import failure_text
            detail = ('\n原始诊断：' + failure_text(str(transport_error))) if transport_error is not None else ''
            raise DialerReferenceError('订阅没有可用缓存或远程代理' + detail) from transport_error
        validate_shapes({'nodes': cache['nodes']}, require_ids=False)
        proxies = adapt_delivered(snapshot, sub, [p for n in filter_subscription_items(sub, cache['nodes'])
                                                  if (p := convert_node_to_mihomo(n))])
    snapshot.validate(proxies, provider_name=sub['name'])
    content = yaml.dump({'proxies': proxies}, Dumper=IndentDumper, allow_unicode=True,
                        default_flow_style=False, sort_keys=False, indent=2)
    return {'name': sub['name'], 'content': content, 'cache_updates': [update] if update else []}


def validate_rendered_bundle(snapshot, providers):
    """Validate all exact provider collections with one monotonic activation.

    Providers are already materialized and their cache updates remain staged.
    Never fetch/render again here: discovery and final validation must inspect
    the same bytes, including providers rendered before a late chain appeared.
    """
    collections = []
    for provider in providers:
        content = provider['content']
        if content:
            proxies = _load_provider_proxies(content, name=provider.get('name', ''),
                        path=provider.get('local_path', ''), kind='节点来源')
        else:
            proxies = None
        collections.append(proxies)
    chains = snapshot.has_chains or any(
        p.get('dialer-proxy') is not None
        for proxies in collections if proxies is not None for p in proxies)
    if chains and any(proxies is None for proxies in collections):
        provider = next(provider for provider, proxies in zip(providers, collections) if proxies is None)
        raise _provider_context(DialerReferenceError('拨号拓扑不能使用未验证的 Provider URL fallback'),
                    name=provider.get('name', ''), path=provider.get('local_path', ''), kind='节点来源')
    # Provider-only activation also applies to the original main namespace.
    snapshot.validate([], force_chains=chains)
    main = json.loads(snapshot.main_json)
    payloads = {}
    for provider, proxies in zip(providers, collections):
        if proxies is None:
            continue
        name = provider.get('name')
        try:
            snapshot.validate(proxies, force_chains=chains, provider_name=name)
        except DialerReferenceError as exc:
            raise _provider_context(exc, name=name, path=provider.get('local_path', ''), kind='节点来源') from exc
        if name:
            payloads[name] = proxies
    if payloads:
        definitions = main.get('proxy-providers', {})
        by_url = {definitions[name]['url']: proxies for name, proxies in payloads.items()
                  if name in definitions and definitions[name].get('url')}
        for name, definition in definitions.items():
            if definition.get('url') in by_url:
                payloads[name] = by_url[definition['url']]
    validate_emitted(main, force_chains=chains, provider_proxies=payloads, require_providers=chains)
    return chains


def commit_cache_updates(updates):
    from backend.utils.subscription_cache import save_subscription_nodes
    for sub_id, nodes, metadata in updates:
        save_subscription_nodes(sub_id, nodes, metadata)


def prepare_provider_bundle(config, main, *, requested=None, render_all=False, allow_transport_fallback=False,
                            prepared=None, subscription_fetch=None, progress=None):
    """Capture once, render each original source once, validate all copies, never commit."""
    from urllib.parse import urlsplit, unquote
    from backend.routes.aggregations import (generate_aggregation_provider,
        get_subscription_proxies_yaml as get_aggregation_subscription)
    from backend.routes.subscriptions import get_subscription_proxies_yaml
    from backend.converters.mihomo import _parse_structured_proxy_string
    from concurrent.futures import Future
    from threading import Lock
    from copy import deepcopy
    from functools import partial
    from backend.utils.subscription_refresh import fetch_subscription_source

    requested_name = None
    if requested:
        kind, resource = requested
        requested_name = resource['name']
        catalog = 'subscriptions' if kind == 'subscription' else 'subscription_aggregations'
        if not any(item['id'] == resource['id'] for item in config.get(catalog, [])):
            config = deepcopy(config)
            config.setdefault(catalog, []).append(deepcopy(resource))
    fetch_results, fetch_lock = {}, Lock()
    for sub in config.get('subscriptions', []):
        if prepared and sub['name'] in prepared:
            result = Future()
            result.set_result((prepared[sub['name']]['content'], 'prepared_yaml'))
            fetch_results[(sub['id'], sub.get('url'))] = result

    def fetch_subscription(id, url, *, fetch):
        key = (id, url)
        with fetch_lock:
            owner = key not in fetch_results
            if owner:
                fetch_results[key] = Future()
            result = fetch_results[key]
        if owner:
            try:
                sub = next(s for s in config.get('subscriptions', []) if s['id'] == id)
                result.set_result(fetch_subscription_source(sub, fetch=subscription_fetch or fetch))
            except Exception as exc:
                result.set_exception(exc)
        return result.result()

    from backend.common.config import get_default_profile_id
    snapshot = DeliverySnapshot.capture(config.get('profile_id') or get_default_profile_id(), main, profile=config)
    definitions = main.get('proxy-providers', {})
    if requested and requested_name not in definitions:
        main = deepcopy(main)
        definitions = main.setdefault('proxy-providers', {})
        kind, resource = requested
        segment, suffix = ('subscriptions', 'proxies') if kind == 'subscription' else ('aggregations', 'provider')
        definitions[requested_name] = {'url': f"/{segment}/{resource['id']}/{suffix}",
                                       'path': f"./providers/{resource['id']}.yaml"}
        snapshot = DeliverySnapshot.capture(config.get('profile_id') or get_default_profile_id(), main, profile=config)
    sources = {}
    for name, definition in definitions.items():
        url = definition.get('url', '')
        sources.setdefault(url, name)
    rendered = {}
    opaque = {n['id'] for n in config.get('nodes', []) if n.get('proxy_string') and
              _parse_structured_proxy_string(n['proxy_string']) is None}

    def materialize(url, name):
        from backend.utils.strategy_references import StrategyReferenceError
        kind = '订阅聚合' if '/aggregations/' in unquote(urlsplit(url).path) else '订阅'
        if progress is not None:
            progress(f'正在准备{kind}「{name}」')
        try:
            _materialize(url, name)
        except StrategyReferenceError as exc:
            kind = '订阅聚合' if '/aggregations/' in unquote(urlsplit(url).path) else '订阅'
            raise _provider_context(exc, name=name, path=definitions[name].get('path', ''), kind=kind) from exc
        if progress is not None:
            progress(f'{kind}「{name}」已准备')

    def _materialize(url, name):
        if prepared and name in prepared:
            rendered[url] = {**prepared[name], 'name': name}
            return
        path = unquote(urlsplit(url).path)
        if '/subscriptions/' in path:
            id = path.rsplit('/subscriptions/', 1)[1].split('/')[0]
            sub = next(s for s in config.get('subscriptions', []) if s['id'] == id)
            value = prepare_subscription_provider(sub, config, snapshot,
                                                  fetch=partial(fetch_subscription, fetch=get_subscription_proxies_yaml),
                                                  allow_transport_fallback=allow_transport_fallback,
                                                  source_prepared=True,
                                                  source_path=definitions[name].get('path', ''))
        elif '/aggregations/' in path:
            id = path.rsplit('/aggregations/', 1)[1].split('/')[0]
            agg = next(a for a in config.get('subscription_aggregations', []) if a['id'] == id)
            value = generate_aggregation_provider(agg, config=config, main_config=main, persist=False,
                                                  subscription_fetch=partial(fetch_subscription, fetch=get_aggregation_subscription),
                                                  source_prepared=True)
            value['aggregation_id'] = id
        else:
            raise DialerReferenceError('拨号拓扑 Provider 必须使用当前配置空间的订阅或聚合来源')
        value['name'] = name
        rendered[url] = value

    for url, name in sources.items():
        path = unquote(urlsplit(url).path)
        agg = next((a for a in config.get('subscription_aggregations', [])
                    if f"/aggregations/{a['id']}/" in path), None)
        if render_all or snapshot.has_chains or name == requested_name or (agg and opaque.intersection(agg.get('nodes', []))):
            materialize(url, name)
    active = snapshot.has_chains
    if not active:
        for url, value in rendered.items():
            if not value['content']:
                continue
            name = sources[url]
            proxies = _load_provider_proxies(value['content'], name=name,
                        path=definitions[name].get('path', ''), kind='节点来源')
            if any(p.get('dialer-proxy') is not None for p in proxies):
                active = True
                break
    if active:
        for url, name in sources.items():
            if url not in rendered:
                materialize(url, name)
    bundle = [{**rendered[definition['url']], 'name': name, 'url': definition['url'],
               'local_path': definition['path']} for name, definition in definitions.items()
              if definition.get('url') in rendered]
    if progress is not None:
        progress(f'正在校验节点来源与拨号引用关系（{len(bundle)} 个文件）', force=True)
    validate_rendered_bundle(snapshot, bundle)
    return bundle


def commit_provider_bundle(profile_id, bundle):
    """Only call after the complete exact bundle passes validation."""
    from backend.common.config import get_repository
    updates, artifacts = {}, {}
    for item in bundle:
        for update in item.get('cache_updates', []):
            updates[update[0]] = update
        if item.get('aggregation_id'):
            artifacts[item['aggregation_id']] = item['content']
    commit_cache_updates(updates.values())
    for id, content in artifacts.items():
        get_repository().write_profile_text(profile_id, f'providers/{id}.yaml', content)
