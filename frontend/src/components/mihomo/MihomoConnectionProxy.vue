<template>
  <div class="min-w-0 space-y-4">
    <LoadingRows v-if="loading && !path.length" :rows="3" />
    <EmptyState v-else-if="error && !path.length" :icon="TriangleAlert" title="读取策略组失败" :description="error">
      <Button variant="outline" class="min-h-11" @click="load">重试</Button>
    </EmptyState>
    <template v-else>
      <nav v-if="path.length" aria-label="代理路径" class="flex min-w-0 flex-wrap items-center gap-1">
        <template v-for="(name, index) in path" :key="`${index}:${name}`">
          <ChevronRight v-if="index" class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
          <button type="button" class="min-h-11 min-w-0 max-w-full rounded-lg border px-3 py-2 text-left text-[13px] transition-colors focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-wait"
            :class="index === activeIndex ? 'border-primary-accent/60 bg-primary-soft text-primary-accent' : 'border-transparent bg-secondary/60 text-foreground hover:bg-accent'"
            :aria-label="`查看 ${name}`" :aria-current="index === activeIndex ? 'step' : undefined" :disabled="!!pending" @click="activeIndex = index">
            <span class="block break-all">{{ name }}</span>
          </button>
        </template>
      </nav>

      <div v-if="error || providerWarning || actionError" role="alert" class="space-y-1 text-[13px]">
        <p v-if="error" class="m-0 text-destructive-accent">{{ error }}</p>
        <p v-if="providerWarning" class="m-0 text-warning-accent">{{ providerWarning }}</p>
        <p v-if="actionError" class="m-0 text-destructive-accent">{{ actionError }}</p>
        <Button v-if="error || providerWarning" variant="outline" size="sm" class="min-h-11" :disabled="loading || !!pending" @click="load">重新读取</Button>
      </div>
      <p v-if="pathWarning" role="status" class="m-0 text-xs text-warning-accent">{{ pathWarning }}</p>
      <p v-if="browsingAlternative" class="m-0 text-xs text-muted-foreground">正在查看未选中的下级策略组，查看不会切换出口。</p>

      <section v-if="current" class="min-w-0 space-y-3" :aria-label="current.name">
        <header class="flex min-w-0 flex-wrap items-center gap-2">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="m-0 break-all text-base font-semibold text-foreground">{{ current.name }}</h3>
              <span class="chip">{{ GROUP_TYPE_LABELS[current.type] || current.type }}</span>
              <span v-if="current.fixed" class="chip chip-warn">已固定</span>
              <span v-if="isGroup(current)" class="num text-xs text-muted-foreground">{{ current.all?.length || 0 }} 个</span>
            </div>
            <p v-if="current.now" class="mt-1 mb-0 break-all text-[13px] text-muted-foreground">当前选择：{{ current.now }}</p>
          </div>
          <Button v-if="current.fixed && current.type !== 'Selector' && canSwitch(current)" variant="outline" size="sm" class="min-h-11" :disabled="loading || !!pending || !!error" @click="unfix">恢复自动</Button>
          <Button variant="ghost" size="icon" class="size-11 shrink-0" aria-label="刷新策略" :disabled="loading || !!pending" @click="load">
            <Loader2 v-if="loading" class="size-4 animate-spin" /><RefreshCw v-else class="size-4" />
          </Button>
        </header>

        <div v-if="isGroup(current) && current.all?.length" class="grid min-w-0 grid-cols-1 gap-2 min-[360px]:grid-cols-2 sm:grid-cols-3 lg:grid-cols-4">
          <div v-for="member in current.all" :key="member" class="flex min-w-0 flex-col rounded-lg border transition-colors"
            :class="member === current.now ? 'border-primary-accent/60 bg-primary-soft' : 'border-border bg-background hover:border-border-strong'">
            <button type="button" class="flex min-h-20 min-w-0 flex-1 flex-col gap-2 rounded-lg px-3 py-2.5 text-left focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default enabled:hover:bg-accent"
              :aria-label="`选择 ${member}`" :aria-pressed="member === current.now" :disabled="!canSwitch(current) || loading || !!pending || !!error" @click="select(member)">
              <span class="line-clamp-2 w-full break-all text-[13px] font-medium text-foreground" :title="member">{{ member }}</span>
              <span class="mt-auto flex w-full flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[11.5px] text-muted-foreground">
                <span>{{ proxies[member]?.type || '未知类型' }}<template v-if="proxies[member]?.udp"> / UDP</template></span>
                <span class="num whitespace-nowrap" :class="toneClass(delayOf(member))">{{ delayText(delayOf(member)) }}</span>
              </span>
            </button>
            <button v-if="isGroup(proxies[member])" type="button" class="flex min-h-11 items-center justify-between gap-1 rounded-b-lg border-t border-border px-3 text-xs text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-wait"
              :aria-label="`查看下级 ${member}`" :disabled="!!pending" @click="inspect(member)">
              查看下级<ChevronRight class="size-3.5 shrink-0" aria-hidden="true" />
            </button>
          </div>
        </div>
        <EmptyState v-else-if="isGroup(current)" :icon="Network" title="该策略组没有候选节点" description="请检查 Mihomo 配置或代理集合是否已加载。" />
        <div v-else class="flex flex-wrap items-center gap-3 border-y border-border py-4 text-[13px] text-muted-foreground">
          <span>出站节点</span><span>{{ current.type }}<template v-if="current.udp"> / UDP</template></span>
          <span class="num" :class="toneClass(delayOf(current.name))">{{ delayText(delayOf(current.name)) }}</span>
          <span>选择上方策略组可切换节点。</span>
        </div>
        <p v-if="isGroup(current) && !canSwitch(current)" class="m-0 text-xs text-muted-foreground">此策略组由内核分配出口，不能手动选择成员；可查看下级策略组。</p>
        <p v-else-if="current.type === 'URLTest' || current.type === 'Fallback'" class="m-0 text-xs text-muted-foreground">手动选择后会固定该节点，点击“恢复自动”可交回内核选择。</p>
      </section>
      <EmptyState v-else :icon="Network" :title="path.length ? '当前配置中找不到此节点或策略组' : '此连接没有代理链信息'" description="连接建立后，配置或订阅中的节点可能已发生变化。">
        <Button variant="outline" class="min-h-11" :disabled="loading" @click="load">重新读取</Button>
      </EmptyState>
    </template>

    <div class="space-y-1 border-t border-border pt-3 text-xs leading-5 text-muted-foreground">
      <p v-if="pending" role="status" class="m-0 flex items-center gap-2 text-primary-accent"><Loader2 class="size-3.5 animate-spin" />{{ pending }}</p>
      <p class="m-0">切换会影响所有使用该策略组的连接。{{ closeOnSwitch ? '按策略页设置，切换后断开经过该组的连接，让客户端重新连接。' : '按策略页设置，保留已有连接，新连接使用新选择。' }}</p>
      <p class="m-0 break-all">此连接建立时：{{ originalPath.join(' → ') || '无代理链' }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { ChevronRight, Loader2, Network, RefreshCw, TriangleAlert } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { mihomoPanelApi } from '@/api'
import { notify } from '@/lib/feedback'
import {
  GROUP_TYPES, GROUP_TYPE_LABELS, SWITCHABLE_GROUP_TYPES, compatibleFirst, delayText, delayTone, latestDelay, loadProxySettings, panelError,
  type ConnectionRow, type MihomoProvider, type MihomoProxy
} from '@/types/mihomo'

const props = defineProps<{ agentId: string; row: ConnectionRow }>()
const emit = defineEmits<{ changed: [] }>()
const proxies = ref<Record<string, MihomoProxy>>({})
const path = ref<string[]>([])
const activeIndex = ref(0)
const current = computed(() => proxies.value[path.value[activeIndex.value]])
const originalPath = computed(() => [...props.row.chains].reverse())
const loading = ref(false)
const error = ref('')
const providerWarning = ref('')
const actionError = ref('')
const pathWarning = ref('')
const pending = ref('')
const closeOnSwitch = ref(loadProxySettings().closeConnectionsOnSwitch)
let generation = 0
let request = 0
const isGroup = (proxy?: MihomoProxy) => !!proxy && GROUP_TYPES.has(proxy.type)
const canSwitch = (proxy: MihomoProxy) => SWITCHABLE_GROUP_TYPES.has(proxy.type)
const browsingAlternative = computed(() => path.value.slice(0, activeIndex.value).some((name, index) => {
  const selected = proxies.value[name]?.now
  return selected && selected !== path.value[index + 1]
}))

/** 当前选择用于浏览，不改写连接建立时的 chains；循环引用到此为止。 */
function follow(prefix: string[]): string[] {
  const result = [...prefix]
  const seen = new Set(result)
  pathWarning.value = ''
  while (result.length && result.length < 32) {
    const proxy = proxies.value[result[result.length - 1]]
    if (!isGroup(proxy) || !proxy.now) return result
    if (seen.has(proxy.now)) {
      pathWarning.value = '策略组存在循环引用，已停止展开后续路径。'
      return result
    }
    result.push(proxy.now)
    seen.add(proxy.now)
  }
  if (result.length >= 32) pathWarning.value = '策略路径过深，已停止展开后续路径。'
  return result
}

function inspect(member: string) {
  if (pending.value || !current.value?.all?.includes(member) || !isGroup(proxies.value[member])) return
  const prefix = path.value.slice(0, activeIndex.value + 1)
  const existing = prefix.indexOf(member)
  if (existing >= 0) { activeIndex.value = existing; return }
  path.value = follow([...prefix, member])
  activeIndex.value = prefix.length
}

function delayOf(name: string): number | undefined {
  const seen = new Set<string>()
  const testUrl = current.value?.testUrl
  while (!seen.has(name) && seen.size < 32) {
    seen.add(name)
    const proxy = proxies.value[name]
    const delay = latestDelay(proxy, testUrl)
    if (delay !== undefined || !proxy?.now || !isGroup(proxy)) return delay
    name = proxy.now
  }
  return undefined
}
const toneClass = (delay: number | undefined) => ({
  success: 'text-success-accent', warning: 'text-warning-accent', danger: 'text-destructive-accent', muted: 'text-muted-foreground'
})[delayTone(delay)]

async function load() {
  const epoch = generation
  const revision = ++request
  const agentId = props.agentId
  loading.value = true
  error.value = ''
  try {
    let failedProvider = false
    const [proxyResult, providerResult] = await Promise.all([
      mihomoPanelApi.proxies(agentId),
      mihomoPanelApi.proxyProviders(agentId).catch(() => { failedProvider = true; return null })
    ])
    if (epoch !== generation || revision !== request) return
    const providers: Record<string, MihomoProvider> = providerResult?.data.data?.providers || {}
    const members = Object.fromEntries(compatibleFirst(Object.values(providers)).flatMap(provider => provider.proxies || []).map(proxy => [proxy.name, proxy]))
    proxies.value = { ...members, ...(proxyResult.data.data?.proxies || {}) }
    providerWarning.value = failedProvider ? '代理集合读取失败，部分节点的类型和延迟暂不可用。' : ''
    const prefix = path.value.length ? path.value.slice(0, activeIndex.value + 1) : originalPath.value.slice(0, 1)
    path.value = follow(prefix)
  } catch (failure) {
    if (epoch === generation && revision === request) error.value = panelError(failure, '暂时无法读取策略组。')
  } finally {
    if (epoch === generation && revision === request) loading.value = false
  }
}

async function select(member: string) {
  const group = current.value
  if (!group || !canSwitch(group) || !group.all?.includes(member) || pending.value || loading.value || error.value) return
  if (member === group.now && (group.type === 'Selector' || group.fixed === member)) return
  const epoch = generation
  const agentId = props.agentId
  const prefix = path.value.slice(0, activeIndex.value + 1)
  closeOnSwitch.value = loadProxySettings().closeConnectionsOnSwitch
  const shouldClose = closeOnSwitch.value && member !== group.now
  pending.value = `正在切换到 ${member}…`
  actionError.value = ''
  try {
    await mihomoPanelApi.selectProxy(agentId, group.name, member)
    if (epoch !== generation) return
    proxies.value[group.name] = { ...group, now: member, fixed: group.type === 'Selector' ? group.fixed : member }
    path.value = follow(prefix)
    let closed = 0
    if (shouldClose) {
      try {
        const { data } = await mihomoPanelApi.connections(agentId)
        if (epoch !== generation) return
        const targets = ((data.data?.connections || []) as Array<{ id: string; chains: string[] }>)
          .filter(connection => connection.chains.includes(group.name))
        for (let offset = 0; offset < targets.length; offset += 8) {
          if (epoch !== generation) return
          const results = await Promise.allSettled(targets.slice(offset, offset + 8).map(connection => mihomoPanelApi.closeConnection(agentId, connection.id)))
          if (epoch !== generation) return
          closed += results.filter(result => result.status === 'fulfilled').length
        }
        if (closed < targets.length) notify.warning('节点已切换，部分旧连接未能断开')
      } catch {
        if (epoch !== generation) return
        notify.warning('节点已切换，未能读取或断开旧连接')
      }
    }
    notify.success(`${group.name} 已切换到 ${member}`, closed ? `已断开经过该策略组的 ${closed} 条连接` : undefined)
    emit('changed')
  } catch (failure) {
    if (epoch === generation) {
      actionError.value = panelError(failure)
      notify.error('切换失败', actionError.value)
    }
  } finally { if (epoch === generation) pending.value = '' }
}

async function unfix() {
  const group = current.value
  if (!group?.fixed || !canSwitch(group) || group.type === 'Selector' || pending.value || loading.value || error.value) return
  const epoch = generation
  const agentId = props.agentId
  pending.value = '正在恢复自动选择…'
  actionError.value = ''
  try {
    await mihomoPanelApi.unfixProxy(agentId, group.name)
    if (epoch !== generation) return
    proxies.value[group.name] = { ...group, fixed: undefined }
    await load()
    if (epoch !== generation) return
    notify.success(`${group.name} 已恢复自动选择`)
    emit('changed')
  } catch (failure) {
    if (epoch === generation) {
      actionError.value = panelError(failure)
      notify.error('恢复自动选择失败', actionError.value)
    }
  } finally { if (epoch === generation) pending.value = '' }
}

watch([() => props.agentId, () => props.row.id], () => {
  generation++; request++
  proxies.value = {}; path.value = []; activeIndex.value = 0
  pending.value = ''; actionError.value = ''; providerWarning.value = ''; pathWarning.value = ''
  closeOnSwitch.value = loadProxySettings().closeConnectionsOnSwitch
  void load()
}, { immediate: true })
onUnmounted(() => { generation++; request++ })
</script>
