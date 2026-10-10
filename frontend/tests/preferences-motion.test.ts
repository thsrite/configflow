import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const key = 'configflow-preferences'

beforeEach(() => {
  vi.resetModules()
  localStorage.clear()
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false })))
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe('motion preference', () => {
  it('starts static without a saved choice, and persists an explicit opt-in', async () => {
    const { usePreferences } = await import('@/stores/preferences')
    const { prefs, motionEnabled } = usePreferences()
    expect(motionEnabled()).toBe(false)
    expect(document.documentElement.dataset.motion).toBe('off')

    prefs.value.motion = true
    await nextTick()
    expect(motionEnabled()).toBe(true)
    expect(document.documentElement.dataset.motion).toBe('on')
    expect(JSON.parse(localStorage.getItem(key)!).motion).toBe(true)
  })

  it.each([true, false])('retains a saved motion choice of %s', async motion => {
    localStorage.setItem(key, JSON.stringify({ motion }))
    const { usePreferences } = await import('@/stores/preferences')
    expect(usePreferences().motionEnabled()).toBe(motion)
    expect(document.documentElement.dataset.motion).toBe(motion ? 'on' : 'off')
  })

  it.each(['{}', '{"motion":"on"}', 'invalid json'])('uses static for missing or invalid preferences: %s', async raw => {
    localStorage.setItem(key, raw)
    const { usePreferences } = await import('@/stores/preferences')
    expect(usePreferences().motionEnabled()).toBe(false)
  })

  it('remains static when browser storage cannot be read', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    const { usePreferences } = await import('@/stores/preferences')
    expect(usePreferences().motionEnabled()).toBe(false)
    expect(document.documentElement.dataset.motion).toBe('off')
  })

  it('gives the system reduced-motion setting priority over an explicit opt-in', async () => {
    localStorage.setItem(key, JSON.stringify({ motion: true }))
    const { usePreferences } = await import('@/stores/preferences')
    const { motionEnabled } = usePreferences()
    expect(motionEnabled()).toBe(true)
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
    expect(motionEnabled()).toBe(false)
  })
})
