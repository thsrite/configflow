<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner
      scope="resource"
      :profile-name="cfProfileName"
      description="把多个订阅与节点合并成一个可引用的集合"
    />

    <PageHeader
      eyebrow="Resource"
      title="订阅聚合"
      description="把多个订阅与节点合并成一个可引用的集合，供策略组直接引用。"
    >
      <template #actions>
        <Button
          v-if="!reorder.active.value"
          variant="outline"
          class="border-border/60 bg-background/40"
          :disabled="aggregations.length < 2"
          @click="reorder.enter"
        >
          <ArrowUpDown class="size-4" />
          调整顺序
        </Button>
        <Button class="shadow-glow" @click="showAddDialog">
          <Plus class="size-4" />
          添加聚合
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

    <SectionCard v-if="aggregations.length === 0" :padded="false">
      <EmptyState
        :icon="Share2"
        title="暂无聚合"
        description="聚合可以把若干订阅与独立节点合并成一个集合，再被策略组统一引用。"
      >
        <Button @click="showAddDialog">
          <Plus class="size-4" />
          添加聚合
        </Button>
      </EmptyState>
    </SectionCard>

    <div
      v-else
      ref="cardContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(360px,1fr))] gap-3 max-md:grid-cols-1"
    >
      <Motion
        v-for="(aggregation, cfIndex) in aggregations"
        :key="aggregation.id"
        v-bind="listItem(cfIndex)"
        :data-id="aggregation.id"
        data-reorder-item
        :class="[
          'hairline edge-light relative flex flex-col gap-3 overflow-hidden rounded-xl border border-border/35 bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft',
          !aggregation.enabled && 'opacity-60'
        ]"
      >
        <header class="flex items-start gap-2.5">
          <DragHandle
            v-if="reorder.active.value"
            :label="aggregation.name || aggregation.id"
            :index="cfIndex"
            :total="aggregations.length"
            :position="reorder.positionLabel(cfIndex)"
            :grabbed="reorder.grabbedIndex.value === cfIndex"
            @up="reorder.moveUp(cfIndex)"
            @down="reorder.moveDown(cfIndex)"
            @keydown="reorder.onHandleKeydown($event, cfIndex)"
          />
          <div class="min-w-0 flex-1">
            <p class="m-0 truncate text-[14px] font-semibold text-foreground">{{ aggregation.name }}</p>
            <p
              v-if="aggregation.description"
              class="mt-0.5 mb-0 truncate text-[12px] text-muted-foreground"
            >
              {{ aggregation.description }}
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            class="cf-reorder-mute shrink-0"
            :class="aggregation.enabled ? 'text-success-accent' : 'text-muted-foreground'"
            :title="aggregation.enabled ? '停用' : '启用'"
            :aria-label="aggregation.enabled ? `停用 ${aggregation.name}` : `启用 ${aggregation.name}`"
            :disabled="aggregation.id ? savingStatus[aggregation.id] : false"
            @click="handleToggle(aggregation)"
          >
            <component :is="aggregation.enabled ? Eye : EyeOff" class="size-4" />
          </Button>
        </header>

        <!-- 节点总数是这张卡最该被一眼看到的数字，单独占一行 -->
        <div
          class="cf-reorder-mute flex items-center gap-3 rounded-lg border border-border/40 bg-background/40 px-3 py-2"
        >
          <span class="text-[11.5px] tracking-[0.04em] text-muted-foreground uppercase">节点总数</span>
          <span class="num ml-auto text-[18px] leading-none font-semibold text-foreground">
            <Loader2 v-if="aggregation.loading_count" class="size-4 animate-spin" aria-hidden="true" />
            <template v-else>{{ aggregation.node_count ?? '-' }}</template>
          </span>
          <Badge variant="outline" class="text-[10.5px]">订阅 {{ aggregation.subscriptions.length }}</Badge>
          <Badge variant="outline" class="text-[10.5px]">节点 {{ aggregation.nodes.length }}</Badge>
        </div>

        <div class="cf-reorder-mute flex flex-col gap-2.5">
          <div>
            <p class="m-0 mb-1.5 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              <Link2 class="size-3" aria-hidden="true" />
              包含订阅
            </p>
            <div class="flex flex-wrap gap-1">
              <Badge
                v-for="subId in aggregation.subscriptions"
                :key="subId"
                variant="info"
                class="max-w-[180px] truncate text-[10.5px]"
              >
                {{ getSubscriptionName(subId) }}
              </Badge>
              <span v-if="!aggregation.subscriptions.length" class="text-[12px] text-muted-foreground">无</span>
            </div>
          </div>

          <div>
            <p class="m-0 mb-1.5 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              <Network class="size-3" aria-hidden="true" />
              包含节点
            </p>
            <div class="flex flex-wrap gap-1">
              <Badge
                v-for="nodeId in aggregation.nodes"
                :key="nodeId"
                variant="success"
                class="max-w-[180px] truncate text-[10.5px]"
              >
                {{ getNodeName(nodeId) }}
              </Badge>
              <span v-if="!aggregation.nodes.length" class="text-[12px] text-muted-foreground">无</span>
            </div>
          </div>

          <div v-if="aggregation.regex_filter">
            <p class="m-0 mb-1.5 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              <Filter class="size-3" aria-hidden="true" />
              正则过滤
            </p>
            <code
              class="block truncate rounded-md border border-border/50 bg-background/50 px-2 py-1 font-mono text-[11.5px] text-muted-foreground"
            >
              {{ aggregation.regex_filter }}
            </code>
          </div>
        </div>

        <footer class="cf-reorder-mute mt-auto flex items-center gap-1 border-0 border-t border-border/50 pt-3">
          <Button variant="ghost" size="sm" @click="handlePreviewNodes(aggregation)">
            <Eye class="size-3.5" />
            预览节点
          </Button>
          <Button variant="ghost" size="sm" @click="editAggregation(aggregation)">
            <Pencil class="size-3.5" />
            编辑
          </Button>
          <Button
            variant="ghost"
            size="sm"
            class="ml-auto text-destructive-accent hover:bg-destructive-soft"
            @click="deleteAggregation(aggregation)"
          >
            <Trash2 class="size-3.5" />
            删除
          </Button>
        </footer>
      </Motion>
    </div>

    <!-- ===== 新增 / 编辑聚合 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline max-w-[760px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑聚合' : '添加聚合' }}</DialogTitle>
          <DialogDescription>选择订阅或节点，构建一个新的聚合输出。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[62dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label for="agg-name">聚合名称</Label>
            <Input id="agg-name" v-model="form.name" class="bg-background/50" placeholder="请输入聚合名称" />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>选择订阅</Label>
            <MultiSelect
              v-model="form.subscriptions"
              :options="subscriptionOptions"
              placeholder="选择要包含的订阅"
            />
            <p class="m-0 text-[12px] text-muted-foreground">选择的订阅中的所有节点都会被包含在聚合中。</p>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>选择节点</Label>
            <MultiSelect
              v-model="form.nodes"
              :options="nodeOptions"
              placeholder="选择要包含的节点"
            />
            <p class="m-0 text-[12px] text-muted-foreground">可额外加入独立节点，与订阅节点一起输出。</p>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="agg-regex">正则过滤</Label>
            <Input
              id="agg-regex"
              v-model="form.regex_filter"
              class="bg-background/50 font-mono"
              placeholder="可选：过滤节点名称，例如 香港|HK"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="agg-health">健康检查</Label>
            <Input
              id="agg-health"
              v-model="form.health_check_url"
              class="bg-background/50 font-mono"
              placeholder="留空使用默认（http://www.gstatic.com/generate_204）"
            />
            <p class="m-0 text-[12px] text-muted-foreground">
              回家 / 内网聚合从国内出网，境外地址会被误判为失活，建议填 http://www.baidu.com
            </p>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="agg-desc">描述</Label>
            <Textarea
              id="agg-desc"
              v-model="form.description"
              class="bg-background/50"
              :rows="3"
              placeholder="可选：说明聚合用途"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2.5">
              <Switch id="agg-enabled" v-model="form.enabled" />
              <Label for="agg-enabled" class="text-[13px] text-muted-foreground">
                {{ form.enabled ? '聚合启用中' : '聚合已停用' }}
              </Label>
            </div>
            <p class="m-0 text-[12px] text-muted-foreground">停用后，该聚合不会出现在策略组的选择列表中。</p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button :disabled="saving" @click="saveAggregation">
            <Loader2 v-if="saving" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 节点预览 ===== -->
    <Dialog v-model:open="previewDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[660px] border-border/50">
        <DialogHeader>
          <DialogTitle>节点预览</DialogTitle>
          <DialogDescription>
            {{ previewLoading ? '正在拉取节点…' : `共 ${previewNodes.length} 个节点` }}
          </DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="previewLoading" :rows="4" />

        <div v-else class="flex max-h-[60dvh] flex-col gap-4 overflow-y-auto pr-1">
          <section v-if="Object.keys(previewSubscriptionCounts).length">
            <p class="m-0 mb-2 text-[11.5px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              订阅统计
            </p>
            <div class="flex flex-col gap-1.5">
              <button
                v-for="(count, subId) in previewSubscriptionCounts"
                :key="subId"
                type="button"
                class="flex cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-left text-[13px] transition-colors hover:border-border-strong hover:bg-accent/50"
                @click="showSubscriptionNodes({ id: String(subId), name: getSubscriptionName(String(subId)) })"
              >
                <Link2 class="size-3.5 shrink-0 text-info-accent" aria-hidden="true" />
                <span class="min-w-0 flex-1 truncate text-foreground">{{ getSubscriptionName(String(subId)) }}</span>
                <Badge variant="info" class="num shrink-0">{{ count }}</Badge>
              </button>
            </div>
          </section>

          <section v-if="previewNodes.length">
            <p class="m-0 mb-2 text-[11.5px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              所有节点
            </p>
            <div class="flex flex-col gap-1.5">
              <div
                v-for="(node, index) in previewNodes"
                :key="index"
                class="rounded-lg border border-border/50 bg-background/40"
              >
                <button
                  type="button"
                  class="flex w-full cursor-pointer items-center gap-2 border-0 bg-transparent px-3 py-2 text-left text-[13px]"
                  @click="togglePreviewExpand(index)"
                >
                  <Network class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <span class="min-w-0 flex-1 truncate text-foreground">{{ node.name }}</span>
                  <Badge variant="outline" class="shrink-0 font-mono text-[10px]">
                    {{ node.type?.toUpperCase() || 'UNKNOWN' }}
                  </Badge>
                  <span class="num shrink-0 font-mono text-[11.5px] text-muted-foreground">
                    {{ node.server }}:{{ node.port }}
                  </span>
                  <ChevronDown
                    class="size-3.5 shrink-0 transition-transform duration-200"
                    :class="expandedPreviewNodes.has(index) && 'rotate-180'"
                    aria-hidden="true"
                  />
                </button>
                <pre
                  v-show="expandedPreviewNodes.has(index)"
                  class="m-0 max-h-52 overflow-auto border-0 border-t border-border/50 p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-muted-foreground"
                >{{ formatNodeToYaml(node) }}</pre>
              </div>
            </div>
          </section>

          <EmptyState v-if="!previewNodes.length" :icon="Network" title="暂无节点" />
        </div>
      </DialogContent>
    </Dialog>

    <!-- ===== 单个订阅的节点列表 ===== -->
    <Dialog v-model:open="subscriptionNodesDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[600px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ currentSubscription.name }} · 节点列表</DialogTitle>
          <DialogDescription>
            {{ subscriptionNodesLoading ? '正在拉取节点…' : `共 ${subscriptionNodes.length} 个节点` }}
          </DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="subscriptionNodesLoading" :rows="4" />

        <div v-else class="flex max-h-[55dvh] flex-col gap-1.5 overflow-y-auto pr-1">
          <div
            v-for="node in subscriptionNodes"
            :key="node.id"
            class="flex items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-[13px]"
          >
            <Network class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span class="min-w-0 flex-1 truncate text-foreground">{{ node.name }}</span>
            <Badge variant="outline" class="shrink-0 font-mono text-[10px]">{{ node.type }}</Badge>
          </div>
          <EmptyState v-if="!subscriptionNodes.length" :icon="Network" title="暂无节点" />
        </div>

        <DialogFooter>
          <Button variant="outline" @click="subscriptionNodesDialogVisible = false">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, nextTick } from 'vue'
import { Motion } from 'motion-v'
import {
  ArrowUpDown,
  ChevronDown,
  Eye,
  EyeOff,
  Filter,
  Link2,
  Loader2,
  Network,
  Pencil,
  Plus,
  Share2,
  Trash2
} from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import { confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { useProfileStore } from '@/stores/profile'
import api from '@/api'
import * as yaml from 'js-yaml'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
interface Subscription {
  id: string
  name: string
}

interface Node {
  id: string
  name: string
}

interface Aggregation {
  id?: string
  name: string
  subscriptions: string[]
  nodes: string[]
  description?: string
  enabled?: boolean
  regex_filter?: string
  /** 健康检查地址，留空用默认（境外）；回家/内网聚合建议填国内地址 */
  health_check_url?: string
  created_at?: string
  updated_at?: string
  node_count?: number
  loading_count?: boolean
}

const aggregations = ref<Aggregation[]>([])
const subscriptions = ref<Subscription[]>([])
const nodes = ref<Node[]>([])
const savingStatus = ref<Record<string, boolean>>({})

const dialogVisible = ref(false)
const isEdit = ref(false)
const saving = ref(false)
const cardContainer = ref<HTMLElement | null>(null)

const previewDialogVisible = ref(false)
const previewLoading = ref(false)
const previewNodes = ref<any[]>([])
const previewSubscriptionCounts = ref<Record<string, number>>({})
const expandedPreviewNodes = ref<Set<number>>(new Set())

const subscriptionNodesDialogVisible = ref(false)
const subscriptionNodesLoading = ref(false)
const subscriptionNodes = ref<any[]>([])
const currentSubscription = ref<{ id: string; name: string }>({ id: '', name: '' })

const form = ref<Aggregation>({
  name: '',
  subscriptions: [],
  nodes: [],
  description: '',
  enabled: true,
  regex_filter: '',
  health_check_url: ''
})

const loadAggregationNodeCount = async (aggregation: Aggregation) => {
  if (!aggregation.id) return

  try {
    aggregation.loading_count = true
    const response = await api.get(`/aggregations/${aggregation.id}/count`)
    if (response.data.success) {
      aggregation.node_count = response.data.total_count
    }
  } catch (error) {
    console.error('Failed to load node count:', error)
    aggregation.node_count = 0
  } finally {
    aggregation.loading_count = false
  }
}

const loadAggregations = async () => {
  try {
    const response = await api.get('/aggregations')
    aggregations.value = response.data

    aggregations.value.forEach(agg => {
      if (agg.id && savingStatus.value[agg.id] === undefined) {
        savingStatus.value[agg.id] = false
      }
      // 从本地缓存加载节点数量
      loadAggregationNodeCount(agg)
    })
  } catch (error) {
    console.error('Failed to load aggregations:', error)
    notify.error('加载聚合失败')
  }
}

const loadSubscriptions = async () => {
  try {
    const response = await api.get('/subscriptions')
    subscriptions.value = response.data.filter((sub: any) => sub.enabled !== false)
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
  }
}

const loadNodes = async () => {
  try {
    const response = await api.get('/nodes')
    nodes.value = response.data.filter((node: any) => node.enabled !== false)
  } catch (error) {
    console.error('Failed to load nodes:', error)
  }
}

/* MultiSelect 需要 {value,label}，由订阅/节点列表派生 */
const subscriptionOptions = computed(() =>
  subscriptions.value.map(sub => ({ value: sub.id, label: sub.name }))
)
const nodeOptions = computed(() => nodes.value.map(node => ({ value: node.id, label: node.name })))

const getSubscriptionName = (subId: string) => {
  const sub = subscriptions.value.find(s => s.id === subId)
  return sub ? sub.name : subId
}

const getNodeName = (nodeId: string) => {
  const node = nodes.value.find(n => n.id === nodeId)
  return node ? node.name : nodeId
}

const formatDate = (dateString: string) => {
  if (!dateString) return '-'
  const date = new Date(dateString)
  return date.toLocaleString('zh-CN')
}

const showAddDialog = () => {
  isEdit.value = false
  form.value = {
    name: '',
    subscriptions: [],
    nodes: [],
    description: '',
    enabled: true,
    regex_filter: ''
  }
  dialogVisible.value = true
}

const handleToggle = (aggregation: Aggregation) => {
  aggregation.enabled = !aggregation.enabled
  toggleAggregationEnabled(aggregation)
}

const toggleAggregationEnabled = async (aggregation: Aggregation) => {
  if (!aggregation.id) return
  const previous = !aggregation.enabled
  savingStatus.value[aggregation.id] = true
  try {
    await api.put(`/aggregations/${aggregation.id}`, aggregation)
    notify.success(aggregation.enabled ? '已启用' : '已禁用')
  } catch (error) {
    notify.error('更新状态失败')
    aggregation.enabled = previous
    loadAggregations()
  } finally {
    savingStatus.value[aggregation.id] = false
  }
}

const editAggregation = (aggregation: Aggregation) => {
  isEdit.value = true
  form.value = { ...aggregation }
  dialogVisible.value = true
}

const saveAggregation = async () => {
  if (!form.value.name) {
    notify.warning('请输入聚合名称')
    return
  }

  if (form.value.subscriptions.length === 0 && form.value.nodes.length === 0) {
    notify.warning('请至少选择一个订阅或节点')
    return
  }

  try {
    saving.value = true

    if (isEdit.value && form.value.id) {
      await api.put(`/aggregations/${form.value.id}`, form.value)
      notify.success('聚合已更新')
    } else {
      await api.post('/aggregations', form.value)
      notify.success('聚合已创建')
    }

    dialogVisible.value = false
    await loadAggregations()
  } catch (error) {
    console.error('Failed to save aggregation:', error)
    notify.error('保存失败')
  } finally {
    saving.value = false
  }
}

const deleteAggregation = async (aggregation: Aggregation) => {
  const ok = await confirmDanger(`确定要删除聚合「${aggregation.name}」吗？`, {
    title: '删除聚合'
  })
  if (!ok) return

  try {
    await api.delete(`/aggregations/${aggregation.id}`)
    notify.success('聚合已删除')

    if (aggregation.id) {
      delete savingStatus.value[aggregation.id]
    }

    await loadAggregations()
  } catch (error) {
    console.error('Failed to delete aggregation:', error)
    notify.error('删除失败')
  }
}

const togglePreviewExpand = (index: number) => {
  if (expandedPreviewNodes.value.has(index)) {
    expandedPreviewNodes.value.delete(index)
  } else {
    expandedPreviewNodes.value.add(index)
  }
  expandedPreviewNodes.value = new Set(expandedPreviewNodes.value)
}

const formatNodeToYaml = (node: any) => {
  return yaml.dump(node, { indent: 2, lineWidth: -1 }).trim()
}

const handlePreviewNodes = async (aggregation: Aggregation) => {
  if (!aggregation.id) return
  previewDialogVisible.value = true
  previewLoading.value = true
  expandedPreviewNodes.value = new Set()

  try {
    const response = await api.get(`/aggregations/${aggregation.id}/preview`)
    if (response.data.success) {
      previewNodes.value = response.data.nodes || []
      previewSubscriptionCounts.value = response.data.subscription_node_counts || {}

      // 预览后更新列表中的节点数量（因为预览会更新各订阅的本地缓存）
      aggregation.node_count = response.data.count || 0
    } else {
      notify.error(response.data.message || '获取节点失败')
    }
  } catch (error) {
    console.error('Failed to preview nodes:', error)
    notify.error('获取节点失败')
  } finally {
    previewLoading.value = false
  }
}

const showSubscriptionNodes = async (subscription: { id: string; name: string }) => {
  currentSubscription.value = subscription
  subscriptionNodesDialogVisible.value = true
  subscriptionNodesLoading.value = true
  subscriptionNodes.value = []

  try {
    const response = await api.post(`/subscriptions/${subscription.id}/fetch`, { preview: true })
    if (response.data.success) {
      subscriptionNodes.value = response.data.nodes || []
    } else {
      notify.error(response.data.message || '获取节点列表失败')
    }
  } catch (error) {
    console.error('Failed to get subscription nodes:', error)
    notify.error('获取节点列表失败')
  } finally {
    subscriptionNodesLoading.value = false
  }
}


/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: aggregations,
  container: cardContainer,
  labelOf: item => item.name || item.id,
  // 按 id 提交，服务端在存量数据上重排，不回写列表接口的计算字段
  persist: async items => {
    await api.post('/aggregations/reorder', {
      ids: items.map(item => item.id),
      position: 'top'
    })
  }
})

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    notify.success('顺序已保存，所有配置空间生效')
  } catch (error) {
    notify.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  await loadAggregations()
  await Promise.all([loadSubscriptions(), loadNodes()])
})
</script>

