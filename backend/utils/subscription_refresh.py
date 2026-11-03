"""Keep upstream subscription refresh separate from client/provider polling."""
from datetime import datetime, timezone
from hashlib import sha256
import json

import yaml

from backend.utils.strategy_references import StrategyReferenceError

DEFAULT_SOURCE_INTERVAL = 86400
DEFAULT_PROVIDER_INTERVAL = 3600
MAX_INTERVAL = 604800


def validate_refresh_interval(value, label='订阅源更新间隔'):
    if type(value) is not int or (value != 0 and not 60 <= value <= MAX_INTERVAL):
        raise ValueError(f'{label}必须为 0（关闭自动更新）或 60 ~ {MAX_INTERVAL} 秒的整数')
    return value


def source_cache_metadata(sub, result):
    metadata = {'subscription_name': sub.get('name', ''), 'url': sub.get('url'),
                'fetched_at': getattr(result, 'fetched_at', None)}
    # Only a downloaded body can carry the subscription's own top-level hosts;
    # Sub-Store conversions and cache renders contain proxies alone.
    try:
        text, source = result
    except (TypeError, ValueError):
        text, source = None, None
    if isinstance(text, str) and source not in ('cache', 'prepared_yaml'):
        from backend.utils.subscription_hosts import extract_subscription_hosts
        hosts = extract_subscription_hosts(text)
        if hosts is not None:
            metadata['hosts'] = hosts
        # Groups the subscription's own chained nodes need; see subscription_dialers.
        from backend.utils.subscription_dialers import extract_subscription_dialers
        dialers = extract_subscription_dialers(text)
        if dialers is not None:
            metadata['dialers'] = dialers
    return metadata


def _is_fresh(timestamp, interval):
    try:
        updated = datetime.fromisoformat(timestamp.replace('Z', '+00:00'))
        if updated.tzinfo is None:
            updated = updated.replace(tzinfo=timezone.utc)
        age = (datetime.now(timezone.utc) - updated).total_seconds()
        return 0 <= age < interval
    except (TypeError, ValueError, AttributeError):
        return False


def _download_cache_path(url):
    # A transport cache is separate from validated/published node caches. It
    # also bounds upstream requests made by dry previews and background checks.
    key = sha256(url.encode('utf-8')).hexdigest()
    return f'subscription-downloads/{key}.json'


def remember_source_download(sub, result):
    from backend.common.config import get_repository
    from backend.utils.sub_store_client import SubscriptionResult
    text, source = result
    result = SubscriptionResult(text, source, getattr(result, 'traffic', None),
                                getattr(result, 'fetched_at', None))
    get_repository().write_shared_json(_download_cache_path(sub['url']), {
        'text': text, 'fetched_at': result.fetched_at, 'traffic': result.traffic,
    })
    return result


def download_subscription_nodes(sub):
    """Download, parse and store a subscription: (nodes, result, cache).

    A failure raises before anything is stored, so the previous nodes stay.
    """
    import uuid
    from backend.utils.sub_store_client import get_subscription_proxies_yaml, parse_proxies_from_yaml, proxies_to_nodes
    from backend.utils.subscription_cache import save_subscription_nodes
    result = remember_source_download(sub, get_subscription_proxies_yaml(sub['id'], sub['url'], refresh=True))
    proxies = parse_proxies_from_yaml(result[0])
    if not proxies:
        raise ValueError('订阅没有返回任何节点，已保留原有缓存')
    nodes = proxies_to_nodes(proxies)
    for node in nodes:
        node['subscription_id'] = sub['id']
        node['subscription_name'] = sub['name']
        node.setdefault('id', f"node_{uuid.uuid4().hex[:8]}")
    cache = save_subscription_nodes(sub['id'], nodes, source_cache_metadata(sub, result))
    return nodes, result, cache


def fetch_subscription_source(sub, *, fetch):
    """Automatic delivery uses the stored nodes; see the scheduled task.

    Manual refresh deliberately calls the transport directly. Returned cached
    content still goes through each consumer's normal filtering and validation.
    Never rewrite its timestamp merely because a client polled the panel.
    """
    from backend.utils.subscription_cache import load_subscription_cache
    from backend.utils.sub_store_client import SubscriptionResult
    from backend.common.config import get_repository
    from backend.common.config_repository import ProfileRepositoryError

    interval = sub.get('interval', DEFAULT_SOURCE_INTERVAL)
    try:
        validate_refresh_interval(interval)
    except ValueError as exc:
        raise StrategyReferenceError(str(exc)) from exc
    cache = load_subscription_cache(sub['id'])
    if cache and cache.get('metadata', {}).get('url') not in (None, sub.get('url')):
        cache = None
    cached_nodes = cache.get('nodes') if cache else None
    usable = isinstance(cached_nodes, list) and bool(cached_nodes)
    # The scheduled task (系统设置 → 定时任务) refreshes stored nodes when the
    # interval is due and keeps them when a refresh fails, so deliveries use
    # nodes stored for this URL as they are. Legacy node-only caches remain
    # usable in manual mode, but cannot prove which URL they came from; they
    # are downloaded once more and then carry the URL.
    current = usable and cache.get('metadata', {}).get('url') == sub.get('url')
    if interval == 0 or current:
        if not usable:
            raise StrategyReferenceError(
                f'订阅「{sub.get("name", sub["id"])}」已关闭自动更新，且没有当前订阅地址的可用缓存。'
                '请先在「订阅来源」中手动更新，再重新发布或同步。')
        from backend.utils.dialer_references import validate_shapes
        from backend.converters.mihomo import convert_node_to_mihomo
        validate_shapes({'nodes': cached_nodes}, require_ids=False)
        proxies = [proxy for node in cached_nodes if (proxy := convert_node_to_mihomo(node))]
        return SubscriptionResult(yaml.safe_dump({'proxies': proxies}, allow_unicode=True), 'cache')
    try:
        downloaded = get_repository().read_shared_json(_download_cache_path(sub['url']))
        if (isinstance(downloaded, dict) and isinstance(downloaded.get('text'), str)
                and _is_fresh(downloaded.get('fetched_at'), interval)):
            return SubscriptionResult(downloaded['text'], 'source_cache', downloaded.get('traffic'), downloaded['fetched_at'])
    except (OSError, ProfileRepositoryError, json.JSONDecodeError):
        pass
    return remember_source_download(sub, fetch(sub['id'], sub['url']))
