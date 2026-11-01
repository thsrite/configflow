<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { Loader2, RefreshCw } from '@lucide/vue'
import { agentApi } from '@/api'
import { notify } from '@/lib/feedback'
import { optionHelp } from '@/lib/optionCopy'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import FormField from '@/components/common/FormField.vue'
import AgentOperationSteps from './AgentOperationSteps.vue'
import AgentStatusRow from './AgentStatusRow.vue'
import type { Agent } from '@/types'
import type { AgentDNSFollowBinding, AgentDNSFollowInput, AgentDNSFollowOptions, AgentDNSFollowState } from '@/types/agentDNSFollow'

const props = defineProps<{ agent: Agent; sourceName?: string; blocked?: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const binding = ref<AgentDNSFollowBinding | null>(props.agent.dns_follow || null)
const state = ref<AgentDNSFollowState | null>(props.agent.dns_follow_state || null)
watch(() => [props.agent.dns_follow, props.agent.dns_follow_state] as const, ([nextBinding, nextState]) => {
  binding.value = nextBinding || null
  state.value = nextState || null
  if (!open.value && nextState?.status !== 'failed') error.value = ''
})
const open = ref(false)
const loading = ref(false)
const action = ref<'save' | 'remove' | 'retry' | ''>('')
const error = ref('')
const options = ref<AgentDNSFollowOptions | null>(null)
const source = ref('')
const upstream = ref('')
const connection = ref<'auto' | 'network'>('auto')
const address = ref('')
const following = computed(() => ['preparing', 'publishing'].includes(state.value?.status || ''))
const busy = computed(() => !!action.value || following.value || !!props.blocked)
const selectedUpstream = computed(() => options.value?.upstreams.find(item => upstreamKey(item) === upstream.value))
const canSave = computed(() => !!options.value && !loading.value && !busy.value && !!selectedUpstream.value
  && options.value.sources.some(item => item.id === source.value)
  && (connection.value === 'auto' || !!address.value.trim()))
const sourceLabel = computed(() => options.value?.sources.find(item => item.id === binding.value?.source_agent_id)?.name || props.sourceName || 'Mihomo')
const shownState = computed<AgentDNSFollowState>(() => state.value || { status: 'waiting', message: '等待 Mihomo 下一次发布成功。' })
const statusLabel = computed(() => ({
  waiting: '等待发布', preparing: '准备同步', publishing: '正在同步', succeeded: '已同步', failed: '同步失败',
})[shownState.value.status])
const statusTone = computed(() => shownState.value.status === 'succeeded' ? 'success'
  : shownState.value.status === 'failed' ? 'danger'
  : following.value || action.value === 'retry' ? 'progress' : 'neutral')
const showNotice = computed(() => following.value || shownState.value.status === 'failed')
const upstreamKey = (item: { tag: string; index: number }) => JSON.stringify([item.tag, item.index])
const suggestedAddress = (id: string) => options.value?.sources.find(item => item.id === id)?.suggested_address || ''
const errorMessage = (failure: any) => failure?.response?.data?.message || failure?.message || '操作失败，请重试。'
let generation = 0
let disposed = false
onUnmounted(() => { disposed = true; generation++ })

watch(source, (next, previous) => {
  if (loading.value) return
  if (!address.value.trim() || address.value.trim() === suggestedAddress(previous)) {
    address.value = suggestedAddress(next)
  }
}, { flush: 'sync' })

function acceptState(data: { binding?: AgentDNSFollowBinding | null; state?: AgentDNSFollowState | null }) {
  if ('binding' in data) binding.value = data.binding || null
  if ('state' in data) state.value = data.state || null
}

async function load() {
  const current = ++generation
  loading.value = true
  options.value = null
  error.value = ''
  try {
    const { data } = await agentApi.getDNSFollow(props.agent.id)
    if (disposed || current !== generation) return
    if (!data.success) throw new Error(data.message || '无法读取 DNS 跟随设置。')
    options.value = data
    acceptState(data)
    source.value = data.binding?.source_agent_id || ''
    upstream.value = data.binding ? upstreamKey({ tag: data.binding.upstream_tag, index: data.binding.upstream_index }) : ''
    connection.value = data.binding?.connection || 'auto'
    address.value = data.binding?.address ?? suggestedAddress(source.value)
  } catch (failure) {
    if (!disposed && current === generation) error.value = errorMessage(failure)
  } finally {
    if (!disposed && current === generation) loading.value = false
  }
}
watch(open, value => {
  if (value) void load()
  else { generation++; loading.value = false }
})

async function run(kind: 'save' | 'remove' | 'retry') {
  if (busy.value || (kind === 'save' && !canSave.value)) return
  action.value = kind
  error.value = ''
  try {
    let response
    if (kind === 'save') {
      const selected = selectedUpstream.value!
      const input: AgentDNSFollowInput = {
        source_agent_id: source.value, upstream_tag: selected.tag, upstream_index: selected.index,
        connection: connection.value,
        ...(connection.value === 'network' ? { address: address.value.trim() } : {}),
      }
      response = await agentApi.setDNSFollow(props.agent.id, input)
    } else if (kind === 'remove') {
      response = await agentApi.removeDNSFollow(props.agent.id)
    } else {
      response = await agentApi.retryDNSFollow(props.agent.id)
    }
    if (disposed) return
    if (response.data.success === false) throw new Error(response.data.message || '操作未完成。')
    acceptState(response.data)
    emit('changed')
    if (kind !== 'retry') open.value = false
    notify.success(kind === 'save' ? '已开启 DNS 跟随，等待 Mihomo 下一次发布' : kind === 'remove' ? '已停止 DNS 跟随，现有配置保留' : '已提交重试，正在确认同步结果')
  } catch (failure) {
    if (disposed) return
    error.value = errorMessage(failure)
    notify.error(kind === 'retry' ? '无法重试 DNS 同步' : '无法更新 DNS 跟随设置', error.value)
  } finally {
    if (!disposed) action.value = ''
  }
}
defineExpose({ showSettings: () => { if (!action.value) open.value = true } })
</script>

<template>
  <section class="min-w-0" data-agent-dns-follow>
    <AgentStatusRow v-if="binding" label="DNS 联动" :status="statusLabel" :tone="statusTone">
      <template #actions>
        <Button variant="ghost" size="sm" class="min-h-11 text-xs" :disabled="!!action" @click="open = true">设置</Button>
      </template>
      <template v-if="showNotice || (error && !open)" #notice>
        <p v-if="showNotice && shownState.message" class="m-0 whitespace-pre-wrap text-xs leading-relaxed [overflow-wrap:anywhere]" :class="shownState.status === 'failed' ? 'text-destructive-accent' : 'text-foreground'">{{ shownState.message }}</p>
        <Button v-if="shownState.status === 'failed'" variant="outline" size="sm" class="mt-2 min-h-11" :disabled="busy" @click="run('retry')">
          <Loader2 v-if="action === 'retry'" class="size-3.5 animate-spin" /><RefreshCw v-else class="size-3.5" />{{ action === 'retry' ? '正在提交…' : '重试同步' }}
        </Button>
        <p v-if="error && !open" role="alert" class="mb-0 mt-2 whitespace-pre-wrap text-xs text-destructive-accent [overflow-wrap:anywhere]">{{ error }}</p>
      </template>
      <div class="min-w-0 space-y-2 text-xs leading-relaxed text-muted-foreground">
        <p class="m-0 [overflow-wrap:anywhere]">跟随 {{ sourceLabel }} DNS</p>
        <p v-if="!showNotice && shownState.message" class="m-0 whitespace-pre-wrap [overflow-wrap:anywhere]">{{ shownState.message }}</p>
        <p v-if="shownState.address" class="m-0 font-mono [overflow-wrap:anywhere]">{{ shownState.address }}</p>
        <AgentOperationSteps :task="shownState" kind="dns-follow" :busy="following || action === 'retry'" embedded :show-current="false" :show-message="false" />
      </div>
    </AgentStatusRow>
    <AgentStatusRow v-else label="DNS 联动" status="未开启" tone="neutral">
      <template #actions>
        <Button variant="ghost" size="sm" class="min-h-11 text-xs" aria-label="跟随 Mihomo DNS" @click="open = true">设置</Button>
      </template>
    </AgentStatusRow>

    <Dialog :open="open" @update:open="value => { if (!action) open = value }">
      <DialogContent class="max-w-[560px]">
        <DialogHeader>
          <DialogTitle>跟随 Mihomo DNS</DialogTitle>
          <DialogDescription>Mihomo 发布成功后，自动更新并发布 {{ agent.name }} 的所选上游。</DialogDescription>
        </DialogHeader>
        <div v-if="loading" role="status" class="flex min-h-24 items-center justify-center gap-2 text-sm text-muted-foreground"><Loader2 class="size-4 animate-spin" />正在读取 DNS 跟随设置…</div>
        <form v-else-if="options" :id="`dns-follow-form-${agent.id}`" class="min-w-0 space-y-4" @submit.prevent="run('save')">
          <fieldset :disabled="busy" class="m-0 min-w-0 space-y-4 border-0 p-0">
            <FormField label="Mihomo 来源" :html-for="`dns-follow-source-${agent.id}`">
              <Select v-model="source" :disabled="busy">
                <SelectTrigger :id="`dns-follow-source-${agent.id}`" class="min-h-11 w-full" aria-label="Mihomo 来源"><SelectValue placeholder="选择 Mihomo Agent" /></SelectTrigger>
                <SelectContent class="max-w-(--reka-select-content-available-width)"><SelectItem v-for="item in options.sources" :key="item.id" :value="item.id" class="min-h-11 whitespace-normal [overflow-wrap:anywhere]">{{ item.name }}</SelectItem></SelectContent>
              </Select>
              <p v-if="!options.sources.length" class="m-0 text-xs text-warning-accent">请先接入一个 Mihomo Agent。</p>
            </FormField>
            <FormField label="OxiDNS 上游" :html-for="`dns-follow-upstream-${agent.id}`" hint="只联动选中的上游，其他查询服务器保持原样。" :help="optionHelp('已配置并发布 OxiDNS，且列表中有可绑定的 UDP / TCP 上游。', '选中这一条后，让其跟随目标 Mihomo DNS。其他上游不被修改；列表为空时先配置并发布 OxiDNS，再重新选择绑定项。')">
              <Select v-model="upstream" :disabled="busy">
                <SelectTrigger :id="`dns-follow-upstream-${agent.id}`" class="min-h-11 w-full" aria-label="OxiDNS 上游"><SelectValue placeholder="选择要跟随的上游" /></SelectTrigger>
                <SelectContent class="max-w-(--reka-select-content-available-width)"><SelectItem v-for="item in options.upstreams" :key="upstreamKey(item)" :value="upstreamKey(item)" class="min-h-11 whitespace-normal [overflow-wrap:anywhere]">{{ item.tag }} · {{ item.index + 1 }} · {{ item.address }}</SelectItem></SelectContent>
              </Select>
              <p v-if="!options.upstreams.length" class="m-0 text-xs text-warning-accent">当前配置没有可绑定的 UDP / TCP 上游，请先配置并发布 OxiDNS。</p>
            </FormField>
            <FormField label="连接方式" :html-for="`dns-follow-connection-${agent.id}`" hint="服务在不同容器或虚拟机中时，指定网络地址。" :help="optionHelp('两服务位于同一 LXC 或三合一容器时，可用自动验证。', '同一 PVE 上的不同 LXC 或虚拟机仍属于不同网络环境，应选指定网络地址。OxiDNS 必须能够访问该地址，不能仅因同一宿主机就选同容器。')">
              <Select v-model="connection" :disabled="busy">
                <SelectTrigger :id="`dns-follow-connection-${agent.id}`" class="min-h-11 w-full" aria-label="连接方式"><SelectValue /></SelectTrigger>
                <SelectContent class="max-w-(--reka-select-content-available-width)">
                  <SelectItem value="auto" class="min-h-11 whitespace-normal">同一 LXC / All-in-one 容器（自动验证）</SelectItem>
                  <SelectItem value="network" class="min-h-11">指定网络地址</SelectItem>
                </SelectContent>
              </Select>
            </FormField>
            <FormField v-if="connection === 'network'" label="Mihomo DNS 地址" :html-for="`dns-follow-address-${agent.id}`" hint="填写 OxiDNS 可访问的地址，端口自动跟随。" :help="optionHelp('两服务通过网络连接，默认 Agent 地址不一定能直接访问。', '默认采用所选 Mihomo Agent 的地址，也可改成 OxiDNS 能访问的地址。DNS 端口会自动跟随 Mihomo，不必在此手动追踪端口变化。')">
              <Input :id="`dns-follow-address-${agent.id}`" v-model="address" class="min-h-11 font-mono" placeholder="192.168.0.3" autocomplete="off" spellcheck="false" />
            </FormField>
          </fieldset>
          <details class="rounded-md border border-border text-xs text-muted-foreground">
            <summary class="min-h-11 cursor-pointer px-3 py-3 font-medium text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">跟随规则</summary>
            <div class="space-y-2 border-t border-border p-3 leading-relaxed">
              <p class="m-0">读取设备已确认生效的 Mihomo dns.listen，首次跟随从下一次发布成功开始。关闭页面后也会继续同步。</p>
              <p class="m-0">请先成功发布一次 OxiDNS。有其他未发布修改时会暂停同步并提示原因；处理后可重试。手动修改或调整所选上游的顺序后，请重新绑定。</p>
              <p class="m-0">自动验证需要两个核心由同一套 All-in-one Agent 管理，且 Agent 为 1.8.24 或更新版本。</p>
            </div>
          </details>
        </form>
        <p v-if="error" role="alert" class="m-0 whitespace-pre-wrap text-sm text-destructive-accent [overflow-wrap:anywhere]">{{ error }}</p>
        <DialogFooter class="flex-wrap gap-2">
          <Button v-if="binding" variant="outline" class="min-h-11 sm:mr-auto" :disabled="busy || loading" @click="run('remove')"><Loader2 v-if="action === 'remove'" class="size-3.5 animate-spin" />停止跟随</Button>
          <Button variant="outline" class="min-h-11" :disabled="!!action" @click="open = false">关闭</Button>
          <Button v-if="!loading && !options" variant="outline" class="min-h-11" @click="load">重新读取</Button>
          <Button v-else :form="`dns-follow-form-${agent.id}`" type="submit" class="min-h-11" :disabled="!canSave"><Loader2 v-if="action === 'save'" class="size-3.5 animate-spin" />{{ binding ? '保存跟随设置' : '开启自动同步并发布' }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>
