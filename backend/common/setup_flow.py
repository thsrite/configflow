"""Resumable first-use configuration, scoped to a newly created profile."""
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
import re
import secrets
import tempfile
from urllib.parse import urlsplit

import yaml

from backend.common.config_repository import ProfileExists, ProfileInUse
from backend.common.gateway_onboarding import (
    DEFAULT_LOCAL_DNS, DEFAULT_MIHOMO_NAMESERVERS, DNS_TARGET, GatewayOnboardingError, _local_upstreams,
    _prepare_mihomo_dns)
from backend.converters.mihomo import generate_mihomo_config, convert_node_to_mihomo
from backend.utils.subscription_cache import load_subscription_cache
from backend.utils.subscription_filter import filter_subscription_items


class SetupError(ValueError):
    def __init__(self, message, status=400):
        super().__init__(message)
        self.status = status


def _now():
    return datetime.now(timezone.utc).isoformat()


def _path(repository, session_id):
    if not isinstance(session_id, str) or not re.fullmatch('[a-f0-9]{32}', session_id):
        raise SetupError('向导记录不存在', 404)
    root = repository.data_dir / 'setup-sessions'
    root.mkdir(parents=True, mode=0o700, exist_ok=True)
    return root / f'{session_id}.json'


@contextmanager
def locked(repository, session_id):
    with _path(repository, session_id).with_suffix('.lock').open('a+b') as handle:
        os.chmod(handle.name, 0o600)
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def read(repository, session_id):
    try:
        return json.loads(_path(repository, session_id).read_text(encoding='utf-8'))
    except FileNotFoundError:
        raise SetupError('向导记录不存在，请重新开始', 404) from None


def public(session):
    result = deepcopy(session)
    if result.get('router_draft'):
        from backend.routers.store import public_view
        result['router_draft'] = public_view(result['router_draft'])
    return result


def write(repository, session):
    path = _path(repository, session['id'])
    session['updated_at'] = _now()
    fd, temporary = tempfile.mkstemp(prefix='.setup-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as handle:
            json.dump(session, handle, ensure_ascii=False)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def create(repository, body):
    session_id, goal, name = body.get('id'), body.get('goal'), body.get('name')
    if goal not in ('router', 'gateway'):
        raise SetupError('请选择使用方式')
    if not isinstance(name, str) or not 1 <= len(name.strip()) <= 64:
        raise SetupError('配置名称需为 1–64 个字符')
    with locked(repository, session_id):
        path = _path(repository, session_id)
        if path.exists():
            session = read(repository, session_id)
            if session['goal'] != goal or session['name'] != name.strip():
                raise SetupError('该向导已创建，请继续原向导或新建一次配置', 409)
        else:
            profile_id = 'setup_' + session_id[:20]
            if any(p['id'] == profile_id for p in repository.list_profiles()):
                raise SetupError('配置名称标识冲突，请重新开始', 409)
            session = {'id': session_id, 'profile_id': profile_id, 'name': name.strip(),
                       'goal': goal, 'step': 1, 'max_step': 1, 'created_at': _now(),
                       'subscription_ids': [], 'node_ids': [], 'prepared': False,
                       'profile_revision': 0, 'completed': False}
            write(repository, session)
        try:
            repository.create_profile({'id': session['profile_id'], 'name': session['name']})
        except ProfileExists:
            pass  # A retry after the profile commit uses the same reserved identity.
        if not repository.get_system().get('system_config', {}).get('config_token'):
            def ensure_token(system):
                settings = system.setdefault('system_config', {})
                if not settings.get('config_token'):
                    settings['config_token'] = secrets.token_urlsafe(32)
            repository.update_system_transaction(ensure_token)
        return session


# Fields the wizard writes. Router records, probes and release receipts also
# advance the profile revision and must not count as edits made elsewhere.
_OWNED_FIELDS = ('proxy_groups', 'rule_configs', 'mihomo', 'oxidns')


def remove(repository, session_id):
    """Delete an abandoned session record; the caller holds its lock."""
    _path(repository, session_id).unlink(missing_ok=True)


def owned_digest(profile, *, base=True):
    """Digest of the fields the wizard writes; ``base=False`` leaves out the Mihomo base YAML."""
    owned = {key: profile.get(key) for key in _OWNED_FIELDS}
    if not base and isinstance(owned['mihomo'], dict):
        owned['mihomo'] = {key: value for key, value in owned['mihomo'].items() if key != 'custom_config'}
    return hashlib.sha256(json.dumps(owned, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def adopt_owned_write(repository, profile_id, before, after):
    """Record an embedded installation save as the wizard's own edit."""
    root = repository.data_dir / 'setup-sessions'
    for path in root.glob('*.json'):
        with locked(repository, path.stem):
            session = _read_existing(repository, path.stem)
            if session and (session['profile_id'] == profile_id and not session.get('completed')
                    and session.get('owned_digest') == owned_digest(before)):
                session['owned_digest'] = owned_digest(after)
                session['owned_digest_without_base'] = owned_digest(after, base=False)
                write(repository, session)


def _read_existing(repository, session_id):
    """Read a listed session, or None if it was abandoned after the listing."""
    try:
        return read(repository, session_id)
    except SetupError:
        return None


def list_sessions(repository):
    root = repository.data_dir / 'setup-sessions'
    records = []
    for path in root.glob('*.json'):
        with locked(repository, path.stem):
            session = _read_existing(repository, path.stem)
        if session and any(p['id'] == session['profile_id'] for p in repository.list_profiles()):
            records.append(session)
    return sorted(records, key=lambda item: item['updated_at'], reverse=True)


def legacy_takeover(profile):
    """A router profile prepared before the full TUN takeover uses the Linux
    gateway, and its host still forwards through it; that takeover stays."""
    from backend.common.linux_gateway import gateway_settings
    from backend.common.router_split import read_marker
    return read_marker(profile)[0] == 'absent' and gateway_settings(profile)['enabled']


_SETUP_DNS = {'enable': True, 'listen': DNS_TARGET, 'enhanced-mode': 'fake-ip'}
# The initial template's controller password; the wizard never keeps it.
TEMPLATE_SECRET = '123456'


def initial_mihomo_base():
    """The initial template's base YAML, which a new installation's default profile starts with."""
    from backend.common.resource import get_backend_resource
    with open(get_backend_resource('config_template.json'), encoding='utf-8') as handle:
        return json.load(handle)['mihomo']['custom_config']


def controller_secret_error(value):
    """Why ``value`` cannot protect the controller, or None."""
    if value == TEMPLATE_SECRET:
        return '不能使用初始模板的默认密码 123456，请自己设置一个'
    if not isinstance(value, str) or not 8 <= len(value) <= 64 or not all('!' <= c <= '~' for c in value):
        return '控制面板密码需为 8–64 个字母、数字或符号，不含空格'
    return None


def custom_controller_secret(document):
    secret = document.get('secret') if isinstance(document, dict) else None
    return isinstance(secret, str) and bool(secret) and secret != TEMPLATE_SECRET


def setup_mihomo_base(text, *, full_tun, reserved=None, secret=None):
    """The wizard's Mihomo base YAML: the profile's own, changed only where the plan needs it.

    Returns the YAML and what was changed, for the wizard to show. An empty
    base starts from the initial template; an unreadable one is reported
    instead of replaced. ``secret`` replaces the controller password; it is
    required unless the base already has one of the user's own.
    """
    from backend.common.linux_gateway import LinuxGatewayError
    from backend.common.router_split import ROUTE_KEYS_REMOVED, _load_base, full_tun_document, tun_filters
    empty = isinstance(text, str) and not text.strip()
    if empty:
        text = initial_mihomo_base()
    try:
        original = _load_base(text)
        holder = {'mihomo': {'custom_config': text}}
        # Fills dns.nameserver only when missing and rejects loops to OxiDNS.
        _prepare_mihomo_dns(holder)
    except (LinuxGatewayError, GatewayOnboardingError) as error:
        raise SetupError(str(error)) from error
    document = yaml.safe_load(holder['mihomo']['custom_config'])
    changes = ['基础配置为空，已使用初始模板'] if empty else []
    if not (original.get('dns') or {}).get('nameserver'):
        changes.append('dns.nameserver 为空，已补入 ' + '、'.join(DEFAULT_MIHOMO_NAMESERVERS))
    if secret is not None:
        error = controller_secret_error(secret)
        if error:
            raise SetupError(error)
        if document.get('secret') != secret:
            changes.append('控制面板密码（secret）已' + ('更新' if custom_controller_secret(document) else '设置'))
            document['secret'] = secret
    elif not custom_controller_secret(document):
        raise SetupError('请设置控制面板密码：Mihomo 控制面板（external-controller）需要用它防止他人访问')
    dns = document['dns']
    for key, value in _SETUP_DNS.items():
        if dns.get(key) != value:
            shown = [item if isinstance(item, str) else json.dumps(item) for item in (dns.get(key), value)]
            changes.append(f'dns.{key} 由 {shown[0]} 改为 {shown[1]}' if key in dns else f'dns.{key} 设为 {shown[1]}')
            dns[key] = value
    if document.get('mode', 'rule') != 'rule':
        changes.append(f"mode 由 {document['mode']} 改为 rule")
        document['mode'] = 'rule'
    tun = document.get('tun')
    if tun is not None and not isinstance(tun, dict):
        raise SetupError('Mihomo tun 必须是对象，请先修正基础配置')
    if full_tun:
        try:
            document = full_tun_document(document)
        except LinuxGatewayError as error:
            raise SetupError(str(error)) from error
        if document['tun'] != tun:
            removed = [f'tun.{key}' for key in ROUTE_KEYS_REMOVED if key in (tun or {})]
            changes.append('tun 设为完整 TUN（enable、auto-route、auto-redirect、auto-detect-interface，'
                           'dns-hijack 为 any:53）' + ('，移除 ' + '、'.join(removed) if removed else ''))
    elif tun:
        for key in tun_filters(tun):
            raise SetupError(f'Linux 网关与现有 tun.{key} 过滤冲突，请先在 Mihomo 基础 YAML 中移除该过滤，避免客户端绕过接管')
        if tun.get('enable'):
            # The Linux gateway turns the TUN on with its own takeover keys.
            tun['enable'] = False
            changes.append('tun 已停用，由 Linux 网关启用时接管')
    from backend.converters.mihomo import mihomo_listen_ports
    for port, key in mihomo_listen_ports(document).items():
        if port in (reserved or {}):
            raise SetupError(f'Mihomo 基础配置的 {key} 使用端口 {port}，与{reserved[port]}冲突；请先在 Mihomo 基础配置中改用其他端口')
    return yaml.safe_dump(document, allow_unicode=True, sort_keys=False), changes


def profile_sessions(repository, profile_id):
    """The sessions of one profile, newest first, read without the session locks.

    ``write`` replaces files atomically, so an unlocked read is whole and never
    waits for a wizard step that holds a lock while it calls an Agent. Files
    that are not session records are skipped instead of failing the caller.
    """
    records = []
    for path in (repository.data_dir / 'setup-sessions').glob('*.json'):
        if not re.fullmatch('[a-f0-9]{32}', path.stem):
            continue
        try:
            session = json.loads(path.read_text(encoding='utf-8'))
        except FileNotFoundError:
            continue
        if isinstance(session, dict) and session.get('profile_id') == profile_id:
            records.append(session)
    return sorted(records, key=lambda item: item.get('updated_at') or '', reverse=True)


def _http_url(url):
    try:
        parsed = urlsplit(url)
        parsed.port
    except ValueError:
        return False
    return parsed.scheme in ('http', 'https') and bool(parsed.hostname)


def selectable_subscriptions(shared):
    """Enabled subscriptions whose every line is a link; Sub-Store fetches each line,
    and the first-start placeholder has none."""
    def linked(url):
        lines = [line.strip() for line in str(url or '').splitlines() if line.strip()]
        return bool(lines) and all(_http_url(line) for line in lines)
    return [item for item in shared['subscriptions'] if item.get('enabled', True) and linked(item.get('url'))]


def source(repository, session, body):
    if session['prepared']:
        raise SetupError('方案已生成；修改来源请前往订阅来源或创建新的向导', 409)
    subscription_ids = body.get('subscription_ids', [])
    node_ids = body.get('node_ids', [])
    for values in (subscription_ids, node_ids):
        if not isinstance(values, list) or len(values) > 100 or any(not isinstance(v, str) for v in values):
            raise SetupError('请选择有效的订阅或节点')
    url = body.get('url', '')
    if not isinstance(url, str) or len(url) > 8192:
        raise SetupError('订阅链接格式不正确')
    if url.strip():
        url = url.strip()
        if not _http_url(url):
            raise SetupError('请输入 HTTP(S) 订阅链接')
        subscription_id = 'setup_sub_' + session['id']
        def update(shared):
            existing = next((s for s in shared['subscriptions'] if s['id'] == subscription_id), None)
            if existing:
                if existing['url'] != url:
                    existing.update(url=url)
            else:
                shared['subscriptions'].append({'id': subscription_id, 'name': session['name'] + '订阅',
                                                'url': url, 'enabled': True, 'interval': 86400})
        repository.update_shared_transaction(update)
        subscription_ids = [subscription_id]
        node_ids = []
    shared = repository.get_shared()
    for values, field in ((subscription_ids, 'subscriptions'), (node_ids, 'nodes')):
        available = {item['id'] for item in shared[field] if item.get('enabled', True)}
        if set(values) - available:
            raise SetupError('所选来源已删除或停用，请刷新后重选', 409)
    if set(subscription_ids) - {item['id'] for item in selectable_subscriptions(shared)}:
        raise SetupError('所选订阅未填写订阅链接，请改用订阅链接添加')
    if not subscription_ids and not node_ids:
        raise SetupError('请添加订阅链接或选择已有节点')
    session.update(subscription_ids=list(dict.fromkeys(subscription_ids)),
                   node_ids=list(dict.fromkeys(node_ids)), step=2, max_step=2)
    return session


def _resources(config, session):
    """Return the usable node count and the subscription and manual nodes."""
    available, sources = 0, {'subscriptions': [], 'nodes': []}
    for field, selected in (('subscriptions', session['subscription_ids']), ('nodes', session['node_ids'])):
        items = {item['id']: item for item in config.get(field, []) if item.get('enabled', True)}
        for resource_id in selected:
            if resource_id not in items:
                raise SetupError('所选来源已删除或停用，请返回检查节点来源', 409)
            if field == 'subscriptions':
                cache = load_subscription_cache(resource_id) or {}
                if cache.get('metadata', {}).get('url', items[resource_id]['url']) != items[resource_id]['url']:
                    raise SetupError('订阅链接已变化，请重新读取订阅')
                nodes = filter_subscription_items(items[resource_id], cache.get('nodes', []))
            else:
                nodes = [items[resource_id]]
            usable = [node for node in nodes if node.get('enabled', True) and convert_node_to_mihomo(node)]
            available += len(usable)
            sources[field] += usable
    if not available:
        raise SetupError('当前没有可用节点，请先读取订阅或选择支持的节点')
    return available, sources['subscriptions'], sources['nodes']


def prepare(repository, session, body, base_url):
    if set(body) - {'dns_fallback', 'traffic_fallback', 'local_dns', 'domain_rules', 'template', 'query_recorder',
                    'controller_secret'}:
        raise SetupError('漏网处理参数无效')
    if body.get('template', 'basic') != 'basic':
        raise SetupError('代理优先模式已移除，请刷新向导并分别选择 DNS 与连接的漏网处理')
    dns_fallback = body.get('dns_fallback', 'mihomo')
    traffic_fallback = body.get('traffic_fallback', 'proxy')
    if dns_fallback not in ('mihomo', 'direct') or traffic_fallback not in ('proxy', 'direct'):
        raise SetupError('请选择有效的 DNS 与连接漏网处理方式')
    # On by default for new wizards; one prepared before the switch existed
    # wrote it off and keeps that until the user chooses.
    query_recorder = body.get('query_recorder', session.get('query_recorder', not session['prepared']))
    if type(query_recorder) is not bool:
        raise SetupError('DNS 查询记录开关必须为布尔值')
    local_dns = body.get('local_dns', session.get('local_dns', DEFAULT_LOCAL_DNS))
    try:
        _local_upstreams(local_dns)
    except GatewayOnboardingError as error:
        raise SetupError(str(error)) from error
    local_dns = local_dns.strip()
    from backend.common.setup_presets import (builtin_rule_catalog, canonical_domain_rules, domain_rule_options,
                                              recommended_domain_rules, resolve_domain_rules, template_strategy)
    selections = body.get('domain_rules', session.get('domain_rules', recommended_domain_rules(repository)))
    if not isinstance(selections, dict) or set(selections) != {'direct', 'proxy'}:
        raise SetupError('请选择有效的直连和代理域名规则集')
    for values in selections.values():
        if (not isinstance(values, list) or len(values) > 100
                or any(not isinstance(value, str) or not value or len(value) > 256 for value in values)):
            raise SetupError('域名规则集必须是有效的选项列表')
    selections = canonical_domain_rules(repository, selections)
    if set(selections['direct']) & set(selections['proxy']):
        raise SetupError('同一个域名规则集不能同时设为直连和代理')
    available = {item['id'] for item in domain_rule_options(repository)}
    if any(set(values) - available for values in selections.values()):
        raise SetupError('所选域名规则集已删除、停用或不支持，请刷新后重选', 409)
    config, _ = repository.get_publish_snapshot(session['profile_id'])
    # A new profile has no references yet, so its hydrated snapshot is empty.
    # Only the selected resources are referenced by the generated strategy.
    shared = repository.get_shared()
    config.update({key: shared[key] for key in repository.SHARED_FIELDS})
    count, subscription_nodes, manual_nodes = _resources(config, session)
    rules = [{'id': f'setup_lan_{i}', 'itemType': 'rule', 'rule_type': 'IP-CIDR',
              'value': value, 'policy': 'DIRECT', 'no_resolve': True, 'enabled': True}
             for i, value in enumerate(('127.0.0.0/8', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16'))]
    domain_rules = resolve_domain_rules(repository, selections)
    template_order = {identity: index for index, identity in enumerate(builtin_rule_catalog())}
    selected_rules = sorted(((identity, route, rule_id, item['id'])
                             for route in ('direct', 'proxy')
                             for identity, (rule_id, item) in zip(selections[route], domain_rules[route])),
                            key=lambda entry: template_order.get(entry[0], len(template_order)))
    groups, policies, final = template_strategy(
        [(identity, route) for identity, route, _, _ in selected_rules], traffic_fallback,
        session['subscription_ids'], session['node_ids'], subscription_nodes, manual_nodes)
    rules += [{'id': rule_id, 'itemType': 'ruleset', 'library_rule_id': library_id, 'enabled': True, 'policy': policy}
              for (_, _, rule_id, library_id), policy in zip(selected_rules, policies)]
    rules.append({'id': 'setup_final', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': final, 'enabled': True})
    from backend.common.router_split import MARKER_KEY, read_marker
    stored = repository.get_profile(session['profile_id'])
    legacy = session['goal'] == 'router' and legacy_takeover(stored)
    full_tun = session['goal'] == 'router' and not legacy
    from backend.common.oxidns_settings import get_default_oxidns_settings
    dns = get_default_oxidns_settings()
    dns.update(local_dns=local_dns, remote_dns=DNS_TARGET,
               fallback_dns=DNS_TARGET, direct_rulesets=[identity for identity, _ in domain_rules['direct']],
               proxy_rulesets=[identity for identity, _ in domain_rules['proxy']],
               default_forward='forward_remote' if dns_fallback == 'mihomo' else 'forward_local')
    dns['rule_update']['enabled'] = bool(domain_rules['direct'] or domain_rules['proxy'])
    dns['query_recorder']['enabled'] = query_recorder
    # Same host as OxiDNS: Mihomo must not listen where OxiDNS or its own DNS does.
    reserved = {int(DNS_TARGET.rpartition(':')[2]): f' Mihomo DNS {DNS_TARGET} ',
                int(dns['listen'].rpartition(':')[2]): f' OxiDNS 监听 {dns["listen"]} '}
    if dns['api']['enabled']:
        reserved[int(dns['api']['listen'].rpartition(':')[2])] = f' OxiDNS 管理 API {dns["api"]["listen"]} '
    base, base_changes = setup_mihomo_base(config.get('mihomo', {}).get('custom_config', ''),
                                           full_tun=full_tun, reserved=reserved,
                                           secret=body.get('controller_secret') or None)
    fields = {'proxy_groups': groups, 'rule_configs': rules, 'oxidns': dns,
              'mihomo': {**config.get('mihomo', {}), 'custom_config': base}}
    if full_tun:
        # The router forwards fake-IP, plus rule sets chosen later, to the full
        # TUN; the gateway's selective takeover never applies here.
        rule_ids = {rule['id'] for rule in rules}
        state, marker = read_marker(stored)
        keep = [rule_id for rule_id in (marker['route_rule_set_ids'] if state == 'valid' else [])
                if rule_id in rule_ids]
        identity = marker if state == 'valid' else {'version': 1, 'takeover': 'full_tun', 'origin': 'setup'}
        fields['mihomo'].update(linux_gateway={'enabled': False, 'rule_set_ids': []},
                                **{MARKER_KEY: {**identity, 'route_rule_set_ids': keep}})
    candidate = {**deepcopy(config), **deepcopy(fields)}
    candidate['rule_library'] = repository.get_shared()['rule_library']
    for items in domain_rules.values():
        for identity, item in items:
            rule = next(rule for rule in candidate['rule_configs'] if rule['id'] == identity)
            rule.update({key: item[key] for key in ('name', 'url', 'source_type', 'behavior', 'format', 'content') if key in item})
            if item['source_type'] == 'content':
                rule['url'] = f'/api/profiles/{session["profile_id"]}/rule-library/content/{item["id"]}'
    if session['goal'] == 'gateway':
        # Apps that connect to IPs without a DNS lookup never reach the fake-IP
        # range; their proxy rule sets' IPv4 ranges route them into Mihomo too.
        from backend.common.linux_gateway import gateway_editor_state
        from backend.common.setup_presets import IP_TAKEOVER_RULES
        proxy = {rule_id for identity, (rule_id, _) in zip(selections['proxy'], domain_rules['proxy'])
                 if identity in IP_TAKEOVER_RULES}
        takeover = [item['id'] for item in gateway_editor_state(candidate, include_guidance=False)['candidates']
                    if item['id'] in proxy and item['available']]
        fields['mihomo']['linux_gateway'] = {**fields['mihomo'].get('linux_gateway', {'enabled': False}),
                                             'rule_set_ids': takeover}
        candidate['mihomo'] = deepcopy(fields['mihomo'])
    unchanged = all(stored.get(key) == value for key, value in fields.items())
    if 'owned_digest' not in session:
        edited = stored['_revision'] != session['profile_revision']
    elif 'owned_digest_without_base' in session:
        # Base YAML edits made elsewhere are kept: the wizard only sets what its plan needs there.
        edited = owned_digest(stored, base=False) != session['owned_digest_without_base']
    else:  # Prepared before the base was kept.
        edited = owned_digest(stored) != session['owned_digest']
    if edited and not unchanged:
        raise SetupError('该空间已在其他页面修改，向导不会覆盖；请新建向导或前往配置生成继续', 409)
    from backend.converters.oxidns import generate_oxidns_config
    from backend.common.oxidns_validation import require_valid_document
    require_valid_document(yaml.safe_load(generate_oxidns_config(candidate, base_url=base_url)))
    generated = yaml.safe_load(generate_mihomo_config(candidate, base_url=base_url))
    if not generated.get('proxy-groups') or not generated.get('rules'):
        raise SetupError('生成配置未包含有效代理策略，请检查来源')
    if not unchanged:
        def update(current):
            if current['_revision'] != stored['_revision']:
                raise ProfileInUse('配置已更新，请刷新向导后重试')
            current.update(deepcopy(fields))
        repository.update_profile_transaction(session['profile_id'], update)
        if session['max_step'] > 3:
            # Earlier checks and route instructions described the replaced
            # configuration; the next check must publish and verify again.
            for key in ('installation', 'route_instructions', 'router_verification', 'config_revision'):
                session.pop(key, None)
            session['max_step'] = 3
    saved = repository.get_profile(session['profile_id'])
    session.pop('template', None)
    if not unchanged:
        # What this preparation changed in the base YAML; a repeat that writes nothing keeps the last list.
        session['mihomo_changes'] = base_changes
    # Never the password itself: the page only learns that one is saved and where the controller listens.
    controller = yaml.safe_load(base).get('external-controller')
    session.update(controller_secret_saved=True,
                   controller_address=controller if isinstance(controller, str) else '')
    if session['goal'] == 'router':
        session['legacy_takeover'] = legacy
    session.update(prepared=True, dns_fallback=dns_fallback, traffic_fallback=traffic_fallback, local_dns=local_dns,
                   domain_rules=selections, query_recorder=query_recorder,
                   node_count=count, profile_revision=saved['_revision'], owned_digest=owned_digest(saved),
                   owned_digest_without_base=owned_digest(saved, base=False),
                   config_revision=repository.config_revision(session['profile_id']), step=3,
                   max_step=max(session['max_step'], 3), completed=False)
    return session


def update_progress(repository, session, body):
    if set(body) - {'step', 'router_id', 'router_mode'}:
        raise SetupError('向导进度参数无效')
    if 'step' in body:
        if type(body['step']) is not int or not 0 <= body['step'] <= session['max_step']:
            raise SetupError('请先完成前面的步骤')
        session['step'] = body['step']
    if 'router_mode' in body:
        if body['router_mode'] not in ('manual', 'managed') or session['goal'] != 'router':
            raise SetupError('请选择有效的路由器配置方式')
        session['router_mode'] = body['router_mode']
    if 'router_id' in body:
        routers = repository.get_profile(session['profile_id']).get('routers', {}).get('items', [])
        if not any(router['id'] == body['router_id'] for router in routers):
            raise SetupError('请选择当前配置空间中的路由器', 409)
        session['router_id'] = body['router_id']
    return session


def save_router_draft(session, body):
    """Keep early connection settings outside the frozen core profile revision."""
    if session['goal'] != 'router' or not session['prepared'] or session['max_step'] != 3:
        raise SetupError('请在安装等待期间填写主路由信息；安装完成后使用路由器管理', 409)
    if set(body) != {'router'}:
        raise SetupError('主路由连接参数无效')
    if body['router'] is None:
        session.pop('router_draft', None)
        return
    from backend.routers.store import normalize, RouterValidationError, EDITABLE_FIELDS
    payload = body['router']
    if not isinstance(payload, dict) or set(payload) - set((*EDITABLE_FIELDS, 'password')):
        raise SetupError('主路由连接参数无效')
    if (payload.get('proxy_gateway_agent_id') or payload.get('dns_agent_id')
            or payload.get('auto_rule_sync')):
        raise SetupError('安装完成后自动关联本次设备，暂不启用规则自动同步')
    try:
        draft = normalize(payload, session.get('router_draft'))
    except RouterValidationError as error:
        raise SetupError(str(error)) from None
    if draft['ros_policy_rule_sets']:
        # Checked on the merged draft: a draft saved earlier may still carry sets.
        # Policy routing takes its sources from the profile's client devices,
        # and a new setup profile has none yet, so publishing would be refused.
        raise SetupError('新配置空间还没有设为智能分流的客户端设备，暂不能使用策略路由；'
                         '完成向导后先在「客户端设备」中设置，再到上游路由中启用')
    session['router_draft'] = draft
    session['router_mode'] = 'managed'


def connect_router_draft(repository, session):
    """After real installation checks, attach the saved draft to the actual pair."""
    draft = session.get('router_draft')
    if not draft:
        return
    router = deepcopy(draft)
    pair = session['installation']
    router.update(proxy_gateway_agent_id=pair['mihomo']['id'], dns_agent_id=pair['oxidns']['id'])

    def attach(profile):
        from backend.common.router_split import is_full_tun, route_rule_set_ids
        if is_full_tun(profile) and not router.get('ip_rule_sets'):
            # Start from the rule sets the full TUN setup forwards from the router.
            router['ip_rule_sets'] = route_rule_set_ids(profile)
        items = profile.setdefault('routers', {}).setdefault('items', [])
        existing = next((item for item in items if item['id'] == router['id']), None)
        if existing is None:
            items.append(router)
        elif existing != router:
            raise SetupError('主路由记录已在其他页面修改，请检查路由器设置', 409)
    repository.update_profile_transaction(session['profile_id'], attach)
    session.update(router_id=router['id'], router_mode='managed')
    session.pop('router_draft', None)
