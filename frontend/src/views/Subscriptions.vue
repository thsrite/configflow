<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <PageHeader
      title="订阅来源"
    >
      <template #actions>
        <Button
          variant="outline"
          class="border-border/60 bg-background/40"
          :disabled="isRefreshing || reorder.active.value"
          @click="handleFetchAll"
        >
          <Loader2 v-if="isRefreshing" class="size-4 animate-spin" />
          <RefreshCw v-else class="size-4" />
          拉取全部
        </Button>
        <Button class="shadow-glow" :disabled="reorder.active.value" @click="showAddDialog">
          <Plus class="size-4" />
          添加订阅
        </Button>
      </template>
    </PageHeader>

    <Toolbar v-model:search="keyword" placeholder="搜索订阅名称或地址…">
      <template #filters>
        <Select v-model="statusFilter" :disabled="reorder.active.value">
          <SelectTrigger class="h-9 w-[132px] border-input bg-card text-[13px] dark:border-transparent" aria-label="按状态筛选">
            <SelectValue />
          </SelectTrigger>
          <SelectContent class="glass-strong">
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="enabled">已启用</SelectItem>
            <SelectItem value="disabled">已停用</SelectItem>
            <SelectItem value="error">获取失败</SelectItem>
          </SelectContent>
        </Select>
      </template>

      <template #actions>
        <Button
          v-if="!reorder.active.value"
          variant="ghost"
          size="sm"
          :disabled="subscriptions.length < 2"
          @click="reorder.enter"
        >
          <ArrowUpDown class="size-3.5" />
          调整顺序
        </Button>
        <!-- 数据密集区默认表格，卡片作为可选视图 -->
        <ViewToggle v-model="viewMode" class="max-[900px]:hidden" />
      </template>
    </Toolbar>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      :hint="reorderHint"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <SectionCard v-if="visibleSubscriptions.length === 0" :padded="false">
      <EmptyState :icon="Link2" :title="subscriptions.length ? '没有匹配的订阅' : '还没有订阅来源'" :description="emptyText">
        <Button @click="showAddDialog">
          <Plus class="size-4" />
          添加订阅
        </Button>
      </EmptyState>
    </SectionCard>

    <!-- ===== 表格视图（桌面默认） ===== -->
    <DataTableShell
      v-else-if="effectiveView === 'list'"
      :footer="`共 ${visibleSubscriptions.length} 条`"
    >
      <TableHeader>
        <TableRow class="hover:bg-transparent">
          <TableHead v-if="reorder.active.value" class="w-10"><span class="cf-sr">排序</span></TableHead>
          <TableHead class="w-12 text-right">#</TableHead>
          <TableHead>名称</TableHead>
          <TableHead>地址</TableHead>
          <TableHead class="w-20 text-right">节点</TableHead>
          <TableHead class="w-36">最近更新</TableHead>
          <TableHead class="w-28">状态</TableHead>
          <TableHead class="w-32 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody ref="subscriptionsContainer">
        <TableRow
          v-for="(sub, index) in visibleSubscriptions"
          :key="sub.id"
          :data-id="sub.id"
          data-reorder-item
          :class="!sub.enabled && 'opacity-55'"
        >
          <TableCell v-if="reorder.active.value">
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
          </TableCell>
          <TableCell class="num text-right text-muted-foreground">{{ index + 1 }}</TableCell>
          <TableCell>
            <div class="flex items-center gap-2">
              <span :class="cn('size-1.5 shrink-0 rounded-full', dotTone(sub))" aria-hidden="true" />
              <span class="font-medium whitespace-nowrap text-foreground">{{ sub.name }}</span>
              <Badge :variant="typeTone(sub.type)" class="text-[10.5px]">{{ getTypeLabel(sub.type) }}</Badge>
            </div>
          </TableCell>
          <TableCell class="max-w-[320px]">
            <div class="flex items-center gap-1">
              <span class="min-w-0 truncate font-mono text-[12px] text-muted-foreground">
                {{ getDisplayUrl(sub) }}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                class="cf-reorder-mute size-6 shrink-0"
                :aria-label="`复制 ${sub.name} 的地址`"
                title="复制地址"
                @click="copyUrl(sub)"
              >
                <Copy class="size-3.5" />
              </Button>
            </div>
          </TableCell>
          <TableCell class="num text-right">{{ nodeCount(sub) }}</TableCell>
          <TableCell class="whitespace-nowrap text-muted-foreground">{{ lastUpdated(sub) }}</TableCell>
          <TableCell>
            <Badge :variant="statusTone(subscriptionStatus[sub.id])">{{ statusText(sub) }}</Badge>
          </TableCell>
          <TableCell class="cf-reorder-mute text-right">
            <div class="flex items-center justify-end gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`获取 ${sub.name} 的节点`"
                title="获取节点"
                @click="handleFetchSubscription(sub)"
              >
                <Network class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`编辑 ${sub.name}`"
                title="编辑"
                @click="editSubscription(sub)"
              >
                <Pencil class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                class="text-destructive-accent hover:bg-destructive-soft"
                :aria-label="`删除 ${sub.name}`"
                title="删除"
                @click="deleteSubscription(sub)"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </DataTableShell>

    <!-- ===== 卡片视图（默认） ===== -->
    <div
      v-else
      ref="subscriptionsContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-3.5 max-md:grid-cols-1"
    >
      <Motion
        v-for="(sub, index) in visibleSubscriptions"
        :key="sub.id"
        v-bind="listItem(index)"
        :data-id="sub.id"
        data-reorder-item
        :class="cn(
          'relative overflow-hidden rounded-[18px] border border-border bg-card/90 p-5 shadow-surface transition-[transform,border-color] duration-350 ease-(--ease-flow) hover:-translate-y-[3px] hover:border-border-strong max-md:p-4',
          !sub.enabled && 'opacity-60'
        )"
      >
        <!-- 拉取中：顶部一道扫描光 -->
        <span
          v-if="subscriptionStatus[sub.id]?.status === 'loading'"
          class="sub-sweep pointer-events-none absolute inset-x-0 top-0 h-0.5"
          aria-hidden="true"
        />

        <div class="card-header flex items-center gap-4">
          <span v-if="reorder.active.value" class="text-xs tabular-nums text-muted-foreground">{{ index + 1 }}</span>

          <!-- 流量环：subscription-userinfo 的已用比例；服务商不提供时显示不计量 -->
          <div v-if="!reorder.active.value" class="card-stats relative size-24 shrink-0">
            <svg viewBox="0 0 96 96" class="size-full -rotate-90" aria-hidden="true">
              <defs>
                <linearGradient :id="`ring-${index}`" x1="0" y1="0" x2="1" y2="1">
                  <stop offset="0" stop-color="var(--primary)" />
                  <stop offset="1" stop-color="var(--accent-2)" />
                </linearGradient>
              </defs>
              <circle cx="48" cy="48" r="42" fill="none" stroke="var(--accent)" stroke-width="7" />
              <circle
                cx="48"
                cy="48"
                r="42"
                fill="none"
                :stroke="`url(#ring-${index})`"
                stroke-width="7"
                stroke-linecap="round"
                :stroke-dasharray="RING"
                :stroke-dashoffset="ringOffset(sub)"
                class="transition-[stroke-dashoffset] duration-1600 ease-(--ease-flow)"
              />
            </svg>
            <div class="absolute inset-0 grid place-content-center text-center">
              <template v-if="traffic(sub).metered">
                <b class="font-display text-[24px] leading-none font-medium">
                  {{ Math.round(traffic(sub).ratio * 100) }}<small class="text-[12px]">%</small>
                </b>
                <span class="mt-0.5 font-mono text-[10px] text-muted-foreground">已用</span>
              </template>
              <template v-else>
                <b class="font-display text-[24px] leading-none font-medium">∞</b>
                <span class="mt-0.5 font-mono text-[10px] text-muted-foreground">不计量</span>
              </template>
            </div>
          </div>

          <div class="card-title-group min-w-0 flex-1">
            <h3 class="card-title font-display m-0 truncate text-[20px] leading-tight font-medium tracking-[-0.01em]">
              {{ sub.name }}
            </h3>
            <div v-if="!reorder.active.value" class="card-meta mt-2 flex flex-wrap gap-1.5">
              <span class="chip font-mono">{{ getTypeLabel(sub.type) }}</span>
              <span
                v-if="traffic(sub).expireDays !== null"
                :class="cn('chip', traffic(sub).expireDays! <= 10 && 'chip-warn')"
              >
                {{ traffic(sub).expireDays! > 0 ? `${traffic(sub).expireDays} 天后到期` : '已到期' }}
              </span>
              <span v-if="traffic(sub).ratio > 0.8" class="chip chip-bad">余量不足</span>
              <span v-if="subscriptionStatus[sub.id]?.status === 'error'" class="chip chip-bad" :title="subscriptionStatus[sub.id]?.error">
                拉取失败
              </span>
              <span v-if="!sub.enabled" class="chip">已停用</span>
            </div>
          </div>

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
        </div>

        <template v-if="!reorder.active.value">
          <div class="card-section mt-[18px] grid grid-cols-3 border-t border-border pt-3.5">
            <div>
              <small class="block text-[11px] text-muted-foreground">节点</small>
              <b class="font-mono text-[15px] font-medium">
                <AnimatedNumber v-if="nodeCount(sub) !== '—'" :value="Number(nodeCount(sub))" />
                <template v-else>—</template>
              </b>
            </div>
            <div>
              <small class="block text-[11px] text-muted-foreground">剩余流量</small>
              <b class="font-mono text-[15px] font-medium">
                {{ traffic(sub).left !== null ? formatBytes(traffic(sub).left) : '—' }}
              </b>
            </div>
            <div>
              <small class="block text-[11px] text-muted-foreground">更新间隔</small>
              <b class="font-mono text-[15px] font-medium">{{ sub.interval ? formatInterval(sub.interval) : '—' }}</b>
            </div>
          </div>

          <div class="card-actions mt-4 flex items-center gap-2">
            <span class="mr-auto min-w-0 truncate text-[11.5px] text-muted-foreground">
              上次拉取 · {{ lastFetchText(sub) }}
            </span>
            <Button variant="ghost" size="sm" class="h-[30px]" :aria-label="`编辑 ${sub.name}`" @click="editSubscription(sub)">编辑</Button>
            <Button
              variant="outline"
              size="sm"
              class="h-[30px]"
              :disabled="subscriptionStatus[sub.id]?.status === 'loading'"
              @click="pullOne(sub)"
            >
              <RefreshCw :class="cn('size-3.5', subscriptionStatus[sub.id]?.status === 'loading' && 'animate-spin')" />
              拉取
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="ghost" size="icon-sm" class="size-[30px]" :aria-label="`${sub.name} 的更多操作`">
                  <MoreHorizontal class="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" class="glass-strong min-w-[160px]">
                <DropdownMenuItem @select="handleFetchSubscription(sub)">
                  <Network class="size-4" />
                  查看节点
                </DropdownMenuItem>
                <DropdownMenuItem @select="copyUrl(sub)">
                  <Copy class="size-4" />
                  复制地址
                </DropdownMenuItem>
                <DropdownMenuItem @select="handleToggle(sub)">
                  <component :is="sub.enabled ? EyeOff : Eye" class="size-4" />
                  {{ sub.enabled ? '停用' : '启用' }}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" @select="deleteSubscription(sub)">
                  <Trash2 class="size-4" />
                  删除
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </template>
      </Motion>

      <button
        v-if="!reorder.active.value && !keyword && statusFilter === 'all'"
        type="button"
        class="grid min-h-[250px] cursor-pointer place-content-center gap-2.5 rounded-[18px] border border-dashed border-border-strong bg-transparent text-center text-muted-foreground transition-colors hover:border-primary-accent/50 hover:text-primary-accent focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
        @click="showAddDialog"
      >
        <Link2 class="mx-auto size-7" :stroke-width="1.75" />
        <span>添加订阅来源</span>
      </button>
    </div>

    <!-- 添加/编辑对话框 -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline border-border/50 sm:max-w-[640px]" @pointer-down-outside.prevent>
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑订阅' : '添加订阅' }}</DialogTitle>
          <DialogDescription>填写订阅名称和链接，设置节点更新间隔。</DialogDescription>
        </DialogHeader>

        <div class="grid gap-4">
          <div class="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
            <div class="grid gap-2">
              <Label for="sub-name">订阅名称</Label>
              <Input id="sub-name" v-model="form.name" class="bg-background/50" placeholder="请输入订阅名称" />
            </div>
            <div class="grid gap-2">
              <Label for="sub-type">订阅类型</Label>
              <Select v-model="form.type">
                <SelectTrigger id="sub-type" class="w-full bg-background/50">
                  <SelectValue placeholder="请选择订阅类型" />
                </SelectTrigger>
                <SelectContent class="glass-strong">
                  <SelectItem value="universal">通用</SelectItem>
                  <SelectItem value="mihomo">Mihomo</SelectItem>
                  <SelectItem value="surge">Surge</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div class="grid gap-2">
            <Label for="sub-url">订阅链接</Label>
            <Textarea id="sub-url" v-model="form.url" class="bg-background/50 font-mono text-[12px]" :rows="3" placeholder="请输入订阅 URL" />
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
                class="w-40 bg-background/50"
              />
              <span class="text-xs text-muted-foreground">秒（建议 86400 = 1 天）</span>
            </div>
          </div>

          <div class="grid gap-2">
            <Label for="sub-health">健康检查</Label>
            <Input
              id="sub-health"
              v-model="form.health_check_url"
              class="bg-background/50 font-mono"
              placeholder="留空使用默认（http://www.gstatic.com/generate_204）"
            />
            <p class="m-0 text-xs text-muted-foreground">
              回家或内网节点若无法访问境外测试地址，可能被误判为不可用；建议填 http://www.baidu.com。
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
      <DialogContent class="glass-strong hairline border-border/50 sm:max-w-[800px]">
        <DialogHeader>
          <DialogTitle>节点预览</DialogTitle>
          <DialogDescription>共 {{ previewNodes.length }} 个节点</DialogDescription>
        </DialogHeader>

        <div class="max-h-[500px] overflow-y-auto">
          <EmptyState v-if="previewNodes.length === 0" :icon="Network" title="暂无节点" />
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
              <span class="num shrink-0 font-mono text-xs text-muted-foreground">{{ node.server }}:{{ node.port }}</span>
              <ChevronDown
                :class="cn('size-4 shrink-0 text-muted-foreground transition-transform', expandedPreviewNodes.has(index) && 'rotate-180')"
              />
            </div>
            <pre
              v-show="expandedPreviewNodes.has(index)"
              class="mt-2 mb-0 overflow-x-auto rounded-md border border-border/50 bg-background/50 p-3 font-mono text-xs leading-relaxed text-muted-foreground"
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
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import {
  ArrowUpDown,
  ChevronDown,
  Copy,
  Eye,
  EyeOff,
  Link2,
  Loader2,
  MoreHorizontal,
  Network,
  Pencil,
  Plus,
  RefreshCw,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { subscriptionApi } from '@/api'
import type { Subscription } from '@/types'
import api from '@/api'
import yaml from 'js-yaml'
import PageHeader from '@/components/common/PageHeader.vue'
import AnimatedNumber from '@/components/common/AnimatedNumber.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { useRouter } from 'vue-router'
import { consumeAction } from '@/lib/actions'
import { formatBytes, relativeTime, trafficSummary } from '@/lib/format'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import DataTableShell from '@/components/common/DataTableShell.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Toolbar from '@/components/common/Toolbar.vue'
import ViewToggle from '@/components/common/ViewToggle.vue'
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Motion } from 'motion-v'
import { confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'


const router = useRouter()

/* ---------- 流量环与拉取信息 ---------- */
const RING = 2 * Math.PI * 42
const traffic = (sub: Subscription) => trafficSummary((sub as any).traffic)
const ringOffset = (sub: Subscription) => {
  const t = traffic(sub)
  return RING * (1 - (t.metered ? t.ratio : 0))
}
const lastFetchText = (sub: Subscription) =>
  relativeTime((sub as any).last_fetch?.at || (sub as any).cached_updated_at)
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

const loadSubscriptions = async (keepStatus = false) => {
  try {
    const { data } = await subscriptionApi.getAll()
    subscriptions.value = data
    // 初始化订阅状态；刷新时保留刚拿到的失败状态，避免被缓存值覆盖
    subscriptions.value.forEach(sub => {
      if (keepStatus && subscriptionStatus.value[sub.id]?.status === 'error') return
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
    notify.error('加载订阅列表失败')
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
    // 拉取会更新流量信息与拉取记录，整体刷新一次列表
    await loadSubscriptions(true)
  } finally {
    isRefreshing.value = false
  }
}

/** 单个订阅后台拉取：卡片顶部扫描光，完成后节点数重新滚动 */
const pullOne = async (sub: Subscription) => {
  const previous = subscriptionStatus.value[sub.id]
  subscriptionStatus.value[sub.id] = { status: 'loading', count: previous?.count, updatedAt: previous?.updatedAt }
  try {
    const { data } = await subscriptionApi.fetch(sub.id, true)
    if (!data.success) throw new Error(data.message || '拉取失败')
    const count = data.cached_count ?? (data.nodes?.length || 0)
    subscriptionStatus.value[sub.id] = { status: 'success', count, updatedAt: data.cached_updated_at ?? null }
    notify.success(data.from_cache ? `「${sub.name}」拉取失败，已使用缓存` : `「${sub.name}」已更新 · ${count} 节点`)
  } catch (error: any) {
    const message = error.response?.data?.message || error.message || '拉取失败'
    subscriptionStatus.value[sub.id] = { status: 'error', error: message, count: previous?.count, updatedAt: previous?.updatedAt }
    notify.error(message)
  }
  await loadSubscriptions(true)
}

const showAddDialog = () => {
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
  if (!form.value.name?.trim() || !form.value.url?.trim()) {
    notify.warning('请输入订阅名称和链接')
    return
  }
  // 原生 number 输入不像 el-input-number 那样钳制越界值，也允许留空，
  // 这里在提交前兜底，避免把越界值或空串写进订阅配置
  const interval = Number(form.value.interval)
  if (!Number.isFinite(interval) || interval < INTERVAL_MIN || interval > INTERVAL_MAX) {
    notify.warning(`更新间隔需在 ${INTERVAL_MIN} ~ ${INTERVAL_MAX} 秒之间`)
    return
  }
  form.value.interval = interval

  try {
    if (isEdit.value) {
      await subscriptionApi.update(form.value.id!, form.value)
      notify.success('更新成功')
    } else {
      await subscriptionApi.create(form.value)
      notify.success('添加成功')
    }
    dialogVisible.value = false
    loadSubscriptions()
  } catch (error) {
    notify.error('保存失败')
  }
}

const deleteSubscription = async (row: Subscription) => {
  const ok = await confirmDanger('确定删除该订阅吗？如果仍被配置或聚合使用，或仍保留从该订阅获取的节点，将无法删除。请先取消相关使用，并移除该订阅已获取的节点。删除后无法恢复。', {
    title: '删除订阅'
  })
  if (!ok) return

  try {
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
      notify.success('订阅已删除')
    } else {
      notify.success('删除成功')
    }
    loadSubscriptions()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      notify.error('删除失败')
      console.error('删除订阅失败:', error)
    }
  }
}

const fetchSubscription = async (row: Subscription) => {
  // 全屏遮罩改为轻提示：解析期间页面其余部分仍可查看
  const loadingToast = notify.loading('正在解析节点…')
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
        notify.warning('未解析到任何节点')
      }
    } else {
      notify.error(data.message || '解析节点失败')
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
    notify.error(message)
  } finally {
    notify.dismiss(loadingToast)
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
    notify.success(sub.enabled ? '已启用' : '已禁用')
  } catch (error) {
    notify.error('更新状态失败')
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
  return 'card'
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
    notify.success('地址已复制')
  } catch {
    notify.error('复制失败，请手动选择地址')
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
  subscriptions.value.length === 0 ? '添加订阅链接后，即可在各配置的策略组中选择使用。' : '试试其他关键词，或调整状态筛选。'
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
    notify.success('顺序已保存，对所有配置生效')
  } catch (error) {
    notify.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  syncNarrow()
  window.addEventListener('resize', syncNarrow)
  await loadSubscriptions()
  if (consumeAction(router, 'pull-all')) fetchAllSubscriptionsInBackground()
})

// 已在本页时从命令面板触发
watch(
  () => router?.currentRoute.value.query.run,
  run => {
    if (run === 'pull-all' && consumeAction(router, 'pull-all')) fetchAllSubscriptionsInBackground()
  }
)

onUnmounted(() => {
  window.removeEventListener('resize', syncNarrow)
})
</script>

<style scoped>
.sub-sweep {
  background: linear-gradient(90deg, transparent, var(--primary-accent), transparent);
  animation: sub-sweep 1s linear infinite;
}

@keyframes sub-sweep {
  from {
    transform: translateX(-100%);
  }
  to {
    transform: translateX(100%);
  }
}
</style>
