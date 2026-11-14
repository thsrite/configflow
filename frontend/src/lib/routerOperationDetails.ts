/** Read-only presentation of the exact release plan, never its unrelated evidence. */
export interface RouterPlanOperation { summary: string; kind?: string; [key: string]: unknown }
export interface DetailRow { label: string; value: string; before?: string; action?: '新增' | '移除' | '保留' }
export interface DetailGroup { title: string; note?: string; rows: DetailRow[] }
type Fields = Record<string, unknown>
const record = (value: unknown): Fields => value && typeof value === 'object' && !Array.isArray(value) ? value as Fields : {}
const list = (value: unknown): unknown[] => Array.isArray(value) ? value : []
const strings = (value: unknown) => list(value).filter((item): item is string => typeof item === 'string')
const text = (value: unknown): string => Array.isArray(value) ? value.map(text).join(', ') : value === null || value === undefined || value === '' ? '未设置' : String(value)
const labels: Record<string, string> = {
  sources: '来源范围', destinations: '目的网段', gateway: '下一跳', egress_interface: '出口接口', nat_interface: 'NAT 免转换出口',
  router_ip: '主路由地址', mark: '标记', table: '路由表编号', priority: '规则优先级',
  list: '地址列表', address: '地址', name: '名称', chain: '规则链', action: '动作',
  'src-address': '源地址', 'dst-address': '目的地址', 'src-address-list': '来源地址列表', 'dst-address-list': '目的地址列表',
  'routing-table': '路由表', 'routing-mark': '匹配路由标记', 'new-routing-mark': '写入路由标记', 'jump-target': '跳转链',
  'dst-address-type': '目的地址类型', 'out-interface': '出口接口', 'in-interface': '入口接口',
  'out-interface-list': '出口接口列表', 'in-interface-list': '入口接口列表',
  'src-port': '源端口', 'dst-port': '目的端口', protocol: '协议', passthrough: '继续后续规则',
  disabled: '停用', fib: '加入转发表', comment: '备注', cidr: '目的网段', id: '对象 ID', network: 'DHCP 网段', dns: 'DNS 列表', 'dns-server': 'DNS 列表',
  'to-addresses': '转发到地址', 'to-ports': '转发到端口', 'reject-with': '拒绝方式', host: '检测地址', port: '端口',
  type: '检测方式', interval: '检测间隔', 'up-script': '恢复时执行', 'down-script': '故障时执行'
}
const pathNames: Record<string, string> = {
  '/ip/firewall/address-list': '地址列表', '/ip/firewall/mangle': 'Mangle', '/routing/table': '路由表',
  '/ip/route': '下一跳路由', '/ip/firewall/nat': 'NAT 免转换'
}
const guardPathNames: Record<string, string> = {
  '/ip/firewall/address-list': '放行地址', '/ip/firewall/nat': 'NAT', '/ip/firewall/filter': '防火墙', '/tool/netwatch': 'Netwatch'
}
const pdGuardPaths: Record<string, string> = {
  '/system/script': '脚本', '/system/scheduler': '定时任务', '/ipv6/dhcp-client': 'IPv6 DHCP 客户端',
  '/ipv6/firewall/address-list': '前缀记录', '/ipv6/nd/prefix': '弃用前缀通告'
}
const pickKeys = (from: unknown, keys: unknown): Fields => {
  const source = record(from)
  return Object.fromEntries(Object.keys(record(keys)).map(key => [key, source[key] ?? '']))
}
// Multi-line script text is shown as-is so the preview reads like the router will.
const pdGuardValues = (value: unknown) => Object.entries(record(value))
  .filter(([key, item]) => !/password|secret|token|authorization/i.test(key) && (item === null || typeof item !== 'object'))
  .map(([key, item]) => String(item ?? '').includes('\n') ? `${key}:\n${String(item)}` : `${key}: ${text(item)}`).join('\n')
const safeFields = (value: unknown): DetailRow[] => Object.entries(record(value))
  .filter(([key, value]) => !/password|secret|token|authorization/i.test(key) && (value === null || typeof value !== 'object' || Array.isArray(value)))
  .map(([key, value]) => ({ label: labels[key] ? `${labels[key]} · ${key}` : key, value: text(value) }))
const valuesOf = (value: unknown) => safeFields(value).map(row => `${row.label}: ${row.value}`).join('\n')
function addressDiff(title: string, before: unknown, after: unknown): DetailGroup {
  const old = new Set(strings(before)), next = new Set(strings(after))
  return { title, note: '对比上次受管策略的地址范围。保留表示范围不变，底层对象仍按本次计划重建。', rows: [
    ...[...next].filter(ip => !old.has(ip)).map(ip => ({ label: '新增', value: ip, action: '新增' as const })),
    ...[...old].filter(ip => !next.has(ip)).map(ip => ({ label: '移除', value: ip, action: '移除' as const })),
    ...[...next].filter(ip => old.has(ip)).map(ip => ({ label: '保留', value: ip, action: '保留' as const }))
  ] }
}
function changedFields(title: string, before: Fields, after: Fields, keys: string[]): DetailGroup {
  return { title, rows: keys.filter(key => key in before || key in after).map(key => ({
    label: labels[key] || key, before: text(before[key]), value: text(after[key])
  })) }
}
function objectRow(value: unknown, creating: boolean): DetailRow {
  const item = record(value), fields = record(creating ? item.values : item.config)
  const identity = item.id || fields.list || fields.name || fields.chain || fields['routing-table'] || ''
  return { label: `${pathNames[String(item.path)] || text(item.path)}${identity ? ` · ${identity}` : ''}`, value: valuesOf(fields) }
}
const groupRefs = (op: RouterPlanOperation): DetailRow[] => strings(op.groups).map(value => ({ label: 'IP 分组', value }))
const groupNote = 'iKuai 规则只引用 ConfigFlow 管理的 IP 分组，不再逐条填写网段；增删网段时只改分组内容。'
export function hasOperationDetails(op: RouterPlanOperation) {
  return ['ros_policy_replace', 'dns_guard_replace', 'return_routing_apply', 'route_add', 'route_delete', 'route_comment',
    'nat_add', 'nat_update', 'nat_list_sync', 'dhcp_dns_update', 'ipv6_dns_update', 'ipv6_pd_guard_sync', 'policy_route_add', 'policy_route_update',
    'policy_route_delete', 'ikuai_group_add', 'ikuai_group_update', 'ikuai_group_delete'].includes(op.kind || '')
}
export function operationDetails(op: RouterPlanOperation): DetailGroup[] {
  const groups: DetailGroup[] = []
  const add = (title: string, rows: DetailRow[], note?: string) => groups.push({ title, rows, note })
  if (op.kind === 'ros_policy_replace') {
    const before = record(record(op.previous).spec), after = record(op.spec)
    groups.push(addressDiff('来源范围', before.sources, after.sources), addressDiff('目的网段', before.destinations, after.destinations),
      changedFields('网关与出口', before, after, ['gateway', 'egress_interface', 'nat_interface']))
    const bundle = list(op.bundle)
    for (const [path, label] of Object.entries(pathNames)) {
      add(`新增${label}`, bundle.filter(value => record(value).path === path).map(value => objectRow(value, true)),
        path === '/ip/firewall/mangle' ? '字段来自本次发布计划。按列表顺序创建，最后启用 prerouting 入口规则。' : '以下是本次计划实际创建的对象与字段。')
    }
    add('删除旧受管对象', [...list(op.owned)].reverse().map(value => objectRow(value, false)),
      bundle.length ? '新策略启用成功后，按以下顺序清理旧受管对象；其他程序和手工对象不在此列表中。' : '本次撤销策略，仅删除以下已核实归属的对象。')
    const migrations = (path: string) => list(op.migrations).filter(value => record(value).path === path).map(value => objectRow(value, false))
    add('迁移主表静态路由', migrations('/ip/route'), '新策略启用后删除下列受管主表路由，改由来源策略匹配。')
    add('收窄旧 NAT 免转换', migrations('/ip/firewall/nat'), '删除下列规则，并为其余目的网段按原匹配条件重建。')
    add('移出 NAT 免转换目的列表', migrations('/ip/firewall/address-list'),
      '这些网段改由来源策略处理；仍需免转换的静态网段会在 NAT 阶段重新加入。')
  } else if (op.kind === 'dns_guard_replace') {
    const guardRow = (value: unknown, fields: unknown): DetailRow => {
      const item = record(value)
      return { label: `${guardPathNames[String(item.path)] || text(item.path)} · ${text(item.role)}`, value: valuesOf(fields) }
    }
    add('新建对象', list(op.create).map(value => guardRow(value, record(value).values)),
      '按列表顺序创建：先写放行与回包规则，再启用劫持；劫持与 853 规则放在各自列表最前。')
    // Plans from before incremental publishing list every owned object as replaced.
    const removed = Array.isArray(op.delete) ? list(op.delete) : list(op.owned)
    add('删除旧受管对象', [...removed].reverse().map(value => guardRow(value, record(value).config)),
      list(op.create).length ? '先按以下顺序删除本功能上次创建、内容有变化的对象，再创建新对象；手工规则不在此列表中。'
        : '关闭强制 DNS，仅删除以下已核实归属的对象。')
    const kept = strings(op.keep)
    if (kept.length) add('保持不变', kept.map(role => ({ label: role, value: '内容一致，不删除也不重建' })),
      '这些对象已经符合本次设置，发布时原样保留。')
  } else if (op.kind === 'return_routing_apply') {
    const before = record(op.previous), after = record(op.spec)
    groups.push(changedFields('回流参数', before, after, ['router_ip', 'mark', 'table', 'priority']))
    if (after.safety_version === 2) {
      // Protocol 2 marks by source: there is no destination list to compare.
      const marks = (spec: Fields) => spec.safety_version === 2 ? '主路由转来的全部新连接' : `${strings(spec.proxy_dst).length} 个目的网段`
      groups.push({ title: '回流标记范围', note: '按来源标记后，规则集网段变化不再需要修改回流规则。',
        rows: [{ label: '标记', ...(op.previous ? { before: marks(before) } : {}), value: marks(after) }] })
    } else {
      const scope = addressDiff('回流目的网段', before.proxy_dst, after.proxy_dst)
      scope.note = '对比当前受管回流配置与本次目标网段。'
      groups.push(scope)
    }
  } else if (op.kind === 'ikuai_group_add') {
    add('IP 分组', [{ label: '名称', value: text(op.name) }, { label: '归属标记', value: text(op.comment) }],
      'iKuai 的 IP 分组没有分组备注，归属标记写在第一个网段的备注里。')
    add('分组网段', strings(op.values).map(value => ({ label: '新增', value, action: '新增' })), '新建分组时写入的全部网段。')
  } else if (op.kind === 'ikuai_group_update' && op.mark) {
    add('IP 分组', [{ label: '名称', value: text(op.name) }, { label: '归属标记', value: `改回 ${text(op.comment)}` },
      { label: '网段数', value: String(strings(op.values).length) }],
    '归属标记写在第一个网段的备注里，被改掉后 ConfigFlow 只凭规则引用认出这个分组。这次只写回标记，网段和其他备注不变。')
  } else if (op.kind === 'ikuai_group_update') {
    add('IP 分组', [{ label: '名称', value: text(op.name) }, { label: '更新后网段数', value: String(strings(op.values).length) }])
    add('分组网段变化', [
      ...strings(op.added).map(value => ({ label: '新增', value, action: '新增' as const })),
      ...strings(op.removed).map(value => ({ label: '移除', value, action: '移除' as const }))
    ], '只列出本次加入或移除的网段，其余网段保持不变；引用该分组的规则不需要改写。')
  } else if (op.kind === 'ikuai_group_delete') {
    add('删除 IP 分组', [{ label: '名称', value: text(op.name) }, { label: '原网段数', value: text(op.count) }],
      '删除前会再次确认没有规则引用该分组；非 ConfigFlow 管理的分组不会被删除。')
  } else if (op.kind === 'nat_update') {
    add('现有匹配条件', safeFields(op.expected), '扩充时保留这些源地址、接口、协议等匹配限制。')
    add('新增目的网段', strings(op.add_dst).map(value => ({ label: '新增', value, action: '新增' })))
    add('引用 IP 分组', groupRefs(op), groupNote)
    groups.push(changedFields('备注', record(op.expected), op, ['comment']))
  } else if (op.kind === 'nat_list_sync') {
    const address = (value: unknown) => text(record(record(value).config).address)
    const rule = record(op.rule)
    if (op.withdraw) {
      add('删除规则', [objectRow(op.rule, false)], '已没有需要免转换的代理网段：删除规则，再删除 ConfigFlow 写入的列表条目。')
    } else if (op.rule || list(op.destinations).length) {
      const fields: Fields = { chain: 'srcnat', action: 'accept', 'src-address-list': op.src_list, 'dst-address-list': op.dst_list, comment: op.comment }
      if (op.out_interface) fields['out-interface'] = op.out_interface
      add('规则字段', [{ label: op.rule && !op.replace_rule ? `现有规则 · ${text(rule.id)}` : '新增规则', value: valuesOf(fields) }],
        op.replace_rule ? `先新建限定出口的规则，再删除原规则 ${text(rule.id)}。` : '放在 NAT 表最前，回读确认排在所有源 NAT 转换规则之前。')
    }
    add('来源列表', [...strings(op.src_add).map(value => ({ label: '新增', value, action: '新增' as const })),
      ...list(op.src_remove).map(value => ({ label: '移除', value: address(value), action: '移除' as const }))],
      `${text(op.src_list)}：网关所在 /24 除网关自身外的客户端。`)
    add('目的列表', [...strings(op.dst_add).map(value => ({ label: '新增', value, action: '新增' as const })),
      ...list(op.dst_remove).map(value => ({ label: '移除', value: address(value), action: '移除' as const }))],
      `${text(op.dst_list)}：一个代理网段一个条目。手工加入的条目保留不动。`)
    add('迁移删除的旧规则', list(op.legacy).map(value => objectRow(value, false)),
      `共 ${list(op.legacy).length} 条，合并为上面的 1 条规则：逐条规则过多会拖慢新连接，自动同步也无法更新它们。新规则回读并证明覆盖后批量删除；失败时按原顺序恢复。`)
    add('删除残留的旧规则', list(op.stale).map(value => objectRow(value, false)),
      `共 ${list(op.stale).length} 条，目的网段已不再代理，也没有其他路由把它们送往 mihomo；保留会让去往这些网段的流量经 WAN 出去时不做源 NAT。要删除对应路由时先删这些规则；失败时按原顺序恢复。`)
  } else if (op.kind === 'dhcp_dns_update') {
    add('作用网段', safeFields(op.scope))
    add('现有 DHCP 配置', safeFields(op.expected))
    add('发布后的 DNS', [{ label: 'DNS 列表', value: text(op.dns) }], '替换原 DNS 列表，客户端续租或重新连接后生效。')
  } else if (op.kind === 'ipv6_dns_update') {
    const before = record(op.expected), after = record(op.values)
    add('目标 LAN', [
      { label: 'LAN 接口', value: text(op.interface) },
      ...(op.source_interface && op.source_interface !== op.interface
        ? [{ label: '当前通告来源', value: text(op.source_interface) }] : [])
    ])
    add('发布改动', [{ label: 'DNS 下发 · advertise-dns', before: text(before['advertise-dns']), value: text(after['advertise-dns']) }],
      '只关闭此 LAN 的 DNS 下发；IPv6 地址和默认网关继续通告。')
  } else if (op.kind === 'ipv6_pd_guard_sync') {
    const spec = record(op.spec)
    if (op.spec) {
      add('自动识别的参数', [
        { label: '监控 LAN', value: text(spec.lan) }, { label: '地址池', value: text(spec.pool) },
        { label: 'DHCP 客户端', value: text(spec.client_interface) }, { label: '旧前缀弃用通告（秒）', value: text(spec.hold) }
      ])
    }
    const changes = list(op.changes).map(record)
    const object = (change: Fields) => `${pdGuardPaths[String(change.path)] || text(change.path)}${change.id ? ` · ${text(change.id)}` : ''}`
    add('新建', changes.filter(change => change.method === 'PUT').map(change => ({ label: object(change), value: pdGuardValues(change.values) })),
      '脚本内容按 ConfigFlow 模板生成，参数为上面自动识别的值。')
    add('修改', changes.filter(change => change.method === 'PATCH').map(change => ({
      label: object(change), before: pdGuardValues(pickKeys(change.expected, change.values)), value: pdGuardValues(change.values)
    })), '只改写列出的字段；dhcp-client 只改 script 字段。')
    add('删除', changes.filter(change => change.method === 'DELETE').map(change => ({ label: object(change), value: pdGuardValues(change.expected) })),
      op.enabled ? '脚本改为监控其他 LAN 或地址池时，清除旧的前缀记录，由脚本重新记录。'
        : '只撤回 ConfigFlow 安装或更新过的对象，以及脚本留下的前缀记录和弃用通告；沿用的手动安装不在此列表中。')
    if (!changes.length) add('管理记录', [{ label: '路由器', value: '不写入' }], '路由器上的配置不变，只更新 ConfigFlow 的管理记录。')
  } else if (op.kind === 'nat_add') {
    add('来源范围', strings(op.src).map(value => ({ label: '来源', value })))
    add('目的网段', strings(op.dst).map(value => ({ label: '目的', value })))
    add('引用 IP 分组', groupRefs(op), groupNote)
    add('规则备注', [{ label: '备注', value: text(op.comment) }])
  } else if (hasOperationDetails(op)) {
    const values: Fields = {}
    for (const key of ['id', 'cidr', 'gateway', 'comment']) if (key in op) values[key] = op[key]
    add(op.kind?.endsWith('_delete') ? '删除对象' : '提交字段', safeFields(values), '仅列出本次计划提供的字段；概要中的接管或删除含义保持不变。')
    add('引用 IP 分组', groupRefs(op), groupNote)
    add('目的网段', strings(op.dst).map(value => ({ label: '目的', value })))
  }
  return groups.filter(group => group.rows.length)
}
