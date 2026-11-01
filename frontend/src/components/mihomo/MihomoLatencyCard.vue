<template>
  <SectionCard title="延迟" :padded="false" :help="mode === 'mihomo' ? HELP_MIHOMO : HELP_BROWSER">
    <template #actions>
      <Segmented v-model="mode" :options="LATENCY_MODES" label="测试方式" />
      <Select v-if="mode === 'mihomo'" :model-value="group" :disabled="!groups.length" @update:model-value="pickGroup(String($event))">
        <SelectTrigger class="min-h-11 w-40 min-w-0 max-sm:w-32" aria-label="海外站点经过的策略组" title="海外站点经过的策略组">
          <SelectValue placeholder="选择策略组"><span class="truncate">{{ group || '选择策略组' }}</span></SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="item in groups" :key="item" :value="item">{{ item }}</SelectItem>
        </SelectContent>
      </Select>
      <Button variant="ghost" class="min-h-11 max-sm:size-11 max-sm:gap-0" aria-label="测试延迟" :disabled="testing" :aria-busy="testing" @click="runTests">
        <Loader2 v-if="testing" class="size-3.5 animate-spin" /><Zap v-else class="size-3.5" /><span class="max-sm:sr-only">测试</span>
      </Button>
    </template>
    <p v-if="mode === 'mihomo' && groupsError" role="alert" class="mx-5 mt-0 mb-3 break-words text-[12.5px] text-warning-accent max-md:mx-4 max-sm:mx-3">{{ groupsError }}</p>
    <ul class="m-0 grid list-none grid-cols-5 gap-3 px-5 pb-5 max-lg:grid-cols-3 max-md:gap-2 max-md:px-4 max-sm:grid-cols-2 max-sm:px-3 max-sm:pb-3">
      <li v-for="target in LATENCY_TARGETS" :key="target.key" class="flex min-w-0 flex-col gap-2 rounded-xl bg-secondary/60 px-3 py-2.5" data-latency>
        <div class="min-w-0">
          <div class="truncate text-[13px] font-medium">{{ target.label }}</div>
          <div class="truncate text-[11.5px] text-muted-foreground" :title="viaText(target)">{{ viaText(target) }}</div>
        </div>
        <div class="flex items-end justify-between gap-2">
          <span class="num text-[18px] leading-none" :class="toneClass(latest(target.key))" :title="failures[target.key] || undefined">
            {{ pending.has(target.key) ? '…' : delayText(latest(target.key)) }}
          </span>
          <span class="flex h-5 shrink-0 items-end gap-[3px]" :aria-label="`最近 ${ROUNDS} 次结果`" role="img">
            <span v-for="(bar, index) in bars(target.key)" :key="index" class="w-[5px] rounded-[1.5px]" :class="bar.class" :style="{ height: bar.height }" :title="bar.title" />
          </span>
        </div>
      </li>
    </ul>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Loader2, Zap } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import SectionCard from '@/components/common/SectionCard.vue'
import Segmented from '@/components/common/Segmented.vue'
import { mihomoPanelApi } from '@/api'
import { browserLatency, LATENCY_MODES, LATENCY_TARGETS, type LatencyMode, type LatencyTarget } from '@/lib/mihomo-overview'
import { delayText, delayTone, orderedGroups, panelError, type MihomoProxy } from '@/types/mihomo'

const HELP_MIHOMO = '由 Mihomo 发起测试，反映这台设备的出网质量，与你所在的网络无关。百度走直连，其余站点走所选策略组当前的出口。打开概览时测一次，之后每分钟重测，柱条是最近 5 次结果。'
const HELP_BROWSER = '与 zashboard 相同，在你的浏览器里加载各站点的小图标并计时，包含建立连接的时间。反映的是你这台设备访问这些站点的体验，走不走代理取决于这台设备的网络设置。打开概览时测一次，之后每分钟重测，柱条是最近 5 次结果。'
const ROUNDS = 5
const AUTO_MS = 60_000
const TIMEOUT_MS = 5000
const MODE_KEY = 'configflow:mihomo-overview:latency-mode'
// 柱高的最小刻度：低延迟显示为矮柱，结果都很接近时不会全部顶满
const BAR_SCALE_MS = 300

const props = defineProps<{ agentId: string; active: boolean; reloadToken?: number }>()
const storageKey = () => `configflow:mihomo-overview:latency-group:${props.agentId}`

const readMode = (): LatencyMode => {
  try { return window.localStorage.getItem(MODE_KEY) === 'browser' ? 'browser' : 'mihomo' } catch { return 'mihomo' }
}
const mode = ref<LatencyMode>(readMode())
const groups = ref<string[]>([])
const groupsError = ref('')
const group = ref('')
// 两种方式的结果分开保存，切换后各自保留
const results = ref<Record<LatencyMode, Record<string, number[]>>>({ mihomo: {}, browser: {} })
const failures = ref<Record<string, string>>({})
const pending = ref(new Set<string>())
const testing = computed(() => pending.value.size > 0)

const viaText = (target: LatencyTarget) => mode.value === 'browser'
  ? '浏览器直接访问'
  : target.via === 'direct' ? '直连' : group.value ? `经 ${group.value}` : '未选择策略组'
const history = (key: string) => results.value[mode.value][key] || []
const latest = (key: string) => {
  const values = history(key)
  return values.length ? values[values.length - 1] : undefined
}
const toneClass = (delay: number | undefined) => ({
  success: 'text-success-accent', warning: 'text-warning-accent', danger: 'text-destructive-accent', muted: 'text-muted-foreground'
})[delayTone(delay)]
const barClass = (delay: number) => ({
  success: 'bg-success-accent', warning: 'bg-warning-accent', danger: 'bg-destructive-accent', muted: 'bg-muted-foreground'
})[delayTone(delay)]

function bars(key: string) {
  const values = history(key)
  const ceil = Math.max(BAR_SCALE_MS, ...values.filter(value => value > 0))
  return Array.from({ length: ROUNDS }, (_, index) => {
    // 右对齐：最新的结果在最右边
    const value = values[values.length - ROUNDS + index]
    if (value === undefined) return { class: 'bg-border', height: '15%', title: '' }
    if (value <= 0) return { class: 'bg-destructive-accent opacity-40', height: '100%', title: '超时或失败' }
    return { class: barClass(value), height: `${Math.max(15, Math.round(value / ceil * 100))}%`, title: `${value} ms` }
  })
}

function readGroup(): string {
  try { return window.localStorage.getItem(storageKey()) || '' } catch { return '' }
}

function pickGroup(value: string) {
  if (!value || value === group.value) return
  group.value = value
  try { window.localStorage.setItem(storageKey(), value) } catch { /* 只在本次会话内生效 */ }
  // 换了出口，之前的结果不再可比
  const next = { ...results.value.mihomo }
  for (const target of LATENCY_TARGETS) if (target.via === 'group') delete next[target.key]
  results.value = { ...results.value, mihomo: next }
  void runTests()
}

async function loadGroups() {
  try {
    const { data } = await mihomoPanelApi.proxies(props.agentId)
    const proxies: Record<string, MihomoProxy> = data.data?.proxies || {}
    groups.value = orderedGroups(proxies).map(item => item.name).filter(name => name !== 'GLOBAL')
    groupsError.value = groups.value.length ? '' : '没有可用的策略组，只测试直连站点。'
    const saved = readGroup()
    // 默认取配置里第一个策略组，通常就是主要的代理选择组
    group.value = groups.value.includes(saved) ? saved : groups.value[0] || ''
  } catch (failure) {
    groupsError.value = panelError(failure, '暂时读不到策略组，只测试直连站点。')
  }
}

let disposed = false
async function measure(target: LatencyTarget, testMode: LatencyMode): Promise<number | null> {
  if (testMode === 'browser') return browserLatency(target.icon, TIMEOUT_MS)
  const via = target.via === 'direct' ? 'DIRECT' : group.value
  if (!via) return null
  try {
    const { data } = await mihomoPanelApi.proxyDelay(props.agentId, via, target.url, TIMEOUT_MS)
    return Number(data.data?.delay) || 0
  } catch (failure) {
    failures.value[target.key] = panelError(failure, '测试失败')
    return 0
  }
}

async function testOne(target: LatencyTarget) {
  const testMode = mode.value
  const via = target.via === 'direct' ? 'DIRECT' : group.value
  pending.value = new Set(pending.value).add(target.key)
  delete failures.value[target.key]
  let delay: number | null = null
  try {
    delay = await measure(target, testMode)
  } finally {
    const next = new Set(pending.value)
    next.delete(target.key)
    pending.value = next
  }
  // 测试期间换了策略组，丢弃旧出口的结果
  if (disposed || delay === null || (testMode === 'mihomo' && via !== (target.via === 'direct' ? 'DIRECT' : group.value))) return
  const current = results.value[testMode]
  results.value = { ...results.value, [testMode]: { ...current, [target.key]: [...(current[target.key] || []), delay].slice(-ROUNDS) } }
}

async function runTests() {
  if (testing.value) return
  await Promise.all(LATENCY_TARGETS.map(testOne))
}

let timer: ReturnType<typeof setInterval> | null = null
function startAuto() {
  stopAuto()
  timer = setInterval(() => {
    if (document.visibilityState === 'visible') void runTests()
  }, AUTO_MS)
}
function stopAuto() {
  if (timer) clearInterval(timer)
  timer = null
}

let started = false
async function start() {
  if (!started) {
    started = true
    await loadGroups()
    if (disposed) return
  }
  void runTests()
  startAuto()
}

watch(mode, value => {
  try { window.localStorage.setItem(MODE_KEY, value) } catch { /* 只在本次会话内生效 */ }
  failures.value = {}
  // 切到还没测过的方式时立即测一次
  if (props.active && !Object.keys(results.value[value]).length) void runTests()
})
watch(() => props.active, shown => { if (shown) void start(); else stopAuto() })
watch(() => props.reloadToken, async () => {
  if (!props.active) return
  await loadGroups()
  void runTests()
})
onMounted(() => { if (props.active) void start() })
onUnmounted(() => {
  disposed = true
  stopAuto()
})
</script>
