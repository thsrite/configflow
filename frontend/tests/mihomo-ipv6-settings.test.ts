import { afterEach, describe, expect, it } from 'vitest'
import { shallowMount, type VueWrapper } from '@vue/test-utils'
import { parse } from 'yaml'
import MihomoIPv6Settings from '@/components/generate/MihomoIPv6Settings.vue'
import MihomoIPv6Readiness from '@/components/generate/MihomoIPv6Readiness.vue'
import MihomoIPv6Steps from '@/components/generate/MihomoIPv6Steps.vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent } from '@/components/ui/dialog'

const wrappers: VueWrapper[] = []
const mount = (modelValue = 'ipv6: true\ndns:\n  ipv6: false\n', extra = {}) => {
  const wrapper = shallowMount(MihomoIPv6Settings, {
    props: { open: true, modelValue, profileId: 'bound', preferredAgentId: 'entry-device', ...extra },
    global: { renderStubDefaultSlot: true, stubs: { Button: false, Primitive: false } },
  })
  wrappers.push(wrapper)
  return wrapper
}
const selector = (wrapper: VueWrapper) => wrapper.getComponent(RadioChips)
const choose = async (wrapper: VueWrapper, mode: string) => {
  selector(wrapper).vm.$emit('update:modelValue', mode)
  await wrapper.vm.$nextTick()
  return wrapper.emitted('update:modelValue')?.at(-1)?.[0] as string
}
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('Mihomo IPv6 mode selector', () => {
  it('offers the three modes and reflects handwritten YAML in both directions', async () => {
    const wrapper = mount()
    expect(selector(wrapper).props('options').map((option: { label: string }) => option.label)).toEqual(['关闭', 'Mihomo 用 IPv6，客户端走 IPv4', '客户端也走 IPv6'])
    // Radio chips like the Agent install dialog, named for assistive technology.
    expect(selector(wrapper).attributes('aria-label')).toBe('IPv6 模式')
    expect(selector(wrapper).props('modelValue')).toBe('nodes')
    const changed = await choose(wrapper, 'clients')
    expect(parse(changed)).toEqual({ ipv6: true, dns: { ipv6: true } })
    await wrapper.setProps({ modelValue: changed })
    expect(selector(wrapper).props('modelValue')).toBe('clients')
    await wrapper.setProps({ modelValue: 'ipv6: false\n' })
    expect(selector(wrapper).props('modelValue')).toBe('off')
    expect(wrapper.get('[data-ipv6-mode-description]').text()).toContain('Mihomo 不使用 IPv6')
  })

  it('gives the full mode name in the description and says the upstream routes stay published', () => {
    const wrapper = mount()
    const text = wrapper.get('[data-ipv6-mode-description]').text()
    expect(text).toContain('Mihomo 用 IPv6，客户端走 IPv4 分流')
    expect(text).toContain('IPv6 Fake-IP 网段和规则集里的 IPv6 网段照常发布到上游路由')
    expect(text).toContain('切换模式不用再改路由器')
  })

  it('shows a custom combination with its reason and only rewrites it when a mode is chosen', async () => {
    const source = 'ipv6: false # off\ndns:\n  ipv6: true # AAAA\n'
    const wrapper = mount(source)
    // A custom combination selects none of the three modes.
    expect(selector(wrapper).props('modelValue')).toBe('')
    expect(wrapper.get('[data-ipv6-mode-custom]').text()).toContain('自定义')
    expect(wrapper.get('[data-ipv6-mode-custom]').text()).toContain('dns.ipv6 却为 true')
    expect(wrapper.findComponent(MihomoIPv6Steps).exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const changed = await choose(wrapper, 'off')
    expect(changed).toBe('ipv6: false # off\ndns:\n  ipv6: false # AAAA\n')
  })

  it('fills a missing Fake-IP range without pointing to a fixed upstream router instruction', async () => {
    const wrapper = mount('ipv6: true\ndns:\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\ntun:\n  enable: true\n')
    const changed = await choose(wrapper, 'clients')
    expect(parse(changed).dns).toMatchObject({ ipv6: true, 'fake-ip-range6': 'fc00::1/64' })
    expect(wrapper.get('[role="status"]').text()).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    expect(wrapper.text()).not.toContain('上游路由」查看')
    expect(wrapper.text()).not.toContain('保存后到「上游路由」')
    expect(wrapper.findComponent(MihomoIPv6Readiness).exists()).toBe(false)
  })

  it('retires a mode notice once the YAML is edited by hand', async () => {
    const source = 'ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd00::1/64\n'
    const wrapper = mount(source)
    const changed = await choose(wrapper, 'off')
    await wrapper.setProps({ modelValue: changed })
    expect(wrapper.get('[role="status"]').text()).toBe('已关闭 IPv6 Fake-IP，网段保留供再次开启。')
    await wrapper.setProps({ modelValue: changed.replace('fd00::1/64', 'fd00::2/64') })
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('ignores choosing the current mode', async () => {
    const wrapper = mount('mixed-port: 7890\n')
    selector(wrapper).vm.$emit('update:modelValue', 'nodes')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).toContain('YAML 未填写 ipv6（默认开启）、dns.ipv6（默认关闭）')
  })

  it('fills and shows the range for 「Mihomo 用 IPv6，客户端走 IPv4」 with Fake-IP', async () => {
    const wrapper = mount('ipv6: false\ndns:\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\ntun:\n  enable: true\n')
    expect(wrapper.findComponent(Input).exists()).toBe(false)
    const changed = await choose(wrapper, 'nodes')
    expect(parse(changed)).toMatchObject({ ipv6: true, dns: { ipv6: false, 'fake-ip-range6': 'fc00::1/64' } })
    expect(wrapper.get('[role="status"]').text()).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    await wrapper.setProps({ modelValue: changed })
    expect(wrapper.getComponent(Input).props('modelValue')).toBe('fc00::1/64')
    // Validation guards the range in this mode as well.
    wrapper.getComponent(Input).vm.$emit('update:modelValue', '2001:db8::1/64')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('validation')?.at(-1)).toEqual([{ invalid: true, dirty: true }])
  })

  it('shows the range in both IPv6 modes with Fake-IP and reflects handwritten YAML', async () => {
    const source = 'ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd12:3456::1/64 # custom\n'
    const wrapper = mount(source)
    expect(wrapper.getComponent(Input).props('modelValue')).toBe('fd12:3456::1/64')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ modelValue: source.replace('fd12:3456', 'fd56:789a') })
    expect(wrapper.getComponent(Input).props('modelValue')).toBe('fd56:789a::1/64')
    await wrapper.setProps({ modelValue: source.replace('  ipv6: true', '  ipv6: false') })
    expect(wrapper.getComponent(Input).props('modelValue')).toBe('fd12:3456::1/64')
    await wrapper.setProps({ modelValue: source.replace('ipv6: true\ndns', 'ipv6: false\ndns') })
    expect(wrapper.findComponent(Input).exists()).toBe(false)
    await wrapper.setProps({ modelValue: source.replace('fake-ip\n', 'redir-host\n') })
    expect(wrapper.findComponent(Input).exists()).toBe(false)
  })

  it('retains invalid input without writing YAML and clears validation when corrected', async () => {
    const source = 'ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fc00::1/64 # keep\n'
    const wrapper = mount(source)
    const input = wrapper.getComponent(Input)
    input.vm.$emit('update:modelValue', 'fe80::1/64')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(input.props('modelValue')).toBe('fe80::1/64')
    expect(wrapper.get('[role="alert"]').text()).toContain('内网网段')
    expect(wrapper.emitted('validation')?.at(-1)).toEqual([{ invalid: true, dirty: true }])
    input.vm.$emit('update:modelValue', 'fd12:3456::1/64')
    await wrapper.vm.$nextTick()
    const changed = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as string
    expect(changed).toContain('fake-ip-range6: fd12:3456::1/64 # keep')
    expect(wrapper.emitted('validation')?.at(-1)).toEqual([{ invalid: false, dirty: false }])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    await wrapper.setProps({ modelValue: changed })
    input.vm.$emit('update:modelValue', '')
    await wrapper.vm.$nextTick()
    await wrapper.setProps({ open: false })
    expect(wrapper.emitted('validation')?.at(-1)).toEqual([{ invalid: false, dirty: false }])
  })

  it('locks a shared range and explains how to edit its source', () => {
    const source = 'range: &range fd12::1/64\nipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: *range\n'
    const wrapper = mount(source)
    expect(wrapper.getComponent(Input).attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('下方 YAML')
    wrapper.getComponent(Input).vm.$emit('update:modelValue', 'fd56::1/64')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('disables only the modes that would rewrite a shared value', async () => {
    const wrapper = mount('base: &base\n  ipv6: false\ndns: *base\nipv6: true\n')
    expect(selector(wrapper).props('options').map((option: { disabled: boolean }) => option.disabled)).toEqual([false, false, true])
    expect(wrapper.get('[data-ipv6-mode-description]').text()).toContain('别名')
    selector(wrapper).vm.$emit('update:modelValue', 'clients')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[role="alert"]').text()).toContain('别名')
  })

  it('keeps malformed values visible and blocks accidental replacement', () => {
    const wrapper = mount('ipv6: "true"\ndns: false\n')
    expect(selector(wrapper).props('disabled')).toBe(true)
    expect(selector(wrapper).props('modelValue')).toBe('')
    expect(wrapper.text()).toContain('不要加引号')
    expect(wrapper.text()).toContain('dns 必须是 YAML 对象')
    expect(wrapper.findComponent(MihomoIPv6Steps).exists()).toBe(false)
  })

  it('lists steps for both IPv6 modes but not when IPv6 is off', async () => {
    const wrapper = mount(undefined, { dirty: true, refreshKey: 3 })
    expect(wrapper.getComponent(MihomoIPv6Steps).props()).toMatchObject({ open: true, profileId: 'bound', preferredAgentId: 'entry-device', mode: 'nodes', dirty: true, refreshKey: 3 })
    await wrapper.setProps({ modelValue: 'ipv6: true\ndns:\n  ipv6: true\n' })
    expect(wrapper.getComponent(MihomoIPv6Steps).props('mode')).toBe('clients')
    await wrapper.setProps({ modelValue: 'ipv6: false\n' })
    expect(wrapper.findComponent(MihomoIPv6Steps).exists()).toBe(false)
  })

  it('opens the device check for the step target and refreshes the steps after closing it', async () => {
    const wrapper = mount(undefined, { preferredAgentId: undefined, dirty: true })
    expect(wrapper.findComponent(MihomoIPv6Readiness).exists()).toBe(false)
    wrapper.getComponent(MihomoIPv6Steps).vm.$emit('check', 'picked-device')
    await wrapper.vm.$nextTick()
    expect(wrapper.getComponent(MihomoIPv6Readiness).props()).toMatchObject({ open: true, profileId: 'bound', preferredAgentId: 'picked-device', dirty: true })
    const before = wrapper.getComponent(MihomoIPv6Steps).props('recheckKey')
    wrapper.getComponent(Dialog).vm.$emit('update:open', false)
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent(MihomoIPv6Readiness).exists()).toBe(false)
    expect(wrapper.getComponent(MihomoIPv6Steps).props('recheckKey')).toBe(before + 1)
    expect(wrapper.getComponent(MihomoIPv6Steps).props('refreshKey')).toBeUndefined()
    await wrapper.setProps({ profileId: 'other-profile' })
    expect(wrapper.findComponent(MihomoIPv6Readiness).exists()).toBe(false)
  })

  it('keeps the fixed entry device when a step opens the check', async () => {
    const wrapper = mount()
    wrapper.getComponent(MihomoIPv6Steps).vm.$emit('check', 'other-device')
    await wrapper.vm.$nextTick()
    expect(wrapper.getComponent(MihomoIPv6Readiness).props('preferredAgentId')).toBe('entry-device')
  })

  it('keeps the check window open while busy and returns without changing the YAML', async () => {
    const wrapper = mount()
    const dialog = wrapper.getComponent(Dialog)
    wrapper.getComponent(MihomoIPv6Steps).vm.$emit('check', 'entry-device')
    await wrapper.vm.$nextTick()
    const readiness = wrapper.getComponent(MihomoIPv6Readiness)
    readiness.vm.$emit('busy', true)
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([true])
    expect(wrapper.getComponent(MihomoIPv6Steps).props('disabled')).toBe(true)
    expect(selector(wrapper).props('disabled')).toBe(true)
    dialog.vm.$emit('update:open', false)
    const escape = new Event('keydown', { cancelable: true })
    const outside = new Event('pointerdown', { cancelable: true })
    const content = wrapper.getComponent(DialogContent)
    content.vm.$emit('escapeKeyDown', escape)
    content.vm.$emit('interactOutside', outside)
    await wrapper.vm.$nextTick()
    expect(escape.defaultPrevented).toBe(true)
    expect(outside.defaultPrevented).toBe(true)
    expect(dialog.props('open')).toBe(true)
    expect(content.props('showCloseButton')).toBe(false)
    readiness.vm.$emit('busy', false)
    await wrapper.vm.$nextTick()
    dialog.vm.$emit('update:open', false)
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent(MihomoIPv6Readiness).exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('busy')?.at(-1)).toEqual([false])
  })

  it('returns focus to the step button that opened the check', async () => {
    const wrapper = mount()
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    wrapper.getComponent(MihomoIPv6Steps).vm.$emit('check', 'entry-device')
    await wrapper.vm.$nextTick()
    opener.blur()
    const event = new Event('focusout', { cancelable: true })
    wrapper.getComponent(DialogContent).vm.$emit('closeAutoFocus', event)
    expect(event.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(opener)
    opener.remove()
  })

  it('uses the full server default only when the user edits an empty document', async () => {
    const defaultConfig = 'mixed-port: 7890\ndns:\n  enable: true\n  nameserver: [223.5.5.5]\n'
    const wrapper = mount('', { defaultConfig })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const changed = await choose(wrapper, 'clients')
    expect(parse(changed)).toEqual({ ...parse(defaultConfig), dns: { ...parse(defaultConfig).dns, ipv6: true } })
  })
})
