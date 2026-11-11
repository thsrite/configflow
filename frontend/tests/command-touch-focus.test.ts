import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RulePolicySelect from '@/components/rules/RulePolicySelect.vue'

afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

const media = (coarse: boolean) => vi.fn((query: string) => ({
  matches: coarse && query.includes('pointer: coarse'), media: query, onchange: null,
  addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false
}))

async function openSelect() {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
  const wrapper = mount(RulePolicySelect, { props: { modelValue: 'DIRECT', policies: ['Proxy'], label: '策略' }, attachTo: document.body })
  await wrapper.get('button').trigger('click')
  await flushPromises()
  await new Promise(resolve => setTimeout(resolve, 0))
  return wrapper
}

describe('search box in dropdown lists', () => {
  it('is not focused on a touch screen, so opening a list does not raise the keyboard', async () => {
    vi.stubGlobal('matchMedia', media(true))
    const wrapper = await openSelect()
    const search = document.querySelector('[data-slot="command-input"]')!
    expect(search).toBeTruthy()
    expect(document.activeElement).not.toBe(search)
    wrapper.unmount()
  })

  it('is still focused with a mouse so typing filters right away', async () => {
    vi.stubGlobal('matchMedia', media(false))
    const wrapper = await openSelect()
    await vi.waitFor(() => expect(document.activeElement).toBe(document.querySelector('[data-slot="command-input"]')))
    wrapper.unmount()
  })
})
