<template>
  <div class="min-w-0">
    <SectionCard v-if="providers.length" class="mb-5" title="规则集合" :description="`rule-providers 共 ${providers.length} 个，规则内容按各自的更新间隔自动刷新`" :padded="false">
      <template #actions>
        <Button variant="outline" class="min-h-11" :disabled="updatingAll || busy.size > 0" @click="updateAll">
          <Loader2 v-if="updatingAll" class="size-3.5 animate-spin" /><RefreshCw v-else class="size-3.5" />全部更新
        </Button>
      </template>
      <div class="cf-table-wrap max-h-80 overflow-y-auto rounded-none border-x-0 border-b-0">
        <table class="cf-table min-w-[620px]">
          <thead><tr><th>名称</th><th>类型</th><th>规则数</th><th>更新时间</th><th class="cf-table__right">操作</th></tr></thead>
          <tbody>
            <tr v-for="provider in providers" :key="provider.name">
              <td class="break-all font-medium">{{ provider.name }}</td>
              <td class="text-xs text-muted-foreground">{{ provider.behavior }} · {{ provider.format || '—' }} · {{ provider.vehicleType }}</td>
              <td class="num">{{ provider.ruleCount ?? '—' }}</td>
              <td class="text-xs text-muted-foreground">{{ provider.updatedAt ? relativeTime(provider.updatedAt) : '—' }}</td>
              <td class="cf-table__right">
                <Button v-if="provider.vehicleType === 'HTTP'" variant="ghost" size="sm" class="min-h-11" :disabled="busy.has(provider.name) || updatingAll" @click="updateProvider(provider)">
                  <Loader2 v-if="busy.has(provider.name)" class="size-3.5 animate-spin" />更新
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <Toolbar v-model:search="search" placeholder="搜索规则类型、内容或策略">
      <template #filters>
        <span class="num text-xs text-muted-foreground">{{ filtered.length }} / {{ rules.length }} 条</span>
      </template>
    </Toolbar>

    <p v-if="error && rules.length" role="alert" class="mb-3 flex flex-wrap items-center gap-x-3 break-words text-[12.5px] text-warning-accent">
      <span>读取最新规则失败，下面是上次读到的内容：{{ error }}</span>
      <Button variant="ghost" size="sm" class="min-h-11" :disabled="loading" @click="load">重试</Button>
    </p>
    <LoadingRows v-if="loading && !rules.length" :rows="6" />
    <SectionCard v-else-if="error && !rules.length">
      <EmptyState :icon="TriangleAlert" title="读取规则失败" :description="error">
        <Button variant="outline" class="min-h-11" @click="load">重试</Button>
      </EmptyState>
    </SectionCard>
    <SectionCard v-else-if="!filtered.length">
      <EmptyState :icon="Search" :title="search ? '没有匹配的规则' : '暂无规则'" />
    </SectionCard>
    <div v-else class="cf-table-wrap">
      <table class="cf-table min-w-[640px] table-fixed">
        <thead><tr><th class="cf-table__num">#</th><th class="w-[22%]">类型</th><th>内容</th><th class="w-[24%]">策略</th></tr></thead>
        <tbody>
          <tr v-for="item in filtered.slice(0, limit)" :key="item.index">
            <td class="cf-table__num num">{{ item.index + 1 }}</td>
            <td class="truncate text-xs">{{ item.rule.type }}</td>
            <td class="break-all font-mono text-xs">
              {{ item.rule.payload || '—' }}
              <span v-if="item.rule.size && item.rule.size > 0" class="ml-1 text-muted-foreground">({{ item.rule.size }})</span>
            </td>
            <td class="truncate text-xs">{{ item.rule.proxy }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="filtered.length > limit" class="cf-table__foot flex items-center justify-between gap-3">
        <span class="text-muted-foreground">已显示 {{ limit }} / {{ filtered.length }} 条</span>
        <Button variant="ghost" size="sm" class="min-h-11" @click="limit += PAGE">显示更多</Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Loader2, RefreshCw, Search, TriangleAlert } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import Toolbar from '@/components/common/Toolbar.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { useTabActive } from '@/components/ui/tabs'
import { mihomoPanelApi } from '@/api'
import { cachedPanelData } from '@/lib/mihomoPanelCache'
import { notify } from '@/lib/feedback'
import { relativeTime } from '@/lib/format'
import { panelError, type MihomoProvider, type MihomoRule } from '@/types/mihomo'

const PAGE = 200
const props = defineProps<{ agentId: string; reloadToken: number }>()
// 所在标签是否为当前标签：由 Tabs 注入，切换标签不经过 props，隐藏的面板不会整表重新渲染
const active = useTabActive()

const rules = ref<MihomoRule[]>([])
const providers = ref<MihomoProvider[]>([])
const loading = ref(false)
const error = ref('')
const search = ref('')
const limit = ref(PAGE)
const busy = reactive(new Set<string>())
const updatingAll = ref(false)

const filtered = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const indexed = rules.value.map((rule, index) => ({ rule, index }))
  if (!keyword) return indexed
  return indexed.filter(({ rule }) => [rule.type, rule.payload, rule.proxy].some(value => value?.toLowerCase().includes(keyword)))
})
watch(search, () => { limit.value = PAGE })

const showProviders = (providerMap: Record<string, MihomoProvider>) => {
  providers.value = Object.values(providerMap).sort((a, b) => a.name.localeCompare(b.name))
}

let epoch = 0
async function load() {
  const requestEpoch = ++epoch
  loading.value = true
  error.value = ''
  // 先显示这台设备上次读到的规则和规则集合，读到新数据后替换
  const cached = cachedPanelData<{ rules?: MihomoRule[] }>(props.agentId, 'rules')?.rules
  if (cached && !rules.value.length) {
    rules.value = cached
    showProviders(cachedPanelData<{ providers?: Record<string, MihomoProvider> }>(props.agentId, 'providers/rules')?.providers || {})
  }
  try {
    const [ruleResult, providerResult] = await Promise.all([
      mihomoPanelApi.rules(props.agentId),
      mihomoPanelApi.ruleProviders(props.agentId).catch(() => null)
    ])
    if (requestEpoch !== epoch) return
    rules.value = ruleResult.data.data?.rules || []
    showProviders(providerResult?.data.data?.providers || {})
  } catch (failure) {
    if (requestEpoch === epoch) error.value = panelError(failure, '暂时无法读取规则。')
  } finally {
    if (requestEpoch === epoch) loading.value = false
  }
}

async function updateProvider(provider: MihomoProvider) {
  busy.add(provider.name)
  try {
    await mihomoPanelApi.updateRuleProvider(props.agentId, provider.name)
    notify.success(`${provider.name} 已更新`)
    await load()
  } catch (failure) {
    notify.error(`更新 ${provider.name} 失败`, panelError(failure))
  } finally {
    busy.delete(provider.name)
  }
}

async function updateAll() {
  const targets = providers.value.filter(provider => provider.vehicleType === 'HTTP')
  if (!targets.length) return notify.info('没有可在线更新的规则集合')
  updatingAll.value = true
  try {
    let failed = 0
    // 逐个更新，避免同时向 Agent 和规则源发出大量下载
    for (const provider of targets) {
      try { await mihomoPanelApi.updateRuleProvider(props.agentId, provider.name) } catch { failed++ }
    }
    if (failed) notify.warning(`${targets.length - failed} 个已更新，${failed} 个失败`)
    else notify.success(`已更新 ${targets.length} 个规则集合`)
    await load()
  } finally {
    updatingAll.value = false
  }
}

// 切走后面板保持挂载：隐藏期间的刷新只做标记，切回时再读取
let stale = false
watch(() => props.reloadToken, () => { stale = !active.value; if (active.value) void load() })
watch(active, shown => { if (shown && stale) { stale = false; void load() } })
onMounted(load)
</script>
