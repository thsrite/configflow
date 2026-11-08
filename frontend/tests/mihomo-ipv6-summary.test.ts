import { describe, expect, it } from 'vitest'
import { buildMihomoIPv6Summary } from '@/lib/mihomoIPv6Summary'
import type { IPv6ReadinessCheck, IPv6Verification, MihomoIPv6Readiness } from '@/types/mihomoIPv6'

const check = (id: string, status: IPv6ReadinessCheck['status'] = 'ok', message = `${id} 的检查结果`): IPv6ReadinessCheck => ({ id, label: id, status, message })
const readiness = (overrides: Partial<MihomoIPv6Readiness> = {}): MihomoIPv6Readiness => ({
  success: true, agent_id: 'gw', profile_id: 'default', config_revision: 7,
  configuration: { ipv6: true, dns_ipv6: false, fake_ipv6_range: 'fc00::/64', errors: [] }, host: {},
  checks: [check('host_address'), check('host_default_route'), check('host_persistence')],
  routers: [{ id: 'router_with_underscores', name: '家里的 ROS', status: 'ok', can_prepare: false, checks: [check('dns'), check('wan')] }],
  can_prepare: false, prepare_interface: 'eth0', check_token: 'token',
  connectivity: { status: 'not_checked', message: '尚未验证节点。' }, ...overrides,
})
const runtimeChecks = () => [check('runtime_proxy_http'), check('runtime_node_ipv6_transport')]
const clientChecks = () => ['client_http_ipv4', 'client_mihomo_path', 'client_ros_path', 'client_ipv6_transport'].map(id => check(id))
const verification = (overrides: Partial<IPv6Verification> = {}): IPv6Verification => ({
  status: 'ok', config_revision: 7, scope: 'mihomo_runtime', checks: runtimeChecks(), ...overrides,
})
const group = (id: string, input: Parameters<typeof buildMihomoIPv6Summary>[0]) => buildMihomoIPv6Summary(input).find(row => row.id === id)!

describe('Mihomo IPv6 three outcomes', () => {
  it('shows exactly three outcomes and does not turn a passing inspection into tested connectivity', () => {
    const groups = buildMihomoIPv6Summary({ readiness: readiness() })
    expect(groups.map(row => row.label)).toEqual(['IPv6 网络', 'IPv6 节点连接', '客户端分流'])
    expect(groups[0]).toMatchObject({ tone: 'success', status: '已就绪', issues: [] })
    expect(groups.slice(1).map(row => [row.status, row.tone])).toEqual([['待验证', 'neutral'], ['待验证', 'neutral']])
    expect(groups[1]!.note).toContain('验证连接与分流')
    expect(groups[2]!.note).toContain('验证连接与分流')
  })

  it('requires both a usable address and a default route before calling an inspection ready', () => {
    const current = readiness({ checks: [check('host_address'), check('host_persistence')] })
    expect(group('ipv6-network', { readiness: current })).toMatchObject({ tone: 'neutral', status: '待检查' })
    expect(group('ipv6-network', { readiness: current }).note).toContain('打开「检查设备」，点击「重新检查」')
  })

  it('uses actual IPv6 traffic to show network availability even without a static inspection', () => {
    expect(group('ipv6-network', { verification: verification() })).toMatchObject({ tone: 'success', status: '可用' })
  })

  it('does not turn ROS route/RA/firewall/NAT uncertainty into daily warnings', () => {
    const current = readiness()
    current.routers[0]!.checks = [check('wan', 'warning'), check('dns', 'warning'), check('firewall', 'unknown'),
      check('dhcpv6_input', 'unknown'), check('nat', 'unknown'), check('fake4_return', 'unknown')]
    const input = { readiness: current, verification: verification({ status: 'warning', scope: 'client_path', checks: [
      ...runtimeChecks(), ...clientChecks(), check('router_ros_nat', 'unknown'),
    ] }) }
    const groups = buildMihomoIPv6Summary(input)
    expect(groups.every(row => row.tone === 'success' && row.issues.length === 0)).toBe(true)
  })

  it('does not invent traffic success from successful static node and client paths', () => {
    const groups = buildMihomoIPv6Summary({ verification: verification({ checks: [
      check('router_ros_node_0_0'), check('router_ros_fake4_0'), check('router_ros_dhcpv6_input', 'unknown'),
    ] }) })
    expect(groups.map(row => row.status)).toEqual(['待检查', '待验证', '待验证'])
    expect(groups.every(row => !row.issues.length)).toBe(true)
  })

  it('shows a short actionable problem for definite router blockers without displaying the trace', () => {
    const blocked = { ...check('router_ros_fake4_0', 'error', '按顺序发现阻断'), details: ['filter/forward #7 drop', 'raw/prerouting trace'] }
    const result = group('client', { verification: verification({ checks: [...runtimeChecks(), blocked] }) })
    expect(result).toMatchObject({ status: '未通过', tone: 'danger' })
    expect(result.issues).toEqual(['客户端分流路由或相关规则存在问题，请到「上游路由」检查差异后再验证。'])
    expect(result.issues.join()).not.toMatch(/filter|prerouting|展开/)
  })


  it('keeps actual traffic success while flagging contradictory static rule predictions', () => {
    const report = verification({ status: 'error', scope: 'client_path', checks: [
      ...runtimeChecks(), ...clientChecks(), check('router_ros_node_0_0', 'error'), check('router_ros_fake4_0', 'error'),
    ] })
    const groups = buildMihomoIPv6Summary({ verification: report })
    expect(groups.every(row => row.tone === 'success')).toBe(true)
    expect(groups[0]!.issues).toEqual(['本次连接已通过；路由器规则检查仍有差异，请到「上游路由」核对。'])
    expect(groups[2]!.issues).toEqual(['本次连接已通过；分流路由检查仍有差异，请到「上游路由」核对。'])
  })

  it('does not let passing observations hide current host or runtime failures', () => {
    const report = verification({ status: 'error', scope: 'client_path', checks: [
      ...runtimeChecks(), ...clientChecks(), check('host_address', 'error'), check('client_http_ipv6', 'error'),
      check('router_ros_node_0_0', 'error'), check('router_ros_fake4_0', 'error'),
    ] })
    const groups = buildMihomoIPv6Summary({ verification: report })
    expect(groups[0]!.tone).toBe('danger')
    expect(groups[2]!.tone).toBe('danger')
    expect(groups.flatMap(row => row.issues).join()).not.toContain('本次连接已通过')
  })

  it('includes an explicit next action even when an unknown check already supplies an issue', () => {
    const report = verification({ checks: [check('runtime_node_ipv6_transport', 'unknown', '尚未观察到对应 IPv6 连接')] })
    const result = group('node', { verification: report })
    expect(result).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(result.issues).toEqual(['尚未观察到对应 IPv6 连接', '点击「验证连接与分流」开始测试。'])
  })

  it('uses the new router snapshot and discards errors for a removed router', () => {
    const input = { readiness: readiness(), verification: verification({ checks: [
      ...runtimeChecks(), check('router_removed_wan', 'error'),
    ] }) }
    expect(group('ipv6-network', input)).toMatchObject({ tone: 'success', issues: [] })
    expect(group('ipv6-network', input).checks.some(row => row.id.includes('removed'))).toBe(false)
  })

  it('uses a newly completed verification snapshot instead of an older passing inspection', () => {
    const report = verification({ checks: [check('host_default_route', 'error'), check('router_ros_dns', 'error'), ...runtimeChecks()] })
    const input = { readiness: readiness(), verification: report, preferVerificationSnapshot: true }
    expect(group('ipv6-network', input).tone).toBe('danger')
    expect(group('ipv6-network', input).issues).toHaveLength(2)
  })

  it('does not overwrite the failures of a newly failed run with the older inspection', () => {
    const input = { readiness: readiness(), preferVerificationSnapshot: true,
      verification: verification({ status: 'error', checks: [check('host_address', 'error'), check('verification_failed', 'error')] }) }
    expect(group('ipv6-network', input).tone).toBe('danger')
    expect(group('node', input).tone).toBe('danger')
  })

  it('retains the current inspection while a new run is pending or its snapshot is stale', () => {
    const input = { readiness: readiness(), preferVerificationSnapshot: true, verification: verification({ status: 'running', checks: [] }) }
    expect(group('ipv6-network', input)).toMatchObject({ tone: 'success', status: '已就绪' })
    input.verification = verification({ config_revision: 6, checks: [check('host_address', 'error'), ...runtimeChecks()] })
    expect(group('ipv6-network', input)).toMatchObject({ tone: 'success', status: '已就绪' })
    expect(group('node', input).tone).not.toBe('success')
  })

  it('keeps real restart problems concise but omits unconfirmed persistence details', () => {
    const current = readiness({ checks: [check('host_address'), check('host_default_route'), check('host_persistence', 'warning')] })
    const result = group('ipv6-network', { readiness: current })
    expect(result).toMatchObject({ tone: 'warning', status: '需处理' })
    expect(result.issues).toEqual(['开机恢复配置有缺项或冲突，请检查设备上的网络恢复设置。'])
    current.checks[2]!.status = 'unknown'
    expect(group('ipv6-network', { readiness: current })).toMatchObject({ tone: 'success', status: '已就绪', issues: [] })
  })


  it('keeps a working IPv6 connection available while reporting restart preparation separately', () => {
    const input = { readiness: readiness({ checks: [check('host_persistence', 'warning')] }), verification: verification() }
    const result = group('ipv6-network', input)
    expect(result).toMatchObject({ status: '可用', tone: 'success' })
    expect(result.issues[0]).toContain('开机恢复')
    input.readiness.checks.push(check('host_address', 'error'))
    expect(group('ipv6-network', input).tone).toBe('danger')
  })

  it('only suggests automatic preparation when that action is available', () => {
    const current = readiness({ checks: [check('host_forwarding_ra', 'warning'), check('host_persistence', 'warning'), check('host_tun_recovery', 'warning')] })
    expect(group('ipv6-network', { readiness: current }).issues.join()).not.toContain('补齐缺少的配置')
    current.can_prepare = true
    expect(group('ipv6-network', { readiness: current }).issues.every(issue => issue.includes('补齐缺少的配置'))).toBe(true)
    expect(group('ipv6-network', { verification: verification({ checks: current.checks }) }).issues.join()).not.toContain('补齐缺少的配置')
  })

  it('keeps DNS IPv6 off independent from using IPv6 nodes and IPv4 client forwarding', () => {
    const input = { readiness: readiness(), verification: verification({ scope: 'client_path', checks: [...runtimeChecks(), ...clientChecks()] }) }
    expect(group('node', input)).toMatchObject({ status: '已通过', tone: 'success' })
    expect(group('client', input)).toMatchObject({ status: '已通过', tone: 'success' })
  })

  it('requires both actual proxy HTTP and IPv6 transport evidence, not just a successful port probe', () => {
    expect(group('node', { verification: verification({ checks: [check('runtime_proxy_http')] }) })).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(group('node', { verification: verification({ checks: [check('runtime_node_ipv6_transport')] }) }).tone).toBe('neutral')
    expect(group('node', { readiness: readiness({ connectivity: { status: 'ok', message: '端口可达' } }) }).tone).not.toBe('success')
    expect(group('node', { verification: verification() }).tone).toBe('success')
  })

  it('does not substitute host socket evidence for client IPv6 evidence', () => {
    const report = verification({ scope: 'client_path', checks: [...runtimeChecks(), ...clientChecks().filter(row => row.id !== 'client_ipv6_transport')] })
    expect(group('node', { verification: report }).tone).toBe('success')
    expect(group('client', { verification: report })).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(group('client', { verification: report }).note).toContain('验证连接与分流')
  })

  it('does not hide an additional IPv6 client failure behind passing IPv4 checks', () => {
    const report = verification({ scope: 'client_path', checks: [...runtimeChecks(), ...clientChecks(), check('client_http_ipv6', 'error')] })
    expect(group('client', { verification: report })).toMatchObject({ tone: 'danger', status: '未通过' })
    expect(group('client', { verification: report }).issues).toContain('客户端访问检测网站失败，请检查客户端的网关和 DNS。')
  })

  it('tells an unconfigured client where to set up verification without showing success', () => {
    const result = group('client', { verification: verification({ checks: [...runtimeChecks(), check('client_not_configured', 'unknown')] }) })
    expect(result).toMatchObject({ status: '未配置检测客户端', tone: 'neutral', issues: [] })
    expect(result.note).toContain('检查设备 → 验收设置')
    expect(result.note).toContain('独立检测客户端')
    expect(result.note).toContain('验证连接与分流')
  })

  it('never hides a client failure or a contradictory response behind the unconfigured marker', () => {
    const failed = verification({ checks: [check('client_not_configured', 'unknown'), check('client_http_ipv4', 'error')] })
    expect(group('client', { verification: failed }).tone).toBe('danger')
    const contradictory = verification({ checks: [check('client_not_configured', 'unknown'), ...clientChecks()] })
    expect(group('client', { verification: contradictory })).toMatchObject({ tone: 'neutral', status: '待验证' })
    expect(group('client', { verification: contradictory }).issues[0]).toContain('不一致')
  })

  it.each(['result_stale', 'target_changed', 'interrupted', 'verification_failed', 'runtime_config_stability'])('rejects successful runtime evidence when %s invalidates the run', id => {
    const result = buildMihomoIPv6Summary({ readiness: readiness(), verification: verification({ checks: [
      ...runtimeChecks(), ...clientChecks(), check(id, id === 'verification_failed' ? 'error' : 'unknown'),
    ] }) })
    expect(result[0]!.tone).toBe('success')
    expect(result[1]!.tone).not.toBe('success')
    expect(result[2]!.tone).not.toBe('success')
    expect(result.flatMap(row => row.checks).some(row => row.id === 'runtime_proxy_http')).toBe(false)
    expect(result[1]!.issues.join()).toContain('验证连接与分流')
  })

  it('does not reject a successful stability check', () => {
    expect(group('node', { verification: verification({ checks: [...runtimeChecks(), check('runtime_config_stability')] }) }).tone).toBe('success')
  })

  it('does not merge another saved revision or a stale flag into current success', () => {
    expect(group('node', { readiness: readiness(), verification: verification({ config_revision: 6 }) }).tone).not.toBe('success')
    const stale = { ...verification(), stale: true }
    expect(group('node', { verification: stale }).tone).not.toBe('success')
  })

  it.each(['runtime_deployed_config', 'runtime_expected_config'])('requires publishing when %s is missing', id => {
    const input = { verification: verification({ checks: [...runtimeChecks(), ...clientChecks(), check(id, 'unknown', '未提供本次发布内容的指纹')] }) }
    expect(group('ipv6-network', input)).toMatchObject({ status: '待发布', tone: 'neutral' })
    expect(group('ipv6-network', input).issues[0]).toContain('保存并发布')
    expect(group('node', input)).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(group('node', input).note).toContain('保存并发布')
    expect(group('client', input).tone).not.toBe('success')
  })

  it('keeps unfamiliar errors and deduplicates problems instead of rendering raw details', () => {
    const report = verification({ checks: [check('new_backend_check', 'error', '新检查失败，请检查设备后重试。'),
      check('router_ros_fake4_0', 'error'), check('router_ros_fake4_1', 'error')] })
    expect(group('node', { verification: report })).toMatchObject({ tone: 'danger', issues: ['新检查失败，请检查设备后重试。'] })
    expect(group('client', { verification: report }).issues).toHaveLength(1)
  })

  it('does not turn failed/empty or unexplained warning reports into passing outcomes', () => {
    expect(group('node', { verification: verification({ status: 'error', checks: [] }) }).tone).toBe('danger')
    expect(group('node', { verification: verification({ status: 'warning' }) })).toMatchObject({ status: '待验证', tone: 'neutral' })
    expect(group('node', { verification: verification({ status: 'warning' }) }).issues[0]).toContain('重新验证')
  })

  it('shows running state only for the scopes actually being tested', () => {
    const input = { readiness: readiness(), verification: verification({ status: 'running', checks: [] }) }
    expect(group('node', input)).toMatchObject({ status: '验证中', tone: 'progress' })
    expect(group('client', input).status).toBe('待验证')
    input.verification.scope = 'client_path'
    expect(group('client', input)).toMatchObject({ status: '验证中', tone: 'progress' })
  })

  it('does not claim network traffic success from partial running results', () => {
    expect(group('ipv6-network', { verification: verification({ status: 'running', checks: runtimeChecks() }) }).tone).not.toBe('success')
  })

  it('gives a pending action without a dead expand-details instruction', () => {
    const groups = buildMihomoIPv6Summary({ verification: verification({ checks: [check('runtime_evidence', 'unknown')] }) })
    expect(groups[1]!.issues[0]).toContain('重新验证')
    expect(groups.flatMap(row => [...row.issues, row.note || '']).join()).not.toContain('展开')
  })
})

describe('Mihomo IPv6 outcomes beside the step list', () => {
  it('drops 「IPv6 网络」, which repeats the host check of the 部署 steps', () => {
    const groups = buildMihomoIPv6Summary({ readiness: readiness(), besideSteps: true })
    expect(groups.map(row => row.label)).toEqual(['IPv6 节点连接', '客户端分流'])
    expect(groups.every(row => !!row.help)).toBe(true)
    expect(groups[0]).toMatchObject({ status: '待验证', tone: 'neutral', issues: [] })
    expect(groups[0]!.note).toContain('验证连接与分流')
  })

  it('leaves host problems to the 部署 steps', () => {
    const current = readiness({ checks: [check('host_address', 'error'), check('host_default_route', 'warning'), check('host_persistence')] })
    const [node] = buildMihomoIPv6Summary({ readiness: current, besideSteps: true })
    expect(node).toMatchObject({ status: '待验证', tone: 'neutral', issues: [] })
  })

  it('moves the runtime and router problems of 「IPv6 网络」 to the node outcome with the next action', () => {
    const report = verification({ status: 'error', checks: [...runtimeChecks(), check('runtime_dns_aaaa', 'error'), check('host_address', 'error')] })
    const [node] = buildMihomoIPv6Summary({ verification: report, besideSteps: true })
    expect(node).toMatchObject({ status: '未通过', tone: 'danger' })
    expect(node!.issues).toEqual(['Mihomo 的 DNS 查询失败，请检查 DNS 设置后重新验证。'])

    const current = readiness()
    current.routers[0]!.checks = [check('wan', 'error')]
    const [pending] = buildMihomoIPv6Summary({ readiness: current, besideSteps: true })
    expect(pending).toMatchObject({ status: '未通过', tone: 'danger' })
    expect(pending!.issues).toEqual(['路由器 IPv6 出口存在问题，请检查上网设置。'])
  })

  it('asks to publish first while the saved configuration is not published yet', () => {
    const groups = buildMihomoIPv6Summary({ readiness: readiness({ publication: 'changed' }), besideSteps: true })
    expect(groups.map(row => row.note)).toEqual(['先「保存并发布」到 Mihomo，再点击「验证连接与分流」。', '先「保存并发布」到 Mihomo，再点击「验证连接与分流」。'])
    // The device check and the publish bar keep their own wording.
    expect(group('node', { readiness: readiness({ publication: 'changed' }) }).note).toBe('点击「验证连接与分流」开始测试。')
  })

  it('keeps the next action when a moved problem does not change the status', () => {
    const report = verification({ checks: [...runtimeChecks(), check('runtime_expected_config', 'unknown')] })
    const [node] = buildMihomoIPv6Summary({ verification: report, besideSteps: true })
    expect(node!.status).toBe('待验证')
    expect(node!.note).toBeUndefined()
    expect(node!.issues).toEqual(['尚未确认设备使用的是当前保存配置。先「保存并发布」到 Mihomo，再点击「验证连接与分流」。'])
  })
})
