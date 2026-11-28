import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MihomoIPv6VerificationResult from '@/components/generate/MihomoIPv6VerificationResult.vue'
import type { IPv6Verification } from '@/types/mihomoIPv6'

const report = (extra: Partial<IPv6Verification> = {}): IPv6Verification => ({
  status: 'ok', config_revision: 7, checked_at: '2026-10-30T12:00:00Z', scope: 'client_path',
  checks: [
    { id: 'host_address', label: '主机地址', status: 'ok', message: '地址已就绪。' },
    { id: 'host_persistence', label: '开机恢复', status: 'ok', message: '恢复配置已保存。' },
    { id: 'runtime_dns_a', label: 'DNS 应答', status: 'ok', message: '已取得 IPv4 答案。' },
    ...['client_http_ipv4', 'client_mihomo_path', 'client_ros_path', 'client_ipv6_transport'].map(id => ({ id, label: id, status: 'ok' as const, message: '本次客户端证据已取得。' })),
    { id: 'runtime_proxy_http', label: '代理网站访问', status: 'ok', message: '已通过代理访问检测网站。' },
    { id: 'runtime_node_ipv6_transport', label: '节点 IPv6 连接', status: 'ok', message: '已确认本次 IPv6 连接。' }], ...extra,
})

afterEach(() => { document.body.innerHTML = '' })

describe('IPv6 verification result', () => {
  it.each([false, true])('shows the two direct results without raw rule details or another summary layer (compact=%s)', compact => {
    const wrapper = mount(MihomoIPv6VerificationResult, { attachTo: document.body, props: { compact, verification: report({
      status: 'warning', checks: [...report().checks,
        { id: 'router_ros_firewall', label: '防火墙路径检查', status: 'unknown', message: '静态规则未确认', details: ['raw/prerouting 未命中终止规则', 'protocol=tcp；dst-port=1080'] },
      ],
    }) } })
    expect(wrapper.findAll('[data-ipv6-check-group]').map(row => row.attributes('data-ipv6-check-group'))).toEqual(['ipv6-network', 'client'])
    expect(wrapper.findAll('[data-agent-status-row]')).toHaveLength(2)
    expect(wrapper.findAll('[data-status-details], [data-ipv6-check-id], button')).toHaveLength(0)
    expect(wrapper.text()).not.toMatch(/raw\/prerouting|protocol=tcp|防火墙路径检查|主机自检|客户端全链路验收|展开查看/)
    expect(wrapper.findAll('[data-agent-status-row]').every(row => row.attributes('data-tone') === 'success')).toBe(true)
    expect(wrapper.text()).toContain('验证时间')
    wrapper.unmount()
  })

  it('leaves empty results unverified and tells the user how to verify', () => {
    const wrapper = mount(MihomoIPv6VerificationResult, { props: { verification: report({ checks: [] }) } })
    expect(wrapper.findAll('[data-agent-status-row]').some(row => row.attributes('data-tone') === 'success')).toBe(false)
    expect(wrapper.get('[data-ipv6-check-group="client"]').text()).toContain('验证连接与分流')
    wrapper.unmount()
  })

  it('leaves out the client result without a test client and ignores an older node test', () => {
    const wrapper = mount(MihomoIPv6VerificationResult, { props: { verification: report({ scope: 'mihomo_runtime', checks: [
      ...report().checks.filter(check => !check.id.startsWith('client_')),
      { id: 'client_not_configured', label: '客户端全链路', status: 'unknown', message: '尚未配置独立检测客户端。' },
    ] }) } })
    expect(wrapper.findAll('[data-ipv6-check-group]').map(row => row.attributes('data-ipv6-check-group'))).toEqual(['ipv6-network'])
    expect(wrapper.text()).not.toMatch(/节点|未配置检测客户端/)
    wrapper.unmount()
  })

  it('shows actual failures directly but never renders their protocol trace', () => {
    const wrapper = mount(MihomoIPv6VerificationResult, { props: { verification: report({ status: 'error', checks: [
      ...report().checks.filter(check => check.id !== 'client_http_ipv4'),
      { id: 'client_http_ipv4', label: '客户端访问', status: 'error', message: '客户端访问检测网站失败，请检查客户端网络。', details: ['mangle/prerouting 第1条', 'src-address=192.0.2.100'] },
    ] }) } })
    const client = wrapper.get('[data-ipv6-check-group="client"]')
    expect(client.get('[data-agent-status-row]').attributes('data-tone')).toBe('danger')
    expect(client.get('[data-status-notice]').text()).toMatch(/失败|不通|未通过/)
    expect(wrapper.text()).not.toMatch(/mangle\/prerouting|src-address=/)
    wrapper.unmount()
  })

  it('explains a missing publication using the existing save-and-publish action', () => {
    const wrapper = mount(MihomoIPv6VerificationResult, { props: { verification: report({ checks: [
      { id: 'runtime_deployed_config', label: '已发布配置', status: 'unknown', message: '未提供文件指纹' },
    ] }) } })
    expect(wrapper.text()).toContain('保存并发布')
    expect(wrapper.text()).not.toContain('未提供文件指纹')
    wrapper.unmount()
  })
})
