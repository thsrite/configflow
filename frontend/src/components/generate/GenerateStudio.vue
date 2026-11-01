<template>
  <SectionCard :padded="false" class="mb-5 grid min-w-0 max-md:mb-3 md:grid-cols-[152px_minmax(0,1fr)]" aria-label="配置生成工作台">
    <aside class="min-w-0 border-b border-border bg-background/30 md:border-r md:border-b-0">
      <div class="hidden p-2 md:block">
        <h2 class="m-0 px-3 pt-2 pb-3 text-xs font-medium text-muted-foreground">生成软件</h2>
        <nav class="flex flex-col gap-1" aria-label="生成软件">
          <Button
            v-for="target in targets"
            :key="target.key"
            variant="ghost"
            :class="cn('min-h-11 w-full justify-start gap-2 px-3', active === target.key && 'bg-primary-soft text-primary-accent hover:bg-primary-soft hover:text-primary-accent')"
            :aria-pressed="active === target.key"
            :disabled="running || pushing"
            @click="active = target.key"
          >
            <component :is="target.icon" class="size-4 shrink-0" aria-hidden="true" />
            {{ target.title }}
            <ChevronRight v-if="active === target.key" class="ml-auto size-3.5 shrink-0" aria-hidden="true" />
          </Button>
        </nav>
      </div>
      <div class="flex items-center gap-2 p-3 md:hidden">
        <label for="generate-software" class="shrink-0 text-xs font-medium text-muted-foreground">生成软件</label>
        <Select v-model="active" :disabled="running || pushing">
          <SelectTrigger id="generate-software" class="min-h-11 min-w-0 flex-1 bg-card" aria-label="生成软件">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="target in targets" :key="target.key" :value="target.key" class="min-h-11">
              <span class="flex items-center gap-2">
                <component :is="target.icon" class="size-4 shrink-0" aria-hidden="true" />
                {{ target.title }}
              </span>
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </aside>

    <div class="@container/studio min-w-0">
      <section v-if="current" class="border-b border-border" :aria-label="`${current.title} 配置编辑`">
        <div class="flex flex-wrap items-center justify-between gap-x-5 gap-y-3 px-5 py-4 max-md:gap-2 max-md:p-3">
          <div class="min-w-0">
            <h2 class="m-0 flex items-center gap-2 text-sm font-semibold">
              <component :is="current.icon" class="size-4 text-primary-accent" aria-hidden="true" />
              {{ current.title }} 配置
              <span v-if="isPhone" class="cf-compact-help"><FieldHelp :label="`${current.title} 配置`" :text="current.desc" /></span>
            </h2>
            <p v-if="!isPhone" class="mt-1 mb-0 text-xs text-muted-foreground">{{ current.desc }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2" role="group" :aria-label="`${current.title} 配置操作`">
            <Button
              v-for="action in settingsActions"
              :key="action.label"
              variant="outline"
              class="min-h-11 border-border-strong bg-primary-soft text-primary-accent hover:bg-accent hover:text-foreground"
              :disabled="action.loading || running || pushing"
              @click="action.run"
            >
              <Loader2 v-if="action.loading" class="size-4 animate-spin" />
              <component :is="action.icon" v-else class="size-4" />
              {{ action.label }}
            </Button>
          </div>
        </div>
      </section>

      <div class="space-y-2 border-b border-border px-5 py-3 max-md:px-3">
        <div class="flex flex-wrap items-center gap-2">
          <label for="generate-destination" class="text-xs font-medium">配置用途</label>
          <Select :model-value="destination || undefined" :disabled="running || pushing" @update:model-value="value => chooseDestination(String(value))">
            <SelectTrigger id="generate-destination" class="min-h-11 min-w-0 w-full sm:w-80" aria-label="配置用途"><SelectValue :placeholder="live.loaded.value || active === 'loon' ? '请选择目标设备或手动导出' : '正在读取设备…'" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="manual" class="min-h-11">手动导出</SelectItem>
              <SelectItem v-for="agent in availableAgents" :key="agent.id" :value="agent.id" class="min-h-11" :disabled="!live.isOnline(agent)">{{ agent.name }}{{ live.isOnline(agent) ? '' : '（离线）' }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <p class="m-0 text-xs text-muted-foreground" aria-live="polite">{{ generationBlock || (destination === 'manual' ? '预览和下载使用同一次生成结果。向 Agent 发布时，请先选择目标设备。' : '先生成并检查最终配置，再原样推送。设备适配和配套文件都在生成时完成。') }}</p>
        <p v-if="generation?.target_ids.length && generation.target_ids.length > 1" class="m-0 text-xs text-warning-accent">主备发布将同步更新：{{ generation.target_ids.map(id => live.boundAgents.value.find(a => a.id === id)?.name || id).join('、') }}</p>
        <p v-if="staleMessage" role="alert" class="m-0 text-xs text-warning-accent">{{ staleMessage }}</p>
      </div>

      <div class="grid grid-cols-[minmax(0,1fr)] @min-[720px]/studio:grid-cols-[260px_minmax(0,1fr)]">
        <section class="order-2 min-w-0 border-t border-border p-5 max-md:p-3 @min-[720px]/studio:order-none @min-[720px]/studio:border-t-0 @min-[720px]/studio:border-r" role="list" aria-label="生成步骤">
          <div
            v-for="(step, i) in steps"
            :key="step.title"
            role="listitem"
            :class="cn('gen-step relative grid grid-cols-[26px_1fr] gap-3 pb-5 max-md:gap-2 max-md:pb-2 last:pb-0', stepState(i))"
          >
            <span
              class="gen-bullet grid size-[26px] place-items-center rounded-full border-[1.5px] border-border-strong bg-card font-mono text-[11px] text-muted-foreground transition-all duration-300"
            >
              <Check v-if="stepState(i) === 'done'" class="size-3.5" :stroke-width="2.5" />
              <X v-else-if="stepState(i) === 'fail'" class="size-3.5" :stroke-width="2.5" />
              <template v-else>{{ i + 1 }}</template>
            </span>
            <div class="min-w-0">
              <b class="mt-[3px] block text-[13px] font-semibold">{{ step.title }}</b>
              <span class="block truncate font-mono text-[12px] text-muted-foreground">{{ step.meta }}</span>
            </div>
          </div>
        </section>

        <section class="min-w-0 overflow-hidden" aria-label="生成结果">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-border px-5 py-3 max-md:gap-2 max-md:px-3 max-md:py-2" role="group" aria-label="生成和导出操作">
            <div class="flex min-w-max flex-1 items-center gap-2.5 max-sm:min-w-0">
              <span class="flex shrink-0 gap-1.5 max-sm:hidden" aria-hidden="true">
                <i class="size-2.5 rounded-full bg-accent" /><i class="size-2.5 rounded-full bg-accent" /><i class="size-2.5 rounded-full bg-accent" />
              </span>
              <span class="truncate font-mono text-[12px] text-muted-foreground" :title="fileName">{{ fileName }}</span>
              <span v-if="lines.length && !running" class="whitespace-nowrap font-mono text-[11px] text-muted-foreground">· {{ lines.length }} 行</span>
            </div>
            <Button v-if="isPhone" size="sm" class="min-h-11 shadow-glow" :disabled="running || pushing || !!generationBlock" @click="generate">
              <Loader2 v-if="running" class="size-3.5 animate-spin" />
              <Play v-else class="size-3.5" />
              生成
            </Button>
            <div class="flex flex-wrap items-center gap-1.5 max-sm:w-full max-sm:gap-1 max-sm:[&>button]:flex-1 max-sm:[&>button]:px-1 max-sm:[&>button]:text-[13px]">
              <Button variant="ghost" size="sm" class="min-h-11" :disabled="!output || running" @click="copyOutput">
                <Copy v-if="!isPhone" class="size-3.5" />
                复制
              </Button>
              <Button v-if="generation" variant="outline" size="sm" class="min-h-11" :disabled="running || downloading || fileLoading" @click="downloadGenerated">
                <Loader2 v-if="downloading" class="size-3.5 animate-spin" />
                <Download v-else-if="!isPhone" class="size-3.5" />下载
              </Button>
              <Button
                v-for="action in exportActions"
                :key="action.label"
                variant="outline"
                size="sm"
                class="min-h-11"
                :aria-busy="action.loading || undefined"
                :disabled="action.loading"
                @click="action.run"
              >
                <Loader2 v-if="action.loading" class="size-3.5 animate-spin" />
                <component :is="action.icon" v-else-if="!isPhone" class="size-3.5" />
                {{ action.label }}
              </Button>
              <Button
                variant="outline"
                size="sm"
                class="min-h-11"
                :disabled="!generation || !!staleMessage || running || pushing || deploying || !pushTargets.length"
                :title="pushTitle"
                @click="pushToAgents"
              >
                <Loader2 v-if="pushing || deploying" class="size-3.5 animate-spin" />
                <Send v-else-if="!isPhone" class="size-3.5" />
                推送
              </Button>
              <Button v-if="!isPhone" size="sm" class="min-h-11 shadow-glow" :disabled="running || pushing || !!generationBlock" @click="generate">
                <Loader2 v-if="running" class="size-3.5 animate-spin" />
                <Play v-else class="size-3.5" />
                生成
              </Button>
            </div>
          </div>

          <div v-if="generation" class="flex flex-wrap items-center gap-2 border-b border-border px-5 py-2 max-md:px-3">
            <label for="generated-file" class="text-xs text-muted-foreground">文件（{{ generation.files.length }}）</label>
            <Select :model-value="selectedFile" :disabled="running || fileLoading" @update:model-value="value => showFile(String(value))">
              <SelectTrigger id="generated-file" class="min-h-11 min-w-0 flex-1 font-mono text-xs" aria-label="预览文件"><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem v-for="file in generation.files" :key="file.path" :value="file.path" class="min-h-11 font-mono text-xs">{{ file.path }} · {{ file.size }} B</SelectItem></SelectContent>
            </Select>
            <span class="text-xs text-muted-foreground">{{ destination === 'manual' ? '本次导出' : '最终下发文件' }}</span>
          </div>

          <SubscriptionHostsSummary v-if="hostsReport && !running" :report="hostsReport" />
          <p v-if="previewLimited" class="m-0 border-b border-border px-5 py-2 text-xs text-muted-foreground">文件较长，仅显示前 2000 行；复制和下载包含完整内容。</p>

          <div v-if="pushResults.length" class="min-w-0 border-b border-border px-5 py-2 max-sm:px-3" aria-label="配置发布结果">
            <div class="min-w-0 divide-y divide-border/70">
              <div v-for="result in pushResults" :key="result.id" class="min-w-0" :data-publish-agent="result.id">
                <AgentDeploymentStatus v-if="result.deployment" :task="result.deployment" :label="result.name" compact>
                  <template v-if="deploymentNeedsNotice(result.deployment)" #notice>
                    <AgentOperationSteps :task="result.deployment" :show-current="false" :show-steps="false" />
                    <p v-if="result.deployment.not_recorded && !result.deployment.manual_rollback" class="m-0 text-warning-accent">后端暂未记录此发布，正在继续查询。可前往 Agent 页面重试同一次发布。</p>
                    <p v-if="result.deployment.status === 'ready'" class="m-0 text-warning-accent">请前往 Agent 页面应用并启动。</p>
                    <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="result.deployment" />
                  </template>
                  <template #details>
                    <AgentOperationSteps :task="result.deployment" :show-current="false" :show-message="!deploymentNeedsNotice(result.deployment)" embedded />
                  </template>
                </AgentDeploymentStatus>
                <AgentStatusRow v-else :label="result.name" status="失败" tone="danger">
                  <template #notice>
                    <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="result.task" />
                  </template>
                </AgentStatusRow>
                <AgentDNSChecks :key="result.task.deployment_id" class="ml-6" :checks="result.task.dns_checks" :checked-at="result.task.dns_checked_at" compact />
              </div>
            </div>
            <div class="flex justify-end border-t border-border/70">
              <RouterLink class="inline-flex min-h-11 items-center gap-1 rounded-md text-xs text-primary-accent underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" to="/agents">前往 Agent 查看发布记录<ChevronRight class="size-3.5" aria-hidden="true" /></RouterLink>
            </div>
          </div>
          <div class="relative">
            <div v-if="running && phase === 'steps'" class="absolute inset-0 z-10 flex items-center justify-center gap-2 text-sm text-muted-foreground" role="status">
              <Loader2 class="size-4 animate-spin" aria-hidden="true" />正在生成最终配置…
            </div>
            <pre
              ref="codeEl"
              class="gen-code m-0 h-[520px] overflow-auto bg-background/70 py-4 font-mono text-[12.5px] leading-[1.7] max-md:py-3"
              :class="lines.length ? 'max-md:h-[60dvh]' : running ? 'max-md:h-40' : 'max-md:hidden'"
              :aria-busy="running"
            ><span
              v-for="(line, i) in lines"
              :key="i"
              class="gen-line block whitespace-pre pr-[18px]"
              :class="i >= freshFrom && 'is-new'"
              v-html="line || ' '"
            /><span v-if="running && phase === 'write'" class="gen-caret" aria-hidden="true" /></pre>

            <div
              v-if="!lines.length && !running"
              class="absolute inset-0 grid place-content-center gap-2.5 bg-background/70 px-4 text-center text-muted-foreground max-md:relative max-md:min-h-40 max-md:py-4"
            >
              <BrandMark class="mx-auto size-11 text-primary" />
              <b class="font-display text-[24px] font-medium text-foreground/80">{{ error ? '生成失败' : generationBlock ? (selectedAgent ? '等待设备就绪' : '选择配置用途') : '准备就绪' }}</b>
              <span class="text-[12.5px] [overflow-wrap:anywhere]">{{ error || generationBlock || `按「生成」开始编译，或 ${metaKey} + Enter` }}</span>
            </div>
          </div>
        </section>
      </div>

      <footer v-if="current" class="grid min-w-0 items-center gap-3 border-t border-border px-5 py-4 max-md:gap-2 max-md:p-3 @min-[720px]/studio:grid-cols-[220px_minmax(0,1fr)] @min-[720px]/studio:gap-5" aria-label="订阅地址">
        <div class="min-w-0">
          <h3 class="m-0 flex items-center gap-2 text-[13px] font-semibold">
            <component :is="current.icon" class="size-4 shrink-0 text-primary-accent" aria-hidden="true" />
            {{ current.title }} 订阅地址
          </h3>
          <p class="mt-1 mb-0 text-[11.5px] text-muted-foreground">客户端可直接订阅此地址。</p>
        </div>
        <div class="flex min-w-0 items-center gap-2">
          <Input
            :model-value="current.urlDisplay"
            readonly
            class="h-11 min-w-0 bg-background/50 font-mono text-[11.5px]"
            :aria-label="`${current.title} 配置 URL`"
          />
          <Button
            variant="outline"
            size="icon"
            class="size-11 shrink-0"
            :title="`复制 ${current.title} 配置 URL`"
            :aria-label="`复制 ${current.title} 配置 URL`"
            @click="emit('copy', current.url, current.title)"
          >
            <Copy class="size-4" />
          </Button>
        </div>
      </footer>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch, type Component } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { RouterLink, useRouter } from 'vue-router'
import { Check, ChevronRight, Copy, Download, Loader2, Play, Send, X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import BrandMark from '@/components/common/BrandMark.vue'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import SectionCard from '@/components/common/SectionCard.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { agentApi, generateApi, ruleSetApi, statsApi, type GeneratedConfig } from '@/api'
import { deploymentNeedsNotice, useAgentDeployments, type AgentDeployment } from '@/composables/useAgentDeployments'
import AgentFailureDetails from '@/components/agents/AgentFailureDetails.vue'
import AgentOperationSteps from '@/components/agents/AgentOperationSteps.vue'
import AgentDeploymentStatus from '@/components/agents/AgentDeploymentStatus.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import AgentDNSChecks from '@/components/agents/AgentDNSChecks.vue'
import SubscriptionHostsSummary from '@/components/generate/SubscriptionHostsSummary.vue'
import { submitAgentRequests, uniquePublicationTargets } from '@/lib/agentRequests'
import { deploymentErrorSummary } from '@/lib/agentFailure'
import { consumeAction } from '@/lib/actions'
import { notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { cn } from '@/lib/utils'
import { useLive } from '@/stores/live'
import { usePreferences } from '@/stores/preferences'
import type { SubscriptionHostsReport } from '@/types'

const isPhone = useMediaQuery('(max-width: 639px)')

type TargetKey = 'mihomo' | 'surge' | 'loon' | 'mosdns' | 'oxidns'

export interface StudioTarget {
  key: TargetKey
  title: string
  desc: string
  icon: Component
  url: string
  urlDisplay: string
  actions: Array<{ label: string; icon: Component; run: () => void; loading?: boolean; group?: 'settings' | 'export' }>
}

const props = defineProps<{ targets: StudioTarget[]; profileId: string; loonFileName?: string }>()
const emit = defineEmits<{ (e: 'copy', url: string, title: string): void }>()

const router = useRouter()
const live = useLive()
const { motionEnabled } = usePreferences()
const metaKey = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'

const active = ref<TargetKey>('mihomo')
const current = computed(() => props.targets.find(t => t.key === active.value))
const settingsActions = computed(() => current.value?.actions.filter(action => action.group === 'settings') ?? [])
const exportActions = computed(() => current.value?.actions.filter(action => action.group !== 'settings' && action.label !== '下载') ?? [])
const destination = ref('')
const availableAgents = computed(() => live.boundAgents.value.filter(a => a.service_type === active.value
  && a.enabled !== false && (!a.profile_id || a.profile_id === props.profileId)))
const selectedAgent = computed(() => availableAgents.value.find(a => a.id === destination.value))
const generationBlock = computed(() => {
  if (destination.value === 'manual') return ''
  if (!live.loaded.value) return '正在读取设备，请稍候；也可以主动选择手动导出。'
  if (!destination.value || !selectedAgent.value) return '请先选择目标设备或手动导出，再生成配置。'
  return live.isOnline(selectedAgent.value) ? '' : '目标设备离线，暂时无法生成最终配置；请等待上线或选择手动导出。'
})
const destinationKey = computed(() => `configflow-generate-agent:${encodeURIComponent(props.profileId)}:${active.value}`)
const rememberedAgents = new Map<string, string>()
let automaticSelection = true
let destinationContext = ''
const rememberAgent = (id: string) => {
  if (!id || id === 'manual') return
  rememberedAgents.set(destinationKey.value, id)
  try { localStorage.setItem(destinationKey.value, id) } catch { /* 本次访问仍记住选择。 */ }
}
const chooseDestination = (value: string) => {
  automaticSelection = false
  destination.value = value
  rememberAgent(value)
}
const generation = ref<GeneratedConfig | null>(null)
const selectedFile = ref('')
const staleMessage = ref('')
const fileLoading = ref(false)
const previewLimited = ref(false)
const downloading = ref(false)
let requestSequence = 0
let finishFrame: (() => void) | undefined
const nextFrame = () => new Promise<void>(resolve => {
  const frame = requestAnimationFrame(() => { finishFrame = undefined; resolve() })
  finishFrame = () => { cancelAnimationFrame(frame); finishFrame = undefined; resolve() }
})

const FILES: Record<TargetKey, string> = { mihomo: 'config.yaml', surge: 'surge.conf', loon: 'loon.conf', mosdns: 'mosdns.yaml', oxidns: 'oxidns.yaml' }
const fileName = computed(() => selectedFile.value || FILES[active.value])

/* ---------- 步骤（计数取真实数据） ---------- */
const counts = ref({ subscriptions: 0, nodes: 0, proxyGroups: 0, rules: 0, ruleSets: 0 })

const steps = computed(() => {
  const c = counts.value
  if (active.value === 'mosdns' || active.value === 'oxidns') {
    return [
      { title: '收集资源', meta: `${c.ruleSets} 规则集 · ${c.rules} 条规则` },
      { title: '读取分流规则', meta: '直连 / 代理规则集' },
      { title: '组装 DNS 上游', meta: '国内 / 国外 / Fallback' },
      { title: '编译分流序列', meta: '缓存 · Hosts · 默认转发' },
      { title: '写出配置', meta: fileName.value }
    ]
  }
  return [
    { title: '收集资源', meta: `${c.subscriptions} 订阅 · ${c.nodes} 节点` },
    { title: '解析节点', meta: 'Sub-Store 转换' },
    { title: '组装策略组', meta: `${c.proxyGroups} 组 · 正则筛选` },
    { title: '编译规则', meta: `${c.rules} 条 · ${c.ruleSets} 规则集` },
    { title: '写出配置', meta: fileName.value }
  ]
})

/** -1 未开始；0..4 进行中；5 全部完成 */
const stepIndex = ref(-1)
const failedAt = ref(-1)
const stepState = (i: number) => {
  if (failedAt.value === i) return 'fail'
  if (i < stepIndex.value) return 'done'
  if (i === stepIndex.value) return 'run'
  return ''
}

/* ---------- 生成 ---------- */
const running = ref(false)
const phase = ref<'steps' | 'write' | ''>('')
const output = ref('')
const lines = ref<string[]>([])
const freshFrom = ref(0)
const error = ref('')
const codeEl = ref<HTMLElement | null>(null)
/** Mihomo only: hosts merged from subscriptions; hidden when no subscription carries any. */
const hostsReport = ref<SubscriptionHostsReport | null>(null)

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

/** 轻量高亮：YAML 键 / 字符串 / 数字与布尔 / 注释；Surge / Loon 的 [段] 与 key = value */
const highlight = (raw: string): string => {
  if (/^\s*#/.test(raw) || /^\s*\/\//.test(raw)) return `<span class="y-c">${esc(raw)}</span>`
  if (/^\[.*\]\s*$/.test(raw)) return `<span class="y-k">${esc(raw)}</span>`
  let s = esc(raw)
  s = s.replace(/(&quot;|")([^"]*)\1|'([^']*)'/g, m => `<span class="y-s">${m}</span>`)
  s = s.replace(/^(\s*-?\s*)([\w.-]+)(:)(?=\s|$)/, '$1<span class="y-k">$2</span><span class="y-d">$3</span>')
  s = s.replace(/^([\w .-]+?)(\s=\s)/, '<span class="y-k">$1</span><span class="y-d">$2</span>')
  s = s.replace(/(:\s|=\s|,\s?)(-?\d+(?:\.\d+)?|true|false|null)(?=\s*(?:,|$|\}))/g, '$1<span class="y-n">$2</span>')
  s = s.replace(/\b(DIRECT|REJECT|MATCH|FINAL)\b/g, '<span class="y-n">$1</span>')
  return s
}

const loadCounts = async () => {
  const [stats, ruleSets] = await Promise.allSettled([statsApi.getOverview(), ruleSetApi.getAll()])
  if (stats.status === 'fulfilled' && stats.value.data?.success) {
    const d = stats.value.data.data
    counts.value = {
      ...counts.value,
      subscriptions: d.subscriptions?.total ?? 0,
      nodes: d.nodes?.total ?? 0,
      proxyGroups: d.proxyGroups?.total ?? 0,
      rules: d.rules?.total ?? 0
    }
  }
  if (ruleSets.status === 'fulfilled') counts.value = { ...counts.value, ruleSets: (ruleSets.value.data || []).length }
}

const generate = async () => {
  if (running.value || pushing.value) return
  if (generationBlock.value) {
    notify.info(generationBlock.value)
    return
  }
  automaticSelection = false
  const sequence = ++requestSequence
  const profile = props.profileId
  const device = destination.value
  running.value = true
  phase.value = 'steps'
  error.value = ''
  failedAt.value = -1
  lines.value = []
  output.value = ''
  previewLimited.value = false
  generation.value = null
  selectedFile.value = ''
  staleMessage.value = ''
  hostsReport.value = null
  freshFrom.value = 0
  const target = active.value
  const request = (device === 'manual' ? generateApi.artifact(target, profile) : agentApi.generateConfig(device, profile, target))
    .then(res => ({ ok: true as const, artifact: res.data, content: res.data.content,
      hosts: (res.data?.subscription_hosts ?? null) as SubscriptionHostsReport | null }))
    .catch((err: any) => ({ ok: false as const, message: err.response?.data?.message || '生成失败' }))
  loadCounts()

  try {
    stepIndex.value = -1
    const result = await request
    if (sequence !== requestSequence || props.profileId !== profile || destination.value !== device || active.value !== target) return
    if (!result.ok) {
      error.value = result.message
      notify.error(`${FILES[target]} 生成失败`, result.message)
      return
    }

    stepIndex.value = 4
    phase.value = 'write'
    output.value = result.content
    generation.value = result.artifact
    selectedFile.value = result.artifact.config_path
    const all = result.content.replace(/\n$/, '').split('\n')
    previewLimited.value = all.length > 2000
    const preview = all.slice(0, 2000).map(highlight)
    if (motionEnabled() && !document.hidden) {
      // Only animate rendering the fixed result, after generation has completed.
      const perFrame = Math.max(1, Math.ceil(preview.length / 90))
      for (let i = 0; i < preview.length; i += perFrame) {
        if (sequence !== requestSequence) return
        if (!motionEnabled() || document.hidden) { lines.value = preview; break }
        freshFrom.value = lines.value.length
        lines.value.push(...preview.slice(i, i + perFrame))
        await nextFrame()
        if (sequence !== requestSequence) return
        if (codeEl.value) codeEl.value.scrollTop = codeEl.value.scrollHeight
      }
    } else lines.value = preview
    stepIndex.value = 5
    freshFrom.value = lines.value.length
    const hosts = result.hosts
    hostsReport.value = target === 'mihomo' && hosts && (hosts.merged || hosts.ignored) ? hosts : null
    await nextTick()
    if (codeEl.value) codeEl.value.scrollTop = 0
    notify.success(`${FILES[target]} 已生成 · ${all.length} 行`)
  } finally {
    if (sequence === requestSequence) { running.value = false; phase.value = '' }
  }
}

const showFile = async (path: string) => {
  const snapshot = generation.value
  if (!snapshot) return
  fileLoading.value = true
  selectedFile.value = path
  output.value = ''
  lines.value = []
  previewLimited.value = false
  try {
    let content = snapshot.content
    if (path !== snapshot.config_path) {
      const { data } = snapshot.agent_id === 'manual'
        ? await generateApi.artifactFile(snapshot.id, path)
        : await agentApi.generatedFile(snapshot.agent_id, snapshot.id, path)
      try { content = new TextDecoder('utf-8', { fatal: true }).decode(data); if (content.includes('\0')) throw new Error('binary') }
      catch {
        if (generation.value?.id === snapshot.id && selectedFile.value === path) lines.value = ['此文件为二进制内容，请下载完整生成包查看。']
        return
      }
    }
    if (generation.value?.id !== snapshot.id || selectedFile.value !== path) return
    output.value = content
    const all = content.replace(/\n$/, '').split('\n')
    previewLimited.value = all.length > 2000
    lines.value = all.slice(0, 2000).map(highlight)
  } catch (err: any) { notify.error('读取生成文件失败', err.response?.data?.message || '请重新生成后重试。') }
  finally { fileLoading.value = false }
}

const downloadGenerated = async () => {
  const snapshot = generation.value
  if (!snapshot) return
  downloading.value = true
  try {
    // Single text files can be downloaded directly from the exact displayed artifact.
    const blob = snapshot.files.length === 1
      ? new Blob([snapshot.content], { type: 'text/plain;charset=utf-8' })
      : (snapshot.agent_id === 'manual' ? await generateApi.artifactDownload(snapshot.id)
        : await agentApi.generatedDownload(snapshot.agent_id, snapshot.id)).data
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    const manualName = snapshot.service_type === 'mihomo' ? 'mihomo.yaml'
      : snapshot.service_type === 'loon' ? props.loonFileName || 'loon.lcf' : snapshot.config_path
    link.download = snapshot.files.length === 1 ? (snapshot.agent_id === 'manual' ? manualName : snapshot.config_path)
      : `${snapshot.service_type}-${snapshot.agent_id === 'manual' ? 'config' : 'final-config'}.zip`
    link.click()
    URL.revokeObjectURL(url)
  } catch { notify.error('下载失败', '生成结果可能已过期，请重新生成后重试。') }
  finally { downloading.value = false }
}

const copyOutput = async () => {
  try {
    await copyText(output.value)
    notify.success('已复制到剪贴板')
  } catch {
    notify.error('复制失败，请在面板中手动选择')
  }
}

/* ---------- 推送：当前配置空间里同类型且在线的 Agent ---------- */
const pushing = ref(false)
const submissionFailures = ref<Record<string, Partial<AgentDeployment>>>({})
const pushedAgents = ref<Record<string, { name: string; serviceType: string }>>({})
const pushTargets = computed(() =>
  active.value === 'loon'
    ? []
    : live.boundAgents.value.filter(a => a.id === destination.value && a.service_type === active.value && live.isOnline(a))
)
const pushTitle = computed(() => {
  if (active.value === 'loon') return 'Loon 没有对应的 Agent'
  if (!pushTargets.value.length) return `没有在线的 ${current.value?.title} Agent`
  return `推送到 ${pushTargets.value.map(a => a.name).join('、')}`
})

// push-config 只是提交事务发布（202），必须轮询到终态；否则服务端任务停在中间状态，
// 下一次推送会因「未完成的发布」被 409 拒绝。
const { deployments, busy: deploymentBusy, track: trackDeployment, beginSubmission, endSubmission } = useAgentDeployments(() => live.refresh())
const deploying = computed(() => pushTargets.value.some(a => deploymentBusy(a.id)))
const pushResults = computed(() => Object.entries(pushedAgents.value).flatMap(([id, agent]) => {
  const failure = submissionFailures.value[id]
  const deployment = failure ? undefined : deployments[id]
  const task = failure || deployment
  return agent.serviceType === active.value && live.boundAgents.value.some(bound => bound.id === id) && task
    ? [{ id, name: agent.name, task, deployment }] : []
}))
// 「已校验，待激活」需要在 Agent 页手动激活，这里没有激活入口，只给出指引。
watch(() => Object.entries(deployments).filter(([, task]) => task.status === 'ready').map(([id]) => id).join(','), ids => {
  if (!ids) return
  notify.info('配置已校验，等待激活', '请到 Agent 页面激活本次发布')
})

type PushOutcome = 'submitted' | 'done' | 'failed' | 'busy'

const pushOne = async (agent: { id: string; name: string }): Promise<PushOutcome> => {
  delete submissionFailures.value[agent.id]
  pushedAgents.value[agent.id] = { name: agent.name, serviceType: active.value }
  const snapshot = generation.value
  if (!snapshot || snapshot.agent_id !== agent.id || staleMessage.value) return 'failed'
  const deploymentId = snapshot.deployment_id
  const transactional = active.value !== 'surge'
  if (transactional && !beginSubmission(agent.id, deploymentId)) return 'busy'
  try {
    const { data } = await agentApi.pushConfig(agent.id, deploymentId, { generation_id: snapshot.id })
    if (data.deployment_id === deploymentId && typeof data.status === 'string') {
      endSubmission(agent.id, deploymentId, data)
      return 'submitted'
    }
    if (transactional) {
      endSubmission(agent.id, deploymentId, 'unknown')
      return 'submitted'
    }
    if (data?.success === false || data?.restart?.success === false) {
      submissionFailures.value[agent.id] = { ...data, error: data.restart?.success === false ? `配置已写入，但重启失败：${data.restart.message || '服务未确认启动。'}` : data.error || data.message, error_details: data.restart?.error_details || data.error_details }
      notify.error(`「${agent.name}」配置发布失败`, deploymentErrorSummary(submissionFailures.value[agent.id]))
      return 'failed'
    }
    return 'done'
  } catch (error: any) {
    if (error.response?.data?.code === 'preview_changed') {
      staleMessage.value = error.response.data.message
    }
    // 409 携带的未完成任务同样跟踪到终态，旧任务收尾后即可再次推送。
    if (error.response?.data?.deployment_id) {
      if (error.response.data.deployment_id === deploymentId) {
        endSubmission(agent.id, deploymentId, error.response.data)
        return 'submitted'
      }
      endSubmission(agent.id, deploymentId, 'rejected')
      trackDeployment(agent.id, error.response.data)
      if (error.response.status === 409) notify.warning(`「${agent.name}」上一次发布尚未结束，正在等待其完成后再推送`)
      return 'submitted'
    }
    if (transactional && (!error.response || error.response.status >= 500)) {
      endSubmission(agent.id, deploymentId, 'unknown')
      return 'submitted'
    }
    if (transactional && endSubmission(agent.id, deploymentId, 'rejected')) return 'submitted'
    submissionFailures.value[agent.id] = { ...error.response?.data, error: error.response?.data?.error || error.response?.data?.message || '发布请求失败，请重试。' }
    notify.error(`「${agent.name}」配置发布失败`, deploymentErrorSummary(submissionFailures.value[agent.id]))
    return 'failed'
  }
}

const pushToAgents = async () => {
  const targets = uniquePublicationTargets(pushTargets.value.filter(a => !deploymentBusy(a.id)))
  if (!targets.length || pushing.value || !generation.value || staleMessage.value) return
  pushing.value = true
  try {
    const outcomes = await submitAgentRequests(targets, pushOne)
    const done = outcomes.filter(o => o === 'done').length
    if (done) notify.success(`已推送到 ${done} 台 Agent`)
    // submitted 的结果由 useAgentDeployments 轮询到终态后提示。
  } finally {
    pushing.value = false
    live.refresh()
  }
}

/* ---------- 快捷键与跨页动作 ---------- */
const onKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
    event.preventDefault()
    generate()
  }
}

const pendingGenerate = ref('')
const runPending = () => {
  if (consumeAction(router, 'generate-mihomo')) {
    active.value = 'mihomo'
    pendingGenerate.value = `configflow-generate-agent:${encodeURIComponent(props.profileId)}:mihomo`
  }
}

const clearGenerated = () => {
  requestSequence++
  finishFrame?.()
  running.value = false
  phase.value = ''
  generation.value = null
  previewLimited.value = false
  selectedFile.value = ''
  staleMessage.value = ''
  lines.value = []
  output.value = ''
  hostsReport.value = null
  stepIndex.value = -1
  failedAt.value = -1
  error.value = ''
}
watch([destinationKey, () => live.loaded.value, () => availableAgents.value.map(a => a.id).sort().join('|')], () => {
  if (destinationContext !== destinationKey.value) {
    destinationContext = destinationKey.value
    destination.value = ''
    automaticSelection = true
    clearGenerated()
  }
  if (!live.loaded.value && active.value !== 'loon') return
  if (!automaticSelection) {
    if (destination.value && destination.value !== 'manual' && !selectedAgent.value) {
      destination.value = ''
    }
    return
  }
  let remembered = rememberedAgents.get(destinationKey.value)
  try { remembered = localStorage.getItem(destinationKey.value) || remembered } catch { /* 使用本次访问的选择。 */ }
  const agents = availableAgents.value
  const choice = agents.length === 0 ? 'manual' : agents.length === 1 ? agents[0].id
    : agents.some(a => a.id === remembered) ? remembered! : ''
  destination.value = choice
  if (choice && choice !== 'manual') {
    automaticSelection = false
    rememberAgent(choice)
  }
}, { immediate: true })
watch(destination, clearGenerated, { flush: 'sync' })
watch(() => availableAgents.value.map(a => `${a.id}:${a.profile_id}:${a.service_type}:${a.host}:${a.port}:${a.version}`).join('|'), () => {
  if (generation.value && destination.value !== 'manual') {
    if (!availableAgents.value.some(a => a.id === generation.value?.agent_id)) staleMessage.value = '目标设备已不可用，请重新选择并生成。'
  }
})

watch(() => router?.currentRoute.value.query.run, run => run === 'generate-mihomo' && runPending())
watch([pendingGenerate, destinationKey, generationBlock], () => {
  if (!pendingGenerate.value) return
  if (pendingGenerate.value !== destinationKey.value) { pendingGenerate.value = ''; return }
  if (!generationBlock.value) { pendingGenerate.value = ''; generate() }
}, { flush: 'post' })
const finishHiddenAnimation = () => { if (document.hidden) finishFrame?.() }

onMounted(() => {
  live.start()
  loadCounts()
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('visibilitychange', finishHiddenAnimation)
  runPending()
})

onUnmounted(() => {
  requestSequence++
  finishFrame?.()
  live.stop()
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('visibilitychange', finishHiddenAnimation)
})
</script>

<style scoped>
.gen-step::before {
  content: '';
  position: absolute;
  top: 26px;
  bottom: 2px;
  left: 12px;
  width: 2px;
  background: var(--border-strong);
}

.gen-step::after {
  content: '';
  position: absolute;
  top: 26px;
  left: 12px;
  width: 2px;
  height: 0;
  background: var(--success-accent);
  transition: height 0.5s var(--ease-flow);
}

.gen-step:last-child::before,
.gen-step:last-child::after {
  display: none;
}

.gen-step.done::after {
  height: calc(100% - 28px);
}

.gen-step.run .gen-bullet {
  border-color: var(--primary);
  color: var(--primary-accent);
  box-shadow: 0 0 0 5px var(--primary-soft);
}

.gen-step.done .gen-bullet {
  border-color: var(--success-accent);
  background: var(--success-soft);
  color: var(--success-accent);
}

.gen-step.fail .gen-bullet {
  border-color: var(--destructive);
  background: var(--destructive);
  color: var(--destructive-foreground);
}

.gen-code {
  counter-reset: ln;
}

.gen-line::before {
  counter-increment: ln;
  content: counter(ln);
  display: inline-block;
  width: 46px;
  padding-right: 14px;
  text-align: right;
  color: var(--muted-foreground);
  opacity: 0.6;
}

.gen-line.is-new {
  animation: line-in 0.35s var(--ease-flow);
}

@keyframes line-in {
  from {
    background: var(--primary-soft);
  }
}

.gen-caret {
  display: inline-block;
  width: 8px;
  height: 15px;
  margin-left: 60px;
  vertical-align: -3px;
  background: var(--primary-accent);
  animation: caret-blink 1s steps(1) infinite;
}

@keyframes caret-blink {
  50% {
    opacity: 0;
  }
}

.gen-code :deep(.y-k) {
  color: var(--primary-accent);
}

.gen-code :deep(.y-s) {
  color: var(--success-accent);
}

.gen-code :deep(.y-n) {
  color: var(--info-accent);
}

.gen-code :deep(.y-c) {
  color: var(--muted-foreground);
  font-style: italic;
}

.gen-code :deep(.y-d) {
  color: var(--muted-foreground);
}
</style>
