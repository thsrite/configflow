<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { Check, ChevronRight, ChevronsUpDown, Loader2, Pencil, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import FormField from '@/components/common/FormField.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import Segmented from '@/components/common/Segmented.vue'
import RuleDnsField from '@/components/rules/RuleDnsField.vue'
import RuleContentIssues from '@/components/rules/RuleContentIssues.vue'
import RuleSourceEditor from '@/components/rules/RuleSourceEditor.vue'
import api, { ruleApi, ruleSetApi } from '@/api'
import { confirm, notify } from '@/lib/feedback'
import { isOxidnsRuleType } from '@/lib/oxidns'
import { CLASSICAL_CONTENT_HINT, prepareRuleContent, type RuleContentCheck } from '@/lib/ruleset-content'
import { detectRuleContentFormat, domainRuleLine } from '@/lib/ruleset-format'
import {
  DNS_COPY, DNS_FIELD_HELP, IP_DNS_RULE_TYPES, NO_RESOLVE_RULE_TYPES, PASTE_LIMIT, PASTE_SUGGEST_RULESET, PINNED_HINT,
  RULE_TYPES, SINGLE_RULE_TYPES, choiceLabel, confirmSaveWithoutDns, copyKey, defaultDnsChoice, dnsErrorReason, dnsWarnings, followRoute, isRoutingLocked, isUpstreamValue,
  notifyRuleSaved, parsePastedRules, pasteToRuleset, pinnedWithheld, rulePayload, ruleSetPayload, ruleValueError, splitPaste, stateValue, type DnsWarning
} from '@/lib/ruleDns'
import type { ComposePayload, RuleDnsSnapshot, RuleDnsState, RuleDnsValue } from '@/types'

type SourceKind = 'values' | 'library' | 'new_library'
type Item = Record<string, any>

/**
 * 策略规则的一页编辑器：匹配来源、策略、DNS 与更多选项。
 * 新建统一走 POST /rules/compose（规则、共享规则库与 DNS 同一事务）；
 * 编辑沿用规则 / 规则集的 PUT，只有 DNS 改变时才带 dns。
 */
const props = defineProps<{
  open: boolean
  mode: 'create' | 'edit'
  /** 编辑时的原始规则或规则集引用 */
  item?: Item | null
  initialSource?: SourceKind
  /** 当前配置空间的全部规则，用于重复检查与已添加的规则集 */
  rules: Item[]
  library: Item[]
  policies: string[]
  dns: RuleDnsSnapshot | null
  dnsLoading?: boolean
  profileId: string
}>()
const emit = defineEmits<{
  'update:open': [value: boolean]
  /** dns 为 compose 返回的新规则 DNS 状态，可直接并入列表快照 */
  saved: [result: { ids: string[]; dns?: Record<string, RuleDnsState> }]
  enable: [id: string]
  'source-saved': [source: Item]
  'reload-dns': []
}>()

const SOURCE_OPTIONS: Array<{ value: SourceKind; label: string }> = [
  { value: 'values', label: '输入规则' }, { value: 'library', label: '规则库' }, { value: 'new_library', label: '新建规则集' }
]
const LIBRARY_SOURCES: Array<{ value: 'url' | 'content'; label: string }> = [{ value: 'url', label: '远程地址' }, { value: 'content', label: '粘贴内容' }]
const POSITIONS: Array<{ value: 'top' | 'bottom'; label: string }> = [{ value: 'top', label: '顶部' }, { value: 'bottom', label: '底部' }]
const BEHAVIORS = [
  { value: 'domain', label: '域名 domain' }, { value: 'ipcidr', label: 'IP 网段 ipcidr' }, { value: 'classical', label: '混合 classical' }
]
const DOMAIN_SCOPES = [
  { value: 'DOMAIN', label: '仅此域名' }, { value: 'DOMAIN-SUFFIX', label: '此域名及其子域名' }, { value: 'PATTERN', label: '按输入内容（含通配符）' }
]

const source = ref<SourceKind>('values')
const editingSource = ref<Item | null>(null)
const sourceEditor = ref<InstanceType<typeof RuleSourceEditor> | null>(null)
const sourceSaving = ref(false)
const sourceDirty = ref(false)
const sourceOverrides = ref<Record<string, Item>>({})
const editorBody = ref<HTMLElement | null>(null)
let sourceScrollTop = 0
const contentCheck = ref<RuleContentCheck | null>(null)
const form = reactive({
  ruleType: 'DOMAIN-SUFFIX',
  text: '',
  value: '',
  libraryIds: [] as string[],
  library: { name: '', source_type: 'url' as 'url' | 'content', url: '', content: '', behavior: 'domain', format: 'auto', scope: '' },
  policy: 'DIRECT',
  remark: '',
  group_name: '',
  position: 'top' as 'top' | 'bottom',
  enabled: true,
  no_resolve: false,
  udp_fallback: '' as UdpFallback,
  ntp_direct: false
})
const dnsChoice = ref<RuleDnsValue>('default')
const dnsTouched = ref(false)
/** 编辑偏离的跟随规则时，选择「按策略更新 DNS」 */
const resync = ref(false)
const saving = ref(false)
const busy = computed(() => saving.value || sourceSaving.value)
const saveError = ref('')
const libraryOpen = ref(false)
const urlCheck = ref<'idle' | 'checking' | 'ok' | 'failed'>('idle')
let initial = ''
let initialDns: RuleDnsValue = 'default'
let session = 0

const editing = computed(() => props.mode === 'edit')
const editItem = computed(() => (editing.value ? props.item || null : null))
const isRuleset = computed(() => (editing.value ? editItem.value?.itemType === 'ruleset' : source.value !== 'values'))
const engine = computed(() => props.dns?.engine || null)
const dnsLoaded = computed(() => !!props.dns)
const inUse = computed(() => engine.value?.oxidns_in_use !== false)
const locked = computed(() => engine.value?.editable === false)
const entry = computed(() => (editItem.value ? props.dns?.rules[editItem.value.id] : undefined))
const libraryFormat = computed(() => detectRuleContentFormat(form.library.content))
const singleType = computed(() => !editing.value && source.value === 'values' && SINGLE_RULE_TYPES.includes(form.ruleType))
type UdpFallback = '' | 'DIRECT' | 'REJECT'
const isMatch = computed(() => source.value === 'values' && (editing.value || singleType.value) && form.ruleType === 'MATCH')
const UDP_FALLBACK_OPTIONS: { value: UdpFallback; label: string }[] = [
  { value: '', label: '客户端默认' }, { value: 'DIRECT', label: '直连' }, { value: 'REJECT', label: '拒绝' }
]
const UDP_FALLBACK_HELP = [
  '规则命中的代理节点不支持 UDP（例如没有开启 udp 的 ss 节点）时，QUIC、STUN 等 UDP 连接的去向。Mihomo 会跳过这类规则继续向下匹配，全部跳过后直连。',
  '客户端默认：Mihomo 与 Loon 直连；Surge 6 起默认拒绝。',
  '直连：连接能通，但会绕过代理，网站可能看到真实 IP。',
  '拒绝：Mihomo 在全部规则最后追加 NETWORK,udp,REJECT，Surge 写入 udp-policy-not-supported-behaviour，Loon 写入 udp-fallback-mode。浏览器的 QUIC 会改用 TCP 走代理。',
  '基础配置的 [General] 已写对应项时，Surge 与 Loon 以基础配置为准。'
].join('\n\n')
const NTP_DIRECT_HELP = [
  '在全部规则最前面加一条 UDP 123 端口直连规则（Surge、Loon 写作 PROTOCOL,UDP），对时不再经过代理，也不受节点是否支持 UDP 影响。',
  '对时服务器还需要返回真实地址：新的默认模板已把 +.pool.ntp.org 加进 fake-ip-filter；已有的 Mihomo 基础配置请在 dns.fake-ip-filter 中自行加入这一项。'
].join('\n\n')

const libraryById = computed(() => new Map([...props.library, ...Object.values(sourceOverrides.value)].map(item => [item.id, item])))
const libraryOptions = computed(() => [...libraryById.value.values()])
// 父页面刷新成功后以服务器元数据为准；刷新失败仍保留刚保存的来源预览。
watch(() => props.library, () => { sourceOverrides.value = {} })
/** 已在当前配置空间引用的规则集：#序号 */
const usedLibrary = computed(() => {
  const used = new Map<string, number>()
  props.rules.forEach((item, index) => {
    if (item.itemType === 'ruleset' && item.library_rule_id && item.id !== editItem.value?.id) used.set(item.library_rule_id, index + 1)
  })
  return used
})
/** 编辑规则集引用时可换成另一个规则集（单选）；当前引用的规则集即使已停用也保留可选 */
const editingRuleset = computed(() => editing.value && editItem.value?.itemType === 'ruleset')
const selectedLibrary = computed(() => (editingRuleset.value
  ? form.libraryIds.map(id => libraryById.value.get(id) || (id === editItem.value?.library_rule_id ? editItem.value : null)).filter(Boolean) as Item[]
  : form.libraryIds.map(id => libraryById.value.get(id)).filter(Boolean) as Item[]))
const libraryUnavailable = (entryItem: Item) => usedLibrary.value.has(entryItem.id)
  || (entryItem.enabled === false && !(editingRuleset.value && entryItem.id === editItem.value?.library_rule_id))

/** 已有单条规则：类型 + 值 → #序号 */
const existingRules = computed(() => {
  const map = new Map<string, { index: number; id: string; enabled: boolean }>()
  props.rules.forEach((item, index) => {
    if (item.itemType === 'ruleset') return
    const key = `${item.rule_type}\n${item.value}`
    if (!map.has(key)) map.set(key, { index: index + 1, id: item.id, enabled: item.enabled !== false })
  })
  return map
})
const paste = computed(() => (!editing.value && source.value === 'values' && !singleType.value ? parsePastedRules(form.text, form.ruleType) : null))
/** 服务端 compose 遇到任一无效行会拒绝整批，所以有无效行时不能保存 */
const singleValueError = computed(() => ((editing.value && !isRuleset.value) || singleType.value) ? ruleValueError(form.ruleType, form.value.trim()) : '')
const invalidCount = computed(() => paste.value?.invalid.length ?? 0)
const pasteSummary = computed(() => {
  const result = paste.value
  if (!result?.count) return undefined
  return [
    invalidCount.value ? `有效 ${newRows.value.length} 条，${invalidCount.value} 行无效需修正或删除` : `将添加 ${newRows.value.length} 条`,
    ...result.invalid.slice(0, 2).map(row => `第 ${row.line} 行${row.message}`),
    ...(result.invalid.length > 2 ? [`另有 ${result.invalid.length - 2} 行无效`] : []),
    ...(duplicates.value.length ? [`跳过 ${duplicates.value.length} 条重复`] : [])
  ].join(' · ')
})
const duplicates = computed(() => (paste.value?.rows || []).flatMap(row => {
  const existing = existingRules.value.get(`${row.rule_type}\n${row.value}`)
  return existing ? [{ ...row, ...existing }] : []
}))
const newRows = computed(() => (paste.value?.rows || []).filter(row => !existingRules.value.has(`${row.rule_type}\n${row.value}`)))
const parts = computed(() => splitPaste(newRows.value))

/** 当前来源的单条规则类型；规则集为 null */
const ruleType = computed(() => (editing.value ? (isRuleset.value ? null : form.ruleType) : singleType.value ? form.ruleType : null))
const libraryItems = computed<Item[]>(() => (editing.value
  ? (isRuleset.value ? selectedLibrary.value : [])
  : source.value === 'library' ? selectedLibrary.value
    : source.value === 'new_library' ? [form.library] : []))
const behaviors = computed(() => libraryItems.value.map(item => item.behavior || 'domain'))
const hasDomainPart = computed(() => (ruleType.value
  ? isOxidnsRuleType(ruleType.value)
  : source.value === 'values' && !editing.value ? parts.value.domain.length > 0
    : behaviors.value.some(behavior => behavior !== 'ipcidr')))
const hasIpPart = computed(() => (ruleType.value
  ? IP_DNS_RULE_TYPES.includes(ruleType.value)
  : source.value === 'values' && !editing.value ? parts.value.ip.length > 0
    // classical 规则集按后端读到的内容判断；还没有缓存的按可能含 IP 处理。
    : libraryItems.value.some(item => item.has_ip_part ?? ['ipcidr', 'classical'].includes(item.behavior || 'domain'))))
/** 不参与 DNS 分流的类型：字段禁用并固定为默认上游 */
const unsupportedType = computed(() => {
  if (ruleType.value) return isOxidnsRuleType(ruleType.value) ? '' : ruleType.value
  if (!editing.value && source.value === 'values') return !parts.value.domain.length && parts.value.ip.length ? parts.value.ip[0]!.rule_type : parts.value.unsupported[0]?.rule_type || ''
  if (behaviors.value.length && behaviors.value.every(behavior => behavior === 'ipcidr')) return 'IP 规则集'
  return ''
})
/** 混合粘贴的 DNS 选择只作用于域名行，IP 行始终只用于连接分流。 */
const splitIp = computed(() => !editing.value && source.value === 'values' && parts.value.domain.length > 0 && parts.value.ip.length > 0)
const policyRoute = computed(() => followRoute(isRuleset.value
  ? { itemType: 'ruleset', policy: form.policy, behavior: behaviors.value.every(behavior => behavior === 'ipcidr') ? 'ipcidr' : 'domain' }
  : { itemType: 'rule', rule_type: ruleType.value || (parts.value.domain[0] || parts.value.ip[0])?.rule_type || 'DOMAIN', policy: form.policy }, engine.value?.policy_routes))
const fieldDisabled = computed(() => !dnsLoaded.value || locked.value || !!unsupportedType.value)
const dnsDraftPending = computed(() => (dnsTouched.value || resync.value) && !dnsLoaded.value)
const effectiveDns = computed<RuleDnsValue>(() => (fieldDisabled.value && !dnsDraftPending.value ? 'default' : dnsChoice.value))
const defaultChoice = computed(() => defaultDnsChoice({ domain: hasDomainPart.value, ip: hasIpPart.value && !(source.value === 'values' && !editing.value) }, engine.value))

watch(defaultChoice, value => {
  if (!editing.value && !dnsTouched.value) dnsChoice.value = value
})

const upstreamLabel = computed(() => {
  if (!isUpstreamValue(dnsChoice.value)) return undefined
  const key = dnsChoice.value.slice(9)
  return props.dns?.upstreams.find(item => item.key === key)?.label || entry.value?.upstream_label
})
const upstreams = computed(() => props.dns?.upstreams || [])
/** 绑定了 MosDNS 设备，或这条规则在某个 MosDNS 列表中时才比较 MosDNS 的处理（新规则不在任何列表中） */
const mosdnsAgent = computed(() => !!engine.value?.agents.some(agent => agent.service_type === 'mosdns'))
const mosdnsRoute = computed(() => (editItem.value && props.dns ? props.dns.mosdns[editItem.value.id] : undefined))
const mosdnsContext = computed(() => (props.dns && (mosdnsAgent.value || mosdnsRoute.value !== undefined) ? { route: mosdnsRoute.value ?? null } : null))
/** 编辑时规则已有的单独设置（含停用时暂存的）；从未设置过的默认上游不算 */
const savedExplicit = computed(() => entry.value?.state === 'explicit'
  || (entry.value?.state === 'disabled' && !entry.value.follow && !!entry.value.route))

const warnings = computed<DnsWarning[]>(() => {
  if (dnsDraftPending.value) return []
  if (unsupportedType.value) return engine.value && !engine.value.editable ? dnsWarnings({ engine: engine.value, value: 'default', policy: form.policy, policyRoute: null }).filter(item => item.kind === 'locked') : []
  const policyChanged = editing.value && editItem.value?.policy !== form.policy
  return dnsWarnings({
    engine: engine.value,
    unloaded: !dnsLoaded.value && !props.dnsLoading,
    value: effectiveDns.value,
    policy: form.policy,
    policyRoute: policyRoute.value,
    mosdns: mosdnsContext.value,
    drift: entry.value?.state === 'follow_drift' && !policyChanged ? { route: entry.value.route ?? null, label: entry.value.upstream_label } : null,
    resync: resync.value,
    explainExplicit: dnsTouched.value || (editing.value && savedExplicit.value),
    upstreamLabel: upstreamLabel.value
  }).filter(item => item.action !== 'resync' || !locked.value)
})

const dnsHint = computed(() => {
  if (unsupportedType.value) return `${unsupportedType.value} 不参与 DNS 分流`
  if (props.dnsLoading && !dnsLoaded.value) return '正在读取 DNS 状态…'
  if (!inUse.value) return '当前配置空间未使用 OxiDNS'
  if (splitIp.value) return `域名 ${parts.value.domain.length} 条：${choiceLabel(effectiveDns.value, { policyRoute: policyRoute.value, upstreamLabel: upstreamLabel.value })} ／ IP ${parts.value.ip.length} 条：仅连接分流`
  if (isUpstreamValue(effectiveDns.value)) return PINNED_HINT
  return DNS_COPY[copyKey(effectiveDns.value)].hint
})
const dnsHelp = computed(() => `${DNS_FIELD_HELP}\n\n${DNS_COPY[copyKey(effectiveDns.value)].help}`)

const showNoResolve = computed(() => (ruleType.value ? NO_RESOLVE_RULE_TYPES.includes(ruleType.value)
  : !editing.value && source.value === 'values' ? parts.value.ip.length > 0
    : behaviors.value.includes('ipcidr')))
// 类型或来源变化时按是否含 IP 部分重设默认值；打开对话框时保留规则已存的值。
let noResolveBase = false
watch(showNoResolve, value => {
  if (value === noResolveBase) return
  noResolveBase = value
  form.no_resolve = value
})

/** 本次会新增的条数 */
const addCount = computed(() => {
  if (editing.value) return 1
  if (source.value === 'values') return singleType.value ? (form.value.trim() || form.ruleType === 'MATCH' ? 1 : 0) : newRows.value.length
  if (source.value === 'library') return form.libraryIds.length
  return form.library.name.trim() && (form.library.source_type === 'url' ? form.library.url.trim() : form.library.content.trim()) ? 1 : 0
})
watch(() => [form.library.content, form.library.behavior, source.value], () => { contentCheck.value = null })
const libraryNameTaken = computed(() => source.value === 'new_library' && props.library.some(item => item.name === form.library.name.trim()))
const blocked = computed(() => dnsDraftPending.value || !!singleValueError.value || !!paste.value?.overLimit || invalidCount.value > 0 || (source.value === 'new_library' && (libraryNameTaken.value
  || (form.library.source_type === 'content' && form.library.behavior === 'domain' && libraryFormat.value === 'text' && !form.library.scope))))
const saveLabel = computed(() => {
  if (editing.value) return '保存'
  if (source.value === 'library') return addCount.value ? `添加 ${addCount.value} 个规则集` : '添加规则集'
  if (source.value === 'new_library') return '保存规则集'
  return addCount.value ? `保存 ${addCount.value} 条规则` : '保存规则'
})
const title = computed(() => (editingSource.value ? '编辑规则集来源' : !editing.value ? '添加规则' : isRuleset.value ? '编辑规则集' : '编辑规则'))
const moreSummary = computed(() => [
  form.remark ? `备注：${form.remark}` : '备注', form.group_name ? `分组：${form.group_name}` : '分组名',
  ...(!editing.value && !singleType.value ? [`插入位置（${form.position === 'top' ? '顶部' : '底部'}）`] : []),
  form.enabled ? '启用' : '停用', ...(!inUse.value ? ['DNS'] : [])
].join(' · '))

const snapshot = () => JSON.stringify({ source: source.value, form, dns: dnsChoice.value, resync: resync.value })
const dirty = () => props.open && (sourceDirty.value || snapshot() !== initial)

function reset() {
  saveError.value = ''
  editingSource.value = null
  sourceDirty.value = false
  sourceSaving.value = false
  sourceOverrides.value = {}
  session += 1
  const item = editItem.value
  source.value = editing.value ? (item?.itemType === 'ruleset' ? 'library' : 'values') : props.initialSource || 'values'
  Object.assign(form, {
    ruleType: item && item.itemType !== 'ruleset' ? item.rule_type || 'DOMAIN-SUFFIX' : 'DOMAIN-SUFFIX',
    text: '',
    value: item?.value || '',
    libraryIds: item?.itemType === 'ruleset' ? [item.library_rule_id] : [],
    library: { name: '', source_type: 'url', url: '', content: '', behavior: 'domain', format: 'auto', scope: '' },
    policy: item?.policy || 'DIRECT',
    remark: item?.remark || '',
    group_name: item?.group_name || '',
    position: 'top',
    enabled: item ? item.enabled !== false : true,
    udp_fallback: item && item.itemType !== 'ruleset' ? item.udp_fallback || '' : '',
    ntp_direct: item?.itemType !== 'ruleset' && item?.ntp_direct === true,
    no_resolve: item?.no_resolve ?? (item ? (item.itemType === 'ruleset' ? item.behavior === 'ipcidr' : NO_RESOLVE_RULE_TYPES.includes(item.rule_type)) : false)
  })
  noResolveBase = showNoResolve.value
  dnsTouched.value = false
  resync.value = false
  urlCheck.value = 'idle'
  dnsChoice.value = editing.value ? stateValue(entry.value) : defaultChoice.value
  initialDns = dnsChoice.value
  initial = snapshot()
}

watch(() => props.open, open => { if (open) reset() }, { immediate: true })
// DNS 状态晚于对话框加载完成时补上初值，用户已选择的值保持不变。
watch(() => props.dns, () => {
  if (!props.open || dnsTouched.value) return
  const value = editing.value ? stateValue(entry.value) : defaultChoice.value
  const clean = snapshot() === initial
  dnsChoice.value = value
  initialDns = value
  if (clean) initial = snapshot()
})

function chooseDns(value: RuleDnsValue) {
  dnsChoice.value = value
  dnsTouched.value = true
  resync.value = false
}

function onWarningAction(kind: NonNullable<DnsWarning['action']>) {
  if (kind === 'follow') chooseDns('follow')
  else if (kind === 'resync') resync.value = true
}

async function requestClose(open: boolean) {
  if (open) return emit('update:open', true)
  if (busy.value) return
  if (dirty() && !(await confirm('放弃未保存的修改？', { title: '关闭编辑', confirmText: '放弃', cancelText: '继续编辑' }))) return
  emit('update:open', false)
}

async function openSourceEditor(id: string) {
  const entryItem = libraryById.value.get(id)
  if (!entryItem || busy.value) return
  sourceScrollTop = editorBody.value?.scrollTop || 0
  sourceDirty.value = false
  editingSource.value = { ...entryItem }
  await nextTick()
  if (editorBody.value) editorBody.value.scrollTop = 0
  editorBody.value?.querySelector<HTMLInputElement>('input')?.focus({ preventScroll: true })
}

async function returnToRule() {
  const id = editingSource.value?.id
  editingSource.value = null
  sourceDirty.value = false
  await nextTick()
  if (editorBody.value) editorBody.value.scrollTop = sourceScrollTop
  const trigger = Array.from(editorBody.value?.querySelectorAll<HTMLButtonElement>('[data-edit-source]') || [])
    .find(button => button.dataset.editSource === id)
  trigger?.focus({ preventScroll: true })
}

async function cancelSourceEdit() {
  if (busy.value) return
  if (sourceDirty.value && !(await confirm('放弃未保存的来源修改？', { title: '返回规则设置', confirmText: '放弃', cancelText: '继续编辑' }))) return
  await returnToRule()
}

async function onSourceSaved(updated: Item) {
  sourceOverrides.value = { ...sourceOverrides.value, [updated.id]: updated }
  emit('source-saved', updated)
  notifyPublish(['library'], '规则集来源已保存')
  await returnToRule()
}

function convertToRuleset() {
  const rows = paste.value?.rows || []
  const converted = pasteToRuleset(rows)
  Object.assign(form.library, { source_type: 'content', content: converted.content, behavior: converted.behavior, scope: converted.behavior === 'domain' ? 'PATTERN' : '' })
  source.value = 'new_library'
}

function toggleLibrary(id: string) {
  const entryItem = libraryById.value.get(id)
  if (!entryItem || libraryUnavailable(entryItem)) return
  if (editingRuleset.value) {
    // 更换引用：单选，内容类型变化时与旧表单一致地按 ipcidr 重设 no-resolve
    if (form.libraryIds[0] !== id) form.no_resolve = entryItem.behavior === 'ipcidr'
    form.libraryIds = [id]
    libraryOpen.value = false
    return
  }
  form.libraryIds = form.libraryIds.includes(id) ? form.libraryIds.filter(value => value !== id) : [...form.libraryIds, id]
}

const isFullUrl = (url: string) => /^https?:\/\//.test(url)
async function testUrl() {
  const url = form.library.url.trim()
  if (!isFullUrl(url)) return true
  urlCheck.value = 'checking'
  try {
    const { data } = await api.post('/rule-library/test-single', { url })
    urlCheck.value = data?.success && data.available ? 'ok' : 'failed'
  } catch {
    urlCheck.value = 'failed'
  }
  return urlCheck.value === 'ok'
}
watch(() => form.library.url, () => { urlCheck.value = 'idle' })

/** 新建规则集的来源；内容规则集按所选匹配范围写入并复用格式检查 */
async function libraryPayload(): Promise<Extract<ComposePayload['source'], { kind: 'new_library' }>['library'] | null> {
  const value = form.library
  const library: Extract<ComposePayload['source'], { kind: 'new_library' }>['library'] = { name: value.name.trim(), source_type: value.source_type, behavior: value.behavior, enabled: form.enabled }
  if (value.source_type === 'url') {
    const url = value.url.trim()
    return { ...library, url, ...(value.format !== 'auto' ? { format: value.format } : {}) }
  }
  library.format = libraryFormat.value
  const lines = value.content.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
  const content = library.format === 'yaml' || value.behavior === 'classical' ? value.content
    : value.behavior === 'domain' && value.scope !== 'PATTERN'
      ? lines.map(line => (line.startsWith('#') || line.startsWith('//') ? line : domainRuleLine('domain', value.scope, line))).join('\n')
      : lines.join('\n')
  const checked = await prepareRuleContent({ behavior: value.behavior, content, format: library.format }, undefined, check => { contentCheck.value = check })
  return checked === null ? null : { ...library, content: checked }
}

const errorReason = dnsErrorReason
const routingLocked = isRoutingLocked
const resolveLocked = confirmSaveWithoutDns

function notifyPublish(scopes: Array<'rules' | 'library'>, title: string, extra: string[] = [], warn = false, response?: { pinned_withheld?: boolean }) {
  const pinned = pinnedWithheld(response, isUpstreamValue(effectiveDns.value) && effectiveDns.value !== initialDns)
  notifyRuleSaved(title, { agents: engine.value?.agents, scopes, pinned, extra, warn })
}

async function submitCreate(withDns = true) {
  const sendDns = withDns && dnsLoaded.value && !locked.value && effectiveDns.value !== 'default'
  const rule = { policy: form.policy, remark: form.remark.trim(), group_name: form.group_name.trim(), no_resolve: form.no_resolve, enabled: form.enabled, position: form.position }
  let payload: ComposePayload
  if (source.value === 'values') {
    payload = { source: { kind: 'values', rule_type: form.ruleType, text: form.text }, rule }
    if (sendDns && splitIp.value) payload.dns_ip = 'default'
  } else if (source.value === 'library') {
    payload = { source: { kind: 'library', library_rule_ids: [...form.libraryIds] }, rule }
  } else {
    const library = await libraryPayload()
    if (!library) return false
    payload = { source: { kind: 'new_library', library }, rule }
  }
  if (sendDns) payload.dns = effectiveDns.value
  const { data } = await ruleApi.compose(payload, props.profileId)
  const ids = data.data.rules.map(item => String(item.id))
  const skipped = data.skipped || []
  const ignored = (data.warnings || []).filter(text => /^忽略第 \d+ 行行尾策略/.test(text)).length
  const others = (data.warnings || []).filter(text => !/^忽略第 \d+ 行行尾策略/.test(text))
  const unit = source.value === 'values' ? '条' : '个规则集'
  const summary = [ids.length ? `已添加 ${ids.length} ${unit}` : '没有新增规则',
    ...(skipped.length ? [`跳过 ${skipped.length} 条重复（${skipped.map(item => `#${item.index}`).join('、')}）`] : []),
    ...(ignored ? [`忽略 ${ignored} 处行尾策略`] : [])].join('；')
  const disabledDuplicate = skipped.find(item => !item.enabled)
  if (disabledDuplicate) {
    notify.warning(summary, others.join('\n'), { label: `启用 #${disabledDuplicate.index}`, onClick: () => emit('enable', disabledDuplicate.existing_id) })
  } else {
    notifyPublish([source.value === 'values' ? 'rules' : 'library'], summary, others, skipped.length > 0 || ignored > 0 || others.length > 0, data)
  }
  emit('saved', { ids, dns: data.dns })
  return true
}

async function submitLegacySingle() {
  // 逻辑规则与 MATCH 的值含逗号或为空，不能按行解析，沿用单条添加；它们不参与 DNS 分流。
  const rule = { id: `rule_${Date.now()}`, itemType: 'rule', rule_type: form.ruleType, value: form.value.trim(), policy: form.policy,
    enabled: form.enabled, remark: form.remark.trim(), group_name: form.group_name.trim(), no_resolve: form.no_resolve,
    ...(isMatch.value && form.udp_fallback ? { udp_fallback: form.udp_fallback } : {}),
    ...(isMatch.value && form.ntp_direct ? { ntp_direct: true } : {}) }
  await ruleApi.create(rule, props.profileId)
  notifyPublish(['rules'], '已添加 1 条规则')
  emit('saved', { ids: [rule.id] })
  return true
}

async function submitEdit(withDns = true) {
  const item = editItem.value!
  const changed = dnsLoaded.value && !locked.value && (effectiveDns.value !== initialDns || resync.value)
  const dnsField = withDns && changed ? { dns: resync.value && effectiveDns.value === 'follow' ? 'follow' : effectiveDns.value } : {}
  // 改策略会离开原分组：只有用户没有同时改分组名时才清空
  const groupName = item.policy !== form.policy && form.group_name.trim() === (item.group_name || '').trim() ? '' : form.group_name.trim()
  let response
  if (item.itemType === 'ruleset') {
    const payload = { ...ruleSetPayload(item), library_rule_id: form.libraryIds[0] || item.library_rule_id,
      policy: form.policy, remark: form.remark.trim(), enabled: form.enabled, no_resolve: form.no_resolve, group_name: groupName }
    response = await ruleSetApi.update(item.id, { ...payload, ...dnsField }, props.profileId)
  } else {
    const payload = { ...rulePayload(item), itemType: 'rule', rule_type: form.ruleType, value: form.value.trim(), policy: form.policy,
      remark: form.remark.trim(), enabled: form.enabled, no_resolve: form.no_resolve, group_name: groupName,
      udp_fallback: isMatch.value ? form.udp_fallback : '', ntp_direct: isMatch.value && form.ntp_direct }
    response = await ruleApi.update(item.id, { ...payload, ...dnsField }, props.profileId)
  }
  const warningsList: string[] = response?.data?.warnings || []
  notifyPublish([item.itemType === 'ruleset' ? 'library' : 'rules'], '已保存', warningsList, warningsList.length > 0, response?.data)
  emit('saved', { ids: [item.id] })
  return true
}

async function save() {
  if (saving.value || !addCount.value || blocked.value) return
  const current = session
  saving.value = true
  saveError.value = ''
  try {
    let done = false
    try {
      done = editing.value ? await submitEdit() : singleType.value ? await submitLegacySingle() : await submitCreate()
    } catch (error) {
      if (!routingLocked(error) || current !== session) throw error
      if (!(await resolveLocked(error))) return
      done = editing.value ? await submitEdit(false) : await submitCreate(false)
    }
    if (done && current === session) emit('update:open', false)
  } catch (error) {
    if (current === session) {
      saveError.value = errorReason(error) || '保存失败'
      notify.error(saveError.value)
    }
  } finally {
    if (current === session) saving.value = false
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="requestClose">
    <DialogContent
      class="rule-editor-dialog"
      :show-close-button="false"
      :aria-busy="busy"
      @escape-key-down="event => { if (busy) event.preventDefault() }"
      @interact-outside="event => { if (busy || dirty()) event.preventDefault() }"
    >
      <header class="rule-editor-header">
        <div class="min-w-0">
          <DialogTitle class="text-lg font-semibold">{{ title }}</DialogTitle>
          <DialogDescription class="mt-1 text-[13px]">{{ editingSource ? '修改来源会同步到所有引用此规则集的配置。保存来源后返回，当前策略、DNS 和备注草稿会保留。' : '规则按列表顺序自上而下匹配，命中即停止。' }}</DialogDescription>
        </div>
        <Button type="button" variant="ghost" size="icon" class="rule-editor-close text-muted-foreground" aria-label="关闭" :disabled="busy" @click="requestClose(false)">
          <X class="size-4" />
        </Button>
      </header>

      <!-- 滚动容器不能是 fieldset：Chromium 中 fieldset 不按 flex 高度裁剪，内容会画到底栏下方 -->
      <div ref="editorBody" class="rule-editor-body cf-focus-gutter">
        <RuleSourceEditor
          v-if="editingSource"
          ref="sourceEditor"
          :source="editingSource"
          @busy="sourceSaving = $event"
          @dirty="sourceDirty = $event"
          @saved="onSourceSaved"
        />
        <fieldset v-else :disabled="saving" class="rule-editor-fields">
          <Alert v-if="saveError" class="border-destructive-accent/30" data-rule-save-error>
            <AlertDescription class="whitespace-pre-wrap break-all text-destructive-accent">{{ saveError }}</AlertDescription>
          </Alert>
          <Alert v-if="dnsDraftPending" data-dns-draft-pending>
            <AlertDescription class="flex flex-wrap items-center gap-x-3 gap-y-2 text-[13px]">
              <span>{{ dnsLoading ? 'DNS 状态正在更新，已保留你的选择，读取完成后可保存。' : 'DNS 状态读取失败，已保留你的选择。请重新读取后再保存。' }}</span>
              <Button v-if="!dnsLoading" type="button" variant="outline" size="sm" @click="emit('reload-dns')">重新读取 DNS</Button>
            </AlertDescription>
          </Alert>
          <FormField v-if="!editing" label="匹配来源">
            <Segmented v-model="source" :options="SOURCE_OPTIONS" label="匹配来源" block />
          </FormField>

          <!-- 输入规则 -->
          <template v-if="source === 'values'">
            <FormField
              :label="editing ? '规则类型' : '无前缀的行按'"
              :help="editing ? undefined : '带「类型,」前缀的行按前缀添加；+.x、*.x 为 DOMAIN-SUFFIX，full:x 为 DOMAIN；逻辑规则和 MATCH 每次添加一条。'"
            >
              <Select v-model="form.ruleType">
                <SelectTrigger class="w-full bg-background/50 font-mono" aria-label="规则类型"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="type in RULE_TYPES" :key="type" :value="type" class="font-mono">{{ type }}</SelectItem>
                </SelectContent>
              </Select>
            </FormField>

            <FormField v-if="editing || singleType" label="匹配值" html-for="rule-value">
              <Input id="rule-value" v-model="form.value" class="bg-background/50 font-mono" :placeholder="form.ruleType === 'MATCH' ? '留空即可' : '域名、IP 或规则条件'" :aria-invalid="!!singleValueError" />
              <p v-if="singleValueError" role="alert" class="text-xs text-destructive-accent">{{ singleValueError }}</p>
            </FormField>
            <FormField
              v-else
              label="匹配值"
              html-for="rule-values"
              persistent-hint
              :hint="pasteSummary"
            >
              <Textarea id="rule-values" v-model="form.text" class="min-h-[120px] bg-background/50 font-mono text-[12.5px]" :rows="5" placeholder="example.com（每行一条）" />
              <ul v-if="paste && (paste.rows.length || paste.invalid.length)" class="m-0 flex max-h-40 list-none flex-col gap-0.5 overflow-y-auto p-0 text-[12px]" aria-label="解析预览" data-paste-preview>
                <li v-for="row in newRows.slice(0, 8)" :key="`ok-${row.line}`" class="flex min-w-0 items-baseline gap-2">
                  <code class="shrink-0 font-mono text-[11px] text-muted-foreground">{{ row.rule_type }}</code>
                  <span class="min-w-0 break-all font-mono">{{ row.value }}</span>
                </li>
                <li v-if="newRows.length > 8" class="text-muted-foreground">另有 {{ newRows.length - 8 }} 条</li>
                <li v-for="row in duplicates" :key="`dup-${row.line}`" class="text-muted-foreground">
                  第 {{ row.line }} 行 {{ row.value }} 已在 #{{ row.index }}{{ row.enabled ? '' : '（已停用）' }}
                </li>
                <li v-for="row in paste.invalid" :key="`bad-${row.line}`" class="break-all text-destructive-accent">第 {{ row.line }} 行：{{ row.message }}</li>
                <li v-for="note in paste.notes" :key="note" class="text-muted-foreground">{{ note }}</li>
                <li v-for="row in paste.ignored" :key="`ign-${row.line}`" class="text-muted-foreground">忽略第 {{ row.line }} 行行尾策略 {{ row.value }}</li>
              </ul>
            </FormField>
            <p v-if="paste?.overLimit" class="m-0 text-[12.5px] text-destructive-accent" role="alert">一次最多添加 {{ PASTE_LIMIT }} 条，较多条目请改为新建规则集</p>
            <InfoNote v-if="paste && paste.count > PASTE_SUGGEST_RULESET">
              <p class="flex flex-wrap items-center gap-2">
                条目较多，建议保存为规则集
                <Button type="button" variant="ghost" size="xs" class="text-primary-accent max-sm:h-11" @click="convertToRuleset">改为新建规则集</Button>
              </p>
            </InfoNote>
          </template>

          <!-- 规则库 -->
          <template v-else-if="source === 'library'">
            <FormField label="规则集" :hint="editing ? '可换成另一个规则集，顺序、备注和 DNS 保持不变。' : '已添加的规则集不会重复引用；停用的规则集需先在规则库启用。'">
              <Popover v-model:open="libraryOpen">
                <PopoverTrigger as-child>
                  <button type="button" class="flex h-9 w-full min-w-0 items-center gap-2 rounded-md border border-input bg-background/50 px-3 text-left text-sm outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-primary-accent max-md:min-h-11" aria-label="选择规则集">
                    <span class="min-w-0 flex-1 truncate" :class="!form.libraryIds.length && 'text-muted-foreground'">
                      {{ !form.libraryIds.length ? '搜索规则库…' : form.libraryIds.length === 1 ? selectedLibrary[0]?.name : `已选 ${form.libraryIds.length} 个规则集` }}
                    </span>
                    <ChevronsUpDown class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="start" :collision-padding="12" class="w-(--reka-popover-trigger-width) min-w-[18rem] max-w-[calc(100vw-1.5rem)] overflow-hidden p-0 shadow-overlay">
                  <Command v-if="libraryOpen">
                    <CommandInput aria-label="搜索规则库" placeholder="搜索规则集名称…" />
                    <CommandList class="max-h-[min(20rem,calc(var(--reka-popover-content-available-height)-3rem))] p-1">
                      <CommandEmpty class="px-3 py-6 text-muted-foreground">没有匹配的规则集</CommandEmpty>
                      <CommandGroup>
                        <CommandItem
                          v-for="entryItem in libraryOptions"
                          :key="entryItem.id"
                          :value="entryItem.id"
                          :disabled="libraryUnavailable(entryItem)"
                          class="min-h-11 cursor-pointer gap-2 px-2 py-1.5 md:min-h-9"
                          @select="(event: Event) => { event.preventDefault(); toggleLibrary(entryItem.id) }"
                        >
                          <Check class="size-4 shrink-0" :class="form.libraryIds.includes(entryItem.id) ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
                          <span class="min-w-0 flex-1 truncate">{{ entryItem.name }}</span>
                          <span v-if="usedLibrary.has(entryItem.id)" class="shrink-0 text-xs text-muted-foreground">已添加 · #{{ usedLibrary.get(entryItem.id) }}</span>
                          <span v-else-if="entryItem.enabled === false" class="shrink-0 text-xs text-muted-foreground">已停用</span>
                          <span v-else class="shrink-0 font-mono text-[10.5px] text-muted-foreground">{{ entryItem.behavior }}</span>
                        </CommandItem>
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
            </FormField>
            <div v-for="entryItem in selectedLibrary.slice(0, 3)" :key="entryItem.id" class="min-w-0 rounded-lg border border-border/50 bg-background/50 p-3 text-xs text-muted-foreground">
              <p class="m-0 break-all font-medium text-foreground">{{ entryItem.name }}</p>
              <p class="mt-1 mb-0">{{ entryItem.behavior }} · {{ entryItem.source_type === 'content' ? '共享规则内容' : '远程 URL' }}</p>
              <p class="mt-1 mb-0 max-h-24 overflow-auto whitespace-pre-wrap break-all font-mono">{{ entryItem.source_type === 'content' ? entryItem.content : entryItem.url }}</p>
              <p v-if="entryItem.enabled === false || entryItem.library_enabled === false" class="mt-2 mb-0 text-warning-accent">共享来源已停用；当前配置的启用状态会保留，来源重新启用后生效。</p>
              <Button
                type="button"
                variant="link"
                size="sm"
                class="mt-2 h-auto px-0 text-xs"
                :data-edit-source="editingRuleset ? form.libraryIds[0] : entryItem.id"
                :disabled="!libraryById.has(editingRuleset ? form.libraryIds[0] || '' : entryItem.id)"
                @click="openSourceEditor(editingRuleset ? form.libraryIds[0] || '' : entryItem.id)"
              >
                <Pencil class="size-3.5" aria-hidden="true" />编辑来源
              </Button>
            </div>
            <p v-if="selectedLibrary.length > 3" class="m-0 text-xs text-muted-foreground">另有 {{ selectedLibrary.length - 3 }} 个规则集</p>

          </template>

          <!-- 新建规则集 -->
          <template v-else>
            <FormField label="名称" html-for="rule-library-name" :hint="libraryNameTaken ? `规则库已有同名规则集「${form.library.name.trim()}」` : '将同时保存到规则库，其他配置空间也可引用。'" :persistent-hint="libraryNameTaken">
              <Input id="rule-library-name" v-model="form.library.name" class="bg-background/50" placeholder="例如 广告拦截" />
            </FormField>
            <FormField label="来源">
              <Segmented v-model="form.library.source_type" :options="LIBRARY_SOURCES" label="规则集来源" block />
            </FormField>
            <FormField v-if="form.library.source_type === 'url'" label="地址" html-for="rule-library-url"
              :hint="urlCheck === 'ok' ? '地址可以访问' : urlCheck === 'failed' ? '地址暂时无法访问' : undefined" :persistent-hint="urlCheck === 'failed'">
              <div class="flex min-w-0 gap-2">
                <Input id="rule-library-url" v-model="form.library.url" class="min-w-0 flex-1 bg-background/50 font-mono" placeholder="https://…" />
                <Button type="button" variant="outline" class="shrink-0 max-md:h-11" :disabled="!isFullUrl(form.library.url.trim()) || urlCheck === 'checking'" @click="testUrl">
                  <Loader2 v-if="urlCheck === 'checking'" class="size-3.5 animate-spin" />测试地址
                </Button>
              </div>
            </FormField>
            <FormField v-else label="内容" html-for="rule-library-content">
              <Textarea id="rule-library-content" v-model="form.library.content" class="min-h-[120px] bg-background/50 font-mono text-[12.5px]" :rows="5"
                :placeholder="form.library.behavior === 'domain' ? 'example.com（每行一条）' : form.library.behavior === 'ipcidr' ? '192.0.2.0/24（每行一条）' : 'DOMAIN-SUFFIX,example.com（每行一条）'" />
            </FormField>
            <p v-if="form.library.source_type === 'content' && libraryFormat === 'yaml'" class="m-0 text-xs text-muted-foreground">已识别为 YAML payload，按各条目中的写法匹配。</p>
            <RuleContentIssues :check="contentCheck" />
            <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
              <FormField label="内容类型">
                <Select v-model="form.library.behavior">
                  <SelectTrigger class="w-full bg-background/50" aria-label="内容类型"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="option in BEHAVIORS" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
              <FormField v-if="form.library.source_type === 'url'" label="内容格式" help="自动按地址后缀识别格式。若地址以 .txt 结尾但内容是 payload 列表，请选择 YAML。">
                <Select v-model="form.library.format">
                  <SelectTrigger class="w-full bg-background/50" aria-label="内容格式"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">自动识别</SelectItem>
                    <SelectItem value="text">Text · 每行一条</SelectItem>
                    <SelectItem value="yaml">YAML · payload 列表</SelectItem>
                    <SelectItem value="mrs">MRS · 二进制规则</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
              <FormField v-if="form.library.source_type === 'content' && form.library.behavior === 'domain' && libraryFormat === 'text'" label="匹配范围"
                :hint="form.library.scope ? undefined : '请选择是否包含子域名'" persistent-hint>
                <Select v-model="form.library.scope">
                  <SelectTrigger class="w-full bg-background/50" aria-label="匹配范围"><SelectValue placeholder="选择匹配范围" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="option in DOMAIN_SCOPES" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
            </div>
            <p v-if="form.library.behavior === 'classical'" class="m-0 text-xs leading-relaxed text-muted-foreground">{{ CLASSICAL_CONTENT_HINT }}</p>
          </template>

          <div v-if="showNoResolve" class="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <Switch id="rule-noresolve" v-model="form.no_resolve" />
            <Label for="rule-noresolve" class="text-[13px]">no-resolve</Label>
            <span class="text-[12px] text-muted-foreground">IP 类规则建议开启，匹配时不为此解析域名</span>
          </div>

          <FormField label="策略">
            <Select v-model="form.policy">
              <SelectTrigger class="w-full min-w-0 bg-background/50 [&>span]:truncate" aria-label="策略"><SelectValue placeholder="选择策略" /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="policy in policies" :key="policy" :value="policy" :title="policy"><span class="break-all">{{ policy }}</span></SelectItem>
              </SelectContent>
            </Select>
          </FormField>

          <FormField
            v-if="isMatch"
            label="节点不支持 UDP 时"
            :help="UDP_FALLBACK_HELP"
            :hint="form.udp_fallback === 'REJECT' && !form.ntp_direct ? '拒绝也会拦下对时，建议同时开启下方「对时（NTP）始终直连」。' : undefined"
            persistent-hint
          >
            <Segmented v-model="form.udp_fallback" :options="UDP_FALLBACK_OPTIONS" label="节点不支持 UDP 时" block />
          </FormField>

          <div v-if="isMatch" class="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <Switch id="rule-ntp-direct" v-model="form.ntp_direct" />
            <Label for="rule-ntp-direct" class="text-[13px]">对时（NTP）始终直连</Label>
            <span class="cf-compact-help"><FieldHelp label="对时（NTP）始终直连" :text="NTP_DIRECT_HELP" /></span>
          </div>

          <RuleDnsField
            v-if="inUse"
            :model-value="effectiveDns"
            :hint="dnsHint"
            :help="dnsHelp"
            :persistent-hint="!!unsupportedType || splitIp"
            :warnings="warnings"
            :upstreams="upstreams"
            :upstream-label="upstreamLabel"
            :policy-route="policyRoute"
            :default-forward="engine?.default_forward"
            :disabled="fieldDisabled"
            :loading="dnsLoading && !dnsLoaded"
            @update:model-value="chooseDns"
            @action="onWarningAction"
          />

          <details class="group min-w-0 rounded-lg border border-border bg-background/30" data-rule-more>
            <summary class="flex min-h-11 cursor-pointer list-none items-center gap-2 rounded-lg px-3 py-2 select-none hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-primary-accent focus-visible:outline-none md:min-h-9 [&::-webkit-details-marker]:hidden">
              <ChevronRight class="size-4 shrink-0 text-muted-foreground transition-transform duration-200 group-open:rotate-90" aria-hidden="true" />
              <span class="shrink-0 text-sm font-medium">更多选项</span>
              <span class="min-w-0 flex-1 truncate text-right text-xs text-muted-foreground" :title="moreSummary">{{ moreSummary }}</span>
            </summary>
            <div class="grid gap-3 border-t border-border p-3">
              <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
                <FormField label="备注" html-for="rule-remark">
                  <Input id="rule-remark" v-model="form.remark" class="bg-background/50" placeholder="可选，添加备注说明" />
                </FormField>
                <FormField label="分组名" html-for="rule-group">
                  <Input id="rule-group" v-model="form.group_name" class="bg-background/50" placeholder="可选，相邻同策略且同名规则成组" />
                </FormField>
              </div>
              <FormField v-if="!editing && !singleType" label="插入位置">
                <Segmented v-model="form.position" :options="POSITIONS" label="插入位置" />
              </FormField>
              <div class="flex items-center gap-2.5">
                <Switch id="rule-enabled" v-model="form.enabled" />
                <Label for="rule-enabled" class="text-[13px] text-muted-foreground">{{ form.enabled ? '启用' : '停用' }}</Label>
              </div>
              <RuleDnsField
                v-if="!inUse"
                :model-value="effectiveDns"
                :hint="dnsHint"
                :help="dnsHelp"
                :persistent-hint="!!unsupportedType"
                :warnings="warnings"
                :upstreams="upstreams"
                :upstream-label="upstreamLabel"
                :policy-route="policyRoute"
                :default-forward="engine?.default_forward"
                :disabled="fieldDisabled"
                @update:model-value="chooseDns"
                @action="onWarningAction"
              />
            </div>
          </details>
        </fieldset>
      </div>

      <footer v-if="editingSource" class="rule-editor-footer">
        <span class="min-w-0 text-xs text-muted-foreground">保存到共享规则库</span>
        <div class="flex shrink-0 gap-2">
          <Button type="button" variant="outline" class="max-md:h-11" :disabled="busy" @click="cancelSourceEdit">返回规则设置</Button>
          <Button type="button" class="max-md:h-11" :disabled="busy" @click="sourceEditor?.save()">
            <Loader2 v-if="sourceSaving" class="size-4 animate-spin" />{{ sourceSaving ? '保存中…' : '保存来源' }}
          </Button>
        </div>
      </footer>
      <footer v-else class="rule-editor-footer">
        <span class="min-w-0 text-xs text-muted-foreground">
          <template v-if="invalidCount">请先修正或删除 {{ invalidCount }} 行无效内容</template>
          <template v-else-if="!addCount && !editing">没有可添加的规则</template>
        </span>
        <div class="flex shrink-0 gap-2">
          <Button type="button" variant="outline" class="max-md:h-11" :disabled="saving" @click="requestClose(false)">取消</Button>
          <Button type="button" class="max-md:h-11" :disabled="saving || !addCount || blocked" @click="save">
            <Loader2 v-if="saving" class="size-4 animate-spin" />{{ saving ? '保存中…' : saveLabel }}
          </Button>
        </div>
      </footer>
    </DialogContent>
  </Dialog>
</template>

<style scoped>
:global(.rule-editor-dialog) {
  display: flex;
  flex-direction: column;
  width: min(640px, calc(100vw - 32px));
  max-width: 640px;
  max-height: min(820px, calc(100dvh - 48px));
  overflow: hidden;
  gap: 0;
  padding: 0;
}
.rule-editor-header {
  display: flex;
  flex: none;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 20px 24px 16px;
  border-bottom: 1px solid var(--border);
}
.rule-editor-close { width: var(--cf-touch); height: var(--cf-touch); margin: -8px -12px 0 0; flex: none; }
.rule-editor-body {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  flex-direction: column;
  margin: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 16px 24px 20px;
}
.rule-editor-fields {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 16px;
  margin: 0;
  border: 0;
  padding: 0;
}
.rule-editor-footer {
  display: flex;
  flex: none;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 24px;
  border-top: 1px solid var(--border);
}
@media (max-width: 639px) {
  :global(.rule-editor-dialog) { top: 0; left: 0; width: 100%; height: 100dvh; max-height: 100dvh; max-width: none; translate: none; border: 0; border-radius: 0; }
  .rule-editor-header { padding: max(12px, env(safe-area-inset-top)) 12px 12px 16px; }
  .rule-editor-body { padding: 12px 16px 16px; }
  .rule-editor-fields { gap: 12px; }
  .rule-editor-footer { gap: 8px; padding: 12px 16px max(12px, env(safe-area-inset-bottom)); }
}
</style>
