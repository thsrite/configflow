"""Let domain-to-domain subscription hosts steer node connections in Mihomo.

Mihomo (checked on v1.19.33) resolves a proxy server inside ``hosts`` only
while the chain ends in IP addresses; when it ends in a domain that ``hosts``
does not map, it gives up and asks DNS for the node's original domain. Its DNS
listener treats the same entry as a CNAME and resolves the target, so the
mapping takes effect once that node's domain is resolved through the listener.

``dns.proxy-server-nameserver-policy`` (Mihomo v1.19.20 and later) sends just
the mapped node domains to the listener; every other node keeps resolving
through the user's ``proxy-server-nameserver``, which is never rewritten.
Mihomo refuses a policy without that list, so nothing is added when it is
missing. The listener hands the mapped target a Fake-IP address unless the
target is filtered, so in Fake-IP mode only the targets join
``fake-ip-filter``. Nothing changes unless a merged subscription mapping needs it.
"""
import ipaddress
import re
from functools import lru_cache
from urllib.parse import urlsplit

from backend.utils.subscription_hosts import _address, _matches

POLICY_KEY = 'proxy-server-nameserver-policy'
MIN_CORE_VERSION = (1, 19, 20)
MIN_CORE_TEXT = 'v1.19.20'
# Windows' connectivity check (NCSI): Mihomo's own fake-ip-filter when the key is absent, and
# writing the key replaces it. Mihomo spells two of them "msftnsci", which matches no domain;
# they are written as Windows uses them, so Windows does not report "No Internet" on a Fake-IP.
CONNECTIVITY_FILTER = ('dns.msftncsi.com', 'www.msftncsi.com', 'www.msftconnecttest.com')
_SIMPLE_PATTERN = re.compile(r'^(?:\+\.|\.|\*\.)?[a-z0-9_*.-]+$')
_VERSION = re.compile(r'\s*(?:mihomo\s+)?(?:meta\s+)?v?(\d+)\.(\d+)\.(\d+)', re.IGNORECASE)


@lru_cache(maxsize=1)
def _extractor():
    import tldextract
    # Bundled list only (no network, no cache). Private suffixes count as public
    # here: one airport's names under github.io must not become +.github.io.
    return tldextract.TLDExtract(suffix_list_urls=(), cache_dir=None, include_psl_private_domains=True)


def _registrable(domain):
    result = _extractor()(domain)
    if result.domain and result.suffix:
        return f'{result.domain}.{result.suffix}'
    return None


def compress(domains):
    """Merge names under one registrable domain into ``+.<deepest common suffix>``.

    A name whose suffix is not on the public suffix list stays exact, and a
    merged pattern never covers more than one registrable domain.
    """
    groups, result = {}, set()
    for domain in domains:
        base = _registrable(domain)
        if base is None:
            result.add(domain)
        else:
            groups.setdefault(base, set()).add(domain)
    for base, names in groups.items():
        if len(names) == 1:
            result.update(names)
            continue
        common = []
        for labels in zip(*(reversed(name.split('.')) for name in names)):
            if len(set(labels)) != 1:
                break
            common.append(labels[0])
        suffix = '.'.join(reversed(common))
        result.add('+.' + (suffix if suffix.endswith(base) else base))
    return sorted(result, key=lambda item: (item.removeprefix('+.').split('.')[::-1], item))


def listener_address(listen):
    """Mihomo's own DNS listener as a nameserver, or (None, reason)."""
    if not isinstance(listen, str) or not listen.strip():
        return None, 'no_listen'
    host, separator, port = listen.strip().rpartition(':')
    if not separator or not port.isdigit() or not 0 < int(port) < 65536:
        return None, 'listen_unparsed'
    host = host.strip()
    if host.startswith('[') and host.endswith(']'):
        host = host[1:-1]
    if host in ('', '0.0.0.0', '::'):
        return f'udp://127.0.0.1:{int(port)}', None
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        return None, 'listen_unparsed'
    if address.is_unspecified or address.is_multicast:
        return None, 'listen_unparsed'
    return (f'udp://[{address}]:{int(port)}' if address.version == 6 else f'udp://{address}:{int(port)}'), None


def _points_to_listener(values, listen):
    """True when every given nameserver is this Mihomo's own DNS listener.

    Strict on purpose: a LAN address on the listener's port may be the router.
    """
    address, _ = listener_address(listen)
    if not address or not values:
        return False
    expected = urlsplit(address)
    unspecified = listen.strip().rpartition(':')[0].strip().strip('[]') in ('', '0.0.0.0', '::')
    for value in values:
        if not isinstance(value, str) or not value.strip():
            return False
        text = value.strip() if '://' in value else 'udp://' + value.strip()
        parts = urlsplit(text)
        try:
            port = parts.port or 53
        except ValueError:
            return False
        host = (parts.hostname or '').lower()
        if parts.scheme not in ('udp', 'tcp') or port != expected.port:
            return False
        if host == expected.hostname or (expected.hostname == '127.0.0.1' and host == 'localhost'):
            continue
        try:
            if unspecified and (host == 'localhost' or ipaddress.ip_address(host).is_loopback):
                continue
        except ValueError:
            pass
        return False
    return True


def _lookup(hosts, domain):
    """Mihomo's trie lookup: an exact key wins, then the most specific wildcard."""
    if domain in hosts:
        return hosts[domain]
    best = None
    for key, value in hosts.items():
        if key != domain and _matches(key, domain) and (best is None or len(key) > len(best[0])):
            best = (key, value)
    return best[1] if best else None


def final_target(hosts, domain):
    """The domain a hosts chain from ``domain`` ends on, or None when it ends in IPs."""
    hosts = {str(key).strip().lower().rstrip('.'): value for key, value in (hosts or {}).items()}
    value, seen = _lookup(hosts, domain), set()
    while value is not None:
        if isinstance(value, list):
            if len(value) == 1 and isinstance(value[0], str) and not _address(value[0]):
                value = value[0]
            else:
                return None
        if not isinstance(value, str) or _address(value):
            return None
        target = value.strip().lower().rstrip('.')
        if target in seen:
            return None
        seen.add(target)
        following = _lookup(hosts, target)
        if following is None:
            return target
        value = following
    return None


# ------------------------------------------------------------------ core version

def parse_core_version(text):
    """(major, minor, patch) of a Mihomo version such as ``v1.19.33``, or None (alpha builds, unknown text)."""
    match = _VERSION.match(text) if isinstance(text, str) else None
    return tuple(int(part) for part in match.groups()) if match else None


def core_too_old(version):
    """True only for a recognised version older than the one that reads the policy."""
    parsed = parse_core_version(version)
    return parsed is not None and parsed < MIN_CORE_VERSION


def reported_core_version(agent):
    """The Mihomo version an Agent last reported, or None when ConfigFlow does not know it.

    Heartbeats carry no core version; only domain discovery reports it, every
    five minutes while it is enabled. A report left from before it was turned
    off may be stale, so it is not used.
    """
    if not isinstance(agent, dict) or agent.get('service_type', 'mihomo') != 'mihomo' \
            or not agent.get('domain_discovery_enabled') or not isinstance(agent.get('id'), str):
        return None
    try:
        from backend.agents.traffic_store import get_traffic_store
        status = get_traffic_store().load_status(agent['id']) or {}
    except Exception:
        return None
    version = status.get('mihomo_version') if isinstance(status, dict) else None
    return version.strip() if isinstance(version, str) and version.strip() else None


def outdated_agents(profile_id):
    """Mihomo Agents of a profile whose reported core ignores the policy: ``[{'name', 'version'}]``."""
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_default_profile_id
    default = get_default_profile_id()
    found = []
    for agent in get_agent_manager().get_all_agents():
        if (agent.get('profile_id') or default) != profile_id:
            continue
        version = reported_core_version(agent)
        if version and core_too_old(version):
            found.append({'name': str(agent.get('name') or agent.get('id')), 'version': version})
    return found


# --------------------------------------------------------------- fake-ip filter

def _written_filter(dns):
    value = dns.get('fake-ip-filter', list(CONNECTIVITY_FILTER))
    return [] if value is None else value


def _simple_patterns(entries):
    """Plain domain patterns of a filter list; geosite: / rule-set: entries cannot be evaluated."""
    return [entry.strip().lower().rstrip('.') for entry in entries
            if isinstance(entry, str) and _SIMPLE_PATTERN.match(entry.strip().lower())]


def _unchecked(entries):
    return [entry for entry in entries if isinstance(entry, str) and not _SIMPLE_PATTERN.match(entry.strip().lower())]


def _covered(patterns, domain):
    return any(_matches(pattern, domain) for pattern in patterns)


def _target_rules(entries):
    return [f"DOMAIN-SUFFIX,{entry[2:]},real-ip" if entry.startswith('+.') else f'DOMAIN,{entry},real-ip'
            for entry in entries]


# --------------------------------------------------------------------- policy

def _policy_keys(policy):
    """(pattern, value) of each plain domain key, and the geosite: / rule-set: keys Mihomo checks in order."""
    plain, matchers = [], []
    for key, value in policy.items():
        text = str(key).strip()
        if text.lower().startswith(('geosite:', 'rule-set:')):
            matchers.append(text)
            continue
        plain.extend((part.strip().lower().rstrip('.'), value) for part in text.split(',') if part.strip())
    return plain, matchers


def _user_policy_for(plain, domain):
    """The value of the user's most specific plain policy key covering ``domain``, or None."""
    matches = [(pattern, value) for pattern, value in plain if _matches(pattern, domain)]
    if not matches:
        return None
    exact = [item for item in matches if item[0] == domain]
    return (exact or sorted(matches, key=lambda item: len(item[0]), reverse=True))[0]


WARNINGS = {
    'dns_disabled': 'DNS 没有开启（dns.enable），没有本机 DNS 监听可用',
    'use_hosts_off': 'dns.use-hosts 为 false，DNS 监听不使用 hosts',
    'no_listen': '没有配置 dns.listen，没有本机 DNS 监听可用',
    'listen_unparsed': 'dns.listen 不是「IP:端口」形式，无法推导本机 DNS 监听地址',
    'no_nameserver': ('没有写 proxy-server-nameserver：Mihomo 要求先有它才接受 proxy-server-nameserver-policy，'
                      '否则拒绝加载配置；ConfigFlow 不替你填写，在基础配置的 dns 里写上（例如公网 DoH）后重新生成'),
    'policy_not_mapping': '手写的 proxy-server-nameserver-policy 不是「域名: DNS」的映射，ConfigFlow 无法追加',
    'filter_not_list': 'dns.fake-ip-filter 不是列表，ConfigFlow 无法加入映射目标，映射目标会拿到假地址，所以没有改动',
}


def _old_core_warning(version):
    return (f'Mihomo 内核 {version} 早于 {MIN_CORE_TEXT}，不认识 proxy-server-nameserver-policy，'
            '所以没有加入；升级内核后重新推送，映射才对节点生效')


def apply_node_dns(main, merged, *, core_version=None):
    """Resolve mapped node domains through the DNS listener when a merged mapping needs it.

    ``merged`` holds the subscription entries merged into ``main['hosts']``;
    their keys are server domains of delivered nodes. Returns None (and leaves
    ``main`` untouched) when no merged mapping ends on a domain; otherwise a
    report for the preview. ``core_version`` is the target Mihomo's version
    when known (Agent publication); an older core gets nothing added.
    """
    hosts = main.get('hosts') if isinstance(main.get('hosts'), dict) else {}
    mapped = {}
    for domain, value in merged.items():
        target = final_target(hosts, domain) if isinstance(value, str) else None
        if target:
            mapped[domain] = target
    if not mapped:
        return None
    report = {'mapped': len(mapped), 'applied': False, 'listener': None, 'policy': [], 'filter': [],
              'targets_filtered': 0, 'targets_covered': 0, 'fake_ip': None, 'skipped': [],
              'warnings': [], 'notes': [], 'min_version': MIN_CORE_TEXT}
    dns = main.get('dns')
    if not isinstance(dns, dict) or dns.get('enable') is not True:
        report['warnings'].append(WARNINGS['dns_disabled'])
        return report
    if dns.get('use-hosts') is False:
        report['warnings'].append(WARNINGS['use_hosts_off'])
        return report
    address, reason = listener_address(dns.get('listen'))
    if not address:
        report['warnings'].append(WARNINGS[reason])
        return report
    report['listener'] = address
    if core_version and core_too_old(core_version):
        report['warnings'].append(_old_core_warning(core_version))
        return report
    if not dns.get('proxy-server-nameserver'):
        report['warnings'].append(WARNINGS['no_nameserver'])
        return report
    written = dns.get(POLICY_KEY)
    if written is not None and not isinstance(written, dict):
        report['warnings'].append(WARNINGS['policy_not_mapping'])
        return report
    plain, matchers = _policy_keys(written or {})

    # Mappings the user's own policy already decides.
    chosen = {}
    for domain, target in mapped.items():
        found = _user_policy_for(plain, domain)
        if found is None:
            chosen[domain] = {'domain': domain, 'target': target, 'source': 'configflow'}
            continue
        values = found[1] if isinstance(found[1], list) else [found[1]]
        if _points_to_listener(values, dns['listen']):
            chosen[domain] = {'domain': domain, 'target': target, 'source': 'user'}
        else:
            report['skipped'].append({'domain': domain, 'target': target, 'reason': 'policy_conflict'})
            written_for = (f'你已为 {domain} 写了 proxy-server-nameserver-policy' if found[0] == domain else
                           f'你写的 proxy-server-nameserver-policy 条目 {found[0]} 覆盖了 {domain}')
            report['warnings'].append(f'{written_for}，保留你的设置；这条映射对该节点不生效')
    if matchers:
        report['notes'].append('你写的 proxy-server-nameserver-policy 里有 geosite / rule-set 条目，ConfigFlow 无法检查；'
                               'Mihomo 按顺序先匹配它们，匹配到的节点域名仍按你的设置解析')

    additions, filter_value = [], None
    if dns.get('enhanced-mode') == 'fake-ip':
        mode = dns.get('fake-ip-filter-mode') or 'blacklist'
        current = (dns.get('fake-ip-filter') or []) if mode == 'rule' else _written_filter(dns)
        if not isinstance(current, list):
            report['warnings'].append(WARNINGS['filter_not_list'])
            return report
        report['fake_ip'] = mode
        if mode == 'whitelist':
            # Listed domains get fake addresses here: a covered target would hand
            # its node a fake address through the listener, so that mapping stays out.
            patterns = _simple_patterns(current)
            for domain, item in list(chosen.items()):
                if _covered(patterns, item['target']):
                    del chosen[domain]
                    report['skipped'].append({'domain': domain, 'target': item['target'], 'reason': 'whitelist_covers'})
                    report['warnings'].append(f'fake-ip-filter-mode 为 whitelist，名单覆盖了映射目标 {item["target"]}，'
                                              f'改走本机 DNS 监听会让 {domain} 拿到假地址，所以没有改动这条')
            if chosen:
                report['notes'].append('fake-ip-filter-mode 为 whitelist：只有名单内的域名返回假地址，映射目标不在名单内，'
                                       '无需改动 fake-ip-filter' + ('；名单里的 geosite / rule-set 条目无法检查' if _unchecked(current) else ''))
        else:
            targets = {item['target'] for item in chosen.values()}
            patterns = [] if mode == 'rule' else _simple_patterns(current)
            needed = {target for target in targets if not _covered(patterns, target)}
            report['targets_covered'] = len(targets - needed)
            additions = compress(needed)
            report['targets_filtered'] = len(needed)
            if mode == 'rule':
                rules = _target_rules(additions)
                filter_value = rules + [rule for rule in current if rule not in rules]
            else:
                filter_value = list(current) + [entry for entry in additions if entry not in current]
    else:
        report['fake_ip'] = 'not_needed'
    if not chosen:
        return report

    policy = dict(written or {})
    for domain, item in chosen.items():
        if item['source'] == 'configflow':
            policy[domain] = address
    if policy != (written or {}):
        dns[POLICY_KEY] = policy
    if additions:
        dns['fake-ip-filter'] = filter_value
    report['policy'] = list(chosen.values())
    report['filter'] = additions
    report['applied'] = True
    if parse_core_version(core_version) is None:
        report['notes'].append(f'需要 Mihomo {MIN_CORE_TEXT} 或更新：更旧的内核会忽略 proxy-server-nameserver-policy，'
                               '这些节点照旧连接左侧域名自己的地址')
    return report
