import type {
  PushDiffChange, PushDiffDetailGroup, PushDiffFile, PushDiffFileDetails, PushDiffItem, PushDiffList, PushDiffListEntry,
  PushDiffResponse, PushDiffSummary, PushDiffText,
} from '@/api/agentPushDiff'
import { formatBytes } from '@/lib/format'

export const CHANGE_LABELS: Record<PushDiffChange | 'unchanged', string> = {
  added: '新增', removed: '删除', modified: '修改', unchanged: '未变',
}
export const CHANGE_CHIPS: Record<PushDiffChange, string> = { added: 'chip-ok', removed: 'chip-bad', modified: 'chip-warn' }
export const ROLE_LABELS: Record<string, string> = { config: '主配置', provider: '订阅', rule: '规则集', custom: '附加文件' }

export type Tone = 'plain' | 'muted' | 'added' | 'removed'
export interface Segment { text: string; tone: Tone }

/** 打开「隐藏敏感信息」后，敏感片段显示的文字 */
export const HIDDEN_TEXT = '已隐藏'

/** 显示文字的一段；secret 为服务端标出的敏感片段 */
export interface TextPiece { text: string; secret: boolean }

export function textPieces(value: PushDiffText | null | undefined): TextPiece[] {
  if (value == null || value === '') return []
  if (typeof value === 'string') return [{ text: value, secret: false }]
  return value.map(part => typeof part === 'string' ? { text: part, secret: false } : { text: part.secret, secret: true })
}

/** 拼成纯文本；hidden 时敏感片段换成「已隐藏」 */
export function piecesText(pieces: TextPiece[], hidden: boolean): string {
  return pieces.map(piece => piece.secret && hidden ? HIDDEN_TEXT : piece.text).join('')
}

export const textOf = (value: PushDiffText | null | undefined, hidden: boolean) => piecesText(textPieces(value), hidden)

const plain = (text: string): TextPiece => ({ text, secret: false })

/** 文件表第一列：主配置、订阅名、规则集名，没有名称时用文件名 */
export function fileTitle(file: PushDiffFile): PushDiffText {
  if (file.role === 'config') return '主配置'
  return file.name || file.path.split('/').pop() || file.path
}

export function fileRole(file: PushDiffFile): string {
  return (file.role && ROLE_LABELS[file.role]) || '文件'
}

function delta(added = 0, removed = 0): Segment[] {
  return [{ text: `+${added}`, tone: 'added' }, { text: ' / ', tone: 'muted' }, { text: `−${removed}`, tone: 'removed' }]
}

/** 摘要拆成带色调的片段，模板逐段着色；纯文本见 summaryText */
export function summarySegments(file: PushDiffFile): Segment[] {
  const summary = file.summary
  if (!summary) {
    return [{ text: file.status === 'removed' ? '上次推送的内容未保存' : '上次推送的内容未保存，无法细分', tone: 'muted' }]
  }
  return segmentsFor(summary)
}

function segmentsFor(summary: PushDiffSummary): Segment[] {
  if (summary.kind === 'nodes' || summary.kind === 'rules') {
    const noun = summary.kind === 'nodes' ? '节点' : '规则'
    const unit = summary.kind === 'nodes' ? '个' : '条'
    const added = summary.added || 0, removed = summary.removed || 0, modified = summary.modified || 0
    const total = summary.after ?? summary.before
    const parts: Segment[] = [{ text: `${noun} `, tone: 'plain' }, ...delta(added, removed)]
    if (modified) parts.push({ text: ` · 修改 ${modified}`, tone: 'plain' })
    if (!added && !removed && !modified) {
      return [{ text: summary.kind === 'nodes' ? '节点未增减，其他内容有变化' : '规则未增减，顺序或格式有变化', tone: 'plain' },
        ...(typeof total === 'number' ? [{ text: ` · 共 ${total} ${unit}`, tone: 'muted' as Tone }] : [])]
    }
    if (typeof summary.after === 'number') parts.push({ text: ` · 共 ${summary.after} ${unit}`, tone: 'muted' })
    else if (typeof summary.before === 'number') parts.push({ text: ` · 原有 ${summary.before} ${unit}`, tone: 'muted' })
    return parts
  }
  if (summary.kind === 'config') {
    const size = sizeText(summary.before, summary.after)
    return [{ text: summary.changes ? `${summary.changes} 项配置变化` : '配置项未变，格式有变化', tone: 'plain' },
      ...(size ? [{ text: ` · ${size}`, tone: 'muted' as Tone }] : [])]
  }
  return [{ text: sizeText(summary.before, summary.after) || '大小未知', tone: 'plain' }]
}

export function sizeText(before?: number | null, after?: number | null): string {
  const hasBefore = typeof before === 'number', hasAfter = typeof after === 'number'
  if (hasBefore && hasAfter) {
    if (before === after) return `大小不变（${formatBytes(after)}）`
    const change = after! - before!
    return `${formatBytes(before)} → ${formatBytes(after)}（${change > 0 ? '+' : '−'}${formatBytes(Math.abs(change))}）`
  }
  if (hasAfter) return formatBytes(after)
  if (hasBefore) return `原 ${formatBytes(before)}`
  return ''
}

export const summaryText = (file: PushDiffFile) => summarySegments(file).map(part => part.text).join('')

/** 列表类配置项的新值列：条数、增减与顺序 */
export function listSegments(list: PushDiffList): Segment[] {
  const parts: Segment[] = [{ text: `${list.after} 条 `, tone: 'plain' }]
  if (list.added || list.removed) parts.push(...delta(list.added, list.removed))
  if (list.modified) parts.push({ text: `${list.added || list.removed ? ' · ' : ''}修改 ${list.modified}`, tone: 'plain' })
  if (list.reordered) parts.push({ text: `${list.added || list.removed || list.modified ? ' · ' : ''}顺序调整`, tone: 'muted' })
  return parts
}

/** 原值 / 新值单元格：值本身（敏感片段由模板按开关显示），缺失时为「—」 */
export function valueText(item: PushDiffItem, side: 'old' | 'new'): { value: PushDiffText; empty: boolean } {
  const absent = (side === 'old' && item.change === 'added') || (side === 'new' && item.change === 'removed')
  if (absent) return { value: '—', empty: true }
  if (item.list) return { value: `${side === 'old' ? item.list.before : item.list.after} 条`, empty: false }
  const value = side === 'old' ? item.old : item.new
  return value == null ? { value: '—', empty: true } : { value, empty: false }
}

function joined(values: PushDiffText[], separator: string): TextPiece[] {
  return values.flatMap((value, index) => [...(index ? [plain(separator)] : []), ...textPieces(value)])
}

/** 条目内的字段变化，例如「port：443 → 8388」；password 这类值整段是敏感片段 */
export function fieldPieces(field: PushDiffItem): TextPiece[] {
  const label = plain(`${field.key}：`)
  if (field.list) {
    const groups: TextPiece[][] = []
    if (field.list.added_items?.length) groups.push([plain('+ '), ...joined(field.list.added_items, '、')])
    if (field.list.removed_items?.length) groups.push([plain('− '), ...joined(field.list.removed_items, '、')])
    const more = (field.list.added - (field.list.added_items?.length || 0)) + (field.list.removed - (field.list.removed_items?.length || 0))
    if (more > 0) groups.push([plain(`另有 ${more} 条`)])
    if (field.list.reordered) groups.push([plain('顺序调整')])
    if (!groups.length) return [label, plain(`${field.list.before} → ${field.list.after} 条`)]
    return [label, ...groups.flatMap((group, index) => [...(index ? [plain('；')] : []), ...group])]
  }
  if (field.change === 'added') return [label, plain('新增 '), ...textPieces(field.new)]
  if (field.change === 'removed') return [label, plain('删除（原 '), ...textPieces(field.old), plain('）')]
  return [label, ...(field.old == null ? [plain('—')] : textPieces(field.old)), plain(' → '),
    ...(field.new == null ? [plain('—')] : textPieces(field.new))]
}

export const fieldText = (field: PushDiffItem, hidden = false) => piecesText(fieldPieces(field), hidden).trimEnd()

/** 节点表「变更前 / 变更后」的一格；empty 为「—」（这一侧没有这个节点或字段） */
export interface NodeValue { pieces: TextPiece[]; empty: boolean }

/**
 * 节点表的一行：change 为新增、删除的节点（变更项写「新增 / 删除」，另一侧是类型 · 服务器:端口），
 * field 为修改的节点的一个字段（变更项写字段路径），note 为跨三列的说明（另有 N 处变化）。
 */
export type NodeRow =
  | { kind: 'change'; change: PushDiffChange; before: NodeValue; after: NodeValue }
  | { kind: 'field'; key: string; before: NodeValue; after: NodeValue }
  | { kind: 'note'; text: string }

const NO_VALUE: NodeValue = { pieces: [plain('—')], empty: true }

/** 修改的节点里一个字段的原值或新值；列表字段写条数，删掉的条目在变更前一侧、加上的在变更后一侧 */
export function fieldSide(field: PushDiffItem, side: 'old' | 'new'): NodeValue {
  if ((side === 'old' && field.change === 'added') || (side === 'new' && field.change === 'removed')) return NO_VALUE
  if (field.list) {
    const list = field.list
    const items = (side === 'old' ? list.removed_items : list.added_items) || []
    const count = side === 'old' ? list.removed : list.added
    const pieces = [plain(`${side === 'old' ? list.before : list.after} 条`)]
    if (items.length) {
      pieces.push(plain(`：${side === 'old' ? '−' : '+'} `), ...joined(items, '、'))
      if (count > items.length) pieces.push(plain(` 等 ${count} 条`))
    } else if (count) {
      pieces.push(plain(`：${side === 'old' ? '删除' : '新增'} ${count} 条`))
    }
    if (side === 'new' && list.reordered) pieces.push(plain('，顺序调整'))
    return { pieces, empty: false }
  }
  const value = side === 'old' ? field.old : field.new
  return value == null ? NO_VALUE : { pieces: textPieces(value), empty: false }
}

/** 一个节点在表里占的行：新增、删除一行，修改的节点每个字段一行（type、server、port 在前，服务端已排好） */
export function nodeRows(entry: PushDiffListEntry): NodeRow[] {
  if (entry.change !== 'modified') {
    const info: NodeValue = entry.info ? { pieces: textPieces(entry.info), empty: false } : NO_VALUE
    return [{ kind: 'change', change: entry.change, before: entry.change === 'removed' ? info : NO_VALUE,
      after: entry.change === 'added' ? info : NO_VALUE }]
  }
  const rows = (entry.fields || []).map((field): NodeRow => ({
    kind: 'field', key: field.key, before: fieldSide(field, 'old'), after: fieldSide(field, 'new') }))
  if (entry.fields_omitted) rows.push({ kind: 'note', text: `另有 ${entry.fields_omitted} 处变化` })
  // 逐个字段比较没有差别（例如只是键的顺序不同）时也占一行，节点名不会悬空
  if (!rows.length) rows.push({ kind: 'note', text: '字段顺序或写法有变化' })
  return rows
}

/** 表 2 按顶层键分组时的中文名；其他键直接显示原名，顶层的单个值（secret、mixed-port 等）归入「基础设置」 */
export const CONFIG_GROUP_NAMES: Record<string, string> = {
  dns: 'DNS', hosts: 'Hosts', rules: '分流规则', 'proxy-groups': '代理组', proxies: '节点', 'proxy-providers': '订阅',
  'rule-providers': '规则集', sniffer: '嗅探', tun: 'TUN', listeners: '入站监听',
}
export const BASIC_GROUP_NAME = '基础设置'
/** 改动超过这么多行的组默认收起 */
export const CONFIG_GROUP_FOLD = 20

export type ChangeCounts = Record<PushDiffChange, number>

/** 表 2 的一行配置项：label 为组内的相对路径（块里的行只写子键），nested 表示在某个块下面 */
export interface ConfigRow { item: PushDiffItem; index: number; label: string; nested: boolean }
/** 组里的一行：配置项，或同一个映射块有 2 处及以上改动时的块标题 */
export type ConfigLine = { kind: 'row'; row: ConfigRow } | { kind: 'block'; name: string; counts: ChangeCounts }
/** 一组：id 为顶层键，基础设置为空串；raw 为标题里等宽显示的原始键名（没有中文名或基础设置时为空） */
export interface ConfigGroup { id: string; name: string; raw: string; counts: ChangeCounts; size: number; lines: ConfigLine[]; folded: boolean }

/** 配置项的键路径；旧版服务端没有 path 时按点号拆（域名会被拆开，只影响分组） */
export function itemPath(item: PushDiffItem): string[] {
  return item.path?.length ? item.path : item.key.split('.')
}

function countChanges(rows: ConfigRow[]): ChangeCounts {
  const counts: ChangeCounts = { added: 0, removed: 0, modified: 0 }
  for (const row of rows) counts[row.item.change] += 1
  return counts
}

/** 组或块的汇总，例如「新增 3 项 · 修改 1 项」 */
export function countsText(counts: ChangeCounts): string {
  return (['added', 'removed', 'modified'] as const).filter(change => counts[change])
    .map(change => `${CHANGE_LABELS[change]} ${counts[change]} 项`).join(' · ')
}

/**
 * 表 2 按顶层键分组（顺序按第一次出现）。组内只写相对路径；同一个映射块（相对路径的第一段，
 * 如 dns.proxy-server-nameserver-policy、proxy-providers.某订阅）有 2 处及以上改动时加一行块标题，
 * 下面只写子键；只有 1 处时直接写相对路径。改动超过 CONFIG_GROUP_FOLD 行的组默认收起。
 */
export function configGroups(items: PushDiffItem[]): ConfigGroup[] {
  const buckets = new Map<string, { item: PushDiffItem; index: number; rel: string[]; basic: boolean }[]>()
  items.forEach((item, index) => {
    const path = itemPath(item)
    const top = path[0] ?? item.key
    const basic = path.length <= 1 && !(top in CONFIG_GROUP_NAMES)
    const id = basic ? '' : top
    if (!buckets.has(id)) buckets.set(id, [])
    buckets.get(id)!.push({ item, index, rel: basic ? path : path.slice(1), basic })
  })
  return [...buckets].map(([id, members]) => {
    const blockOf = (rel: string[]) => rel.length >= 2 ? rel[0]! : null
    const sizes = new Map<string, number>()
    for (const member of members) {
      const block = blockOf(member.rel)
      if (block != null) sizes.set(block, (sizes.get(block) || 0) + 1)
    }
    const shared = (rel: string[]) => {
      const block = blockOf(rel)
      return block != null && (sizes.get(block) || 0) >= 2 ? block : null
    }
    const rowOf = (member: (typeof members)[number], nested: boolean): ConfigRow => ({
      item: member.item, index: member.index, nested,
      label: nested ? member.rel.slice(1).join('.') : (member.rel.join('.') || id || member.item.key),
    })
    const lines: ConfigLine[] = []
    const done = new Set<string>()
    const rows: ConfigRow[] = []
    for (const member of members) {
      const block = shared(member.rel)
      if (block == null) {
        const row = rowOf(member, false)
        rows.push(row)
        lines.push({ kind: 'row', row })
        continue
      }
      if (done.has(block)) continue
      done.add(block)
      const inside = members.filter(other => shared(other.rel) === block).map(other => rowOf(other, true))
      rows.push(...inside)
      lines.push({ kind: 'block', name: block, counts: countChanges(inside) }, ...inside.map(row => ({ kind: 'row' as const, row })))
    }
    const known = CONFIG_GROUP_NAMES[id]
    return { id, name: id ? known || id : BASIC_GROUP_NAME, raw: known ? id : '', counts: countChanges(rows), size: rows.length,
      lines, folded: rows.length > CONFIG_GROUP_FOLD }
  })
}

/** 长路径、长键名和值只在「.」「-」「/」之后断行：按这些位置切开，模板在段之间插 <wbr> */
export function breakSegments(text: string): string[] {
  return text ? text.split(/(?<=[./-])(?=.)/) : []
}

/** 明细分组标题里的单位：「新增 2 个节点」「删除 1 条规则」 */
export function detailUnit(kind: PushDiffFileDetails['kind']): { noun: string; unit: string } {
  return kind === 'nodes' ? { noun: '节点', unit: '个' } : { noun: '规则', unit: '条' }
}

/** 同名节点的序号，例如「（同名第 2 个）」 */
export const nthText = (nth?: number) => nth ? `（同名第 ${nth} 个）` : ''

/** 明细末尾的说明：重名节点怎么对应、明细总量是否已达上限（可以加载更多时不必说明） */
export function detailNotes(details: PushDiffFileDetails): string[] {
  const notes: string[] = []
  if (details.repeated) {
    notes.push(`有 ${details.repeated} 个节点名被多个节点共用：内容相同的视为未变，其余按出现顺序对应。`)
  }
  if (details.limited && !details.paging) notes.push('这次变化较多，明细已达总量上限，没有列出的只计入数量。')
  return notes
}

/** 每次「加载更多」读取的条数 */
export const PAGE_SIZE = 50
/** 搜索框停止输入多久后才向服务端搜索 */
export const SEARCH_DELAY_MS = 300
/** 搜索词最长字符数，与服务端一致 */
export const MAX_QUERY_CHARS = 200

/** 一组明细的分页进度：还能加载多少条，多少条超出明细上限没有保留（不能分页时都为 0） */
export function groupPaging(group: PushDiffDetailGroup, loaded: number, pageable: boolean) {
  if (!pageable) return { remaining: 0, notKept: 0 }
  const available = group.available ?? group.total
  return { remaining: Math.max(0, available - loaded), notKept: Math.max(0, group.total - Math.max(available, loaded)) }
}

const NO_BASELINE = '没有可对比的基准'

/** 拿不到差异时的标题与说明；只认服务端的原因代码，不显示原始错误 */
export function unavailableCopy(result: PushDiffResponse): { title: string; description: string; pushable: boolean } {
  if (result.status === 'never-pushed') {
    return { title: NO_BASELINE, description: '这台设备还没有成功推送过配置。推送一次后，这里会列出之后每次推送的变化。', pushable: true }
  }
  switch (result.reason) {
    case 'baseline_missing':
      return { title: NO_BASELINE, description: '上次推送由旧版本完成，没有记录完整的发布内容。成功推送一次后即可查看变化。', pushable: true }
    case 'baseline_invalid':
      return { title: NO_BASELINE, description: '上次推送的记录不完整，无法作为基准。成功推送一次后即可查看变化。', pushable: true }
    case 'profile_mismatch':
      return { title: NO_BASELINE, description: '这台 Agent 已改绑配置空间，上次推送的是原配置空间的内容。推送一次后即可重新对比。', pushable: true }
    case 'service_mismatch':
      return { title: NO_BASELINE, description: '这台 Agent 的服务类型与上次推送时不同。推送一次后即可重新对比。', pushable: true }
    case 'version_mismatch':
      return { title: NO_BASELINE, description: '设备上运行的配置与上次推送的记录不一致（可能在设备上改过或已回滚），无法确定基准。推送一次后即可重新对比。', pushable: true }
    case 'deployment_pending':
      return { title: '正在推送', description: '这台设备有尚未完成的推送任务，完成后再查看变化。', pushable: false }
    case 'profile_unavailable':
      return { title: '无法对比', description: 'Agent 绑定的配置空间暂时不可用，请检查配置空间后重试。', pushable: false }
    case 'check_failed':
      return { title: '无法生成这次将要推送的内容', description: '生成发布包时出错。错误原文可能含有令牌等敏感内容，这里不显示；点「推送」会给出具体原因，也可以到配置生成页检查。', pushable: true }
    default:
      return { title: '暂时无法对比', description: '服务端没有给出可对比的结果，请稍后重试。', pushable: false }
  }
}

/** 已确认有变更、但只能按文件摘要比较或无法列出时的说明 */
export function contentNotice(result: PushDiffResponse): string {
  if (result.files == null) {
    if (result.reason === 'baseline_files_missing') return '上次推送的记录没有逐个文件的摘要（旧版本推送），只能确认有变更。推送一次后即可查看具体内容。'
    return '已确认有变更，但列出具体内容时出错，请稍后重试。'
  }
  if (result.content !== 'digests') return ''
  if (result.content_reason === 'service_without_bundle') {
    return 'MosDNS 推送时不保存发布包，这里只能列出哪些文件变了，无法细分到规则或配置项。'
  }
  return '上次推送的发布包没有保存（可能由旧版本推送），这里只能列出哪些文件变了；推送一次后即可查看具体内容。'
}

/** 主配置区无法按配置项对比时的说明 */
export function configNotice(result: PushDiffResponse): string {
  const config = result.config
  if (!config) return '主配置没有变化。'
  if (config.status === 'formatting') return '配置项没有变化，只是键的顺序或格式不同。'
  if (config.status === 'unparsable') return '主配置不是可解析的 YAML，只能按文件比较。'
  if (config.status === 'unavailable') {
    return config.reason === 'service_without_bundle'
      ? 'MosDNS 不保存上次推送的主配置，无法按配置项对比。'
      : '上次推送的主配置没有保存，无法按配置项对比。'
  }
  return ''
}

export type DiffLineTone = 'added' | 'removed' | 'hunk' | 'meta' | 'plain'

/** 原始差异逐行着色 */
export function diffLineTone(line: string): DiffLineTone {
  if (line.startsWith('+++') || line.startsWith('---')) return 'meta'
  if (line.startsWith('@@')) return 'hunk'
  if (line.startsWith('+')) return 'added'
  if (line.startsWith('-')) return 'removed'
  return 'plain'
}
