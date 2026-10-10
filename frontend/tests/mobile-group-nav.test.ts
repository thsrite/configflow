import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, useRoute } from 'vue-router'
import MobileGroupNav from '@/components/shell/MobileGroupNav.vue'
import { NAV_GROUPS } from '@/navigation'

// Keep the router, navigation and Sheet real. jsdom supplies no responsive
// layout, so provide only resize/media-query events and its missing scroll API.
let viewportWidth = 320
let contentWidth = 288
const observers = new Set<SyntheticResizeObserver>()
const mediaQueries = new Set<SyntheticMediaQueryList>()
const wrappers: ReturnType<typeof mount>[] = []
const originalMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia')
const originalScroll = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollIntoView')

class SyntheticResizeObserver {
  private targets = new Set<Element>()
  constructor(private callback: ResizeObserverCallback) {
    observers.add(this)
  }
  observe(target: Element) {
    this.targets.add(target)
    queueMicrotask(() => this.notify())
  }
  unobserve(target: Element) { this.targets.delete(target) }
  disconnect() {
    this.targets.clear()
    observers.delete(this)
  }
  notify() {
    const entries = [...this.targets].map(target => ({
      target,
      contentRect: new DOMRect(0, 0, contentWidth, 48),
      contentBoxSize: [{ inlineSize: contentWidth, blockSize: 48 }],
      borderBoxSize: [{ inlineSize: contentWidth, blockSize: 48 }],
      devicePixelContentBoxSize: [{ inlineSize: contentWidth, blockSize: 48 }]
    }))
    if (entries.length) this.callback(entries, this as unknown as ResizeObserver)
  }
}

class SyntheticMediaQueryList extends EventTarget {
  onchange: ((event: MediaQueryListEvent) => void) | null = null
  constructor(readonly media: string) {
    super()
    mediaQueries.add(this)
  }
  get matches() {
    const constraints = [...this.media.matchAll(/\((min|max)-width:\s*([\d.]+)(px|rem)\)/g)]
    return constraints.length > 0 && constraints.every(([, bound, amount, unit]) => {
      const width = Number(amount) * (unit === 'rem' ? 16 : 1)
      return bound === 'max' ? viewportWidth <= width : viewportWidth >= width
    })
  }
  addListener(listener: EventListener) { this.addEventListener('change', listener) }
  removeListener(listener: EventListener) { this.removeEventListener('change', listener) }
  notify() {
    const event = Object.assign(new Event('change'), { matches: this.matches, media: this.media })
    this.dispatchEvent(event)
    this.onchange?.(event as MediaQueryListEvent)
  }
}

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string) => new SyntheticMediaQueryList(query)
  })
  Object.defineProperty(Element.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
})

afterAll(() => {
  if (originalMatchMedia) Object.defineProperty(window, 'matchMedia', originalMatchMedia)
  else Reflect.deleteProperty(window, 'matchMedia')
  if (originalScroll) Object.defineProperty(Element.prototype, 'scrollIntoView', originalScroll)
  else Reflect.deleteProperty(Element.prototype, 'scrollIntoView')
})

beforeEach(() => {
  viewportWidth = 320
  contentWidth = 288
  observers.clear()
  mediaQueries.clear()
  vi.stubGlobal('ResizeObserver', SyntheticResizeObserver)
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  observers.clear()
  mediaQueries.clear()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const Harness = defineComponent({
  props: {
    activePathOverride: String,
    aggregationEnabled: { type: Boolean, default: true }
  },
  setup(props) {
    const route = useRoute()
    return () => h(MobileGroupNav, {
      activePath: props.activePathOverride ?? route.path,
      subscriptionAggregationEnabled: props.aggregationEnabled
    })
  }
})

async function render(path: string, aggregationEnabled = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: NAV_GROUPS.flatMap(group => group.items.map(item => ({
      path: item.path,
      component: { render: () => h('div', 'Synthetic route') }
    })))
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(Harness, {
    props: { aggregationEnabled },
    attachTo: document.body,
    global: { plugins: [router] }
  })
  wrappers.push(wrapper)
  await flushPromises()
  observers.forEach(observer => observer.notify())
  await flushPromises()
  return { wrapper, router }
}

function openSheet() {
  return document.querySelector<HTMLElement>('[role="dialog"][data-state="open"]')
}

async function openMenu(wrapper: ReturnType<typeof mount>, group: string, current: string) {
  await wrapper.get(`button[aria-label="切换${group}页面，当前${current}"]`).trigger('click')
  await flushPromises()
  expect(openSheet()).not.toBeNull()
  expect(openSheet()!.textContent).toContain(`切换${group}页面`)
  return openSheet()!.querySelector<HTMLElement>(`nav[aria-label="${group}页面"]`)!
}

describe('mobile group navigation', () => {
  it.each([
    { path: '/subscriptions', group: '资源', current: '订阅来源', enabled: true, labels: ['订阅来源', '节点库', '订阅聚合', '规则库'] },
    { path: '/subscriptions', group: '资源', current: '订阅来源', enabled: false, labels: ['订阅来源', '节点库', '规则库'] },
    { path: '/logs', group: '系统', current: '日志', enabled: true, labels: ['系统设置', '配置空间', 'Agent', '日志'] },
    { path: '/rules', group: '当前配置', current: '策略规则', enabled: true, labels: ['策略组', '策略规则', '域名发现', '配置生成'] }
  ])('shows every $group destination and marks the current page (aggregation=$enabled)', async ({ path, group, current, enabled, labels }) => {
    const { wrapper } = await render(path, enabled)
    const nav = await openMenu(wrapper, group, current)
    expect(nav).not.toBeNull()
    expect([...nav.querySelectorAll('a')].map(link => link.textContent?.trim())).toEqual(labels)
    const selected = nav.querySelectorAll('a[aria-current="page"]')
    expect(selected).toHaveLength(1)
    expect(selected[0].getAttribute('href')).toBe(path)
  })

  it('navigates through real router links and closes the sheet after selecting another page', async () => {
    const { wrapper, router } = await render('/subscriptions')
    const nav = await openMenu(wrapper, '资源', '订阅来源')
    nav.querySelector<HTMLAnchorElement>('a[href="/nodes"]')!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/nodes')
    expect(openSheet()).toBeNull()
    expect(wrapper.find('button[aria-label="切换资源页面，当前节点库"]').exists()).toBe(true)
  })

  it('closes when the current destination is chosen without changing the route', async () => {
    const { wrapper, router } = await render('/subscriptions')
    const nav = await openMenu(wrapper, '资源', '订阅来源')
    nav.querySelector<HTMLAnchorElement>('a[href="/subscriptions"]')!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/subscriptions')
    expect(openSheet()).toBeNull()
  })

  it('keeps the sheet open when a navigation guard cancels the destination change', async () => {
    const { wrapper, router } = await render('/subscriptions')
    router.beforeEach(to => to.path === '/nodes' ? false : undefined)
    const nav = await openMenu(wrapper, '资源', '订阅来源')
    nav.querySelector<HTMLAnchorElement>('a[href="/nodes"]')!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/subscriptions')
    expect(openSheet()).not.toBeNull()
    expect(openSheet()!.querySelector('a[aria-current="page"]')?.getAttribute('href')).toBe('/subscriptions')
  })

  it('cancels the sheet without navigating', async () => {
    const { wrapper, router } = await render('/subscriptions')
    await openMenu(wrapper, '资源', '订阅来源')
    openSheet()!.querySelector<HTMLButtonElement>('[aria-label="关闭页面切换"]')!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/subscriptions')
    expect(openSheet()).toBeNull()
  })

  it('closes an open sheet when activePath changes outside the menu', async () => {
    const { wrapper, router } = await render('/subscriptions')
    await openMenu(wrapper, '资源', '订阅来源')
    await wrapper.setProps({ activePathOverride: '/nodes' })
    await flushPromises()
    expect(openSheet()).toBeNull()
    expect(wrapper.find('button[aria-label="切换资源页面，当前节点库"]').exists()).toBe(true)
    expect(router.currentRoute.value.path).toBe('/subscriptions')
  })

  it('removes the aggregation destination when its feature flag changes', async () => {
    const { wrapper } = await render('/subscriptions')
    await openMenu(wrapper, '资源', '订阅来源')
    await wrapper.setProps({ aggregationEnabled: false })
    await flushPromises()
    expect(openSheet()).toBeNull()
    const nav = await openMenu(wrapper, '资源', '订阅来源')
    expect([...nav.querySelectorAll('a')].map(link => link.textContent?.trim())).toEqual(['订阅来源', '节点库', '规则库'])
  })

  it('keeps all destinations inline when tablet content has enough space', async () => {
    viewportWidth = 768
    contentWidth = 736
    const { wrapper } = await render('/subscriptions')
    const nav = wrapper.get('nav[aria-label="分组内页面"]')
    expect(nav.findAll('a').map(link => link.text())).toEqual(['订阅来源', '节点库', '订阅聚合', '规则库'])
    expect(nav.get('button[aria-label^="切换"]').isVisible()).toBe(false)
    expect(nav.findAll('a').every(link => link.isVisible())).toBe(true)
    expect(nav.findAll('a[aria-current="page"]')).toHaveLength(1)
    expect(openSheet()).toBeNull()
  })

  it('uses the menu on a narrow tablet and closes it once all tabs can fit', async () => {
    viewportWidth = 768
    contentWidth = 470
    const { wrapper } = await render('/subscriptions')
    await openMenu(wrapper, '资源', '订阅来源')
    // Four 112px items and three 8px gaps require 472px of actual content width.
    contentWidth = 472
    observers.forEach(observer => observer.notify())
    await flushPromises()
    expect(openSheet()).toBeNull()
    const nav = wrapper.get('nav[aria-label="分组内页面"]')
    expect(nav.get('button[aria-label^="切换"]').isVisible()).toBe(false)
    expect(nav.findAll('a').every(link => link.isVisible())).toBe(true)
  })

  it('falls back to the menu and exposes every destination when a group grows beyond four items', async () => {
    viewportWidth = 768
    contentWidth = 736
    const systemGroup = NAV_GROUPS.find(group => group.scope === 'system')!
    const originalItems = systemGroup.items
    const additionalItems = Array.from({ length: 4 }, (_, index) => ({
      path: `/synthetic-system-page-${index + 1}`,
      label: `合成入口 ${index + 1}`,
      icon: 'Setting'
    }))
    systemGroup.items = [...originalItems, ...additionalItems]
    let wrapper: ReturnType<typeof mount> | undefined
    try {
      // The real memory router also receives the extra routes through render().
      const rendered = await render('/logs')
      wrapper = rendered.wrapper
      expect(wrapper.get('button[aria-label="切换系统页面，当前日志"]').isVisible()).toBe(true)
      const nav = await openMenu(wrapper, '系统', '日志')
      const links = [...nav.querySelectorAll('a')]
      expect(links).toHaveLength(8)
      expect(links.map(link => link.getAttribute('href'))).toEqual(systemGroup.items.map(item => item.path))
      expect(links.map(link => link.textContent?.trim())).toEqual(systemGroup.items.map(item => item.label))
      expect(nav.querySelector('a[aria-current="page"]')?.getAttribute('href')).toBe('/logs')
    } finally {
      // Restore shared configuration even if an assertion fails, before any
      // subsequent test creates a router or computes its navigation group.
      systemGroup.items = originalItems
      if (wrapper) {
        wrapper.unmount()
        wrappers.splice(wrappers.indexOf(wrapper), 1)
      }
    }
  })

  it('closes an open mobile sheet when the viewport switches to desktop', async () => {
    const { wrapper, router } = await render('/subscriptions')
    await openMenu(wrapper, '资源', '订阅来源')
    viewportWidth = 1200
    contentWidth = 880
    mediaQueries.forEach(query => query.notify())
    observers.forEach(observer => observer.notify())
    await flushPromises()
    expect(openSheet()).toBeNull()
    expect(router.currentRoute.value.path).toBe('/subscriptions')
  })

  it('hides group navigation for the single-page overview group', async () => {
    const { wrapper } = await render('/dashboard')
    expect(wrapper.find('nav[aria-label="分组内页面"]').exists()).toBe(false)
    expect(openSheet()).toBeNull()
  })

  it('does not render a switcher when the active path has no navigation group', async () => {
    const { wrapper } = await render('/subscriptions')
    await wrapper.setProps({ activePathOverride: '/not-in-navigation' })
    await flushPromises()
    expect(wrapper.find('nav[aria-label="分组内页面"]').exists()).toBe(false)
    expect(openSheet()).toBeNull()
  })
})
