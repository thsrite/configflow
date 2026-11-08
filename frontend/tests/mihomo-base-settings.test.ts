import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { parse } from 'yaml'
import {
  applyDnsPreset, checkMihomoBase, controllerScope, controllerSecretError, DNS_POLICY_KEY, dnsLoop, editMihomoBase,
  FOREIGN_DOH, DOMESTIC_DOH, readDnsPreset, readMihomoBase, splitHostPort,
} from '@/lib/mihomoBaseSettings'

const template: string = JSON.parse(readFileSync(resolve(process.cwd(), '../backend/config_template.json'), 'utf8')).mihomo.custom_config
// What the server answers as default_config for an empty base (backend _default_mihomo_template).
const coreDefault = `mixed-port: 7890
allow-lan: true
bind-address: '*'
mode: rule
log-level: info
external-controller: 127.0.0.1:9090
dns:
  enable: true
  listen: 0.0.0.0:53
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver:
  - 223.5.5.5
  - 119.29.29.29
  fallback:
  - https://1.1.1.1/dns-query
  - https://dns.google/dns-query
`
// Read as PyYAML / Mihomo would (YAML 1.1): `off` would be false, `123456` a number.
const yaml11 = (text: string) => parse(text, { version: '1.1' })
const changedLines = (before: string, after: string) => {
  const old = new Set(before.split('\n'))
  return after.split('\n').filter(line => !old.has(line))
}

describe('Mihomo base YAML editing', () => {
  it('reads the template and changes only the targeted line, keeping comments', () => {
    const view = readMihomoBase(template)
    expect(view.error).toBe('')
    expect(view.value(['mixed-port'])).toBe(7893)
    expect(view.value(['secret'])).toBe('123456')
    const next = editMihomoBase(template, [{ path: ['secret'], value: 'Fine-Pass_2026' }])
    expect(changedLines(template, next)).toEqual(['secret: "Fine-Pass_2026"'])
    for (const comment of ['# 控制面板', '#进程匹配 always/strict/off', "# 'rule-set:gfw_domain': '8.8.8.8'", '#    - "rule-set:Private,Direct"', '# DNS模块']) {
      expect(next).toContain(comment)
    }
    expect(yaml11(next)).toEqual({ ...yaml11(template), secret: 'Fine-Pass_2026' })
  })

  it('quotes values that YAML 1.1 would read as numbers or booleans', () => {
    const next = editMihomoBase(template, [{ path: ['secret'], value: '12345678' }, { path: ['find-process-mode'], value: 'off' },
      { path: ['global-client-fingerprint'], value: '360' }, { path: ['bind-address'], value: 'yes' }])
    const values = yaml11(next)
    expect(values.secret).toBe('12345678')
    expect(values['find-process-mode']).toBe('off')
    expect(values['global-client-fingerprint']).toBe('360')
    expect(values['bind-address']).toBe('yes')
    // Numbers and booleans replace a quoted string without quotes.
    const typed = editMihomoBase("mixed-port: '7890'\nallow-lan: 'no'\n", [{ path: ['mixed-port'], value: 7891 }, { path: ['allow-lan'], value: true }])
    expect(typed).toBe('mixed-port: 7891\nallow-lan: true\n')
  })

  it('adds a missing key next to related keys and keeps a heading comment when removing one', () => {
    const source = 'mixed-port: 7890\n# 控制面板\nexternal-controller: 0.0.0.0:9090\ndns:\n  enable: true\n'
    const added = editMihomoBase(source, [{ path: ['secret'], value: 'Fine-Pass_2026' }, { path: ['allow-lan'], value: false }])
    expect(added).toBe('mixed-port: 7890\nallow-lan: false\n# 控制面板\nexternal-controller: 0.0.0.0:9090\nsecret: Fine-Pass_2026\ndns:\n  enable: true\n')
    const removed = editMihomoBase(source, [{ path: ['mixed-port'], value: undefined }])
    expect(removed).toBe('# 控制面板\nexternal-controller: 0.0.0.0:9090\ndns:\n  enable: true\n')
    const heading = editMihomoBase(source, [{ path: ['external-controller'], value: undefined }])
    expect(heading).toBe('mixed-port: 7890\n# 控制面板\ndns:\n  enable: true\n')
  })

  it('edits lists in place, keeping the comments of entries that remain', () => {
    const source = 'sniffer:\n  skip-domain:\n    - a.com # keep\n    - b.com\n'
    const next = editMihomoBase(source, [{ path: ['sniffer', 'skip-domain'], value: ['a.com', 'c.com'], kind: 'list' }])
    expect(next).toContain('a.com # keep')
    expect(parse(next).sniffer['skip-domain']).toEqual(['a.com', 'c.com'])
    const created = editMihomoBase('dns:\n  enable: true\n', [{ path: ['dns', 'fake-ip-filter'], value: ['*.lan', '+.pool.ntp.org'], kind: 'list' }])
    expect(parse(created).dns['fake-ip-filter']).toEqual(['*.lan', '+.pool.ntp.org'])
    expect(editMihomoBase(source, [{ path: ['sniffer', 'skip-domain'], value: undefined, kind: 'list' }])).toBe('sniffer: {}\n')
    // A missing parent is created; a null one (`sniffer:`) is replaced.
    expect(parse(editMihomoBase('sniffer:\n', [{ path: ['sniffer', 'enable'], value: true }]))).toEqual({ sniffer: { enable: true } })
  })

  it('keeps the list style of a base written by the server (PyYAML, lists not indented)', () => {
    const source = 'mixed-port: 7890\ntun:\n  enable: true\n  dns-hijack:\n  - any:53\n  - tcp://any:53\ndns:\n  enable: true\n  nameserver:\n  - https://223.5.5.5/dns-query\n'
    const next = editMihomoBase(source, [{ path: ['dns', 'nameserver'], value: ['https://doh.pub/dns-query'], kind: 'list' },
      { path: ['tun', 'mtu'], value: 1500 }, { path: ['socks-port'], value: 7891 }])
    expect(next).toBe('socks-port: 7891\nmixed-port: 7890\ntun:\n  enable: true\n  dns-hijack:\n  - any:53\n  - tcp://any:53\n  mtu: 1500\ndns:\n  enable: true\n  nameserver:\n  - https://doh.pub/dns-query\n')
  })

  it('keeps four-space sections, trailing spaces and a missing final newline', () => {
    const source = 'mixed-port: 7890   \nsniffer:\n  enable: true\n  sniff:\n    HTTP:\n      ports: [80, 8080-8880]\ndns:\n    enable: true\n    listen: 0.0.0.0:1053\n    nameserver:\n      - https://223.5.5.5/dns-query'
    const next = editMihomoBase(source, [{ path: ['dns', 'listen'], value: '0.0.0.0:53' }, { path: ['dns', 'ipv6'], value: false }])
    expect(next).toBe(source.replace('listen: 0.0.0.0:1053', 'listen: 0.0.0.0:53') + '\n    ipv6: false')
  })

  it('starts from the default config when the base is empty, like the IPv6 settings', () => {
    const next = editMihomoBase('', [{ path: ['log-level'], value: 'warning' }], coreDefault)
    expect(parse(next)).toMatchObject({ 'mixed-port': 7890, 'log-level': 'warning' })
  })

  it('refuses to edit unreadable YAML, aliases and merged values instead of overwriting them', () => {
    const broken = 'dns: [\n'
    expect(readMihomoBase(broken).error).toContain('YAML 无法解析')
    expect(() => editMihomoBase(broken, [{ path: ['log-level'], value: 'info' }])).toThrow('YAML 无法解析')
    const anchored = 'base: &base\n  enable: true\nsniffer: *base\nprofile:\n  <<: { store-selected: true }\n'
    const view = readMihomoBase(anchored)
    expect(view.field(['sniffer', 'enable']).editable).toBe(false)
    expect(view.field(['sniffer', 'enable']).reason).toContain('别名')
    expect(view.field(['profile', 'store-selected']).reason).toContain('合并')
    expect(view.field(['profile', 'store-fake-ip']).editable).toBe(true)
    expect(() => editMihomoBase(anchored, [{ path: ['sniffer', 'enable'], value: false }])).toThrow('别名')
    expect(readMihomoBase('dns:\n  fake-ip-filter: { a: 1 }\n').field(['dns', 'fake-ip-filter'], 'list').editable).toBe(false)
  })
})

describe('DNS presets', () => {
  it('recognizes the initial template as 国内 DoH', () => {
    expect(readDnsPreset(readMihomoBase(template))).toMatchObject({ preset: 'domestic', editable: true })
    expect(readDnsPreset(readMihomoBase('', coreDefault)).preset).toBe('custom')
  })

  it('switches to 国内外分开解析 and back, keeping the user nameserver-policy entries first', () => {
    const split = applyDnsPreset(template, 'split')
    const values = yaml11(split)
    expect(values.dns.nameserver).toEqual(FOREIGN_DOH)
    expect(values.dns['direct-nameserver-follow-policy']).toBe(true)
    expect(values.dns['respect-rules']).toBe(true)
    expect(Object.keys(values.dns['nameserver-policy'])).toEqual(['+.115.com', DNS_POLICY_KEY])
    expect(values.dns['nameserver-policy'][DNS_POLICY_KEY]).toEqual(DOMESTIC_DOH)
    expect(split).toContain("# 'rule-set:gfw_domain': '8.8.8.8'")
    expect(readDnsPreset(readMihomoBase(split)).preset).toBe('split')
    // Nothing outside the preset keys changes.
    const { dns: _a, ...rest } = values
    const { dns: _b, ...before } = yaml11(template)
    expect(rest).toEqual(before)

    // Only the preset's own lines change; every comment and the rest of the layout stay as written.
    const added = changedLines(template, split)
    expect(added.length).toBeLessThanOrEqual(8)
    // The entry next to the inserted one may take the printer's spacing before its comment.
    expect(added.every(line => /\+\.115\.com|follow-policy|geosite:cn,private|^ +- https:\/\//.test(line))).toBe(true)
    for (const line of template.split('\n').filter(line => line.trim().startsWith('#'))) expect(split).toContain(line)

    const back = applyDnsPreset(split, 'domestic')
    // Back to the template apart from the spacing of the lines next to the removed entry.
    expect(changedLines(template, back).length).toBeLessThanOrEqual(2)
    expect(readDnsPreset(readMihomoBase(back)).preset).toBe('domestic')
    expect(yaml11(back)).toEqual(yaml11(template))
  })

  it('removes the old fallback and creates nameserver-policy when needed', () => {
    const domestic = yaml11(applyDnsPreset('', 'domestic', coreDefault))
    expect(domestic.dns.fallback).toBeUndefined()
    expect(domestic.dns.nameserver).toEqual(DOMESTIC_DOH)
    const split = yaml11(applyDnsPreset('dns:\n  enable: true\n  fallback-filter: { geoip: true }\n', 'split'))
    expect(split.dns['fallback-filter']).toBeUndefined()
    expect(split.dns['nameserver-policy']).toEqual({ [DNS_POLICY_KEY]: DOMESTIC_DOH })
  })

  it('shows hand-edited lists as 自定义 and does not switch a shared DNS section', () => {
    const custom = editMihomoBase(template, [{ path: ['dns', 'nameserver'], value: ['https://dns.google/dns-query'], kind: 'list' }])
    expect(readDnsPreset(readMihomoBase(custom)).preset).toBe('custom')
    const shared = 'x: &dns\n  enable: true\ndns: *dns\n'
    expect(readDnsPreset(readMihomoBase(shared)).editable).toBe(false)
    expect(() => applyDnsPreset(shared, 'domestic')).toThrow()
  })
})

describe('Mihomo base checks (same rules as the server)', () => {
  const blocking = (source: string, saved = template, context = {}) => checkMihomoBase(source, saved, undefined, context).filter(item => item.blocking)
  const edit = (edits: Parameters<typeof editMihomoBase>[1], source = template) => editMihomoBase(source, edits)

  it('only warns about problems the saved YAML already has', () => {
    const issues = checkMihomoBase(template, template, undefined)
    expect(issues).toEqual([{ field: 'secret', message: '不能使用初始模板的默认密码 123456，请自己设置一个', blocking: false }])
    expect(blocking(edit([{ path: ['log-level'], value: 'info' }]))).toEqual([])
  })

  it('applies the wizard password rules and requires one for a LAN controller', () => {
    expect(blocking(edit([{ path: ['secret'], value: 'short' }]))[0].message).toContain('8–64')
    expect(blocking(edit([{ path: ['secret'], value: undefined }]))[0].message).toContain('必须设置控制面板密码')
    expect(blocking(edit([{ path: ['external-controller'], value: '0.0.0.0:9091' }]))[0].message).toContain('123456')
    expect(blocking(edit([{ path: ['external-controller'], value: '127.0.0.1:9090' }, { path: ['secret'], value: undefined }]))).toEqual([])
    expect(blocking(edit([{ path: ['external-controller'], value: '192.168.1.2:9090' }, { path: ['secret'], value: 'Fine-Pass_2026' }]))[0].message).toContain('只能监听')
    expect(controllerSecretError('Fine-Pass_2026')).toBe('')
    expect(controllerSecretError('含 空格的密码aaa')).toContain('8–64')
  })

  it('finds port conflicts with each other, the DNS listener, the controller and Agents', () => {
    const conflict = blocking(edit([{ path: ['socks-port'], value: 7893 }]))
    expect(conflict.map(item => item.field).sort()).toEqual(['mixed-port', 'socks-port'])
    expect(conflict[0].message).toBe('端口 7893 同时被 mixed-port 和 socks-port 使用，请改用不同端口')
    expect(blocking(edit([{ path: ['mixed-port'], value: 1053 }]))[0].message).toContain('dns.listen')
    expect(blocking(edit([{ path: ['mixed-port'], value: 9090 }]))[0].message).toContain('external-controller')
    const agent = blocking(edit([{ path: ['mixed-port'], value: 8080 }]), template, { reservedPorts: [{ port: 8080, owner: 'Mihomo Agent「box」的端口' }] })
    expect(agent[0].message).toBe('mixed-port 使用端口 8080，与Mihomo Agent「box」的端口冲突，请改用其他端口')
    expect(blocking(edit([{ path: ['mixed-port'], value: 70000 }]))[0].message).toContain('1–65535')
  })

  it('rejects DNS upstreams that loop through this host', () => {
    expect(blocking(edit([{ path: ['dns', 'nameserver'], value: ['127.0.0.1'], kind: 'list' }]))[0].message).toContain('解析循环')
    expect(blocking(edit([{ path: ['dns', 'direct-nameserver'], value: ['system'], kind: 'list' }]))[0].field).toBe('dns.direct-nameserver')
    const host = edit([{ path: ['dns', 'nameserver'], value: ['10.0.0.5'], kind: 'list' }])
    expect(blocking(host, template, { localAddresses: ['10.0.0.5'] })[0].message).toContain('解析循环')
    expect(blocking(host)).toEqual([])
    expect(dnsLoop({ fallback: ['tcp://[::1]:53'] })).toEqual({ key: 'fallback', value: 'tcp://[::1]:53' })
    expect(dnsLoop({ nameserver: ['https://[::ffff:127.0.0.1]/dns-query'] })?.key).toBe('nameserver')
    expect(dnsLoop({ nameserver: ['https://223.5.5.5/dns-query'] })).toBeNull()
    expect(blocking(edit([{ path: ['dns', 'nameserver'], value: undefined, kind: 'list' }]))[0].message).toContain('至少填写一个')
    expect(blocking(edit([{ path: ['dns', 'proxy-server-nameserver'], value: undefined, kind: 'list' }]))[0].message).toContain('respect-rules')
  })

  it('checks enums and value types like the server', () => {
    expect(blocking(edit([{ path: ['log-level'], value: 'verbose' }]))[0].message).toContain('silent / error')
    expect(blocking(edit([{ path: ['tun', 'stack'], value: 'lwip' }]))[0].message).toContain('system / gvisor / mixed')
    expect(blocking(edit([{ path: ['tun', 'mtu'], value: 100 }]))[0].message).toContain('576–65535')
    expect(blocking(edit([{ path: ['bind-address'], value: 'lan' }]))[0].message).toContain('bind-address')
    expect(blocking(edit([{ path: ['external-ui-url'], value: 'ftp://x/ui.zip' }]))[0].message).toContain('http(s)')
    expect(blocking(template.replace('allow-lan: true', "allow-lan: 'true'"))[0].message).toContain('true 或 false')
    // Unquoted off is accepted, as generation maps it back to the mode.
    expect(blocking(template.replace("find-process-mode: 'strict'", 'find-process-mode: off'))).toEqual([])
  })

  it('parses controller listeners as the Agent does', () => {
    expect(splitHostPort('0.0.0.0:9090')).toEqual({ host: '0.0.0.0', port: 9090 })
    expect(splitHostPort('[::1]:9091')).toEqual({ host: '::1', port: 9091 })
    expect(splitHostPort(':9090')).toEqual({ host: '', port: 9090 })
    expect(splitHostPort('::1:9090')).toBeNull()
    expect(controllerScope('::')).toBe('lan')
    expect(controllerScope('localhost')).toBe('local')
    expect(controllerScope('127.0.0.2')).toBe('local')
    expect(controllerScope('10.0.0.1')).toBe('other')
  })
})
