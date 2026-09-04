<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner
      scope="resource"
      :profile-name="cfProfileName"
      description="订阅拉取与手动录入的节点，按配置空间隔离"
    />

    <PageHeader eyebrow="Resource" title="节点库" description="订阅拉取与手动录入的节点集中在此。">
      <template #actions>
        <Button
          v-if="!reorder.active.value"
          variant="outline"
          class="border-border/60 bg-background/40"
          :disabled="nodes.length < 2"
          @click="reorder.enter"
        >
          <ArrowUpDown class="size-4" />
          调整顺序
        </Button>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="showBatchAddDialog">
          <FilePlus2 class="size-4" />
          批量添加
        </Button>
        <Button class="shadow-glow" @click="showAddDialog">
          <Plus class="size-4" />
          添加节点
        </Button>
      </template>
    </PageHeader>

    <Toolbar v-model:search="keyword" placeholder="搜索名称、地址或备注…">
      <template #filters>
        <Select v-model="protocolFilter" :disabled="reorder.active.value">
          <SelectTrigger class="h-9 w-[150px] border-transparent bg-background/50 text-[13px]">
            <SelectValue placeholder="全部协议" />
          </SelectTrigger>
          <SelectContent class="glass-strong">
            <SelectItem value="all">全部协议</SelectItem>
            <SelectItem v-for="p in protocolOptions" :key="p" :value="p">
              {{ p.toUpperCase() }}
            </SelectItem>
          </SelectContent>
        </Select>
      </template>

      <template #actions>
        <Button
          v-if="nodes.length > 0"
          variant="ghost"
          size="sm"
          :disabled="reorder.active.value"
          @click="toggleSelectAll"
        >
          {{ isAllSelected ? '取消全选' : '全选' }}
        </Button>
        <Button
          v-if="selectedNodeIds.size > 0"
          variant="outline"
          size="sm"
          class="border-destructive-accent/30 bg-destructive-soft/40 text-destructive-accent"
          @click="batchDeleteNodes"
        >
          <Trash2 class="size-3.5" />
          删除 {{ selectedNodeIds.size }} 项
        </Button>
        <ViewToggle v-model="viewMode" class="max-md:hidden" />
      </template>
    </Toolbar>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <SectionCard v-if="visibleNodes.length === 0" :padded="false">
      <EmptyState :icon="Network" title="没有匹配的节点" :description="nodesEmptyText">
        <Button @click="showAddDialog">
          <Plus class="size-4" />
          添加节点
        </Button>
      </EmptyState>
    </SectionCard>

    <!-- ===== 表格视图（桌面默认） ===== -->
    <DataTableShell
      v-else-if="effectiveView === 'list'"
      :footer="`共 ${visibleNodes.length} 个节点`"
    >
      <TableHeader>
        <TableRow class="hover:bg-transparent">
          <TableHead v-if="reorder.active.value" class="w-10"><span class="cf-sr">排序</span></TableHead>
          <TableHead class="w-10"><span class="cf-sr">选择</span></TableHead>
          <TableHead class="w-12 text-right">#</TableHead>
          <TableHead>名称</TableHead>
          <TableHead class="w-28">协议</TableHead>
          <TableHead>地址</TableHead>
          <TableHead class="w-40">来源</TableHead>
          <TableHead class="w-32 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody ref="nodesContainer">
        <TableRow
          v-for="(node, cfIndex) in visibleNodes"
          :key="node.id || node.name"
          :data-name="node.name"
          data-reorder-item
          :class="!node.enabled && 'opacity-55'"
        >
          <TableCell v-if="reorder.active.value">
            <DragHandle
              :label="node.name || node.id"
              :index="cfIndex"
              :total="nodes.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
          </TableCell>
          <TableCell class="cf-reorder-mute">
            <Checkbox
              :model-value="selectedNodeIds.has(node.id)"
              :aria-label="`选择 ${node.name}`"
              @update:model-value="toggleNodeSelection(node.id)"
            />
          </TableCell>
          <TableCell class="num text-right text-muted-foreground">{{ cfIndex + 1 }}</TableCell>
          <TableCell>
            <div class="flex items-center gap-2">
              <span
                class="size-1.5 shrink-0 rounded-full"
                :class="node.enabled
                  ? 'bg-success-accent shadow-[0_0_6px_var(--success-accent)]'
                  : 'bg-muted-foreground'"
                aria-hidden="true"
              />
              <span class="min-w-0 truncate font-medium text-foreground">{{ node.name }}</span>
              <span v-if="node.remark" class="truncate text-[12px] text-muted-foreground">
                {{ node.remark }}
              </span>
            </div>
          </TableCell>
          <TableCell>
            <Badge variant="outline" class="font-mono text-[10.5px]">{{ nodeProtocol(node) }}</Badge>
          </TableCell>
          <TableCell class="font-mono text-[12px] text-muted-foreground">{{ nodeAddress(node) }}</TableCell>
          <TableCell class="truncate text-[12.5px] text-muted-foreground">
            {{ node.subscription_name || '手动添加' }}
          </TableCell>
          <TableCell class="cf-reorder-mute text-right">
            <div class="flex items-center justify-end gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="node.enabled ? `停用 ${node.name}` : `启用 ${node.name}`"
                :title="node.enabled ? '停用' : '启用'"
                :disabled="savingStatus[node.id]"
                @click="handleToggle(node)"
              >
                <component :is="node.enabled ? Eye : EyeOff" class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`编辑 ${node.name}`"
                title="编辑"
                @click="editNode(node)"
              >
                <Pencil class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                class="text-destructive-accent hover:bg-destructive-soft"
                :aria-label="`删除 ${node.name}`"
                title="删除"
                @click="deleteNode(node)"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
          </TableCell>
        </TableRow>
      </TableBody>
    </DataTableShell>

    <!-- ===== 卡片视图 ===== -->
    <div
      v-else
      ref="nodesContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-3 max-md:grid-cols-1"
    >
      <Motion
        v-for="(node, cfIndex) in visibleNodes"
        :key="node.id || node.name"
        v-bind="listItem(cfIndex)"
        :data-name="node.name"
        data-reorder-item
        :class="[
          'hairline edge-light relative flex flex-col gap-3 overflow-hidden rounded-xl border bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft',
          selectedNodeIds.has(node.id) ? 'border-primary-accent/45' : 'border-border/35',
          !node.enabled && 'opacity-60'
        ]"
      >
        <header class="flex items-start gap-2.5">
          <DragHandle
            v-if="reorder.active.value"
            :label="node.name || node.id"
            :index="cfIndex"
            :total="nodes.length"
            :position="reorder.positionLabel(cfIndex)"
            :grabbed="reorder.grabbedIndex.value === cfIndex"
            @up="reorder.moveUp(cfIndex)"
            @down="reorder.moveDown(cfIndex)"
            @keydown="reorder.onHandleKeydown($event, cfIndex)"
          />
          <Checkbox
            class="cf-reorder-mute mt-0.5"
            :model-value="selectedNodeIds.has(node.id)"
            :aria-label="`选择 ${node.name}`"
            @update:model-value="toggleNodeSelection(node.id)"
          />
          <div class="min-w-0 flex-1">
            <p class="m-0 truncate text-[14px] font-semibold text-foreground" :title="node.name">
              {{ node.name }}
            </p>
            <p
              v-if="node.remark"
              class="mt-0.5 mb-0 truncate text-[12px] text-muted-foreground"
              :title="node.remark"
            >
              {{ node.remark }}
            </p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            class="cf-reorder-mute shrink-0"
            :class="node.enabled ? 'text-success-accent' : 'text-muted-foreground'"
            :title="node.enabled ? '停用' : '启用'"
            :aria-label="node.enabled ? `停用 ${node.name}` : `启用 ${node.name}`"
            :disabled="savingStatus[node.id]"
            @click="handleToggle(node)"
          >
            <component :is="node.enabled ? Eye : EyeOff" class="size-4" />
          </Button>
        </header>

        <div class="cf-reorder-mute flex flex-wrap items-center gap-1.5">
          <Badge variant="outline" class="font-mono text-[10.5px]">
            {{ getProtocol(node.proxy_string) }}
          </Badge>
          <Badge v-if="node.subscription_name" variant="info" class="max-w-[180px] truncate text-[10.5px]">
            {{ node.subscription_name }}
          </Badge>
        </div>

        <div class="cf-reorder-mute">
          <button
            type="button"
            class="flex w-full cursor-pointer items-center gap-1.5 border-0 bg-transparent p-0 text-[11.5px] font-medium tracking-[0.04em] text-muted-foreground uppercase transition-colors hover:text-foreground"
            @click="toggleNodeExpand(node.id)"
          >
            <Link2 class="size-3.5" aria-hidden="true" />
            节点字符串
            <ChevronDown
              class="ml-auto size-3.5 transition-transform duration-200"
              :class="expandedNodes.has(node.id) && 'rotate-180'"
              aria-hidden="true"
            />
          </button>
          <pre
            v-show="expandedNodes.has(node.id)"
            class="mt-2 max-h-40 overflow-auto rounded-lg border border-border/50 bg-background/50 p-2.5 font-mono text-[11px] leading-relaxed break-all whitespace-pre-wrap text-muted-foreground"
          >{{ formatProxyStringForDisplay(node.proxy_string) }}</pre>
        </div>

        <footer class="cf-reorder-mute mt-auto flex items-center gap-2 border-0 border-t border-border/50 pt-3">
          <Button variant="ghost" size="sm" @click="editNode(node)">
            <Pencil class="size-3.5" />
            编辑
          </Button>
          <Button
            variant="ghost"
            size="sm"
            class="ml-auto text-destructive-accent hover:bg-destructive-soft"
            @click="deleteNode(node)"
          >
            <Trash2 class="size-3.5" />
            删除
          </Button>
        </footer>
      </Motion>
    </div>

    <!-- ===== 新增 / 编辑节点 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline max-w-[720px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑节点' : '添加节点' }}</DialogTitle>
          <DialogDescription>填写节点名称与连接字符串，支持 URI / JSON / YAML 格式。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[60dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label for="node-name">节点名称</Label>
            <Input
              id="node-name"
              v-model="form.name"
              class="bg-background/50"
              placeholder="例如：香港节点 01"
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="node-remark">备注</Label>
            <Input
              id="node-remark"
              v-model="form.remark"
              class="bg-background/50"
              placeholder="可选，添加备注信息"
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="node-string">节点字符串</Label>
            <Textarea
              id="node-string"
              v-model="form.proxy_string"
              class="min-h-[220px] bg-background/50 font-mono text-[12px]"
              :rows="12"
              placeholder="支持 URI、JSON、YAML 等格式"
            />
          </div>
          <div class="flex items-center gap-2.5">
            <Switch id="node-enabled" v-model="form.enabled" />
            <Label for="node-enabled" class="text-[13px] text-muted-foreground">
              {{ form.enabled ? '节点启用中' : '节点已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button @click="saveNode">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 批量添加 ===== -->
    <Dialog v-model:open="batchDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[780px] border-border/50">
        <DialogHeader>
          <DialogTitle>批量添加节点</DialogTitle>
          <DialogDescription>粘贴多个节点链接或配置，系统会自动识别格式并导入。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label for="batch-nodes">节点链接或配置</Label>
            <Textarea
              id="batch-nodes"
              v-model="batchForm.nodes_text"
              class="min-h-[300px] bg-background/50 font-mono text-[12px]"
              :rows="16"
              placeholder="支持 URI、JSON、YAML 多种格式，自动忽略空行和 // 注释"
            />
          </div>
          <div class="flex items-center gap-2.5">
            <Switch id="batch-enabled" v-model="batchForm.enabled" />
            <Label for="batch-enabled" class="text-[13px] text-muted-foreground">
              {{ batchForm.enabled ? '导入后默认启用节点' : '导入后默认禁用节点' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="batchDialogVisible = false">取消</Button>
          <Button @click="saveBatchNodes">批量添加</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, watch, ref, computed, onMounted, nextTick } from 'vue'
import { Motion } from 'motion-v'
import {
  ArrowUpDown,
  ChevronDown,
  Eye,
  EyeOff,
  FilePlus2,
  Link2,
  Network,
  Pencil,
  Plus,
  Trash2
} from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import DataTableShell from '@/components/common/DataTableShell.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import Toolbar from '@/components/common/Toolbar.vue'
import ViewToggle from '@/components/common/ViewToggle.vue'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import { useReorder } from '@/composables/useReorder'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { useProfileStore } from '@/stores/profile'
import { nodeApi, subStoreUrlApi } from '@/api'
import type { ProxyNode } from '@/types'
import api from '@/api'
import * as yaml from 'js-yaml'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
const nodes = ref<ProxyNode[]>([])
const savingStatus = ref<Record<string, boolean>>({})
const nodesContainer = ref<HTMLElement | null>(null)
const dialogVisible = ref(false)
const isEdit = ref(false)
const form = ref<Partial<ProxyNode>>({
  name: '',
  proxy_string: '',
  enabled: true,
  remark: ''
})

// 节点字符串展开/收起状态
const expandedNodes = ref<Set<string>>(new Set())

// 批量添加相关
const batchDialogVisible = ref(false)
const batchForm = ref({
  nodes_text: '',
  enabled: true
})

// 批量删除相关
const selectedNodeIds = ref<Set<string>>(new Set())
const isAllSelected = computed(() => {
  return nodes.value.length > 0 && selectedNodeIds.value.size === nodes.value.length
})
const isSomeSelected = computed(() => {
  return selectedNodeIds.value.size > 0 && selectedNodeIds.value.size < nodes.value.length
})

// 从节点字符串中提取协议类型
const getProtocol = (proxyString: string) => {
  if (!proxyString) return 'Unknown'

  // 去除 YAML 列表标记
  let str = proxyString.trim()
  if (str.startsWith('- ')) {
    str = str.substring(2).trim()
  }

  // 检查是否是单行 JSON 对象格式
  if (str.startsWith('{') && str.endsWith('}') && !str.includes('\n')) {
    try {
      // 尝试解析为 JSON
      const obj = JSON.parse(str)
      if (obj && obj.type) {
        return obj.type.toUpperCase()
      }
    } catch {
      // JSON 解析失败，尝试正则提取
      try {
        const typeMatch = str.match(/"type":\s*"([a-z0-9]+)"|type:\s*([a-z0-9]+)/i)
        if (typeMatch) {
          return (typeMatch[1] || typeMatch[2]).toUpperCase()
        }
      } catch {
        // 忽略解析错误
      }
    }
  }

  // 检查是否是多行 YAML/JSON 格式
  if (str.includes('\n') || (str.startsWith('{') && str.endsWith('}'))) {
    try {
      // 尝试解析为 JSON（多行）
      const obj = JSON.parse(str)
      if (obj && obj.type) {
        return obj.type.toUpperCase()
      }
    } catch {
      // JSON 解析失败，尝试正则提取 type 字段
      const typeMatch = str.match(/["\']?type["\']?\s*:\s*["\']?([a-z0-9]+)["\']?/i)
      if (typeMatch) {
        return typeMatch[1].toUpperCase()
      }
    }
  }

  // 检查 URI 格式
  const match = str.match(/^([a-z0-9]+):\/\//)
  if (match) {
    return match[1].toUpperCase()
  }
  return 'Unknown'
}

// 获取协议标签颜色
const getProtocolTagType = (proxyString: string) => {
  const protocol = getProtocol(proxyString).toLowerCase()
  const types: Record<string, string> = {
    'ss': 'primary',
    'vmess': 'success',
    'vless': 'success',
    'trojan': 'warning',
    'hysteria2': 'danger',
    'wireguard': 'info',
    'http': '',
    'https': 'info'
  }
  return types[protocol] || ''
}

// 格式化节点字符串用于显示（多行格式化）
const formatProxyStringForDisplay = (str: string) => {
  if (!str) return ''

  const trimmed = str.trim()

  // 去除 YAML 列表标记
  let content = trimmed
  if (content.startsWith('- ')) {
    content = content.substring(2).trim()
  }

  // 检查是否是单行 JSON 对象格式
  if (content.startsWith('{') && content.endsWith('}') && !content.includes('\n')) {
    try {
      // 尝试解析为 JSON
      const obj = JSON.parse(content)
      if (obj && typeof obj === 'object') {
        // 返回格式化的 JSON（带缩进）
        return JSON.stringify(obj, null, 2)
      }
    } catch {
      // JSON 解析失败，返回原字符串
    }
  }

  // 如果已经是格式化的多行内容（YAML 或 JSON），直接返回
  if (content.includes('\n')) {
    return content
  }

  return str
}

// 截断字符串
const truncateString = (str: string, maxLength: number) => {
  if (!str) return ''
  if (str.length <= maxLength) return str
  return str.substring(0, maxLength) + '...'
}

const loadNodes = async () => {
  try {
    const { data } = await nodeApi.getAll()
    nodes.value = data
  } catch (error) {
    notify.error('加载节点列表失败')
  }
}

const checkSubStoreUrl = async (): Promise<boolean> => {
  try {
    const response = await subStoreUrlApi.get()
    const url = response.data?.sub_store_url || ''
    if (!url) {
      return await confirm(
        '尚未配置 Sub-Store URL，节点格式转换功能将不可用。请前往「配置生成」页面配置 Sub-Store 地址。',
        { title: '未配置 Sub-Store', confirmText: '继续添加' }
      )
    }
    return true
  } catch (error) {
    // 读取设置失败不应阻断添加流程
    console.error('Failed to check Sub-Store URL:', error)
    return true
  }
}

const showAddDialog = async () => {
  if (!await checkSubStoreUrl()) return
  isEdit.value = false
  form.value = {
    name: '',
    proxy_string: '',
    enabled: true,
    remark: ''
  }
  dialogVisible.value = true
}

const showBatchAddDialog = async () => {
  if (!await checkSubStoreUrl()) return
  batchForm.value = {
    nodes_text: '',
    enabled: true
  }
  batchDialogVisible.value = true
}

// 切换节点选中状态
const toggleNodeSelection = (nodeId: string) => {
  if (selectedNodeIds.value.has(nodeId)) {
    selectedNodeIds.value.delete(nodeId)
  } else {
    selectedNodeIds.value.add(nodeId)
  }
  // 触发响应式更新
  selectedNodeIds.value = new Set(selectedNodeIds.value)
}

// 切换节点字符串展开/收起
const toggleNodeExpand = (nodeId: string) => {
  if (expandedNodes.value.has(nodeId)) {
    expandedNodes.value.delete(nodeId)
  } else {
    expandedNodes.value.add(nodeId)
  }
  // 触发响应式更新
  expandedNodes.value = new Set(expandedNodes.value)
}

// 全选/取消全选
const toggleSelectAll = () => {
  if (isAllSelected.value) {
    selectedNodeIds.value.clear()
  } else {
    selectedNodeIds.value = new Set(nodes.value.map(n => n.id))
  }
}

// 批量删除
const batchDeleteNodes = async () => {
  if (selectedNodeIds.value.size === 0) {
    notify.warning('请先选择要删除的节点')
    return
  }

  const confirmed = await confirmDanger(
    `确定要删除选中的 ${selectedNodeIds.value.size} 个节点吗？删除后将同步清理策略组中对这些节点的引用。`,
    { title: '批量删除节点' }
  )
  if (!confirmed) return

  try {
    const nodeIdsToDelete = Array.from(selectedNodeIds.value)
    let successCount = 0
    let failCount = 0

    // 批量删除节点
    for (const nodeId of nodeIdsToDelete) {
      try {
        await nodeApi.delete(nodeId)
        successCount++
      } catch (error) {
        failCount++
        console.error(`删除节点 ${nodeId} 失败:`, error)
      }
    }

    // 获取所有策略组，清理引用
    try {
      const { data: proxyGroups } = await api.get('/proxy-groups')
      let updatedGroupCount = 0

      for (const group of proxyGroups) {
        let groupModified = false

        if (group.manual_nodes && group.manual_nodes.length > 0) {
          const originalLength = group.manual_nodes.length
          group.manual_nodes = group.manual_nodes.filter(
            (id: string) => !nodeIdsToDelete.includes(id)
          )
          if (group.manual_nodes.length !== originalLength) {
            groupModified = true
          }
        }

        if (group.proxies_order && group.proxies_order.length > 0) {
          const originalLength = group.proxies_order.length
          group.proxies_order = group.proxies_order.filter(
            (item: any) => !(item.type === 'node' && nodeIdsToDelete.includes(item.id))
          )
          if (group.proxies_order.length !== originalLength) {
            groupModified = true
          }
        }

        if (groupModified) {
          try {
            await api.put(`/proxy-groups/${group.id}`, group)
            updatedGroupCount++
          } catch (error) {
            console.error(`更新策略组 ${group.name} 失败:`, error)
          }
        }
      }

      // 显示结果
      if (failCount === 0) {
        if (updatedGroupCount > 0) {
          notify.success(`批量删除成功！已删除 ${successCount} 个节点，清理了 ${updatedGroupCount} 个策略组中的引用`)
        } else {
          notify.success(`批量删除成功！已删除 ${successCount} 个节点`)
        }
      } else {
        notify.warning(`批量删除完成！成功 ${successCount} 个，失败 ${failCount} 个`)
      }
    } catch (error) {
      console.error('清理策略组引用失败:', error)
      notify.warning(`已删除 ${successCount} 个节点，但清理策略组引用时出现错误`)
    }

    // 清空选择并刷新列表
    selectedNodeIds.value.clear()
    loadNodes()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      notify.error('批量删除失败')
      console.error('批量删除节点失败:', error)
    }
  }
}

const handleToggle = (node: ProxyNode) => {
  node.enabled = !node.enabled
  toggleNodeEnabled(node)
}

const toggleNodeEnabled = async (node: ProxyNode) => {
  const previous = !node.enabled
  savingStatus.value[node.id] = true
  try {
    await nodeApi.update(node.id, node)
    notify.success(node.enabled ? '已启用' : '已禁用')
  } catch (error) {
    notify.error('更新状态失败')
    node.enabled = previous
    loadNodes()
  } finally {
    savingStatus.value[node.id] = false
  }
}

const editNode = (row: ProxyNode) => {
  isEdit.value = true
  form.value = { ...row }

  // 自动格式化对象格式的节点字符串
  if (form.value.proxy_string) {
    form.value.proxy_string = formatProxyString(form.value.proxy_string)
  }

  dialogVisible.value = true
}

// 格式化节点字符串
const formatProxyString = (str: string) => {
  if (!str) return str

  const trimmed = str.trim()

  // 去除 YAML 列表标记
  let content = trimmed
  if (content.startsWith('- ')) {
    content = content.substring(2).trim()
  }

  // 如果是多行 YAML 格式（不以 { 开头），直接返回
  if (content.includes('\n') && !content.startsWith('{')) {
    return content
  }

  // 如果已经是格式化的 JSON，直接返回
  if (content.startsWith('{') && content.includes('\n')) {
    try {
      // 验证是否是有效的 JSON
      JSON.parse(content)
      return content
    } catch {
      // 不是有效的 JSON，继续处理
    }
  }

  // 检查是否是单行 JSON 对象格式
  if (content.startsWith('{') && content.endsWith('}') && !content.includes('\n')) {
    try {
      // 尝试直接解析为 JSON
      const obj = JSON.parse(content)
      return JSON.stringify(obj, null, 2)
    } catch {
      // JSON 解析失败，尝试作为 YAML 对象解析
      try {
        // 移除首尾的大括号
        let inner = content.substring(1, content.length - 1).trim()

        // 分割键值对
        const pairs: string[] = []
        let currentPair = ''
        let depth = 0

        for (let i = 0; i < inner.length; i++) {
          const char = inner[i]

          if (char === '{' || char === '[') {
            depth++
          } else if (char === '}' || char === ']') {
            depth--
          } else if (char === ',' && depth === 0) {
            pairs.push(currentPair.trim())
            currentPair = ''
            continue
          }

          currentPair += char
        }

        if (currentPair.trim()) {
          pairs.push(currentPair.trim())
        }

        // 构建 JSON 对象
        const obj: Record<string, any> = {}

        for (const pair of pairs) {
          const colonIndex = pair.indexOf(':')
          if (colonIndex > 0) {
            let key = pair.substring(0, colonIndex).trim()
            let value = pair.substring(colonIndex + 1).trim()

            // 移除键周围的引号（如果有）
            if ((key.startsWith('"') && key.endsWith('"')) ||
                (key.startsWith("'") && key.endsWith("'"))) {
              key = key.substring(1, key.length - 1)
            }

            // 移除值周围的引号（如果有）
            if ((value.startsWith('"') && value.endsWith('"')) ||
                (value.startsWith("'") && value.endsWith("'"))) {
              value = value.substring(1, value.length - 1)
            }

            // 尝试解析值
            if (value === 'true') {
              obj[key] = true
            } else if (value === 'false') {
              obj[key] = false
            } else if (!isNaN(Number(value)) && value !== '' && !/^0\d+/.test(value)) {
              obj[key] = Number(value)
            } else {
              obj[key] = value
            }
          }
        }

        // 转换为格式化的 JSON
        return JSON.stringify(obj, null, 2)
      } catch (e) {
        // 解析失败，返回原字符串
        console.warn('Failed to format proxy string:', e)
      }
    }
  }

  return str
}

const saveNode = async () => {
  try {
    // 在保存前格式化节点字符串
    if (form.value.proxy_string) {
      form.value.proxy_string = formatProxyString(form.value.proxy_string)
    }

    if (isEdit.value) {
      await nodeApi.update(form.value.id!, form.value)
      notify.success('更新成功')
    } else {
      await nodeApi.create(form.value)
      notify.success('添加成功')
    }
    dialogVisible.value = false
    loadNodes()
  } catch (error) {
    notify.error('保存失败')
  }
}

// 从节点链接中提取名称（通常在#后面）
const extractNodeName = (proxyString: string): string | undefined => {
  try {
    const trimmed = proxyString.trim()

    // 检查是否包含 #
    const hashIndex = trimmed.indexOf('#')
    if (hashIndex === -1) {
      return undefined
    }

    // 提取 # 后面的部分
    let name = trimmed.substring(hashIndex + 1).trim()

    // URL 解码（节点名称可能是编码的）
    try {
      name = decodeURIComponent(name)
    } catch {
      // 解码失败，使用原始名称
    }

    // 如果名称为空或只包含空格，返回 undefined
    if (!name || name.length === 0) {
      return undefined
    }

    return name
  } catch {
    return undefined
  }
}

const saveBatchNodes = async () => {
  console.log('[批量添加] 版本: v2.0 - 支持 YAML 格式')
  try {
    const text = batchForm.value.nodes_text.trim()
    if (!text) {
      notify.warning('请输入节点链接')
      return
    }

    console.log('[批量添加] 输入文本长度:', text.length, '字符')
    console.log('[批量添加] 输入文本前100字符:', text.substring(0, 100))

    let successCount = 0
    let failCount = 0
    const errors: string[] = []
    let autoNameCounter = 1 // 自动命名计数器

    // 检查是否是完整的 YAML 格式（包含 proxies: 或以 - name:/- type: 开头的列表）
    const hasProxiesKey = text.includes('proxies:')
    const hasYamlList = /^[\s]*-[\s]+(name|type):/m.test(text)
    const isYamlFormat = hasProxiesKey || hasYamlList

    console.log('[批量添加] 格式检测 - proxies:', hasProxiesKey, ', YAML列表:', hasYamlList, ', 判定为YAML:', isYamlFormat)

    if (isYamlFormat) {
      console.log('[批量添加] 检测到 YAML 格式，开始解析')
      try {
        // 尝试解析为 YAML
        const parsed = yaml.load(text)
        console.log('[批量添加] YAML 解析结果:', parsed)
        let proxies: any[] = []

        // 如果包含 proxies 字段，提取 proxies 数组
        if (parsed && typeof parsed === 'object' && 'proxies' in parsed) {
          proxies = Array.isArray(parsed.proxies) ? parsed.proxies : []
          console.log('[批量添加] 从 proxies 字段提取到', proxies.length, '个节点')
        }
        // 如果直接是数组（以 - 开头的 YAML 列表）
        else if (Array.isArray(parsed)) {
          proxies = parsed
          console.log('[批量添加] 直接解析为数组，包含', proxies.length, '个节点')
        }

        if (proxies.length === 0) {
          notify.warning('未找到有效的节点定义')
          console.warn('[批量添加] 未找到有效的节点定义')
          return
        }

        // 批量创建节点
        for (let i = 0; i < proxies.length; i++) {
          const proxy = proxies[i]
          try {
            // 将节点对象转换为 YAML 字符串
            const proxyYaml = yaml.dump(proxy, { indent: 2, lineWidth: -1 })

            // 使用节点中的 name 字段作为名称
            const nodeName = proxy.name || `节点_${autoNameCounter++}`

            // 创建节点
            const nodeData: any = {
              name: nodeName,
              proxy_string: proxyYaml.trim(),
              enabled: batchForm.value.enabled
            }

            await nodeApi.create(nodeData)
            successCount++
          } catch (error: any) {
            failCount++
            const errorMsg = error?.response?.data?.detail || error?.message || '未知错误'
            errors.push(`节点 ${i + 1} (${proxy.name || 'unnamed'}): ${errorMsg}`)
          }
        }
      } catch (yamlError: any) {
        notify.error(`YAML 解析失败: ${yamlError.message}`)
        return
      }
    } else {
      // 原有的按行处理逻辑（用于 URI 格式和单行 JSON）
      console.log('[批量添加] 使用按行处理模式')
      const lines = text.split('\n')
        .map(line => line.trim())
        .filter(line => line && !line.startsWith('//'))

      console.log('[批量添加] 过滤后的行数:', lines.length)

      if (lines.length === 0) {
        notify.warning('没有有效的节点链接')
        return
      }

      // 批量处理每一行
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        try {
          // 格式化节点字符串
          const formattedProxyString = formatProxyString(line)

          // 提取节点名称
          let nodeName = extractNodeName(line)

          // 如果提取不到名称，自动生成一个
          if (!nodeName) {
            nodeName = `节点_${autoNameCounter}`
            autoNameCounter++
          }

          // 创建节点
          const nodeData: any = {
            name: nodeName,
            proxy_string: formattedProxyString,
            enabled: batchForm.value.enabled
          }

          await nodeApi.create(nodeData)
          successCount++
        } catch (error: any) {
          failCount++
          const errorMsg = error?.response?.data?.detail || error?.message || '未知错误'
          errors.push(`第 ${i + 1} 行: ${errorMsg}`)
        }
      }
    }

    // 显示结果摘要
    if (failCount === 0) {
      notify.success(`批量添加完成！成功添加 ${successCount} 个节点`)
    } else if (successCount === 0) {
      notify.error(`批量添加失败！所有 ${failCount} 个节点都添加失败`)
      if (errors.length > 0) {
        console.error('批量添加错误详情:', errors)
      }
    } else {
      notify.warning(`批量添加完成！成功 ${successCount} 个，失败 ${failCount} 个`)
      if (errors.length > 0 && errors.length <= 5) {
        // 如果错误不多，显示错误详情
        setTimeout(() => {
          errors.forEach(err => notify.error(err))
        }, 500)
      } else if (errors.length > 5) {
        console.error('批量添加错误详情:', errors)
        notify.info('查看控制台了解详细错误信息')
      }
    }

    // 如果有成功的，关闭对话框并刷新列表
    if (successCount > 0) {
      batchDialogVisible.value = false
      loadNodes()
    }
  } catch (error) {
    notify.error('批量添加失败')
    console.error('批量添加错误:', error)
  }
}

const deleteNode = async (row: ProxyNode) => {
  const confirmed = await confirmDanger(
    '确定要删除该节点吗？删除后将同步清理策略组中对该节点的引用。',
    { title: '删除节点' }
  )
  if (!confirmed) return

  try {
    // 先删除节点
    await nodeApi.delete(row.id)

    // 获取所有策略组，清理引用
    const { data: proxyGroups } = await api.get('/proxy-groups')
    let updatedCount = 0

    // 查找引用了该节点的策略组
    for (const group of proxyGroups) {
      if (group.manual_nodes && group.manual_nodes.includes(row.id)) {
        // 从节点列表中移除该节点ID
        group.manual_nodes = group.manual_nodes.filter((id: string) => id !== row.id)

        // 同步更新 proxies_order
        if (group.proxies_order && group.proxies_order.length > 0) {
          group.proxies_order = group.proxies_order.filter(
            (item: any) => !(item.type === 'node' && item.id === row.id)
          )
        }

        try {
          await api.put(`/proxy-groups/${group.id}`, group)
          updatedCount++
        } catch (error) {
          console.error(`更新策略组 ${group.name} 失败:`, error)
        }
      }
    }

    if (updatedCount > 0) {
      notify.success(`删除成功，已同步清理 ${updatedCount} 个策略组中的引用`)
    } else {
      notify.success('删除成功')
    }
    loadNodes()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      notify.error('删除失败')
      console.error('删除节点失败:', error)
    }
  }
}



/* ---------- 视图模式与筛选 ---------- */
type ViewMode = 'list' | 'card'
const VIEW_KEY = 'configflow-nodes-view'

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

// 节点表格列多，窄屏不可读，移动端一律用卡片
const isNarrow = ref(false)
const syncNarrow = () => {
  isNarrow.value = window.matchMedia('(max-width: 900px)').matches
}
const effectiveView = computed<ViewMode>(() => (isNarrow.value ? 'card' : viewMode.value))

const keyword = ref('')
const protocolFilter = ref('all')

const nodeProtocol = (node: any): string => {
  const raw = node.type || getProtocol(node.proxy_string) || ''
  return String(raw).toUpperCase() || '—'
}

const nodeAddress = (node: any): string => {
  if (!node.server) return '—'
  return node.port ? `${node.server}:${node.port}` : String(node.server)
}

const protocolOptions = computed(() => {
  const set = new Set<string>()
  nodes.value.forEach(n => {
    const p = (n.type || getProtocol(n.proxy_string) || '').toString().toLowerCase()
    if (p) set.add(p)
  })
  return [...set].sort()
})

// 排序模式下必须展示完整列表，否则筛选会让保存的顺序丢条目
const visibleNodes = computed(() => {
  if (reorder.active.value) return nodes.value
  const q = keyword.value.trim().toLowerCase()
  return nodes.value.filter(n => {
    if (protocolFilter.value !== 'all') {
      const p = (n.type || getProtocol(n.proxy_string) || '').toString().toLowerCase()
      if (p !== protocolFilter.value) return false
    }
    if (!q) return true
    return [n.name, n.server, n.remark, n.subscription_name]
      .some(v => String(v || '').toLowerCase().includes(q))
  })
})

const nodesEmptyText = computed(() =>
  nodes.value.length === 0 ? '还没有节点' : '没有匹配的节点'
)

/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: nodes,
  container: nodesContainer,
  labelOf: item => item.name || item.id,
  // 按 id 提交，服务端在存量数据上重排，不回写列表接口的计算字段
  persist: async items => {
    await api.post('/nodes/reorder', {
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

onMounted(() => {
  syncNarrow()
  window.addEventListener('resize', syncNarrow)
  loadNodes()
})

onUnmounted(() => {
  window.removeEventListener('resize', syncNarrow)
})
</script>

