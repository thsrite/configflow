<template>
  <div class="proxy-groups-page" :class="reorder.active.value && 'cf-reordering'">

    <PageHeader
      title="策略组"
      class="shrink-0"
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

    <div class="shrink-0">
    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />
    </div>

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

    <div v-else class="group-workspace grid min-h-0 grid-cols-[280px_minmax(0,1fr)] gap-3.5 max-[1180px]:grid-cols-[minmax(0,1fr)]">
      <Button
        v-show="!reorder.active.value"
        variant="outline"
        class="hidden h-auto min-h-11 w-full min-w-0 justify-between gap-3 bg-card px-3 py-2.5 max-[1180px]:flex"
        aria-haspopup="dialog"
        :aria-label="`切换策略组，当前${selected?.name || '未选择'}`"
        :aria-expanded="groupPickerOpen"
        @click="groupPickerOpen = true"
      >
        <span class="min-w-0 truncate text-left">{{ selected?.name || '选择策略组' }}</span>
        <span class="ml-auto shrink-0 text-xs text-muted-foreground">{{ proxyGroups.length }} 个策略组</span>
        <ChevronDown class="size-4 shrink-0" />
      </Button>
      <!-- 左：策略组列表 -->
      <SectionCard :padded="false" class="group-list-card flex min-h-0 flex-col p-2" :class="!reorder.active.value && 'max-[1180px]:hidden'">
        <div ref="groupsContainer" class="group-list min-h-0 flex-1 overflow-y-auto overscroll-contain [scrollbar-gutter:stable]" role="list" aria-label="策略组">
          <div
            v-for="(group, cfIndex) in proxyGroups"
            :key="group.id || group.name"
            :data-name="group.name"
            data-reorder-item
            role="listitem"
            class="group/row mb-0.5 flex items-center gap-1"
          >
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
            <button
              type="button"
              :title="group.name"
              :aria-current="selectedKey === keyOf(group) ? 'true' : undefined"
              :class="cn(
                'flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 rounded-xl px-3 py-[11px] text-left transition-colors focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
                selectedKey === keyOf(group) ? 'bg-secondary shadow-[inset_0_0_0_1px_var(--border-strong)]' : 'hover:bg-secondary',
                group.enabled === false && 'opacity-55'
              )"
              @click="selectGroup(group)"
            >
              <span
                :class="cn(
                  'grid size-8 shrink-0 place-items-center rounded-[9px] font-mono text-[10px]',
                  selectedKey === keyOf(group) ? 'bg-primary-soft text-primary-accent' : 'bg-background/70 text-muted-foreground'
                )"
              >
                {{ group.follow_group ? 'FOL' : String(group.type || 'sel').slice(0, 3).toUpperCase() }}
              </span>
              <span class="min-w-0 flex-1">
                <b class="block truncate text-[13px] font-semibold">{{ group.name }}</b>
                <span class="font-mono text-[11px] text-muted-foreground">
                  {{ group.follow_group ? '跟随' : getGroupTypeLabel(group.type) }}
                </span>
              </span>
              <span class="num font-mono text-[12px] text-muted-foreground" :title="'命中节点数'">
                {{ matchCounts[keyOf(group)] ?? '' }}
              </span>
            </button>
            <Button
              v-if="!reorder.active.value"
              variant="ghost"
              size="icon-sm"
              class="shrink-0 opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 max-md:opacity-100"
              :title="`编辑 ${group.name}`"
              @click="editGroup(group)"
            >
              <Pencil class="size-3.5" aria-hidden="true" />
              <span class="sr-only">编辑</span>
            </Button>
            <Button
              v-if="!reorder.active.value"
              variant="ghost"
              size="icon-sm"
              class="shrink-0 text-destructive-accent opacity-0 transition-opacity group-hover/row:opacity-100 hover:bg-destructive-soft focus-visible:opacity-100 max-md:opacity-100"
              :title="`删除 ${group.name}`"
              @click="deleteGroup(group)"
            >
              <Trash2 class="size-3.5" aria-hidden="true" />
              <span class="sr-only">删除</span>
            </Button>
          </div>
        </div>
      </SectionCard>

      <!-- 右：来源 → 筛选 → 命中节点 -->
      <SectionCard v-if="selected" :padded="false" class="group-detail-card flex min-h-0 min-w-0 flex-col" :class="reorder.active.value && 'max-[1180px]:hidden'" role="region" aria-label="策略组详情">
        <header class="flex shrink-0 flex-wrap items-center gap-2.5 border-b border-border px-[18px] py-4">
          <h2 class="m-0 min-w-0 break-words text-[13.5px] font-semibold [overflow-wrap:anywhere]">{{ selected.name }}</h2>
          <span v-if="selected.follow_group" class="chip chip-warn">跟随</span>
          <span v-else class="chip chip-acc font-mono">{{ selected.type }}</span>
          <div class="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              :class="selected.enabled ? 'text-success-accent' : 'text-muted-foreground'"
              :title="selected.enabled ? '停用' : '启用'"
              :aria-label="selected.enabled ? `停用 ${selected.name}` : `启用 ${selected.name}`"
              :disabled="selected.id ? savingStatus[selected.id] : false"
              @click="handleToggle(selected)"
            >
              <component :is="selected.enabled ? Eye : EyeOff" class="size-4" />
            </Button>
            <Button variant="ghost" size="sm" @click="editGroup(selected)">
              <Pencil class="size-3.5" />
              编辑
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              class="text-destructive-accent hover:bg-destructive-soft"
              :aria-label="`删除 ${selected.name}`"
              title="删除"
              @click="deleteGroup(selected)"
            >
              <Trash2 class="size-4" />
            </Button>
          </div>
        </header>

        <div ref="groupDetailsBody" class="group-detail-body min-h-0 flex-1 overflow-y-auto overscroll-contain [scrollbar-gutter:stable]">
        <!-- 代理链：前置 → 落地，没有来源与正则 -->
        <div v-if="selected.type === 'chain'" class="flex flex-col gap-3 p-5 max-md:p-4">
          <div class="flex flex-wrap items-center gap-2 text-[13px]">
            <template v-if="selected.chain">
              <span class="chip font-mono">{{ entryLabel(selected.chain.entry) }}</span>
              <span class="pipe-link w-10" aria-hidden="true" />
              <span class="chip chip-acc font-mono">{{ entryLabel(selected.chain.exit) }}</span>
            </template>
            <span v-else class="text-muted-foreground">链配置不可用</span>
          </div>
          <p class="m-0 text-[12.5px] text-muted-foreground">
            流量先经过前置，再由落地访问目标。仅 Mihomo 支持，不能导出为 Surge。
          </p>
        </div>

        <div v-else class="pipe grid grid-cols-[minmax(0,0.9fr)_34px_minmax(0,1.1fr)_34px_minmax(0,1.5fr)] items-stretch p-5 max-md:grid-cols-[minmax(0,1fr)] max-md:gap-2 max-md:p-4">
          <!-- 来源 -->
          <div class="flex min-w-0 flex-col gap-2.5">
            <div class="font-mono text-[10.5px] tracking-[0.14em] text-muted-foreground uppercase">来源</div>
            <div class="flex flex-1 flex-col gap-2 rounded-[14px] border border-border bg-background/60 p-3">
              <p v-if="selected.follow_group" class="m-0 text-[12.5px] text-muted-foreground">
                跟随「<b class="text-foreground">{{ getFollowGroupName(selected.follow_group) || '—' }}</b>」，节点与其保持一致。
              </p>
              <template v-else>
                <label
                  v-for="option in sourceOptions"
                  :key="option.id"
                  :class="cn(
                    'flex cursor-pointer items-center gap-2 rounded-[10px] border border-border bg-card px-2.5 py-2 text-[12.5px] transition-opacity',
                    !draft.sources.includes(option.id) && 'opacity-50'
                  )"
                >
                  <Checkbox
                    :model-value="draft.sources.includes(option.id)"
                    :disabled="draftSaving"
                    @update:model-value="toggleDraftSource(option.id)"
                  />
                  <span class="min-w-0 flex-1 truncate">{{ option.name }}</span>
                  <span class="font-mono text-[11px] text-muted-foreground">{{ option.count ?? '' }}</span>
                </label>
                <p v-if="!sourceOptions.length" class="m-0 text-[12px] text-muted-foreground">
                  {{ draft.kind === 'aggregation' ? '没有可用的聚合' : '还没有订阅' }}
                </p>
                <div v-if="extraSources.length" class="flex flex-wrap gap-1.5 pt-1">
                  <span v-for="extra in extraSources" :key="extra" class="chip">{{ extra }}</span>
                </div>
              </template>
            </div>
          </div>

          <div class="pipe-link" aria-hidden="true" />

          <!-- 筛选 -->
          <div class="flex min-w-0 flex-col gap-2.5">
            <div class="font-mono text-[10.5px] tracking-[0.14em] text-muted-foreground uppercase">筛选</div>
            <div class="flex flex-1 flex-col gap-2 rounded-[14px] border border-border bg-background/60 p-3">
              <div class="relative">
                <span class="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 font-mono text-primary-accent">/</span>
                <Input
                  v-model="draft.regex"
                  class="pl-7 font-mono text-[13px]"
                  spellcheck="false"
                  placeholder="不填则全部节点"
                  :disabled="!!selected.follow_group || draftSaving"
                  aria-label="正则过滤"
                />
              </div>
              <p class="m-0 min-h-4 text-[11.5px] text-destructive-accent">{{ preview.error }}</p>
              <div class="flex items-baseline gap-2">
                <b class="font-display text-[38px] leading-none font-medium"><AnimatedNumber :value="preview.nodes.length" /></b>
                <span class="font-mono text-[11.5px] text-muted-foreground">/ {{ preview.total }} 节点</span>
              </div>
              <div class="mt-auto rounded-xl border border-dashed border-border-strong p-3 text-[12.5px] text-muted-foreground">
                <b class="text-foreground">{{ selected.follow_group ? '跟随' : getGroupTypeLabel(selected.type) }}</b>
                · {{ strategyText }}
                <template v-if="winner">
                  <br />{{ selected.type === 'select' ? '默认选中' : '按测速预计生效' }}：
                  <b class="text-primary-accent">{{ winner.name }}</b>
                </template>
              </div>
            </div>
          </div>

          <div class="pipe-link" aria-hidden="true" />

          <!-- 命中节点 -->
          <div class="flex min-w-0 flex-col gap-2.5">
            <div class="font-mono text-[10.5px] tracking-[0.14em] text-muted-foreground uppercase">
              命中节点
              <Loader2 v-if="preview.loading" class="ml-1 inline size-3 animate-spin" />
            </div>
            <div class="flex flex-1 flex-col gap-2 rounded-[14px] border border-border bg-background/60 p-3">
              <div ref="matchedNodesContainer" class="flex max-h-[260px] flex-wrap content-start gap-1.5 overflow-auto">
                <span
                  v-for="(node, i) in preview.nodes"
                  :key="node.name"
                  :class="cn(
                    'matched-node inline-flex min-h-7 max-w-full items-start gap-1.5 rounded-lg border border-border bg-card px-2.5 py-1 text-[12px]',
                    winner && winner.name === node.name && 'is-winner'
                  )"
                  :style="{ animationDelay: `${Math.min(i, 24) * 18}ms` }"
                >
                  <span class="min-w-0 [overflow-wrap:anywhere]">{{ node.name }}</span>
                  <i class="shrink-0 font-mono text-[10.5px] not-italic text-muted-foreground">{{ latencyLabel(node.name) }}</i>
                </span>
                <span v-if="!preview.nodes.length && !preview.loading" class="text-[12.5px] text-muted-foreground">
                  {{ selected.follow_group ? '跟随策略组不单独筛选节点。' : draft.sources.length ? '没有节点命中这个正则。' : '先选择至少一个来源。' }}
                </span>
              </div>
            </div>
          </div>
        </div>
        </div>

        <footer
          v-if="draftDirty && selected.type !== 'chain'"
          class="flex shrink-0 flex-wrap items-center gap-2 border-t border-border px-5 py-3 text-[12.5px] text-muted-foreground"
        >
          来源或正则有改动，保存后进入配置
          <Button variant="ghost" size="sm" class="ml-auto min-h-11" :disabled="draftSaving" @click="resetDraft">还原</Button>
          <Button size="sm" class="min-h-11" :disabled="draftSaving || !!preview.error" @click="saveDraft()">
            <Loader2 v-if="draftSaving" class="size-3.5 animate-spin" />
            保存
          </Button>
        </footer>
      </SectionCard>
    </div>

    <Dialog v-model:open="groupPickerOpen">
      <DialogContent class="max-w-[480px]">
        <DialogHeader>
          <DialogTitle>选择策略组</DialogTitle>
          <DialogDescription>搜索名称，选择要查看的策略组。</DialogDescription>
        </DialogHeader>
        <Input v-model="groupPickerSearch" placeholder="搜索策略组名称…" aria-label="搜索策略组名称" class="min-h-11" />
        <div class="max-h-[50dvh] overflow-y-auto overscroll-contain" role="list" aria-label="搜索结果">
          <div v-for="group in filteredPickerGroups" :key="keyOf(group)" role="listitem">
            <button
              type="button"
              :aria-current="selectedKey === keyOf(group) ? 'true' : undefined"
              :class="cn('mb-1 flex min-h-11 w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none', selectedKey === keyOf(group) ? 'bg-secondary' : 'hover:bg-secondary')"
              @click="groupPickerOpen = false; selectGroup(group)"
            >
              <span class="min-w-0 flex-1 break-words text-sm [overflow-wrap:anywhere]">{{ group.name }}</span>
              <span class="shrink-0 text-xs text-muted-foreground">{{ selectedKey === keyOf(group) ? '当前' : getGroupTypeLabel(group.type) }}</span>
            </button>
          </div>
          <p v-if="!filteredPickerGroups.length" class="py-6 text-center text-sm text-muted-foreground">没有匹配的策略组</p>
        </div>
      </DialogContent>
    </Dialog>

    <!-- ===== 新增 / 编辑策略组 ===== -->
    <Dialog v-model:open="dialogVisible">
      <DialogContent class="max-w-[720px]">
        <DialogHeader>
          <DialogTitle>{{ isEdit ? '编辑策略组' : '添加策略组' }}</DialogTitle>
          <DialogDescription>选择订阅、节点或聚合，或把前置和落地组合成代理链。这里只修改当前配置，不会修改共用的节点。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[64dvh] flex-col gap-4 overflow-y-auto">
          <div class="flex flex-col gap-1.5">
            <Label for="group-name">名称</Label>
            <Input id="group-name" v-model="form.name" class="bg-background/50" placeholder="请输入策略组名称" />
          </div>

          <div v-if="!enabledSources.includes('follow')" class="flex flex-col gap-1.5">
            <Label>类型</Label>
            <Select :model-value="form.type" @update:model-value="changeGroupType">
              <SelectTrigger data-testid="group-type" class="w-full bg-background/50">
                <SelectValue placeholder="请选择策略组类型" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="select">手动选择 (Select)</SelectItem>
                <SelectItem value="url-test">自动测速 (URL-Test)</SelectItem>
                <SelectItem value="fallback">故障转移 (Fallback)</SelectItem>
                <SelectItem value="load-balance">负载均衡 (Load-Balance)</SelectItem>
                <SelectItem value="chain">代理链（仅 Mihomo）</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <template v-if="form.type === 'chain' && form.chain">
            <p class="text-xs leading-relaxed text-muted-foreground">流量先经过前置，再由落地访问目标；代理链名称就是新出口的名称。前置使用原策略组当前选中的节点；落地若选策略组，会为此链单独创建一组。订阅和聚合中的节点会随更新变化。原节点和策略组仍可单独使用。仅支持 Mihomo，不能导出为 Surge。</p>
            <div class="flex min-w-0 flex-col gap-1.5">
              <Label for="chain-search">搜索节点或策略组</Label>
              <Input id="chain-search" v-model="chainSearch" placeholder="搜索名称" />
              <Label>前置节点或策略组</Label>
              <Select v-model="chainEntrySelection">
                <SelectTrigger data-testid="chain-entry" class="data-[size=default]:h-auto min-h-9 w-full min-w-0 [&_[data-slot=select-value]]:line-clamp-none"><SelectValue class="min-w-0 whitespace-normal break-all text-left">{{ chainEntryLabel }}</SelectValue></SelectTrigger>
                <SelectContent class="max-w-[calc(100vw-32px)]">
                  <SelectItem v-for="candidate in chainEntryOptions" :key="candidate.value" :value="candidate.value" class="whitespace-normal break-all">{{ candidate.label }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="flex min-w-0 flex-col gap-1.5">
              <Label>落地节点或策略组</Label>
              <Select v-model="chainExitSelection">
                <SelectTrigger data-testid="chain-exit" class="data-[size=default]:h-auto min-h-9 w-full min-w-0 [&_[data-slot=select-value]]:line-clamp-none"><SelectValue class="min-w-0 whitespace-normal break-all text-left">{{ chainExitLabel }}</SelectValue></SelectTrigger>
                <SelectContent class="max-w-[calc(100vw-32px)]">
                  <SelectItem v-for="candidate in chainExitOptions" :key="candidate.value" :value="candidate.value" class="whitespace-normal break-all">{{ candidate.label }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <p v-if="chainUnavailable" role="alert" class="text-xs text-destructive-accent">所选节点或策略组已不存在、已停用，或会造成循环使用，请重新选择。原有选择不会自动清除。</p>
            <p class="text-xs text-muted-foreground">两端可选手动节点、策略组（包括使用订阅、聚合或跟随的组）和其他代理链，但不能循环使用。前置必须启用。代理链停用时可保留已停用的落地节点或策略组；启用代理链前，也需启用落地。</p>
          </template>

          <template v-if="form.type !== 'chain'">
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
                    ? 'border-primary-accent bg-primary-soft text-primary-accent dark:border-primary-accent/40 dark:bg-primary-soft/50 dark:text-foreground'
                    : 'border-input bg-card text-muted-foreground hover:border-border-strong dark:border-border/50 dark:bg-background/40'
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
              <SelectContent>
                <SelectItem v-for="group in followStrategies" :key="group.id" :value="group.id">
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
            <p class="m-0 text-[12px] text-muted-foreground">加入所选聚合筛选后的节点；还可用下方正则表达式继续筛选。</p>
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
              <SelectContent>
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
              <SelectContent>
                <SelectItem value="unset">未设置</SelectItem>
                <SelectItem value="true">是</SelectItem>
                <SelectItem value="false">否</SelectItem>
              </SelectContent>
            </Select>
          </div>
          </template>

          <p v-if="formError" role="alert" class="break-all text-sm text-destructive-accent">{{ formError }}</p>

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
      <DialogContent class="max-w-[680px]">
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
            <Badge variant="outline" class="min-w-0 max-w-[45%] whitespace-normal break-all text-[10px]">{{ getPreviewSourceLabel(node) }}</Badge>
            <Badge variant="success" class="shrink-0 font-mono text-[10px]">
              {{ (node.type || 'unknown').toUpperCase() }}
            </Badge>
          </div>
          <EmptyState
            v-if="!regexPreviewNodes.length"
            :icon="Filter"
            title="当前正则没有匹配到节点"
            description="试着放宽筛选条件，或检查所选订阅、聚合中是否有节点。"
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
  Loader2,
  Network,
  Pencil,
  Plus,
  Scale,
  Share2,
  Timer,
  Trash2
} from '@lucide/vue'
import { Motion } from 'motion-v'
import { cn } from '@/lib/utils'
import AnimatedNumber from '@/components/common/AnimatedNumber.vue'
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
import { getActiveProfileId } from '@/profileContext'
import type { ProxyGroup, ProxyNode, Subscription } from '@/types'
import api from '@/api'
import Sortable from 'sortablejs'


const profileId = getActiveProfileId()
const profileRequest = { headers: { 'X-ConfigFlow-Profile': profileId } }
const proxyGroups = ref<ProxyGroup[]>([])
const nodes = ref<ProxyNode[]>([])
const subscriptions = ref<Subscription[]>([])
const aggregations = ref<any[]>([])
const savingStatus = ref<Record<string, boolean>>({})
const dialogVisible = ref(false)
const isEdit = ref(false)
const enabledSources = ref<string[]>([])
const formError = ref('')
const chainSearch = ref('')
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
const groupDetailsBody = ref<HTMLElement | null>(null)
const matchedNodesContainer = ref<HTMLElement | null>(null)
const groupPickerOpen = ref(false)
const groupPickerSearch = ref('')
const filteredPickerGroups = computed(() => {
  const query = groupPickerSearch.value.trim().toLocaleLowerCase()
  return proxyGroups.value.filter(group => !query || group.name.toLocaleLowerCase().includes(query))
})
watch(groupPickerOpen, open => { if (open) groupPickerSearch.value = '' })
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
const followStrategies = computed(() => proxyGroups.value.filter(group => group.id !== form.value.id && group.type !== 'chain'))
type ChainEndpoint = NonNullable<ProxyGroup['chain']>['entry']
const entryValue = (entry: ChainEndpoint) => JSON.stringify({ type: entry.type, id: entry.id })
const entryLabel = (entry: ChainEndpoint) => {
  const target = entry.type === 'node' ? nodes.value.find(node => node.id === entry.id) : proxyGroups.value.find(group => group.id === entry.id)
  return `${entry.type === 'node' ? '节点' : '策略组'} · ${target?.name || `${entry.id}（不可用）`}`
}
const chainSummary = (group: ProxyGroup) => group.chain
  ? `${entryLabel(group.chain.entry)} → ${entryLabel(group.chain.exit)}`
  : '链配置不可用'
const validChainEndpoint = (endpoint: ChainEndpoint, allowDisabled = false, seen = new Set<string>()): boolean => {
  if (endpoint.type === 'node') {
    return nodes.value.some(node => node.id === endpoint.id && !node.subscription_id && (allowDisabled || node.enabled !== false))
  }
  if (endpoint.id === form.value.id || seen.has(endpoint.id)) return false
  const group = proxyGroups.value.find(group => group.id === endpoint.id)
  if (!group || (!allowDisabled && group.enabled === false)) return false
  const next = new Set(seen).add(endpoint.id)
  if (group.type === 'chain') {
    return !!group.chain && validChainEndpoint(group.chain.entry, false, next) && validChainEndpoint(group.chain.exit, allowDisabled && group.enabled === false, next)
  }
  const groupIds = new Set([
    ...(group.include_groups || []),
    ...(group.follow_group ? [group.follow_group] : []),
    ...(group.proxies_order || []).filter(member => member.type === 'strategy').map(member => member.id)
  ])
  const nodeIds = new Set([
    ...(group.manual_nodes || []),
    ...(group.proxies_order || []).filter(member => member.type === 'node').map(member => member.id)
  ])
  return [...groupIds].every(id => validChainEndpoint({ type: 'group', id }, allowDisabled, next)) &&
    [...nodeIds].every(id => ['DIRECT', 'REJECT'].includes(id) || validChainEndpoint({ type: 'node', id }, allowDisabled, next))
}
const chainCandidates = (side: 'entry' | 'exit') => {
  const allowDisabled = side === 'exit' && form.value.enabled === false
  const other = form.value.chain?.[side === 'entry' ? 'exit' : 'entry']
  const endpoints: ChainEndpoint[] = [
    ...nodes.value.map(node => ({ type: 'node' as const, id: node.id })),
    ...proxyGroups.value.map(group => ({ type: 'group' as const, id: group.id }))
  ]
  return endpoints.filter(endpoint => validChainEndpoint(endpoint, allowDisabled) &&
    !(endpoint.type === 'node' && other?.type === 'node' && endpoint.id === other.id))
    .map(endpoint => ({ value: entryValue(endpoint), label: entryLabel(endpoint) }))
}
const chainEntryCandidates = computed(() => chainCandidates('entry'))
const chainExitCandidates = computed(() => chainCandidates('exit'))
const matchesChainSearch = (candidate: { label: string }) => candidate.label.toLocaleLowerCase().includes(chainSearch.value.trim().toLocaleLowerCase())
const chainEntryOptions = computed(() => chainEntryCandidates.value.filter(matchesChainSearch))
const chainExitOptions = computed(() => chainExitCandidates.value.filter(matchesChainSearch))
const chainEntrySelection = computed({
  get: () => form.value.chain?.entry.id ? entryValue(form.value.chain.entry) : '',
  set: (value: string) => { if (form.value.chain) form.value.chain.entry = JSON.parse(value) }
})
const chainExitSelection = computed({
  get: () => form.value.chain?.exit.id ? entryValue(form.value.chain.exit) : '',
  set: (value: string) => { if (form.value.chain) form.value.chain.exit = JSON.parse(value) }
})
const chainEntryLabel = computed(() => form.value.chain?.entry.id ? entryLabel(form.value.chain.entry) : '选择前置节点或策略组')
const chainExitLabel = computed(() => form.value.chain?.exit.id ? entryLabel(form.value.chain.exit) : '选择落地节点或策略组')
const chainUnavailable = computed(() => !!form.value.chain && (
  (!!form.value.chain.entry.id && !chainEntryCandidates.value.some(candidate => candidate.value === chainEntrySelection.value)) ||
  (!!form.value.chain.exit.id && !chainExitCandidates.value.some(candidate => candidate.value === chainExitSelection.value))
))
const changeGroupType = (value: unknown) => {
  if (typeof value !== 'string' || value === form.value.type) return
  formError.value = ''
  const oldType = form.value.type
  if (value === 'chain') {
    const { id, name, enabled } = form.value
    form.value = { id, name, enabled, type: 'chain', chain: { entry: { type: 'node', id: '' }, exit: { type: 'node', id: '' } } }
    enabledSources.value = []
  } else if (oldType === 'chain') {
    const { id, name, enabled } = form.value
    form.value = { id, name, enabled, type: value, subscriptions: [], manual_nodes: [], aggregations: [], include_groups: [], proxies_order: [] }
    updateTypeDefaults(value, oldType)
  } else {
    delete form.value.chain
    form.value.type = value
    updateTypeDefaults(value, oldType)
  }
}

const availableAggregations = computed(() => {
  // 只返回已开启的聚合,包含正则过滤器信息
  return aggregations.value
    .filter(a => a.enabled !== false)
    .map(a => ({ id: a.id, name: a.name, regex_filter: a.regex_filter }))
})

// MultiSelect 使用 value/label，复用现有候选列表以保留内置节点及过滤规则。
const nodeOptions = computed(() =>
  availableNodes.value.map(node => ({ value: node.id, label: node.name }))
)
const aggregationOptions = computed(() =>
  availableAggregations.value.map(aggregation => ({ value: aggregation.id, label: aggregation.name }))
)
const strategyOptions = computed(() =>
  availableStrategies.value.map(strategy => ({ value: strategy.id, label: strategy.name }))
)

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

// 仅用户切换类型时调整默认参数，编辑回填不改写已保存值。
const updateTypeDefaults = (newType: string, oldType?: string) => {
  if (!oldType) return
  if (newType === 'chain') return
  if (oldType === 'chain') {
    if (newType && ['url-test', 'fallback', 'load-balance'].includes(newType)) Object.assign(form.value, findPreviousSameTypeConfig(newType))
    return
  }

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
}

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
  if (form.value.type === 'chain') return
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
  if (form.value.type === 'chain') return
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
    'chain': '代理链',
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
    const { data } = await proxyGroupApi.previewRegex(payload, profileId)

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
    const { data } = await proxyGroupApi.previewRegex(payload, profileId)

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
    const { data } = await proxyGroupApi.getAll(profileId)

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
        await api.post('/proxy-groups/reorder', { groups: fixedData }, profileRequest)
        console.log('已自动修复缺少ID的策略组')
      } catch (error) {
        console.error('保存修复后的策略组失败:', error)
      }
    }
  } catch (error) {
    notify.error('加载策略组列表失败')
  }
}

const loadResources = async () => {
  try {
    const [nodeRows, subscriptionRows, aggregationRows] = await Promise.all([
      nodeApi.getAll(), api.get('/subscriptions'), api.get('/aggregations')
    ])
    nodes.value = nodeRows.data
    subscriptions.value = subscriptionRows.data
    aggregations.value = aggregationRows.data
  } catch {
    notify.error('加载订阅、节点或聚合失败')
  }
}

const showAddDialog = () => {
  formError.value = ''
  chainSearch.value = ''
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
    await proxyGroupApi.update(group.id, group, profileId)
    notify.success(group.enabled ? '已启用' : '已禁用')
  } catch (error: any) {
    notify.error(error?.response?.data?.message || '更新状态失败')
    group.enabled = previous
    loadProxyGroups()
  } finally {
    savingStatus.value[group.id] = false
  }
}

const editGroup = (row: ProxyGroup) => {
  formError.value = ''
  chainSearch.value = ''
  if (row.type === 'chain') {
    isEdit.value = true
    enabledSources.value = []
    form.value = { id: row.id, name: row.name, type: 'chain', enabled: row.enabled, chain: row.chain ? { entry: { ...row.chain.entry }, exit: { ...row.chain.exit } } : { entry: { type: 'node', id: '' }, exit: { type: 'node', id: '' } } }
    dialogVisible.value = true
    return
  }
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
  let proxies_order = row.proxies_order ? row.proxies_order.map(item => ({ ...item })) : []
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
  formError.value = ''
  if (!form.value.name?.trim()) {
    formError.value = '请输入策略组名称'
    notify.warning(formError.value)
    return
  }
  if (form.value.type === 'chain' && (!form.value.chain?.entry.id || !form.value.chain.exit.id || chainUnavailable.value)) {
    formError.value = '请选择可用的前置和落地节点或策略组，且不能循环使用'
    return
  }

  // 验证至少选择了一种来源
  const hasSubscriptions = form.value.subscriptions && form.value.subscriptions.length > 0
  const hasManualNodes = form.value.manual_nodes && form.value.manual_nodes.length > 0
  const hasAggregations = form.value.aggregations && form.value.aggregations.length > 0
  const hasIncludeGroups = form.value.include_groups && form.value.include_groups.length > 0
  const hasFollowGroup = form.value.follow_group !== undefined && form.value.follow_group !== null && form.value.follow_group !== ''

  if (form.value.type !== 'chain' && !hasSubscriptions && !hasManualNodes && !hasAggregations && !hasIncludeGroups && !hasFollowGroup) {
    formError.value = '请至少选择一种节点来源（订阅、节点、聚合、策略或跟随）'
    notify.warning(formError.value)
    return
  }

  // 跟随模式验证
  if (hasFollowGroup && !form.value.follow_group) {
    notify.warning('跟随模式下请选择要跟随的策略组')
    return
  }

  try {
    // 准备保存的数据
    const saveData = { ...form.value, name: form.value.name.trim() }

    if (saveData.type !== 'chain') {
      delete saveData.chain

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
    }

    if (isEdit.value) {
      // Server atomically updates the group and all policy references.
      // 使用 id 进行API调用
      await proxyGroupApi.update(saveData.id!, saveData, profileId)
      notify.success('更新成功')
    } else {
      await proxyGroupApi.create(saveData, profileId)
      notify.success('添加成功')
    }
    dialogVisible.value = false
    loadProxyGroups()
  } catch (error: any) {
    formError.value = error?.response?.data?.message || '保存失败'
    notify.error(formError.value)
  }
}

const deleteGroup = async (row: ProxyGroup) => {
  if (!row.id) {
    notify.error('策略组信息不完整，无法删除，请刷新后重试')
    console.error('策略组数据异常，缺少ID字段:', row)
    return
  }

  try {
    const ok = await confirmDanger('确定从当前配置中删除该策略组吗？删除后无法恢复。若规则或其他策略组仍在使用它，请先修改这些设置。', { title: '删除策略组' })
    if (!ok) return
    await proxyGroupApi.delete(row.id, profileId)
    notify.success('删除成功')
    loadProxyGroups()
  } catch (error: any) {
    notify.error(error?.response?.data?.message || '删除失败')
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
    }, profileRequest)
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

/* ================================================================
 * 来源 → 筛选 → 命中节点：选中策略组的实时管道
 * ================================================================ */
const keyOf = (group: ProxyGroup) => group.id || group.name
const selectedKey = ref<string>('')
const selected = computed(() => proxyGroups.value.find(g => keyOf(g) === selectedKey.value) || null)

type SourceKind = 'subscription' | 'aggregation'
const draft = ref<{ kind: SourceKind; sources: string[]; regex: string }>({
  kind: 'subscription',
  sources: [],
  regex: ''
})

/** 有聚合引用的策略组按聚合筛选，否则按订阅筛选，与生成配置时的取数口径一致 */
const kindOf = (group: ProxyGroup): SourceKind =>
  ((group as any).aggregations || []).length ? 'aggregation' : 'subscription'

const draftFrom = (group: ProxyGroup) => {
  const kind = kindOf(group)
  return {
    kind,
    sources: [...((kind === 'aggregation' ? (group as any).aggregations : group.subscriptions) || [])],
    regex: (kind === 'aggregation' ? (group as any).aggregation_regex : group.regex) || ''
  }
}

let selectionSwitchPending = false
let selectionVersion = 0

const applyGroupSelection = async (group: ProxyGroup | null): Promise<boolean> => {
  const version = ++selectionVersion
  ++previewSeq
  clearTimeout(previewTimer)
  preview.value = { nodes: [], total: 0, loading: false, error: '' }
  selectedKey.value = group ? keyOf(group) : ''
  draft.value = group ? draftFrom(group) : { kind: 'subscription', sources: [], regex: '' }
  groupPickerOpen.value = false
  await nextTick()
  if (!viewActive || version !== selectionVersion) return false
  if (groupDetailsBody.value) groupDetailsBody.value.scrollTop = 0
  if (matchedNodesContainer.value) matchedNodesContainer.value.scrollTop = 0
  return true
}

const selectGroup = async (group: ProxyGroup): Promise<boolean> => {
  if (!viewActive || selectionSwitchPending || draftSaving.value) return false
  const targetKey = keyOf(group)
  if (targetKey === selectedKey.value) {
    groupPickerOpen.value = false
    return true
  }
  if (!proxyGroups.value.some(item => keyOf(item) === targetKey)) return false

  selectionSwitchPending = true
  const version = selectionVersion
  const isCurrent = () => viewActive && version === selectionVersion
  try {
    if (selected.value && draftDirty.value) {
      const choice = await choose(`「${selected.value.name}」的来源或正则尚未保存。`, {
        title: '切换策略组前保存改动？',
        confirmText: '保存并切换',
        altText: '放弃并切换',
        cancelText: '继续编辑'
      })
      if (!isCurrent() || choice === 'cancel') return false
      if (choice === 'confirm') {
        if (!await persistDraft(true) || !isCurrent()) return false
        // A late edit must not be discarded even if the form was changed while saving.
        if (draftDirty.value) {
          notify.warning('保存期间内容又有改动，请确认后再切换')
          return false
        }
      }
    }
    if (!isCurrent()) return false
    const target = proxyGroups.value.find(item => keyOf(item) === targetKey)
    return target ? await applyGroupSelection(target) : false
  } finally {
    selectionSwitchPending = false
  }
}

const resetDraft = () => {
  if (selected.value) draft.value = draftFrom(selected.value)
}

const draftDirty = computed(() => {
  if (!selected.value || selected.value.follow_group) return false
  const base = draftFrom(selected.value)
  return (
    base.regex !== draft.value.regex ||
    base.sources.length !== draft.value.sources.length ||
    base.sources.some(id => !draft.value.sources.includes(id))
  )
})

const toggleDraftSource = (id: string) => {
  const list = draft.value.sources
  draft.value.sources = list.includes(id) ? list.filter(x => x !== id) : [...list, id]
}

const sourceOptions = computed(() => {
  if (draft.value.kind === 'aggregation') {
    return aggregations.value
      .filter(a => a.enabled !== false)
      .map(a => ({ id: a.id, name: a.name, count: aggregationCounts.value[a.id] }))
  }
  return subscriptions.value.map(s => ({
    id: s.id,
    name: s.name,
    count: typeof s.cached_node_count === 'number' ? s.cached_node_count : undefined
  }))
})

/** 不参与正则筛选的其他来源，只做提示 */
const extraSources = computed(() => {
  const g = selected.value as any
  if (!g) return []
  const extras: string[] = []
  const manual = (g.manual_nodes || []).length
  if (manual) extras.push(`手动节点 ${manual}`)
  for (const name of getIncludeGroupsList(g)) extras.push(`策略 · ${name}`)
  return extras
})

const aggregationCounts = ref<Record<string, number>>({})

/* ---------- 预览 ---------- */
const preview = ref<{ nodes: any[]; total: number; loading: boolean; error: string }>({
  nodes: [],
  total: 0,
  loading: false,
  error: ''
})

const requestPreview = async (kind: SourceKind, sources: string[], regex: string) => {
  const { data } = await proxyGroupApi.previewRegex({
    source: kind,
    regex: regex || '.*',
    ...(kind === 'aggregation' ? { aggregations: sources } : { subscriptions: sources })
  }, profileId)
  return data
}

let previewTimer: number | undefined
let previewSeq = 0
let viewActive = true
const refreshPreview = () => {
  clearTimeout(previewTimer)
  const seq = ++previewSeq
  const group = selected.value
  if (!viewActive || !group || group.follow_group || !draft.value.sources.length) {
    preview.value = { nodes: [], total: 0, loading: false, error: '' }
    return
  }
  // 与配置生成共用后端正则语法（包括 (?i)），不使用 JavaScript RegExp 校验。
  preview.value = { ...preview.value, loading: true, error: '' }
  previewTimer = window.setTimeout(async () => {
    try {
      const data = await requestPreview(draft.value.kind, draft.value.sources, draft.value.regex)
      if (!viewActive || seq !== previewSeq) return
      preview.value = { nodes: data.nodes || [], total: data.total_candidates || 0, loading: false, error: '' }
      if (!draftDirty.value) matchCounts.value = { ...matchCounts.value, [keyOf(group)]: data.count ?? 0 }
    } catch (error: any) {
      if (!viewActive || seq !== previewSeq) return
      preview.value = { ...preview.value, loading: false, error: error.response?.data?.message || '预览失败' }
    }
  }, 280)
}

watch([selectedKey, () => draft.value.sources, () => draft.value.regex], refreshPreview, { deep: true })

/* ---------- 每个策略组的命中数（列表右侧），限并发逐个计算 ---------- */
const matchCounts = ref<Record<string, number>>({})

const computeMatchCounts = async () => {
  const queue = proxyGroups.value.filter(g => !g.follow_group)
  const worker = async () => {
    while (viewActive && queue.length) {
      const group = queue.shift()!
      const d = draftFrom(group)
      if (!d.sources.length) continue
      try {
        const data = await requestPreview(d.kind, d.sources, d.regex)
        if (!viewActive) return
        matchCounts.value = { ...matchCounts.value, [keyOf(group)]: data.count ?? 0 }
      } catch {
        // 单个失败不影响其他
      }
    }
  }
  await Promise.all([worker(), worker(), worker()])
}

/* ---------- 延迟与预计生效节点 ---------- */
const latencyMap = ref<Record<string, { latency: number | null }>>({})
const latencyLabel = (name: string) => {
  const r = latencyMap.value[name]
  if (!r) return '—'
  return r.latency === null ? '超时' : String(r.latency)
}

const winner = computed(() => {
  const group = selected.value
  const nodes = preview.value.nodes
  if (!group || !nodes.length) return null
  const reachable = nodes.filter(n => typeof latencyMap.value[n.name]?.latency === 'number')
  if (group.type === 'url-test') {
    return [...reachable].sort(
      (a, b) => (latencyMap.value[a.name].latency as number) - (latencyMap.value[b.name].latency as number)
    )[0] || null
  }
  if (group.type === 'fallback') return reachable[0] || null
  if (group.type === 'select') return nodes[0]
  return null
})

const strategyText = computed(() => {
  const group = selected.value as any
  if (!group) return ''
  if (group.follow_group) return '节点与被跟随的策略组保持一致。'
  switch (group.type) {
    case 'url-test':
      return `每 ${group.interval || 300}s 测速，自动切到延迟最低的节点。`
    case 'fallback':
      return '按顺序检测，首个可用节点生效。'
    case 'load-balance':
      return `${getStrategyLabel(group.strategy || 'consistent-hashing')}，流量分摊到 ${preview.value.nodes.length} 个节点。`
    default:
      return '手动选择，默认使用第一个命中节点。'
  }
})

/* ---------- 保存管道里的改动 ---------- */
const draftSaving = ref(false)
const persistDraft = async (fromSelection = false): Promise<boolean> => {
  if (!viewActive || draftSaving.value || (selectionSwitchPending && !fromSelection)) return false
  const group = selected.value as any
  if (!group?.id) return false
  if (preview.value.error) {
    notify.error('请先解决预览错误，再保存改动')
    return false
  }
  const snapshot = { kind: draft.value.kind, sources: [...draft.value.sources], regex: draft.value.regex }
  const savedCount = !preview.value.loading ? preview.value.nodes.length : null
  draftSaving.value = true
  const patch =
    snapshot.kind === 'aggregation'
      ? { aggregations: [...snapshot.sources], aggregation_regex: snapshot.regex }
      : { subscriptions: [...snapshot.sources], regex: snapshot.regex }
  try {
    await proxyGroupApi.update(group.id, { ...group, ...patch }, profileId)
    if (!viewActive) return false
    const savedGroup = proxyGroups.value.find(item => keyOf(item) === keyOf(group))
    if (!savedGroup) return false
    Object.assign(savedGroup, patch)
    if (selectedKey.value === keyOf(group) && !draftDirty.value && savedCount !== null) {
      matchCounts.value = { ...matchCounts.value, [keyOf(group)]: savedCount }
    }
    notify.success(`「${group.name}」已保存`)
    return true
  } catch (error: any) {
    if (viewActive) notify.error(error.response?.data?.message || '保存失败')
    return false
  } finally {
    draftSaving.value = false
  }
}
const saveDraft = (): Promise<boolean> => persistDraft()

/** 聚合节点数只在查看聚合来源的策略组时才统计，每个页面只取一次 */
let aggregationCountsLoaded = false
const loadAggregationCounts = async () => {
  if (!viewActive || aggregationCountsLoaded || !aggregations.value.length) return
  aggregationCountsLoaded = true
  for (const agg of aggregations.value) {
    if (!viewActive) return
    try {
      const { data } = await api.get(`/aggregations/${agg.id}/count`)
      if (!viewActive) return
      aggregationCounts.value = { ...aggregationCounts.value, [agg.id]: data.total_count ?? 0 }
    } catch {
      // 计数失败只是不显示数字
    }
  }
}

// 列表变化（新增、删除、重排后重载）时保证始终有一个选中项
watch(proxyGroups, groups => {
  if (!viewActive) return
  // A deleted selection has no draft destination to protect; don't prompt here.
  if (!groups.some(g => keyOf(g) === selectedKey.value)) void applyGroupSelection(groups[0] || null)
})

onMounted(async () => {
  await Promise.all([loadProxyGroups(), loadResources()])
  if (!viewActive) return
  try {
    const { data } = await nodeApi.latency()
    if (!viewActive) return
    latencyMap.value = data?.results || {}
  } catch {
    // 没有测速结果时只显示节点名
  }
  if (viewActive) computeMatchCounts()
})

// 聚合列表可能晚于选中项加载完成，两者任一变化都再检查一次
watch([() => draft.value.kind, aggregations], ([kind]) => {
  if (kind === 'aggregation') loadAggregationCounts()
}, { immediate: true })

onUnmounted(() => {
  viewActive = false
  ++previewSeq
  clearTimeout(previewTimer)
})
</script>

<style scoped>
/* Keep desktop browsing within one viewport; only the list and detail body scroll. */
@media (min-width: 1181px) {
  .proxy-groups-page {
    display: flex;
    flex-direction: column;
    /* App main padding (pt-7 + pb-12) and the content-box header's 1px border. */
    height: calc(100dvh - var(--cf-topbar-h) - 4.75rem - 1px - env(safe-area-inset-top));
    min-height: 360px;
  }

  .group-workspace {
    flex: 1;
  }
}

@media (max-width: 1180px) {
  .group-list {
    max-height: 60dvh;
  }

  .group-detail-body {
    flex: none;
    overflow: visible;
  }
}

.pipe-link {
  position: relative;
  align-self: center;
  height: 2px;
  margin: 0 4px;
  background: repeating-linear-gradient(90deg, var(--primary-accent) 0 6px, transparent 6px 12px);
  background-size: 24px 2px;
  opacity: 0.8;
  animation: pipe-dash 0.6s linear infinite;
}

.pipe-link::after {
  content: '';
  position: absolute;
  top: -4px;
  right: -2px;
  border: 5px solid transparent;
  border-right: 0;
  border-left-color: var(--primary-accent);
}

@keyframes pipe-dash {
  to {
    background-position: 24px 0;
  }
}

@media (max-width: 767.98px) {
  .pipe-link {
    justify-self: center;
    width: 2px;
    height: 24px;
    margin: 0;
    background: repeating-linear-gradient(180deg, var(--primary-accent) 0 6px, transparent 6px 12px);
    background-size: 2px 24px;
    animation-name: pipe-dash-v;
  }

  .pipe-link::after {
    display: none;
  }

  @keyframes pipe-dash-v {
    to {
      background-position: 0 24px;
    }
  }
}

.matched-node {
  animation: node-pop 0.4s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

.matched-node.is-winner {
  border-color: var(--primary-accent);
  background: var(--primary-soft);
  color: var(--foreground);
  box-shadow: 0 0 20px -6px var(--primary-accent);
}

.matched-node.is-winner i {
  color: var(--primary-accent);
}

@keyframes node-pop {
  from {
    opacity: 0;
    transform: scale(0.8);
  }
}
</style>
