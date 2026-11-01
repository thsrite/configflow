<template>
  <Dialog :open="true" @update:open="open => !open && emit('close')">
    <DialogContent class="flex min-h-0 max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden p-0 max-sm:p-0 sm:max-w-[880px]" data-push-diff>
      <DialogHeader class="shrink-0 border-b border-border px-4 py-4 pr-14 sm:px-5 sm:pr-14">
        <DialogTitle class="break-words">{{ agent.name }} · 待推送的变更</DialogTitle>
        <div class="flex flex-wrap items-center justify-between gap-x-4">
          <DialogDescription class="min-w-0 flex-1 basis-72">对比上次成功推送的发布包与按当前配置将要推送的发布包，只读，不会改动设备。</DialogDescription>
          <!-- 截图分享前打开；每次打开窗口都默认显示原值 -->
          <label v-if="result?.status === 'changed'" for="push-diff-hide"
            class="-my-2.5 flex min-h-11 shrink-0 cursor-pointer items-center gap-2 text-[12.5px] text-foreground" data-push-diff-hide>
            <Switch id="push-diff-hide" v-model="hideSecrets" />隐藏敏感信息
          </label>
        </div>
      </DialogHeader>

      <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 sm:px-5" :aria-busy="loading">
        <div v-if="loading" role="status" data-push-diff-loading>
          <p class="m-0 flex items-center gap-2 text-[13px] text-muted-foreground">
            <Loader2 class="size-4 animate-spin text-primary-accent" aria-hidden="true" />
            正在生成这次将要推送的内容并与上次推送对比…
          </p>
          <LoadingRows :rows="4" class="px-0" />
        </div>

        <div v-else-if="error" role="alert" class="flex flex-col items-center gap-3 px-4 py-10 text-center" data-push-diff-error>
          <div class="flex size-11 items-center justify-center rounded-lg bg-destructive-soft text-destructive-accent">
            <CircleAlert class="size-5" aria-hidden="true" />
          </div>
          <p class="m-0 text-[14px] font-medium">无法读取待推送的变更</p>
          <p class="m-0 max-w-[52ch] break-words text-[13px] leading-relaxed text-muted-foreground">{{ error }}</p>
          <Button variant="outline" size="sm" class="mt-1" @click="load()">
            <RefreshCw class="size-3.5" />
            重试
          </Button>
        </div>

        <EmptyState v-else-if="result?.status === 'synced'" :icon="CircleCheck" title="无变更"
          :description="`当前配置生成的发布包与上次推送${pushedAt ? `（${pushedAt}）` : ''}一致，推送不会改动设备上的文件。`" data-push-diff-synced />

        <EmptyState v-else-if="unavailable" :icon="unavailable.title === '正在推送' ? Loader2 : FileQuestion" :title="unavailable.title"
          :description="unavailable.description" data-push-diff-unavailable />

        <div v-else-if="result?.status === 'changed'" class="flex flex-col gap-5" data-push-diff-changed>
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12px] text-muted-foreground">
            <span v-if="pushedAt">上次推送：{{ pushedAt }}<template v-if="typeof result.baseline?.config_revision === 'number'"> · 修订 <span class="num">v{{ result.baseline.config_revision }}</span></template></span>
            <span v-if="result.checked_at">{{ checkedAgo(result.checked_at, '对比') }}</span>
            <span v-if="result.counts" class="flex flex-wrap gap-1.5" data-push-diff-counts>
              <span v-for="key in COUNT_KEYS" :key="key" :class="['chip', key !== 'unchanged' && result.counts[key] ? CHANGE_CHIPS[key] : '']">
                {{ CHANGE_LABELS[key] }} <span class="num">{{ result.counts[key] }}</span>
              </span>
            </span>
          </div>
          <p v-if="notice" class="m-0 flex items-start gap-2 text-[12.5px] leading-relaxed text-muted-foreground" data-push-diff-notice>
            <Info class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />{{ notice }}
          </p>

          <section v-if="result.files" aria-labelledby="push-diff-files-title" class="flex flex-col gap-2">
            <h3 id="push-diff-files-title" class="m-0 text-[14px] font-semibold">哪些文件变了</h3>
            <div class="cf-table-wrap">
              <table class="cf-table cf-mobile-records push-diff-files" data-push-diff-files>
                <thead>
                  <tr><th>文件</th><th class="w-20">变化</th><th>摘要</th></tr>
                </thead>
                <tbody>
                  <template v-for="(file, fileIndex) in result.files" :key="file.path">
                    <tr :data-file="file.path" :class="{ 'is-expanded': file.details && filesOpen.has(file.path) }">
                      <td class="min-w-0 align-top">
                        <div class="break-words text-[13px] font-medium"><PushDiffText :value="fileTitle(file)" :hidden="hideSecrets" /></div>
                        <div class="mt-0.5 break-all text-[11.5px] text-muted-foreground"><template v-if="file.role !== 'config'">{{ fileRole(file) }} · </template><span class="font-mono">{{ file.path }}</span></div>
                      </td>
                      <td class="align-top"><span :class="['chip', CHANGE_CHIPS[file.status as PushDiffChange]]">{{ CHANGE_LABELS[file.status] }}</span></td>
                      <td class="align-top text-[12.5px]" data-summary>
                        <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                          <span><span v-for="(part, index) in summarySegments(file)" :key="index" :class="TONE_CLASSES[part.tone]">{{ part.text }}</span></span>
                          <Button v-if="file.details" variant="ghost" size="xs" class="-my-1 text-primary-accent max-md:-my-2"
                            :aria-expanded="filesOpen.has(file.path)" :aria-controls="`push-diff-file-detail-${fileIndex}`" @click="toggleFile(file.path)">
                            <ChevronDown :class="['size-3 transition-transform', filesOpen.has(file.path) && 'rotate-180']" />
                            {{ filesOpen.has(file.path) ? '收起明细' : '展开明细' }}
                          </Button>
                        </div>
                      </td>
                    </tr>
                    <tr v-if="file.details && filesOpen.has(file.path)" :id="`push-diff-file-detail-${fileIndex}`" class="push-diff-detail">
                      <td colspan="3">
                        <PushDiffFileDetails :details="file.details" :hidden="hideSecrets" :agent-id="agent.id" :path="file.path"
                          :version="result.version" :stale="stale" @stale="stale = true" @reload="load(true)" />
                      </td>
                    </tr>
                  </template>
                  <tr v-if="result.files_omitted" class="push-diff-more">
                    <td colspan="3" class="text-[12px] text-muted-foreground">还有 <span class="num">{{ result.files_omitted }}</span> 个变化的文件未列出。</td>
                  </tr>
                  <tr v-if="unchangedCount" class="push-diff-more">
                    <td colspan="3">
                      <Button variant="ghost" size="sm" class="-ml-2 text-muted-foreground" :aria-expanded="unchangedOpen" aria-controls="push-diff-unchanged" @click="unchangedOpen = !unchangedOpen">
                        <ChevronRight :class="['size-3.5 transition-transform', unchangedOpen && 'rotate-90']" />
                        未变的 <span class="num">{{ unchangedCount }}</span> 个文件
                      </Button>
                      <ul v-if="unchangedOpen" id="push-diff-unchanged" class="m-0 mt-1 flex list-none flex-col gap-1 p-0 pb-1.5 text-[12px]">
                        <li v-for="file in result.unchanged" :key="file.path" class="min-w-0 break-all">
                          <PushDiffText :value="fileTitle(file)" :hidden="hideSecrets" /> <span class="text-muted-foreground"><template v-if="file.role !== 'config'">· {{ fileRole(file) }} </template>· <span class="font-mono">{{ file.path }}</span></span>
                        </li>
                        <li v-if="result.unchanged_omitted" class="text-muted-foreground">还有 <span class="num">{{ result.unchanged_omitted }}</span> 个未列出</li>
                      </ul>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section v-if="result.files" aria-labelledby="push-diff-config-title" class="flex flex-col gap-2">
            <h3 id="push-diff-config-title" class="m-0 text-[14px] font-semibold">主配置改了哪些项</h3>
            <p v-if="configNoticeText" class="m-0 text-[12.5px] text-muted-foreground" data-push-diff-config-notice>{{ configNoticeText }}</p>
            <div v-if="items.length" class="cf-table-wrap">
              <table class="cf-table cf-mobile-records push-diff-config" data-push-diff-config>
                <colgroup><col class="w-[36%]"><col class="w-20"><col><col></colgroup>
                <thead>
                  <tr><th scope="col">配置项</th><th scope="col">变化</th><th scope="col">原值</th><th scope="col">新值</th></tr>
                </thead>
                <!-- 按顶层键分组：组标题可点开 / 收起；同一个映射块有多处改动时加块标题，组内只写相对路径 -->
                <tbody v-for="group in groups" :key="group.id" :data-config-group="group.id || 'basic'">
                  <tr class="push-diff-group">
                    <th colspan="4" scope="rowgroup">
                      <Button variant="ghost" size="sm" class="push-diff-group-toggle" :aria-expanded="!isFolded(group)" data-group-toggle
                        @click="toggleGroup(group)">
                        <ChevronRight :class="['size-3.5 shrink-0 text-muted-foreground transition-transform', !isFolded(group) && 'rotate-90']" />
                        <!-- 同一行写出空格：读屏和复制得到「DNS dns 新增 3 项」，而不是粘在一起 -->
                        <span class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5 text-left"><span class="text-[13px] font-semibold text-foreground">{{ group.name }}</span> <span v-if="group.raw" class="font-mono text-[11.5px] font-normal text-muted-foreground">{{ group.raw }}</span> <span class="text-[12px] font-normal text-muted-foreground" data-group-counts>{{ countsText(group.counts) }}</span></span>
                      </Button>
                    </th>
                  </tr>
                  <template v-if="!isFolded(group)">
                    <template v-for="line in group.lines" :key="line.kind === 'row' ? line.row.index : `block-${line.name}`">
                      <tr v-if="line.kind === 'block'" class="push-diff-block" data-config-block>
                        <th colspan="4"><span class="font-mono"><PushDiffText :value="line.name" :hidden="hideSecrets" breaks /></span> · {{ countsText(line.counts) }}</th>
                      </tr>
                      <template v-else>
                        <tr :data-key="line.row.item.key" :class="{ 'is-expanded': line.row.item.list && expanded.has(line.row.index), 'is-nested': line.row.nested }">
                          <td class="push-diff-key align-top font-mono text-[12px] wrap-break-word" data-config-label>
                            <PushDiffText :value="line.row.label" :hidden="hideSecrets" breaks /><span class="hidden max-md:inline" aria-hidden="true">：</span>
                          </td>
                          <td class="push-diff-change align-top"><span :class="['chip', CHANGE_CHIPS[line.row.item.change]]">{{ CHANGE_LABELS[line.row.item.change] }}</span></td>
                          <td :class="['align-top', valueText(line.row.item, 'old').empty && 'push-diff-empty']" data-config-old>
                            <span :class="valueClass(line.row.item, 'old')"><PushDiffText :value="valueText(line.row.item, 'old').value" :hidden="hideSecrets" breaks /></span>
                          </td>
                          <td :class="['align-top', !line.row.item.list && valueText(line.row.item, 'new').empty && 'push-diff-empty']" data-config-new>
                            <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 max-md:inline">
                              <span v-if="!valueText(line.row.item, 'old').empty" class="hidden max-md:inline" aria-hidden="true"> → </span>
                              <span v-if="line.row.item.list" class="text-[12.5px]" data-list-summary>
                                <span v-for="(part, partIndex) in listSegments(line.row.item.list)" :key="partIndex" :class="TONE_CLASSES[part.tone]">{{ part.text }}</span>
                              </span>
                              <span v-else :class="valueClass(line.row.item, 'new')"><PushDiffText :value="valueText(line.row.item, 'new').value" :hidden="hideSecrets" breaks /></span>
                              <Button v-if="line.row.item.list?.entries?.length" variant="ghost" size="xs" class="-my-1 text-primary-accent max-md:my-0 max-md:ml-1"
                                :aria-expanded="expanded.has(line.row.index)" :aria-controls="`push-diff-detail-${line.row.index}`" @click="toggle(line.row.index)">
                                <ChevronDown :class="['size-3 transition-transform', expanded.has(line.row.index) && 'rotate-180']" />
                                {{ expanded.has(line.row.index) ? '收起明细' : '展开明细' }}
                              </Button>
                            </div>
                          </td>
                        </tr>
                        <tr v-if="line.row.item.list && expanded.has(line.row.index)" :id="`push-diff-detail-${line.row.index}`" class="push-diff-detail">
                          <td colspan="4">
                            <ul class="m-0 flex list-none flex-col gap-1 rounded-md border border-border bg-background/50 p-2.5 font-mono text-[12px] leading-relaxed">
                              <li v-for="(entry, entryIndex) in line.row.item.list.entries" :key="entryIndex" class="min-w-0 break-all">
                                <span :class="ENTRY_TONES[entry.change]">{{ ENTRY_SIGNS[entry.change] }} <PushDiffText :value="entry.text" :hidden="hideSecrets" /></span>
                                <ul v-if="entry.fields?.length" class="m-0 mt-0.5 flex list-none flex-col gap-0.5 border-l border-border py-0.5 pl-3 text-muted-foreground">
                                  <li v-for="(field, fieldIndex) in entry.fields" :key="fieldIndex"><PushDiffText :pieces="fieldPieces(field)" :hidden="hideSecrets" /></li>
                                  <li v-if="entry.fields_omitted">另有 <span class="num">{{ entry.fields_omitted }}</span> 处变化</li>
                                </ul>
                              </li>
                            </ul>
                            <p v-if="line.row.item.list.omitted" class="m-0 mt-1.5 text-[12px] text-muted-foreground">
                              还有 <span class="num">{{ line.row.item.list.omitted }}</span> 条未列出{{ rawLines.length ? '，完整内容见下方「原始差异」' : '' }}。
                            </p>
                          </td>
                        </tr>
                      </template>
                    </template>
                  </template>
                </tbody>
              </table>
            </div>
            <p v-if="result.config?.omitted" class="m-0 text-[12px] text-muted-foreground">
              还有 <span class="num">{{ result.config.omitted }}</span> 项配置变化未列出{{ rawLines.length ? '，见下方「原始差异」' : '' }}。
            </p>
          </section>

          <section v-if="result.config?.raw" class="flex flex-col gap-2 border-t border-border pt-3" data-push-diff-raw>
            <div class="flex flex-wrap items-center gap-x-2">
              <Button variant="ghost" size="sm" class="-ml-2" :aria-expanded="rawOpen" aria-controls="push-diff-raw-text" :disabled="!rawLines.length" @click="rawOpen = !rawOpen">
                <ChevronRight :class="['size-3.5 transition-transform', rawOpen && 'rotate-90']" />
                原始差异
              </Button>
              <span class="text-[12px] text-muted-foreground">{{ rawCaption }}</span>
            </div>
            <pre v-if="rawOpen && rawLines.length" id="push-diff-raw-text" class="m-0 max-h-96 overflow-auto rounded-lg border border-border bg-muted p-3 font-mono text-[12px] leading-relaxed"><span v-for="(row, index) in rawRows" :key="index" :class="['block whitespace-pre', LINE_TONES[row.tone]]"><PushDiffText v-if="row.text" :value="row.line" :hidden="hideSecrets" /><template v-else>{{ ' ' }}</template></span></pre>
            <p v-if="rawOpen && result.config.raw.truncated" class="m-0 text-[12px] text-muted-foreground">
              差异较长，只显示前面部分，另有 <span class="num">{{ result.config.raw.omitted_lines }}</span> 行未显示。
            </p>
          </section>
        </div>
      </div>

      <DialogFooter class="shrink-0 flex-row flex-wrap items-center justify-end gap-2 border-t border-border px-4 py-3 sm:px-5">
        <span v-if="pushOffered && offline" class="mr-auto text-[12px] text-muted-foreground">Agent 离线，上线后才能推送。</span>
        <Button variant="outline" @click="emit('close')">关闭</Button>
        <Button v-if="pushOffered && !offline" :disabled="!canPush" data-push-diff-push @click="emit('push')">
          <Loader2 v-if="pushing" class="size-4 animate-spin" />
          <Send v-else class="size-4" />
          推送
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronDown, ChevronRight, CircleAlert, CircleCheck, FileQuestion, Info, Loader2, RefreshCw, Send } from '@lucide/vue'
import type { Agent } from '@/types'
import { agentPushDiffApi, type PushDiffChange, type PushDiffItem, type PushDiffResponse } from '@/api/agentPushDiff'
import {
  CHANGE_CHIPS, CHANGE_LABELS, configGroups, configNotice, contentNotice, countsText, diffLineTone, fieldPieces, fileRole,
  fileTitle, listSegments, summarySegments, textOf, unavailableCopy, valueText, type ConfigGroup, type DiffLineTone, type Tone,
} from '@/lib/agentPushDiff'
import { checkedAgo } from '@/lib/format'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import PushDiffFileDetails from '@/components/agents/PushDiffFileDetails.vue'
import PushDiffText from '@/components/agents/PushDiffText.vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'

const props = defineProps<{
  agent: Agent
  /** 与卡片「推送」按钮相同的可用条件 */
  canPush: boolean
  pushing: boolean
  offline: boolean
}>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'push'): void; (event: 'refresh'): void }>()

const POLL_MS = 1500
const POLL_LIMIT_MS = 120_000
const COUNT_KEYS = ['modified', 'added', 'removed', 'unchanged'] as const
const TONE_CLASSES: Record<Tone, string> = {
  plain: '', muted: 'text-muted-foreground', added: 'num text-success-accent', removed: 'num text-destructive-accent',
}
const ENTRY_TONES: Record<PushDiffChange, string> = {
  added: 'text-success-accent', removed: 'text-destructive-accent', modified: 'text-warning-accent',
}
const ENTRY_SIGNS: Record<PushDiffChange, string> = { added: '+', removed: '−', modified: '~' }
const LINE_TONES: Record<DiffLineTone, string> = {
  added: 'text-success-accent', removed: 'text-destructive-accent', hunk: 'text-muted-foreground', meta: 'text-muted-foreground', plain: '',
}

const loading = ref(true)
const error = ref('')
const result = ref<PushDiffResponse | null>(null)
const expanded = ref(new Set<number>())
// 表 1 展开了明细的文件（按路径）
const filesOpen = ref(new Set<string>())
const unchangedOpen = ref(false)
const rawOpen = ref(false)
// 截图分享时才需要；不记住，每次打开窗口都显示原值
const hideSecrets = ref(false)
// 明细分页发现对比结果已更新：所有展开的明细都提示重新加载
const stale = ref(false)
let controller: AbortController | null = null
let request = 0

const unavailable = computed(() => result.value && (result.value.status === 'never-pushed' || result.value.status === 'unknown')
  ? unavailableCopy(result.value) : null)
const notice = computed(() => result.value ? contentNotice(result.value) : '')
const configNoticeText = computed(() => result.value ? configNotice(result.value) : '')
const items = computed<PushDiffItem[]>(() => result.value?.config?.items || [])
const groups = computed(() => configGroups(items.value))
// 表 2 中与默认展开状态相反的组（按顶层键；改动多的组默认收起）
const groupsToggled = ref(new Set<string>())
const rawLines = computed(() => result.value?.config?.raw?.lines || [])
const rawRows = computed(() => rawOpen.value ? rawLines.value.map(line => {
  const text = textOf(line, false)
  return { line, text, tone: diffLineTone(text) }
}) : [])
const rawCaption = computed(() => {
  const raw = result.value?.config?.raw
  if (!raw?.lines) return '主配置过大，没有生成原始差异'
  if (!raw.lines.length) return '解析后的内容相同（只有注释、空白或写法不同）'
  return hideSecrets.value ? '主配置的 unified diff，敏感信息已隐藏' : '主配置的 unified diff'
})
const unchangedCount = computed(() => result.value?.counts?.unchanged || 0)
const pushedAt = computed(() => {
  const value = result.value?.baseline?.pushed_at
  return value ? new Date(value).toLocaleString() : ''
})
// 推送能消除差异或建立基准时才提供；正在推送、无法对比时不提供。
const pushOffered = computed(() => result.value?.status === 'changed' || Boolean(unavailable.value?.pushable))

function valueClass(item: PushDiffItem, side: 'old' | 'new') {
  if (valueText(item, side).empty) return 'text-[12.5px] text-muted-foreground'
  // 在「.」「-」「/」处断行；没有这些位置的长串（如令牌）才任意处折行
  return 'font-mono text-[12px] wrap-anywhere'
}

function toggled<T>(set: Set<T>, key: T) {
  const next = new Set(set)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  return next
}

function toggle(index: number) {
  expanded.value = toggled(expanded.value, index)
}

function toggleFile(path: string) {
  filesOpen.value = toggled(filesOpen.value, path)
}

const isFolded = (group: ConfigGroup) => group.folded !== groupsToggled.value.has(group.id)

function toggleGroup(group: ConfigGroup) {
  groupsToggled.value = toggled(groupsToggled.value, group.id)
}

function wait(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener('abort', () => { clearTimeout(timer); reject(new DOMException('aborted', 'AbortError')) }, { once: true })
  })
}

/** 读取对比结果；keepOpen 为明细里的「重新加载」，保留已展开的文件 */
async function load(keepOpen = false) {
  controller?.abort()
  const current = ++request
  const own = new AbortController()
  controller = own
  const reopen = keepOpen ? filesOpen.value : new Set<string>()
  loading.value = true
  error.value = ''
  result.value = null
  stale.value = false
  expanded.value = new Set()
  groupsToggled.value = new Set()
  filesOpen.value = new Set()
  rawOpen.value = false
  const started = Date.now()
  try {
    for (;;) {
      const { data } = await agentPushDiffApi.get(props.agent.id, own.signal)
      if (current !== request) return
      if (!data || typeof data.status !== 'string') throw new Error('服务端返回的格式异常，请更新 ConfigFlow 后重试。')
      if (data.status !== 'checking') {
        result.value = data
        filesOpen.value = new Set((data.files || []).filter(file => file.details && reopen.has(file.path)).map(file => file.path))
        break
      }
      if (Date.now() - started > POLL_LIMIT_MS) throw new Error('生成发布包超过 2 分钟仍未完成，请稍后重试。')
      await wait(POLL_MS, own.signal)
    }
    // 卡片上的状态是更早的比较结果；对不上时让页面重新读取列表。
    const card = props.agent.content_sync?.status
    const now = result.value?.status
    if ((now === 'synced' || now === 'changed') && card && card !== now) emit('refresh')
  } catch (failure: any) {
    if (current !== request || own.signal.aborted) return
    error.value = failure?.response?.data?.message || failure?.message || '读取失败，请重试。'
  } finally {
    if (current === request) loading.value = false
  }
}

onMounted(() => load())
onBeforeUnmount(() => {
  request++
  controller?.abort()
})
</script>
