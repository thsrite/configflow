import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import CommandPalette from '@/components/shell/CommandPalette.vue'
import { NAV_GROUPS } from '@/navigation'

const wrappers: VueWrapper[] = []
beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = ''; vi.unstubAllGlobals() })

async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: NAV_GROUPS.flatMap(g => g.items.map(i => ({ path: i.path, component: { render: () => h('div') } }))) })
  await router.push('/dashboard'); await router.isReady()
  const wrapper = mount(CommandPalette, { props: { subscriptionAggregationEnabled: true }, attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  return { wrapper, router }
}
const openPalette = async (wrapper: VueWrapper) => { (wrapper.vm as any).show(); await flushPromises() }
const headings = () => [...document.querySelectorAll('[role="dialog"] [cmdk-group-heading], [role="dialog"] [data-slot="command-group"] > div:first-child')].map(e => e.textContent?.trim())
const frequentLabels = () => [...document.querySelectorAll('[data-frequent-pages] [data-slot="command-item"]')].map(e => e.querySelector('span')?.textContent?.trim())

describe('command palette suggestions', () => {
  it('shows only frequent pages and common actions before typing, with sensible defaults', async () => {
    const { wrapper } = await render()
    await openPalette(wrapper)
    expect(frequentLabels()).toEqual(['数据统计', 'OxiDNS 面板', 'Mihomo 面板', '策略规则', 'Agent'])
    const text = document.querySelector('[role="dialog"]')!.textContent!
    expect(text).toContain('常用页面')
    expect(text).toContain('常用操作')
    // 全部页面的分组标题不出现
    expect(text).not.toContain('共享资源')
  })

  it('ranks pages by how often they are opened on this device', async () => {
    const { wrapper, router } = await render()
    // 节点库 4 次、上游路由 3 次、数据统计 1 次
    for (let i = 0; i < 4; i++) { await router.push('/nodes'); await router.push(i === 0 ? '/dashboard' : '/routers') }
    await openPalette(wrapper)
    expect(frequentLabels()!.slice(0, 3)).toEqual(['节点库', '上游路由', '数据统计'])
    expect(frequentLabels()).toContain('上游路由')
    expect(frequentLabels()).toHaveLength(5)
  })

  it('searches every page once the user types', async () => {
    const { wrapper } = await render()
    await openPalette(wrapper)
    const input = document.querySelector<HTMLInputElement>('[data-slot="command-input"]')!
    input.value = '路由'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    const text = document.querySelector('[role="dialog"]')!.textContent!
    expect(text).toContain('上游路由')
    expect(document.querySelector('[data-frequent-pages]')).toBeNull()
  })

  it('does not repeat the bottom bar on phones and suggests pages only reachable from 更多', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
      matches: query.includes('max-width: 900px'), media: query, onchange: null,
      addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false
    })))
    const { wrapper, router } = await render()
    // 底栏页面去得再多也不推荐
    for (let i = 0; i < 5; i++) { await router.push('/rules'); await router.push('/routers') }
    await openPalette(wrapper)
    expect(frequentLabels()).toEqual(['上游路由', '节点库', '订阅来源', '策略组', '客户端设备'])
    for (const tab of ['数据统计', 'OxiDNS 面板', 'Mihomo 面板', '策略规则', 'Agent']) expect(frequentLabels()).not.toContain(tab)
  })
})
