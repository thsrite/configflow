<template>
  <div class="min-w-0">
    <PageHeader title="OxiDNS 面板" class="oxidns-monitor-header">
      <template #title-extra>
        <Select :model-value="agentId" :disabled="!devices?.length || switchingDevice" @update:model-value="switchDevice(String($event))">
          <SelectTrigger class="min-h-11 w-auto min-w-0 max-w-32 sm:max-w-48" aria-label="监控设备" :title="currentDevice?.name || currentDevice?.id">
            <SelectValue class="min-w-0 flex-1 text-left" :placeholder="devicesLoading ? '读取设备…' : '选择设备'">
              <span class="truncate font-medium">{{ currentDevice?.name || currentDevice?.id || (devicesLoading ? '读取设备…' : '选择设备') }}</span>
            </SelectValue>
          </SelectTrigger>
          <SelectContent class="w-80 max-w-[calc(100vw-2rem)]">
            <SelectItem v-for="device in devices || []" :key="device.id" :value="device.id" :text-value="device.name || device.id" class="min-h-14 [&>span:last-child]:min-w-0 [&>span:last-child]:flex-1">
              <span class="min-w-0 flex-1">
                <span class="block whitespace-normal break-all font-medium">{{ device.name || device.id }}</span>
                <span class="block whitespace-normal break-all font-mono text-xs text-muted-foreground">{{ device.host || '—' }}</span>
              </span>
              <StatusDot class="shrink-0" :tone="device.status === 'online' ? 'success' : 'muted'" :label="device.status === 'online' ? '在线' : '离线'" />
            </SelectItem>
          </SelectContent>
        </Select>
      </template>
      <template #actions>
        <div class="flex w-full flex-wrap items-center justify-between gap-2">
          <label class="flex min-h-11 items-center gap-2 text-[13px] text-muted-foreground">
            <Switch v-model="autoRefresh" aria-label="每 10 秒自动刷新监控" />自动刷新 · 10 秒
          </label>
          <div class="ml-auto flex shrink-0 items-center gap-2 max-sm:gap-1">
            <Button variant="outline" class="min-h-11 max-sm:size-11 max-sm:gap-0" aria-label="刷新监控" title="刷新监控" :aria-busy="overviewLoading || queriesLoading || devicesLoading" :disabled="overviewLoading || queriesLoading || devicesLoading" @click="refreshAll">
              <RefreshCw class="size-4" :class="{ 'animate-spin': overviewLoading || queriesLoading || devicesLoading }" /><span class="max-sm:sr-only">刷新</span>
            </Button>
            <Button variant="outline" class="min-h-11 max-sm:size-11 max-sm:gap-0" :aria-label="openingSettings ? '正在打开设置…' : 'OxiDNS 设置'" :title="openingSettings ? '正在打开设置…' : 'OxiDNS 设置'" :aria-busy="openingSettings" :disabled="openingSettings || switchingDevice || !currentDevice" @click="openSettings()">
              <Loader2 v-if="openingSettings" class="size-4 animate-spin" /><Settings v-else class="size-4" /><span class="max-sm:sr-only">{{ openingSettings ? '正在打开…' : '设置' }}</span>
            </Button>
          </div>
        </div>
        <p v-if="openingSettings" role="status" class="m-0 w-full text-xs text-primary-accent sm:hidden">正在打开设置…</p>
      </template>
    </PageHeader>
    <p v-if="settingsError" role="alert" class="mb-4 break-words text-sm text-destructive-accent">{{ settingsError }}</p>
    <div v-if="devicesError || switchError" role="alert" class="mb-4 flex flex-wrap items-center gap-x-3 text-sm text-warning-accent">
      <span>{{ devicesError || switchError }}</span>
      <Button v-if="devicesError" variant="ghost" class="min-h-11" :disabled="devicesLoading" @click="loadDevices">重试设备列表</Button>
    </div>
    <SectionCard v-if="devices && !devices.length">
      <EmptyState :icon="Server" title="暂无 OxiDNS Agent" description="接入 OxiDNS Agent 后，即可在这里查看查询记录、缓存命中率和运行统计。">
        <Button class="min-h-11" @click="router.push('/agents')">前往 Agent 接入</Button>
      </EmptyState>
    </SectionCard>
    <template v-else>
      <div v-if="currentDevice || overview" class="cf-monitor-status oxidns-monitor-status mb-5 flex flex-wrap items-center gap-x-4 gap-y-2 text-[12.5px] text-muted-foreground">
        <StatusDot v-if="currentDevice?.status === 'offline'" tone="muted" label="Agent 离线" />
        <StatusDot v-if="overview" :tone="coreRunning ? 'success' : 'warning'" :label="coreRunning ? '核心运行中' : '核心状态：' + overview.core.status" />
        <span v-if="currentDevice?.host" class="min-w-0 break-all font-mono" aria-label="当前设备 IP">{{ currentDevice.host }}</span>
        <span v-if="overview?.core.version" class="font-mono">OxiDNS {{ overview.core.version }}</span>
        <span v-if="overview">已运行 {{ uptime }}</span>
        <span v-if="overview" class="ml-auto text-xs">采样 {{ sampleTime }}</span>
      </div>
      <p v-if="overviewError && overview" role="alert" class="mb-4 rounded-lg border border-warning-accent/30 bg-warning-soft p-3 text-sm text-warning-accent">
        {{ overviewError }} 保留上次成功读取的数据，采样时间见上方。
      </p>
      <Tabs :model-value="tab" class="min-w-0" @update:model-value="changeTab(String($event))">
        <div class="mb-4 flex min-w-0 flex-wrap items-start justify-between gap-x-4 gap-y-2 max-sm:mb-2 max-sm:gap-y-1">
          <TabsList class="min-h-11 shrink-0 max-sm:grid max-sm:h-auto max-sm:w-full max-sm:grid-cols-2">
            <TabsTrigger value="overview" class="min-h-11 px-4 max-sm:px-2">运行概览</TabsTrigger>
            <TabsTrigger value="queries" class="min-h-11 px-4 max-sm:px-2">查询记录</TabsTrigger>
          </TabsList>
          <div class="oxidns-panel-actions ml-auto flex min-w-0 max-w-full flex-wrap items-center gap-2 max-sm:w-full sm:min-w-[28rem] sm:justify-end">
            <OxiDNSStatisticsControls v-if="tab === 'overview' && overview" :statistics="overview.statistics" :resetting="resettingStatistics" @reset="confirmResetStatistics" />
            <OxiDNSRuleUpdate :agent-id="agentId" :disabled="switchingDevice || currentDevice?.status !== 'online'" @updated="refreshAll" />
            <OxiDNSCacheFlush :agent-id="agentId" :device-name="currentDevice?.name || agentId" :disabled="switchingDevice || currentDevice?.status !== 'online'"
              @flushed="id => { if (id === agentId) refresh(true) }" />
          </div>
        </div>
        <template v-if="!overview">
          <LoadingRows v-if="overviewLoading" :rows="5" />
          <SectionCard v-else>
            <EmptyState :icon="TriangleAlert" title="暂时无法读取 OxiDNS 数据" :description="overviewError || '等待核心返回监控数据。'">
              <div class="flex flex-wrap justify-center gap-2">
                <Button v-if="overviewErrorCode === 'API_NOT_CONFIGURED'" class="min-h-11" :disabled="openingSettings" @click="openSettings('api')"><Settings class="size-4" />{{ openingSettings ? '正在打开…' : '前往设置' }}</Button>
                <Button variant="outline" class="min-h-11" :disabled="overviewLoading" @click="refresh(true)">重试</Button>
              </div>
            </EmptyState>
          </SectionCard>
        </template>
        <p v-if="clientNotesError" role="alert" class="mb-3 break-words text-xs text-warning-accent">
          设备备注暂未读取：{{ clientNotesError }}<span v-if="Object.keys(clientNotes).length"> 保留上次成功读取的备注。</span>
        </p>
        <TabsContent value="overview"><OxiDNSOverviewPanel v-if="overview" :overview="overview" :qps="qps" :recorder="recorder" :resetting="resettingStatistics" :notes="clientNotes"
          @recorder="selectRecorder" /></TabsContent>
        <TabsContent value="queries">
          <SectionCard v-if="overview && !overview.recorders.length">
            <EmptyState :icon="Search" title="尚未启用查询记录" description="打开此 Agent 所属配置空间的查询记录设置，启用并保存草稿后，发布到此 Agent 生效。">
              <Button class="min-h-11" :disabled="openingSettings" @click="openSettings('query_recorder')"><Settings class="size-4" />{{ openingSettings ? '正在打开…' : '前往设置' }}</Button>
            </EmptyState>
          </SectionCard>
          <OxiDNSQueryTable v-else-if="overview" :agent-id="agentId" :instance-id="overview.core.instance_id" :recorders="overview.recorders" :recorder="recorder" :filters="filters" :data="queries"
            :collected-at="overview.collected_at" :clock-skew-ms="clockSkewMs" :notes="clientNotes"
            :loading="queriesLoading" :error="queriesError" :page="page" :has-next="hasNext"
            @filter="applyFilters" @recorder="selectRecorder" @next="nextPage" @previous="previousPage" @retry="loadQueries(true)" @detail="selectedRecord = $event" />
        </TabsContent>
      </Tabs>
    </template>
    <OxiDNSQueryDetail v-if="selectedRecord" :agent-id="agentId" :record-id="selectedRecord.id" :recorder="recorder" @close="selectedRecord = null" />
    <OxidnsSettings v-if="settingsEditor" :open="true" :profile-id="settingsEditor.profileId" :context-label="settingsEditor.label"
      :initial-feature="settingsEditor.initialFeature" back-label="返回 OxiDNS 面板" @update:open="value => { if (!value) settingsEditor = null }" />
  </div>
</template>
<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2, RefreshCw, Search, Server, Settings, TriangleAlert } from '@lucide/vue'
import { agentApi, clientToolsApi, profileApi } from '@/api'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import OxiDNSOverviewPanel from '@/components/agents/OxiDNSOverview.vue'
import OxiDNSStatisticsControls from '@/components/agents/OxiDNSStatisticsControls.vue'
import OxiDNSQueryTable from '@/components/agents/OxiDNSQueryTable.vue'
import OxiDNSQueryDetail from '@/components/agents/OxiDNSQueryDetail.vue'
import OxiDNSRuleUpdate from '@/components/agents/OxiDNSRuleUpdate.vue'
import OxiDNSCacheFlush from '@/components/agents/OxiDNSCacheFlush.vue'
import { useOxiDNSMonitor } from '@/composables/useOxiDNSMonitor'
import { oxidnsMonitorPath, rememberOxiDNSDevice, useOxiDNSDevices } from '@/composables/useOxiDNSDevices'
import { monitorNumber, type OxiDNSQuery } from '@/types/oxidnsMonitor'
import { confirm, notify } from '@/lib/feedback'
const OxidnsSettings = defineAsyncComponent(() => import('@/components/generate/OxidnsSettings.vue'))
const settingsEditor = ref<{ profileId: string; label: string; initialFeature?: string } | null>(null)
const openingSettings = ref(false)
const settingsError = ref('')
let settingsRequest = 0
const route = useRoute()
const router = useRouter()
const agentId = computed(() => String(route.params.agentId || ''))
watch(agentId, () => { settingsRequest++; settingsEditor.value = null; openingSettings.value = false; settingsError.value = '' })
async function openSettings(initialFeature?: string) {
  if (openingSettings.value) return
  const current = ++settingsRequest
  const id = agentId.value
  openingSettings.value = true
  settingsError.value = ''
  try {
    // Agent binding is independent of the active sidebar profile. Re-read it
    // before opening the editor rather than trusting a stale monitor snapshot.
    // Binding lookup needs no full-profile content comparison. Bypass the picker
    // cache so a just-rebound device still opens the current profile.
    const [agents, profiles] = await Promise.all([agentApi.summaries(), profileApi.list()])
    if (disposed || current !== settingsRequest || id !== agentId.value) return
    const agent = agents.data.find((item: any) => item.id === id && item.service_type === 'oxidns')
    if (!agent) throw new Error('这台 OxiDNS Agent 已不存在，请刷新设备列表。')
    const profileId = agent.profile_id || profiles.data.find((item: any) => item.is_default ?? item.id === 'default')?.id
    const profile = profiles.data.find((item: any) => item.id === profileId)
    if (!profile) throw new Error('找不到此 Agent 绑定的配置空间，请检查设备绑定后重试。')
    settingsEditor.value = { profileId, label: `${profile.name || profileId} · ${agent.name || id}`, initialFeature }
  } catch (failure: any) {
    if (!disposed && current === settingsRequest) settingsError.value = failure?.response?.data?.message || failure?.message || '暂时无法打开设置，请重试。'
  } finally {
    if (current === settingsRequest) openingSettings.value = false
  }
}
const tab = ref(route.query.tab === 'queries' ? 'queries' : 'overview')
function changeTab(value: string) {
  tab.value = value
}
const selectedRecord = ref<OxiDNSQuery | null>(null)
const { overview, overviewLoading, overviewError, overviewErrorCode, resettingStatistics, resetError, resetStatistics, clockSkewMs, qps, queries, queriesLoading, queriesError, recorder, filters,
  page, hasNext, autoRefresh, refresh, loadQueries, selectRecorder, applyFilters, nextPage, previousPage } = useOxiDNSMonitor(agentId, tab)
const { devices, loading: devicesLoading, error: devicesError, load: loadDevices } = useOxiDNSDevices()
const currentDevice = computed(() => devices.value?.find(device => device.id === agentId.value) || overview.value?.agent)
const switchingDevice = ref(false)
const switchError = ref('')
watch([agentId, recorder], () => { selectedRecord.value = null })
watch([agentId, devices], () => {
  if (devices.value?.some(device => device.id === agentId.value)) rememberOxiDNSDevice(agentId.value)
})
onMounted(loadDevices)
async function switchDevice(id: string) {
  if (id === agentId.value || switchingDevice.value || !devices.value?.some(device => device.id === id)) return
  selectedRecord.value = null
  switchingDevice.value = true
  switchError.value = ''
  try {
    const failure = await router.replace({ path: oxidnsMonitorPath(id), query: { ...route.query, tab: tab.value } })
    if (!failure) rememberOxiDNSDevice(id)
  } catch (failure: any) {
    switchError.value = failure?.message || '切换设备失败，请重试。'
  } finally {
    switchingDevice.value = false
  }
}
async function refreshAll() {
  await Promise.allSettled([loadDevices(), refresh(true), loadClientNotes()])
}
let disposed = false
const clientNotes = ref<Record<string, string>>({})
const clientNotesError = ref('')
let notesRequest = 0
async function loadClientNotes() {
  const request = ++notesRequest
  const id = agentId.value
  if (!id) return
  try {
    const { data } = await clientToolsApi.notes(id)
    if (disposed || request !== notesRequest || id !== agentId.value) return
    if (!data?.success) throw new Error(data?.message || '读取备注失败，请重试。')
    clientNotes.value = data.notes || {}; clientNotesError.value = ''
  } catch (failure: any) {
    if (!disposed && request === notesRequest && id === agentId.value) clientNotesError.value = failure?.response?.data?.message || failure?.message || '读取备注失败，请重试。'
  }
}
watch(agentId, () => { clientNotes.value = {}; clientNotesError.value = ''; void loadClientNotes() }, { immediate: true })
onUnmounted(() => { disposed = true; notesRequest++ })
async function confirmResetStatistics() {
  if (resettingStatistics.value || !overview.value?.statistics) return
  const requestAgent = agentId.value
  const accepted = await confirm('将从现在起重新计算「' + (currentDevice.value?.name || requestAgent) + '」的累计指标和请求排行，所有查看此设备的页面都会使用新起点。查询记录和 DNS 服务保持不变。', {
    title: '重置面板统计？', confirmText: '重新开始统计'
  })
  if (!accepted || disposed || requestAgent !== agentId.value) return
  if (await resetStatistics()) notify.success('已重新开始统计', '查询记录已保留，DNS 服务持续运行。')
  else if (resetError.value) notify.error('重置统计失败', resetError.value)
}
const coreRunning = computed(() => ['running', 'online', 'healthy', 'ready', 'ok'].includes(overview.value?.core.status || ''))
const sampleTime = computed(() => overview.value ? new Date(overview.value.collected_at).toLocaleTimeString() : '—')
const uptime = computed(() => {
  const value = overview.value?.core.uptime_ms
  if (!monitorNumber(value)) return '—'
  const seconds = Math.max(0, Math.floor(value / 1000))
  if (seconds < 60) return seconds + ' 秒'
  if (seconds < 3600) return Math.floor(seconds / 60) + ' 分钟'
  if (seconds < 86400) return Math.floor(seconds / 3600) + ' 小时 ' + Math.floor(seconds % 3600 / 60) + ' 分钟'
  return Math.floor(seconds / 86400) + ' 天 ' + Math.floor(seconds % 86400 / 3600) + ' 小时'
})
</script>

<style scoped>
@media (width < 640px) {
  .oxidns-monitor-header :deep(.cf-page-heading) {
    flex-basis: 100%;
    flex-wrap: nowrap;
    justify-content: space-between;
    gap: var(--cf-sp-2);
  }

  .oxidns-monitor-header :deep(.cf-page-actions) {
    width: 100%;
    margin-left: 0;
  }

  .oxidns-monitor-status {
    margin-bottom: var(--cf-sp-2);
    gap: var(--cf-sp-1) var(--cf-sp-3);
  }
}
</style>
