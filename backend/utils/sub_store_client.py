"""Sub-Store API 客户端

通过 Sub-Store 的 API 获取订阅的 proxies YAML，替代内置的订阅解析和节点转换。

Sub-Store API 参考:
  - GET  /api/subs              — 获取所有订阅
  - POST /api/subs              — 创建订阅 {name, url, ...}
  - GET  /api/sub/:name         — 获取单个订阅
  - GET  /download/:name        — 下载订阅 (query: target, url, ua, noCache 等)
  - GET  /download/:name/:target — 下载订阅 (target 放在路径中)
"""
import os
import uuid
from collections import OrderedDict
from copy import deepcopy
from datetime import datetime, timezone
from concurrent.futures import Future
from threading import Lock
from time import monotonic
from urllib.parse import quote

import requests
import yaml

from backend.utils.logger import get_logger
from backend.utils.sub_store_runtime import runtime
from backend.utils.proxy_utils import fix_proxy_fields
from backend.utils.url_utils import safe_exception_details, safe_url_for_log

logger = get_logger(__name__)

# Shared by manual refresh, provider delivery and aggregation within this process.
# Automatic delivery tolerates five minutes; explicit refresh keeps a short debounce.
FETCH_REUSE_SECONDS = 300
MANUAL_REUSE_SECONDS = 30
BACKGROUND_REUSE_SECONDS = 900
BLOCKED_REUSE_SECONDS = 60
FETCH_CACHE_LIMIT = 128
_fetch_lock = Lock()
_fetch_results = {}
_fetch_failures = {}


class SubscriptionResult(tuple):
    """Keep the existing two-value API while carrying already-fetched traffic."""

    def __new__(cls, text, source, traffic=None, fetched_at=None):
        result = super().__new__(cls, (text, source))
        result.traffic = traffic
        result.fetched_at = fetched_at or datetime.now(timezone.utc).isoformat()
        return result


class SubscriptionFetchError(Exception):
    """Safe public error retaining both underlying failures for callers."""

    def __init__(self, sub_store_error, direct_error):
        super().__init__("subscription fetch failed via Sub-Store and direct fallback")
        self.sub_store_error = sub_store_error
        self.direct_error = direct_error


def _get_base_url():
    """获取 sub-store API 基础 URL

    官方镜像固定使用内置 Sub-Store；仅本地开发（无内置 bundle）时
    才读取环境变量 SUB_STORE_URL 指向自己启动的 Sub-Store。
    """
    from backend.utils.dependencies import builtin_sub_store_url, is_builtin_available
    if is_builtin_available():
        return builtin_sub_store_url()
    return (os.environ.get('SUB_STORE_URL') or builtin_sub_store_url()).rstrip('/')


def _is_yaml_response(text):
    """检查响应文本是否像有效的 YAML（而非 HTML 页面）"""
    stripped = text.strip()
    # HTML 页面特征
    if stripped.startswith('<!') or stripped.startswith('<html') or '<head>' in stripped[:500]:
        return False
    # 有效的 proxies YAML 应该包含 proxies 键
    if 'proxies:' in stripped[:200] or stripped.startswith('proxies:'):
        return True
    # 尝试 YAML 解析，如果结果是 dict 且包含 proxies，则有效
    try:
        data = yaml.safe_load(stripped)
        if isinstance(data, dict) and 'proxies' in data:
            return True
    except Exception:
        pass
    return False


def _fetch_direct_subscription_yaml(url):
    """直接拉取订阅 URL，并在其本身已返回 proxies YAML 时直接使用。"""
    headers = {
        'User-Agent': 'clash.meta'
    }
    logger.info("Direct subscription fetch started")
    resp = requests.get(url, headers=headers, timeout=30)
    resp.raise_for_status()

    text = resp.text
    if not _is_yaml_response(text):
        preview = text[:200].replace('\n', '\\n')
        raise Exception(
            "订阅 URL 未直接返回有效的 proxies YAML。"
            f"响应预览: {preview}"
        )

    logger.info("订阅 URL 直接返回有效 YAML，跳过 Sub-Store 转换")
    return text


def _create_subscription(base, sub_id, url, *, content=None):
    """在 sub-store 中创建临时订阅。

    Returns:
        True 如果创建成功
    """
    # 追加 #noFlow 让 Sub-Store 跳过流量信息拉取（下载前会剥掉该片段）。
    # 该步骤单次超时 8s，会把聚合 provider 拖过 Mihomo 的拉取超时；
    # 已带 # 片段的 URL 是 Sub-Store 自有参数，保持原样避免破坏。
    if '#' not in url:
        url = f'{url}#noFlow'
    payload = {'name': sub_id, 'url': url, 'noFlow': True}
    if content is not None:
        # Convert the body we already downloaded, not the upstream URL again.
        payload.update(source='local', content=content)
    try:
        logger.info(f"Sub-Store 创建临时订阅 '{sub_id}'")
        create_resp = requests.post(
            f'{base}/api/subs',
            json=payload,
            timeout=10
        )
        if create_resp.status_code in (200, 201):
            return True
        logger.warning(f"Sub-Store 创建订阅 '{sub_id}' 返回状态码: {create_resp.status_code}")
        return False
    except Exception as e:
        logger.warning("Sub-Store 创建订阅失败: %s", safe_exception_details(e))
        return False


def _delete_subscription(base, sub_id):
    """从 sub-store 中删除订阅（用完即删）。"""
    try:
        resp = requests.delete(f'{base}/api/sub/{quote(sub_id, safe="")}', timeout=10)
        if resp.status_code in (200, 204):
            logger.info(f"Sub-Store 已删除临时订阅 '{sub_id}'")
        else:
            logger.debug(f"Sub-Store 删除订阅 '{sub_id}' 返回状态码: {resp.status_code}")
    except Exception as e:
        logger.debug("Sub-Store 删除订阅失败（可忽略）: %s", safe_exception_details(e))


def get_subscription_proxies_yaml(sub_id, url, target='ClashMeta', *, refresh=False, prefer_cached=False):
    """合并同 URL / 格式的并发请求，自动复用 5 分钟，手动刷新复用 30 秒。

    跨订阅 ID / 配置空间共享原始响应；筛选与图校验仍由调用方独立进行。
    多进程部署中每个进程各自合并，不是持久化节点缓存。
    """
    key = (url, target, _get_base_url())
    with _fetch_lock:
        now = monotonic()
        for old_key, (_, last_failure) in list(_fetch_failures.items()):
            if now - last_failure >= 3600:
                del _fetch_failures[old_key]
        for old_key, (future, expires) in list(_fetch_results.items()):
            retention = (BACKGROUND_REUSE_SECONDS - FETCH_REUSE_SECONDS
                         if future.done() and future.exception() is None else 0)
            if future.done() and expires + retention <= now:
                del _fetch_results[old_key]
        if key in _fetch_results:
            future, expires = _fetch_results[key]
            reuse = (MANUAL_REUSE_SECONDS if refresh else
                     BACKGROUND_REUSE_SECONDS if prefer_cached else FETCH_REUSE_SECONDS)
            # Never bypass an in-flight request or a failure's cooldown.
            if (future.done() and future.exception() is None
                    and now >= expires - FETCH_REUSE_SECONDS + reuse):
                del _fetch_results[key]
        owner = key not in _fetch_results
        if owner:
            # Evict only completed entries; never evict a flight with waiters.
            completed = [k for k, (f, _) in _fetch_results.items() if f.done()]
            while len(_fetch_results) >= FETCH_CACHE_LIMIT and completed:
                del _fetch_results[completed.pop(0)]
            future = Future()
            tracked = len(_fetch_results) < FETCH_CACHE_LIMIT
            if tracked:
                _fetch_results[key] = (future, float('inf'))
        else:
            future, _ = _fetch_results[key]
    if not owner:
        logger.info('复用订阅近期结果或等待正在进行的拉取')
        return future.result()
    try:
        result = _get_subscription_proxies_yaml(sub_id, url, target)
    except BaseException as exc:
        future.set_exception(exc)
        with _fetch_lock:
            if tracked:
                failures = min(_fetch_failures.get(key, (0, 0))[0] + 1, 8)
                if key not in _fetch_failures and len(_fetch_failures) >= FETCH_CACHE_LIMIT:
                    del _fetch_failures[next(iter(_fetch_failures))]
                _fetch_failures[key] = (failures, monotonic())
                direct_error = getattr(exc, 'direct_error', None)
                status = getattr(getattr(direct_error, 'response', None), 'status_code', None)
                cooldown = min(300, 3 * 2 ** (failures - 1))
                if status in (403, 429):
                    cooldown = max(BLOCKED_REUSE_SECONDS, cooldown)
                _fetch_results[key] = (future, monotonic() + cooldown)
        raise
    future.set_result(result)
    with _fetch_lock:
        _fetch_failures.pop(key, None)
        if tracked:
            _fetch_results[key] = (future, monotonic() + FETCH_REUSE_SECONDS)
    return result


def _get_subscription_proxies_yaml(sub_id, url, target='ClashMeta'):
    """获取订阅的 proxies YAML，并返回真实来源。

    自动创建 sub-store 中的临时订阅（如果需要）。

    Args:
        sub_id: 订阅 ID，用作 sub-store 中的订阅 name
        url: 原始订阅 URL
        target: 目标格式，默认 ClashMeta

    Returns:
        tuple[str, str]: (目标格式文本, 来源)
            来源可能为:
              - rendered_yaml: 订阅 URL 本身已是 Sub-Store 导出的最终 YAML
              - direct_yaml: 普通订阅 URL 已返回目标 YAML
              - sub_store: 通过 Sub-Store 下载/转换成功
              - direct_url_fallback: Sub-Store 失败后直接拉取原始订阅 URL 成功

    Raises:
        Exception: 获取失败或返回的不是有效 YAML
    """
    from backend.utils.subscription_health import parse_userinfo

    raw_text = None
    traffic = None
    direct_error = None
    # URL fragments may contain Sub-Store UA/proxy/insecure/merge options.
    # Leave those to Sub-Store instead of silently bypassing their semantics.
    direct_attempted = '#' not in url and url.lower().startswith(('http://', 'https://'))
    if direct_attempted:
        try:
            logger.info('拉取订阅正文与流量信息（单次请求）')
            resp = requests.get(url, headers={'User-Agent': 'clash.meta'}, timeout=30)
            resp.raise_for_status()
            raw_text = resp.text
            traffic = parse_userinfo(resp.headers.get('subscription-userinfo', ''))
            if _is_yaml_response(raw_text) and target == 'ClashMeta':
                source = 'rendered_yaml' if 'sub-store' in resp.headers.get('x-powered-by', '').lower() else 'direct_yaml'
                return SubscriptionResult(raw_text, source, traffic)
        except Exception as exc:
            direct_error = exc
            if getattr(getattr(exc, 'response', None), 'status_code', None) in (403, 429):
                # A blocked/rate-limited upstream must not be retried through
                # Sub-Store immediately; that would compound the restriction.
                logger.warning('订阅上游拒绝或限流，暂缓拉取: %s', safe_exception_details(exc))
                raise SubscriptionFetchError(None, exc) from exc
            logger.warning('直接拉取订阅失败，尝试 Sub-Store: %s', safe_exception_details(exc))

    base = _get_base_url()
    logger.info(f"Sub-Store base URL: {safe_url_for_log(base)}")

    # 使用带前缀和随机后缀的临时名称，避免和 Sub-Store 中已有订阅冲突，
    # 也避免并发请求同一订阅时临时订阅撞名导致 500
    temp_name = f"_cf_tmp_{sub_id}_{uuid.uuid4().hex[:8]}"
    sub_store_error = None
    try:
        with runtime.use():
            _create_subscription(base, temp_name, url, content=raw_text)
            try:
                # 下载订阅（target 放在路径中，这是 Sub-Store 推荐的方式）
                download_url = f'{base}/download/{quote(temp_name, safe="")}/{target}'
                logger.info(f"Sub-Store 下载: {safe_url_for_log(download_url)}")

                resp = requests.get(download_url, params={'noFlow': 'true'}, timeout=30)
                resp.raise_for_status()

                text = resp.text

                # 验证响应是有效的 YAML 而非 HTML 页面
                if not _is_yaml_response(text):
                    preview = text[:200].replace('\n', '\\n')
                    raise Exception(
                        f"Sub-Store 返回了非 YAML 内容（可能是 HTML 页面），"
                        f"请检查 Sub-Store URL 配置是否正确。响应预览: {preview}"
                    )

                traffic = traffic or parse_userinfo(resp.headers.get('subscription-userinfo', ''))
                return SubscriptionResult(text, 'sub_store', traffic)
            finally:
                _delete_subscription(base, temp_name)
    except Exception as e:
        sub_store_error = e
        logger.warning("Sub-Store 获取订阅失败: %s", safe_exception_details(e))

    # Do not re-download a URL already attempted above, even on conversion failure.
    if direct_attempted:
        if direct_error is None:
            direct_error = ValueError('已拉取的订阅正文无法转换为目标 YAML')
        raise SubscriptionFetchError(sub_store_error, direct_error) from direct_error
    try:
        text = _fetch_direct_subscription_yaml(url)
        return SubscriptionResult(text, 'direct_url_fallback')
    except Exception as direct_error:
        logger.warning("直接拉取订阅失败: %s", safe_exception_details(direct_error))
        raise SubscriptionFetchError(sub_store_error, direct_error) from direct_error


NODE_CONVERSION_LIMIT = 512
_node_conversions = OrderedDict()  # (Sub-Store version, target, node string) -> converted proxy
_node_conversions_lock = Lock()


def _node_conversion_version():
    """Version of the bundled Sub-Store, or None where conversions are not kept.

    A node converts to the same proxy for as long as the converter is the same
    program. Only the bundled Sub-Store's version is known without asking it;
    one reached through SUB_STORE_URL can change without notice.
    """
    from backend.utils.dependencies import active_bundle, is_builtin_available
    try:
        return (active_bundle()[1] or None) if is_builtin_available() else None
    except Exception:
        return None


def convert_proxy_string(proxy_string, target='ClashMeta'):
    """通过 Sub-Store 将单个节点字符串（URI / YAML）转换为 mihomo 格式的 proxy dict。

    成功的结果按内置 Sub-Store 的版本记住：每次核对配置、生成订阅聚合都要转换
    同样的节点，而按需模式下每问一次都要把 Sub-Store 启动起来、再多留几分钟。

    Args:
        proxy_string: 节点字符串，如 vless://...、ss://... 或 YAML 对象
        target: 目标格式，默认 ClashMeta

    Returns:
        dict 或 None: 转换后的 mihomo proxy dict，失败返回 None
    """
    version = _node_conversion_version() if isinstance(proxy_string, str) else None
    key = (version, target, proxy_string)
    if version is not None:
        with _node_conversions_lock:
            kept = _node_conversions.get(key)
            if kept is not None:
                _node_conversions.move_to_end(key)
                return deepcopy(kept)
    proxy = _convert_proxy_string(proxy_string, target)
    # An online update in between means another program may have answered.
    if proxy is not None and version is not None and _node_conversion_version() == version:
        with _node_conversions_lock:
            _node_conversions[key] = deepcopy(proxy)
            _node_conversions.move_to_end(key)
            while len(_node_conversions) > NODE_CONVERSION_LIMIT:
                _node_conversions.popitem(last=False)
    return proxy


def _convert_proxy_string(proxy_string, target):
    base = _get_base_url()
    proxy_type = "unknown"
    if isinstance(proxy_string, str) and '://' in proxy_string:
        candidate = proxy_string.split('://', 1)[0].lower()
        if candidate.replace('+', '').replace('-', '').replace('.', '').isalnum():
            proxy_type = candidate[:32]
    input_length = len(proxy_string) if isinstance(proxy_string, str) else 0
    logger.info("节点转换开始: proxy_type=%s input_length=%d", proxy_type, input_length)

    # 创建临时订阅（需要一个名字才能调用 download）
    # 随机后缀避免并发节点转换时撞名导致 500
    temp_name = f"_cf_tmp_node_convert_{uuid.uuid4().hex[:8]}"
    try:
        with runtime.use():
            _create_subscription(base, temp_name, 'https://example.com')
            try:
                # 通过 content 参数直接传入节点内容，覆盖订阅 URL
                download_url = f'{base}/download/{quote(temp_name, safe="")}/{target}'
                resp = requests.get(
                    download_url,
                    params={'content': proxy_string},
                    timeout=15
                )
                resp.raise_for_status()

                text = resp.text

                if not _is_yaml_response(text):
                    logger.warning("节点转换返回无效内容: proxy_type=%s input_length=%d", proxy_type, input_length)
                    return None

                proxies = parse_proxies_from_yaml(text)
                if proxies:
                    logger.info("节点转换成功: proxy_type=%s input_length=%d", proxy_type, input_length)
                    return proxies[0]
                logger.warning("节点转换结果为空: proxy_type=%s input_length=%d", proxy_type, input_length)
                return None
            finally:
                _delete_subscription(base, temp_name)
    except Exception:
        logger.warning("节点转换失败: proxy_type=%s input_length=%d", proxy_type, input_length)
        return None


def parse_proxies_from_yaml(yaml_text):
    """从 sub-store 返回的 YAML 文本中解析 proxies 列表。

    Args:
        yaml_text: sub-store 返回的 YAML 文本

    Returns:
        list[dict]: proxies 列表（mihomo 格式的 dict）
    """
    data = yaml.safe_load(yaml_text)
    if isinstance(data, dict):
        proxies = data.get('proxies', [])
        for proxy in proxies:
            fix_proxy_fields(proxy)
        return proxies
    return []


def proxies_to_nodes(proxies):
    """将 mihomo 格式的 proxies 列表转换为内部 node 格式（用于缓存和 UI 展示）。

    Args:
        proxies: mihomo 格式的 proxy dict 列表

    Returns:
        list[dict]: 内部 node 格式列表
    """
    nodes = []
    for proxy in proxies:
        node = {
            'name': proxy.get('name', 'Unnamed'),
            'type': proxy.get('type', '').lower(),
            'server': proxy.get('server', ''),
            'port': proxy.get('port', 0),
            'params': {k: v for k, v in proxy.items() if k not in ('name', 'type', 'server', 'port')}
        }
        nodes.append(node)
    return nodes
