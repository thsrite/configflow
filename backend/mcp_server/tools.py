"""MCP 工具定义

按功能域收敛的工具集，覆盖 ConfigFlow 的全部业务能力。
每个工具通过 invoker 复用既有 REST 路由，不重复实现业务逻辑。

两处必要的适配（REST 层的既有契约决定）：
1. 部分资源（订阅/规则/策略组/规则仓库）由客户端生成 id，这里按前端同样的
   `{prefix}_{毫秒时间戳}` 惯例补齐，并避开已存在的 id。
2. 所有 PUT 都是整体替换而非局部更新，因此 update 走 read-modify-write，
   避免调用方只传部分字段时丢数据。
"""
import time
from typing import Any, Callable, Dict, List, Optional

from backend.mcp_server.invoker import (
    ApiError,
    call_api,
    reset_profile_context,
    set_profile_context,
)

# 工具注册表：name -> {'definition': {...}, 'handler': callable}
_REGISTRY: Dict[str, Dict[str, Any]] = {}


def tool(name: str, description: str, schema: Dict[str, Any]) -> Callable:
    """把一个函数注册为 MCP 工具"""

    def decorator(func: Callable) -> Callable:
        _REGISTRY[name] = {
            'definition': {
                'name': name,
                'description': description,
                'inputSchema': schema,
            },
            'handler': func,
        }
        return func

    return decorator


def list_tools() -> List[Dict[str, Any]]:
    """返回全部工具定义（tools/list）"""
    return [entry['definition'] for entry in _REGISTRY.values()]


def has_tool(name: str) -> bool:
    """工具是否存在"""
    return name in _REGISTRY


def call_tool(name: str, arguments: Dict[str, Any]) -> Any:
    """执行一个工具（tools/call）"""
    arguments = arguments or {}
    from backend.common.config import get_default_profile_id
    selected_profile_id = arguments.get('profile_id') or get_default_profile_id()
    token = set_profile_context(selected_profile_id)
    try:
        result = _REGISTRY[name]['handler'](arguments)
        if isinstance(result, dict):
            result.setdefault('profile_id', selected_profile_id)
        return result
    finally:
        reset_profile_context(token)


# ---------------------------------------------------------------- schema 助手

def obj(properties: Dict[str, Any], required: Optional[List[str]] = None) -> Dict[str, Any]:
    properties = dict(properties)
    properties.setdefault(
        'profile_id',
        {'type': 'string', 'description': "独立配置的目标 id；省略时使用当前默认配置空间，不读取浏览器选择。全局资源和系统设置不受此参数限制。"},
    )
    return {
        'type': 'object',
        'properties': properties,
        'required': required or [],
        'additionalProperties': False,
    }


NO_ARGS = obj({})


def string(desc: str, enum: Optional[List[str]] = None) -> Dict[str, Any]:
    schema: Dict[str, Any] = {'type': 'string', 'description': desc}
    if enum:
        schema['enum'] = enum
    return schema


def boolean(desc: str) -> Dict[str, Any]:
    return {'type': 'boolean', 'description': desc}


def integer(desc: str) -> Dict[str, Any]:
    return {'type': 'integer', 'description': desc}


def array(desc: str, item_type: str = 'string') -> Dict[str, Any]:
    return {'type': 'array', 'description': desc, 'items': {'type': item_type}}


def health_fields(desc: str, *, provider=False):
    from backend.utils.health_check import GROUP_DEFAULTS, PROVIDER_DEFAULTS
    defaults = PROVIDER_DEFAULTS if provider else {**GROUP_DEFAULTS, 'tolerance': 50}
    properties = {}
    for key, value in defaults.items():
        properties[key] = {'type': 'boolean' if type(value) is bool else 'integer' if type(value) is int else 'string', 'description': key + ('（毫秒）' if key in ('timeout', 'tolerance') else '（秒）' if key == 'interval' else ''), 'default': value}
        if type(value) is int:
            properties[key].update(minimum=0 if key == 'tolerance' else 1, maximum=86400 if key == 'interval' else 600000)
    if provider:
        return {'type': 'object', 'description': desc, 'additionalProperties': True, 'properties': {'health_check': {'type':'object', 'properties':properties, 'additionalProperties':False}}}
    properties['delete_fields'] = {'type': 'array', 'description': '策略组 PUT/MCP 部分更新显式删除健康字段；不能同时赋值', 'uniqueItems': True, 'items': {'type': 'string', 'enum': list(defaults)}}
    return {'type': 'object', 'description': desc, 'additionalProperties':True, 'properties':properties}


def provider_fields(desc):
    return health_fields(desc, provider=True)


def merge_resource_data(current, data, item_id):
    """MCP partial edits preserve sibling health settings, including false."""
    merged = {**current, **data, 'id': item_id}
    if isinstance(data.get('health_check'), dict):
        previous = current.get('health_check')
        if not isinstance(previous, dict):
            previous = {}
            if current.get('health_check_url'):
                previous['url'] = current['health_check_url']
        merged['health_check'] = {**previous, **data['health_check']}
    return merged


def free_object(desc: str) -> Dict[str, Any]:
    return {'type': 'object', 'description': desc, 'additionalProperties': True}


ACTION = string('操作类型', ['create', 'update', 'delete'])


# ---------------------------------------------------------------- 通用 CRUD 助手

def _new_id(prefix: str, existing: List[Dict[str, Any]]) -> str:
    """按前端惯例生成 id，并避开已有 id"""
    taken = {item.get('id') for item in existing if isinstance(item, dict)}
    base = f"{prefix}_{int(time.time() * 1000)}"
    if base not in taken:
        return base
    suffix = 1
    while f"{base}_{suffix}" in taken:
        suffix += 1
    return f"{base}_{suffix}"


def _find(items: List[Dict[str, Any]], item_id: str) -> Dict[str, Any]:
    for item in items:
        if isinstance(item, dict) and item.get('id') == item_id:
            return item
    raise ApiError(404, f"未找到 id 为 '{item_id}' 的记录")


def _require(arguments: Dict[str, Any], key: str) -> Any:
    value = arguments.get(key)
    if value in (None, ''):
        raise ApiError(400, f"缺少必填参数 '{key}'")
    return value


def _crud(
    arguments: Dict[str, Any],
    collection_path: str,
    id_prefix: Optional[str],
    label: str,
) -> Dict[str, Any]:
    """create / update / delete 的统一实现

    Args:
        collection_path: 集合路径，如 '/api/subscriptions'
        id_prefix: 需要客户端生成 id 时的前缀；None 表示服务端生成
    """
    action = _require(arguments, 'action')
    data = arguments.get('data') or {}

    if action == 'create':
        if not data:
            raise ApiError(400, f"创建{label}需要提供 data")
        payload = dict(data)
        if id_prefix and not payload.get('id'):
            existing = call_api('GET', collection_path) or []
            payload['id'] = _new_id(id_prefix, existing if isinstance(existing, list) else [])
        result = call_api('POST', collection_path, body=payload)
        return {'action': 'create', 'item': _unwrap(result, payload)}

    item_id = _require(arguments, 'id')

    if action == 'delete':
        call_api('DELETE', f"{collection_path}/{item_id}")
        return {'action': 'delete', 'id': item_id}

    if action == 'update':
        if not data:
            raise ApiError(400, f"更新{label}需要提供 data")
        # Groups have a transactional partial-merge contract; do not turn MCP
        # partial edits into stale full-object replacements.
        if collection_path == '/api/proxy-groups':
            result = call_api('PUT', f"{collection_path}/{item_id}", body=data)
            return {'action': 'update', 'item': _unwrap(result, data)}
        existing = call_api('GET', collection_path) or []
        current = _find(existing if isinstance(existing, list) else [], item_id)
        merged = merge_resource_data(current, data, item_id)
        result = call_api('PUT', f"{collection_path}/{item_id}", body=merged)
        return {'action': 'update', 'item': _unwrap(result, merged)}

    raise ApiError(400, f"不支持的 action: {action}")


# collection -> reorder 接口路径。排序按 id 提交，由服务端在存量数据上重排，
# 避免把列表接口加工过的展示数据（拼过域名的 URL、缓存计数、脱敏字段）回写进配置。
_REORDER_COLLECTIONS = {
    'subscriptions': '/api/subscriptions/reorder',
    'nodes': '/api/nodes/reorder',
    'rules': '/api/rules/reorder',
    'rule_library': '/api/rule-library/reorder',
    'proxy_groups': '/api/proxy-groups/reorder',
}
_REORDER_NAMES = list(_REORDER_COLLECTIONS)


def _reorder(collection: str, ids: List[str], position: str) -> Dict[str, Any]:
    if collection not in _REORDER_COLLECTIONS:
        raise ApiError(400, f"不支持的 collection: {collection}，可选 {_REORDER_NAMES}")
    if position not in ('top', 'bottom'):
        raise ApiError(400, "position 必须是 'top' 或 'bottom'")
    if not ids:
        raise ApiError(400, '需要提供至少一个 id')
    if len(set(ids)) != len(ids):
        raise ApiError(400, 'ids 中存在重复项')

    result = call_api(
        'POST',
        _REORDER_COLLECTIONS[collection],
        body={'ids': ids, 'position': position},
    )
    return {
        'collection': collection,
        'position': position,
        'moved': ids,
        'order': (result or {}).get('order'),
    }


def _unwrap(result: Any, fallback: Any) -> Any:
    if isinstance(result, dict) and 'data' in result:
        return result['data']
    return fallback


# ================================================================ 排序

@tool(
    'reorder_items',
    '调整集合内条目的顺序。ids 里的条目按给定顺序整体移到最前（position=top，默认）'
    '或最后（position=bottom），未列出的条目保持原有相对顺序。'
    '规则顺序即匹配优先级，策略组顺序即客户端里的展示顺序。',
    obj(
        {
            'collection': string('目标集合', _REORDER_NAMES),
            'ids': array('要移动的条目 id，按目标先后顺序排列'),
            'position': string('移动到集合的哪一端，默认 top', ['top', 'bottom']),
        },
        ['collection', 'ids'],
    ),
)
def _reorder_items(args):
    return _reorder(
        _require(args, 'collection'),
        _require(args, 'ids'),
        args.get('position', 'top'),
    )


# ================================================================ Profiles

@tool(
    'list_profiles',
    '列出所有配置 profile。',
    NO_ARGS,
)
def _list_profiles(args):
    return call_api('GET', '/api/profiles')


@tool(
    'get_profile',
    '获取一个 profile 的元数据。',
    obj({'id': string('profile id')}, ['id']),
)
def _get_profile(args):
    profile_id = _require(args, 'id')
    return call_api('GET', f'/api/profiles/{profile_id}')


@tool(
    'manage_profile',
    '创建、更新或删除 profile。默认 profile 不可删除；配置选择仅保存在各浏览器会话中。',
    obj(
        {
            'action': string('操作类型', ['create', 'update', 'delete']),
            'id': string('profile id，update / delete 时必填'),
            'data': free_object('profile 元数据；create 时至少包含 id 或 name'),
        },
        ['action'],
    ),
)
def _manage_profile(args):
    action = _require(args, 'action')
    if action == 'create':
        return call_api('POST', '/api/profiles', body=args.get('data') or {})
    profile_id = _require(args, 'id')
    if action == 'update':
        return call_api('PUT', f'/api/profiles/{profile_id}', body=args.get('data') or {})
    if action == 'delete':
        call_api('DELETE', f'/api/profiles/{profile_id}')
        return {'success': True, 'profile_id': profile_id}
    raise ApiError(400, f'不支持的 action: {action}')


@tool(
    'clone_profile',
    '从一个 profile 克隆出新的 profile。',
    obj(
        {
            'source_profile_id': string('源 profile id'),
            'data': free_object('目标 profile 元数据，至少包含 id 或 name'),
        },
        ['source_profile_id', 'data'],
    ),
)
def _clone_profile(args):
    source_id = _require(args, 'source_profile_id')
    return call_api('POST', f'/api/profiles/{source_id}/clone', body=args['data'])


@tool(
    'import_client_config',
    '把 Mihomo / Surge / Loon / Shadowrocket 配置文件导入为新的 profile：节点、订阅和规则集写入共享资源'
    '（相同定义复用），策略组与规则写入新 profile。dry_run=true 时只返回解析摘要与提示。',
    obj(
        {
            'client_type': string('配置类型', ['mihomo', 'surge', 'loon', 'shadowrocket']),
            'content': string('配置文件全文'),
            'name': string('新 profile 名称'),
            'id': string('新 profile id，可选'),
            'description': string('说明，可选'),
            'default_subscription_ids': array('仅含筛选条件（include-all / 正则）的策略组额外关联的已有订阅 id'),
            'dry_run': boolean('只解析不写入'),
        },
        ['client_type', 'content', 'name'],
    ),
)
def _import_client_config(args):
    return call_api('POST', '/api/profiles/import-client', body=args)


@tool(
    'bind_agent_profile',
    '把 Agent 绑定到指定 profile；后续配置读取和推送都使用该 profile。',
    obj(
        {'id': string('Agent id'), 'profile_id': string('目标 profile id')},
        ['id', 'profile_id'],
    ),
)
def _bind_agent_profile(args):
    agent_id = _require(args, 'id')
    profile_id = _require(args, 'profile_id')
    current = call_api('GET', f'/api/agents/{agent_id}') or {}
    return call_api('PUT', f'/api/agents/{agent_id}', body={**current, 'profile_id': profile_id})


# ================================================================ 订阅

@tool(
    'list_subscriptions',
    '列出所有订阅源，含名称、URL、启用状态和缓存的节点数。'
    '需要订阅 id 时先调用本工具。',
    NO_ARGS,
)
def _list_subscriptions(args):
    return call_api('GET', '/api/subscriptions')


@tool(
    'manage_subscription',
    '创建、更新或删除一个订阅源。create 时 data 至少包含 name 和 url；'
    'update 只需给出要改的字段，其余字段自动保留。',
    obj(
        {
            'action': ACTION,
            'id': string('订阅 id，update / delete 时必填'),
            'data': provider_fields(
                '订阅字段：name（名称）、url（订阅链接）、type（mihomo / surge / general）、'
                'enabled（是否启用）、interval（更新间隔秒）、'
                'regex_filter（节点名筛选正则，只保留匹配的节点）、'
                'health_check_url（健康检查地址，留空用默认）'
            ),
        },
        ['action'],
    ),
)
def _manage_subscription(args):
    return _crud(args, '/api/subscriptions', 'sub', '订阅')


@tool(
    'fetch_subscription',
    '拉取指定订阅的最新内容并刷新节点缓存。preview=true 时只解析不写入缓存。',
    obj(
        {
            'id': string('订阅 id'),
            'preview': boolean('仅预览，不更新缓存，默认 false'),
        },
        ['id'],
    ),
)
def _fetch_subscription(args):
    sub_id = _require(args, 'id')
    return call_api(
        'POST',
        f"/api/subscriptions/{sub_id}/fetch",
        body={'preview': bool(args.get('preview', False))},
    )


@tool(
    'get_subscription_nodes',
    '获取指定订阅下已解析的节点列表。不传 id 时返回全部订阅的节点。',
    obj({'id': string('订阅 id，留空表示全部订阅')}),
)
def _get_subscription_nodes(args):
    sub_id = args.get('id')
    if sub_id:
        return call_api('GET', f"/api/subscriptions/{sub_id}/nodes")
    return call_api('GET', '/api/subscriptions/proxies')


# ================================================================ 订阅聚合

@tool(
    'list_aggregations',
    '列出所有订阅聚合（把多个订阅与手动节点合并成一个节点池）。',
    NO_ARGS,
)
def _list_aggregations(args):
    return call_api('GET', '/api/aggregations')


@tool(
    'manage_aggregation',
    '创建、更新或删除订阅聚合。create 时 data 至少包含 name；'
    'subscriptions 为订阅 id 列表，nodes 为手动节点 id 列表；'
    '筛选正则字段名是 regex_filter。',
    obj(
        {
            'action': ACTION,
            'id': string('聚合 id，update / delete 时必填'),
            'data': provider_fields(
                '聚合字段：name、subscriptions（订阅 id 数组）、nodes（手动节点 id 数组）、'
                'regex_filter（节点名筛选正则）、description、enabled、health_check_url'
            ),
        },
        ['action'],
    ),
)
def _manage_aggregation(args):
    # 聚合的 id 由服务端生成，且单条 GET 可用，因此走专用分支
    action = _require(args, 'action')
    data = args.get('data') or {}

    if action == 'create':
        if not data:
            raise ApiError(400, '创建聚合需要提供 data')
        return {'action': 'create', 'item': _unwrap(call_api('POST', '/api/aggregations', body=data), data)}

    agg_id = _require(args, 'id')
    if action == 'delete':
        call_api('DELETE', f"/api/aggregations/{agg_id}")
        return {'action': 'delete', 'id': agg_id}

    if action == 'update':
        if not data:
            raise ApiError(400, '更新聚合需要提供 data')
        current = call_api('GET', f"/api/aggregations/{agg_id}") or {}
        merged = merge_resource_data(current, data, agg_id)
        return {'action': 'update', 'item': _unwrap(call_api('PUT', f"/api/aggregations/{agg_id}", body=merged), merged)}

    raise ApiError(400, f"不支持的 action: {action}")


@tool(
    'preview_aggregation',
    '预览聚合最终产出的节点列表与节点数，用于确认筛选规则是否符合预期。'
    'mode=count 只返回节点数量，mode=provider 返回给客户端用的 provider YAML 文本。',
    obj(
        {
            'id': string('聚合 id'),
            'mode': string('返回内容，默认 nodes', ['nodes', 'count', 'provider']),
            'count_only': boolean('等价于 mode=count，保留兼容'),
        },
        ['id'],
    ),
)
def _preview_aggregation(args):
    agg_id = _require(args, 'id')
    mode = 'count' if args.get('count_only') else args.get('mode', 'nodes')
    if mode == 'count':
        return call_api('GET', f"/api/aggregations/{agg_id}/count")
    if mode == 'provider':
        return {'id': agg_id, 'provider': call_api('GET', f"/api/aggregations/{agg_id}/provider")}
    if mode != 'nodes':
        raise ApiError(400, f"不支持的 mode: {mode}")
    return call_api('GET', f"/api/aggregations/{agg_id}/preview")


# ================================================================ 节点

@tool(
    'list_nodes',
    '列出所有手动添加的代理节点（不含从订阅解析出的节点）。',
    NO_ARGS,
)
def _list_nodes(args):
    return call_api('GET', '/api/nodes')


@tool(
    'manage_node',
    '创建、更新或删除手动节点。create 时 data 至少包含 name 和 proxy_string；'
    'proxy_string 是完整的节点链接（ss:// / vmess:// / trojan:// / hysteria2:// 等），'
    '也可以是 mihomo 的结构化 proxy（YAML / JSON 文本）。协议、服务器、端口都从中解析，'
    '不要拆成 server / port / password 之类的独立字段。',
    obj(
        {
            'action': ACTION,
            'id': string('节点 id，update / delete 时必填'),
            'data': free_object(
                '节点字段：name（节点名）、proxy_string（节点链接或结构化 proxy 文本）、'
                'enabled、remark（备注）。共享节点不能保存 dialer_ref；'
                '配置独立代理链请使用 manage_proxy_group 的 chain 类型。'
            ),
        },
        ['action'],
    ),
)
def _manage_node(args):
    return _crud(args, '/api/nodes', None, '节点')


# ================================================================ 规则

RULE_DNS = string(
    'OxiDNS DNS 处理（只对 OxiDNS 生效）：follow（跟随策略）、default（默认上游）、direct（直连 DNS）、'
    'proxy（代理 DNS）、upstream:<key>（指定上游，key 取自 get_rule_dns 的 upstreams）、'
    'refused（拒绝）、nxdomain（不存在）、nodata（空应答）、blackhole（黑洞）'
)


@tool(
    'list_rules',
    '列出规则配置列表（含直接规则与规则集，通过 itemType 区分：rule / ruleset），'
    '顺序即匹配优先级。',
    NO_ARGS,
)
def _list_rules(args):
    return call_api('GET', '/api/rules')


@tool(
    'manage_rule',
    '创建、更新或删除一条规则或规则集。'
    'itemType=rule 时 data 需含 rule_type（如 DOMAIN-SUFFIX / IP-CIDR）、value、policy；'
    'itemType=ruleset 时 data 需含 name、policy，以及 url 或 library_rule_id。'
    '新建的规则默认插在列表最前（优先级最高），要放到最后传 position=bottom。'
    'update 时可在 data.dns 中同时修改这条规则的 OxiDNS DNS 处理；'
    '新建并设置 DNS 请用 compose_rule，它在一次写入中完成。',
    obj(
        {
            'action': ACTION,
            'id': string('规则 id，update / delete 时必填'),
            'data': free_object(
                '规则字段：itemType（rule / ruleset）、rule_type、value、'
                'policy（目标策略组名）、enabled、no_resolve、remark、group_name；'
                'ruleset 另有 name、url、behavior、library_rule_id。'
                'dns（仅 update）：OxiDNS DNS 处理，取值同 set_rule_dns；省略则不变，'
                '跟随策略的规则会随 policy 自动调整'
            ),
            'position': string(
                'create 时新规则放在列表哪一端，默认 top（优先级最高）',
                ['top', 'bottom'],
            ),
        },
        ['action'],
    ),
)
def _manage_rule(args):
    data = args.get('data') or {}
    if args.get('action') == 'create' and 'dns' in data:
        # POST stores the object as given; DNS is profile state, not a rule field.
        if data['dns'] != 'default':
            raise ApiError(400, '新建规则并设置 DNS 请使用 compose_rule')
        args = {**args, 'data': {key: value for key, value in data.items() if key != 'dns'}}
    prefix = 'ruleset' if data.get('itemType') == 'ruleset' else 'rule'
    result = _crud(args, '/api/rules', prefix, '规则')
    # 后端 POST /api/rules 固定插到队首，要放队尾就创建后再移一次
    position = args.get('position', 'top')
    if result.get('action') == 'create' and position == 'bottom':
        new_id = (result.get('item') or {}).get('id')
        if new_id:
            _reorder('rules', [new_id], 'bottom')
            result['position'] = 'bottom'
    return result


@tool(
    'compose_rule',
    '一次写入新建规则或规则集引用，并设置它们的 OxiDNS DNS 处理；任何一步失败都不保存。'
    'source.kind=values：text 每行一条（可写 TYPE,value；+.x / *.x 为 DOMAIN-SUFFIX，full:x 为 DOMAIN），'
    '无前缀的行按 source.rule_type，最多 200 行，已存在的行会跳过；'
    'source.kind=library：library_rule_ids 引用规则仓库条目；'
    'source.kind=new_library：library 为新规则仓库条目（name，source_type=url 给 url，content 给 content；enabled 缺省为 true）。'
    'id 由服务端生成。',
    obj(
        {
            'source': free_object('规则来源：{kind: values | library | new_library, ...}'),
            'rule': free_object(
                '规则字段：policy（必填，策略组名或 DIRECT / REJECT 等）、position（top / bottom，默认 top）、'
                'enabled、no_resolve、remark、group_name'
            ),
            'dns': RULE_DNS,
            'dns_ip': string('粘贴内容里 IP-CIDR / IP-CIDR6 行的 DNS 处理；省略时与 dns 相同。取值同 dns'),
        },
        ['source', 'rule'],
    ),
)
def _compose_rule(args):
    body = {key: args[key] for key in ('source', 'rule', 'dns', 'dns_ip') if args.get(key) is not None}
    return call_api('POST', '/api/rules/compose', body=body)


@tool(
    'get_rule_dns',
    '读取每条规则的 OxiDNS DNS 处理：engine（模式、是否可编辑及原因、开启规则自动同步的设备）、'
    'upstreams（可固定的上游及其 key）和 rules（未列出的规则为默认上游）。',
    NO_ARGS,
)
def _get_rule_dns(args):
    return call_api('GET', '/api/rules/dns')


@tool(
    'set_rule_dns',
    '批量修改规则的 OxiDNS DNS 处理，一次最多 200 条，在一次写入中完成。'
    '修改规则 DNS 请优先使用本工具，而不是 update_oxidns_settings。',
    obj(
        {
            'routes': {'type': 'object', 'description': '规则 id → DNS 处理。' + RULE_DNS['description'],
                       'additionalProperties': {'type': 'string'}},
            'skip_unsupported': boolean('为 true 时跳过不参与 DNS 分流的规则（如 GEOIP），否则报错'),
        },
        ['routes'],
    ),
)
def _set_rule_dns(args):
    body = {'routes': _require(args, 'routes')}
    if args.get('skip_unsupported') is not None:
        body['skip_unsupported'] = args['skip_unsupported']
    return call_api('PUT', '/api/rules/dns', body=body)


@tool(
    'batch_add_rules',
    '按同一规则类型和策略批量添加多条规则，适合一次导入大量域名。',
    obj(
        {
            'rule_type': string('规则类型，如 DOMAIN-SUFFIX、DOMAIN、DOMAIN-KEYWORD、IP-CIDR'),
            'domains': array('规则值列表，每项一个域名或 IP 段'),
            'policy': string('目标策略组名称'),
        },
        ['rule_type', 'domains', 'policy'],
    ),
)
def _batch_add_rules(args):
    return call_api(
        'POST',
        '/api/rules/batch',
        body={
            'rule_type': _require(args, 'rule_type'),
            'domains': _require(args, 'domains'),
            'policy': _require(args, 'policy'),
        },
    )


@tool(
    'test_rule_match',
    '测试一个域名或 IP 会命中哪条规则、走哪个策略组，用于排查分流问题。',
    obj({'query': string('待测试的域名或 IP')}, ['query']),
)
def _test_rule_match(args):
    return call_api('POST', '/api/rules/match-test', body={'query': _require(args, 'query')})


@tool(
    'find_duplicate_rules',
    '扫描直接规则与规则集内容，找出重复的规则条目（重复会让后面的规则永不生效）。',
    NO_ARGS,
)
def _find_duplicate_rules(args):
    return call_api('POST', '/api/rules/find-duplicates', body={})


# ================================================================ 规则仓库

@tool(
    'list_rule_library',
    '列出规则仓库中集中管理的规则集（rule-providers），含名称、来源 URL 和启用状态。',
    NO_ARGS,
)
def _list_rule_library(args):
    return call_api('GET', '/api/rule-library')


@tool(
    'manage_rule_library',
    '创建、更新或删除规则仓库条目。create 时 data 至少包含 name；'
    'source_type=url 时需给 url，source_type=content 时需给 content。',
    obj(
        {
            'action': ACTION,
            'id': string('规则仓库条目 id，update / delete 时必填'),
            'data': free_object(
                '条目字段：name、source_type（url / content）、url、content、'
                'behavior（domain / ipcidr / classical）、enabled'
            ),
        },
        ['action'],
    ),
)
def _manage_rule_library(args):
    return _crud(args, '/api/rule-library', 'lib', '规则仓库条目')


@tool(
    'get_rule_library_content',
    '读取规则仓库中 source_type=content 的条目正文（URL 类型的条目没有正文）。',
    obj({'id': string('规则仓库条目 id')}, ['id']),
)
def _get_rule_library_content(args):
    rule_id = _require(args, 'id')
    return {'id': rule_id, 'content': call_api('GET', f"/api/rule-library/content/{rule_id}")}


@tool(
    'cache_rule_library',
    '把指定的 URL 类型规则仓库条目拉取并缓存到本地，之后生成配置可离线引用。',
    obj({'ids': array('要缓存的规则仓库条目 id 列表')}, ['ids']),
)
def _cache_rule_library(args):
    return call_api('POST', '/api/rule-library/cache', body={'rule_ids': _require(args, 'ids')})


@tool(
    'test_rule_library',
    '测试规则仓库条目的连通性。给 url 时只测这一个地址；'
    '不给 url 时批量测试仓库中所有 URL 类型的条目（不支持只测其中几条）。',
    obj({'url': string('只测试某个 URL 是否可访问；留空则批量测试全部条目')}),
)
def _test_rule_library(args):
    if args.get('url'):
        return call_api('POST', '/api/rule-library/test-single', body={'url': args['url']})
    return call_api('POST', '/api/rule-library/test', body={})


# ================================================================ 策略组

@tool(
    'list_proxy_groups',
    '列出所有策略组，含类型、包含的节点/订阅/聚合以及筛选规则。',
    NO_ARGS,
)
def _list_proxy_groups(args):
    return call_api('GET', '/api/proxy-groups')


@tool(
    'manage_proxy_group',
    '创建、更新或删除策略组。create 时 data 至少包含 name 和 type；'
    'type 支持 select（手动选择）、url-test（自动测速）、fallback（故障转移）、'
    'load-balance（负载均衡）、chain（Mihomo 代理链）。'
    '普通组可直接引用共享订阅（subscriptions + regex）、聚合（aggregations + aggregation_regex）、'
    '手动节点（manual_nodes）或当前配置其它策略组/代理链（include_groups），无需资源预选；'
    'follow_group 可整体跟随普通策略组，不能跟随代理链。'
    'chain 类型使用 chain={entry:{type:"node"|"group",id:稳定ID},exit:{type:"node"|"group",id:稳定ID}}。'
    '节点引用仅支持手动节点；订阅和聚合通过策略组使用。前置直接引用原组，落地生成独立的链内节点/组，'
    '保留来源、筛选、测速和选路参数，不改变原资源；Provider 刷新可更新候选。Surge 拒绝启用链。'
    '引用类字段填的都是 id，不是名称。update 为事务内 partial merge，type 不代表替换；'
    'delete_fields 显式删除健康字段（仅 update，不能同时赋值）。精确排序项须与已选来源一致。',
    obj(
        {
            'action': ACTION,
            'id': string('策略组 id，update / delete 时必填'),
            'data': health_fields(
                '策略组字段：name、type、enabled、'
                'subscriptions（订阅 id 数组）、regex（订阅节点名筛选正则）、'
                'aggregations（聚合 id 数组）、aggregation_regex（聚合节点名筛选正则）、'
                'manual_nodes（节点 id 数组，DIRECT / REJECT 直接用名称）、'
                'include_groups（引用的其他策略组 id 数组）、'
                'follow_group（跟随的策略组 id）、proxies_order（节点顺序）、'
                'url（测试地址）、interval（测试间隔秒）、'
                'strategy（load-balance 的 round-robin / consistent-hashing / sticky-sessions）、'
                'lazy（懒加载）、chain（代理链的 entry 与 exit；不与普通来源字段混用）'
            ),
        },
        ['action'],
    ),
)
def _manage_proxy_group(args):
    return _crud(args, '/api/proxy-groups', 'group', '策略组')


@tool(
    'preview_proxy_group_regex',
    '预览一个筛选正则在指定节点来源下会匹配到哪些节点，用于在保存策略组前验证筛选规则。'
    'source=subscription 时按 subscriptions 里的订阅取节点，'
    'source=aggregation 时按 aggregations 里的聚合取节点（含聚合内的手动节点）。',
    obj(
        {
            'regex': string('节点名筛选正则'),
            'source': string('节点来源', ['subscription', 'aggregation']),
            'subscriptions': array('source=subscription 时参与筛选的订阅 id 列表'),
            'aggregations': array('source=aggregation 时参与筛选的聚合 id 列表'),
        },
        ['regex', 'source'],
    ),
)
def _preview_proxy_group_regex(args):
    source = _require(args, 'source')
    if source not in ('subscription', 'aggregation'):
        raise ApiError(400, "source 必须是 'subscription' 或 'aggregation'")
    body = {
        'regex': _require(args, 'regex'),
        'source': source,
        'subscriptions': args.get('subscriptions', []),
        'aggregations': args.get('aggregations', []),
    }
    return call_api('POST', '/api/proxy-groups/preview-regex', body=body)


# ================================================================ 配置生成

_CONFIG_TARGETS = ['mihomo', 'surge', 'mosdns', 'oxidns']


@tool(
    'preview_config',
    '根据当前的订阅、节点、规则和策略组生成配置内容并直接返回文本，不写入磁盘。'
    '用于在正式生成前检查配置。',
    obj(
        {
            'target': string('目标格式', _CONFIG_TARGETS),
            'base_url': string('生成规则引用链接时使用的服务地址，留空则用系统设置'),
        },
        ['target'],
    ),
)
def _preview_config(args):
    target = _require(args, 'target')
    if target not in _CONFIG_TARGETS:
        raise ApiError(400, f"target 必须是 {_CONFIG_TARGETS} 之一")
    return call_api(
        'POST',
        f"/api/generate/{target}/preview",
        body={'base_url': args.get('base_url', '')},
    )


@tool(
    'generate_config',
    'Mihomo / Surge：生成配置并保存到服务端数据目录（config.yaml / config.conf）。'
    'MosDNS / OxiDNS：打包成含规则文件的 zip 供界面下载，不落盘；订阅链接实时生成。'
    '只返回结果摘要，需要查看配置内容请用 preview_config。',
    obj(
        {
            'target': string('目标格式', _CONFIG_TARGETS),
            'base_url': string('生成规则引用链接时使用的服务地址，留空则用系统设置'),
        },
        ['target'],
    ),
)
def _generate_config(args):
    target = _require(args, 'target')
    if target not in _CONFIG_TARGETS:
        raise ApiError(400, f"target 必须是 {_CONFIG_TARGETS} 之一")
    result = call_api('POST', f"/api/generate/{target}", body={'base_url': args.get('base_url', '')})
    # 生成接口返回的是配置文件本身，内容可达数千行且含节点凭证，
    # 不回灌进模型上下文；要看内容用 preview_config。
    if isinstance(result, (str, bytes)):
        size = len(result)
    elif isinstance(result, dict) and result.get('binary'):
        size = result.get('size')
    else:
        size = None

    # MosDNS 走的是内存打包下载，不写入数据目录；其订阅链接为实时生成
    saved = target not in ('mosdns', 'oxidns')
    message = (
        f'{target} 配置已生成并保存到数据目录，订阅链接即可取到新配置'
        if saved else
        f'{target} 规则包已打包（含规则文件，需在界面下载）；'
        '订阅链接为实时生成，配置改动无需此步即已生效'
    )
    return {
        'success': True,
        'target': target,
        'saved': saved,
        'size': size,
        'message': message,
    }


@tool(
    'manage_config_backup',
    '导出、导入或重置 ConfigFlow 配置。'
    'export 返回配置 JSON；import 用 data 覆盖；reset 恢复出厂设置。'
    'scope=system（默认）作用于系统设置、共享资源及全部配置；scope=profile 只导入导出资源引用和独立参数。'
    'MCP 返回会移除内部凭据；完整恢复备份请在系统设置下载。profile 粒度不支持 reset。',
    obj(
        {
            'action': string('操作类型', ['export', 'import', 'reset']),
            'scope': string('作用范围，默认 system', ['system', 'profile']),
            'data': free_object('import 时要导入的配置 JSON'),
            'desensitize': boolean('export 时脱敏订阅 URL 与节点凭证，默认 false'),
        },
        ['action'],
    ),
)
def _manage_config_backup(args):
    action = _require(args, 'action')
    scope = args.get('scope', 'system')
    if scope not in ('system', 'profile'):
        raise ApiError(400, f"不支持的 scope: {scope}")

    if scope == 'profile':
        from backend.common.config import get_default_profile_id
        profile_id = args.get('profile_id') or get_default_profile_id()
        if action == 'export':
            return call_api('GET', f"/api/profiles/{profile_id}/export")
        if action == 'import':
            data = args.get('data')
            if not data:
                raise ApiError(400, '导入配置需要提供 data')
            return call_api('POST', f"/api/profiles/{profile_id}/import", body=data)
        raise ApiError(400, f"scope=profile 不支持 action: {action}")

    if action == 'export':
        return call_api('GET', '/api/config/export', query={'desensitize': args.get('desensitize')})
    if action == 'import':
        data = args.get('data')
        if not data:
            raise ApiError(400, '导入配置需要提供 data')
        return call_api('POST', '/api/config/import', body=data)
    if action == 'reset':
        return call_api('POST', '/api/config/reset', body={})
    raise ApiError(400, f"不支持的 action: {action}")


# ================================================================ Agent

@tool(
    'list_agents',
    '列出所有已注册的远程 Agent，含在线状态、版本和最后心跳时间。',
    NO_ARGS,
)
def _list_agents(args):
    return call_api('GET', '/api/agents')


@tool(
    'get_agent',
    '获取单个 Agent 的详细信息。include_status=true 时附带实时运行状态。',
    obj(
        {
            'id': string('Agent id'),
            'include_status': boolean('是否附带实时运行状态，默认 false'),
        },
        ['id'],
    ),
)
def _get_agent(args):
    agent_id = _require(args, 'id')
    result = {'agent': call_api('GET', f"/api/agents/{agent_id}")}
    if args.get('include_status'):
        result['status'] = call_api('GET', f"/api/agents/{agent_id}/status")
    return result


@tool(
    'manage_agent',
    '对 Agent 执行管理操作：update（修改配置字段）、delete（从列表移除）、'
    'restart（重启被管服务）、push_config（把最新配置推送到 Agent）、'
    'uninstall（远程卸载）、upgrade（升级 Agent 到最新版本）、'
    'clear_log（清空指定日志文件，需 log_path）、validate_log_path（校验日志路径，需 log_path）、'
    'get_logging（读取日志采集开关）、set_logging（开关日志采集，需 enabled）。',
    obj(
        {
            'action': string(
                '操作类型',
                [
                    'update', 'delete', 'restart', 'push_config', 'uninstall', 'upgrade',
                    'clear_log', 'validate_log_path', 'get_logging', 'set_logging',
                ],
            ),
            'id': string('Agent id'),
            'data': free_object(
                'update 时要修改的字段，仅 name、host、port、enabled、service_type 生效；'
                '配置路径、重启命令等属于 Agent 端的安装参数，需在 Agent 侧调整'
            ),
            'base_url': string('push_config 时 Agent 回取配置用的服务地址，留空自动推断'),
            'log_path': string('clear_log / validate_log_path 时的日志文件路径'),
            'enabled': boolean('set_logging 时是否启用日志采集'),
        },
        ['action', 'id'],
    ),
)
def _manage_agent(args):
    action = _require(args, 'action')
    agent_id = _require(args, 'id')

    if action == 'update':
        data = args.get('data') or {}
        if not data:
            raise ApiError(400, '更新 Agent 需要提供 data')
        current = call_api('GET', f"/api/agents/{agent_id}") or {}
        merged = {**current, **data}
        return call_api('PUT', f"/api/agents/{agent_id}", body=merged)
    if action == 'delete':
        return call_api('DELETE', f"/api/agents/{agent_id}")
    if action == 'restart':
        return call_api('POST', f"/api/agents/{agent_id}/restart", body={})
    if action == 'push_config':
        return call_api(
            'POST',
            f"/api/agents/{agent_id}/push-config",
            body={'base_url': args.get('base_url', '')},
        )
    if action == 'uninstall':
        return call_api('POST', f"/api/agents/{agent_id}/uninstall", body={})
    if action == 'upgrade':
        return call_api('POST', f"/api/agents/{agent_id}/update", body={})
    if action == 'clear_log':
        return call_api(
            'POST',
            f"/api/agents/{agent_id}/logs/clear",
            body={'log_path': _require(args, 'log_path')},
        )
    if action == 'validate_log_path':
        return call_api(
            'POST',
            f"/api/agents/{agent_id}/logs/validate",
            body={'path': _require(args, 'log_path')},
        )
    if action == 'get_logging':
        return call_api('GET', f"/api/agents/{agent_id}/config/logging")
    if action == 'set_logging':
        if 'enabled' not in args:
            raise ApiError(400, 'set_logging 需要提供 enabled')
        return call_api(
            'POST',
            f"/api/agents/{agent_id}/config/logging",
            body={'enabled': bool(args['enabled'])},
        )
    raise ApiError(400, f"不支持的 action: {action}")


@tool(
    'get_agent_logs',
    '读取 Agent 所管服务的日志尾部，用于排查节点或 DNS 异常。',
    obj(
        {
            'id': string('Agent id'),
            'lines': integer('读取的行数，默认 100'),
            'log_path': string('自定义日志路径，留空使用 Agent 的默认配置'),
        },
        ['id'],
    ),
)
def _get_agent_logs(args):
    agent_id = _require(args, 'id')
    return call_api(
        'GET',
        f"/api/agents/{agent_id}/logs",
        query={'lines': args.get('lines', 100), 'log_path': args.get('log_path')},
    )


@tool(
    'get_agent_metrics',
    'Agent 的监控数据。scope=current 取实时指标，summary 取汇总，'
    'history 取历史序列，traffic 取流量统计，traffic_trend 取流量趋势。',
    obj(
        {
            'id': string('Agent id'),
            'scope': string('数据范围', ['current', 'summary', 'history', 'traffic', 'traffic_trend']),
            'hours': integer('history / traffic_trend 的时间跨度（小时），默认 24'),
        },
        ['id'],
    ),
)
def _get_agent_metrics(args):
    agent_id = _require(args, 'id')
    scope = args.get('scope', 'current')
    hours = args.get('hours')
    paths = {
        'current': ('/metrics', None),
        'summary': ('/metrics/summary', None),
        'history': ('/metrics/history', {'hours': hours}),
        'traffic': ('/traffic/stats', None),
        'traffic_trend': ('/traffic/trend', {'hours': hours}),
    }
    if scope not in paths:
        raise ApiError(400, f"不支持的 scope: {scope}")
    suffix, query = paths[scope]
    return call_api('GET', f"/api/agents/{agent_id}{suffix}", query=query)


# method -> 生成接口路径
_AGENT_INSTALL_ENDPOINTS = {
    'install_script': '/api/agents/install-script',
    'docker_compose': '/api/agents/docker-agent-compose',
    'docker_run': '/api/agents/docker-agent-run',
    'latest_version': '/api/agents/latest-version',
}


@tool(
    'get_agent_install_info',
    '获取新增 Agent 所需的安装物料：install_script（一键安装脚本）、'
    'docker_compose（compose 文件）、docker_run（docker run 命令）、'
    'latest_version（最新 Agent 版本号）。',
    obj(
        {
            'method': string('要获取的物料', list(_AGENT_INSTALL_ENDPOINTS)),
            'params': free_object(
                '生成参数（作为 query 传给后端），如 server_url、name / agent_name、'
                'type（mihomo / mosdns / oxidns）、port、agent_ip、config_path、restart_command、'
                'data_dir、network_mode、enable_mihomo、enable_mosdns、enable_oxidns'
            ),
        },
        ['method'],
    ),
)
def _get_agent_install_info(args):
    method = _require(args, 'method')
    if method not in _AGENT_INSTALL_ENDPOINTS:
        raise ApiError(400, f"不支持的 method: {method}")
    result = call_api('GET', _AGENT_INSTALL_ENDPOINTS[method], query=args.get('params') or {})
    return result if isinstance(result, dict) else {'method': method, 'content': result}


# ================================================================ MosDNS

# section -> REST 路径。POST 为整体替换，因此更新走 read-modify-write。
_MOSDNS_SECTIONS = {
    'rulesets': '/api/mosdns/rulesets',
    'custom_matches': '/api/mosdns/custom-matches',
    'dns_servers': '/api/mosdns/dns-servers',
    'log': '/api/mosdns/log-settings',
    'api': '/api/mosdns/api-settings',
    'cache': '/api/mosdns/cache-settings',
}
_MOSDNS_SECTION_NAMES = list(_MOSDNS_SECTIONS)


@tool(
    'get_mosdns_settings',
    '读取 MosDNS 配置。section 分别对应：rulesets（分流规则集）、'
    'custom_matches（自定义匹配）、dns_servers（上游 DNS 与 hosts）、'
    'log（日志）、api（API 监听）、cache（缓存）。留空则返回全部。',
    obj({'section': string('配置分区，留空返回全部', _MOSDNS_SECTION_NAMES)}),
)
def _get_mosdns_settings(args):
    section = args.get('section')
    if section:
        if section not in _MOSDNS_SECTIONS:
            raise ApiError(400, f"不支持的 section: {section}")
        return call_api('GET', _MOSDNS_SECTIONS[section])
    return {name: call_api('GET', path) for name, path in _MOSDNS_SECTIONS.items()}


@tool(
    'update_mosdns_settings',
    '更新 MosDNS 某个配置分区。只需给出要改的字段，同分区其余字段自动保留。',
    obj(
        {
            'section': string('配置分区', _MOSDNS_SECTION_NAMES),
            'data': free_object(
                '该分区的字段，例如 dns_servers 分区支持 local_dns、remote_dns、'
                'fallback_dns、default_forward、custom_hosts；cache 分区支持 '
                'cache_enabled、cache_size、cache_lazy_ttl、cache_dump_enabled'
            ),
        },
        ['section', 'data'],
    ),
)
def _update_mosdns_settings(args):
    section = _require(args, 'section')
    data = _require(args, 'data')
    if section not in _MOSDNS_SECTIONS:
        raise ApiError(400, f"不支持的 section: {section}")
    path = _MOSDNS_SECTIONS[section]
    current = call_api('GET', path) or {}
    merged = {**(current if isinstance(current, dict) else {}), **data}
    return call_api('POST', path, body=merged)


@tool(
    'convert_mosdns_rule',
    '把一个远程规则文件（Clash / list 格式）转换成 MosDNS 域名规则格式并返回文本，'
    '用于确认某个规则源能否被 MosDNS 正确解析。',
    obj({'url': string('规则文件地址')}, ['url']),
)
def _convert_mosdns_rule(args):
    url = _require(args, 'url')
    return {'url': url, 'content': call_api('GET', '/api/mosdns/rule-proxy', query={'url': url})}


# ================================================================ OxiDNS

@tool(
    'get_oxidns_settings',
    '读取当前配置空间的 OxiDNS 参数：分流、上游、缓存、探测缓存、线程、API、日志和高级插件。',
    obj({}),
)
def _get_oxidns_settings(args):
    return call_api('GET', '/api/oxidns/settings')


@tool(
    'update_oxidns_settings',
    '更新独立的 OxiDNS 参数。仅需提交修改字段，嵌套对象会合并，列表整体替换；不影响 MosDNS。'
    '高级插件以 enabled 开关和 args 原生参数配置，生成后可通过 preview_config 检查。'
    '规则的 DNS 处理请用 set_rule_dns；在这里只能用 rule_actions.<规则 id>: null 删除一条。',
    obj({'data': free_object('OxiDNS 参数，例如 worker_threads、cache_lazy_ttl、probe_cache_size、api 或 query_recorder')}, ['data']),
)
def _update_oxidns_settings(args):
    from copy import deepcopy
    data = _require(args, 'data')
    if not isinstance(data, dict):
        raise ApiError(400, 'data 必须是对象')
    current = call_api('GET', '/api/oxidns/settings')

    def merge(previous, changes):
        result = deepcopy(previous) if isinstance(previous, dict) else {}
        for key, value in changes.items():
            result[key] = merge(result.get(key), value) if isinstance(value, dict) else deepcopy(value)
        return result

    return call_api('POST', '/api/oxidns/settings', body=merge(current, data))


# ================================================================ 系统

# section -> (GET 路径, POST 路径, GET 响应字段 -> POST 请求字段)
_SETTING_SECTIONS = {
    'server_domain': ('/api/server-domain', '/api/server-domain', {'server_domain': 'new_domain'}),
    'config_token': ('/api/config-token', '/api/config-token', {'config_token': 'token'}),
    'subscription_aggregation': (
        '/api/settings/subscription-aggregation',
        '/api/settings/subscription-aggregation',
        {},
    ),
    'backup': ('/api/backup/config', '/api/backup/config', {}),
    'github_proxy': ('/api/rule-library/proxy-domains', '/api/rule-library/proxy-domains', {}),
    'version': ('/api/version', None, {}),
}
_SETTING_SECTION_NAMES = list(_SETTING_SECTIONS)


@tool(
    'get_overview',
    '获取系统概览统计：订阅数、节点数、规则数、策略组数、在线 Agent 数等。'
    '适合作为了解当前状态的第一个调用。',
    NO_ARGS,
)
def _get_overview(args):
    return call_api('GET', '/api/stats/overview')


@tool(
    'get_settings',
    '读取系统设置。section 对应：server_domain（对外域名）、config_token（订阅链接令牌）、'
    'subscription_aggregation（聚合功能开关）、'
    'backup（WebDAV 备份配置）、github_proxy（GitHub 规则源加速域名）、'
    'version（版本信息）。留空返回全部。',
    obj({'section': string('设置分区，留空返回全部', _SETTING_SECTION_NAMES)}),
)
def _get_settings(args):
    section = args.get('section')
    if section:
        if section not in _SETTING_SECTIONS:
            raise ApiError(400, f"不支持的 section: {section}")
        return call_api('GET', _SETTING_SECTIONS[section][0])
    return {name: call_api('GET', paths[0]) for name, paths in _SETTING_SECTIONS.items()}


@tool(
    'update_settings',
    '更新系统设置。server_domain 传 {"server_domain": "..."}；'
    'config_token 传 {"config_token": "..."} 或 {"generate": true} 生成随机令牌；'
    'subscription_aggregation 传 {"enabled": true/false}；'
    'backup 传 webdav_url / webdav_username / webdav_password / webdav_path / auto_backup / '
    'keep_latest（保留最近份数，0 不清理）/ keep_daily（另保留最近几天每天最后一份）；'
    'github_proxy 传 {"proxy_domains": "https://ghproxy.example.com/"}，传空字符串表示关闭。',
    obj(
        {
            'section': string(
                '设置分区',
                [n for n in _SETTING_SECTION_NAMES if n != 'version'],
            ),
            'data': free_object('该分区的字段'),
        },
        ['section', 'data'],
    ),
)
def _update_settings(args):
    section = _require(args, 'section')
    data = dict(_require(args, 'data'))
    if section not in _SETTING_SECTIONS:
        raise ApiError(400, f"不支持的 section: {section}")

    get_path, post_path, field_map = _SETTING_SECTIONS[section]
    if post_path is None:
        raise ApiError(400, f"'{section}' 是只读设置")

    if section == 'backup':
        # 备份配置的 POST 会整体覆盖，先读回补齐未传字段。
        # GET 把已存的密码掩码成 '******'，而路由正是以这个字面量表示
        # 「沿用已存密码」，所以必须原样回传：一旦抹掉该键，路由会把密码写成空串。
        current = call_api('GET', get_path) or {}
        data = {**current, **data}

    # 少数端点的读写字段名不一致，按映射改写
    for read_key, write_key in field_map.items():
        if read_key in data and write_key not in data:
            data[write_key] = data.pop(read_key)

    return call_api('POST', post_path, body=data)


@tool(
    'run_backup',
    '立即执行一次 WebDAV 备份，或仅测试 WebDAV 连通性。',
    obj({'action': string('操作类型', ['backup_now', 'test_connection'])}, ['action']),
)
def _run_backup(args):
    action = _require(args, 'action')
    if action not in ('test_connection', 'backup_now'):
        raise ApiError(400, f"不支持的 action: {action}")

    # 两个端点都从请求体读取 WebDAV 凭证，不会回落到已存配置，
    # 因此把设置里的配置整份带上（密码保持 '******' 让路由取用已存值）。
    settings = call_api('GET', '/api/backup/config') or {}
    path = '/api/backup/test' if action == 'test_connection' else '/api/backup/now'
    return call_api('POST', path, body=settings)


@tool(
    'get_app_logs',
    '读取 ConfigFlow 服务端日志尾部，支持关键字与日志级别过滤，用于排查后端错误。',
    obj(
        {
            'lines': integer('读取行数，默认 100'),
            'search': string('关键字过滤'),
            'level': string('日志级别过滤', ['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL']),
        }
    ),
)
def _get_app_logs(args):
    return call_api(
        'GET',
        '/api/logs/tail',
        query={
            'lines': args.get('lines', 100),
            'search': args.get('search'),
            'level': args.get('level'),
        },
    )


@tool(
    'manage_app_logs',
    '查看服务端日志文件信息（大小、修改时间）或清空日志文件。读日志内容用 get_app_logs。',
    obj({'action': string('操作类型', ['info', 'clear'])}, ['action']),
)
def _manage_app_logs(args):
    action = _require(args, 'action')
    if action == 'info':
        return call_api('GET', '/api/logs/info')
    if action == 'clear':
        return call_api('POST', '/api/logs/clear', body={})
    raise ApiError(400, f"不支持的 action: {action}")


@tool(
    'manage_custom_config',
    '读取或写入自定义配置片段。这段内容会被合并进生成的配置，'
    '用于放入 ConfigFlow 界面未覆盖的原生字段（如 Mihomo 的 tun、dns 段）。',
    obj(
        {
            'target': string('目标格式', _CONFIG_TARGETS),
            'action': string('操作类型', ['get', 'set']),
            'content': string('action=set 时要写入的配置片段文本；传空字符串表示清空'),
        },
        ['target', 'action'],
    ),
)
def _manage_custom_config(args):
    target = _require(args, 'target')
    action = _require(args, 'action')
    if target not in _CONFIG_TARGETS:
        raise ApiError(400, f"target 必须是 {_CONFIG_TARGETS} 之一")
    path = f"/api/custom-config/{target}"
    if action == 'get':
        return call_api('GET', path)
    if action == 'set':
        content = args.get('content')
        if content is None:
            raise ApiError(400, 'action=set 需要提供 content')
        return call_api('POST', path, body={'config': content})
    raise ApiError(400, f"不支持的 action: {action}")
