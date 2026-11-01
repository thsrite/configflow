<template>
  <SectionCard title="规则命中" :padded="false" :help="HELP">
    <template #actions>
      <span v-if="stats.supported && stats.total" class="num text-[12px] text-muted-foreground">共 {{ stats.total.toLocaleString() }} 次</span>
    </template>
    <p v-if="error" role="alert" class="mx-5 mt-0 mb-3 break-words text-[12.5px] text-warning-accent max-md:mx-4 max-sm:mx-3">{{ error }}</p>
    <LoadingRows v-if="!loaded && !error" :rows="4" />
    <EmptyState v-else-if="loaded && !stats.supported" :icon="CircleSlash" title="内核没有提供命中次数" description="Mihomo v1.19.19 起才在规则接口里提供命中次数，更新内核后显示。" />
    <EmptyState v-else-if="loaded && !stats.items.length" :icon="ListOrdered" title="还没有规则被命中" description="有连接经过 Mihomo 后，这里按命中次数列出规则。" />
    <ol v-else-if="loaded" class="m-0 grid list-none grid-flow-col grid-cols-2 gap-x-6 border-t border-border px-5 py-1 max-lg:grid-flow-row max-lg:grid-cols-1 max-md:px-4 max-sm:px-3"
      :style="{ gridTemplateRows: wide ? `repeat(${Math.ceil(top.length / 2)}, auto)` : undefined }">
      <li v-for="(item, index) in top" :key="item.index" class="flex min-w-0 items-center gap-3 border-b border-border/60 py-2" data-rule-hit>
        <span class="num w-5 shrink-0 text-right text-[11.5px] text-muted-foreground">{{ index + 1 }}</span>
        <span class="min-w-0 flex-1">
          <span class="flex min-w-0 items-baseline gap-2">
            <span class="min-w-0 flex-1 truncate font-mono text-[12.5px]" :title="ruleText(item)">
              <span class="text-muted-foreground">{{ item.type }}</span><template v-if="item.payload">,{{ item.payload }}</template>
            </span>
            <span class="num shrink-0 text-[13px]">{{ item.hits.toLocaleString() }}</span>
          </span>
          <span class="mt-1 flex min-w-0 items-center gap-2">
            <span class="h-1 w-24 shrink-0 overflow-hidden rounded-full bg-secondary max-sm:w-16">
              <span class="block h-full rounded-full bg-primary-accent/70" :style="{ width: `${Math.max(3, item.hits / top[0]!.hits * 100)}%` }" />
            </span>
            <span class="min-w-0 truncate text-[11.5px] text-muted-foreground" :title="`${item.proxy} · 最近命中 ${relativeTime(item.hitAt)}`">
              {{ item.proxy }} · {{ relativeTime(item.hitAt) }}
            </span>
          </span>
        </span>
      </li>
    </ol>
  </SectionCard>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { CircleSlash, ListOrdered } from '@lucide/vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { mihomoPanelApi } from '@/api'
import { relativeTime } from '@/lib/format'
import { ruleHitCounts, type MihomoRuleWithExtra, type RuleHitItem } from '@/lib/mihomo-overview'
import { panelError } from '@/types/mihomo'

const HELP = '命中次数由 Mihomo 内核统计，从内核上次启动或重载配置起累计，重启后清零。只列出命中最多的 10 条，完整顺序在「规则」标签查看。'
const TOP = 10
const POLL_MS = 15_000

const props = defineProps<{ agentId: string; active: boolean; reloadToken?: number }>()
// 宽屏分两列，按列从上到下排名
const wide = useMediaQuery('(min-width: 1024px)')
const rules = ref<MihomoRuleWithExtra[]>([])
const loaded = ref(false)
const error = ref('')
const stats = computed(() => ruleHitCounts(rules.value))
const top = computed(() => stats.value.items.slice(0, TOP))
const ruleText = (item: RuleHitItem) => `${item.type}${item.payload ? ',' + item.payload : ''} → ${item.proxy}`

let disposed = false
let epoch = 0
async function load() {
  const request = ++epoch
  try {
    const { data } = await mihomoPanelApi.rules(props.agentId)
    if (disposed || request !== epoch) return
    rules.value = data.data?.rules || []
    loaded.value = true
    error.value = ''
  } catch (failure) {
    if (disposed || request !== epoch) return
    error.value = panelError(failure, '暂时读不到规则。')
  }
}

let timer: ReturnType<typeof setInterval> | null = null
function stop() {
  if (timer) clearInterval(timer)
  timer = null
}
function start() {
  stop()
  void load()
  timer = setInterval(() => {
    if (document.visibilityState === 'visible') void load()
  }, POLL_MS)
}

watch(() => props.active, shown => { if (shown) start(); else stop() })
watch(() => props.reloadToken, () => { if (props.active) void load() })
onMounted(() => { if (props.active) start() })
onUnmounted(() => {
  disposed = true
  stop()
})
</script>
