<template>
  <div class="min-w-0">
    <PageHeader title="客户端设备" :mobile-help="pageHelp">
      <template #title-extra>
        <span v-if="state && (fromAgent || fromProfile)" class="min-w-0 max-w-48 truncate rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground" :title="`配置空间：${state.profile_name}`">配置：{{ state.profile_name }}</span>
      </template>
      <template #actions>
        <Button v-if="fromAgent" variant="outline" class="min-h-11" @click="router.push(oxidnsMonitorPath(fromAgent))"><ArrowLeft class="size-4" />返回 OxiDNS 面板</Button>
        <Button v-else-if="backTo" variant="outline" class="min-h-11" @click="router.push(backTo)"><ArrowLeft class="size-4" />{{ fromSetup ? '返回配置向导' : '返回上游路由' }}</Button>
        <Button variant="outline" class="min-h-11 max-sm:size-11 max-sm:gap-0" aria-label="重新载入" title="重新载入设备和路由器" :disabled="loading || publishing || fixing" @click="reload">
          <RefreshCw class="size-4" :class="{ 'animate-spin motion-reduce:animate-none': loading || discovering }" /><span class="max-sm:sr-only">重新载入</span>
        </Button>
      </template>
    </PageHeader>
    <p class="mb-5 mt-0 max-w-3xl text-[13px] leading-relaxed text-muted-foreground max-md:hidden">{{ pageHelp }}</p>

    <SectionCard v-if="loading && !state" title="设备列表"><LoadingRows :rows="4" /></SectionCard>
    <SectionCard v-else-if="!state">
      <EmptyState :icon="MonitorSmartphone" title="暂时无法读取客户端设备" :description="error || '正在读取配置空间。'">
        <Button variant="outline" class="min-h-11" :disabled="loading" @click="load">重试</Button>
      </EmptyState>
    </SectionCard>
    <template v-else>
      <SectionCard title="设备列表" :help="phone ? `${listHelp}\n\n${modeHelp}` : listHelp" class="mb-5">
        <template #actions>
          <span class="flex min-h-11 items-center gap-1.5 text-xs text-muted-foreground" role="status" aria-live="polite">
            <template v-if="previewing"><Loader2 v-if="loading" class="size-3.5 animate-spin motion-reduce:animate-none" />{{ loading ? '正在核对最新记录…' : '上次记录 · 暂不可编辑' }}</template>
            <template v-else-if="savingIp"><Loader2 class="size-3.5 animate-spin motion-reduce:animate-none" />正在保存…</template>
            <template v-else-if="savedOnce"><Check class="size-3.5" />已保存</template>
            <template v-else>修改自动保存</template>
          </span>
        </template>
        <p v-if="!state.editable" role="alert" class="mb-4 mt-0 rounded-lg bg-warning-soft p-3 text-sm text-warning-accent">{{ state.reason }} 名称仍可修改。</p>
        <p v-else-if="fromSetup" role="status" class="mb-4 mt-0 rounded-lg bg-info-soft p-3 text-sm text-info-accent" data-setup-names-only>配置向导进行中，这里只用于命名。DNS 策略会改动向导刚发布的 OxiDNS 配置，请完成向导后再设置 DNS 策略。</p>
        <div v-if="state.editable && !fromSetup && sideEntries.length" role="note" class="mb-4 rounded-lg bg-warning-soft p-3 text-sm max-sm:p-2.5" data-side-entries>
          <p class="m-0 flex items-start gap-1.5 font-medium text-warning-accent"><TriangleAlert class="mt-0.5 size-4 shrink-0" />以下入口没有经过主处理链，设备策略对它们不生效：</p>
          <ul class="mb-0 mt-1.5 list-none space-y-1 p-0 pl-[1.375rem] text-[13px]">
            <li v-for="item in sideEntries" :key="item.tags.join('|')" class="break-words [overflow-wrap:anywhere]">
              <span class="font-mono">{{ item.tags.join('、') }}</span>（<span class="font-mono">{{ item.listen }}</span>）<span class="text-muted-foreground"> · 处理链 <span class="font-mono">{{ item.entry }}</span></span>
            </li>
          </ul>
          <p class="mb-0 mt-1.5 pl-[1.375rem] text-xs leading-relaxed text-muted-foreground">它们用自己的处理链，客户端只有查询主入口时才按设备策略处理。要让这些入口也受控，请在 OxiDNS 完整参数中把它们的 entry 改为 sequence_main。</p>
        </div>
        <p v-if="error" role="alert" class="mb-4 mt-0 text-sm text-destructive-accent">{{ error }}</p>
        <p v-if="fixedError" role="alert" class="mb-4 mt-0 text-sm text-destructive-accent">{{ fixedError }}</p>
        <p v-if="fixing" role="status" class="mb-3 flex items-center gap-2 text-sm text-muted-foreground"><Loader2 class="size-4 animate-spin motion-reduce:animate-none" />正在处理固定 IP，请保持页面打开…</p>
        <div v-if="reminder" role="status" class="mb-4 flex flex-wrap items-start gap-x-3 gap-y-2 rounded-lg bg-primary-soft p-3 text-sm max-sm:p-2.5">
          <div class="min-w-0 flex-1">
            <p class="m-0 font-medium text-primary-accent">已添加 {{ reminder }} 台设备</p>
            <p class="mb-0 mt-1 text-[13px]">{{ state.agents.length ? 'DNS 策略要在下方「发布 DNS 策略」发布到 OxiDNS 后生效。' : '此配置空间没有 OxiDNS Agent，DNS 策略会在接入 OxiDNS 并发布后生效。' }}<template v-if="policyRouting">已启用 RouterOS 策略路由，智能分流设备还需在<RouterLink to="/routers" class="text-primary-accent underline-offset-2 hover:underline">上游路由</RouterLink>重新发布。</template></p>
          </div>
          <div class="flex items-center gap-1">
            <Button v-if="state.agents.length" variant="outline" class="min-h-11" @click="toPublish">前往发布</Button>
            <Button variant="ghost" size="icon" class="size-11" aria-label="关闭提醒" @click="reminder = 0"><X class="size-4" /></Button>
          </div>
        </div>

        <Tabs :model-value="tab" class="min-w-0" @update:model-value="changeTab(String($event))">
          <TabsList class="mb-3 min-h-11 max-sm:grid max-sm:h-auto max-sm:w-full max-sm:grid-cols-3">
            <TabsTrigger v-for="item in tabItems" :key="item.value" :value="item.value" class="min-h-11 px-4 max-sm:px-2">
              {{ item.label }}
              <Loader2 v-if="item.count === null && discovering" class="size-3.5 animate-spin motion-reduce:animate-none" aria-label="正在读取" />
              <span v-else-if="item.count !== null" class="num text-xs opacity-80">{{ item.count }}</span>
            </TabsTrigger>
          </TabsList>

          <div v-if="!unsupported || tab === 'added'" class="mb-3 flex min-h-5 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground" aria-live="polite">
            <template v-if="discovery && discovery.state !== 'unsupported'">
              <span>设备来源</span>
              <StatusDot v-for="source in discovery.sources" :key="source.id" :tone="source.ok ? 'success' : 'danger'"
                :label="`${source.name}（${source.type === 'ikuai' ? 'iKuai' : 'RouterOS'}）`" />
              <span v-if="checkedAt" class="num" :class="{ 'text-warning-accent': discovery.cached }">{{ discovery.cached ? '上次读取' : '读取时间' }} {{ checkedAt }}</span>
            </template>
            <template v-if="discovering"><Loader2 class="size-3.5 animate-spin text-primary-accent motion-reduce:animate-none" />{{ discovery ? '正在后台更新路由器上的设备…' : '正在读取路由器上的设备，最长约 20 秒…' }}</template>
            <span v-if="discoveryError" role="alert" class="text-destructive-accent">暂时无法读取路由器：{{ discoveryError }}{{ discovery ? '；仍显示上次结果。' : '' }}</span>
            <template v-else-if="discovery?.state === 'unsupported'">{{ discovery.message }}</template>
          </div>
          <ul v-if="sourceProblems.length" class="mb-3 mt-0 list-none space-y-1 p-0 text-xs" aria-label="设备来源提示">
            <li v-for="item in sourceProblems" :key="item.key" :role="item.error ? 'alert' : undefined"
              :class="['flex items-start gap-1.5', item.error ? 'text-destructive-accent' : 'text-muted-foreground']">
              <TriangleAlert v-if="item.error" class="mt-px size-3.5 shrink-0" /><Info v-else class="mt-px size-3.5 shrink-0" />
              <span class="min-w-0">{{ item.text }}</span>
            </li>
          </ul>

          <div v-if="!unsupported || tab === 'added'" class="mb-3 flex min-w-0 flex-wrap items-center gap-2">
            <div class="relative min-w-0 flex-1 basis-56 sm:max-w-sm">
              <Search class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input v-model="search" class="min-h-11 pl-9" aria-label="搜索设备" placeholder="搜索名称、IP 或 MAC…" />
            </div>
            <div v-if="tab === 'added'" class="flex min-w-0 items-center gap-1 max-sm:w-full sm:ml-auto">
              <Input id="client-device-ip" v-model="newIP" :disabled="previewing" aria-label="手动添加 IP / 网段" class="min-h-11 min-w-0 flex-1 font-mono sm:w-60" :placeholder="phone ? 'IP / 网段' : '192.168.50.0/24 或 IPv6'"
                @keydown.enter.prevent="add(newIP)" />
              <FieldHelp label="手动添加 IP / 网段" :text="rangeHelp" />
              <Button variant="outline" class="min-h-11" :disabled="previewing || !newIP.trim()" @click="add(newIP)"><Plus class="size-4" />添加</Button>
            </div>
            <div v-else-if="tab === 'new' && picked.length" class="flex flex-wrap items-center gap-1 max-sm:w-full sm:ml-auto">
              <label v-if="!fromSetup" class="mr-2 flex min-h-11 items-center gap-2 text-xs max-sm:basis-full" :title="pickedFixable.length ? '仅固定已选择且符合条件的设备，其余设备只添加记录' : '所选设备均不满足固定 IP 条件，原因见各行'">
                <Checkbox :model-value="allFixedPicked ? true : someFixedPicked ? 'indeterminate' : false" :disabled="!pickedFixable.length || !!adding || fixing" aria-label="同时固定所选设备 IP" @update:model-value="value => pickFixedBatch(value === true)" />
                同时固定 IP<span class="num">（{{ pickedFixable.length }} 台可用）</span>
              </label>
              <Button class="min-h-11 max-sm:flex-1" :disabled="previewing || publishing || !!adding || fixing" @click="addFound(pickedFound)">
                <Loader2 v-if="adding" class="size-4 animate-spin motion-reduce:animate-none" /><Plus v-else class="size-4" />{{ adding ? `正在添加（${adding}）…` : `添加所选（${picked.length}）` }}
              </Button>
              <Button variant="ghost" class="min-h-11" :disabled="previewing || !!adding" @click="picked = []">取消选择</Button>
            </div>
          </div>

          <TabsContent v-for="value in savedTabs" :key="value" :value="value" class="mt-0 min-w-0">
            <EmptyState v-if="value === 'offline' && unsupported" :icon="Router" title="无法判断设备是否离线" :description="discovery!.message">
              <Button as-child variant="outline" class="min-h-11"><RouterLink to="/routers">前往上游路由</RouterLink></Button>
            </EmptyState>
            <LoadingRows v-else-if="value === 'offline' && !discovery && discovering" :rows="3" />
            <EmptyState v-else-if="value === 'offline' && discovery?.state === 'failed'" :icon="Router" title="无法判断设备是否离线" :description="discovery.message" />
            <EmptyState v-else-if="value === 'offline' && !discovery && discoveryError" :icon="Router" title="无法判断设备是否离线" :description="discoveryError">
              <Button variant="outline" class="min-h-11" :disabled="discovering" @click="rediscover">重试</Button>
            </EmptyState>
            <div v-else-if="visibleRows.length" class="cf-table-wrap">
              <table class="cf-table cf-mobile-records client-device-records w-full min-w-[760px]" role="table">
                <thead role="rowgroup">
                  <tr role="row">
                    <th role="columnheader" scope="col">设备</th>
                    <th role="columnheader" scope="col">IP / 网段</th>
                    <th role="columnheader" scope="col">状态</th>
                    <th role="columnheader" scope="col"><span class="inline-flex items-center gap-1">DNS 策略<FieldHelp label="DNS 策略" :text="modeHelp" /></span></th>
                    <th role="columnheader" scope="col" class="text-right"><span class="sr-only">操作</span></th>
                  </tr>
                </thead>
                <tbody role="rowgroup">
                  <tr v-for="row in visibleRows" :key="row.ip" role="row" :data-client-ip="row.ip" @focusin="hold(row.ip)">
                    <td role="cell">
                      <Input v-model="row.name" :aria-label="row.ip + ' 名称'" maxlength="80" :placeholder="infoOf(row.ip)?.hostname || '设备或网段名称'" class="min-h-11 min-w-36 max-sm:min-w-0"
                        :readonly="previewing" :disabled="publishing || fixing" @blur="save(row.ip)" @keydown.enter.prevent="save(row.ip)" />
                      <p v-if="infoOf(row.ip)?.mac" class="mb-0 mt-1 font-mono text-[11px] text-muted-foreground">{{ infoOf(row.ip)!.mac }}</p>
                    </td>
                    <td role="cell">
                      <span class="whitespace-nowrap font-mono text-xs max-sm:text-[13px] max-sm:font-medium">{{ row.ip }}</span>
                      <span v-if="!saved(row.ip)" class="chip ml-2">未保存</span>
                      <ClientSightingChips :sources="infoOf(row.ip)?.sources" />
                    </td>
                    <td role="cell" data-label="状态">
                      <StatusDot v-bind="statusOf(row.ip)" />
                      <span v-if="!fromSetup && infoOf(row.ip)?.fixed_ip?.status === 'static'" class="chip chip-ok ml-2" :title="infoOf(row.ip)?.fixed_ip?.owned ? '由 ConfigFlow 固定 IP' : '路由器已有静态 DHCP 绑定'">已固定</span>
                      <p v-if="hintOf(row.ip)" class="mb-0 mt-0.5 text-[11.5px] text-muted-foreground">{{ hintOf(row.ip) }}</p>
                    </td>
                    <td role="cell" data-label="DNS 策略">
                      <div class="flex flex-wrap items-center gap-2">
                        <Select :model-value="row.mode || 'default'" :disabled="previewing || publishing || fixing || !policyEditable" @update:model-value="value => setMode(row, String(value))">
                          <SelectTrigger :aria-label="row.ip + ' DNS 策略'" class="min-h-11 w-36 max-sm:w-full"><SelectValue /></SelectTrigger>
                          <SelectContent>
                            <SelectItem value="default">默认</SelectItem>
                            <SelectItem value="smart">智能分流</SelectItem>
                            <SelectItem value="direct">直连</SelectItem>
                          </SelectContent>
                        </Select>
                        <span v-if="routesSource(row)" class="chip chip-acc" title="RouterOS 策略路由按智能分流的 IPv4 来源转发">参与策略路由</span>
                      </div>
                    </td>
                    <td role="cell" class="text-right">
                      <div class="flex items-center justify-end gap-2">
                        <div v-if="!fromSetup && infoOf(row.ip)?.fixed_ip?.status === 'dynamic'" class="min-w-0">
                          <Button variant="outline" class="min-h-11" :aria-label="`固定 IP ${row.ip}`" :disabled="previewing || publishing || fixing || !!adding || !canFix(infoOf(row.ip))" @click="fixExisting(row)">固定 IP</Button>
                          <p v-if="!previewing && !canFix(infoOf(row.ip))" class="mb-0 mt-1 max-w-48 text-left text-[11.5px] text-muted-foreground">{{ fixedReason(infoOf(row.ip)) }}</p>
                        </div>
                        <Loader2 v-if="savingIp === row.ip" class="size-4 animate-spin text-muted-foreground motion-reduce:animate-none" aria-label="正在保存" />
                        <span v-else-if="failures[row.ip]" class="flex items-center gap-1 text-xs text-destructive-accent" role="alert">
                          {{ failures[row.ip] }}<Button variant="ghost" class="min-h-11 px-2 text-xs" @click="save(row.ip)">重试</Button>
                        </span>
                        <Button variant="ghost" size="icon" class="size-11" :aria-label="'移除 ' + row.ip" :disabled="previewing || publishing || fixing || (!policyEditable && !!saved(row.ip)?.mode)" @click="remove(row)"><Trash2 class="size-4" /></Button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else-if="search.trim() && tabRows.length" class="m-0 py-6 text-center text-sm text-muted-foreground">没有匹配「{{ search }}」的设备。</p>
            <EmptyState v-else-if="value === 'offline'" :icon="MonitorSmartphone" title="没有离线的设备" description="已添加的设备都在线，或路由器无法判断。" />
            <EmptyState v-else :icon="MonitorSmartphone" title="尚未添加客户端设备" description="在「未添加」中选择路由器发现的设备，或输入 IP / 网段。可为设备命名，或设置 DNS 策略。" />
          </TabsContent>

          <TabsContent value="new" class="mt-0 min-w-0">
            <EmptyState v-if="unsupported" :icon="Router" title="无法发现设备" :description="discovery!.message">
              <Button as-child variant="outline" class="min-h-11"><RouterLink to="/routers">前往上游路由</RouterLink></Button>
            </EmptyState>
            <LoadingRows v-else-if="!discovery && discovering" :rows="4" />
            <div v-else-if="visibleFound.length" class="cf-table-wrap">
              <table class="cf-table cf-mobile-records client-discovery-records w-full min-w-[760px]" role="table">
                <thead role="rowgroup">
                  <tr role="row">
                    <th role="columnheader" scope="col" class="w-11">
                      <label class="-m-2.5 flex size-11 cursor-pointer items-center justify-center">
                        <Checkbox :model-value="allPicked ? true : somePicked ? 'indeterminate' : false" :disabled="!pickable.length || !!adding" :aria-label="search.trim() ? '选择全部搜索结果' : '选择全部设备'"
                          @update:model-value="value => pickAll(value === true)" />
                      </label>
                    </th>
                    <th role="columnheader" scope="col">设备</th>
                    <th role="columnheader" scope="col">IP</th>
                    <th role="columnheader" scope="col">状态</th>
                    <th role="columnheader" scope="col"><span class="inline-flex items-center gap-1">DNS 策略<FieldHelp label="DNS 策略" :text="foundHelp" /></span></th>
                    <th role="columnheader" scope="col" class="text-right"><span class="sr-only">操作</span></th>
                  </tr>
                </thead>
                <tbody role="rowgroup">
                  <tr v-for="item in visibleFound" :key="item.ip" role="row" :data-found-ip="item.ip">
                    <td role="cell">
                      <label class="-m-2.5 flex size-11 cursor-pointer items-center justify-center">
                        <Checkbox :model-value="picked.includes(item.ip)" :disabled="!canAdd(item) || !!adding" :aria-label="`选择 ${item.ip}`"
                          @update:model-value="value => pick(item.ip, value === true)" />
                      </label>
                    </td>
                    <td role="cell">
                      <span :class="['block break-all', item.hostname ? 'font-medium' : 'text-muted-foreground']">{{ item.hostname || '未命名设备' }}</span>
                      <span v-if="item.mac" class="block font-mono text-[11px] text-muted-foreground">{{ item.mac }}</span>
                      <div v-if="!fromSetup" class="mt-1 max-w-64 text-left">
                        <span v-if="item.fixed_ip?.status === 'static'" class="chip chip-ok">已固定</span>
                        <template v-else>
                          <label class="flex min-h-11 items-center gap-2 text-xs">
                            <Checkbox :model-value="fixedPicked.includes(item.ip)" :disabled="!canFix(item) || !canAdd(item) || !!adding || fixing" :aria-label="`同时固定 IP ${item.ip}`" @update:model-value="value => pickFixed(item.ip, value === true)" />同时固定 IP
                          </label>
                          <p v-if="!canFix(item)" class="m-0 text-[11.5px] text-muted-foreground">{{ fixedReason(item) }}</p>
                        </template>
                      </div>
                    </td>
                    <td role="cell">
                      <span class="whitespace-nowrap font-mono text-xs max-sm:text-[13px]">{{ item.ip }}</span>
                      <ClientSightingChips :sources="item.sources" />
                    </td>
                    <td role="cell">
                      <StatusDot v-bind="statusOf(item.ip)" />
                      <p v-if="hintOf(item.ip)" class="mb-0 mt-0.5 text-[11.5px] text-muted-foreground">{{ hintOf(item.ip) }}</p>
                    </td>
                    <td role="cell" :class="{ 'client-found-default': !covering(item.ip) }">
                      <span v-if="covering(item.ip)" class="chip max-w-full" :title="`落在网段 ${covering(item.ip)!.ip}${covering(item.ip)!.name ? `（${covering(item.ip)!.name}）` : ''}，沿用它的 DNS 策略`">
                        <span class="truncate">随网段 <span class="font-mono">{{ covering(item.ip)!.ip }}</span> · {{ modeLabel(covering(item.ip)!.mode) }}</span>
                      </span>
                      <span v-else class="text-xs text-muted-foreground">默认</span>
                    </td>
                    <td role="cell" class="text-right">
                      <Button variant="outline" class="min-h-11" :disabled="publishing || !canAdd(item) || !!adding || fixing" :title="previewing ? '正在核对最新记录，请稍候。' : canAdd(item) ? '' : 'DNS 策略只读，且没有主机名可作名称'"
                        :aria-label="`添加 ${item.hostname || item.ip}`" @click="addFound([item])"><Plus class="size-4" />添加</Button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else-if="search.trim() && found.length" class="m-0 py-6 text-center text-sm text-muted-foreground">没有匹配「{{ search }}」的设备。</p>
            <EmptyState v-else-if="discovery && discovery.state === 'failed'" :icon="Router" title="暂时无法发现设备" :description="discovery.message" />
            <EmptyState v-else-if="discovery" :icon="MonitorSmartphone" title="没有未添加的设备" description="路由器看到的局域网设备都已添加。" />
            <EmptyState v-else :icon="Router" title="暂时无法发现设备" :description="discoveryError || '正在读取路由器。'">
              <Button variant="outline" class="min-h-11" :disabled="discovering" @click="rediscover">重试</Button>
            </EmptyState>
          </TabsContent>
        </Tabs>
      </SectionCard>

      <SectionCard id="client-device-publish" title="发布 DNS 策略" :help="publishHelp">
        <p v-if="!state.agents.length" class="m-0 text-sm text-muted-foreground">此配置空间没有 OxiDNS Agent。DNS 策略会保存在草稿中，接入 OxiDNS 并发布后生效。</p>
        <ul v-else class="m-0 list-none divide-y divide-border/70 p-0" aria-label="OxiDNS 发布目标">
          <li v-for="agent in state.agents" :key="agent.id" class="py-2">
            <div class="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1">
              <label class="flex min-h-11 min-w-0 cursor-pointer items-center gap-3">
                <Checkbox :model-value="selected.includes(agent.id)" :disabled="previewing || publishing" :aria-label="`发布到 ${agent.name}`"
                  @update:model-value="value => toggle(agent.id, value === true)" />
                <span class="min-w-0 break-all font-medium">{{ agent.name }}</span>
              </label>
              <span class="min-w-0 break-all font-mono text-xs text-muted-foreground">{{ agent.host || '—' }}</span>
              <StatusDot :tone="agent.status === 'online' ? 'success' : 'muted'" :label="agent.status === 'online' ? '在线' : '离线'" />
              <Button v-if="deployments[agent.id]?.status === 'ready'" class="ml-auto min-h-11" :disabled="previewing || publishing" @click="activate(agent.id)">应用并启动</Button>
            </div>
            <AgentDeploymentStatus v-if="deployments[agent.id]" :task="deployments[agent.id]" compact>
              <template v-if="needsNotice(agent.id)" #notice>
                <AgentOperationSteps :task="deployments[agent.id]" :show-current="false" :show-steps="false" />
                <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="deployments[agent.id]" />
              </template>
              <template #details><AgentOperationSteps :task="deployments[agent.id]" :show-current="false" :show-message="!needsNotice(agent.id)" embedded /></template>
            </AgentDeploymentStatus>
            <AgentStatusRow v-else-if="rejected[agent.id]" label="配置推送" status="失败" tone="danger">
              <template #notice><AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="rejected[agent.id]" /></template>
            </AgentStatusRow>
            <AgentDNSChecks :checks="(deployments[agent.id] || rejected[agent.id])?.dns_checks" :checked-at="(deployments[agent.id] || rejected[agent.id])?.dns_checked_at" compact />
            <p v-if="deployments[agent.id]?.status === 'ready'" class="mb-0 mt-1 text-xs text-muted-foreground">待激活的是已上传的配置快照，不包含之后保存的草稿修改。</p>
          </li>
        </ul>
        <div v-if="state.agents.length" class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <p class="m-0 min-w-0 flex-1 text-xs text-muted-foreground">发布的是「{{ state.profile_name }}」的完整已保存配置，包含其他草稿修改。</p>
          <Button class="min-h-11" :disabled="previewing || !selected.length || publishing || fixing || !!savingIp || selected.some(busy)" @click="publish">
            <Loader2 v-if="publishing" class="size-4 animate-spin" /><Upload v-else class="size-4" />{{ publishing ? '发布中…' : selected.length ? `发布到 ${selected.length} 台 OxiDNS` : '选择要发布的 OxiDNS' }}
          </Button>
        </div>
      </SectionCard>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { ArrowLeft, Check, Info, Loader2, MonitorSmartphone, Plus, RefreshCw, Router, Search, Trash2, TriangleAlert, Upload, X } from '@lucide/vue'
import { agentApi, clientDeviceApi, type ClientDevice, type ClientDeviceMode, type ClientDevicesState, type ClientDiscovery, type ClientFixedIPPreview, type ClientFixedIPTarget, type ClientPresenceInfo } from '@/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import ClientSightingChips from '@/components/clients/ClientSightingChips.vue'
import AgentDeploymentStatus from '@/components/agents/AgentDeploymentStatus.vue'
import AgentOperationSteps from '@/components/agents/AgentOperationSteps.vue'
import AgentFailureDetails from '@/components/agents/AgentFailureDetails.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import AgentDNSChecks from '@/components/agents/AgentDNSChecks.vue'
import { createDeploymentId, deploymentNeedsNotice, useAgentDeployments, type AgentDeployment } from '@/composables/useAgentDeployments'
import { oxidnsMonitorPath } from '@/composables/useOxiDNSDevices'
import { choose, confirm, confirmDanger, notify } from '@/lib/feedback'
import { normalizeClientRange } from '@/lib/oxidnsClientRange'
import { uniquePublicationTargets } from '@/lib/agentRequests'
import { usePageReloadGuard } from '@/lib/pageReloadGuard'
import { getActiveProfileId } from '@/profileContext'
import { getAuthSessionGeneration, getAuthToken } from '@/authSession'
import { forgetClientDevicesSnapshot, readClientDevicesSnapshot, rememberClientDevicesSnapshot } from '@/lib/clientDevicesSnapshot'

const pageHelp = '为局域网设备命名，并按需设置 OxiDNS 的 DNS 策略。设备从 RouterOS / iKuai 主路由读取，优先显示上次结果，过期后后台更新；名称在本配置空间共用，Mihomo 连接、数据统计和 OxiDNS 查询记录都按它显示。'
const listHelp = [
  '「已添加」：在本页保存过的设备，可以起名字、设置 DNS 策略。只是方便管理，不代表走代理。',
  '「未添加」：主路由能看到、但还没保存的设备。它们照常上网，按全局的分流规则走。',
  '「已离线」：已添加、但路由器判断已经离线的设备。',
  '打开页面先显示上次读取的设备和状态；一分钟内复用，超过一分钟在后台更新。点「重新载入」立即重查。服务重启后的首次读取最长约 20 秒。',
  '改名字：立即生效，不用发布。\n改 DNS 策略：先存为草稿，在下方「发布 DNS 策略」发布到 OxiDNS 后才生效。开了策略路由时，智能分流的设备有变化，还要到上游路由重新发布。',
  '固定 IP：避免设备续租换了地址后，名字和策略对不上。它会修改来源主路由的 DHCP 静态绑定，要先预览确认再执行。',
  '名称和 DNS 策略都清空后，设备会从「已添加」中移除；已有的固定 IP 默认保留。',
].join('\n\n')
const modeHelp = [
  '默认：不单独设置，按全局的分流规则决定哪些网站走代理。如果它在某个设了策略的网段里，就跟那个网段一样。',
  '智能分流：同样按全局的分流规则走代理。以下两种情况需要选它：\n1. 所在网段设成了「直连」，但这一台想照常走代理；\n2. 上游路由开了「策略路由」时，只有选了智能分流的设备，按 IP 分流的流量（如 Telegram 的 IP 段）才会转给代理。改动后要到上游路由重新发布。',
  '直连：这台设备访问的网站都不走代理，DNS 一律返回真实地址。路由器按 IP 段直接转给代理的流量不受影响。',
  '同一台设备被多条设置覆盖时，范围越小越优先：单个 IP 优先于网段，小网段优先于大网段，与添加顺序无关。',
].join('\n\n')
const rangeHelp = [
  '支持单个 IPv4 / IPv6 和 CIDR 网段，例如 192.168.50.50、192.168.50.0/24，网段会整理为标准写法。',
  '设备需使用 OxiDNS 作为 DNS，且 OxiDNS 能识别真实客户端 IP；填写地址不会修改设备的 DNS。',
].join('\n\n')
const foundHelp = computed(() => (policyEditable.value ? '添加后默认设为智能分流，名称取设备主机名。' : '添加时只写入名称（取设备主机名），不设置 DNS 策略。')
  + '地址落在已设策略的网段内时，未添加前沿用该网段的策略。')
const publishHelp = '只发布到勾选的 OxiDNS。未勾选的设备保持现有配置；同一配置空间的 OxiDNS 共用这份草稿。'

const route = useRoute()
const router = useRouter()
const agentOf = (value: unknown) => typeof value === 'string' ? value : ''
/** 从 OxiDNS 面板进入时固定使用该设备绑定的配置空间，不切换全局空间 */
const fromAgent = ref(agentOf(route.query.agent))
/** 从上游路由或配置向导进入时的返回地址，只接受站内路径 */
const backTo = computed(() => { const value = route.query.from; return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '' })
/** 配置向导进行中只命名：DNS 策略会改动向导已发布并检查过的 OxiDNS 配置 */
const fromSetup = computed(() => !fromAgent.value && backTo.value.startsWith('/setup'))
/** 从上游路由等页面进入其他配置空间时同样不切换全局空间 */
const fromProfile = ref(agentOf(route.query.profile))
// Matches the breakpoint where .cf-mobile-records hides the table header and its help.
const phone = useMediaQuery('(width < 640px)')
const snapshotKey = () => fromAgent.value ? `agent:${fromAgent.value}` : `profile:${fromProfile.value || getActiveProfileId()}`
const initialSnapshot = readClientDevicesSnapshot(snapshotKey())
const pageToken = getAuthToken()
const pageSession = getAuthSessionGeneration()
const sameSession = () => getAuthToken() === pageToken && getAuthSessionGeneration() === pageSession
const previewing = ref(!!initialSnapshot)
const state = ref<ClientDevicesState | null>(initialSnapshot?.state || null)
// Listeners on other ports with their own chain; node-test entries are left out by the server.
const sideEntries = computed(() => state.value?.side_entries ?? [])
const policyEditable = computed(() => !!state.value?.editable && !fromSetup.value)
const rows = ref<ClientDevice[]>(initialSnapshot?.state.devices.map(row => ({ ...row })) || [])
const loading = ref(false)
const error = ref('')
const newIP = ref('')
const search = ref('')
const savingIp = ref('')
const savedOnce = ref(false)
const failures = reactive<Record<string, string>>({})
const selected = ref<string[]>(initialSnapshot?.state.agents.filter(agent => fromAgent.value ? agent.id === fromAgent.value : agent.status === 'online').map(agent => agent.id) || [])
const publishing = ref(false)
const rejected = reactive<Record<string, Partial<AgentDeployment>>>({})
const discovery = ref<ClientDiscovery | null>(initialSnapshot?.discovery ? { ...initialSnapshot.discovery, cached: true } : null)
const discovering = ref(false)
const discoveryError = ref('')
/** 未添加标签中勾选的地址；添加后移出 */
const picked = ref<string[]>([])
/** 固定 IP 必须逐次主动选择；重新载入、完成添加后清空。 */
const fixedPicked = ref<string[]>([])
const fixing = ref(false)
const fixedError = ref('')
const adding = ref(0)
/** 刚添加的设备数，用于提醒发布 */
const reminder = ref(0)
let discoveryRun = 0
let loadRun = 0
let disposed = false
let queue: Promise<unknown> = Promise.resolve()

const { deployments, busy, track, beginSubmission, endSubmission, activate } = useAgentDeployments(() => undefined)
usePageReloadGuard(() => publishing.value || fixing.value || !!savingIp.value || unsaved().length > 0)

const saved = (ip: string) => state.value?.devices.find(row => row.ip === ip)
const unsaved = () => rows.value.filter(row => {
  const previous = saved(row.ip)
  return previous ? previous.name !== row.name.trim() || previous.mode !== row.mode : !!(row.name.trim() || row.mode)
})

// 「已添加 / 未添加 / 已离线」与地址栏 ?tab= 同步。
type Tab = 'added' | 'new' | 'offline'
const savedTabs: Tab[] = ['added', 'offline']
const tabOf = (value: unknown): Tab => value === 'new' || value === 'offline' ? value : 'added'
const tab = ref<Tab>(tabOf(route.query.tab))
watch(() => route.query.tab, value => {
  if (tab.value !== tabOf(value)) release()
  tab.value = tabOf(value)
})
function changeTab(value: string) {
  release()
  tab.value = tabOf(value)
  void router.replace({ query: { ...route.query, tab: tab.value === 'added' ? undefined : tab.value } })
}

/** 路由器对每个地址的判断：已保存的行和新发现的地址 */
const seen = computed(() => {
  const map = new Map<string, ClientPresenceInfo>()
  for (const row of discovery.value?.devices || []) map.set(row.ip, row)
  for (const row of discovery.value?.discovered || []) map.set(row.ip, row)
  return map
})
const infoOf = (ip: string) => seen.value.get(ip)
const unsupported = computed(() => discovery.value?.state === 'unsupported')
const policyRouting = computed(() => !!discovery.value?.sources.some(source => source.policy_routing))
const presenceOf = (ip: string) => infoOf(ip)?.presence || (ip.includes('/') ? 'network' : 'unknown')
const listed = computed(() => new Set(rows.value.map(row => row.ip)))
const ipKey = (ip: string) => ip.split('.').reduce((total, part) => total * 256 + Number(part), 0)
/** 未添加：路由器看到、本页还没有这一行的 IPv4 地址（删除的行回到这里） */
const found = computed(() => [...(discovery.value?.discovered || []), ...(discovery.value?.devices || []).filter(row => row.discoverable)]
  .filter(item => !listed.value.has(item.ip))
  .sort((a, b) => ipKey(a.ip) - ipKey(b.ip)))
/**
 * 正在编辑的行留在原标签：路由器结果最长约 20 秒才返回，不让输入中的行跳到另一个标签；
 * 切换标签或重新载入后按最新状态归类。
 */
const held = reactive<Record<string, Tab>>({})
function release() {
  for (const ip of Object.keys(held)) delete held[ip]
}
function hold(ip: string) {
  if (!held[ip]) held[ip] = tab.value === 'offline' ? 'offline' : 'added'
}
const placeOf = (ip: string): Tab => held[ip] || (presenceOf(ip) === 'offline' ? 'offline' : 'added')
const tabRows = computed(() => rows.value.filter(row => placeOf(row.ip) === tab.value))
const matches = (term: string, ...values: (string | undefined)[]) => values.some(value => value?.toLowerCase().includes(term))
const visibleRows = computed(() => {
  const term = search.value.trim().toLowerCase()
  return term ? tabRows.value.filter(row => matches(term, row.ip, row.name, infoOf(row.ip)?.hostname, infoOf(row.ip)?.mac)) : tabRows.value
})
const visibleFound = computed(() => {
  const term = search.value.trim().toLowerCase()
  return term ? found.value.filter(item => matches(term, item.ip, item.hostname, item.mac)) : found.value
})
const tabItems = computed(() => {
  // 全部路由器读取失败时数量未知，不显示 0。
  const ready = !!discovery.value && !unsupported.value && discovery.value.state !== 'failed'
  const offline = rows.value.filter(row => placeOf(row.ip) === 'offline').length
  return [
    { value: 'added', label: '已添加', count: rows.value.length - offline },
    { value: 'new', label: '未添加', count: ready ? found.value.length : null },
    { value: 'offline', label: '已离线', count: ready ? offline : null }
  ]
})
const checkedAt = computed(() => {
  const time = discovery.value?.checked_at ? new Date(discovery.value.checked_at) : null
  if (!time || Number.isNaN(time.getTime())) return ''
  const options = { hour: '2-digit', minute: '2-digit' } as const
  return time.toDateString() === new Date().toDateString() ? time.toLocaleTimeString('zh-CN', options) : time.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', ...options })
})
/** 路由器读取失败与来源说明，逐台列出 */
const sourceProblems = computed(() => (discovery.value?.sources || []).flatMap(source => [
  ...(source.ok ? [] : [{ key: `${source.id}:error`, error: true, text: `「${source.name}」读取失败：${source.error || '未知原因'}` }]),
  ...source.notes.map((note, index) => ({ key: `${source.id}:${index}`, error: false, text: `「${source.name}」${note}` }))
]))

const PRESENCE: Record<string, { label: string; tone: 'success' | 'warning' | 'muted' }> = {
  online: { label: '在线', tone: 'success' }, recent: { label: '近期见过', tone: 'warning' }, offline: { label: '离线', tone: 'muted' },
  unknown: { label: '未知', tone: 'muted' }, network: { label: '网段', tone: 'muted' }
}
function statusOf(ip: string) {
  if (!infoOf(ip) && !ip.includes('/') && !ip.includes(':') && discovering.value) return { label: '读取中', tone: 'muted' as const }
  return PRESENCE[presenceOf(ip)]
}
function ago(seconds: number) {
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  return `${Math.floor(seconds / 86400)} 天前`
}
/** 状态下方的依据与时间 */
function hintOf(ip: string) {
  const info = infoOf(ip)
  if (!info) {
    if (ip.includes('/') || discovering.value) return ''
    if (ip.includes(':')) return '只检测 IPv4'
    if (unsupported.value) return '未接入 RouterOS / iKuai'
    return discovery.value ? '重新载入后判断' : ''
  }
  const renewed = info.last_seen === null ? '' : `${ago(info.last_seen)}续租`
  const hint = {
    arp: info.presence === 'offline' ? 'ARP 无应答' : 'ARP 可达',
    lease: info.presence === 'offline' ? (renewed ? `${renewed}，已错过续租` : '租约已过期') : renewed || 'DHCP 租约有效',
    probe: info.presence === 'offline' ? '路由器探测无应答' : '路由器探测有应答',
    terminal: '近 2 分钟有流量经过路由器',
    stale: renewed ? `未能确认，${renewed}` : '路由器有记录，未能确认',
    absent: '路由器没有看到这个地址',
    router: '路由器自己的地址',
    outside: '不在路由器扫描的网段内',
    '': info.presence !== 'unknown' ? '' : ip.includes(':') ? '只检测 IPv4' : unsupported.value ? '未接入 RouterOS / iKuai'
      : discovery.value?.state !== 'ok' ? '路由器未全部读取成功' : ''
  }[info.basis] || ''
  const router = (discovery.value?.sources.length || 0) > 1 && info.router_id ? discovery.value!.sources.find(source => source.id === info.router_id)?.name : ''
  return [router, hint].filter(Boolean).join(' · ')
}
const ipv4Number = (ip: string) => {
  const parts = ip.split('.').map(Number)
  return parts.length === 4 && parts.every(part => Number.isInteger(part) && part >= 0 && part <= 255) ? ipKey(ip) : null
}
/** 对这个地址生效的网段策略：设了策略的网段中前缀最长的一条，与 OxiDNS 和策略路由一致 */
function covering(ip: string): ClientDevice | null {
  const address = ipv4Number(ip)
  let best: ClientDevice | null = null, longest = -1
  if (address === null) return null
  for (const row of state.value?.devices || []) {
    const [base, bits] = row.ip.split('/')
    const prefix = Number(bits), network = ipv4Number(base)
    if (!row.mode || bits === undefined || network === null || prefix <= longest) continue
    const size = 2 ** (32 - prefix)
    if (Math.floor(address / size) === Math.floor(network / size)) { best = row; longest = prefix }
  }
  return best
}
const modeLabel = (mode: ClientDeviceMode) => ({ smart: '智能分流', direct: '直连', '': '默认' })[mode]
/** RouterOS 策略路由的来源：智能分流的 IPv4 设备或网段，代理网关本机除外（与上游路由页一致） */
const routesSource = (row: ClientDevice) => row.mode === 'smart' && !!saved(row.ip) && !row.ip.includes(':')
  && !!discovery.value?.sources.some(source => source.policy_routing && ![source.gateway, `${source.gateway}/32`].includes(row.ip))
const cleanName = (value: string) => value.replace(/[\u0000-\u001f\u007f]/g, '').trim().slice(0, 80)
/** 没有主机名时只能靠 DNS 策略保存这一行 */
const canAdd = (item: ClientPresenceInfo) => !previewing.value && !!state.value && (policyEditable.value || !!cleanName(item.hostname))
const pickable = computed(() => visibleFound.value.filter(canAdd).map(item => item.ip))
const allPicked = computed(() => pickable.value.length > 0 && pickable.value.every(ip => picked.value.includes(ip)))
/** 表头复选框只反映当前搜索结果的勾选情况 */
const somePicked = computed(() => pickable.value.some(ip => picked.value.includes(ip)))
const pickedFound = computed(() => found.value.filter(item => picked.value.includes(item.ip)))
const canFix = (item?: ClientPresenceInfo) => !previewing.value && !fromSetup.value && !!item?.mac && !!item?.router_id && item.fixed_ip?.eligible === true && item.fixed_ip.status !== 'static'
function fixedReason(item?: ClientPresenceInfo) {
  if (!item?.mac) return '路由器未提供 MAC，无法固定 IP。'
  return item.fixed_ip?.reason || (item.router_id ? '尚未确认此路由器的固定 IP 能力，请重新载入。' : '无法确定设备的来源路由器。')
}
const pickedFixable = computed(() => pickedFound.value.filter(item => canAdd(item) && canFix(item)))
const allFixedPicked = computed(() => pickedFixable.value.length > 0 && pickedFixable.value.every(item => fixedPicked.value.includes(item.ip)))
const someFixedPicked = computed(() => pickedFixable.value.some(item => fixedPicked.value.includes(item.ip)))
function pickFixed(ip: string, value: boolean) {
  fixedPicked.value = value ? [...new Set([...fixedPicked.value, ip])] : fixedPicked.value.filter(item => item !== ip)
}
function pickFixedBatch(value: boolean) {
  const eligible = new Set(pickedFixable.value.map(item => item.ip))
  fixedPicked.value = value ? [...new Set([...fixedPicked.value, ...eligible])] : fixedPicked.value.filter(ip => !eligible.has(ip))
}
watch(found, items => {
  const present = new Set(items.map(item => item.ip))
  if (picked.value.some(ip => !present.has(ip))) picked.value = picked.value.filter(ip => present.has(ip))
  fixedPicked.value = fixedPicked.value.filter(ip => items.some(item => item.ip === ip && canFix(item)))
})
function pick(ip: string, value: boolean) {
  picked.value = value ? [...new Set([...picked.value, ip])] : picked.value.filter(item => item !== ip)
}
/** 有搜索词时只作用于匹配项，保留其他已选项 */
function pickAll(value: boolean) {
  const visible = new Set(pickable.value)
  picked.value = value ? [...new Set([...picked.value, ...pickable.value])] : picked.value.filter(ip => !visible.has(ip))
}
const needsNotice = (id: string) => !!deployments[id] && deploymentNeedsNotice(deployments[id])
const messageOf = (failure: any) => failure?.response?.data?.message || failure?.message || '操作失败，请重试。'

function adopt(data: ClientDevicesState) {
  const latest = new Map(data.devices.map(row => [row.ip, row]))
  const before = new Map((state.value?.devices || []).map(row => [row.ip, row]))
  state.value = data
  // Rows keep their place. Edits still being typed stay, and rows only added
  // here stay until they get a name or a policy; rows removed elsewhere leave.
  const local = rows.value.flatMap(row => {
    const next = latest.get(row.ip), previous = before.get(row.ip)
    if (!next) return previous ? [] : [row]
    if (!previous) return [{ ...next }]
    // Only the fields edited here stay local; the rest follows the server, so a
    // later save never sends back a value changed elsewhere meanwhile.
    return [{ ...next, name: row.name.trim() !== previous.name ? row.name : next.name, mode: row.mode !== previous.mode ? row.mode : next.mode }]
  })
  const listed = new Set(local.map(row => row.ip))
  rows.value = [...local, ...data.devices.filter(row => !listed.has(row.ip)).map(row => ({ ...row }))]
  rememberSnapshot()
}

function rememberSnapshot() {
  if (sameSession() && state.value && !previewing.value) rememberClientDevicesSnapshot(snapshotKey(), state.value, discovery.value)
}

type DiscoveryReply = { data: ClientDiscovery } | { failure: unknown }

// Observe rejections immediately even when the device-record request is slower.
async function requestDiscovery(profileId: string, refresh: boolean): Promise<DiscoveryReply> {
  try {
    const { data } = await (refresh ? clientDeviceApi.discover(profileId, { refresh: true }) : clientDeviceApi.discover(profileId))
    return { data }
  } catch (failure) { return { failure } }
}

/** keep：保留本页未保存的修改，只合并最新数据（单行冲突后使用） */
async function load(keep = false, refresh = false) {
  const run = ++loadRun
  ++discoveryRun
  discovering.value = false
  const key = snapshotKey()
  const profileId = fromProfile.value || getActiveProfileId()
  loading.value = true
  error.value = ''
  const records = clientDeviceApi.get({ agentId: fromAgent.value || undefined, profileId: fromProfile.value || undefined })
  // The navigation already knows a profile; Agent-bound profiles must first be resolved by the server.
  const pending = !fromAgent.value && !keep ? requestDiscovery(profileId, refresh) : undefined
  try {
    const { data } = await records
    if (disposed || !sameSession() || run !== loadRun || key !== snapshotKey()) return
    const merge = keep && data.profile_id === state.value?.profile_id
    previewing.value = false
    if (merge) {
      adopt(data)
      return
    }
    for (const ip of Object.keys(failures)) delete failures[ip]
    release()
    rows.value = []
    if (data.profile_id !== state.value?.profile_id) discovery.value = null
    state.value = null
    picked.value = []
    fixedPicked.value = []
    fixedError.value = ''
    reminder.value = 0
    adopt(data)
    const online = data.agents.filter(agent => agent.status === 'online').map(agent => agent.id)
    selected.value = fromAgent.value ? data.agents.filter(agent => agent.id === fromAgent.value).map(agent => agent.id) : online
    for (const agent of data.agents) if (agent.deployment) track(agent.id, agent.deployment, false)
    void discover(data.profile_id, refresh, data.profile_id === profileId ? pending : undefined)
  } catch (failure: any) {
    if (disposed || !sameSession() || run !== loadRun || key !== snapshotKey()) return
    error.value = messageOf(failure)
    forgetClientDevicesSnapshot(key)
    if ([401, 404].includes(failure?.response?.status)) {
      state.value = null
      rows.value = []
      discovery.value = null
    }
  } finally {
    if (run === loadRun) loading.value = false
  }
}

/** 先显示快照，过期后后台重查；只采用当前配置空间最后一次请求的结果。 */
async function discover(profileId: string, refresh = false, pending = requestDiscovery(profileId, refresh)) {
  const run = ++discoveryRun
  discovering.value = true
  discoveryError.value = ''
  try {
    const current = () => !disposed && sameSession() && run === discoveryRun && state.value?.profile_id === profileId
    const apply = (reply: DiscoveryReply) => {
      if ('failure' in reply) throw reply.failure
      discovery.value = reply.data
      discoveryError.value = reply.data.refresh_error
      rememberSnapshot()
      return reply.data
    }
    const reply = await pending
    if (!current()) return
    const data = apply(reply)
    if (data.stale && !refresh) {
      const latest = await requestDiscovery(profileId, true)
      if (current()) apply(latest)
    }
  } catch (failure) {
    if (!disposed && run === discoveryRun) discoveryError.value = messageOf(failure)
  } finally {
    if (run === discoveryRun) discovering.value = false
  }
}

function rediscover() {
  if (state.value) void discover(state.value.profile_id, true)
}

async function reload() {
  await queue
  if (!unsaved().length || await confirm('重新载入会丢弃尚未保存的修改。', { title: '重新载入客户端设备', confirmText: '重新载入' })) await load(false, true)
}

function add(value: string) {
  if (previewing.value) return
  let ip = value.trim()
  if (!ip) return
  try {
    ip = normalizeClientRange(ip)
  } catch {
    notify.error('请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。')
    return
  }
  newIP.value = ''
  search.value = ''
  if (!rows.value.some(row => row.ip === ip)) rows.value.unshift({ ip, name: '', mode: '' })
  else notify.info('此设备已在列表中。')
  // 离线的地址出现在「已离线」，切到它所在的标签再聚焦名称。
  const target: Tab = presenceOf(ip) === 'offline' ? 'offline' : 'added'
  if (tab.value !== target) changeTab(target)
  // A tab that was just opened renders its rows a moment later.
  const focus = () => [...document.querySelectorAll<HTMLElement>('[data-client-ip]')]
    .find(row => row.dataset.clientIp === ip)?.querySelector('input')?.focus()
  void nextTick(() => setTimeout(focus))
}

/** 添加路由器发现的设备：名称取主机名，默认智能分流，逐行排队保存 */
async function addFound(items: ClientPresenceInfo[]) {
  if (!state.value || adding.value || fixing.value) return
  const profileId = state.value.profile_id
  const mode: ClientDeviceMode = policyEditable.value ? 'smart' : ''
  const added = items.filter(item => canAdd(item) && !rows.value.some(row => row.ip === item.ip)).map(item => item.ip)
  if (!added.length) return
  const fixedTargets = items.filter(item => added.includes(item.ip) && fixedPicked.value.includes(item.ip) && canFix(item))
    .map(item => fixedTarget(item, cleanName(item.hostname)))
  if (fixedTargets.length > 64) {
    fixedError.value = '每次最多固定 64 台设备的 IP，请减少勾选后分批添加。'
    return
  }
  let preview: ClientFixedIPPreview | null = null
  const savedIPs = new Set<string>()
  adding.value = added.length
  fixedError.value = ''
  try {
    if (fixedTargets.length) {
      fixing.value = true
      preview = await previewFixed(profileId, fixedTargets)
      if (!preview || !fixedContext(profileId)) return
    }
    for (const ip of [...added].reverse()) rows.value.unshift({ ip, name: cleanName(infoOf(ip)?.hostname || ''), mode })
    picked.value = picked.value.filter(ip => !added.includes(ip))
    for (const ip of added) {
      if (!recordContext(profileId)) return
      if (await save(ip)) savedIPs.add(ip)
      adding.value = Math.max(adding.value - 1, 0)
    }
    if (preview && fixedContext(profileId)) {
      if (added.some(ip => !savedIPs.has(ip))) {
        fixedError.value = '部分设备记录未保存，本次未执行固定 IP。处理保存失败后，可在已添加设备行重新操作。'
      } else {
        try {
          if (!await applyFixed(profileId, preview)) return
          notify.success(`已固定 ${fixedTargets.length} 台设备的 IP`)
        } catch (failure) {
          fixedError.value = `设备记录已保存，固定 IP 失败：${messageOf(failure)}`
          notify.error('固定 IP 失败', fixedError.value)
        }
      }
      await discover(profileId, true)
    }
  } catch (failure) {
    fixedError.value = `无法预览固定 IP：${messageOf(failure)}`
    notify.error('无法固定 IP', fixedError.value)
    return
  } finally {
    adding.value = 0
    fixing.value = false
  }
  if (!recordContext(profileId)) return
  const done = savedIPs.size
  const failed = added.length - done
  if (failed) notify.error(`${failed} 台设备未能添加`, '保存失败的行标「未保存」并可在该行重试；被其他页面修改的设备已按最新数据显示。')
  if (!done) return
  if (mode) reminder.value += done
  notify.success(`已添加 ${done} 台设备`, mode ? 'DNS 策略需发布到 OxiDNS 后生效。' : undefined)
}

const recordContext = (profileId: string) => !disposed && state.value?.profile_id === profileId
const fixedContext = (profileId: string) => recordContext(profileId) && !fromSetup.value
function fixedTarget(item: ClientPresenceInfo, name: string): ClientFixedIPTarget {
  return { ip: item.ip, mac: item.mac, router_id: item.router_id, name }
}

/** 路由器状态由服务端重新读取；确认框始终展示此次预览的真实目标与动作。 */
async function previewFixed(profileId: string, targets: ClientFixedIPTarget[], operation: 'add' | 'delete' = 'add') {
  const { data } = await clientDeviceApi.previewFixedIP(profileId, targets, operation)
  if (!fixedContext(profileId)) return null
  if (!data.token || !data.actions.length) throw new Error('路由器没有返回可执行的变更，请重新载入。')
  const lines = data.actions.map(action => `「${action.router_name}」${action.description}`)
  const accepted = await confirm(`${lines.join('\n\n')}\n\n${operation === 'delete' ? '将删除以上由 ConfigFlow 创建的 DHCP 静态绑定，设备之后可能获得其他 IP。' : '将修改以上来源主路由的 DHCP 设置，让这些 MAC 保持当前 IP。设备继续沿用网段的 DNS。'}`, {
    title: operation === 'delete' ? '预览删除固定 IP' : '预览固定 IP',
    confirmText: operation === 'delete' ? '确认删除固定 IP' : '确认固定 IP', danger: operation === 'delete'
  })
  return accepted && fixedContext(profileId) ? data : null
}

async function applyFixed(profileId: string, preview: ClientFixedIPPreview) {
  if (!fixedContext(profileId)) return false
  const { data } = await clientDeviceApi.applyFixedIP(profileId, preview.token)
  if (!fixedContext(profileId)) return false
  adopt(data)
  return true
}

async function fixExisting(row: ClientDevice) {
  const info = infoOf(row.ip)
  if (!state.value || fixing.value || !info || !canFix(info)) return
  const profileId = state.value.profile_id
  fixing.value = true
  fixedError.value = ''
  try {
    await queue
    const preview = await previewFixed(profileId, [fixedTarget(info, row.name.trim())])
    if (!preview) return
    if (await applyFixed(profileId, preview)) notify.success(`已固定 ${row.ip}`)
  } catch (failure) {
    fixedError.value = `固定 IP 失败：${messageOf(failure)}`
    notify.error('固定 IP 失败', fixedError.value)
  } finally {
    if (fixedContext(profileId)) await discover(profileId, true)
    fixing.value = false
  }
}

const ownsFixedIP = (ip: string) => !!saved(ip)?.fixed_ip || infoOf(ip)?.fixed_ip?.owned === true

/** 清空字段与点击移除共用此流程，默认只删记录；非 ConfigFlow 绑定永远没有删除入口。 */
async function removeFixedIP(row: ClientDevice, previous: ClientDevice): Promise<boolean> {
  if (fromSetup.value || !ownsFixedIP(row.ip) || !state.value) return true
  const profileId = state.value.profile_id
  const choice = await choose(`将清除「${previous.name || row.ip}」的名称和 DNS 策略。此设备有 ConfigFlow 创建的固定 IP，默认保留；也可预览后一起删除。`, {
    title: '移除客户端设备', confirmText: '仅移除设备，保留固定 IP', altText: '一并删除固定 IP', cancelText: '取消'
  })
  if (choice === 'cancel' || !fixedContext(profileId)) return false
  if (choice !== 'alt') return true
  const info = infoOf(row.ip)
  const binding = previous.fixed_ip || info?.fixed_ip?.binding
  const target = { ip: row.ip, name: previous.name, mac: binding?.mac || info?.mac || '', router_id: binding?.router_id || info?.router_id || '' }
  fixing.value = true
  fixedError.value = ''
  try {
    const preview = await previewFixed(profileId, [target], 'delete')
    if (!preview) return false
    return await applyFixed(profileId, preview)
  } catch (failure) {
    fixedError.value = `固定 IP 未能删除，设备记录已保留：${messageOf(failure)}`
    notify.error('无法删除固定 IP', fixedError.value)
    return false
  } finally {
    if (fixedContext(profileId)) await discover(profileId, true)
    fixing.value = false
  }
}

function toPublish() {
  document.getElementById('client-device-publish')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function setMode(row: ClientDevice, value: string) {
  row.mode = (value === 'default' ? '' : value) as ClientDeviceMode
  void save(row.ip)
}

async function remove(row: ClientDevice) {
  if (previewing.value) return
  const previous = saved(row.ip)
  if (previous && (!ownsFixedIP(row.ip) || fromSetup.value) && !await confirmDanger(`将清除「${previous.name || row.ip}」的名称${previous.mode ? '和 DNS 策略' : ''}。${previous.mode ? 'DNS 策略的修改需要发布后生效。' : ''}`,
    { title: '移除客户端设备', confirmText: '移除' })) return
  if (!previous) {
    delete failures[row.ip]
    rows.value = rows.value.filter(item => item.ip !== row.ip)
    return
  }
  row.name = ''
  row.mode = ''
  await save(row.ip)
}

/** 逐行保存，排队执行，避免同一行的两次修改乱序 */
function save(ip: string): Promise<unknown> {
  if (previewing.value) return Promise.resolve(false)
  const profileId = state.value?.profile_id
  queue = queue.then(() => profileId ? saveNow(ip, profileId) : false)
  return queue
}

async function saveNow(ip: string, profileId: string) {
  const row = rows.value.find(item => item.ip === ip)
  if (!row || !recordContext(profileId)) return false
  const previous = saved(ip) || { ip, name: '', mode: '' as ClientDeviceMode }
  const device = { ip, name: row.name.trim(), mode: row.mode }
  delete failures[ip]
  if (device.name === previous.name && device.mode === previous.mode) return true
  if (!device.name && !device.mode && !await removeFixedIP(row, previous)) {
    Object.assign(row, previous)
    return false
  }
  if (!recordContext(profileId)) return false
  savingIp.value = ip
  try {
    const { data } = await clientDeviceApi.save(profileId, device, { name: previous.name, mode: previous.mode })
    if (!recordContext(profileId)) return false
    adopt(data)
    savedOnce.value = true
    return true
  } catch (failure: any) {
    if (!recordContext(profileId)) return false
    if (failure?.response?.status === 409) {
      notify.error('设备已在其他页面修改', messageOf(failure))
      // Drop only this row's edit; other rows keep what is still being typed.
      Object.assign(row, saved(ip) || { name: '', mode: '' })
      if (!saved(ip)) rows.value = rows.value.filter(item => item.ip !== ip)
      await load(true)
    } else {
      failures[ip] = messageOf(failure)
    }
  } finally {
    if (savingIp.value === ip) savingIp.value = ''
  }
}

function toggle(id: string, value: boolean) {
  selected.value = value ? [...new Set([...selected.value, id])] : selected.value.filter(item => item !== id)
}

async function publish() {
  if (previewing.value) return
  await queue
  const failing = rows.value.some(row => failures[row.ip])
  if (!state.value || publishing.value || fixing.value || failing) {
    if (failing) notify.error('有设备未保存', '先处理保存失败的设备，再发布。')
    return
  }
  const targets = state.value.agents.filter(agent => selected.value.includes(agent.id))
  const expected = { expected_profile_id: state.value.profile_id, expected_oxidns_revision: state.value.oxidns_revision }
  if (!targets.length || !await confirm(`将发布「${state.value.profile_name}」的完整已保存配置到 ${targets.map(agent => `「${agent.name}」`).join('、')}，并按现有发布流程校验、备份和应用配置。`,
    { title: '发布 DNS 策略', confirmText: '发布' })) return
  publishing.value = true
  try {
    for (const agent of uniquePublicationTargets(targets)) {
      if (disposed) return
      delete rejected[agent.id]
      const deploymentId = createDeploymentId()
      if (!beginSubmission(agent.id, deploymentId)) continue
      try {
        const { data } = await agentApi.pushConfig(agent.id, deploymentId, expected)
        endSubmission(agent.id, deploymentId, data)
      } catch (failure: any) {
        if (failure.response?.data?.deployment_id) endSubmission(agent.id, deploymentId, failure.response.data)
        else if (!failure.response || failure.response.status >= 500) endSubmission(agent.id, deploymentId, 'unknown')
        else if (!endSubmission(agent.id, deploymentId, 'rejected')) rejected[agent.id] = { ...failure.response.data, error: messageOf(failure) }
        // A changed draft rejects every device; a busy or rolled-back device fails alone.
        if (failure.response?.data?.code === 'draft_changed') {
          notify.error('发布已停止', messageOf(failure))
          return
        }
      }
    }
  } finally {
    publishing.value = false
  }
}

onBeforeRouteLeave(async () => {
  if (fixing.value || adding.value) return false
  await queue
  return !unsaved().length || await confirm('有设备尚未保存，离开会丢失这些修改。', { title: '离开客户端设备', confirmText: '放弃修改' })
})
onBeforeRouteUpdate(async to => {
  const next = agentOf(to.query.agent), profile = agentOf(to.query.profile)
  if (next === fromAgent.value && profile === fromProfile.value) return
  if (fixing.value || adding.value) return false
  await queue
  if (unsaved().length && !await confirm('有设备尚未保存，切换后会丢失这些修改。', { title: '离开客户端设备', confirmText: '放弃修改' })) return false
  fromAgent.value = next
  fromProfile.value = profile
  const snapshot = readClientDevicesSnapshot(snapshotKey())
  previewing.value = !!snapshot
  state.value = snapshot?.state || null
  rows.value = snapshot?.state.devices.map(row => ({ ...row })) || []
  discovery.value = snapshot?.discovery ? { ...snapshot.discovery, cached: true } : null
  void load()
})
onMounted(() => load())
onUnmounted(() => { disposed = true })
</script>
