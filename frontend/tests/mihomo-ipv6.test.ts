import { describe, expect, it } from 'vitest'
import { parse } from 'yaml'
import { DEFAULT_MIHOMO_FAKE_IPV6_RANGE, readMihomoFakeIPv6Range, readMihomoIPv6, readMihomoIPv6Mode, setMihomoFakeIPv6Range, setMihomoIPv6, setMihomoIPv6Mode, setMihomoIPv6WithDetails, validateMihomoFakeIPv6Range } from '@/lib/mihomoIPv6'

describe('Mihomo IPv6 YAML controls', () => {
  it('shows manually written values independently without changing the document', () => {
    const source = '# core stays off\nipv6: false\ndns:\n  ipv6: true # AAAA\n'
    expect(readMihomoIPv6(source)).toMatchObject({ core: { value: false, source: 'explicit' }, dns: { value: true, source: 'explicit' }, error: '' })
  })

  it('distinguishes absent settings and core defaults from explicit false', () => {
    expect(readMihomoIPv6('mixed-port: 7890\n')).toMatchObject({ core: { value: true, source: 'default' }, dns: { value: false, source: 'default' } })
    expect(readMihomoIPv6('ipv6: false\ndns: { ipv6: false }\n')).toMatchObject({ core: { value: false, source: 'explicit' }, dns: { value: false, source: 'explicit' } })
  })

  it('changes only the chosen setting, retaining comments, quoted strings and anchors', () => {
    const source = '# manual core\nipv6: false # keep core note\nservers: &servers ["1.1.1.1"]\ndns:\n  ipv6: false # keep DNS note\n  nameserver: *servers\nsecret: "00123"\n'
    const next = setMihomoIPv6(source, 'core', true)
    expect(next).toContain('# manual core')
    expect(next).toContain('ipv6: true # keep core note')
    expect(next).toContain('ipv6: false # keep DNS note')
    expect(next).toContain('&servers')
    expect(next).toContain('*servers')
    expect(parse(next)).toEqual({ ...parse(source), ipv6: true })
    const dns = setMihomoIPv6(next, 'dns', true)
    expect(parse(dns)).toMatchObject({ ipv6: true, dns: { ipv6: true, nameserver: ['1.1.1.1'] }, secret: '00123' })
  })

  it('adds a nested DNS flag without losing existing DNS options', () => {
    const source = 'mixed-port: 7890\ndns:\n  listen: "0.0.0.0:1053"\n  nameserver: [223.5.5.5]\n'
    expect(parse(setMihomoIPv6(source, 'dns', true))).toEqual({ ...parse(source), dns: { ...parse(source).dns, ipv6: true } })
    expect(parse(setMihomoIPv6('mixed-port: 7890\n', 'dns', true))).toEqual({ 'mixed-port': 7890, dns: { ipv6: true } })
  })

  it('materializes the actual server default before editing an empty configuration', () => {
    const fallback = 'mixed-port: 7890\ndns:\n  enable: true\n  listen: "0.0.0.0:53"\n  nameserver: [223.5.5.5]\n'
    expect(readMihomoIPv6('', fallback).core).toMatchObject({ value: true, source: 'default' })
    expect(parse(setMihomoIPv6('  \n', 'core', false, fallback))).toEqual({ ...parse(fallback), ipv6: false })
    expect(() => setMihomoIPv6('', 'core', false)).toThrow()
    // A comment-only custom document is invalid in the backend, not an empty fallback.
    expect(readMihomoIPv6('# nothing\n', fallback).error).not.toBe('')
  })

  it.each(['ipv6: "true"\n', 'ipv6: ture\n', 'ipv6: 1\n', 'ipv6: null\n'])('does not silently repair malformed values: %s', source => {
    expect(readMihomoIPv6(source).core).toMatchObject({ value: null, source: 'invalid', editable: false })
    expect(() => setMihomoIPv6(source, 'core', true)).toThrow()
  })

  it.each(['[true, false]\n', 'dns: [\n', 'ipv6: true\nipv6: false\n'])('blocks automatic edits of invalid documents: %s', source => {
    expect(readMihomoIPv6(source).error).not.toBe('')
    expect(() => setMihomoIPv6(source, 'dns', true)).toThrow()
  })

  it('does not replace a non-mapping DNS configuration', () => {
    const source = 'ipv6: true\ndns: false\n'
    expect(readMihomoIPv6(source).dns.editable).toBe(false)
    expect(() => setMihomoIPv6(source, 'dns', true)).toThrow('dns 必须')
  })

  it('reads aliases without mutating their shared source', () => {
    const source = 'flag: &flag true\nipv6: *flag\nbase: &base\n  ipv6: true\ndns: *base\n'
    expect(readMihomoIPv6(source)).toMatchObject({ core: { value: true }, dns: { value: true, editable: false } })
    const next = parse(setMihomoIPv6(source, 'core', false))
    expect(next.flag).toBe(true)
    expect(next.ipv6).toBe(false)
    expect(() => setMihomoIPv6(source, 'dns', false)).toThrow('别名')
  })

  it('blocks changing an IPv6 scalar anchor referenced by another setting', () => {
    const source = 'ipv6: &flag false\nother: *flag\ndns: { ipv6: false }\n'
    expect(readMihomoIPv6(source).core).toMatchObject({ value: false, editable: false })
    expect(readMihomoIPv6(source).dns.editable).toBe(true)
    expect(() => setMihomoIPv6(source, 'core', true)).toThrow('锚点')
    const nested = 'ipv6: true\ndns:\n  ipv6: &flag false\nother: *flag\n'
    expect(readMihomoIPv6(nested).dns.editable).toBe(false)
    expect(() => setMihomoIPv6(nested, 'dns', true)).toThrow('锚点')
  })

  it('blocks changing a shared DNS mapping while allowing unreferenced anchors', () => {
    const source = 'ipv6: true\ndns: &dns\n  ipv6: false\nother: *dns\n'
    expect(readMihomoIPv6(source).dns).toMatchObject({ value: false, editable: false })
    expect(() => setMihomoIPv6(source, 'dns', true)).toThrow('锚点')
    const unused = 'ipv6: &flag false\ndns: &dns\n  ipv6: false\n'
    expect(parse(setMihomoIPv6(unused, 'core', true)).ipv6).toBe(true)
    expect(parse(setMihomoIPv6(unused, 'dns', true)).dns.ipv6).toBe(true)
  })

  it('does not replace a DNS mapping inherited from a merge with a partial override', () => {
    const source = 'base: &base\n  dns:\n    ipv6: false\n    nameserver: [223.5.5.5]\n<<: *base\n'
    expect(readMihomoIPv6(source).dns).toMatchObject({ value: false, editable: false })
    expect(() => setMihomoIPv6(source, 'dns', true)).toThrow('合并')
  })
})


describe('Mihomo IPv6 Fake-IP range auto-fill', () => {
  const fakeIP = 'ipv6: true\ndns:\n  enable: true\n  ipv6: false # keep DNS note\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n  nameserver: [223.5.5.5]\ntun:\n  enable: false\n'

  it('adds the missing range when DNS IPv6 is enabled and leaves other settings intact', () => {
    const edit = setMihomoIPv6WithDetails(fakeIP, 'dns', true)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(edit.notice).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    expect(edit.content).toContain('ipv6: true # keep DNS note')
    expect(parse(edit.content)).toEqual({ ...parse(fakeIP), dns: { ...parse(fakeIP).dns,
      ipv6: true, 'fake-ip-range6': DEFAULT_MIHOMO_FAKE_IPV6_RANGE } })
    expect(parse(edit.content).tun.enable).toBe(false)
    const repeated = setMihomoIPv6WithDetails(edit.content, 'dns', true)
    expect(repeated.addedFakeIPv6Range).toBe(false)
    expect(repeated.notice).toBe('')
    expect(repeated.content).toBe(edit.content)
  })

  it('fills the range when enabling the core activates previously configured DNS IPv6', () => {
    const source = fakeIP.replace('ipv6: true', 'ipv6: false').replace('  ipv6: false', '  ipv6: true')
    const edit = setMihomoIPv6WithDetails(source, 'core', true)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(parse(edit.content)).toMatchObject({ ipv6: true, dns: { ipv6: true, 'fake-ip-range6': DEFAULT_MIHOMO_FAKE_IPV6_RANGE } })
  })

  it('uses the actual default template for a new empty draft', () => {
    const template = fakeIP.replace('ipv6: true\n', '')
    const edit = setMihomoIPv6WithDetails('', 'dns', true, template)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(parse(edit.content)).toEqual({ ...parse(template), dns: { ...parse(template).dns,
      ipv6: true, 'fake-ip-range6': DEFAULT_MIHOMO_FAKE_IPV6_RANGE } })
  })

  it.each([
    ['DNS off', fakeIP.replace('enable: true', 'enable: false'), 'dns'],
    ['DNS not explicitly enabled', fakeIP.replace('  enable: true\n', ''), 'dns'],
    ['redir-host mode', fakeIP.replace('enhanced-mode: fake-ip', 'enhanced-mode: redir-host'), 'dns'],
    ['mode omitted', fakeIP.replace('  enhanced-mode: fake-ip\n', ''), 'dns'],
    ['core disabled', fakeIP.replace('ipv6: true', 'ipv6: false'), 'dns'],
  ] as const)('does not change the setup for %s', (_name, source, field) => {
    const edit = setMihomoIPv6WithDetails(source, field, true)
    expect(edit.addedFakeIPv6Range).toBe(false)
    expect(parse(edit.content).dns['fake-ip-range6']).toBeUndefined()
    const expected = parse(source)
    if (field === 'core') expected.ipv6 = true
    else expected.dns.ipv6 = true
    expect(parse(edit.content)).toEqual(expected)
  })

  it('fills the range when Mihomo alone starts using IPv6 with Fake-IP, since the route is published anyway', () => {
    const edit = setMihomoIPv6WithDetails(fakeIP.replace('ipv6: true', 'ipv6: false'), 'core', true)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(edit.notice).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    expect(parse(edit.content)).toMatchObject({ ipv6: true, dns: { ipv6: false, 'fake-ip-range6': DEFAULT_MIHOMO_FAKE_IPV6_RANGE } })
    expect(parse(edit.content).tun.enable).toBe(false)
  })

  it('preserves an existing custom range and leaves it in place when either switch is disabled', () => {
    const source = fakeIP.replace('  fake-ip-range:', '  fake-ip-range6: fd12:3456:789a::1/64 # custom subnet\n  fake-ip-range:')
    const enabled = setMihomoIPv6WithDetails(source, 'dns', true)
    expect(enabled.addedFakeIPv6Range).toBe(false)
    expect(enabled.content).toContain('fake-ip-range6: fd12:3456:789a::1/64 # custom subnet')
    for (const field of ['core', 'dns'] as const) {
      const edit = setMihomoIPv6WithDetails(enabled.content, field, false)
      expect(parse(edit.content).dns['fake-ip-range6']).toBe('fd12:3456:789a::1/64')
      expect(edit.addedFakeIPv6Range).toBe(false)
      // Without dns.ipv6 the range is still routed and stays on screen: nothing to announce.
      expect(edit.notice).toBe(field === 'core' ? '已关闭 IPv6 Fake-IP，网段保留供再次开启。' : '')
    }
  })

  it.each(['null', '""', 'false', '123'])('does not overwrite an explicitly malformed range (%s)', value => {
    const source = fakeIP.replace('  fake-ip-range:', `  fake-ip-range6: ${value}\n  fake-ip-range:`)
    const edit = setMihomoIPv6WithDetails(source, 'dns', true)
    expect(edit.addedFakeIPv6Range).toBe(false)
    expect(edit.notice).toContain('未自动覆盖')
    expect(parse(edit.content).dns['fake-ip-range6']).toEqual(parse(source).dns['fake-ip-range6'])
  })

  it('keeps inherited custom ranges and aliases rather than masking them with defaults', () => {
    const source = 'dns-base: &base\n  fake-ip-range6: fd12:3456::1/64\nrange: &range fd99::1/64\nipv6: true\ndns:\n  <<: *base\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\n'
    const inherited = setMihomoIPv6WithDetails(source, 'dns', true)
    expect(inherited.addedFakeIPv6Range).toBe(false)
    expect(inherited.content).toContain('<<: *base')
    expect(inherited.content.match(/fake-ip-range6:/g)).toHaveLength(1)
    const aliased = setMihomoIPv6WithDetails(source.replace('  <<: *base', '  fake-ip-range6: *range'), 'dns', true)
    expect(aliased.addedFakeIPv6Range).toBe(false)
    expect(aliased.content).toContain('fake-ip-range6: *range')
  })

  it.each([
    'base: &base\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\ndns: *base\nipv6: false\n',
    'ipv6: false\ndns: &dns\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\nother: *dns\n',
    'base: &base\n  dns:\n    enable: true\n    ipv6: true\n    enhanced-mode: fake-ip\n<<: *base\nipv6: false\n',
  ])('changes only the core when the missing range belongs to shared or inherited DNS', source => {
    const edit = setMihomoIPv6WithDetails(source, 'core', true)
    expect(edit.addedFakeIPv6Range).toBe(false)
    expect(edit.notice).toContain('未自动补充')
    expect(edit.notice).toContain('fake-ip-range6')
    expect(edit.content).not.toContain('fake-ip-range6:')
    expect(edit.content).toContain('ipv6: true')
  })

  it('can add a local range next to inherited DNS options without modifying the source anchor', () => {
    const source = 'base: &base\n  enable: true\n  enhanced-mode: fake-ip\ndns:\n  <<: *base\n  ipv6: false\n'
    const edit = setMihomoIPv6WithDetails(source, 'dns', true)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(edit.content).toContain('<<: *base')
    expect(parse(edit.content).base).toEqual(parse(source).base)
    expect(parse(edit.content).dns['fake-ip-range6']).toBe(DEFAULT_MIHOMO_FAKE_IPV6_RANGE)
  })
})


describe('Mihomo editable IPv6 Fake-IP range', () => {
  const active = 'ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  nameserver: [223.5.5.5]\ntun:\n  enable: false\n'
  const withRange = (value: string) => active.replace('  nameserver:', `  fake-ip-range6: ${value} # keep range note\n  nameserver:`)

  it('shows a missing default without editing and does not require TUN to edit', () => {
    expect(readMihomoFakeIPv6Range(active)).toMatchObject({ visible: true, value: 'fc00::1/64', source: 'default', editable: true, error: '' })
    const next = setMihomoFakeIPv6Range(active, 'fd12:3456::1/64')
    expect(parse(next)).toEqual({ ...parse(active), dns: { ...parse(active).dns, 'fake-ip-range6': 'fd12:3456::1/64' } })
    expect(active).not.toContain('fake-ip-range6')
  })

  it.each([
    ['core off', active.replace('ipv6: true', 'ipv6: false')],
    ['invalid core', active.replace('ipv6: true', 'ipv6: "true"')],
    ['DNS off', active.replace('enable: true', 'enable: false')],
    ['redir-host', active.replace('enhanced-mode: fake-ip', 'enhanced-mode: redir-host')],
    ['DNS omitted', 'ipv6: true\n'],
    ['malformed YAML', 'dns: [\n'],
  ])('hides the range for %s without letting the setter activate it', (_name, source) => {
    expect(readMihomoFakeIPv6Range(source).visible).toBe(false)
    expect(() => setMihomoFakeIPv6Range(source, 'fd12::1/64')).toThrow()
  })

  it('uses the effective core default and server template only after an explicit edit', () => {
    const template = active.replace('ipv6: true\n', '')
    expect(readMihomoFakeIPv6Range('', template)).toMatchObject({ visible: true, source: 'default', value: 'fc00::1/64' })
    expect(parse(setMihomoFakeIPv6Range('', 'fd00:1234::1/64', template))).toMatchObject({ dns: { 'fake-ip-range6': 'fd00:1234::1/64' }, tun: { enable: false } })
  })

  it('keeps inline comments and unrelated YAML when changing a custom range', () => {
    const source = '# custom\n' + withRange('fd12::1/64') + 'secret: "00123"\n'
    const next = setMihomoFakeIPv6Range(source, ' fd99:ABCD::1/80 ')
    expect(next).toContain('fake-ip-range6: fd99:ABCD::1/80 # keep range note')
    expect(next).toContain('# custom')
    expect(parse(next)).toEqual({ ...parse(source), dns: { ...parse(source).dns, 'fake-ip-range6': 'fd99:ABCD::1/80' } })
    expect(readMihomoFakeIPv6Range(next)).toMatchObject({ value: 'fd99:ABCD::1/80', source: 'explicit', editable: true, error: '' })
  })

  it.each(['fc00::1/64', 'FDFF:FFFF:FFFF:FFFF:FFFF:FFFF:FFFF:FFFF/128', 'fd00::/7', 'fc00:0:0:0:0:0:0:1/64', 'fd00::192.0.2.1/96'])('accepts a valid ULA CIDR %s', value => {
    expect(validateMihomoFakeIPv6Range(value)).toBe('')
    expect(readMihomoFakeIPv6Range(setMihomoFakeIPv6Range(active, value)).value).toBe(value)
  })

  it.each(['', 'fd00::1', 'fd00::1/129', 'fd00::1/-1', 'fd00::1/6', '2001:db8::1/64', 'fe80::1/64', '::/0', '198.18.0.1/16', 'fd00:::1/64', 'fd00::1::2/64', 'fd00:0:0:0:0:0:0:0:1/64', '[fd00::1]/64', 'fd00::1%eth0/64', 'fd00::1/64/extra'])('rejects an invalid or unsuitable range without writing %s', value => {
    expect(validateMihomoFakeIPv6Range(value)).not.toBe('')
    expect(() => setMihomoFakeIPv6Range(active, value)).toThrow()
  })

  it.each(['null', 'false', '123', '!!int 123', '""', '2001:db8::1/64'])('allows an explicit correction of a malformed local scalar %s', value => {
    const source = withRange(value)
    expect(readMihomoFakeIPv6Range(source)).toMatchObject({ visible: true, editable: true, source: 'invalid' })
    expect(readMihomoFakeIPv6Range(source).error).not.toBe('')
    const next = setMihomoFakeIPv6Range(source, 'fd00::1/64')
    expect(parse(next).dns['fake-ip-range6']).toBe('fd00::1/64')
    expect(next).toContain('# keep range note')
  })

  it.each([
    'base: &base\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd00::1/64\ndns: *base\n',
    active.replace('dns:', 'dns: &dns') + 'other: *dns\n',
    withRange('&range fd00::1/64') + 'other: *range\n',
    'range: &range fd00::1/64\n' + withRange('*range'),
    'base: &base\n  fake-ip-range6: fd00::1/64\n' + active.replace('dns:\n', 'dns:\n  <<: *base\n'),
    'base: &base\n  dns:\n    enable: true\n    ipv6: true\n    enhanced-mode: fake-ip\n    fake-ip-range6: fd00::1/64\n<<: *base\n',
  ])('displays shared or inherited ranges but leaves their sources manual', source => {
    const state = readMihomoFakeIPv6Range(source)
    expect(state).toMatchObject({ visible: true, editable: false })
    expect(state.message).toContain('YAML')
    expect(() => setMihomoFakeIPv6Range(source, 'fd99::1/64')).toThrow()
  })

  it('allows a local range beside inherited DNS settings and an unreferenced anchor', () => {
    const source = 'base: &base\n  enable: true\n  enhanced-mode: fake-ip\ndns:\n  <<: *base\n  ipv6: true\n  fake-ip-range6: &unused fd00::1/64\n'
    const next = setMihomoFakeIPv6Range(source, 'fd99::1/64')
    expect(next).toContain('<<: *base')
    expect(next).toContain('&unused')
    expect(parse(next).base).toEqual(parse(source).base)
    expect(readMihomoFakeIPv6Range(next).value).toBe('fd99::1/64')
  })

  it('keeps showing and editing the range with DNS IPv6 off, as the route is still published', () => {
    const nodes = active.replace('  ipv6: true', '  ipv6: false')
    expect(readMihomoFakeIPv6Range(nodes)).toMatchObject({ visible: true, value: 'fc00::1/64', source: 'default', editable: true })
    expect(readMihomoFakeIPv6Range(nodes).message).toContain('上游路由暂不发布')
    expect(parse(setMihomoFakeIPv6Range(nodes, 'fd42::1/64')).dns).toMatchObject({ ipv6: false, 'fake-ip-range6': 'fd42::1/64' })
  })

  it('keeps the chosen range when switching IPv6 off and on again', () => {
    const source = setMihomoFakeIPv6Range(active, 'fd42:1234::1/64')
    const nodes = setMihomoIPv6WithDetails(source, 'dns', false)
    expect(readMihomoFakeIPv6Range(nodes.content)).toMatchObject({ visible: true, value: 'fd42:1234::1/64' })
    const off = setMihomoIPv6WithDetails(nodes.content, 'core', false)
    expect(readMihomoFakeIPv6Range(off.content)).toMatchObject({ visible: false, value: 'fd42:1234::1/64' })
    expect(off.notice).toContain('网段保留')
    const on = setMihomoIPv6WithDetails(off.content, 'core', true)
    expect(readMihomoFakeIPv6Range(on.content)).toMatchObject({ visible: true, value: 'fd42:1234::1/64' })
    expect(on.addedFakeIPv6Range).toBe(false)
  })
})


describe('Mihomo IPv6 mode', () => {
  const fakeIP = '# keep header\nipv6: false # core note\ndns:\n  enable: true\n  ipv6: false # dns note\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\ntun:\n  enable: true\n'

  it.each([
    ['ipv6: false\ndns:\n  ipv6: false\n', 'off'],
    ['ipv6: false\n', 'off'],
    ['ipv6: true\ndns:\n  ipv6: false\n', 'nodes'],
    ['mixed-port: 7890\n', 'nodes'],
    ['ipv6: true\ndns:\n  ipv6: true\n', 'clients'],
    ['dns:\n  ipv6: true\n', 'clients'],
  ] as const)('maps the effective values of %j to %s', (source, mode) => {
    const state = readMihomoIPv6Mode(source)
    expect(state.mode).toBe(mode)
    expect(state.customReason).toBe('')
  })

  it('reports a handwritten combination outside the modes as custom and never rewrites it on read', () => {
    const source = 'ipv6: false\ndns:\n  ipv6: true # AAAA only\n'
    const state = readMihomoIPv6Mode(source)
    expect(state.mode).toBe('custom')
    expect(state.customReason).toContain('顶层 ipv6 为 false')
    expect(state.customReason).toContain('dns.ipv6 却为 true')
    expect(Object.values(state.options).every(option => option.available)).toBe(true)
  })

  it('notes missing values that use Mihomo defaults', () => {
    expect(readMihomoIPv6Mode('mixed-port: 7890\n').defaultNote).toBe('YAML 未填写 ipv6（默认开启）、dns.ipv6（默认关闭），按 Mihomo 默认值显示。')
    expect(readMihomoIPv6Mode('ipv6: true\n').defaultNote).toBe('YAML 未填写 dns.ipv6（默认关闭），按 Mihomo 默认值显示。')
    expect(readMihomoIPv6Mode('ipv6: true\ndns:\n  ipv6: false\n').defaultNote).toBe('')
  })

  it.each(['ipv6: "true"\n', 'dns: false\n', 'dns: [\n', 'ipv6: true\nipv6: false\n'])('locks every mode for unreadable values: %j', source => {
    const state = readMihomoIPv6Mode(source)
    expect(state.mode).toBeNull()
    expect(state.lockedReason).not.toBe('')
    expect(Object.values(state.options).some(option => option.available)).toBe(false)
    expect(() => setMihomoIPv6Mode(source, 'nodes')).toThrow()
  })

  it('fills the Fake-IP range for 「Mihomo 用 IPv6，客户端走 IPv4」 as well', () => {
    const nodes = setMihomoIPv6Mode(fakeIP, 'nodes')
    expect(nodes.addedFakeIPv6Range).toBe(true)
    expect(nodes.notice).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    expect(parse(nodes.content)).toEqual({ ...parse(fakeIP), ipv6: true, dns: { ...parse(fakeIP).dns, 'fake-ip-range6': 'fc00::1/64' } })
    expect(readMihomoFakeIPv6Range(nodes.content)).toMatchObject({ visible: true, value: 'fc00::1/64', source: 'explicit' })
    const clients = setMihomoIPv6Mode(nodes.content, 'clients')
    expect(clients.addedFakeIPv6Range).toBe(false)
    expect(clients.content.match(/fake-ip-range6:/g)).toHaveLength(1)
    // redir-host has no Fake-IP range to fill.
    const redir = fakeIP.replace('enhanced-mode: fake-ip', 'enhanced-mode: redir-host')
    expect(setMihomoIPv6Mode(redir, 'nodes').content).not.toContain('fake-ip-range6')
  })

  it('switches between all three modes, keeping comments and filling the Fake-IP range once', () => {
    const clients = setMihomoIPv6Mode(fakeIP, 'clients')
    expect(clients.addedFakeIPv6Range).toBe(true)
    expect(clients.notice).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    expect(clients.content).toContain('# keep header')
    expect(clients.content).toContain('ipv6: true # core note')
    expect(clients.content).toContain('ipv6: true # dns note')
    expect(parse(clients.content)).toEqual({ ...parse(fakeIP), ipv6: true, dns: { ...parse(fakeIP).dns, ipv6: true, 'fake-ip-range6': 'fc00::1/64' } })
    expect(readMihomoIPv6Mode(clients.content).mode).toBe('clients')

    const nodes = setMihomoIPv6Mode(clients.content, 'nodes')
    expect(parse(nodes.content)).toMatchObject({ ipv6: true, dns: { ipv6: false, 'fake-ip-range6': 'fc00::1/64' } })
    expect(nodes.notice).toBe('')
    expect(readMihomoIPv6Mode(nodes.content).mode).toBe('nodes')

    const off = setMihomoIPv6Mode(clients.content, 'off')
    expect(parse(off.content)).toMatchObject({ ipv6: false, dns: { ipv6: false, 'fake-ip-range6': 'fc00::1/64' } })
    expect(off.notice).toBe('已关闭 IPv6 Fake-IP，网段保留供再次开启。')
    expect(readMihomoIPv6Mode(off.content).mode).toBe('off')

    const again = setMihomoIPv6Mode(off.content, 'clients')
    expect(again.addedFakeIPv6Range).toBe(false)
    expect(again.content.match(/fake-ip-range6:/g)).toHaveLength(1)
    for (const content of [clients.content, nodes.content, off.content, again.content]) {
      expect(content).not.toContain('上游路由')
      expect(parse(content)['fake-ip-range']).toBeUndefined()
      expect(parse(content).tun).toEqual({ enable: true })
    }
  })

  it('writes only the values a mode changes', () => {
    const source = 'mixed-port: 7890\n'
    expect(setMihomoIPv6Mode(source, 'nodes').content).toBe(source)
    expect(parse(setMihomoIPv6Mode(source, 'off').content)).toEqual({ 'mixed-port': 7890, ipv6: false })
    expect(parse(setMihomoIPv6Mode(source, 'clients').content)).toEqual({ 'mixed-port': 7890, dns: { ipv6: true } })
    // redir-host clients never receive a Fake-IP range.
    const redir = 'ipv6: true\ndns:\n  enable: true\n  enhanced-mode: redir-host\n'
    expect(parse(setMihomoIPv6Mode(redir, 'clients').content).dns).toEqual({ enable: true, 'enhanced-mode': 'redir-host', ipv6: true })
  })

  it('resolves a custom combination only when a mode is chosen', () => {
    const custom = 'ipv6: false\ndns:\n  ipv6: true\n'
    expect(parse(setMihomoIPv6Mode(custom, 'off').content)).toEqual({ ipv6: false, dns: { ipv6: false } })
    expect(parse(setMihomoIPv6Mode(custom, 'nodes').content)).toEqual({ ipv6: true, dns: { ipv6: false } })
    expect(parse(setMihomoIPv6Mode(custom, 'clients').content)).toEqual({ ipv6: true, dns: { ipv6: true } })
  })

  it('materializes the server template for an empty draft only when a mode changes it', () => {
    const template = 'mixed-port: 7890\ndns:\n  enable: true\n  enhanced-mode: fake-ip\n'
    expect(readMihomoIPv6Mode('', template).mode).toBe('nodes')
    const edit = setMihomoIPv6Mode('', 'clients', template)
    expect(parse(edit.content)).toEqual({ ...parse(template), dns: { ...parse(template).dns, ipv6: true, 'fake-ip-range6': 'fc00::1/64' } })
    expect(parse(setMihomoIPv6Mode('', 'off', template).content)).toEqual({ ...parse(template), ipv6: false })
  })

  it('offers only the modes that leave shared or inherited values untouched', () => {
    const aliased = 'base: &base\n  ipv6: false\ndns: *base\nipv6: true\n'
    const state = readMihomoIPv6Mode(aliased)
    expect(state.mode).toBe('nodes')
    expect(state.options).toMatchObject({ off: { available: true }, nodes: { available: true }, clients: { available: false } })
    expect(state.options.clients.reason).toContain('别名')
    expect(state.lockedReason).toContain('别名')
    expect(parse(setMihomoIPv6Mode(aliased, 'off').content)).toMatchObject({ ipv6: false, base: { ipv6: false } })
    expect(() => setMihomoIPv6Mode(aliased, 'clients')).toThrow('别名')
    const anchored = 'ipv6: &flag true\nother: *flag\ndns:\n  ipv6: false\n'
    expect(readMihomoIPv6Mode(anchored).options).toMatchObject({ off: { available: false }, nodes: { available: true }, clients: { available: true } })
    expect(() => setMihomoIPv6Mode(anchored, 'off')).toThrow('锚点')
    expect(parse(setMihomoIPv6Mode(anchored, 'clients').content)).toMatchObject({ ipv6: true, other: true, dns: { ipv6: true } })
  })

  it('keeps a shared Fake-IP DNS untouched and explains why the range was not filled', () => {
    const source = 'ipv6: false\ndns: &dns\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\nother: *dns\n'
    expect(readMihomoIPv6Mode(source).mode).toBe('custom')
    const edit = setMihomoIPv6Mode(source, 'clients')
    expect(edit.addedFakeIPv6Range).toBe(false)
    expect(edit.notice).toContain('未自动补充')
    expect(edit.content).not.toContain('fake-ip-range6')
    expect(() => setMihomoIPv6Mode(source, 'nodes')).toThrow('锚点')
  })
})


describe('Mihomo IPv6 values read like the server (PyYAML, YAML 1.1)', () => {
  it.each(['yes', 'Yes', 'YES', 'on', 'On', 'ON', 'true', 'True', 'TRUE'])('reads %s as on', spelling => {
    expect(readMihomoIPv6(`ipv6: ${spelling}\ndns:\n  ipv6: ${spelling}\n`)).toMatchObject({
      core: { value: true, source: 'explicit', editable: true }, dns: { value: true, source: 'explicit', editable: true }, error: '' })
  })

  it.each(['no', 'No', 'NO', 'off', 'Off', 'OFF', 'false', 'False', 'FALSE'])('reads %s as off', spelling => {
    expect(readMihomoIPv6(`ipv6: ${spelling}\ndns:\n  ipv6: ${spelling}\n`)).toMatchObject({
      core: { value: false, source: 'explicit' }, dns: { value: false, source: 'explicit' }, error: '' })
  })

  it.each([
    ['ipv6: yes\ndns:\n  ipv6: on\n', 'clients'],
    ['ipv6: on\ndns: { ipv6: off }\n', 'nodes'],
    ['ipv6: off\ndns:\n  ipv6: no\n', 'off'],
    ['ipv6: no\n', 'off'],
    ['ipv6: no\ndns:\n  ipv6: yes\n', 'custom'],
  ] as const)('derives the mode of %j', (source, mode) => {
    expect(readMihomoIPv6Mode(source).mode).toBe(mode)
  })

  // PyYAML keeps y / n as text (unlike the YAML 1.1 spec); numbers and null are not booleans either,
  // so the server refuses them (backend/converters/mihomo.py validate_ipv6_flags).
  it.each(['y', 'Y', 'n', 'N', '~', 'null', '', '1', '0', '017', '0x1', '1:20', '1e3', '09', '"yes"', "'off'"])('reports ipv6: %s as invalid, as the server does', spelling => {
    const state = readMihomoIPv6Mode(`ipv6: ${spelling}\n`)
    expect(state.mode).toBeNull()
    expect(state.config.core).toMatchObject({ value: null, source: 'invalid', editable: false })
    expect(state.lockedReason).toContain('true 或 false')
    expect(() => setMihomoIPv6Mode(`ipv6: ${spelling}\n`, 'nodes')).toThrow()
  })

  it('keeps a yes / on spelling that already has the chosen value and writes true / false for a change', () => {
    const source = 'ipv6: yes   # keep\ndns:\n  ipv6: off # DNS\n'
    expect(setMihomoIPv6Mode(source, 'nodes').content).toBe(source)
    expect(setMihomoIPv6WithDetails(source, 'core', true).content).toBe(source)
    expect(setMihomoIPv6WithDetails(source, 'dns', false).content).toBe(source)
    expect(setMihomoIPv6Mode(source, 'clients').content).toBe('ipv6: yes   # keep\ndns:\n  ipv6: true # DNS\n')
    expect(setMihomoIPv6Mode(source, 'off').content).toBe('ipv6: false # keep\ndns:\n  ipv6: off # DNS\n')
  })

  it('shows the Fake-IP range when DNS is enabled with yes / on, but not with y', () => {
    expect(readMihomoFakeIPv6Range('ipv6: on\ndns:\n  enable: yes\n  enhanced-mode: fake-ip\n')).toMatchObject({ visible: true, source: 'default' })
    expect(readMihomoFakeIPv6Range('ipv6: on\ndns:\n  enable: y\n  enhanced-mode: fake-ip\n').visible).toBe(false)
    const edit = setMihomoIPv6WithDetails('ipv6: off\ndns:\n  enable: on\n  enhanced-mode: fake-ip\n', 'core', true)
    expect(edit.addedFakeIPv6Range).toBe(true)
    expect(edit.content).toBe('ipv6: true\ndns:\n  enable: on\n  enhanced-mode: fake-ip\n  fake-ip-range6: fc00::1/64\n')
  })
})
