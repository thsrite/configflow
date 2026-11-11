import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import PageHeader from '@/components/common/PageHeader.vue'
import { CURRENT_NAV_PATH } from '@/navigation'

const media = (phone: boolean) => vi.fn((query: string) => ({
  matches: phone && /max-width: (639|767)px/.test(query), media: query, onchange: null,
  addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false
}))

function render(path: string | null, phone: boolean, props: Record<string, unknown> = {}) {
  vi.stubGlobal('matchMedia', media(phone))
  return mount(PageHeader, {
    props: { title: '策略规则', ...props },
    slots: { actions: '<button>添加规则</button>' },
    global: { provide: path === null ? {} : { [CURRENT_NAV_PATH as symbol]: ref(path) } }
  })
}

afterEach(() => { vi.unstubAllGlobals() })

describe('page title on a phone', () => {
  it('keeps nav page titles for screen readers only, because the group button and tab bar already name the page', () => {
    const h1 = render('/rules', true).get('h1')
    expect(h1.text()).toBe('策略规则')
    expect(h1.classes()).toContain('sr-only')
  })

  it('shows the title on pages outside the navigation, on wider screens and outside the app shell', () => {
    expect(render('/setup', true).get('h1').classes()).not.toContain('sr-only')
    expect(render('/rules', false).get('h1').classes()).not.toContain('sr-only')
    expect(render(null, true).get('h1').classes()).not.toContain('sr-only')
  })

  it('moves the page help next to the actions instead of leaving it on its own row', () => {
    const wrapper = render('/client-devices', true, { title: '客户端设备', mobileHelp: '说明' })
    expect(wrapper.find('.cf-page-heading').exists()).toBe(false)
    const actions = wrapper.get('.cf-page-actions')
    expect(actions.find('.cf-compact-help').exists()).toBe(true)
    expect(actions.text()).toContain('添加规则')
  })
})
