import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import Nodes from '@/views/Nodes.vue'
import { nodeApi } from '@/api'
import { listItemStyle, markListItemEntered } from '@/lib/motion'

vi.mock('@/api', () => ({
  default: { get: vi.fn(), post: vi.fn() },
  nodeApi: { getAll: vi.fn(), latency: vi.fn(), testLatency: vi.fn() },
  profileApi: { list: vi.fn() }
}))

const matchMediaDescriptor = Object.getOwnPropertyDescriptor(window, 'matchMedia')
let wrapper: VueWrapper | undefined

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  })
})
afterAll(() => {
  if (matchMediaDescriptor) Object.defineProperty(window, 'matchMedia', matchMediaDescriptor)
  else delete (window as any).matchMedia
})
beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  const nodes = Array.from({ length: 14 }, (_, i) => ({ id: `n${i}`, name: `节点 ${i}`, enabled: true, proxy_string: `trojan://p@192.0.2.${i + 1}:443` }))
  vi.mocked(nodeApi.getAll).mockResolvedValue({ data: nodes } as any)
  vi.mocked(nodeApi.latency).mockResolvedValue({ data: { results: {} } } as any)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('列表卡片进场', () => {
  it('只给前 12 项写错峰序号，之后沿用 CSS 的最大延迟', () => {
    expect(listItemStyle(0)).toEqual({ '--i': 0 })
    expect(listItemStyle(11)).toEqual({ '--i': 11 })
    expect(listItemStyle(12)).toBeUndefined()
  })

  it('节点磁贴是普通元素加 CSS 进场，不再逐张包 motion-v', async () => {
    localStorage.setItem('configflow-nodes-view', 'card')
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Nodes }] })
    await router.push('/')
    await router.isReady()
    wrapper = mount(Nodes, { attachTo: document.body, global: { plugins: [router] } })
    await flushPromises()

    const tiles = wrapper.findAll('[data-tile]')
    expect(tiles).toHaveLength(14)
    expect(tiles.every(tile => tile.element.tagName === 'DIV' && tile.classes('cf-list-in'))).toBe(true)
    // 静态 class 与原有 :class 合并，卡片样式不丢
    expect(tiles[0].classes('node-tile')).toBe(true)
    expect((tiles[0].element as HTMLElement).style.getPropertyValue('--i')).toBe('0')
    expect((tiles[13].element as HTMLElement).style.getPropertyValue('--i')).toBe('')
  })

  it('进场播完才标记，其他动画结束不影响', () => {
    const card = document.createElement('div')
    markListItemEntered({ animationName: 'spin', target: card } as unknown as AnimationEvent)
    expect(card.hasAttribute('data-entered')).toBe(false)
    markListItemEntered({ animationName: 'cf-list-in', target: card } as unknown as AnimationEvent)
    expect(card.hasAttribute('data-entered')).toBe(true)
  })

  it('进场被打断也标记，之后重新显示或移动不再补播', () => {
    const card = document.createElement('div')
    const cancel = new Event('animationcancel', { bubbles: true }) as AnimationEvent
    Object.defineProperty(cancel, 'animationName', { value: 'cf-list-in' })
    document.body.appendChild(card)
    document.addEventListener('animationcancel', markListItemEntered)
    try {
      card.dispatchEvent(cancel)
    } finally {
      document.removeEventListener('animationcancel', markListItemEntered)
    }
    expect(card.hasAttribute('data-entered')).toBe(true)
  })

  it('静止模式和系统减少动效都不播放进场', () => {
    const css = readFileSync(resolve(__dirname, '../src/styles/theme.css'), 'utf8').replace(/\s+/g, ' ')
    expect(css).toMatch(/:root\[data-motion='off'\] \.cf-list-in \{ animation: none; \}/)
    const reduce = css.match(/@media \(prefers-reduced-motion: reduce\) \{(.*?)\} \}/g) ?? []
    expect(reduce.some(block => block.includes('.cf-list-in') && block.includes('animation: none'))).toBe(true)
    // 已播完的卡片被移动或拖动克隆时不重播
    expect(css).toContain('.cf-list-in:not([data-entered]) {')
  })
})
