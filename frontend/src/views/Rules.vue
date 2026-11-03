<template>
  <div :class="reorder.active.value && 'cf-reordering'">

    <PageHeader
      title="策略规则"
    >
      <template #actions>
        <Button @click="openEditor('values')">
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
            <DropdownMenuItem @select="openEditor('library')">
              <FolderOpen class="size-4" />
              添加规则集
            </DropdownMenuItem>
            <DropdownMenuItem v-if="dnsColumn" :disabled="!followCandidates.length" @select="unifyFollow">
              <Workflow class="size-4" />
              统一为跟随策略
            </DropdownMenuItem>
            <DropdownMenuItem @select="showDuplicateDialog">
              <Copy class="size-4" />
              查找重复
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem :disabled="!foldableGroups.length || reorder.active.value" @select="toggleAllGroups">
              <component :is="allGroupsCollapsed ? ChevronsUpDown : ChevronsDownUp" class="size-4" />
              {{ allGroupsCollapsed ? '全部展开分组' : '全部收起分组' }}
            </DropdownMenuItem>
            <DropdownMenuItem :disabled="reorderUnits.length < 2" @select="enterReorder">
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
      <form class="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-3.5 p-5 max-md:p-4 max-sm:gap-2 max-sm:p-3" @submit.prevent="simulate()">
        <label class="min-w-0">
          <span class="mb-2 block font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase max-sm:mb-1">命中模拟</span>
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
        <div class="col-span-full flex min-h-10 flex-wrap items-center gap-2 text-[13.5px] text-muted-foreground" :class="!simRunning && !simResult && !simSamples.length && 'max-sm:hidden'">
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
            <template v-if="simDns">
              <span aria-hidden="true">·</span>
              <span :class="cn('chip', chipClass(simDns.tone))" :title="simDns.title" data-sim-dns>DNS：{{ simDns.text }}</span>
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

    <!-- DNS 状态读取失败时保留 DNS 列，并在列表上方提供重试 -->
    <InfoNote v-if="displayList.length && dnsColumn && dnsError" class="mb-3.5" data-dns-error>
      <p class="flex flex-wrap items-center gap-2">
        DNS 状态读取失败，DNS 列暂不可修改
        <Button type="button" variant="ghost" size="xs" class="text-primary-accent max-sm:h-11" :disabled="dnsLoading" @click="loadDns">
          <Loader2 v-if="dnsLoading" class="size-3 animate-spin" />重试
        </Button>
      </p>
    </InfoNote>
    <InfoNote v-else-if="displayList.length && dnsColumn && dnsSnapshot && !dnsSnapshot.engine.editable" class="mb-3.5" data-dns-locked>
      <p class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span class="min-w-0 break-words">DNS 处理需在完整参数修改：{{ dnsSnapshot.engine.reason || 'OxiDNS 分流含自定义结构' }}</span>
        <a href="/generate?oxidns=config" target="_blank" rel="noopener" class="inline-flex min-h-6 items-center gap-1 text-primary-accent underline-offset-4 hover:underline max-sm:min-h-11">打开完整参数<ExternalLink class="size-3" aria-hidden="true" /></a>
      </p>
    </InfoNote>

    <SectionCard v-if="displayList.length === 0" :padded="false">
      <EmptyState
        :icon="FileText"
        title="暂无规则"
        description="添加单条规则或从规则库选择规则集，再指定处理流量的策略。规则从上到下匹配。"
      >
        <Button @click="openEditor('values')">
          <Plus class="size-4" />
          添加规则
        </Button>
      </EmptyState>
    </SectionCard>

    <!-- ===== 列表视图 ===== -->
    <SectionCard v-else-if="viewMode === 'list'" :padded="false">
      <div class="max-w-full overflow-x-auto" role="region" aria-label="策略规则列表" tabindex="0">
      <div :class="dnsColumn ? 'min-w-[828px] max-lg:min-w-[668px] xl:min-w-[868px]' : 'min-w-[680px]'">
      <div
        class="flex items-center gap-2.5 border-b border-border px-4 py-3 font-mono text-[10.5px] tracking-[0.12em] text-muted-foreground uppercase"
        aria-hidden="true"
      >
        <span v-if="reorder.active.value" class="w-11" />
        <span class="w-9">#</span>
        <span class="w-[118px] max-md:w-[92px]">类型</span>
        <span class="min-w-0 flex-1">匹配值</span>
        <span class="w-[160px] max-lg:hidden">备注</span>
        <span class="w-[120px] pl-2">策略</span>
        <span v-if="dnsColumn" class="w-[148px] pl-2 xl:w-[188px]">DNS</span>
        <span class="w-[136px]" />
      </div>
      <div id="sortable-rules" ref="rulesContainer" class="flex flex-col">
        <div
          v-for="(item, cfIndex) in displayList"
          :key="item.uniqueId"
          class="cf-cv-row max-md:[--cf-cv-row-h:67px]"
          data-reorder-item
          :data-id="item.uniqueId"
        >
          <!-- 组头：相邻、同策略且同名的规则为一段；成员行仍按普通行展示，列从上到下对齐 -->
          <div
            v-if="item.isGroup"
            :class="cn(
              'flex w-full items-center gap-2.5 border-0 border-b border-border px-4 py-2 transition-colors',
              !reorder.active.value && 'mt-4',
              !reorder.active.value && 'cursor-pointer hover:bg-accent/40'
            )"
            :title="item.groupLabel"
            data-group-head
            @click="!reorder.active.value && toggleGroup(item.groupId)"
          >
            <DragHandle
              v-if="reorder.active.value"
              :label="groupTitle(item)"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <span class="flex w-9 shrink-0 items-center">
              <Button
                v-if="!reorder.active.value"
                variant="ghost"
                size="icon-sm"
                class="-ml-2 text-muted-foreground"
                :aria-expanded="!item.collapsed"
                :aria-label="`${item.collapsed ? '展开' : '收起'}分组 ${groupTitle(item)}`"
                @click.stop="toggleGroup(item.groupId)"
              >
                <ChevronRight :class="cn('size-4 transition-transform', !item.collapsed && 'rotate-90')" />
              </Button>
            </span>
            <span class="flex min-w-0 flex-1 items-baseline gap-2 overflow-hidden">
              <span :class="cn('min-w-0 max-w-[60%] truncate text-[14px]', item.groupName ? 'font-medium text-foreground' : 'text-muted-foreground')">
                {{ groupTitle(item) }}
              </span>
              <span class="num shrink-0 font-mono text-[11px] text-muted-foreground">
                {{ groupRange(item) }}<template v-if="item.groupName"> · {{ item.count }} 项</template>
              </span>
              <span v-if="item.collapsed" class="min-w-0 truncate font-mono text-[11.5px] text-muted-foreground" :title="groupPreview(item)">
                {{ groupPreview(item) }}
              </span>
            </span>
            <span class="w-[160px] shrink-0 max-lg:hidden" aria-hidden="true" />
            <span class="flex w-[120px] shrink-0">
              <RulePolicySelect
                :model-value="item.policy"
                :policies="availablePolicies"
                :label="`${groupTitle(item)} 的策略：${item.policy}，点击修改整组`"
                :disabled="reorder.active.value"
                :loading="!!policyPending[item.groupId]"
                trigger-class="text-foreground"
                @update:model-value="value => setGroupPolicy(item, value)"
              />
            </span>
            <span v-if="dnsColumn" class="cf-reorder-mute flex w-[148px] shrink-0 items-center xl:w-[188px]">
              <span v-if="dnsLoading && (!dnsSnapshot || groupStale(item))" class="ml-2 inline-block h-6 w-24 animate-pulse rounded-md bg-primary/10" role="status" aria-label="DNS 状态加载中" />
              <button
                v-else-if="dnsEditable"
                type="button"
                :disabled="reorder.active.value"
                :class="cn('rule-group-dns-trigger relative inline-flex h-[30px] w-full min-w-0 items-center gap-1.5 rounded-md px-2 text-left text-[12px] transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent disabled:cursor-default', groupDns(item).tone === 'warn' ? 'text-warning-accent' : 'text-muted-foreground')"
                :title="groupDns(item).title"
                :aria-label="`批量设置 ${groupTitle(item)} 的 DNS`"
                @click.stop="openBatch(item)"
              >
                <span class="min-w-0 flex-1 truncate" data-group-dns>{{ groupDns(item).text }}</span>
                <ChevronDown class="size-3 shrink-0 opacity-60" aria-hidden="true" />
              </button>
              <span v-else :class="cn('min-w-0 truncate px-2 text-[12px]', groupDns(item).tone === 'warn' ? 'text-warning-accent' : 'text-muted-foreground')" :title="groupDns(item).title" data-group-dns>{{ groupDns(item).text }}</span>
            </span>
            <!-- 与成员行的「启停 / 编辑 / 删除」同位，重命名落在编辑的位置 -->
            <span class="flex w-[136px] shrink-0 items-center justify-end gap-0.5">
              <span class="size-8" aria-hidden="true" />
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
              <span class="size-8" aria-hidden="true" />
            </span>
          </div>

          <!-- 普通行 -->
          <div
            v-else
            :data-rule-key="item.uniqueId"
            :class="[
              'rule-row flex items-center gap-2.5 border-0 border-b px-4 py-[11px] transition-colors hover:bg-accent/30',
              item.isGroupMember && !item.isLastInGroup ? 'border-transparent' : item.isLastInGroup ? 'border-border' : 'border-border/40',
              item.isGroupMember && 'in-group',
              !item.enabled && 'opacity-55',
              (simMatchKey === item.uniqueId || recentKeys.has(item.uniqueId)) && 'is-match'
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
                <Button
                  v-if="item.library_rule_id && !reorder.active.value"
                  as-child
                  variant="ghost"
                  size="icon-xs"
                  class="cf-reorder-mute text-muted-foreground hover:text-primary-accent"
                >
                  <RouterLink
                    :to="{ path: '/rule-library', query: { rule: item.library_rule_id } }"
                    :title="`在规则库中查看 ${item.name}`"
                    :aria-label="`在规则库中查看 ${item.name}`"
                    @click.stop
                  >
                    <FolderOpen class="size-3.5" aria-hidden="true" />
                  </RouterLink>
                </Button>
                <FolderOpen
                  v-else-if="item.library_rule_id"
                  class="size-3.5 shrink-0 text-muted-foreground"
                  title="排序时无法跳转规则库"
                  aria-label="排序时无法跳转规则库"
                />
              </template>
              <span v-if="udpFallbackLabel(item)" class="chip shrink-0 text-[11px]" :title="UDP_FALLBACK_TITLE">{{ udpFallbackLabel(item) }}</span>
              <span v-if="afterMatch.has(item.uniqueId)" class="chip shrink-0 text-[11px]" :title="AFTER_MATCH_TITLE">仅 Mihomo</span>
            </div>
            <span class="w-[160px] shrink-0 truncate text-[12px] text-muted-foreground max-lg:hidden" :title="item.remark">
              {{ item.remark || '—' }}
            </span>

            <span class="flex w-[120px] shrink-0">
              <!-- 组内策略已在组头显示，成员行降为文字；改单条会离开原分组 -->
              <RulePolicySelect
                :model-value="item.policy"
                :policies="availablePolicies"
                :label="`${ruleName(item)} 的策略：${item.policy}，点击修改`"
                :disabled="reorder.active.value"
                :loading="!!policyPending[item.uniqueId]"
                :trigger-class="item.isGroupMember ? 'text-muted-foreground hover:text-foreground' : 'text-foreground'"
                @update:model-value="value => setRulePolicy(item, value)"
              />
            </span>
            <span v-if="dnsColumn" class="cf-reorder-mute flex w-[148px] shrink-0 items-center xl:w-[188px]">
              <RuleDnsCell
                :item="item"
                :snapshot="dnsSnapshot"
                :loading="dnsLoading"
                :stale="dnsStale.has(item.id)"
                :pending="dnsPending[item.id]"
                :disabled="reorder.active.value || dnsError"
                @change="value => setRuleDns(item, value)"
              />
            </span>

            <div class="cf-reorder-mute flex w-[136px] shrink-0 items-center justify-end gap-0.5">
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

      </div>
      </div>
      <Separator />
      <p class="m-0 px-4 py-2.5 text-xs text-muted-foreground">共 {{ allRules.length }} 项</p>
    </SectionCard>

    <!-- ===== 卡片视图 ===== -->
    <div
      v-else
      id="sortable-rules"
      ref="rulesContainer"
      class="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3 max-md:grid-cols-1"
    >
      <div
        v-for="(item, cfIndex) in cardList"
        :key="item.uniqueId"
        class="cf-list-in"
        :style="listItemStyle(cfIndex)"
        data-reorder-item
        :data-id="item.uniqueId"
        :aria-hidden="item.isGroupEnd || undefined"
        :class="[
          item.isGroupEnd
            ? 'col-span-full h-0 border-0 border-b border-border'
            : item.isGroup && !reorder.active.value
            ? 'col-span-full flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 border-0 border-b border-border px-1 pt-3 pb-2'
            : 'cf-cv-row [--cf-cv-row-h:192px] relative flex flex-col gap-3 overflow-hidden rounded-[18px] border border-border/35 bg-card/90 shadow-surface p-4 transition-all duration-300 hover:border-border-strong max-sm:gap-2 max-sm:p-3',
          !item.isGroup && !item.enabled && 'dark:opacity-60'
        ]"
      >
        <!-- 分段标题：成员卡片紧随其后；排序时收起的分组作为一张卡片整组移动 -->
        <template v-if="item.isGroupEnd"></template>
        <template v-else-if="item.isGroup && !reorder.active.value">
          <Button
            variant="ghost"
            size="icon-sm"
            class="text-muted-foreground"
            :aria-expanded="!item.collapsed"
            :aria-label="`${item.collapsed ? '展开' : '收起'}分组 ${groupTitle(item)}`"
            @click="toggleGroup(item.groupId)"
          >
            <ChevronRight :class="cn('size-4 transition-transform', !item.collapsed && 'rotate-90')" />
          </Button>
          <span :class="cn('min-w-0 truncate text-[14px]', item.groupName ? 'font-medium text-foreground' : 'text-muted-foreground')" :title="item.groupLabel">
            {{ groupTitle(item) }}
          </span>
          <span class="num shrink-0 font-mono text-[11px] text-muted-foreground">
            {{ groupRange(item) }}<template v-if="item.groupName"> · {{ item.count }} 项</template>
          </span>
          <RulePolicySelect
            :model-value="item.policy"
            :policies="availablePolicies"
            :label="`${groupTitle(item)} 的策略：${item.policy}，点击修改整组`"
            :loading="!!policyPending[item.groupId]"
            trigger-class="max-w-[150px] text-foreground"
            @update:model-value="value => setGroupPolicy(item, value)"
          />
          <span class="ml-auto flex min-w-0 items-center gap-1">
            <span v-if="dnsColumn" class="flex min-w-0 items-center gap-1.5 text-[12px]">
              <span class="shrink-0 text-muted-foreground">DNS</span>
              <span v-if="dnsLoading && (!dnsSnapshot || groupStale(item))" class="inline-block h-5 w-24 animate-pulse rounded-md bg-primary/10" role="status" aria-label="DNS 状态加载中" />
              <button v-else-if="dnsEditable" type="button" :class="cn('rule-group-dns-trigger relative inline-flex h-[30px] min-w-0 items-center gap-1.5 rounded-md px-2 text-left transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent', groupDns(item).tone === 'warn' ? 'text-warning-accent' : 'text-muted-foreground')" :title="groupDns(item).title" :aria-label="`批量设置 ${groupTitle(item)} 的 DNS`" @click.stop="openBatch(item)">
                <span class="min-w-0 truncate" data-group-dns>{{ groupDns(item).text }}</span>
                <ChevronDown class="size-3 shrink-0 opacity-60" aria-hidden="true" />
              </button>
              <span v-else :class="cn('min-w-0 truncate', groupDns(item).tone === 'warn' ? 'text-warning-accent' : 'text-muted-foreground')" :title="groupDns(item).title" data-group-dns>{{ groupDns(item).text }}</span>
            </span>
            <Button variant="ghost" size="icon-sm" class="shrink-0" title="重命名" aria-label="重命名分组" @click="showGroupRenameDialog(item)">
              <Pencil class="size-4" />
            </Button>
          </span>
          <p v-if="item.collapsed" class="m-0 basis-full truncate pl-10 font-mono text-[11.5px] text-muted-foreground" :title="groupPreview(item)">
            {{ groupPreview(item) }}
          </p>
        </template>

        <template v-else-if="item.isGroup">
          <header class="flex items-start gap-2.5">
            <DragHandle
              :label="groupTitle(item)"
              :index="cfIndex"
              :total="displayList.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <p :class="cn('m-0 min-w-0 flex-1 truncate text-[14px]', item.groupName ? 'font-medium text-foreground' : 'text-muted-foreground')">
              {{ groupTitle(item) }}
            </p>
          </header>
          <div class="flex flex-wrap items-center gap-1.5">
            <Badge variant="outline" class="num text-[10.5px]">{{ groupRange(item) }} · {{ item.count }} 项</Badge>
            <Badge variant="secondary" class="min-w-0 max-w-full text-[10.5px]" :title="item.policy"><span class="truncate">{{ item.policy }}</span></Badge>
          </div>
          <p class="m-0 truncate font-mono text-[11.5px] text-muted-foreground" :title="groupPreview(item)">{{ groupPreview(item) }}</p>
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
                <Button
                  v-if="item.library_rule_id && !reorder.active.value"
                  as-child
                  variant="ghost"
                  size="icon-xs"
                  class="cf-reorder-mute text-muted-foreground hover:text-primary-accent"
                >
                  <RouterLink
                    :to="{ path: '/rule-library', query: { rule: item.library_rule_id } }"
                    :title="`在规则库中查看 ${item.name}`"
                    :aria-label="`在规则库中查看 ${item.name}`"
                    @click.stop
                  >
                    <FolderOpen class="size-3.5" aria-hidden="true" />
                  </RouterLink>
                </Button>
                <FolderOpen
                  v-else-if="item.library_rule_id"
                  class="size-3.5 shrink-0 text-muted-foreground"
                  title="排序时无法跳转规则库"
                  aria-label="排序时无法跳转规则库"
                />
              </p>
            </template>
            <p v-if="item.remark" class="mt-1.5 mb-0 break-all text-[11.5px] text-muted-foreground" :title="item.remark">
              {{ item.remark }}
            </p>
            <div v-if="udpFallbackLabel(item) || afterMatch.has(item.uniqueId)" class="mt-1.5 flex flex-wrap gap-1.5">
              <span v-if="udpFallbackLabel(item)" class="chip text-[11px]" :title="UDP_FALLBACK_TITLE">{{ udpFallbackLabel(item) }}</span>
              <span v-if="afterMatch.has(item.uniqueId)" class="chip text-[11px]" :title="AFTER_MATCH_TITLE">仅 Mihomo</span>
            </div>
          </div>

          <div v-if="dnsColumn" class="cf-reorder-mute mt-auto flex w-full min-w-0 items-center gap-2 text-[12px]" data-card-dns>
            <span class="shrink-0 text-muted-foreground">DNS</span>
            <RuleDnsCell
              class="min-w-0 flex-1"
              :item="item"
              :snapshot="dnsSnapshot"
              :loading="dnsLoading"
              :stale="dnsStale.has(item.id)"
              :pending="dnsPending[item.id]"
              :disabled="reorder.active.value || dnsError"
              @change="value => setRuleDns(item, value)"
            />
          </div>
          <footer :class="cn('cf-reorder-mute flex items-center gap-1 border-0 border-t border-border/50 pt-3 max-sm:pt-2', !dnsColumn && 'mt-auto')">
            <RulePolicySelect
              :model-value="item.policy"
              :policies="availablePolicies"
              :label="`${ruleName(item)} 的策略：${item.policy}，点击修改`"
              :disabled="reorder.active.value"
              :loading="!!policyPending[item.uniqueId]"
              trigger-class="max-w-[150px] text-foreground"
              @update:model-value="value => setRulePolicy(item, value)"
            />
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
      </div>
    </div>

    <!-- ===== 添加 / 编辑规则（一页表单） ===== -->
    <RuleEditorDialog
      v-model:open="editorOpen"
      :mode="editorItem ? 'edit' : 'create'"
      :item="editorItem"
      :initial-source="editorSource"
      :rules="allRules"
      :library="ruleLibrary"
      :policies="availablePolicies"
      :dns="editorDns"
      :dns-loading="dnsLoading"
      :profile-id="profileId"
      @saved="onEditorSaved"
      @source-saved="onSourceSaved"
      @reload-dns="loadDns"
      @enable="enableRule"
    />

    <RuleDnsBatchDialog
      v-model:open="batchOpen"
      :items="batchItems"
      :group-name="batchName"
      :snapshot="dnsSnapshot"
      :profile-id="profileId"
      @applied="mergeDns"
    />

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
import {
  ArrowUpDown,
  ChevronDown,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  CircleCheck,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  FolderOpen,
  Loader2,
  Pencil,
  Play,
  Plus,
  Trash2,
  Workflow
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
import { Separator } from '@/components/ui/separator'
import EmptyState from '@/components/common/EmptyState.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ViewToggle from '@/components/common/ViewToggle.vue'
import RuleEditorDialog from '@/components/rules/RuleEditorDialog.vue'
import RuleDnsBatchDialog from '@/components/rules/RuleDnsBatchDialog.vue'
import RuleDnsCell from '@/components/rules/RuleDnsCell.vue'
import RulePolicySelect from '@/components/rules/RulePolicySelect.vue'
import { confirm, notify } from '@/lib/feedback'
import { listItemStyle } from '@/lib/motion'
import { ruleApi, ruleSetApi, proxyGroupApi } from '@/api'
import { cn } from '@/lib/utils'
import { chipClass, dnsChip, dnsLabel, followRoute, isUpstreamValue, notifyRuleSaved, pinnedWithheld, ruleSetPayload } from '@/lib/ruleDns'
import { isOxidnsRule } from '@/lib/oxidns'
import type { ProxyGroup, RuleDnsSnapshot, RuleDnsState, RuleDnsValue } from '@/types'
import api from '@/api'
import { getActiveProfileId } from '@/profileContext'
import { systemReducesMotion } from '@/stores/preferences'
import { isAxiosError } from 'axios'

const errorMessage = (error: unknown, fallback: string) =>
  isAxiosError<{ message?: string }>(error) ? error.response?.data?.message || fallback : fallback
/** DNS 写入失败：native 分流被手动修改时，服务端在 usages[0].reason 给出具体原因 */
const dnsFailure = (error: unknown, fallback: string) =>
  (isAxiosError<{ usages?: Array<{ reason?: string }> }>(error) && error.response?.data?.usages?.[0]?.reason) || errorMessage(error, fallback)

const profileId = getActiveProfileId()
const profileRequestConfig = { headers: { 'X-ConfigFlow-Profile': profileId } }


/* DIRECT 绿、REJECT 红，其余走强调色，与生成的配置语义一致 */
const policyTone = (policy: string) =>
  policy === 'DIRECT' ? ('success' as const) : policy === 'REJECT' ? ('danger' as const) : ('brand' as const)

const allRules = ref<any[]>([])  // 包含规则和规则集的合并数组
const proxyGroups = ref<ProxyGroup[]>([])
const ruleLibrary = ref<any[]>([])  // 规则仓库
const viewMode = ref<'list' | 'card'>('list') // 默认表格视图，与匹配顺序一致
const rulesContainer = ref<HTMLElement | null>(null)

/* ================================================================
 * 命中模拟
 * ================================================================ */
const simQuery = ref('')
const simRunning = ref(false)
const simResult = ref<any>(null)
const simMatchKey = ref('')
const simVia = ref<{ label: string; hint: string } | null>(null)
/** 命中项的 DNS 处理，与列表 chip 同源 */
const simDns = computed(() => {
  const result = simResult.value
  const snapshot = dnsSnapshot.value
  if (!result?.matched || !result.rule_id || !snapshot?.engine.oxidns_in_use) return null
  const entry = snapshot.rules[result.rule_id]
  const chip = dnsChip(entry, { ...snapshot.engine, editable: true })
  return { text: dnsLabel(entry, snapshot.engine), tone: chip.tone, title: chip.title }
})

const policyChip = (policy: string) =>
  policy === 'DIRECT' ? 'chip-ok font-mono' : policy === 'REJECT' ? 'chip-bad font-mono' : 'chip-acc'

const UDP_FALLBACK_TITLE = '代理节点不支持 UDP 时的处理，编辑这条 MATCH 规则可修改'
const AFTER_MATCH_TITLE = '位于 MATCH 之后：Mihomo 只用它匹配因节点不支持 UDP 而跳过的连接，Surge 和 Loon 会忽略这一条'
const udpFallbackLabel = (item: any) =>
  item.itemType === 'rule' && item.rule_type === 'MATCH' && item.udp_fallback
    ? `UDP 不支持时${item.udp_fallback === 'REJECT' ? '拒绝' : '直连'}` : ''
/** 第一条启用的 MATCH 之后的条目（含已停用的），键与列表行的 uniqueId 相同 */
const afterMatch = computed(() => {
  const index = allRules.value.findIndex(item => item.itemType === 'rule' && item.rule_type === 'MATCH' && item.enabled !== false)
  return new Set(index < 0 ? [] : allRules.value.slice(index + 1).map(item => `${item.itemType}-${item.id}`))
})

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
  return idx.length > 1 ? `${pad(Math.min(...idx))}–${pad(Math.max(...idx))}` : pad(idx[0])
}
const groupTitle = (group: any) => group.groupName || group.groupDefaultName
/** 收起时在组头预览前几项，展开后成员行自己会显示 */
const groupPreview = (group: any) => {
  const names = group.items.map((item: any) => (item.itemType === 'rule' ? item.value : item.name))
  return names.slice(0, 3).join('、') + (names.length > 3 ? '…' : '')
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

/*
 * 扫描高亮直接改行的 class，不写响应式状态：每一步都改响应式会让整个列表重渲染，
 * 规则多时扫描一遍要卡十几秒。最终命中结果仍写回响应式状态。
 */
let scanRow: HTMLElement | null = null
let scanTimer = 0
let finishScan: (() => void) | undefined

const markScanRow = (row: HTMLElement | null) => {
  scanRow?.classList.remove('is-scan')
  scanRow = row
  row?.classList.add('is-scan')
}

/** 逐行扫描到 stop（含），总时长控制在 1.6s 内，行多时每帧跳过几行；stopScan() 随时结束并清掉高亮 */
const scanRows = (rows: HTMLElement[], stop: number) => new Promise<void>(resolve => {
  const stride = Math.max(1, Math.ceil((stop + 1) * 16 / 1600))
  const step = Math.max(16, Math.min(90, 1600 / Math.max(1, Math.ceil((stop + 1) / stride))))
  let i = 0
  finishScan = () => {
    window.clearTimeout(scanTimer)
    finishScan = undefined
    markScanRow(null)
    resolve()
  }
  const next = () => {
    if (i > stop || i >= rows.length) {
      finishScan?.()
      return
    }
    markScanRow(rows[i])
    i += stride
    scanTimer = window.setTimeout(next, step)
  }
  next()
})
const stopScan = () => finishScan?.()

/** 仅展示手动策略组的默认项；自动策略组的实际出口由目标设备决定。 */
const resolveVia = async (policy: string) => {
  const group: any = proxyGroups.value.find(g => g.name === policy)
  if (!group || group.follow_group || group.type !== 'select') return null
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
    return {
      label: `默认：${nodes[0].name}`,
      hint: '策略组默认选中，实际出口以目标设备 Mihomo 面板为准'
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

    // 命中项在收起的分组里时临时展开，不改动记住的收起状态
    const matchKey = data.matched && data.rule_id ? `${data.item_type}-${data.rule_id}` : ''
    if (matchKey) {
      const group = ruleGroups.value.find(item => item.count > 1 && item.items.some((i: any) => i.uniqueId === matchKey))
      if (group) revealGroups([group.groupId])
      await nextTick()
    }

    // 逐行扫描到命中行（未命中则扫完全部）；系统要求减少动效时直接显示结果
    const rows = Array.from(rulesContainer.value?.querySelectorAll<HTMLElement>('[data-rule-key]') || [])
    const stop = matchKey ? rows.findIndex(r => r.dataset.ruleKey === matchKey) : rows.length - 1
    if (!systemReducesMotion()) await scanRows(rows, stop)
    simMatchKey.value = matchKey
    simResult.value = { ...data, query }

    // 扫描中离开页面时行已卸载，不再滚动
    const row = rows[stop]
    if (matchKey && row?.isConnected) {
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
    stopScan()
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

// 可用的策略选项：DIRECT、REJECT和所有策略组
const availablePolicies = computed(() => {
  const policies = ['DIRECT', 'REJECT']
  const groupNames = proxyGroups.value.map(g => g.name)
  return [...policies, ...groupNames]
})

/* ---------- 分组展开状态 ----------
 * 分组默认展开；用户收起的分组按配置空间记在本机，读写失败时按全部展开处理。
 * 命中模拟、保存后定位只临时展开（revealedGroups），不改写记住的状态。
 */
const collapsedStorageKey = `cf-rules-collapsed:${profileId}`
const readCollapsed = () => {
  try {
    const ids = JSON.parse(localStorage.getItem(collapsedStorageKey) || '[]')
    return new Set<string>(Array.isArray(ids) ? ids.filter(id => typeof id === 'string') : [])
  } catch {
    return new Set<string>()
  }
}
const collapsedGroups = ref<Set<string>>(readCollapsed())
const revealedGroups = ref<Set<string>>(new Set())
const rulesLoaded = ref(false)
const isCollapsed = (groupId: string) => collapsedGroups.value.has(groupId) && !revealedGroups.value.has(groupId)

const saveCollapsed = (ids: Iterable<string>) => {
  // 只记录现有分组，规则调整后失效的记录随之清理
  const current = new Set(ruleGroups.value.map(group => group.groupId))
  const next = new Set([...ids].filter(id => current.has(id)))
  if (next.size === collapsedGroups.value.size && [...next].every(id => collapsedGroups.value.has(id))) return
  collapsedGroups.value = next
  try {
    localStorage.setItem(collapsedStorageKey, JSON.stringify([...collapsedGroups.value]))
  } catch {
    // 本机存储不可用时只在本页生效
  }
}

const toggleGroup = (groupId: string) => {
  const next = new Set(collapsedGroups.value)
  if (isCollapsed(groupId)) next.delete(groupId)
  else next.add(groupId)
  revealedGroups.value = new Set([...revealedGroups.value].filter(id => id !== groupId))
  saveCollapsed(next)
}

const revealGroups = (ids: string[]) => {
  revealedGroups.value = new Set([...revealedGroups.value, ...ids])
}

const foldableGroups = computed(() => ruleGroups.value.filter(group => group.count > 1))
const allGroupsCollapsed = computed(() => foldableGroups.value.every(group => isCollapsed(group.groupId)))
const toggleAllGroups = () => {
  revealedGroups.value = new Set()
  saveCollapsed(allGroupsCollapsed.value ? [] : foldableGroups.value.map(group => group.groupId))
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
    await ruleApi.renameGroup(groupRenameForm.value.items.map(item => item.id), newGroupName, profileId)
    notify.success('重命名成功')
    groupRenameDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    notify.error(errorMessage(error, '重命名失败'))
  }
}

// 只合并相邻、同策略且 trim 后同名的规则；缺失或空白名称均为无名。
// 分组只影响展示和整组操作的范围，不改写原始名称，也不进入生成的配置。
const ruleGroups = computed(() => {
  const groups: any[] = []
  let current: any = null

  allRules.value.forEach((item) => {
    const itemWithId = { ...item, uniqueId: `${item.itemType}-${item.id}` }
    const groupName = (item.group_name || '').trim()
    if (current && current.policy === item.policy && current.groupName === groupName) {
      current.items.push(itemWithId)
      current.count++
      return
    }
    // 用首项的复合键：规则与规则集的 id 可能重复；不含策略，整组改策略后收起状态不变
    const groupId = `group_${itemWithId.uniqueId}`
    current = { isGroup: true, groupId, uniqueId: groupId, policy: item.policy, groupName, count: 1, items: [itemWithId] }
    groups.push(current)
  })

  groups.forEach(group => Object.assign(group, getGroupMeta(group.items)))
  return groups
})

// 展示列表：多项分组先放组头，展开时成员紧随其后，成员行与普通行同结构
const allRulesAndSets = computed(() => ruleGroups.value.flatMap((group) => {
  if (group.count === 1) return [group.items[0]]
  const collapsed = isCollapsed(group.groupId)
  const header = { ...group, collapsed }
  if (collapsed) return [header]
  return [header, ...group.items.map((item: any, index: number) => ({
    ...item,
    isGroupMember: true,
    groupId: group.groupId,
    isLastInGroup: index === group.items.length - 1
  }))]
}))

// 组 id 取自组首：新增、删除或移动组首后按成员把收起状态迁到当前的组 id，
// 避免用户的设置丢失，也避免过期记录日后让分组自己收起。
watch(() => ruleGroups.value, (groups) => {
  if (!rulesLoaded.value) return
  saveCollapsed(groups
    // 也认 group_<首项>_<策略> 的旧格式，升级后已收起的分组不会展开
    .filter(group => group.count > 1 && group.items.some((item: any) =>
      collapsedGroups.value.has(`group_${item.uniqueId}`) || collapsedGroups.value.has(`group_${item.uniqueId}_${group.policy}`)))
    .map(group => group.groupId))
})

const loadAllRules = async () => {
  try {
    const { data } = await ruleApi.getAll(profileId)
    allRules.value = data
    rulesLoaded.value = true
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

const deleteRule = async (row: any) => {
  try {
    await ruleApi.delete(row.id, profileId)

    notify.success('删除成功')
    loadAllRules()
    void loadDns()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

const deleteRuleSet = async (row: any) => {
  try {
    await ruleSetApi.delete(row.id, profileId)

    notify.success('删除成功')
    loadAllRules()
    void loadDns()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

/* ================================================================
 * 一页表单：新建走 compose，编辑沿用规则 / 规则集的 PUT
 * ================================================================ */
const editorOpen = ref(false)
const editorItem = ref<Record<string, any> | null>(null)
const editorSource = ref<'values' | 'library' | 'new_library'>('values')
const recentKeys = ref<Set<string>>(new Set())
let recentTimer: number | undefined

const openEditor = (source: 'values' | 'library' | 'new_library') => {
  editorItem.value = null
  editorSource.value = source
  editorOpen.value = true
}

// 统一的编辑方法：传原始对象，表单不会带上展示用的分组字段
const editItem = (row: any) => {
  editorItem.value = allRules.value.find(r => r.id === row.id && r.itemType === row.itemType) || row
  editorOpen.value = true
}

/**
 * 保存后刷新列表与 DNS，滚动到新增或修改的行并高亮。
 * compose 已返回新规则的 DNS 时直接并入快照；其余保存过的行在 DNS 重新读取完成前显示骨架。
 */
const onEditorSaved = async ({ ids, dns }: { ids: string[]; dns?: Record<string, RuleDnsState> }) => {
  if (dns && dnsSnapshot.value) dnsSnapshot.value = { ...dnsSnapshot.value, rules: { ...dnsSnapshot.value.rules, ...dns } }
  const unknown = ids.filter(id => !dns?.[id])
  if (unknown.length) dnsStale.value = new Set([...dnsStale.value, ...unknown])
  void loadDns()
  await loadAllRules()
  const keys = allRules.value.filter(item => ids.includes(item.id)).map(getItemKey)
  if (!keys.length) return
  revealGroups(ruleGroups.value.filter(group => group.count > 1 && group.items.some((sub: any) => keys.includes(sub.uniqueId))).map(group => group.groupId))
  recentKeys.value = new Set(keys)
  window.clearTimeout(recentTimer)
  recentTimer = window.setTimeout(() => { recentKeys.value = new Set() }, 4000)
  await nextTick()
  rulesContainer.value?.querySelector<HTMLElement>(`[data-rule-key="${keys[0]}"]`)?.scrollIntoView?.({ block: 'center', behavior: 'smooth' })
}

// 来源单独保存后刷新共享内容与 DNS，保留仍在编辑的当前规则草稿。
const onSourceSaved = async (source: Record<string, any>) => {
  ruleLibrary.value = ruleLibrary.value.map(item => item.id === source.id ? source : item)
  const affected = allRules.value.filter(item => item.library_rule_id === source.id).map(item => item.id)
  dnsStale.value = new Set([...dnsStale.value, ...affected])
  await Promise.all([loadRuleLibrary(), loadAllRules(), loadDns()])
}

/** 粘贴时跳过的重复项已停用：「启用 #12」 */
const enableRule = (id: string) => {
  const item = allRules.value.find(r => r.id === id)
  // 传展示用副本，与列表行一致；原始对象由 toggleItemStatus 自己查找并更新
  if (item && !item.enabled) void toggleItemStatus({ ...item })
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

  // 切换状态：只取反一次，item 与 originalItem 可能是同一个对象
  const nextEnabled = !oldEnabled
  item.enabled = nextEnabled
  originalItem.enabled = nextEnabled


  try {
    // DNS 状态单独存放，原始对象里没有 dns，启停永远不是严格的 DNS 修改。
    const { data } = item.itemType === 'rule'
      ? await ruleApi.update(item.id, originalItem, profileId)
      : await ruleSetApi.update(item.id, ruleSetPayload(originalItem), profileId)
    const warnings: string[] = Array.isArray(data?.warnings) ? data.warnings : []
    if (warnings.length) notify.warning('状态已更新', warnings.join('\n'))
    else notify.success('状态已更新')
    void loadDns()
  } catch (error) {
    notify.error(errorMessage(error, '更新状态失败'))
    // 失败后恢复状态
    item.enabled = oldEnabled
    originalItem.enabled = oldEnabled
  }
}

/* ================================================================
 * DNS 处理：独立于规则对象的状态表（GET /rules/dns）
 * ================================================================ */
const dnsSnapshot = ref<RuleDnsSnapshot | null>(null)
const dnsLoading = ref(false)
const dnsError = ref(false)
const dnsPending = ref<Record<string, RuleDnsValue>>({})
/** 快照读取之后保存过、或列表刷新时新出现的规则：快照中的值（或缺省的默认上游）可能已过时 */
const dnsStale = ref<Set<string>>(new Set())
let dnsSequence = 0

/** 使用 OxiDNS 时显示 DNS 列；加载中或读取失败时保留，避免误显示为默认上游 */
const dnsColumn = computed(() => (dnsSnapshot.value ? dnsSnapshot.value.engine.oxidns_in_use : dnsLoading.value || dnsError.value))
const dnsEditable = computed(() => !!dnsSnapshot.value?.engine.editable && !dnsError.value)
/** 读取失败或该规则的 DNS 未知时，表单按未加载处理：字段禁用且不发送 dns */
const editorDns = computed(() => (dnsError.value || (editorItem.value && dnsStale.value.has(editorItem.value.id)) ? null : dnsSnapshot.value))
const groupStale = (group: any) => group.items.some((item: any) => dnsStale.value.has(item.id))

// 列表刷新带来的新行（例如在规则库页添加的引用）在 DNS 重新读取完成前同样视为未知。
watch(() => allRules.value.map(item => item.id), (ids, previous) => {
  if (!dnsLoading.value || !previous) return
  const known = new Set(previous)
  const fresh = ids.filter(id => !known.has(id) && !dnsSnapshot.value?.rules[id])
  if (fresh.length) dnsStale.value = new Set([...dnsStale.value, ...fresh])
})

const loadDns = async () => {
  const sequence = ++dnsSequence
  dnsLoading.value = true
  try {
    const { data } = await ruleApi.getDns(profileId)
    if (sequence !== dnsSequence) return
    if (!data?.engine || !data.rules) throw new Error('invalid DNS state')
    dnsSnapshot.value = { engine: data.engine, upstreams: data.upstreams || [], rules: data.rules, mosdns: data.mosdns || {} }
    dnsStale.value = new Set()
    dnsError.value = false
  } catch {
    if (sequence === dnsSequence) dnsError.value = true
  } finally {
    if (sequence === dnsSequence) dnsLoading.value = false
  }
}

const mergeDns = (rules: Record<string, RuleDnsState>) => {
  if (dnsSnapshot.value) dnsSnapshot.value = { ...dnsSnapshot.value, rules: { ...dnsSnapshot.value.rules, ...rules } }
  // 规则的 DNS 状态可能随之变化，后台刷新一次。
  void loadDns()
}

/** 列表中的快速切换：chip 内显示进度，失败时保持原值 */
const setRuleDns = async (item: any, value: RuleDnsValue) => {
  if (dnsPending.value[item.id]) return
  dnsPending.value = { ...dnsPending.value, [item.id]: value }
  try {
    const { data } = await ruleApi.setDns({ [item.id]: value }, {}, profileId)
    mergeDns(data.rules || {})
    notifyRuleSaved('DNS 已更新', {
      agents: dnsSnapshot.value?.engine.agents,
      scopes: [item.itemType === 'ruleset' ? 'library' : 'rules'],
      pinned: pinnedWithheld(data, isUpstreamValue(value)),
      extra: data.warnings
    })
  } catch (error) {
    notify.error(dnsFailure(error, 'DNS 处理修改失败'))
  } finally {
    const next = { ...dnsPending.value }
    delete next[item.id]
    dnsPending.value = next
  }
}

/** 分组行的只读汇总：设置一致时显示共同值，否则「DNS 混合 3/5」 */
const groupDns = (group: any) => {
  const engine = dnsSnapshot.value?.engine
  if (!engine || groupStale(group)) return { text: '—', title: 'DNS 状态读取失败', tone: 'muted' }
  const counts = new Map<string, number>()
  let drift = false
  for (const item of group.items) {
    const chip = dnsChip(dnsSnapshot.value!.rules[item.id], { ...engine, editable: true })
    drift ||= chip.tone === 'warn'
    counts.set(chip.text, (counts.get(chip.text) || 0) + 1)
  }
  const [top, count] = [...counts].sort((a, b) => b[1] - a[1])[0] || ['—', 0]
  if (counts.size === 1) return { text: top, title: `${group.items.length} 项均为「${top}」`, tone: drift ? 'warn' : 'muted' }
  return { text: `DNS 混合 ${count}/${group.items.length}`, title: `${group.items.length} 项中 ${count} 项为「${top}」`, tone: drift ? 'warn' : 'muted' }
}

const batchOpen = ref(false)
const batchItems = ref<any[]>([])
const batchName = ref('')
const openBatch = (group: any) => {
  batchItems.value = group.items.map((item: any) => allRules.value.find(r => r.id === item.id && r.itemType === item.itemType) || item)
  batchName.value = group.groupName || group.groupDefaultName
  batchOpen.value = true
}

/* ================================================================
 * 策略快速切换：单条改动离开原分组（清空组名），组头改整组并保留组名。
 * 跟随策略的 DNS 由服务端随之调整，单独设置的 DNS 不变。
 * ================================================================ */
const policyPending = ref<Record<string, string>>({})
const ruleName = (item: any) => (item.itemType === 'ruleset' ? item.name || item.id : item.value || item.rule_type || item.id)

/** 改策略后 DNS 仍是单独设置、与新策略不同的项；已停用规则暂存的单独设置同样计入 */
const explicitDnsCount = (items: any[], policy: string) => (dnsColumn.value ? items.filter((item) => {
  const entry = dnsSnapshot.value?.rules[item.id]
  const own = entry && !entry.follow && (entry.state === 'explicit' || entry.state === 'disabled') ? entry.route ?? null : null
  return own !== null && own !== followRoute({ ...item, policy }, dnsSnapshot.value?.engine.policy_routes)
}).length : 0)

const applyPolicy = async (key: string, items: any[], policy: string, options: { groupName?: string } = {}) => {
  if (policyPending.value[key]) return
  policyPending.value = { ...policyPending.value, [key]: policy }
  try {
    const explicit = explicitDnsCount(items, policy)
    const { data } = await ruleApi.setPolicy(items.map(item => item.id), policy, options, profileId)
    // 跟随策略的 DNS 已在服务端改变：重新读取前显示骨架，读取失败显示「—」，不显示旧路由
    dnsStale.value = new Set([...dnsStale.value, ...items.map(item => item.id)])
    void loadDns()
    await loadAllRules()
    // 命中模拟的结果行被改了策略时，结果栏跟着更新
    if (simResult.value?.matched && items.some(item => `${item.itemType}-${item.id}` === simMatchKey.value)) {
      simResult.value = { ...simResult.value, policy }
      simVia.value = await resolveVia(policy)
    }
    notifyRuleSaved(items.length > 1 ? `已将 ${items.length} 项的策略改为「${policy}」` : `策略已改为「${policy}」`, {
      agents: dnsSnapshot.value?.engine.agents,
      scopes: [...new Set(items.map(item => (item.itemType === 'ruleset' ? 'library' as const : 'rules' as const)))],
      pinned: data.pinned_withheld === true,
      extra: [...(explicit ? [`${explicit} 项 DNS 为单独设置，不随策略变化`] : []), ...(data.warnings || [])]
    })
  } catch (error) {
    notify.error(errorMessage(error, '修改策略失败'))
  } finally {
    const next = { ...policyPending.value }
    delete next[key]
    policyPending.value = next
  }
}

const setRulePolicy = (item: any, policy: string) => applyPolicy(item.uniqueId, [item], policy, { groupName: '' })

const setGroupPolicy = async (group: any, policy: string) => {
  const explicit = explicitDnsCount(group.items, policy)
  const dns = !dnsColumn.value ? ''
    : !dnsEditable.value ? 'DNS 分流需在完整参数修改，本次只改策略；跟随策略的规则会显示与策略不一致。'
    : `跟随策略的 DNS 会一起变化${explicit ? `；${explicit} 项 DNS 为单独设置，不随策略变化` : ''}。`
  const accepted = await confirm(`将把「${groupTitle(group)}」的 ${group.count} 项策略从「${group.policy}」改为「${policy}」，组名保留。${dns}`, {
    title: '修改整组策略', confirmText: '修改'
  })
  if (accepted) await applyPolicy(group.groupId, group.items, policy)
}

/** 「统一为跟随策略」：只标记当前 DNS 已与策略一致的规则，生成结果不变 */
const followCandidates = computed(() => {
  const snapshot = dnsSnapshot.value
  if (!snapshot?.engine.editable) return []
  return allRules.value.filter(item => {
    const entry = snapshot.rules[item.id]
    const state = entry?.state || 'default'
    const route = followRoute(item, snapshot.engine.policy_routes)
    if (state === 'explicit') return entry!.route === route
    // 默认上游且策略不单独处理（例如 REJECT）：标记后同样不改变生成结果。
    return state === 'default' && route === null && isOxidnsRule(item)
  })
})

const unifyFollow = async () => {
  const items = followCandidates.value
  if (!items.length) return
  const accepted = await confirm(`将 ${items.length} 条规则标记为跟随策略。它们当前的 DNS 已与策略一致，生成结果不变；之后修改策略时 DNS 会一起变化。`, {
    title: '统一为跟随策略', confirmText: '统一'
  })
  if (!accepted) return
  const merged: Record<string, RuleDnsState> = {}
  try {
    for (let index = 0; index < items.length; index += 200) {
      const routes = Object.fromEntries(items.slice(index, index + 200).map(item => [item.id, 'follow' as const]))
      const { data } = await ruleApi.setDns(routes, {}, profileId)
      Object.assign(merged, data.rules || {})
    }
    notify.success(`已将 ${items.length} 条规则设为跟随策略`)
  } catch (error) {
    notify.error(dnsFailure(error, '统一为跟随策略失败'))
  } finally {
    mergeDns(merged)
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
  window.clearTimeout(recentTimer)
  stopScan()
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


/* ---------- 统一拖动排序 ----------
 * 可见列表是展示项（分组会折叠多条原始规则），排序结果必须展开回
 * 原始 rule_configs。展示项的键是 itemType-id 复合键，说明原始 id 可能
 * 跨类型重复，因此这里沿用后端仍兼容的完整数组格式而不是 ids 契约。
 */
const reorderDisplayItems = ref<any[]>([])
const reorderWarnings: string[] = []

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
    const { data } = await api.post('/rules/reorder', {
      rule_configs: rebuilt.map(item => item.itemType === 'ruleset' ? ruleSetPayload(item) : item)
    }, profileRequestConfig)
    // native OxiDNS 按新顺序重建分流；自定义结构时 DNS 保持不变并返回说明。
    reorderWarnings.splice(0, reorderWarnings.length, ...(Array.isArray(data?.warnings) ? data.warnings : []))
    allRules.value = rebuilt
  }
})

/**
 * 排序单元：收起的分组整组移动，展开的分组逐条移动。展开时的组头不参与排序，
 * 成员去掉组内样式，移出原组后不会被误读为其他组的成员。
 */
const reorderUnits = computed(() => allRulesAndSets.value
  .filter(item => !(item.isGroup && !item.collapsed))
  .map(item => (item.isGroupMember ? { ...item, isGroupMember: false, isLastInGroup: false } : item)))

/** 卡片视图：分组成员后插入通栏分隔，后面的卡片另起一行；排序时不插入，保证每个子元素都可拖动 */
const cardList = computed(() => (reorder.active.value
  ? displayList.value
  : displayList.value.flatMap(item => (item.isLastInGroup ? [item, { isGroupEnd: true, uniqueId: `${item.groupId}-end` }] : [item]))))

const enterReorder = () => {
  reorderDisplayItems.value = [...reorderUnits.value]
  reorder.enter()
}

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    await loadAllRules()
    void loadDns()
    if (reorderWarnings.length) notify.warning('顺序已保存', reorderWarnings.splice(0).join('\n'))
    else notify.success('顺序已保存')
  } catch (error) {
    notify.error(errorMessage(error, '保存顺序失败，顺序已还原'))
  }
}

onMounted(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary(), loadDns()])
})


// 页面激活时重新加载数据（从其他页面返回时）
onActivated(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary(), loadDns()])
})
</script>

<style scoped>
.rule-row.is-scan {
  background: var(--secondary);
}

/* 与两列的行内选择器一致：视觉 30px，透明区域补足 44px 点击区。 */
.rule-group-dns-trigger::before {
  content: '';
  position: absolute;
  inset: calc((30px - var(--cf-touch, 44px)) / 2) 0;
}

.rule-row.is-match {
  background: var(--primary-soft);
  box-shadow: inset 3px 0 0 var(--primary-accent);
}

.rule-row.is-match .rule-index {
  color: var(--primary-accent);
}
</style>
