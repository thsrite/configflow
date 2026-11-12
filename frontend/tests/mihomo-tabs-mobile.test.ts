import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoRules from '@/components/mihomo/MihomoRules.vue'
import MihomoProxies from '@/components/mihomo/MihomoProxies.vue'
import { mihomoPanelApi } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({
  mihomoPanelApi: {
    rules: vi.fn(), ruleProviders: vi.fn(), updateRuleProvider: vi.fn(),
    proxies: vi.fn(), proxyProviders: vi.fn(), selectProxy: vi.fn(), connections: vi.fn(), closeConnection: vi.fn(),
    updateProxyProvider: vi.fn(), healthcheckProxyProvider: vi.fn(), groupDelay: vi.fn(), proxyDelay: vi.fn(), providerProxyDelay: vi.fn()
  }
}))
vi.mock('@/lib/feedback', () => ({ notify: { info: vi.fn(), success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))

const reply = (data: unknown) => ({ data: { success: true, data } }) as any

/** 手机宽度：只有 < 640px 的查询命中 */
function stubPhone() {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: /max-width:\s*(639|767)px|width < 640px/.test(String(query)),
    media: query, onchange: null,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false
  }))
}

/** 去掉空白后的 scoped 样式，确认手机布局写在 < 640px 的媒体查询里 */
function phoneCss(file: string) {
  const source = readFileSync(resolve(__dirname, '../src/components/mihomo', file), 'utf8').replace(/\s+/g, ' ')
  const style = source.slice(source.indexOf('<style scoped>'))
  return style.slice(style.search(/@media \((max-width: 639px|width < 640px)\)/))
}

const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.unstubAllGlobals() })
beforeEach(() => {
  vi.clearAllMocks()
  stubPhone()
  vi.mocked(mihomoPanelApi.rules).mockResolvedValue(reply({ rules: [
    { type: 'DomainSuffix', payload: 'example.com', proxy: 'Proxy' },
    { type: 'RuleSet', payload: 'ads', proxy: 'REJECT', size: 12 },
    { type: 'Match', payload: '', proxy: 'DIRECT' }
  ] }))
  vi.mocked(mihomoPanelApi.ruleProviders).mockResolvedValue(reply({ providers: {
    ads: { name: 'ads', type: 'Rule', vehicleType: 'HTTP', behavior: 'Domain', format: 'MrsRule', ruleCount: 12 },
    local: { name: 'local', type: 'Rule', vehicleType: 'File', behavior: 'IPCIDR', format: 'YamlRule', ruleCount: 3 }
  } }))
  vi.mocked(mihomoPanelApi.updateRuleProvider).mockResolvedValue(reply({}))
  vi.mocked(mihomoPanelApi.proxies).mockResolvedValue(reply({ proxies: {
    GLOBAL: { name: 'GLOBAL', type: 'Selector', all: ['Proxy'], now: 'Proxy' },
    Proxy: { name: 'Proxy', type: 'Selector', all: ['HK', 'SH'], now: 'HK' },
    HK: { name: 'HK', type: 'Shadowsocks', history: [{ time: '', delay: 88 }] }
  } }))
  vi.mocked(mihomoPanelApi.proxyProviders).mockResolvedValue(reply({ providers: {
    sub: { name: 'sub', type: 'Proxy', vehicleType: 'HTTP', proxies: [{ name: 'SH', type: 'Trojan', history: [{ time: '', delay: 42 }] }],
      subscriptionInfo: { Upload: 1024, Download: 1024, Total: 1024 * 1024 } },
    file: { name: 'file', type: 'Proxy', vehicleType: 'File', proxies: [] }
  } }))
  vi.mocked(mihomoPanelApi.selectProxy).mockResolvedValue(reply({}))
  vi.mocked(mihomoPanelApi.updateProxyProvider).mockResolvedValue(reply({}))
  vi.mocked(mihomoPanelApi.healthcheckProxyProvider).mockResolvedValue(reply({}))
})

async function render(component: typeof MihomoRules | typeof MihomoProxies) {
  const wrapper = mount(component as any, { props: { agentId: 'gw', reloadToken: 0 } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('Mihomo 规则页手机布局', () => {
  it('规则集合与规则用紧凑行，类型和条数挪到次行', async () => {
    const wrapper = await render(MihomoRules)
    const providerRows = wrapper.findAll('.rule-provider-rows tbody tr')
    expect(providerRows).toHaveLength(2)
    expect(providerRows[0].findAll('td').map(td => td.attributes('data-cell'))).toEqual(['name', 'meta', 'meta', 'meta', 'actions'])
    expect(providerRows[0].get('[data-cell="name"]').attributes('title')).toBe('ads')
    // 格式与来源只留给读屏，规则数带单位
    expect(providerRows[0].get('[class~="max-sm:sr-only"]').text()).toContain('MrsRule')
    expect(providerRows[0].text()).toContain('12 条')
    // 非 HTTP 集合没有更新按钮，操作格为空
    expect(providerRows[1].find('[data-cell="actions"] button').exists()).toBe(false)

    const rows = wrapper.findAll('.mihomo-rule-rows tbody tr')
    expect(rows).toHaveLength(3)
    const ruleSet = rows[1]
    expect(ruleSet.get('[data-cell="index"]').text()).toBe('2')
    expect(ruleSet.get('[data-cell="payload"]').attributes('title')).toBe('ads')
    expect(ruleSet.get('[data-cell="type"]').text()).toContain('RuleSet')
    expect(ruleSet.get('[data-cell="type"]').text()).toContain('12 条')
    expect(ruleSet.get('.mihomo-rule-proxy').text()).toBe('REJECT')
    expect(wrapper.get('.mihomo-rule-rows').classes()).toContain('max-sm:min-w-0')
  })

  it('图标更新按钮仍可更新规则集合，搜索照常筛选', async () => {
    const wrapper = await render(MihomoRules)
    const button = wrapper.get('.rule-provider-rows [data-cell="actions"] button')
    expect(button.classes()).toContain('mihomo-row-action')
    expect(button.text()).toBe('更新')
    await button.trigger('click'); await flushPromises()
    expect(mihomoPanelApi.updateRuleProvider).toHaveBeenCalledWith('gw', 'ads')
    expect(notify.success).toHaveBeenCalledWith('ads 已更新')

    await wrapper.get('input').setValue('example')
    expect(wrapper.findAll('.mihomo-rule-rows tbody tr')).toHaveLength(1)
    expect(wrapper.text()).toContain('1 / 3 条')
  })

  it('手机样式把表格行改成网格，按钮不被 44px 最小尺寸撑高', () => {
    const css = phoneCss('MihomoRules.vue')
    expect(css).toMatch(/\.rule-provider-rows tbody tr \{ display: grid;/)
    expect(css).toMatch(/\.mihomo-rule-rows tbody tr \{ display: grid;/)
    expect(css).toMatch(/grid-template-areas: 'index payload proxy' 'index type proxy';/)
    expect(css).toMatch(/\.mihomo-row-action \{ position: relative; width: 28px; height: 28px; min-width: 0; min-height: 0;/)
    expect(css).toMatch(/\.mihomo-row-action::before \{ content: ''; position: absolute; inset: -8px; \}/)
  })
})

describe('Mihomo 策略页手机布局', () => {
  it('代理集合用两行紧凑行，健康检查和更新仍可用', async () => {
    const wrapper = await render(MihomoProxies)
    const rows = wrapper.findAll('.proxy-provider-rows tbody tr')
    expect(rows).toHaveLength(2)
    const [sub, file] = rows
    expect(sub.findAll('td').map(td => td.attributes('data-cell'))).toEqual(['name', 'count', 'usage', 'updated', 'actions'])
    expect(sub.get('[data-cell="count"]').text()).toBe('1 个节点')
    expect(sub.get('[data-cell="usage"]').attributes('data-empty')).toBeUndefined()
    expect(file.get('[data-cell="usage"]').attributes('data-empty')).toBe('true')
    expect(file.findAll('[data-cell="actions"] button')).toHaveLength(1)

    const [check, update] = sub.findAll('[data-cell="actions"] button')
    expect(check.classes()).toContain('mihomo-row-action')
    expect(check.text()).toBe('健康检查')
    await check.trigger('click'); await flushPromises()
    expect(mihomoPanelApi.healthcheckProxyProvider).toHaveBeenCalledWith('gw', 'sub')
    await update.trigger('click'); await flushPromises()
    expect(mihomoPanelApi.updateProxyProvider).toHaveBeenCalledWith('gw', 'sub')
  })

  it('展开分组后仍可选择节点，当前节点保持标记', async () => {
    const wrapper = await render(MihomoProxies)
    await wrapper.get('button[aria-expanded="false"]').trigger('click')
    const card = (name: string) => wrapper.findAll('button[aria-pressed]').find(button => button.find('span').text() === name)!
    expect(card('HK').attributes('aria-pressed')).toBe('true')
    await card('SH').trigger('click'); await flushPromises()
    expect(mihomoPanelApi.selectProxy).toHaveBeenCalledWith('gw', 'Proxy', 'SH')
    expect(card('SH').attributes('aria-pressed')).toBe('true')
  })

  it('手机样式让代理集合不再横向滚动', () => {
    const css = phoneCss('MihomoProxies.vue')
    expect(css).toMatch(/\.proxy-provider-rows tbody tr \{ display: grid;/)
    expect(css).toMatch(/grid-template-areas: 'name name name name actions' 'count usage updated \. actions';/)
    expect(css).toMatch(/\.mihomo-row-action \{ position: relative; width: 28px; height: 28px; min-width: 0; min-height: 0;/)
  })
})
