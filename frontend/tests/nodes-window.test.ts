import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { h } from 'vue'
import NodeWindow from '@/components/nodes/NodeWindow.vue'

let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.stubGlobal('scrollTo', vi.fn())
  vi.stubGlobal('innerHeight', 3 * 61)
})
afterEach(() => {
  wrapper?.unmount()
  document.body.replaceChildren()
  vi.unstubAllGlobals()
})

it('滚动只渲染新进入窗口的节点，保留重叠项并复用离屏实例', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const nodes = Array.from({ length: 40 }, (_, i) => String(i))
  const render = vi.fn(({ node }: { node: string }) => h('button', { 'data-name': node }, node))
  wrapper = mount(NodeWindow, {
    attachTo: container,
    props: { items: nodes, itemKey: (node: string) => node, full: false },
    slots: { default: render },
  })
  await flushPromises()
  const buttons = () => new Map(wrapper!.findAll('button').map(row => [row.attributes('data-name'), row.element]))
  const before = buttons()
  render.mockClear()

  vi.stubGlobal('scrollY', 3 * 61)
  window.dispatchEvent(new Event('scroll'))
  await flushPromises()
  const after = buttons()
  const retained = [...before.keys()].filter(id => after.has(id))
  const evicted = [...before].filter(([id]) => !after.has(id))
  const rendered = render.mock.calls.map(([cell]) => cell.node)
  expect(retained.length).toBeGreaterThan(0)
  for (const id of retained) {
    expect(after.get(id)).toBe(before.get(id))
    expect(rendered).not.toContain(id)
  }
  expect(evicted.some(([, el]) => [...after.values()].includes(el))).toBe(true)
  expect(rendered.length).toBeGreaterThan(0)
})

it('表格行高回报晚于下一次翻页时，按新的滚动位置补偿高度差', async () => {
  const reportSize = new Map<Element, (height: number) => void>()
  vi.stubGlobal('ResizeObserver', class {
    constructor(private notify: (entries: Partial<ResizeObserverEntry>[]) => void) {}
    observe(target: HTMLElement) {
      if (target.dataset.nodeIndex !== undefined) {
        reportSize.set(target, height => this.notify([{ target, borderBoxSize: [{ inlineSize: 0, blockSize: height }] }]))
      }
    }
    unobserve(target: Element) { reportSize.delete(target) }
    disconnect() { reportSize.clear() }
  })
  wrapper = mount(NodeWindow, {
    props: { items: Array.from({ length: 40 }, (_, i) => String(i)), itemKey: (id: string) => id, full: false, table: true },
    slots: { default: ({ node }: { node: string }) => h('tr', [h('td', node)]) },
  })
  await flushPromises()
  vi.stubGlobal('scrollY', 10 * 61)
  window.dispatchEvent(new Event('scroll'))
  await flushPromises()
  const previousRow = wrapper.get('[data-node-index="9"]').element
  // 浏览器已翻到下一页，上一页的 ResizeObserver 回调先于新的 scroll 事件到达。
  vi.stubGlobal('scrollY', 13 * 61)
  reportSize.get(previousRow)!(71)
  expect(window.scrollTo).toHaveBeenLastCalledWith(expect.objectContaining({ top: 13 * 61 + 10 }))
})
