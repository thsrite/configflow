<template>
  <SectionCard data-router-rule-sync :title="`规则同步 · ${name}`" description="定时同步已发布规则的 IP 变化，保留手工路由。" :help="syncHelp">
    <template #actions>
      <Button variant="outline" class="min-h-11" :disabled="loading || saving || disabled" @click="load"><RefreshCw class="size-4" />刷新状态</Button>
      <Button variant="outline" class="min-h-11" :disabled="saving || disabled || running" @click="emit('preview')">预览发布</Button>
      <Button class="min-h-11" :disabled="!settings?.auto_rule_sync || !state?.ready || state.status === 'paused' || running || saving || loading || disabled" @click="syncNow">
        <Loader2 v-if="running" class="size-4 animate-spin" /><RefreshCw v-else class="size-4" />{{ running ? '同步中' : '立即同步' }}
      </Button>
    </template>
    <LoadingRows v-if="loading && !settings" :rows="3" />
    <div v-if="settings" class="grid min-w-0 gap-4 text-[13px] leading-relaxed">
      <div class="flex flex-wrap items-center justify-between gap-4 border-b border-border pb-4">
        <div class="flex min-h-11 items-center gap-3">
          <Switch :id="`rule-sync-${id}`" :model-value="settings.auto_rule_sync" :disabled="saving || loading || disabled" @update:model-value="value => save({ ...settings!, auto_rule_sync: Boolean(value) })" />
          <Label :for="`rule-sync-${id}`" class="flex min-h-11 items-center text-[13px] leading-relaxed">自动同步规则到 ROS</Label>
        </div>
        <div class="flex items-center gap-3 max-sm:w-full">
          <Label :for="`rule-sync-interval-${id}`" class="whitespace-nowrap text-[13px] font-normal text-muted-foreground">检查频率</Label>
          <Select :model-value="String(settings.rule_sync_interval)" :disabled="saving || loading || disabled" @update:model-value="value => save({ ...settings!, rule_sync_interval: Number(value) })">
            <SelectTrigger :id="`rule-sync-interval-${id}`" class="min-h-11 w-36 text-[13px] max-sm:flex-1"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem v-for="item in intervals" :key="item.value" :value="String(item.value)">{{ item.label }}</SelectItem></SelectContent>
          </Select>
        </div>
      </div>
      <div v-if="state" class="grid gap-2" role="status" aria-live="polite">
        <div :class="['flex items-center gap-2 font-medium', tone]">
          <span class="size-2 shrink-0 rounded-full bg-current" aria-hidden="true" />
          <span>{{ statusLabel }}</span>
        </div>
        <p class="m-0 break-words text-[13px] leading-relaxed text-muted-foreground">{{ statusNote }}</p>
      </div>
      <dl v-if="state && (state.ready || state.checked_at)" class="m-0 grid grid-cols-2 gap-x-4 gap-y-4 border-t border-border pt-4 sm:grid-cols-2 xl:grid-cols-4">
        <div class="grid gap-1"><dt class="text-muted-foreground">最近检查</dt><dd class="m-0 num">{{ time(state.checked_at) }}</dd></div>
        <div class="grid gap-1"><dt class="text-muted-foreground">规则内容更新</dt><dd class="m-0 num">{{ time(state.updated_at) }}</dd></div>
        <div class="grid gap-1"><dt class="text-muted-foreground">最近同步到 ROS</dt><dd class="m-0 num">{{ time(state.synced_at) }}</dd></div>
        <div class="grid gap-1"><dt class="text-muted-foreground">上次同步变更</dt><dd class="m-0 num">新增 {{ state.added }} · 删除 {{ state.removed }}</dd></div>
      </dl>
      <div v-if="state?.rules.length" class="cf-table-wrap">
        <table class="cf-table min-w-[660px]">
          <thead><tr><th>规则集</th><th class="text-right">可同步的 IP</th><th class="text-right">域名</th><th class="text-right">其他条目</th><th>最近读取版本</th><th>ROS 已同步版本</th></tr></thead>
          <tbody><tr v-for="rule in state.rules" :key="rule.id"><td>{{ rule.name }}<span v-if="rule.source_type !== 'url'" class="ml-2 text-xs text-muted-foreground">本地</span></td><td class="text-right num">{{ rule.ip_count }}</td><td class="text-right num">{{ rule.domain_count }}</td><td class="text-right num">{{ rule.ignored_count }}</td><td class="font-mono text-xs">{{ rule.version }}</td><td class="font-mono text-xs">{{ rule.synced_version || '—' }}</td></tr></tbody>
        </table>
      </div>
      <details class="text-xs leading-relaxed text-muted-foreground">
        <summary class="min-h-11 cursor-pointer content-center rounded-sm py-2 outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">同步范围与暂停条件</summary>
        <div class="space-y-2 pb-2"><p v-for="note in syncDetails" :key="note" class="m-0">{{ note }}</p></div>
      </details>
      <details v-if="logEntries.length" class="group/sync-log overflow-hidden rounded-xl border border-border">
        <summary class="flex min-h-11 cursor-pointer list-none items-center gap-2 px-4 py-3 hover:bg-muted/50 [&::-webkit-details-marker]:hidden">
          <ChevronRight class="size-4 shrink-0 text-muted-foreground group-open/sync-log:rotate-90" aria-hidden="true" />
          <span class="font-medium">上次同步明细</span>
          <span class="ml-auto whitespace-nowrap text-muted-foreground"><span class="num">{{ logEntries.length }}</span> 条</span>
        </summary>
        <div class="max-h-80 overflow-y-auto overscroll-contain border-t border-border" role="region" aria-label="上次同步明细" tabindex="0">
          <table class="cf-table table-fixed">
            <caption class="sr-only">上次同步的 IP 变更记录</caption>
            <thead class="sticky top-0 bg-card">
              <tr><th scope="col" class="w-24">变更</th><th scope="col">IP / 网段</th></tr>
            </thead>
            <tbody>
              <tr v-for="(entry, index) in logEntries" :key="index">
                <td>
                  <span :class="['inline-flex items-center gap-1.5 whitespace-nowrap', entry.tone]">
                    <component :is="entry.icon" class="size-3.5 shrink-0" aria-hidden="true" />{{ entry.label }}
                  </span>
                </td>
                <td :class="entry.isAddress ? 'break-all font-mono' : 'break-words [overflow-wrap:anywhere]'">{{ entry.content }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="state && state.added + state.removed > logEntries.length" class="m-0 border-t border-border px-4 py-3 text-[13px] leading-relaxed text-muted-foreground">
          本次共 <span class="num">{{ state.added + state.removed }}</span> 项变更，这里显示最后 <span class="num">{{ logEntries.length }}</span> 项。
        </p>
      </details>
    </div>
    <p v-if="error" role="alert" class="mt-4 mb-0 break-words text-[13px] leading-relaxed text-destructive-accent">{{ error }}</p>
  </SectionCard>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ChevronRight, Info, Loader2, Minus, Plus, RefreshCw } from '@lucide/vue'
import SectionCard from '@/components/common/SectionCard.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { routerApi, type RouterRuleSyncSettings, type RouterRuleSyncState } from '@/api'
import { notify } from '@/lib/feedback'
import { optionHelp } from '@/lib/optionCopy'
import { stripTrailingPunctuation } from '@/lib/text'
import { beginScopedRequest, endScopedRequest } from '@/profileContext'
const props = defineProps<{ id: string; name: string; profile: string; disabled?: boolean }>()
const emit = defineEmits<{ preview: []; busy: [value: boolean] }>()
const settings = ref<RouterRuleSyncSettings | null>(null), state = ref<RouterRuleSyncState | null>(null)
const loading = ref(false), saving = ref(false), error = ref('')
const running = computed(() => ['running', 'queued'].includes(state.value?.status || ''))
const logEntries = computed(() => (state.value?.log || []).map(item => {
  const match = item.status === 'ok' && /^(新增|删除)\s+([\d.]+(?:\/\d+)?)$/.exec(item.summary)
  if (!match) return { label: '说明', content: item.summary, icon: Info, tone: 'text-muted-foreground', isAddress: false }
  const added = match[1] === '新增'
  return { label: match[1], content: match[2], icon: added ? Plus : Minus,
    tone: added ? 'text-success-accent' : 'text-warning-accent', isAddress: true }
}))
const syncHelp = optionHelp('规则集已选中且成功发布到 ROS，希望自动跟进远程 IP 变化。', '仅同步新增和删除的 IP：静态组更新路由，策略组更新地址列表。域名不转 IP，手工规则不接管；异常时暂停保留旧规则，关闭不删已发布规则。')
const syncDetails = [
  '开启后，按所选频率检查这台 ROS 已选中并成功发布过的远程规则集，只同步其中 IP 的新增和删除。',
  '静态路由规则集更新路由；策略路由规则集更新目的地址列表。域名不会转成 IP 下发，未登记的手工路由不会被自动接管或删除。',
  '规则格式不对，或某个规则集原有的可用 IP 全部消失时，会暂停同步，保留 ROS 上的原规则。',
  '修改来源设备、规则集选择、网关或其他设置后，需要重新「预览发布」。关闭自动同步不会删除已发布的规则。',
  '这里只显示 ROS 的同步结果。Mihomo、OxiDNS 的更新仍按各自的刷新或发布流程进行。',
]
const labels: Record<string, string> = { synced: '已同步', waiting: '等待首次发布', queued: '等待同步', running: '正在同步', paused: '已暂停，需核对', failed: '同步失败' }
const needsAttention = computed(() => ['paused', 'failed'].includes(state.value?.status || ''))
const statusLabel = computed(() => running.value || needsAttention.value || settings.value?.auto_rule_sync
  ? labels[state.value?.status || 'waiting'] || '待核对'
  : '自动同步已关闭')
const tone = computed(() => state.value?.status === 'failed' ? 'text-destructive-accent' : needsAttention.value ? 'text-warning-accent' : !settings.value?.auto_rule_sync && !running.value
  ? 'text-muted-foreground' : state.value?.status === 'synced' ? 'text-success-accent' : 'text-foreground')
const statusNote = computed(() => {
  const current = state.value
  if (!current) return ''
  // A paused sync resumes only after a confirmed release records a new baseline.
  if (current.status === 'paused' && !current.message.includes('预览发布')) {
    return `${stripTrailingPunctuation(current.message)}。处理后点「预览发布」并确认，重新建立同步基准。`
  }
  if (needsAttention.value || running.value) return current.message
  if (!current.ready) {
    if (current.message.startsWith('本次发布未建立自动同步基准：')) {
      return current.message.replace('本次发布未建立自动同步基准：', '暂时无法自动同步：')
    }
    return settings.value?.auto_rule_sync
      ? '先点「预览发布」，确认并成功发布一次，之后才能自动同步。'
      : '先点「预览发布」，确认并成功发布一次，再开启自动同步。'
  }
  if (!settings.value?.auto_rule_sync) return '已停止自动检查，ROS 上已发布的规则会保留。'
  if (current.message === '已建立成功发布的同步基准') return '已记住所选规则，后续会按设置的频率检查并同步 IP 变化。'
  if (current.message === '远程规则已核对，ROS 已同步') return '已检查远程规则，IP 变更已同步到 ROS。'
  return current.message
})
const intervals = [15, 30, 60, 180, 360, 720, 1440].map(value => ({ value, label: value < 60 ? `每 ${value} 分钟` : `每 ${value / 60} 小时` }))
const time = (value?: string | null) => value ? new Date(value).toLocaleString() : '暂无记录'
const message = (reason: any) => reason?.response?.data?.message || reason?.message || '操作失败，请重试'
let epoch = 0, timer: ReturnType<typeof setTimeout> | undefined
function schedule() {
  clearTimeout(timer)
  if (running.value) timer = setTimeout(() => { if (!document.hidden) void load() }, 2000)
}
async function load() {
  const current = ++epoch
  clearTimeout(timer); loading.value = true; error.value = ''
  try {
    const { data } = await routerApi.ruleSync(props.id, props.profile)
    if (current !== epoch) return
    settings.value = data.settings; state.value = data.sync; schedule()
  } catch (reason) { if (current === epoch) error.value = message(reason) }
  finally { if (current === epoch) loading.value = false }
}
async function mutate(action: () => ReturnType<typeof routerApi.ruleSync>, success: string) {
  if (saving.value || props.disabled) return
  const current = ++epoch
  clearTimeout(timer); loading.value = false; saving.value = true; error.value = ''
  emit('busy', true); beginScopedRequest()
  try {
    const { data } = await action()
    if (current !== epoch) return
    settings.value = data.settings; state.value = data.sync
    notify.success(success); schedule()
  } catch (reason) { if (current === epoch) error.value = message(reason) }
  finally { endScopedRequest(); if (current === epoch) { saving.value = false; emit('busy', false) } }
}
function save(value: RouterRuleSyncSettings) { return mutate(() => routerApi.saveRuleSync(props.id, value, props.profile), '同步设置已保存') }
function syncNow() { return mutate(() => routerApi.startRuleSync(props.id, props.profile), '已开始检查远程规则') }
function visible() { if (!document.hidden && running.value && !saving.value) void load() }
document.addEventListener('visibilitychange', visible)
// A release from the page (disabled while it runs) may have recorded a new baseline.
watch(() => props.disabled, (now, before) => { if (before && !now && !saving.value) void load() })
watch(() => [props.id, props.profile], () => { settings.value = null; state.value = null; saving.value = false; emit('busy', false); void load() }, { immediate: true })
onBeforeUnmount(() => { epoch++; clearTimeout(timer); document.removeEventListener('visibilitychange', visible); emit('busy', false) })
</script>
