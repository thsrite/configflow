import type { GatewayInstallation, GatewayOnboardingSettings } from '@/api/gatewayOnboarding'
import type { LinuxGatewaySpec } from '@/api/linuxGateway'
import { gatewaySpecErrors, cleanGatewaySpec } from './linuxGateway'

export function gatewayEnvironmentErrors(name: string, serverUrl: string, spec: LinuxGatewaySpec): string[] {
  const errors = gatewaySpecErrors(spec)
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$/.test(name.trim())) errors.unshift('设备名称使用 1–48 个字母、数字、短横线或下划线。')
  try {
    const url = new URL(serverUrl.trim())
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash
      || (url.pathname !== '/' && url.pathname !== '') || ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)) throw new Error()
  } catch { errors.push('填写目标 Linux 能访问的 ConfigFlow 服务地址，例如 http://192.0.2.10:5010，不带路径或登录凭据。') }
  return errors
}
export const onboardingSettingsKey = (settings: GatewayOnboardingSettings) => JSON.stringify({
  rule_set_ids: [...new Set(settings.rule_set_ids)].sort(),
  direct_rule_set_ids: [...new Set(settings.direct_rule_set_ids)].sort(),
  proxy_rule_set_ids: [...new Set(settings.proxy_rule_set_ids)].sort(),
  default_forward: settings.default_forward || 'forward_remote',
  local_dns: settings.local_dns.trim(), query_recorder: settings.query_recorder === true,
  confirm_template_conversion: settings.confirm_template_conversion === true,
})
export function installationHostMatches(session: GatewayInstallation | null, profileId: string): boolean {
  const agent = session?.agents?.mihomo
  return !!session?.id && session.profile_id === profileId && !!agent?.id && agent.profile_id === profileId
    && agent.service_type === 'mihomo' && agent.host === session.network?.address
}
export function installationMatches(session: GatewayInstallation, profileId: string): boolean {
  if (!session?.id || session.profile_id !== profileId || !session.network || !session.agents || !session.deployments) return false
  if (!['waiting', 'enrolled', 'publishing', 'publish_failed', 'published'].includes(session.phase)) return false
  if (session.phase !== 'waiting' && (!session.agents.mihomo?.id || !session.agents.oxidns?.id
    || session.agents.mihomo.id === session.agents.oxidns.id)) return false
  return Object.entries(session.agents).every(([service, agent]) => agent?.id && agent.profile_id === profileId
    && agent.service_type === service && agent.host === cleanGatewaySpec(session.network).address)
}
export const installationPhaseLabel = (phase: GatewayInstallation['phase']) => ({
  waiting: '等待安装与登记', enrolled: '两项服务已登记', publishing: '正在依次发布配置',
  publish_failed: '发布暂停，需要处理', published: '两项服务健康检查已通过',
})[phase] || '等待确认状态'
