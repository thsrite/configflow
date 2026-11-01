<template>
  <div class="min-w-0">
    <PageHeader title="Mihomo 面板" class="mihomo-panel-header">
      <template #title-extra>
        <Select :model-value="agentId" :disabled="!devices?.length" @update:model-value="selectDevice(String($event))">
          <SelectTrigger class="min-h-11 w-auto min-w-0 max-w-32 sm:max-w-48" aria-label="Mihomo 设备" :title="currentDevice?.name || currentDevice?.id">
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
      <template v-if="agentId" #actions>
        <div class="flex w-full flex-wrap items-center justify-between gap-2">
          <Segmented class="shrink-0 max-sm:[&_[data-segment]]:min-w-11 max-sm:[&_[data-segment]]:px-2" :model-value="mode" :options="MODE_OPTIONS" label="代理模式" :disabled="!coreVersion || modeSaving" @update:model-value="changeMode" />
          <div class="ml-auto flex shrink-0 items-center gap-2 max-sm:gap-1">
            <MihomoCoreActions :agent-id="agentId" :disabled="!coreVersion && !statusError" @changed="reloadAll" @status-change="coreActionStatus = $event" />
            <Button variant="outline" class="min-h-11 max-sm:size-11 max-sm:gap-0" aria-label="刷新面板" title="刷新面板" :aria-busy="statusLoading" :disabled="statusLoading" @click="reloadAll">
              <RefreshCw class="size-4" :class="{ 'animate-spin': statusLoading }" /><span class="max-sm:sr-only">刷新</span>
            </Button>
            <Button variant="outline" class="min-h-11 max-sm:size-11 max-sm:gap-0" :aria-label="openingSettings ? '正在打开设置…' : 'Mihomo 设置'" :title="openingSettings ? '正在打开设置…' : 'Mihomo 设置'" :aria-busy="openingSettings" :disabled="openingSettings || !currentDevice" @click="openSettings">
              <Loader2 v-if="openingSettings" class="size-4 animate-spin" /><Settings v-else class="size-4" /><span class="max-sm:sr-only">{{ openingSettings ? '正在打开…' : '设置' }}</span>
            </Button>
          </div>
        </div>
        <p v-if="coreActionStatus" role="status" class="m-0 w-full break-words text-xs text-primary-accent sm:hidden">{{ coreActionStatus }}</p>
        <p v-if="openingSettings" role="status" class="m-0 w-full text-xs text-primary-accent sm:hidden">正在打开设置…</p>
      </template>
    </PageHeader>
    <p v-if="settingsError" role="alert" class="mb-4 break-words text-sm text-destructive-accent">{{ settingsError }}</p>

    <div v-if="devicesError" role="alert" class="mb-4 flex flex-wrap items-center gap-x-3 text-sm text-warning-accent">
      <span>{{ devicesError }}</span>
      <Button variant="ghost" class="min-h-11" :disabled="devicesLoading" @click="loadDevices">重试设备列表</Button>
    </div>

    <SectionCard v-if="devices && !devices.length">
      <EmptyState :icon="Server" title="暂无 Mihomo Agent" description="接入 Mihomo Agent 后，即可在这里查看概览、切换策略组、查看连接、规则和日志。">
        <Button class="min-h-11" @click="router.push('/agents')">前往 Agent 接入</Button>
      </EmptyState>
    </SectionCard>
    <SectionCard v-else-if="!agentId" :padded="false"><LoadingRows :rows="3" /></SectionCard>
    <template v-else>
      <div class="mb-5 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 text-[12.5px] text-muted-foreground max-sm:mb-2 max-sm:gap-x-3 max-sm:gap-y-1">
        <StatusDot v-if="currentDevice?.status === 'offline'" tone="muted" label="Agent 离线" />
        <StatusDot v-else-if="coreVersion" tone="success" label="核心运行中" />
        <StatusDot v-else-if="statusError" tone="warning" label="无法连接核心" />
        <span v-if="currentDevice?.host" class="min-w-0 break-all font-mono">{{ currentDevice.host }}</span>
        <span v-if="coreVersion" class="min-w-0 break-all font-mono">Mihomo {{ coreVersion }}</span>
      </div>

      <SectionCard v-if="statusError && !coreVersion && tab !== 'logs'">
        <EmptyState :icon="TriangleAlert" title="暂时无法连接 Mihomo" :description="statusError">
          <Button variant="outline" class="min-h-11" :disabled="statusLoading" @click="reloadAll">重试</Button>
          <Button variant="outline" class="min-h-11" @click="changeTab('logs')">查看日志</Button>
          <Button v-if="upgradeRequired" class="min-h-11" @click="router.push('/agents')">前往升级 Agent</Button>
        </EmptyState>
      </SectionCard>
      <!-- 子标签首次打开后保持挂载，切回保留测速结果和连接记录；换设备时重建，只挂载当前标签 -->
      <Tabs v-else :key="agentId" keep-visited :model-value="tab" class="min-w-0" @update:model-value="changeTab(String($event))">
        <TabsList class="mb-4 min-h-11 max-w-full overflow-x-auto max-sm:mb-0">
          <TabsTrigger value="overview" class="min-h-11 px-4">概览</TabsTrigger>
          <TabsTrigger value="proxies" class="min-h-11 px-4">策略</TabsTrigger>
          <TabsTrigger value="connections" class="min-h-11 px-4">连接</TabsTrigger>
          <TabsTrigger value="rules" class="min-h-11 px-4">规则</TabsTrigger>
          <TabsTrigger value="logs" class="min-h-11 px-4">日志</TabsTrigger>
          <TabsTrigger value="stats" class="min-h-11 px-4">统计</TabsTrigger>
        </TabsList>
        <TabsContent value="overview"><MihomoOverview :key="agentId" :agent-id="agentId" :agent-name="currentDevice?.name" :reload-token="reloadToken" /></TabsContent>
        <TabsContent value="stats"><MihomoStatsTab :key="agentId" :agent-id="agentId" :reload-token="reloadToken" /></TabsContent>
        <TabsContent value="proxies"><MihomoProxies :key="agentId" :agent-id="agentId" :reload-token="reloadToken" /></TabsContent>
        <TabsContent value="connections"><MihomoConnections :key="agentId" :agent-id="agentId" /></TabsContent>
        <TabsContent value="rules"><MihomoRules :key="agentId" :agent-id="agentId" :reload-token="reloadToken" /></TabsContent>
        <TabsContent value="logs"><MihomoLogs :key="agentId" :agent-id="agentId" :reload-token="reloadToken" /></TabsContent>
      </Tabs>
    </template>
    <MihomoSettings v-if="settingsEditor" :open="true" :profile-id="settingsEditor.profileId" :preferred-agent-id="settingsEditor.agentId" :context-label="settingsEditor.label"
      @update:open="value => { if (!value) settingsEditor = null }" />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Loader2, RefreshCw, Server, Settings, TriangleAlert } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import Segmented from '@/components/common/Segmented.vue'
import MihomoOverview from '@/components/mihomo/MihomoOverview.vue'
import MihomoStatsTab from '@/components/mihomo/MihomoStatsTab.vue'
import MihomoProxies from '@/components/mihomo/MihomoProxies.vue'
import MihomoConnections from '@/components/mihomo/MihomoConnections.vue'
import MihomoRules from '@/components/mihomo/MihomoRules.vue'
import MihomoLogs from '@/components/mihomo/MihomoLogs.vue'
import MihomoCoreActions from '@/components/mihomo/MihomoCoreActions.vue'
import { agentApi, mihomoPanelApi, profileApi } from '@/api'
import { loadAgentSummaries } from '@/lib/agentSummaries'
import { notify } from '@/lib/feedback'
import { panelError, type MihomoMode } from '@/types/mihomo'
import type { Agent } from '@/types'

const MihomoSettings = defineAsyncComponent(() => import('@/components/generate/MihomoSettings.vue'))
type Device = Pick<Agent, 'id' | 'name' | 'host' | 'status'>
const STORAGE_KEY = 'configflow:mihomo-panel:agent'
const TABS = ['overview', 'proxies', 'connections', 'rules', 'logs', 'stats']
const MODE_OPTIONS: Array<{ value: MihomoMode; label: string }> = [
  { value: 'rule', label: '规则' },
  { value: 'global', label: '全局' },
  { value: 'direct', label: '直连' }
]

const route = useRoute()
const router = useRouter()
const devices = ref<Device[] | null>(null)
const devicesLoading = ref(false)
const devicesError = ref('')
const agentId = computed(() => typeof route.query.agent === 'string' ? route.query.agent : '')
const currentDevice = computed(() => devices.value?.find(device => device.id === agentId.value))
const tab = computed(() => TABS.includes(String(route.query.tab)) ? String(route.query.tab) : 'overview')

const coreVersion = ref('')
const mode = ref<MihomoMode>('rule')
const modeSaving = ref(false)
const statusLoading = ref(false)
const statusError = ref('')
const upgradeRequired = ref(false)
const reloadToken = ref(0)
const settingsEditor = ref<{ profileId: string; agentId: string; label: string } | null>(null)
const openingSettings = ref(false)
const coreActionStatus = ref('')
const settingsError = ref('')
let settingsRequest = 0
let disposed = false

async function openSettings() {
  if (openingSettings.value || !currentDevice.value) return
  const request = ++settingsRequest
  const id = agentId.value
  openingSettings.value = true
  settingsError.value = ''
  try {
    // Read the selected device's binding; the sidebar may show another profile.
    // Read fresh bindings without loading/hashing every profile's full config.
    const [agents, profiles] = await Promise.all([agentApi.summaries(), profileApi.list()])
    if (disposed || request !== settingsRequest || id !== agentId.value) return
    const agent = agents.data.find((item: Agent) => item.id === id && (item.service_type || 'mihomo') === 'mihomo')
    if (!agent) throw new Error('这台 Mihomo Agent 已不存在，请刷新设备列表。')
    const defaultProfile = profiles.data.find((item: any) => item.is_default === true)
      || profiles.data.find((item: any) => item.is_default === undefined && item.id === 'default')
    const profileId = agent.profile_id || defaultProfile?.id
    const profile = profiles.data.find((item: any) => item.id === profileId)
    if (!profile) throw new Error('找不到此 Agent 绑定的配置空间，请检查设备绑定后重试。')
    settingsEditor.value = { profileId: profile.id, agentId: id, label: `${profile.name || profile.id} · ${agent.name || id}` }
  } catch (failure) {
    if (!disposed && request === settingsRequest) settingsError.value = panelError(failure, '暂时无法打开设置，请重试。')
  } finally {
    if (request === settingsRequest) openingSettings.value = false
  }
}

function remember(id: string) {
  try { window.localStorage.setItem(STORAGE_KEY, id) } catch { /* Storage can be unavailable in private browsers. */ }
}

function preferred(list: Device[]) {
  let saved: string | null = null
  try { saved = window.localStorage.getItem(STORAGE_KEY) } catch { /* Selection still works without storage. */ }
  return list.find(device => device.id === saved) || list.find(device => device.status === 'online') || list[0]
}

async function loadDevices() {
  devicesLoading.value = true
  devicesError.value = ''
  try {
    const data = await loadAgentSummaries()
    if (!Array.isArray(data)) throw new Error('设备列表格式异常，请稍后重试。')
    devices.value = data.filter((agent: Agent) => agent && (agent.service_type || 'mihomo') === 'mihomo')
    if (!currentDevice.value && devices.value.length) selectDevice(preferred(devices.value).id)
  } catch (failure) {
    devicesError.value = panelError(failure, '暂时无法读取设备列表。')
  } finally {
    devicesLoading.value = false
  }
}

function selectDevice(id: string) {
  if (!id || id === agentId.value) return
  remember(id)
  router.replace({ query: { ...route.query, agent: id } })
}

function changeTab(value: string) {
  router.replace({ query: { ...route.query, tab: value === 'overview' ? undefined : value } })
}

let statusEpoch = 0
async function loadStatus() {
  const id = agentId.value
  if (!id) return
  const epoch = ++statusEpoch
  statusLoading.value = true
  statusError.value = ''
  upgradeRequired.value = false
  try {
    const [version, configs] = await Promise.all([mihomoPanelApi.version(id), mihomoPanelApi.configs(id)])
    if (epoch !== statusEpoch) return
    coreVersion.value = version.data.data?.version || '未知版本'
    mode.value = (configs.data.data?.mode || 'rule').toLowerCase()
  } catch (failure: any) {
    if (epoch !== statusEpoch) return
    coreVersion.value = ''
    statusError.value = panelError(failure, '暂时无法连接 Mihomo。')
    upgradeRequired.value = failure?.response?.data?.code === 'UPGRADE_REQUIRED'
  } finally {
    if (epoch === statusEpoch) statusLoading.value = false
  }
}

async function changeMode(value: MihomoMode) {
  if (value === mode.value) return
  const previous = mode.value
  mode.value = value
  modeSaving.value = true
  try {
    await mihomoPanelApi.setMode(agentId.value, value)
    notify.success(`已切换为${MODE_OPTIONS.find(option => option.value === value)?.label}模式`)
  } catch (failure) {
    mode.value = previous
    notify.error('切换代理模式失败', panelError(failure))
  } finally {
    modeSaving.value = false
  }
}

function reloadAll() {
  reloadToken.value++
  loadStatus()
}

watch(agentId, () => {
  settingsRequest++
  openingSettings.value = false
  settingsError.value = ''
  coreVersion.value = ''
  loadStatus()
})
onMounted(() => {
  loadDevices()
  loadStatus()
})
onUnmounted(() => { disposed = true })
</script>

<style scoped>
@media (width < 640px) {
  .mihomo-panel-header :deep(.cf-page-heading) {
    flex-basis: 100%;
    flex-wrap: nowrap;
    justify-content: space-between;
    gap: var(--cf-sp-2);
  }

  .mihomo-panel-header :deep(.cf-page-actions) {
    width: 100%;
    margin-left: 0;
  }
}
</style>
