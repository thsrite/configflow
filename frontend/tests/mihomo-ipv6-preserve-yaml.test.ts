import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { parse } from 'yaml'
import { readMihomoIPv6Mode, setMihomoFakeIPv6Range, setMihomoIPv6Mode, setMihomoIPv6WithDetails } from '@/lib/mihomoIPv6'

// `legacy.on` switches the IPv6 edits back to the previous full printout
// (`document.toString({ lineWidth: 0 })`), so each case compares both outputs.
const legacy = vi.hoisted(() => ({ on: false }))
vi.mock('@/lib/yamlPreserve', async importOriginal => {
  const actual = await importOriginal<typeof import('@/lib/yamlPreserve')>()
  return {
    ...actual,
    printYamlPreserving: (...args: Parameters<typeof actual.printYamlPreserving>) =>
      legacy.on ? args[1].toString({ lineWidth: 0 }) : actual.printYamlPreserving(...args),
  }
})


const template: string = JSON.parse(readFileSync(resolve(process.cwd(), '../backend/config_template.json'), 'utf8')).mihomo.custom_config

const custom = '# 自定义配置   \n' + `mixed-port: 7890
allow-lan: true
bind-address: "*"
ipv6: false          # 先关着
log-level: 'warning'
secret: "123456"
mode: 'off'

sniffer:
  enable: true
  sniff:
    HTTP:
      ports: [80, 8080-8880]
    TLS: { ports: [443, 8443] }
  skip-domain:
  - "+.push.apple.com"
 #   - "rule-set:Private"
  - '+.qq.com'


tun:
  enable: true
  dns-hijack: ["any:53", "tcp://any:53"]
dns:
  enable: true
  ipv6: false   # AAAA
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver-policy:
    '+.example.com': '223.5.5.5'    # 行尾注释
  nameserver:
    - https://223.5.5.5/dns-query

rules:
  - MATCH,DIRECT
`

type Edit = (source: string) => { content: string; addedFakeIPv6Range?: boolean; notice?: string } | string

function run(source: string, edit: Edit) {
  legacy.on = false
  const next = edit(source)
  legacy.on = true
  try {
    return { next: typeof next === 'string' ? { content: next } : next, old: (r => typeof r === 'string' ? { content: r } : r)(edit(source)) }
  } finally {
    legacy.on = false
  }
}

/** Lines removed from / added to `before`, by longest common subsequence. */
function lineDiff(before: string, after: string) {
  const a = before.split('\n')
  const b = after.split('\n')
  const table = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0))
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1])
  }
  const removed: string[] = []
  const added: string[] = []
  let i = 0
  let j = 0
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { i++; j++ }
    else if (j < b.length && (i === a.length || table[i][j + 1] >= table[i + 1][j])) added.push(b[j++])
    else removed.push(a[i++])
  }
  return { removed, added }
}

const IPV6_LINE = /^\s*(ipv6|fake-ip-range6):/

/** Same meaning as the old full printout, for the editor (YAML 1.2) and for the server (YAML 1.1, as PyYAML). */
function expectSameMeaning(next: string, old: string) {
  expect(parse(next, { merge: true })).toEqual(parse(old, { merge: true }))
  expect(parse(next, { version: '1.1', merge: true })).toEqual(parse(old, { version: '1.1', merge: true }))
}

/** Only IPv6 lines changed; every other line of the source is still there, verbatim and in order. */
function expectOnlyIPv6Lines(source: string, next: string) {
  const { removed, added } = lineDiff(source, next)
  for (const line of [...removed, ...added]) expect(line).toMatch(IPV6_LINE)
  return { removed, added }
}

describe('IPv6 settings write back only the lines they change', () => {
  it.each([
    ['template', template],
    ['custom YAML', custom],
  ])('%s: every mode keeps the old values and leaves unrelated lines as typed', (_name, source) => {
    for (const mode of ['nodes', 'clients', 'off'] as const) {
      const { next, old } = run(source, text => setMihomoIPv6Mode(text, mode))
      expectSameMeaning(next.content, old.content)
      expect(next.addedFakeIPv6Range).toBe(old.addedFakeIPv6Range)
      expect(next.notice).toBe(old.notice)
      expectOnlyIPv6Lines(source, next.content)
      expect(readMihomoIPv6Mode(next.content).mode).toBe(mode)
    }
    // The old printout rewrote the whole file.
    const { old } = run(source, text => setMihomoIPv6Mode(text, 'clients'))
    expect(lineDiff(source, old.content).removed.filter(line => !IPV6_LINE.test(line)).length).toBeGreaterThan(0)
  })

  it('turning IPv6 on fills fake-ip-range6 with one added line and keeps the value comments', () => {
    const { next } = run(custom, text => setMihomoIPv6Mode(text, 'clients'))
    expect(next.addedFakeIPv6Range).toBe(true)
    expect(next.notice).toBe('已补充 IPv6 Fake-IP 网段 fc00::1/64。')
    const { removed, added } = expectOnlyIPv6Lines(custom, next.content)
    expect(removed).toEqual(['ipv6: false          # 先关着', '  ipv6: false   # AAAA'])
    expect(added).toEqual(['ipv6: true # 先关着', '  ipv6: true # AAAA', '  fake-ip-range6: fc00::1/64'])
    expect(next.content).toContain('      ports: [80, 8080-8880]\n')
    expect(next.content).toContain("    '+.example.com': '223.5.5.5'    # 行尾注释\n")
    expect(next.content.startsWith('# 自定义配置   \n')).toBe(true)
    expect(parse(next.content, { version: '1.1' }).mode).toBe('off')
  })

  it('template: switching on and off again only touches the two switches and the range', () => {
    const on = run(template, text => setMihomoIPv6Mode(text, 'clients')).next
    expect(expectOnlyIPv6Lines(template, on.content).added).toEqual(['ipv6: true', '  ipv6: true', '  fake-ip-range6: fc00::1/64'])
    const { next, old } = run(on.content, text => setMihomoIPv6Mode(text, 'off'))
    expectSameMeaning(next.content, old.content)
    expect(next.notice).toBe('已关闭 IPv6 Fake-IP，网段保留供再次开启。')
    const back = lineDiff(template, next.content)
    expect(back.removed).toEqual([])
    expect(back.added).toEqual(['  fake-ip-range6: fc00::1/64'])
  })

  it.each([
    ['template', template],
    ['custom YAML', custom],
  ])('%s: a mode that is already set returns the text unchanged', (_name, source) => {
    expect(setMihomoIPv6Mode(source, 'off').content).toBe(source)
    expect(setMihomoIPv6WithDetails(source, 'core', false).content).toBe(source)
    expect(setMihomoIPv6WithDetails(source, 'dns', false).content).toBe(source)
  })

  it('editing the range replaces only its line', () => {
    const on = setMihomoIPv6Mode(custom, 'nodes').content
    const { next, old } = run(on, text => setMihomoFakeIPv6Range(text, 'fd42::1/64'))
    expectSameMeaning(next.content, old.content)
    expect(lineDiff(on, next.content)).toEqual({ removed: ['  fake-ip-range6: fc00::1/64'], added: ['  fake-ip-range6: fd42::1/64'] })
  })

  it('an empty draft keeps the default template as written', () => {
    const { next, old } = run('', text => setMihomoIPv6Mode(text, 'clients', template))
    expectSameMeaning(next.content, old.content)
    expectOnlyIPv6Lines(template, next.content)
  })

  it('keeps anchors, aliases and merges as written', () => {
    const source = 'flag: &flag true   # shared\nipv6: *flag\nbase: &base\n  enable: true\n  enhanced-mode: fake-ip\ndns:\n  <<: *base\n  ipv6: false\nlist: [a, b]   # flow\n'
    const { next, old } = run(source, text => setMihomoIPv6WithDetails(text, 'core', false))
    expectSameMeaning(next.content, old.content)
    expect(lineDiff(source, next.content)).toEqual({ removed: ['ipv6: *flag'], added: ['ipv6: false'] })
    const dns = run(source, text => setMihomoIPv6WithDetails(text, 'dns', true))
    expectSameMeaning(dns.next.content, dns.old.content)
    expect(dns.next.addedFakeIPv6Range).toBe(true)
    expect(lineDiff(source, dns.next.content)).toEqual({ removed: ['  ipv6: false'], added: ['  ipv6: true', '  fake-ip-range6: fc00::1/64'] })
    // Shared values stay locked, exactly as before.
    expect(() => setMihomoIPv6WithDetails('ipv6: &v false\nother: *v\n', 'core', true)).toThrow('锚点')
  })

  it('follows a four-space indentation', () => {
    const source = 'ipv6: false\ndns:\n    enable: true\n    ipv6: false\n    enhanced-mode: fake-ip\n    nameserver:\n        - 223.5.5.5\ntun:\n    enable: true\n'
    const { next, old } = run(source, text => setMihomoIPv6Mode(text, 'clients'))
    expectSameMeaning(next.content, old.content)
    expect(lineDiff(source, next.content)).toEqual({
      removed: ['ipv6: false', '    ipv6: false'],
      added: ['ipv6: true', '    ipv6: true', '    fake-ip-range6: fc00::1/64'],
    })
  })

  it('mixed indentation still keeps the values and the sections it does not touch', () => {
    const dns = custom.slice(custom.indexOf('dns:\n'), custom.indexOf('\nrules:'))
    const source = custom.replace(dns, dns.replace(/^  /gm, '    '))
    expect(source).toContain('    ipv6: false   # AAAA')
    const { next, old } = run(source, text => setMihomoIPv6Mode(text, 'clients'))
    expectSameMeaning(next.content, old.content)
    const kept = source.slice(0, source.indexOf('dns:'))
    expect(next.content.startsWith(kept.replace('ipv6: false          # 先关着', 'ipv6: true # 先关着'))).toBe(true)
  })

  it('keeps values the old printout changed for the server (YAML 1.1 octal)', () => {
    const source = 'port: 017\nipv6: false\ndns:\n  enable: true\n  enhanced-mode: fake-ip\n'
    const { next, old } = run(source, text => setMihomoIPv6Mode(text, 'nodes'))
    expect(parse(next.content, { merge: true })).toEqual(parse(old.content, { merge: true }))
    // The editor reads 017 as 17 and printed it so; PyYAML reads the original as 15.
    expect(old.content).toContain('port: 17\n')
    expect(next.content).toContain('port: 017\n')
    expect(parse(next.content, { version: '1.1' }).port).toBe(parse(source, { version: '1.1' }).port)
  })

  it('still refuses YAML it cannot parse', () => {
    for (const source of ['dns: [\n', '[true, false]\n', 'ipv6: true\nipv6: false\n']) {
      expect(() => setMihomoIPv6Mode(source, 'clients')).toThrow()
      expect(() => setMihomoIPv6WithDetails(source, 'core', false)).toThrow()
    }
  })
})
