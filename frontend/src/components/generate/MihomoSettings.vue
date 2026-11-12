<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { customConfigApi, type PublishGuard } from '@/api'
import { notify } from '@/lib/feedback'
import YamlEditor from '@/components/YamlEditor.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { Button } from '@/components/ui/button'
import SettingsWorkspace from './SettingsWorkspace.vue'
import SettingsPublishBar from './SettingsPublishBar.vue'
import MihomoSyncInterval from './MihomoSyncInterval.vue'
import MihomoIPv6Settings from './MihomoIPv6Settings.vue'
import MihomoBaseSettings, { type MihomoBaseServerContext } from './MihomoBaseSettings.vue'
import MihomoEasyTierSettings from './MihomoEasyTierSettings.vue'
import { defaultEasyTier, easyTierKey, readEasyTier, type EasyTierSettings } from '@/lib/mihomoEasyTier'
import { isRefreshInterval } from '@/lib/subscriptionRefresh'

const props = defineProps<{ open: boolean; profileId: string; contextLabel: string; preferredAgentId?: string }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
const content = ref('')
const savedContent = ref('')
const defaultConfig = ref('')
const networkBusy = ref(false)
const ipv6Validation = ref({ invalid: false, dirty: false })
const baseValidation = ref({ invalid: false, dirty: false })
const baseContext = ref<MihomoBaseServerContext | null>(null)
const easyTier = ref<EasyTierSettings>(defaultEasyTier())
const savedEasyTier = ref(easyTierKey(defaultEasyTier()))
const easyTierInvalid = ref(false)
// A confirmed publish changes what the IPv6 steps read from the device.
const ipv6Published = ref(0)
const syncInterval = ref<number | string>(3600)
const savedSyncInterval = ref(3600)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const dirty = computed(() => content.value !== savedContent.value || syncInterval.value !== savedSyncInterval.value || ipv6Validation.value.dirty || baseValidation.value.dirty
  || easyTierKey(easyTier.value) !== savedEasyTier.value)
const invalid = computed(() => ipv6Validation.value.invalid || baseValidation.value.invalid || easyTierInvalid.value)
let session = 0
let disposed = false
const current = (request: number) => !disposed && props.open && session === request

async function load() {
  const request = ++session
  loading.value = true
  saving.value = false
  loadError.value = ''
  content.value = savedContent.value = ''
  defaultConfig.value = ''
  baseContext.value = null
  easyTier.value = defaultEasyTier()
  savedEasyTier.value = easyTierKey(easyTier.value)
  try {
    const response = await customConfigApi.getMihomo(props.profileId)
    if (current(request)) {
      content.value = savedContent.value = response.data.config || ''
      defaultConfig.value = response.data.default_config || ''
      baseContext.value = response.data.base_settings ?? null
      syncInterval.value = savedSyncInterval.value = response.data.provider_update_interval ?? 3600
      easyTier.value = readEasyTier(response.data.easytier)
      savedEasyTier.value = easyTierKey(easyTier.value)
    }
  } catch (failure: any) {
    if (current(request)) loadError.value = failure?.response?.data?.message || failure?.message || '基础配置读取失败，请重试。'
  } finally {
    if (current(request)) loading.value = false
  }
}

async function save(close = false): Promise<PublishGuard | null> {
  if (!props.open || disposed || loading.value || loadError.value || saving.value || networkBusy.value || invalid.value) return null
  const request = session
  const submitted = content.value
  const interval = syncInterval.value
  const easytier = JSON.parse(JSON.stringify(easyTier.value)) as EasyTierSettings
  if (!isRefreshInterval(interval)) {
    notify.warning('订阅同步间隔需为 0 或 60 ~ 604800 秒的整数')
    return null
  }
  saving.value = true
  try {
    // Sent only when edited, so saving the YAML alone never rewrites the stored EasyTier setting.
    const easytierChanged = easyTierKey(easytier) !== savedEasyTier.value
    const response = await customConfigApi.saveMihomo({ config: submitted, provider_update_interval: interval, ...(easytierChanged ? { easytier } : {}) }, props.profileId)
    if (!current(request)) return null
    savedContent.value = submitted
    savedSyncInterval.value = interval
    savedEasyTier.value = easyTierKey(easytier)
    notify.success('基础配置草稿已保存')
    if (close) emit('update:open', false)
    return response.data.publish_guard ?? null
  } catch (failure: any) {
    if (current(request)) notify.error('保存基础配置失败', failure?.response?.data?.message || failure?.message)
    return null
  } finally {
    if (current(request)) saving.value = false
  }
}

watch(() => [props.open, props.profileId] as const, () => {
  if (props.open) void load()
  else session++
}, { immediate: true, flush: 'sync' })
onUnmounted(() => { disposed = true })
</script>

<template>
  <SettingsWorkspace :open="open" title="Mihomo 基础配置" :dirty="dirty" :busy="saving || networkBusy"
    :save-disabled="loading || Boolean(loadError) || invalid" save-label="保存草稿" back-label="返回 Mihomo 面板" size="wide"
    description="设置端口、DNS、TUN 等基础选项。生成时会合并此配置空间的节点、策略组和规则。请使用 YAML 格式；留空并保存可恢复默认基础配置。"
    @update:open="emit('update:open', $event)" @save="save()">
    <p class="m-0 shrink-0 break-words border-b border-border px-4 py-2 text-xs text-muted-foreground sm:px-5">{{ contextLabel }}</p>
    <LoadingRows v-if="loading" :rows="5" />
    <div v-else-if="loadError" role="alert" class="p-4 text-sm text-destructive-accent">
      <p class="mb-3 break-words">{{ loadError }}</p>
      <Button variant="outline" class="min-h-11" @click="load">重新读取</Button>
    </div>
    <div v-else :inert="saving" class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">
      <MihomoSyncInterval v-model="syncInterval" :inert="networkBusy" />
      <MihomoIPv6Settings v-model="content" :open="open" :profile-id="profileId" :preferred-agent-id="preferredAgentId"
        :default-config="defaultConfig" :dirty="dirty" :disabled="saving" @busy="networkBusy = $event" @validation="ipv6Validation = $event"
        :refresh-key="ipv6Published" />
      <MihomoEasyTierSettings v-model="easyTier" :disabled="saving" :inert="networkBusy" @validation="easyTierInvalid = $event" />
      <MihomoBaseSettings v-model="content" :open="open" :saved-content="savedContent" :default-config="defaultConfig" :context="baseContext"
        :disabled="saving" :inert="networkBusy" @validation="baseValidation = $event" />
      <div :inert="networkBusy" class="min-h-80 flex-1 p-4 md:p-5">
        <YamlEditor v-model="content" class="h-full min-h-0" placeholder="输入 Mihomo 基础 YAML 配置，例如 mixed-port、dns、tun。" />
      </div>
    </div>
    <template #footer="{ disabled }">
      <SettingsPublishBar :open="open" :profile-id="profileId" :preferred-agent-id="preferredAgentId" service-type="mihomo" :disabled="disabled"
        :save="() => save(false)" @save-draft="save()"
        @published="ipv6Published++" />
    </template>
  </SettingsWorkspace>
</template>
