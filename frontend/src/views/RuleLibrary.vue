<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner
      scope="resource"
      :profile-name="cfProfileName"
      description="规则集来源与缓存，按配置空间隔离"
    />

    <PageHeader
      eyebrow="Resource"
      title="规则库"
      description="集中维护规则集来源与缓存，供策略规则引用。"
    >
      <template #actions>
        <Button class="shadow-glow" @click="showAddDialog">
          <Plus class="size-4" />
          添加规则集
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <Button variant="outline" class="border-border/60 bg-background/40">
              更多
              <ChevronDown class="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" class="glass-strong">
            <DropdownMenuItem @select="showBatchImportDialog">
              <Upload class="size-4" />
              批量导入
            </DropdownMenuItem>
            <DropdownMenuItem :disabled="ruleLibrary.length === 0 || testing" @select="handleBatchTest">
              <Loader2 v-if="testing" class="size-4 animate-spin" />
              <Network v-else class="size-4" />
              {{ testing ? '测试中…' : '批量测试连通性' }}
            </DropdownMenuItem>
            <DropdownMenuItem :disabled="ruleLibrary.length < 2" @select="reorder.enter">
              <ArrowUpDown class="size-4" />
              调整顺序
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem @select="showProxyConfigDialog">
              <Settings class="size-4" />
              GitHub 代理
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <ViewToggle v-model="viewMode" class="max-md:hidden" />
      </template>
    </PageHeader>

    <SectionCard v-if="ruleLibrary.length === 0" :padded="false">
      <EmptyState
        :icon="FolderOpen"
        title="规则库还是空的"
        description="添加规则集来源（URL 或直接粘贴规则内容），策略规则即可引用它。"
      >
        <Button @click="showAddDialog">
          <Plus class="size-4" />
          添加规则集
        </Button>
      </EmptyState>
    </SectionCard>

    <template v-else>
      <Toolbar v-model:search="keyword" placeholder="搜索名称、地址或内容…">
        <template #filters>
          <Select v-model="behaviorFilter" :disabled="reorder.active.value">
            <SelectTrigger class="h-9 w-[140px] border-transparent bg-background/50 text-[13px]">
              <SelectValue placeholder="全部类型" />
            </SelectTrigger>
            <SelectContent class="glass-strong">
              <SelectItem value="all">全部类型</SelectItem>
              <SelectItem v-for="b in behaviorOptions" :key="b" :value="b">{{ b }}</SelectItem>
            </SelectContent>
          </Select>

          <label
            class="flex h-9 cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 text-[12.5px] text-muted-foreground"
          >
            <Checkbox
              :model-value="allSelected"
              :indeterminate="someSelected && !allSelected"
              @update:model-value="toggleSelectAll"
            />
            全选
            <span v-if="selectedRules.length" class="num text-primary-accent">
              已选 {{ selectedRules.length }}
            </span>
          </label>
        </template>

        <template #actions>
          <Button
            v-if="selectedRules.length"
            variant="outline"
            size="sm"
            class="border-border/60 bg-background/40"
            :disabled="caching"
            @click="handleBatchCache"
          >
            <Loader2 v-if="caching" class="size-3.5 animate-spin" />
            <Download v-else class="size-3.5" />
            {{ caching ? '缓存中…' : '批量缓存' }}
          </Button>
          <Button
            v-if="selectedRules.length"
            variant="outline"
            size="sm"
            class="border-destructive-accent/30 bg-destructive-soft/40 text-destructive-accent"
            @click="batchDeleteRules"
          >
            <Trash2 class="size-3.5" />
            删除 {{ selectedRules.length }} 项
          </Button>
        </template>
      </Toolbar>

      <ReorderBar
        :active="reorder.active.value"
        :saving="reorder.saving.value"
        :announcement="reorder.announcement.value"
        @cancel="reorder.cancel"
        @save="handleSaveOrder"
      />

      <SectionCard v-if="visibleRules.length === 0" :padded="false">
        <EmptyState :icon="FolderOpen" title="没有匹配的规则集" :description="rulesEmptyText" />
      </SectionCard>

      <!-- ===== 表格视图 ===== -->
      <DataTableShell
        v-else-if="effectiveView === 'list'"
        :footer="`共 ${visibleRules.length} 个规则集`"
      >
        <TableHeader>
          <TableRow class="hover:bg-transparent">
            <TableHead v-if="reorder.active.value" class="w-10"><span class="cf-sr">排序</span></TableHead>
            <TableHead class="w-10"><span class="cf-sr">选择</span></TableHead>
            <TableHead class="w-12 text-right">#</TableHead>
            <TableHead>名称</TableHead>
            <TableHead class="w-28">类型</TableHead>
            <TableHead>来源</TableHead>
            <TableHead class="w-44 text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody ref="rulesContainer">
          <TableRow
            v-for="(rule, cfIndex) in visibleRules"
            :key="rule.id"
            :data-id="rule.id"
            data-reorder-item
            :class="!rule.enabled && 'opacity-55'"
          >
            <TableCell v-if="reorder.active.value">
              <DragHandle
                :label="rule.name || rule.id"
                :index="cfIndex"
                :total="ruleLibrary.length"
                :position="reorder.positionLabel(cfIndex)"
                :grabbed="reorder.grabbedIndex.value === cfIndex"
                @up="reorder.moveUp(cfIndex)"
                @down="reorder.moveDown(cfIndex)"
                @keydown="reorder.onHandleKeydown($event, cfIndex)"
              />
            </TableCell>
            <TableCell class="cf-reorder-mute">
              <Checkbox
                :model-value="selectedRules.includes(rule.id)"
                :aria-label="`选择 ${rule.name}`"
                @update:model-value="toggleRuleSelection(rule.id)"
              />
            </TableCell>
            <TableCell class="num text-right text-muted-foreground">{{ cfIndex + 1 }}</TableCell>
            <TableCell>
              <div class="flex items-center gap-2">
                <span
                  class="size-1.5 shrink-0 rounded-full"
                  :class="rule.enabled
                    ? 'bg-success-accent shadow-[0_0_6px_var(--success-accent)]'
                    : 'bg-muted-foreground'"
                  aria-hidden="true"
                />
                <span class="min-w-0 truncate font-medium text-foreground">{{ rule.name }}</span>
              </div>
            </TableCell>
            <TableCell>
              <Badge variant="outline" class="font-mono text-[10.5px]">{{ rule.behavior }}</Badge>
            </TableCell>
            <TableCell class="max-w-[320px]">
              <span
                v-if="rule.source_type === 'content'"
                class="block truncate font-mono text-[12px] text-muted-foreground"
              >
                {{ getContentPreview(rule.content) }}
              </span>
              <a
                v-else
                class="block truncate font-mono text-[12px] text-info-accent hover:underline"
                :href="rule.url"
                target="_blank"
                rel="noreferrer"
              >
                {{ rule.url }}
              </a>
            </TableCell>
            <TableCell class="cf-reorder-mute text-right">
              <div class="flex items-center justify-end gap-0.5">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  :class="rule.enabled ? 'text-success-accent' : 'text-muted-foreground'"
                  :aria-label="rule.enabled ? `停用 ${rule.name}` : `启用 ${rule.name}`"
                  :title="rule.enabled ? '停用' : '启用'"
                  @click="handleToggle(rule)"
                >
                  <component :is="rule.enabled ? Eye : EyeOff" class="size-4" />
                </Button>
                <Button
                  v-if="rule.source_type === 'content'"
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="`向 ${rule.name} 添加规则`"
                  title="添加规则"
                  @click="showAddRuleToSetDialog(rule)"
                >
                  <Plus class="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="`复制 ${rule.name} 的下载地址`"
                  title="复制下载地址"
                  @click="copyRuleUrl(rule)"
                >
                  <Copy class="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  :aria-label="`编辑 ${rule.name}`"
                  title="编辑"
                  @click="editRule(rule)"
                >
                  <Pencil class="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="text-destructive-accent hover:bg-destructive-soft"
                  :aria-label="`删除 ${rule.name}`"
                  title="删除"
                  @click="deleteRule(rule)"
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
        ref="rulesContainer"
        class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-3 max-md:grid-cols-1"
      >
        <Motion
          v-for="(rule, cfIndex) in visibleRules"
          :key="rule.id"
          v-bind="listItem(cfIndex)"
          :data-id="rule.id"
          data-reorder-item
          :class="[
            'hairline edge-light relative flex flex-col gap-3 overflow-hidden rounded-xl border bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft',
            selectedRules.includes(rule.id) ? 'border-primary-accent/45' : 'border-border/35',
            !rule.enabled && 'opacity-60'
          ]"
        >
          <header class="flex items-start gap-2.5">
            <Checkbox
              class="cf-reorder-mute mt-0.5"
              :model-value="selectedRules.includes(rule.id)"
              :aria-label="`选择 ${rule.name}`"
              @update:model-value="toggleRuleSelection(rule.id)"
            />
            <DragHandle
              v-if="reorder.active.value"
              :label="rule.name || rule.id"
              :index="cfIndex"
              :total="ruleLibrary.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <p class="m-0 min-w-0 flex-1 truncate text-[14px] font-semibold text-foreground">
              {{ rule.name }}
            </p>
            <Button
              variant="ghost"
              size="icon-sm"
              class="cf-reorder-mute shrink-0"
              :class="rule.enabled ? 'text-success-accent' : 'text-muted-foreground'"
              :title="rule.enabled ? '停用' : '启用'"
              :aria-label="rule.enabled ? `停用 ${rule.name}` : `启用 ${rule.name}`"
              @click="handleToggle(rule)"
            >
              <component :is="rule.enabled ? Eye : EyeOff" class="size-4" />
            </Button>
          </header>

          <div class="cf-reorder-mute flex flex-wrap gap-1.5">
            <Badge variant="outline" class="font-mono text-[10.5px]">{{ rule.behavior }}</Badge>
            <Badge :variant="rule.source_type === 'content' ? 'info' : 'secondary'" class="text-[10.5px]">
              {{ rule.source_type === 'content' ? '规则内容' : 'URL 地址' }}
            </Badge>
          </div>

          <div class="cf-reorder-mute">
            <p class="m-0 mb-1.5 flex items-center gap-1.5 text-[11px] font-medium tracking-[0.04em] text-muted-foreground uppercase">
              <component :is="rule.source_type === 'content' ? FileText : Link2" class="size-3" aria-hidden="true" />
              {{ rule.source_type === 'content' ? '规则内容' : '规则地址' }}
            </p>
            <pre
              v-if="rule.source_type === 'content'"
              class="m-0 max-h-24 overflow-auto rounded-lg border border-border/50 bg-background/50 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-muted-foreground"
            >{{ getContentPreview(rule.content) }}</pre>
            <a
              v-else
              class="block truncate rounded-lg border border-border/50 bg-background/50 px-2.5 py-1.5 font-mono text-[11.5px] text-info-accent hover:underline"
              :href="rule.url"
              target="_blank"
              rel="noreferrer"
            >
              {{ rule.url }}
            </a>
          </div>

          <footer class="cf-reorder-mute mt-auto flex items-center gap-0.5 border-0 border-t border-border/50 pt-3">
            <Button
              v-if="rule.source_type === 'content'"
              variant="ghost"
              size="sm"
              @click="showAddRuleToSetDialog(rule)"
            >
              <Plus class="size-3.5" />
              添加规则
            </Button>
            <Button variant="ghost" size="icon-sm" title="复制下载地址" aria-label="复制下载地址" @click="copyRuleUrl(rule)">
              <Copy class="size-4" />
            </Button>
            <Button variant="ghost" size="icon-sm" class="ml-auto" title="编辑" aria-label="编辑" @click="editRule(rule)">
              <Pencil class="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              class="text-destructive-accent hover:bg-destructive-soft"
              title="删除"
              aria-label="删除"
              @click="deleteRule(rule)"
            >
              <Trash2 class="size-4" />
            </Button>
          </footer>
        </Motion>
      </div>
    </template>

    <!-- ===== 添加 / 编辑规则集 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="glass-strong hairline max-w-[700px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑规则' : '添加规则' }}</DialogTitle>
          <DialogDescription>可填写远程 URL，或直接粘贴规则内容由本机托管。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[62dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label for="lib-name">规则名</Label>
            <Input id="lib-name" v-model="form.name" class="bg-background/50" placeholder="请输入规则名称" />
          </div>

          <div class="flex flex-col gap-2">
            <Label>来源类型</Label>
            <RadioGroup v-model="form.source_type" class="flex gap-2">
              <label
                v-for="option in SOURCE_TYPES"
                :key="option.value"
                :class="[
                  'flex flex-1 cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors',
                  form.source_type === option.value
                    ? 'border-primary-accent/40 bg-primary-soft/50 text-foreground'
                    : 'border-border/50 bg-background/40 text-muted-foreground hover:border-border-strong'
                ]"
              >
                <RadioGroupItem :value="option.value" />
                {{ option.label }}
              </label>
            </RadioGroup>
          </div>

          <div v-if="form.source_type === 'url'" class="flex flex-col gap-1.5">
            <Label for="lib-url">规则地址</Label>
            <Input
              id="lib-url"
              v-model="form.url"
              class="bg-background/50 font-mono"
              placeholder="请输入规则集 URL 地址"
            />
          </div>

          <div v-if="form.source_type === 'content'" class="flex flex-col gap-1.5">
            <Label for="lib-content">规则内容</Label>
            <Textarea
              id="lib-content"
              v-model="form.content"
              class="min-h-[200px] bg-background/50 font-mono text-[12px]"
              :rows="10"
              :placeholder="ruleContentPlaceholder"
            />
            <p class="m-0 text-[12px] text-muted-foreground">{{ ruleContentHelperText }}</p>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select v-model="form.behavior">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem value="domain">Domain</SelectItem>
                <SelectItem value="ipcidr">IP CIDR</SelectItem>
                <SelectItem value="classical">Classical</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch
              id="lib-enabled"
              v-model="form.enabled"
              @update:model-value="value => handleFormStatusChange(Boolean(value))"
            />
            <Label for="lib-enabled" class="text-[13px] text-muted-foreground">
              {{ form.enabled ? '规则启用中' : '规则已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button @click="saveRule">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 向规则集添加规则 ===== -->
    <Dialog v-model:open="addRuleToSetDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[600px] border-border/50">
        <DialogHeader>
          <DialogTitle>添加规则到 {{ currentRuleSet?.name }}</DialogTitle>
          <DialogDescription>{{ addRuleToSetHelperText }}</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-4">
          <div v-if="currentRuleSet?.behavior === 'classical'" class="flex flex-col gap-1.5">
            <Label>规则类型</Label>
            <Select v-model="addRuleToSetForm.rule_type">
              <SelectTrigger class="w-full bg-background/50 font-mono">
                <SelectValue placeholder="选择规则类型" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="type in CLASSICAL_RULE_TYPES" :key="type" :value="type" class="font-mono">
                  {{ type }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="add-rule-value">值</Label>
            <Textarea
              id="add-rule-value"
              v-model="addRuleToSetForm.value"
              class="min-h-[120px] bg-background/50 font-mono text-[12px]"
              :rows="5"
              placeholder="域名、IP 或规则集名称，每行一条"
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="addRuleToSetDialogVisible = false">取消</Button>
          <Button @click="saveRuleToSet">添加</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 批量导入 ===== -->
    <Dialog v-model:open="batchImportDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[700px] border-border/50">
        <DialogHeader>
          <DialogTitle>批量导入规则</DialogTitle>
          <DialogDescription>粘贴 YAML 格式的 rule-providers 配置。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-3">
          <Alert class="border-info-accent/30 bg-info-soft/40">
            <AlertDescription class="text-[12.5px]">
              示例：
              <code class="ml-1 rounded border border-border/50 bg-background/60 px-1.5 py-0.5 font-mono text-[11.5px]">
                private_block: { type: http, behavior: classical, url: "https://…" }
              </code>
            </AlertDescription>
          </Alert>
          <Textarea
            v-model="batchImportText"
            class="min-h-[260px] bg-background/50 font-mono text-[12px]"
            :rows="12"
            placeholder="粘贴 rule-providers 配置内容…"
          />
        </div>

        <DialogFooter>
          <Button variant="outline" @click="batchImportDialogVisible = false">取消</Button>
          <Button @click="processBatchImport">
            <Upload class="size-4" />
            导入
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== GitHub 代理域名配置 ===== -->
    <Dialog v-model:open="proxyConfigDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[520px] border-border/50">
        <DialogHeader>
          <DialogTitle>GitHub 代理域名配置</DialogTitle>
          <DialogDescription>
            配置后将在连通性测试、Mihomo 生成、MosDNS 转换时自动使用代理域名。
          </DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-1.5">
          <Label for="proxy-domain">GitHub 代理</Label>
          <Input
            id="proxy-domain"
            v-model="proxyDomains.proxy"
            class="bg-background/50 font-mono"
            placeholder="例如 ghproxy.com 或 https://ghproxy.com"
          />
          <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
            支持的域名：github.com、raw.githubusercontent.com、gist.githubusercontent.com、api.github.com
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="proxyConfigDialogVisible = false">取消</Button>
          <Button @click="handleSaveProxyDomains">保存配置</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, computed, watch } from 'vue'
import { Motion } from 'motion-v'
import {
  ArrowUpDown,
  ChevronDown,
  Copy,
  Download,
  Eye,
  EyeOff,
  FileText,
  FolderOpen,
  Link2,
  Loader2,
  Network,
  Pencil,
  Plus,
  Settings,
  Trash2,
  Upload
} from '@lucide/vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
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
import { choose, confirm, confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { useProfileStore } from '@/stores/profile'
import api from '@/api'
import { activeProfileId } from '@/profileContext'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
interface RuleLibraryItem {
  id: string
  name: string
  url: string
  behavior: string
  enabled: boolean
  source_type?: 'url' | 'content'
  content?: string
}

const ruleLibrary = ref<RuleLibraryItem[]>([])
const dialogVisible = ref(false)
const batchImportDialogVisible = ref(false)
const addRuleToSetDialogVisible = ref(false)
const proxyConfigDialogVisible = ref(false)
const batchImportText = ref('')
const isEdit = ref(false)
const testing = ref(false)
const caching = ref(false) // 批量缓存加载状态
const testResults = ref<Record<string, boolean>>({})
const currentRuleSet = ref<RuleLibraryItem | null>(null)
const rulesContainer = ref<HTMLElement | null>(null)
const SOURCE_TYPES = [
  { value: 'url', label: 'URL 地址' },
  { value: 'content', label: '规则内容' }
]

const CLASSICAL_RULE_TYPES = [
  'DOMAIN',
  'DOMAIN-SUFFIX',
  'DOMAIN-KEYWORD',
  'IP-CIDR',
  'IP-CIDR6',
  'IP-SUFFIX',
  'DST-PORT'
]

const viewMode = ref<'list' | 'card'>('list') // 默认列表视图
const selectedRules = ref<string[]>([]) // 选中的规则ID列表

// 专业功能开关
// 处理按钮点击
const handleBatchTest = () => {
  batchTestConnectivity()
}

const handleBatchCache = () => {
  batchCacheRules()
}

const proxyDomains = ref({
  proxy: ''
})

const form = ref<Partial<RuleLibraryItem>>({
  name: '',
  url: '',
  behavior: 'classical',
  enabled: true,
  source_type: 'url',
  content: ''
})

const ruleContentPlaceholder = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return [
        '请输入域名规则，每行一条规则',
        '示例：',
        'DOMAIN-SUFFIX,example.com',
        'DOMAIN,github.com',
        'DOMAIN-KEYWORD,google'
      ].join('\n')
    case 'ipcidr':
      return [
        '请输入 IP 规则，每行一条规则',
        '示例：',
        'IP-CIDR,192.168.0.0/16',
        'IP-CIDR,10.0.0.0/8',
        'IP-CIDR6,2001:db8::/32'
      ].join('\n')
    default:
      return [
        '请输入规则内容，每行一条规则',
        '示例：',
        'RULE-SET,ads',
        'GEOSITE,category-ads-all',
        'PROCESS-NAME,Google Chrome'
      ].join('\n')
  }
})

const ruleContentHelperText = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return '支持 DOMAIN、DOMAIN-SUFFIX、DOMAIN-KEYWORD 等域名规则，每行一条。'
    case 'ipcidr':
      return '支持 IPv4/IPv6 CIDR 写法，例如 192.168.0.0/16 或 2001:db8::/32。'
    default:
      return '可混合使用 DOMAIN、GEOIP、RULE-SET 等规则前缀，请确保格式符合 Clash/Mihomo 要求。'
  }
})

const addRuleToSetForm = ref({
  rule_type: 'DOMAIN-SUFFIX',
  value: ''
})

const addRuleToSetHelperText = computed(() => {
  switch (currentRuleSet.value?.behavior) {
    case 'domain':
      return '每行一个域名，例如 example.com 或 baidu.com。'
    case 'ipcidr':
      return '每行一个 CIDR，例如 1.1.1.0/24 或 2001:db8::/32。'
    default:
      return '每行一个值，例如 example.com、baidu.com 或 192.168.1.0/24。'
  }
})

const loadRuleLibrary = async () => {
  try {
    const { data } = await api.get('/rule-library')
    ruleLibrary.value = data
    nextTick(() => {
    })
  } catch (error) {
    notify.error('加载规则仓库失败')
  }
}

const showAddDialog = () => {
  isEdit.value = false
  form.value = {
    id: `lib_${Date.now()}`,
    name: '',
    url: '',
    behavior: 'classical',
    enabled: true,
    source_type: 'url',
    content: ''
  }
  dialogVisible.value = true
}

const editRule = (row: RuleLibraryItem) => {
  isEdit.value = true
  form.value = {
    ...row,
    source_type: row.source_type || 'url',
    content: row.content || ''
  }
  dialogVisible.value = true
}

const isFullUrl = (url: string): boolean => {
  if (!url) return false
  return url.startsWith('http://') || url.startsWith('https://')
}

const testSingleRule = async (url: string): Promise<boolean> => {
  try {
    if (!isFullUrl(url)) {
      return true
    }

    const { data } = await api.post('/rule-library/test-single', { url })
    if (data.success) {
      return data.available
    }
    return false
  } catch (error) {
    return false
  }
}

const handleFormStatusChange = async (enabled: boolean) => {
  if (!enabled) {
    return
  }

  if (form.value.source_type === 'url' && form.value.url && isFullUrl(form.value.url)) {
    const loadingToast = notify.loading('正在测试规则连通性…')
    const isAvailable = await testSingleRule(form.value.url)
    notify.dismiss(loadingToast)

    if (!isAvailable) {
      notify.error('规则地址无法访问，无法开启')
      form.value.enabled = false
      return
    }
  }
}

const saveRule = async () => {
  if (!form.value.name) {
    notify.warning('请输入规则名称')
    return
  }

  if (form.value.source_type === 'url') {
    if (!form.value.url) {
      notify.warning('请输入规则地址')
      return
    }
  } else if (form.value.source_type === 'content') {
    if (!form.value.content || !form.value.content.trim()) {
      notify.warning('请输入规则内容')
      return
    }
  }

  let isAvailable = true

  if (form.value.source_type === 'url' && form.value.url) {
    const loadingToast = notify.loading('正在测试规则连通性…')
    isAvailable = await testSingleRule(form.value.url)
    notify.dismiss(loadingToast)

    if (!isAvailable) {
      const ok = await confirm('该规则地址无法访问，规则将被添加但状态为关闭。是否继续？', {
        title: '连通性测试失败',
        confirmText: '继续添加'
      })
      if (!ok) return
      form.value.enabled = false
    }
  }

  try {
    if (isEdit.value) {
      const { data } = await api.put(`/rule-library/${form.value.id}`, form.value)

      // 显示同步信息
      if (data.synced_count > 0) {
        notify.success(`更新成功，并同步${form.value.enabled ? '启用' : '禁用'}了 ${data.synced_count} 个关联的规则配置`)
      } else {
        notify.success('更新成功')
      }
    } else {
      await api.post('/rule-library', form.value)
      notify.success(isAvailable ? '添加成功' : '添加成功（规则已关闭）')
    }
    dialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    notify.error('保存失败')
  }
}

const disableRelatedRuleConfigs = async (libraryRuleId: string) => {
  try {
    const { data: allRules } = await api.get('/rules')

    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && item.library_rule_id === libraryRuleId
    )

    for (const ruleSet of relatedRuleSets) {
      if (ruleSet.enabled) {
        ruleSet.enabled = false
        await api.put(`/rule-sets/${ruleSet.id}`, ruleSet)
      }
    }

    if (relatedRuleSets.length > 0) {
      notify.info(`已同时关闭 ${relatedRuleSets.length} 个关联的规则配置`)
    }
  } catch (error) {
    console.error('关闭关联规则配置失败:', error)
  }
}

const deleteRule = async (row: RuleLibraryItem) => {
  try {
    const { data: allRules } = await api.get('/rules')
    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && item.library_rule_id === row.id
    )

    if (relatedRuleSets.length > 0) {
      const action = await choose(
        `该规则被 ${relatedRuleSets.length} 个规则配置引用，是否一起删除这些规则配置？`,
        {
          title: '删除规则',
          confirmText: '一起删除',
          altText: '仅删除规则仓库',
          cancelText: '取消',
          danger: true
        }
      )
      if (action === 'cancel') return

      if (action === 'confirm') {
        const deletedRuleSetIds: string[] = []
        for (const ruleSet of relatedRuleSets) {
          try {
            await api.delete(`/rule-sets/${ruleSet.id}`)
            deletedRuleSetIds.push(ruleSet.id)
          } catch (error) {
            console.error(`删除规则配置 ${ruleSet.name} 失败:`, error)
          }
        }

        if (deletedRuleSetIds.length > 0) {
          try {
            const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

            const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter(
              (id: string) => !deletedRuleSetIds.includes(id)
            )
            const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter(
              (id: string) => !deletedRuleSetIds.includes(id)
            )

            if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
                updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
              await api.post('/mosdns/rulesets', {
                direct_rulesets: updatedDirectRulesets,
                proxy_rulesets: updatedProxyRulesets,
                direct_rules: mosdnsConfig.direct_rules,
                proxy_rules: mosdnsConfig.proxy_rules
              })
              console.log('已同步更新 MosDNS 配置，移除了对已删除规则集的引用')
            }
          } catch (error) {
            console.error('同步更新 MosDNS 配置失败:', error)
            notify.warning('规则配置已删除，但 MosDNS 配置同步失败，请手动检查')
          }
        }

        await api.delete(`/rule-library/${row.id}`)
        notify.success(`已删除规则仓库及 ${relatedRuleSets.length} 个关联的规则配置`)
      } else {
        await api.delete(`/rule-library/${row.id}`)
        notify.success('已删除规则仓库，关联的规则配置保留')
      }
      loadRuleLibrary()
    } else {
      const ok = await confirmDanger('确定要删除该规则吗？', { title: '删除规则' })
      if (!ok) return

      await api.delete(`/rule-library/${row.id}`)
      notify.success('删除成功')
      loadRuleLibrary()
    }
  } catch (error) {
    notify.error('删除失败')
    console.error('删除规则失败:', error)
  }
}

const handleToggle = async (rule: RuleLibraryItem) => {
  rule.enabled = !rule.enabled
  await toggleEnabled(rule)
}

const toggleEnabled = async (row: RuleLibraryItem) => {
  if (row.enabled && row.source_type === 'url' && isFullUrl(row.url)) {
    const loadingToast = notify.loading('正在测试规则连通性…')
    const isAvailable = await testSingleRule(row.url)
    notify.dismiss(loadingToast)

    if (!isAvailable) {
      notify.error('规则地址无法访问，无法开启')
      row.enabled = false
      return
    }
  }

  try {
    const { data } = await api.put(`/rule-library/${row.id}`, row)

    // 显示同步信息
    if (data.synced_count > 0) {
      notify.success(`${row.enabled ? '已开启' : '已关闭'}，并同步${row.enabled ? '启用' : '禁用'}了 ${data.synced_count} 个关联的规则配置`)
    } else {
      notify.success(row.enabled ? '已开启' : '已关闭')
    }
  } catch (error) {
    notify.error('更新失败')
    row.enabled = !row.enabled
  }
}

const showAddRuleToSetDialog = (row: RuleLibraryItem) => {
  currentRuleSet.value = row
  addRuleToSetForm.value = {
    rule_type: row.behavior === 'ipcidr' ? 'IP-CIDR' : 'DOMAIN-SUFFIX',
    value: ''
  }
  addRuleToSetDialogVisible.value = true
}

const formatRuleValueForRuleSet = (value: string) => {
  const trimmedValue = value.trim()

  if (currentRuleSet.value?.behavior === 'classical') {
    return `${addRuleToSetForm.value.rule_type},${trimmedValue}`
  }

  return trimmedValue
}

const isValidIpv4Cidr = (value: string) => {
  const match = value.match(/^(\d{1,3})(?:\.(\d{1,3})){3}\/(\d{1,2})$/)
  if (!match) return false

  const [address, prefix] = value.split('/')
  const octets = address.split('.').map(Number)
  const prefixNumber = Number(prefix)

  return octets.every(octet => octet >= 0 && octet <= 255) && prefixNumber >= 0 && prefixNumber <= 32
}

const isValidIpv6Cidr = (value: string) => {
  const match = value.match(/^([0-9a-fA-F:]+)\/(\d{1,3})$/)
  if (!match || !match[1].includes(':')) return false

  const prefixNumber = Number(match[2])
  return prefixNumber >= 0 && prefixNumber <= 128
}

const isValidCidr = (value: string) => isValidIpv4Cidr(value) || isValidIpv6Cidr(value)

const isLikelyDomainValue = (value: string) => {
  if (value.includes(',') || value.includes('/')) return false
  return /^[a-zA-Z0-9+*_.-]+$/.test(value)
}

const getInvalidRuleValue = (values: string[]) => {
  switch (currentRuleSet.value?.behavior) {
    case 'domain':
      return values.find(value => !isLikelyDomainValue(value.trim()))
    case 'ipcidr':
      return values.find(value => !isValidCidr(value.trim()))
    default:
      return undefined
  }
}

const saveRuleToSet = async () => {
  if (!currentRuleSet.value) {
    return
  }

  if (!addRuleToSetForm.value.value || !addRuleToSetForm.value.value.trim()) {
    notify.warning('请输入规则值')
    return
  }

  const values = addRuleToSetForm.value.value.trim().split('\n').filter(line => line.trim())
  const invalidValue = getInvalidRuleValue(values)
  if (invalidValue) {
    const behavior = currentRuleSet.value.behavior
    const expectedText = behavior === 'ipcidr' ? 'CIDR，例如 1.1.1.0/24' : '域名，例如 example.com'
    notify.warning(`"${invalidValue.trim()}" 不符合 ${currentRuleSet.value.name} 的规则集类型，请输入${expectedText}`)
    return
  }

  const newRules = values.map(formatRuleValueForRuleSet).join('\n')

  const existingContent = currentRuleSet.value.content || ''
  const updatedContent = existingContent ? `${existingContent}\n${newRules}` : newRules

  try {
    const updatedRule = {
      ...currentRuleSet.value,
      content: updatedContent
    }

    await api.put(`/rule-library/${currentRuleSet.value.id}`, updatedRule)
    notify.success('添加成功')
    addRuleToSetDialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    notify.error('保存失败')
  }
}

const showBatchImportDialog = () => {
  batchImportText.value = ''
  batchImportDialogVisible.value = true
}

const processBatchImport = async () => {
  const text = batchImportText.value.trim()
  if (!text) {
    notify.warning('请粘贴配置内容')
    return
  }

  try {
    const rules = parseRuleProviders(text)
    if (rules.length === 0) {
      notify.warning('未解析到有效的规则配置')
      return
    }

    let successCount = 0
    for (const rule of rules) {
      try {
        await api.post('/rule-library', rule)
        successCount++
      } catch (error) {
        console.error(`导入规则 ${rule.name} 失败:`, error)
      }
    }

    notify.success(`成功导入 ${successCount} 条规则`)
    batchImportDialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    notify.error('解析配置失败，请检查格式')
  }
}

const parseRuleProviders = (text: string): RuleLibraryItem[] => {
  const rules: RuleLibraryItem[] = []

  text = text.replace(/^rule-providers:\s*$/m, '')

  const lines = text.split('\n')

  let counter = 0
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue

    const match = trimmed.match(/^([a-zA-Z0-9_-]+):\s*\{(.+)\}/)
    if (!match) continue

    const name = match[1]
    const content = match[2]

    const urlMatch = content.match(/url:\s*["']([^"']+)["']/)
    if (!urlMatch) continue
    const url = urlMatch[1]

    const behaviorMatch = content.match(/behavior:\s*([a-z]+)/)
    const behavior = behaviorMatch ? behaviorMatch[1] : 'classical'

    rules.push({
      id: `lib_${Date.now()}_${counter++}`,
      name,
      url,
      behavior,
      enabled: true
    })
  }

  return rules
}



const getContentPreview = (content?: string) => {
  if (!content) return '无内容'

  const lines = content.split('\n').filter(line => line.trim())
  const totalLines = lines.length

  const previewLines = lines.slice(0, 3)
  const preview = previewLines.join('\n')

  if (totalLines > 3) {
    return `${preview}\n... (共 ${totalLines} 条规则)`
  }

  return preview
}

const batchTestConnectivity = async () => {
  const ok = await confirm('即将测试所有规则地址的连通性，不可用的规则将被自动关闭。是否继续？', {
    title: '批量测试连通性'
  })
  if (!ok) return

  try {
    testing.value = true
    testResults.value = {}

    const { data } = await api.post('/rule-library/test')

    if (data.success) {
      data.results.forEach((result: any) => {
        testResults.value[result.id] = result.available
      })

      const failedCount = data.failed_count
      const totalCount = data.total_count
      const successCount = totalCount - failedCount

      const failedResults = data.results.filter((result: any) => !result.available)
      for (const failedRule of failedResults) {
        await disableRelatedRuleConfigs(failedRule.id)
      }

      if (failedCount > 0) {
        notify.warning(`测试完成！成功: ${successCount}，失败: ${failedCount}。不可用的规则和关联的规则配置已自动关闭。`)
      } else {
        notify.success(`测试完成！所有 ${totalCount} 条规则均可用。`)
      }

      await loadRuleLibrary()
    } else {
      notify.error('测试失败：' + data.message)
    }
  } catch (error) {
    notify.error('测试失败')
    console.error('测试连通性失败:', error)
  } finally {
    testing.value = false
  }
}

const batchCacheRules = async () => {
  const ok = await confirm(
    `即将缓存选中的 ${selectedRules.value.length} 条规则到本地，缓存失败的规则将被自动关闭。是否继续？`,
    { title: '批量缓存规则' }
  )
  if (!ok) return

  try {
    caching.value = true

    const { data } = await api.post('/rule-library/cache', {
      rule_ids: selectedRules.value
    })

    if (data.success) {
      const successCount = data.success_count
      const failedCount = data.failed_count
      const totalCount = data.total_count

      // 清空选择
      selectedRules.value = []

      if (failedCount > 0) {
        notify.warning(`缓存完成！成功: ${successCount}，失败: ${failedCount}。缓存失败的规则和关联的规则配置已自动关闭。`)
      } else {
        notify.success(`缓存完成！成功缓存 ${totalCount} 条规则。`)
      }

      await loadRuleLibrary()
    } else {
      notify.error('缓存失败：' + data.message)
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      notify.error('缓存失败')
      console.error('批量缓存失败:', error)
    }
  } finally {
    caching.value = false
  }
}

const loadProxyDomains = async () => {
  try {
    const { data } = await api.get('/rule-library/proxy-domains')
    proxyDomains.value = {
      proxy: data.proxy_domains || ''
    }
  } catch (error) {
    console.error('加载代理域名配置失败:', error)
  }
}

const showProxyConfigDialog = () => {
  proxyConfigDialogVisible.value = true
}

const handleSaveProxyDomains = async () => {
  try {
    const proxyValue = proxyDomains.value.proxy?.trim() || ''

    await api.post('/rule-library/proxy-domains', {
      proxy_domains: proxyValue
    })

    notify.success('代理域名配置已保存')
    proxyConfigDialogVisible.value = false
  } catch (error) {
    notify.error('保存代理域名配置失败')
    console.error('保存代理域名配置失败:', error)
  }
}

// 生成规则的可下载 URL
const getRuleDownloadUrl = (rule: RuleLibraryItem): string => {
  if (rule.source_type === 'content') {
    const baseUrl = `${window.location.protocol}//${window.location.host}`
    return `${baseUrl}/api/profiles/${encodeURIComponent(activeProfileId.value)}/rule-library/content/${rule.id}`
  }
  return rule.url || ''
}

// 复制 URL 到剪贴板
const copyRuleUrl = async (rule: RuleLibraryItem) => {
  const url = getRuleDownloadUrl(rule)
  if (!url) {
    notify.warning('该规则没有可用的URL')
    return
  }

  try {
    await navigator.clipboard.writeText(url)
    notify.success('URL已复制到剪贴板')
  } catch (err) {
    // 降级方案
    const input = document.createElement('input')
    input.value = url
    document.body.appendChild(input)
    input.select()
    document.execCommand('copy')
    document.body.removeChild(input)
    notify.success('URL已复制到剪贴板')
  }
}

// 选择相关的计算属性
const allSelected = computed(() => {
  return ruleLibrary.value.length > 0 && selectedRules.value.length === ruleLibrary.value.length
})

const someSelected = computed(() => {
  return selectedRules.value.length > 0 && selectedRules.value.length < ruleLibrary.value.length
})

// 切换单个规则的选择状态
const toggleRuleSelection = (ruleId: string) => {
  const index = selectedRules.value.indexOf(ruleId)
  if (index > -1) {
    selectedRules.value.splice(index, 1)
  } else {
    selectedRules.value.push(ruleId)
  }
}

// 切换全选/取消全选
const toggleSelectAll = () => {
  if (allSelected.value) {
    selectedRules.value = []
  } else {
    selectedRules.value = ruleLibrary.value.map(rule => rule.id)
  }
}

// 批量删除规则
const batchDeleteRules = async () => {
  if (selectedRules.value.length === 0) return

  try {
    // 获取所有规则配置，检查关联
    const { data: allRules } = await api.get('/rules')
    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && selectedRules.value.includes(item.library_rule_id)
    )

    let confirmMessage = `确定要删除选中的 ${selectedRules.value.length} 条规则吗？`
    if (relatedRuleSets.length > 0) {
      confirmMessage = `选中的规则被 ${relatedRuleSets.length} 个规则配置引用，是否一起删除这些规则配置？`
    }

    const ok = await confirmDanger(confirmMessage, {
      title: '批量删除规则',
      confirmText: relatedRuleSets.length > 0 ? '一起删除' : '删除'
    })
    if (!ok) return

    // 删除关联的规则配置
    const deletedRuleSetIds: string[] = []
    if (relatedRuleSets.length > 0) {
      for (const ruleSet of relatedRuleSets) {
        try {
          await api.delete(`/rule-sets/${ruleSet.id}`)
          deletedRuleSetIds.push(ruleSet.id)
        } catch (error) {
          console.error(`删除规则配置 ${ruleSet.name} 失败:`, error)
        }
      }

      // 同步更新 MosDNS 配置
      if (deletedRuleSetIds.length > 0) {
        try {
          const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')
          const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter(
            (id: string) => !deletedRuleSetIds.includes(id)
          )
          const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter(
            (id: string) => !deletedRuleSetIds.includes(id)
          )

          if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
              updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
            await api.post('/mosdns/rulesets', {
              direct_rulesets: updatedDirectRulesets,
              proxy_rulesets: updatedProxyRulesets,
              direct_rules: mosdnsConfig.direct_rules,
              proxy_rules: mosdnsConfig.proxy_rules
            })
          }
        } catch (error) {
          console.error('同步更新 MosDNS 配置失败:', error)
        }
      }
    }

    // 批量删除规则仓库
    let successCount = 0
    for (const ruleId of selectedRules.value) {
      try {
        await api.delete(`/rule-library/${ruleId}`)
        successCount++
      } catch (error) {
        console.error(`删除规则 ${ruleId} 失败:`, error)
      }
    }

    // 清空选择
    selectedRules.value = []

    if (relatedRuleSets.length > 0) {
      notify.success(`已删除 ${successCount} 条规则及 ${deletedRuleSetIds.length} 个关联的规则配置`)
    } else {
      notify.success(`已删除 ${successCount} 条规则`)
    }

    loadRuleLibrary()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      notify.error('批量删除失败')
      console.error('批量删除失败:', error)
    }
  }
}

// 监听视图模式切换，重新初始化拖拽
watch(viewMode, () => {
  nextTick(() => {
  })
})

/* ---------- 窄屏视图回退与筛选 ---------- */
// 规则集表格列多，窄屏不可读，移动端一律用卡片
const isNarrow = ref(false)
const syncNarrow = () => {
  isNarrow.value = window.matchMedia('(max-width: 900px)').matches
}
const effectiveView = computed(() => (isNarrow.value ? 'card' : viewMode.value))

const keyword = ref('')
const behaviorFilter = ref('all')

const behaviorOptions = computed(() => {
  const set = new Set<string>()
  ruleLibrary.value.forEach(r => {
    if (r.behavior) set.add(String(r.behavior))
  })
  return [...set].sort()
})

// 排序模式下必须展示完整列表，否则筛选会让保存的顺序丢条目
const visibleRules = computed(() => {
  if (reorder.active.value) return ruleLibrary.value
  const q = keyword.value.trim().toLowerCase()
  return ruleLibrary.value.filter(r => {
    if (behaviorFilter.value !== 'all' && r.behavior !== behaviorFilter.value) return false
    if (!q) return true
    return [r.name, r.url, r.content].some(v => String(v || '').toLowerCase().includes(q))
  })
})

const rulesEmptyText = computed(() =>
  ruleLibrary.value.length === 0 ? '还没有规则集' : '没有匹配的规则集'
)

/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: ruleLibrary,
  container: rulesContainer,
  labelOf: item => item.name || item.id,
  // 按 id 提交，服务端在存量数据上重排，不回写列表接口的计算字段
  persist: async items => {
    await api.post('/rule-library/reorder', {
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
  loadRuleLibrary()
  loadProxyDomains()
})

onUnmounted(() => {
  window.removeEventListener('resize', syncNarrow)
})
</script>

