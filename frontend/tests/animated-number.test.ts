import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import AnimatedNumber from '@/components/common/AnimatedNumber.vue'
import { usePreferences } from '@/stores/preferences'

const { prefs } = usePreferences()
const wrappers: VueWrapper[] = []
const frames = new Map<number, FrameRequestCallback>()
let nextFrame = 0
let now = 0
let hidden = false
let previousMotion = false
let reduced = false
let mediaListeners: Set<(event: MediaQueryListEvent) => void>

beforeEach(() => {
  previousMotion = prefs.value.motion
  prefs.value.motion = false
  frames.clear()
  nextFrame = 0
  now = 0
  hidden = false
  reduced = false
  mediaListeners = new Set()
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    const id = ++nextFrame
    frames.set(id, callback)
    return id
  }))
  vi.stubGlobal('cancelAnimationFrame', vi.fn((id: number) => { frames.delete(id) }))
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    media: query,
    get matches() { return reduced },
    addEventListener: (type: string, listener: (event: MediaQueryListEvent) => void) => {
      if (type === 'change') mediaListeners.add(listener)
    },
    removeEventListener: (type: string, listener: (event: MediaQueryListEvent) => void) => {
      if (type === 'change') mediaListeners.delete(listener)
    }
  })))
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  prefs.value.motion = previousMotion
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function render(value = 100, extra: { precision?: number; instant?: boolean } = {}) {
  const wrapper = mount(AnimatedNumber, { props: { value, ...extra } })
  wrappers.push(wrapper)
  return wrapper
}

async function advanceFrame(timestamp: number) {
  now = timestamp
  const pending = [...frames.entries()]
  pending.forEach(([id]) => frames.delete(id))
  pending.forEach(([, callback]) => callback(timestamp))
  await nextTick()
}

async function setReduced(value: boolean) {
  reduced = value
  const event = new Event('change') as MediaQueryListEvent
  Object.defineProperty(event, 'matches', { value })
  mediaListeners.forEach(listener => listener(event))
  await nextTick()
}

async function setHidden(value: boolean) {
  hidden = value
  document.dispatchEvent(new Event('visibilitychange'))
  await nextTick()
}

describe('AnimatedNumber motion lifecycle', () => {
  it('shows current values immediately without scheduling frames when motion is off', async () => {
    const wrapper = render(1234.56, { precision: 2 })
    expect(wrapper.text()).toBe('1,234.56')
    await wrapper.setProps({ value: 9876.54 })
    expect(wrapper.text()).toBe('9,876.54')
    expect(requestAnimationFrame).not.toHaveBeenCalled()
    expect(frames.size).toBe(0)
  })

  it('animates enabled changes to completion and does not animate unchanged values', async () => {
    prefs.value.motion = true
    const wrapper = render(1000)
    expect(wrapper.text()).toBe('0')
    expect(frames.size).toBe(1)
    await advanceFrame(100)
    expect(Number(wrapper.text().replaceAll(',', ''))).toBeGreaterThan(0)
    expect(Number(wrapper.text().replaceAll(',', ''))).toBeLessThan(1000)
    await advanceFrame(1000)
    expect(wrapper.text()).toBe('1,000')
    expect(frames.size).toBe(0)
    vi.mocked(requestAnimationFrame).mockClear()
    await wrapper.setProps({ value: 1000 })
    expect(requestAnimationFrame).not.toHaveBeenCalled()
  })

  it('cancels an active animation when preferences turn off and does not replay it on enable', async () => {
    prefs.value.motion = true
    const wrapper = render(1000)
    await advanceFrame(100)
    prefs.value.motion = false
    await nextTick()
    expect(wrapper.text()).toBe('1,000')
    expect(frames.size).toBe(0)
    expect(cancelAnimationFrame).toHaveBeenCalled()
    await wrapper.setProps({ value: 2000 })
    expect(wrapper.text()).toBe('2,000')
    prefs.value.motion = true
    await nextTick()
    expect(frames.size).toBe(0)
    await wrapper.setProps({ value: 3000 })
    expect(frames.size).toBe(1)
    await advanceFrame(1100)
    expect(wrapper.text()).toBe('3,000')
  })

  it('respects reduced motion on mount and reacts to system preference changes', async () => {
    prefs.value.motion = true
    reduced = true
    const wrapper = render(1000)
    expect(wrapper.text()).toBe('1,000')
    expect(requestAnimationFrame).not.toHaveBeenCalled()
    await setReduced(false)
    expect(frames.size).toBe(0)
    await wrapper.setProps({ value: 2000 })
    expect(frames.size).toBe(1)
    await setReduced(true)
    expect(wrapper.text()).toBe('2,000')
    expect(frames.size).toBe(0)
    await wrapper.setProps({ value: 3000 })
    expect(wrapper.text()).toBe('3,000')
    expect(frames.size).toBe(0)
  })

  it('stops in a hidden document and keeps subsequent data current without replay on return', async () => {
    prefs.value.motion = true
    const wrapper = render(1000)
    await advanceFrame(100)
    await setHidden(true)
    expect(wrapper.text()).toBe('1,000')
    expect(frames.size).toBe(0)
    vi.mocked(requestAnimationFrame).mockClear()
    await wrapper.setProps({ value: 2000 })
    expect(wrapper.text()).toBe('2,000')
    expect(requestAnimationFrame).not.toHaveBeenCalled()
    await setHidden(false)
    expect(frames.size).toBe(0)
    await wrapper.setProps({ value: 3000 })
    expect(frames.size).toBe(1)
  })

  it('does not start animations when mounted in the background or given an unchanged zero', () => {
    prefs.value.motion = true
    hidden = true
    expect(render(1000).text()).toBe('1,000')
    hidden = false
    expect(render(0).text()).toBe('0')
    expect(requestAnimationFrame).not.toHaveBeenCalled()
  })

  it('reacts to instant mode and preserves finite-value formatting', async () => {
    prefs.value.motion = true
    const wrapper = render(1234.56, { precision: 2 })
    expect(frames.size).toBe(1)
    await wrapper.setProps({ instant: true })
    expect(wrapper.text()).toBe('1,234.56')
    expect(frames.size).toBe(0)
    await wrapper.setProps({ value: Number.NaN })
    expect(wrapper.text()).toBe('0.00')
    await wrapper.setProps({ instant: false, value: Number.POSITIVE_INFINITY })
    expect(wrapper.text()).toBe('0.00')
    expect(frames.size).toBe(0)
  })

  it('cancels pending frames and removes both listeners when unmounted', async () => {
    prefs.value.motion = true
    const add = vi.spyOn(document, 'addEventListener')
    const remove = vi.spyOn(document, 'removeEventListener')
    const wrapper = render(1000)
    const visibilityListener = add.mock.calls.find(([type]) => type === 'visibilitychange')![1]
    const staleFrame = [...frames.values()][0]!
    expect(mediaListeners.size).toBe(1)
    expect(frames.size).toBe(1)
    wrapper.unmount()
    expect(frames.size).toBe(0)
    expect(mediaListeners.size).toBe(0)
    expect(remove).toHaveBeenCalledWith('visibilitychange', visibilityListener)
    vi.mocked(requestAnimationFrame).mockClear()
    staleFrame(100)
    await setHidden(true)
    await setReduced(true)
    expect(requestAnimationFrame).not.toHaveBeenCalled()
  })
})
