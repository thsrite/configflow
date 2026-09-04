<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner scope="profile" :profile-name="cfProfileName" />

    <PageHeader
      eyebrow="Profile"
      title="策略组"
      description="分组、筛选与节点引用，仅属于当前配置空间。"
    >
      <template #actions>
        <Button
          v-if="!reorder.active.value"
          variant="outline"
          class="border-border/60 bg-background/40"
          :disabled="proxyGroups.length < 2"
          @click="reorder.enter"
        >
          <ArrowUpDown class="size-4" />
          调整顺序
        </Button>
        <Button class="shadow-glow" @click="showAddDialog">
          <Plus class="size-4" />
          添加策略组
        </Button>
      </template>
    </PageHeader>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <SectionCard v-if="!proxyGroups.length" :padded="false">
      <EmptyState
        :icon="LayoutGrid"
        title="还没有策略组"
        description="策略组决定流量走哪些节点，是规则生效的落点。"
      >
        <Button @click="showAddDialog">
          <Plus class="size-4" />
          添加策略组
        </Button>
      </EmptyState>
    </SectionCard>

    <div
      v-else
      ref="groupsContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(340px,1fr))] gap-3 max-md:grid-cols-1"
    >
      <Motion
        v-for="(group, cfIndex) in proxyGroups"
        :key="group.id || group.name"
        v-bind="listItem(cfIndex)"
        :data-name="group.name"
        data-reorder-item
        :class="[
          'hairline edge-light relative flex flex-col gap-3 overflow-hidden rounded-xl border border-border/35 bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft',
          !group.enabled && 'opacity-60'
        ]"
      >
        <header class="flex items-start gap-2.5">
          <DragHandle
            v-if="reorder.active.value"
            :label="group.name || group.id"
            :index="cfIndex"
            :total="proxyGroups.length"
            :position="reorder.positionLabel(cfIndex)"
            :grabbed="reorder.grabbedIndex.value === cfIndex"
            @up="reorder.moveUp(cfIndex)"
            @down="reorder.moveDown(cfIndex)"
            @keydown="reorder.onHandleKeydown($event, cfIndex)"
          />
          <span v-if="getGroupIcon(group.name)" class="shrink-0 text-[18px] leading-none">
            {{ getGroupIcon(group.name) }}
          </span>
          <p class="m-0 min-w-0 flex-1 truncate text-[14px] font-semibold text-foreground">
            {{ getGroupNameWithoutIcon(group.name) }}
          </p>
          <Badge v-if="group.follow_group" variant="warning" class="shrink-0">跟随</Badge>
          <Badge v-else variant="outline" class="shrink-0 text-[10.5px]">
            {{ getGroupTypeLabel(group.type) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon-sm"
            class="cf-reorder-mute shrink-0"
            :class="group.enabled ? 'text-success-accent' : 'text-muted-foreground'"
            :title="group.enabled ? '停用' : '启用'"
            :aria-label="group.enabled ? `停用 ${group.name}` : `启用 ${group.name}`"
            :disabled="group.id ? savingStatus[group.id] : false"
            @click="handleToggle(group)"
          >
            <component :is="group.enabled ? Eye : EyeOff" class="size-4" />
          </Button>
        </header>

        <Collapsible
          class="cf-reorder-mute"
          :open="isCardExpanded(group.id || group.name)"
          @update:open="toggleCardExpand(group.id || group.name)"
        >
          <div class="flex items-center gap-2 rounded-lg border border-border/40 bg-background/40 px-3 py-2">
            <span class="min-w-0 flex-1 truncate text-[12.5px] text-muted-foreground">
              {{ getSourceSummary(group) }}
            </span>
            <CollapsibleTrigger as-child>
              <Button
                variant="ghost"
                size="icon-sm"
                class="size-6 shrink-0"
                :title="isCardExpanded(group.id || group.name) ? '收起详情' : '展开详情'"
                :aria-label="isCardExpanded(group.id || group.name) ? '收起详情' : '展开详情'"
              >
                <ChevronDown
                  class="size-3.5 transition-transform duration-200"
                  :class="isCardExpanded(group.id || group.name) && 'rotate-180'"
                />
              </Button>
            </CollapsibleTrigger>
          </div>

          <CollapsibleContent class="mt-2.5 flex flex-col gap-2.5">
            <template v-if="group.follow_group">
              <GroupField label="跟随策略" :icon="GitBranch">
                {{ getFollowGroupName(group.follow_group) || '-' }}
              </GroupField>
            </template>

            <template v-else>
              <GroupField v-if="hasAggregations(group)" label="聚合来源" :icon="Share2">
                <div class="flex flex-wrap gap-1">
                  <Badge
                    v-for="aggName in getAggregationsList(group)"
                    :key="aggName"
                    variant="warning"
                    class="max-w-[180px] truncate text-[10.5px]"
                  >
                    {{ aggName }}
                  </Badge>
                  <span v-if="!getAggregationsList(group).length" class="text-[12px] text-muted-foreground">无</span>
                </div>
              </GroupField>

              <GroupField
                v-if="hasAggregations(group) && getAggregationSubscriptions(group)"
                label="包含订阅"
                :icon="Link2"
              >
                {{ getAggregationSubscriptions(group) }}
              </GroupField>

              <GroupField
                v-if="hasAggregations(group) && getAggregationNodes(group)"
                label="包含节点"
                :icon="Network"
              >
                {{ getAggregationNodes(group) }}
              </GroupField>

              <GroupField v-if="group.aggregation_regex" label="聚合正则" :icon="Filter">
                <div class="flex items-center gap-2">
                  <code
                    class="min-w-0 flex-1 truncate rounded-md border border-border/50 bg-background/50 px-2 py-1 font-mono text-[11.5px]"
                  >
                    {{ group.aggregation_regex }}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    class="shrink-0 border-border/60 bg-background/40"
                    :disabled="regexPreviewLoading && regexPreviewSource === 'aggregation'"
                    @click.stop="previewSavedRegexMatches(group, 'aggregation')"
                  >
                    <Eye class="size-3.5" />
                    预览
                  </Button>
                </div>
              </GroupField>

              <GroupField
                v-if="hasSubscriptions(group) && !hasAggregations(group)"
                label="订阅来源"
                :icon="Link2"
              >
                {{ getSubscriptionDisplay(group) }}
              </GroupField>

              <GroupField v-if="group.regex && hasSubscriptions(group)" label="订阅正则" :icon="Filter">
                <div class="flex items-center gap-2">
                  <code
                    class="min-w-0 flex-1 truncate rounded-md border border-border/50 bg-background/50 px-2 py-1 font-mono text-[11.5px]"
                  >
                    {{ group.regex }}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    class="shrink-0 border-border/60 bg-background/40"
                    :disabled="regexPreviewLoading && regexPreviewSource === 'subscription'"
                    @click.stop="previewSavedRegexMatches(group, 'subscription')"
                  >
                    <Eye class="size-3.5" />
                    预览
                  </Button>
                </div>
              </GroupField>

              <GroupField
                v-if="hasManualNodes(group) && !hasAggregations(group)"
                label="节点来源"
                :icon="Network"
              >
                {{ getManualNodesDisplay(group) }}
              </GroupField>

              <GroupField v-if="hasIncludeGroups(group)" label="策略组来源" :icon="LayoutGrid">
                <div class="flex flex-wrap gap-1">
                  <Badge
                    v-for="name in getIncludeGroupsList(group)"
                    :key="name"
                    variant="info"
                    class="max-w-[180px] truncate text-[10.5px]"
                  >
                    {{ name }}
                  </Badge>
                </div>
              </GroupField>

              <GroupField
                v-if="hasManualNodes(group) && hasIncludeGroups(group)"
                label="节点顺序"
                :icon="ArrowUpDown"
              >
                {{ getProxyOrderLabel(group.proxy_order) }}
              </GroupField>

              <GroupField v-if="group.type !== 'select'" label="测试 URL" :icon="Link2">
                <span class="font-mono text-[11.5px]">{{ group.url || '-' }}</span>
              </GroupField>

              <GroupField v-if="group.type !== 'select'" label="测试间隔" :icon="Timer">
                <span class="num">{{ group.interval || '-' }} 秒</span>
              </GroupField>

              <GroupField
                v-if="group.type === 'load-balance' && group.strategy"
                label="负载策略"
                :icon="Scale"
              >
                {{ getStrategyLabel(group.strategy) }}
              </GroupField>

              <GroupField
                v-if="group.type === 'load-balance' && group.lazy !== undefined"
                label="懒加载"
                :icon="Scale"
              >
                {{ group.lazy ? '是' : '否' }}
              </GroupField>
            </template>
          </CollapsibleContent>
        </Collapsible>

        <footer class="cf-reorder-mute mt-auto flex items-center gap-1 border-0 border-t border-border/50 pt-3">
          <Button variant="ghost" size="sm" @click="editGroup(group)">
            <Pencil class="size-3.5" />
            编辑
          </Button>
          <Button
            variant="ghost"
            size="sm"
            class="ml-auto text-destructive-accent hover:bg-destructive-soft"
            @click="deleteGroup(group)"
          >
            <Trash2 class="size-3.5" />
            删除
          </Button>
        </footer>
      </Motion>
    </div>

    <!-- ===== 新增 / 编辑策略组 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline max-w-[720px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑策略组' : '添加策略组' }}</DialogTitle>
          <DialogDescription>选择节点来源并设置测试参数，顺序可拖拽调整。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[64dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label for="group-name">名称</Label>
            <Input id="group-name" v-model="form.name" class="bg-background/50" placeholder="请输入策略组名称" />
          </div>

          <div v-if="!enabledSources.includes('follow')" class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select v-model="form.type">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="请选择策略组类型" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem value="select">手动选择 (Select)</SelectItem>
                <SelectItem value="url-test">自动测速 (URL-Test)</SelectItem>
                <SelectItem value="fallback">故障转移 (Fallback)</SelectItem>
                <SelectItem value="load-balance">负载均衡 (Load-Balance)</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-2">
            <Label>节点来源</Label>
            <!-- 「跟随」与其它来源互斥，勾选后其余选项隐藏 -->
            <div class="flex flex-wrap gap-2">
              <label
                v-for="source in visibleSourceOptions"
                :key="source.value"
                :class="[
                  'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors',
                  enabledSources.includes(source.value)
                    ? 'border-primary-accent/40 bg-primary-soft/50 text-foreground'
                    : 'border-border/50 bg-background/40 text-muted-foreground hover:border-border-strong'
                ]"
              >
                <Checkbox
                  :model-value="enabledSources.includes(source.value)"
                  @update:model-value="toggleSource(source.value)"
                />
                {{ source.label }}
              </label>
            </div>
          </div>

          <!-- 跟随策略组 -->
          <div v-if="enabledSources.includes('follow')" class="flex flex-col gap-1.5">
            <Label>跟随策略</Label>
            <Select v-model="form.follow_group">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="选择要跟随的策略组" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="group in availableStrategies" :key="group.id" :value="group.id">
                  {{ group.name }}
                </SelectItem>
              </SelectContent>
            </Select>
            <p class="m-0 text-[12px] text-muted-foreground">
              跟随模式将完全复制被跟随策略组的所有配置（类型、节点来源、测试参数等），只保留自己的名称。
            </p>
          </div>

          <!-- 订阅筛选 -->
          <template v-if="enabledSources.includes('subscription')">
            <div class="flex flex-col gap-1.5">
              <Label>订阅筛选</Label>
              <MultiSelect
                v-model="form.subscriptions"
                :options="subscriptionOptions"
                placeholder="选择订阅（自动包含订阅的所有节点）"
              />
            </div>
            <div v-if="form.subscriptions && form.subscriptions.length" class="flex flex-col gap-1.5">
              <Label for="group-regex">正则过滤</Label>
              <div class="flex items-center gap-2">
                <Input
                  id="group-regex"
                  v-model="form.regex"
                  class="bg-background/50 font-mono"
                  placeholder="可选，过滤订阅节点名称"
                />
                <Button
                  variant="outline"
                  class="shrink-0 border-border/60 bg-background/40"
                  :disabled="regexPreviewLoading && regexPreviewSource === 'subscription'"
                  @click="previewRegexMatches('subscription')"
                >
                  <Eye class="size-4" />
                  预览
                </Button>
              </div>
            </div>
          </template>

          <!-- 手动节点 -->
          <div v-if="enabledSources.includes('node')" class="flex flex-col gap-1.5">
            <Label>包含节点</Label>
            <MultiSelect v-model="form.manual_nodes" :options="nodeOptions" placeholder="手动选择节点" />
          </div>

          <!-- 引用聚合 -->
          <div v-if="enabledSources.includes('aggregation')" class="flex flex-col gap-1.5">
            <Label>引用聚合</Label>
            <MultiSelect
              v-model="form.aggregations"
              :options="aggregationOptions"
              placeholder="选择订阅聚合"
            />
            <p class="m-0 text-[12px] text-muted-foreground">聚合中的所有订阅和节点都会被包含。</p>
          </div>

          <div
            v-if="enabledSources.includes('aggregation') && form.aggregations && form.aggregations.length"
            class="flex flex-col gap-1.5"
          >
            <Label for="group-agg-regex">正则过滤</Label>
            <div class="flex items-center gap-2">
              <Input
                id="group-agg-regex"
                v-model="form.aggregation_regex"
                class="bg-background/50 font-mono"
                placeholder="可选，过滤聚合节点名称"
              />
              <Button
                variant="outline"
                class="shrink-0 border-border/60 bg-background/40"
                :disabled="regexPreviewLoading && regexPreviewSource === 'aggregation'"
                @click="previewRegexMatches('aggregation')"
              >
                <Eye class="size-4" />
                预览
              </Button>
            </div>
            <p class="m-0 text-[12px] text-muted-foreground">
              此正则应用于聚合中的节点，不使用聚合自带的正则过滤器。
            </p>
          </div>

          <!-- 引用策略 -->
          <div v-if="enabledSources.includes('strategy')" class="flex flex-col gap-1.5">
            <Label>引用策略</Label>
            <MultiSelect
              v-model="form.include_groups"
              :options="strategyOptions"
              placeholder="选择已有策略组"
            />
          </div>

          <!-- 已选择的节点和策略排序 -->
          <Collapsible v-if="orderedProxiesList.length > 0" v-model:open="orderPanelOpen">
            <CollapsibleTrigger as-child>
              <button
                type="button"
                class="flex w-full cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-left text-[13px] text-muted-foreground transition-colors hover:border-border-strong"
              >
                <GripVertical class="size-3.5" aria-hidden="true" />
                顺序调整 · {{ orderedProxiesList.length }} 项
                <ChevronDown
                  class="ml-auto size-3.5 transition-transform duration-200"
                  :class="orderPanelOpen && 'rotate-180'"
                />
              </button>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div ref="orderedProxiesRef" class="mt-2 flex flex-col gap-1.5">
                <div
                  v-for="(item, index) in orderedProxiesList"
                  :key="`${item.type}-${item.id}`"
                  class="flex cursor-grab items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-[13px]"
                  :data-index="index"
                >
                  <GripVertical class="drag-handle size-3.5 shrink-0 cursor-grab text-muted-foreground" aria-hidden="true" />
                  <Badge
                    :variant="item.type === 'node' ? 'success' : item.type === 'aggregation' ? 'warning' : 'info'"
                    class="shrink-0 text-[10.5px]"
                  >
                    {{ item.type === 'node' ? '节点' : item.type === 'aggregation' ? '聚合' : '策略' }}
                  </Badge>
                  <span class="min-w-0 truncate text-foreground">{{ item.name }}</span>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>

          <div v-if="needsUrl && !enabledSources.includes('follow')" class="flex flex-col gap-1.5">
            <Label for="group-url">测试 URL</Label>
            <Input
              id="group-url"
              v-model="form.url"
              class="bg-background/50 font-mono"
              placeholder="http://www.gstatic.com/generate_204"
            />
          </div>

          <div v-if="needsUrl && !enabledSources.includes('follow')" class="flex flex-col gap-1.5">
            <Label for="group-interval">测试间隔（秒）</Label>
            <Input
              id="group-interval"
              v-model.number="form.interval"
              type="number"
              :min="60"
              :max="3600"
              class="w-44 bg-background/50"
            />
          </div>

          <div
            v-if="form.type === 'load-balance' && !enabledSources.includes('follow')"
            class="flex flex-col gap-1.5"
          >
            <Label>负载策略</Label>
            <Select v-model="form.strategy">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="请选择负载策略（可选）" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem value="round-robin">轮询 (round-robin)</SelectItem>
                <SelectItem value="consistent-hashing">一致性哈希 (consistent-hashing)</SelectItem>
                <SelectItem value="sticky-sessions">会话保持 (sticky-sessions)</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div
            v-if="form.type === 'load-balance' && !enabledSources.includes('follow')"
            class="flex flex-col gap-1.5"
          >
            <Label>懒加载</Label>
            <!-- 保持三态：未设置时不写入该字段，与旧行为一致 -->
            <Select v-model="lazyChoice">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="请选择是否启用懒加载（可选）" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem value="unset">未设置</SelectItem>
                <SelectItem value="true">是</SelectItem>
                <SelectItem value="false">否</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch id="group-enabled" v-model="form.enabled" />
            <Label for="group-enabled" class="text-[13px] text-muted-foreground">
              {{ form.enabled ? '策略组启用中' : '策略组已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button @click="saveGroup">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 正则匹配预览 ===== -->
    <Dialog v-model:open="regexPreviewVisible">
      <DialogContent class="glass-strong hairline max-w-[680px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ regexPreviewTitle }}</DialogTitle>
          <DialogDescription v-if="regexPreviewResult">
            匹配 {{ regexPreviewNodes.length }} 个节点，候选 {{ regexPreviewResult.total_candidates }} 个
          </DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="regexPreviewLoading" :rows="4" />

        <div v-else class="flex max-h-[55dvh] flex-col gap-1.5 overflow-y-auto pr-1">
          <div
            v-for="node in regexPreviewNodes"
            :key="`${node.source_id || ''}-${node.name}`"
            class="flex items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-[13px]"
          >
            <Network class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span class="min-w-0 flex-1 truncate text-foreground">{{ node.name }}</span>
            <Badge variant="outline" class="shrink-0 text-[10px]">{{ getPreviewSourceLabel(node) }}</Badge>
            <Badge variant="success" class="shrink-0 font-mono text-[10px]">
              {{ (node.type || 'unknown').toUpperCase() }}
            </Badge>
          </div>
          <EmptyState
            v-if="!regexPreviewNodes.length"
            :icon="Filter"
            title="当前正则没有匹配到节点"
            description="放宽表达式，或确认所选订阅/聚合中确实存在候选节点。"
          />
        </div>

        <DialogFooter>
          <Button variant="outline" @click="regexPreviewVisible = false">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import PageHeader from '@/components/common/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import { useProfileStore } from '@/stores/profile'
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import {
  ArrowUpDown,
  ChevronDown,
  Eye,
  EyeOff,
  Filter,
  GitBranch,
  GripVertical,
  LayoutGrid,
  Link2,
  Network,
  Pencil,
  Plus,
  Scale,
  Share2,
  Timer,
  Trash2
} from '@lucide/vue'
import { Motion } from 'motion-v'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import EmptyState from '@/components/common/EmptyState.vue'
import GroupField from '@/components/common/GroupField.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import { choose, confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { proxyGroupApi, nodeApi } from '@/api'
import type { ProxyGroup, ProxyNode, Subscription } from '@/types'
import api from '@/api'
import Sortable from 'sortablejs'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
const proxyGroups = ref<ProxyGroup[]>([])
const nodes = ref<ProxyNode[]>([])
const subscriptions = ref<Subscription[]>([])
const aggregations = ref<any[]>([])
const savingStatus = ref<Record<string, boolean>>({})
const dialogVisible = ref(false)
const isEdit = ref(false)
const enabledSources = ref<string[]>([])
const regexPreviewVisible = ref(false)
const regexPreviewLoading = ref(false)
const regexPreviewSource = ref<'subscription' | 'aggregation' | ''>('')
const regexPreviewResult = ref<{ total_candidates: number } | null>(null)
const regexPreviewNodes = ref<any[]>([])
const regexPreviewTitle = ref('正则匹配预览')

/* 节点来源选项：勾选「跟随」后与其它来源互斥，其余选项直接隐藏 */
const SOURCE_OPTIONS = [
  { value: 'subscription', label: '订阅' },
  { value: 'node', label: '节点' },
  { value: 'aggregation', label: '聚合' },
  { value: 'strategy', label: '策略' },
  { value: 'follow', label: '跟随' }
]

const visibleSourceOptions = computed(() =>
  enabledSources.value.includes('follow')
    ? SOURCE_OPTIONS.filter(option => option.value === 'follow')
    : SOURCE_OPTIONS
)

const toggleSource = (value: string): void => {
  const checked = !enabledSources.value.includes(value)
  if (value === 'follow') {
    handleFollowChange(checked)
    if (!checked) enabledSources.value = enabledSources.value.filter(item => item !== 'follow')
    return
  }
  enabledSources.value = checked
    ? [...enabledSources.value, value]
    : enabledSources.value.filter(item => item !== value)
}

/* MultiSelect 需要 {value,label} */
const subscriptionOptions = computed(() =>
  subscriptions.value.map(sub => ({ value: sub.id, label: sub.name }))
)

/* 懒加载保持三态：'unset' 表示不写入该字段 */
const lazyChoice = computed<string>({
  get: () => (form.value.lazy === undefined ? 'unset' : String(form.value.lazy)),
  set: value => {
    form.value.lazy = value === 'unset' ? undefined : value === 'true'
  }
})

const orderPanelOpen = ref(false)

const groupsContainer = ref<HTMLElement | null>(null)
const orderedProxiesRef = ref<HTMLElement | null>(null)
let orderedProxiesSortable: any = null
const originalGroupName = ref<string>('') // 保存原始策略组名称，用于检测名称变化
const expandedCards = ref<Set<string>>(new Set()) // 展开的卡片ID集合
const form = ref<Partial<ProxyGroup>>({
  name: '',
  type: 'select',
  url: 'http://www.gstatic.com/generate_204',
  interval: 300,
  subscriptions: [],
  regex: '',
  aggregation_regex: '',
  manual_nodes: [],
  aggregations: [],
  include_groups: [],
  proxies_order: []
})

const availableNodes = computed(() => {
  // 返回所有节点（包含 id 和 name），默认包含DIRECT和REJECT
  const nodeOptions = nodes.value.map(n => ({ id: n.id, name: n.name }))
  // DIRECT 和 REJECT 使用名称作为 ID
  return [
    { id: 'DIRECT', name: 'DIRECT' },
    { id: 'REJECT', name: 'REJECT' },
    ...nodeOptions
  ]
})

const availableStrategies = computed(() => {
  // 排除当前正在编辑的策略组，避免循环引用
  return proxyGroups.value
    .filter(g => g.id !== form.value.id)
    .map(g => ({ id: g.id, name: g.name }))
})

const availableAggregations = computed(() => {
  // 只返回已开启的聚合,包含正则过滤器信息
  return aggregations.value
    .filter(a => a.enabled !== false)
    .map(a => ({ id: a.id, name: a.name, regex_filter: a.regex_filter }))
})

const needsUrl = computed(() => {
  return ['url-test', 'fallback', 'load-balance'].includes(form.value.type || '')
})

// 已排序的代理列表（用于显示）
const orderedProxiesList = computed(() => {
  const result: Array<{type: string, id: string, name: string}> = []
  const proxiesOrder = form.value.proxies_order || []

  // 如果有排序数据，使用排序数据
  if (proxiesOrder.length > 0) {
    proxiesOrder.forEach(item => {
      if (item.type === 'node') {
        const node = availableNodes.value.find(n => n.id === item.id)
        if (node) {
          result.push({ type: 'node', id: item.id, name: node.name })
        }
      } else if (item.type === 'strategy') {
        const strategy = availableStrategies.value.find(s => s.id === item.id)
        if (strategy) {
          result.push({ type: 'strategy', id: item.id, name: strategy.name })
        }
      } else if (item.type === 'aggregation') {
        const aggregation = availableAggregations.value.find(a => a.id === item.id)
        if (aggregation) {
          result.push({ type: 'aggregation', id: item.id, name: aggregation.name })
        }
      }
    })
  } else {
    // 没有排序数据时，自动生成（节点在前，聚合在中间，策略在后）
    const manualNodes = form.value.manual_nodes || []
    const aggregations = form.value.aggregations || []
    const includeGroups = form.value.include_groups || []

    manualNodes.forEach(nodeId => {
      const node = availableNodes.value.find(n => n.id === nodeId)
      if (node) {
        result.push({ type: 'node', id: nodeId, name: node.name })
      }
    })

    aggregations.forEach(aggId => {
      const aggregation = availableAggregations.value.find(a => a.id === aggId)
      if (aggregation) {
        result.push({ type: 'aggregation', id: aggId, name: aggregation.name })
      }
    })

    includeGroups.forEach(groupId => {
      const strategy = availableStrategies.value.find(s => s.id === groupId)
      if (strategy) {
        result.push({ type: 'strategy', id: groupId, name: strategy.name })
      }
    })
  }

  return result
})

// 查找上一个同类型策略组的配置
const findPreviousSameTypeConfig = (type: string) => {
  // 在现有策略组中查找最后一个同类型的策略组
  const sameTypeGroups = proxyGroups.value.filter(g => g.type === type)
  if (sameTypeGroups.length > 0) {
    // 返回最后一个同类型策略组的配置
    const lastGroup = sameTypeGroups[sameTypeGroups.length - 1]
    return {
      url: lastGroup.url || 'http://www.gstatic.com/generate_204',
      interval: lastGroup.interval || 300
    }
  }
  // 如果没有找到，返回默认值
  return {
    url: 'http://www.gstatic.com/generate_204',
    interval: 300
  }
}

// 监听策略组类型变化
watch(() => form.value.type, (newType, oldType) => {
  if (!oldType) return

  const needsUrlTypes = ['url-test', 'fallback', 'load-balance']
  const oldNeedsUrl = needsUrlTypes.includes(oldType)
  const newNeedsUrl = needsUrlTypes.includes(newType || '')

  // 从需要测速的类型切换到 select，删除测速参数
  if (oldNeedsUrl && newType === 'select') {
    delete form.value.url
    delete form.value.interval
  }
  // 从 select 切换到需要测速的类型，自动获取上一个同类型的配置
  else if (oldType === 'select' && newNeedsUrl && newType) {
    const config = findPreviousSameTypeConfig(newType)
    form.value.url = config.url
    form.value.interval = config.interval
  }
  // 从一个需要测速的类型切换到另一个需要测速的类型
  else if (oldNeedsUrl && newNeedsUrl && newType && oldType !== newType) {
    const config = findPreviousSameTypeConfig(newType)
    form.value.url = config.url
    form.value.interval = config.interval
  }

  // 从负载均衡切换到其他类型，删除负载均衡特有字段
  if (oldType === 'load-balance' && newType !== 'load-balance') {
    delete form.value.strategy
    delete form.value.lazy
  }
})

// 处理跟随模式切换
const handleFollowChange = (checked: boolean) => {
  if (checked) {
    // 选择跟随时，清除其他来源
    enabledSources.value = ['follow']
    form.value.manual_nodes = []
    form.value.aggregations = []
    form.value.include_groups = []
    form.value.subscriptions = []
    form.value.regex = ''
  } else {
    // 取消跟随时，清除跟随策略
    form.value.follow_group = undefined
  }
}

// 监听节点来源勾选状态变化
watch(enabledSources, (newSources) => {
  // 如果取消勾选"节点"，清空手动节点列表
  if (!newSources.includes('node')) {
    form.value.manual_nodes = []
  }
  // 如果取消勾选"聚合"，清空聚合列表和聚合正则
  if (!newSources.includes('aggregation')) {
    form.value.aggregations = []
    form.value.aggregation_regex = ''
  }
  // 如果取消勾选"策略"，清空引用策略列表
  if (!newSources.includes('strategy')) {
    form.value.include_groups = []
  }
  // 如果取消勾选"订阅"，清空订阅列表
  if (!newSources.includes('subscription')) {
    form.value.subscriptions = []
    form.value.regex = ''
  }
  // 如果取消勾选"跟随"，清空跟随策略
  if (!newSources.includes('follow')) {
    form.value.follow_group = undefined
  }
}, { deep: true })

// 监听节点、聚合和策略组选择变化，自动同步排序列表
watch([() => form.value.manual_nodes, () => form.value.aggregations, () => form.value.include_groups], ([newNodes, newAggregations, newGroups]) => {
  const currentOrder = form.value.proxies_order || []
  const newOrder: Array<{type: string, id: string}> = []

  // 保留已有的顺序
  currentOrder.forEach(item => {
    if (item.type === 'node' && newNodes?.includes(item.id)) {
      newOrder.push(item)
    } else if (item.type === 'aggregation' && newAggregations?.includes(item.id)) {
      newOrder.push(item)
    } else if (item.type === 'strategy' && newGroups?.includes(item.id)) {
      newOrder.push(item)
    }
  })

  // 添加新选择的节点（不在原顺序中的）
  newNodes?.forEach(nodeId => {
    if (!newOrder.find(item => item.type === 'node' && item.id === nodeId)) {
      newOrder.push({ type: 'node', id: nodeId })
    }
  })

  // 添加新选择的聚合（不在原顺序中的）
  newAggregations?.forEach(aggId => {
    if (!newOrder.find(item => item.type === 'aggregation' && item.id === aggId)) {
      newOrder.push({ type: 'aggregation', id: aggId })
    }
  })

  // 添加新选择的策略组（不在原顺序中的）
  newGroups?.forEach(groupId => {
    if (!newOrder.find(item => item.type === 'strategy' && item.id === groupId)) {
      newOrder.push({ type: 'strategy', id: groupId })
    }
  })

  form.value.proxies_order = newOrder
}, { deep: true })

const getGroupTypeLabel = (type: string) => {
  const labels: Record<string, string> = {
    'select': '手动选择',
    'url-test': '自动测速',
    'fallback': '故障转移',
    'load-balance': '负载均衡'
  }
  return labels[type] || type
}

const getTypeTagType = (type: string) => {
  const types: Record<string, string> = {
    'select': 'primary',
    'url-test': 'success',
    'fallback': 'warning',
    'load-balance': 'danger'
  }
  return types[type] || 'primary'
}

const getStrategyLabel = (strategy: string) => {
  const labels: Record<string, string> = {
    'round-robin': '轮询',
    'consistent-hashing': '一致性哈希',
    'sticky-sessions': '会话保持'
  }
  return labels[strategy] || strategy
}

const getProxyOrderLabel = (order?: string) => {
  const labels: Record<string, string> = {
    'nodes_first': '节点优先',
    'strategies_first': '策略优先'
  }
  return labels[order || 'nodes_first'] || '节点优先'
}

const getGroupIcon = (name: string) => {
  // 从名称中提取emoji图标（支持更广泛的emoji范围，包括国旗）
  // 国旗emoji由两个区域指示符组成
  const flagMatch = name.match(/^[\u{1F1E6}-\u{1F1FF}]{2}/u)
  if (flagMatch) return flagMatch[0]

  // 其他常见emoji
  const emojiMatch = name.match(/^[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1F000}-\u{1F02F}\u{1F0A0}-\u{1F0FF}\u{1F100}-\u{1F64F}]/u)
  return emojiMatch ? emojiMatch[0] : null
}

const getGroupNameWithoutIcon = (name: string) => {
  // 去掉名称开头的emoji图标（包括国旗），只返回文字部分
  // 先移除国旗emoji（两个区域指示符）
  let result = name.replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '')
  // 再移除其他emoji
  result = result.replace(/^[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1F000}-\u{1F02F}\u{1F0A0}-\u{1F0FF}\u{1F100}-\u{1F64F}]\s*/u, '')
  return result.trim()
}

const getSubscriptionNames = (subIds: string[]) => {
  return subIds
    .map(id => {
      const sub = subscriptions.value.find(s => s.id === id)
      return sub ? sub.name : id
    })
    .filter(Boolean)
}

// 判断是否有订阅来源
const hasSubscriptions = (group: ProxyGroup) => {
  if (group.subscriptions && group.subscriptions.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'subscription' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 判断是否有手动节点
const hasManualNodes = (group: ProxyGroup) => {
  if (group.manual_nodes && group.manual_nodes.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'node' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 判断是否有引用聚合
const hasAggregations = (group: ProxyGroup) => {
  if (group.aggregations && group.aggregations.length > 0) {
    return true
  }
  return false
}

// 判断是否有引用策略组
const hasIncludeGroups = (group: ProxyGroup) => {
  if (group.include_groups && group.include_groups.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'strategy' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 获取订阅显示文本
const getSubscriptionDisplay = (group: ProxyGroup) => {
  const subIds = group.subscriptions || []
  const subNames = getSubscriptionNames(subIds)
  return subNames.join('、')
}

// 获取手动节点显示文本
const getManualNodesDisplay = (group: ProxyGroup) => {
  // 新格式：将节点 ID 转换为名称
  if (group.manual_nodes && group.manual_nodes.length > 0) {
    const nodeNames = group.manual_nodes.map(nodeId => {
      // DIRECT 和 REJECT 直接返回
      if (nodeId === 'DIRECT' || nodeId === 'REJECT') {
        return nodeId
      }
      // 根据 ID 查找节点名称
      const node = nodes.value.find(n => n.id === nodeId)
      return node ? node.name : nodeId
    })
    return nodeNames.join('、')
  }
  // 兼容旧格式
  if (group.source === 'node' && group.proxies && group.proxies.length > 0) {
    return group.proxies.join('、')
  }
  return '-'
}

// 获取引用聚合列表
const getAggregationsList = (group: ProxyGroup) => {
  // 将聚合 ID 转换为名称
  if (group.aggregations && group.aggregations.length > 0) {
    return group.aggregations.map(aggId => {
      // 根据 ID 查找聚合名称
      const agg = aggregations.value.find(a => a.id === aggId)
      return agg ? agg.name : aggId
    })
  }
  return []
}

// 获取聚合包含的订阅信息
const getAggregationSubscriptions = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allSubNames: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.subscriptions && agg.subscriptions.length > 0) {
      const subNames = agg.subscriptions.map((subId: string) => {
        const sub = subscriptions.value.find(s => s.id === subId)
        return sub ? sub.name : subId
      })
      allSubNames.push(...subNames)
    }
  })

  return allSubNames.length > 0 ? allSubNames.join('、') : ''
}

// 获取聚合包含的节点信息
const getAggregationNodes = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allNodeNames: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.nodes && agg.nodes.length > 0) {
      const nodeNames = agg.nodes.map((nodeId: string) => {
        if (nodeId === 'DIRECT' || nodeId === 'REJECT') {
          return nodeId
        }
        const node = nodes.value.find(n => n.id === nodeId)
        return node ? node.name : nodeId
      })
      allNodeNames.push(...nodeNames)
    }
  })

  return allNodeNames.length > 0 ? allNodeNames.join('、') : ''
}

// 获取聚合的正则过滤器
const getAggregationRegex = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allRegex: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.regex_filter && agg.regex_filter.trim()) {
      allRegex.push(agg.regex_filter.trim())
    }
  })

  return allRegex.length > 0 ? allRegex.join(' | ') : ''
}

// 获取引用策略组列表
const getIncludeGroupsList = (group: ProxyGroup) => {
  // 新格式：将策略组 ID 转换为名称
  if (group.include_groups && group.include_groups.length > 0) {
    return group.include_groups.map(groupId => {
      // 根据 ID 查找策略组名称
      const refGroup = proxyGroups.value.find(g => g.id === groupId)
      return refGroup ? refGroup.name : groupId
    })
  }
  // 兼容旧格式
  if (group.source === 'strategy' && group.proxies && group.proxies.length > 0) {
    return group.proxies
  }
  return []
}

// 获取跟随策略组名称
const getFollowGroupName = (groupId: string) => {
  const refGroup = proxyGroups.value.find(g => g.id === groupId)
  return refGroup ? refGroup.name : groupId
}

// 切换卡片展开/收起状态
const toggleCardExpand = (groupId: string) => {
  if (expandedCards.value.has(groupId)) {
    expandedCards.value.delete(groupId)
  } else {
    expandedCards.value.add(groupId)
  }
}

// 判断卡片是否展开
const isCardExpanded = (groupId: string) => {
  return expandedCards.value.has(groupId)
}

// 获取卡片的简要来源信息（用于折叠状态）
const getSourceSummary = (group: ProxyGroup) => {
  const sources: string[] = []

  if (group.follow_group) {
    const followName = getFollowGroupName(group.follow_group)
    return `跟随: ${followName}`
  }

  if (hasAggregations(group)) {
    const aggList = getAggregationsList(group)
    sources.push(`聚合(${aggList.length})`)
  }

  if (hasSubscriptions(group)) {
    const subIds = group.subscriptions || []
    sources.push(`订阅(${subIds.length})`)
  }

  if (hasManualNodes(group)) {
    const nodeCount = (group.manual_nodes || []).length
    sources.push(`节点(${nodeCount})`)
  }

  if (hasIncludeGroups(group)) {
    const groupCount = (group.include_groups || []).length
    sources.push(`策略(${groupCount})`)
  }

  return sources.length > 0 ? sources.join(' + ') : '无来源'
}

const getPreviewSourceLabel = (node: any) => {
  if (node.source_type === 'subscription') {
    return node.source_name ? `订阅: ${node.source_name}` : '订阅'
  }
  if (node.source_type === 'aggregation') {
    return node.source_name ? `聚合: ${node.source_name}` : '聚合'
  }
  return node.subscription_name || '节点'
}

const previewRegexMatches = async (source: 'subscription' | 'aggregation') => {
  const regex = source === 'subscription' ? form.value.regex : form.value.aggregation_regex
  const sourceIds = source === 'subscription' ? (form.value.subscriptions || []) : (form.value.aggregations || [])

  if (!sourceIds.length) {
    notify.warning(source === 'subscription' ? '请先选择订阅' : '请先选择聚合')
    return
  }

  if (!regex || !regex.trim()) {
    notify.warning('请先输入正则表达式')
    return
  }

  regexPreviewVisible.value = true
  regexPreviewLoading.value = true
  regexPreviewSource.value = source
  regexPreviewResult.value = null
  regexPreviewNodes.value = []
  regexPreviewTitle.value = source === 'subscription' ? '订阅正则匹配预览' : '聚合正则匹配预览'

  try {
    const payload = source === 'subscription'
      ? { source, regex, subscriptions: sourceIds }
      : { source, regex, aggregations: sourceIds }
    const { data } = await proxyGroupApi.previewRegex(payload)

    if (data.success) {
      regexPreviewResult.value = {
        total_candidates: data.total_candidates || 0
      }
      regexPreviewNodes.value = data.nodes || []
    } else {
      notify.error(data.message || '预览失败')
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || '预览失败')
  } finally {
    regexPreviewLoading.value = false
  }
}

const previewSavedRegexMatches = async (group: ProxyGroup, source: 'subscription' | 'aggregation') => {
  const regex = source === 'subscription' ? group.regex : group.aggregation_regex
  const sourceIds = source === 'subscription' ? (group.subscriptions || []) : (group.aggregations || [])

  if (!sourceIds.length) {
    notify.warning(source === 'subscription' ? '该策略组没有订阅来源' : '该策略组没有聚合来源')
    return
  }

  if (!regex || !regex.trim()) {
    notify.warning('该策略组没有配置正则表达式')
    return
  }

  regexPreviewVisible.value = true
  regexPreviewLoading.value = true
  regexPreviewSource.value = source
  regexPreviewResult.value = null
  regexPreviewNodes.value = []
  regexPreviewTitle.value = `${group.name} - ${source === 'subscription' ? '订阅正则匹配预览' : '聚合正则匹配预览'}`

  try {
    const payload = source === 'subscription'
      ? { source, regex, subscriptions: sourceIds }
      : { source, regex, aggregations: sourceIds }
    const { data } = await proxyGroupApi.previewRegex(payload)

    if (data.success) {
      regexPreviewResult.value = {
        total_candidates: data.total_candidates || 0
      }
      regexPreviewNodes.value = data.nodes || []
    } else {
      notify.error(data.message || '预览失败')
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || '预览失败')
  } finally {
    regexPreviewLoading.value = false
  }
}

const loadProxyGroups = async () => {
  try {
    const { data } = await proxyGroupApi.getAll()

    // 修复缺少 ID 的策略组
    let needsSave = false
    const fixedData = data.map((group: ProxyGroup, index: number) => {
      if (!group.id) {
        needsSave = true
        return {
          ...group,
          id: `group_${Date.now()}_${index}`
        }
      }
      return group
    })

    proxyGroups.value = fixedData

    proxyGroups.value.forEach(group => {
      if (group.id && savingStatus.value[group.id] === undefined) {
        savingStatus.value[group.id] = false
      }
    })

    // 如果有修复的数据，保存回后端
    if (needsSave) {
      try {
        await api.post('/proxy-groups/reorder', { groups: fixedData })
        console.log('已自动修复缺少ID的策略组')
      } catch (error) {
        console.error('保存修复后的策略组失败:', error)
      }
    }
  } catch (error) {
    notify.error('加载策略组列表失败')
  }
}

const loadNodes = async () => {
  try {
    const { data } = await nodeApi.getAll()
    nodes.value = data
  } catch (error) {
    notify.error('加载节点列表失败')
  }
}

const loadSubscriptions = async () => {
  try {
    const { data } = await api.get('/subscriptions')
    subscriptions.value = data
  } catch (error) {
    notify.error('加载订阅列表失败')
  }
}

const loadAggregations = async () => {
  try {
    const { data } = await api.get('/aggregations')
    aggregations.value = data
  } catch (error) {
    notify.error('加载聚合列表失败')
  }
}

const showAddDialog = () => {
  isEdit.value = false
  enabledSources.value = []
  // select 类型不需要 url 和 interval
  form.value = {
    id: `group_${Date.now()}`,
    name: '',
    type: 'select',
    enabled: true,
    subscriptions: [],
    regex: '',
    aggregation_regex: '',
    manual_nodes: [],
    aggregations: [],
    include_groups: [],
    proxies_order: []
  }
  dialogVisible.value = true
}

const handleToggle = (group: ProxyGroup) => {
  group.enabled = !group.enabled
  toggleGroupEnabled(group)
}

const toggleGroupEnabled = async (group: ProxyGroup) => {
  if (!group.id) return
  const previous = !group.enabled
  savingStatus.value[group.id] = true
  try {
    await proxyGroupApi.update(group.id, group)
    notify.success(group.enabled ? '已启用' : '已禁用')
  } catch (error) {
    notify.error('更新状态失败')
    group.enabled = previous
    loadProxyGroups()
  } finally {
    savingStatus.value[group.id] = false
  }
}

const editGroup = (row: ProxyGroup) => {
  isEdit.value = true

  // 保存原始策略组名称，用于检测名称变化
  originalGroupName.value = row.name

  // 数据迁移：处理旧格式数据
  let manual_nodes: string[] = []
  let group_aggregations: string[] = []
  let include_groups: string[] = []
  let subscriptions: string[] = row.subscriptions ? [...row.subscriptions] : []

  if (row.manual_nodes || row.aggregations || row.include_groups) {
    // 新格式数据
    manual_nodes = row.manual_nodes ? [...row.manual_nodes] : []
    group_aggregations = row.aggregations ? [...row.aggregations] : []
    include_groups = row.include_groups ? [...row.include_groups] : []

    // 数据清理:如果策略组有聚合,需要从subscriptions和manual_nodes中过滤掉聚合包含的订阅/节点
    if (group_aggregations.length > 0 && aggregations.value.length > 0) {
      // 收集所有聚合中的订阅和节点ID
      const aggregationSubIds = new Set<string>()
      const aggregationNodeIds = new Set<string>()

      group_aggregations.forEach(aggId => {
        // 使用响应式变量 aggregations.value 查找聚合定义
        const agg = aggregations.value.find((a: any) => a.id === aggId)
        if (agg) {
          // 收集聚合的订阅ID
          if (agg.subscriptions && Array.isArray(agg.subscriptions)) {
            agg.subscriptions.forEach((subId: string) => aggregationSubIds.add(subId))
          }
          // 收集聚合的节点ID
          if (agg.nodes && Array.isArray(agg.nodes)) {
            agg.nodes.forEach((nodeId: string) => aggregationNodeIds.add(nodeId))
          }
        }
      })

      // 从策略组的subscriptions中过滤掉聚合的订阅
      subscriptions = subscriptions.filter(subId => !aggregationSubIds.has(subId))
      // 从策略组的manual_nodes中过滤掉聚合的节点
      manual_nodes = manual_nodes.filter(nodeId => !aggregationNodeIds.has(nodeId))
    }
  } else if (row.proxies && row.proxies.length > 0) {
    // 旧格式数据，根据 source 判断
    const source = row.source || 'subscription'
    if (source === 'node') {
      manual_nodes = [...row.proxies]
    } else if (source === 'strategy') {
      include_groups = [...row.proxies]
    }
  }

  // 根据实际数据初始化 enabledSources
  const sources: string[] = []
  if (row.follow_group) {
    sources.push('follow')
  } else {
    if (subscriptions.length > 0) {
      sources.push('subscription')
    }
    if (manual_nodes.length > 0) {
      sources.push('node')
    }
    if (group_aggregations.length > 0) {
      sources.push('aggregation')
    }
    if (include_groups.length > 0) {
      sources.push('strategy')
    }
  }
  enabledSources.value = sources

  // 修复 proxies_order：确保包含所有已选择的节点、聚合和策略组
  let proxies_order = row.proxies_order || []
  const existingIds = new Set(proxies_order.map((item: any) => `${item.type}:${item.id}`))

  // 添加缺失的节点
  manual_nodes.forEach(nodeId => {
    if (!existingIds.has(`node:${nodeId}`)) {
      proxies_order.push({ type: 'node', id: nodeId })
    }
  })

  // 添加缺失的聚合
  group_aggregations.forEach(aggId => {
    if (!existingIds.has(`aggregation:${aggId}`)) {
      proxies_order.push({ type: 'aggregation', id: aggId })
    }
  })

  // 添加缺失的策略组
  include_groups.forEach(groupId => {
    if (!existingIds.has(`strategy:${groupId}`)) {
      proxies_order.push({ type: 'strategy', id: groupId })
    }
  })

  // 清理 proxies_order 中已被删除的项
  proxies_order = proxies_order.filter((item: any) => {
    if (item.type === 'node') return manual_nodes.includes(item.id)
    if (item.type === 'aggregation') return group_aggregations.includes(item.id)
    if (item.type === 'strategy') return include_groups.includes(item.id)
    return false
  })

  // 构建表单数据，select 类型不需要 url 和 interval
  const formData: any = {
    id: row.id,
    name: row.name,
    type: row.type,
    enabled: row.enabled !== undefined ? row.enabled : true,
    subscriptions,
    regex: row.regex || '',
    aggregation_regex: row.aggregation_regex || '',
    manual_nodes,
    aggregations: group_aggregations,
    include_groups,
    proxies_order,
    follow_group: row.follow_group
  }

  // 只有需要测速的类型才添加 url 和 interval
  if (row.type !== 'select') {
    formData.url = row.url
    formData.interval = row.interval
  }

  // 负载均衡类型特有字段
  if (row.type === 'load-balance') {
    if (row.strategy) {
      formData.strategy = row.strategy
    }
    if (row.lazy !== undefined && row.lazy !== null) {
      formData.lazy = row.lazy
    } else {
      // 默认值为 true
      formData.lazy = true
    }
  }

  form.value = formData
  dialogVisible.value = true
}

const saveGroup = async () => {
  if (!form.value.name) {
    notify.warning('请输入策略组名称')
    return
  }

  // 验证至少选择了一种来源
  const hasSubscriptions = form.value.subscriptions && form.value.subscriptions.length > 0
  const hasManualNodes = form.value.manual_nodes && form.value.manual_nodes.length > 0
  const hasAggregations = form.value.aggregations && form.value.aggregations.length > 0
  const hasIncludeGroups = form.value.include_groups && form.value.include_groups.length > 0
  const hasFollowGroup = form.value.follow_group !== undefined && form.value.follow_group !== null && form.value.follow_group !== ''

  if (!hasSubscriptions && !hasManualNodes && !hasAggregations && !hasIncludeGroups && !hasFollowGroup) {
    notify.warning('请至少选择一种节点来源（订阅、节点、聚合、策略或跟随）')
    return
  }

  // 跟随模式验证
  if (hasFollowGroup && !form.value.follow_group) {
    notify.warning('跟随模式下请选择要跟随的策略组')
    return
  }

  try {
    // 准备保存的数据
    const saveData = { ...form.value }

    // 根据 enabledSources 清理未选中来源的数据，避免产生脏数据
    if (!enabledSources.value.includes('subscription')) {
      saveData.subscriptions = []
      saveData.regex = ''
    }
    if (!enabledSources.value.includes('node')) {
      saveData.manual_nodes = []
    }
    if (!enabledSources.value.includes('aggregation')) {
      saveData.aggregations = []
      saveData.aggregation_regex = ''
    }
    if (!enabledSources.value.includes('strategy')) {
      saveData.include_groups = []
    }
    if (!enabledSources.value.includes('follow')) {
      delete saveData.follow_group
    }

    // 调试日志
    console.log('[策略组保存] 保存数据:', {
      name: saveData.name,
      enabledSources: enabledSources.value,
      subscriptions: saveData.subscriptions,
      aggregations: saveData.aggregations,
      manual_nodes: saveData.manual_nodes,
      include_groups: saveData.include_groups,
      proxies_order: saveData.proxies_order
    })

    // select 类型不需要 url 和 interval
    if (saveData.type === 'select') {
      delete saveData.url
      delete saveData.interval
    }

    // 如果负载策略和懒加载未选择，删除这些字段
    if (saveData.type === 'load-balance') {
      if (!saveData.strategy) {
        delete saveData.strategy
      }
      if (saveData.lazy === undefined || saveData.lazy === null) {
        delete saveData.lazy
      }
    }

    if (isEdit.value) {
      // 检查策略组名称是否发生变化
      const nameChanged = originalGroupName.value !== form.value.name

      if (nameChanged) {
        // 名称发生变化，需要同步更新所有引用该策略的规则配置
        try {
          // 获取所有规则配置
          const { data: allRules } = await api.get('/rules')

          // 找到所有引用该策略的规则（单条规则和规则集）
          const relatedRules = allRules.filter(
            (item: any) => item.policy === originalGroupName.value
          )

          if (relatedRules.length > 0) {
            // 批量更新规则的 policy 字段
            const updatePromises = relatedRules.map((rule: any) => {
              const updatedRule = { ...rule, policy: form.value.name }
              if (rule.itemType === 'rule') {
                return api.put(`/rules/${rule.id}`, updatedRule)
              } else if (rule.itemType === 'ruleset') {
                return api.put(`/rule-sets/${rule.id}`, updatedRule)
              }
              return Promise.resolve()
            })

            await Promise.all(updatePromises)
            console.log(`已同步更新 ${relatedRules.length} 个规则配置的策略引用`)
          }
        } catch (error) {
          console.error('同步更新规则配置失败:', error)
          notify.warning('策略组名称已更新，但部分规则配置同步失败，请手动检查')
        }
      }

      // 使用 id 进行API调用
      await proxyGroupApi.update(saveData.id!, saveData)
      notify.success('更新成功')
    } else {
      await proxyGroupApi.create(saveData)
      notify.success('添加成功')
    }
    dialogVisible.value = false
    loadProxyGroups()
  } catch (error) {
    notify.error('保存失败')
  }
}

const deleteGroup = async (row: ProxyGroup) => {
  if (!row.id) {
    notify.error('策略组缺少ID，无法删除')
    console.error('策略组数据异常，缺少ID字段:', row)
    return
  }

  try {
    // 先检查是否有规则配置引用了这个策略组
    const { data: allRules } = await api.get('/rules')
    const relatedRules = allRules.filter(
      (item: any) => item.policy === row.name
    )

    if (relatedRules.length > 0) {
      // 有关联的规则配置：一起删 / 仅删策略组 / 取消，三选一
      const choice = await choose(
        `该策略组被 ${relatedRules.length} 个规则配置引用，是否一起删除这些规则配置？`,
        {
          title: '删除策略组',
          confirmText: '一起删除',
          altText: '仅删除策略组',
          cancelText: '取消',
          danger: true
        }
      )
      if (choice === 'cancel') return

      if (choice === 'confirm') {
        // 先删除关联的规则配置
        for (const rule of relatedRules) {
          try {
            if (rule.itemType === 'rule') {
              await api.delete(`/rules/${rule.id}`)
            } else if (rule.itemType === 'ruleset') {
              await api.delete(`/rule-sets/${rule.id}`)
            }
          } catch (error) {
            console.error('删除规则配置失败:', error)
          }
        }
        await proxyGroupApi.delete(row.id)
        notify.success(`已删除策略组及 ${relatedRules.length} 个关联的规则配置`)
      } else {
        await proxyGroupApi.delete(row.id)
        notify.success('已删除策略组，关联的规则配置保留')
      }
      loadProxyGroups()
    } else {
      const ok = await confirmDanger('确定要删除该策略组吗？', { title: '删除策略组' })
      if (!ok) return

      await proxyGroupApi.delete(row.id)
      notify.success('删除成功')
      loadProxyGroups()
    }
  } catch (error) {
    notify.error('删除失败')
    console.error('删除策略组失败:', error)
  }
}



// 初始化已排序代理列表的拖拽功能
const initOrderedProxiesSortable = () => {
  nextTick(() => {
    if (orderedProxiesRef.value) {
      // 销毁已有实例
      if (orderedProxiesSortable) {
        orderedProxiesSortable.destroy()
      }

      orderedProxiesSortable = Sortable.create(orderedProxiesRef.value, {
        animation: 150,
        handle: '.drag-handle',
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        dragClass: 'sortable-drag',
        onEnd: (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex || oldIndex === undefined || newIndex === undefined) return

          // 更新 proxies_order 顺序
          const proxiesOrder = [...(form.value.proxies_order || [])]
          const movedItem = proxiesOrder.splice(oldIndex, 1)[0]
          proxiesOrder.splice(newIndex, 0, movedItem)
          form.value.proxies_order = proxiesOrder
        }
      })
    }
  })
}

// 监听对话框显示状态，初始化排序功能
watch(dialogVisible, (visible) => {
  if (visible) {
    // 对话框打开时，延迟初始化Sortable（等待DOM渲染）
    setTimeout(() => {
      initOrderedProxiesSortable()
    }, 100)
  } else {
    // 对话框关闭时，销毁Sortable实例
    if (orderedProxiesSortable) {
      orderedProxiesSortable.destroy()
      orderedProxiesSortable = null
    }
  }
})

// 监听排序列表变化，重新初始化Sortable
watch(orderedProxiesList, () => {
  if (dialogVisible.value && orderedProxiesList.value.length > 0) {
    initOrderedProxiesSortable()
  }
}, { deep: true })

// 折叠面板展开时才有 DOM，此时才初始化 Sortable
watch(orderPanelOpen, open => {
  if (open && orderedProxiesList.value.length > 0) {
    // 等待折叠动画渲染出列表节点
    setTimeout(() => initOrderedProxiesSortable(), 50)
  }
})

/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: proxyGroups,
  container: groupsContainer,
  labelOf: group => group.name || group.id,
  // 按 id 提交，服务端在存量数据上重排
  persist: async items => {
    await api.post('/proxy-groups/reorder', {
      ids: items.map(item => item.id),
      position: 'top'
    })
  }
})

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    notify.success('顺序已保存')
  } catch (error) {
    notify.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  loadProxyGroups().then(() => {
  })
  loadNodes()
  loadSubscriptions()

  loadAggregations()
})

onUnmounted(() => {
})
</script>


