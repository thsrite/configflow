import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentLogLines from '@/components/agents/AgentLogLines.vue'
import { markLogTokens, parseAgentLogLine, parseAgentLogs, splitFields, tokenizeLogMessage, type LogToken } from '@/lib/agentLogs'

const OXIDNS_DOWNLOAD = '2026-10-20T23:22:20+08:00 T+341494.611 INFO oxidns::plugin::executor::download:184:download completed plugin=rules_download url=https://cf.example/api/profiles/default/mosdns/rule-proxy?url=https%3A%2F%2Fcf.example%2Fx&part=domain&token=[REDACTED] target=rules/rule_27b8_domain.txt timeout_ms=60000 socks5=""'
const OXIDNS_WARN = '2026-10-20T23:48:57+08:00 T+343092.132 WARN oxidns::plugin::server::udp:232:Error receiving message on UDP socket: DNS protocol error: dns compression pointer exceeds packet length'
const MIHOMO = 'time="2026-10-21T00:01:02.123456789+08:00" level=warning msg="[TCP] dial Proxy (match RuleSet(google)) 192.168.0.5:51234 --> www.google.com:443 error: i/o timeout"'
const AGENT = '2026/10/20 23:39:47 Heartbeat sent successfully (service status:  -> active)'

describe('agent log parsing', () => {
  it('splits OxiDNS tracing lines into time, level, source, message and fields', () => {
    const entry = parseAgentLogLine(OXIDNS_DOWNLOAD)
    expect(entry).toMatchObject({
      date: '2026-10-20',
      time: '23:22:20',
      level: 'INFO',
      source: 'oxidns::plugin::executor::download:184',
      sourceShort: 'download:184',
      message: 'download completed',
      continuation: false
    })
    expect(entry.fields).toEqual([
      { key: 'plugin', value: 'rules_download' },
      { key: 'url', value: 'https://cf.example/api/profiles/default/mosdns/rule-proxy?url=https%3A%2F%2Fcf.example%2Fx&part=domain&token=[REDACTED]' },
      { key: 'target', value: 'rules/rule_27b8_domain.txt' },
      { key: 'timeout_ms', value: '60000' },
      { key: 'socks5', value: '' }
    ])
  })

  it('keeps colons inside OxiDNS messages and normalizes WARN', () => {
    expect(parseAgentLogLine(OXIDNS_WARN)).toMatchObject({
      level: 'WARN',
      sourceShort: 'udp:232',
      message: 'Error receiving message on UDP socket: DNS protocol error: dns compression pointer exceeds packet length',
      fields: []
    })
  })

  it('reads Mihomo logrus lines', () => {
    expect(parseAgentLogLine(MIHOMO)).toMatchObject({
      date: '2026-10-21',
      time: '00:01:02',
      level: 'WARN',
      message: '[TCP] dial Proxy (match RuleSet(google)) 192.168.0.5:51234 --> www.google.com:443 error: i/o timeout'
    })
  })

  it('reads Go Agent lines without inventing a level', () => {
    expect(parseAgentLogLine(AGENT)).toMatchObject({
      date: '2026-10-20',
      time: '23:39:47',
      level: '',
      message: 'Heartbeat sent successfully (service status:  -> active)'
    })
  })

  it('keeps unknown lines as continuations and drops blank lines', () => {
    const entries = parseAgentLogs(`${AGENT}\n\n  goroutine 1 [running]:\r\n`)
    expect(entries).toHaveLength(2)
    expect(entries[1]).toMatchObject({ continuation: true, message: '  goroutine 1 [running]:', time: '' })
  })

  it('leaves prose with a later key=value untouched', () => {
    expect(splitFields('set a=1 then restarted')).toEqual({ message: 'set a=1 then restarted', fields: [] })
  })
})

describe('AgentLogLines', () => {
  it('renders rows with a date divider, level column and wrapped fields', () => {
    const wrapper = mount(AgentLogLines, { props: { text: [OXIDNS_DOWNLOAD, OXIDNS_WARN, MIHOMO].join('\n') } })
    expect(wrapper.findAll('.log-date').map(node => node.text())).toEqual(['2026-10-20', '2026-10-21'])
    expect(wrapper.findAll('.log-row')).toHaveLength(3)
    expect(wrapper.find('.lv-row-WARN .log-source').attributes('title')).toBe('oxidns::plugin::server::udp:232')
    expect(wrapper.findAll('.log-key').map(node => node.text())).toContain('url=')
  })

  it('hides the level column for Agent logs and shows notices first', () => {
    const plain = mount(AgentLogLines, { props: { text: AGENT } })
    expect(plain.find('ol').classes()).toContain('no-levels')
    expect(plain.find('.log-level').exists()).toBe(false)

    const notice = mount(AgentLogLines, { props: { text: AGENT, notice: '获取日志失败: 超时' } })
    expect(notice.text()).toBe('获取日志失败: 超时')
    expect(mount(AgentLogLines, { props: { text: '' } }).text()).toBe('该日志暂无内容')
  })
})

const kinds = (tokens: LogToken[]) => tokens.filter(token => token.kind).map(token => [token.kind, token.text])

describe('log message colouring', () => {
  it('splits a Mihomo connection line by meaning without changing the text', () => {
    const message = '[TCP] 192.168.1.23:52314 --> www.example.com:443 match DomainSuffix(example.com) using 节点选择[HK-01]'
    const tokens = tokenizeLogMessage(message)
    expect(tokens.map(token => token.text).join('')).toBe(message)
    expect(kinds(tokens)).toEqual([
      ['tag', '[TCP]'], ['src', '192.168.1.23:52314'], ['arrow', '-->'], ['dst', 'www.example.com:443'],
      ['kw', 'match'], ['rule', 'DomainSuffix(example.com)'], ['kw', 'using'], ['proxy', '节点选择[HK-01]']
    ])
  })

  it('tells direct and reject policies apart', () => {
    expect(kinds(tokenizeLogMessage('[UDP] 192.168.1.40:5353 --> 224.0.0.251:5353 match IPCIDR(224.0.0.0/3) using DIRECT')).at(-1))
      .toEqual(['direct', 'DIRECT'])
    expect(kinds(tokenizeLogMessage('[TCP] 192.168.1.57:61022 --> ads.example.net:443 match RuleSet(reject) using REJECT-DROP')).at(-1))
      .toEqual(['reject', 'REJECT-DROP'])
  })

  it('colours the reason after error: in dial failures', () => {
    const message = '[TCP] dial Proxy (match RuleSet(google)) 192.168.0.5:51234 --> www.example.com:443 error: i/o timeout'
    const tokens = tokenizeLogMessage(message)
    expect(tokens.map(token => token.text).join('')).toBe(message)
    expect(kinds(tokens)).toEqual([
      ['tag', '[TCP]'], ['kw', 'match'], ['rule', 'RuleSet(google)'], ['src', '192.168.0.5:51234'], ['arrow', '-->'],
      ['dst', 'www.example.com:443'], ['err', 'error: i/o timeout']
    ])
  })

  it('reads the slash rule form and process names from real dial failures', () => {
    const message = '[TCP] dial DIRECT (match RuleSet/private_ip) 10.0.0.8:46188(chrome) --> 198.51.100.10:80 error: dial tcp 198.51.100.10:80: i/o timeout'
    const tokens = tokenizeLogMessage(message)
    expect(tokens.map(token => token.text).join('')).toBe(message)
    expect(kinds(tokens)).toEqual([
      ['tag', '[TCP]'], ['kw', 'match'], ['rule', 'RuleSet/private_ip'], ['src', '10.0.0.8:46188(chrome)'], ['arrow', '-->'],
      ['dst', '198.51.100.10:80'], ['err', 'error: dial tcp 198.51.100.10:80: i/o timeout']
    ])
  })

  it('stops addresses at brackets and leaves prose alone', () => {
    expect(kinds(tokenizeLogMessage('[Sniffer] Sniff TCP [192.168.1.23:52320-->203.0.113.44:443] success'))).toEqual([
      ['tag', '[Sniffer]'], ['src', '192.168.1.23:52320'], ['arrow', '-->'], ['dst', '203.0.113.44:443']
    ])
    expect(kinds(tokenizeLogMessage('Get "https://sub.example.com/x": context deadline exceeded'))).toEqual([
      ['str', '"https://sub.example.com/x"']
    ])
    expect(kinds(tokenizeLogMessage('reload done using cached config, no match found'))).toEqual([])
  })

  it('colours single-quoted names but not contractions', () => {
    expect(kinds(tokenizeLogMessage("节点 'Claude-2' 为 URI/base64 格式"))).toEqual([['str', "'Claude-2'"]])
    expect(kinds(tokenizeLogMessage("don't retry, it's fine"))).toEqual([])
  })

  it('marks search hits across token boundaries, ignoring case', () => {
    const tokens = markLogTokens(tokenizeLogMessage('[TCP] 192.168.1.23:1 --> WWW.example.com:443 match Domain(x) using DIRECT'), '--> www')
    expect(tokens.filter(token => token.hit).map(token => [token.kind, token.text])).toEqual([
      ['arrow', '-->'], ['', ' '], ['dst', 'WWW']
    ])
    expect(markLogTokens([{ text: 'abc', kind: '' }], '  ')).toEqual([{ text: 'abc', kind: '' }])
  })

  it('renders coloured tokens and search marks', () => {
    const wrapper = mount(AgentLogLines, {
      props: { text: 'time="2026-10-21T00:01:02+08:00" level=info msg="[TCP] 192.168.0.5:1 --> www.example.com:443 match Match using DIRECT"', highlight: 'example' }
    })
    expect(wrapper.find('.log-tk-dst').text()).toBe('www.')
    expect(wrapper.find('mark.log-hit.log-tk-dst').text()).toBe('example')
    expect(wrapper.find('.log-tk-direct').text()).toBe('DIRECT')
    expect(wrapper.find('.log-message').text()).toBe('[TCP] 192.168.0.5:1 --> www.example.com:443 match Match using DIRECT')
  })
})
