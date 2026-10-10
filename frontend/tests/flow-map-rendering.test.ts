import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import FlowMap from '@/components/dashboard/FlowMap.vue'
import { usePreferences } from '@/stores/preferences'
import { useThemeStore } from '@/stores/theme'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

// Browser rendering primitives are controllable; the component, stores and
// exposed burst API run unchanged. No component internals are inspected.
const { prefs } = usePreferences()
const { setTheme } = useThemeStore()
const wrappers: VueWrapper[] = []
const frames = new Map<number, FrameRequestCallback>()
let nextFrame: number
let hidden: boolean
let resize: ResizeObserverCallback
let intersect: IntersectionObserverCallback
let resizeDisconnect: ReturnType<typeof vi.fn>
let intersectionDisconnect: ReturnType<typeof vi.fn>
let reduce: boolean
let media: MediaQueryList
let mediaEvents: EventTarget
const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden')
let ctx: ReturnType<typeof context>

function context() {
  return {
    setTransform: vi.fn(), clearRect: vi.fn(), beginPath: vi.fn(), moveTo: vi.fn(),
    bezierCurveTo: vi.fn(), setLineDash: vi.fn(), stroke: vi.fn(), arc: vi.fn(),
    fill: vi.fn(), lineTo: vi.fn(),
    createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() }))
  }
}

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'performance'] })
  frames.clear()
  nextFrame = 0
  hidden = false
  reduce = false
  prefs.value.motion = false
  prefs.value.accent = 'clay'
  prefs.value.density = 1
  setTheme('dark')
  await nextTick()
  ctx = context()
  vi.spyOn(Math, 'random').mockReturnValue(0)
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(ctx as unknown as CanvasRenderingContext2D)
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const id = this.getAttribute('data-fid')
    const x = id === 'b' || id === 'd' ? 220 : 20
    const y = id === 'c' || id === 'd' ? 120 : 40
    return new DOMRect(id ? x : 0, id ? y : 0, id ? 100 : 500, id ? 40 : 220)
  })
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  vi.stubGlobal('CSS', { escape: (value: string) => value })
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    const id = ++nextFrame
    frames.set(id, callback)
    return id
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => { frames.delete(id) }))
  resizeDisconnect = vi.fn()
  intersectionDisconnect = vi.fn()
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: ResizeObserverCallback) { resize = callback }
    observe = vi.fn()
    disconnect = resizeDisconnect
  })
  vi.stubGlobal('IntersectionObserver', class {
    constructor(callback: IntersectionObserverCallback) { intersect = callback }
    observe = vi.fn()
    disconnect = intersectionDisconnect
  })
  mediaEvents = new EventTarget()
  media = {
    get matches() { return reduce }, media: '(prefers-reduced-motion: reduce)',
    addEventListener: vi.fn(mediaEvents.addEventListener.bind(mediaEvents)),
    removeEventListener: vi.fn(mediaEvents.removeEventListener.bind(mediaEvents))
  } as unknown as MediaQueryList
  vi.stubGlobal('matchMedia', vi.fn(() => media))
})

afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  prefs.value.motion = false
  await nextTick()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  if (hiddenDescriptor) Object.defineProperty(document, 'hidden', hiddenDescriptor)
  else Reflect.deleteProperty(document, 'hidden')
  document.body.innerHTML = ''
})

async function render(options = {}) {
  const wrapper = mount(FlowMap, {
    attachTo: document.body,
    props: {
      columns: [
        { key: 'left', title: 'Sources', nodes: [{ id: 'a', title: 'A' }, { id: 'c', title: 'C' }] },
        { key: 'right', title: 'Targets', nodes: [{ id: 'b', title: 'B' }, { id: 'd', title: 'D' }] }
      ],
      edges: [{ from: 'a', to: 'b' }, { from: 'c', to: 'd' }],
      density: 0,
      ...options
    }
  })
  wrappers.push(wrapper)
  await nextTick()
  return wrapper
}

async function step(milliseconds = 16) {
  vi.advanceTimersByTime(milliseconds)
  const callbacks = [...frames.values()]
  frames.clear()
  callbacks.forEach(callback => callback(performance.now()))
  await nextTick()
}

async function visibility(value: boolean) {
  hidden = !value
  document.dispatchEvent(new Event('visibilitychange'))
  await nextTick()
}

async function intersection(value: boolean) {
  intersect([{ isIntersecting: value } as IntersectionObserverEntry], {} as IntersectionObserver)
  await nextTick()
}

async function reducedMotion(value: boolean) {
  reduce = value
  const event = new Event('change')
  Object.defineProperty(event, 'matches', { value })
  mediaEvents.dispatchEvent(event)
  await nextTick()
}

function burst(wrapper: VueWrapper, count = 12) {
  (wrapper.vm as unknown as { burst(to: string, count: number): void }).burst('b', count)
}

describe('FlowMap rendering lifecycle', () => {
  it('draws a static map once and coalesces layout, data, theme and focus changes', async () => {
    const wrapper = await render()
    expect(frames.size).toBe(1)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(1)
    expect(ctx.arc).toHaveBeenCalledTimes(6)
    expect(frames.size).toBe(0)
    for (let i = 0; i < 10; i++) await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(1)

    resize([], {} as ResizeObserver)
    await wrapper.setProps({ edges: [{ from: 'a', to: 'd' }], gapX: 80 })
    setTheme('light')
    prefs.value.accent = 'sky'
    await wrapper.get('[data-fid="a"]').trigger('pointerenter')
    expect(frames.size).toBe(1)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(2)
    expect(frames.size).toBe(0)
    expect(wrapper.get('[data-fid="c"]').classes()).toContain('opacity-30')
    await wrapper.get('[data-fid="a"]').trigger('blur')
    expect(frames.size).toBe(1)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(3)
    expect(frames.size).toBe(0)
  })

  it('defers all hidden/offscreen invalidations and draws the latest state on return', async () => {
    hidden = true
    const wrapper = await render()
    expect(frames.size).toBe(0)
    expect(Element.prototype.getBoundingClientRect).not.toHaveBeenCalled()
    resize([], {} as ResizeObserver)
    await wrapper.setProps({ edges: [{ from: 'a', to: 'd' }], height: 450 })
    prefs.value.accent = 'kraft'
    setTheme('light')
    await intersection(false)
    await visibility(true)
    expect(frames.size).toBe(0)
    expect(ctx.clearRect).not.toHaveBeenCalled()
    expect(Element.prototype.getBoundingClientRect).not.toHaveBeenCalled()

    await intersection(true)
    expect(frames.size).toBe(1)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(1)
    expect(ctx.bezierCurveTo).toHaveBeenCalledTimes(1)
    expect(frames.size).toBe(0)
    resize([], {} as ResizeObserver)
    expect(frames.size).toBe(1)
    await intersection(false)
    expect(frames.size).toBe(0)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(1)
    await intersection(true)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(2)
    expect(frames.size).toBe(0)
  })

  it('keeps one animation loop and stops it on motion, page and viewport changes', async () => {
    prefs.value.motion = true
    const wrapper = await render()
    await step()
    expect(frames.size).toBe(1)
    resize([], {} as ResizeObserver)
    await wrapper.setProps({ gapX: 40 })
    await wrapper.get('[data-fid="a"]').trigger('pointerenter')
    expect(frames.size).toBe(1)
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(2)
    expect(frames.size).toBe(1)
    const canceledFrame = [...frames.values()][0]!
    await visibility(false)
    expect(frames.size).toBe(0)
    for (let i = 0; i < 10; i++) await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(2)
    await visibility(true)
    expect(frames.size).toBe(1)
    canceledFrame(performance.now())
    expect(ctx.clearRect).toHaveBeenCalledTimes(2)
    expect(frames.size).toBe(1)
    await step()
    expect(frames.size).toBe(1)
    await intersection(false)
    expect(frames.size).toBe(0)
    await intersection(true)
    expect(frames.size).toBe(1)
    prefs.value.motion = false
    await nextTick()
    expect(frames.size).toBe(1)
    await step()
    expect(frames.size).toBe(0)
    const calls = ctx.clearRect.mock.calls.length
    await step()
    expect(ctx.clearRect).toHaveBeenCalledTimes(calls)
  })

  it('reacts to system reduced-motion changes without leaving an idle loop', async () => {
    prefs.value.motion = true
    reduce = true
    await render()
    await step()
    expect(frames.size).toBe(0)
    await reducedMotion(false)
    expect(frames.size).toBe(1)
    await step()
    expect(frames.size).toBe(1)
    await reducedMotion(true)
    await step()
    expect(frames.size).toBe(0)
    await visibility(false)
    await reducedMotion(false)
    expect(frames.size).toBe(0)
    await visibility(true)
    await step()
    expect(frames.size).toBe(1)
    // A query is subscribed once, rather than re-created on every frame.
    expect(window.matchMedia).toHaveBeenCalledTimes(1)
  })

  it('ignores bursts while static/hidden and drops existing particles on suspension', async () => {
    const wrapper = await render()
    await step()
    burst(wrapper)
    expect(frames.size).toBe(0)
    prefs.value.motion = true
    await nextTick()
    await step()
    expect(ctx.createLinearGradient).not.toHaveBeenCalled()
    burst(wrapper)
    await step()
    expect(ctx.createLinearGradient).toHaveBeenCalled()
    await visibility(false)
    burst(wrapper)
    expect(frames.size).toBe(0)
    ctx.createLinearGradient.mockClear()
    await visibility(true)
    await step()
    expect(ctx.createLinearGradient).not.toHaveBeenCalled()
    await intersection(false)
    burst(wrapper)
    await intersection(true)
    await step()
    expect(ctx.createLinearGradient).not.toHaveBeenCalled()
  })

  it('clears hit timers when made static and removes every task/listener on unmount', async () => {
    prefs.value.motion = true
    const wrapper = await render()
    await step()
    burst(wrapper, 1)
    for (let i = 0; i < 20 && !wrapper.find('.is-hit').exists(); i++) await step(50)
    expect(wrapper.find('.is-hit').exists()).toBe(true)
    expect(vi.getTimerCount()).toBeGreaterThan(0)
    prefs.value.motion = false
    await nextTick()
    // jsdom dispatches persisted preference storage events via a zero-delay timer.
    // Drain only those events; an uncleared 260 ms hit timer would remain pending.
    vi.advanceTimersByTime(0)
    expect(wrapper.find('.is-hit').exists()).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    await step()
    expect(frames.size).toBe(0)

    prefs.value.motion = true
    await nextTick()
    await step()
    burst(wrapper, 1)
    for (let i = 0; i < 20 && !wrapper.find('.is-hit').exists(); i++) await step(50)
    expect(wrapper.find('.is-hit').exists()).toBe(true)
    const pending = [...frames.values()]
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(frames.size).toBe(0)
    expect(vi.getTimerCount()).toBe(0)
    expect(resizeDisconnect).toHaveBeenCalledOnce()
    expect(intersectionDisconnect).toHaveBeenCalledOnce()
    expect(media.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
    const calls = ctx.clearRect.mock.calls.length
    // Even a late observer/frame callback cannot resurrect an unmounted map.
    pending.forEach(callback => callback(performance.now()))
    resize([], {} as ResizeObserver)
    await reducedMotion(true)
    await visibility(false)
    await visibility(true)
    burst(wrapper)
    await step()
    expect(frames.size).toBe(0)
    expect(ctx.clearRect).toHaveBeenCalledTimes(calls)
  })
})
