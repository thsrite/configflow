import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import RouterOperationPreview from '@/components/routers/RouterOperationPreview.vue'
import { operationDetails, type RouterPlanOperation } from '@/lib/routerOperationDetails'
const address = '/ip/firewall/address-list'
const fixture = (): RouterPlanOperation => ({ kind: 'ros_policy_replace', summary: '发布来源策略',
  previous: { spec: { sources: ['192.0.2.101/32'], destinations: ['203.0.113.0/24'], gateway: '192.0.2.3' }, resources: [{ id: '*missing', path: address, config: { address: '192.0.2.51' } }] },
  spec: { sources: ['192.0.2.105/32'], destinations: ['203.0.113.0/24', '198.51.100.0/24'], gateway: '192.0.2.4', egress_interface: 'lan' },
  bundle: [{ path: address, values: { list: 'CF_Source_Proxy_IP_02', address: '192.0.2.105/32', disabled: 'false' } },
    { path: '/ip/firewall/mangle', values: { chain: 'CF_Proxy_Routing_02', action: 'mark-routing', 'new-routing-mark': 'CF_Proxy_Routing_02', passthrough: 'false' } }],
  owned: [{ id: '*owned', path: address, config: { list: 'CF_Source_Proxy_IP_01', address: '192.0.2.101/32' } }],
  migrations: [{ id: '*static', path: '/ip/route', config: { 'dst-address': '198.51.100.0/24', gateway: '192.0.2.4', 'routing-table': 'main' } }],
  evidence: { unrelated_rule: 'do-not-render-evidence' }
})
const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(w => w.unmount()))
describe('reviewable release operation details', () => {
  it('distinguishes logical address changes from exact object replacement and excludes unrelated evidence', () => {
    const groups = operationDetails(fixture())
    expect(groups.find(g => g.title === '来源范围')!.rows).toEqual([
      { label: '新增', value: '192.0.2.105/32', action: '新增' }, { label: '移除', value: '192.0.2.101/32', action: '移除' }])
    expect(groups.find(g => g.title === '目的网段')!.rows).toContainEqual({ label: '保留', value: '203.0.113.0/24', action: '保留' })
    expect(groups.find(g => g.title === '网关与出口')!.rows[0]).toEqual({ label: '下一跳', before: '192.0.2.3', value: '192.0.2.4' })
    expect(groups.find(g => g.title === '新增Mangle')!.rows[0].value).toContain('new-routing-mark: CF_Proxy_Routing_02')
    expect(groups.find(g => g.title === '删除旧受管对象')!.rows).toHaveLength(1)
    expect(groups.find(g => g.title === '迁移主表静态路由')!.rows[0].label).toContain('*static')
    expect(JSON.stringify(groups)).not.toContain('*missing'); expect(JSON.stringify(groups)).not.toContain('do-not-render-evidence')
  })
  it('shows return marking by source instead of a destination list', () => {
    const spec = { router_ip: '192.0.2.1', mark: '0x400000', table: 104, priority: 104, safety_version: 2 }
    const moved = operationDetails({ kind: 'return_routing_apply', summary: '应用回流规则', spec,
      previous: { ...spec, safety_version: 1, proxy_dst: ['198.18.0.0/16', '91.108.0.0/16'] } })
    expect(moved.find(g => g.title === '回流标记范围')!.rows).toEqual([
      { label: '标记', before: '2 个目的网段', value: '主路由转来的全部新连接' }])
    expect(moved.some(g => g.title === '回流目的网段')).toBe(false)
    const fresh = operationDetails({ kind: 'return_routing_apply', summary: '应用回流规则', spec, previous: null })
    expect(fresh.find(g => g.title === '回流标记范围')!.rows).toEqual([{ label: '标记', value: '主路由转来的全部新连接' }])
  })
  it('shows withdrawal as removals without inventing new objects', () => {
    const op = fixture(); op.bundle = []; op.spec = null; op.migrations = []
    const groups = operationDetails(op)
    expect(groups.find(g => g.title === '来源范围')!.rows.every(r => r.action === '移除')).toBe(true)
    expect(groups.some(g => g.title.startsWith('新增'))).toBe(false)
    expect(groups.find(g => g.title === '删除旧受管对象')!.note).toContain('撤销策略')
  })
  it('retains NAT source restrictions and separates appended destinations from existing matchers', () => {
    const groups = operationDetails({ kind: 'nat_update', summary: '扩充 NAT', expected: { 'src-address': '192.0.2.0/24', protocol: 'tcp', 'out-interface': 'lan', password: 'not-public' }, add_dst: ['203.0.113.0/24'], comment: 'cf-test' })
    expect(groups.find(g => g.title === '现有匹配条件')!.rows.map(row => row.value)).toEqual(['192.0.2.0/24', 'tcp', 'lan'])
    expect(groups.find(g => g.title === '新增目的网段')!.rows[0].value).toBe('203.0.113.0/24')
    expect(JSON.stringify(groups)).not.toContain('not-public')
  })
  it('shows the NAT list rule, member changes and the old rules it replaces', () => {
    const legacy = Array.from({ length: 3 }, (_, i) => ({ id: `*L${i}`, path: '/ip/firewall/nat',
      config: { chain: 'srcnat', action: 'accept', 'src-address': '192.0.2.1/32', 'dst-address': `203.0.113.${i}/32`, comment: 'cf-mihomo' } }))
    const groups = operationDetails({ kind: 'nat_list_sync', summary: '新增 NAT 免转换规则', comment: 'cf-mihomo-nat',
      src_list: 'cf-mihomo-nat-src', dst_list: 'cf-mihomo-nat-dst', rule: null, out_interface: '', replace_rule: false, withdraw: false,
      src_add: ['192.0.2.1/32'], src_remove: [], dst_add: ['203.0.113.0/24'], destinations: ['203.0.113.0/24'],
      dst_remove: [{ id: '*9', path: address, config: { list: 'cf-mihomo-nat-dst', address: '198.51.100.0/24' } }], legacy })
    expect(groups.map(g => g.title)).toEqual(['规则字段', '来源列表', '目的列表', '迁移删除的旧规则'])
    expect(groups[0].rows[0].value).toContain('目的地址列表 · dst-address-list: cf-mihomo-nat-dst')
    expect(groups[2].rows).toEqual([{ label: '新增', value: '203.0.113.0/24', action: '新增' }, { label: '移除', value: '198.51.100.0/24', action: '移除' }])
    expect(groups[3].rows).toHaveLength(3); expect(groups[3].note).toContain('共 3 条，合并为上面的 1 条规则')
    const stale = operationDetails({ kind: 'nat_list_sync', summary: '删除残留', comment: 'cf-mihomo-nat', src_list: 'cf-mihomo-nat-src',
      dst_list: 'cf-mihomo-nat-dst', rule: { id: '*R', path: '/ip/firewall/nat', config: {} }, out_interface: '', replace_rule: false,
      withdraw: false, src_add: [], src_remove: [], dst_add: [], dst_remove: [], legacy: [], stale: legacy.slice(0, 2) })
    expect(stale.map(g => g.title)).toEqual(['规则字段', '删除残留的旧规则'])
    expect(stale[1].rows).toHaveLength(2); expect(stale[1].note).toContain('目的网段已不再代理')
    // Removed ahead of the routes before any list rule exists: no rule to show.
    const early = operationDetails({ kind: 'nat_list_sync', summary: '删除残留', comment: 'cf-mihomo-nat', src_list: 'cf-mihomo-nat-src',
      dst_list: 'cf-mihomo-nat-dst', rule: null, out_interface: '', replace_rule: false, destinations: [],
      withdraw: false, src_add: [], src_remove: [], dst_add: [], dst_remove: [], legacy: [], stale: legacy.slice(0, 2) })
    expect(early.map(g => g.title)).toEqual(['删除残留的旧规则'])
    const narrowed = operationDetails({ kind: 'nat_list_sync', summary: '限定出口', comment: 'cf-mihomo-nat', src_list: 'cf-mihomo-nat-src',
      dst_list: 'cf-mihomo-nat-dst', rule: { id: '*R', path: '/ip/firewall/nat', config: {} }, out_interface: 'bridge', replace_rule: true,
      withdraw: false, src_add: [], src_remove: [], dst_add: [], dst_remove: [], legacy: [] })
    expect(narrowed[0].rows[0].value).toContain('出口接口 · out-interface: bridge'); expect(narrowed[0].note).toContain('*R')
  })
  it('separates policy migrations by object type', () => {
    const op = fixture(); op.migrations = [...(op.migrations as unknown[]), { id: '*M', path: address, config: { list: 'cf-mihomo-nat-dst', address: '203.0.113.0/24' } }]
    const groups = operationDetails(op)
    expect(groups.find(g => g.title === '迁移主表静态路由')!.rows).toHaveLength(1)
    expect(groups.find(g => g.title === '移出 NAT 免转换目的列表')!.rows[0].label).toContain('*M')
  })
  it('shows iKuai IP group writes and the groups a rule references', () => {
    const add = operationDetails({ kind: 'ikuai_group_add', summary: '新建', name: 'cf_pa_1', comment: 'cf-mihomo', values: ['198.51.100.0/24', '203.0.113.0/24'] })
    expect(add.find(g => g.title === 'IP 分组')!.rows[0]).toEqual({ label: '名称', value: 'cf_pa_1' })
    expect(add.find(g => g.title === '分组网段')!.rows.map(row => row.value)).toEqual(['198.51.100.0/24', '203.0.113.0/24'])
    const update = operationDetails({ kind: 'ikuai_group_update', summary: '更新', name: 'cf_pa_2', values: ['192.0.2.0/24', '198.51.100.0/24'],
      added: ['192.0.2.0/24'], removed: ['203.0.113.0/24'], expected: 'abc' })
    expect(update.find(g => g.title === '分组网段变化')!.rows).toEqual([
      { label: '新增', value: '192.0.2.0/24', action: '新增' }, { label: '移除', value: '203.0.113.0/24', action: '移除' }])
    expect(update.find(g => g.title === 'IP 分组')!.rows[1]).toEqual({ label: '更新后网段数', value: '2' })
    const mark = operationDetails({ kind: 'ikuai_group_update', summary: '恢复标记', name: 'cf_pa_1', comment: 'cf-mihomo', mark: true,
      values: ['192.0.2.0/24'], added: [], removed: [], expected: 'abc' })
    expect(mark.map(g => g.title)).toEqual(['IP 分组'])
    expect(mark[0].rows[1]).toEqual({ label: '归属标记', value: '改回 cf-mihomo' })
    const removal = operationDetails({ kind: 'ikuai_group_delete', summary: '删除', name: 'cf_pa_3', count: 20 })
    expect(removal[0].rows.map(row => row.value)).toEqual(['cf_pa_3', '20'])
    const rule = operationDetails({ kind: 'policy_route_update', summary: '迁移', id: '1', gateway: '192.0.2.3', comment: 'cf-mihomo',
      groups: ['cf_pa_1', 'cf_pa_2'], dst: ['198.51.100.0/24'] })
    expect(rule.find(g => g.title === '引用 IP 分组')!.rows.map(row => row.value)).toEqual(['cf_pa_1', 'cf_pa_2'])
    const nat = operationDetails({ kind: 'nat_add', summary: '新增', src: ['192.0.2.1-192.0.2.2'], dst: ['198.51.100.0/24'], groups: ['cf_nat_1'], comment: 'cf-mihomo' })
    expect(nat.map(g => g.title)).toEqual(['来源范围', '目的网段', '引用 IP 分组', '规则备注'])
  })
  it('shows only the planned IPv6 DNS setting and its LAN, without exposing inspection snapshots', () => {
    const groups = operationDetails({ kind: 'ipv6_dns_update', summary: '关闭 bridge-lan 的 IPv6 DNS 下发',
      interface: 'bridge-lan', source_interface: 'bridge-lan', values: { 'advertise-dns': 'no' },
      expected: { 'advertise-dns': 'yes', 'ra-lifetime': '30m', 'dns-servers': '2001:db8::53' },
      expected_rows: [{ interface: 'pppoe-out', 'advertise-dns': 'self' }], evidence: { unrelated: 'do-not-render' } })
    expect(groups.map(group => group.title)).toEqual(['目标 LAN', '发布改动'])
    expect(groups[0].rows).toEqual([{ label: 'LAN 接口', value: 'bridge-lan' }])
    expect(groups[1].rows).toEqual([{ label: 'DNS 下发 · advertise-dns', before: 'yes', value: 'no' }])
    expect(groups[1].note).toContain('IPv6 地址和默认网关继续通告')
    expect(JSON.stringify(groups)).not.toContain('pppoe-out')
    expect(JSON.stringify(groups)).not.toContain('ra-lifetime')
    expect(JSON.stringify(groups)).not.toContain('do-not-render')
  })
  it('shows the cf-pd changes with the detected parameters, old and new fields, and owned removals', () => {
    const install = operationDetails({ kind: 'ipv6_pd_guard_sync', summary: '更新 IPv6 前缀变化保护（lan4，地址池 ipv6pool）', enabled: true,
      spec: { lan: 'lan4', pool: 'ipv6pool', client: '*0', client_interface: 'pppoe-out1', hold: 3600 },
      changes: [
        { role: 'scheduler', method: 'PUT', path: '/system/scheduler', values: { name: 'cf-pd-poll', interval: '30s' } },
        { role: 'script', method: 'PATCH', path: '/system/script', id: '*S1', values: { source: ':local hold 3600\n:log info x' },
          expected: { name: 'cf-pd-watch', source: ':local hold 1800\n:log info x', owner: 'admin' } },
        { role: 'state', method: 'DELETE', path: '/ipv6/firewall/address-list', id: '*L1', expected: { list: 'cf-pd-last', address: '2001:db8::/64' } },
      ] })
    expect(install.map(group => group.title)).toEqual(['自动识别的参数', '新建', '修改', '删除'])
    expect(install[0].rows.map(row => row.value)).toEqual(['lan4', 'ipv6pool', 'pppoe-out1', '3600'])
    expect(install[1].rows[0]).toEqual({ label: '定时任务', value: 'name: cf-pd-poll\ninterval: 30s' })
    expect(install[2].rows[0]).toEqual({ label: '脚本 · *S1', before: 'source:\n:local hold 1800\n:log info x', value: 'source:\n:local hold 3600\n:log info x' })
    expect(install[3].rows[0].label).toBe('前缀记录 · *L1')
    const withdraw = operationDetails({ kind: 'ipv6_pd_guard_sync', summary: '撤回', enabled: false, spec: null, changes: [
      { role: 'hook', method: 'PATCH', path: '/ipv6/dhcp-client', id: '*0', values: { script: '' }, expected: { script: ':if (1) do={}', interface: 'pppoe-out1' } }] })
    expect(withdraw.map(group => group.title)).toEqual(['修改'])
    expect(withdraw[0].rows[0]).toEqual({ label: 'IPv6 DHCP 客户端 · *0', before: 'script: :if (1) do={}', value: 'script: 未设置' })
    const record = operationDetails({ kind: 'ipv6_pd_guard_sync', summary: '记录', enabled: false, spec: null, changes: [] })
    expect(record.map(group => group.title)).toEqual(['管理记录'])
  })
  it('mounts detail rows only when expanded, searches every address and paginates large plans', async () => {
    const op = fixture(); op.previous = null
    op.spec = { sources: Array.from({ length: 120 }, (_, i) => `192.0.2.${i + 1}/32`), destinations: [], gateway: '192.0.2.3' }
    const w = mount(RouterOperationPreview, { props: { op } }); wrappers.push(w)
    expect(w.findAll('dd')).toHaveLength(0)
    await w.get('button').trigger('click')
    expect(w.findAll('dd')).toHaveLength(50)
    await w.findAll('button').find(b => b.text() === '下一页')!.trigger('click')
    expect(w.find('dd').text()).toBe('192.0.2.51/32')
    await w.get('[aria-label="搜索变更详情"]').setValue('192.0.2.120/32')
    expect(w.findAll('dd')).toHaveLength(1); expect(w.find('dd').text()).toBe('192.0.2.120/32')
    await w.setProps({ op: { kind: 'route_add', summary: '新预览', cidr: '198.51.100.0/24', gateway: '192.0.2.4' } })
    expect(w.findAll('dd')).toHaveLength(0); expect(w.text()).toContain('新预览')
  })

  it('lists DNS guard objects in creation order and only previously owned objects for removal', () => {
    const create = [
      { role: 'hairpin-udp', path: '/ip/firewall/nat', values: { chain: 'srcnat', action: 'masquerade', 'dst-address': '192.0.2.53' } },
      { role: 'hijack-udp', path: '/ip/firewall/nat', top: true, values: { chain: 'dstnat', 'to-addresses': '192.0.2.53' } }
    ]
    const owned = [{ role: 'bypass:192.0.2.53/32', path: address, id: '*1', config: { list: 'cf-dnsguard-bypass' } },
      { role: 'netwatch', path: '/tool/netwatch', id: '*2', config: { host: '192.0.2.53', port: '53' } }]
    const groups = operationDetails({ kind: 'dns_guard_replace', summary: '更新', spec: {}, create, owned })
    expect(groups.find(g => g.title === '新建对象')!.rows.map(row => row.label)).toEqual(['NAT · hairpin-udp', 'NAT · hijack-udp'])
    expect(groups.find(g => g.title === '新建对象')!.rows[1].value).toContain('转发到地址 · to-addresses: 192.0.2.53')
    expect(groups.find(g => g.title === '删除旧受管对象')!.rows.map(row => row.label)).toEqual(['Netwatch · netwatch', '放行地址 · bypass:192.0.2.53/32'])
    const withdrawal = operationDetails({ kind: 'dns_guard_replace', summary: '撤销', spec: null, create: [], owned })
    expect(withdrawal.map(g => g.title)).toEqual(['删除旧受管对象'])
    expect(withdrawal[0].note).toContain('关闭强制 DNS')
    // An incremental plan deletes only what changes and says what stays.
    const incremental = operationDetails({ kind: 'dns_guard_replace', summary: '更新', spec: {}, create: create.slice(0, 1),
      owned, delete: owned.slice(1), keep: ['bypass:192.0.2.53/32', 'hijack-udp'] })
    expect(incremental.find(g => g.title === '删除旧受管对象')!.rows.map(row => row.label)).toEqual(['Netwatch · netwatch'])
    expect(incremental.find(g => g.title === '保持不变')!.rows.map(row => row.label)).toEqual(['bypass:192.0.2.53/32', 'hijack-udp'])
  })
})

