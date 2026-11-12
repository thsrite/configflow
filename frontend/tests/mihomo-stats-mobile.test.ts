import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import MihomoConnectionStats from '@/components/mihomo/MihomoConnectionStats.vue'
import { mihomoPanelApi, trafficHistoryApi } from '@/api'
import { confirmDanger } from '@/lib/feedback'
import type { MihomoConnection } from '@/types/mihomo'

vi.mock('@/api', () => ({
  mihomoPanelApi: { proxies: vi.fn() },
  trafficHistoryApi: { get: vi.fn(), setEnabled: vi.fn(), clear: vi.fn(), setRetention: vi.fn() }
}))
vi.mock('@/lib/feedback', () => ({ choose: vi.fn(), confirmDanger: vi.fn(), notify: { success: vi.fn(), error: vi.fn() } }))

/** 手机宽度：只有 < 640px（以及 < 768px）的查询命中 */
function stubViewport(phone: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: phone && /max-width:\s*(639|767)px|width < 640px/.test(String(query)),
    media: query, onchange: null,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false
  }))
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
}

/** 去掉空白后的 scoped 样式中 < 640px 的部分 */
function phoneCss() {
  const source = readFileSync(resolve(__dirname, '../src/components/mihomo/MihomoConnectionStats.vue'), 'utf8').replace(/\s+/g, ' ')
  const style = source.slice(source.indexOf('<style scoped>'))
  return style.slice(style.search(/@media \(max-width: 639px\)/))
}

const connection = (id: string, source: string, down: number, chains: string[]): MihomoConnection => ({
  id, upload: 10, download: down, start: '2026-11-01T00:00:00Z', chains, rule: 'Match', rulePayload: '',
  metadata: { network: 'tcp', type: 'Tun', sourceIP: source, sourcePort: '50000', destinationIP: '', destinationPort: '443', host: `${id}.example.com` }
})
const state = (extra: Record<string, unknown> = {}) => ({ data: { success: true, data: {
  enabled: true, since: '2026-11-01T05:46:00+00:00', supported: true, min_version: '1.8.51-go', retention_days: 90,
  retention_choices: [30, 90, 180, 365], range: '24h', by: 'source', step: 3600,
  rows: [{ key: '10.0.0.2', up: 10, down: 4096, conns: 7 }], total: { up: 10, down: 4096, conns: 7 }, trend: [], ...extra
} } }) as any

const wrappers: VueWrapper[] = []
async function mountCard() {
  const wrapper = mount(MihomoConnectionStats, {
    attachTo: document.body,
    props: {
      connections: [
        connection('a', '10.0.0.2', 3072, ['HK', 'Proxy']),
        connection('b', '10.0.0.2', 1024, ['DIRECT'])
      ],
      speeds: new Map(), notes: {}, agentId: 'gw', active: true
    }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
const recordButtons = (wrapper: VueWrapper) => wrapper.get('.stats-record').findAll('button').map(button => button.attributes('aria-label') || button.text())

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  document.body.innerHTML = ''
  vi.mocked(trafficHistoryApi.get).mockResolvedValue(state())
  vi.mocked(trafficHistoryApi.clear).mockResolvedValue({} as any)
  vi.mocked(mihomoPanelApi.proxies).mockResolvedValue({ data: { data: { proxies: { HK: { type: 'Shadowsocks' } } } } } as any)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.unstubAllGlobals()
})

describe('connection stats on phones', () => {
  beforeEach(() => stubViewport(true))

  it('keeps one settings button in the record bar with the status on one line', async () => {
    const wrapper = await mountCard()
    // 开关 + 一个设置按钮，「清空记录」不再单独占位
    expect(recordButtons(wrapper)).toEqual(['后台记录流量', '记录设置'])
    const status = wrapper.get('.stats-record-status')
    expect(status.text()).toMatch(/^记录中 · 从 .+ 开始 · 保留 90 天$/)
    expect(status.attributes('title')).toBe(status.text())
    expect(phoneCss()).toMatch(/\.stats-record-status \{[^}]*white-space: nowrap/)
    // 视觉 32px，透明 ::before 保留 44px 点击区，并覆盖基础按钮的 max-md 最小尺寸
    expect(phoneCss()).toMatch(/\.stats-record-menu \{[^}]*width: 32px; height: 32px; min-width: 0; min-height: 0/)
    expect(phoneCss()).toMatch(/\.stats-record-menu::before \{ content: ''; position: absolute; inset: -6px; \}/)
  })

  it('reaches retention and a confirmed clear from the settings button', async () => {
    const wrapper = await mountCard()
    await wrapper.get('button[aria-label="记录设置"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[role="dialog"][aria-label="记录设置"]')!
    expect(menu).not.toBeNull()
    expect(menu.querySelector('[aria-label="保留时长"]')).not.toBeNull()
    const clear = () => [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(button => button.textContent?.includes('清空记录'))!

    // 取消确认时不清空
    vi.mocked(confirmDanger).mockResolvedValueOnce(false)
    clear().click()
    await flushPromises()
    expect(confirmDanger).toHaveBeenCalledTimes(1)
    expect(trafficHistoryApi.clear).not.toHaveBeenCalled()
    // 点清空后菜单收起
    expect(document.querySelector('[role="dialog"][aria-label="记录设置"]')).toBeNull()

    await wrapper.get('button[aria-label="记录设置"]').trigger('click')
    await flushPromises()
    vi.mocked(confirmDanger).mockResolvedValueOnce(true)
    clear().click()
    await flushPromises()
    expect(confirmDanger).toHaveBeenCalledTimes(2)
    expect(trafficHistoryApi.clear).toHaveBeenCalledWith('gw')
  })

  it('lays the summary out in two columns with the total across the first row', async () => {
    const wrapper = await mountCard()
    const tiles = wrapper.findAll('[data-tile]')
    // 总流量独占首行，其余两两成行，不留空格
    expect(tiles[0].classes()).toContain('stats-metric-primary')
    expect(tiles[0].text()).toContain('总流量')
    expect((tiles.length - 1) % 2).toBe(0)
    const css = phoneCss()
    expect(css).toContain('.stats-summary { grid-template-columns: repeat(2, minmax(0, 1fr));')
    expect(css).toMatch(/\.stats-metric\.stats-metric-primary \{ grid-column: 1 \/ -1;/)
    expect(css).toMatch(/\.stats-metric-sub \{[^}]*white-space: nowrap/)
  })

  it('still switches the range and keeps the compact fold button next to the dimensions', async () => {
    const wrapper = await mountCard()
    // 「收起 / 展开」可见，「全部明细」留给读屏
    const fold = wrapper.get('.stats-toggle-all')
    expect(fold.text()).toBe('收起全部明细')
    expect(fold.get('.max-sm\\:sr-only').text()).toBe('全部明细')
    const css = phoneCss()
    expect(css).toMatch(/\.stats-toolbar \{ flex-wrap: nowrap;/)
    expect(css).toMatch(/\.stats-toolbar \.stats-aggregate :deep\(\.cf-segmented-options\) \{[^}]*flex-wrap: nowrap/)
    expect(css).toMatch(/\.stats-card :deep\(\.cf-section-actions\) \{ flex: 1 1 auto;/)

    const range = wrapper.get('[role="radiogroup"][aria-label="统计范围"]')
    await range.findAll('[data-segment]').find(button => button.text() === '7 天')!.trigger('click')
    await flushPromises()
    expect(trafficHistoryApi.get).toHaveBeenLastCalledWith('gw', '7d', 'source', expect.anything())
    expect(localStorage.getItem('configflow:mihomo-overview:range')).toBe('7d')
  })
})

describe('connection stats on wider screens', () => {
  beforeEach(() => stubViewport(false))

  it('keeps the labelled settings and clear buttons', async () => {
    const wrapper = await mountCard()
    expect(recordButtons(wrapper)).toEqual(['后台记录流量', '记录设置', '清空记录'])
    expect(wrapper.find('.stats-record-menu').exists()).toBe(false)
    expect(wrapper.find('.stats-record-status').exists()).toBe(false)
    expect(wrapper.get('.stats-record').text()).toContain('保留 90 天')
  })
})
