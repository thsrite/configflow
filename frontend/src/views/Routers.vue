<template>
  <div>
    <PageHeader title="上游路由">
      <template #actions>
        <Button @click="openCreate">
          <Plus class="size-4" />
          添加路由器
        </Button>
      </template>
    </PageHeader>

    <p class="mt-0 mb-4 text-[12.5px] leading-relaxed text-muted-foreground">
      在这里查看和调整主路由的分流设置。「差异报告」只检查，不会修改设备。
      点「预览发布」查看具体改动，确认后再应用；如果发布失败，会尝试撤销本次改动，并显示结果。
    </p>

    <LoadingRows v-if="loading && !routers.length" :rows="3" />

    <SectionCard v-else-if="!routers.length" :padded="false">
      <EmptyState
        :icon="RouterIcon"
        title="还没有接入路由器"
        description="接入 iKuai 4.x 或 RouterOS 7.1+，查看主路由配置与期望状态的差异。"
      >
        <Button @click="openCreate">
          <Plus class="size-4" />
          添加路由器
        </Button>
      </EmptyState>
    </SectionCard>

    <SectionCard v-else :padded="false" title="路由器">
      <div class="cf-table-wrap">
        <table class="cf-table min-w-[880px]">
          <thead>
            <tr>
              <th>名称</th>
              <th>类型</th>
              <th>管理地址</th>
              <th>版本</th>
              <th>状态</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="router in routers" :key="router.id">
              <td class="font-medium">{{ router.name }}</td>
              <td>{{ typeLabel(router.type) }}</td>
              <td class="font-mono text-[12px]">{{ router.endpoint }}</td>
              <td class="font-mono text-[12px]">{{ router.last_probe?.ok ? router.last_probe.version : '—' }}</td>
              <td>
                <StatusDot
                  :tone="!router.last_probe ? 'muted' : router.last_probe.ok ? 'success' : 'danger'"
                  :label="!router.last_probe ? '未探测' : router.last_probe.ok ? '可连接' : '连接失败'"
                />
                <p v-if="router.last_probe && !router.last_probe.ok" class="mt-1 mb-0 max-w-[260px] text-[11.5px] text-destructive-accent">
                  {{ router.last_probe.message }}
                </p>
              </td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  <Button size="sm" variant="outline" :disabled="busyId === router.id" @click="probe(router)">
                    <Loader2 v-if="busyId === router.id && busyAction === 'probe'" class="size-3.5 animate-spin" />
                    <Activity v-else class="size-3.5" />
                    探测
                  </Button>
                  <Button size="sm" variant="outline" class="max-md:min-h-11" :disabled="routeBusy || applying || policyLocked" @click="openRouteManager(router)">
                    静态路由
                  </Button>
                  <Button v-if="router.type === 'routeros'" size="sm" variant="outline" class="max-md:min-h-11" :disabled="routeBusy || applying || policyLocked" @click="openPolicyManager(router)">
                    策略路由
                  </Button>
                  <Button size="sm" :disabled="busyId === router.id || routeBusy || applying || policyLocked" @click="openReport(router)">
                    <Loader2 v-if="busyId === router.id && busyAction === 'report'" class="size-3.5 animate-spin" />
                    <GitCompare v-else class="size-3.5" />
                    差异报告
                  </Button>
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
      <p class="m-0 px-5 pb-4 text-[12px] leading-relaxed text-muted-foreground max-md:px-4">
        此处只修改选中的静态路由。带 cf 标记的路由可能被后续自动发布调整；NAT、DHCP DNS 和 mihomo 回流配置不随手工操作变化。
      </p>
      <p v-if="routeBusy" role="status" class="m-0 flex items-center gap-2 px-5 pb-4 text-[12px] text-muted-foreground max-md:px-4"><Loader2 class="size-3.5 animate-spin" />正在准备或执行路由变更…</p>
      <p v-if="routeError" role="alert" class="m-0 break-words px-5 pb-4 text-[12.5px] text-destructive-accent max-md:px-4">{{ routeError }}</p>
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

    <SectionCard v-if="reportRouter && !report" class="mt-4" :title="`差异报告 · ${reportRouter.name}`">
      <p v-if="reportLoading" role="status" class="m-0 flex items-center gap-2 text-muted-foreground"><Loader2 class="size-4 animate-spin" />正在生成差异报告…</p>
      <template v-else-if="reportError">
        <p role="alert" class="mt-0 break-words text-destructive-accent">{{ reportError }}</p>
        <Button variant="outline" @click="loadReport(reportRouter)">重试</Button>
      </template>
    </SectionCard>

    <template v-if="report">
      <SectionCard class="mt-4" :title="`差异报告 · ${reportRouter?.name}`" :description="`生成于 ${reportTime}`">
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
      </SectionCard>

      <SectionCard
        v-if="report.policy_routing"
        class="mt-4"
        :padded="false"
        title="按设备和目标 IP 分流"
        :description="report.policy_routing.enabled
          ? '指定设备访问下表 IP 网段时，将流量交给 Mihomo 处理。修改后需发布到路由器才会生效。'
          : '本次未选择按设备分流的规则集。'"
        help="设备 IP 和访问的目标 IP 都符合条件时，才会使用这项策略；其他流量仍按 ROS 原有路由处理。

设备范围来自当前配置空间已保存的 OxiDNS「智能分流」设置。范围重叠时，更具体的 IP 或网段设置优先；Mihomo 网关本身不参与。这里统计的是地址或网段条数，不是设备台数。

Fake-IP 和未选入这项策略的规则集，仍按上方的静态路由设置处理。

「已同步」表示路由器配置与当前设置一致，不代表已测试上网是否正常。ROS 内部通过地址列表、Mangle 标记规则和独立路由表实现分流。"
      >
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] max-md:px-4">
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
        <p v-if="report.nat_bypass.disabled_rules.length" class="m-0 px-5 pb-4 text-[12px] text-muted-foreground max-md:px-4">
          另有 {{ report.nat_bypass.disabled_rules.length }} 条已停用的免转换规则：
          {{ report.nat_bypass.disabled_rules.map(rule => rule.comment || rule.rule_id).join('、') }}
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
        description="只更新包含所选 mihomo 网关的唯一 DHCP 网段；未关联 OxiDNS Agent 时保留现有 DNS。"
      >
        <template #actions>
          <StatusDot :tone="dhcpDnsStatus.tone" :label="dhcpDnsStatus.label" />
        </template>
        <div class="flex flex-col gap-2 px-5 pb-4 text-[12.5px] max-md:px-4">
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
        <p v-if="dhcpDns.status !== 'disabled'" class="m-0 px-5 pb-4 text-[12px] leading-relaxed text-muted-foreground max-md:px-4">
          发布会将 DNS 列表替换为所选服务器，不保留旧备用 DNS。终端在 DHCP 续租或重新联网后获得新 DNS。
        </p>
      </SectionCard>

      <SectionCard
        v-if="report.routes.unmanaged_other.length || report.routes.extra.length"
        class="mt-4"
        :padded="false"
        title="其他路由"
        :description="report.routes.mode === 'policy'
          ? '指向代理网关的静态路由会让 mihomo 自身流量回环：手工路由将迁入只增不删的策略路由「cf-mihomo-manual」后删除；多余项为 ConfigFlow 标记但已不再需要的网段。'
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
          <p v-if="!plan.blockers.length && plan.empty" class="m-0 text-muted-foreground">已全部同步，无需发布。</p>

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

          <div v-if="releaseResult" class="flex flex-col gap-1.5">
            <p class="m-0 font-semibold">{{ releaseStatus }}</p>
            <ul class="m-0 flex list-none flex-col gap-1 p-0">
              <li v-for="(entry, index) in [...releaseResult.log, ...releaseResult.rollback]" :key="index" class="flex items-start gap-2">
                <span :class="['chip shrink-0', LOG_STATUS[entry.status]?.chip || 'chip-acc']">{{ LOG_STATUS[entry.status]?.label || entry.status }}</span>
                <span>{{ stageTitle(entry.stage) }} · {{ entry.summary }}</span>
              </li>
            </ul>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="planVisible = false">{{ releaseResult ? '关闭' : '取消' }}</Button>
          <Button v-if="!releaseResult" :disabled="!canApply || applying" @click="applyPlan">
            <Loader2 v-if="applying" class="size-4 animate-spin" />
            确认发布
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="dialogVisible" @update:open="value => { if (!saving) dialogVisible = value }">
      <DialogContent class="max-h-[90dvh] max-w-[480px] overflow-y-auto max-md:[&_[data-slot=button]]:min-h-11 max-md:[&_[role=combobox]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-w-11">
        <DialogHeader>
          <DialogTitle>{{ editingId ? '编辑路由器' : '添加路由器' }}</DialogTitle>
          <DialogDescription>保存仅更新 ConfigFlow 设置，确认发布后才会写入路由器。</DialogDescription>
        </DialogHeader>

        <form class="flex min-w-0 flex-col gap-4" @submit.prevent="submit">
          <div class="flex flex-col gap-1.5">
            <Label for="router-name">名称</Label>
            <Input id="router-name" v-model="form.name" class="bg-background/50" maxlength="80" placeholder="例如 主路由" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="router-type">类型</Label>
            <Select v-model="form.type">
              <SelectTrigger id="router-type" class="w-full bg-background/50"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="ikuai">iKuai 4.x</SelectItem>
                <SelectItem value="routeros">RouterOS 7.1+</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <FormField label="管理地址" html-for="router-endpoint" :help="form.type === 'routeros' ? routerHelp.endpointROS : routerHelp.endpointIkuai">
            <Input
              id="router-endpoint"
              v-model="form.endpoint"
              class="bg-background/50 font-mono"
              :placeholder="form.type === 'routeros' ? 'https://192.168.88.1' : 'http://192.168.1.1'"
            />
          </FormField>
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <FormField label="用户名" html-for="router-username" :help="accountHelp">
              <Input id="router-username" v-model="form.username" class="bg-background/50" autocomplete="off" />
            </FormField>
            <div class="flex flex-col gap-1.5">
              <Label for="router-password">密码</Label>
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
            <FieldHelp label="校验 HTTPS 证书" :text="routerHelp.tls" />
          </div>
          <FormField label="代理网关（mihomo Agent）" html-for="router-gateway" :help="form.type === 'routeros' ? routerHelp.gatewayROS : routerHelp.gateway">
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
          <div class="flex flex-col gap-1.5">
            <div class="flex min-h-11 items-center justify-between gap-3">
              <div class="flex min-w-0 items-center gap-1">
                <Label for="router-return-routing">自动配置回流</Label>
                <FieldHelp label="自动配置回流" :text="routerHelp.returnRouting" />
              </div>
              <Switch
                id="router-return-routing"
                :model-value="form.manage_return_routing"
                @update:model-value="value => (form.manage_return_routing = Boolean(value))"
              />
            </div>
          </div>
          <FormField :label="form.type === 'routeros' ? '使用静态路由的 IP 规则集' : '按 IP 路由给 mihomo 的规则集'" html-for="router-ip-rule-sets" :help="routerHelp.staticRules">
            <div class="[&_[role=combobox]]:min-h-11">
              <MultiSelect :model-value="form.ip_rule_sets" trigger-id="router-ip-rule-sets" :options="ruleSetOptions" placeholder="仅 fake-ip 网段" @update:model-value="selectStaticRuleSets" />
            </div>
          </FormField>
          <FormField v-if="form.type === 'routeros'" label="使用策略路由的 IP 规则集" html-for="router-policy-rule-sets" :help="routerHelp.policyRules">
            <div class="[&_[role=combobox]]:min-h-11">
              <MultiSelect :model-value="form.ros_policy_rule_sets" trigger-id="router-policy-rule-sets" :options="ruleSetOptions" placeholder="不使用策略路由" @update:model-value="selectPolicyRuleSets" />
            </div>
          </FormField>
          <FormField label="DHCP DNS 服务器（OxiDNS Agent）" html-for="router-dns" :help="routerHelp.dns">
            <Select v-model="form.dns_agent_id">
              <SelectTrigger id="router-dns" class="w-full bg-background/50"><SelectValue placeholder="不关联" /></SelectTrigger>
              <SelectContent class="max-w-[calc(100vw-2rem)]">
                <SelectItem :value="NONE">不关联</SelectItem>
                <SelectItem v-for="agent in agentsOf('oxidns')" :key="agent.id" :value="agent.id" class="whitespace-normal break-words">
                  {{ agent.name }}（{{ agent.host }}）
                </SelectItem>
              </SelectContent>
            </Select>
          </FormField>
        </form>

        <DialogFooter>
          <Button variant="outline" :disabled="saving" @click="dialogVisible = false">取消</Button>
          <Button :disabled="saving" @click="submit">
            <Loader2 v-if="saving" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { matchedRouteKey, onBeforeRouteLeave } from 'vue-router'
import {
  Activity,
  CircleX,
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
import PolicyRoutingPanel from '@/components/routers/PolicyRoutingPanel.vue'
import RouterOperationPreview from '@/components/routers/RouterOperationPreview.vue'
import { agentApi, routerApi, ruleApi, type StaticRouteChange, type StaticRouteItem, type StaticRoutePlan, type StaticRouteValues } from '@/api'
import { activeProfileId, getActiveProfileId } from '@/profileContext'
import { confirm, confirmDanger, confirmState, notify, settleConfirm } from '@/lib/feedback'

type RouterType = 'ikuai' | 'routeros'
type Status = 'synced' | 'adoptable' | 'conflict' | 'missing' | 'extra' | 'not_required' | 'unknown' | 'pending'
type ReportSummary = Partial<Record<Status, number>>
type DhcpDnsStatus = 'disabled' | 'synced' | 'pending' | 'unknown'

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
  has_password: boolean
  last_probe?: { ok: boolean; version?: string; message?: string; at: string }
}

interface AgentItem {
  id: string
  name: string
  host: string
  service_type: string
}

const NONE = '__none__'
const routerHelp = {
  endpointROS: '填写路由器管理地址。RouterOS 7.1+ 支持 HTTPS（www-ssl）；HTTP（www）需要 7.9+。',
  endpointIkuai: '填写 iKuai 4.x 的管理地址，例如 http://192.168.1.1。',
  tls: '使用 HTTPS 时校验证书的有效性和签发机构；使用 HTTP 时此选项不生效。',
  gateway: '选择用于代理转发的 mihomo Agent，其地址将作为静态代理路由的下一跳。',
  gatewayROS: '选择用于代理转发的 mihomo Agent。这个网关同时用于本路由器的静态代理路由和来源策略路由。',
  returnRouting: '用于需要将回复经主路由返回的网络，默认关闭。关闭只停止管理，已有回流规则会保留。',
  staticRules: 'fake-ip 网段始终包含。只勾选客户端直接连 IP、不经过 DNS 的规则集（如 Telegram）；其余流量经 OxiDNS 解析成 fake-ip 后自然进入 mihomo。',
  policyRules: '两组独立选择，同一规则集只能使用一种方式；选到另一组时自动移出原组。fake-ip 始终使用静态路由。取消选择后停止下发该规则集，保存后需预览并发布才生效。\n\n来源取当前配置空间已保存的 OxiDNS 设备策略，仅包含显式设为智能分流的 IPv4 地址或网段；保留更具体的直连例外并排除 Mihomo 自身。请同步发布 OxiDNS 设备策略。',
  dns: '选择后，将包含所选 mihomo 网关的唯一 DHCP 网段的 DNS 列表替换为此服务器，不保留旧备用 DNS。默认不关联，不修改现有 DNS；终端续租或重新联网后获得新 DNS。'
}
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
const DHCP_DNS_STATUS = {
  disabled: { label: '未关联', tone: 'muted' },
  synced: { label: '已同步', tone: 'success' },
  pending: { label: '待更新', tone: 'warning' },
  unknown: { label: '待确认', tone: 'warning' }
} as const
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
const busyId = ref('')
const busyAction = ref<'probe' | 'report' | ''>('')
const report = ref<any>(null)
const reportSummary = computed(() => (report.value?.summary || {}) as ReportSummary)
const dhcpDns = computed(() => report.value?.dhcp_dns as DhcpDnsReport | undefined)
const dhcpDnsStatus = computed(() => DHCP_DNS_STATUS[dhcpDns.value?.status || 'disabled'])
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
  if (result.ok) return '发布完成'
  if (result.rollback.some(entry => entry.status === 'failed')) return '发布失败，回滚未完成'
  if (result.rollback.length) return '发布失败，已撤销已完成的变更'
  return '发布失败，请检查下方记录'
})

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
  ros_policy_rule_sets: [] as string[]
})
const form = reactive(emptyForm())

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
const policyPanel = ref<InstanceType<typeof PolicyRoutingPanel> | null>(null)
const policyLocked = ref(false)
const canLeavePolicy = async () => await policyPanel.value?.canLeave() ?? true
if (inject(matchedRouteKey, undefined)?.value) onBeforeRouteLeave(canLeavePolicy)
const openPolicyManager = async (router: RouterItem) => {
  if (routeBusy.value || applying.value || policyLocked.value || policyRouter.value?.id === router.id) return
  if (!await canLeavePolicy()) return
  closeRouteManager()
  clearInspection()
  policyRouter.value = router
}
const routeRows = ref<StaticRouteItem[]>([])
const routeLoading = ref(false)
const routeBusy = ref(false)
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
    const response = await routerApi.routeApply(context.id!, { ...change, fingerprint: preview.fingerprint }, context.profile)
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
  closeRouteManager()
  clearInspection()
}
watch(activeProfileId, closePanels, { flush: 'sync' })
onBeforeUnmount(closePanels)

const returnTone = computed(() => {
  const state = report.value?.return_routing
  if (!state || state.status !== 'ok') return 'muted'
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
      routerApi.list(),
      agentApi.getAll(),
      ruleApi.getAll()
    ])
    if (!isCurrent()) return false
    if (routerResult.status === 'rejected') throw routerResult.reason
    routers.value = routerResult.value.data.routers || []
    const agentData = agentResult.status === 'fulfilled' ? agentResult.value.data : []
    agents.value = Array.isArray(agentData) ? agentData : agentData?.data || agentData?.agents || []
    const ruleData = ruleResult.status === 'fulfilled' ? ruleResult.value.data : []
    const rules = Array.isArray(ruleData) ? ruleData : ruleData?.data || []
    ruleSets.value = rules
      .filter((rule: any) => rule.itemType === 'ruleset')
      .map((rule: any) => ({ id: rule.id, name: rule.name || rule.id, policy: rule.policy || '' }))
    if (agentResult.status === 'rejected' || ruleResult.status === 'rejected') {
      notify.error('关联 Agent 或规则集暂时无法读取，仍可管理设备上的静态路由')
    }
    return true
  } catch (error) {
    if (isCurrent()) notify.error(errorMessage(error, '加载路由器失败'))
    return false
  } finally {
    if (isCurrent()) loading.value = false
  }
}

const openCreate = async () => {
  if (!await canLeavePolicy()) return
  policyRouter.value = null
  Object.assign(form, emptyForm())
  editingId.value = ''
  editingHasPassword.value = false
  dialogVisible.value = true
}

const openEdit = async (router: RouterItem) => {
  if (!await canLeavePolicy()) return
  policyRouter.value = null
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
    ros_policy_rule_sets: [...(router.ros_policy_rule_sets || [])]
  })
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
  const payload = {
    ...form,
    ros_policy_rule_sets: form.type === 'routeros' ? [...form.ros_policy_rule_sets] : [],
    manage_return_routing: form.manage_return_routing === true,
    proxy_gateway_agent_id: form.proxy_gateway_agent_id === NONE ? '' : form.proxy_gateway_agent_id,
    dns_agent_id: form.dns_agent_id === NONE ? '' : form.dns_agent_id
  }
  try {
    if (editingId.value) await routerApi.update(editingId.value, payload)
    else await routerApi.create(payload)
    notify.success(editingId.value ? '路由器已更新' : '路由器已添加')
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
    await routerApi.remove(router.id)
    clearInspection(router.id)
    if (routeRouter.value?.id === router.id) closeRouteManager()
    if (policyRouter.value?.id === router.id) policyRouter.value = null
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
  closeRouteManager()
  clearInspection()
  reportRouter.value = router
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

const canApply = computed(() => Boolean(plan.value && !plan.value.blockers.length && !plan.value.empty))
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
  applying.value = true
  try {
    const { data } = await routerApi.apply(router.id, plan.value.fingerprint)
    if (revision !== inspectionRevision) return
    releaseResult.value = data.result
    if (data.result.ok) notify.success('发布完成')
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

onMounted(load)
</script>
