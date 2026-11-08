import type { IPv6ReadinessCheck, IPv6Verification, MihomoIPv6Readiness } from '@/types/mihomoIPv6'

type SummaryTone = 'success' | 'warning' | 'danger' | 'progress' | 'neutral'
type GroupId = 'ipv6-network' | 'node' | 'client'

export interface IPv6SummaryCheck extends IPv6ReadinessCheck {
  displayLabel?: string
  displayStatus?: string
  displayTone?: SummaryTone
}

export interface IPv6SummaryGroup {
  id: GroupId
  label: string
  status: string
  tone: SummaryTone
  issues: string[]
  // What the result tests, for the step list's help button.
  help?: string
  // Retain source evidence for result decisions; the UI renders only the three
  // outcomes and their short problems, never the raw rule trace or details.
  checks: IPv6SummaryCheck[]
  note?: string
}

interface SummaryInput {
  readiness?: MihomoIPv6Readiness | null
  verification?: IPv6Verification | null
  preferVerificationSnapshot?: boolean
  // Shown beside the IPv6 step list, whose 部署 steps already report the host
  // and the publication: 「IPv6 网络」 then mostly repeats them, so its other
  // problems (runtime, routers) move to 「IPv6 节点连接」.
  besideSteps?: boolean
}

const definitions: Array<{ id: GroupId; label: string; help?: string }> = [
  { id: 'ipv6-network', label: 'IPv6 网络' },
  { id: 'node', label: 'IPv6 节点连接', help: '在 Mihomo 主机上经代理节点打开检测网址，确认 Mihomo 能用 IPv6 连到节点。' },
  { id: 'client', label: '客户端分流', help: '在「检查设备 → 验收设置」选的检测客户端像手机一样上网，确认它的请求经上游路由到 Mihomo，并按规则分流。「Mihomo 用 IPv6，客户端走 IPv4」时它用 IPv4 测。' },
]
const invalidResultIds = new Set(['result_stale', 'target_changed', 'interrupted', 'verification_failed', 'runtime_config_stability'])
const publicationIds = new Set(['runtime_deployed_config', 'runtime_expected_config', 'deployed_config'])
const rank = { error: 0, warning: 1, unknown: 2, ok: 3 }
const recoveryIds = new Set(['host_persistence', 'persistence', 'host_tun_recovery', 'prepare_tun_recovery'])
const nodeRequired = ['runtime_proxy_http', 'runtime_node_ipv6_transport']
const clientRequired = ['client_http_ipv4', 'client_mihomo_path', 'client_ros_path', 'client_ipv6_transport']
const verifyAction = '点击「验证连接与分流」开始测试。'
// Checks a 部署 step reports (the host and the saved publication); see besideSteps.
// Router problems stay: the steps only show the routes and the client Fake-IP path.
const coveredBySteps = (id: string) => id.startsWith('host_') || ['configuration', 'deployed_config', 'prepare_tun_recovery'].includes(id)
const toneRank: Record<SummaryTone, number> = { danger: 0, warning: 1, neutral: 2, progress: 3, success: 4 }
const publishAction = '先「保存并发布」到 Mihomo，再点击「验证连接与分流」。'
const clientSetup = '在「检查设备 → 验收设置」选择独立检测客户端，再点击「验证连接与分流」。'

function groupFor(check: IPv6ReadinessCheck): GroupId {
  const id = check.id
  if (id.startsWith('client_') || id === 'client') return 'client'
  if (id.startsWith('router_')) {
    return /_(?:fake[46]_|fake_ipv6)/.test(id) ? 'client' : 'ipv6-network'
  }
  if (id.startsWith('host_') || id.startsWith('runtime_dns_') || publicationIds.has(id)
    || ['configuration', 'gateway_role', 'router', 'ra', 'persistence', 'prepare_tun_recovery',
      'runtime_runtime_core', 'runtime_runtime_ipv6', 'runtime_runtime_tun'].includes(id)) return 'ipv6-network'
  // An unfamiliar backend failure must remain visible, even before this client
  // knows the exact check. Successful unfamiliar observations prove no traffic.
  return 'node'
}

// Also used by the IPv6 step list, so a host problem reads the same in both places.
export function issueFor(check: IPv6SummaryCheck, canPrepare = false): string {
  const id = check.id
  if (publicationIds.has(id)) return `尚未确认设备使用的是当前保存配置。${publishAction}`
  if (id === 'host_default_route') return 'Mihomo 尚未找到 IPv6 出口，请检查上游路由和自动获取网关设置。'
  if (id === 'host_address') return 'Mihomo 尚未取得可用的 IPv6 地址，请检查设备网络。'
  if (id === 'host_forwarding_ra') return canPrepare
    ? 'IPv6 转发和自动获取网关设置未齐全，可点击「补齐缺少的配置」。'
    : '请检查设备的 IPv6 转发和自动获取网关设置。'
  if (id === 'host_persistence' || id === 'persistence') return canPrepare
    ? '开机恢复配置有缺项，可点击「补齐缺少的配置」。'
    : '开机恢复配置有缺项或冲突，请检查设备上的网络恢复设置。'
  if (id === 'host_tun_recovery' || id === 'prepare_tun_recovery') return canPrepare
    ? 'Mihomo 的 IPv6 接收尚未恢复，可点击「补齐缺少的配置」。'
    : 'Mihomo 的 IPv6 接收尚未恢复，请打开「检查设备」重新检查并处理。'
  if (id.startsWith('router_')) {
    if (/_(?:fake[46]_|fake_ipv6)/.test(id)) return '客户端分流路由或相关规则存在问题，请到「上游路由」检查差异后再验证。'
    if (id.endsWith('_dns')) return '路由器 DNS 配置存在问题，请检查是否使用预期的 DNS。'
    if (id.endsWith('_wan')) return '路由器 IPv6 出口存在问题，请检查上网设置。'
    return '路由器有规则阻断相关通信，请检查防火墙或 NAT 设置后重新验证。'
  }
  if (id === 'runtime_proxy_http' && check.status === 'error') return '通过节点访问检测网站失败，请检查节点状态或更换节点后重新验证。'
  if (id.startsWith('runtime_dns_') && check.status === 'error') return 'Mihomo 的 DNS 查询失败，请检查 DNS 设置后重新验证。'
  if (id.startsWith('client_http_') && check.status === 'error') return '客户端访问检测网站失败，请检查客户端的网关和 DNS。'
  if (id === 'runtime_evidence') return '设备没有返回完整验收结果，请检查 Agent 版本后重新验证。'
  if (invalidResultIds.has(id)) {
    return id === 'verification_failed' || id === 'interrupted'
      ? '本次验证未完成，请检查设备是否在线，再点击「验证连接与分流」。'
      : '配置或设备已变化，请重新检查设备，再点击「验证连接与分流」。'
  }
  // Server messages are already concise descriptions; never surface raw details
  // or leave a dead instruction pointing to a removed expandable panel.
  return (check.message || `${check.displayLabel || check.label}未完成，请重新验证。`)
    .replace(/请展开查看(?:缺项或冲突|原因|详情)?[。]?/g, '请重新检查后处理。')
    .replace(/请展开核对(?:对应路径)?[。]?/g, '请核对配置后重新验证。')
}

function reportChecks(readiness: MihomoIPv6Readiness | null | undefined, verification: IPv6Verification | null | undefined, preferVerificationSnapshot = false): IPv6SummaryCheck[] {
  const checks = new Map<string, IPv6SummaryCheck>()
  const mismatched = readiness?.config_revision !== undefined && verification && readiness.config_revision !== verification.config_revision
  const invalid = verification?.checks.filter(check => invalidResultIds.has(check.id) && check.status !== 'ok') || []
  const stale = verification && 'stale' in verification && verification.stale === true
  if (verification) {
    if (mismatched || stale || invalid.length) {
      const reasons = invalid.length ? invalid : [{ id: 'result_stale', label: '验收结果需更新', status: 'unknown' as const,
        message: '当前配置或设备已变化，请重新验证连接与分流。' }]
      for (const check of reasons) checks.set(check.id, check)
      // A failed current run can contain useful failures, but no successful
      // observation from an invalidated run is accepted as current evidence.
      if (!mismatched && !stale && !invalid.some(check => ['result_stale', 'target_changed'].includes(check.id))) {
        for (const check of verification.checks.filter(check => check.status !== 'ok')) checks.set(check.id, check)
      }
    } else {
      for (const check of verification.checks) checks.set(check.id, check)
    }
    if (verification.status === 'error' && ![...checks.values()].some(check => check.status === 'error') && !mismatched && !stale) {
      for (const [id, check] of checks) if ((id.startsWith('runtime_') || id.startsWith('client_')) && check.status === 'ok') checks.delete(id)
      checks.set('verification_failed', { id: 'verification_failed', label: '验收未完成', status: 'error', message: verification.message || '验收未完成，请重新验证。' })
    }
    if (verification.status === 'warning' && ![...checks.values()].some(check => check.status !== 'ok' && check.id !== 'client_not_configured')) {
      checks.set('verification_incomplete', { id: 'verification_incomplete', label: '验收未完成', status: 'unknown', message: '未取得完整验收结果，请点击「验证连接与分流」重新验证。' })
    }
  }
  const useVerificationSnapshot = preferVerificationSnapshot && verification && verification.status !== 'running'
    && !mismatched && !stale && !invalid.some(check => ['result_stale', 'target_changed'].includes(check.id))
  if (readiness && !useVerificationSnapshot) {
    for (const [id] of checks) {
      if (id.startsWith('host_') || id.startsWith('router_') || ['configuration', 'deployed_config', 'router', 'gateway_role', 'prepare_tun_recovery'].includes(id)) checks.delete(id)
    }
    for (const check of readiness.checks) checks.set(check.id, check)
    for (const router of readiness.routers) {
      for (const check of router.checks) {
        const row = { ...check, id: `router_${router.id}_${check.id}`, displayLabel: `${router.name} · ${check.label}` }
        checks.set(row.id, row)
      }
    }
    if (readiness.connectivity.status !== 'not_checked') {
      checks.set('node_port_probe', { id: 'node_port_probe', label: '节点端口测试', ...readiness.connectivity, status: readiness.connectivity.status })
    }
  }
  return [...checks.values()]
}

export function buildMihomoIPv6Summary({ readiness, verification, preferVerificationSnapshot, besideSteps }: SummaryInput): IPv6SummaryGroup[] {
  const checks = reportChecks(readiness, verification, preferVerificationSnapshot)
  const invalidated = checks.some(check => invalidResultIds.has(check.id) && check.status !== 'ok')
  const publicationMissing = checks.some(check => publicationIds.has(check.id) && check.status !== 'ok')
  const running = verification?.status === 'running' && !invalidated
  const accepted = !!verification && !running && !invalidated && !publicationMissing
  const passed = (ids: string[]) => accepted && ids.every(id => checks.some(check => check.id === id && check.status === 'ok'))
  const nodeComplete = passed(nodeRequired)
  const clientComplete = passed(clientRequired)
  const hasActualIPv6 = nodeComplete || clientComplete

  // The problems of 「IPv6 网络」 that decide its status, kept for besideSteps.
  let networkProblems: IPv6SummaryCheck[] = []
  let networkIssues: Array<{ check: IPv6SummaryCheck; issue: string }> = []
  const groups = definitions.map(definition => {
    const rows = checks.filter(check => groupFor(check) === definition.id)
    const group: IPv6SummaryGroup = { ...definition, status: '待验证', tone: 'neutral', issues: [], checks: rows }
    const notConfigured = definition.id === 'client' && rows.some(check => check.id === 'client_not_configured' && check.status === 'unknown')
    const problems = rows.filter(check => {
      if (check.status === 'ok' || (check.id === 'client_not_configured' && check.status === 'unknown')) return false
      // A static trace is troubleshooting evidence, not a connectivity verdict.
      // Keep definite blockers, but don't present all unsupported/unknown paths.
      if (check.id.startsWith('router_')) return check.status === 'error'
      if (definition.id === 'ipv6-network') {
        if (publicationIds.has(check.id)) return true
        if (hasActualIPv6 && ['host_address', 'host_default_route'].includes(check.id) && check.status !== 'error') return false
        return check.status === 'error' || check.status === 'warning'
      }
      return true
    }).sort((left, right) => rank[left.status] - rank[right.status])
    const trafficPassed = definition.id === 'ipv6-network' ? hasActualIPv6
      : definition.id === 'node' ? nodeComplete : clientComplete
    const nonRouterProblems = problems.filter(check => !check.id.startsWith('router_'))
    // A candidate-packet rule analysis cannot refute the observed request. Keep
    // its configuration concern visible, but don't label real traffic failed.
    // Current host/runtime failures still take priority over passing observations.
    const advisoryOnly = trafficPassed && !nonRouterProblems.some(check => check.status === 'error')
      && (definition.id === 'ipv6-network' || !nonRouterProblems.length)
    const staticAdvisories = advisoryOnly ? problems.filter(check => check.id.startsWith('router_')) : []
    const recoveryAdvisories = definition.id === 'ipv6-network' && hasActualIPv6
      ? problems.filter(check => recoveryIds.has(check.id) && check.status === 'warning') : []
    const actionable = problems.filter(check => !staticAdvisories.includes(check) && !recoveryAdvisories.includes(check))
    const canPrepare = readiness?.can_prepare === true && !preferVerificationSnapshot
    group.issues = [...new Set([...actionable, ...recoveryAdvisories].map(check => issueFor(check, canPrepare)))]
    if (definition.id === 'ipv6-network') {
      networkProblems = actionable
      networkIssues = [...actionable, ...recoveryAdvisories].map(check => ({ check, issue: issueFor(check, canPrepare) }))
    }

    if (actionable.some(check => check.status === 'error')) {
      group.status = '未通过'; group.tone = 'danger'
    } else if (actionable.some(check => check.status === 'warning')) {
      group.status = '需处理'; group.tone = 'warning'
    } else if (actionable.length) {
      group.status = publicationMissing && definition.id === 'ipv6-network' ? '待发布' : '待验证'
    } else if (definition.id === 'ipv6-network') {
      if (hasActualIPv6) {
        group.status = '可用'; group.tone = 'success'
      } else if (['host_address', 'host_default_route'].every(id => rows.some(check => check.id === id && check.status === 'ok'))) {
        group.status = '已就绪'; group.tone = 'success'
      } else {
        group.status = '待检查'
        group.note = '打开「检查设备」，点击「重新检查」确认 IPv6 地址和出口。'
      }
    } else if (notConfigured) {
      if (rows.some(check => check.id.startsWith('client_') && check.id !== 'client_not_configured')) {
        group.issues = ['检测客户端的设置与结果不一致，请在「验收设置」重新确认客户端后验证。']
      } else {
        group.status = '未配置检测客户端'
        group.note = clientSetup
      }
    } else if (running && (definition.id === 'node' || verification?.scope === 'client_path')) {
      group.status = '验证中'; group.tone = 'progress'
    } else if (definition.id === 'node' ? nodeComplete : clientComplete) {
      group.status = '已通过'; group.tone = 'success'
    }

    if ((definition.id === 'node' || definition.id === 'client') && group.tone === 'neutral' && !notConfigured && !group.issues.length) {
      group.note = publicationMissing ? publishAction : invalidated
        ? '请重新检查设备，再点击「验证连接与分流」。' : verifyAction
    }
    for (const check of staticAdvisories) {
      const message = /_(?:fake[46]_|fake_ipv6)/.test(check.id)
        ? '本次连接已通过；分流路由检查仍有差异，请到「上游路由」核对。'
        : '本次连接已通过；路由器规则检查仍有差异，请到「上游路由」核对。'
      if (!group.issues.includes(message)) group.issues.push(message)
    }
    // Components display either issues or the note. An unknown result with its
    // own explanation must therefore carry the next action in the issue list.
    if ((group.status === '待验证' || group.status === '待发布') && group.issues.length) {
      const instructionPresent = group.issues.some(issue => issue.includes('验证连接与分流')
        && (!publicationMissing || issue.includes('保存并发布')))
      if (!instructionPresent) group.issues.push(publicationMissing ? publishAction : verifyAction)
    }
    return group
  })
  if (!besideSteps) return groups
  const [, node, client] = groups
  // 「保存并发布 Mihomo」 waits for a publish: verifying first would test the previous files.
  if (readiness?.publication === 'changed') {
    for (const group of [node, client]) if (group.note === verifyAction) group.note = publishAction
  }
  const extra = networkProblems.filter(check => !coveredBySteps(check.id))
  const worst: SummaryTone | null = extra.some(check => check.status === 'error') ? 'danger'
    : extra.some(check => check.status === 'warning') ? 'warning' : null
  if (worst && toneRank[worst] < toneRank[node.tone]) {
    node.status = worst === 'danger' ? '未通过' : '需处理'
    node.tone = worst
  }
  const moved = networkIssues.filter(item => !coveredBySteps(item.check.id)).map(item => item.issue)
  if (moved.length) {
    // The rows show either the issues or the note, so the next action joins the issues.
    const action = node.tone === 'neutral' && !node.issues.length && !moved.some(issue => issue.includes('验证连接与分流')) ? node.note : undefined
    node.issues = [...new Set([...moved, ...node.issues, ...(action ? [action] : [])])]
    delete node.note
  }
  return [node, client]
}
