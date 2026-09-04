<template>
  <div :class="reorder.active.value && 'cf-reordering'">
    <ScopeBanner scope="profile" :profile-name="cfProfileName" />

    <PageHeader
      eyebrow="Profile"
      title="策略规则"
      description="单条规则与规则集按顺序匹配，命中即生效，仅属于当前配置空间。"
    >
      <template #actions>
        <Button class="shadow-glow" @click="showAddRuleDialog">
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
          <DropdownMenuContent align="end" class="glass-strong">
            <DropdownMenuItem @select="showAddRuleSetDialog">
              <FolderOpen class="size-4" />
              添加规则集
            </DropdownMenuItem>
            <DropdownMenuItem @select="handleShowRuleIndex">
              <Search class="size-4" />
              规则索引
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

    <SectionCard v-if="displayList.length === 0" :padded="false">
      <EmptyState
        :icon="FileText"
        title="暂无规则"
        description="添加单条规则或引用一个规则集，规则会按列表顺序自上而下匹配。"
      >
        <Button @click="showAddRuleDialog">
          <Plus class="size-4" />
          添加规则
        </Button>
      </EmptyState>
    </SectionCard>

    <!-- ===== 列表视图 ===== -->
    <SectionCard v-else-if="viewMode === 'list'" :padded="false">
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
            <Layers class="size-4 shrink-0 text-warning-accent" aria-hidden="true" />
            <span class="min-w-0 truncate text-[13px] font-semibold text-foreground">
              {{ item.groupName || item.groupDefaultName }}
            </span>
            <Badge variant="warning" class="shrink-0 text-[10.5px]">{{ item.groupLabel }}</Badge>
            <Badge v-if="item.groupName" variant="outline" class="num shrink-0 text-[10.5px]">
              {{ item.count }} 个
            </Badge>
            <Badge variant="secondary" class="ml-auto shrink-0 max-w-[160px] truncate text-[10.5px]">
              {{ item.policy }}
            </Badge>
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
            <ChevronDown class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>

          <!-- 普通行 -->
          <div
            v-else
            :class="[
              'flex items-center gap-2.5 border-0 border-b border-border/40 px-4 py-2.5 transition-colors hover:bg-accent/30',
              item.isExpandedGroupItem && 'bg-background/40 pl-8',
              !item.enabled && 'opacity-55'
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
            <Badge
              :variant="item.itemType === 'rule' ? 'brand' : 'info'"
              class="w-12 shrink-0 justify-center text-[10.5px]"
            >
              {{ item.itemType === 'rule' ? '规则' : '规则集' }}
            </Badge>

            <div class="flex min-w-0 flex-1 items-center gap-2">
              <template v-if="item.itemType === 'rule'">
                <span class="shrink-0 font-mono text-[11.5px] text-info-accent">{{ item.rule_type }}</span>
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
              <span
                v-if="item.remark"
                class="hidden min-w-0 shrink truncate text-[11.5px] text-muted-foreground md:inline"
                :title="item.remark"
              >
                {{ item.remark }}
              </span>
            </div>

            <Badge variant="secondary" class="max-w-[160px] shrink-0 truncate text-[10.5px]">
              {{ item.policy }}
            </Badge>

            <div class="cf-reorder-mute flex shrink-0 items-center gap-0.5">
              <Button
                v-if="item.isExpandedGroupItem && item.isFirstInGroup"
                variant="ghost"
                size="sm"
                @click.stop="toggleGroup(item.groupId)"
              >
                <ChevronUp class="size-3.5" />
                收起分组
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
                title="删除"
                aria-label="删除"
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
          'hairline edge-light relative flex flex-col gap-3 overflow-hidden rounded-xl border bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft',
          item.isGroup ? 'border-warning-accent/35' : 'border-border/35',
          item.isExpandedGroupItem && 'border-primary-accent/30',
          !item.isGroup && !item.enabled && 'opacity-60'
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
            <Badge variant="secondary" class="max-w-[160px] truncate text-[10.5px]">{{ item.policy }}</Badge>
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
            <p v-if="item.remark" class="mt-1.5 mb-0 text-[11.5px] text-muted-foreground" :title="item.remark">
              {{ item.remark }}
            </p>
          </div>

          <footer class="cf-reorder-mute mt-auto flex items-center gap-1 border-0 border-t border-border/50 pt-3">
            <Badge variant="secondary" class="max-w-[150px] truncate text-[10.5px]">{{ item.policy }}</Badge>
            <Button variant="ghost" size="icon-sm" class="ml-auto" title="编辑" aria-label="编辑" @click="editItem(item)">
              <Pencil class="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              class="text-destructive-accent hover:bg-destructive-soft"
              title="删除"
              aria-label="删除"
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
      <DialogContent class="glass-strong hairline max-w-[520px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEditRule ? '编辑规则' : '添加规则' }}</DialogTitle>
          <DialogDescription>规则按列表顺序自上而下匹配，命中即停止。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[60dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label>规则类型</Label>
            <Select v-model="ruleForm.rule_type">
              <SelectTrigger class="w-full bg-background/50 font-mono">
                <SelectValue />
              </SelectTrigger>
              <SelectContent class="glass-strong">
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
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="选择策略" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="policy in availablePolicies" :key="policy" :value="policy">
                  {{ policy }}
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
              IP 类规则建议开启；逻辑规则会写入其 IP 子条件。
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
      <DialogContent class="glass-strong hairline max-w-[520px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ isEditRuleSet ? '编辑规则集' : '添加规则集' }}</DialogTitle>
          <DialogDescription>可从规则仓库选取，或手动填写地址与类型。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[60dvh] flex-col gap-4 overflow-y-auto pr-1">
          <div class="flex flex-col gap-1.5">
            <Label>选择规则</Label>
            <Select v-model="selectedLibraryRule" @update:model-value="value => onLibraryRuleSelect(String(value))">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="从规则仓库选择（可选）" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
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
              <p class="m-0 flex-1 text-[12px] text-muted-foreground">已添加的规则不会重复显示在列表中。</p>
              <Button v-if="selectedLibraryRule" variant="ghost" size="sm" @click="onLibraryRuleClear">
                清除选择
              </Button>
            </div>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="ruleset-name">名称</Label>
            <Input
              id="ruleset-name"
              v-model="ruleSetForm.name"
              class="bg-background/50"
              placeholder="规则集名称"
              :disabled="!!selectedLibraryRule"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="ruleset-url">URL</Label>
            <Input
              id="ruleset-url"
              v-model="ruleSetForm.url"
              class="bg-background/50 font-mono"
              placeholder="规则集 URL 地址"
              :disabled="!!selectedLibraryRule"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select v-model="ruleSetForm.behavior" :disabled="!!selectedLibraryRule">
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

          <div class="flex flex-col gap-1.5">
            <Label>策略</Label>
            <Select v-model="ruleSetForm.policy">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue placeholder="选择策略" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="policy in availablePolicies" :key="policy" :value="policy">
                  {{ policy }}
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

          <div v-if="ruleSetForm.behavior === 'ipcidr'" class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2.5">
              <Switch id="ruleset-noresolve" v-model="ruleSetForm.no_resolve" />
              <Label for="ruleset-noresolve" class="text-[13px] text-muted-foreground">no-resolve</Label>
            </div>
            <p class="m-0 text-[12px] text-muted-foreground">IP CIDR 类规则集建议开启。</p>
          </div>

          <div class="flex items-center gap-2.5">
            <Switch
              id="ruleset-enabled"
              v-model="ruleSetForm.enabled"
              @update:model-value="value => handleRuleSetStatusChange(Boolean(value))"
            />
            <Label for="ruleset-enabled" class="text-[13px] text-muted-foreground">
              {{ ruleSetForm.enabled ? '规则集启用中' : '规则集已停用' }}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="ruleSetDialogVisible = false">取消</Button>
          <Button @click="saveRuleSet">保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 规则组重命名 ===== -->
    <Dialog v-model:open="groupRenameDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[440px] border-border/50">
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

    <!-- ===== 规则索引 ===== -->
    <Dialog v-model:open="ruleIndexDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[620px] border-border/50">
        <DialogHeader>
          <DialogTitle>规则索引</DialogTitle>
          <DialogDescription>输入域名或 IP，查询它会命中哪一条规则。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-2">
            <Input
              v-model="ruleIndexQuery"
              class="bg-background/50 font-mono"
              placeholder="例如 google.com 或 192.168.1.1"
              @keyup.enter="performRuleIndexQuery"
            />
            <Button class="shrink-0" :disabled="ruleIndexLoading" @click="performRuleIndexQuery">
              <Loader2 v-if="ruleIndexLoading" class="size-4 animate-spin" />
              <Search v-else class="size-4" />
              查询
            </Button>
          </div>

          <div
            v-if="ruleIndexResult"
            class="rounded-xl border border-border/50 bg-background/40 p-4"
          >
            <template v-if="ruleIndexResult.matched">
              <div class="mb-3 flex items-center gap-2">
                <CircleCheck class="size-4 text-success-accent" aria-hidden="true" />
                <span class="text-[14px] font-semibold text-foreground">{{ ruleIndexResult.rule_name }}</span>
              </div>
              <dl class="m-0 grid grid-cols-[88px_minmax(0,1fr)] gap-x-3 gap-y-2 text-[12.5px]">
                <dt class="text-muted-foreground">规则类型</dt>
                <dd class="m-0">
                  <Badge :variant="ruleIndexResult.rule_type === 'rule' ? 'brand' : 'info'">
                    {{ ruleIndexResult.rule_type === 'rule' ? '直接规则' : '规则集' }}
                  </Badge>
                </dd>
                <dt class="text-muted-foreground">匹配规则</dt>
                <dd class="m-0 font-mono break-all text-foreground">{{ ruleIndexResult.matched_line }}</dd>
                <dt class="text-muted-foreground">执行策略</dt>
                <dd class="m-0"><Badge :variant="policyTone(ruleIndexResult.policy)">{{ ruleIndexResult.policy }}</Badge></dd>
                <dt class="text-muted-foreground">规则来源</dt>
                <dd class="m-0 break-all text-foreground">{{ ruleIndexResult.source }}</dd>
                <dt class="text-muted-foreground">优先级</dt>
                <dd class="num m-0 text-foreground">第 {{ ruleIndexResult.priority }} 条</dd>
                <dt class="text-muted-foreground">Behavior</dt>
                <dd class="m-0 font-mono text-foreground">{{ ruleIndexResult.behavior }}</dd>
                <template v-if="ruleIndexResult.elapsed_time !== undefined">
                  <dt class="text-muted-foreground">索引耗时</dt>
                  <dd class="num m-0 text-foreground">{{ ruleIndexResult.elapsed_time }} ms</dd>
                </template>
              </dl>
            </template>

            <template v-else>
              <div class="mb-2 flex items-center gap-2">
                <TriangleAlert class="size-4 text-warning-accent" aria-hidden="true" />
                <span class="text-[14px] font-semibold text-foreground">未匹配任何规则</span>
              </div>
              <p class="m-0 text-[12.5px] text-muted-foreground">{{ ruleIndexResult.message }}</p>
              <p
                v-if="ruleIndexResult.elapsed_time !== undefined"
                class="num mt-1 mb-0 text-[12px] text-muted-foreground"
              >
                索引耗时 {{ ruleIndexResult.elapsed_time }} ms
              </p>
            </template>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="ruleIndexDialogVisible = false">关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 查找重复规则 ===== -->
    <Dialog v-model:open="duplicateDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[720px] border-border/50">
        <DialogHeader>
          <DialogTitle>查找重复规则</DialogTitle>
          <DialogDescription>
            检查已启用的直接规则与规则集内容，找出完全相同的规则条目。首次扫描需拉取规则集内容，可能较慢。
          </DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="duplicateLoading" :rows="5" />

        <div v-else-if="duplicateResult" class="flex max-h-[58dvh] flex-col gap-3 overflow-y-auto pr-1">
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

          <Alert v-if="duplicateResult.stats.failed_rulesets.length" variant="default" class="border-warning-accent/35 bg-warning-soft/40">
            <AlertDescription class="text-[12.5px]">
              以下规则集内容获取失败，未参与查重：{{ duplicateResult.stats.failed_rulesets.join('、') }}
            </AlertDescription>
          </Alert>

          <EmptyState
            v-if="!duplicateResult.duplicates.length"
            :icon="CircleCheck"
            title="未发现重复规则"
            description="当前启用的规则与规则集之间没有完全相同的条目。"
          />

          <div
            v-for="group in duplicateResult.duplicates"
            :key="`${group.rule_type},${group.value}`"
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
              v-for="(occ, i) in group.occurrences"
              :key="i"
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
          </div>
        </div>

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
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import { useProfileStore } from '@/stores/profile'
import { ref, onMounted, onUnmounted, onActivated, computed, nextTick, watch } from 'vue'
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
  Plus,
  Search,
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
import { ruleApi, ruleSetApi, proxyGroupApi } from '@/api'
import type { Rule, RuleSet, ProxyGroup } from '@/types'
import { activeProfileId } from '@/profileContext'
import Sortable from 'sortablejs'
import api from '@/api'


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

const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
const allRules = ref<any[]>([])  // 包含规则和规则集的合并数组
const proxyGroups = ref<ProxyGroup[]>([])
const ruleLibrary = ref<any[]>([])  // 规则仓库
const selectedLibraryRule = ref('')  // 选中的规则仓库项ID
const ruleDialogVisible = ref(false)
const ruleSetDialogVisible = ref(false)
const isEditRule = ref(false)
const isEditRuleSet = ref(false)
const viewMode = ref<'list' | 'card'>('card') // 默认卡片视图
const rulesContainer = ref<HTMLElement | null>(null)
let sortableInstance: Sortable | null = null

// 处理按钮点击
const handleShowRuleIndex = () => {
  showRuleIndexDialog()
}

// 规则索引相关
const ruleIndexDialogVisible = ref(false)
const ruleIndexQuery = ref('')
const ruleIndexResult = ref<any>(null)
const ruleIndexLoading = ref(false)

// 查找重复规则相关
const duplicateDialogVisible = ref(false)
const duplicateResult = ref<any>(null)
const duplicateLoading = ref(false)
const isSavingOrder = ref(false) // 防止并发保存排序

const ruleForm = ref<Partial<Rule>>({
  rule_type: 'DOMAIN-SUFFIX',
  value: '',
  policy: 'DIRECT',
  enabled: true,
  remark: '',
  no_resolve: false
})

const ruleSetForm = ref<Partial<RuleSet>>({
  name: '',
  url: '',
  behavior: 'classical',
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
        await ruleApi.update(item.id, updatedItem)
      } else {
        await ruleSetApi.update(item.id, updatedItem)
      }
    }
    notify.success('重命名成功')
    groupRenameDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    notify.error('重命名失败')
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

  // 过滤：启用的 + 未被使用的
  return ruleLibrary.value.filter(rule =>
    rule.enabled && !usedLibraryIds.has(rule.id)
  )
})

const loadAllRules = async () => {
  try {
    const { data } = await ruleApi.getAll()
    allRules.value = data
  } catch (error) {
    notify.error('加载规则列表失败')
  }
}

const loadProxyGroups = async () => {
  try {
    const { data } = await proxyGroupApi.getAll()
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
    notify.error('加载规则仓库失败')
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
      await ruleApi.update(ruleData.id!, ruleData)
      notify.success('更新成功')
    } else {
      await ruleApi.create(ruleData)
      notify.success('添加成功')
    }
    ruleDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    notify.error('保存失败')
  }
}

const deleteRule = async (row: any) => {
  try {
    await ruleApi.delete(row.id)

    // 同步更新 MosDNS 配置，移除对该规则的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

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
        })
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
    notify.error('删除失败')
  }
}

const showAddRuleSetDialog = () => {
  isEditRuleSet.value = false
  selectedLibraryRule.value = ''  // 清空选中的规则
  ruleSetForm.value = {
    id: `ruleset_${Date.now()}`,
    name: '',
    url: '',
    behavior: 'classical',
    policy: 'DIRECT',
    enabled: true,
    library_rule_id: '',
    remark: '',
    no_resolve: false,  // classical 不需要 no-resolve
    itemType: 'ruleset'
  }
  ruleSetDialogVisible.value = true
}

// 规则仓库选择处理
const onLibraryRuleSelect = (libraryRuleId: string) => {
  const selectedRule = ruleLibrary.value.find(r => r.id === libraryRuleId)
  if (selectedRule) {
    ruleSetForm.value.name = selectedRule.name
    ruleSetForm.value.behavior = selectedRule.behavior
    ruleSetForm.value.library_rule_id = libraryRuleId  // 保存规则仓库ID
    // 根据 behavior 自动设置 no_resolve
    ruleSetForm.value.no_resolve = selectedRule.behavior === 'ipcidr'

    // 根据规则来源类型设置 URL
    if (selectedRule.source_type === 'content') {
      // 规则内容类型，使用内容接口
      const baseUrl = `${window.location.protocol}//${window.location.host}`
      ruleSetForm.value.url = `${baseUrl}/api/profiles/${encodeURIComponent(activeProfileId.value)}/rule-library/content/${libraryRuleId}`
    } else {
      // URL 类型，使用原始 URL
      ruleSetForm.value.url = selectedRule.url
    }
  }
}

// 规则仓库清除处理
const onLibraryRuleClear = () => {
  // 清除后允许手动输入
  ruleSetForm.value.name = ''
  ruleSetForm.value.url = ''
  ruleSetForm.value.behavior = 'classical'
  ruleSetForm.value.library_rule_id = ''  // 清除关联
  ruleSetForm.value.no_resolve = false  // classical 不需要 no-resolve
}

const editRuleSet = (row: RuleSet) => {
  isEditRuleSet.value = true
  ruleSetForm.value = { ...row }
  // 如果没有 no_resolve 字段，根据 behavior 设置默认值
  if (ruleSetForm.value.no_resolve === undefined || ruleSetForm.value.no_resolve === null) {
    ruleSetForm.value.no_resolve = ruleSetForm.value.behavior === 'ipcidr'
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
  try {
    const ruleSetData = { ...ruleSetForm.value, itemType: 'ruleset' }
    if (isEditRuleSet.value) {
      // 检查策略是否发生变化
      const originalItem = allRules.value.find(r => r.id === ruleSetData.id && r.itemType === 'ruleset')
      if (originalItem && originalItem.policy !== ruleSetData.policy) {
        // 策略变化，清除组名称
        ruleSetData.group_name = ''
      }
      await ruleSetApi.update(ruleSetData.id!, ruleSetData)
      notify.success('更新成功')
    } else {
      await ruleSetApi.create(ruleSetData)
      notify.success('添加成功')
    }
    ruleSetDialogVisible.value = false
    // 保存后重置所有展开状态，确保分组状态正确
    expandedGroups.value = new Set()
    loadAllRules()
  } catch (error) {
    notify.error('保存失败')
  }
}

const deleteRuleSet = async (row: any) => {
  try {
    await ruleSetApi.delete(row.id)

    // 同步更新 MosDNS 配置，移除对该规则集的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

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
        })
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
    notify.error('删除失败')
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

// 判断是否为完整URL（http/https开头）
const isFullUrl = (url: string): boolean => {
  if (!url) return false
  return url.startsWith('http://') || url.startsWith('https://')
}

// 测试URL的连通性
const testUrlConnectivity = async (url: string): Promise<boolean> => {
  try {
    // 只测试完整URL，排除相对路径
    if (!isFullUrl(url)) {
      return true  // 相对路径不测试，默认认为可用
    }

    // 调用后端测试接口
    const { data } = await api.post('/rule-library/test-single', { url })
    if (data.success) {
      return data.available
    }
    return false
  } catch (error) {
    return false
  }
}

// 测试单个规则库规则的连通性
const testLibraryRuleConnectivity = async (libraryRuleId: string): Promise<boolean> => {
  try {
    // 查找规则库中的规则
    const libraryRule = ruleLibrary.value.find(r => r.id === libraryRuleId)
    if (!libraryRule) {
      return false
    }

    // 只测试完整URL
    return await testUrlConnectivity(libraryRule.url)
  } catch (error) {
    return false
  }
}

// 处理编辑对话框中规则集状态切换
const handleRuleSetStatusChange = async (enabled: boolean) => {
  // 如果是关闭，直接允许
  if (!enabled) {
    return
  }

  // 如果是开启，需要测试连通性（仅测试完整URL）
  if (ruleSetForm.value.url && isFullUrl(ruleSetForm.value.url)) {
    const loadingToast = notify.loading('正在测试规则连通性…')
    const isAvailable = await testUrlConnectivity(ruleSetForm.value.url)
    notify.dismiss(loadingToast)

    if (!isAvailable) {
      notify.error('规则地址无法访问，无法开启')
      // 自动关闭开关
      ruleSetForm.value.enabled = false
      return
    }
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

  // 如果是规则集且正在开启，需要先测试连通性（只测试完整URL）
  if (item.itemType === 'ruleset' && item.enabled) {
    // 如果有URL且是完整URL，进行测试
    if (item.url && isFullUrl(item.url)) {
      const loadingToast = notify.loading('正在测试规则连通性…')
      const isAvailable = await testUrlConnectivity(item.url)
      notify.dismiss(loadingToast)

      if (!isAvailable) {
        notify.error('规则地址无法访问，无法开启')
        // 回滚状态
        item.enabled = oldEnabled
        originalItem.enabled = oldEnabled

        // 如果关联了规则仓库，也更新规则仓库的状态
        if (item.library_rule_id) {
          const libraryRule = ruleLibrary.value.find(r => r.id === item.library_rule_id)
          if (libraryRule) {
            libraryRule.enabled = false
            try {
              await api.put(`/rule-library/${item.library_rule_id}`, libraryRule)
            } catch (error) {
              console.error('更新规则仓库状态失败:', error)
            }
          }
        }

        return
      }
    }
  }

  try {
    if (item.itemType === 'rule') {
      await ruleApi.update(item.id, originalItem)
    } else {
      await ruleSetApi.update(item.id, originalItem)
    }
    notify.success('状态已更新')
  } catch (error) {
    notify.error('更新状态失败')
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


const saveRulesOrder = async (): Promise<boolean> => {
  // 防止并发请求
  if (isSavingOrder.value) {
    console.log('排序保存中，跳过本次请求')
    return false
  }

  // 验证数据
  if (!allRules.value || allRules.value.length === 0) {
    console.warn('没有规则数据，跳过保存')
    return false
  }

  try {
    isSavingOrder.value = true
    // 批量更新规则和规则集顺序（使用合并数组）
    await api.post('/rules/reorder', {
      rule_configs: allRules.value
    })
    return true
  } finally {
    isSavingOrder.value = false
  }
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
  performDuplicateScan()
}

const performDuplicateScan = async () => {
  duplicateLoading.value = true
  duplicateResult.value = null

  try {
    // 需要拉取并解析规则集内容，与规则索引一样使用较长超时
    const { data } = await ruleApi.findDuplicates()
    if (data.success) {
      duplicateResult.value = data
    } else {
      notify.error(data.message || '查重失败')
    }
  } catch (error: any) {
    console.error('Find duplicates failed:', error)
    if (error.code === 'ECONNABORTED') {
      notify.error('查重超时，请检查规则集配置是否正确')
    } else {
      notify.error(error.response?.data?.message || '查重失败，请稍后重试')
    }
  } finally {
    duplicateLoading.value = false
  }
}

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

// 监听视图模式切换，重新初始化拖拽
watch(viewMode, () => {
  nextTick(() => {
  })
})

// 监听规则类型变化，自动设置 no_resolve 默认值
watch(() => ruleForm.value.rule_type, (newType) => {
  // 切换规则类型时，自动设置 no_resolve（IP 类型默认开启）
  const isIpType = ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP'].includes(newType || '')
  ruleForm.value.no_resolve = isIpType
})

// 监听规则集类型变化，自动设置 no_resolve 默认值
watch(() => ruleSetForm.value.behavior, (newBehavior) => {
  // 切换 behavior 时，自动设置 no_resolve（ipcidr 类型默认开启）
  ruleSetForm.value.no_resolve = newBehavior === 'ipcidr'
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
    await api.post('/rules/reorder', { rule_configs: rebuilt })
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
    notify.error('保存顺序失败，顺序已还原')
  }
}

onMounted(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()]).then(() => {
  })
})

onUnmounted(() => {
  if (sortableInstance) {
    sortableInstance.destroy()
    sortableInstance = null
  }
})

// 页面激活时重新加载数据（从其他页面返回时）
onActivated(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()])
})
</script>

