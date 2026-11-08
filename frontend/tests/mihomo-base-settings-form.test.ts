import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, shallowMount, type VueWrapper } from '@vue/test-utils'
import { parse } from 'yaml'
import MihomoBaseSettings from '@/components/generate/MihomoBaseSettings.vue'
import MihomoBaseField from '@/components/generate/MihomoBaseField.vue'
import MihomoBaseCell from '@/components/generate/MihomoBaseCell.vue'
import MihomoBaseGroup from '@/components/generate/MihomoBaseGroup.vue'
import MihomoControllerSettings from '@/components/generate/MihomoControllerSettings.vue'
import MihomoDnsPresetSettings from '@/components/generate/MihomoDnsPresetSettings.vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Input } from '@/components/ui/input'
import { ADVANCED_FIELDS, PORT_FIELDS, logLevelField } from '@/lib/mihomoBaseCopy'
import { readDnsPreset, readMihomoBase } from '@/lib/mihomoBaseSettings'

const template: string = JSON.parse(readFileSync(resolve(process.cwd(), '../backend/config_template.json'), 'utf8')).mihomo.custom_config
const wrappers: VueWrapper[] = []
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

function form(modelValue = template, extra: Record<string, unknown> = {}) {
  const wrapper = shallowMount(MihomoBaseSettings, {
    props: { modelValue, savedContent: template, open: true, context: { takeover: null, reserved_ports: [], local_addresses: [] }, ...extra,
      'onUpdate:modelValue': (value: string) => wrapper.setProps({ modelValue: value }) },
    global: { renderStubDefaultSlot: true },
  })
  wrappers.push(wrapper)
  return wrapper
}
const lastValidation = (wrapper: VueWrapper) => wrapper.emitted('validation')?.at(-1)?.[0] as { invalid: boolean; dirty: boolean }
const field = (wrapper: VueWrapper, id: string) => wrapper.findAllComponents(MihomoBaseField).find(item => item.props('field').path.join('.') === id)!

describe('Mihomo base settings form', () => {
  it('lays every group out as one sheet with all values in view, the read-only group last', () => {
    const wrapper = form()
    const sections = wrapper.findAllComponents(MihomoBaseGroup)
    expect(sections.map(item => item.props('title'))).toEqual(['控制面板', '代理端口', '局域网访问', 'DNS 上游', 'Fake-IP 过滤', '域名嗅探', '进程匹配与日志', '进阶', '接管与 DNS 监听'])
    expect(sections.map(item => item.props('readonly'))).toEqual([...Array(8).fill(false), true])
    // Nothing is folded: every field of the sheet is rendered up front.
    expect(wrapper.findAllComponents(MihomoBaseField).map(item => item.props('field').path.join('.'))).toEqual(expect.arrayContaining(['mixed-port', 'log-level', 'tun.mtu', 'global-ua']))
    const advanced = ADVANCED_FIELDS.map(item => item.path.join('.'))
    expect(advanced).toEqual(['tun.stack', 'tun.mtu', 'unified-delay', 'tcp-concurrent', 'global-client-fingerprint', 'keep-alive-idle',
      'keep-alive-interval', 'disable-keep-alive', 'profile.store-selected', 'profile.store-fake-ip', 'global-ua'])
    // Every option explains itself.
    for (const item of wrapper.findAllComponents(MihomoBaseField)) expect(item.props('field').help).toMatch(/【作用】[\s\S]+【默认 \/ 推荐】[\s\S]+【修改后】/)
  })

  it('writes a form change into the YAML and follows edits made in the YAML', async () => {
    const wrapper = form()
    field(wrapper, 'log-level').vm.$emit('change', 'debug')
    await wrapper.vm.$nextTick()
    const changed = wrapper.props('modelValue') as string
    expect(parse(changed)['log-level']).toBe('debug')
    expect(changed).toContain('# 控制面板')
    await wrapper.setProps({ modelValue: changed.replace('log-level: debug', 'log-level: error') })
    expect(field(wrapper, 'log-level').props('state').value).toBe('error')
  })

  it('blocks saving for a problem the edit introduced and only warns about saved ones', async () => {
    const wrapper = form()
    expect(lastValidation(wrapper)).toEqual({ invalid: false, dirty: false })
    expect(wrapper.findAllComponents(MihomoBaseGroup)[0].props('warnings')).toBe(1)
    field(wrapper, 'socks-port').vm.$emit('change', 7893)
    await wrapper.vm.$nextTick()
    expect(lastValidation(wrapper)).toEqual({ invalid: true, dirty: false })
    expect(wrapper.get('[data-base-blocking]').text()).toContain('2 处需要修正')
    expect(wrapper.findAllComponents(MihomoBaseGroup)[1].props('blocking')).toBe(2)
    expect(field(wrapper, 'socks-port').props('issues')[0]).toMatchObject({ blocking: true, message: '端口 7893 同时被 mixed-port 和 socks-port 使用，请改用不同端口' })
  })

  it('reports Agent port conflicts from the server context', async () => {
    const wrapper = form(template, { context: { takeover: null, reserved_ports: [{ port: 8080, owner: 'Mihomo Agent「box」的端口' }], local_addresses: [] } })
    field(wrapper, 'mixed-port').vm.$emit('change', 8080)
    await wrapper.vm.$nextTick()
    expect(field(wrapper, 'mixed-port').props('issues')[0].message).toContain('Mihomo Agent「box」')
  })

  it('switches an optional port on with a number nothing else uses', () => {
    const wrapper = form('mixed-port: 7891\nexternal-controller: 127.0.0.1:7892\n', { context: { reserved_ports: [{ port: 7893, owner: 'Agent' }] } })
    expect(field(wrapper, 'port').props('toggle')).toBe(7894)
    expect(field(wrapper, 'mixed-port').props('state').value).toBe(7891)
  })

  it('treats typed but unparsed numbers as pending input that blocks saving', async () => {
    const wrapper = form()
    field(wrapper, 'tun.mtu').vm.$emit('draft', 'TUN MTU（tun.mtu）需为整数')
    await wrapper.vm.$nextTick()
    expect(lastValidation(wrapper)).toEqual({ invalid: true, dirty: true })
  })

  it('hides the sheet without touching unreadable YAML, and locks it when the editor is disabled', async () => {
    const wrapper = form('dns: [\n')
    expect(wrapper.get('[data-base-yaml-error]').text()).toContain('表单已停用')
    // No current values can be read, so none are shown (defaults would look like settings).
    expect(wrapper.find('[data-base-sheet]').exists()).toBe(false)
    expect(wrapper.findAllComponents(MihomoBaseField)).toHaveLength(0)
    expect(wrapper.props('modelValue')).toBe('dns: [\n')
    expect(lastValidation(wrapper).invalid).toBe(false)
    const disabled = form(template, { disabled: true })
    expect(disabled.get('[data-base-sheet]').attributes()).toMatchObject({ inert: expect.anything(), 'aria-disabled': 'true' })
    field(disabled, 'log-level').vm.$emit('change', 'debug')
    await disabled.vm.$nextTick()
    expect(disabled.props('modelValue')).toBe(template)
  })

  it('shows TUN and DNS listener keys read-only, with who manages them', async () => {
    const wrapper = form()
    const managed = wrapper.get('[data-base-managed]')
    expect(managed.get('[data-base-managed-field="dns.listen"] [data-base-managed-value]').text()).toBe('0.0.0.0:1053')
    expect(managed.get('[data-base-managed-field="tun.dns-hijack"] [data-base-managed-value]').text()).toBe('any:53, tcp://any:53')
    expect(managed.findAll('input, button[role="switch"], textarea')).toHaveLength(0)
    expect(wrapper.get('[data-base-managed-note]').text()).toContain('只显示不修改')
    await wrapper.setProps({ context: { takeover: 'linux_gateway' } })
    expect(wrapper.get('[data-base-managed-note]').text()).toContain('Linux 网关')
    await wrapper.setProps({ context: { takeover: 'full_tun' } })
    expect(wrapper.get('[data-base-managed-note]').text()).toContain('完整 TUN')
  })

  it('switches DNS presets while keeping the user policy entry', async () => {
    const wrapper = form()
    wrapper.getComponent(MihomoDnsPresetSettings).vm.$emit('preset', 'split')
    await wrapper.vm.$nextTick()
    const policy = parse(wrapper.props('modelValue') as string).dns['nameserver-policy']
    expect(Object.keys(policy)).toEqual(['+.115.com', 'geosite:cn,private'])
    expect(readDnsPreset(readMihomoBase(wrapper.props('modelValue') as string)).preset).toBe('split')
  })

  it('picks the DNS preset in a menu, keeps the upstreams editable beside it and locks both when dns is not an object', async () => {
    const preset = (source: string) => {
      const wrapper = shallowMount(MihomoDnsPresetSettings, { props: { view: readMihomoBase(source), issues: () => [] }, global: { renderStubDefaultSlot: true } })
      wrappers.push(wrapper)
      return wrapper
    }
    const wrapper = preset(template)
    const chips = wrapper.getComponent(RadioChips)
    expect(chips.attributes('aria-label')).toBe('DNS 方案')
    expect(chips.props('options')).toEqual([{ value: 'domestic', label: '国内 DoH', disabled: false }, { value: 'split', label: '国内外分开解析', disabled: false },
      { value: 'custom', label: '自定义', disabled: true }])
    expect(chips.props('modelValue')).toBe('domestic')
    expect(wrapper.get('[data-base-dns-preset]').text()).toContain('国内 DoH')
    expect(wrapper.get('[data-base-dns-preset]').attributes('disabled')).toBeUndefined()
    chips.vm.$emit('update:modelValue', 'split')
    expect(wrapper.emitted('preset')).toEqual([['split']])
    chips.vm.$emit('update:modelValue', 'custom')
    expect(wrapper.emitted('preset')).toHaveLength(1)
    expect(wrapper.findAllComponents(MihomoBaseField).map(item => item.props('field').path.join('.'))).toEqual(['dns.default-nameserver', 'dns.nameserver',
      'dns.proxy-server-nameserver', 'dns.direct-nameserver', 'dns.respect-rules', 'dns.direct-nameserver-follow-policy'])
    const locked = preset('dns: false\n')
    expect(locked.get('[data-base-dns-preset]').attributes('disabled')).toBeDefined()
    expect(locked.getComponent(MihomoBaseCell).props('status')).toContain('dns 必须是 YAML 对象')
    expect(locked.findAllComponents(MihomoBaseField).every(item => item.props('disabled'))).toBe(true)
  })

  it('fills the common sniff protocols when sniffing is turned on for the first time', async () => {
    const wrapper = form('sniffer:\n  enable: false\n')
    field(wrapper, 'sniffer.enable').vm.$emit('change', true)
    await wrapper.vm.$nextTick()
    expect(parse(wrapper.props('modelValue') as string).sniffer).toEqual({ enable: true,
      sniff: { HTTP: { ports: [80, '8080-8880'], 'override-destination': true }, TLS: { ports: [443, 8443] }, QUIC: { ports: [443, 8443] } } })
    field(wrapper, 'sniffer.enable').vm.$emit('change', false)
    await wrapper.vm.$nextTick()
    expect(parse(wrapper.props('modelValue') as string).sniffer.enable).toBe(false)
  })
})

describe('Controller settings', () => {
  const states = (source: string) => {
    const view = readMihomoBase(source)
    return { secret: view.field(['secret']), controller: view.field(['external-controller']), ui: view.field(['external-ui']), uiUrl: view.field(['external-ui-url']) }
  }
  const controller = (source = template) => {
    const wrapper = shallowMount(MihomoControllerSettings, { props: { ...states(source), issues: () => [] },
      global: { renderStubDefaultSlot: true, stubs: { Button: false, Primitive: false } } })
    wrappers.push(wrapper)
    return wrapper
  }

  const cell = (wrapper: VueWrapper, id: string) => wrapper.findAllComponents(MihomoBaseCell).find(item => item.attributes('data-base-field') === id)!

  it('keeps the password hidden until asked, and removes an emptied one', async () => {
    const wrapper = controller()
    const input = wrapper.get<HTMLInputElement>('#mihomo-base-secret')
    expect(input.element.value).toBe('123456')
    expect(input.attributes('type')).toBe('password')
    // 123456 is reported once, by the check under the field, not again by the status line.
    expect(cell(wrapper, 'secret').props('status')).toBe('')
    expect(cell(controller('mixed-port: 7890\n'), 'secret').props('status')).toContain('未设置密码')
    await wrapper.get('button[aria-label="显示控制面板密码"]').trigger('click')
    expect(wrapper.get('#mihomo-base-secret').attributes('type')).toBe('text')
    await input.setValue('')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([['secret'], undefined])
  })

  it('maps the listen choices to external-controller and keeps the port', async () => {
    const wrapper = controller()
    const scope = wrapper.getComponent(RadioChips)
    expect(scope.props('modelValue')).toBe('lan')
    expect(wrapper.get('[data-base-controller-scope]').text()).toContain('局域网可访问')
    expect(wrapper.get('[data-base-controller-note]').text()).toContain('靠密码保护')
    scope.vm.$emit('update:modelValue', 'local')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([['external-controller'], '127.0.0.1:9090'])
    scope.vm.$emit('update:modelValue', 'custom')
    await wrapper.vm.$nextTick()
    wrapper.getComponent(Input).vm.$emit('update:modelValue', '::')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([['external-controller'], '[::]:9090'])
    await wrapper.get('#mihomo-base-controller-port').setValue('90x')
    expect(wrapper.emitted('draft')?.at(-1)).toEqual(['external-controller', '控制面板端口需为整数'])
  })

  it('warns that a missing controller leaves the Agent blind and turns the web UI on with a download address', async () => {
    const wrapper = controller('mixed-port: 7890\n')
    // The warning stays in view instead of inside the menu.
    expect(cell(wrapper, 'external-controller').props('status')).toContain('Agent 无法读取连接和流量')
    expect(wrapper.get('[data-base-controller-scope]').text()).toContain('未设置')
    expect(wrapper.find('[data-base-controller-note]').exists()).toBe(false)
    // Nothing selected until a listen choice writes the key.
    expect(wrapper.getComponent(RadioChips).props('modelValue')).toBe('')
    wrapper.findAllComponents({ name: 'Switch' }).at(-1)!.vm.$emit('update:modelValue', true)
    expect(wrapper.emitted('change')).toEqual([[['external-ui'], 'ui'], [['external-ui-url'], 'https://github.com/Zephyruso/zashboard/archive/refs/heads/gh-pages.zip']])
  })
})

describe('Base field inputs', () => {
  const portField = PORT_FIELDS.find(item => item.key === 'port')!
  it('switches an optional port on and off and rejects non-numbers without writing them', async () => {
    const view = readMihomoBase('mixed-port: 7890\n')
    const wrapper = mount(MihomoBaseField, { props: { field: portField, state: view.field(['port']), toggle: 7891 } })
    wrappers.push(wrapper)
    expect(wrapper.get('input').attributes()).toMatchObject({ placeholder: '未启用', disabled: '' })
    wrapper.findComponent({ name: 'Switch' }).vm.$emit('update:modelValue', true)
    expect(wrapper.emitted('change')?.at(-1)).toEqual([7891])
    await wrapper.setProps({ state: readMihomoBase('port: 7891\n').field(['port']) })
    await wrapper.get('input[inputmode="numeric"]').setValue('78a')
    expect(wrapper.emitted('draft')?.at(-1)).toEqual([`${portField.label}需为整数`])
    expect(wrapper.emitted('change')).toHaveLength(1)
    expect(wrapper.get('[role="alert"]').text()).toContain('需为整数')
  })

  it('puts back the value it had when focused on Esc, without closing the workspace', async () => {
    const wrapper = mount(MihomoBaseField, { props: { field: portField, state: readMihomoBase('port: 7891\n').field(['port']), toggle: 7891 } })
    wrappers.push(wrapper)
    const input = wrapper.get('input[inputmode="numeric"]')
    await input.trigger('focus')
    await input.setValue('7895')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([7895])
    const esc = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    const outer = vi.fn()
    wrapper.element.parentElement?.addEventListener('keydown', outer)
    input.element.dispatchEvent(esc)
    expect(wrapper.emitted('change')?.at(-1)).toEqual([7891])
    expect(outer).not.toHaveBeenCalled()
  })

  it('shows the value in a short cell label and keeps the YAML key in the help text', () => {
    const wrapper = shallowMount(MihomoBaseField, { props: { field: portField, state: readMihomoBase('port: 7891\n').field(['port']) }, global: { renderStubDefaultSlot: true } })
    wrappers.push(wrapper)
    const cellStub = wrapper.getComponent(MihomoBaseCell)
    expect(cellStub.props('label')).toBe('HTTP 代理端口')
    expect(cellStub.props('help')).toMatch(/^YAML 键：port\n【作用】/)
  })

  it('picks an option from the menu, including taking an optional key out', () => {
    const fingerprint = ADVANCED_FIELDS.find(item => item.path.join('.') === 'global-client-fingerprint')!
    const pick = (field: typeof logLevelField, source: string) => {
      const wrapper = shallowMount(MihomoBaseField, { props: { field, state: readMihomoBase(source).field(field.path) }, global: { renderStubDefaultSlot: true } })
      wrappers.push(wrapper)
      return wrapper
    }
    const level = pick(logLevelField, 'log-level: info\n')
    expect(level.get('button').text()).toContain('info')
    level.getComponent(RadioChips).vm.$emit('update:modelValue', 'debug')
    expect(level.emitted('change')).toEqual([['debug']])
    const unset = pick(fingerprint, 'global-client-fingerprint: chrome\n')
    unset.getComponent(RadioChips).vm.$emit('update:modelValue', '__unset__')
    expect(unset.emitted('change')).toEqual([[undefined]])
  })

  it('edits a list one entry per line and removes it when emptied', async () => {
    const listField = { path: ['dns', 'fake-ip-filter'], label: 'Fake-IP 过滤列表', type: 'list' as const, help: 'x' }
    const wrapper = shallowMount(MihomoBaseField, { props: { field: listField, state: readMihomoBase('dns:\n  fake-ip-filter: [a.com]\n').field(['dns', 'fake-ip-filter'], 'list') },
      global: { renderStubDefaultSlot: true, stubs: { MihomoBaseCell: false, Textarea: false } } })
    wrappers.push(wrapper)
    expect(wrapper.get('button').text()).toContain('1 条')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('a.com')
    await wrapper.get('textarea').setValue('a.com\n\n b.com \n')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([['a.com', 'b.com']])
    await wrapper.get('textarea').setValue('  \n')
    expect(wrapper.emitted('change')?.at(-1)).toEqual([undefined])
  })
})
