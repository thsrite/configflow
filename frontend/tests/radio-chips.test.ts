import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import RadioChips from '@/components/common/RadioChips.vue'

const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))
const options = [{ value: 'a', label: '关闭' }, { value: 'b', label: 'Mihomo 用 IPv6，客户端走 IPv4', disabled: true }, { value: 'c', label: '客户端也走 IPv6' }]
const render = (props: Record<string, unknown> = {}, attrs: Record<string, unknown> = {}) => {
  const wrapper = mount(RadioChips, {
    props: { modelValue: 'a', options, 'onUpdate:modelValue': (value: string) => wrapper.setProps({ modelValue: value }), ...props },
    attrs: { 'aria-label': 'IPv6 模式', ...attrs },
    attachTo: document.body,
  })
  wrappers.push(wrapper)
  return wrapper
}
const radios = (wrapper: VueWrapper) => wrapper.findAll('[role="radio"]')
const checked = (wrapper: VueWrapper) => radios(wrapper).map(item => item.attributes('aria-checked'))

describe('RadioChips', () => {
  it('renders a named radio group with one chip per option, like the Agent install dialog', () => {
    const wrapper = render()
    const group = wrapper.get('[role="radiogroup"]')
    expect(group.attributes('aria-label')).toBe('IPv6 模式')
    expect(group.classes()).toEqual(expect.arrayContaining(['flex', 'flex-wrap', 'gap-2']))
    const chips = wrapper.findAll('label')
    expect(chips.map(item => item.text())).toEqual(options.map(item => item.label))
    expect(radios(wrapper).map(item => item.attributes('value'))).toEqual(['a', 'b', 'c'])
    expect(checked(wrapper)).toEqual(['true', 'false', 'false'])
    // The exact classes of the install dialog's chips (44px touch target included).
    expect(chips[0].classes()).toEqual(expect.arrayContaining(['flex', 'min-h-11', 'cursor-pointer', 'items-center', 'gap-2', 'rounded-lg', 'border', 'px-3', 'py-2', 'text-[13px]', 'transition-colors',
      'border-primary-accent', 'bg-primary-soft', 'text-primary-accent', 'dark:border-primary-accent/40', 'dark:bg-primary-soft/50', 'dark:text-foreground']))
    expect(chips[2].classes()).toEqual(expect.arrayContaining(['cursor-pointer', 'border-input', 'bg-card', 'text-muted-foreground', 'hover:border-border-strong', 'dark:border-border/50', 'dark:bg-background/40']))
    expect(chips[2].classes()).not.toContain('bg-primary-soft')
  })

  it('updates v-model when a chip is clicked', async () => {
    const wrapper = render()
    await wrapper.findAll('label')[2].trigger('click')
    await radios(wrapper)[2].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['c'])
    expect(checked(wrapper)).toEqual(['false', 'false', 'true'])
    expect(wrapper.findAll('label')[2].classes()).toContain('bg-primary-soft')
  })

  it('selects nothing for a value outside the options', () => {
    const wrapper = render({ modelValue: '' })
    expect(checked(wrapper)).toEqual(['false', 'false', 'false'])
    expect(wrapper.findAll('label').some(item => item.classes().includes('bg-primary-soft'))).toBe(false)
  })

  it('disables a single option and dims only its chip', async () => {
    const wrapper = render()
    expect(radios(wrapper).map(item => item.attributes('disabled') !== undefined)).toEqual([false, true, false])
    const chips = wrapper.findAll('label')
    expect(chips[1].classes()).toEqual(expect.arrayContaining(['cursor-not-allowed', 'opacity-50']))
    expect(chips[1].classes()).not.toContain('hover:border-border-strong')
    expect(chips[0].classes()).not.toContain('opacity-50')
    await radios(wrapper)[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('disables the whole group', async () => {
    const wrapper = render({ disabled: true })
    expect(radios(wrapper).every(item => item.attributes('disabled') !== undefined)).toBe(true)
    expect(wrapper.findAll('label').every(item => item.classes().includes('opacity-50'))).toBe(true)
    await radios(wrapper)[2].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('leaves a disabled option out of the arrow-key order', () => {
    // Arrow keys come from reka-ui RovingFocus; jsdom cannot move focus with it, so the browser check presses the keys.
    const wrapper = render()
    expect(radios(wrapper).map(item => item.attributes('data-disabled') !== undefined)).toEqual([false, true, false])
  })

  it('replaces the layout and adds item classes when given', () => {
    const wrapper = render({ class: 'grid grid-cols-3 gap-2 sm:flex', itemClass: 'justify-center max-sm:px-2' })
    const group = wrapper.get('[role="radiogroup"]')
    expect(group.classes()).toEqual(['grid', 'grid-cols-3', 'gap-2', 'sm:flex'])
    expect(wrapper.findAll('label').every(item => item.classes().includes('justify-center') && item.classes().includes('max-sm:px-2'))).toBe(true)
  })
})
