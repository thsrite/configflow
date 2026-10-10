<template>
  <div class="flex flex-col gap-4">
    <SectionCard
      title="区域检测"
      description="在推送给 Agent 的 Mihomo 配置里加一个只监听本机的检测入口，Agent 经它切换到不同节点访问服务和域名，判断是否受地区限制。不影响真实流量，手机等订阅的配置不变。"
      :icon="Globe"
    >
      <div v-if="!region" class="py-2"><LoadingRows :rows="2" /></div>
      <div v-else class="flex flex-col gap-3">
        <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
          <label class="flex items-center gap-2 text-[13px]">
            <Switch
              :model-value="region.enabled"
              :disabled="saving"
              aria-label="开启区域检测"
              @update:model-value="value => saveSettings({ region_check_enabled: Boolean(value) })"
            />
            开启区域检测
          </label>
          <label class="flex items-center gap-2 text-[13px]">
            <span class="text-muted-foreground">入口端口</span>
            <Input
              v-model="portDraft"
              inputmode="numeric"
              class="h-8 w-[92px] bg-background/50 font-mono text-[12.5px]"
              aria-label="检测入口端口"
              :disabled="saving"
              @keydown.enter="savePort"
              @blur="savePort"
            />
          </label>
          <div class="ml-auto flex items-center gap-2">
            <Button :disabled="!region.enabled || busy" @click="emit('check', { services: true })">
              <Loader2 v-if="busy" class="size-4 animate-spin" />
              <ScanSearch v-else class="size-4" />
              检测服务
            </Button>
          </div>
        </div>
        <InfoNote v-if="region.enabled && !region.ready">
          <p>需要 Agent {{ region.min_agent_version }} 及以上并开启域名发现；开启区域检测后还要<strong class="text-foreground">部署一次配置</strong>，检测入口才会出现在 Agent 上的 Mihomo 里。</p>
        </InfoNote>
        <p v-else-if="region.result" class="m-0 text-[12px] text-muted-foreground">
          最近一次检测：{{ relativeTime(region.result.checked_at) }}<template v-if="region.agent"> · 由 {{ region.agent.name }} 执行</template>
        </p>
      </div>
    </SectionCard>

    <SectionCard v-if="region?.result" title="服务解锁" description="列为直连、当前策略组（测其当前选中的节点）与每个地区的代表节点；列头括号里是实际出口地区。悬停单元格查看判断依据。" :padded="false">
      <div class="overflow-x-auto" role="region" aria-label="服务解锁矩阵" tabindex="0">
        <table class="cf-table">
          <thead>
            <tr>
              <th>服务</th>
              <th v-for="target in region.result.targets" :key="target.name">
                <span class="block max-w-[140px] truncate" :title="target.name">{{ targetLabel(target) }}</span>
                <span
                  class="block font-mono text-[11px] font-normal"
                  :class="exitMismatch(target) ? 'text-warning-accent' : 'text-muted-foreground'"
                  :title="exitTitle(target)"
                >{{ exitText(target) }}</span>
              </th>
              <th class="cf-table__right">走向</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in region.services" :key="item.id">
              <td class="font-medium whitespace-nowrap">{{ item.name }}</td>
              <td v-for="target in region.result.targets" :key="target.name">
                <template v-if="cell(target.name, item.id)">
                  <span :class="['chip', STATUS_CLASSES[cell(target.name, item.id)!.status]]" :title="cellTitle(cell(target.name, item.id)!)">
                    {{ STATUS_LABELS[cell(target.name, item.id)!.status] }}<template v-if="cell(target.name, item.id)!.region"> · {{ cell(target.name, item.id)!.region }}</template>
                  </span>
                </template>
                <span v-else class="text-[12px] text-muted-foreground" :title="target.error || ''">—</span>
              </td>
              <td class="cf-table__right">
                <Button size="sm" variant="ghost" @click="openRoute('service', item.id, item.name)">指定走向</Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <SectionCard v-if="domainEntries.length" title="域名地区差异" description="在域名列表里点「检测地区差异」的结果。这是启发式判断：根据状态码 451、受限提示文字和跳转到地区不可用页面来识别。" :padded="false">
      <ul class="m-0 flex list-none flex-col divide-y divide-border p-0">
        <li v-for="[host, entry] in domainEntries" :key="host" class="flex flex-col gap-2 px-5 py-3 max-md:px-4">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-mono text-[13px]">{{ host }}</span>
            <span :class="['chip', VERDICT_CLASSES[entry.verdict]]">{{ VERDICT_LABELS[entry.verdict] }}</span>
            <span class="text-[11.5px] text-muted-foreground">{{ relativeTime(entry.checked_at) }}</span>
            <Button size="sm" variant="ghost" class="ml-auto" @click="openRoute('domain', host, host)">指定走向</Button>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <span
              v-for="(result, name) in entry.targets"
              :key="name"
              :class="['chip', DOMAIN_STATE_CLASSES[result.state]]"
              :title="`${result.detail}${result.final_url ? `\n${result.final_url}` : ''}`"
            >{{ name }} · {{ DOMAIN_STATE_LABELS[result.state] }}</span>
          </div>
        </li>
      </ul>
    </SectionCard>

    <SectionCard v-if="regionalRulesets.length" title="地区规则集" description="「指定走向」写入的规则集，按策略组区分，位于默认代理规则集之前。" :padded="false">
      <ul class="m-0 flex list-none flex-col divide-y divide-border p-0">
        <li v-for="[policy, info] in regionalRulesets" :key="policy" class="flex flex-wrap items-center gap-2 px-5 py-2.5 text-[13px] max-md:px-4">
          <span class="font-medium">{{ policy }}</span>
          <span class="text-muted-foreground">{{ info?.name || '规则集已删除' }}</span>
          <span v-if="info && !info.active" class="chip chip-warn">未在当前配置启用</span>
        </li>
      </ul>
    </SectionCard>

    <Dialog v-model:open="routeDialog.open">
      <DialogContent class="max-w-[480px]">
        <DialogHeader>
          <DialogTitle>指定「{{ routeDialog.label }}」的走向</DialogTitle>
          <DialogDescription>
            <template v-if="routeDialog.kind === 'service'">把 {{ serviceDomains(routeDialog.value).join('、') }} 写入所选策略组的规则集。</template>
            <template v-else>把 {{ routeDialog.value }} 写入所选策略组的规则集。</template>
            之后由 Agent 自动刷新生效；第一次使用某个策略组时会新建规则集，需要部署一次。
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-1.5">
          <Label for="route-policy">策略组</Label>
          <Select v-model="routeDialog.policy">
            <SelectTrigger id="route-policy" class="w-full bg-background/50">
              <SelectValue placeholder="选择策略组" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="policy in region?.policies || []" :key="policy" :value="policy">{{ policy }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="routeDialog.open = false">取消</Button>
          <Button :disabled="!routeDialog.policy || routing" @click="submitRoute">
            <Loader2 v-if="routing" class="size-4 animate-spin" />
            写入
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { isAxiosError } from 'axios'
import { Globe, Loader2, ScanSearch } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import InfoNote from '@/components/common/InfoNote.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import api from '@/api'
import { notify } from '@/lib/feedback'
import { relativeTime } from '@/lib/format'

export interface RegionTarget {
  name: string
  kind: 'direct' | 'group' | 'node'
  region_hint: string | null
  exit: { loc: string | null; ip: string | null; error: string | null } | null
  error: string | null
}

interface ServiceResult {
  status: 'available' | 'partial' | 'blocked' | 'unknown'
  region: string | null
  reason: string
  evidence: string[]
}

interface DomainTargetResult {
  state: 'ok' | 'restricted' | 'denied' | 'error'
  detail: string
  final_url: string | null
}

export interface DomainRegionEntry {
  checked_at: string
  verdict: 'suspected' | 'all_restricted' | 'no_difference' | 'unreachable'
  targets: Record<string, DomainTargetResult>
}

export interface RegionPayload {
  enabled: boolean
  port: number
  ready: boolean
  min_agent_version: string
  services: Array<{ id: string; name: string; domains: string[] }>
  result: { checked_at: string; targets: RegionTarget[]; matrix: Record<string, Record<string, ServiceResult>> } | null
  agent: { id: string; name: string } | null
  domains: Record<string, DomainRegionEntry>
  policies: string[]
  regional_rulesets: Record<string, { name: string | null; active: boolean } | null>
}

const props = defineProps<{ region: RegionPayload | null; busy: boolean }>()
const emit = defineEmits<{ check: [payload: { services: boolean; domains?: string[] }]; changed: [] }>()

const STATUS_LABELS: Record<ServiceResult['status'], string> = {
  available: '可用', partial: '部分可用', blocked: '不可用', unknown: '未知'
}
const STATUS_CLASSES: Record<ServiceResult['status'], string> = {
  available: 'chip-ok', partial: 'chip-warn', blocked: 'chip-bad', unknown: ''
}
const VERDICT_LABELS: Record<DomainRegionEntry['verdict'], string> = {
  suspected: '疑似区域限制', all_restricted: '各地区都受限', no_difference: '无地区差异', unreachable: '无法访问'
}
const VERDICT_CLASSES: Record<DomainRegionEntry['verdict'], string> = {
  suspected: 'chip-warn', all_restricted: 'chip-bad', no_difference: 'chip-ok', unreachable: ''
}
const DOMAIN_STATE_LABELS: Record<DomainTargetResult['state'], string> = {
  ok: '正常', restricted: '受限', denied: '拒绝', error: '失败'
}
const DOMAIN_STATE_CLASSES: Record<DomainTargetResult['state'], string> = {
  ok: 'chip-ok', restricted: 'chip-bad', denied: 'chip-warn', error: ''
}

const saving = ref(false)
const routing = ref(false)
const portDraft = ref('')
const routeDialog = reactive({ open: false, kind: 'service' as 'service' | 'domain', value: '', label: '', policy: '' })

watch(() => props.region?.port, port => { portDraft.value = port ? String(port) : '' }, { immediate: true })

const domainEntries = computed(() =>
  Object.entries(props.region?.domains || {}).sort((a, b) => b[1].checked_at.localeCompare(a[1].checked_at))
)
const regionalRulesets = computed(() => Object.entries(props.region?.regional_rulesets || {}))

const errorMessage = (error: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(error) ? error.response?.data?.message || fallback : fallback

const cell = (target: string, service: string) => props.region?.result?.matrix[target]?.[service]
const cellTitle = (result: ServiceResult) => [result.reason, ...result.evidence].filter(Boolean).join('\n')
const targetLabel = (target: RegionTarget) => (target.name === 'DIRECT' ? '直连' : target.name)
const exitText = (target: RegionTarget) => {
  if (target.error) return '检测失败'
  return target.exit?.loc ? `出口 ${target.exit.loc}` : '出口未知'
}
const exitMismatch = (target: RegionTarget) =>
  Boolean(target.kind === 'node' && target.region_hint && target.exit?.loc && target.exit.loc !== target.region_hint)
const exitTitle = (target: RegionTarget) => {
  if (target.error) return target.error
  const parts = [target.exit?.ip ? `出口 IP ${target.exit.ip}` : '']
  if (exitMismatch(target)) parts.push(`节点名显示 ${target.region_hint}，实际出口是 ${target.exit?.loc}`)
  return parts.filter(Boolean).join('\n')
}
const serviceDomains = (id: string) => props.region?.services.find(item => item.id === id)?.domains || []

const saveSettings = async (changes: Record<string, unknown>) => {
  saving.value = true
  try {
    await api.put('/domain-discovery/settings', changes)
    if ('region_check_enabled' in changes) {
      notify.success(changes.region_check_enabled ? '已开启区域检测' : '已关闭区域检测', '部署一次配置后在 Agent 上生效。')
    } else {
      notify.success('已保存', '部署一次配置后在 Agent 上生效。')
    }
    emit('changed')
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
    portDraft.value = props.region?.port ? String(props.region.port) : ''
  } finally {
    saving.value = false
  }
}

const savePort = () => {
  const port = Number(portDraft.value)
  if (!portDraft.value || port === props.region?.port) return
  if (!Number.isInteger(port) || port < 1024 || port > 65535) {
    notify.warning('端口需要在 1024-65535 之间')
    portDraft.value = props.region?.port ? String(props.region.port) : ''
    return
  }
  void saveSettings({ region_probe_port: port })
}

const openRoute = (kind: 'service' | 'domain', value: string, label: string) => {
  Object.assign(routeDialog, { open: true, kind, value, label, policy: '' })
}

const submitRoute = async () => {
  routing.value = true
  try {
    const { data } = await api.post('/domain-discovery/region/route', {
      kind: routeDialog.kind, value: routeDialog.value, policy: routeDialog.policy
    })
    const added = data.added.length
    notify.success(added ? `已写入 ${added} 个域名到「${routeDialog.policy}」` : '这些域名已在规则集中',
      '已部署过该规则集的 Agent 会自动刷新；第一次使用的策略组需要部署一次。')
    for (const warning of data.warnings) notify.warning(warning)
    routeDialog.open = false
    emit('changed')
  } catch (error) {
    notify.error(errorMessage(error, '写入失败'))
  } finally {
    routing.value = false
  }
}
</script>
