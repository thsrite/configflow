<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner scope="resource" :profile-name="cfProfileName" description="订阅源按配置空间隔离，切换配置空间会看到各自的列表" />

    <PageHeader title="订阅来源" description="订阅拉取后的节点进入本配置空间的节点库">
      <template #actions>
        <Button variant="outline" :disabled="isRefreshing || reorder.active.value" @click="handleFetchAll">
          <Loader2 v-if="isRefreshing" class="size-4 animate-spin" />
          <RefreshCw v-else class="size-4" />
          批量更新
        </Button>
        <Button :disabled="reorder.active.value" @click="showAddDialog">
          <Plus class="size-4" />
          添加订阅
        </Button>
      </template>
    </PageHeader>

    <!-- 工具栏：搜索 + 筛选 + 排序入口 + 视图切换 -->
    <div class="mb-4 flex flex-wrap items-center gap-2">
      <div class="relative min-w-0 flex-[1_1_240px] max-w-md">
        <Search class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          v-model="keyword"
          class="h-9 pl-9"
          placeholder="搜索订阅名称或地址"
          :disabled="reorder.active.value"
          aria-label="搜索订阅"
        />
      </div>

      <Select v-model="statusFilter" :disabled="reorder.active.value">
        <SelectTrigger class="h-9 w-[132px]" aria-label="按状态筛选">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">全部状态</SelectItem>
          <SelectItem value="enabled">已启用</SelectItem>
          <SelectItem value="disabled">已停用</SelectItem>
          <SelectItem value="error">获取失败</SelectItem>
        </SelectContent>
      </Select>

      <Button
        v-if="!reorder.active.value"
        variant="outline"
        class="h-9"
        :disabled="subscriptions.length < 2"
        @click="reorder.enter"
      >
        <ArrowUpDown class="size-4" />
        调整顺序
      </Button>

      <!-- 数据密集区默认表格，卡片作为可选视图 -->
      <div
        class="ml-auto flex shrink-0 gap-0.5 rounded-md border border-border bg-secondary p-0.5 max-[900px]:hidden"
        role="group"
        aria-label="视图切换"
      >
        <button
          v-for="opt in VIEW_OPTIONS"
          :key="opt.value"
          type="button"
          :class="cn(
            'grid size-8 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground transition-colors',
            viewMode === opt.value && 'bg-card text-foreground shadow-xs'
          )"
          :aria-pressed="viewMode === opt.value"
          :aria-label="opt.label"
          @click="viewMode = opt.value"
        >
          <component :is="opt.icon" class="size-4" />
        </button>
      </div>
    </div>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      :hint="reorderHint"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <Card v-if="visibleSubscriptions.length === 0" class="py-0">
      <p class="m-0 px-5 py-14 text-center text-[13px] text-muted-foreground">{{ emptyText }}</p>
    </Card>

    <!-- ===== 表格视图（桌面默认） ===== -->
    <Card v-else-if="effectiveView === 'list'" class="gap-0 overflow-hidden py-0">
      <div class="overflow-x-auto">
        <table class="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              <th v-if="reorder.active.value" :class="TH" scope="col"><span class="cf-sr">排序</span></th>
              <th :class="cn(TH, 'w-11')" scope="col">#</th>
              <th :class="TH" scope="col">名称</th>
              <th :class="TH" scope="col">地址</th>
              <th :class="cn(TH, 'text-right')" scope="col">节点</th>
              <th :class="TH" scope="col">最近更新</th>
              <th :class="TH" scope="col">状态</th>
              <th :class="cn(TH, 'text-right')" scope="col">操作</th>
            </tr>
          </thead>
          <tbody ref="subscriptionsContainer">
            <tr
              v-for="(sub, index) in visibleSubscriptions"
              :key="sub.id"
              :data-id="sub.id"
              data-reorder-item
              :class="cn('transition-colors hover:bg-accent/40 last:[&>td]:border-b-0', !sub.enabled && 'opacity-60')"
            >
              <td v-if="reorder.active.value" :class="cn(TD, 'pr-0')">
                <DragHandle
                  :label="sub.name"
                  :index="index"
                  :total="subscriptions.length"
                  :position="reorder.positionLabel(index)"
                  :grabbed="reorder.grabbedIndex.value === index"
                  @up="reorder.moveUp(index)"
                  @down="reorder.moveDown(index)"
                  @keydown="reorder.onHandleKeydown($event, index)"
                />
              </td>
              <td :class="cn(TD, 'w-11 text-xs tabular-nums text-muted-foreground')">{{ index + 1 }}</td>
              <td :class="TD">
                <div class="flex items-center gap-2">
                  <span :class="cn('size-1.5 shrink-0 rounded-full', dotTone(sub))" aria-hidden="true" />
                  <span class="font-medium whitespace-nowrap text-foreground">{{ sub.name }}</span>
                  <Badge :variant="typeTone(sub.type)">{{ getTypeLabel(sub.type) }}</Badge>
                </div>
              </td>
              <td :class="cn(TD, 'max-w-[320px]')">
                <div class="flex items-center gap-1">
                  <span class="cf-mono min-w-0 truncate text-muted-foreground">{{ getDisplayUrl(sub) }}</span>
                  <button
                    type="button"
                    class="cf-reorder-mute grid size-6 shrink-0 cursor-pointer place-items-center rounded-sm border-0 bg-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    :aria-label="`复制 ${sub.name} 的地址`"
                    @click="copyUrl(sub)"
                  >
                    <Copy class="size-3.5" />
                  </button>
                </div>
              </td>
              <td :class="cn(TD, 'text-right tabular-nums')">{{ nodeCount(sub) }}</td>
              <td :class="cn(TD, 'whitespace-nowrap text-muted-foreground')">{{ lastUpdated(sub) }}</td>
              <td :class="TD">
                <Badge :variant="statusTone(subscriptionStatus[sub.id])">{{ statusText(sub) }}</Badge>
              </td>
              <td :class="cn(TD, 'cf-reorder-mute text-right whitespace-nowrap')">
                <Button variant="ghost" size="icon-sm" :aria-label="`获取 ${sub.name} 的节点`" @click="handleFetchSubscription(sub)">
                  <Network class="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" :aria-label="`编辑 ${sub.name}`" @click="editSubscription(sub)">
                  <Pencil class="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="text-destructive-accent hover:bg-destructive-soft hover:text-destructive-accent"
                  :aria-label="`删除 ${sub.name}`"
                  @click="deleteSubscription(sub)"
                >
                  <Trash2 class="size-4" />
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="border-t border-border px-5 py-2.5 text-xs text-muted-foreground">
        共 {{ visibleSubscriptions.length }} 条
      </footer>
    </Card>

    <!-- ===== 卡片视图（移动端与可选） ===== -->
    <div v-else ref="subscriptionsContainer" class="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3">
      <Card
        v-for="(sub, index) in visibleSubscriptions"
        :key="sub.id"
        :class="cn('gap-0 py-0', !sub.enabled && 'opacity-60')"
        :data-id="sub.id"
        data-reorder-item
      >
        <div class="card-header flex items-center gap-2 px-4 pt-3.5 pb-2">
          <span v-if="reorder.active.value" class="text-xs tabular-nums text-muted-foreground">{{ index + 1 }}</span>
          <span :class="cn('size-1.5 shrink-0 rounded-full', dotTone(sub))" aria-hidden="true" />
          <div class="card-title min-w-0 flex-1 truncate text-sm font-semibold text-foreground">{{ sub.name }}</div>

          <DragHandle
            v-if="reorder.active.value"
            :label="sub.name"
            :index="index"
            :total="subscriptions.length"
            :position="reorder.positionLabel(index)"
            :grabbed="reorder.grabbedIndex.value === index"
            @up="reorder.moveUp(index)"
            @down="reorder.moveDown(index)"
            @keydown="reorder.onHandleKeydown($event, index)"
          />
          <Button
            v-else
            variant="ghost"
            size="icon-sm"
            class="cf-reorder-mute shrink-0"
            :class="sub.enabled ? 'text-primary-accent' : 'text-muted-foreground'"
            :aria-label="sub.enabled ? `停用 ${sub.name}` : `启用 ${sub.name}`"
            @click="handleToggle(sub)"
          >
            <Eye v-if="sub.enabled" class="size-4" />
            <EyeOff v-else class="size-4" />
          </Button>
        </div>

        <template v-if="!reorder.active.value">
          <div class="card-meta flex flex-wrap items-center gap-1.5 px-4 pb-2">
            <Badge :variant="typeTone(sub.type)">{{ getTypeLabel(sub.type) }}</Badge>
            <Badge :variant="statusTone(subscriptionStatus[sub.id])">{{ statusText(sub) }}</Badge>
            <span class="text-xs text-muted-foreground">更新周期 {{ formatInterval(sub.interval) }}</span>
          </div>

          <div class="flex items-center gap-2 px-4 pb-2">
            <span class="cf-mono min-w-0 flex-1 truncate text-xs text-muted-foreground">{{ getDisplayUrl(sub) }}</span>
            <button
              type="button"
              class="shrink-0 cursor-pointer border-0 bg-transparent text-xs font-medium text-primary-accent hover:underline"
              @click="toggleUrlReveal(sub.id)"
            >
              {{ revealedUrls[sub.id] ? '隐藏' : '显示原文' }}
            </button>
          </div>

          <Separator />

          <div class="card-actions flex items-center gap-1 px-3 py-2">
            <Button variant="ghost" size="sm" @click="handleFetchSubscription(sub)">
              <Network class="size-3.5" />
              获取节点
            </Button>
            <Button variant="ghost" size="sm" @click="editSubscription(sub)">
              <Pencil class="size-3.5" />
              编辑
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="ml-auto text-destructive-accent hover:bg-destructive-soft hover:text-destructive-accent"
              @click="deleteSubscription(sub)"
            >
              <Trash2 class="size-3.5" />
              删除
            </Button>
          </div>
        </template>
      </Card>
    </div>

    <!-- 添加/编辑对话框 -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="sm:max-w-[640px]" @pointer-down-outside.prevent>
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑订阅' : '添加订阅' }}</DialogTitle>
          <DialogDescription>配置订阅名称、链接与同步策略以保持节点数据最新</DialogDescription>
        </DialogHeader>

        <div class="grid gap-4">
          <div class="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
            <div class="grid gap-2">
              <Label for="sub-name">订阅名称</Label>
              <Input id="sub-name" v-model="form.name" placeholder="请输入订阅名称" />
            </div>
            <div class="grid gap-2">
              <Label for="sub-type">订阅类型</Label>
              <Select v-model="form.type">
                <SelectTrigger id="sub-type" class="w-full">
                  <SelectValue placeholder="请选择订阅类型" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="universal">通用</SelectItem>
                  <SelectItem value="mihomo">Mihomo</SelectItem>
                  <SelectItem value="surge">Surge</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div class="grid gap-2">
            <Label for="sub-url">订阅链接</Label>
            <Textarea id="sub-url" v-model="form.url" :rows="3" placeholder="请输入订阅 URL" />
          </div>

          <div class="grid gap-2">
            <Label for="sub-interval">更新间隔</Label>
            <div class="flex items-center gap-2">
              <Input
                id="sub-interval"
                v-model.number="form.interval"
                type="number"
                :min="60"
                :max="604800"
                :step="3600"
                class="w-40"
              />
              <span class="text-xs text-muted-foreground">秒（建议 86400 = 1 天）</span>
            </div>
          </div>

          <div class="grid gap-2">
            <Label for="sub-health">健康检查</Label>
            <Input
              id="sub-health"
              v-model="form.health_check_url"
              placeholder="留空使用默认（http://www.gstatic.com/generate_204）"
            />
            <p class="m-0 text-xs text-muted-foreground">
              回家 / 内网订阅从国内出网，境外地址会被误判为失活，建议填 http://www.baidu.com
            </p>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch id="sub-enabled" v-model="form.enabled" />
            <Label for="sub-enabled" class="font-normal text-muted-foreground">
              {{ form.enabled ? '订阅启用中' : '订阅已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button @click="saveSubscription">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- 节点预览对话框 -->
    <Dialog v-model:open="nodesPreviewVisible">
      <DialogContent class="sm:max-w-[800px]">
        <DialogHeader>
          <DialogTitle>节点预览</DialogTitle>
          <DialogDescription>共 {{ previewNodes.length }} 个节点</DialogDescription>
        </DialogHeader>

        <div class="max-h-[500px] overflow-y-auto">
          <p v-if="previewNodes.length === 0" class="m-0 py-10 text-center text-[13px] text-muted-foreground">
            暂无节点
          </p>
          <div
            v-for="(node, index) in previewNodes"
            :key="node.id"
            class="cursor-pointer border-b border-border py-2.5 last:border-b-0"
            @click="togglePreviewExpand(index)"
          >
            <div class="flex items-center gap-2 text-[13px]">
              <Network class="size-4 shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate font-medium text-foreground">{{ node.name }}</span>
              <Badge variant="secondary">{{ node.type?.toUpperCase() || 'UNKNOWN' }}</Badge>
              <span class="cf-mono shrink-0 text-xs text-muted-foreground">{{ node.server }}:{{ node.port }}</span>
              <ChevronDown
                :class="cn('size-4 shrink-0 text-muted-foreground transition-transform', expandedPreviewNodes.has(index) && 'rotate-180')"
              />
            </div>
            <pre
              v-show="expandedPreviewNodes.has(index)"
              class="cf-mono mt-2 mb-0 overflow-x-auto rounded-md bg-secondary p-3 text-xs leading-relaxed text-muted-foreground"
              @click.stop
            >{{ formatNodeToYaml(node) }}</pre>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="nodesPreviewVisible = false">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { useProfileStore } from '@/stores/profile'
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElLoading, ElMessageBox } from 'element-plus'
import {
  ArrowUpDown,
  ChevronDown,
  Copy,
  Eye,
  EyeOff,
  LayoutGrid,
  List,
  Loader2,
  Network,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Trash2
} from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { subscriptionApi, subStoreUrlApi } from '@/api'
import type { Subscription } from '@/types'
import api from '@/api'
import yaml from 'js-yaml'
import PageHeader from '@/components/shell/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'


/* 表格骨架样式：本页已脱离全局 .cf-table（仍由未改造页面使用） */
const TH =
  'border-b border-border px-5 py-2 text-left text-[11px] font-semibold tracking-wide whitespace-nowrap text-muted-foreground uppercase'
const TD = 'border-b border-border px-5 py-2.5 align-middle'

const VIEW_OPTIONS = [
  { value: 'list' as const, label: '列表视图', icon: List },
  { value: 'card' as const, label: '卡片视图', icon: LayoutGrid }
]

const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
const subscriptions = ref<Subscription[]>([])
const subscriptionsContainer = ref<HTMLElement | null>(null)
const dialogVisible = ref(false)
const isEdit = ref(false)
const isRefreshing = ref(false)
// 使用构建时常量控制专业功能
// 处理按钮点击
const handleFetchAll = () => {
  fetchAllSubscriptionsInBackground()
}

const handleFetchSubscription = (sub: Subscription) => {
  fetchSubscription(sub)
}

const form = ref<Partial<Subscription>>({
  name: '',
  url: '',
  type: 'universal',
  enabled: true,
  interval: 86400,
  health_check_url: ''
})

// 节点预览相关
const nodesPreviewVisible = ref(false)
const previewNodes = ref<any[]>([])
const expandedPreviewNodes = ref<Set<number>>(new Set())
const revealedUrls = ref<Record<string, boolean>>({})

// 订阅状态管理
interface SubscriptionStatusItem {
  status: 'idle' | 'loading' | 'success' | 'error'
  count?: number
  error?: string
  updatedAt?: string | null
}
const subscriptionStatus = ref<Record<string, SubscriptionStatusItem>>({})

// 获取类型显示标签
const getTypeLabel = (type: string) => {
  const labels: Record<string, string> = {
    'mihomo': 'Mihomo',
    'surge': 'Surge',
    'universal': '通用'
  }
  return labels[type] || type
}

// 订阅类型标签配色
const typeToneMap: Record<string, 'info' | 'brand' | 'secondary'> = {
  mihomo: 'info',
  surge: 'brand',
  universal: 'secondary'
}
const typeTone = (type: string) => typeToneMap[type] || 'secondary'

// 节点获取状态标签配色
const statusTone = (status?: SubscriptionStatusItem) => {
  if (!status || status.status === 'idle') return 'secondary' as const
  if (status.status === 'loading') return 'warning' as const
  if (status.status === 'success') return 'success' as const
  return 'danger' as const
}

// 格式化更新间隔
const formatInterval = (seconds: number) => {
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)} 分钟`
  } else if (seconds < 86400) {
    return `${Math.floor(seconds / 3600)} 小时`
  } else {
    return `${Math.floor(seconds / 86400)} 天`
  }
}

// 切换预览节点展开/收起
const togglePreviewExpand = (index: number) => {
  if (expandedPreviewNodes.value.has(index)) {
    expandedPreviewNodes.value.delete(index)
  } else {
    expandedPreviewNodes.value.add(index)
  }
  expandedPreviewNodes.value = new Set(expandedPreviewNodes.value)
}

// 将节点数据格式化为 YAML
const formatNodeToYaml = (node: any) => {
  const { id, ...rest } = node
  const proxy: Record<string, any> = {
    name: rest.name,
    type: rest.type,
    server: rest.server,
    port: rest.port,
    ...rest.params
  }
  return yaml.dump(proxy, { indent: 2, lineWidth: -1 }).trim()
}

const loadSubscriptions = async () => {
  try {
    const { data } = await subscriptionApi.getAll()
    subscriptions.value = data
    // 初始化订阅状态
    subscriptions.value.forEach(sub => {
      if (revealedUrls.value[sub.id] === undefined) {
        revealedUrls.value[sub.id] = false
      }
      const cachedCount = typeof sub.cached_node_count === 'number' ? sub.cached_node_count : null
      const updatedAt = sub.cached_updated_at ?? null
      if (cachedCount !== null) {
        subscriptionStatus.value[sub.id] = {
          status: 'success',
          count: cachedCount,
          updatedAt
        }
      } else {
        subscriptionStatus.value[sub.id] = { status: 'idle' }
      }
    })
  } catch (error) {
    ElMessage.error('加载订阅列表失败')
  }
}

// 后台获取所有订阅的节点数量
const fetchAllSubscriptionsInBackground = async () => {
  if (isRefreshing.value) return

  isRefreshing.value = true
  try {
    // 并发获取所有订阅的节点预览
    const promises = subscriptions.value.map(async (sub) => {
      // 跳过已禁用的订阅
      if (!sub.enabled) {
        return
      }

      try {
        const previous = subscriptionStatus.value[sub.id]
        subscriptionStatus.value[sub.id] = {
          status: 'loading',
          count: previous?.count,
          updatedAt: previous?.updatedAt
        }
        const { data } = await subscriptionApi.fetch(sub.id, true)

        if (data.success) {
          const cachedCount = data.cached_count ?? (data.nodes?.length || 0)
          const updatedAt = data.cached_updated_at ?? null
          sub.cached_node_count = cachedCount
          sub.cached_updated_at = updatedAt
          subscriptionStatus.value[sub.id] = {
            status: 'success',
            count: cachedCount,
            updatedAt
          }
        } else {
          subscriptionStatus.value[sub.id] = {
            status: 'error',
            error: data.message
          }
        }
      } catch (error: any) {
        subscriptionStatus.value[sub.id] = {
          status: 'error',
          error: error.response?.data?.message || '获取失败'
        }
      }
    })

    await Promise.allSettled(promises)
  } finally {
    isRefreshing.value = false
  }
}

const checkSubStoreUrl = async (): Promise<boolean> => {
  try {
    const response = await subStoreUrlApi.get()
    const url = response.data?.sub_store_url || ''
    if (!url) {
      await ElMessageBox.confirm(
        '尚未配置 Sub-Store URL，订阅解析和节点格式转换功能将不可用。请前往「生成配置」页面配置 Sub-Store 地址。',
        '提示',
        {
          confirmButtonText: '继续添加',
          cancelButtonText: '取消',
          type: 'warning'
        }
      )
    }
    return true
  } catch (error: any) {
    if (error === 'cancel' || error?.toString?.().includes('cancel')) {
      return false
    }
    return true
  }
}

const showAddDialog = async () => {
  if (!await checkSubStoreUrl()) return
  isEdit.value = false
  form.value = {
    id: `sub_${Date.now()}`,
    name: '',
    url: '',
    type: 'universal',
    enabled: true,
    interval: 86400
  }
  dialogVisible.value = true
}

const editSubscription = (row: Subscription) => {
  isEdit.value = true
  form.value = { ...row }
  dialogVisible.value = true
}

const INTERVAL_MIN = 60
const INTERVAL_MAX = 604800

const saveSubscription = async () => {
  // 原生 number 输入不像 el-input-number 那样钳制越界值，也允许留空，
  // 这里在提交前兜底，避免把越界值或空串写进订阅配置
  const interval = Number(form.value.interval)
  if (!Number.isFinite(interval) || interval < INTERVAL_MIN || interval > INTERVAL_MAX) {
    ElMessage.warning(`更新间隔需在 ${INTERVAL_MIN} ~ ${INTERVAL_MAX} 秒之间`)
    return
  }
  form.value.interval = interval

  try {
    if (isEdit.value) {
      await subscriptionApi.update(form.value.id!, form.value)
      ElMessage.success('更新成功')
    } else {
      await subscriptionApi.create(form.value)
      ElMessage.success('添加成功')
    }
    dialogVisible.value = false
    loadSubscriptions()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const deleteSubscription = async (row: Subscription) => {
  try {
    // 确认删除
    await ElMessageBox.confirm(
      '确定要删除该订阅吗？删除后将同步清理策略组中对该订阅的引用。',
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    // 先删除订阅
    await subscriptionApi.delete(row.id)

    // 获取所有策略组，清理引用
    const { data: proxyGroups } = await api.get('/proxy-groups')
    let updatedCount = 0

    // 查找引用了该订阅的策略组
    for (const group of proxyGroups) {
      if (group.subscriptions && group.subscriptions.includes(row.id)) {
        // 从订阅列表中移除该订阅ID
        group.subscriptions = group.subscriptions.filter((id: string) => id !== row.id)

        try {
          await api.put(`/proxy-groups/${group.id}`, group)
          updatedCount++
        } catch (error) {
          console.error(`更新策略组 ${group.name} 失败:`, error)
        }
      }
    }

    if (updatedCount > 0) {
      ElMessage.success(`删除成功，已同步清理 ${updatedCount} 个策略组中的引用`)
    } else {
      ElMessage.success('删除成功')
    }
    loadSubscriptions()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除失败')
      console.error('删除订阅失败:', error)
    }
  }
}

const fetchSubscription = async (row: Subscription) => {
  const loading = ElLoading.service({
    lock: true,
    text: '正在解析节点...',
    background: 'rgba(0, 0, 0, 0.7)'
  })
  try {
    // 使用预览模式
    const { data } = await subscriptionApi.fetch(row.id, true)

    if (data.success) {
      // 显示节点预览
      previewNodes.value = data.nodes || []
      expandedPreviewNodes.value = new Set()
      nodesPreviewVisible.value = true
      const cachedCount = data.cached_count ?? previewNodes.value.length
      const updatedAt = data.cached_updated_at ?? null
      row.cached_node_count = cachedCount
      row.cached_updated_at = updatedAt
      subscriptionStatus.value[row.id] = {
        status: 'success',
        count: cachedCount,
        updatedAt
      }

      if (previewNodes.value.length === 0) {
        ElMessage.warning('未解析到任何节点')
      }
    } else {
      ElMessage.error(data.message || '解析节点失败')
    }
  } catch (error: any) {
    console.error('获取节点失败:', error)
    const message = error.response?.data?.message || '解析节点失败'
    subscriptionStatus.value[row.id] = {
      status: 'error',
      error: message,
      count: subscriptionStatus.value[row.id]?.count,
      updatedAt: subscriptionStatus.value[row.id]?.updatedAt
    }
    ElMessage.error(message)
  } finally {
    loading.close()
  }
}

const handleToggle = async (sub: Subscription) => {
  sub.enabled = !sub.enabled
  await toggleSubscriptionEnabled(sub)
}

const toggleUrlReveal = (id: string) => {
  revealedUrls.value[id] = !revealedUrls.value[id]
}

const getDisplayUrl = (sub: Subscription) => {
  if (revealedUrls.value[sub.id]) {
    return sub.url
  }

  const url = sub.url || ''
  if (url.length <= 12) return url

  try {
    const parsed = new URL(url)
    const maskedSearchParams = new URLSearchParams(parsed.search)

    maskedSearchParams.forEach((value, key) => {
      if (value.length > 8) {
        maskedSearchParams.set(key, `${value.slice(0, 3)}****${value.slice(-3)}`)
      }
    })

    parsed.search = maskedSearchParams.toString() ? `?${maskedSearchParams.toString()}` : ''
    return parsed.toString()
  } catch (error) {
    // 对于非标准 URL，使用通用掩码
    return `${url.slice(0, 12)}****${url.slice(-6)}`
  }
}

const toggleSubscriptionEnabled = async (sub: Subscription) => {
  try {
    await subscriptionApi.update(sub.id, sub)
    ElMessage.success(sub.enabled ? '已启用' : '已禁用')
  } catch (error) {
    ElMessage.error('更新状态失败')
    // 回滚状态
    sub.enabled = !sub.enabled
    loadSubscriptions()
  }
}

/* ---------- 视图模式 ---------- */
type ViewMode = 'list' | 'card'
const VIEW_KEY = 'configflow-subscriptions-view'

const readView = (): ViewMode => {
  try {
    const v = localStorage.getItem(VIEW_KEY)
    if (v === 'list' || v === 'card') return v
  } catch {
    // 存储不可用时用默认视图
  }
  return 'list'
}

const viewMode = ref<ViewMode>(readView())

watch(viewMode, mode => {
  try {
    localStorage.setItem(VIEW_KEY, mode)
  } catch {
    // 仅当前会话生效
  }
})

// 表格在窄屏无法阅读，移动端一律用卡片，不受用户选择影响
const isNarrow = ref(false)
const syncNarrow = () => {
  // 与 Tailwind 的 max-[900px]（不含 900）对齐，避免正好 900px 时两者判断相反
  isNarrow.value = window.matchMedia('(max-width: 899.98px)').matches
}

const effectiveView = computed<ViewMode>(() => (isNarrow.value ? 'card' : viewMode.value))

/* ---------- 状态筛选 ---------- */
const statusFilter = ref<'all' | 'enabled' | 'disabled' | 'error'>('all')

const matchesStatus = (sub: Subscription): boolean => {
  if (statusFilter.value === 'all') return true
  if (statusFilter.value === 'enabled') return !!sub.enabled
  if (statusFilter.value === 'disabled') return !sub.enabled
  return subscriptionStatus.value[sub.id]?.status === 'error'
}

/* ---------- 表格列展示 ---------- */
const nodeCount = (sub: Subscription): string => {
  const cached = subscriptionStatus.value[sub.id]?.count
  if (typeof cached === 'number') return String(cached)
  const stored = (sub as any).cached_node_count
  return typeof stored === 'number' ? String(stored) : '—'
}

const lastUpdated = (sub: Subscription): string => {
  const raw = (sub as any).cached_updated_at
  if (!raw) return '—'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', { hour12: false }).replace(/\//g, '-')
}

const statusText = (sub: Subscription): string => {
  const st = subscriptionStatus.value[sub.id]?.status
  if (st === 'loading') return '获取中'
  if (st === 'success') return '正常'
  if (st === 'error') return '获取失败'
  return sub.enabled ? '未获取' : '已停用'
}

const copyUrl = async (sub: Subscription) => {
  try {
    await navigator.clipboard.writeText(sub.url || '')
    ElMessage.success('地址已复制')
  } catch {
    ElMessage.error('复制失败，请手动选择地址')
  }
}

/* ---------- 排序提示 ---------- */
const reorderHint = computed(() => {
  const i = reorder.grabbedIndex.value
  if (i !== null) return `${reorder.positionLabel(i)}；方向键移动，空格放下`
  return '拖动手柄调整顺序；手柄聚焦后可用空格抓取、方向键移动'
})

/* ---------- 搜索与排序 ---------- */
const reorder = useReorder<Subscription>({
  items: subscriptions,
  container: subscriptionsContainer,
  labelOf: sub => sub.name,
  // 按 id 提交：后端 utils/reorder.py 的首选契约，服务端在存量数据上重排，
  // 避免把列表接口的计算字段（cached_node_count 等）回写进配置
  persist: async items => {
    await api.post('/subscriptions/reorder', {
      ids: items.map(item => item.id),
      position: 'top'
    })
  }
})


const keyword = ref('')

// 排序模式下必须展示完整列表，否则筛选会让保存的顺序丢条目
const visibleSubscriptions = computed(() => {
  if (reorder.active.value) return subscriptions.value
  const q = keyword.value.trim().toLowerCase()
  return subscriptions.value.filter(sub => {
    if (!matchesStatus(sub)) return false
    if (!q) return true
    return (
      sub.name?.toLowerCase().includes(q) || sub.url?.toLowerCase().includes(q)
    )
  })
})

const emptyText = computed(() =>
  subscriptions.value.length === 0 ? '还没有订阅来源' : '没有匹配的订阅'
)

const dotTone = (sub: Subscription): string => {
  if (!sub.enabled) return 'bg-muted-foreground'
  return subscriptionStatus.value[sub.id]?.status === 'error'
    ? 'bg-warning-accent'
    : 'bg-success-accent'
}

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    ElMessage.success('顺序已保存，所有配置空间生效')
  } catch (error) {
    ElMessage.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  syncNarrow()
  window.addEventListener('resize', syncNarrow)
  await loadSubscriptions()
})

onUnmounted(() => {
  window.removeEventListener('resize', syncNarrow)
})
</script>
