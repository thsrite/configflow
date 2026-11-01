<template>
  <SectionCard title="延迟" :padded="false" :help="HELP">
    <template #actions>
      <Select :model-value="group" :disabled="!groups.length" @update:model-value="pickGroup(String($event))">
        <SelectTrigger class="min-h-11 w-40 min-w-0 max-sm:w-32" aria-label="海外站点经过的策略组" title="海外站点经过的策略组">
          <SelectValue placeholder="选择策略组"><span class="truncate">{{ group || '选择策略组' }}</span></SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="item in groups" :key="item" :value="item">{{ item }}</SelectItem>
        </SelectContent>
      </Select>
      <Button variant="ghost" class="min-h-11" :disabled="testing || (!group && !groupsError)" :aria-busy="testing" @click="runTests">
        <Loader2 v-if="testing" class="size-3.5 animate-spin" /><Zap v-else class="size-3.5" />测试
      </Button>
    </template>
    <p v-if="groupsError" role="alert" class="mx-5 mt-0 mb-3 break-words text-[12.5px] text-warning-accent max-md:mx-4 max-sm:mx-3">{{ groupsError }}</p>
    <ul class="m-0 list-none border-t border-border p-0">
      <li v-for="target in LATENCY_TARGETS" :key="target.key" class="flex min-h-12 items-center gap-3 border-b border-border px-5 py-2 last:border-b-0 max-md:px-4 max-sm:px-3">
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[13px] font-medium">{{ target.label }}</span>
          <span class="block truncate text-[11.5px] text-muted-foreground" :title="viaText(target)">{{ viaText(target) }}</span>
        </span>
        <span class="flex h-6 w-20 shrink-0 items-end gap-0.5" :aria-label="`最近 ${ROUNDS} 次结果`" role="img">
          <span v-for="(bar, index) in bars(target.key)" :key="index" class="flex-1 rounded-[1px]" :class="bar.class" :style="{ height: bar.height }" :title="bar.title" />
        </span>
        <span class="num w-16 shrink-0 text-right text-[13px]" :class="toneClass(latest(target.key))" :title="failures[target.key] || undefined">
          {{ pending.has(target.key) ? '…' : delayText(latest(target.key)) }}
        </span>
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
import { mihomoPanelApi } from '@/api'
import { LATENCY_TARGETS, type LatencyTarget } from '@/lib/mihomo-overview'
import { delayText, delayTone, orderedGroups, panelError, type MihomoProxy } from '@/types/mihomo'

const HELP = '由 Mihomo 发起测试，反映这台设备的出网质量，而不是你当前浏览器的网络。百度走直连，其余站点走所选策略组当前的出口。打开概览时测一次，之后每分钟自动重测，柱条是最近 5 次结果。'
const ROUNDS = 5
const AUTO_MS = 60_000
const TIMEOUT_MS = 5000

const props = defineProps<{ agentId: string; active: boolean; reloadToken?: number }>()
const storageKey = () => `configflow:mihomo-overview:latency-group:${props.agentId}`

const groups = ref<string[]>([])
const groupsError = ref('')
const group = ref('')
const history = ref<Record<string, number[]>>({})
const failures = ref<Record<string, string>>({})
const pending = ref(new Set<string>())
const testing = computed(() => pending.value.size > 0)

const viaText = (target: LatencyTarget) => target.via === 'direct' ? '直连' : group.value ? `经 ${group.value}` : '未选择策略组'
const latest = (key: string) => {
  const values = history.value[key] || []
  return values.length ? values[values.length - 1] : undefined
}
const toneClass = (delay: number | undefined) => ({
  success: 'text-success-accent', warning: 'text-warning-accent', danger: 'text-destructive-accent', muted: 'text-muted-foreground'
})[delayTone(delay)]
const barClass = (delay: number) => ({
  success: 'bg-success-accent', warning: 'bg-warning-accent', danger: 'bg-destructive-accent', muted: 'bg-muted-foreground'
})[delayTone(delay)]

function bars(key: string) {
  const values = history.value[key] || []
  const ceil = Math.max(1, ...values.filter(value => value > 0))
  return Array.from({ length: ROUNDS }, (_, index) => {
    // 右对齐：最新的结果在最右边
    const value = values[values.length - ROUNDS + index]
    if (value === undefined) return { class: 'bg-border', height: '18%', title: '' }
    if (value <= 0) return { class: 'bg-destructive-accent opacity-40', height: '100%', title: '超时或失败' }
    return { class: barClass(value), height: `${Math.max(18, Math.round(value / ceil * 100))}%`, title: `${value} ms` }
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
  for (const target of LATENCY_TARGETS) if (target.via === 'group') {
    delete history.value[target.key]
    delete failures.value[target.key]
  }
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
async function testOne(target: LatencyTarget) {
  const via = target.via === 'direct' ? 'DIRECT' : group.value
  if (!via) return
  pending.value = new Set(pending.value).add(target.key)
  let delay = 0
  try {
    const { data } = await mihomoPanelApi.proxyDelay(props.agentId, via, target.url, TIMEOUT_MS)
    delay = Number(data.data?.delay) || 0
    delete failures.value[target.key]
  } catch (failure) {
    failures.value[target.key] = panelError(failure, '测试失败')
  } finally {
    const next = new Set(pending.value)
    next.delete(target.key)
    pending.value = next
  }
  // 测试期间换了策略组，丢弃旧出口的结果
  if (disposed || via !== (target.via === 'direct' ? 'DIRECT' : group.value)) return
  history.value = { ...history.value, [target.key]: [...(history.value[target.key] || []), delay].slice(-ROUNDS) }
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
