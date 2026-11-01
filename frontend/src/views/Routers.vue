<template>
  <div>
    <PageHeader v-if="!embedded" title="上游路由" :mobile-help="routerPageHelp">
      <template #actions>
        <Button @click="openCreate">
          <Plus class="size-4" />
          添加路由器
        </Button>
      </template>
    </PageHeader>

    <div v-if="embedded && !(setupDraftSession && routers.length)" class="mb-3 flex items-center justify-end"><Button class="min-h-11" @click="openCreate"><Plus class="size-4" />添加路由器</Button></div>
    <p v-if="!embedded" class="mt-0 mb-4 hidden text-[12.5px] leading-relaxed text-muted-foreground md:block">
      {{ routerPageHelp }}
    </p>

    <LoadingRows v-if="loading && !routers.length" :rows="3" />

    <SectionCard v-else-if="!routers.length" :padded="false">
      <EmptyState
        :icon="RouterIcon"
        title="还没有接入路由器"
        :description="setupDraftSession ? '先填写主路由的管理地址和账号，安装完成后自动关联本次设备。' : '接入 iKuai 4.x 或 RouterOS 7.1+，查看主路由配置与期望状态的差异。'"
      >
        <Button @click="openCreate">
          <Plus class="size-4" />
          添加路由器
        </Button>
      </EmptyState>
    </SectionCard>

    <SectionCard v-else :padded="false" title="路由器">
      <div class="cf-table-wrap">
        <table class="cf-table cf-mobile-records router-records min-w-[880px]" role="table">
          <thead role="rowgroup">
            <tr role="row">
              <th role="columnheader">名称</th>
              <th role="columnheader">类型</th>
              <th role="columnheader">管理地址</th>
              <th role="columnheader">版本</th>
              <th role="columnheader">状态</th>
              <th role="columnheader" class="text-right">操作</th>
            </tr>
          </thead>
          <tbody role="rowgroup">
            <tr role="row" v-for="router in routers" :key="router.id">
              <td role="cell" class="cf-record-wide font-medium">{{ router.name }}</td>
              <td role="cell" data-label="类型">{{ typeLabel(router.type) }}</td>
              <td role="cell" class="cf-record-wide font-mono text-[12px]" data-label="管理地址">{{ router.endpoint }}</td>
              <td role="cell" class="font-mono text-[12px]" data-label="版本">{{ router.last_probe?.ok ? router.last_probe.version : '—' }}</td>
              <td role="cell" data-label="状态">
                <StatusDot
                  class="whitespace-nowrap"
                  :tone="!router.last_probe ? 'muted' : router.last_probe.ok ? 'success' : 'danger'"
                  :label="setupDraftSession ? '已保存，待安装' : !router.last_probe ? '未探测' : router.last_probe.ok ? '可连接' : '连接失败'"
                />
                <p v-if="router.last_probe && !router.last_probe.ok" class="mt-1 mb-0 max-w-[260px] text-[11.5px] text-destructive-accent">
                  {{ router.last_probe.message }}
                </p>
                <!-- 代理网关的转发确定不可用或可能被防火墙拦截时才提示；状态与开启入口在 Agent 页。 -->
                <p v-if="forwardingWarnings[router.id]" class="mt-1 mb-0 max-w-[260px] text-[11.5px] leading-relaxed text-warning-accent" data-router-forwarding-warning>
                  <!-- 设备名不在中间断行，过长时截断并在悬停提示中给出全名。 -->
                  <template v-if="forwardingWarnings[router.id]!.agent">代理网关<span class="inline-block max-w-full truncate align-bottom" :title="forwardingWarnings[router.id]!.agent!">「{{ forwardingWarnings[router.id]!.agent }}」</span></template>{{ forwardingWarnings[router.id]!.problem }}
                  <RouterLink to="/agents" class="inline-flex items-center text-primary-accent underline underline-offset-4 max-md:min-h-11">前往 Agent 页处理</RouterLink>
                </p>
              </td>
              <td role="cell" class="cf-record-wide">
                <div class="router-record-actions flex items-center justify-end gap-1">
                  <template v-if="!setupDraftSession">
                  <Button size="sm" variant="outline" :disabled="busyId === router.id" @click="probe(router)">
                    <Loader2 v-if="busyId === router.id && busyAction === 'probe'" class="size-3.5 animate-spin" />
                    <Activity v-else class="size-3.5" />
                    {{ embedded ? '检查连接' : '探测' }}
                  </Button>
                  <Button size="sm" variant="outline" class="max-md:min-h-11" :disabled="routeBusy || applying || policyLocked" @click="openRouteManager(router)">
                    静态路由
                  </Button>
                  <Button v-if="router.type === 'routeros' && (!embedded || router.ros_policy_rule_sets?.length)" size="sm" variant="outline" class="max-md:min-h-11" :disabled="routeBusy || applying || policyLocked" @click="openPolicyManager(router)">
                    策略路由
                  </Button>
                  <Button v-if="router.type === 'routeros'" size="sm" variant="outline" class="max-md:min-h-11" :disabled="routeBusy || applying || policyLocked" @click="openRuleSync(router)">规则同步</Button>
                  <Button size="sm" :disabled="busyId === router.id || routeBusy || applying || policyLocked" @click="openReport(router)">
                    <Loader2 v-if="busyId === router.id && busyAction === 'report'" class="size-3.5 animate-spin" />
                    <GitCompare v-else class="size-3.5" />
                    {{ embedded ? '查看改动' : '差异报告' }}
                  </Button>
                  </template>
                  <Button variant="ghost" size="icon-sm" title="编辑" aria-label="编辑" @click="openEdit(router)">
                    <Pencil class="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="text-destructive-accent hover:bg-destructive-soft"
                    title="删除"
                    aria-label="删除"
                    @click="remove(router)"
                  >
                    <Trash2 class="size-4" />
                  </Button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <SectionCard v-if="routeRouter" class="mt-4 min-w-0 max-md:[&>header]:flex-col max-md:[&>header]:items-stretch max-md:[&_[data-slot=button]]:min-h-11" :padded="false" :title="`静态路由 · ${routeRouter.name}`"
      description="直接管理这台设备的静态路由，无需关联 Agent。每次写入前先预览并确认。">
      <template #actions>
        <Button size="sm" variant="outline" :disabled="routeLoading || routeBusy" @click="refreshRoutes()">
          <Loader2 v-if="routeLoading" class="size-3.5 animate-spin" />
          刷新路由
        </Button>
        <Button size="sm" :disabled="routeLoading || routeBusy || Boolean(routeError)" @click="openRouteEditor()"><Plus class="size-3.5" />新增路由</Button>
        <Button size="sm" variant="ghost" :disabled="routeBusy" @click="closeRouteManager">收起路由列表</Button>
      </template>
      <p class="m-0 px-5 pb-4 text-[12px] leading-relaxed text-muted-foreground max-md:px-4 max-sm:px-3">
        此处只修改选中的静态路由。带 cf 标记的路由可能被后续自动发布调整；NAT、DHCP DNS 和 mihomo 回流配置不随手工操作变化。
      </p>
      <p v-if="routeBusy" role="status" class="m-0 flex items-center gap-2 px-5 pb-4 text-[12px] text-muted-foreground max-md:px-4 max-sm:px-3"><Loader2 class="size-3.5 animate-spin" />正在准备或执行路由变更…</p>
      <p v-if="routeError" role="alert" class="m-0 break-words px-5 pb-4 text-[12.5px] text-destructive-accent max-md:px-4 max-sm:px-3">{{ routeError }}</p>
      <LoadingRows v-if="routeLoading" :rows="3" />
      <div v-else-if="routeRows.length" class="cf-table-wrap">
        <table class="cf-table min-w-[920px]">
          <thead><tr><th>目的网段</th><th>下一跳</th><th>路由表</th><th>状态</th><th>备注 / 管理说明</th><th class="text-right">操作</th></tr></thead>
          <tbody>
            <tr v-for="item in routeRows" :key="item.id">
              <td class="whitespace-nowrap font-mono text-[12px]">{{ item.cidr }}</td>
              <td class="whitespace-nowrap font-mono text-[12px]">{{ item.gateway || '—' }}</td>
              <td class="font-mono text-[12px]">{{ item.table || '默认' }}</td>
              <td><span :class="['chip', item.enabled ? 'chip-ok' : '']">{{ item.enabled ? '已启用' : '已停用' }}</span></td>
              <td class="min-w-[240px] max-w-[360px] whitespace-normal break-words text-[12px]">
                <p class="m-0">{{ item.comment || '无备注' }}</p>
                <p v-if="item.managed" class="mt-1 mb-0 text-warning-accent">由自动发布管理</p>
                <p v-if="!item.editable" class="mt-1 mb-0 text-muted-foreground">只读：{{ item.reason || '该路由不支持手工修改' }}</p>
              </td>
              <td><div class="flex justify-end gap-1">
                <Button size="sm" variant="outline" :aria-label="`编辑路由 ${item.cidr}`" :disabled="!item.editable || routeBusy || Boolean(routeError)" @click="openRouteEditor(item)">编辑</Button>
                <Button size="sm" variant="ghost" class="text-destructive-accent hover:bg-destructive-soft" :aria-label="`删除路由 ${item.cidr}`" :disabled="!item.editable || routeBusy || Boolean(routeError)" @click="previewRouteChange({ action: 'delete', id: item.id, revision: item.revision })">删除</Button>
              </div></td>
            </tr>
          </tbody>
        </table>
      </div>
      <EmptyState v-else-if="!routeError" :icon="RouterIcon" title="没有静态路由" description="可新增目的网段和下一跳，确认变更后写入这台路由器。" />
    </SectionCard>

    <PolicyRoutingPanel v-if="policyRouter" ref="policyPanel" :id="policyRouter.id" :key="policyRouter.id" :name="policyRouter.name" :profile="activeProfileId"
      @saved="load" @busy="policyLocked = $event" @close="policyRouter = null" />
    <RouterRuleSync v-if="syncRouter" class="mt-4" :id="syncRouter.id" :name="syncRouter.name" :profile="activeProfileId"
      :disabled="routeBusy || applying" @busy="policyLocked = $event" @preview="openReport(syncRouter)" />

    <Dialog :open="routeEditorVisible" @update:open="value => { if (!routeBusy) routeEditorVisible = value }">
      <DialogContent class="max-w-[480px] max-md:[&_[data-slot=button]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-w-11" :show-close-button="!routeBusy">
        <DialogHeader>
          <DialogTitle>{{ routeEditing ? '编辑路由' : '新增路由' }} · {{ routeRouter?.name }}</DialogTitle>
          <DialogDescription>填写 IPv4 目的网段和下一跳，预览完整变更后再确认写入。</DialogDescription>
        </DialogHeader>
        <form @submit.prevent="submitRoute">
          <fieldset :disabled="routeBusy" class="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
            <div class="flex flex-col gap-1.5"><Label for="static-route-cidr">目的网段（IPv4 CIDR）</Label><Input id="static-route-cidr" v-model="routeForm.cidr" class="font-mono" placeholder="例如 192.168.20.0/24" /></div>
            <div class="flex flex-col gap-1.5"><Label for="static-route-gateway">下一跳（IPv4）</Label><Input id="static-route-gateway" v-model="routeForm.gateway" class="font-mono" placeholder="例如 192.168.1.2" /></div>
            <div class="flex flex-col gap-1.5"><Label for="static-route-comment">备注</Label><Textarea id="static-route-comment" v-model="routeForm.comment" :rows="3" placeholder="可选，最多 512 个字符" /></div>
            <div class="flex min-h-11 items-center justify-between gap-3"><Label for="static-route-enabled" class="flex min-h-11 flex-1 items-center">启用路由</Label><Switch id="static-route-enabled" :disabled="routeBusy" :model-value="routeForm.enabled" @update:model-value="value => (routeForm.enabled = Boolean(value))" /></div>
            <p v-if="routeEditing?.managed" class="m-0 text-[12px] text-warning-accent">此路由带 cf 标记，后续自动发布可能调整本次手工变更。</p>
            <p v-if="routeFormError" role="alert" class="m-0 break-words text-[12px] text-destructive-accent">{{ routeFormError }}</p>
          </fieldset>
          <DialogFooter class="mt-5">
            <Button type="button" variant="outline" :disabled="routeBusy" @click="routeEditorVisible = false">取消</Button>
            <Button type="submit" :disabled="routeBusy || routeLoading"><Loader2 v-if="routeBusy" class="size-4 animate-spin" />预览路由变更</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <SectionCard v-if="reportRouter && !report" class="mt-4 scroll-mt-20" data-router-report :title="`差异报告 · ${reportRouter.name}`">
      <p v-if="reportLoading" role="status" class="m-0 flex items-center gap-2 text-muted-foreground"><Loader2 class="size-4 animate-spin" />正在生成差异报告…</p>
      <template v-else-if="reportError">
        <p role="alert" class="mt-0 break-words text-destructive-accent">{{ reportError }}</p>
        <Button variant="outline" @click="loadReport(reportRouter)">重试</Button>
      </template>
    </SectionCard>

    <template v-if="report">
      <SectionCard class="mt-4 scroll-mt-20" data-router-report :title="`差异报告 · ${reportRouter?.name}`" :description="`生成于 ${reportTime}`" persistent-description>
        <template #actions>
          <span :class="['chip', report.in_sync ? 'chip-ok' : 'chip-warn']">
            {{ report.in_sync ? '已全部同步' : reportSummary.unknown ? '存在待确认项' : '存在差异' }}
          </span>
          <Button size="sm" :disabled="planLoading" @click="openPlan">
            <Loader2 v-if="planLoading" class="size-3.5 animate-spin" />
            <Rocket v-else class="size-3.5" />
            预览发布
          </Button>
        </template>
        <div class="flex flex-wrap gap-2">
          <span v-for="(count, key) in reportSummary" :key="key" :class="['chip', statusInfo(key).chip]">
            {{ statusInfo(key).label }} <span class="num">{{ count }}</span>
          </span>
        </div>
        <p v-if="reportRouter?.last_release?.ok === false" class="mt-3 break-words text-[13px] text-destructive-accent" data-router-last-release>
          上次发布失败：{{ reportRouter.last_release.message || `阶段「${reportRouter.last_release.failed_stage || '未知'}」未完成，请检查差异后重新预览。` }}
        </p>
        <dl class="mt-4 mb-0 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[12.5px]">
          <dt class="text-muted-foreground">fake-ip 网段</dt>
          <dd class="m-0 font-mono">{{ report.desired.fake_ip_range }}</dd>
          <dt class="text-muted-foreground">代理网关（下一跳）</dt>
          <dd class="m-0 font-mono">{{ report.desired.proxy_gateway || '未关联' }}</dd>
        </dl>
        <ul v-if="report.desired.warnings.length" class="mt-4 mb-0 flex list-none flex-col gap-1.5 p-0">
          <li
            v-for="warning in report.desired.warnings"
            :key="warning"
            class="flex items-start gap-2 rounded-md bg-warning-soft px-3 py-2 text-[12.5px] text-warning-accent"
          >
            <TriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
            {{ warning }}
          </li>
        </ul>
      </SectionCard>

      <SectionCard class="mt-4" :padded="false" title="代理路由" description="期望的代理网段及其下一跳是否已在主路由上配置。">
        <div class="cf-table-wrap">
          <table class="cf-table">
            <thead>
              <tr><th>网段</th><th>来源</th><th>状态</th><th>说明</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in report.routes.items" :key="item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td class="text-[12px] text-muted-foreground">{{ item.sources.join('、') }}</td>
                <td><span :class="['chip', statusInfo(item.status).chip]">{{ statusInfo(item.status).label }}</span></td>
                <td class="text-[12px] text-muted-foreground">{{ item.detail || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="ipv4ForwardHint" class="flex min-w-0 flex-col gap-2 border-t border-border px-5 py-4 max-md:px-4 max-sm:px-3" data-router-ipv4-forward-hint>
          <p role="status" class="m-0 text-[12.5px] leading-relaxed text-warning-accent [overflow-wrap:anywhere]">{{ ipv4ForwardHint.warning }}</p>
          <div v-if="ipv4ForwardHint.fix_command" class="min-w-0 space-y-1.5 text-[12.5px]" data-router-ipv4-forward-command>
            <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
              <p class="m-0 min-w-0 font-medium">在 RouterOS 终端执行</p>
              <Button variant="ghost" size="sm" class="shrink-0 px-2 text-xs max-md:min-h-11" aria-label="复制 IPv4 同口转发放行命令" @click="copyCommand(ipv4ForwardHint.fix_command)">
                <Copy class="size-3.5" aria-hidden="true" />复制
              </Button>
            </div>
            <pre class="m-0 min-w-0 overflow-x-auto whitespace-pre rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs leading-relaxed text-foreground">{{ ipv4ForwardHint.fix_command }}</pre>
            <p class="m-0 text-muted-foreground">只放行从这个 LAN 口进、又从同一口转出的连接，放在实际丢弃它们的规则之前；执行后重新生成差异报告确认。</p>
          </div>
        </div>
        <div v-if="ipv6FakeIP" class="min-w-0 border-t border-border" data-router-ipv6-fakeip>
          <div class="flex min-w-0 flex-col gap-2 px-5 py-4 max-md:px-4 max-sm:px-3">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h3 class="m-0 min-w-0 text-[13px] font-semibold">IPv6 Fake-IP 分流路由</h3>
              <span :class="['chip', ipv6FakeIPStatusInfo(ipv6FakeIP.status).chip]">{{ ipv6FakeIPStatusInfo(ipv6FakeIP.status).label }}</span>
            </div>
            <p class="m-0 text-[12.5px] leading-relaxed text-muted-foreground">根据已保存的 Mihomo 配置，把对应 IPv6 网段交给 Mihomo；改动随上方「预览发布」一起应用。</p>
            <p v-for="blocker in ipv6FakeIP.blockers || []" :key="blocker" role="alert" class="m-0 text-[12.5px] leading-relaxed text-destructive-accent [overflow-wrap:anywhere]">{{ blocker }}</p>
            <p v-for="warning in ipv6FakeIP.warnings || []" :key="warning" role="status" class="m-0 text-[12.5px] leading-relaxed text-warning-accent [overflow-wrap:anywhere]">{{ warning }}</p>
            <div v-if="ipv6FakeIP.fix_command" class="min-w-0 space-y-1.5 text-[12.5px]" data-router-ipv6-fakeip-command>
              <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
                <p class="m-0 min-w-0 font-medium">在 RouterOS 终端执行</p>
                <Button variant="ghost" size="sm" class="shrink-0 px-2 text-xs max-md:min-h-11" aria-label="复制 IPv6 同口转发放行命令" @click="copyCommand(ipv6FakeIP.fix_command)">
                  <Copy class="size-3.5" aria-hidden="true" />复制
                </Button>
              </div>
              <pre class="m-0 min-w-0 overflow-x-auto whitespace-pre rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs leading-relaxed text-foreground">{{ ipv6FakeIP.fix_command }}</pre>
              <p class="m-0 text-muted-foreground">只放行从这个 LAN 口进、又从同一口转出的连接，放在「drop invalid」之前；执行后重新生成差异报告确认。</p>
            </div>
            <p v-if="!ipv6FakeIP.items.length" class="m-0 text-[12.5px] text-muted-foreground">{{ !ipv6FakeIP.enabled ? '当前保存的配置未启用 IPv6 Fake-IP。' : '暂无可确认的 IPv6 Fake-IP 路由，请查看上方提示。' }}</p>
          </div>
          <div v-if="ipv6FakeIP.items.length" class="cf-table-wrap">
            <table class="cf-table cf-mobile-records w-full table-fixed" aria-label="IPv6 Fake-IP 分流路由" role="table">
              <thead role="rowgroup"><tr role="row"><th role="columnheader">网段</th><th role="columnheader">下一跳</th><th role="columnheader">状态</th><th role="columnheader">说明</th></tr></thead>
              <tbody role="rowgroup">
                <tr v-for="(item, index) in ipv6FakeIP.items" :key="`${item.cidr}-${index}`" role="row">
                  <td role="cell" data-label="网段" class="min-w-0 font-mono text-[12px] whitespace-normal [overflow-wrap:anywhere]">{{ item.cidr || '未确定' }}</td>
                  <td role="cell" data-label="下一跳" class="min-w-0 font-mono text-[12px] whitespace-normal [overflow-wrap:anywhere]">{{ item.gateway || '未确定' }}</td>
                  <td role="cell" data-label="状态"><span :class="['chip', ipv6FakeIPStatusInfo(item.status).chip]">{{ ipv6FakeIPStatusInfo(item.status).label }}</span></td>
                  <td role="cell" data-label="说明" class="cf-record-wide min-w-0 text-[12px] whitespace-normal text-muted-foreground [overflow-wrap:anywhere]">
                    <p class="m-0">{{ item.detail || '—' }}</p>
                    <p v-if="item.status === 'borrowed'" class="mt-1 mb-0">本次仅沿用，不接管；关闭 IPv6 Fake-IP 后不会自动撤销这条路由。</p>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div v-if="ipv6Routes" class="min-w-0 border-t border-border" data-router-ipv6-routes>
          <div class="flex min-w-0 flex-col gap-2 px-5 py-4 max-md:px-4 max-sm:px-3">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h3 class="m-0 min-w-0 text-[13px] font-semibold">IPv6 规则集路由</h3>
              <span :class="['chip', ipv6FakeIPStatusInfo(ipv6RoutesStatus).chip]">{{ ipv6FakeIPStatusInfo(ipv6RoutesStatus).label }}</span>
              <FieldHelp label="IPv6 规则集路由说明" :text="routerCopy.ipv6Routes" />
            </div>
            <p class="m-0 text-[12.5px] leading-relaxed text-muted-foreground">
              {{ ipv6Routes.enabled ? '所选规则集中的公网 IPv6 网段指向这台 Mihomo；改动随上方「预览发布」一起应用。' : 'Mihomo 已关闭 IPv6，发布时撤回本功能添加的 IPv6 路由。' }}
              <template v-if="ipv6Routes.gateway">下一跳 <span class="font-mono [overflow-wrap:anywhere]">{{ ipv6Routes.gateway }}</span>。</template>
            </p>
            <p v-for="reason in ipv6Routes.skip" :key="reason" role="alert" class="m-0 text-[12.5px] leading-relaxed text-warning-accent [overflow-wrap:anywhere]">{{ reason }}</p>
            <p v-if="ipv6Routes.skip.length" class="m-0 text-[12.5px] text-muted-foreground">其他路由、规则和 DNS 照常发布，只有这一项保留现状。</p>
            <p v-for="warning in ipv6RoutesWarnings" :key="warning" role="status" class="m-0 text-[12.5px] leading-relaxed text-warning-accent [overflow-wrap:anywhere]">{{ warning }}</p>
            <p v-if="ipv6RoutesFixShared" class="m-0 text-[12.5px] leading-relaxed text-warning-accent" data-router-ipv6-routes-fix-shared>IPv6 防火墙会丢弃同口转发的连接，放行命令见上方「IPv6 Fake-IP 分流路由」，同一条命令覆盖这两项。</p>
            <div v-else-if="ipv6Routes.fix_command" class="min-w-0 space-y-1.5 text-[12.5px]" data-router-ipv6-routes-command>
              <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
                <p class="m-0 min-w-0 font-medium">在 RouterOS 终端执行</p>
                <Button variant="ghost" size="sm" class="shrink-0 px-2 text-xs max-md:min-h-11" aria-label="复制 IPv6 同口转发放行命令" @click="copyCommand(ipv6Routes.fix_command)">
                  <Copy class="size-3.5" aria-hidden="true" />复制
                </Button>
              </div>
              <pre class="m-0 min-w-0 overflow-x-auto whitespace-pre rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs leading-relaxed text-foreground">{{ ipv6Routes.fix_command }}</pre>
              <p class="m-0 text-muted-foreground">只放行从这个 LAN 口进、又从同一口转出的连接，放在「drop invalid」之前；执行后重新生成差异报告确认。</p>
            </div>
            <p v-if="!ipv6Routes.items.length" class="m-0 text-[12.5px] text-muted-foreground">所选规则集没有需要处理的公网 IPv6 网段。</p>
          </div>
          <div v-if="ipv6Routes.items.length && !ipv6Routes.skip.length" class="cf-table-wrap">
            <table class="cf-table cf-mobile-records w-full table-fixed" aria-label="IPv6 规则集路由" role="table">
              <thead role="rowgroup"><tr role="row"><th role="columnheader">网段</th><th role="columnheader">来源</th><th role="columnheader">状态</th><th role="columnheader">说明</th></tr></thead>
              <tbody role="rowgroup">
                <tr v-for="item in ipv6Routes.items" :key="item.cidr" role="row">
                  <td role="cell" data-label="网段" class="min-w-0 font-mono text-[12px] whitespace-normal [overflow-wrap:anywhere]">{{ item.cidr }}</td>
                  <td role="cell" data-label="来源" class="min-w-0 text-[12px] whitespace-normal text-muted-foreground">{{ item.sources.join('、') || '—' }}</td>
                  <td role="cell" data-label="状态"><span :class="['chip', ipv6FakeIPStatusInfo(item.status).chip]">{{ ipv6FakeIPStatusInfo(item.status).label }}</span></td>
                  <td role="cell" data-label="说明" class="cf-record-wide min-w-0 text-[12px] whitespace-normal text-muted-foreground [overflow-wrap:anywhere]">
                    <p class="m-0">{{ item.detail || '—' }}</p>
                    <p v-if="item.status === 'borrowed'" class="mt-1 mb-0">本次仅沿用，不接管；关闭 Mihomo IPv6 后不会自动撤销这条路由。</p>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </SectionCard>

      <SectionCard
        v-if="report.policy_routing"
        class="mt-4"
        :padded="false"
        title="按设备和目标 IP 分流"
        persistent-description
        :description="report.policy_routing.enabled
          ? '指定设备访问下表 IP 网段时，将流量交给 Mihomo 处理。修改后需发布到路由器才会生效。'
          : '本次未选择按设备分流的规则集。'"
        help="设备 IP 和访问的目标 IP 都符合条件时，才会使用这项策略；其他流量仍按 ROS 原有路由处理。

设备范围来自当前配置空间已保存的 OxiDNS「智能分流」设置。范围重叠时，更具体的 IP 或网段设置优先；Mihomo 网关本身不参与。这里统计的是地址或网段条数，不是设备台数。

Fake-IP 和未选入这项策略的规则集，仍按上方的静态路由设置处理。

「已同步」表示路由器配置与当前设置一致，不代表已测试上网是否正常。ROS 内部通过地址列表、Mangle 标记规则和独立路由表实现分流。"
      >
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] max-md:px-4 max-sm:px-3">
          <p v-if="report.policy_routing.enabled" class="m-0 text-muted-foreground">
            设备范围来自当前配置空间的 OxiDNS「智能分流」设置。
          </p>
          <p v-else class="m-0 text-muted-foreground">发布后将移除本功能的分流配置，相关网段按上方的静态路由设置处理。未发布前保持原样。</p>
          <details v-if="report.policy_routing.sources.length" class="rounded-md border border-border">
            <summary class="min-h-11 cursor-pointer rounded-md px-3 py-3 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">查看适用的设备 IP · <span class="num">{{ report.policy_routing.sources.length }}</span> 个 IPv4 地址或网段</summary>
            <div class="max-h-40 overflow-y-auto px-3 pb-3 font-mono text-[12px]">
              <p v-for="source in report.policy_routing.sources" :key="source" class="m-0">{{ source }}</p>
            </div>
          </details>
          <p v-for="blocker in report.policy_routing.blockers" :key="blocker" role="alert" class="m-0 break-words text-warning-accent">{{ blocker }}</p>
        </div>
        <div v-if="report.policy_routing.items.length" class="cf-table-wrap">
          <table class="cf-table min-w-[440px]">
            <thead><tr><th>访问的目标 IP / 网段</th><th>所属规则集</th><th>配置状态</th></tr></thead>
            <tbody>
              <tr v-for="item in report.policy_routing.items" :key="item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td class="text-[12px] text-muted-foreground">{{ item.sources.join('、') }}</td>
                <td><span :class="['chip', statusInfo(item.status).chip]">{{ statusInfo(item.status).label }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>

      <SectionCard class="mt-4" :padded="false" title="NAT 免转换" description="先判断发往代理网段的流量是否可能受源 NAT 影响；未受影响时无需额外规则，证据不足时标为待确认。">
        <div class="cf-table-wrap">
          <table class="cf-table">
            <thead>
              <tr><th>网段</th><th>状态</th><th>说明</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in report.nat_bypass.items" :key="item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td><span :class="['chip', statusInfo(item.status).chip]">{{ statusInfo(item.status).label }}</span></td>
                <td class="text-[12px] text-muted-foreground">{{ item.detail || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="report.nat_bypass.disabled_rules.length" class="m-0 px-5 pb-4 text-[12px] text-muted-foreground max-md:px-4 max-sm:px-3">
          另有 {{ report.nat_bypass.disabled_rules.length }} 条已停用的免转换规则：
          {{ report.nat_bypass.disabled_rules.map(disabledNatRuleLabel).join('、') }}
        </p>
      </SectionCard>

      <SectionCard class="mt-4" title="回流规则（旁路网关）" description="按需管理 mihomo 主机上将回包送回主路由的规则，自动配置默认关闭。">
        <template #actions>
          <StatusDot :tone="returnTone" :label="returnLabel" />
        </template>
        <p v-if="report.return_routing.status !== 'ok'" class="m-0 text-[12.5px] text-muted-foreground">
          {{ report.return_routing.message }}
        </p>
        <div v-else class="flex flex-col gap-3 text-[12.5px]">
          <div v-if="!report.return_routing.nft_available || !report.return_routing.iproute_available" class="text-warning-accent">
            旁路网关缺少 {{ [!report.return_routing.nft_available && 'nftables', !report.return_routing.iproute_available && 'iproute2'].filter(Boolean).join('、') }}，无法完整检查。
          </div>
          <div>
            <p class="mt-0 mb-1 text-muted-foreground">连接标记表</p>
            <p v-if="!report.return_routing.return_tables?.length" class="m-0">未发现</p>
            <p v-for="table in report.return_routing.return_tables" :key="table.name" class="m-0 font-mono">
              {{ table.family }} {{ table.name }} · {{ table.marks.join(', ') }}
            </p>
          </div>
          <div>
            <p class="mt-0 mb-1 text-muted-foreground">按标记的策略路由</p>
            <p v-if="!report.return_routing.fwmark_rules?.length" class="m-0">未发现</p>
            <div v-for="rule in report.return_routing.fwmark_rules" :key="rule.priority" class="font-mono">
              <p class="m-0">优先级 {{ rule.priority }} · fwmark {{ rule.mark }} → 表 {{ rule.table }}</p>
              <p v-for="line in report.return_routing.table_routes?.[rule.table] || []" :key="line" class="m-0 pl-4 text-muted-foreground">
                {{ line }}
              </p>
            </div>
          </div>
          <p v-if="report.return_routing.tun_devices?.length" class="m-0 text-muted-foreground">
            TUN 网卡：<span class="font-mono">{{ report.return_routing.tun_devices.join(', ') }}</span>
          </p>
        </div>
      </SectionCard>

      <SectionCard
        v-if="dhcpDns"
        class="mt-4"
        :padded="false"
        title="DHCP DNS"
        description="通过 IPv4 DHCP 下发 DNS，只更新包含所选 mihomo 网关的唯一 DHCP 网段；未关联 OxiDNS Agent 时保留现有 DHCP DNS。"
      >
        <template #actions>
          <StatusDot :tone="dhcpDnsStatus.tone" :label="dhcpDnsStatus.label" />
        </template>
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] max-md:px-4 max-sm:px-3">
          <p v-if="dhcpDns.message" class="m-0 text-muted-foreground">{{ dhcpDns.message }}</p>
          <p v-if="dhcpDns.target_dns" class="m-0">
            目标 DNS：<span class="font-mono">{{ dhcpDns.target_dns }}</span>
          </p>
          <ul v-if="dhcpDns.blockers.length" class="m-0 list-disc pl-5 text-warning-accent">
            <li v-for="blocker in dhcpDns.blockers" :key="blocker">{{ blocker }}</li>
          </ul>
        </div>
        <div v-if="dhcpDns.items.length" class="cf-table-wrap">
          <table class="cf-table min-w-[760px]">
            <thead>
              <tr><th>网段</th><th>当前 DNS</th><th>下发 DNS</th><th>状态</th><th>说明</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in dhcpDns.items" :key="item.id">
                <td class="whitespace-nowrap font-mono text-[12px]">{{ item.network }}</td>
                <td class="whitespace-nowrap font-mono text-[12px]">{{ item.current_dns.length ? item.current_dns.join('、') : '自动/未指定' }}</td>
                <td class="whitespace-nowrap font-mono text-[12px]">{{ item.desired_dns.length ? item.desired_dns.join('、') : '待确认' }}</td>
                <td><span :class="['chip', statusInfo(item.status).chip]">{{ statusInfo(item.status).label }}</span></td>
                <td class="min-w-[240px] whitespace-normal break-words text-[12px] text-muted-foreground">{{ item.detail || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="dhcpDns.status !== 'disabled'" class="m-0 px-5 pb-4 text-[12px] leading-relaxed text-muted-foreground max-md:px-4 max-sm:px-3">
          发布会将 DNS 列表替换为所选服务器，不保留旧备用 DNS。终端在 DHCP 续租或重新联网后获得新 DNS。
        </p>
      </SectionCard>

      <SectionCard
        v-if="ipv6Dns"
        data-router-ipv6-dns
        class="mt-4"
        :padded="false"
        title="IPv6 DNS 下发"
        help="关联 OxiDNS 后，核对同一 LAN 是否通过 IPv6 路由器通告（RA）另发 DNS。关闭 DNS 下发可以避免客户端绕过 DHCP 指定的 OxiDNS；IPv6 地址和默认网关继续通告。改动先进入「预览发布」，确认后才应用。客户端已缓存的 DNS 可能要等过期或重新联网才消失。"
      >
        <template #actions>
          <StatusDot :tone="ipv6DnsStatus.tone" :label="ipv6DnsStatus.label" />
        </template>
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] leading-relaxed max-md:px-4 max-sm:px-3">
          <p v-if="ipv6Dns.interface" class="m-0">
            目标 LAN：<span class="font-mono [overflow-wrap:anywhere]">{{ ipv6Dns.interface }}</span>
          </p>
          <p class="m-0">{{ ipv6DnsMessage }}</p>
          <p v-if="ipv6Dns.status === 'pending'" class="m-0 text-muted-foreground">
            在「预览发布」确认后关闭；IPv6 地址和默认网关继续通告。
          </p>
          <p v-for="blocker in ipv6Dns.blockers" :key="blocker" role="alert" class="m-0 break-words text-warning-accent">{{ blocker }}</p>
          <p v-for="warning in ipv6Dns.warnings" :key="warning" role="status" class="m-0 break-words text-warning-accent">{{ warning }}</p>
          <p v-if="ipv6Dns.blockers.length" class="m-0 text-muted-foreground">其他路由、规则和 DHCP DNS 照常发布，只有这一项保留现状。</p>
          <div v-if="ipv6Dns.fix_command" class="min-w-0 space-y-1.5" data-router-ipv6-dns-command>
            <div class="flex min-h-8 items-center justify-between gap-2 max-md:min-h-11">
              <p class="m-0 min-w-0 font-medium">在 RouterOS 终端执行</p>
              <Button variant="ghost" size="sm" class="shrink-0 px-2 text-xs max-md:min-h-11" aria-label="复制 IPv6 通告命令" @click="copyIPv6DnsCommand">
                <Copy class="size-3.5" aria-hidden="true" />复制
              </Button>
            </div>
            <pre class="m-0 min-w-0 overflow-x-auto whitespace-pre rounded-md border border-border bg-muted px-3 py-2 font-mono text-xs leading-relaxed text-foreground">{{ ipv6Dns.fix_command }}</pre>
            <p class="m-0 text-muted-foreground">为 {{ ipv6Dns.interface }} 单独建立不下发 DNS 的通告，其余参数照抄当前 all 条目；all 保持不变，其他接口不受影响。执行后重新生成差异报告确认。</p>
          </div>
        </div>
      </SectionCard>

      <SectionCard
        v-if="dnsGuard"
        class="mt-4"
        :padded="false"
        title="强制 DNS"
        description="写死公共 DNS 的设备，其 53 端口查询经路由器转给 OxiDNS；只管理本功能创建并核实归属的对象。"
      >
        <template #actions>
          <StatusDot :tone="dnsGuardStatus.tone" :label="dnsGuardStatus.label" />
        </template>
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] max-md:px-4 max-sm:px-3">
          <p v-if="dnsGuard.message" class="m-0 text-muted-foreground">{{ dnsGuard.message }}</p>
          <ul v-if="dnsGuard.blockers.length || dnsGuard.warnings.length" class="m-0 list-disc pl-5 text-warning-accent">
            <li v-for="blocker in dnsGuard.blockers" :key="blocker">{{ blocker }}</li>
            <li v-for="warning in dnsGuard.warnings" :key="warning">{{ warning }}</li>
          </ul>
        </div>
        <div v-if="dnsGuard.items.length" class="cf-table-wrap">
          <table class="cf-table min-w-[560px]">
            <thead>
              <tr><th>对象</th><th>作用</th><th>状态</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in dnsGuard.items" :key="item.role">
                <td class="whitespace-nowrap">{{ item.object }}</td>
                <td class="min-w-[240px] whitespace-normal break-words font-mono text-[12px]">{{ item.detail }}</td>
                <td>
                  <span v-if="item.paused" class="chip chip-warn">已暂停</span>
                  <span v-else :class="['chip', statusInfo(item.status).chip]">{{ statusInfo(item.status).label }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>

      <SectionCard
        v-if="pdGuard"
        data-router-ipv6-pd-guard
        class="mt-4"
        :padded="false"
        title="IPv6 前缀变化保护"
        :help="routerCopy.ipv6PdGuard.help"
      >
        <template #actions>
          <StatusDot :tone="pdGuardStatus.tone" :label="pdGuardStatus.label" />
        </template>
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] leading-relaxed max-md:px-4 max-sm:px-3">
          <p v-if="pdGuard.lan" class="m-0">
            监控 LAN：<span class="font-mono [overflow-wrap:anywhere]">{{ pdGuard.lan }}</span>
            · 地址池 <span class="font-mono [overflow-wrap:anywhere]">{{ pdGuard.pool }}</span>
            <template v-if="pdGuard.client"> · DHCP 客户端 <span class="font-mono [overflow-wrap:anywhere]">{{ pdGuard.client }}</span></template>
          </p>
          <!-- A blocked plan's message is its first blocker: shown once, in the warning color below. -->
          <p v-if="pdGuard.message && !pdGuard.blockers.includes(pdGuard.message)" class="m-0">{{ pdGuard.message }}</p>
          <p v-if="pdGuard.status === 'pending'" class="m-0 text-muted-foreground">在「预览发布」确认后才写入路由器。</p>
          <p v-for="blocker in pdGuard.blockers" :key="blocker" role="alert" class="m-0 break-words text-warning-accent">{{ blocker }}</p>
          <p v-for="warning in pdGuard.warnings" :key="warning" role="status" class="m-0 break-words text-warning-accent">{{ warning }}</p>
          <p v-if="pdGuard.blockers.length" class="m-0 text-muted-foreground">其他路由、规则和 DNS 改动照常发布，只有这一项保留现状。</p>
          <p v-if="pdGuard.rollback_script" class="m-0 text-muted-foreground">
            路由器上还有手动安装留下的卸载脚本 <span class="font-mono">cf-pd-rollback</span>；ConfigFlow 不使用也不删除它。
          </p>
        </div>
        <div v-if="pdGuard.items.length" class="cf-table-wrap">
          <table class="cf-table min-w-[560px]">
            <thead>
              <tr><th>对象</th><th>说明</th><th>状态</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in pdGuard.items" :key="item.role">
                <td class="whitespace-nowrap">{{ item.object }}</td>
                <td class="min-w-[240px] whitespace-normal break-words text-[12px] text-muted-foreground">{{ item.detail || '—' }}</td>
                <td><span :class="['chip', pdGuardItemInfo(item.status).chip]">{{ pdGuardItemInfo(item.status).label }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>

      <SectionCard
        v-if="report.routes.unmanaged_other.length || report.routes.extra.length || report.routes.retired_policy?.length"
        class="mt-4"
        :padded="false"
        title="其他路由"
        persistent-description
        :description="report.routes.mode === 'policy'
          ? '指向代理网关的静态路由会让 mihomo 自身流量回环：手工路由将迁入只增不删的策略路由「cf-mihomo-manual」后删除；多余项为 ConfigFlow 标记但已不再需要的网段。'
          : report.routes.retired_policy?.length
            ? '未开启自动配置回流：ConfigFlow 写入的端口分流将改回静态路由后删除，手工网段恢复为手工静态路由。端口分流需要回流配合，否则连接会被主路由中断。'
            : '指向代理网关但不由期望状态推导的手工路由不会被修改；多余项为 ConfigFlow 标记但已不再需要的路由。'"
      >
        <template #actions><Button v-if="reportRouter" size="sm" variant="outline" :disabled="applying" @click="openRouteManager(reportRouter)">静态路由</Button></template>
        <div class="cf-table-wrap">
          <table class="cf-table">
            <thead>
              <tr><th>网段</th><th>下一跳</th><th>类别</th><th>备注</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in report.routes.unmanaged_other" :key="'u' + item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td class="font-mono text-[12px]">{{ item.gateway }}</td>
                <td>
                  <span v-if="item.migrate" class="chip chip-sky">手工路由 · 将迁移为策略路由</span>
                  <span v-else class="chip chip-acc">手工路由{{ item.enabled ? '' : '（已停用）' }}</span>
                </td>
                <td class="text-[12px] text-muted-foreground">{{ item.comment || '—' }}</td>
              </tr>
              <tr v-for="item in report.routes.retired_policy || []" :key="'r' + item.comment + item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td class="font-mono text-[12px]">{{ item.gateway }}</td>
                <td><span class="chip chip-sky">端口分流 · {{ item.restore ? '将恢复为手工静态路由' : '将改回静态路由' }}</span></td>
                <td class="text-[12px] text-muted-foreground">{{ item.comment }}</td>
              </tr>
              <tr v-for="item in report.routes.extra" :key="'e' + item.cidr">
                <td class="font-mono text-[12px]">{{ item.cidr }}</td>
                <td class="font-mono text-[12px]">{{ item.gateway }}</td>
                <td><span class="chip chip-warn">多余</span></td>
                <td class="text-[12px] text-muted-foreground">{{ item.comment || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>
    </template>

    <Dialog v-model:open="planVisible">
      <DialogContent class="max-w-[640px]">
        <DialogHeader>
          <DialogTitle>发布到 {{ reportRouter?.name }}</DialogTitle>
          <DialogDescription>
            以下列出各阶段及本次变更，仅执行已启用的阶段；写入后回读校验，失败时按相反顺序尝试撤销已完成的步骤，结果会列在下方。
          </DialogDescription>
        </DialogHeader>

        <div v-if="plan" class="flex max-h-[60vh] flex-col gap-4 overflow-y-auto text-[12.5px]">
          <ul v-if="plan.blockers.length" class="m-0 flex list-none flex-col gap-1.5 p-0">
            <li v-for="item in plan.blockers" :key="item" class="flex items-start gap-2 rounded-md bg-destructive-soft px-3 py-2 text-destructive-accent">
              <CircleX class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
              {{ item }}
            </li>
          </ul>
          <ul v-if="plan.warnings.length" class="m-0 flex list-none flex-col gap-1.5 p-0">
            <li v-for="item in plan.warnings" :key="item" class="flex items-start gap-2 rounded-md bg-warning-soft px-3 py-2 text-warning-accent">
              <TriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
              {{ item }}
            </li>
          </ul>
          <p v-if="!plan.blockers.length && plan.empty" class="m-0 text-muted-foreground">
            {{ plan.sync_baseline ? '路由器上的内容已和设置一致，没有需要写入的变更。确认后会重新建立规则同步的基准，自动同步随后恢复。' : '已全部同步，无需发布。' }}
          </p>

          <ol v-if="plan.stages.length" class="m-0 flex list-none flex-col gap-3 p-0">
            <li v-for="(stage, index) in plan.stages" :key="stage.key" class="rounded-md border border-border px-3 py-2.5">
              <div class="flex items-center gap-2">
                <span class="num text-muted-foreground">{{ index + 1 }}</span>
                <span class="font-semibold">{{ stage.title }}</span>
                <span v-if="stage.enabled === false" class="chip">已跳过</span>
                <span v-else-if="!stage.ops.length" class="chip chip-ok">无变更</span>
                <span v-else class="chip chip-acc">{{ stage.ops.length }} 项</span>
              </div>
              <p v-if="stage.note" class="mt-2 mb-0 text-muted-foreground">{{ stage.note }}</p>
              <ul v-if="stage.ops.length" class="mt-2 mb-0 flex list-disc flex-col gap-1 pl-8 text-muted-foreground">
                <li v-for="op in stage.ops" :key="op.summary" class="min-w-0"><RouterOperationPreview :op="op" /></li>
              </ul>
            </li>
          </ol>

          <AgentStatusRow
            v-if="applying || releaseResult"
            label="路由器发布"
            :status="applying ? '发布中' : releaseStatus"
            :tone="applying ? 'progress' : !releaseResult?.ok ? 'danger' : releaseNotices.length ? 'warning' : 'success'"
          >
            <template v-if="applying || releaseNotices.length" #notice>
              <p v-if="applying" class="m-0 text-muted-foreground">正在发布并回读校验，请等待结果。</p>
              <ul v-else class="m-0 flex list-none flex-col gap-1.5 p-0">
                <li v-for="(entry, index) in releaseNotices" :key="index" :class="entry.status === 'failed' ? 'text-destructive-accent' : 'text-warning-accent'">
                  {{ entry.phase }} · {{ stageTitle(entry.stage) }}：{{ entry.summary }}
                </li>
              </ul>
            </template>
            <template v-if="releaseLogs.length" #default>
              <div v-for="group in releaseLogs" :key="group.label" class="space-y-2">
                <p class="m-0 font-medium">{{ group.label }} · {{ group.entries.length }}</p>
                <ul class="m-0 flex list-none flex-col gap-1.5 p-0">
                  <li v-for="(entry, index) in group.entries" :key="index" class="flex min-w-0 items-start gap-2">
                    <span :class="['chip shrink-0', LOG_STATUS[entry.status]?.chip || 'chip-acc']">{{ LOG_STATUS[entry.status]?.label || entry.status }}</span>
                    <span class="min-w-0">{{ stageTitle(entry.stage) }} · {{ entry.summary }}</span>
                  </li>
                </ul>
              </div>
            </template>
          </AgentStatusRow>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="planVisible = false">{{ releaseResult ? '关闭' : '取消' }}</Button>
          <Button v-if="!releaseResult" :disabled="!canApply || applying" @click="applyPlan">
            <Loader2 v-if="applying" class="size-4 animate-spin" />
            {{ plan?.empty && plan.sync_baseline ? '重新建立同步基准' : '确认发布' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="dialogVisible" @update:open="value => { if (!saving) dialogVisible = value }">
      <DialogContent class="max-h-[90dvh] max-w-[480px] overflow-y-auto max-md:[&_[data-slot=button]]:min-h-11 max-md:[&_[role=combobox]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-w-11">
        <DialogHeader>
          <DialogTitle>{{ editingId ? '编辑路由器' : '添加路由器' }}</DialogTitle>
          <DialogDescription>{{ embedded ? '选择路由器类型，填写管理地址、用户名和密码。其余选项可保留默认值。' : '保存仅更新 ConfigFlow 设置，确认发布后才会写入路由器。' }}</DialogDescription>
        </DialogHeader>

        <form class="flex min-w-0 flex-col gap-4" @submit.prevent="submit">
          <div class="flex flex-col gap-1.5">
            <Label for="router-name">名称</Label>
            <Input id="router-name" v-model="form.name" class="bg-background/50" maxlength="80" placeholder="例如 主路由" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="router-type">{{ embedded ? '路由器类型（必选）' : '类型' }}</Label>
            <Select v-model="form.type">
              <SelectTrigger id="router-type" class="w-full bg-background/50"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="ikuai">iKuai 4.x</SelectItem>
                <SelectItem value="routeros">RouterOS 7.1+</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <FormField :label="embedded ? '管理地址（必填）' : '管理地址'" html-for="router-endpoint" v-bind="form.type === 'routeros' ? routerCopy.endpointROS : routerCopy.endpointIkuai">
            <Input
              id="router-endpoint"
              v-model="form.endpoint"
              class="bg-background/50 font-mono"
              :placeholder="form.type === 'routeros' ? 'https://192.168.88.1' : 'https://192.168.9.1'"
            />
          </FormField>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <FormField :label="embedded ? '用户名（必填）' : '用户名'" html-for="router-username" :help="accountHelp">
              <Input id="router-username" v-model="form.username" class="bg-background/50" autocomplete="off" />
            </FormField>
            <div class="flex flex-col gap-1.5">
              <!-- 与左侧用户名说明按钮的标签行保持同高，包括触屏尺寸。 -->
              <Label for="router-password" class="min-h-6 max-md:min-h-11 pointer-coarse:min-h-11">{{ embedded && !editingHasPassword ? '密码（必填）' : '密码' }}</Label>
              <Input
                id="router-password"
                v-model="form.password"
                type="password"
                class="bg-background/50"
                autocomplete="new-password"
                :placeholder="editingId && editingHasPassword ? '留空则保持不变' : ''"
              />
            </div>
          </div>
          <div class="flex items-center gap-2">
            <Switch id="router-verify-tls" :model-value="form.verify_tls" @update:model-value="value => (form.verify_tls = Boolean(value))" />
            <Label for="router-verify-tls">校验 HTTPS 证书</Label>
            <FieldHelp label="校验 HTTPS 证书" :text="routerCopy.tls.help" />
          </div>
          <FormField v-if="!setupDraftSession" label="代理网关（Mihomo Agent）" html-for="router-gateway" v-bind="form.type === 'routeros' ? routerCopy.gatewayROS : routerCopy.gateway">
            <Select v-model="form.proxy_gateway_agent_id">
              <SelectTrigger id="router-gateway" class="w-full bg-background/50"><SelectValue placeholder="不关联" /></SelectTrigger>
              <SelectContent>
                <SelectItem :value="NONE">不关联</SelectItem>
                <SelectItem v-for="agent in agentsOf('mihomo')" :key="agent.id" :value="agent.id">
                  {{ agent.name }}（{{ agent.host }}）
                </SelectItem>
              </SelectContent>
            </Select>
          </FormField>
          <FormField v-else label="安装设备" help="安装并检查完成后，向导会把代理网关设为本次 Mihomo Agent，把 DHCP DNS 设为本次 OxiDNS Agent，无需预先选择其他设备。"><p class="m-0 text-sm text-muted-foreground">安装后自动关联本次 Mihomo + OxiDNS</p></FormField>
          <component :is="embedded ? 'details' : 'div'" :class="embedded ? 'space-y-4 border-t border-border pt-3' : 'contents'">
          <summary v-if="embedded" class="min-h-11 cursor-pointer text-sm font-medium">高级路由选项<span class="ml-2 text-xs font-normal text-muted-foreground">可选，默认无需修改</span></summary>
          <div class="flex flex-col gap-1.5">
            <div class="flex min-h-11 items-center justify-between gap-3">
              <div class="flex min-w-0 items-center gap-1">
                <Label for="router-return-routing">自动配置回流</Label>
                <FieldHelp label="自动配置回流" :text="routerCopy.returnRouting.help" />
              </div>
              <Switch
                id="router-return-routing"
                :model-value="form.manage_return_routing"
                @update:model-value="value => (form.manage_return_routing = Boolean(value))"
              />
            </div>
          </div>
          <FormField :label="form.type === 'routeros' ? '使用静态路由的 IP 规则集' : '按 IP 路由给 Mihomo 的规则集'" html-for="router-ip-rule-sets" v-bind="routerCopy.staticRules">
            <div class="[&_[role=combobox]]:min-h-11">
              <MultiSelect :model-value="form.ip_rule_sets" trigger-id="router-ip-rule-sets" :options="ruleSetOptions" placeholder="仅 fake-ip 网段" @update:model-value="selectStaticRuleSets" />
            </div>
            <p v-if="form.type === 'ikuai'" class="m-0 mt-1.5 text-[12px] leading-relaxed text-muted-foreground" data-router-ikuai-ipv6-note>iKuai 只发布 IPv4 网段，规则集中的 IPv6 网段不会处理。</p>
          </FormField>
          <FormField v-if="showPolicyOption" label="使用策略路由的 IP 规则集" html-for="router-policy-rule-sets" v-bind="routerCopy.policyRules">
            <div class="[&_[role=combobox]]:min-h-11">
              <MultiSelect :model-value="form.ros_policy_rule_sets" trigger-id="router-policy-rule-sets" :options="ruleSetOptions" placeholder="不使用策略路由" @update:model-value="selectPolicyRuleSets" />
            </div>
          </FormField>
          <p v-if="form.type === 'routeros'" class="m-0 text-xs leading-relaxed text-muted-foreground">取消规则集后，需保存、预览并确认发布，才会清理对应的受管规则。</p>
          </component>
          <FormField v-if="!setupDraftSession" label="DHCP DNS 服务器（OxiDNS Agent）" html-for="router-dns" v-bind="form.type === 'routeros' ? routerCopy.dnsROS : routerCopy.dns">
            <Select v-model="form.dns_agent_id">
              <SelectTrigger id="router-dns" class="w-full bg-background/50"><SelectValue placeholder="不关联" /></SelectTrigger>
              <SelectContent class="max-w-[calc(100vw-2rem)]">
                <SelectItem :value="NONE">不关联</SelectItem>
                <SelectItem v-for="agent in agentsOf('oxidns')" :key="agent.id" :value="agent.id" class="whitespace-normal break-words">
                  {{ agent.name }}（{{ agent.host }}）
                </SelectItem>
              </SelectContent>
            </Select>
            <p v-if="dnsHaVip" class="m-0 text-xs leading-relaxed text-muted-foreground" data-dns-ha-vip>所选 OxiDNS 已开启 DNS 高可用，将下发虚拟 IP <span class="font-mono">{{ dnsHaVip }}</span>。</p>
          </FormField>
          <template v-if="form.type === 'routeros' && !setupDraftSession">
            <div class="flex flex-col gap-1.5">
              <div class="flex min-h-11 items-center justify-between gap-3">
                <div class="flex min-w-0 items-center gap-1">
                  <Label for="router-dns-guard">强制局域网 DNS 走 OxiDNS</Label>
                  <FieldHelp label="强制局域网 DNS 走 OxiDNS" :text="routerCopy.dnsGuard.help" />
                </div>
                <Switch
                  id="router-dns-guard"
                  :disabled="form.dns_agent_id === NONE"
                  :model-value="form.dns_guard.enabled"
                  @update:model-value="value => (form.dns_guard.enabled = Boolean(value))"
                />
              </div>
              <p class="m-0 text-xs leading-relaxed text-muted-foreground">
                {{ form.dns_agent_id === NONE ? '先选择上方的 DHCP DNS 服务器才能开启。' : routerCopy.dnsGuard.hint }}
              </p>
            </div>
            <template v-if="form.dns_guard.enabled">
              <div v-for="option in guardOptions" :key="option.key" class="flex min-h-11 items-center justify-between gap-3 pl-3">
                <div class="flex min-w-0 items-center gap-1">
                  <Label :for="`router-dns-guard-${option.key}`">{{ option.label }}</Label>
                  <FieldHelp :label="option.label" :text="option.copy.help" />
                </div>
                <Switch
                  :id="`router-dns-guard-${option.key}`"
                  :model-value="form.dns_guard[option.key]"
                  @update:model-value="value => (form.dns_guard[option.key] = Boolean(value))"
                />
              </div>
              <FormField label="放行设备" html-for="router-dns-guard-bypass" class="pl-3" v-bind="routerCopy.dnsGuardBypass">
                <Textarea id="router-dns-guard-bypass" v-model="guardBypassText" :rows="3" class="font-mono" placeholder="每行一个，如 192.168.0.50 或 192.168.0.64/28" />
              </FormField>
            </template>
            <div class="flex flex-col gap-1.5" data-router-ipv6-pd-guard-setting>
              <div class="flex min-h-11 items-center justify-between gap-3">
                <div class="flex min-w-0 items-center gap-1">
                  <Label for="router-ipv6-pd-guard">IPv6 前缀变化保护</Label>
                  <FieldHelp label="IPv6 前缀变化保护" :text="routerCopy.ipv6PdGuard.help" />
                </div>
                <Switch
                  id="router-ipv6-pd-guard"
                  :model-value="form.ipv6_pd_guard.enabled"
                  @update:model-value="value => (form.ipv6_pd_guard.enabled = Boolean(value))"
                />
              </div>
              <p class="m-0 text-xs leading-relaxed text-muted-foreground">{{ routerCopy.ipv6PdGuard.hint }}</p>
            </div>
          </template>
        </form>

        <DialogFooter>
          <Button variant="outline" :disabled="saving" @click="dialogVisible = false">取消</Button>
          <Button :disabled="saving" @click="submit">
            <Loader2 v-if="saving" class="size-4 animate-spin" />
            {{ setupDraftSession ? '保存到向导' : '保存' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, matchedRouteKey, onBeforeRouteLeave, routeLocationKey, routerKey } from 'vue-router'
import {
  Activity,
  CircleX,
  Copy,
  GitCompare,
  Loader2,
  Pencil,
  Plus,
  Rocket,
  Router as RouterIcon,
  Trash2,
  TriangleAlert
} from '@lucide/vue'
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
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import FormField from '@/components/common/FormField.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import PolicyRoutingPanel from '@/components/routers/PolicyRoutingPanel.vue'
import RouterRuleSync from '@/components/routers/RouterRuleSync.vue'
import { focusSetting } from '@/lib/settingLink'
import { canReloadPage } from '@/lib/pageReloadGuard'
import RouterOperationPreview from '@/components/routers/RouterOperationPreview.vue'
import { agentApi, routerApi as baseRouterApi, ruleApi, type StaticRouteChange, type StaticRouteItem, type StaticRoutePlan, type StaticRouteValues } from '@/api'
import { setupApi } from '@/api/setup'
import { activeProfileId as selectedProfileId, scopedRequests, setActiveProfileId } from '@/profileContext'
import { useProfileStore } from '@/stores/profile'
import { ROUTER_DEEP_LINK_KEYS, readRouterDeepLink, type RouterDeepLink } from '@/lib/upstreamRouterPending'
import { confirm, confirmDanger, confirmState, notify, settleConfirm } from '@/lib/feedback'
import { routerCopy } from '@/lib/routerCopy'
import { copyText } from '@/lib/clipboard'
import { routerForwardingWarning, type GatewayForwardingEntry, type RouterForwardingWarning } from '@/lib/gatewayForwarding'
import { usePageReloadGuard, withPageReloadBlocked } from '@/lib/pageReloadGuard'

const props = defineProps<{ embedded?: boolean; profileId?: string; setupAgents?: { mihomo: string; oxidns: string }; setupDraftSession?: string }>()
const emit = defineEmits<{ selected: [id: string]; busy: [value: boolean] }>()
const activeProfileId = computed(() => props.profileId || selectedProfileId.value)
const getActiveProfileId = () => activeProfileId.value
// Embedded setup stays pinned to its new profile, including delayed confirmations.
// Ordinary router pages retain the original API call signatures.
const routerApi = {
  ...baseRouterApi,
  list: () => props.profileId ? baseRouterApi.list(props.profileId) : baseRouterApi.list(),
  create: (data: unknown) => props.profileId ? baseRouterApi.create(data, props.profileId) : baseRouterApi.create(data),
  update: (id: string, data: unknown) => props.profileId ? baseRouterApi.update(id, data, props.profileId) : baseRouterApi.update(id, data),
  remove: (id: string) => props.profileId ? baseRouterApi.remove(id, props.profileId) : baseRouterApi.remove(id),
  probe: (id: string) => props.profileId ? baseRouterApi.probe(id, props.profileId) : baseRouterApi.probe(id),
  report: (id: string) => props.profileId ? baseRouterApi.report(id, props.profileId) : baseRouterApi.report(id),
  plan: (id: string) => props.profileId ? baseRouterApi.plan(id, props.profileId) : baseRouterApi.plan(id),
  apply: (id: string, fingerprint: string) => props.profileId ? baseRouterApi.apply(id, fingerprint, props.profileId) : baseRouterApi.apply(id, fingerprint),
}

const routerPageHelp = '在这里查看和调整主路由的分流设置。「差异报告」只检查，不会修改设备。点「预览发布」查看具体改动，确认后再应用；如果发布失败，会尝试撤销本次改动，并显示结果。'

type RouterType = 'ikuai' | 'routeros'
type Status = 'synced' | 'adoptable' | 'conflict' | 'missing' | 'extra' | 'not_required' | 'unknown' | 'pending'
type ReportSummary = Partial<Record<Status, number>>
type DhcpDnsStatus = 'disabled' | 'synced' | 'pending' | 'unknown'
type IPv6FakeIPStatus = 'synced' | 'missing' | 'pending' | 'conflict' | 'unknown' | 'disabled' | 'borrowed'
interface IPv6RoutesReport {
  enabled: boolean
  status: IPv6FakeIPStatus
  gateway: string
  items: { cidr: string; sources: string[]; gateway: string; status: IPv6FakeIPStatus; detail: string }[]
  skip: string[]
  warnings: string[]
  fix_command?: string
}
interface IPv6FakeIPReport {
  enabled: boolean
  status: IPv6FakeIPStatus
  items: { cidr: string; gateway: string; status: IPv6FakeIPStatus; detail: string }[]
  warnings: string[]
  blockers: string[]
  fix_command?: string
}

type IPv6DnsStatus = 'disabled' | 'not_required' | 'synced' | 'pending' | 'unknown' | 'blocked'
interface IPv6DnsReport {
  enabled: boolean
  status: IPv6DnsStatus
  interface: string
  target_dns: string
  message?: string
  items: {
    id: string
    interface: string
    source_interface: string
    advertise_dns: string
    dns_servers: string[]
    status: IPv6DnsStatus
    detail: string
  }[]
  blockers: string[]
  warnings: string[]
  fix_command?: string
}

interface DhcpDnsReport {
  status: DhcpDnsStatus
  message: string
  target_dns: string
  items: {
    id: string
    network: string
    current_dns: string[]
    desired_dns: string[]
    status: Exclude<DhcpDnsStatus, 'disabled'>
    detail: string
  }[]
  blockers: string[]
}

interface RouterItem {
  id: string
  name: string
  type: RouterType
  endpoint: string
  username: string
  verify_tls: boolean
  proxy_gateway_agent_id: string
  manage_return_routing?: boolean
  dns_agent_id: string
  ip_rule_sets?: string[]
  ros_policy_rule_sets?: string[]
  dns_guard?: DnsGuardSettings
  ipv6_pd_guard?: { enabled: boolean }
  has_password: boolean
  last_probe?: { ok: boolean; version?: string; message?: string; at: string }
  last_release?: { ok: boolean; failed_stage?: string; message?: string; at?: string }
}

interface DnsGuardSettings {
  enabled: boolean
  block_dot: boolean
  failover: boolean
  bypass: string[]
}

interface DnsGuardReport {
  enabled: boolean
  status: 'disabled' | 'synced' | 'pending' | 'unknown'
  message: string
  items: { role: string; object: string; detail: string; status: 'synced' | 'pending'; paused: boolean }[]
  blockers: string[]
  warnings: string[]
}

type PdGuardStatus = 'disabled' | 'pending' | 'synced' | 'borrowed' | 'blocked'
type PdGuardItemStatus = 'missing' | 'update' | 'conflict' | 'synced' | 'adopt' | 'borrowed' | 'remove' | 'blocked'
interface PdGuardReport {
  enabled: boolean
  status: PdGuardStatus
  message: string
  lan: string
  pool: string
  client: string
  hold: number
  managed: boolean
  rollback_script: boolean
  items: { role: string; object: string; status: PdGuardItemStatus; detail: string }[]
  blockers: string[]
  warnings: string[]
}

interface AgentItem {
  id: string
  name: string
  host: string
  service_type: string
  oxidns_ha?: { enabled?: boolean; vip?: string } | null
}

const NONE = '__none__'
const STATUS: Record<Status, { label: string; chip: string }> = {
  synced: { label: '已同步', chip: 'chip-ok' },
  adoptable: { label: '可接管', chip: 'chip-sky' },
  conflict: { label: '冲突', chip: 'chip-bad' },
  missing: { label: '缺失', chip: 'chip-warn' },
  extra: { label: '多余', chip: 'chip-warn' },
  not_required: { label: '无需免转换', chip: 'chip-ok' },
  unknown: { label: '待确认', chip: 'chip-warn' },
  pending: { label: '待更新', chip: 'chip-warn' }
}
const statusInfo = (status: string) => STATUS[status as Status] || STATUS.unknown
// IPv6 routes have their own ownership/status meanings; do not reuse NAT's
// not_required label or describe borrowed routes as ConfigFlow-managed.
const IPV6_FAKEIP_STATUS: Record<IPv6FakeIPStatus, { label: string; chip: string }> = {
  synced: { label: '已同步', chip: 'chip-ok' },
  missing: { label: '待添加', chip: 'chip-warn' },
  pending: { label: '待更新', chip: 'chip-warn' },
  conflict: { label: '冲突', chip: 'chip-bad' },
  unknown: { label: '待确认', chip: 'chip-warn' },
  disabled: { label: '未启用', chip: '' },
  borrowed: { label: '沿用已有路由', chip: '' },
}
const ipv6FakeIPStatusInfo = (status: string) => IPV6_FAKEIP_STATUS[status as IPv6FakeIPStatus] || IPV6_FAKEIP_STATUS.unknown
const disabledNatRuleLabel = (rule: { comment?: string; rule_id: string }) => rule.comment || rule.rule_id
const DNS_GUARD_STATUS = {
  disabled: { label: '未开启', tone: 'muted' },
  synced: { label: '已同步', tone: 'success' },
  pending: { label: '待更新', tone: 'warning' },
  unknown: { label: '待确认', tone: 'warning' }
} as const
const guardDefaults = (): DnsGuardSettings => ({ enabled: false, block_dot: false, failover: true, bypass: [] })
const guardOptions = [
  { key: 'block_dot', label: '拦截加密 DNS（853 端口）', copy: routerCopy.dnsGuardDot },
  { key: 'failover', label: 'OxiDNS 故障时自动暂停', copy: routerCopy.dnsGuardFailover }
] as const
const PD_GUARD_STATUS: Record<PdGuardStatus, { label: string; tone: 'muted' | 'success' | 'warning' }> = {
  disabled: { label: '未开启', tone: 'muted' },
  pending: { label: '待发布', tone: 'warning' },
  synced: { label: '已安装', tone: 'success' },
  borrowed: { label: '已安装（沿用）', tone: 'success' },
  blocked: { label: '需处理', tone: 'warning' }
}
const PD_GUARD_ITEM: Record<PdGuardItemStatus, { label: string; chip: string }> = {
  missing: { label: '待安装', chip: 'chip-warn' },
  update: { label: '待更新', chip: 'chip-warn' },
  conflict: { label: '冲突', chip: 'chip-bad' },
  synced: { label: '已同步', chip: 'chip-ok' },
  adopt: { label: '一致，发布后接管', chip: 'chip-sky' },
  borrowed: { label: '沿用', chip: 'chip-ok' },
  remove: { label: '待撤回', chip: 'chip-warn' },
  blocked: { label: '需处理', chip: 'chip-warn' }
}
const pdGuardItemInfo = (status: string) => PD_GUARD_ITEM[status as PdGuardItemStatus] || PD_GUARD_ITEM.blocked
const DHCP_DNS_STATUS = {
  disabled: { label: '未关联', tone: 'muted' },
  synced: { label: '已同步', tone: 'success' },
  pending: { label: '待更新', tone: 'warning' },
  unknown: { label: '待确认', tone: 'warning' }
} as const
const IPV6_DNS_STATUS = {
  disabled: { label: '未检查', tone: 'muted' },
  not_required: { label: '无需调整', tone: 'success' },
  synced: { label: '无需调整', tone: 'success' },
  pending: { label: '待关闭', tone: 'warning' },
  unknown: { label: '需处理', tone: 'warning' },
  blocked: { label: '需处理', tone: 'warning' }
} as const
const IPV6_DNS_MESSAGE: Record<IPv6DnsStatus, string> = {
  disabled: '未关联 OxiDNS，本次不检查或修改 IPv6 DNS 下发。',
  not_required: '该 LAN 未通过 IPv6 另发 DNS，无需调整。',
  synced: '该 LAN 未通过 IPv6 另发 DNS，无需调整。',
  pending: '该 LAN 正在通过 IPv6 另发 DNS，需要关闭以统一使用 OxiDNS。',
  unknown: '尚未确定可以安全修改的 LAN 配置，请先处理下方问题，再重新检查。',
  blocked: '尚未确定可以安全修改的 LAN 配置，请先处理下方问题，再重新检查。'
}
const LOG_STATUS: Record<string, { label: string; chip: string }> = {
  ok: { label: '完成', chip: 'chip-ok' },
  verified: { label: '已校验', chip: 'chip-ok' },
  warning: { label: '需处理', chip: 'chip-warn' },
  failed: { label: '失败', chip: 'chip-bad' }
}

interface PlanStage {
  key: string
  title: string
  enabled?: boolean
  note?: string
  ops: { kind: string; summary: string }[]
}
interface Plan {
  stages: PlanStage[]
  blockers: string[]
  warnings: string[]
  fingerprint: string
  empty: boolean
  /** 无需写入，但确认后会建立规则自动同步的基准 */
  sync_baseline?: boolean
}
interface ReleaseResult {
  ok: boolean
  log: { stage: string; summary: string; status: string }[]
  rollback: { stage: string; summary: string; status: string }[]
}

const routers = ref<RouterItem[]>([])
const agents = ref<AgentItem[]>([])
const ruleSets = ref<{ id: string; name: string; policy: string }[]>([])
const loading = ref(false)
const saving = ref(false)
const dialogVisible = ref(false)
const editingId = ref('')
const editingHasPassword = ref(false)
// Policy sources are the profile's client devices, and the wizard's new profile
// has none yet. A router that already uses policy routing keeps the option so it
// can be cleared; a draft never does, so an older draft's sets are dropped on save.
const keepPolicyOption = ref(false)
const showPolicyOption = computed(() => form.type === 'routeros' && !props.setupDraftSession
  && (!props.embedded || keepPolicyOption.value))
const busyId = ref('')
const busyAction = ref<'probe' | 'report' | ''>('')
const report = ref<any>(null)
const reportSummary = computed(() => (report.value?.summary || {}) as ReportSummary)
// RouterOS only: the forward chain drops same-port IPv4 traffic to Mihomo (or could not be read).
const ipv4ForwardHint = computed(() => report.value?.routes?.forward_hint as { status: string; warning: string; fix_command: string } | undefined)
const ipv6FakeIP = computed(() => report.value?.ipv6_fakeip as IPv6FakeIPReport | undefined)
const ipv6Routes = computed(() => report.value?.ipv6_routes as IPv6RoutesReport | undefined)
// A skipped item is left as it is: show it as unconfirmed, never as in sync.
const ipv6RoutesStatus = computed(() => ipv6Routes.value?.skip.length ? 'unknown' : ipv6Routes.value?.status || 'unknown')
// One same-port accept covers Fake-IP and rule-set routes: show it once, in the Fake-IP card above.
const ipv6RoutesFixShared = computed(() => !!ipv6Routes.value?.fix_command && ipv6Routes.value.fix_command === ipv6FakeIP.value?.fix_command)
const ipv6RoutesWarnings = computed(() => (ipv6Routes.value?.warnings || [])
  .filter(warning => !ipv6RoutesFixShared.value || !(ipv6FakeIP.value?.warnings || []).includes(warning)))
const dhcpDns = computed(() => report.value?.dhcp_dns as DhcpDnsReport | undefined)
const dhcpDnsStatus = computed(() => DHCP_DNS_STATUS[dhcpDns.value?.status || 'disabled'])
const ipv6Dns = computed(() => report.value?.ipv6_dns as IPv6DnsReport | undefined)
const ipv6DnsStatus = computed(() => IPV6_DNS_STATUS[ipv6Dns.value?.status || 'unknown'] || IPV6_DNS_STATUS.unknown)
const ipv6DnsMessage = computed(() => ipv6Dns.value?.message || IPV6_DNS_MESSAGE[ipv6Dns.value?.status || 'unknown'])
async function copyCommand(command?: string) {
  try {
    await copyText(command || '')
    notify.success('命令已复制。')
  } catch {
    notify.error('复制失败，请手动选择命令。')
  }
}
async function copyIPv6DnsCommand() {
  try {
    await copyText(ipv6Dns.value?.fix_command || '')
    notify.success('命令已复制。')
  } catch {
    notify.error('复制失败，请手动选择命令。')
  }
}
const dnsGuard = computed(() => report.value?.dns_guard as DnsGuardReport | undefined)
const dnsGuardStatus = computed(() => DNS_GUARD_STATUS[dnsGuard.value?.status || 'disabled'])
const pdGuard = computed(() => {
  const value = report.value?.ipv6_pd_guard as PdGuardReport | undefined
  // Nothing to show while the switch is off and ConfigFlow owns nothing.
  return value && (value.enabled || value.status !== 'disabled') ? value : undefined
})
const pdGuardStatus = computed(() => PD_GUARD_STATUS[pdGuard.value?.status || 'disabled'] || PD_GUARD_STATUS.blocked)
const reportRouter = ref<RouterItem | null>(null)
const reportTime = ref('')
const reportLoading = ref(false)
const reportError = ref('')
const plan = ref<Plan | null>(null)
const planVisible = ref(false)
const planLoading = ref(false)
const applying = ref(false)
const releaseResult = ref<ReleaseResult | null>(null)
let inspectionRevision = 0
const releaseStatus = computed(() => {
  const result = releaseResult.value
  if (!result) return ''
  if (result.ok) return releaseNotices.value.length ? '发布完成，需处理' : '发布完成'
  if (result.rollback.some(entry => entry.status === 'failed')) return '发布失败，回滚未完成'
  if (result.rollback.length) return '发布失败，已撤销已完成的变更'
  return '发布失败，请检查下方记录'
})

const releaseLogs = computed(() => releaseResult.value ? [
  { label: '发布记录', entries: releaseResult.value.log },
  { label: '回滚记录', entries: releaseResult.value.rollback }
].filter(group => group.entries.length) : [])
const releaseNotices = computed(() => releaseLogs.value.flatMap(group => group.entries
  .filter(entry => entry.status === 'failed' || entry.status === 'warning')
  .map(entry => ({ ...entry, phase: group.label === '回滚记录' ? '回滚' : '发布' }))))

const emptyForm = () => ({
  name: '',
  type: 'ikuai' as RouterType,
  endpoint: '',
  username: '',
  password: '',
  verify_tls: true,
  proxy_gateway_agent_id: NONE,
  manage_return_routing: false,
  dns_agent_id: NONE,
  ip_rule_sets: [] as string[],
  ros_policy_rule_sets: [] as string[],
  dns_guard: guardDefaults(),
  ipv6_pd_guard: { enabled: false }
})
const form = reactive(emptyForm())
const guardBypassText = ref('')
// The guard targets the DHCP DNS Agent; without one it cannot stay enabled.
watch(() => form.dns_agent_id, value => { if (value === NONE) form.dns_guard.enabled = false })

const typeLabel = (type: RouterType) => (type === 'routeros' ? 'RouterOS' : 'iKuai')
const ruleSetOptions = computed(() =>
  ruleSets.value.map(rule => ({ value: rule.id, label: `${rule.name}（${rule.policy}）` }))
)
const selectStaticRuleSets = (selected: string[]) => {
  form.ip_rule_sets = selected
  if (form.type === 'routeros') {
    form.ros_policy_rule_sets = form.ros_policy_rule_sets.filter(id => !selected.includes(id))
  }
}
const selectPolicyRuleSets = (selected: string[]) => {
  form.ros_policy_rule_sets = selected
  form.ip_rule_sets = form.ip_rule_sets.filter(id => !selected.includes(id))
}
const agentsOf = (serviceType: string) => agents.value.filter(agent => agent.service_type === serviceType)
/** 所选 DHCP DNS Agent 开启高可用时下发的虚拟 IP；未开启时为空，界面不变。 */
const dnsHaVip = computed(() => {
  const ha = agents.value.find(agent => agent.id === form.dns_agent_id)?.oxidns_ha
  return ha?.enabled === true ? ha.vip || '' : ''
})
const accountHelp = computed(() => form.type === 'routeros'
  ? '如何设置 RouterOS 账号权限：\n1. 使用管理员账号登录 WinBox，打开 System → Users。\n2. 在 Groups 中新建专用用户组（例如 configflow），勾选 read、write、api、rest-api；已有专用组可补齐这些权限。\n3. 在 Users 中新建或选择供 ConfigFlow 使用的账号，将 Group 设为该专用组，再填写账号和密码。'
    + (form.username ? `\n\n当前填写的账号：${form.username}。请检查这个账号所属的组。` : '')
    + '\n\nread 用于读取配置，write 用于修改配置，api 和 rest-api 用于接口访问。保存后先测试连接，再重新生成发布预览。连接成功只表示能读取配置，不代表已验证写入权限。'
  : 'iKuai 需要管理员账号，用于读取和发布路由配置。')
const errorMessage = (error: any, fallback: string) => error?.response?.data?.message || fallback

// Keep route management independent of Agent inspection and pin each operation
// to the device/profile it started from, including time spent in confirmation.
const routeRouter = ref<RouterItem | null>(null)
const policyRouter = ref<RouterItem | null>(null)
const syncRouter = ref<RouterItem | null>(null)
const policyPanel = ref<InstanceType<typeof PolicyRoutingPanel> | null>(null)
const policyLocked = ref(false)
const canLeavePolicy = async () => !policyLocked.value && (await policyPanel.value?.canLeave() ?? true)
if (inject(matchedRouteKey, undefined)?.value) onBeforeRouteLeave(canLeavePolicy)
const openPolicyManager = async (router: RouterItem) => {
  if (routeBusy.value || applying.value || policyLocked.value || policyRouter.value?.id === router.id) return
  if (!await canLeavePolicy()) return
  closeRouteManager()
  clearInspection()
  policyRouter.value = router
  syncRouter.value = null
}

const openRuleSync = async (router: RouterItem) => {
  if (routeBusy.value || applying.value || !await canLeavePolicy()) return
  closeRouteManager()
  clearInspection()
  policyRouter.value = null
  syncRouter.value = router
}
const routeRows = ref<StaticRouteItem[]>([])
const routeLoading = ref(false)
const routeBusy = ref(false)
usePageReloadGuard(() => routeBusy.value || applying.value)
const routeError = ref('')
const routeEditorVisible = ref(false)
const routeEditing = ref<StaticRouteItem | null>(null)
const routeForm = reactive<StaticRouteValues>({ cidr: '', gateway: '', enabled: true, comment: '' })
const routeFormError = ref('')
let routeEpoch = 0
let routeRead = 0
let ownedRouteConfirmation: typeof confirmState.resolve = null
const routeContext = () => ({ id: routeRouter.value?.id, profile: getActiveProfileId(), epoch: routeEpoch })
type RouteContext = ReturnType<typeof routeContext>
const currentRouteContext = (context: RouteContext) => Boolean(context.id && context.id === routeRouter.value?.id
  && context.profile === getActiveProfileId() && context.epoch === routeEpoch)
const closeRouteManager = () => {
  if (ownedRouteConfirmation && confirmState.resolve === ownedRouteConfirmation) settleConfirm('cancel')
  ownedRouteConfirmation = null
  routeEpoch++
  routeRouter.value = null
  routeRows.value = []
  routeEditorVisible.value = false
  routeEditing.value = null
  routeBusy.value = false
  routeLoading.value = false
  routeError.value = ''
}
const refreshRoutes = async (context = routeContext()) => {
  if (!currentRouteContext(context)) return false
  const read = ++routeRead
  routeLoading.value = true
  routeError.value = ''
  try {
    const { data } = await routerApi.routes(context.id!, context.profile)
    if (!currentRouteContext(context) || read !== routeRead) return false
    if (!data.success) throw new Error(data.message || '读取路由失败')
    routeRows.value = data.routes || []
    return true
  } catch (error: any) {
    if (currentRouteContext(context) && read === routeRead) {
      routeError.value = errorMessage(error, error?.message || '读取路由失败，请重试')
      notify.error(routeError.value)
    }
    return false
  } finally {
    if (currentRouteContext(context) && read === routeRead) routeLoading.value = false
  }
}
const openRouteManager = async (router: RouterItem) => {
  if (routeBusy.value || applying.value) return
  if (!await canLeavePolicy()) return
  policyRouter.value = null
  syncRouter.value = null
  clearInspection()
  closeRouteManager()
  routeRouter.value = router
  await refreshRoutes()
}
const openRouteEditor = (item?: StaticRouteItem) => {
  if (!routeRouter.value || routeBusy.value || routeLoading.value || routeError.value || (item && !item.editable)) return
  routeEditing.value = item ? { ...item } : null
  Object.assign(routeForm, item ? { cidr: item.cidr, gateway: item.gateway, enabled: item.enabled, comment: item.comment }
    : { cidr: '', gateway: '', enabled: true, comment: '' })
  routeFormError.value = ''
  routeEditorVisible.value = true
}
const routeDetails = (value: StaticRouteValues | null) => value
  ? `网段：${value.cidr}\n下一跳：${value.gateway}\n状态：${value.enabled ? '启用' : '停用'}\n备注：${value.comment || '无备注'}`
  : '无'
const previewRouteChange = async (change: StaticRouteChange) => {
  if (routeBusy.value || routeLoading.value || routeError.value) return
  const context = routeContext()
  if (!currentRouteContext(context)) return
  routeBusy.value = true
  let reopenEditor = routeEditorVisible.value
  routeFormError.value = ''
  try {
    const { data } = await routerApi.routePlan(context.id!, change, context.profile)
    if (!currentRouteContext(context)) return
    if (!data.success) throw new Error(data.message || '生成路由预览失败')
    const preview = data.plan as StaticRoutePlan
    if (preview.empty) {
      notify.success('路由配置没有变化')
      return
    }
    const description = [preview.summary, `修改前\n${routeDetails(preview.before)}`, `修改后\n${routeDetails(preview.after)}`,
      ...(preview.warnings || [])].join('\n\n')
    // The shared confirmation host must be the only open modal. Restore the
    // editor and its draft on cancellation or failure.
    routeEditorVisible.value = false
    await nextTick()
    if (!currentRouteContext(context)) return
    const confirmation = (change.action === 'delete' ? confirmDanger : confirm)(description, {
      title: `${change.action === 'delete' ? '删除' : change.action === 'add' ? '新增' : '修改'}路由 · ${routeRouter.value?.name}`,
      confirmText: change.action === 'delete' ? '确认删除路由' : '确认写入路由'
    })
    ownedRouteConfirmation = confirmState.resolve
    const accepted = await confirmation
    if (!currentRouteContext(context)) return
    ownedRouteConfirmation = null
    if (!accepted) return
    const response = await withPageReloadBlocked(() => routerApi.routeApply(
      context.id!, { ...change, fingerprint: preview.fingerprint }, context.profile
    ))
    if (!currentRouteContext(context)) return
    if (!response.data.success) throw new Error(response.data.message || '路由写入失败')
    reopenEditor = false
    notify.success(response.data.result?.changed === false ? '路由配置没有变化' : '路由变更已完成')
    routeEditorVisible.value = false
    clearInspection(context.id!)
    await refreshRoutes(context)
  } catch (error: any) {
    if (!currentRouteContext(context)) return
    const conflict = error?.response?.status === 409
    const message = errorMessage(error, error?.message || '路由操作失败')
    routeFormError.value = message
    notify.error(message)
    if (conflict) {
      const refreshed = await refreshRoutes(context)
      if (!currentRouteContext(context)) return
      const latest = routeRows.value.find(item => item.id === change.id)
      if (change.action === 'update' && refreshed && latest?.editable) routeEditing.value = { ...latest }
      else if (change.action !== 'add') reopenEditor = false
      routeFormError.value = refreshed ? '路由配置已变化，列表已刷新。请核对后重新预览。' : '刷新失败，请关闭编辑窗口并刷新路由后重试。'
    }
  } finally {
    if (currentRouteContext(context)) {
      routeBusy.value = false
      if (reopenEditor) routeEditorVisible.value = true
    }
  }
}
const validIPv4 = (value: string) => /^(?:0|[1-9]\d{0,2})(?:\.(?:0|[1-9]\d{0,2})){3}$/.test(value)
  && value.split('.').every(part => Number(part) <= 255)
const submitRoute = async () => {
  if (routeBusy.value) return
  const values = { ...routeForm, cidr: routeForm.cidr.trim(), gateway: routeForm.gateway.trim() }
  const [address, prefix, ...extra] = values.cidr.split('/')
  if (!validIPv4(address || '') || !/^(?:[0-9]|[12][0-9]|3[0-2])$/.test(prefix || '') || extra.length) {
    routeFormError.value = '请输入有效 IPv4 网段，例如 192.168.20.0/24。'
    return
  }
  if (!validIPv4(values.gateway)) {
    routeFormError.value = '下一跳必须是 IPv4 地址。'
    return
  }
  if (values.comment.length > 512 || values.comment.includes('\0')) {
    routeFormError.value = '备注不能超过 512 个字符，且不能包含空字符。'
    return
  }
  await previewRouteChange(routeEditing.value
    ? { action: 'update', id: routeEditing.value.id, revision: routeEditing.value.revision, values }
    : { action: 'add', values })
}
const closePanels = () => {
  policyRouter.value = null
  syncRouter.value = null
  closeRouteManager()
  clearInspection()
}
watch(activeProfileId, closePanels, { flush: 'sync' })
onBeforeUnmount(closePanels)

const returnTone = computed(() => {
  const state = report.value?.return_routing
  if (!state || ['disabled', 'unlinked'].includes(state.status)) return 'muted'
  if (state.status === 'unavailable') return 'danger'
  if (state.status !== 'ok') return 'warning'
  return state.return_tables?.length && state.fwmark_rules?.length ? 'success' : 'warning'
})
const returnLabel = computed(() => {
  const state = report.value?.return_routing
  if (!state) return ''
  if (state.status === 'disabled') return '自动配置已关闭'
  if (state.status === 'unlinked') return '未关联'
  if (state.status === 'offline') return 'Agent 离线'
  if (state.status !== 'ok') return '无法读取'
  return returnTone.value === 'success' ? '已配置' : '未配置'
})

const load = async (isCurrent = () => true) => {
  loading.value = true
  try {
    const [routerResult, agentResult, ruleResult] = await Promise.allSettled([
      props.setupDraftSession ? setupApi.get(props.setupDraftSession).then(({ data }) => ({ data: { routers: data.router_draft ? [data.router_draft] : [] } })) : routerApi.list(),
      props.setupDraftSession ? Promise.resolve({ data: [] }) : agentApi.getAll(),
      props.profileId ? ruleApi.getAll(props.profileId) : ruleApi.getAll()
    ])
    if (!isCurrent()) return false
    if (routerResult.status === 'rejected') throw routerResult.reason
    routers.value = routerResult.value.data.routers || []
    const agentData = agentResult.status === 'fulfilled' ? agentResult.value.data : []
    agents.value = Array.isArray(agentData) ? agentData : agentData?.data || agentData?.agents || []
    if (props.profileId) agents.value = agents.value.filter((agent: any) => agent.profile_id === props.profileId)
    const ruleData = ruleResult.status === 'fulfilled' ? ruleResult.value.data : []
    const rules = Array.isArray(ruleData) ? ruleData : ruleData?.data || []
    ruleSets.value = rules
      .filter((rule: any) => rule.itemType === 'ruleset')
      .map((rule: any) => ({ id: rule.id, name: rule.name || rule.id, policy: rule.policy || '' }))
    if (agentResult.status === 'rejected' || ruleResult.status === 'rejected') {
      notify.error('关联 Agent 或规则集暂时无法读取，仍可管理设备上的静态路由')
    }
    void loadForwarding()
    return true
  } catch (error) {
    if (isCurrent()) notify.error(errorMessage(error, '加载路由器失败'))
    return false
  } finally {
    if (isCurrent()) loading.value = false
  }
}

// 代理网关的转发提示：独立读取，不阻塞路由器列表，失败时不提示；过期响应按请求令牌丢弃。
const showForwarding = computed(() => !props.embedded && !props.setupDraftSession)
const forwardingEntries = ref<Record<string, GatewayForwardingEntry>>({})
let forwardingRequest = 0
const forwardingWarnings = computed(() => {
  const warnings: Record<string, RouterForwardingWarning> = {}
  if (!showForwarding.value) return warnings
  for (const router of routers.value) {
    const warning = router.proxy_gateway_agent_id ? routerForwardingWarning(forwardingEntries.value[router.proxy_gateway_agent_id]) : null
    if (warning) warnings[router.id] = warning
  }
  return warnings
})
const loadForwarding = async () => {
  if (!showForwarding.value) return
  const request = ++forwardingRequest
  const profileId = activeProfileId.value
  try {
    const { data } = await routerApi.gatewayForwarding(profileId)
    if (request !== forwardingRequest) return
    const entries: Record<string, GatewayForwardingEntry> = {}
    for (const entry of Array.isArray(data?.gateways) ? data.gateways : []) {
      if (entry && typeof entry.agent_id === 'string') entries[entry.agent_id] = entry
    }
    forwardingEntries.value = entries
  } catch {
    if (request === forwardingRequest) forwardingEntries.value = {}
  }
}
watch(activeProfileId, () => {
  forwardingRequest += 1
  forwardingEntries.value = {}
  void loadForwarding()
})

const openCreate = async () => {
  if (!await canLeavePolicy()) return
  policyRouter.value = null
  syncRouter.value = null
  Object.assign(form, emptyForm())
  guardBypassText.value = ''
  if (props.embedded) form.name = '主路由'
  keepPolicyOption.value = false
  if (props.setupAgents && !props.setupDraftSession) Object.assign(form, { proxy_gateway_agent_id: props.setupAgents.mihomo, dns_agent_id: props.setupAgents.oxidns })
  editingId.value = ''
  editingHasPassword.value = false
  dialogVisible.value = true
}

const openEdit = async (router: RouterItem) => {
  if (!await canLeavePolicy()) return
  policyRouter.value = null
  syncRouter.value = null
  Object.assign(form, {
    name: router.name,
    type: router.type,
    endpoint: router.endpoint,
    username: router.username,
    password: '',
    verify_tls: router.verify_tls,
    proxy_gateway_agent_id: router.proxy_gateway_agent_id || NONE,
    manage_return_routing: router.manage_return_routing === true,
    dns_agent_id: router.dns_agent_id || NONE,
    ip_rule_sets: (router.ip_rule_sets || []).filter(id => router.type !== 'routeros' || !router.ros_policy_rule_sets?.includes(id)),
    ros_policy_rule_sets: [...(router.ros_policy_rule_sets || [])],
    dns_guard: { ...guardDefaults(), ...router.dns_guard, bypass: [...(router.dns_guard?.bypass || [])] },
    ipv6_pd_guard: { enabled: router.ipv6_pd_guard?.enabled === true }
  })
  guardBypassText.value = (router.dns_guard?.bypass || []).join('\n')
  keepPolicyOption.value = !!router.ros_policy_rule_sets?.length
  editingId.value = router.id
  editingHasPassword.value = router.has_password
  dialogVisible.value = true
}

const clearInspection = (routerId?: string) => {
  if (routerId && reportRouter.value?.id !== routerId) return
  inspectionRevision += 1
  report.value = null
  reportRouter.value = null
  reportTime.value = ''
  reportLoading.value = false
  reportError.value = ''
  if (busyAction.value === 'report') {
    busyId.value = ''
    busyAction.value = ''
  }
  plan.value = null
  planVisible.value = false
  planLoading.value = false
  releaseResult.value = null
}

const submit = async () => {
  if (saving.value) return
  saving.value = true
  const { dns_guard: guard, ipv6_pd_guard: pdGuardSetting, ...fields } = form
  const payload = {
    ...fields,
    // Setup drafts link the DNS Agent later; only RouterOS can keep the guard on.
    ...(props.setupDraftSession ? {} : { dns_guard: {
      ...guard, enabled: form.type === 'routeros' && guard.enabled,
      bypass: guardBypassText.value.split(/[\s,，]+/).map(item => item.trim()).filter(Boolean)
    } }),
    ...(props.setupDraftSession ? {} : { ipv6_pd_guard: { enabled: form.type === 'routeros' && pdGuardSetting.enabled } }),
    ros_policy_rule_sets: showPolicyOption.value ? [...form.ros_policy_rule_sets] : [],
    manage_return_routing: form.manage_return_routing === true,
    proxy_gateway_agent_id: form.proxy_gateway_agent_id === NONE ? '' : form.proxy_gateway_agent_id,
    dns_agent_id: form.dns_agent_id === NONE ? '' : form.dns_agent_id
  }
  try {
    if (props.setupDraftSession) await setupApi.saveRouterDraft(props.setupDraftSession, payload)
    else if (editingId.value) await routerApi.update(editingId.value, payload)
    else await routerApi.create(payload)
    notify.success(props.setupDraftSession ? '主路由信息已保存，安装后可继续配置' : editingId.value ? '路由器已更新' : '路由器已添加')
    form.password = ''
    dialogVisible.value = false
    if (editingId.value) {
      clearInspection(editingId.value)
      if (routeRouter.value?.id === editingId.value) closeRouteManager()
    }
    await load()
  } catch (error) {
    notify.error(errorMessage(error, '保存失败'))
  } finally {
    saving.value = false
  }
}

const remove = async (router: RouterItem) => {
  if (!await canLeavePolicy()) return
  if (!(await confirmDanger(`删除路由器「${router.name}」？只删除 ConfigFlow 中的接入记录，不会改动路由器。`))) return
  try {
    if (props.setupDraftSession) await setupApi.saveRouterDraft(props.setupDraftSession, null)
    else await routerApi.remove(router.id)
    clearInspection(router.id)
    if (routeRouter.value?.id === router.id) closeRouteManager()
    if (policyRouter.value?.id === router.id) policyRouter.value = null
    if (syncRouter.value?.id === router.id) syncRouter.value = null
    notify.success('已删除')
    await load()
  } catch (error) {
    notify.error(errorMessage(error, '删除失败'))
  }
}

const probe = async (router: RouterItem) => {
  busyId.value = router.id
  busyAction.value = 'probe'
  try {
    const { data } = await routerApi.probe(router.id)
    notify.success(`连接成功：${typeLabel(router.type)} ${data.probe.version}`)
  } catch (error) {
    notify.error(errorMessage(error, '探测失败'))
  } finally {
    busyId.value = ''
    busyAction.value = ''
    await load()
  }
}

// Changing panels invalidates pending inspection/preview responses. The refresh
// after publishing keeps the current plan and its result available for review.
const openReport = async (router: RouterItem) => {
  if (routeBusy.value || applying.value) return
  if (!await canLeavePolicy()) return
  policyRouter.value = null
  syncRouter.value = null
  closeRouteManager()
  clearInspection()
  reportRouter.value = router
  emit('selected', router.id)
  await loadReport(router)
}

const loadReport = async (router: RouterItem) => {
  const revision = inspectionRevision
  reportLoading.value = true
  reportError.value = ''
  busyId.value = router.id
  busyAction.value = 'report'
  try {
    const { data } = await routerApi.report(router.id)
    if (revision !== inspectionRevision) return
    report.value = data.report
    reportRouter.value = router
    reportTime.value = new Date(data.generated_at).toLocaleString()
  } catch (error) {
    if (revision !== inspectionRevision) return
    reportError.value = errorMessage(error, '生成差异报告失败')
    notify.error(reportError.value)
  } finally {
    if (revision === inspectionRevision) {
      reportLoading.value = false
      busyId.value = ''
      busyAction.value = ''
    }
  }
}

const canApply = computed(() => Boolean(plan.value && !plan.value.blockers.length && (!plan.value.empty || plan.value.sync_baseline)))
const stageTitle = (key: string) => plan.value?.stages.find(stage => stage.key === key)?.title || key

const openPlan = async () => {
  const router = reportRouter.value
  if (!router) return
  const revision = inspectionRevision
  planLoading.value = true
  try {
    const { data } = await routerApi.plan(router.id)
    if (revision !== inspectionRevision || reportRouter.value?.id !== router.id) return
    plan.value = data.plan
    releaseResult.value = null
    planVisible.value = true
  } catch (error) {
    if (revision !== inspectionRevision) return
    notify.error(errorMessage(error, '生成发布计划失败'))
  } finally {
    if (revision === inspectionRevision) planLoading.value = false
  }
}

const applyPlan = async () => {
  const router = reportRouter.value
  if (!router || !plan.value || applying.value) return
  const revision = inspectionRevision
  const fingerprint = plan.value.fingerprint
  applying.value = true
  try {
    const { data } = await withPageReloadBlocked(() => routerApi.apply(router.id, fingerprint))
    if (revision !== inspectionRevision) return
    releaseResult.value = data.result
    if (data.result.ok) notify.success(plan.value?.empty ? '已重新建立同步基准' : '发布完成')
    else notify.error('发布失败，请检查发布与回滚记录')
  } catch (error: any) {
    if (revision !== inspectionRevision) return
    const data = error?.response?.data
    if (data?.result) releaseResult.value = data.result
    else if (data?.plan) plan.value = data.plan    // changed since preview: show the fresh plan
    notify.error(errorMessage(error, '发布失败'))
  } finally {
    applying.value = false
    if (revision === inspectionRevision) await loadReport(router)
  }
}

watch(() => !!(applying.value || saving.value || routeBusy.value || policyLocked.value || dialogVisible.value || planVisible.value || busyId.value), value => emit('busy', value))
onBeforeUnmount(() => emit('busy', false))

// 直达链接 /routers?router=<id>&profile=<配置空间>&preview=1：打开这台路由器的差异报告，
// preview=1 时接着打开「预览发布」（仍需用户确认才写入）。路由器在其他配置空间时先切换空间，
// 页面随空间重建后由新页面继续打开；处理后从地址中移除这三个参数。嵌入向导时不处理。
const currentRoute = inject(routeLocationKey, null)
const navigation = inject(routerKey, null)
const deepLinkOf = () => (props.embedded || props.setupDraftSession || !currentRoute ? null : readRouterDeepLink(currentRoute.query))
let pageDisposed = false
let deepLinkBusy = false
onBeforeUnmount(() => { pageDisposed = true })
const clearDeepLink = () => {
  if (!navigation || !currentRoute) return
  const query = { ...currentRoute.query }
  for (const key of ROUTER_DEEP_LINK_KEYS) delete query[key]
  void navigation.replace({ path: currentRoute.path, query, hash: currentRoute.hash })
}
/** true：已切换到链接中的配置空间。 */
const followDeepLinkProfile = async (link: RouterDeepLink) => {
  if (!link.profile || link.profile === activeProfileId.value) return false
  const profileStore = useProfileStore()
  let profiles = profileStore.profiles.value
  if (!profiles.some(item => item.id === link.profile)) {
    try { profiles = await profileStore.refreshProfiles() } catch { /* 下方按不存在处理 */ }
  }
  const profile = profiles.find(item => item.id === link.profile)
  if (!profile) {
    notify.error('链接中的配置空间不存在或已删除，下面是当前配置空间的路由器。')
    return false
  }
  if (scopedRequests.value) {
    notify.warning(`这台路由器属于配置空间「${profile.name}」。当前配置正在保存，完成后请切换到该空间再查看。`)
    return false
  }
  if (!canReloadPage()) {
    notify.warning('请先保存或关闭当前编辑，再切换到通知中的配置空间。')
    return false
  }
  setActiveProfileId(profile.id)
  notify.info(`已切换到配置空间「${profile.name}」，查看链接中的路由器。`)
  return true
}
const scrollToReport = () => document.querySelector<HTMLElement>('[data-router-report]')?.scrollIntoView?.({ block: 'start' })
const openDeepLink = async (link: RouterDeepLink) => {
  clearDeepLink()
  if (link.profile && link.profile !== activeProfileId.value) return
  const target = routers.value.find(item => item.id === link.router)
  if (!target) {
    notify.error('链接中的路由器不存在或已删除，请从提醒处重新打开。')
    return
  }
  if (dialogVisible.value || saving.value || applying.value || policyLocked.value || !canReloadPage()) {
    notify.warning('请先保存或关闭当前编辑，再打开对应路由器设置。')
    return
  }
  if (link.section === 'rule-sync') {
    await openRuleSync(target)
    await nextTick()
    await focusSetting(document.querySelector<HTMLElement>('[data-router-rule-sync]'))
    return
  }
  const opening = openReport(target)
  await nextTick()
  scrollToReport()
  await opening
  if (pageDisposed || reportRouter.value?.id !== target.id || !report.value) return
  await nextTick()
  scrollToReport()
  if (link.section) {
    const section = document.querySelector<HTMLElement>(`[data-router-${link.section}]`)
    if (section) await focusSetting(section)
    else notify.info('此设置区域当前不适用，已打开对应路由器的最新差异报告。')
  }
  if (link.preview) await openPlan()
}
const handleDeepLink = async (link: RouterDeepLink, loaded: boolean) => {
  deepLinkBusy = true
  try {
    if (await followDeepLinkProfile(link)) {
      await nextTick()
      if (pageDisposed) return  // 页面随配置空间重建，由新页面继续
      loaded = false
    }
    if (!loaded && !await load()) return
    if (!pageDisposed) await openDeepLink(link)
  } finally {
    deepLinkBusy = false
  }
}
onMounted(() => {
  const link = deepLinkOf()
  void (link ? handleDeepLink(link, false) : load())
})
watch(() => JSON.stringify(deepLinkOf()), () => {
  const link = deepLinkOf()
  if (link && !deepLinkBusy && !loading.value) void handleDeepLink(link, true)
})
</script>
