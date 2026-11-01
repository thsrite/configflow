<template>
  <div ref="libraryPage" :class="reorder.active.value && 'cf-reordering'">
    <PageHeader title="规则库">
      <template #actions>
        <Button @click="showAddDialog">
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
          <DropdownMenuContent align="end">
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
        description="添加规则集链接或粘贴规则内容，即可在策略规则中选择使用。"
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
            <SelectTrigger class="h-9 w-[140px] border-input bg-card text-[13px] dark:border-transparent">
              <SelectValue placeholder="全部类型" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部类型</SelectItem>
              <SelectItem v-for="b in behaviorOptions" :key="b" :value="b">{{ b }}</SelectItem>
            </SelectContent>
          </Select>

          <label
            class="flex min-h-9 max-md:min-h-11 cursor-pointer flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border border-border/50 bg-background/40 px-3 py-1 text-[12.5px] text-muted-foreground"
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

        <template v-if="selectedRules.length" #actions>
          <Button v-if="selectedRules.length" variant="outline" size="sm" @click="showReferenceDialog(selectedRules)">
            <Send class="size-3.5" />加入当前配置
          </Button>
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
            {{ caching ? '下载中…' : '批量下载内容' }}
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

      <Alert v-if="cacheRequestError || Object.keys(cacheFailures).length" class="mb-4" data-cache-errors>
        <AlertDescription>
          <p class="m-0 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] font-semibold text-foreground">
            规则集内容更新失败
            <RouterLink to="/system-settings?tab=tasks" class="inline-flex items-center text-[12px] font-normal text-primary-accent underline underline-offset-4 max-md:min-h-11">在定时任务中查看</RouterLink>
          </p>
          <p v-if="cacheRequestError" class="mt-2 whitespace-pre-wrap break-all text-destructive-accent">{{ cacheRequestError }}</p>
          <ul v-if="Object.keys(cacheFailures).length" class="m-0 mt-2 list-none divide-y divide-border p-0">
            <li v-for="failure in cacheFailures" :key="failure.id" :data-cache-failure="failure.id" class="min-w-0 py-2 first:pt-0 last:pb-0">
              <p class="m-0 break-all text-[13px] font-medium text-foreground">{{ failure.name }}</p>
              <p class="m-0 mt-1 text-xs" :class="failure.cache_retained ? 'text-warning-accent' : 'text-destructive-accent'">
                {{ failure.cache_retained ? (failure.cache_updated_at ? `已保留 ${absoluteTime(failure.cache_updated_at)} 的有效缓存，继续使用原有内容。` : '已保留上一次有效缓存，继续使用原有内容。') : '没有可用的有效缓存，请修正规则来源或类型后重试。' }}
              </p>
              <p class="m-0 mt-1 whitespace-pre-wrap break-all text-xs text-foreground">{{ failure.error }}</p>
            </li>
          </ul>
        </AlertDescription>
      </Alert>

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
            :data-library-rule-id="rule.id"
            :data-highlighted="highlightedRuleId === rule.id || undefined"
            :tabindex="highlightedRuleId === rule.id ? -1 : undefined"
            data-reorder-item
            :class="[
              !rule.enabled && 'dark:opacity-55',
              highlightedRuleId === rule.id && 'bg-primary-soft hover:bg-primary-soft outline-2 -outline-offset-2 outline-primary-accent'
            ]"
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
            <TableCell class="max-w-[260px]">
              <div class="flex min-w-0 items-center gap-2">
                <span
                  class="size-1.5 shrink-0 rounded-full"
                  :class="rule.enabled
                    ? 'bg-success-accent shadow-[0_0_6px_var(--success-accent)]'
                    : 'bg-muted-foreground'"
                  aria-hidden="true"
                />
                <span class="min-w-0 truncate font-medium text-foreground" :title="rule.name">{{ rule.name }}</span>
                <span v-if="highlightedRuleId === rule.id" class="shrink-0 text-[11px] text-primary-accent">已定位</span>
              </div>
            </TableCell>
            <TableCell>
              <Badge variant="outline" class="font-mono text-[10.5px]">{{ rule.behavior }}</Badge>
            </TableCell>
            <TableCell class="max-w-[320px]">
              <span
                v-if="rule.source_type === 'content'"
                class="block truncate font-mono text-[12px] text-muted-foreground"
                :title="getContentPreview(rule.content)"
              >
                {{ getContentPreview(rule.content) }}
              </span>
              <a
                v-else
                class="block truncate font-mono text-[12px] text-info-accent hover:underline"
                :href="rule.url"
                :title="rule.url"
                target="_blank"
                rel="noreferrer"
              >
                {{ rule.url }}
              </a>
            </TableCell>
            <TableCell class="cf-reorder-mute text-right">
              <div class="flex items-center justify-end gap-0.5">
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
                <Button variant="ghost" size="icon-sm" title="加入当前配置" :aria-label="`将 ${rule.name} 加入当前配置`" @click="showReferenceDialog([rule.id])">
                  <Send class="size-4" />
                </Button>
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
        <div
          v-for="(rule, cfIndex) in visibleRules"
          :key="rule.id"
          class="cf-list-in"
          :style="listItemStyle(cfIndex)"
          :data-id="rule.id"
          :data-library-rule-id="rule.id"
          :data-highlighted="highlightedRuleId === rule.id || undefined"
          :tabindex="highlightedRuleId === rule.id ? -1 : undefined"
          data-reorder-item
          :class="[
            'relative flex flex-col gap-3 overflow-hidden rounded-[18px] border bg-card/90 p-4 shadow-surface transition-colors duration-200 hover:border-border-strong max-sm:gap-2 max-sm:p-3',
            selectedRules.includes(rule.id) ? 'border-primary-accent' : 'border-border',
            !rule.enabled && 'dark:opacity-60',
            highlightedRuleId === rule.id && 'outline-2 -outline-offset-2 outline-primary-accent'
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
            <span v-if="highlightedRuleId === rule.id" class="shrink-0 text-[11px] text-primary-accent">已定位</span>
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
              class="m-0 max-h-24 overflow-auto rounded-lg border border-border/50 bg-background/50 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all text-muted-foreground"
            >{{ getContentPreview(rule.content) }}</pre>
            <a
              v-else
              class="block truncate rounded-lg border border-border/50 bg-background/50 px-2.5 py-1.5 font-mono text-[11.5px] text-info-accent hover:underline"
              :href="rule.url"
              :title="rule.url"
              target="_blank"
              rel="noreferrer"
            >
              {{ rule.url }}
            </a>
          </div>

          <footer class="cf-reorder-mute mt-auto flex items-center gap-0.5 border-0 border-t border-border/50 pt-3 max-sm:pt-2">
            <Button
              v-if="rule.source_type === 'content'"
              variant="ghost"
              size="sm"
              :aria-label="`向 ${rule.name} 添加规则`"
              title="添加规则"
              @click="showAddRuleToSetDialog(rule)"
            >
              <Plus class="size-3.5" />
              <span class="max-sm:hidden">添加规则</span>
            </Button>
            <Button variant="ghost" size="icon-sm" title="加入当前配置" :aria-label="`将 ${rule.name} 加入当前配置`" @click="showReferenceDialog([rule.id])">
              <Send class="size-4" />
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
        </div>
      </div>
    </template>

    <Dialog v-model:open="referenceDialogVisible">
      <DialogContent class="max-h-[calc(100dvh-2rem)] max-w-[520px] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>加入配置</DialogTitle>
          <DialogDescription class="break-all">将 {{ referenceRuleIds.length }} 个规则集加入「{{ referenceProfileName }}」，已添加的会跳过。内容仍与规则库共用，后续修改会影响使用它的所有配置。</DialogDescription>
        </DialogHeader>
        <div class="flex min-w-0 flex-col gap-4">
          <div class="flex flex-col gap-2">
            <Label for="reference-policy">目标策略</Label>
            <Select v-model="referencePolicy">
              <SelectTrigger id="reference-policy" class="w-full min-w-0 bg-background/50 [&>span]:truncate"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="policy in referencePolicies" :key="policy" :value="policy" :title="policy"><span class="break-all">{{ policy }}</span></SelectItem>
              </SelectContent>
            </Select>
          </div>
          <RuleDnsField
            v-if="referenceDnsShown"
            :model-value="referenceDnsValue"
            :hint="referenceDnsHint"
            :help="referenceDnsHelp"
            :persistent-hint="referenceDnsDisabled"
            :warnings="referenceDnsWarnings"
            :upstreams="referenceDns?.upstreams"
            :upstream-label="referenceUpstreamLabel"
            :policy-route="referencePolicyRoute"
            :default-forward="referenceDns?.engine.default_forward"
            :disabled="referenceDnsDisabled || addingReferences"
            @update:model-value="chooseReferenceDns"
            @action="kind => { if (kind === 'follow') chooseReferenceDns('follow') }"
          />
        </div>
        <DialogFooter>
          <Button variant="outline" :disabled="addingReferences" @click="referenceDialogVisible = false">取消</Button>
          <Button :disabled="addingReferences" @click="addReferences">{{ addingReferences ? '添加中…' : '加入配置' }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 添加 / 编辑规则集 ===== -->
    <Dialog :open="dialogVisible" @update:open="value => { if (!savingRule) dialogVisible = value }">
      <DialogContent class="max-w-[700px]" @escape-key-down="event => { if (savingRule) event.preventDefault() }" @interact-outside="event => { if (savingRule) event.preventDefault() }">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑规则' : '添加规则' }}</DialogTitle>
          <DialogDescription>修改来源或内容会影响使用此规则集的所有配置，但不会改变各配置的策略、顺序或启用状态。</DialogDescription>
        </DialogHeader>

        <fieldset :disabled="savingRule" class="cf-focus-gutter m-0 flex max-h-[62dvh] min-w-0 flex-col gap-4 overflow-y-auto border-0 p-0">
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
                    ? 'border-primary-accent bg-primary-soft text-primary-accent dark:border-primary-accent/40 dark:bg-primary-soft/50 dark:text-foreground'
                    : 'border-input bg-card text-muted-foreground hover:border-border-strong dark:border-border/50 dark:bg-background/40'
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
            <RuleContentIssues :check="contentCheck" />
          </div>

          <div class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select v-model="form.behavior">
              <SelectTrigger class="w-full bg-background/50">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="domain">Domain</SelectItem>
                <SelectItem value="ipcidr">IP CIDR</SelectItem>
                <SelectItem value="classical">Classical</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex flex-col gap-1.5">
            <Label for="lib-format">内容格式</Label>
            <Select v-model="selectedRuleFormat">
              <SelectTrigger id="lib-format" class="w-full bg-background/50" aria-label="内容格式"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">自动识别</SelectItem>
                <SelectItem value="text">Text · 每行一条</SelectItem>
                <SelectItem value="yaml">YAML · payload 列表</SelectItem>
                <SelectItem v-if="form.source_type === 'url'" value="mrs">MRS · 二进制规则</SelectItem>
              </SelectContent>
            </Select>
            <p v-if="form.source_type === 'content'" class="m-0 text-xs text-muted-foreground">Text 每行一条；YAML 使用 payload 列表。自动识别会按内容保存格式。</p>
          </div>

          <p v-if="form.behavior === 'classical'" class="m-0 text-xs leading-relaxed text-muted-foreground">{{ CLASSICAL_CONTENT_HINT }}</p>

          <Alert v-if="saveError" class="border-destructive-accent/30" data-rule-save-error>
            <AlertDescription class="whitespace-pre-wrap break-all text-destructive-accent">{{ saveError }}</AlertDescription>
          </Alert>

          <div class="flex items-center gap-2.5">
            <Switch
              id="lib-enabled"
              v-model="form.enabled"
            />
            <Label for="lib-enabled" class="text-[13px] text-muted-foreground">
              {{ form.enabled ? '规则启用中' : '规则已停用' }}
            </Label>
          </div>
        </fieldset>

        <DialogFooter>
          <Button variant="outline" :disabled="savingRule" @click="dialogVisible = false">取消</Button>
          <Button :disabled="savingRule" @click="saveRule">{{ savingRule ? '保存中…' : '保存' }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 向规则集添加规则 ===== -->
    <Dialog :open="addRuleToSetDialogVisible" @update:open="value => { if (!savingRule) addRuleToSetDialogVisible = value }">
      <DialogContent class="max-w-[600px]" @escape-key-down="event => { if (savingRule) event.preventDefault() }" @interact-outside="event => { if (savingRule) event.preventDefault() }">
        <DialogHeader>
          <DialogTitle>添加规则到 {{ currentRuleSet?.name }}</DialogTitle>
          <DialogDescription>{{ addRuleToSetHelperText }}</DialogDescription>
        </DialogHeader>

        <fieldset :disabled="savingRule" class="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
          <div v-if="currentRuleSet?.behavior === 'domain'" class="flex flex-col gap-1.5">
            <Label for="add-domain-scope">匹配范围</Label>
            <Select v-model="addRuleToSetForm.rule_type" :disabled="savingRule">
              <SelectTrigger id="add-domain-scope" class="w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="DOMAIN">仅此域名</SelectItem>
                <SelectItem value="DOMAIN-SUFFIX">此域名及其子域名</SelectItem>
                <SelectItem value="PATTERN">按输入内容（含通配符）</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div v-if="currentRuleSet?.behavior === 'classical'" class="flex flex-col gap-1.5">
            <Label>规则类型</Label>
            <Select v-model="addRuleToSetForm.rule_type">
              <SelectTrigger class="w-full bg-background/50 font-mono">
                <SelectValue placeholder="选择规则类型" />
              </SelectTrigger>
              <SelectContent>
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
              :placeholder="currentRuleSet?.behavior === 'ipcidr' ? '192.0.2.0/24\n2001:db8::/64' : 'example.com\napi.example.org'"
            />
          </div>
          <RuleContentIssues :check="appendContentCheck" />
          <p v-if="currentRuleSet?.behavior === 'classical'" class="m-0 text-xs leading-relaxed text-muted-foreground">{{ CLASSICAL_CONTENT_HINT }}</p>
          <div v-if="addRulePreview" class="flex min-w-0 flex-col gap-1.5">
            <Label>将写入的内容</Label>
            <pre class="m-0 max-h-36 overflow-auto rounded-lg bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">{{ addRulePreview }}</pre>
          </div>
        </fieldset>

        <DialogFooter>
          <Button variant="outline" :disabled="savingRule" @click="addRuleToSetDialogVisible = false">取消</Button>
          <Button :disabled="savingRule" @click="saveRuleToSet">{{ savingRule ? '添加中…' : '添加' }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 批量导入 ===== -->
    <Dialog :open="batchImportDialogVisible" @update:open="value => { if (!batchImporting) batchImportDialogVisible = value }">
      <DialogContent class="max-w-[700px]" @escape-key-down="event => { if (batchImporting) event.preventDefault() }" @interact-outside="event => { if (batchImporting) event.preventDefault() }">
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
            :disabled="batchImporting"
            placeholder="粘贴 rule-providers 配置内容…"
          />
          <ul v-if="batchImportFailures.length" role="alert" class="m-0 max-h-40 list-none space-y-1 overflow-y-auto p-0 text-xs text-destructive-accent">
            <li v-for="failure in batchImportFailures" :key="failure" class="whitespace-pre-wrap break-words">{{ failure }}</li>
          </ul>
        </div>

        <DialogFooter>
          <Button variant="outline" :disabled="batchImporting" @click="batchImportDialogVisible = false">取消</Button>
          <Button :disabled="batchImporting" @click="processBatchImport">
            <Upload class="size-4" />
            {{ batchImporting ? '导入中…' : '导入' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== GitHub 代理域名配置 ===== -->
    <Dialog v-model:open="proxyConfigDialogVisible">
      <DialogContent class="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>GitHub 代理域名配置</DialogTitle>
          <DialogDescription>
            面板下载 GitHub 资源时先尝试原始地址，失败后使用此备用地址。支持 https://gh-proxy.com 这样的镜像前缀；生成配置中的 GitHub 链接也会使用此设置。
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
import { ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useSettingLink, queryText, focusSetting } from '@/lib/settingLink'
import { load as loadYaml } from 'js-yaml'
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
  Send,
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
import { useReorder } from '@/composables/useReorder'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { listItemStyle } from '@/lib/motion'
import { useProfileStore } from '@/stores/profile'
import api, { ruleApi, tasksApi } from '@/api'
import { absoluteTime } from '@/lib/scheduledTasks'
import { activeProfileId, getActiveProfileId } from '@/profileContext'
import { isAxiosError } from 'axios'
import { detectRuleContentFormat, domainRuleLine } from '@/lib/ruleset-format'
import { CLASSICAL_CONTENT_HINT, prepareRuleContent, type RuleContentCheck } from '@/lib/ruleset-content'
import RuleDnsField from '@/components/rules/RuleDnsField.vue'
import RuleContentIssues from '@/components/rules/RuleContentIssues.vue'
import {
  DNS_COPY, DNS_FIELD_HELP, PINNED_HINT, confirmSaveWithoutDns, copyKey, defaultDnsChoice, dnsErrorReason, dnsWarnings,
  followRoute, isRoutingLocked, isUpstreamValue, notifyRuleSaved, pinnedWithheld
} from '@/lib/ruleDns'
import type { ComposePayload, RuleDnsSnapshot, RuleDnsValue } from '@/types'

const errorMessage = (error: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(error) ? error.response?.data?.message || fallback : error instanceof Error ? error.message : fallback


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
  format?: string
  /** 规则库来源内容是否含 IP 条目；OxiDNS 只使用其中的域名部分 */
  has_ip_part?: boolean
}

const libraryPayload = (item: Partial<RuleLibraryItem>): Partial<RuleLibraryItem> => ({
  id: item.id,
  name: item.name,
  behavior: item.behavior,
  format: item.format,
  enabled: item.enabled,
  source_type: item.source_type || 'url',
  url: item.source_type === 'content' ? '' : item.url,
  content: item.source_type === 'content' ? item.content : ''
})

const referenceDialogVisible = ref(false)
const referenceRuleIds = ref<string[]>([])
const referenceProfileId = ref('')
const referenceProfileName = ref('')
const referencePolicy = ref('DIRECT')
const referencePolicies = ref<string[]>([])
const addingReferences = ref(false)

// 目标配置空间的规则 DNS 状态（GET /rules/dns）；读取失败时不修改 DNS（默认上游）。
const referenceDns = ref<RuleDnsSnapshot | null>(null)
const referenceDnsChoice = ref<RuleDnsValue>('default')
const referenceDnsTouched = ref(false)

const showReferenceDialog = async (ids: string[]) => {
  const profileId = getActiveProfileId()
  const profileName = cfProfileName.value
  try {
    const [{ data }, dns] = await Promise.all([
      api.get<{ name: string }[]>('/proxy-groups', { headers: { 'X-ConfigFlow-Profile': profileId } }),
      ruleApi.getDns(profileId).then(response => response.data, () => null)
    ])
    referenceProfileId.value = profileId
    referenceProfileName.value = profileName
    referenceRuleIds.value = [...ids]
    referencePolicy.value = 'DIRECT'
    referencePolicies.value = ['DIRECT', 'REJECT', ...data.map(group => group.name)]
    referenceDns.value = dns?.engine ? dns : null
    referenceDnsTouched.value = false
    referenceDnsChoice.value = referenceDefaultDns.value
    referenceDialogVisible.value = true
  } catch (error) {
    notify.error(errorMessage(error, '加载配置策略失败'))
  }
}

const referenceItems = computed(() => referenceRuleIds.value.flatMap(id => ruleLibrary.value.filter(item => item.id === id)))
const referenceHasIp = computed(() => referenceItems.value.some(item => item.has_ip_part ?? ['ipcidr', 'classical'].includes(item.behavior)))
const referenceEngine = computed(() => referenceDns.value?.engine ?? null)
/** 不使用 OxiDNS 的配置空间不显示 DNS；读取失败时仍显示，说明本次不修改 DNS */
const referenceDnsShown = computed(() => !referenceEngine.value || referenceEngine.value.oxidns_in_use)
const referenceHasDomain = computed(() => referenceItems.value.some(item => item.behavior !== 'ipcidr'))
const referenceDnsDisabled = computed(() => !referenceEngine.value || !referenceEngine.value.editable || !referenceHasDomain.value)
const referenceDnsValue = computed<RuleDnsValue>(() => (referenceDnsDisabled.value ? 'default' : referenceDnsChoice.value))
const referencePolicyRoute = computed(() => followRoute({ itemType: 'ruleset', policy: referencePolicy.value }, referenceEngine.value?.policy_routes))
const referenceDefaultDns = computed(() => defaultDnsChoice({
  domain: referenceItems.value.some(item => item.behavior !== 'ipcidr'), ip: referenceHasIp.value
}, referenceEngine.value))
const referenceUpstreamLabel = computed(() => {
  const value = referenceDnsValue.value
  return isUpstreamValue(value) ? referenceDns.value?.upstreams.find(item => item.key === value.slice(9))?.label : undefined
})
const referenceDnsWarnings = computed(() => dnsWarnings({
  engine: referenceEngine.value,
  unloaded: !referenceEngine.value,
  value: referenceDnsValue.value,
  policy: referencePolicy.value,
  policyRoute: referencePolicyRoute.value,
  // 新加入的规则集不在 MosDNS 列表中，MosDNS 按默认上游处理。
  mosdns: referenceEngine.value?.agents.some(agent => agent.service_type === 'mosdns') ? { route: null } : null,
  explainExplicit: referenceDnsTouched.value,
  upstreamLabel: referenceUpstreamLabel.value
}).filter(warning => warning.action !== 'resync'))
const referenceDnsHint = computed(() => (!referenceHasDomain.value ? 'IP 规则集只用于连接分流，不参与 DNS' : isUpstreamValue(referenceDnsValue.value)
  ? PINNED_HINT : DNS_COPY[copyKey(referenceDnsValue.value)].hint))
const referenceDnsHelp = computed(() => `${DNS_FIELD_HELP}\n\n${DNS_COPY[copyKey(referenceDnsValue.value)].help}`)
function chooseReferenceDns(value: RuleDnsValue) {
  referenceDnsTouched.value = true
  referenceDnsChoice.value = value
}

const addReferences = async () => {
  addingReferences.value = true
  const profileId = referenceProfileId.value
  const requestConfig = { headers: { 'X-ConfigFlow-Profile': profileId } }
  let added = 0
  try {
    const { data } = await api.get<{ itemType?: string; library_rule_id?: string }[]>('/rules', requestConfig)
    const usedIds = new Set(data.filter(item => item.itemType === 'ruleset').map(item => item.library_rule_id))
    const pending = referenceRuleIds.value.filter(id => !usedIds.has(id))
    const skipped = referenceRuleIds.value.length - pending.length
    const ipcidr = (id: string) => ruleLibrary.value.find(item => item.id === id)?.behavior === 'ipcidr'
    // 共享来源、规则引用与 DNS 由 compose 一次写入；IP 网段规则集默认 no-resolve，与其他类型各提交一次。
    const batches = [true, false].map(noResolve => ({ noResolve, ids: pending.filter(id => ipcidr(id) === noResolve) }))
      .filter(batch => batch.ids.length)
    let dns: RuleDnsValue | undefined = referenceDnsShown.value && referenceValueSendable() ? referenceDnsValue.value : undefined
    let pinned = false
    const warnings: string[] = []
    for (const [index, batch] of batches.entries()) {
      const payload = (choice?: RuleDnsValue): ComposePayload => ({
        source: { kind: 'library', library_rule_ids: batch.ids },
        rule: { policy: referencePolicy.value, remark: '', group_name: '', no_resolve: batch.noResolve, enabled: true, position: 'top' },
        ...(choice && !batch.noResolve ? { dns: choice } : {})
      })
      let response
      try {
        response = await ruleApi.compose(payload(dns), profileId)
      } catch (error) {
        if (!dns || !isRoutingLocked(error)) throw error
        if (!(await confirmSaveWithoutDns(error))) {
          // 已取消的批次没有加入：如实说明，并只保留它们的选择以便重试
          const cancelled = batches.slice(index).flatMap(item => item.ids)
          if (added) {
            notifyAdded(added, skipped, warnings, pinned, cancelled.length)
            referenceRuleIds.value = cancelled
          }
          return
        }
        dns = undefined
        response = await ruleApi.compose(payload(), profileId)
      }
      added += response.data.data.rules.length
      warnings.push(...(response.data.warnings || []))
      pinned ||= pinnedWithheld(response.data, dns !== undefined && isUpstreamValue(dns))
    }
    notifyAdded(added, skipped, warnings, pinned)
    referenceDialogVisible.value = false
  } catch (error) {
    const reason = dnsErrorReason(error) || errorMessage(error, '添加失败')
    notify.error(added ? `已添加 ${added} 个规则集；${reason}` : reason)
  } finally {
    addingReferences.value = false
  }
}
/** 字段可用且值不是默认上游时才发送 dns；默认上游不写 OxiDNS */
const referenceValueSendable = () => !referenceDnsDisabled.value && referenceDnsValue.value !== 'default'
function notifyAdded(added: number, skipped: number, warnings: string[], pinned: boolean, cancelled = 0) {
  const title = cancelled
    ? `已向「${referenceProfileName.value}」添加 ${added} 个规则集，未添加 ${cancelled} 个（已取消）${skipped ? `，跳过 ${skipped} 个已添加的规则集` : ''}`
    : `已向「${referenceProfileName.value}」添加 ${added} 个规则集，跳过 ${skipped} 个已添加的规则集`
  if (!added) {
    notify.success(title)
    return
  }
  notifyRuleSaved(title, { agents: referenceEngine.value?.agents, scopes: ['library'], pinned, extra: warnings, warn: cancelled > 0 })
}

const route = useRoute()
const libraryPage = ref<HTMLElement | null>(null)
const libraryLoaded = ref(false)
const linkedRuleId = computed(() => typeof route?.query.rule === 'string' ? route.query.rule : '')
const highlightedRuleId = ref('')
const ruleLibrary = ref<RuleLibraryItem[]>([])
const dialogVisible = ref(false)
const batchImportDialogVisible = ref(false)
const addRuleToSetDialogVisible = ref(false)
const proxyConfigDialogVisible = ref(false)
const batchImportText = ref('')
const batchImporting = ref(false)
const batchImportFailures = ref<string[]>([])
const batchImportResults = new Map<string, 'saved' | 'pending'>()
const isEdit = ref(false)
const testing = ref(false)
interface RuleCacheResult {
  id: string
  name: string
  success: boolean
  error?: string
  cache_retained?: boolean
  cache_updated_at?: string | null
}
const caching = ref(false) // 批量缓存加载状态
const cacheFailures = ref<Record<string, RuleCacheResult>>({})
const cacheRequestError = ref('')
const savingRule = ref(false)
const saveError = ref('')
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

const selectedRuleFormat = computed({
  get: () => form.value.format || 'auto',
  set: (value: string) => { form.value.format = value === 'auto' ? undefined : value }
})

const contentCheck = ref<RuleContentCheck | null>(null)
const appendContentCheck = ref<RuleContentCheck | null>(null)
watch(() => [form.value.content, form.value.behavior, form.value.format, dialogVisible.value], () => { contentCheck.value = null })

const ruleContentPlaceholder = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return [
        '请输入域名规则，每行一条规则',
        '示例：',
        '+.example.com',
        'api.github.com'
      ].join('\n')
    case 'ipcidr':
      return [
        '请输入 IP 规则，每行一条规则',
        '示例：',
        '192.168.0.0/16',
        '10.0.0.0/8',
        '2001:db8::/32'
      ].join('\n')
    default:
      return [
        '请输入规则内容，每行一条规则',
        '示例：',
        'DOMAIN-SUFFIX,example.com',
        'IP-CIDR,192.0.2.0/24',
        'PROCESS-NAME,Google Chrome'
      ].join('\n')
  }
})

const ruleContentHelperText = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return 'example.com 仅匹配此域名；+.example.com 包含它的所有子域名。两种写法可混用，每行一条，不加 DOMAIN 等前缀。'
    case 'ipcidr':
      return '支持 IPv4/IPv6 CIDR 写法，例如 192.168.0.0/16 或 2001:db8::/32。'
    default:
      return '每行一条，支持完整规则，例如 DOMAIN,example.com、DOMAIN-SUFFIX,example.com 或 IP-CIDR,192.0.2.0/24；不支持嵌套 RULE-SET。'
  }
})

const addRuleToSetForm = ref({
  rule_type: 'DOMAIN-SUFFIX',
  value: ''
})

watch(() => [addRuleToSetForm.value.value, addRuleToSetForm.value.rule_type, addRuleToSetDialogVisible.value], () => { appendContentCheck.value = null })

const addRuleToSetHelperText = computed(() => {
  switch (currentRuleSet.value?.behavior) {
    case 'domain':
      return '每行一个域名，由下方选择是否包含子域名；已有通配符可选择「按输入内容」。'
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
    libraryLoaded.value = true
    const existingIds = new Set(ruleLibrary.value.map(rule => rule.id))
    for (const id of Object.keys(cacheFailures.value)) {
      if (!existingIds.has(id)) delete cacheFailures.value[id]
    }
  } catch (error) {
    notify.error('加载规则库失败')
  }
  await loadRefreshFailures()
}

// 定时更新与批量下载的结果保存在服务端，刷新页面后仍显示失败原因与是否在用旧缓存。
const loadRefreshFailures = async () => {
  try {
    const { data } = await tasksApi.list()
    for (const task of data.tasks || []) {
      if (task.category !== 'rule_source') continue
      if (task.last_status === 'failed' && !task.running) {
        cacheFailures.value[task.resource_id] = {
          id: task.resource_id, name: task.name, success: false, error: task.last_error || '更新失败，请重试。',
          cache_retained: Boolean(task.cache_retained), cache_updated_at: task.cache_updated_at
        }
      } else {
        delete cacheFailures.value[task.resource_id]
      }
    }
  } catch {
    // 读取失败时保留本页已有的结果，不额外提示。
  }
}

const showAddDialog = () => {
  saveError.value = ''
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
  saveError.value = ''
  isEdit.value = true
  form.value = libraryPayload(row)
  dialogVisible.value = true
}

const isFullUrl = (url: string): boolean => {
  if (!url) return false
  return url.startsWith('http://') || url.startsWith('https://')
}

const performSaveRule = async () => {
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
    const checked = await prepareRuleContent({ ...form.value, format: form.value.format || detectRuleContentFormat(form.value.content) }, undefined, check => { contentCheck.value = check })
    if (checked === null) return
    form.value.content = checked
  }

  const payload = { ...libraryPayload(form.value), ...(form.value.source_type === 'content' ? { format: form.value.format || detectRuleContentFormat(form.value.content || '') } : {}) }
  try {
    if (isEdit.value) {
      await api.put(`/rule-library/${form.value.id}`, payload)
      notify.success('共享来源已更新，各配置的策略、顺序和启用状态保持不变')
    } else {
      await api.post('/rule-library', payload)
      notify.success('添加成功')
    }
    if (form.value.id) delete cacheFailures.value[form.value.id]
    dialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    saveError.value = errorMessage(error, '保存失败')
    notify.error(saveError.value)
  }
}

const saveRule = async () => {
  if (savingRule.value) return
  savingRule.value = true
  saveError.value = ''
  try { await performSaveRule() }
  catch (error) {
    saveError.value = errorMessage(error, '检查规则内容失败')
    notify.error(saveError.value)
  }
  finally { savingRule.value = false }
}

const deleteRule = async (row: RuleLibraryItem) => {
  const ok = await confirmDanger('确定删除此规则集吗？删除后无法恢复。仍在配置中使用的规则集无法删除，请先从对应配置中移除。', { title: '删除共享规则集' })
  if (!ok) return
  try {
    await api.delete(`/rule-library/${row.id}`)
    selectedRules.value = selectedRules.value.filter(id => id !== row.id)
    notify.success('删除成功')
    await loadRuleLibrary()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

const handleToggle = async (rule: RuleLibraryItem) => {
  rule.enabled = !rule.enabled
  await toggleEnabled(rule)
}

const toggleEnabled = async (row: RuleLibraryItem) => {
  try {
    await api.put(`/rule-library/${row.id}`, libraryPayload(row))
    notify.success(row.enabled ? '共享来源已开启，各配置启用状态保持不变' : '共享来源已关闭，各配置启用状态保持不变')
  } catch (error) {
    notify.error(errorMessage(error, '更新失败'))
    row.enabled = !row.enabled
  }
}

const showAddRuleToSetDialog = (row: RuleLibraryItem) => {
  currentRuleSet.value = { ...row }
  addRuleToSetForm.value = {
    rule_type: row.behavior === 'ipcidr' ? 'IP-CIDR' : row.behavior === 'domain' ? 'DOMAIN' : 'DOMAIN-SUFFIX',
    value: ''
  }
  addRuleToSetDialogVisible.value = true
}

const formatRuleValueForRuleSet = (value: string) => {
  const trimmedValue = value.trim()

  if (currentRuleSet.value?.behavior !== 'ipcidr') {
    return domainRuleLine(currentRuleSet.value?.behavior, addRuleToSetForm.value.rule_type, trimmedValue)
  }

  return trimmedValue
}

const addRulePreview = computed(() => addRuleToSetForm.value.value.split('\n').filter(value => value.trim()).map(formatRuleValueForRuleSet).join('\n'))
const performSaveRuleToSet = async () => {
  if (!currentRuleSet.value) {
    return
  }

  if (!addRuleToSetForm.value.value || !addRuleToSetForm.value.value.trim()) {
    notify.warning('请输入规则值')
    return
  }

  const values = addRuleToSetForm.value.value.trim().split('\n').filter(line => line.trim())
  if (currentRuleSet.value.behavior === 'domain' && addRuleToSetForm.value.rule_type !== 'PATTERN'
      && values.some(value => /[+*]/.test(value) || value.trim().startsWith('.'))) {
    notify.warning('使用通配符时请选择「按输入内容」，或只填写域名并在上方选择匹配范围。')
    return
  }
  const newRules = values.map(formatRuleValueForRuleSet)

  try {
    const checked = await prepareRuleContent(currentRuleSet.value, newRules, check => { appendContentCheck.value = check })
    if (checked === null) return
    const updatedRule = {
      ...libraryPayload(currentRuleSet.value),
      content: checked
    }

    await api.put(`/rule-library/${currentRuleSet.value.id}`, updatedRule)
    notify.success('添加成功')
    addRuleToSetDialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  }
}

const saveRuleToSet = async () => {
  if (savingRule.value) return
  savingRule.value = true
  try { await performSaveRuleToSet() }
  finally { savingRule.value = false }
}
const showBatchImportDialog = () => {
  if (batchImporting.value) return
  batchImportText.value = ''
  batchImportFailures.value = []
  batchImportResults.clear()
  batchImportDialogVisible.value = true
}

const processBatchImport = async () => {
  if (batchImporting.value) return
  const text = batchImportText.value.trim()
  if (!text) {
    notify.warning('请粘贴配置内容')
    return
  }
  batchImportFailures.value = []
  batchImporting.value = true
  try {
    const rules = parseRuleProviders(text)
    let successCount = 0
    let unconfirmedCount = 0
    for (const rule of rules) {
      const key = JSON.stringify([rule.name, rule.url, rule.behavior, rule.format])
      const previous = batchImportResults.get(key)
      if (previous === 'saved') { successCount++; continue }
      if (previous === 'pending') {
        unconfirmedCount++
        batchImportFailures.value.push(`${rule.name}：保存结果待确认，请核对规则库，勿重复导入。`)
        continue
      }
      try {
        await api.post('/rule-library', rule)
        batchImportResults.set(key, 'saved')
        successCount++
      } catch (error) {
        if (isAxiosError<{ confirmation_required?: boolean }>(error) && error.response?.data?.confirmation_required) {
          batchImportResults.set(key, 'pending')
          unconfirmedCount++
          batchImportFailures.value.push(`${rule.name}：保存结果待确认，请核对规则库，勿重复导入。`)
        } else {
          batchImportFailures.value.push(`${rule.name}：${errorMessage(error, '导入失败，请重试。')}`)
        }
      }
    }

    const failCount = rules.length - successCount - unconfirmedCount
    if (unconfirmedCount > 0) notify.warning(`批量导入：成功 ${successCount} 条，失败 ${failCount} 条，保存结果待确认 ${unconfirmedCount} 条；请核对列表，勿重复导入`)
    else if (failCount === 0) notify.success(`成功导入 ${successCount} 条规则`)
    else if (successCount === 0) notify.error(`批量导入失败，${failCount} 条规则均未导入`)
    else notify.warning(`批量导入完成：成功 ${successCount} 条，失败 ${failCount} 条`)
    if (!batchImportFailures.value.length) batchImportDialogVisible.value = false
    await loadRuleLibrary()
  } catch (error) {
    const reason = error instanceof Error ? error.message : '解析配置失败，请检查格式'
    batchImportFailures.value = [reason]
    notify.error(reason)
  } finally {
    batchImporting.value = false
  }
}

const parseRuleProviders = (text: string): RuleLibraryItem[] => {
  const document = loadYaml(text)
  const mapping = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
  if (!mapping(document)) throw new Error('请填写 rule-providers 对象，每个规则集需包含 URL 和类型。')
  const providers = document['rule-providers'] ?? document
  if (!mapping(providers) || !Object.keys(providers).length) throw new Error('未解析到有效的规则配置。')
  const rules: RuleLibraryItem[] = []
  const errors: string[] = []
  for (const [name, item] of Object.entries(providers)) {
    if (!mapping(item)) { errors.push(`${name}：规则集配置必须是对象。`); continue }
    const behavior = item.behavior ?? 'classical'
    if (typeof behavior !== 'string' || !['domain', 'ipcidr', 'classical'].includes(behavior)) {
      errors.push(`${name}：不支持的类型 ${String(behavior)}；请选择 domain、ipcidr 或 classical。`)
      continue
    }
    if (item.type !== undefined && item.type !== 'http') { errors.push(`${name}：批量导入仅支持 http URL 来源。`); continue }
    if (typeof item.url !== 'string' || !isFullUrl(item.url)) { errors.push(`${name}：请填写有效的 HTTP 或 HTTPS URL。`); continue }
    if (item.format !== undefined && item.format !== null && item.format !== '' && (typeof item.format !== 'string' || !['text', 'yaml', 'mrs'].includes(item.format))) {
      errors.push(`${name}：不支持的格式 ${String(item.format)}；请选择 text、yaml 或 mrs。`)
      continue
    }
    rules.push({ id: `lib_${Date.now()}_${rules.length}`, name, url: item.url, behavior,
      ...(typeof item.format === 'string' && item.format ? { format: item.format } : {}), source_type: 'url', enabled: true })
  }
  if (errors.length) throw new Error(errors.join('\n'))
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


      if (failedCount > 0) {
        notify.warning(`测试完成！成功: ${successCount}，失败: ${failedCount}。不可用的共享来源已关闭，各配置启用状态保持不变。`)
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
    `将下载选中的 ${selectedRules.value.length} 个规则集，并按所选类型校验。内容不匹配时不转换；更新失败会保留上一次有效缓存，启用状态不变。是否继续？`,
    { title: '批量下载规则集内容' }
  )
  if (!ok) return

  try {
    caching.value = true
    cacheRequestError.value = ''

    const { data } = await api.post('/rule-library/cache', {
      rule_ids: selectedRules.value
    })

    if (data.success) {
      const successCount = data.success_count
      const failedCount = data.failed_count
      const totalCount = data.total_count

      const results: RuleCacheResult[] = data.results || []
      for (const result of results) {
        if (result.success) delete cacheFailures.value[result.id]
        else cacheFailures.value[result.id] = { ...result, error: result.error || '更新失败，请重试。' }
      }
      // 保留失败项的选择，便于修正后重试；其他来源的失败详情继续可见。
      selectedRules.value = results.filter(result => !result.success).map(result => result.id)

      if (failedCount > 0) {
        notify.warning(`下载完成：成功 ${successCount} 个，失败 ${failedCount} 个。失败详情与缓存状态见下方，启用状态不变。`)
      } else {
        notify.success(`已下载 ${totalCount} 个规则集的内容。`)
      }

      await loadRuleLibrary()
    } else {
      cacheRequestError.value = data.message || '下载规则集内容失败'
      notify.error(cacheRequestError.value)
    }
  } catch (error) {
    cacheRequestError.value = errorMessage(error, '下载规则集内容失败')
    notify.error(cacheRequestError.value)
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
    await copyText(url)
    notify.success('URL已复制到剪贴板')
  } catch {
    notify.error('复制失败，请手动选择并复制地址')
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

// 共享删除由服务端检查所有配置，不级联删除配置引用。
const batchDeleteRules = async () => {
  if (selectedRules.value.length === 0) return
  const ok = await confirmDanger(`确定删除选中的 ${selectedRules.value.length} 个规则集吗？删除后无法恢复。仍在配置中使用的规则集会保留。`, { title: '批量删除共享规则集' })
  if (!ok) return
  const failedIds: string[] = []
  const failures: string[] = []
  let deleted = 0
  for (const id of selectedRules.value) {
    try {
      await api.delete(`/rule-library/${id}`)
      deleted++
    } catch (error) {
      failedIds.push(id)
      const name = ruleLibrary.value.find(item => item.id === id)?.name || id
      failures.push(`${name}: ${errorMessage(error, '删除失败')}`)
    }
  }
  selectedRules.value = failedIds
  if (failures.length) notify.error(`已删除 ${deleted} 个，保留 ${failedIds.length} 个。${failures.join('；')}`)
  else notify.success(`已删除 ${deleted} 个共享规则集`)
  await loadRuleLibrary()
}


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
  ruleLibrary.value.length === 0 ? '添加规则集后，即可加入配置使用。' : '试试其他关键词，或调整类型筛选。'
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

// 链接按共享来源 ID 定位；首次请求失败时只显示加载错误，不误报条目已删除。
let reportedMissingRuleId = ''
watch([linkedRuleId, libraryLoaded, effectiveView], async ([id, loaded], _previous, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  highlightedRuleId.value = ''
  if (!id) {
    reportedMissingRuleId = ''
    return
  }
  if (!loaded) return
  if (!ruleLibrary.value.some(rule => rule.id === id)) {
    if (reportedMissingRuleId !== id) notify.warning('未找到对应的规则集，可能已被删除')
    reportedMissingRuleId = id
    return
  }
  reportedMissingRuleId = ''
  if (!visibleRules.value.some(rule => rule.id === id)) {
    keyword.value = ''
    behaviorFilter.value = 'all'
  }
  highlightedRuleId.value = id
  await nextTick()
  if (cancelled) return
  const target = Array.from(libraryPage.value?.querySelectorAll<HTMLElement>('[data-library-rule-id]') || [])
    .find(element => element.dataset.libraryRuleId === id)
  target?.focus({ preventScroll: true })
  target?.scrollIntoView?.({ block: 'center', behavior: 'auto' })
}, { immediate: true })

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    notify.success('共享规则库顺序已保存，各配置规则顺序保持不变')
  } catch (error) {
    notify.error('保存顺序失败，顺序已还原')
  }
}

useSettingLink({
  keys: ['rule', 'section'], ready: () => libraryLoaded.value,
  matches: query => !!queryText(query.rule) && query.section === 'settings',
  open: async query => {
    if (dialogVisible.value || batchImportDialogVisible.value || savingRule.value) {
      notify.warning('请先保存或关闭正在编辑的规则集。'); return
    }
    const rule = ruleLibrary.value.find(item => item.id === queryText(query.rule))
    if (!rule) { notify.warning('对应规则集不存在或已删除。'); return }
    editRule(rule)
    await nextTick()
    await focusSetting(document.getElementById('lib-url'))
  },
})

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
