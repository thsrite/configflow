import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { flushPromises } from '@vue/test-utils'
import { useScrollToCurrent } from '@/lib/scrollToCurrent'
import RulePolicySelect from '@/components/rules/RulePolicySelect.vue'

afterEach(() => { vi.unstubAllGlobals(); document.body.innerHTML = '' })

describe('scroll the current option into the middle of the list on open', () => {
  it('only moves the list itself, centring the current option', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 0 })
    const open = ref(false)
    mount(defineComponent({ setup() { useScrollToCurrent(open); return () => h('div') } }), { attachTo: document.body })
    const list = document.createElement('div')
    list.dataset.slot = 'command-list'
    const item = document.createElement('div')
    item.dataset.current = 'true'
    list.append(item)
    document.body.append(list)
    Object.defineProperty(list, 'clientHeight', { value: 300 })
    Object.defineProperty(item, 'offsetHeight', { value: 40 })
    list.getBoundingClientRect = () => ({ top: 100 }) as DOMRect
    item.getBoundingClientRect = () => ({ top: 900 }) as DOMRect
    const pageScroll = vi.fn()
    item.scrollIntoView = pageScroll

    open.value = true
    await nextTick(); await nextTick()
    // 选项距列表顶部 800px，居中需要再减去 (300 - 40) / 2
    expect(list.scrollTop).toBe(800 - 130)
    expect(pageScroll).not.toHaveBeenCalled()
  })

  it('marks the chosen policy as the current option when the list opens', async () => {
    vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
    const policies = Array.from({ length: 30 }, (_, index) => `组 ${index + 1}`)
    const wrapper = mount(RulePolicySelect, { props: { modelValue: '组 25', policies, label: '策略' }, attachTo: document.body })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    const current = [...document.querySelectorAll('[data-slot="command-list"] [data-current]')]
    expect(current.map(item => item.textContent?.trim())).toEqual(['组 25'])
    wrapper.unmount()
  })
})
