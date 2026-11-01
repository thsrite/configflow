import type {
  PushDiffChange, PushDiffFile, PushDiffFileDetails, PushDiffItem, PushDiffList, PushDiffResponse, PushDiffSummary,
  PushDiffText,
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

/** 明细分组标题里的单位：「新增 2 个节点」「删除 1 条规则」 */
export function detailUnit(kind: PushDiffFileDetails['kind']): { noun: string; unit: string } {
  return kind === 'nodes' ? { noun: '节点', unit: '个' } : { noun: '规则', unit: '条' }
}

/** 同名节点的序号，例如「（同名第 2 个）」 */
export const nthText = (nth?: number) => nth ? `（同名第 ${nth} 个）` : ''

/** 明细末尾的说明：重名节点怎么对应、明细总量是否已达上限 */
export function detailNotes(details: PushDiffFileDetails): string[] {
  const notes: string[] = []
  if (details.repeated) {
    notes.push(`有 ${details.repeated} 个节点名被多个节点共用：内容相同的视为未变，其余按出现顺序对应。`)
  }
  if (details.limited) notes.push('这次变化较多，明细已达总量上限，没有列出的只计入数量。')
  return notes
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
