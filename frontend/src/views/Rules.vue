<template>
  <div :class="reorder.active.value && 'cf-reordering'">

    <PageHeader
      title="策略规则"
    >
      <template #actions>
        <Button @click="showAddRuleDialog">
          <Plus class="size-4" />
          添加规则
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger as-child>
            <Button variant="outline" class="border-border/60 bg-background/40">
              更多
              <ChevronDown class="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem @select="showAddRuleSetDialog">
              <FolderOpen class="size-4" />
              添加规则集
            </DropdownMenuItem>
            <DropdownMenuItem @select="showDuplicateDialog">
              <Copy class="size-4" />
              查找重复
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem :disabled="allRulesAndSets.length < 2" @select="enterReorder">
              <ArrowUpDown class="size-4" />
              调整顺序
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <ViewToggle v-model="viewMode" />
      </template>
    </PageHeader>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <!-- 命中模拟：后端按真实规则与规则集内容逐条匹配，前端按顺序扫描到命中行 -->
    <SectionCard v-if="!reorder.active.value && allRules.length" :padded="false" class="mb-3.5 overflow-hidden">
      <form class="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-3.5 p-5 max-md:grid-cols-[minmax(0,1fr)] max-md:p-4" @submit.prevent="simulate()">
        <label class="min-w-0">
          <span class="mb-2 block font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">命中模拟</span>
          <Input
            v-model="simQuery"
            class="h-[46px] font-mono text-[16px]"
            spellcheck="false"
            autocomplete="off"
            placeholder="输入域名或 IP，例如 api.anthropic.com"
            aria-label="要模拟的域名或 IP"
          />
        </label>
        <Button type="submit" class="h-[46px] px-5 shadow-glow" :disabled="simRunning">
          <Loader2 v-if="simRunning" class="size-4 animate-spin" />
          <Play v-else class="size-4" />
          模拟
        </Button>
        <div class="col-span-full flex min-h-10 flex-wrap items-center gap-2 text-[13.5px] text-muted-foreground max-md:col-span-1">
          <template v-if="simRunning">
            <span class="live-dot" aria-hidden="true" />
            <span class="font-mono">逐条匹配 {{ simQuery.trim() }} …</span>
          </template>
          <template v-else-if="simResult && simResult.matched">
            <span class="chip font-mono">{{ simResult.query }}</span>
            <span aria-hidden="true">→</span>
            <span class="chip font-mono" :title="simResult.matched_line">
              #{{ String(simResult.priority).padStart(2, '0') }} {{ simResult.item_type === 'ruleset' ? simResult.rule_name : simResult.matched_line }}
            </span>
            <span aria-hidden="true">→</span>
            <span :class="cn('chip', policyChip(simResult.policy))">{{ simResult.policy }}</span>
            <template v-if="simVia">
              <span aria-hidden="true">→</span>
              <span class="chip font-mono" :title="simVia.hint">{{ simVia.label }}</span>
            </template>
            <span class="ml-auto font-mono text-[11.5px]">{{ simResult.elapsed_time }} ms</span>
          </template>
          <template v-else-if="simResult">
            <span class="chip font-mono">{{ simResult.query }}</span>
            <span>未命中任何规则，将使用默认策略</span>
          </template>
          <template v-else-if="simSamples.length">
            试试
            <button v-for="sample in simSamples" :key="sample" type="button" class="chip font-mono" @click="simulate(sample)">
              {{ sample }}
            </button>
          </template>
        </div>
      </form>
    </SectionCard>

    <SectionCard v-if="displayList.length === 0" :padded="false">
      <EmptyState
        :icon="FileText"
        title="暂无规则"
        description="添加单条规则或从规则库选择规则集，再指定处理流量的策略。规则从上到下匹配。"
      >
        <Button @click="showAddRuleDialog">
          <Plus class="size-4" />
          添加规则
        </Button>
      </EmptyState>
    </SectionCard>

    <!-- ===== 列表视图 ===== -->
    <SectionCard v-else-if="viewMode === 'list'" :padded="false">
      <div
        class="flex items-center gap-2.5 border-b border-border px-4 py-3 font-mono text-[10.5px] tracking-[0.12em] text-muted-foreground uppercase"
        aria-hidden="true"
      >
        <span v-if="reorder.active.value" class="w-11" />
        <span class="w-9">#</span>
        <span class="w-[118px] max-md:w-[92px]">类型</span>
        <span class="min-w-0 flex-1">匹配值</span>
        <span class="w-[160px] max-lg:hidden">备注</span>
        <span class="w-[120px] text-right">策略</span>
        <span class="w-[136px]" />
      </div>
      <div id="sortable-rules" ref="rulesContainer" class="flex flex-col">
        <div
          v-for="(item, cfIndex) in displayList"
          :key="item.uniqueId"
          data-reorder-item
          :data-id="item.uniqueId"
        >
          <!-- 分组行（收起状态） -->
          <button
            v-if="item.isGroup"
            type="button"
            class="flex w-full cursor-pointer items-center gap-2.5 border-0 border-b border-border/40 bg-transparent px-4 py-2.5 text-left transition-colors hover:bg-accent/40"
            @click="toggleGroup(item.groupId)"
          >
            <DragHandle
              v-if="reorder.active.value"
              :label="item.groupName || item.groupDefaultName"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <span class="w-9 shrink-0 font-mono text-[11px] text-muted-foreground">{{ groupRange(item) }}</span>
            <Layers class="size-4 shrink-0 text-warning-accent" aria-hidden="true" />
            <span class="min-w-0 truncate text-[13px] font-semibold text-foreground">
              {{ item.groupName || item.groupDefaultName }}
            </span>
            <Badge variant="warning" class="shrink-0 text-[10.5px]">{{ item.groupLabel }}</Badge>
            <Badge v-if="item.groupName" variant="outline" class="num shrink-0 text-[10.5px]">
              {{ item.count }} 个
            </Badge>
            <span class="ml-auto flex w-[120px] shrink-0 justify-end">
              <span :class="cn('chip max-w-full truncate', policyChip(item.policy))">{{ item.policy }}</span>
            </span>
            <span class="flex w-[136px] shrink-0 items-center justify-end gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                class="cf-reorder-mute shrink-0"
                title="重命名"
                aria-label="重命名分组"
                @click.stop="showGroupRenameDialog(item)"
              >
                <Pencil class="size-4" />
              </Button>
              <span class="grid size-8 place-items-center" aria-hidden="true">
                <ChevronDown class="size-4 text-muted-foreground" />
              </span>
            </span>
          </button>

          <!-- 普通行 -->
          <div
            v-else
            :data-rule-key="item.uniqueId"
            :class="[
              'rule-row flex items-center gap-2.5 border-0 border-b border-border/40 px-4 py-[11px] transition-colors hover:bg-accent/30',
              item.isExpandedGroupItem && 'bg-background/40',
              !item.enabled && 'opacity-55',
              simScanKey === item.uniqueId && 'is-scan',
              simMatchKey === item.uniqueId && 'is-match'
            ]"
          >
            <DragHandle
              v-if="reorder.active.value"
              :label="item.name || item.id"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <span class="rule-index w-9 shrink-0 font-mono text-[11px] text-muted-foreground">
              {{ String(rawIndex(item.uniqueId)).padStart(2, '0') }}
            </span>
            <span class="w-[118px] shrink-0 truncate font-mono text-[11px] text-muted-foreground max-md:w-[92px]">
              {{ item.itemType === 'rule' ? item.rule_type : 'RULE-SET' }}
            </span>

            <div class="flex min-w-0 flex-1 items-center gap-2">
              <template v-if="item.itemType === 'rule'">
                <span class="min-w-0 truncate font-mono text-[12.5px] text-foreground" :title="item.value">
                  {{ item.value }}
                </span>
              </template>
              <template v-else>
                <span
                  class="min-w-0 truncate text-[13px] font-medium text-foreground"
                  :title="`类型: ${item.behavior}\nURL: ${item.url}`"
                >
                  {{ item.name }}
                </span>
                <FolderOpen
                  v-if="item.library_rule_id"
                  class="size-3.5 shrink-0 text-warning-accent"
                  title="来自规则仓库"
                  aria-label="来自规则仓库"
                />
              </template>
            </div>
            <span class="w-[160px] shrink-0 truncate text-[12px] text-muted-foreground max-lg:hidden" :title="item.remark">
              {{ item.remark || '—' }}
            </span>

            <span class="flex w-[120px] shrink-0 justify-end">
              <span :class="cn('chip max-w-full truncate', policyChip(item.policy))">{{ item.policy }}</span>
            </span>

            <div class="cf-reorder-mute flex w-[136px] shrink-0 items-center justify-end gap-0.5">
              <Button
                v-if="item.isExpandedGroupItem && item.isFirstInGroup"
                variant="ghost"
                size="icon-sm"
                title="收起分组"
                aria-label="收起分组"
                @click.stop="toggleGroup(item.groupId)"
              >
                <ChevronUp class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :class="item.enabled ? 'text-success-accent' : 'text-muted-foreground'"
                :title="item.enabled ? '停用' : '启用'"
                :aria-label="item.enabled ? '停用该项' : '启用该项'"
                @click="toggleItemStatus(item)"
              >
                <component :is="item.enabled ? Eye : EyeOff" class="size-4" />
              </Button>
              <Button variant="ghost" size="icon-sm" title="编辑" aria-label="编辑" @click="editItem(item)">
                <Pencil class="size-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                class="text-destructive-accent hover:bg-destructive-soft"
                title="从当前配置删除，删除后无法恢复"
                aria-label="从当前配置删除，删除后无法恢复"
                @click="deleteItem(item)"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
          </div>
        </div>
      </div>

      <Separator />
      <p class="m-0 px-4 py-2.5 text-xs text-muted-foreground">共 {{ displayList.length }} 项</p>
    </SectionCard>

    <!-- ===== 卡片视图 ===== -->
    <div
      v-else
      id="sortable-rules"
      ref="rulesContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3 max-md:grid-cols-1"
    >
      <Motion
        v-for="(item, cfIndex) in displayList"
        :key="item.uniqueId"
        v-bind="listItem(cfIndex)"
        data-reorder-item
        :data-id="item.uniqueId"
        :class="[
          'relative flex flex-col gap-3 overflow-hidden rounded-[18px] border bg-card/90 shadow-surface p-4 transition-all duration-300 hover:border-border-strong',
          item.isGroup ? 'border-warning-accent/35' : 'border-border/35',
          item.isExpandedGroupItem && 'border-primary-accent/30',
          !item.isGroup && !item.enabled && 'dark:opacity-60'
        ]"
      >
        <!-- 分组卡片 -->
        <template v-if="item.isGroup">
          <header class="flex items-start gap-2.5">
            <DragHandle
              v-if="reorder.active.value"
              :label="item.groupName || item.groupDefaultName"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <Layers class="mt-0.5 size-4 shrink-0 text-warning-accent" aria-hidden="true" />
            <p class="m-0 min-w-0 flex-1 truncate text-[14px] font-semibold text-foreground">
              {{ item.groupName || item.groupDefaultName }}
            </p>
          </header>

          <div class="flex flex-wrap gap-1.5">
            <Badge variant="warning" class="text-[10.5px]">{{ item.groupLabel }}</Badge>
            <Badge v-if="item.groupName" variant="outline" class="num text-[10.5px]">{{ item.count }} 个</Badge>
            <Badge variant="secondary" class="min-w-0 max-w-full text-[10.5px]" :title="item.policy"><span class="truncate">{{ item.policy }}</span></Badge>
          </div>

          <footer class="mt-auto flex items-center gap-1 border-0 border-t border-border/50 pt-3">
            <Button variant="ghost" size="sm" @click.stop="showGroupRenameDialog(item)">
              <Pencil class="size-3.5" />
              重命名
            </Button>
            <Button variant="ghost" size="sm" class="ml-auto" @click.stop="toggleGroup(item.groupId)">
              查看全部
              <ChevronDown class="size-3.5" />
            </Button>
          </footer>
        </template>

        <!-- 普通卡片 -->
        <template v-else>
          <header class="flex items-start gap-2.5">
            <DragHandle
              v-if="reorder.active.value"
              :label="item.name || item.id"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <Badge :variant="item.itemType === 'rule' ? 'brand' : 'info'" class="shrink-0 text-[10.5px]">
              {{ item.itemType === 'rule' ? '规则' : '规则集' }}
            </Badge>
            <div class="cf-reorder-mute ml-auto flex shrink-0 items-center gap-0.5">
              <Button
                v-if="item.isExpandedGroupItem && item.isFirstInGroup"
                variant="ghost"
                size="sm"
                @click.stop="toggleGroup(item.groupId)"
              >
                <ChevronUp class="size-3.5" />
                收起
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                :class="item.enabled ? 'text-success-accent' : 'text-muted-foreground'"
                :title="item.enabled ? '停用' : '启用'"
                :aria-label="item.enabled ? '停用该项' : '启用该项'"
                @click="toggleItemStatus(item)"
              >
                <component :is="item.enabled ? Eye : EyeOff" class="size-4" />
              </Button>
            </div>
          </header>

          <div class="cf-reorder-mute min-w-0">
            <template v-if="item.itemType === 'rule'">
              <p class="m-0 font-mono text-[11px] tracking-[0.04em] text-info-accent uppercase">
                {{ item.rule_type }}
              </p>
              <p class="mt-1 mb-0 font-mono text-[13px] break-all text-foreground" :title="item.value">
                {{ item.value }}
              </p>
            </template>
            <template v-else>
              <p
                class="m-0 flex items-center gap-1.5 text-[13px] font-medium break-all text-foreground"
                :title="`类型: ${item.behavior}\nURL: ${item.url}`"
              >
                {{ item.name }}
                <FolderOpen
                  v-if="item.library_rule_id"
                  class="size-3.5 shrink-0 text-warning-accent"
                  aria-label="来自规则仓库"
                />
              </p>
            </template>
            <p v-if="item.remark" class="mt-1.5 mb-0 break-all text-[11.5px] text-muted-foreground" :title="item.remark">
              {{ item.remark }}
            </p>
          </div>

          <footer class="cf-reorder-mute mt-auto flex items-center gap-1 border-0 border-t border-border/50 pt-3">
            <Badge variant="secondary" class="min-w-0 max-w-[150px] text-[10.5px]" :title="item.policy"><span class="truncate">{{ item.policy }}</span></Badge>
            <Button variant="ghost" size="icon-sm" class="ml-auto" title="编辑" aria-label="编辑" @click="editItem(item)">
              <Pencil class="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              class="text-destructive-accent hover:bg-destructive-soft"
              title="从当前配置删除，删除后无法恢复"
              aria-label="从当前配置删除，删除后无法恢复"
              @click="deleteItem(item)"
            >
              <Trash2 class="size-4" />
            </Button>
          </footer>
        </template>
      </Motion>
    </div>

    <!-- ===== 添加 / 编辑规则 ===== -->
    <Dialog v-model:open="ruleDialogVisible">
      <DialogContent class="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{{ isEditRule ? '编辑规则' : '添加规则' }}</DialogTitle>
          <DialogDescription>规则按列表顺序自上而下匹配，命中即停止。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[60dvh] flex-col gap-4 overflow-y-auto">
          <div class="flex flex-col gap-1.5">
            <Label>规则类型</Label>
            <Select v-model="ruleForm.rule_type">
              <SelectTrigger class="w-full bg-background/50 font-mono">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="type in RULE_TYPES" :key="type" :value="type" class="font-mono">
                  {{ type }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="rule-value">值</Label>
            <Input
              id="rule-value"
              v-model="ruleForm.value"
              class="bg-background/50 font-mono"
              placeholder="域名、IP 或规则集名称"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>策略</Label>
            <Select v-model="ruleForm.policy">
              <SelectTrigger class="w-full min-w-0 bg-background/50 [&>span]:truncate">
                <SelectValue placeholder="选择策略" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="policy in availablePolicies" :key="policy" :value="policy" :title="policy">
                  <span class="break-all">{{ policy }}</span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="rule-remark">备注</Label>
            <Input
              id="rule-remark"
              v-model="ruleForm.remark"
              class="bg-background/50"
              placeholder="可选，添加备注说明"
            />
          </div>

          <div v-if="isIpRuleType" class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2.5">
              <Switch id="rule-noresolve" v-model="ruleForm.no_resolve" />
              <Label for="rule-noresolve" class="text-[13px] text-muted-foreground">no-resolve</Label>
            </div>
            <p class="m-0 text-[12px] text-muted-foreground">
              开启后，匹配 IP 条件时不为此解析域名。IP 类规则建议开启；逻辑规则中仅对 IP 子条件生效。
            </p>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch id="rule-enabled" v-model="ruleForm.enabled" />
            <Label for="rule-enabled" class="text-[13px] text-muted-foreground">
              {{ ruleForm.enabled ? '规则启用中' : '规则已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="ruleDialogVisible = false">取消</Button>
          <Button @click="saveRule">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 添加 / 编辑规则集 ===== -->
    <Dialog v-model:open="ruleSetDialogVisible">
      <DialogContent class="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{{ isEditRuleSet ? '编辑规则集' : '添加规则集' }}</DialogTitle>
          <DialogDescription>从规则库选择规则集，并设置当前配置使用的策略和启用状态。不会修改规则集内容或其他配置。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[60dvh] flex-col gap-4 overflow-y-auto">
          <div class="flex flex-col gap-1.5">
            <Label>选择规则</Label>
            <Select v-model="selectedLibraryRule" @update:model-value="value => onLibraryRuleSelect(String(value))">
              <SelectTrigger class="w-full min-w-0 bg-background/50 [&>span]:truncate">
                <SelectValue placeholder="选择共享规则集（必选）">
                  {{ selectedLibraryRuleLabel || '选择共享规则集（必选）' }}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="rule in enabledLibraryRules" :key="rule.id" :value="rule.id">
                  <span class="flex w-full items-center gap-2">
                    <span class="min-w-0 truncate">{{ rule.name }}</span>
                    <span class="ml-auto font-mono text-[10.5px] text-muted-foreground">
                      {{ rule.behavior }}
                    </span>
                  </span>
                </SelectItem>
              </SelectContent>
            </Select>
            <div class="flex items-center gap-2">
              <p class="m-0 flex-1 text-[12px] text-muted-foreground">当前配置已添加的规则集不会重复显示。</p>
              <Button v-if="selectedLibraryRule" variant="ghost" size="sm" @click="onLibraryRuleClear">
                清除选择
              </Button>
            </div>
          </div>

          <div v-if="selectedLibrarySource" class="min-w-0 rounded-lg border border-border/50 bg-background/50 p-3 text-xs text-muted-foreground">
            <p class="m-0 break-all font-medium text-foreground">{{ selectedLibrarySource.name }}</p>
            <p class="mt-1 mb-0">{{ selectedLibrarySource.behavior }} · {{ selectedLibrarySource.source_type === 'content' ? '共享规则内容' : '远程 URL' }}</p>
            <p class="mt-1 mb-0 max-h-24 overflow-auto whitespace-pre-wrap break-all font-mono">{{ selectedLibrarySource.source_type === 'content' ? selectedLibrarySource.content : selectedLibrarySource.url }}</p>
            <p v-if="selectedLibrarySource.enabled === false" class="mt-2 mb-0 text-warning-accent">共享来源已停用；当前配置的启用状态会保留，来源重新启用后生效。</p>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>策略</Label>
            <Select v-model="ruleSetForm.policy">
              <SelectTrigger class="w-full min-w-0 bg-background/50 [&>span]:truncate">
                <SelectValue placeholder="选择策略" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="policy in availablePolicies" :key="policy" :value="policy" :title="policy">
                  <span class="break-all">{{ policy }}</span>
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="ruleset-remark">备注</Label>
            <Input
              id="ruleset-remark"
              v-model="ruleSetForm.remark"
              class="bg-background/50"
              placeholder="可选，添加备注说明"
            />
          </div>

          <div v-if="selectedLibrarySource?.behavior === 'ipcidr'" class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2.5">
              <Switch id="ruleset-noresolve" v-model="ruleSetForm.no_resolve" />
              <Label for="ruleset-noresolve" class="text-[13px] text-muted-foreground">no-resolve</Label>
            </div>
            <p class="m-0 text-[12px] text-muted-foreground">开启后，匹配 IP 条件时不为此解析域名。IP CIDR 类规则集建议开启。</p>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch
              id="ruleset-enabled"
              v-model="ruleSetForm.enabled"
            />
            <Label for="ruleset-enabled" class="text-[13px] text-muted-foreground">
              {{ ruleSetForm.enabled ? '规则集启用中' : '规则集已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="ruleSetDialogVisible = false">取消</Button>
          <Button :disabled="!selectedLibraryRule" @click="saveRuleSet">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 规则组重命名 ===== -->
    <Dialog v-model:open="groupRenameDialogVisible">
      <DialogContent class="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{{ groupRenameDialogTitle }}</DialogTitle>
          <DialogDescription>留空将显示默认名称（按数量）。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-1.5">
          <Label for="group-rename">组名称</Label>
          <Input
            id="group-rename"
            v-model="groupRenameForm.groupName"
            class="bg-background/50"
            placeholder="输入名称，留空显示数量"
            @keyup.enter="saveGroupName"
          />
        </div>

        <DialogFooter>
          <Button variant="outline" @click="groupRenameDialogVisible = false">取消</Button>
          <Button @click="saveGroupName">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 查找重复规则 ===== -->
    <Dialog v-model:open="duplicateDialogVisible">
      <DialogContent class="max-w-[720px]">
        <DialogHeader>
          <DialogTitle>查找重复规则</DialogTitle>
          <DialogDescription>
            检查当前配置中已启用的单条规则和规则集，找出完全相同的条目。首次检查需要下载规则集内容，可能较慢。
          </DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="duplicateLoading" :rows="5" />

        <Alert v-else-if="duplicateError" variant="destructive">
          <AlertDescription>{{ duplicateError }}</AlertDescription>
        </Alert>

        <div v-else-if="duplicateResult" ref="duplicateResultsContainer" class="flex max-h-[58dvh] flex-col gap-3 overflow-y-auto pr-1">
          <div class="flex flex-wrap items-center gap-2 text-[12.5px] text-muted-foreground">
            <span class="num">
              已检查 {{ duplicateResult.stats.rules_checked }} 条规则、{{ duplicateResult.stats.rulesets_checked }} 个规则集
            </span>
            <Badge v-if="duplicateResult.duplicates.length" variant="warning" class="num">
              {{ duplicateResult.duplicates.length }} 组重复
            </Badge>
            <Badge v-else variant="success">无重复</Badge>
            <Badge v-if="duplicateResult.elapsed_time !== undefined" variant="outline" class="num">
              {{ duplicateResult.elapsed_time }} ms
            </Badge>
          </div>
          <p class="m-0 text-[12px] text-muted-foreground">结果为上次扫描的快照，修改规则后请重新扫描。</p>

          <Alert v-if="duplicateResult.stats.failed_rulesets.length" variant="default" class="border-warning-accent/35 bg-warning-soft/40">
            <AlertDescription class="text-[12.5px]">
              以下规则集内容获取失败，未参与查重：{{ duplicateResult.stats.failed_rulesets.join('、') }}
            </AlertDescription>
          </Alert>

          <EmptyState
            v-if="!duplicateResult.duplicates.length"
            :icon="CircleCheck"
            title="未发现重复规则"
            description="本次成功检查的规则和规则集中，没有完全相同的条目。"
          />

          <article
            v-for="(group, groupIndex) in duplicateGroupsPage"
            :key="`${group.rule_type},${group.value}`"
            :aria-label="`重复规则：${group.rule_type},${group.value}`"
            class="rounded-xl border border-border/50 bg-background/40 p-3"
          >
            <div class="mb-2 flex flex-wrap items-center gap-2">
              <code class="rounded-md border border-border/50 bg-background/60 px-2 py-0.5 font-mono text-[11.5px] break-all">
                {{ group.rule_type }},{{ group.value }}
              </code>
              <Badge variant="warning" class="num text-[10.5px]">{{ group.count }} 处</Badge>
              <Badge v-if="group.policy_conflict" variant="danger" class="text-[10.5px]">策略冲突</Badge>
            </div>

            <div
              v-for="(occ, i) in duplicateOccurrencesPage(group, groupIndex)"
              :key="(occurrencePage(groupIndex) - 1) * DUPLICATE_OCCURRENCE_PAGE_SIZE + i"
              class="flex items-center gap-2 border-0 border-t border-border/40 py-2 text-[12.5px] first:border-t-0"
            >
              <Badge :variant="occ.source_type === 'rule' ? 'brand' : 'info'" class="shrink-0 text-[10.5px]">
                {{ occ.source_type === 'rule' ? '直接规则' : '规则集' }}
              </Badge>
              <span class="min-w-0 flex-1 truncate text-foreground" :title="occ.line">
                {{ occ.source_type === 'rule' ? occ.line : `${occ.source} · 第 ${occ.line_no} 行` }}
              </span>
              <span class="num shrink-0 text-muted-foreground">#{{ occ.priority }}</span>
              <Badge :variant="policyTone(occ.policy)" class="shrink-0 text-[10.5px]">{{ occ.policy }}</Badge>
              <Button
                v-if="occ.source_type === 'rule'"
                variant="ghost"
                size="icon-sm"
                class="shrink-0 text-destructive-accent hover:bg-destructive-soft"
                title="删除该条直接规则"
                aria-label="删除该条直接规则"
                @click="deleteDuplicateRule(occ)"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
            <nav
              v-if="group.occurrences.length > DUPLICATE_OCCURRENCE_PAGE_SIZE"
              :aria-label="`${group.value} 出现位置分页`"
              class="mt-2 flex flex-wrap items-center gap-2 border-t border-border/40 pt-2 text-[12px] text-muted-foreground"
            >
              <span class="num mr-auto">第 {{ occurrencePage(groupIndex) }} / {{ occurrencePageCount(group) }} 页 · 共 {{ group.occurrences.length }} 处</span>
              <Button variant="ghost" size="sm" :disabled="occurrencePage(groupIndex) === 1" @click="setOccurrencePage(groupIndex, 1)">首页</Button>
              <Button variant="outline" size="sm" :disabled="occurrencePage(groupIndex) === 1" @click="setOccurrencePage(groupIndex, occurrencePage(groupIndex) - 1)">上一页</Button>
              <Button variant="outline" size="sm" :disabled="occurrencePage(groupIndex) === occurrencePageCount(group)" @click="setOccurrencePage(groupIndex, occurrencePage(groupIndex) + 1)">下一页</Button>
              <Button variant="ghost" size="sm" :disabled="occurrencePage(groupIndex) === occurrencePageCount(group)" @click="setOccurrencePage(groupIndex, occurrencePageCount(group))">末页</Button>
            </nav>
          </article>
        </div>

        <nav v-if="!duplicateLoading && duplicateResult && duplicatePageCount > 1" aria-label="重复规则分页" class="flex flex-wrap items-center gap-2 text-[12px] text-muted-foreground">
          <span class="num mr-auto">第 {{ duplicatePage }} / {{ duplicatePageCount }} 页 · 每页 {{ DUPLICATE_PAGE_SIZE }} 组</span>
          <Button variant="ghost" size="sm" :disabled="duplicatePage === 1" @click="setDuplicatePage(1)">首页</Button>
          <Button variant="outline" size="sm" :disabled="duplicatePage === 1" @click="setDuplicatePage(duplicatePage - 1)">上一页</Button>
          <Button variant="outline" size="sm" :disabled="duplicatePage === duplicatePageCount" @click="setDuplicatePage(duplicatePage + 1)">下一页</Button>
          <Button variant="ghost" size="sm" :disabled="duplicatePage === duplicatePageCount" @click="setDuplicatePage(duplicatePageCount)">末页</Button>
        </nav>

        <DialogFooter>
          <Button variant="outline" @click="duplicateDialogVisible = false">关闭</Button>
          <Button :disabled="duplicateLoading" @click="performDuplicateScan">
            <Loader2 v-if="duplicateLoading" class="size-4 animate-spin" />
            重新扫描
          </Button>
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
import { ref, shallowRef, onMounted, onUnmounted, onActivated, computed, nextTick, watch } from 'vue'
import { Motion } from 'motion-v'
import {
  ArrowUpDown,
  ChevronDown,
  ChevronUp,
  CircleCheck,
  Copy,
  Eye,
  EyeOff,
  FileText,
  FolderOpen,
  Layers,
  Loader2,
  Pencil,
  Play,
  Plus,
  Trash2,
  TriangleAlert
} from '@lucide/vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
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
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ViewToggle from '@/components/common/ViewToggle.vue'
import { notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { ruleApi, ruleSetApi, proxyGroupApi, nodeApi } from '@/api'
import { cn } from '@/lib/utils'
import type { Rule, RuleSet, ProxyGroup } from '@/types'
import api from '@/api'
import { getActiveProfileId } from '@/profileContext'
import { isAxiosError } from 'axios'

const errorMessage = (error: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(error) ? error.response?.data?.message || fallback : fallback

const profileId = getActiveProfileId()
const profileRequestConfig = { headers: { 'X-ConfigFlow-Profile': profileId } }


const RULE_TYPES = [
  'DOMAIN',
  'DOMAIN-SUFFIX',
  'DOMAIN-KEYWORD',
  'IP-CIDR',
  'IP-CIDR6',
  'IP-SUFFIX',
  'DST-PORT',
  'SRC-PORT',
  'GEOIP',
  'GEOSITE',
  'RULE-SET',
  'AND',
  'OR',
  'NOT',
  'MATCH'
]

/* DIRECT 绿、REJECT 红，其余走强调色，与生成的配置语义一致 */
const policyTone = (policy: string) =>
  policy === 'DIRECT' ? ('success' as const) : policy === 'REJECT' ? ('danger' as const) : ('brand' as const)

const allRules = ref<any[]>([])  // 包含规则和规则集的合并数组
const proxyGroups = ref<ProxyGroup[]>([])
const ruleLibrary = ref<any[]>([])  // 规则仓库
const selectedLibraryRule = ref('')  // 选中的规则仓库项ID
const selectedLibrarySource = computed(() =>
  ruleLibrary.value.find(rule => rule.id === selectedLibraryRule.value)
)
const selectedLibraryRuleLabel = computed(() => selectedLibrarySource.value?.name || '')
const ruleDialogVisible = ref(false)
const ruleSetDialogVisible = ref(false)
const isEditRule = ref(false)
const isEditRuleSet = ref(false)
const viewMode = ref<'list' | 'card'>('list') // 默认表格视图，与匹配顺序一致
const rulesContainer = ref<HTMLElement | null>(null)

/* ================================================================
 * 命中模拟
 * ================================================================ */
const simQuery = ref('')
const simRunning = ref(false)
const simResult = ref<any>(null)
const simScanKey = ref('')
const simMatchKey = ref('')
const simVia = ref<{ label: string; hint: string } | null>(null)
const latencyMap = ref<Record<string, { latency: number | null }>>({})
let latencyLoaded = false
/** 测速结果只在模拟命中策略组、需要推断出口节点时才读取 */
const ensureLatency = async () => {
  if (latencyLoaded) return
  try {
    const { data } = await nodeApi.latency()
    latencyMap.value = data?.results || {}
    latencyLoaded = true
  } catch {
    // 没有测速结果时只按策略组默认项推断
  }
}

const policyChip = (policy: string) =>
  policy === 'DIRECT' ? 'chip-ok font-mono' : policy === 'REJECT' ? 'chip-bad font-mono' : 'chip-acc'

/** 原始顺序号（与匹配优先级一致，从 1 开始） */
const rawIndexMap = computed(() => {
  const map = new Map<string, number>()
  allRules.value.forEach((r, i) => map.set(r.uniqueId || `${r.itemType}-${r.id}`, i + 1))
  return map
})
const rawIndex = (key: string) => rawIndexMap.value.get(key) ?? 0
const groupRange = (group: any) => {
  const idx = (group.items || []).map((i: any) => rawIndex(i.uniqueId)).filter(Boolean)
  if (!idx.length) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return idx.length > 1 ? `${pad(Math.min(...idx))}+` : pad(idx[0])
}

/** 示例取自用户自己的域名规则，不写死 */
const simSamples = computed(() => {
  const seen = new Set<string>()
  for (const r of allRules.value) {
    if (r.itemType !== 'rule' || !r.enabled) continue
    if (!['DOMAIN', 'DOMAIN-SUFFIX'].includes(r.rule_type)) continue
    const domain = r.rule_type === 'DOMAIN-SUFFIX' && !String(r.value).startsWith('www.') ? `www.${r.value}` : r.value
    seen.add(domain)
    if (seen.size >= 4) break
  }
  return [...seen]
})

const wait = (ms: number) => new Promise(resolve => window.setTimeout(resolve, ms))

/** 命中的是策略组时，按其来源与正则取节点，再用最近测速推断出口节点 */
const resolveVia = async (policy: string) => {
  const group: any = proxyGroups.value.find(g => g.name === policy)
  if (!group || group.follow_group) return null
  const useAgg = (group.aggregations || []).length > 0
  const sources = useAgg ? group.aggregations : group.subscriptions || []
  if (!sources.length) return null
  try {
    const { data } = await proxyGroupApi.previewRegex({
      source: useAgg ? 'aggregation' : 'subscription',
      regex: (useAgg ? group.aggregation_regex : group.regex) || '.*',
      ...(useAgg ? { aggregations: sources } : { subscriptions: sources })
    }, profileId)
    const nodes: any[] = data.nodes || []
    if (!nodes.length) return null
    await ensureLatency()
    const reachable = nodes.filter(n => typeof latencyMap.value[n.name]?.latency === 'number')
    const pick =
      group.type === 'url-test'
        ? [...reachable].sort((a, b) => (latencyMap.value[a.name].latency as number) - (latencyMap.value[b.name].latency as number))[0]
        : group.type === 'fallback'
          ? reachable[0]
          : group.type === 'select'
            ? nodes[0]
            : null
    if (!pick) return null
    const ms = latencyMap.value[pick.name]?.latency
    return {
      label: typeof ms === 'number' ? `${pick.name} · ${ms}ms` : pick.name,
      hint: group.type === 'select' ? '策略组默认选中' : '按最近测速推断'
    }
  } catch {
    return null
  }
}

const simulate = async (sample?: string) => {
  if (sample) simQuery.value = sample
  const query = simQuery.value.trim().replace(/^https?:\/\//, '').split('/')[0]
  if (!query || simRunning.value) return
  simRunning.value = true
  simResult.value = null
  simVia.value = null
  simMatchKey.value = ''
  try {
    const { data } = await api.post('/rules/match-test', { query }, { ...profileRequestConfig, timeout: 120000 })
    if (!data.success) throw new Error(data.message || '查询失败')

    // 命中项在收起的分组里时先展开
    const matchKey = data.matched && data.rule_id ? `${data.item_type}-${data.rule_id}` : ''
    if (matchKey && viewMode.value === 'list') {
      const group: any = allRulesAndSets.value.find(
        (item: any) => item.isGroup && item.items?.some((i: any) => i.uniqueId === matchKey)
      )
      if (group && !expandedGroups.value.has(group.groupId)) toggleGroup(group.groupId)
      await nextTick()
    }

    // 逐行扫描到命中行（未命中则扫完全部），总时长控制在 1.6s 内
    const rows = Array.from(rulesContainer.value?.querySelectorAll<HTMLElement>('[data-rule-key]') || [])
    const stop = matchKey ? rows.findIndex(r => r.dataset.ruleKey === matchKey) : rows.length - 1
    const step = Math.max(16, Math.min(90, 1600 / Math.max(1, stop + 1)))
    for (let i = 0; i <= stop && i < rows.length; i++) {
      simScanKey.value = rows[i].dataset.ruleKey || ''
      await wait(step)
    }
    simScanKey.value = ''
    simMatchKey.value = matchKey
    simResult.value = { ...data, query }

    const row = rows[stop]
    if (matchKey && row) {
      const rect = row.getBoundingClientRect()
      if (rect.top < 80 || rect.bottom > window.innerHeight) {
        window.scrollTo({ top: window.scrollY + rect.top - window.innerHeight / 2, behavior: 'smooth' })
      }
    }
    if (data.matched) simVia.value = await resolveVia(data.policy)
  } catch (error: any) {
    if (error.code === 'ECONNABORTED') notify.error('查询超时，请检查规则集配置是否正确')
    else notify.error(error.response?.data?.message || error.message || '查询失败')
  } finally {
    simRunning.value = false
    simScanKey.value = ''
  }
}

// 规则索引相关
const ruleIndexDialogVisible = ref(false)
const ruleIndexQuery = ref('')
const ruleIndexResult = ref<any>(null)
const ruleIndexLoading = ref(false)

// 查找重复规则相关
interface DuplicateOccurrence {
  source_type: string
  source: string
  rule_id: string
  policy: string
  priority: number
  line: string
  line_no?: number
}
interface DuplicateGroup {
  rule_type: string
  value: string
  count: number
  policy_conflict: boolean
  occurrences: DuplicateOccurrence[]
}
interface DuplicateScanResult {
  duplicates: DuplicateGroup[]
  stats: { rules_checked: number; rulesets_checked: number; failed_rulesets: string[] }
  elapsed_time?: number
}
const DUPLICATE_PAGE_SIZE = 20
const DUPLICATE_OCCURRENCE_PAGE_SIZE = 10
const duplicateDialogVisible = ref(false)
// Scan results are immutable snapshots; do not proxy every nested occurrence.
const duplicateResult = shallowRef<DuplicateScanResult | null>(null)
const duplicateLoading = ref(false)
const duplicateError = ref('')
const duplicateStarted = ref(false)
const duplicatePage = ref(1)
const duplicateOccurrencePages = ref<Record<number, number>>({})
const duplicateResultsContainer = ref<HTMLElement | null>(null)
const duplicatePageCount = computed(() => Math.max(1, Math.ceil((duplicateResult.value?.duplicates.length || 0) / DUPLICATE_PAGE_SIZE)))
const duplicateGroupsPage = computed(() => duplicateResult.value?.duplicates.slice((duplicatePage.value - 1) * DUPLICATE_PAGE_SIZE, duplicatePage.value * DUPLICATE_PAGE_SIZE) || [])
const occurrencePage = (index: number) => duplicateOccurrencePages.value[(duplicatePage.value - 1) * DUPLICATE_PAGE_SIZE + index] || 1
const occurrencePageCount = (group: DuplicateGroup) => Math.max(1, Math.ceil(group.occurrences.length / DUPLICATE_OCCURRENCE_PAGE_SIZE))
const duplicateOccurrencesPage = (group: DuplicateGroup, index: number) => {
  const start = (occurrencePage(index) - 1) * DUPLICATE_OCCURRENCE_PAGE_SIZE
  return group.occurrences.slice(start, start + DUPLICATE_OCCURRENCE_PAGE_SIZE)
}
const setOccurrencePage = (index: number, page: number) => {
  duplicateOccurrencePages.value[(duplicatePage.value - 1) * DUPLICATE_PAGE_SIZE + index] = page
}
const setDuplicatePage = (page: number) => {
  duplicatePage.value = page
  if (duplicateResultsContainer.value) duplicateResultsContainer.value.scrollTop = 0
}
let duplicateController: AbortController | undefined
let duplicateScanSequence = 0
let duplicateViewActive = true

const ruleForm = ref<Partial<Rule> & { itemType?: string }>({
  rule_type: 'DOMAIN-SUFFIX',
  value: '',
  policy: 'DIRECT',
  enabled: true,
  remark: '',
  no_resolve: false
})

type RuleSetReference = Partial<Pick<RuleSet, 'id' | 'library_rule_id' | 'policy' | 'enabled' | 'order' | 'remark' | 'no_resolve' | 'group_name'>> & { itemType?: string; formats?: string[]; mosdns?: unknown }

const ruleSetPayload = (item: RuleSetReference) => ({
  id: item.id,
  itemType: 'ruleset',
  library_rule_id: item.library_rule_id,
  policy: item.policy,
  enabled: item.enabled,
  order: item.order,
  remark: item.remark,
  no_resolve: item.no_resolve,
  group_name: item.group_name,
  formats: item.formats,
  mosdns: item.mosdns
})

const ruleSetForm = ref<RuleSetReference>({
  policy: 'DIRECT',
  enabled: true,
  library_rule_id: '',  // 关联的规则仓库ID
  remark: '',
  no_resolve: false
})

// 可用的策略选项：DIRECT、REJECT和所有策略组
const availablePolicies = computed(() => {
  const policies = ['DIRECT', 'REJECT']
  const groupNames = proxyGroups.value.map(g => g.name)
  return [...policies, ...groupNames]
})

// 判断当前规则类型是否为 IP 类型（需要 no-resolve）
// 逻辑规则内部可包含 IP 类条件，同样需要 no-resolve（生成时会写入子条件内）
const LOGIC_RULE_TYPES = ['AND', 'OR', 'NOT']
const IP_RULE_TYPES = ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP']

const isIpRuleType = computed(() => {
  const type = ruleForm.value.rule_type || ''
  return IP_RULE_TYPES.includes(type) || LOGIC_RULE_TYPES.includes(type)
})

// 展开的组ID集合
const expandedGroups = ref<Set<string>>(new Set())

// 切换组的展开/收起状态
const toggleGroup = (groupId: string) => {
  const nextExpandedGroups = new Set(expandedGroups.value)

  if (nextExpandedGroups.has(groupId)) {
    nextExpandedGroups.delete(groupId)
  } else {
    nextExpandedGroups.add(groupId)
  }

  expandedGroups.value = nextExpandedGroups
}

// 规则组重命名相关
const groupRenameDialogVisible = ref(false)
const groupRenameForm = ref({
  groupId: '',
  groupLabel: '规则组',
  groupName: '',
  items: [] as any[]
})

const groupRenameDialogTitle = computed(() => `重命名${groupRenameForm.value.groupLabel}`)

const getGroupMeta = (items: any[]) => {
  const itemTypes = new Set(items.map(item => item.itemType))

  if (itemTypes.size === 1) {
    const itemType = items[0]?.itemType
    if (itemType === 'ruleset') {
      return {
        groupLabel: '规则集组',
        groupDefaultName: `${items.length} 个规则集`
      }
    }

    return {
      groupLabel: '规则组',
      groupDefaultName: `${items.length} 条规则`
    }
  }

  return {
    groupLabel: '混合规则组',
    groupDefaultName: `${items.length} 个规则项`
  }
}

// 显示重命名对话框
const showGroupRenameDialog = (group: any) => {
  const { groupLabel } = getGroupMeta(group.items)
  groupRenameForm.value = {
    groupId: group.groupId,
    groupLabel,
    groupName: group.groupName || '',
    items: group.items
  }
  groupRenameDialogVisible.value = true
}

// 保存规则组名称
const saveGroupName = async () => {
  try {
    const newGroupName = groupRenameForm.value.groupName.trim()
    // 更新组内所有规则项的 group_name 字段
    for (const item of groupRenameForm.value.items) {
      const updatedItem = { ...item, group_name: newGroupName }
      delete updatedItem.uniqueId // 移除前端添加的字段
      if (item.itemType === 'rule') {
        await ruleApi.update(item.id, updatedItem, profileId)
      } else {
        await ruleSetApi.update(item.id, ruleSetPayload(updatedItem), profileId)
      }
    }
    notify.success('重命名成功')
    groupRenameDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '重命名失败'))
  }
}

// 分组并合并连续相同策略的规则
const allRulesAndSets = computed(() => {
  const result: any[] = []
  let currentGroup: any = null

  allRules.value.forEach((item) => {
    const uniqueId = `${item.itemType}-${item.id}`
    const itemWithId = { ...item, uniqueId }

    // 连续且同策略的规则项都参与分组
    if (currentGroup && currentGroup.policy === item.policy) {
      currentGroup.items.push(itemWithId)
      currentGroup.count++
      if (!currentGroup.groupName && item.group_name) {
        currentGroup.groupName = item.group_name
      }
    } else {
      if (currentGroup) {
        result.push(currentGroup)
      }

      const groupId = `group_${item.id}_${item.policy}`
      currentGroup = {
        isGroup: true,
        groupId,
        policy: item.policy,
        groupName: item.group_name || '',
        count: 1,
        items: [itemWithId],
        uniqueId: groupId
      }
    }
  })

  if (currentGroup) {
    result.push(currentGroup)
  }

  const finalResult: any[] = []
  result.forEach(item => {
    const { groupLabel, groupDefaultName } = getGroupMeta(item.items)
    item.groupLabel = groupLabel
    item.groupDefaultName = groupDefaultName

    if (item.isGroup) {
      if (item.count === 1) {
        const singleItem = { ...item.items[0] }
        delete singleItem.isExpandedGroupItem
        delete singleItem.isFirstInGroup
        delete singleItem.isLastInGroup
        delete singleItem.groupId
        delete singleItem.groupPolicy
        finalResult.push(singleItem)
      }
      else if (expandedGroups.value.has(item.groupId)) {
        item.items.forEach((subItem: any, index: number) => {
          finalResult.push({
            ...subItem,
            isExpandedGroupItem: true,
            groupId: item.groupId,
            groupPolicy: item.policy,
            groupLabel: item.groupLabel,
            isFirstInGroup: index === 0,
            isLastInGroup: index === item.items.length - 1
          })
        })
      }
      else {
        finalResult.push(item)
      }
    }
  })

  return finalResult
})

// 过滤出已启用的规则仓库项，并排除已被使用的
const enabledLibraryRules = computed(() => {
  // 获取所有已使用的规则仓库ID（规则集中关联的）
  const usedLibraryIds = new Set(
    allRules.value
      .filter(item => item.itemType === 'ruleset' && item.library_rule_id)
      .map(item => item.library_rule_id)
  )

  // 编辑模式下，当前规则关联的ID应该保留（允许保持当前选择）
  if (isEditRuleSet.value && ruleSetForm.value.library_rule_id) {
    usedLibraryIds.delete(ruleSetForm.value.library_rule_id)
  }

  // 保留正在编辑的引用，即使共享来源已停用。
  return ruleLibrary.value.filter(rule =>
    (rule.enabled !== false || rule.id === selectedLibraryRule.value) && !usedLibraryIds.has(rule.id)
  )
})

const loadAllRules = async () => {
  try {
    const { data } = await ruleApi.getAll(profileId)
    allRules.value = data
  } catch (error) {
    notify.error('加载规则列表失败')
  }
}

const loadProxyGroups = async () => {
  try {
    const { data } = await proxyGroupApi.getAll(profileId)
    proxyGroups.value = data
  } catch (error) {
    notify.error('加载策略组列表失败')
  }
}

const loadRuleLibrary = async () => {
  try {
    const { data } = await api.get('/rule-library')
    ruleLibrary.value = data
  } catch (error) {
    notify.error('加载规则库失败')
  }
}

const showAddRuleDialog = () => {
  isEditRule.value = false
  ruleForm.value = {
    id: `rule_${Date.now()}`,
    rule_type: 'DOMAIN-SUFFIX',
    value: '',
    policy: 'DIRECT',
    enabled: true,
    remark: '',
    no_resolve: false,  // DOMAIN-SUFFIX 不需要 no-resolve
    itemType: 'rule'
  }
  ruleDialogVisible.value = true
}

const editRule = (row: Rule) => {
  isEditRule.value = true
  ruleForm.value = { ...row }
  // 如果没有 no_resolve 字段，根据规则类型设置默认值
  if (ruleForm.value.no_resolve === undefined || ruleForm.value.no_resolve === null) {
    ruleForm.value.no_resolve = IP_RULE_TYPES.includes(ruleForm.value.rule_type || '')
  }
  ruleDialogVisible.value = true
}

const saveRule = async () => {
  try {
    const ruleData = { ...ruleForm.value, itemType: 'rule' }
    if (isEditRule.value) {
      const originalItem = allRules.value.find(r => r.id === ruleData.id && r.itemType === 'rule')
      if (originalItem && originalItem.policy !== ruleData.policy) {
        ruleData.group_name = ''
      }
      await ruleApi.update(ruleData.id!, ruleData, profileId)
      notify.success('更新成功')
    } else {
      await ruleApi.create(ruleData, profileId)
      notify.success('添加成功')
    }
    ruleDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  }
}

const deleteRule = async (row: any) => {
  try {
    await ruleApi.delete(row.id, profileId)

    // 同步更新 MosDNS 配置，移除对该规则的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets', profileRequestConfig)

      // 从 direct_rules 和 proxy_rules 中移除该规则 ID
      const updatedDirectRules = mosdnsConfig.direct_rules.filter((id: string) => id !== row.id)
      const updatedProxyRules = mosdnsConfig.proxy_rules.filter((id: string) => id !== row.id)

      // 如果有变化，保存更新
      if (updatedDirectRules.length !== mosdnsConfig.direct_rules.length ||
          updatedProxyRules.length !== mosdnsConfig.proxy_rules.length) {
        await api.post('/mosdns/rulesets', {
          direct_rulesets: mosdnsConfig.direct_rulesets,
          proxy_rulesets: mosdnsConfig.proxy_rulesets,
          direct_rules: updatedDirectRules,
          proxy_rules: updatedProxyRules
        }, profileRequestConfig)
        console.log('已同步更新 MosDNS 配置，移除了对规则的引用')
      }
    } catch (error) {
      console.error('同步更新 MosDNS 配置失败:', error)
      // 不阻断删除操作，只记录警告
      notify.warning('规则已删除，但 MosDNS 配置同步失败，请手动检查')
    }

    notify.success('删除成功')
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

const showAddRuleSetDialog = () => {
  isEditRuleSet.value = false
  selectedLibraryRule.value = ''  // 清空选中的规则
  ruleSetForm.value = {
    id: `ruleset_${Date.now()}`,
    policy: 'DIRECT',
    enabled: true,
    library_rule_id: '',
    remark: '',
    no_resolve: false,  // classical 不需要 no-resolve
    itemType: 'ruleset'
  }
  ruleSetDialogVisible.value = true
}

// 来源只从共享目录读取，不复制到当前配置。
const onLibraryRuleSelect = (libraryRuleId: string) => {
  selectedLibraryRule.value = libraryRuleId
  ruleSetForm.value.library_rule_id = libraryRuleId
  ruleSetForm.value.no_resolve = selectedLibrarySource.value?.behavior === 'ipcidr'
}

const onLibraryRuleClear = () => {
  selectedLibraryRule.value = ''
  ruleSetForm.value.library_rule_id = ''
  ruleSetForm.value.no_resolve = false
}

const editRuleSet = (row: RuleSet) => {
  isEditRuleSet.value = true
  ruleSetForm.value = ruleSetPayload(row)
  // 如果没有 no_resolve 字段，根据 behavior 设置默认值
  if (ruleSetForm.value.no_resolve === undefined || ruleSetForm.value.no_resolve === null) {
    ruleSetForm.value.no_resolve = row.behavior === 'ipcidr'
  }

  // 如果有关联的规则仓库ID，则反显
  if (row.library_rule_id) {
    selectedLibraryRule.value = row.library_rule_id
  } else {
    selectedLibraryRule.value = ''
  }

  ruleSetDialogVisible.value = true
}

const saveRuleSet = async () => {
  if (!selectedLibraryRule.value) {
    notify.warning('请先选择共享规则库中的规则集')
    return
  }
  try {
    const ruleSetData = ruleSetPayload(ruleSetForm.value)
    if (isEditRuleSet.value) {
      // 检查策略是否发生变化
      const originalItem = allRules.value.find(r => r.id === ruleSetData.id && r.itemType === 'ruleset')
      if (originalItem && originalItem.policy !== ruleSetData.policy) {
        // 策略变化，清除组名称
        ruleSetData.group_name = ''
      }
      await ruleSetApi.update(ruleSetData.id!, ruleSetData, profileId)
      notify.success('更新成功')
    } else {
      await ruleSetApi.create(ruleSetData, profileId)
      notify.success('添加成功')
    }
    ruleSetDialogVisible.value = false
    // 保存后重置所有展开状态，确保分组状态正确
    expandedGroups.value = new Set()
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  }
}

const deleteRuleSet = async (row: any) => {
  try {
    await ruleSetApi.delete(row.id, profileId)

    // 同步更新 MosDNS 配置，移除对该规则集的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets', profileRequestConfig)

      // 从 direct_rulesets 和 proxy_rulesets 中移除该规则集 ID
      const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter((id: string) => id !== row.id)
      const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter((id: string) => id !== row.id)

      // 如果有变化，保存更新
      if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
          updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
        await api.post('/mosdns/rulesets', {
          direct_rulesets: updatedDirectRulesets,
          proxy_rulesets: updatedProxyRulesets,
          direct_rules: mosdnsConfig.direct_rules,
          proxy_rules: mosdnsConfig.proxy_rules
        }, profileRequestConfig)
        console.log('已同步更新 MosDNS 配置，移除了对规则集的引用')
      }
    } catch (error) {
      console.error('同步更新 MosDNS 配置失败:', error)
      // 不阻断删除操作，只记录警告
      notify.warning('规则集已删除，但 MosDNS 配置同步失败，请手动检查')
    }

    notify.success('删除成功')
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

// 统一的编辑方法
const editItem = (row: any) => {
  if (row.itemType === 'rule') {
    editRule(row)
  } else {
    editRuleSet(row)
  }
}

// 统一的删除方法
const deleteItem = (row: any) => {
  if (row.itemType === 'rule') {
    deleteRule(row)
  } else {
    deleteRuleSet(row)
  }
}


// 切换启用/禁用状态
const toggleItemStatus = async (item: any) => {
  // 找到原始数据中的对应项
  const originalItem = allRules.value.find(r => r.id === item.id && r.itemType === item.itemType)
  if (!originalItem) {
    notify.error('未找到对应的规则项')
    return
  }

  // 保存旧状态用于回滚
  const oldEnabled = originalItem.enabled

  // 切换状态
  item.enabled = !item.enabled
  originalItem.enabled = !originalItem.enabled


  try {
    if (item.itemType === 'rule') {
      await ruleApi.update(item.id, originalItem, profileId)
    } else {
      await ruleSetApi.update(item.id, ruleSetPayload(originalItem), profileId)
    }
    notify.success('状态已更新')
  } catch (error) {
    notify.error(errorMessage(error, '更新状态失败'))
    // 失败后恢复状态
    item.enabled = oldEnabled
    originalItem.enabled = oldEnabled
  }
}

const getItemKey = (item: any) => `${item.itemType}-${item.id}`

const rebuildRulesOrderFromDisplay = (displayItems: any[], orderedIds: string[]) => {
  const rawItems = new Map(allRules.value.map(item => [getItemKey(item), item]))
  const displayMap = new Map(displayItems.map(item => [item.uniqueId, item]))
  const reorderedRules: any[] = []

  orderedIds.forEach(uniqueId => {
    const displayItem = displayMap.get(uniqueId)
    if (!displayItem) return

    if (displayItem.isGroup) {
      displayItem.items.forEach((groupItem: any) => {
        const rawItem = rawItems.get(getItemKey(groupItem))
        if (rawItem) {
          reorderedRules.push(rawItem)
        }
      })
      return
    }

    const rawItem = rawItems.get(getItemKey(displayItem))
    if (rawItem) {
      reorderedRules.push(rawItem)
    }
  })

  return reorderedRules
}



// 规则索引相关方法
const showRuleIndexDialog = () => {
  ruleIndexQuery.value = ''
  ruleIndexResult.value = null
  ruleIndexDialogVisible.value = true
}

const performRuleIndexQuery = async () => {
  const query = ruleIndexQuery.value.trim()
  if (!query) {
    notify.warning('请输入域名或IP地址')
    return
  }

  ruleIndexLoading.value = true
  ruleIndexResult.value = null

  try {
    // 规则索引需要更长的超时时间（2分钟），因为需要获取和解析规则集内容
    const { data } = await api.post('/rules/match-test', { query }, {
      ...profileRequestConfig,
      timeout: 120000  // 2分钟超时
    })
    if (data.success) {
      ruleIndexResult.value = data
    } else {
      notify.error(data.message || '查询失败')
    }
  } catch (error: any) {
    console.error('Rule index query failed:', error)
    if (error.response?.status === 404) {
      notify.error('该功能需要专业版')
    } else if (error.code === 'ECONNABORTED') {
      notify.error('查询超时，请检查规则集配置是否正确')
    } else {
      notify.error(error.response?.data?.message || '查询失败，请稍后重试')
    }
  } finally {
    ruleIndexLoading.value = false
  }
}

// 查找重复规则相关方法
const showDuplicateDialog = () => {
  duplicateDialogVisible.value = true
  // Closing the dialog does not start another server scan when it is reopened.
  if (!duplicateStarted.value) void performDuplicateScan()
}

const performDuplicateScan = async () => {
  if (!duplicateViewActive || duplicateLoading.value) return
  const sequence = ++duplicateScanSequence
  const controller = new AbortController()
  duplicateController = controller
  const isCurrent = () => duplicateViewActive && sequence === duplicateScanSequence && !controller.signal.aborted
  duplicateStarted.value = true
  duplicateLoading.value = true
  duplicateResult.value = null
  duplicateError.value = ''
  duplicatePage.value = 1
  duplicateOccurrencePages.value = {}

  try {
    // 需要拉取并解析规则集内容，与规则索引一样使用较长超时
    const { data } = await ruleApi.findDuplicates(profileId, controller.signal)
    if (!isCurrent()) return
    if (data.success) {
      duplicateResult.value = data
    } else {
      duplicateError.value = data.message || '查重失败，请重新扫描'
    }
  } catch (error: any) {
    if (!isCurrent() || error.code === 'ERR_CANCELED') return
    duplicateError.value = ['ECONNABORTED', 'ETIMEDOUT'].includes(error.code)
      ? '查重超时，请稍后重新扫描；规则集较多时首次检查可能较慢'
      : error.response?.data?.message || '查重失败，请稍后重新扫描'
  } finally {
    if (isCurrent()) {
      duplicateLoading.value = false
      duplicateController = undefined
    }
  }
}

onUnmounted(() => {
  duplicateViewActive = false
  ++duplicateScanSequence
  duplicateController?.abort()
  duplicateController = undefined
})

const deleteDuplicateRule = async (occ: any) => {
  // 后端对缺失 itemType 的旧数据按 'rule' 兜底，这里保持同样口径
  const rule = allRules.value.find(r => r.id === occ.rule_id && (r.itemType ?? 'rule') === 'rule')
  if (!rule) {
    notify.error('未找到对应的规则，可能已被删除')
    return
  }
  await deleteRule(rule)
  // 删除后刷新查重结果
  performDuplicateScan()
}


// 监听规则类型变化，自动设置 no_resolve 默认值
watch(() => ruleForm.value.rule_type, (newType) => {
  // 切换规则类型时，自动设置 no_resolve（IP 类型默认开启）
  const isIpType = ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP'].includes(newType || '')
  ruleForm.value.no_resolve = isIpType
})


/* ---------- 统一拖动排序 ----------
 * 可见列表是展示项（分组会折叠多条原始规则），排序结果必须展开回
 * 原始 rule_configs。展示项的键是 itemType-id 复合键，说明原始 id 可能
 * 跨类型重复，因此这里沿用后端仍兼容的完整数组格式而不是 ids 契约。
 */
const reorderDisplayItems = ref<any[]>([])

const displayList = computed(() =>
  reorder.active.value ? reorderDisplayItems.value : allRulesAndSets.value
)

const reorder = useReorder<any>({
  items: reorderDisplayItems,
  container: rulesContainer,
  labelOf: item =>
    item.isGroup ? item.groupName || item.groupDefaultName : item.name || item.id,
  persist: async items => {
    const rebuilt = rebuildRulesOrderFromDisplay(items, items.map(item => item.uniqueId))
    if (rebuilt.length !== allRules.value.length) {
      // 数量对不上说明展示项与原始数据失配，宁可报错也不提交残缺顺序
      throw new Error(
        `排序重建失败：期望 ${allRules.value.length} 条，实际 ${rebuilt.length} 条`
      )
    }
    await api.post('/rules/reorder', {
      rule_configs: rebuilt.map(item => item.itemType === 'ruleset' ? ruleSetPayload(item) : item)
    }, profileRequestConfig)
    allRules.value = rebuilt
  }
})

const enterReorder = () => {
  reorderDisplayItems.value = [...allRulesAndSets.value]
  reorder.enter()
}

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    // 折叠分组，让合并后的分组卡片立即反映最新顺序
    expandedGroups.value = new Set()
    await loadAllRules()
    notify.success('顺序已保存')
  } catch (error) {
    notify.error(errorMessage(error, '保存顺序失败，顺序已还原'))
  }
}

onMounted(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()])
})


// 页面激活时重新加载数据（从其他页面返回时）
onActivated(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()])
})
</script>

<style scoped>
.rule-row.is-scan {
  background: var(--secondary);
}

.rule-row.is-match {
  background: var(--primary-soft);
  box-shadow: inset 3px 0 0 var(--primary-accent);
}

.rule-row.is-match .rule-index {
  color: var(--primary-accent);
}
</style>
