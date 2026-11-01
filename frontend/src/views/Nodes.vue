<template>
  <div :class="reorder.active.value && 'cf-reordering'">

    <PageHeader title="节点库">
      <template #actions>
        <DropdownMenu v-if="isPhone">
          <DropdownMenuTrigger as-child>
            <Button variant="outline" size="icon" aria-label="节点库更多操作">
              <MoreHorizontal class="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem @select="showBatchAddDialog">
              <FilePlus2 class="size-4" />
              批量添加
            </DropdownMenuItem>
            <DropdownMenuItem v-if="!reorder.active.value" :disabled="nodes.length < 2" @select="reorder.enter">
              <ArrowUpDown class="size-4" />
              调整顺序
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          v-if="!isPhone && !reorder.active.value"
          variant="outline"
          class="border-border/60 bg-background/40"
          :disabled="nodes.length < 2"
          @click="reorder.enter"
        >
          <ArrowUpDown class="size-4" />
          调整顺序
        </Button>
        <Button v-if="!isPhone" variant="outline" class="border-border/60 bg-background/40" @click="showBatchAddDialog">
          <FilePlus2 class="size-4" />
          批量添加
        </Button>
        <Button variant="outline" class="border-border/60 bg-background/40" aria-label="添加节点" @click="showAddDialog">
          <Plus class="size-4" />
          {{ isPhone ? '添加' : '添加节点' }}
        </Button>
        <NodeLatencySettings :disabled="testing || reorder.active.value" @saved="loadLatency" />
        <Button class="shadow-glow" title="从 ConfigFlow 所在机器，经节点访问测试地址" :disabled="testing || reorder.active.value" :aria-label="testing ? `正在测速… ${completedTests}/${totalTests}` : '全部测速'" @click="speedTest">
          <Loader2 v-if="testing" class="size-4 animate-spin" aria-hidden="true" />
          <Zap v-else class="size-4" aria-hidden="true" />
          {{ testing ? `正在测速… ${completedTests}/${totalTests}` : isPhone ? '测速' : '全部测速' }}
        </Button>
      </template>
    </PageHeader>

    <Toolbar v-model:search="keyword" placeholder="搜索名称、地址或备注…">

      <template #search-actions>
        <Button
          v-if="nodes.length > 0"
          variant="ghost"
          size="sm"
          :disabled="reorder.active.value"
          @click="toggleSelectAll"
        >
          {{ isAllSelected ? '取消全选' : '全选' }}
        </Button>
      </template>
      <template v-if="selectedNodeIds.size > 0 || !isNarrow" #actions>
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
        <ViewToggle v-if="!isNarrow" v-model="viewMode" />
      </template>
    </Toolbar>

    <!-- 地区 / 协议筛选：点一次只看这一类，再点取消 -->
    <div v-if="!reorder.active.value && (nodes.length || subNodes.length)" class="cf-node-filters mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
      <div class="cf-node-filter-row flex flex-wrap gap-1.5" role="group" aria-label="按来源筛选">
        <span class="cf-node-filter-label" aria-hidden="true">来源</span>
        <button type="button" class="chip" :aria-pressed="sourceFilter === 'all'" @click="sourceFilter = 'all'">全部来源</button>
        <button type="button" class="chip" :aria-pressed="sourceFilter === 'manual'" @click="sourceFilter = 'manual'">手动节点</button>
        <button type="button" class="chip" :aria-pressed="sourceFilter === 'subscription'" @click="sourceFilter = 'subscription'">订阅节点</button>
      </div>
      <div class="cf-node-filter-row flex flex-wrap gap-1.5" role="group" aria-label="按地区筛选">
        <span class="cf-node-filter-label" aria-hidden="true">地区</span>
        <button type="button" class="chip" :aria-pressed="!regionFilter" @click="regionFilter = null">全部</button>
        <button
          v-for="region in regionOptions"
          :key="region.code"
          type="button"
          class="chip"
          :aria-pressed="regionFilter === region.code"
          @click="regionFilter = regionFilter === region.code ? null : region.code"
        >
          {{ region.name }}
        </button>
      </div>
      <div class="cf-node-filter-row ml-auto flex flex-wrap gap-1.5" role="group" aria-label="按协议筛选">
        <span class="cf-node-filter-label" aria-hidden="true">协议</span>
        <button
          v-for="p in protocolOptions"
          :key="p"
          type="button"
          class="chip font-mono"
          :aria-pressed="protocolFilter === p"
          @click="protocolFilter = protocolFilter === p ? 'all' : p"
        >
          {{ p.toUpperCase() }}
        </button>
      </div>
    </div>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <SectionCard v-if="tileNodes.length === 0" :padded="false">
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
      :footer="`共 ${tileNodes.length} 个节点`"
    >
      <TableHeader>
        <TableRow class="hover:bg-transparent">
          <TableHead v-if="reorder.active.value" class="w-10"><span class="cf-sr">排序</span></TableHead>
          <TableHead class="w-10"><span class="cf-sr">选择</span></TableHead>
          <TableHead class="w-12 text-right">#</TableHead>
          <TableHead>名称</TableHead>
          <TableHead class="w-28">协议</TableHead>
          <TableHead>地址</TableHead>
          <TableHead class="w-24 text-right">代理延迟</TableHead>
          <TableHead class="w-40">来源</TableHead>
          <TableHead class="w-40 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody ref="nodesContainer">
        <TableRow
          v-for="(node, cfIndex) in tileNodes"
          :key="node.readonly ? `sub:${subscriptionNodeKey(node)}` : node.id || node.name"
          :data-name="node.name"
          :data-reorder-item="node.readonly ? undefined : ''"
          :data-state="!node.readonly && selectedNodeIds.has(node.id) ? 'selected' : undefined"
          :class="cn(!node.enabled && 'opacity-55', !node.readonly && !reorder.active.value && 'cursor-pointer')"
          @click="handleNodeSelectionClick(node, $event)"
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
              v-if="!node.readonly"
              :model-value="selectedNodeIds.has(node.id)"
              :aria-label="`选择 ${node.name}`"
              :disabled="reorder.active.value"
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
          <TableCell
            :class="cn('num text-right font-mono text-[12px] transition-colors', latencyTone(node), flashKeys.has(latencyKey(node)) && 'bg-primary-soft/40')"
            :title="testedAtLabel(node)"
          >
            <span v-if="testingKeys.has(latencyKey(node))" class="inline-flex items-center justify-end gap-1.5 font-sans" :aria-busy="true">
              <Loader2 v-if="activeTestingKeys.has(latencyKey(node))" class="size-3.5 shrink-0 animate-spin" aria-hidden="true" />
              {{ activeTestingKeys.has(latencyKey(node)) ? '正在测速…' : '等待测速' }}
            </span>
            <span v-else-if="unresolvedKeys.has(latencyKey(node))" class="text-muted-foreground" title="节点已删除或停用，请刷新后重试">不可用</span>
            <span v-else-if="latencyOf(node) === undefined" class="text-muted-foreground">未测速</span>
            <template v-else-if="latencyOf(node) === null">{{ failureLabel(node) }}</template>
            <template v-else>{{ latencyOf(node) }}<small class="ml-0.5 text-[10.5px] text-muted-foreground">ms</small></template>
          </TableCell>
          <TableCell class="truncate text-[12.5px] text-muted-foreground">
            {{ node.subscription_name || '手动添加' }}
          </TableCell>
          <TableCell class="cf-reorder-mute text-right">
            <div class="flex items-center justify-end gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                :aria-label="`单独测速 ${node.name}`"
                title="单独测速"
                :disabled="testing || node.enabled === false"
                @click="testOne(node)"
              >
                <Zap class="size-4" />
              </Button>
              <Button
                v-if="!node.readonly"
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
                v-if="!node.readonly"
                variant="ghost"
                size="icon-sm"
                :aria-label="`编辑 ${node.name}`"
                title="编辑"
                @click="editNode(node)"
              >
                <Pencil class="size-4" />
              </Button>
              <Button
                v-if="!node.readonly"
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

    <!-- ===== 延迟磁贴（默认） ===== -->
    <div
      v-else
      ref="nodesContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(212px,1fr))] gap-2.5 max-[420px]:grid-cols-1"
    >
      <div
        v-for="(node, cfIndex) in tileNodes"
        :key="node.readonly ? `sub:${subscriptionNodeKey(node)}` : node.id || node.name"
        class="cf-list-in"
        :style="listItemStyle(cfIndex)"
        :data-name="node.name"
        :data-tile="cfIndex"
        :aria-busy="testingKeys.has(latencyKey(node)) || undefined"
        :data-reorder-item="node.readonly ? undefined : ''"
        :class="cn(
          'node-tile cf-cv-row [--cf-cv-row-h:85px] max-md:[--cf-cv-row-h:101px] group relative overflow-hidden rounded-[14px] border px-[13px] pt-3 pb-2.5 transition-[transform,border-color,opacity] duration-300 ease-(--ease-flow) hover:-translate-y-0.5',
          !node.readonly && selectedNodeIds.has(node.id) ? 'border-primary-accent/55 bg-primary-soft/40 hover:border-primary-accent/70' : 'border-border bg-card/90 hover:border-border-strong',
          !node.readonly && !reorder.active.value && 'cursor-pointer',
          !node.enabled && 'opacity-55',
          flashKeys.has(latencyKey(node)) && 'is-flash'
        )"
        @click="handleNodeSelectionClick(node, $event)"
      >
        <header class="flex items-center gap-2">
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
            v-else-if="!node.readonly"
            class="cf-reorder-mute"
            :model-value="selectedNodeIds.has(node.id)"
            :aria-label="`选择 ${node.name}`"
            @update:model-value="toggleNodeSelection(node.id)"
          />
          <span class="min-w-0 flex-1 truncate text-[12.5px] font-medium" :title="node.remark ? `${node.name} · ${node.remark}` : node.name">
            {{ node.name }}
          </span>
          <Button
            v-if="node.readonly"
            variant="ghost"
            size="icon-sm"
            class="-my-1 -mr-1.5 size-7 shrink-0 opacity-60 group-hover:opacity-100"
            :title="`单独测速 ${node.name}`"
            :aria-label="`单独测速 ${node.name}`"
            :disabled="testing"
            @click="testOne(node)"
          >
            <Zap class="size-3.5" />
          </Button>
          <template v-else-if="!reorder.active.value">
          <!-- 高频操作直接放在卡片上，悬停显示；其余收进「更多」 -->
          <Button
            variant="ghost"
            size="icon-sm"
            :class="cn(
              '-my-1 size-7 shrink-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 max-md:opacity-100',
              node.enabled ? 'text-success-accent' : 'text-muted-foreground'
            )"
            :title="node.enabled ? '停用' : '启用'"
            :aria-label="node.enabled ? `停用 ${node.name}` : `启用 ${node.name}`"
            :disabled="savingStatus[node.id]"
            @click="handleToggle(node)"
          >
            <component :is="node.enabled ? Eye : EyeOff" class="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            class="-my-1 size-7 shrink-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 max-md:opacity-100"
            title="编辑"
            :aria-label="`编辑 ${node.name}`"
            @click="editNode(node)"
          >
            <Pencil class="size-3.5" />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button
                variant="ghost"
                size="icon-sm"
                class="cf-reorder-mute -my-1 -mr-1.5 size-7 shrink-0 opacity-60 group-hover:opacity-100"
                :aria-label="`${node.name} 的更多操作`"
              >
                <MoreHorizontal class="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="glass-strong min-w-[168px]">
              <DropdownMenuItem @select="testOne(node)">
                <Zap class="size-4" />
                单独测速
              </DropdownMenuItem>
              <DropdownMenuItem @select="viewingNode = node">
                <Link2 class="size-4" />
                节点字符串
              </DropdownMenuItem>
              <DropdownMenuItem @select="toggleNodeSelection(node.id)">
                <CheckSquare class="size-4" />
                {{ selectedNodeIds.has(node.id) ? '取消选择' : '选择' }}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" @select="deleteNode(node)">
                <Trash2 class="size-4" />
                删除
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          </template>
        </header>

        <div class="cf-reorder-mute mt-3 flex items-end justify-between gap-2">
          <span :title="testedAtLabel(node)" :class="cn('font-display text-[26px] leading-none font-medium tracking-[-0.02em]', latencyTone(node))">
            <span v-if="testingKeys.has(latencyKey(node))" class="inline-flex items-center gap-1.5 font-sans text-[14px] leading-6 tracking-normal">
              <Loader2 v-if="activeTestingKeys.has(latencyKey(node))" class="size-4 shrink-0 animate-spin" aria-hidden="true" />
              {{ activeTestingKeys.has(latencyKey(node)) ? '正在测速…' : '等待测速' }}
            </span>
            <template v-else-if="unresolvedKeys.has(latencyKey(node))"><span class="text-[18px] text-muted-foreground" title="节点已删除或停用，请刷新后重试">不可用</span></template>
            <template v-else-if="latencyOf(node) === undefined"><span class="text-[18px] text-muted-foreground">未测速</span></template>
            <template v-else-if="latencyOf(node) === null">{{ failureLabel(node) }}</template>
            <template v-else>{{ latencyOf(node) }}<small class="ml-0.5 font-mono text-[10.5px] text-muted-foreground">ms</small></template>
          </span>
          <span class="min-w-0 text-right font-mono text-[10.5px] leading-snug text-muted-foreground">
            <template v-if="regionOf(node.name).code !== '其他'">{{ regionOf(node.name).code }} · </template>{{ nodeProtocol(node) }}<br />
            <span class="block max-w-[110px] truncate">{{ node.subscription_name || '手动添加' }}</span>
          </span>
        </div>

        <p v-if="!testingKeys.has(latencyKey(node)) && latencyMap[latencyKey(node)]?.error" class="mt-2 break-words text-xs text-destructive-accent">
          {{ latencyMap[latencyKey(node)]?.error }}
        </p>
        <!-- 最近 14 次代理延迟；失败画一截红色短柱 -->
        <div class="cf-reorder-mute mt-2 flex h-4 items-end gap-0.5" aria-hidden="true">
          <i
            v-for="(bar, i) in historyBars(node)"
            :key="i"
            :class="cn('flex-1 rounded-[1px]', bar === null ? 'bg-destructive-accent/70' : bar < 0 ? 'bg-border-strong' : 'bg-border-strong')"
            :style="{ height: `${bar === null ? 18 : bar < 0 ? 12 : Math.round(bar * 100)}%` }"
          />
        </div>
      </div>
    </div>

    <NodeDeleteDialog ref="nodeDeleteDialog" @deleted="onNodesDeleted" />
    <!-- 节点字符串 -->
    <Dialog :open="!!viewingNode" @update:open="v => !v && (viewingNode = null)">
      <DialogContent class="glass-strong max-w-[680px]">
        <DialogHeader>
          <DialogTitle>{{ viewingNode?.name }}</DialogTitle>
          <DialogDescription>节点字符串</DialogDescription>
        </DialogHeader>
        <pre
          class="max-h-[60dvh] overflow-auto rounded-lg border border-border bg-background/60 p-3 font-mono text-[12px] leading-relaxed break-all whitespace-pre-wrap text-muted-foreground"
        >{{ viewingNode ? formatProxyStringForDisplay(viewingNode.proxy_string) : '' }}</pre>
        <DialogFooter>
          <Button variant="outline" @click="viewingNode = null">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 新增 / 编辑节点 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline max-w-[720px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑节点' : '添加节点' }}</DialogTitle>
          <DialogDescription>填写节点名称，粘贴节点链接或 JSON / YAML 配置。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[60dvh] flex-col gap-4 overflow-y-auto">
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
              placeholder="支持 URI、JSON、YAML、Surge / Loon 节点行等格式"
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
              placeholder="支持 URI、JSON、YAML，以及 Surge / Loon 节点行（如 HK = snell, 1.2.3.4, 443, psk=xxx, version=4），自动忽略空行和 // 注释"
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
import { useMediaQuery } from '@vueuse/core'
import {
  ArrowUpDown,
  CheckSquare,
  ChevronDown,
  Eye,
  EyeOff,
  FilePlus2,
  Link2,
  Loader2,
  MoreHorizontal,
  Network,
  Pencil,
  Plus,
  Trash2,
  Zap
} from '@lucide/vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { useRouter } from 'vue-router'
import { cn } from '@/lib/utils'
import { consumeAction } from '@/lib/actions'
import { OTHER_REGION, regionOf } from '@/lib/regions'
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
import NodeLatencySettings from '@/components/nodes/NodeLatencySettings.vue'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import { notify } from '@/lib/feedback'
import NodeDeleteDialog from '@/components/nodes/NodeDeleteDialog.vue'
import { listItemStyle } from '@/lib/motion'
import { nodeApi } from '@/api'
import type { ProxyNode } from '@/types'
import api from '@/api'
import * as yaml from 'js-yaml'


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
const nodeDeleteDialog = ref<InstanceType<typeof NodeDeleteDialog> | null>(null)
const isAllSelected = computed(() => {
  return visibleNodes.value.length > 0 && visibleNodes.value.every(n => selectedNodeIds.value.has(n.id))
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
    await loadSubscriptionNodes()
  } catch (error) {
    notify.error('加载节点列表失败')
  }
}

const showAddDialog = () => {
  isEdit.value = false
  form.value = {
    name: '',
    proxy_string: '',
    enabled: true,
    remark: ''
  }
  dialogVisible.value = true
}

const showBatchAddDialog = () => {
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

const handleNodeSelectionClick = (node: { id: string; readonly?: boolean }, event: MouseEvent) => {
  if (node.readonly || reorder.active.value || event.defaultPrevented) return
  const target = event.target
  // Child controls retain their own action, including clicks on their icons.
  if (!(target instanceof Element) || target.closest('button, a, input, select, textarea, label, [role="button"], [role="checkbox"], [role="menuitem"], [contenteditable="true"]')) return
  // Selecting node text for copying must not also select the node.
  const selection = window.getSelection()
  if (selection && !selection.isCollapsed && event.currentTarget instanceof Element
    && (event.currentTarget.contains(selection.anchorNode) || event.currentTarget.contains(selection.focusNode))) return
  toggleNodeSelection(node.id)
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
  // Only toggle visible rows; keep selections made before filtering.
  const next = new Set(selectedNodeIds.value)
  const deselect = isAllSelected.value
  for (const node of visibleNodes.value) {
    if (deselect) next.delete(node.id)
    else next.add(node.id)
  }
  selectedNodeIds.value = next
}

// 批量删除
const batchDeleteNodes = () => {
  if (selectedNodeIds.value.size === 0) {
    notify.warning('请先选择要删除的节点')
    return
  }

  nodeDeleteDialog.value?.show(Array.from(selectedNodeIds.value))
}

const onNodesDeleted = (ids: string[]) => {
  const remaining = new Set(selectedNodeIds.value)
  ids.forEach(id => remaining.delete(id))
  selectedNodeIds.value = remaining
  void loadNodes()
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
  } catch (error: any) {
    notify.error(error?.response?.data?.message || '更新状态失败')
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
    const raw = form.value.proxy_string?.trim() || ''
    if (raw && !raw.includes('\n') && isClientProxyLine(raw)) {
      // 单个添加同样支持 Surge / Loon 节点行
      const response = await nodeApi.parseLines([raw])
      const proxy = response.data.results[0]?.proxy
      if (!proxy) {
        notify.error(response.data.results[0]?.error || '无法识别的节点格式')
        return
      }
      if (!form.value.name) form.value.name = proxy.name
      form.value.proxy_string = proxyToYaml(proxy)
    } else if (form.value.proxy_string) {
      // 在保存前格式化节点字符串
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
  } catch (error: any) {
    notify.error(error?.response?.data?.message || '保存失败')
  }
}

// Surge / Loon 等客户端配置里的节点行：「名称 = 类型, 服务器, 端口, ...」。snell 没有通用 URI，只有这种写法
const CLIENT_PROXY_LINE = /^[^=]+?=\s*[A-Za-z][\w-]*\s*,/
const isClientProxyLine = (line: string) =>
  CLIENT_PROXY_LINE.test(line) && !line.slice(0, line.indexOf('=')).includes('://')

const proxyToYaml = (proxy: Record<string, any>) => yaml.dump(proxy, { indent: 2, lineWidth: -1 }).trim()

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

      // 客户端节点行一次性交给后端解析，转成结构化 YAML 保存（保留名称和协议）
      const clientLines = lines.filter(isClientProxyLine)
      const parsedClientLines = new Map<string, { proxy?: Record<string, any>; error?: string }>()
      if (clientLines.length) {
        try {
          const response = await nodeApi.parseLines(clientLines)
          clientLines.forEach((line, index) => parsedClientLines.set(line, response.data.results[index] || {}))
        } catch (error: any) {
          const message = error?.response?.data?.message || '解析节点行失败'
          clientLines.forEach(line => parsedClientLines.set(line, { error: message }))
        }
      }

      // 批量处理每一行
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        try {
          const parsed = parsedClientLines.get(line)
          if (parsed) {
            if (!parsed.proxy) throw new Error(parsed.error || '无法识别的节点格式')
            await nodeApi.create({
              name: parsed.proxy.name || `节点_${autoNameCounter++}`,
              proxy_string: proxyToYaml(parsed.proxy),
              enabled: batchForm.value.enabled
            })
            successCount++
            continue
          }

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

const deleteNode = (row: ProxyNode) => nodeDeleteDialog.value?.show([row.id])



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

// 节点表格列多，窄屏不可读，移动端一律用卡片
const isNarrow = ref(false)
const isPhone = useMediaQuery('(max-width: 639px)')
const syncNarrow = () => {
  isNarrow.value = window.matchMedia('(max-width: 900px)').matches
}
const effectiveView = computed<ViewMode>(() => (isNarrow.value ? 'card' : viewMode.value))

const keyword = ref('')
const protocolFilter = ref('all')
const regionFilter = ref<string | null>(null)
const sourceFilter = ref<'all' | 'manual' | 'subscription'>('all')

/* ---------- 订阅节点：只读，与节点库一起测速与筛选 ---------- */
const subNodes = ref<any[]>([])
const eligibleImportedNodeIds = ref<Set<string>>(new Set())
const subscriptionNodeKey = (node: any): string =>
  JSON.stringify([node.source_id || node.subscription_id, node.name])
const extraSubscriptionNodes = computed(() => {
  const importedKeys = new Set(nodes.value.filter(n => n.subscription_id).map(subscriptionNodeKey))
  return subNodes.value.filter(n => !importedKeys.has(subscriptionNodeKey(n)))
})
const loadSubscriptionNodes = async () => {
  try {
    const { data: subs } = await api.get('/subscriptions')
    const ids = (subs || []).filter((s: any) => s.enabled !== false && s.id).map((s: any) => s.id)
    if (!ids.length) {
      subNodes.value = []
      eligibleImportedNodeIds.value = new Set()
      return
    }
    const { data } = await api.post('/proxy-groups/preview-regex', {
      source: 'subscription',
      regex: '.*',
      subscriptions: ids,
      deduplicate_by_source: true
    })
    eligibleImportedNodeIds.value = new Set(data.imported_node_ids || [])
    subNodes.value = (data.nodes || [])
      .filter((n: any) => n.source_type === 'subscription')
      .map((n: any) => ({ ...n, subscription_name: n.subscription_name || n.source_name, enabled: true, readonly: true }))
  } catch {
    subNodes.value = []
    eligibleImportedNodeIds.value = new Set()
    notify.error('加载订阅节点失败')
  }
}

const matchesFilters = (n: any) => {
  if (protocolFilter.value !== 'all') {
    const p = (n.type || getProtocol(n.proxy_string) || '').toString().toLowerCase()
    if (p !== protocolFilter.value) return false
  }
  if (regionFilter.value && regionOf(n.name).code !== regionFilter.value) return false
  const q = keyword.value.trim().toLowerCase()
  if (!q) return true
  return [n.name, n.server, n.remark, n.subscription_name].some(v => String(v || '').toLowerCase().includes(q))
}

/** 两种视图共用来源筛选；排序模式只显示可排序的节点库 */
const tileNodes = computed<any[]>(() => {
  if (reorder.active.value) return visibleNodes.value
  const fromSubs = sourceFilter.value === 'manual' ? [] : extraSubscriptionNodes.value.filter(matchesFilters)
  return [...visibleNodes.value, ...fromSubs]
})

/* ---------- 地区 ---------- */
const regionOptions = computed(() => {
  const counts = new Map<string, { code: string; name: string; count: number }>()
  ;[...nodes.value, ...extraSubscriptionNodes.value].forEach(n => {
    const r = regionOf(n.name)
    const item = counts.get(r.code) || { ...r, count: 0 }
    item.count++
    counts.set(r.code, item)
  })
  // 「其他」放最后，其余按数量排
  return [...counts.values()].sort((a, b) =>
    a.code === OTHER_REGION.code ? 1 : b.code === OTHER_REGION.code ? -1 : b.count - a.count
  )
})

/* ---------- 代理延迟（独立组件经节点访问测试地址，按来源区分同名节点） ---------- */
interface LatencyRecord {
  latency: number | null
  tested_at?: string
  history?: Array<number | null>
  error?: string
  error_code?: string
  url?: string
  first_latency?: number
  unified_delay?: boolean
}
const router = useRouter()
const latencyMap = ref<Record<string, LatencyRecord>>({})
const testing = ref(false)
const completedTests = ref(0)
const totalTests = ref(0)
let latencyDisposed = false
const testingKeys = ref<Set<string>>(new Set())
/** 当前发往服务器的批次；其余 testingKeys 仍在等待。 */
const activeTestingKeys = ref<Set<string>>(new Set())
const flashKeys = ref<Set<string>>(new Set())
/** 测试期间已删除或停用的节点 */
const unresolvedKeys = ref<Set<string>>(new Set())
const viewingNode = ref<any>(null)

const latencyKey = (node: any): string => node.readonly
  ? JSON.stringify(['subscription', node.source_id || node.subscription_id, node.name])
  : JSON.stringify(['node', node.id])
const latencyOf = (node: any): number | null | undefined => latencyMap.value[latencyKey(node)]?.latency
const failureLabel = (node: any): string => latencyMap.value[latencyKey(node)]?.error_code === 'timeout' ? '超时' : '失败'
const testedAtLabel = (node: any): string | undefined => {
  if (testingKeys.value.has(latencyKey(node))) return activeTestingKeys.value.has(latencyKey(node)) ? '正在测速…' : '等待测速'
  const entry = latencyMap.value[latencyKey(node)]
  if (!entry) return undefined
  return [
    entry.error,
    entry.url && `ConfigFlow → 节点 → ${entry.url}`,
    entry.first_latency != null && `首连 ${entry.first_latency}ms${entry.unified_delay ? ' · 当前显示统一延迟' : ''}`,
    entry.tested_at && `测于 ${new Date(entry.tested_at).toLocaleString()}`,
  ].filter(Boolean).join('\n')
}
const latencyTone = (node: any): string => {
  if (activeTestingKeys.value.has(latencyKey(node))) return 'text-primary-accent'
  if (testingKeys.value.has(latencyKey(node))) return 'text-muted-foreground'
  const v = latencyOf(node)
  if (v === undefined) return ''
  if (v === null) return 'text-destructive-accent'
  return v < 80 ? 'text-success-accent' : v < 180 ? 'text-warning-accent' : 'text-destructive-accent'
}
/** 14 格：已有记录按 1 - ms/300 归一（越快越高），不足的格子用 -1 占位 */
const historyBars = (node: any): Array<number | null> => {
  const records = (latencyMap.value[latencyKey(node)]?.history || []).slice(-14)
  const pad = Array.from({ length: 14 - records.length }, () => -1)
  return [...pad, ...records.map(v => (v === null ? null : Math.min(1, Math.max(0.12, 1 - v / 300))))]
}

const loadLatency = async () => {
  try {
    const { data } = await nodeApi.latency()
    latencyMap.value = data?.results || {}
  } catch {
    // 没有历史结果时显示「未测速」
  }
}

/** 结构化节点可直接探测；URI 等还需经 Sub-Store 转换。后端再次校验实际转换预算。 */
const requiresProxyConversion = (node: any): boolean => {
  if (!node.proxy_string) return false
  try {
    const parsed = yaml.load(node.proxy_string)
    return !(parsed && typeof parsed === 'object' && !Array.isArray(parsed) && 'type' in parsed && parsed.type)
  } catch {
    return true
  }
}

/** 保持节点顺序，每批最多 20 个，其中最多 5 个需要外部转换。 */
const LATENCY_BATCH_SIZE = 20
const LATENCY_CONVERSION_BATCH_SIZE = 5
const latencyBatches = (targets: Map<string, any>): string[][] => {
  const batches: string[][] = []
  let batch: string[] = []
  let conversions = 0
  for (const [key, node] of targets) {
    const requiresConversion = requiresProxyConversion(node)
    if (batch.length === LATENCY_BATCH_SIZE || (requiresConversion && conversions === LATENCY_CONVERSION_BATCH_SIZE)) {
      batches.push(batch)
      batch = []
      conversions = 0
    }
    batch.push(key)
    if (requiresConversion) conversions++
  }
  if (batch.length) batches.push(batch)
  return batches
}

const runLatency = async (targets: any[]) => {
  const targetsByKey = new Map<string, any>(targets.filter(n => n.enabled !== false).map(node => [latencyKey(node), node]))
  const keys = [...targetsByKey.keys()]
  if (!keys.length || testing.value) return
  testing.value = true
  completedTests.value = 0
  totalTests.value = keys.length
  testingKeys.value = new Set(keys)
  const results: Record<string, LatencyRecord> = {}
  const missing: string[] = []
  try {
    for (const batch of latencyBatches(targetsByKey)) {
      if (latencyDisposed) return
      activeTestingKeys.value = new Set(batch)
      const { data } = await nodeApi.testLatency(batch)
      if (latencyDisposed) return
      Object.assign(results, data?.results || {})
      latencyMap.value = { ...latencyMap.value, ...data?.results }
      missing.push(...(data?.missing || []))
      const unresolved = new Set(unresolvedKeys.value)
      batch.forEach(key => unresolved.delete(key))
      ;(data?.missing || []).forEach((key: string) => unresolved.add(key))
      unresolvedKeys.value = unresolved
      completedTests.value += batch.length
      testingKeys.value = new Set(keys.slice(completedTests.value))
      activeTestingKeys.value = new Set()
    }
    const measured = Object.entries(results).filter(([, r]) => r.latency !== null)
    const failed = Object.keys(results).length - measured.length
    const best = measured.sort((a, b) => (a[1].latency as number) - (b[1].latency as number))[0]
    if (best) {
      notify.success(
        `测速完成 · 最快 ${targetsByKey.get(best[0])?.name || '目标节点'} ${best[1].latency}ms`,
        [failed && `${failed} 个失败`, missing.length && `${missing.length} 个已删除或停用`].filter(Boolean).join('，') || undefined
      )
    } else {
      const reason = keys.length === 1 ? results[keys[0] ?? '']?.error : undefined
      notify.warning('测速完成，没有节点通过测试', reason || (missing.length ? `${missing.length} 个已删除或停用` : '可在节点上查看失败原因'))
    }
  } catch (error: any) {
    if (!latencyDisposed) notify.error(error.response?.data?.message || '测速失败', completedTests.value ? `已完成 ${completedTests.value}/${totalTests.value}，结果已保留` : undefined)
  } finally {
    testing.value = false
    testingKeys.value = new Set()
    activeTestingKeys.value = new Set()
  }
}

const speedTest = () => runLatency(tileNodes.value)
const testOne = (node: any) => runLatency([node])

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
  ;[...nodes.value, ...extraSubscriptionNodes.value].forEach(n => {
    const p = (n.type || getProtocol(n.proxy_string) || '').toString().toLowerCase()
    if (p) set.add(p)
  })
  return [...set].sort()
})

// 排序模式下必须展示完整列表，否则筛选会让保存的顺序丢条目
const visibleNodes = computed(() => {
  if (reorder.active.value) return nodes.value
  return nodes.value.filter(n => {
    const fromSubscription = Boolean(n.subscription_id)
    if (sourceFilter.value === 'manual' && fromSubscription) return false
    if (sourceFilter.value === 'subscription' && (!fromSubscription || !eligibleImportedNodeIds.value.has(n.id))) return false
    return matchesFilters(n)
  })
})

const nodesEmptyText = computed(() =>
  nodes.value.length === 0 ? '添加节点链接或配置，也可一次粘贴多个节点批量添加。' : '试试其他关键词，或调整筛选条件。'
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
    notify.success('顺序已保存，对所有配置生效')
  } catch (error) {
    notify.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  syncNarrow()
  window.addEventListener('resize', syncNarrow)
  await Promise.all([loadNodes(), loadLatency()])
  if (consumeAction(router, 'speedtest')) speedTest()
})

// 已在本页时从命令面板触发
watch(
  () => router?.currentRoute.value.query.run,
  run => {
    if (run === 'speedtest' && consumeAction(router, 'speedtest')) speedTest()
  }
)

onUnmounted(() => {
  latencyDisposed = true
  window.removeEventListener('resize', syncNarrow)
})
</script>

<style scoped>
.node-tile.is-flash {
  animation: tile-flash 0.9s var(--ease-flow);
}

@keyframes tile-flash {
  0% {
    border-color: var(--primary-accent);
    box-shadow: 0 0 0 0 oklch(from var(--primary-accent) l c h / 45%);
  }
  100% {
    box-shadow: 0 0 0 12px transparent;
  }
}
</style>
