import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentDeploymentHistory from '@/components/agents/AgentDeploymentHistory.vue'
import type { Agent } from '@/types'
import { ruleLocationDetails } from './fixtures/agent-failure'

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = '' })
const old = { deployment_id: 'old-success', status: 'succeeded', updated_at: '2026-10-15T08:00:00Z' }
const latest = { deployment_id: 'current-success', status: 'succeeded', updated_at: '2026-10-16T08:00:00Z' }
const agent = { id: 'oxi', name: 'OxiDNS test', status: 'online', service_type: 'oxidns', latest_deployment: latest, deployments: { old, latest } } as unknown as Agent
async function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(AgentDeploymentHistory, { attachTo: document.body, props: { agent, busy: false, blocked: false, ...props } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('Agent deployment history rollback availability', () => {
  it('offers only the latest successful record and identifies the rollback scope', async () => {
    const wrapper = await render()
    const buttons = [...document.querySelectorAll('button')].filter(button => button.textContent === '回滚到发布前')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].closest('tr')?.textContent).toContain('current-success')
    expect(document.body.textContent).toContain('state/ 运行数据保留')
    buttons[0].click()
    expect(wrapper.emitted('rollback')?.[0]).toEqual([latest])
  })
  it.each([{ busy: true }, { blocked: true }, { agent: { ...agent, status: 'offline' } }])('disables rollback when unavailable: %j', async props => {
    await render(props)
    const button = [...document.querySelectorAll('button')].find(button => button.textContent === '回滚到发布前')!
    expect(button.disabled).toBe(true)
  })
  it('shows completed manual rollback as success without offering it again', async () => {
    await render({ progress: { ...latest, status: 'rolled_back', manual_rollback: true } })
    const status = document.querySelector('[data-agent-status-row="配置回滚"]')!
    expect(status.getAttribute('data-tone')).toBe('success')
    expect(status.querySelector('button')?.textContent).toContain('成功')
    expect(document.body.textContent).not.toContain('回滚到发布前')
    expect(document.body.textContent).not.toContain('发布失败，已回滚')
  })
})

describe('Agent deployment history diagnostics', () => {
  it('focuses the requested historical failure and expands its diagnostic details', async () => {
    const failure = { deployment_id: 'failed-id', status: 'failed', error: '校验失败', error_details: '具体失败位置', updated_at: '2026-10-15T09:00:00Z' }
    await render({ agent: { ...agent, deployments: { ...agent.deployments, failure } }, focusDeployment: 'failed-id' })
    const target = document.querySelector('[data-deployment-record="failed-id"]')!
    expect(target.hasAttribute('data-notification-focus')).toBe(true)
    expect(target.querySelector('details')?.open).toBe(true)
    expect(target.textContent).toContain('具体失败位置')
  })
  it('reopens the recorded operation steps and their specific work items', async () => {
    const events = [{ at: '2026-10-18T12:00:00Z', status: 'uploading', message: '上传配置和 3 个规则文件' },
      { at: '2026-10-18T12:00:01Z', status: 'succeeded', message: 'OxiDNS 配置已应用并通过 DNS 检查' }]
    await render({ agent: { ...agent, latest_deployment: { ...latest, events }, deployments: {} } })
    const row = document.querySelector('[data-agent-status-row="配置推送"]')!
    const details = row.querySelector<HTMLElement>('[data-status-details]')!
    expect(details.style.display).toBe('none')
    expect(details.textContent).toContain('上传配置和 3 个规则文件')
    expect(details.querySelectorAll('li')).toHaveLength(2)
    expect(details.querySelector('details')).toBeNull()
    row.querySelector<HTMLButtonElement>('button')!.click()
    await flushPromises()
    expect(details.style.display).not.toBe('none')
    expect(details.textContent).toContain('OxiDNS 配置已应用并通过 DNS 检查')
  })
  it('keeps DNS warnings and their fixes visible in successful release history', async () => {
    await render({ progress: { ...latest, dns_checks: [{ name: 'DNS 劫持', level: 'warn', message: 'any:53 可能绕过 OxiDNS', fix: '检查劫持规则' }] } })
    const report = document.querySelector('[aria-label="DNS 检查结果"]')!
    expect(report.querySelector('[data-status-notice]')?.textContent).toContain('警告 · DNS 劫持')
    const details = report.querySelector<HTMLElement>('[data-status-details]')!
    expect(details.style.display).toBe('none')
    report.querySelector<HTMLButtonElement>('button')!.click()
    await flushPromises()
    expect(details.style.display).not.toBe('none')
    expect(details.textContent).toContain('修复建议：检查劫持规则')
  })
  it('retains record identity and revision while an unconfirmed prior success stays a visible warning', async () => {
    await render({ progress: { ...latest, config_revision: 42, query_error: true, message: '最近已完成服务检查' } })
    const row = [...document.querySelectorAll('tbody tr')].find(row => row.textContent?.includes(latest.deployment_id))!
    expect(row.textContent).toContain('修订 42')
    expect(row.textContent).toContain(new Date(latest.updated_at).toLocaleString())
    expect(row.textContent).toContain('最新记录')
    const status = row.querySelector('[data-agent-status-row="配置推送"]')!
    expect(status.getAttribute('data-tone')).toBe('warning')
    expect(status.querySelector('button')?.textContent).toContain('待确认')
    expect(status.querySelector('[data-status-notice]')?.textContent).toContain('暂时无法取得最新状态')
    expect(status.querySelector<HTMLElement>('[data-status-details]')?.style.display).toBe('none')
  })
  it('renders persisted publication and rollback diagnostics independently', async () => {
    await render({ progress: { ...latest, status: 'rollback_failed', error: '配置校验失败', failed_stage: 'verifying', error_details: ruleLocationDetails, rollback_error: '服务恢复失败', rollback_error_details: 'failed to bind :53' } })
    expect(document.body.textContent).toContain('失败步骤：校验中')
    expect([...document.querySelectorAll('pre')].map(element => element.textContent)).toEqual([ruleLocationDetails, 'failed to bind :53'])
    expect([...document.querySelectorAll('details')].every(element => !element.open)).toBe(true)
  })
  it('shows rule locations from a saved record after reopening the page', async () => {
    const failed = { ...latest, status: 'failed', error: '规则集内容无效', error_details: ruleLocationDetails }
    await render({ agent: { ...agent, latest_deployment: failed, deployments: { latest: failed } } })
    expect(document.querySelector('pre')?.textContent).toBe(ruleLocationDetails)
  })
})
