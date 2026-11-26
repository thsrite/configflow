import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RuleDownloadRemedy from '@/components/agents/RuleDownloadRemedy.vue'
import { githubProxyDomainApi, ruleFetchProxyApi } from '@/api'

vi.mock('@/api', () => ({
  githubProxyDomainApi: { get: vi.fn(), update: vi.fn() },
  ruleFetchProxyApi: { get: vi.fn(), update: vi.fn() },
}))
const response = (data: unknown) => ({ data }) as any

const wrappers: ReturnType<typeof mount>[] = []
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  vi.mocked(githubProxyDomainApi.get).mockResolvedValue(response({ proxy_domains: '' }))
  vi.mocked(ruleFetchProxyApi.get).mockResolvedValue(response({ rule_fetch_proxy: 'http://192.0.2.2:7890' }))
  vi.mocked(githubProxyDomainApi.update).mockImplementation(async value => response({ success: true, proxy_domains: value }))
  vi.mocked(ruleFetchProxyApi.update).mockImplementation(async data => response({ success: true, ...data }))
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

async function render(github = true) {
  const wrapper = mount(RuleDownloadRemedy, { props: { github }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const saveButton = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('button').find(button => button.text().includes('保存并重试发布'))!

describe('RuleDownloadRemedy', () => {
  it('saves only the changed setting, then asks to retry', async () => {
    const wrapper = await render()
    expect((wrapper.get('#remedy-rule-proxy').element as HTMLInputElement).value).toBe('http://192.0.2.2:7890')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    await wrapper.get('#remedy-github-proxy').setValue(' https://mirror.example.com ')
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(githubProxyDomainApi.update).toHaveBeenCalledWith('https://mirror.example.com')
    expect(ruleFetchProxyApi.update).not.toHaveBeenCalled()
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('keeps the form and does not retry when the proxy is rejected', async () => {
    vi.mocked(ruleFetchProxyApi.update).mockRejectedValue(new Error('400'))
    const wrapper = await render(false)
    expect(wrapper.find('#remedy-github-proxy').exists()).toBe(false)
    await wrapper.get('#remedy-rule-proxy').setValue('socks5://bad')
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('规则下载代理无效')
    expect(wrapper.emitted('retry')).toBeUndefined()
  })

  it('does not allow saving when current settings cannot be read', async () => {
    vi.mocked(ruleFetchProxyApi.get).mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.text()).toContain('读取当前下载设置失败')
    expect(wrapper.find('#remedy-rule-proxy').exists()).toBe(false)
  })
})
