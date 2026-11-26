import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentDeploymentStatus from '@/components/agents/AgentDeploymentStatus.vue'
import type { AgentHAPublication } from '@/types/agentHAPublication'
import type { AgentDeployment } from '@/composables/useAgentDeployments'

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
const publication: AgentHAPublication = {
  id: 'pair-handoff', status: 'checking', phase: 'handoff', owner_agent_id: 'original-holder',
  owner_deployment_id: 'holder-release', active_agent_id: 'new-holder',
  members: [
    { agent_id: 'new-holder', name: '接管设备', deployment_id: 'standby-release', status: 'succeeded' },
    { agent_id: 'original-holder', name: '原持有设备', deployment_id: 'holder-release', status: 'waiting' },
  ],
}

describe('planned HA handoff progress across publication surfaces', () => {
  it.each([false, true])('keeps the handoff visible after the first device succeeds (compact=%s)', compact => {
    const task: AgentDeployment = { deployment_id: 'holder-release', status: 'checking', ha_publication: publication }
    const wrapper = mount(AgentDeploymentStatus, { props: { task, compact } })
    wrappers.push(wrapper)
    expect(wrapper.get('[data-ha-publication]').text()).toContain('迁移虚拟 IP 并确认解析')
    expect(wrapper.get('[data-ha-publication]').text()).toContain('接管设备：发布成功')
    expect(wrapper.get('[data-ha-publication]').text()).toContain('原持有设备：等待前一台完成')
    expect(wrapper.text()).toContain('正在迁移虚拟 IP')
    if (compact) {
      expect(wrapper.get('[data-agent-status-row]').attributes('data-tone')).toBe('progress')
      expect(wrapper.get('[data-status-notice] [data-ha-publication]').exists()).toBe(true)
    }
  })

  it('shows the server handoff failure without masking the stopped second device', () => {
    const task: AgentDeployment = { deployment_id: 'holder-release', status: 'failed', ha_publication: {
      ...publication, status: 'failed', message: '虚拟 IP 解析检查失败，原持有设备未更新',
      members: [publication.members[0], { ...publication.members[1], status: 'skipped' }],
    } }
    const wrapper = mount(AgentDeploymentStatus, { props: { task, compact: true } })
    wrappers.push(wrapper)
    expect(wrapper.get('[data-agent-status-row]').attributes('data-tone')).toBe('danger')
    expect(wrapper.get('[data-status-notice]').text()).toContain('虚拟 IP 解析检查失败')
    expect(wrapper.get('[data-status-notice]').text()).toContain('原持有设备：未执行')
  })
})

describe('automatic completion after a lost member returns', () => {
  const stopped: AgentHAPublication = {
    ...publication, status: 'failed',
    message: '主备设备连接中断超过 10 分钟，本次主备发布已停止；设备恢复后会自动完成本次主备发布。设备无法恢复时可以强制停用高可用',
    auto_complete: { status: 'waiting', message: '等待设备恢复后自动完成发布' },
  }
  it.each(['waiting', 'running'] as const)('leaves the %s state to the status row and keeps the stop message', state => {
    const task: AgentDeployment = { deployment_id: 'holder-release', status: 'failed', ha_publication: { ...stopped, auto_complete: { status: state, message: '等待设备恢复后自动完成发布' } } }
    const wrapper = mount(AgentDeploymentStatus, { props: { task } })
    wrappers.push(wrapper)
    expect(wrapper.get('[data-ha-publication]').text()).toContain('设备恢复后会自动完成本次主备发布')
    expect(wrapper.find('[data-ha-auto-complete]').exists()).toBe(false)
    expect(wrapper.find('[data-ha-auto-attempt]').exists()).toBe(false)
  })

  it('names the reason when the stopped pair cannot be completed automatically', () => {
    const message = '无法自动完成本次主备发布：本次发布的内容已无法读取；请手动完整发布一次主备配置'
    const task: AgentDeployment = { deployment_id: 'holder-release', status: 'failed', ha_publication: { ...stopped, auto_complete: { status: 'blocked', message } } }
    const wrapper = mount(AgentDeploymentStatus, { props: { task } })
    wrappers.push(wrapper)
    const line = wrapper.get('[data-ha-auto-complete]')
    expect(line.text()).toBe(message)
    expect(line.classes()).toContain('text-destructive-accent')
  })

  it('marks the completing publication in the history and shows nothing for an ordinary one', () => {
    const attempt: AgentDeployment = { deployment_id: 'auto-release', status: 'succeeded',
      ha_publication: { ...publication, status: 'succeeded', phase: 'done', message: '两台均已发布成功', auto_complete: { status: '', message: '自动补完', attempt: true } } }
    const wrapper = mount(AgentDeploymentStatus, { props: { task: attempt } })
    wrappers.push(wrapper)
    expect(wrapper.get('[data-ha-auto-attempt]').text()).toBe('自动补完')
    expect(wrapper.find('[data-ha-auto-complete]').exists()).toBe(false)
    const plain = mount(AgentDeploymentStatus, { props: { task: { deployment_id: 'holder-release', status: 'failed', ha_publication: { ...publication, status: 'failed' } } } })
    wrappers.push(plain)
    expect(plain.find('[data-ha-auto-attempt]').exists()).toBe(false)
    expect(plain.find('[data-ha-auto-complete]').exists()).toBe(false)
  })
})
