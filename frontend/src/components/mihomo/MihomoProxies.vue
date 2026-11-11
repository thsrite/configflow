<template>
  <div class="min-w-0">
    <Toolbar v-model:search="search" class="mihomo-proxy-toolbar" placeholder="搜索策略组或节点">
      <template #search-actions>
        <Button variant="outline" size="icon" class="size-11 shrink-0" aria-label="策略显示设置" title="设置" @click="settingsOpen = true"><Settings2 class="size-4" /></Button>
      </template>
      <template #actions>
        <Button variant="outline" class="min-h-11 max-sm:gap-1 max-sm:px-2 max-sm:text-[13px]" :disabled="!groups.length" @click="toggleAll">
          <ChevronsUpDown class="size-3.5" />{{ allExpanded ? '全部收起' : '全部展开' }}
        </Button>
        <Button variant="outline" class="min-h-11 max-sm:gap-1 max-sm:px-2 max-sm:text-[13px]" :aria-busy="batchRunning" :disabled="!allTestGroups.length || testing.size > 0 || nodeTesting.size > 0 || batchRunning" @click="testAll">
          <Loader2 v-if="batchRunning" class="size-3.5 animate-spin" aria-hidden="true" /><Zap v-else class="size-3.5" aria-hidden="true" />{{ batchRunning ? '正在测速…' : '全部分组测速' }}
        </Button>
      </template>
    </Toolbar>
    <div v-if="batchRunning" role="status" aria-atomic="true" class="mb-3 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <span>已完成 {{ batchDone }} / {{ batchTotal }} 组 · 依次测速，包含隐藏分组</span>
      <Button variant="ghost" size="sm" class="min-h-11" :disabled="batchStop" @click="batchStop = true">{{ batchStop ? '当前分组结束后停止' : '停止后续测速' }}</Button>
    </div>
    <MihomoProxySettings v-model:open="settingsOpen" :settings="settings" @update:settings="applySettings" />

    <p v-if="error && groups.length" role="alert" class="mb-3 flex flex-wrap items-center gap-x-3 break-words text-[12.5px] text-warning-accent">
      <span>读取最新策略组失败，下面是上次读到的内容：{{ error }}</span>
      <Button variant="ghost" size="sm" class="min-h-11" :disabled="loading" @click="load">重试</Button>
    </p>
    <LoadingRows v-if="loading && !groups.length" :rows="5" />
    <SectionCard v-else-if="error && !groups.length">
      <EmptyState :icon="TriangleAlert" title="读取策略组失败" :description="error">
        <Button variant="outline" class="min-h-11" @click="load">重试</Button>
      </EmptyState>
    </SectionCard>
    <SectionCard v-else-if="!visibleGroups.length">
      <EmptyState :icon="Search" :title="search ? '没有匹配的策略组' : '暂无策略组'" :description="search ? '换个关键词试试。' : 'Mihomo 配置中没有可展示的策略组。'" />
    </SectionCard>

    <div v-else class="grid min-w-0 items-start gap-3 xl:grid-cols-2" :class="{ 'select-none': settings.disableTextSelection }">
      <section v-for="group in visibleGroups" :key="group.name" class="min-w-0 rounded-xl border border-border bg-card shadow-surface">
        <header class="flex min-w-0 items-center gap-2 p-2 pl-4">
          <button type="button" class="flex min-h-11 min-w-0 flex-1 items-center gap-3 rounded-md text-left focus-visible:outline-2 focus-visible:outline-ring"
            :aria-expanded="isExpanded(group.name)" @click="toggle(group.name)">
            <ChevronRight class="size-4 shrink-0 text-muted-foreground transition-transform duration-200" :class="{ 'rotate-90': isExpanded(group.name) }" aria-hidden="true" />
            <span class="min-w-0 flex-1">
              <span class="flex min-w-0 flex-wrap items-center gap-2">
                <span class="break-all font-semibold text-foreground">{{ group.name }}</span>
                <span class="chip">{{ GROUP_TYPE_LABELS[group.type] || group.type }}</span>
                <span v-if="group.fixed" class="chip chip-warn">已固定</span>
                <span v-if="group.hidden" class="chip">隐藏</span>
                <span class="num text-xs text-muted-foreground">{{ group.all?.length || 0 }} 个</span>
              </span>
              <span v-if="group.now" class="mt-0.5 flex min-w-0 items-center gap-1.5 text-[12.5px] text-muted-foreground">
                <ArrowRight class="size-3 shrink-0" aria-hidden="true" />
                <span class="truncate">{{ group.now }}<template v-if="settings.showFinalNode && finalNode(proxies, group.now) !== group.now"> → {{ finalNode(proxies, group.now) }}</template></span>
                <span class="num shrink-0" :class="toneClass(delayOf(group.now, group))">{{ delayText(delayOf(group.now, group)) }}</span>
              </span>
            </span>
          </button>
          <Button v-if="group.fixed" variant="ghost" size="sm" class="min-h-11" :disabled="busy.has(group.name)" @click="unfix(group)">取消固定</Button>
          <Button variant="ghost" size="icon" class="size-11 shrink-0" :aria-label="`测速 ${group.name}`" :title="'测速'"
            :disabled="batchRunning || testing.has(group.name) || nodeTesting.size > 0" @click="testGroup(group)">
            <Loader2 v-if="testing.has(group.name)" class="size-4 animate-spin" />
            <Zap v-else class="size-4" />
          </Button>
        </header>
        <div v-if="isExpanded(group.name)" class="space-y-3 border-t border-border p-3">
          <div v-for="section in sectionsOf(group)" :key="section.title">
            <div v-if="settings.groupByProvider" class="mb-1.5 text-[11.5px] text-muted-foreground">{{ section.title }} · {{ section.members.length }}</div>
            <div class="grid gap-2" :style="{ gridTemplateColumns: `repeat(auto-fill, minmax(min(${settings.cardMinWidth}px, 100%), 1fr))` }">
              <div v-for="member in section.members" :key="member" class="flex min-w-0 items-center rounded-lg border transition-colors duration-200"
                :class="member === group.now ? 'border-primary-accent/60 bg-primary-soft' : 'border-border bg-background hover:border-border-strong'">
                <button type="button" class="flex min-h-11 min-w-0 flex-1 flex-col justify-center rounded-lg px-3 py-1.5 text-left focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default enabled:hover:bg-accent"
                  :disabled="!canSwitch(group) || busy.has(group.name)" :aria-pressed="member === group.now" @click="select(group, member)">
                  <span class="max-w-full truncate text-[13px] text-foreground" :title="member">{{ member }}</span>
                  <span class="flex max-w-full flex-wrap items-center gap-x-2 text-[11.5px] text-muted-foreground">
                    <span class="truncate" :title="proxies[member]?.type">{{ proxies[member]?.type || '—' }}</span>
                    <span v-if="!canTestNode(member)" class="num whitespace-nowrap" :class="toneClass(delayOf(member, group))">{{ delayText(delayOf(member, group)) }}</span>
                  </span>
                </button>
                <Button v-if="canTestNode(member)" variant="ghost" size="icon" class="h-auto min-h-11 min-w-11 w-auto shrink-0 flex-col gap-0.5 self-stretch px-1.5 text-muted-foreground"
                  :aria-label="`${GROUP_TYPES.has(proxies[member]?.type || '') ? '当前出口测速' : '单节点测速'} ${member}（${group.name}）`"
                  :title="GROUP_TYPES.has(proxies[member]?.type || '') ? '测试当前出口的延迟' : '测速'"
                  :aria-busy="nodeTesting.has(delayKey(member, group))"
                  :disabled="batchRunning || testing.size > 0 || nodeTesting.has(delayKey(member, group))" @click.stop="testNode(member, group)">
                  <Loader2 v-if="nodeTesting.has(delayKey(member, group))" class="size-3.5 animate-spin" /><Zap v-else class="size-3.5" />
                  <span class="num whitespace-nowrap text-[11px] font-normal" :class="toneClass(delayOf(member, group))">{{ delayText(delayOf(member, group)) }}</span>
                </Button>
              </div>
            </div>
          </div>
          <p v-if="!sectionsOf(group).length" class="m-0 text-xs text-muted-foreground">没有可显示的节点{{ settings.hideUnavailable ? '（已隐藏不可用节点）' : '' }}。</p>
        </div>
      </section>
    </div>

    <SectionCard v-if="providers.length" class="mt-5" title="代理集合" description="proxy-providers 订阅，可手动更新或触发健康检查" :padded="false">
      <div class="cf-table-wrap rounded-none border-x-0 border-b-0">
        <table class="cf-table proxy-provider-rows min-w-[640px] max-sm:min-w-0">
          <thead><tr><th>名称</th><th>节点数</th><th>流量 / 到期</th><th>更新时间</th><th class="cf-table__right">操作</th></tr></thead>
          <tbody>
            <tr v-for="provider in providers" :key="provider.name">
              <td class="break-all font-medium" data-cell="name"><span class="proxy-provider-name" :title="provider.name">{{ provider.name }}</span> <span class="chip ml-1">{{ provider.vehicleType }}</span></td>
              <td class="num" data-cell="count">{{ provider.proxies?.length || 0 }}<span class="sm:hidden"> 个节点</span></td>
              <td class="text-xs text-muted-foreground" data-cell="usage" :data-empty="subscriptionText(provider) === '—' || undefined">{{ subscriptionText(provider) }}</td>
              <td class="text-xs text-muted-foreground" data-cell="updated">{{ provider.updatedAt ? relativeTime(provider.updatedAt) : '—' }}</td>
              <td class="cf-table__right" data-cell="actions">
                <Button variant="ghost" size="sm" class="mihomo-row-action min-h-11" title="健康检查" :disabled="busy.has('p:' + provider.name)" @click="healthcheck(provider)">
                  <Activity class="size-3.5 sm:hidden" aria-hidden="true" /><span class="max-sm:sr-only">健康检查</span>
                </Button>
                <Button v-if="provider.vehicleType === 'HTTP'" variant="ghost" size="sm" class="mihomo-row-action min-h-11" title="更新" :disabled="busy.has('p:' + provider.name)" @click="updateProvider(provider)">
                  <Loader2 v-if="busy.has('p:' + provider.name)" class="size-3.5 animate-spin" /><RefreshCw v-else class="size-3.5 sm:hidden" aria-hidden="true" /><span class="max-sm:sr-only">更新</span>
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, toRaw, watch } from 'vue'
import { Activity, ArrowRight, ChevronRight, ChevronsUpDown, Loader2, RefreshCw, Search, Settings2, TriangleAlert, Zap } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import Toolbar from '@/components/common/Toolbar.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import MihomoProxySettings from './MihomoProxySettings.vue'
import { useTabActive } from '@/components/ui/tabs'
import { mihomoPanelApi } from '@/api'
import { notify } from '@/lib/feedback'
import { formatBytes, relativeTime } from '@/lib/format'
import { cachedPanelData, rememberPanelProxy } from '@/lib/mihomoPanelCache'
import {
  GROUP_TYPE_LABELS, GROUP_TYPES, SWITCHABLE_GROUP_TYPES, compatibleFirst, delayText, delayTone, finalNode, latestDelay, loadProxySettings,
  orderedGroups, panelError, saveProxySettings, type MihomoProvider, type MihomoProxy, type ProxyPanelSettings
} from '@/types/mihomo'

const props = defineProps<{ agentId: string; reloadToken: number }>()
// 所在标签是否为当前标签：由 Tabs 注入，切换标签不经过 props，隐藏的面板不会整组件重新渲染
const active = useTabActive()

const proxies = ref<Record<string, MihomoProxy>>({})
const providers = ref<MihomoProvider[]>([])
/** 保留核心 /proxies 的节点，兼容旧核心将订阅节点一并返回的情况。 */
const coreProxyNames = ref(new Set<string>())
/** 同名节点可能来自多个代理集合，不能只保留最后一个来源。 */
const nodeProviders = ref(new Map<string, Set<string>>())
/** ConfigFlow 给带排除条件的策略组生成的「<组>_<集合>_筛选」副本，与原集合下载同一订阅。 */
const FILTER_COPY = /_筛选(?: \(\d+\))?$/
/** 节点的来源集合；与原集合同时出现的筛选副本是同一批节点，不算另一个来源。 */
function sourcesOf(name: string): string[] {
  const sources = [...(nodeProviders.value.get(name) || [])]
  return sources.filter(source => {
    const base = source.replace(FILTER_COPY, '')
    return base === source || !sources.some(original => original !== source && base.endsWith(`_${original}`))
  })
}
const settings = ref<ProxyPanelSettings>(loadProxySettings())
const settingsOpen = ref(false)
function applySettings(value: ProxyPanelSettings) {
  settings.value = value
  saveProxySettings(value)
}
const loading = ref(false)
const error = ref('')
const search = ref('')
const expanded = ref(new Set<string>())
const testing = reactive(new Set<string>())
const nodeTesting = reactive(new Set<string>())
const batchRunning = ref(false)
const batchStop = ref(false)
const batchDone = ref(0)
const batchTotal = ref(0)
let disposed = false
let testEpoch = 0
const delayKey = (name: string, group?: MihomoProxy) => JSON.stringify([name, group?.testUrl || ''])
const canTestNode = (name: string) => !!proxies.value[name] && !['Reject', 'RejectDrop'].includes(proxies.value[name].type)
const busy = reactive(new Set<string>())
/** 本次会话里测速得到的延迟，优先于核心记录的历史 */
const delays = reactive(new Map<string, { delay: number; target: string; revision: number }>())
let delayRevision = 0

const allTestGroups = computed(() => orderedGroups(proxies.value, true))
const groups = computed(() => orderedGroups(proxies.value, settings.value.showHiddenGroups))
const visibleGroups = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return groups.value
  return groups.value.filter(group => group.name.toLowerCase().includes(keyword)
    || group.all?.some(member => member.toLowerCase().includes(keyword)))
})
const allExpanded = computed(() => groups.value.length > 0 && groups.value.every(group => expanded.value.has(group.name)))

const isExpanded = (name: string) => expanded.value.has(name) || (!!search.value.trim() && visibleGroups.value.length <= 6)
function toggle(name: string) {
  const next = new Set(expanded.value)
  if (next.has(name)) next.delete(name)
  else next.add(name)
  expanded.value = next
}
function toggleAll() {
  expanded.value = allExpanded.value ? new Set() : new Set(groups.value.map(group => group.name))
}

function membersOf(group: MihomoProxy) {
  const keyword = search.value.trim().toLowerCase()
  let members = group.all || []
  if (keyword && !group.name.toLowerCase().includes(keyword)) members = members.filter(member => member.toLowerCase().includes(keyword))
  if (settings.value.hideUnavailable) {
    members = members.filter(member => member === group.now || (delayOf(member, group) !== 0 && proxies.value[member]?.alive !== false))
  }
  if (settings.value.sort === 'name') members = [...members].sort((a, b) => a.localeCompare(b))
  if (settings.value.sort === 'delay') {
    // 未测速和超时排在最后
    const rank = (member: string) => { const delay = delayOf(member, group); return delay && delay > 0 ? delay : Number.MAX_SAFE_INTEGER }
    members = [...members].sort((a, b) => rank(a) - rank(b))
  }
  return members
}

function sectionsOf(group: MihomoProxy): Array<{ title: string; members: string[] }> {
  const members = membersOf(group)
  if (!settings.value.groupByProvider) return members.length ? [{ title: '', members }] : []
  const sections = new Map<string, string[]>()
  for (const member of members) {
    const sources = sourcesOf(member)
    const title = GROUP_TYPES.has(proxies.value[member]?.type || '') ? '策略组'
      : sources.length > 1 ? '多个代理集合'
      : !sources.length || sources[0] === 'default' ? '配置节点' : sources[0]
    sections.set(title, [...(sections.get(title) || []), member])
  }
  return [...sections].map(([title, items]) => ({ title, members: items }))
}

const canSwitch = (group: MihomoProxy) => SWITCHABLE_GROUP_TYPES.has(group.type)

/** 成员是策略组时，延迟取它当前选中的节点 */
function delayOf(name: string, group?: MihomoProxy, depth = 0): number | undefined {
  // 只读取当前出口的结果；单项与整组测试取较新一批，同批优先保留本卡片的结果。
  const target = finalNode(proxies.value, name)
  const ownResult = delays.get(delayKey(name, group))
  const targetResult = delays.get(delayKey(target, group))
  let cached = ownResult?.target === target ? ownResult : undefined
  if (targetResult?.target === target && (!cached || targetResult.revision > cached.revision)) cached = targetResult
  if (cached) return cached.delay
  const proxy = proxies.value[name]
  if (!proxy) return undefined
  const own = latestDelay(proxy, group?.testUrl)
  if (own !== undefined || !proxy.now || depth > 5) return own
  return delayOf(proxy.now, group, depth + 1)
}

const toneClass = (delay: number | undefined) => ({
  success: 'text-success-accent', warning: 'text-warning-accent', danger: 'text-destructive-accent', muted: 'text-muted-foreground'
})[delayTone(delay)]

function subscriptionText(provider: MihomoProvider) {
  const info = provider.subscriptionInfo
  if (!info?.Total) return '—'
  const used = formatBytes((info.Upload || 0) + (info.Download || 0))
  const expire = info.Expire ? ' · ' + new Date(info.Expire * 1000).toLocaleDateString() + ' 到期' : ''
  return `${used} / ${formatBytes(info.Total)}${expire}`
}

let epoch = 0
/** 上次读到的订阅节点，重新读取时先沿用，免得节点类型和延迟闪成空白 */
let providerProxies: Record<string, MihomoProxy> = {}
/** 正在读的代理集合；单节点测速要等它读完才能判断节点来源 */
let providersRead: Promise<unknown> | null = null
async function load() {
  const requestEpoch = ++epoch
  loading.value = true
  error.value = ''
  // 代理集合列出每个订阅节点，比 /proxies 大得多、慢得多；策略组先显示，节点信息读到后再补上
  const providerRequest = mihomoPanelApi.proxyProviders(props.agentId).catch(() => null)
  providersRead = providerRequest
  void providerRequest.then(() => { if (providersRead === providerRequest) providersRead = null })
  try {
    const proxyResult = await mihomoPanelApi.proxies(props.agentId)
    if (requestEpoch !== epoch) return
    const coreProxies = proxyResult.data.data?.proxies || {}
    coreProxyNames.value = new Set(Object.keys(coreProxies))
    proxies.value = { ...providerProxies, ...coreProxies }
  } catch (failure) {
    if (requestEpoch === epoch) error.value = panelError(failure, '暂时无法读取策略组。')
    return
  } finally {
    if (requestEpoch === epoch) loading.value = false
  }
  const providerResult = await providerRequest
  if (requestEpoch !== epoch || !providerResult) return
  applyProviders(providerResult.data.data?.providers || {})
}

function applyProviders(providerMap: Record<string, MihomoProvider>) {
  // /proxies 不含 proxy-providers 里的节点，合并进来才能显示策略组成员的类型和延迟。
  // Compatible 集合是 /proxies 已经完整列出的配置节点和策略组，后端只给名字，排在前面不覆盖完整信息。
  providerProxies = Object.fromEntries(compatibleFirst(Object.values(providerMap))
    .flatMap(provider => provider.proxies || []).map(proxy => [proxy.name, proxy]))
  const sources = new Map<string, Set<string>>()
  for (const [providerName, provider] of Object.entries(providerMap)) {
    // 普通节点会同时出现在 default 和各策略组的 Compatible 集合中，均属于配置节点。
    const source = provider.vehicleType === 'Compatible' ? 'default' : providerName
    for (const proxy of provider.proxies || []) {
      if (!sources.has(proxy.name)) sources.set(proxy.name, new Set())
      sources.get(proxy.name)!.add(source)
    }
  }
  nodeProviders.value = sources
  proxies.value = { ...providerProxies, ...Object.fromEntries([...coreProxyNames.value].map(name => [name, proxies.value[name]])) }
  providers.value = Object.values(providerMap).filter(provider => provider.vehicleType !== 'Compatible')
}

/** 先显示这台设备上次读到的策略组和订阅节点，load() 读到新数据后替换 */
function showCached() {
  const core = cachedPanelData<{ proxies?: Record<string, MihomoProxy> }>(props.agentId, 'proxies')?.proxies
  if (!core || groups.value.length) return
  coreProxyNames.value = new Set(Object.keys(core))
  proxies.value = { ...core }
  const providerMap = cachedPanelData<{ providers?: Record<string, MihomoProvider> }>(props.agentId, 'providers/proxies')?.providers
  if (providerMap) applyProviders(providerMap)
}

async function select(group: MihomoProxy, member: string) {
  if (member === group.now && !group.fixed) return
  busy.add(group.name)
  try {
    await mihomoPanelApi.selectProxy(props.agentId, group.name, member)
    const switched = { ...toRaw(group), now: member, fixed: group.type === 'Selector' ? group.fixed : member }
    proxies.value[group.name] = switched
    rememberPanelProxy(props.agentId, switched)
    const closed = settings.value.closeConnectionsOnSwitch ? await closeGroupConnections(group.name) : 0
    notify.success(`${group.name} 已切换到 ${member}`, closed ? `已断开经过该策略组的 ${closed} 条连接` : undefined)
  } catch (failure) {
    notify.error('切换失败', panelError(failure))
  } finally {
    busy.delete(group.name)
  }
}

/** 断开链路经过该策略组的连接，让后续连接走新节点；失败不影响切换结果 */
async function closeGroupConnections(groupName: string): Promise<number> {
  try {
    const { data } = await mihomoPanelApi.connections(props.agentId)
    const targets = ((data.data?.connections || []) as Array<{ id: string; chains: string[] }>)
      .filter(connection => connection.chains.includes(groupName))
    const results = await Promise.allSettled(targets.map(connection => mihomoPanelApi.closeConnection(props.agentId, connection.id)))
    return results.filter(result => result.status === 'fulfilled').length
  } catch {
    return 0
  }
}

async function unfix(group: MihomoProxy) {
  busy.add(group.name)
  try {
    await mihomoPanelApi.unfixProxy(props.agentId, group.name)
    notify.success(`${group.name} 已恢复自动选择`)
    await load()
  } catch (failure) {
    notify.error('取消固定失败', panelError(failure))
  } finally {
    busy.delete(group.name)
  }
}

/** 核心会自行切换策略组出口（自动测速、重载后先用首个成员），测成员前先读实时出口，不测页面里过期的那个。 */
async function refreshExits(agentId: string, generation: number) {
  const { data } = await mihomoPanelApi.proxies(agentId)
  if (disposed || generation !== testEpoch) return false
  const coreProxies = data.data?.proxies || {}
  coreProxyNames.value = new Set(Object.keys(coreProxies))
  proxies.value = { ...proxies.value, ...coreProxies }
  return true
}

async function testNode(name: string, group: MihomoProxy) {
  const key = delayKey(name, group)
  if (batchRunning.value || testing.size || nodeTesting.has(key)) return
  const generation = testEpoch
  const agentId = props.agentId
  nodeTesting.add(key)
  let exit = ''
  try {
    // 策略组先于代理集合显示，订阅节点的来源要等集合读完才知道
    if (providersRead) {
      await providersRead
      if (disposed || generation !== testEpoch) return
    }
    if (GROUP_TYPES.has(proxies.value[name]?.type || '') && !await refreshExits(agentId, generation)) return
    const target = finalNode(proxies.value, name)
    const proxy = proxies.value[target]
    if (!proxy) throw new Error('找不到该节点，请刷新后重试。')
    // 负载均衡和链式代理没有固定 now，由核心通过单项端点测量实际出站。
    const nativeRoute = !proxy.now && ['LoadBalance', 'Relay'].includes(proxy.type)
    if (GROUP_TYPES.has(proxy.type) && !nativeRoute) throw new Error('策略组没有可用的当前出口，请刷新后重试。')
    if (['Reject', 'RejectDrop'].includes(proxy.type)) throw new Error('当前出口拒绝连接，无法测速。')
    const sources = sourcesOf(target)
    const inCore = coreProxyNames.value.has(target)
    // 点的是策略组成员时，由核心沿这个组当前的路线测速，不必判断出口节点来自哪个集合。
    const viaGroup = sources.length > 1 && name !== target && coreProxyNames.value.has(name)
    if (sources.length > 1 && !viaGroup) throw new Error('多个来源中存在同名节点，请使用所属策略组的分组测速。')
    if (!viaGroup && !inCore && !sources.length) throw new Error('找不到该节点，请刷新后重试。')
    if (target !== name) exit = target
    const { data } = await (viaGroup || inCore
      ? mihomoPanelApi.proxyDelay(agentId, viaGroup ? name : target, group.testUrl)
      : mihomoPanelApi.providerProxyDelay(agentId, sources[0], target, group.testUrl))
    if (disposed || generation !== testEpoch) return
    const delay = data.data?.delay
    if (typeof delay !== 'number' || !Number.isFinite(delay) || delay < 0) throw new Error('测速结果无效，请重试。')
    delays.set(delayKey(target, group), { delay, target, revision: ++delayRevision })
  } catch (failure) {
    // 组成员失败时写明实际测的出口，免得误以为整组不可用
    if (!disposed && generation === testEpoch) notify.error(`${name} 测速失败`, exit ? `当前出口 ${exit}：${panelError(failure)}` : panelError(failure))
  } finally { if (generation === testEpoch) nodeTesting.delete(key) }
}

async function testGroup(group: MihomoProxy, inBatch = false): Promise<boolean> {
  if (testing.has(group.name) || nodeTesting.size || (batchRunning.value && !inBatch)) return false
  const generation = testEpoch
  const agentId = props.agentId
  const targets = (group.all || []).map(member => ({ member, target: finalNode(proxies.value, member) }))
  testing.add(group.name)
  try {
    const { data } = await mihomoPanelApi.groupDelay(agentId, group.name, group.testUrl)
    if (disposed || generation !== testEpoch) return false
    const result: Record<string, number> = data.data || {}
    const revision = ++delayRevision
    for (const { member, target } of targets) {
      if (finalNode(proxies.value, member) !== target) continue
      delays.set(delayKey(member, group), { delay: result[member] ?? 0, target, revision })
    }
    if (!inBatch && group.type !== 'Selector') await load()
    return true
  } catch (failure) {
    if (!disposed && generation === testEpoch) notify.error(`${group.name} 测速失败`, panelError(failure))
    return false
  } finally { if (generation === testEpoch) testing.delete(group.name) }
}

async function testAll() {
  if (batchRunning.value || testing.size || nodeTesting.size) return
  const queue = [...allTestGroups.value]
  if (!queue.length) return
  const generation = testEpoch
  batchRunning.value = true
  batchStop.value = false
  batchDone.value = 0
  batchTotal.value = queue.length
  let failed = 0
  try {
    for (const group of queue) {
      if (disposed || generation !== testEpoch || batchStop.value) break
      const success = await testGroup(group, true)
      if (disposed || generation !== testEpoch) return
      if (!success) failed++
      batchDone.value++
    }
    if (disposed || generation !== testEpoch) return
    notify.info(batchStop.value ? '已停止后续测速' : '全部分组测速完成', `已测 ${batchDone.value} 组${failed ? `，${failed} 组失败` : ''}`)
    await load()
  } finally { if (generation === testEpoch) batchRunning.value = false }
}

async function updateProvider(provider: MihomoProvider) {
  busy.add('p:' + provider.name)
  try {
    await mihomoPanelApi.updateProxyProvider(props.agentId, provider.name)
    notify.success(`${provider.name} 已更新`)
    await load()
  } catch (failure) {
    notify.error('更新代理集合失败', panelError(failure))
  } finally {
    busy.delete('p:' + provider.name)
  }
}

async function healthcheck(provider: MihomoProvider) {
  busy.add('p:' + provider.name)
  try {
    await mihomoPanelApi.healthcheckProxyProvider(props.agentId, provider.name)
    notify.success(`${provider.name} 健康检查完成`)
    delays.clear()
    await load()
  } catch (failure) {
    notify.error('健康检查失败', panelError(failure))
  } finally {
    busy.delete('p:' + provider.name)
  }
}

function resetTests() {
  testEpoch++; testing.clear(); nodeTesting.clear(); delays.clear()
  batchRunning.value = false; batchStop.value = true
}
// 切走后面板保持挂载：隐藏期间的刷新只做标记，切回时再读取
let stale = false
watch(() => props.reloadToken, () => { resetTests(); stale = !active.value; if (active.value) void load() })
watch(active, shown => { if (shown && stale) { stale = false; void load() } })
watch(() => props.agentId, () => {
  resetTests(); epoch++; proxies.value = {}; providerProxies = {}; providers.value = []; coreProxyNames.value.clear(); nodeProviders.value.clear()
  showCached()
  void load()
})
onUnmounted(() => { disposed = true; testEpoch++; epoch++; batchStop.value = true })
onMounted(() => { showCached(); void load() })
</script>

<style scoped>
@media (width < 640px) {
  .mihomo-proxy-toolbar :deep(.cf-toolbar-search) {
    flex-basis: 100%;
  }

  .mihomo-proxy-toolbar :deep(.cf-toolbar-actions) {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
    margin-left: 0;
  }

  /* 代理集合两行：名称与来源在上、健康检查和更新两个 28px 图标在右；节点数 · 流量 / 到期 · 更新时间在下 */
  .proxy-provider-rows,
  .proxy-provider-rows tbody { display: block; }

  .proxy-provider-rows thead {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .proxy-provider-rows tbody tr {
    display: grid;
    /* 流量 / 到期过长时只截断它，更新时间紧跟其后；末尾空列吸收剩余宽度 */
    grid-template-columns: auto minmax(0, max-content) auto minmax(0, 1fr) auto;
    grid-template-areas: 'name name name name actions' 'count usage updated . actions';
    align-items: center;
    column-gap: 6px;
    row-gap: 2px;
    padding: 8px 8px 8px 12px;
    border-bottom: 1px solid var(--cf-bd);
  }

  .proxy-provider-rows tbody tr:last-child { border-bottom: 0; }

  .proxy-provider-rows td {
    display: block;
    min-width: 0;
    padding: 0;
    border: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11.5px;
    line-height: 16px;
    color: var(--muted-foreground);
  }

  .proxy-provider-rows td[data-cell='name'] {
    grid-area: name;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    line-height: 20px;
    color: var(--foreground);
  }

  .proxy-provider-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .proxy-provider-rows td[data-cell='name'] .chip {
    flex-shrink: 0;
    height: 18px;
    margin-left: 0;
    padding: 0 6px;
    font-size: 10.5px;
  }

  .proxy-provider-rows td[data-cell='count'] { grid-area: count; }
  .proxy-provider-rows td[data-cell='usage'] { grid-area: usage; }
  .proxy-provider-rows td[data-cell='updated'] { grid-area: updated; }
  .proxy-provider-rows td[data-cell='usage'][data-empty] { display: none; }
  .proxy-provider-rows td[data-cell='usage']::before,
  .proxy-provider-rows td[data-cell='updated']::before { content: '· '; }

  .proxy-provider-rows td[data-cell='actions'] {
    grid-area: actions;
    display: flex;
    gap: 16px;
    overflow: visible;
    margin-left: 4px;
  }

  /* 行内按钮看起来 28px，透明 ::before 向外 8px，点击区仍为 44px；不被基础按钮的 44px 最小尺寸撑高 */
  .mihomo-row-action {
    position: relative;
    width: 28px;
    height: 28px;
    min-width: 0;
    min-height: 0;
    padding: 0;
  }

  .mihomo-row-action::before {
    content: '';
    position: absolute;
    inset: -8px;
  }
}
</style>
