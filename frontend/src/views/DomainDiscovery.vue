<template>
  <div>
    <PageHeader title="域名发现">
      <template #actions>
        <Segmented v-model="days" :options="DAY_OPTIONS" label="时间范围" @update:model-value="loadDomains" />
        <Button variant="outline" class="border-border/60 bg-background/40" :disabled="loading" @click="reload">
          <RefreshCw class="size-4" :class="loading && 'animate-spin'" />
          刷新
        </Button>
      </template>
    </PageHeader>

    <div class="mb-4 grid gap-4 lg:grid-cols-2">
      <!-- 采集设备 -->
      <SectionCard title="采集设备" description="开启后，Agent 会把本机 Mihomo 访问过的域名、命中规则和直连失败汇总上报（只含域名，不含网址路径和设备 IP，保留 7 天）。" :icon="Radar">
        <LoadingRows v-if="loading && !loaded" :rows="2" />
        <EmptyState
          v-else-if="!agents.length"
          title="当前配置没有 Mihomo Agent"
          description="在 Agent 页面部署一个 Mihomo Agent，并把它关联到当前配置。"
        >
          <Button variant="outline" size="sm" @click="router.push('/agents')">前往 Agent</Button>
        </EmptyState>
        <ul v-else class="m-0 flex list-none flex-col gap-2 p-0">
          <li
            v-for="agent in agents"
            :key="agent.id"
            class="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg border border-border bg-background/40 px-3 py-2.5"
          >
            <div class="min-w-0 flex-[1_1_160px]">
              <p class="m-0 truncate text-[13px] font-medium text-foreground" :title="agent.name">{{ agent.name || agent.id }}</p>
              <p class="m-0 mt-0.5 text-[11.5px] text-muted-foreground">
                <template v-if="!agent.supported">Agent {{ agent.version || '版本未知' }}，需要升级到 {{ minAgentVersion }}</template>
                <template v-else-if="agent.last_report">
                  {{ relativeTime(agent.last_report) }}上报<span v-if="agent.mihomo_version" class="font-mono"> · Mihomo {{ agent.mihomo_version }}</span>
                  <span v-if="!agent.probe_supported"> · 升级到 {{ probeAgentVersion }} 后可主动探测</span>
                </template>
                <template v-else-if="agent.enabled">等待首次上报</template>
                <template v-else>未开启</template>
              </p>
            </div>
            <StatusDot v-if="agent.enabled" :tone="statusTone(agent.status)" :label="STATUS_LABELS[agent.status] || agent.status" />
            <div class="flex items-center gap-1">
              <Button
                v-if="agent.last_report"
                variant="ghost"
                size="icon"
                class="size-8"
                :aria-label="`清除 ${agent.name} 已采集的数据`"
                title="清除已采集的数据"
                @click="clearAgent(agent)"
              >
                <Eraser class="size-3.5" />
              </Button>
              <Switch
                :model-value="agent.enabled"
                :disabled="togglingAgent === agent.id || (!agent.supported && !agent.enabled)"
                :aria-label="`${agent.enabled ? '关闭' : '开启'} ${agent.name} 的域名发现`"
                @update:model-value="value => toggleAgent(agent, Boolean(value))"
              />
            </div>
          </li>
        </ul>
      </SectionCard>

      <!-- 默认规则集 -->
      <SectionCard title="默认规则集" description="「加入直连 / 加入代理」会把域名追加到这两个规则集。规则集需要在策略规则里启用，并位于兜底规则之前。" :icon="ListPlus">
        <LoadingRows v-if="!settings" :rows="2" />
        <div v-else class="flex flex-col gap-3">
          <div v-for="target in TARGETS" :key="target" class="flex flex-col gap-1.5">
            <Label :for="`ruleset-${target}`" class="text-[12.5px]">{{ TARGET_LABELS[target] }}</Label>
            <div class="flex flex-wrap items-center gap-2">
              <Select
                :model-value="settings[target]?.id || ''"
                :disabled="savingSettings"
                @update:model-value="value => saveRuleset(target, String(value))"
              >
                <SelectTrigger :id="`ruleset-${target}`" class="h-9 min-w-0 flex-[1_1_200px] bg-background/50 text-[13px]">
                  <SelectValue placeholder="未设置" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="item in settings.candidates" :key="item.id" :value="item.id">
                    {{ item.name }}<span class="ml-1.5 font-mono text-[11px] text-muted-foreground">{{ item.behavior }}</span>
                  </SelectItem>
                </SelectContent>
              </Select>
              <span v-if="settings[target] && settings[target]?.problem" class="chip chip-bad">规则集不可写入</span>
              <span v-else-if="settings[target] && !settings[target]?.active" class="chip chip-warn">未在当前配置启用</span>
              <span v-else-if="settings[target]" class="chip chip-ok">
                {{ settings[target]?.references[0]?.policy }}
              </span>
            </div>
          </div>
          <div v-if="missingTargets.length" class="flex flex-wrap items-center gap-2 pt-1">
            <Button variant="outline" size="sm" @click="openInitDialog">
              <Plus class="size-3.5" />
              一键创建{{ missingTargets.length === 2 ? '' : TARGET_LABELS[missingTargets[0]] }}规则集
            </Button>
            <span class="text-[12px] text-muted-foreground">创建空的规则集，并加到兜底规则之前</span>
          </div>

          <div class="mt-1 flex flex-col gap-2.5 border-t border-border pt-3">
            <p class="m-0 text-[12.5px] font-medium text-foreground">主动探测</p>
            <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
              <template v-if="settings.probe_path">
                经 Agent 本机的 Mihomo 分别用直连和 <span class="font-mono text-foreground">{{ settings.probe_path }}</span> 访问域名，判断该走哪条路。
              </template>
              <template v-else>设置默认代理规则集并在当前配置启用后才能探测，代理路径使用它指向的策略组。</template>
            </p>
            <label class="flex items-center justify-between gap-3 text-[13px]">
              <span>
                自动探测新域名
                <span class="block text-[11.5px] text-muted-foreground">每 10 分钟探测一批未覆盖的域名，并复检加入超过 30 天的条目</span>
              </span>
              <Switch
                :model-value="settings.auto_probe"
                :disabled="savingSettings || !settings.probe_path"
                aria-label="自动探测新域名"
                @update:model-value="value => saveAutomation({ auto_probe: Boolean(value) })"
              />
            </label>
            <div class="flex flex-wrap items-center justify-between gap-3 text-[13px]">
              <span>
                自动采纳建议
                <span class="block text-[11.5px] text-muted-foreground">置信度达到阈值的建议直接写入默认规则集，可在「已加入」里撤销</span>
              </span>
              <div class="flex items-center gap-2">
                <Select
                  :model-value="String(settings.auto_apply_min_confidence)"
                  :disabled="savingSettings || !settings.auto_probe"
                  @update:model-value="value => saveAutomation({ auto_apply_min_confidence: Number(value) })"
                >
                  <SelectTrigger class="h-8 w-[92px] bg-background/50 text-[12.5px]" aria-label="自动采纳的置信度阈值">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="choice in settings.confidence_choices" :key="choice" :value="String(choice)">≥ {{ choice }}</SelectItem>
                  </SelectContent>
                </Select>
                <Switch
                  :model-value="settings.auto_apply"
                  :disabled="savingSettings || !settings.auto_probe"
                  aria-label="自动采纳建议"
                  @update:model-value="value => saveAutomation({ auto_apply: Boolean(value) })"
                />
              </div>
            </div>
          </div>
        </div>
      </SectionCard>
    </div>

    <Toolbar v-model:search="search" placeholder="搜索域名…">
      <template #filters>
        <Segmented v-model="view" :options="VIEW_OPTIONS" label="筛选" @update:model-value="loadDomains" />
      </template>
      <template #actions>
        <span v-if="probeJob?.status === 'running'" class="chip chip-sky font-mono" role="status">
          <Loader2 class="size-3 animate-spin" aria-hidden="true" />
          {{ probeJob.kind === 'region' ? '区域检测中' : '探测中' }} {{ probeJob.done }}/{{ probeJob.total || '…' }}
        </span>
        <span v-if="sniffCoverage !== null && view !== 'history' && view !== 'region'" class="chip font-mono" title="有域名的连接占全部连接的比例；过低说明嗅探没有开启或没有生效">
          嗅探覆盖 {{ Math.round(sniffCoverage * 100) }}%
        </span>
        <template v-if="selected.size && view !== 'history' && view !== 'region'">
          <span class="text-[12.5px] text-muted-foreground">已选 {{ selected.size }}</span>
          <template v-if="view !== 'ignored'">
            <Button size="sm" variant="outline" :disabled="probing" @click="probeSelected">
              <ScanSearch class="size-3.5" />
              探测
            </Button>
            <Button v-if="adoptableSelected.length" size="sm" variant="outline" :disabled="applying" @click="adoptSelected">
              <Sparkles class="size-3.5" />
              采纳建议 {{ adoptableSelected.length }}
            </Button>
            <Button size="sm" :disabled="applying" @click="applySelected('proxy')">加入代理</Button>
            <Button size="sm" variant="outline" :disabled="applying" @click="applySelected('direct')">加入直连</Button>
            <Button size="sm" variant="ghost" :disabled="applying" @click="ignoreSelected">忽略</Button>
          </template>
          <Button v-else size="sm" variant="outline" :disabled="applying" @click="unignoreSelected">取消忽略</Button>
        </template>
      </template>
    </Toolbar>

    <RegionPanel
      v-if="view === 'region'"
      :region="region"
      :busy="probing"
      @check="startRegionCheck"
      @changed="loadRegion"
    />

    <template v-else-if="view === 'history'">
      <SectionCard v-if="historyLoading && !history.length" :padded="false"><LoadingRows /></SectionCard>
      <SectionCard v-else-if="!visibleHistory.length" :padded="false">
        <EmptyState title="还没有加入过域名" description="在列表里加入直连或代理后，会记录在这里，可以随时撤销。" :icon="ListPlus" />
      </SectionCard>
      <DataTableShell v-else :footer="`共 ${visibleHistory.length} 条`">
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead>域名</TableHead>
            <TableHead class="w-36">规则集</TableHead>
            <TableHead class="w-40">加入依据</TableHead>
            <TableHead class="w-28">加入时间</TableHead>
            <TableHead>复检</TableHead>
            <TableHead class="w-24 text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="entry in visibleHistory" :key="`${entry.value}:${entry.target}`" :class="entry.recheck?.suggest_remove && 'bg-warning-soft/40'">
            <TableCell class="font-mono text-[13px]">
              {{ entry.value }}
              <span class="ml-1 text-[11px] text-muted-foreground">{{ entry.rule_type }}</span>
            </TableCell>
            <TableCell>
              <span :class="['chip', entry.target === 'proxy' ? 'chip-acc' : 'chip-ok']">{{ TARGET_LABELS[entry.target] }}</span>
              <span v-if="entry.ruleset" class="ml-1.5 text-[11.5px] text-muted-foreground">{{ entry.ruleset }}</span>
            </TableCell>
            <TableCell class="text-[12.5px]">
              {{ entry.source === 'auto' ? '自动采纳' : '手动' }}
              <span v-if="entry.verdict" class="text-muted-foreground"> · {{ VERDICT_LABELS[entry.verdict] || entry.verdict }}<template v-if="entry.confidence"> {{ entry.confidence }}</template></span>
            </TableCell>
            <TableCell class="text-[12.5px] text-muted-foreground">{{ relativeTime(entry.applied_at) }}</TableCell>
            <TableCell class="text-[12.5px]">
              <template v-if="entry.recheck">
                <span v-if="entry.recheck.suggest_remove" class="chip chip-warn">建议移除</span>
                <span v-else class="chip chip-ok">仍然需要</span>
                <span class="ml-1.5 text-muted-foreground">{{ entry.recheck.reason || VERDICT_LABELS[entry.recheck.verdict || ''] }} · {{ relativeTime(entry.recheck.at) }}</span>
              </template>
              <span v-else class="text-muted-foreground">—</span>
            </TableCell>
            <TableCell class="text-right">
              <Button size="sm" variant="ghost" :disabled="applying" @click="undo(entry)">
                <Undo2 class="size-3.5" />
                撤销
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </DataTableShell>
    </template>

    <SectionCard v-else-if="loading && !loaded" :padded="false"><LoadingRows /></SectionCard>
    <SectionCard v-else-if="loadError" :padded="false">
      <EmptyState title="读取失败" :description="loadError" :icon="TriangleAlert">
        <Button variant="outline" size="sm" @click="loadDomains">重试</Button>
      </EmptyState>
    </SectionCard>
    <SectionCard v-else-if="!visibleItems.length" :padded="false">
      <EmptyState :title="emptyTitle" :description="emptyDescription" :icon="Radar" />
    </SectionCard>
    <DataTableShell v-else :footer="`共 ${visibleItems.length} 个域名`">
      <TableHeader>
        <TableRow class="hover:bg-transparent">
          <TableHead class="w-10">
            <Checkbox :model-value="allSelected" aria-label="全选" @update:model-value="toggleAll" />
          </TableHead>
          <TableHead>域名</TableHead>
          <TableHead class="w-24 text-right">连接</TableHead>
          <TableHead class="w-36">直连失败</TableHead>
          <TableHead>当前走向</TableHead>
          <TableHead class="w-40">探测建议</TableHead>
          <TableHead class="w-28">最近出现</TableHead>
          <TableHead class="w-[250px] text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <template v-for="item in visibleItems" :key="item.domain">
          <TableRow>
            <TableCell>
              <Checkbox
                :model-value="selected.has(item.domain)"
                :aria-label="`选择 ${item.domain}`"
                @update:model-value="toggleSelected(item.domain)"
              />
            </TableCell>
            <TableCell class="min-w-[220px]">
              <div class="flex flex-wrap items-center gap-1.5">
                <button
                  type="button"
                  class="inline-flex items-center gap-1 rounded-sm font-mono text-[13px] text-foreground hover:text-primary-accent focus-visible:outline-2 focus-visible:outline-ring"
                  :aria-expanded="expanded.has(item.domain)"
                  :aria-label="`${expanded.has(item.domain) ? '收起' : '展开'} ${item.domain} 的子域`"
                  @click="toggleExpanded(item.domain)"
                >
                  <ChevronRight class="size-3.5 transition-transform" :class="expanded.has(item.domain) && 'rotate-90'" aria-hidden="true" />
                  {{ item.domain }}
                </button>
                <span v-if="item.hosts.length > 1" class="text-[11.5px] text-muted-foreground">{{ item.hosts.length }} 个子域</span>
                <span v-for="tag in item.tags" :key="tag" :class="['chip', TAG_CLASSES[tag]]" :title="tagTitle(item, tag)">{{ TAG_LABELS[tag] }}</span>
              </div>
            </TableCell>
            <TableCell class="num text-right">{{ item.conns }}</TableCell>
            <TableCell>
              <span v-if="item.fails" class="num text-destructive-accent">{{ item.fails }}</span>
              <span v-else class="text-muted-foreground">—</span>
              <span v-if="item.fails" class="ml-1.5 text-[11.5px] text-muted-foreground">{{ failKindsText(item.fail_kinds) }}</span>
            </TableCell>
            <TableCell class="max-w-[280px]">
              <span v-if="item.routes[0]" class="block truncate text-[12.5px]" :title="routeText(item.routes[0])">{{ routeText(item.routes[0]) }}</span>
            </TableCell>
            <TableCell>
              <span
                v-if="item.suggestion"
                :class="['chip', suggestionClass(item.suggestion)]"
                :title="suggestionTitle(item.suggestion)"
              >{{ suggestionText(item.suggestion) }}</span>
              <span v-else class="text-[12px] text-muted-foreground">未探测</span>
            </TableCell>
            <TableCell class="text-[12.5px] text-muted-foreground">{{ relativeTime(item.last_seen) }}</TableCell>
            <TableCell class="text-right whitespace-nowrap">
              <template v-if="view !== 'ignored'">
                <Button size="icon" variant="ghost" class="size-8" :disabled="probing" :aria-label="`探测 ${item.domain}`" title="探测直连与代理" @click="startProbe([item.domain])">
                  <ScanSearch class="size-3.5" />
                </Button>
                <Button
                  v-if="region?.enabled"
                  size="icon"
                  variant="ghost"
                  class="size-8"
                  :disabled="probing"
                  :aria-label="`检测 ${item.domain} 的地区差异`"
                  title="检测地区差异"
                  @click="startRegionCheck({ services: false, domains: [item.domain] })"
                >
                  <Globe class="size-3.5" />
                </Button>
                <Button size="sm" variant="ghost" :disabled="applying" @click="apply([{ value: item.domain, target: 'proxy' }])">加入代理</Button>
                <Button size="sm" variant="ghost" :disabled="applying" @click="apply([{ value: item.domain, target: 'direct' }])">加入直连</Button>
                <Button size="icon" variant="ghost" class="size-8" :disabled="applying" :aria-label="`忽略 ${item.domain}`" title="忽略" @click="ignore([item.domain])">
                  <EyeOff class="size-3.5" />
                </Button>
              </template>
              <Button v-else size="sm" variant="ghost" :disabled="applying" @click="unignore([item.domain])">取消忽略</Button>
            </TableCell>
          </TableRow>
          <TableRow v-if="expanded.has(item.domain)" class="bg-muted/40 hover:bg-muted/40">
            <TableCell />
            <TableCell colspan="7" class="py-2">
              <ul class="m-0 flex list-none flex-col gap-1 p-0">
                <li v-for="host in item.hosts" :key="host.host" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px]">
                  <span class="min-w-0 flex-[1_1_220px] truncate font-mono" :title="host.host">{{ host.host }}</span>
                  <span class="num text-muted-foreground">连接 {{ host.conns }}</span>
                  <span class="num" :class="host.fails ? 'text-destructive-accent' : 'text-muted-foreground'">失败 {{ host.fails }}</span>
                  <span v-if="host.pending_rule" class="chip chip-sky" :title="host.pending_rule.matched_line">已有规则 · {{ host.pending_rule.policy }}</span>
                  <span v-else-if="!host.uncovered" class="chip">已被规则覆盖</span>
                  <span
                    v-if="region?.domains[host.host]"
                    :class="['chip', region.domains[host.host].verdict === 'suspected' ? 'chip-warn' : '']"
                    :title="regionTitle(region.domains[host.host])"
                  >{{ REGION_VERDICT_LABELS[region.domains[host.host].verdict] }}</span>
                  <span v-if="host.probe" class="text-muted-foreground" :title="host.probe.reasons.join('；')">
                    {{ VERDICT_LABELS[host.probe.verdict] || host.probe.verdict }} · {{ relativeTime(host.probe.checked_at) }}
                  </span>
                  <span v-if="view !== 'ignored'" class="ml-auto flex gap-1">
                    <Button size="sm" variant="ghost" class="h-7 text-[12px]" :disabled="applying" @click="apply([{ value: host.host, target: 'proxy', rule_type: 'DOMAIN' }])">仅此子域代理</Button>
                    <Button size="sm" variant="ghost" class="h-7 text-[12px]" :disabled="applying" @click="apply([{ value: host.host, target: 'direct', rule_type: 'DOMAIN' }])">仅此子域直连</Button>
                  </span>
                </li>
              </ul>
            </TableCell>
          </TableRow>
        </template>
      </TableBody>
    </DataTableShell>

    <Dialog v-model:open="initDialogVisible">
      <DialogContent class="max-w-[480px]">
        <DialogHeader>
          <DialogTitle>创建默认规则集</DialogTitle>
          <DialogDescription>
            在规则库新建{{ missingTargets.map(target => `「${DEFAULT_NAMES[target]}」`).join('和') }}，并加入当前配置的兜底规则之前。部署一次配置后，之后加入的域名会由 Agent 自动刷新生效。
          </DialogDescription>
        </DialogHeader>
        <div v-if="missingTargets.includes('proxy')" class="flex flex-col gap-1.5">
          <Label for="init-proxy-policy">代理规则集使用的策略组</Label>
          <Select v-model="initProxyPolicy">
            <SelectTrigger id="init-proxy-policy" class="w-full bg-background/50">
              <SelectValue placeholder="选择策略组" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="policy in settings?.policies || []" :key="policy" :value="policy">{{ policy }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="initDialogVisible = false">取消</Button>
          <Button :disabled="initializing || (missingTargets.includes('proxy') && !initProxyPolicy)" @click="initRulesets">
            <Loader2 v-if="initializing" class="size-4 animate-spin" />
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { isAxiosError } from 'axios'
import { ChevronRight, Eraser, EyeOff, Globe, ListPlus, Loader2, Plus, Radar, RefreshCw, ScanSearch, Sparkles, TriangleAlert, Undo2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import DataTableShell from '@/components/common/DataTableShell.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Segmented from '@/components/common/Segmented.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import Toolbar from '@/components/common/Toolbar.vue'
import RegionPanel, { type DomainRegionEntry, type RegionPayload } from '@/components/domain-discovery/RegionPanel.vue'
import api from '@/api'
import { confirmDanger, notify } from '@/lib/feedback'
import { relativeTime } from '@/lib/format'

type Target = 'direct' | 'proxy'
type View = 'uncovered' | 'failing' | 'all' | 'ignored' | 'history' | 'region'
type Tag = 'uncovered' | 'failing' | 'pending' | 'ignored'

interface AgentInfo {
  id: string
  name: string
  status: string
  last_report: string | null
  mihomo_version: string
  enabled: boolean
  supported: boolean
  probe_supported: boolean
  version: string
}

interface PathStats {
  ok: number
  total: number
  delays: number[]
}

interface Probe {
  host: string
  checked_at: string
  verdict: string
  target: Target | null
  confidence: number
  reasons: string[]
  direct?: PathStats
  proxy?: PathStats
  proxy_path?: string
}

interface ProbeJob {
  id: string
  kind: 'probe' | 'region'
  status: 'running' | 'done' | 'failed'
  total: number
  done: number
  errors: Array<{ message: string }>
  message: string
}

interface HistoryEntry {
  value: string
  rule_type: string
  target: Target
  ruleset: string | null
  source: 'manual' | 'auto'
  applied_at: string
  verdict: string | null
  confidence: number | null
  recheck: { at: string; verdict: string | null; suggest_remove: boolean; reason: string } | null
}

interface Route {
  rule: string
  rule_payload: string
  outlet: 'direct' | 'proxy' | 'reject'
  policy: string
  conns: number
  fails: number
}

interface MatchedRule {
  policy: string
  matched_line: string
  rule_name: string
}

interface HostItem {
  host: string
  conns: number
  fails: number
  uncovered: boolean
  failing: boolean
  pending_rule: MatchedRule | null
  probe: Probe | null
}

interface DomainItem {
  domain: string
  tags: Tag[]
  conns: number
  fails: number
  fail_kinds: Record<string, number>
  routes: Route[]
  hosts: HostItem[]
  last_seen: string
  pending_rule: MatchedRule | null
  suggestion: Probe | null
}

interface RulesetInfo {
  id: string
  name: string | null
  behavior: string | null
  problem: string | null
  active: boolean
  references: Array<{ policy: string; enabled: boolean; after_match: boolean }>
}

interface Settings {
  direct: RulesetInfo | null
  proxy: RulesetInfo | null
  ignored: string[]
  candidates: Array<{ id: string; name: string; behavior: string }>
  policies: string[]
  auto_probe: boolean
  auto_apply: boolean
  auto_apply_min_confidence: number
  confidence_choices: number[]
  probe_path: string | null
}

interface ApplyItem {
  value: string
  target: Target
  rule_type?: 'DOMAIN-SUFFIX' | 'DOMAIN'
}

const TARGETS: Target[] = ['direct', 'proxy']
const TARGET_LABELS: Record<Target, string> = { direct: '直连', proxy: '代理' }
const DEFAULT_NAMES: Record<Target, string> = { direct: '域名发现-直连', proxy: '域名发现-代理' }
const DAY_OPTIONS = [
  { value: '1', label: '24 小时' },
  { value: '7', label: '7 天' }
]
const VIEW_OPTIONS: Array<{ value: View; label: string }> = [
  { value: 'uncovered', label: '未覆盖' },
  { value: 'failing', label: '直连失败' },
  { value: 'all', label: '全部' },
  { value: 'ignored', label: '已忽略' },
  { value: 'history', label: '已加入' },
  { value: 'region', label: '地区限制' }
]
const REGION_VERDICT_LABELS: Record<DomainRegionEntry['verdict'], string> = {
  suspected: '疑似区域限制', all_restricted: '各地区都受限', no_difference: '无地区差异', unreachable: '地区检测失败'
}
const TAG_LABELS: Record<Tag, string> = { uncovered: '未覆盖', failing: '直连失败', pending: '待部署', ignored: '已忽略' }
const TAG_CLASSES: Record<Tag, string> = { uncovered: 'chip-acc', failing: 'chip-bad', pending: 'chip-sky', ignored: '' }
const STATUS_LABELS: Record<string, string> = {
  running: '采集中',
  no_controller: '未配置控制接口',
  auth_failed: '控制接口密钥错误',
  unreachable: '连不上 Mihomo',
  no_data: '等待上报'
}
const VERDICT_LABELS: Record<string, string> = {
  needs_proxy: '直连不通，代理可达',
  direct_only: '代理不通，直连可达',
  both_ok: '直连与代理都可达',
  unreachable: '直连与代理都不通',
  proxy_down: '代理节点不可用',
  direct_down: '本机直连异常',
  flaky: '结果不稳定',
  incomplete: '探测不完整'
}
const PROBE_POLL_MS = 1500
const FAIL_KIND_LABELS: Record<string, string> = {
  timeout: '超时', reset: '重置', refused: '拒绝', eof: '断开', dns: '解析', other: '其他'
}

const router = useRouter()
const days = ref('7')
const view = ref<View>('uncovered')
const search = ref('')
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const agents = ref<AgentInfo[]>([])
const items = ref<DomainItem[]>([])
const sniffCoverage = ref<number | null>(null)
const minAgentVersion = ref('')
const probeAgentVersion = ref('')
const settings = ref<Settings | null>(null)
const selected = ref(new Set<string>())
const expanded = ref(new Set<string>())
const applying = ref(false)
const savingSettings = ref(false)
const togglingAgent = ref('')
const initDialogVisible = ref(false)
const initProxyPolicy = ref('')
const initializing = ref(false)
const probeJob = ref<ProbeJob | null>(null)
const history = ref<HistoryEntry[]>([])
const region = ref<RegionPayload | null>(null)
const historyLoading = ref(false)
let probeTimer: ReturnType<typeof setTimeout> | null = null

const probing = computed(() => probeJob.value?.status === 'running')

const errorMessage = (error: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(error) ? error.response?.data?.message || fallback : fallback

const visibleItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return items.value
  return items.value.filter(item => item.domain.includes(keyword) || item.hosts.some(host => host.host.includes(keyword)))
})

const visibleHistory = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return keyword ? history.value.filter(entry => entry.value.includes(keyword)) : history.value
})

const adoptableSelected = computed(() =>
  items.value.filter(item => selected.value.has(item.domain) && item.suggestion?.target)
)

const allSelected = computed(() =>
  visibleItems.value.length > 0 && visibleItems.value.every(item => selected.value.has(item.domain))
)

const missingTargets = computed<Target[]>(() =>
  settings.value ? TARGETS.filter(target => !settings.value?.[target] || settings.value[target]?.problem) : []
)

const enabledAgents = computed(() => agents.value.filter(agent => agent.enabled))

const emptyTitle = computed(() => {
  if (!enabledAgents.value.length) return '还没有开启域名发现'
  if (search.value.trim()) return '没有匹配的域名'
  return {
    uncovered: '没有未覆盖的域名',
    failing: '没有直连失败的域名',
    all: '暂无数据',
    ignored: '没有忽略的域名',
    history: '还没有加入过域名',
    region: ''
  }[view.value]
})

const emptyDescription = computed(() => {
  if (!enabledAgents.value.length) return '在上方「采集设备」里打开 Agent 的开关，几分钟后这里会出现访问过的域名。'
  if (view.value === 'uncovered') return '访问过的域名都已被规则覆盖。'
  return ''
})

const statusTone = (status: string) =>
  status === 'running' ? 'success' : status === 'no_data' ? 'muted' : 'warning'

const routeText = (route: Route) => {
  // 全局 / 直连模式或指定了出口的入站不经过规则匹配，rule 为空
  const rule = route.rule ? (route.rule_payload ? `${route.rule}(${route.rule_payload})` : route.rule) : '未经规则'
  const outlet = route.outlet === 'direct' ? '直连' : route.outlet === 'reject' ? '拒绝' : '代理'
  return `${rule} → ${route.policy} · ${outlet}`
}

const suggestionText = (probe: Probe) =>
  probe.target ? `建议${TARGET_LABELS[probe.target]} ${probe.confidence}` : VERDICT_LABELS[probe.verdict] || probe.verdict

const suggestionClass = (probe: Probe) => {
  if (probe.target === 'proxy') return probe.confidence >= 80 ? 'chip-acc' : ''
  if (probe.target === 'direct') return probe.confidence >= 80 ? 'chip-ok' : ''
  return ['proxy_down', 'direct_down', 'unreachable'].includes(probe.verdict) ? 'chip-warn' : ''
}

const suggestionTitle = (probe: Probe) =>
  [`${probe.host}：${VERDICT_LABELS[probe.verdict] || probe.verdict}`, ...probe.reasons, `探测于 ${relativeTime(probe.checked_at)}`].join('\n')

const failKindsText = (kinds: Record<string, number>) =>
  Object.entries(kinds)
    .sort((a, b) => b[1] - a[1])
    .map(([kind, count]) => `${FAIL_KIND_LABELS[kind] || kind} ${count}`)
    .join(' · ')

const tagTitle = (item: DomainItem, tag: Tag) => {
  if (tag === 'pending' && item.pending_rule) return `已命中「${item.pending_rule.rule_name}」→ ${item.pending_rule.policy}，部署到 Agent 后生效`
  if (tag === 'uncovered') return '没有被任何规则覆盖，落到了兜底规则'
  if (tag === 'failing') return '直连失败占比超过一半'
  return ''
}

const toggleSelected = (domain: string) => {
  const next = new Set(selected.value)
  if (next.has(domain)) next.delete(domain)
  else next.add(domain)
  selected.value = next
}

const toggleAll = () => {
  selected.value = allSelected.value ? new Set() : new Set(visibleItems.value.map(item => item.domain))
}

const toggleExpanded = (domain: string) => {
  const next = new Set(expanded.value)
  if (next.has(domain)) next.delete(domain)
  else next.add(domain)
  expanded.value = next
}

const loadHistory = async () => {
  historyLoading.value = true
  try {
    const { data } = await api.get('/domain-discovery/history')
    history.value = data.items
  } catch (error) {
    notify.error(errorMessage(error, '无法读取已加入的记录'))
  } finally {
    historyLoading.value = false
  }
}

const loadRegion = async () => {
  try {
    const { data } = await api.get('/domain-discovery/region')
    region.value = data
  } catch (error) {
    notify.error(errorMessage(error, '无法读取区域检测结果'))
  }
}

const regionTitle = (entry: DomainRegionEntry) =>
  Object.entries(entry.targets).map(([name, result]) => `${name}：${result.detail}`).join('\n')

const loadDomains = async () => {
  if (view.value === 'region') {
    selected.value = new Set()
    return loadRegion()
  }
  if (view.value === 'history') {
    selected.value = new Set()
    return loadHistory()
  }
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await api.get('/domain-discovery/domains', { params: { days: days.value, view: view.value } })
    agents.value = data.agents
    items.value = data.items
    sniffCoverage.value = data.sniff_coverage
    minAgentVersion.value = data.min_agent_version
    probeAgentVersion.value = data.min_probe_agent_version
    if (data.probe_job && !probeJob.value) {
      probeJob.value = data.probe_job
      scheduleProbePoll()
    }
    const present = new Set(data.items.map((item: DomainItem) => item.domain))
    selected.value = new Set([...selected.value].filter(domain => present.has(domain)))
  } catch (error) {
    loadError.value = errorMessage(error, '无法读取域名发现数据')
  } finally {
    loading.value = false
    loaded.value = true
  }
}

const loadSettings = async () => {
  try {
    const { data } = await api.get('/domain-discovery/settings')
    settings.value = data
  } catch (error) {
    notify.error(errorMessage(error, '无法读取默认规则集设置'))
  }
}

const reload = () => Promise.all([loadDomains(), loadSettings(), view.value === 'region' ? null : loadRegion()])

const scheduleProbePoll = () => {
  if (probeTimer) clearTimeout(probeTimer)
  probeTimer = setTimeout(pollProbe, PROBE_POLL_MS)
}

const pollProbe = async () => {
  probeTimer = null
  const job = probeJob.value
  if (!job) return
  try {
    const { data } = await api.get(`/domain-discovery/probe/${job.id}`)
    probeJob.value = data.job
  } catch (error) {
    notify.error(errorMessage(error, '无法读取探测进度'))
    probeJob.value = null
    return
  }
  if (probeJob.value?.status === 'running') {
    scheduleProbePoll()
    return
  }
  const finished = probeJob.value
  const label = finished?.kind === 'region' ? '区域检测' : '探测'
  if (finished?.status === 'failed') {
    notify.error(`${label}失败`, finished.message)
  } else if (finished?.errors.length) {
    notify.warning(`${label}完成，${finished.errors.length} 个目标失败`, finished.errors[0].message)
  } else if (finished?.kind === 'region') {
    notify.success(`区域检测完成（${finished.total} 个目标）`, view.value === 'region' ? undefined : '在「地区限制」里查看结果。')
  } else {
    notify.success(`已探测 ${finished?.total ?? 0} 个子域`)
  }
  await Promise.all([loadDomains(), finished?.kind === 'region' && view.value !== 'region' ? loadRegion() : null])
}

const startProbe = async (domains: string[]) => {
  if (!settings.value?.probe_path) {
    notify.warning('请先设置并启用默认代理规则集', '探测代理路径时会使用它指向的策略组。')
    return
  }
  if (!agents.value.some(agent => agent.enabled && agent.probe_supported)) {
    notify.warning(`没有可用于探测的 Agent`, `需要开启域名发现，且 Agent 版本不低于 ${probeAgentVersion.value}。`)
    return
  }
  try {
    const { data } = await api.post('/domain-discovery/probe', { domains })
    probeJob.value = data.job
    scheduleProbePoll()
  } catch (error) {
    if (isAxiosError(error) && error.response?.status === 409 && error.response.data?.job) {
      probeJob.value = error.response.data.job
      scheduleProbePoll()
      notify.info('已有探测任务在运行，完成后再试')
      return
    }
    notify.error(errorMessage(error, '无法开始探测'))
  }
}

const startRegionCheck = async (payload: { services: boolean; domains?: string[] }) => {
  try {
    const { data } = await api.post('/domain-discovery/region/check', payload)
    probeJob.value = data.job
    scheduleProbePoll()
  } catch (error) {
    if (isAxiosError(error) && error.response?.status === 409 && error.response.data?.job) {
      probeJob.value = error.response.data.job
      scheduleProbePoll()
      notify.info('已有检测任务在运行，完成后再试')
      return
    }
    notify.error(errorMessage(error, '无法开始区域检测'))
  }
}

const probeSelected = () => startProbe([...selected.value].slice(0, 100))

const adoptSelected = () => apply(adoptableSelected.value.map(item => ({
  value: item.domain,
  target: item.suggestion!.target as Target
})))

const saveAutomation = async (changes: Partial<Pick<Settings, 'auto_probe' | 'auto_apply' | 'auto_apply_min_confidence'>>) => {
  savingSettings.value = true
  try {
    const { data } = await api.put('/domain-discovery/settings', changes)
    settings.value = data
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  } finally {
    savingSettings.value = false
  }
}

const undo = async (entry: HistoryEntry) => {
  if (!(await confirmDanger(`从「${entry.ruleset || TARGET_LABELS[entry.target]}」中移除 ${entry.value}？`))) return
  applying.value = true
  try {
    const { data } = await api.post('/domain-discovery/undo', { value: entry.value, target: entry.target })
    notify.success(data.removed.length ? `已移除 ${entry.value}` : '规则集中已没有这一条，已删除记录',
      '已部署过该规则集的 Agent 会在下次上报时自动刷新。')
    await loadHistory()
  } catch (error) {
    notify.error(errorMessage(error, '撤销失败'))
  } finally {
    applying.value = false
  }
}

const toggleAgent = async (agent: AgentInfo, enabled: boolean) => {
  togglingAgent.value = agent.id
  try {
    await api.put(`/agents/${agent.id}/domain-discovery`, { enabled })
    agent.enabled = enabled
    notify.success(enabled ? `已开启 ${agent.name} 的域名发现` : `已关闭 ${agent.name} 的域名发现`,
      enabled ? 'Agent 会在下次心跳（约 30 秒内）开始采集。' : undefined)
  } catch (error) {
    notify.error(errorMessage(error, '切换失败'))
  } finally {
    togglingAgent.value = ''
  }
}

const clearAgent = async (agent: AgentInfo) => {
  if (!(await confirmDanger(`清除 ${agent.name} 已采集的域名数据？此操作不可撤销。`))) return
  try {
    await api.post(`/agents/${agent.id}/domain-discovery/clear`)
    notify.success('已清除')
    await loadDomains()
  } catch (error) {
    notify.error(errorMessage(error, '清除失败'))
  }
}

const saveRuleset = async (target: Target, id: string) => {
  savingSettings.value = true
  try {
    const { data } = await api.put('/domain-discovery/settings', { [`${target}_ruleset`]: id || null })
    settings.value = data
    notify.success(`已设置默认${TARGET_LABELS[target]}规则集`)
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  } finally {
    savingSettings.value = false
  }
}

const openInitDialog = () => {
  initProxyPolicy.value = settings.value?.policies[0] || ''
  initDialogVisible.value = true
}

const initRulesets = async () => {
  initializing.value = true
  try {
    const { data } = await api.post('/domain-discovery/rulesets/init', {
      targets: missingTargets.value,
      proxy_policy: initProxyPolicy.value || undefined
    })
    settings.value = data
    initDialogVisible.value = false
    notify.success('已创建默认规则集', '需要把配置部署到 Agent 一次，规则集才会生效。')
  } catch (error) {
    notify.error(errorMessage(error, '创建失败'))
  } finally {
    initializing.value = false
  }
}

const apply = async (entries: ApplyItem[]) => {
  const missing = TARGETS.filter(target => entries.some(entry => entry.target === target) && missingTargets.value.includes(target))
  if (missing.length) {
    notify.warning(`请先设置默认${missing.map(target => TARGET_LABELS[target]).join('、')}规则集`)
    return
  }
  applying.value = true
  try {
    const { data } = await api.post('/domain-discovery/apply', {
      items: entries.map(entry => ({ rule_type: 'DOMAIN-SUFFIX', ...entry }))
    })
    const added = data.added.length
    const skipped = data.skipped.length
    if (added) {
      notify.success(`已加入 ${added} 个域名${skipped ? `，${skipped} 个已存在` : ''}`,
        '已部署过该规则集的 Agent 会在下次上报时自动刷新；否则需要重新部署配置。')
    } else {
      notify.info('这些域名已在规则集中')
    }
    for (const warning of data.warnings) notify.warning(warning)
    selected.value = new Set()
    await Promise.all([loadDomains(), loadSettings()])
  } catch (error) {
    notify.error(errorMessage(error, '加入规则集失败'))
  } finally {
    applying.value = false
  }
}

const applySelected = (target: Target) => apply([...selected.value].map(value => ({ value, target })))

const updateIgnored = async (values: string[], add: boolean) => {
  applying.value = true
  try {
    await api.request({ url: '/domain-discovery/ignore', method: add ? 'post' : 'delete', data: { values } })
    notify.success(add ? `已忽略 ${values.length} 个域名` : `已取消忽略 ${values.length} 个域名`)
    selected.value = new Set()
    await loadDomains()
  } catch (error) {
    notify.error(errorMessage(error, '操作失败'))
  } finally {
    applying.value = false
  }
}

const ignore = (values: string[]) => updateIgnored(values, true)
const unignore = (values: string[]) => updateIgnored(values, false)
const ignoreSelected = () => ignore([...selected.value])
const unignoreSelected = () => unignore([...selected.value])

onMounted(reload)
onUnmounted(() => {
  if (probeTimer) clearTimeout(probeTimer)
})
</script>
