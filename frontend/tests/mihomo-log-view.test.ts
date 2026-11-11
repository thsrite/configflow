import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import MihomoLogView from '@/components/mihomo/MihomoLogView.vue'
import { mihomoLiveEntry } from '@/lib/agentLogs'
import { groupLogRows, parseConnectionLog, parseDialFailure, rowMatches, ruleLabel, toLogRows } from '@/lib/mihomoLogView'

let seq = 0
const pad = (value: number) => String(value).padStart(2, '0')
const now = new Date()
// 本地时区的今天（不带 Z），时间列不加日期
const today = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
const entry = (level: string, payload: string, second = 0, date = today) =>
  mihomoLiveEntry({ seq: ++seq, level, payload, time: `${date}T12:00:${pad(second)}` })

describe('Mihomo connection log parsing', () => {
  it('splits source, target, rule and the policy chain', () => {
    expect(parseConnectionLog('[TCP] 192.168.1.105:54954 --> api.telegram.org:443 match RuleSet(Telegram) using Telegram[美国 02]')).toMatchObject({
      network: 'TCP', sourceIP: '192.168.1.105', inner: false, host: 'api.telegram.org', port: '443',
      rule: 'RuleSet(Telegram)', ruleLabel: 'Telegram', group: 'Telegram', node: '美国 02', kind: 'proxy'
    })
    expect(parseConnectionLog('[TCP] 192.168.1.63:2656 --> ecs.office.com:443 match RuleSet(Microsoft) using 微软服务[DIRECT]'))
      .toMatchObject({ group: '微软服务', node: 'DIRECT', kind: 'direct' })
    // 节点名自带括号时按第一个 [ 拆
    expect(parseConnectionLog('[UDP] 10.0.0.2:5000 --> 1.1.1.1:53 match Match() using Proxy[HK [IPLC] 01]'))
      .toMatchObject({ network: 'UDP', ruleLabel: 'Match', group: 'Proxy', node: 'HK [IPLC] 01' })
  })

  it('handles the inner core, processes, IPv6, unmatched and mode lines', () => {
    expect(parseConnectionLog('[TCP] mihomo --> 10.97.0.185:5001 match GeoIP(lan) using DIRECT'))
      .toMatchObject({ inner: true, sourceIP: '', ruleLabel: 'GeoIP lan', group: 'DIRECT', node: 'DIRECT', kind: 'direct' })
    expect(parseConnectionLog('[TCP] 192.168.1.5:5000(curl, uid=0) --> [2001:db8::1]:443 doesn\'t match any rule using REJECT'))
      .toMatchObject({ sourceIP: '192.168.1.5', process: 'curl, uid=0', host: '[2001:db8::1]', port: '443', ruleLabel: '未匹配', kind: 'reject' })
    expect(parseConnectionLog('[TCP] [fd00::5]:5000 --> example.com:443 using GLOBAL')).toMatchObject({ sourceIP: 'fd00::5', ruleLabel: '模式', node: 'GLOBAL' })
    expect(parseConnectionLog('[Sniffer] All sniffing sniff failed')).toBeNull()
    expect(ruleLabel('RuleSet(Proxy)')).toBe('Proxy')
  })

  it('parses dial failures with either rule notation', () => {
    expect(parseDialFailure('[TCP] dial DIRECT (match RuleSet/private_ip) 10.97.0.188:46214 --> 198.51.100.10:80 error: dial tcp 198.51.100.10:80: i/o timeout'))
      .toEqual({ network: 'TCP', sourceIP: '10.97.0.188', inner: false, target: '198.51.100.10:80', policy: 'DIRECT', ruleLabel: 'private_ip', error: 'dial tcp 198.51.100.10:80: i/o timeout' })
    expect(parseDialFailure('[TCP] dial 节点选择 (match RuleSet(Proxy)) 192.168.1.31:51022 --> www.example.com:443 error: 香港 01 connect timeout'))
      .toMatchObject({ policy: '节点选择', ruleLabel: 'Proxy', error: '香港 01 connect timeout' })
  })

  it('groups repeats by device, target and policy, newest group last', () => {
    const rows = toLogRows([
      entry('info', '[TCP] 192.168.1.45:1 --> 149.154.167.92:443 match RuleSet(Telegram) using Telegram[美国 02]', 1),
      entry('info', '[TCP] 192.168.1.101:2 --> api.anthropic.com:443 match RuleSet(Claude) using Claude[日本 01]', 2),
      entry('info', '[TCP] 192.168.1.45:3 --> 149.154.167.92:443 match RuleSet(Telegram) using Telegram[美国 02]', 3),
      // 同一目标换了节点，单独成组
      entry('info', '[TCP] 192.168.1.45:4 --> 149.154.167.92:443 match RuleSet(Telegram) using Telegram[日本 01]', 4)
    ])
    const groups = groupLogRows(rows)
    expect(groups.map(group => [group.last.connection!.sourceIP, group.last.connection!.node, group.count]))
      .toEqual([['192.168.1.101', '日本 01', 1], ['192.168.1.45', '美国 02', 2], ['192.168.1.45', '日本 01', 1]])
    expect(groups[1].first.entry.time).toBe(rows[0].entry.time)
  })

  it('searches device notes as well as the raw line', () => {
    const [row] = toLogRows([entry('info', '[TCP] 192.168.1.45:1 --> example.com:443 match Match() using DIRECT')])
    expect(rowMatches(row, '书房', { '192.168.1.45': '书房 iPad' })).toBe(true)
    expect(rowMatches(row, 'example', {})).toBe(true)
    expect(rowMatches(row, '客厅', { '192.168.1.45': '书房 iPad' })).toBe(false)
  })
})

describe('Mihomo log view', () => {
  const rows = () => toLogRows([
    entry('info', '[TCP] 192.168.1.45:1 --> 149.154.167.92:443 match RuleSet(Telegram) using Telegram[美国 02]', 1),
    entry('warning', '[TCP] dial 节点选择 (match RuleSet(Proxy)) 192.168.1.31:2 --> www.example.com:443 error: 香港 01 connect timeout', 2),
    entry('info', '[TCP] 192.168.1.45:3 --> 149.154.167.92:443 match RuleSet(Telegram) using Telegram[美国 02]', 3),
    entry('warning', '[TCP] dial 节点选择 (match RuleSet(Proxy)) 192.168.1.31:4 --> www.example.com:443 error: 香港 01 connect timeout', 4),
    entry('debug', '[Sniffer] All sniffing sniff failed', 5),
    entry('info', '[TCP] 192.168.1.63:5 --> ads.example.net:443 match RuleSet(Reject) using REJECT', 6)
  ])
  const notes = { '192.168.1.45': '书房 iPad', '192.168.1.31': 'iPhone 15' }

  it('merges repeats and lifts warnings into cards', () => {
    const wrapper = mount(MihomoLogView, { props: { rows: rows(), notes, merged: true } })
    const alerts = wrapper.findAll('[data-log-alert]')
    expect(alerts).toHaveLength(1)
    expect(alerts[0].text()).toContain('连接失败')
    expect(alerts[0].text()).toContain('×2')
    expect(alerts[0].text()).toContain('iPhone 15 → www.example.com:443')
    // 时间按浏览器时区显示，期望值从同一条目取，避免依赖测试机时区
    const [, , third, , fifth, sixth] = wrapper.props('rows').map(row => row.entry.time)
    const cells = wrapper.findAll('.log-line').map(line => [...line.element.children].map(cell => cell.textContent!.trim()))
    // 两条 Telegram 合并；警告只在卡片里
    expect(cells).toEqual([
      [third, '书房 iPad', '149.154.167.92:443TCP', 'Telegram', 'Telegram · 美国 02', '×2'],
      [fifth, 'DEBUG[Sniffer] All sniffing sniff failed', '×1'],
      [sixth, '192.168.1.63', 'ads.example.net:443TCP', 'Reject', 'REJECT', '×1']
    ])
    expect(wrapper.find('.policy-reject').exists()).toBe(true)
  })

  it('lists every line in order and expands the raw log', async () => {
    const wrapper = mount(MihomoLogView, { props: { rows: rows(), notes, merged: false } })
    const lines = wrapper.findAll('.log-item')
    expect(lines).toHaveLength(6)
    expect(lines[1].classes()).toContain('is-warn')
    await lines[0].get('button').trigger('click')
    expect(lines[0].get('.log-raw').text()).toContain('[TCP] 192.168.1.45:1 --> 149.154.167.92:443')
    expect(lines[0].get('button').attributes('aria-expanded')).toBe('true')
  })

  it('shows the notice instead of rows', () => {
    const wrapper = mount(MihomoLogView, { props: { rows: [], notes: {}, merged: true, notice: '正在读取日志…' } })
    expect(wrapper.text()).toContain('正在读取日志…')
    expect(wrapper.find('.log-item').exists()).toBe(false)
  })
})

describe('Mihomo log grouping of noisy messages', () => {
  it('treats messages that differ only in pointers and numbers as one group', () => {
    const rows = toLogRows([
      entry('warning', '[Metadata] not valid: &constant.Metadata{SrcPort:0xc780, RawSrcAddr:(*net.TCPAddr)(0x2ea606cc6bd0)}', 1),
      entry('warning', '[Metadata] not valid: &constant.Metadata{SrcPort:0xc772, RawSrcAddr:(*net.TCPAddr)(0x2ea606d144e0)}', 2),
      entry('warning', '[DNS] resolve example.com failed', 3)
    ])
    expect(groupLogRows(rows).map(group => group.count)).toEqual([2, 1])
  })

  it('clamps long alert text until clicked', async () => {
    const long = '[Metadata] not valid: ' + 'x'.repeat(400)
    const wrapper = mount(MihomoLogView, { props: { rows: toLogRows([entry('warning', long, 1)]), notes: {}, merged: true } })
    const text = wrapper.get('[data-log-alert-text]')
    expect(text.classes()).toContain('line-clamp-2')
    await text.trigger('click')
    expect(wrapper.get('[data-log-alert-text]').classes()).not.toContain('line-clamp-2')
  })

  it('prefixes the month and day for logs from another day', () => {
    const wrapper = mount(MihomoLogView, {
      props: { rows: toLogRows([entry('info', 'old line', 1, '2020-01-02'), entry('info', 'new line', 2)]), notes: {}, merged: false }
    })
    const times = wrapper.findAll('.log-line .c-time').map(cell => cell.text())
    expect(times).toEqual(['01-02 12:00:01', '12:00:02'])
    expect(wrapper.get('ol').classes()).toContain('has-dates')
  })
})
