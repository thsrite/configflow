import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PushDiffNodeTable from '@/components/agents/PushDiffNodeTable.vue'
import type { PushDiffItem, PushDiffListEntry } from '@/api/agentPushDiff'
import { fieldSide, nodeRows, piecesText, type NodeRow } from '@/lib/agentPushDiff'

const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})

const field = (overrides: Partial<PushDiffItem>): PushDiffItem => ({ key: 'port', change: 'modified', sensitive: false, ...overrides })
const side = (item: PushDiffItem, which: 'old' | 'new') => {
  const value = fieldSide(item, which)
  return [piecesText(value.pieces, false), value.empty]
}
/** A row as plain cells: [变更项, 变更前, 变更后], or the note across them. */
const flat = (row: NodeRow) => row.kind === 'note' ? row.text
  : [row.kind === 'change' ? row.change : row.key, piecesText(row.before.pieces, false), piecesText(row.after.pieces, false)]

describe('node table rows', () => {
  it('gives an added or removed node one row with its type and address on its own side', () => {
    expect(nodeRows({ change: 'added', text: 'C', info: 'trojan · c.lab:443' }).map(flat)).toEqual([['added', '—', 'trojan · c.lab:443']])
    expect(nodeRows({ change: 'removed', text: 'E', info: 'socks5 · [2001:db8::e]:1080' }).map(flat))
      .toEqual([['removed', 'socks5 · [2001:db8::e]:1080', '—']])
    // An older server without the removed node's info: a dash, not an empty cell.
    expect(nodeRows({ change: 'removed', text: 'E' }).map(flat)).toEqual([['removed', '—', '—']])
  })

  it('gives a modified node a row per field, then what was left out', () => {
    const entry: PushDiffListEntry = { change: 'modified', text: 'A', fields_omitted: 3, fields: [
      field({ key: 'server', old: 'a.lab', new: '2001:db8::a' }), field({ key: 'udp', change: 'added', new: 'true' }),
      field({ key: 'plugin-opts.host', change: 'removed', old: 'cdn.lab' })] }
    expect(nodeRows(entry).map(flat)).toEqual([['server', 'a.lab', '2001:db8::a'], ['udp', '—', 'true'],
      ['plugin-opts.host', 'cdn.lab', '—'], '另有 3 处变化'])
    // Nothing listable (only the order of keys differs): still one row, so the name heads something.
    expect(nodeRows({ change: 'modified', text: 'B', fields: [], fields_omitted: 0 }).map(flat)).toEqual(['字段顺序或写法有变化'])
  })

  it('puts a list field’s removed items before and added ones after', () => {
    const alpn = field({ key: 'alpn', list: { before: 2, after: 2, added: 1, removed: 1, modified: 0, reordered: false,
      added_items: ['h3'], removed_items: ['http/1.1'] } })
    expect([side(alpn, 'old'), side(alpn, 'new')]).toEqual([['2 条：− http/1.1', false], ['2 条：+ h3', false]])
    const many = field({ key: 'alpn', list: { before: 1, after: 9, added: 8, removed: 0, modified: 0, reordered: true,
      added_items: ['a', 'b', 'c'], removed_items: [] } })
    expect([side(many, 'old'), side(many, 'new')]).toEqual([['1 条', false], ['9 条：+ a、b、c 等 8 条，顺序调整', false]])
    const counted = field({ key: 'alpn', list: { before: 3, after: 1, added: 0, removed: 2, modified: 0, reordered: false } })
    expect(side(counted, 'old')).toEqual(['3 条：删除 2 条', false])
    const appeared = field({ key: 'alpn', change: 'added', list: { before: 0, after: 1, added: 1, removed: 0, modified: 0,
      reordered: false, added_items: ['h2'], removed_items: [] } })
    expect([side(appeared, 'old'), side(appeared, 'new')]).toEqual([['—', true], ['1 条：+ h2', false]])
  })

  it('keeps a credential a marked piece on either side', () => {
    const password = field({ key: 'password', sensitive: true, old: [{ secret: 'before-pw' }], new: [{ secret: 'after-pw' }] })
    expect(fieldSide(password, 'old').pieces).toEqual([{ text: 'before-pw', secret: true }])
    expect(piecesText(fieldSide(password, 'new').pieces, true)).toBe('已隐藏')
  })
})

describe('node table', () => {
  const ENTRIES: PushDiffListEntry[] = [
    { change: 'modified', text: 'A 香港', nth: 2, fields_omitted: 1, fields: [
      field({ key: 'server', old: 'edge-a.lab', new: 'edge-a2.lab' }),
      field({ key: 'password', sensitive: true, old: [{ secret: 'node-pw-before' }], new: [{ secret: 'node-pw-after' }] }),
      field({ key: 'plugin-opts.host', old: 'cdn-a.lab', new: ['user:', { secret: 'cdn-token-1' }, '@cdn-b.lab'] })] },
    { change: 'modified', text: 'B', fields: [field({ key: 'port', old: '1', new: '2' })], fields_omitted: 0 },
  ]

  function render(entries: PushDiffListEntry[], hidden = false) {
    const wrapper = mount(PushDiffNodeTable, { attachTo: document.body, props: { entries, hidden, labelledby: 'group-title' } })
    wrappers.push(wrapper)
    return wrapper
  }

  it('lays out 节点 | 变更项 | 变更前 | 变更后 with equal before and after columns', () => {
    const table = render(ENTRIES).element as HTMLTableElement
    expect(table.className).toContain('cf-mobile-records')
    expect(table.getAttribute('aria-labelledby')).toBe('group-title')
    expect([...table.querySelectorAll('thead th')].map(cell => [cell.textContent, cell.getAttribute('scope')]))
      .toEqual([['节点', 'col'], ['变更项', 'col'], ['变更前', 'col'], ['变更后', 'col']])
    const widths = [...table.querySelectorAll('col')].map(col => col.className)
    expect(widths).toEqual(['w-[24%]', 'w-[18%]', 'w-[29%]', 'w-[29%]'])
    // One tbody per node; the name (with 同名第 N 个) spans the node's rows, the note spans the three value columns.
    const [first, second] = [...table.tBodies]
    expect(first!.rows).toHaveLength(4)
    const name = first!.querySelector('th')!
    expect([name.textContent, name.getAttribute('rowspan'), name.getAttribute('scope')]).toEqual(['A 香港（同名第 2 个）', '4', 'rowgroup'])
    expect(first!.querySelectorAll('th')).toHaveLength(1)
    const note = first!.querySelector('[data-node-note]')!
    expect([note.textContent, note.getAttribute('colspan')]).toEqual(['另有 1 处变化', '3'])
    expect(second!.querySelector('th')!.getAttribute('rowspan')).toBe('1')
    // Field paths and values in monospace; the phone-only separators are hidden from screen readers.
    expect(first!.querySelector('[data-node-key] .font-mono')!.textContent).toBe('server')
    expect(first!.querySelector('[data-node-after]')!.className).toContain('font-mono')
    expect([...first!.querySelectorAll('[aria-hidden="true"]')].map(node => node.textContent)).toEqual(['：', ' → ', '：', ' → ', '：', ' → '])
  })

  it('marks only an added or removed node’s missing side for the phone to drop', () => {
    const table = render([{ change: 'added', text: 'C', info: 'trojan · c.lab:443' },
      { change: 'modified', text: 'D', fields: [field({ key: 'udp', change: 'added', new: 'true' })], fields_omitted: 0 }]).element
    const [added, modified] = [...(table as HTMLTableElement).tBodies]
    expect(added!.querySelector('[data-node-before]')!.className).toContain('push-diff-empty')
    expect(added!.querySelector('[data-node-after]')!.className).not.toContain('push-diff-empty')
    expect(added!.querySelector('[aria-hidden="true"]')).toBeNull()  // 新增 chip, then the address: no arrow
    // A field that appears keeps its dash: 「udp：— → true」.
    const before = modified!.querySelector('[data-node-before]')!
    expect([before.textContent, before.className.includes('push-diff-empty')]).toEqual(['—', false])
    expect(modified!.querySelector('tr')!.textContent).toBe('Dudp：— → true')
  })

  it('shows 已隐藏 for credentials when hidden, with no original left in the page', async () => {
    const wrapper = render(ENTRIES)
    for (const secret of ['node-pw-before', 'node-pw-after', 'cdn-token-1']) expect(document.body.innerHTML).toContain(secret)
    await wrapper.setProps({ hidden: true })
    for (const secret of ['node-pw-before', 'node-pw-after', 'cdn-token-1']) expect(document.body.innerHTML).not.toContain(secret)
    expect(wrapper.element.querySelectorAll('[data-secret-hidden]')).toHaveLength(3)
    const rows = [...wrapper.element.querySelectorAll('tbody tr')].map(row => [...row.querySelectorAll('td')].map(cell => cell.textContent).join(''))
    expect(rows.slice(0, 3)).toEqual(['server：edge-a.lab → edge-a2.lab', 'password：已隐藏 → 已隐藏',
      'plugin-opts.host：cdn-a.lab → user:已隐藏@cdn-b.lab'])
  })
})
