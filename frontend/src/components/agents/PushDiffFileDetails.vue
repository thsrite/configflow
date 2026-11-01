<template>
  <div class="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-background/50 p-2.5" :data-file-details="details.kind">
    <div v-if="searchable" class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <div class="relative min-w-0 flex-[1_1_240px]">
        <Search class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <Input v-model="search" :placeholder="placeholder" :aria-label="placeholder" :maxlength="MAX_QUERY_CHARS" :disabled="stale"
          enterkeyhint="search" autocomplete="off" class="h-8 bg-background pr-9 pl-8 text-[12.5px] max-md:pr-11" data-detail-search
          @keydown.enter.prevent="applyQuery(search.trim())" />
        <!-- 不用 translate 居中：按钮按下时的 active:translate-y-px 会把它挪开 -->
        <Button v-if="search && !stale" variant="ghost" size="icon-sm" class="absolute inset-y-0 right-0 my-auto text-muted-foreground"
          aria-label="清空搜索" @click="clearSearch">
          <X class="size-3.5" />
        </Button>
      </div>
      <!-- 手机上只给读屏：每组标题已写匹配条数，不再单占一行 -->
      <span class="text-[12px] text-muted-foreground max-sm:sr-only" aria-live="polite" data-search-summary><template v-if="query && !stale"><template v-if="searching">正在搜索…</template><template v-else>匹配 <span class="num">{{ matched }}</span> {{ unit.unit }}</template></template></span>
    </div>

    <div v-if="stale" role="alert" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-md bg-warning-soft px-2.5 py-1.5 text-[12.5px]" data-details-stale>
      <span class="flex min-w-0 flex-1 basis-48 items-start gap-1.5">
        <Info class="mt-0.5 size-3.5 shrink-0 text-warning-accent" aria-hidden="true" />对比结果已更新，请重新加载。
      </span>
      <Button ref="reloadButton" variant="outline" size="sm" data-details-reload @click="emit('reload')">
        <RefreshCw class="size-3.5" />重新加载
      </Button>
    </div>

    <div class="-mx-1 flex max-h-[420px] min-w-0 flex-col gap-2.5 overflow-y-auto overscroll-contain px-1 max-sm:max-h-[60dvh]"
      tabindex="0" role="region" :aria-label="`${unit.noun}明细`" data-details-scroll>
      <section v-for="group in details.groups" :key="group.change" :data-group="group.change">
        <p :id="`${uid}-${group.change}`" class="m-0 text-[12px] text-muted-foreground">
          {{ CHANGE_LABELS[group.change] }} <span class="num">{{ group.total }}</span> {{ unit.unit }}{{ unit.noun }}<template v-if="query"> · <template v-if="view(group).loading && !view(group).entries.length">正在搜索…</template><template v-else>匹配 <span class="num">{{ view(group).total }}</span> {{ unit.unit }}</template></template>
        </p>
        <!-- 节点用表格（节点 | 变更项 | 变更前 | 变更后），规则仍是一行一条的 + / − 列表 -->
        <PushDiffNodeTable v-if="details.kind === 'nodes' && view(group).entries.length" class="mt-1" :entries="view(group).entries"
          :hidden="hidden" :labelledby="`${uid}-${group.change}`" />
        <ul v-else-if="view(group).entries.length" class="m-0 mt-1 flex list-none flex-col gap-1 p-0 font-mono text-[12px] leading-relaxed">
          <li v-for="(entry, index) in view(group).entries" :key="index" class="min-w-0 break-all">
            <span :class="ENTRY_TONES[entry.change]">{{ ENTRY_SIGNS[entry.change] }} <PushDiffText :value="entry.text" :hidden="hidden" /></span>
          </li>
        </ul>
        <p v-if="!pageable && group.omitted" class="m-0 mt-1 text-[12px] text-muted-foreground">
          还有 <span class="num">{{ group.omitted }}</span> {{ unit.unit }}未列出。
        </p>
        <div v-else-if="pageable && footer(group)" class="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground" data-group-footer>
          <template v-if="footer(group) === 'error'">
            <span class="min-w-0 break-words text-destructive-accent" data-load-error>加载失败：{{ view(group).error }}</span>
            <Button variant="ghost" size="sm" class="-ml-1 text-primary-accent" @click="more(group)">
              <RefreshCw class="size-3.5" />重试
            </Button>
          </template>
          <Button v-else-if="footer(group) === 'more'" variant="ghost" size="sm" class="-ml-2 text-primary-accent"
            :disabled="view(group).loading" data-load-more @click="more(group)">
            <Loader2 v-if="view(group).loading" class="size-3.5 animate-spin" />
            <template v-if="view(group).loading">正在加载…</template>
            <template v-else>加载更多（还剩 <span class="num">{{ remaining(group) }}</span> {{ unit.unit }}）</template>
          </Button>
          <span v-else-if="footer(group) === 'not-kept'" data-not-kept>
            另有 <span class="num">{{ notKept(group) }}</span> {{ unit.unit }}超出明细上限，未列出。
          </span>
          <span v-else-if="footer(group) === 'done'" data-all-loaded>已全部加载</span>
        </div>
      </section>
      <p v-if="query && !stale && !searching && !matched && !failed" class="m-0 text-[12px] text-muted-foreground" data-search-empty>
        没有匹配「{{ query }}」的{{ unit.noun }}。
      </p>
    </div>
    <p v-for="note in notes" :key="note" class="m-0 text-[12px] leading-relaxed text-muted-foreground">{{ note }}</p>
  </div>
</template>

<script setup lang="ts">
// 「待推送的变更」表 1 里订阅、规则集展开的明细：按新增、删除、修改分组，节点每组一张表，规则每组一个列表；
// 接口只带每组前 20 条，其余用「加载更多」和服务端搜索按页读取（带对比结果的版本，结果更新后提示重新加载）；
// 敏感片段随开关隐藏。
import { computed, nextTick, onBeforeUnmount, reactive, ref, useId, watch } from 'vue'
import { Info, Loader2, RefreshCw, Search, X } from '@lucide/vue'
import { agentPushDiffApi, type PushDiffChange, type PushDiffDetailGroup, type PushDiffFileDetails, type PushDiffListEntry } from '@/api/agentPushDiff'
import {
  CHANGE_LABELS, MAX_QUERY_CHARS, PAGE_SIZE, SEARCH_DELAY_MS, detailNotes, detailUnit, groupPaging,
} from '@/lib/agentPushDiff'
import PushDiffNodeTable from '@/components/agents/PushDiffNodeTable.vue'
import PushDiffText from '@/components/agents/PushDiffText.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const props = defineProps<{
  details: PushDiffFileDetails
  hidden: boolean
  agentId: string
  /** 文件路径，分页请求用 */
  path: string
  /** 对比结果的版本；没有时不能分页 */
  version?: string
  /** 对比结果已更新（任一明细发现即整窗有效） */
  stale?: boolean
}>()
const emit = defineEmits<{ (event: 'stale'): void; (event: 'reload'): void }>()

const ENTRY_TONES: Record<PushDiffChange, string> = {
  added: 'text-success-accent', removed: 'text-destructive-accent', modified: 'text-warning-accent',
}
const ENTRY_SIGNS: Record<PushDiffChange, string> = { added: '+', removed: '−', modified: '~' }

interface ListState {
  entries: PushDiffListEntry[]
  /** 能读取的条数；搜索时为匹配的条数 */
  total: number
  loading: boolean
  error: string
  /** 用户点过「加载更多」，读完后显示「已全部加载」 */
  loadedMore: boolean
}

// 分组标题的 id 前缀：节点表用它作名称
const uid = useId()
const unit = computed(() => detailUnit(props.details.kind))
const pageable = computed(() => Boolean(props.details.paging && props.version))
// 还有没随接口返回的条目时才需要搜索
const searchable = computed(() => pageable.value && props.details.groups.some(group => group.total > group.entries.length))
const placeholder = computed(() => props.details.kind === 'nodes' ? '搜索节点名、服务器或字段' : '搜索规则')

const base = reactive<Record<string, ListState>>({})
const found = reactive<Record<string, ListState>>({})
const search = ref('')
const query = ref('')
const reloadButton = ref<InstanceType<typeof Button> | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0
let searchController: AbortController | null = null
const controller = new AbortController()

const blank = (entries: PushDiffListEntry[] = [], total = 0): ListState => ({ entries, total, loading: false, error: '', loadedMore: false })

watch(() => props.details, details => {
  for (const group of details.groups) base[group.change] = blank([...group.entries], group.available ?? group.total)
}, { immediate: true })

const view = (group: PushDiffDetailGroup): ListState => (query.value ? found[group.change] : base[group.change]) || blank()
const searching = computed(() => props.details.groups.some(group => found[group.change]?.loading && !found[group.change]?.entries.length))
const matched = computed(() => props.details.groups.reduce((sum, group) => sum + (found[group.change]?.total || 0), 0))
const failed = computed(() => props.details.groups.some(group => found[group.change]?.error))

function remaining(group: PushDiffDetailGroup) {
  const state = view(group)
  return query.value ? Math.max(0, state.total - state.entries.length) : groupPaging(group, state.entries.length, pageable.value).remaining
}

const notKept = (group: PushDiffDetailGroup) => groupPaging(group, base[group.change]?.entries.length || 0, pageable.value).notKept

/** 每组底部：加载失败、加载更多、超出上限、已全部加载，或什么都不显示 */
function footer(group: PushDiffDetailGroup): 'error' | 'more' | 'not-kept' | 'done' | '' {
  const state = view(group)
  if (props.stale) return ''
  if (state.error) return 'error'
  if (remaining(group) > 0) return 'more'
  if (query.value) return state.loadedMore ? 'done' : ''
  if (notKept(group)) return 'not-kept'
  return state.loadedMore ? 'done' : ''
}

const notes = computed(() => {
  const list = detailNotes(props.details)
  const cut = props.details.groups.reduce((sum, group) => sum + notKept(group), 0)
  if (query.value && cut) list.push(`有 ${cut} ${unit.value.unit}超出明细上限，没有保留，搜索不到。`)
  return list
})

function message(failure: any) {
  return failure?.response?.data?.message || failure?.message || '读取失败，请重试。'
}

async function markStale() {
  emit('stale')
  await nextTick()
  ;(reloadButton.value?.$el as HTMLElement | undefined)?.focus()
}

/** 读下一页：基础列表接着已显示的条目读，搜索时接着已找到的读；同一组同一时间只有一个请求 */
async function more(group: PushDiffDetailGroup) {
  const searchingNow = query.value
  const state = view(group)
  if (state.loading || !props.version || props.stale) return
  const own = searchingNow ? generation : -1
  const signal = searchingNow ? searchController?.signal : controller.signal
  const first = !state.entries.length && Boolean(searchingNow)
  state.loading = true
  state.error = ''
  try {
    const { data } = await agentPushDiffApi.entries(props.agentId, {
      version: props.version, file: props.path, change: group.change, offset: state.entries.length, limit: PAGE_SIZE,
      ...(searchingNow ? { q: searchingNow } : {}),
    }, signal)
    if (searchingNow && own !== generation) return
    if (data?.status === 'stale') {
      await markStale()
      return
    }
    if (data?.status !== 'ok' || !Array.isArray(data.entries)) throw new Error('服务端返回的格式异常，请更新 ConfigFlow 后重试。')
    state.entries.push(...data.entries)
    if (typeof data.total === 'number') state.total = data.total
    if (!first) state.loadedMore = true
  } catch (failure: any) {
    if (signal?.aborted || (searchingNow && own !== generation)) return
    state.error = message(failure)
  } finally {
    if (!searchingNow || own === generation) state.loading = false
  }
}

/** 换搜索词：取消上一次搜索，每组从头读第一页；空词回到原列表 */
function applyQuery(next: string) {
  clearTimeout(timer)
  if (next === query.value || props.stale) return
  searchController?.abort()
  generation++
  query.value = next
  if (!next) return
  searchController = new AbortController()
  for (const group of props.details.groups) {
    found[group.change] = blank()
    void more(group)
  }
}

function clearSearch() {
  search.value = ''
  applyQuery('')
}

watch(search, value => {
  clearTimeout(timer)
  const next = value.trim()
  if (!next) applyQuery('')
  else timer = setTimeout(() => applyQuery(next), SEARCH_DELAY_MS)
})

onBeforeUnmount(() => {
  clearTimeout(timer)
  generation++
  searchController?.abort()
  controller.abort()
})
</script>
