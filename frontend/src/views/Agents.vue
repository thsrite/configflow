<template>
  <div>

    <PageHeader
      title="Agent"
      class="agents-page-header"
    >
      <template #actions>
        <Button variant="outline" class="border-border/60 bg-background/40 max-sm:size-11 max-sm:shrink-0 max-sm:gap-0" aria-label="刷新 Agent" title="刷新 Agent" @click="loadAgents">
          <RefreshCw class="size-4" />
          <span class="max-sm:sr-only">刷新</span>
        </Button>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="handleGenerateScript">
          <FileText class="size-4" />
          <span><span class="max-sm:sr-only">生成</span>安装脚本</span>
        </Button>
        <Button class="shadow-glow" :disabled="pushingAll || !onlineCount" @click="pushAll">
          <Loader2 v-if="pushingAll" class="size-4 animate-spin" />
          <Send v-else class="size-4" />
          推送到全部
        </Button>
      </template>
    </PageHeader>

    <SectionCard v-if="agents.length === 0" :padded="false">
      <EmptyState
        :icon="Server"
        title="暂无 Agent"
        description="生成安装脚本并在目标机器上执行，Agent 注册后会出现在这里。"
      >
        <Button @click="handleGenerateScript">
          <FileText class="size-4" />
          生成安装脚本
        </Button>
      </EmptyState>
    </SectionCard>

    <template v-else>
      <div class="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3.5 max-md:grid-cols-1">
        <div
          v-for="(agent, index) in agents"
          :key="agent.id"
          :data-notification-agent="agent.id"
          class="cf-list-in"
          :style="listItemStyle(index)"
          :class="cn(
            'relative flex flex-col overflow-hidden rounded-[18px] border border-border bg-card/90 p-[18px] shadow-surface max-sm:p-3',
            agent.status !== 'online' && !deployments[agent.id] && !upgrades[agent.id] && !agent.dns_follow && agent.oxidns_ha?.enabled !== true && !ruleUpdateRefs[agent.id]?.hasStatus && 'opacity-80'
          )"
        >
          <header class="flex items-center gap-2.5">
            <span
              :class="cn('size-[9px] shrink-0 rounded-full', agent.status === 'online' ? 'agent-pulse bg-success-accent' : 'bg-muted-foreground/60')"
              role="img"
              :aria-label="agent.status === 'online' ? '在线' : '离线'"
              :title="agent.status === 'online' ? '在线' : '离线'"
            />
            <p class="font-display m-0 min-w-0 flex-1 truncate text-[19px] font-medium" :title="agent.name">{{ agent.name }}</p>
            <Badge variant="outline" :class="cn('chip font-mono', ['mosdns', 'oxidns'].includes(agent.service_type) && 'chip-sky')">{{ serviceTypeLabels[agent.service_type] || agent.service_type }}</Badge>
            <span class="font-mono text-[11px] text-muted-foreground">{{ agent.version || '' }}</span>
          </header>
          <p class="mt-1 mb-0 truncate font-mono text-[11.5px] text-muted-foreground">
            {{ agent.host }}:{{ agent.port }}
            · {{ agent.deployment_method === 'shell' ? 'shell' : agent.deployment_method === 'docker' ? 'docker' : agent.deployment_method || '—' }}
            · 心跳 {{ formatTime(agent.last_heartbeat) }}
          </p>

          <div class="mt-3 flex items-center gap-2" data-agent-monitor-actions>
            <Button
              variant="outline"
              size="sm"
              class="min-w-0 flex-1 border-border-strong bg-secondary/60 text-xs text-foreground only:flex-none"
              :aria-label="`查看 ${agent.name} 的系统监控`"
              @click="showMetricsDetail(agent)"
            >
              <ChartLine class="size-3.5" />
              系统监控
            </Button>
            <Button
              v-if="agent.service_type === 'oxidns'"
              variant="outline"
              size="sm"
              class="min-w-0 flex-1 border-border-strong bg-secondary/60 text-xs text-foreground only:flex-none"
              :aria-label="`查看 ${agent.name} 的 OxiDNS 面板`"
              @click="router.push('/agents/' + encodeURIComponent(agent.id) + '/oxidns')"
            >
              <ChartLine class="size-3.5" />
              OxiDNS 面板
            </Button>
            <Button
              v-if="agent.service_type === 'mihomo'"
              variant="outline"
              size="sm"
              class="min-w-0 flex-1 border-border-strong bg-secondary/60 text-xs text-foreground only:flex-none"
              :aria-label="`查看 ${agent.name} 的 Mihomo 面板`"
              @click="router.push({ name: 'MihomoPanel', query: { agent: agent.id } })"
            >
              <ChartLine class="size-3.5" />
              Mihomo 面板
            </Button>
          </div>

          <!-- 资源占用使用百分比；网络速率单独展示趋势。 -->
          <div class="my-3 grid grid-cols-[44px_minmax(0,1fr)_max-content] gap-2.5 max-sm:gap-2">
            <div
              v-for="metric in metricsOf(agent)"
              :key="metric.label"
              class="col-span-3 grid grid-cols-subgrid items-center gap-2.5 text-[11.5px] text-muted-foreground"
              :title="metric.detail || undefined"
            >
              {{ metric.label }}
              <i class="block h-1.5 overflow-hidden rounded-full bg-accent" aria-hidden="true">
                <span
                  class="block h-full rounded-full transition-[width] duration-1000 ease-(--ease-flow)"
                  :style="{ width: `${agent.system_metrics ? Math.min(100, metric.percent) : 0}%`, minWidth: metric.percent > 0 ? '2px' : undefined, background: metric.color }"
                />
              </i>
              <b class="whitespace-nowrap text-right font-mono font-medium text-foreground/80">{{ agent.system_metrics ? metric.text : '—' }}</b>
            </div>
          </div>
          <AgentTraffic :agent="agent" :now="trafficNow" />

          <div class="mb-3.5 flex items-center gap-2 text-[11.5px] text-muted-foreground">
            配置空间
            <Select
              :model-value="agent.profile_id || defaultProfileId"
              :disabled="bindingAgentId === agent.id || settingsLoadingAgentId === agent.id || profileBindingBlocked(agent.id)"
              @update:model-value="value => handleAgentProfileChange(agent, String(value))"
            >
              <SelectTrigger class="agent-profile-select h-7 min-w-0 flex-1 text-[12px] font-medium" :aria-label="`${agent.name} 绑定的配置空间`">
                <SelectValue />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="profile in profiles" :key="profile.id" :value="profile.id">
                  {{ profile.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="mb-3 min-w-0 divide-y divide-border/70 border-t border-border/70">
            <AgentStatusRow v-if="submissionFailures[agent.id]" label="推送请求" status="失败" tone="danger">
              <template #notice>
                <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="submissionFailures[agent.id]" />
                <AgentDNSChecks :checks="submissionFailures[agent.id].dns_checks" :checked-at="submissionFailures[agent.id].dns_checked_at" compact />
              </template>
            </AgentStatusRow>
            <!-- 成功记录收起；正在执行的步骤、失败原因与恢复操作始终可见。 -->
            <AgentDeploymentStatus v-if="deployments[agent.id]" :task="deployments[agent.id]" compact>
              <template v-if="deploymentNeedsNotice(deployments[agent.id])" #notice>
                <AgentOperationSteps :task="deployments[agent.id]" :busy="deploymentBusy(agent.id)" :show-current="false" :show-steps="false" />
                <p v-if="deployments[agent.id].not_recorded && !deployments[agent.id].manual_rollback" class="m-0 text-warning-accent">后端暂未记录此发布，正在继续查询。可使用原发布编号重新提交。</p>
                <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="deployments[agent.id]" />
                <Button v-if="deployments[agent.id].status === 'ready'" variant="outline" size="sm" class="min-h-11" @click="activateDeployment(agent.id)">应用并启动</Button>
                <Button v-if="deployments[agent.id].status === 'rollback_failed'" variant="outline" size="sm" class="min-h-11" @click="refreshDeployment(agent.id)">查询恢复状态</Button>
                <Button v-if="!deployments[agent.id].manual_rollback && (deployments[agent.id].not_recorded || deployments[agent.id].retrying)" variant="outline" size="sm" class="min-h-11" :disabled="deployments[agent.id].retrying" @click="retryDeployment(agent.id)">{{ deployments[agent.id].retrying ? '正在重新提交…' : '重试同一次发布' }}</Button>
              </template>
              <template #details>
                <AgentOperationSteps :task="deployments[agent.id]" :busy="deploymentBusy(agent.id)" :show-current="false" :show-message="!deploymentNeedsNotice(deployments[agent.id])" embedded />
              </template>
            </AgentDeploymentStatus>
            <AgentDNSChecks v-if="deployments[agent.id]" :key="deployments[agent.id].deployment_id" :checks="deployments[agent.id].dns_checks" :checked-at="deployments[agent.id].dns_checked_at" compact />

            <AgentDNSFollow
              :ref="instance => setDNSFollowRef(agent.id, instance)"
              v-if="agent.service_type === 'oxidns'"
              :agent="agent"
              :source-name="agents.find(item => item.id === agent.dns_follow?.source_agent_id)?.name"
              :blocked="deploymentBusy(agent.id) || upgradeBusy(agent.id) || pushingIds.has(agent.id)"
              @changed="loadAgents"
            />

            <AgentDNSHA
              v-if="agent.service_type === 'oxidns'"
              :agent="agent"
              :agents="agents"
              :blocked="deploymentBusy(agent.id) || upgradeBusy(agent.id) || pushingIds.has(agent.id)"
              @changed="loadAgents"
              @install-guide="openOxiDNSHAGuide(agent)"
            />

            <OxiDNSRuleUpdate v-if="agent.service_type === 'oxidns'" :ref="instance => setRuleUpdateRef(agent.id, instance)"
              :agent-id="agent.id" presentation="card" :disabled="ruleUpdateBlocked(agent)" @updated="loadAgents" />

            <!-- 被路由器用作代理网关的设备：IPv4 转发状态与一键开启。 -->
            <AgentGatewayForwarding
              v-if="forwardingEntries[agent.id]"
              :entry="forwardingEntries[agent.id]"
              :busy="forwardingBusy === agent.id"
              :disabled="Boolean(forwardingBusy) && forwardingBusy !== agent.id"
              @persist="persistForwarding(agent.id)"
            />

            <AgentUpgradeStatus v-if="upgrades[agent.id]" :task="upgrades[agent.id]" :busy="upgradeBusy(agent.id)">
              <template #actions>
                <Button v-if="!upgrades[agent.id].update_id && upgrades[agent.id].status === 'unknown'" variant="outline" size="sm" class="min-h-11" :disabled="upgradeQuerying.has(agent.id)" @click="retryUnconfirmedUpgrade(agent.id)">重新确认并提交更新</Button>
                <Button
                  v-if="upgradeBusy(agent.id)"
                  variant="outline"
                  size="sm"
                  class="min-h-11"
                  :disabled="upgradeQuerying.has(agent.id)"
                  @click="queryUpgrade(agent.id)"
                >
                  <Loader2 v-if="upgradeQuerying.has(agent.id)" class="size-3.5 animate-spin" />
                  {{ upgradeQuerying.has(agent.id) ? '正在查询…' : '查询更新结果' }}
                </Button>
              </template>
            </AgentUpgradeStatus>
          </div>

          <div v-if="agent.has_update" class="mb-3 flex items-center gap-2 rounded-xl border border-primary-accent/30 bg-primary-soft/40 px-3 py-2 text-[12px]">
            <span class="min-w-0 flex-1 text-muted-foreground">
              {{ agent.deployment_method === 'docker'
                ? '有新版 Agent，按步骤拉取镜像并重建容器'
                : agent.upgrade_available ? '有新版 Agent 可在线更新' : '更新文件或安装方式尚未就绪，请先确认 ConfigFlow 主服务已更新' }}
            </span>
            <Button
              variant="outline"
              size="sm"
              class="h-7 shrink-0"
              :disabled="agent.deployment_method !== 'docker' && (!agent.upgrade_available || upgradeBusy(agent.id) || deploymentBusy(agent.id))"
              @click="updateAgent(agent)"
            >
              <Download class="size-3.5" />
              更新
            </Button>
          </div>

          <AgentRuleSync v-if="['mihomo', 'oxidns'].includes(agent.service_type)" :agent-id="agent.id" :high-availability="agent.oxidns_ha?.enabled === true">
            <template v-if="agent.service_type === 'oxidns'" #actions>
              <Button type="button" variant="outline" size="sm" class="shrink-0"
                :disabled="ruleUpdateBlocked(agent) || !ruleUpdateRefs[agent.id] || ruleUpdateRefs[agent.id]?.busy"
                @click="ruleUpdateRefs[agent.id]?.act()">
                <Loader2 v-if="ruleUpdateRefs[agent.id]?.busy" class="size-3.5 animate-spin" />
                <RefreshCw v-else class="size-3.5" />
                {{ ruleUpdateRefs[agent.id]?.actionLabel || '更新规则' }}
              </Button>
            </template>
          </AgentRuleSync>

          <footer class="mt-auto flex items-center gap-2 border-t border-border pt-3.5 max-sm:pt-2">
            <span v-if="syncOf(agent).diffable" class="mr-auto flex min-w-0 flex-1">
              <!-- 服务端给出比较结论时可点开「待推送的变更」窗口；手机触区 44px。 -->
              <button
                type="button"
                :class="cn('-mx-1 inline-flex min-w-0 max-w-full cursor-pointer items-center gap-0.5 rounded-sm px-1 text-[12px] transition-colors hover:bg-accent max-md:min-h-11', syncTone(agent))"
                :title="syncOf(agent).title"
                :aria-label="`${syncOf(agent).label}，查看「${agent.name}」这次推送会改动的内容`"
                data-sync-diff
                @click="pushDiffAgentId = agent.id"
              >
                <span class="truncate">{{ syncOf(agent).label }}</span>
                <ChevronRight class="size-3.5 shrink-0 opacity-70" aria-hidden="true" />
              </button>
            </span>
            <span
              v-else
              :class="cn('mr-auto min-w-0 flex-1 truncate text-[12px]', syncTone(agent))"
              :title="syncOf(agent).title"
            >
              {{ syncOf(agent).label }}
            </span>
            <Button
              v-if="supportsAgentSettings(agent.service_type)"
              variant="outline"
              size="sm"
              :aria-label="`设置 ${agent.name} 的配置`"
              :title="`编辑「${profileStore.profileName(agent.profile_id || defaultProfileId)}」中的 ${serviceTypeLabels[agent.service_type]} 配置`"
              :disabled="Boolean(settingsLoadingAgentId) || bindingAgentId === agent.id"
              @click="openAgentSettings(agent)"
            >
              <Loader2 v-if="settingsLoadingAgentId === agent.id" class="size-3.5 animate-spin" />
              <Settings v-else class="size-3.5" />
              设置
            </Button>
            <Button
              v-if="agent.status === 'online'"
              variant="outline"
              size="sm"
              :disabled="pushBlocked(agent)"
              @click="pushConfig(agent)"
            >
              <Loader2 v-if="pushingIds.has(agent.id) || deploymentBusy(agent.id)" class="size-3.5 animate-spin" />
              <Send v-else class="size-3.5" />
              推送
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="ghost" size="icon-sm" :aria-label="`${agent.name} 的更多操作`">
                  <MoreHorizontal class="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" class="glass-strong min-w-[160px]">
                <DropdownMenuItem v-if="['mihomo', 'mosdns', 'oxidns'].includes(agent.service_type)" @select="deploymentHistoryAgentId = agent.id">
                  <History class="size-4" />
                  发布记录与回滚
                </DropdownMenuItem>
                <DropdownMenuItem @select="viewLogs(agent)">
                  <ScrollText class="size-4" />
                  日志
                </DropdownMenuItem>
                <DropdownMenuItem v-if="agent.system_metrics" @select="showMetricsDetail(agent)">
                  <ChartLine class="size-4" />
                  系统监控
                </DropdownMenuItem>
                <DropdownMenuItem
                  :disabled="upgradeBusy(agent.id) || deploymentBusy(agent.id) || deployments[agent.id]?.status === 'rollback_failed'"
                  @select="restartAgent(agent)"
                >
                  <RotateCw class="size-4" />
                  重启服务
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" @select="uninstallAgent(agent)">
                  <Trash2 class="size-4" />
                  卸载 Agent
                </DropdownMenuItem>
                <DropdownMenuItem :disabled="!isHeartbeatExpired(agent)" @select="deleteAgent(agent)">
                  <X class="size-4" />
                  删除记录
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </footer>
        </div>
      </div>
    </template>

    <component
      :is="agentSettingsEditor"
      v-if="agentSettings && agentSettingsEditor"
      :key="agentSettings.agentId + ':' + agentSettings.profileId"
      :agent-settings="agentSettings"
      @ready="settingsLoadingAgentId = null"
      @close="closeAgentSettings"
    />
    <DockerAgentUpdateDialog v-if="dockerUpdateAgent" :agent="dockerUpdateAgent" @close="dockerUpdateAgent = null" @refresh="loadAgents" />
    <AgentPushDiff
      v-if="pushDiffAgent"
      :agent="pushDiffAgent"
      :can-push="!pushBlocked(pushDiffAgent)"
      :pushing="pushingIds.has(pushDiffAgent.id) || deploymentBusy(pushDiffAgent.id)"
      :offline="pushDiffAgent.status !== 'online'"
      @close="pushDiffAgentId = null"
      @refresh="loadAgents"
      @push="pushFromDiff(pushDiffAgent)"
    />
    <AgentDeploymentHistory v-if="deploymentHistoryAgent" :agent="deploymentHistoryAgent" :focus-deployment="notificationDeploymentId" :progress="deployments[deploymentHistoryAgent.id]" :busy="deploymentBusy(deploymentHistoryAgent.id)" :blocked="upgradeBusy(deploymentHistoryAgent.id) || pushingIds.has(deploymentHistoryAgent.id) || rollbackConfirming" @close="deploymentHistoryAgentId = null; notificationDeploymentId = ''" @rollback="requestRollback(deploymentHistoryAgent, $event)" />

    <GatewayInstallWizard
      v-if="gatewayInstallContext"
      :open="true"
      :profile-id="gatewayInstallContext.profileId"
      :profile-name="gatewayInstallContext.profileName"
      @update:open="value => !value && closeGatewayInstaller()"
      @agents-changed="loadAgents"
    />

    <!-- ===== 生成安装脚本 ===== -->
    <Dialog v-model:open="scriptDialogVisible">
      <DialogContent class="flex max-w-[760px] flex-col overflow-hidden">
        <DialogHeader class="shrink-0">
          <DialogTitle>生成 Agent 安装脚本</DialogTitle>
          <DialogDescription>选择服务，填写机器信息，再复制命令执行。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[65dvh] min-h-0 flex-col gap-5 overflow-y-auto">
          <FormField label="使用模式" hint="安装服务选普通模式，需要局域网分流选 Linux 网关。" :help="optionHelp('普通模式按服务选择 Shell 或 Docker 安装；需要局域网分流时选 Linux 网关。', 'OxiDNS 的高可用安装在对应服务内选择；网关向导会安装代理与 DNS，并检查网络转发。')">
            <RadioGroup v-model="installationMode" class="flex flex-wrap gap-2" aria-label="使用模式">
              <label :class="[chipClass(installationMode === 'ordinary'), 'min-h-11']"><RadioGroupItem value="ordinary" />普通模式</label>
              <label :class="[chipClass(installationMode === 'gateway'), 'min-h-11']"><RadioGroupItem value="gateway" />Linux 网关</label>
            </RadioGroup>
          </FormField>
          <div v-if="installationMode === 'gateway'" class="space-y-4 rounded-lg border border-border bg-muted p-4">
            <div class="space-y-2 text-sm leading-relaxed">
              <p class="m-0 font-semibold">从一台 Linux 开始，按步骤装好网关</p>
              <p class="m-0 text-muted-foreground">选择分流规则 → 一条命令安装核心和 Agent → 自动检测网络 → 发布并检查服务 → 应用转发。主路由不需要静态路由。</p>
              <p class="m-0 break-words text-muted-foreground">本次固定使用配置空间「{{ activeProfileName }}」。建议使用专用配置空间，已有设备也会共用此空间的 Mihomo 和 OxiDNS 配置。</p>
            </div>
            <Button class="min-h-11" @click="openGatewayInstaller">开始或继续网关安装</Button>
          </div>
          <template v-else>
          <div class="space-y-4">
            <FormField label="安装类型" v-bind="installTypeCopy">
              <RadioGroup v-model="scriptForm.installType" class="flex gap-2" @update:model-value="onInstallTypeChange">
                <label v-for="option in INSTALL_TYPES" :key="option.value" :class="[chipClass(scriptForm.installType === option.value), 'min-h-11']">
                  <RadioGroupItem :value="option.value" />
                  {{ option.label }}
                </label>
              </RadioGroup>
            </FormField>

            <FormField v-if="scriptForm.installType === 'shell'" label="Shell 模式" hint="只管理一个服务，或在同机管理代理与 DNS。" :help="optionHelp('只需一种服务选单服务；代理与 DNS 同机运行选同机模式。', '同机模式分别登记 Mihomo 和 OxiDNS Agent，可管理两项服务。是否补装核心由下方开关决定，选择同机模式不会自动启用网关转发。')">
              <RadioGroup v-model="scriptForm.shellMode" class="flex flex-wrap gap-2" @update:model-value="resetForm">
                <label v-for="option in SHELL_MODES" :key="option.value" :class="[chipClass(scriptForm.shellMode === option.value), 'min-h-11']">
                  <RadioGroupItem :value="option.value" />
                  {{ option.label }}
                </label>
              </RadioGroup>
            </FormField>

            <FormField v-if="scriptForm.installType === 'shell' && scriptForm.shellMode === 'single'" label="服务类型">
              <RadioGroup v-model="scriptForm.type" class="grid grid-cols-3 gap-2 sm:flex" @update:model-value="onServiceTypeChange">
                <label v-for="option in serviceTypeOptions" :key="option.value" :class="[chipClass(scriptForm.type === option.value), 'min-h-11 justify-center max-sm:gap-1.5 max-sm:px-2']">
                  <RadioGroupItem :value="option.value" />
                  {{ option.label }}
                </label>
              </RadioGroup>
            </FormField>

            <FormField v-if="scriptForm.installType === 'docker'" label="Docker 模式" v-bind="dockerModeCopy">
              <RadioGroup v-model="scriptForm.dockerMode" class="flex flex-wrap gap-2" @update:model-value="onDockerModeChange">
                <label v-for="option in DOCKER_MODES" :key="option.value" :class="[chipClass(scriptForm.dockerMode === option.value), 'min-h-11']">
                  <RadioGroupItem :value="option.value" />
                  {{ option.label }}
                </label>
              </RadioGroup>
            </FormField>

            <FormField v-if="isOxiDNSInstallation" label="OxiDNS 部署方式">
              <RadioGroup v-model="oxidnsInstallMode" class="flex flex-wrap gap-2" aria-label="OxiDNS 部署方式">
                <label :class="[chipClass(oxidnsInstallMode === 'standard'), 'min-h-11']"><RadioGroupItem value="standard" />常规安装</label>
                <label :class="[chipClass(oxidnsInstallMode === 'ha'), 'min-h-11']"><RadioGroupItem value="ha" />高可用安装</label>
              </RadioGroup>
            </FormField>
          </div>

          <section v-if="showShellHAGuide" class="flex min-w-0 flex-col gap-4" aria-label="OxiDNS Shell HA 安装引导">
            <div>
              <h3 class="m-0 font-display text-xl">两台主机，共用一个 DNS 地址</h3>
              <p class="mb-0 mt-2 text-[13px] leading-relaxed text-muted-foreground">支持两台 Shell，或 Shell 与 Docker 混合。本次使用配置空间「{{ haGuideProfile.name }}」。</p>
            </div>
            <nav class="grid grid-cols-2 gap-2" aria-label="Shell 高可用安装步骤">
              <Button v-for="item in shellHASteps" :key="item.value" variant="outline" class="min-h-11 h-auto whitespace-normal px-2 py-2 text-xs" :class="shellHAStep === item.value ? 'border-primary-accent bg-primary-soft text-primary-accent' : ''" :aria-current="shellHAStep === item.value ? 'step' : undefined" @click="shellHAStep = item.value">{{ item.label }}</Button>
            </nav>
            <ShellHAPreparation v-if="shellHAStep === 'install'" :existing-agent="!!shellHAAgentId" />
          </section>
          <DockerHAInstallGuide v-if="showDockerHAGuide" :profile-id="haGuideProfile.id" :profile-name="haGuideProfile.name" :server-url="guideServerUrl" @changed="loadAgents" />
          <OxiDNSHAPairing v-if="showShellHAGuide && shellHAPairingVisited" v-show="shellHAStep === 'pair'" :active="shellHAStep === 'pair'" :profile-id="haGuideProfile.id" :profile-name="haGuideProfile.name" :initial-agent-id="shellHAAgentId" @changed="loadAgents" />
          <template v-if="showInstallForm">
          <div class="grid grid-cols-2 gap-4 border-t border-border/60 pt-4">
            <FormField label="Agent 名称" html-for="agent-name" :class="scriptForm.installType === 'shell' && scriptForm.shellMode === 'single' ? 'min-w-0' : 'col-span-2 sm:col-span-1'">
              <Input id="agent-name" v-model="scriptForm.name" class="min-h-11 bg-background/50" placeholder="例如：home-server" />
            </FormField>

            <FormField v-if="scriptForm.installType === 'shell' && scriptForm.shellMode === 'single'" label="Agent 端口" html-for="agent-port">
              <Input id="agent-port" v-model.number="scriptForm.port" type="number" :min="1024" :max="65535" class="min-h-11 bg-background/50 font-mono" />
            </FormField>

            <FormField :label="dockerDNSHA ? (scriptForm.haNetworkMode === 'host' ? '宿主机固定 IPv4（必填）' : '容器固定 IPv4（必填）') : 'Agent IP（可选）'" html-for="agent-ip" :hint="dockerDNSHA ? (scriptForm.haNetworkMode === 'host' ? '填写这台 Linux 主机的静态局域网地址，不是虚拟 IP。' : '使用未占用的局域网地址，与宿主机地址、虚拟 IP 分别不同。') : undefined" :class="scriptForm.installType === 'shell' && scriptForm.shellMode === 'single' ? 'col-span-2' : 'col-span-2 sm:col-span-1'">
              <Input id="agent-ip" v-model="scriptForm.agent_ip" class="min-h-11 bg-background/50 font-mono" :placeholder="dockerDNSHA ? '例如：192.168.1.21' : '留空则自动获取'" :aria-required="dockerDNSHA" />
            </FormField>

            <FormField v-if="scriptForm.installType === 'docker'" label="Agent 容器名" html-for="docker-container" class="col-span-2">
              <Input id="docker-container" v-model="scriptForm.containerName" class="min-h-11 bg-background/50 font-mono" placeholder="configflow-agent" />
            </FormField>

            <template v-if="!(scriptForm.installType === 'shell' && scriptForm.shellMode === 'single')">
              <FormField v-if="shellAio || scriptForm.dockerMode === 'mihomo' || scriptForm.dockerMode === 'aio'" label="Mihomo Agent 端口" html-for="mihomo-agent-port">
                <Input id="mihomo-agent-port" v-model.number="scriptForm.mihomoAgentPort" type="number" :min="1024" :max="65535" class="min-h-11 bg-background/50 font-mono" />
              </FormField>
              <FormField v-if="!shellAio && scriptForm.dockerMode === 'mosdns'" label="MosDNS Agent 端口" html-for="mosdns-agent-port">
                <Input id="mosdns-agent-port" v-model.number="scriptForm.mosdnsAgentPort" type="number" :min="1024" :max="65535" class="min-h-11 bg-background/50 font-mono" />
              </FormField>
              <FormField v-if="shellAio || scriptForm.dockerMode === 'oxidns' || scriptForm.dockerMode === 'aio'" label="OxiDNS Agent 端口" html-for="oxidns-agent-port">
                <Input id="oxidns-agent-port" v-model.number="scriptForm.oxidnsAgentPort" type="number" :min="1024" :max="65535" class="min-h-11 bg-background/50 font-mono" />
              </FormField>
            </template>
          </div>

          <div v-if="scriptForm.installType === 'docker' && scriptForm.dockerMode === 'oxidns'" class="flex flex-col gap-3">
            <label for="docker-dns-ha" class="flex min-h-11 cursor-pointer items-center gap-2 text-[13px] font-medium">
              <Checkbox id="docker-dns-ha" v-model="scriptForm.dockerDNSHA" aria-describedby="docker-dns-ha-note" />
              为 DNS 高可用准备容器
            </label>
            <p id="docker-dns-ha-note" class="m-0 text-[12px] leading-relaxed text-muted-foreground">
              为两台不同 Linux 宿主机准备 OxiDNS 高可用服务。已有容器需按原数据挂载重建，不能只更新 Agent 或打开开关获得所需网络与权限。
            </p>
            <template v-if="dockerDNSHA">
              <FormField label="高可用网络">
                <RadioGroup v-model="scriptForm.haNetworkMode" class="flex flex-wrap gap-2" aria-label="高可用网络">
                  <label :class="[chipClass(scriptForm.haNetworkMode === 'host'), 'min-h-11']"><RadioGroupItem value="host" />Host（推荐）</label>
                  <label :class="[chipClass(scriptForm.haNetworkMode === 'macvlan'), 'min-h-11']"><RadioGroupItem value="macvlan" />Macvlan</label>
                </RadioGroup>
              </FormField>
              <p v-if="scriptForm.haNetworkMode === 'host'" class="m-0 text-[12px] leading-relaxed text-muted-foreground">
                使用宿主机网络与固定地址，另起独立的高可用容器管理虚拟 IP。请确认宿主机的 DNS 53 端口与 Agent 端口未被其他服务占用，并保留高可用容器运行。
              </p>
              <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <FormField label="宿主机网卡（必填）" html-for="docker-ha-parent" hint="接入这段局域网的网卡，可用 ip -br addr 查看。">
                  <Input id="docker-ha-parent" v-model="scriptForm.haParent" class="min-h-11 font-mono" placeholder="例如：eth0 或 enp1s0" aria-required="true" />
                </FormField>
                <FormField label="局域网网段（必填）" html-for="docker-ha-subnet">
                  <Input id="docker-ha-subnet" v-model="scriptForm.haSubnet" class="min-h-11 font-mono" placeholder="例如：192.168.1.0/24" aria-required="true" />
                </FormField>
                <FormField label="局域网网关（可选）" html-for="docker-ha-gateway" hint="留空由 Docker 选择该网段的默认网关；建议填写实际路由器地址。" class="sm:col-span-2">
                  <Input id="docker-ha-gateway" v-model="scriptForm.haGateway" class="min-h-11 font-mono" placeholder="例如：192.168.1.1" />
                </FormField>
              </div>
              <p v-if="scriptForm.haNetworkMode === 'macvlan'" class="m-0 rounded-md border border-warning/30 bg-warning-soft p-3 text-[12px] leading-relaxed text-foreground">
                macvlan 默认不能与本宿主机通信。ConfigFlow 和 DNS 上游须位于可达的其他局域网主机，或已配置互通的 macvlan 网络；不支持 macvlan 的主机可选择 Host。
              </p>
              <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">两台上线后还需在「DNS 高可用」中配对。普通 bridge 网络不支持此模式。</p>
            </template>
          </div>

          <div v-if="shellAio || singleCoreSupported" class="min-w-0">
            <label :for="shellAio ? 'aio-install-cores' : 'agent-install-core'" class="flex min-h-11 cursor-pointer items-center gap-2 text-[13px] font-medium">
              <Checkbox :id="shellAio ? 'aio-install-cores' : 'agent-install-core'" v-model="installCoreChoice" :aria-describedby="shellAio ? 'aio-install-cores-note' : 'agent-install-core-note'" />
              同时安装 {{ shellAio ? 'Mihomo 与 OxiDNS' : scriptForm.type === 'mihomo' ? 'Mihomo' : 'OxiDNS' }} 核心
            </label>
            <p :id="shellAio ? 'aio-install-cores-note' : 'agent-install-core-note'" class="m-0 pl-6 text-[12px] leading-relaxed text-muted-foreground">
              {{ showShellHAGuide
                ? combinedCoreInstall
                  ? '新主机自动安装 OxiDNS；已有核心和配置保留。'
                  : '使用本机已有的 OxiDNS，仅安装 Agent 和高可用依赖。'
                : combinedCoreInstall
                  ? '保留已有核心，仅补装缺少部分；使用默认路径。'
                  : '未勾选时仅安装 Agent，接管已有核心。' }}
            </p>
          </div>

          <div v-if="singleMihomoForwarding" class="min-w-0">
            <label for="agent-skip-forwarding" :class="['flex min-h-11 items-center gap-2 text-[13px] font-medium', forwardingCustomLifecycle ? 'cursor-not-allowed text-muted-foreground' : 'cursor-pointer']">
              <Checkbox id="agent-skip-forwarding" v-model="scriptForm.skipIpv4Forwarding" :disabled="forwardingCustomLifecycle" aria-describedby="agent-skip-forwarding-note" />
              不开启 IPv4 转发
            </label>
            <p id="agent-skip-forwarding-note" class="m-0 pl-6 text-[12px] leading-relaxed text-muted-foreground">
              {{ forwardingCustomLifecycle
                ? '自定义启停命令的安装不自动开启 IPv4 转发。'
                : scriptForm.skipIpv4Forwarding
                  ? '不修改本机 IPv4 转发；已有的持久化设置会保留。被路由器用作代理网关后，可在这台设备的卡片上开启并持久化。'
                  : '默认开启并持久化本机 IPv4 转发（写入 /etc/sysctl.d/99-configflow-aio-forward.conf，Agent 启动前检查），作为主路由的代理网关时需要。' }}
            </p>
          </div>

          <div class="divide-y divide-border/60 border-y border-border/60">
            <Collapsible v-if="shellAio" v-model:open="lifecyclePanelOpen">
              <CollapsibleTrigger aria-label="核心服务高级配置" class="flex min-h-11 w-full items-center gap-2 py-3 text-left text-[13px] font-medium">
                核心服务高级配置
                <span class="ml-auto text-[12px] font-normal text-muted-foreground">路径 / 服务</span>
                <ChevronDown class="size-3.5 shrink-0 transition-transform" :class="lifecyclePanelOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent class="space-y-4 pb-4">
                <p v-if="aioCoreInstall" class="m-0 text-[12px] text-muted-foreground">同时安装核心时使用默认路径；接管自定义部署请取消勾选。</p>
                <div class="grid gap-4 sm:grid-cols-2">
                  <FormField label="Mihomo 配置文件路径" html-for="aio-mihomo-config-path">
                    <Input id="aio-mihomo-config-path" :model-value="aioServiceSettings.mihomoConfigPath" :disabled="aioCoreInstall" @update:model-value="scriptForm.mihomoConfigPath = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="/etc/mihomo/config.yaml" />
                  </FormField>
                  <FormField label="OxiDNS 配置文件路径" html-for="aio-oxidns-config-path">
                    <Input id="aio-oxidns-config-path" :model-value="aioServiceSettings.oxidnsConfigPath" :disabled="aioCoreInstall" @update:model-value="scriptForm.oxidnsConfigPath = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="/etc/oxidns/config.yaml" />
                  </FormField>
                  <FormField label="Mihomo 服务名称" html-for="aio-mihomo-service-unit">
                    <Input id="aio-mihomo-service-unit" :model-value="aioServiceSettings.mihomoServiceUnit" :disabled="aioCoreInstall" @update:model-value="scriptForm.mihomoServiceUnit = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="mihomo" />
                  </FormField>
                  <FormField label="OxiDNS 服务名称" html-for="aio-oxidns-service-unit">
                    <Input id="aio-oxidns-service-unit" :model-value="aioServiceSettings.oxidnsServiceUnit" :disabled="aioCoreInstall" @update:model-value="scriptForm.oxidnsServiceUnit = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="oxidns" />
                  </FormField>
                  <FormField label="Mihomo 内核程序路径" html-for="aio-mihomo-service-binary" v-bind="serviceBinaryCopy">
                    <Input id="aio-mihomo-service-binary" :model-value="aioServiceSettings.mihomoServiceBinary" :disabled="aioCoreInstall" @update:model-value="scriptForm.mihomoServiceBinary = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="mihomo" />
                  </FormField>
                  <FormField label="OxiDNS 内核程序路径" html-for="aio-oxidns-service-binary" v-bind="serviceBinaryCopy">
                    <Input id="aio-oxidns-service-binary" :model-value="aioServiceSettings.oxidnsServiceBinary" :disabled="aioCoreInstall" @update:model-value="scriptForm.oxidnsServiceBinary = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="oxidns" />
                  </FormField>
                </div>
                <p v-if="!aioCoreInstall" class="m-0 text-[12px] leading-relaxed text-muted-foreground">OxiDNS 服务的 <code class="font-mono">-d</code> 工作目录须与配置文件所在目录一致。</p>
              </CollapsibleContent>
            </Collapsible>

            <Collapsible v-if="scriptForm.installType === 'shell' && scriptForm.shellMode === 'single'" v-model:open="lifecyclePanelOpen">
              <CollapsibleTrigger aria-label="服务管理高级配置（可选）" class="flex min-h-11 w-full items-center gap-2 py-3 text-left text-[13px] font-medium">
                服务管理高级配置（可选）
                <span class="ml-auto whitespace-nowrap text-[12px] font-normal text-muted-foreground">路径 / 服务</span>
                <ChevronDown class="size-3.5 shrink-0 transition-transform" :class="lifecyclePanelOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent class="space-y-4 pb-4">
                <p v-if="singleCoreInstall" class="m-0 text-[12px] text-muted-foreground">
                  {{ showShellHAGuide ? '新装核心使用默认路径；已有 Agent 沿用本机配置。' : '同时安装核心时使用默认路径；接管自定义部署请取消勾选。' }}
                </p>
                <FormField label="配置文件路径" html-for="agent-config-path" hint="填写目标设备上实际使用的配置文件完整路径。" :help="optionHelp('接管已有核心，且配置不在默认位置时填写。', '使用目标设备上的完整路径，必须含文件名，如 /etc/mihomo/config.yaml。Agent 按此位置管理配置，应与现有服务实际读取的位置一致。')">
                  <Input id="agent-config-path" :model-value="singleServiceSettings.config_path" :disabled="singleCoreInstall" @update:model-value="scriptForm.config_path = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="/etc/mihomo/config.yaml" />
                </FormField>
                <p v-if="scriptForm.type === 'oxidns' && !singleCoreInstall" class="m-0 text-[12px] leading-relaxed text-muted-foreground">OxiDNS 的 <code class="font-mono">-d</code> 工作目录须与配置文件所在目录一致；分离布局暂不支持，详见安装说明。</p>
                <FormField label="重启命令（可选）" html-for="agent-restart" hint="标准系统服务可留空，自定义部署再填写。" :help="optionHelp('现有服务无法通过标准系统服务方式管理时填写。', '留空会自动识别 systemd / OpenRC。自定义重启命令还需配套停止、启动和状态检查命令，让 Agent 能确认切换结果及处理失败。')">
                  <Input id="agent-restart" :model-value="singleServiceSettings.restart_command" :disabled="singleCoreInstall" @update:model-value="scriptForm.restart_command = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" placeholder="留空自动识别" />
                </FormField>
                <div class="grid gap-4 sm:grid-cols-2">
                  <FormField label="服务管理方式">
                    <Select :model-value="singleServiceSettings.service_manager" :disabled="singleCoreInstall" @update:model-value="scriptForm.service_manager = String($event ?? '')">
                      <SelectTrigger class="min-h-11 w-full bg-background/50"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">自动识别 systemd / OpenRC</SelectItem>
                        <SelectItem value="systemd">systemd</SelectItem>
                        <SelectItem value="openrc">OpenRC</SelectItem>
                        <SelectItem value="command">自定义命令</SelectItem>
                      </SelectContent>
                    </Select>
                  </FormField>
                  <FormField label="服务名称" html-for="agent-service-unit" hint="服务使用自定义名称时填写，默认可留空。" :help="optionHelp('已有系统服务名称与核心名称不同，如自定义安装时填写。', '填写实际 systemd 单元或 OpenRC 服务名，Agent 会用它管理服务。留空使用核心名称；此处不是设备备注，也不会重命名已有服务。')">
                    <Input id="agent-service-unit" :model-value="singleServiceSettings.service_unit" :disabled="singleCoreInstall" @update:model-value="scriptForm.service_unit = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" :placeholder="scriptForm.type" />
                  </FormField>
                </div>
                <FormField label="内核程序路径" html-for="agent-service-binary" v-bind="serviceBinaryCopy">
                  <Input id="agent-service-binary" :model-value="singleServiceSettings.service_binary" :disabled="singleCoreInstall" @update:model-value="scriptForm.service_binary = String($event ?? '')" class="min-h-11 bg-background/50 font-mono" :placeholder="`/usr/local/bin/${scriptForm.type}`" />
                </FormField>
                <template v-if="!singleCoreInstall && (scriptForm.service_manager === 'command' || scriptForm.restart_command.trim())">
                  <FormField label="停止命令" html-for="agent-service-stop" hint="填写能停止服务并等待其完全退出的命令。" :help="optionHelp('使用自定义服务管理命令时，需提供可靠的停止操作。', '命令必须等服务完全停止后再退出，让 Agent 确认旧进程已结束。仅发送停止信号就返回，可能使后续启动与旧进程冲突。')">
                    <Input id="agent-service-stop" v-model="scriptForm.stop_command" class="min-h-11 bg-background/50 font-mono" placeholder="例如：/opt/service/control stop" />
                  </FormField>
                  <FormField label="启动命令" html-for="agent-service-start">
                    <Input id="agent-service-start" v-model="scriptForm.start_command" class="min-h-11 bg-background/50 font-mono" placeholder="例如：/opt/service/control start" />
                  </FormField>
                  <FormField label="状态检查命令" html-for="agent-service-status" hint="通过命令结果，告诉 Agent 服务是否正在运行。" :help="optionHelp('使用自定义命令管理服务，需要检查启动和停止结果。', '服务运行时命令退出码必须为 0，未运行时为非 0。Agent 根据退出码判断状态，不依靠输出文字；应检查实际核心进程。')">
                    <Input id="agent-service-status" v-model="scriptForm.status_command" class="min-h-11 bg-background/50 font-mono" placeholder="例如：/opt/service/control status" />
                  </FormField>
                </template>
              </CollapsibleContent>
            </Collapsible>

            <Collapsible v-if="scriptForm.installType === 'docker'" v-model:open="dockerOptionsOpen">
              <CollapsibleTrigger class="flex min-h-11 w-full items-center gap-2 py-3 text-left text-[13px] font-medium">
                Docker 高级配置
                <span class="ml-auto text-[12px] font-normal text-muted-foreground">镜像 / 网络</span>
                <ChevronDown class="size-3.5 shrink-0 transition-transform" :class="dockerOptionsOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent class="space-y-4 pb-4">
                <FormField label="Docker 镜像" html-for="docker-image">
                  <Input id="docker-image" v-model="scriptForm.dockerImage" class="min-h-11 bg-background/50 font-mono" placeholder="留空使用官方镜像" />
                </FormField>
                <FormField v-if="dockerDNSHA" label="宿主机身份文件" html-for="docker-ha-host-id" hint="默认 /etc/machine-id。若系统使用其他位置，填写现有的稳定主机身份文件；同一宿主机上的容器必须使用同一个文件。">
                  <Input id="docker-ha-host-id" v-model="scriptForm.haHostIdPath" class="min-h-11 bg-background/50 font-mono" placeholder="/etc/machine-id" />
                </FormField>
                <p v-if="dockerDNSHA" class="m-0 text-[13px]">网络模式：{{ scriptForm.haNetworkMode === 'host' ? 'Host，配合独立高可用容器' : '专用 Macvlan，使用固定局域网地址' }}，不设置端口映射。</p>
                <FormField v-else label="网络模式" hint="共享主机网络，或用端口映射访问容器服务。" :help="optionHelp('使用 Docker 安装，需决定设备如何访问容器内服务。', 'host 直接使用主机网络，bridge 通过端口映射访问。使用 bridge 时需确认所需端口已映射；切换网络方式不代表自动配置局域网网关。')">
                  <RadioGroup v-model="scriptForm.networkMode" class="flex flex-wrap gap-2">
                    <label v-for="option in networkModeOptions" :key="option.value" :class="[chipClass(scriptForm.networkMode === option.value), 'min-h-11']">
                      <RadioGroupItem :value="option.value" />
                      {{ option.label }}
                    </label>
                  </RadioGroup>
                </FormField>
              </CollapsibleContent>
            </Collapsible>

            <Collapsible v-model:open="installHelpOpen">
              <CollapsibleTrigger aria-label="安装说明" class="flex min-h-11 w-full items-center gap-2 py-3 text-left text-[13px] font-medium">
                安装说明
                <span class="ml-auto text-[12px] font-normal text-muted-foreground">{{ scriptForm.installType === 'docker' ? '部署须知' : 'DNS / TUN' }}</span>
                <ChevronDown class="size-3.5 shrink-0 transition-transform" :class="installHelpOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent class="space-y-3 pb-4 text-[12.5px] leading-relaxed text-muted-foreground">
                <p class="m-0">Agent 名称可使用字母、数字、短横线或下划线。生成命令不会注册 Agent，需在目标机器执行后才会接入面板。</p>
                <template v-if="scriptForm.installType === 'shell'">
                  <p class="m-0">在目标机器以 root 身份执行，支持 systemd / OpenRC。勾选安装核心后，脚本先安装核心，成功后再安装 Agent；已有核心和配置会保留，仅补装缺少的核心。</p>
                  <p class="m-0">默认使用 curl，也可切换 wget；完整下载为当前目录的 configflow-install.sh 后才执行。环境检测与依赖补装由脚本完成；两个下载工具都没有时，请先安装一个。</p>
                  <p class="m-0 break-words">安装核心时使用 <code class="font-mono">/etc/{{ shellAio ? '{mihomo,oxidns}' : scriptForm.type }}/config.yaml</code>，程序位于 <code class="font-mono">/usr/local/bin</code>，服务名与核心同名。已有部署须与默认布局一致；自定义部署请取消勾选，并在高级配置中填写实际路径。</p>
                  <template v-if="shellAio">
                    <p class="m-0"><strong class="font-medium text-foreground">初始 DNS：</strong>OxiDNS <code class="font-mono">53</code> → Mihomo <code class="font-mono">127.0.0.1:1053</code>。发布时保留自定义监听地址、端口及 <code class="font-mono">dns-hijack</code>，并检查实际 DNS 配置。</p>
                    <p v-if="aioCoreInstall" class="m-0">新装 OxiDNS 会自动使用上述上游；已有配置保持不变，请确认已有核心的 DNS 监听与上游设置。</p>
                    <p v-else class="m-0">请先准备两个核心及其 systemd / OpenRC 服务，并将 OxiDNS 的远端 DNS 上游设为 Mihomo 实际的 DNS 监听地址和端口。</p>
                  </template>
                  <p v-else-if="singleCoreInstall && scriptForm.type === 'mihomo'" class="m-0"><strong class="font-medium text-foreground">Mihomo DNS：</strong>监听 <code class="font-mono">1053</code>，使用公共 DNS 上游。</p>
                  <p v-else-if="singleCoreInstall && scriptForm.type === 'oxidns'" class="m-0"><strong class="font-medium text-foreground">OxiDNS：</strong>监听 <code class="font-mono">53</code>，使用公共 DNS 上游，独立运行且不依赖 Mihomo。接入后可在面板修改上游。</p>
                  <p v-if="shellAio || scriptForm.type === 'mihomo'" class="m-0"><strong class="font-medium text-foreground">TUN：</strong>首次安装 Mihomo 时检测设备与权限，可用则开启 TUN 和自动路由，否则关闭并提示修复方法。初始流量直连，尚无代理节点；作为网关还需开启 IPv4 转发，脚本仅提示、不修改。发布前请检查面板草稿的 TUN 开关。</p>
                  <p v-if="(shellAio || scriptForm.type === 'oxidns') && !combinedCoreInstall" class="m-0"><strong class="font-medium text-foreground">OxiDNS 路径：</strong>服务的 <code class="font-mono">-d</code> 工作目录须与配置目录一致，例如 <code class="font-mono">/opt/oxidns/config.yaml</code> 对应 <code class="font-mono">-d /opt/oxidns</code>。官方 Debian 的 <code class="font-mono">/etc</code> 配置与 <code class="font-mono">/var/lib</code> 工作目录分离布局暂不支持，需先调整服务与规则资源布局。</p>
                </template>
                <template v-else>
                  <p class="m-0 font-medium text-foreground">{{ dockerNote.title }}</p>
                  <ul class="m-0 list-disc space-y-1 pl-4"><li v-for="line in dockerNote.lines" :key="line">{{ line }}</li></ul>
                </template>
              </CollapsibleContent>
            </Collapsible>

            <Collapsible v-if="shellAio && !aioCoreInstall" v-model:open="coreOnlyPanelOpen">
              <CollapsibleTrigger id="core-install-title" aria-label="仅安装核心" class="flex min-h-11 w-full items-center gap-2 py-3 text-left text-[13px] font-medium">
                仅安装核心
                <span class="ml-auto text-[12px] font-normal text-muted-foreground">不安装 Agent</span>
                <ChevronDown class="size-3.5 shrink-0 transition-transform" :class="coreOnlyPanelOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <section aria-labelledby="core-install-title" class="min-w-0 space-y-3 pb-4">
                  <p class="m-0 text-[12.5px] text-muted-foreground">无需填写 Agent 信息。保留已有核心和配置，仅补装缺少部分。</p>
                  <RadioGroup v-model="coreInstallTarget" aria-label="要安装的核心" class="flex flex-wrap gap-2">
                    <label v-for="option in CORE_INSTALL_TARGETS" :key="option.value" :class="[chipClass(coreInstallTarget === option.value), 'min-h-11']">
                      <RadioGroupItem :value="option.value" />
                      {{ option.label }}
                    </label>
                  </RadioGroup>
                  <p v-if="coreInstallTarget === 'both'" class="m-0 text-[12px] leading-relaxed text-muted-foreground">OxiDNS 53 → Mihomo 1053。已有 Mihomo 时仅补装 OxiDNS；请确认 Mihomo DNS 监听 <code class="font-mono">127.0.0.1:1053</code>。</p>
                  <p v-else-if="coreInstallTarget === 'oxidns'" class="m-0 text-[12px] leading-relaxed text-muted-foreground">OxiDNS 监听 53，使用公共 DNS 上游，无需 Mihomo。</p>
                  <p v-else class="m-0 text-[12px] leading-relaxed text-muted-foreground">Mihomo DNS 监听 1053，使用公共 DNS 上游。</p>
                  <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">以 root 执行，支持 systemd / OpenRC；自动补齐依赖，完整下载后安装。使用默认路径，不受高级配置影响。TUN 检测规则见安装说明。</p>
                  <template v-if="coreInstallCommand">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <RadioGroup v-model="installerDownloader" aria-label="核心安装命令下载方式" class="flex gap-2">
                        <label v-for="downloader in INSTALLER_DOWNLOADERS" :key="downloader" :class="[chipClass(installerDownloader === downloader), 'min-h-11']">
                          <RadioGroupItem :value="downloader" />{{ downloader }}
                        </label>
                      </RadioGroup>
                      <Button variant="outline" class="min-h-11" @click="copyAgentContent(coreInstallCommand, '核心安装命令已复制到剪贴板')">
                        <Copy class="size-4" />复制命令
                      </Button>
                    </div>
                    <p class="m-0 text-[11.5px] text-muted-foreground">链接有效至 {{ coreInstallLinkExpiry }}，过期后请重新生成。</p>
                    <Textarea id="core-install-command" :model-value="coreInstallCommand" :aria-label="`${coreInstallLabel}的独立安装命令`" readonly :rows="3" class="max-h-48 bg-background/50 font-mono text-[11.5px]" />
                    <Collapsible>
                      <CollapsibleTrigger class="min-h-11 w-full text-left text-[12.5px] text-muted-foreground">查看核心安装脚本</CollapsibleTrigger>
                      <CollapsibleContent>
                        <p class="mt-0 text-[12px] text-muted-foreground">已包含所选核心，与上方链接下载的脚本一致。</p>
                        <Textarea :model-value="coreInstallScript" :aria-label="`${coreInstallLabel}的独立安装脚本`" readonly :rows="12" class="max-h-64 bg-background/50 font-mono text-[11.5px]" />
                      </CollapsibleContent>
                    </Collapsible>
                  </template>
                  <Button v-else variant="outline" class="min-h-11" :disabled="coreInstallLoading" @click="generateCoreInstallCommand">
                    <Loader2 v-if="coreInstallLoading" class="size-4 animate-spin" />
                    <Download v-else class="size-4" />
                    {{ coreInstallLoading ? '正在生成核心安装命令…' : `生成${coreInstallLabel}的安装命令` }}
                  </Button>
                </section>
              </CollapsibleContent>
            </Collapsible>
          </div>

          <section v-if="installCommand || dockerComposeContent || dockerRunCommand" ref="installCommandSection" aria-label="一键安装命令" class="space-y-4">
            <LabeledDivider label="一键安装命令" />
            <RadioGroup v-if="scriptForm.installType === 'shell'" v-model="installerDownloader" aria-label="安装命令下载方式" class="flex gap-2">
              <label v-for="downloader in INSTALLER_DOWNLOADERS" :key="downloader" :class="[chipClass(installerDownloader === downloader), 'min-h-11']">
                <RadioGroupItem :value="downloader" />{{ downloader }}
              </label>
            </RadioGroup>
            <p v-if="scriptForm.installType === 'shell'" class="m-0 text-[11.5px] text-muted-foreground">链接有效至 {{ installLinkExpiry }}，过期后请重新生成。</p>
            <div v-for="block in commandBlocks" :key="block.title" class="flex min-w-0 flex-col gap-2">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="text-[12.5px] font-semibold text-foreground">{{ block.title }}</span>
                <Button variant="outline" class="min-h-11 border-border/60 bg-background/40" @click="block.copy"><Copy class="size-4" />复制</Button>
              </div>
              <p class="m-0 text-[11.5px] text-muted-foreground">{{ block.note }}</p>
              <Textarea :model-value="block.value" readonly :rows="block.rows" class="max-h-48 bg-background/50 font-mono text-[11.5px]" />
            </div>
            <Collapsible v-model:open="scriptPanelOpen">
              <CollapsibleTrigger class="flex min-h-11 w-full cursor-pointer items-center gap-2 py-2 text-left text-[13px] text-muted-foreground">
                查看完整安装脚本
                <ChevronDown class="ml-auto size-3.5 transition-transform duration-200" :class="scriptPanelOpen && 'rotate-180'" />
              </CollapsibleTrigger>
              <CollapsibleContent>
                <Textarea :model-value="installScript" readonly :rows="15" class="mt-2 max-h-72 bg-background/50 font-mono text-[11.5px]" />
              </CollapsibleContent>
            </Collapsible>
          </section>
          </template>
          </template>
        </div>

        <DialogFooter class="shrink-0 flex-row items-center justify-end gap-2 border-t border-border/60 pt-4">
          <Button variant="outline" class="min-h-11" @click="scriptDialogVisible = false">关闭</Button>
          <template v-if="showInstallForm">
          <Button v-if="installCommand || dockerComposeContent || dockerRunCommand" variant="outline" class="min-h-11" @click="resetForm">重新生成</Button>
          <Button v-else class="min-h-11" :disabled="scriptLoading" @click="generateScript">
            <Loader2 v-if="scriptLoading" class="size-4 animate-spin" />
            <FileText v-else class="size-4" />
            {{ scriptLoading ? '正在生成安装命令…' : scriptForm.installType === 'docker' ? '生成 Docker 部署命令' : '生成安装命令' }}
          </Button>
          </template>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== Agent 日志 ===== -->
    <Dialog v-model:open="logsDialogVisible">
      <DialogContent class="max-w-[1100px]">
        <DialogHeader>
          <DialogTitle>Agent 日志 · {{ currentAgent?.name }}</DialogTitle>
          <DialogDescription>日志位置由 Agent 按本机的服务配置自动查找。清空会删除所选文件中的全部日志，且无法恢复。</DialogDescription>
        </DialogHeader>

        <div class="flex min-w-0 flex-col gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <Select v-model="selectedLogSource" :disabled="logSourcesLoading" @update:model-value="value => onLogSourceChange(String(value))">
              <SelectTrigger class="h-9 w-[260px] bg-background/50 text-[13px] max-sm:w-full">
                <SelectValue placeholder="选择日志" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="source in logSources" :key="source.id" :value="source.id">
                  {{ source.label }}{{ source.kind === 'unavailable' ? '（不可读）' : '' }}
                </SelectItem>
                <SelectItem value="custom">自定义路径…</SelectItem>
              </SelectContent>
            </Select>

            <template v-if="selectedLogSource === 'custom'">
              <Input
                v-model="customLogPath"
                class="w-[300px] bg-background/50 font-mono max-sm:w-full"
                placeholder="/var/log/your-file.log"
                @keyup.enter="loadLogs"
              />
              <Button
                variant="outline"
                class="border-border/60 bg-background/40"
                :disabled="logsLoading"
                @click="loadLogs"
              >
                <Loader2 v-if="logsLoading" class="size-4 animate-spin" />
                查看日志
              </Button>
            </template>
          </div>

          <p v-if="currentLogLocation" class="m-0 break-all font-mono text-[12px] text-muted-foreground">
            {{ currentLogLocation }}
          </p>

          <AgentLogLines
            ref="logsPaneRef"
            :text="logs"
            :notice="logsNotice || (logsLoading && !logs ? '正在读取日志…' : '')"
          />
        </div>

        <DialogFooter>
          <Button variant="outline" @click="logsDialogVisible = false">关闭</Button>
          <Button
            variant="outline"
            class="border-destructive-accent/30 bg-destructive-soft/40 text-destructive-accent"
            :disabled="!canClearLog"
            :title="canClearLog ? undefined : '系统日志（journald）和不可读的来源不能在这里清空'"
            @click="clearLogs"
          >
            <Trash2 class="size-4" />
            清空
          </Button>
          <Button :disabled="logsLoading" @click="refreshLogs">
            <Loader2 v-if="logsLoading" class="size-4 animate-spin" />
            <RefreshCw v-else class="size-4" />
            刷新
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 监控详情 ===== -->
    <Dialog v-model:open="metricsDialogVisible">
      <DialogContent class="max-w-[1100px]">
        <DialogHeader>
          <DialogTitle>系统监控 · {{ currentMetricsAgent?.name }}</DialogTitle>
          <DialogDescription>查看最近 24 小时的资源使用情况，已有 {{ metricsHistory.length }} 条记录。</DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="metricsLoading" :rows="4" />

        <EmptyState
          v-else-if="metricsHistory.length === 0"
          :icon="ChartLine"
          title="暂无监控数据"
          description="等待 Agent 连接并发送运行数据后，点击「刷新」查看。"
        />

        <div v-else class="flex max-h-[68dvh] flex-col gap-3 overflow-y-auto pr-1">
          <div
            v-if="currentMetricsAgent?.system_metrics"
            class="grid grid-cols-5 gap-2 max-[900px]:grid-cols-2"
          >
            <div
              v-for="item in metricsSummary"
              :key="item.label"
              class="rounded-lg border border-border/50 bg-background/40 p-3"
            >
              <p class="m-0 text-[11px] text-muted-foreground">{{ item.label }}</p>
              <p class="num mt-1 mb-0 text-[15px] font-semibold" :style="{ color: item.color }">
                {{ item.value }}
              </p>
              <p v-if="item.detail" class="num mt-0.5 mb-0 text-[11px] text-muted-foreground">
                {{ item.detail }}
              </p>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3 max-[900px]:grid-cols-1">
            <div
              v-for="chart in metricsCharts"
              :key="chart.key"
              class="rounded-xl border border-border/50 bg-background/40 p-2"
            >
              <v-chart :option="chart.option" :autoresize="true" style="height: 280px" />
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="metricsDialogVisible = false">关闭</Button>
          <Button @click="showMetricsDetail(currentMetricsAgent)">
            <RefreshCw class="size-4" />
            刷新
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { useChartPalette, withAlpha } from '@/lib/chartPalette'
import PageHeader from '@/components/common/PageHeader.vue'
import { ref, shallowRef, computed, onMounted, onUnmounted, nextTick, watch, type ComponentPublicInstance } from 'vue'
import { useRouter } from 'vue-router'
import { consumeAction } from '@/lib/actions'
import { cn } from '@/lib/utils'
import { optionCopy, optionHelp } from '@/lib/optionCopy'
import { getAgentConfigSync } from '@/lib/agentConfigSync'
import { buildInstallCommand, AGENT_CORE_INSTALL_INTERFACE, CORE_INSTALL_INTERFACE, DNS_HA_PREPARE_INTERFACE, type CoreInstallTarget, type InstallerDownloader } from '@/lib/coreInstallCommand'
import { supportsAgentSettings, type AgentSettingsContext } from '@/lib/agentSettings'
import DockerAgentUpdateDialog from '@/components/agents/DockerAgentUpdateDialog.vue'
import AgentTraffic from '@/components/agents/AgentTraffic.vue'
import AgentLogLines from '@/components/agents/AgentLogLines.vue'
import GatewayInstallWizard from '@/components/agents/GatewayInstallWizard.vue'
import {
  ChartLine,
  ChevronDown,
  ChevronRight,
  CircleCheck,
  Copy,
  Download,
  FileText,
  History,
  Loader2,
  MoreHorizontal,
  RefreshCw,
  RotateCw,
  ScrollText,
  Send,
  Server,
  Settings,
  Trash2,
  TriangleAlert,
  Upload,
  X
} from '@lucide/vue'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import EmptyState from '@/components/common/EmptyState.vue'
import FormField from '@/components/common/FormField.vue'
import LabeledDivider from '@/components/common/LabeledDivider.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import StatTile from '@/components/common/StatTile.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { listItemStyle } from '@/lib/motion'
import { agentApi, type AgentInstallScriptParams, type AgentLogSource } from '@/api'
import api from '@/api'
import type { Agent } from '@/types'
import { useProfileStore } from '@/stores/profile'
import { createDeploymentId, deploymentNeedsNotice, useAgentDeployments, type AgentDeployment } from '@/composables/useAgentDeployments'
import AgentDeploymentHistory from '@/components/agents/AgentDeploymentHistory.vue'
import AgentPushDiff from '@/components/agents/AgentPushDiff.vue'
import AgentDeploymentStatus from '@/components/agents/AgentDeploymentStatus.vue'
import AgentFailureDetails from '@/components/agents/AgentFailureDetails.vue'
import AgentOperationSteps from '@/components/agents/AgentOperationSteps.vue'
import AgentDNSFollow from '@/components/agents/AgentDNSFollow.vue'
import AgentDNSHA from '@/components/agents/AgentDNSHA.vue'
import OxiDNSHAPairing from '@/components/agents/OxiDNSHAPairing.vue'
import ShellHAPreparation from '@/components/agents/ShellHAPreparation.vue'
import DockerHAInstallGuide from '@/components/agents/DockerHAInstallGuide.vue'
import AgentDNSChecks from '@/components/agents/AgentDNSChecks.vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import AgentUpgradeStatus from '@/components/agents/AgentUpgradeStatus.vue'
import AgentGatewayForwarding from '@/components/agents/AgentGatewayForwarding.vue'
import OxiDNSRuleUpdate from '@/components/agents/OxiDNSRuleUpdate.vue'
import AgentRuleSync from '@/components/agents/AgentRuleSync.vue'
import { useSettingLink, queryText, focusSetting } from '@/lib/settingLink'
import { submitAgentRequests, uniquePublicationTargets } from '@/lib/agentRequests'
import { deploymentErrorSummary } from '@/lib/agentFailure'
import { upgradeLabels, useAgentUpgrades } from '@/composables/useAgentUpgrades'
import { useAgentGatewayForwarding } from '@/composables/useAgentGatewayForwarding'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

// Register ECharts components
use([LineChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent, CanvasRenderer])


const agents = ref<Agent[]>([])
const ruleUpdateRefs = ref<Record<string, InstanceType<typeof OxiDNSRuleUpdate> | null>>({})
function setRuleUpdateRef(id: string, instance: Element | ComponentPublicInstance | null) {
  ruleUpdateRefs.value[id] = instance as InstanceType<typeof OxiDNSRuleUpdate> | null
}
function ruleUpdateBlocked(agent: Agent) {
  return agent.status !== 'online' || pushingIds.value.has(agent.id) || upgradeBusy(agent.id) || deploymentBusy(agent.id) || deployments[agent.id]?.status === 'rollback_failed'
}
const { deployments, busy: deploymentBusy, track: trackDeployment, beginSubmission, endSubmission, refresh: refreshDeployment, activate: activateDeployment, retryPublish: retryDeployment, rollback: rollbackDeployment } = useAgentDeployments(() => { void loadAgents() })
const deploymentHistoryAgentId = ref<string | null>(null)
const notificationDeploymentId = ref('')
const dnsFollowRefs = new Map<string, InstanceType<typeof AgentDNSFollow>>()
function setDNSFollowRef(id: string, instance: Element | ComponentPublicInstance | null) {
  if (instance) dnsFollowRefs.set(id, instance as InstanceType<typeof AgentDNSFollow>)
  else dnsFollowRefs.delete(id)
}
const deploymentHistoryAgent = computed(() => agents.value.find(agent => agent.id === deploymentHistoryAgentId.value))
const rollbackConfirming = ref(false)
const requestRollback = async (agent: Agent, record: AgentDeployment) => {
  if (rollbackConfirming.value || agent.status !== 'online' || deploymentBusy(agent.id) || upgradeBusy(agent.id) || pushingIds.value.has(agent.id)) return
  if (agent.latest_deployment?.deployment_id !== record.deployment_id || record.status !== 'succeeded') return
  rollbackConfirming.value = true
  try {
    const accepted = await confirm(`将 ${agent.name} 恢复到发布 ${record.deployment_id} 之前的配置和受管规则，并重启核心检查健康状态。state/ 运行数据保留。`, {
      title: '回滚到发布前的配置？', confirmText: '回滚并重启', cancelText: '取消'
    })
    if (!accepted) return
    const current = agents.value.find(item => item.id === agent.id)
    if (!current || current.status !== 'online' || deploymentBusy(agent.id) || upgradeBusy(agent.id) || pushingIds.value.has(agent.id) || current.latest_deployment?.deployment_id !== record.deployment_id) return
    await rollbackDeployment(agent.id, record.deployment_id)
  } finally {
    rollbackConfirming.value = false
  }
}
const {
  upgrades, busy: upgradeBusy, querying: upgradeQuerying, track: trackUpgrade, query: queryAgentUpgrade, start: startUpgrade, retryUnknown: retryUnknownUpgrade
} = useAgentUpgrades(() => { void loadAgents() })
const { entries: forwardingEntries, busy: forwardingBusy, persist: persistForwarding } = useAgentGatewayForwarding(agents)

const retryUnconfirmedUpgrade = async (agentId: string) => {
  try {
    await retryUnknownUpgrade(agentId, () => confirm('尚未发现本次更新的新任务记录。重新提交前会再次由 Agent 检查是否已有更新在执行；如已有任务，系统会阻止重复执行，请查看最新结果。', {
      title: '重新提交更新请求？', confirmText: '重新提交', cancelText: '继续等待'
    }))
  } catch (error: any) { notify.error('暂时无法重新提交', error.response?.data?.message || error.message || '请稍后查询更新结果。') }
}

const queryUpgrade = async (agentId: string) => {
  try {
    const state = await queryAgentUpgrade(agentId)
    if (!state) return
    const label = upgradeLabels[state.status] || state.status
    if (state.status === 'succeeded') notify.success(label)
    else if (['failed', 'rolled_back', 'rollback_failed'].includes(state.status)) notify.error(state.error || label)
    else notify.info(`当前状态：${label}`)
  } catch (error: any) {
    notify.error(error.response?.data?.message || '查询更新结果失败，请稍后重试')
  }
}
const { profiles, refreshProfiles, defaultProfileId } = useProfileStore()
const bindingAgentId = ref<string | null>(null)
const agentSettings = ref<AgentSettingsContext | null>(null)
const agentSettingsEditor = shallowRef<typeof import('./Generate.vue')['default']>()
const settingsLoadingAgentId = ref<string | null>(null)
let settingsPageActive = true

const openAgentSettings = async (agent: Agent) => {
  if (settingsLoadingAgentId.value || agentSettings.value || bindingAgentId.value === agent.id || !supportsAgentSettings(agent.service_type)) return
  settingsLoadingAgentId.value = agent.id
  try {
    // Refresh both sources before capturing the scope: another tab may have
    // rebound the device or changed the default for an older unbound Agent.
    // 只需要绑定、名称和类型：用不含内容同步计算的轻量列表，仍是实时请求。
    const [response, currentProfiles, editor] = await Promise.all([
      agentApi.summaries(), refreshProfiles(), import('./Generate.vue')
    ])
    if (!settingsPageActive) return
    if (!Array.isArray(response.data)) throw new Error('设备列表响应无效。')
    const currentAgents = response.data as Agent[]
    const current = currentAgents.find(item => item.id === agent.id)
    if (!current) throw new Error('此 Agent 已不存在，请刷新列表。')
    if (!supportsAgentSettings(current.service_type)) throw new Error('此 Agent 的软件类型暂不支持配置设置。')
    const profileId = current.profile_id || defaultProfileId.value
    const profile = currentProfiles.find(item => item.id === profileId)
    if (!profile) throw new Error('此 Agent 绑定的配置空间已不存在，请重新绑定后再设置。')
    agentSettingsEditor.value = editor.default
    agentSettings.value = {
      agentId: current.id, agentName: current.name, serviceType: current.service_type,
      profileId: profile.id, profileName: profile.name,
      targets: { agents: currentAgents, profiles: currentProfiles },
    }
  } catch (error: any) {
    if (settingsPageActive) {
      settingsLoadingAgentId.value = null
      notify.error('打开配置设置失败', error.response?.data?.message || error.message || '请稍后重试。')
    }
  }
}

const closeAgentSettings = () => {
  agentSettings.value = null
  settingsLoadingAgentId.value = null
  void loadAgents()
  void refreshProfiles().catch(() => undefined)
}
const scriptDialogVisible = ref(false)
const installationMode = ref<'ordinary' | 'gateway'>('ordinary')
const oxidnsInstallMode = ref<'standard' | 'ha'>('standard')
const shellHAStep = ref<'install' | 'pair'>('install')
const shellHASteps = [{ value: 'install', label: '1. 安装与准备' }, { value: 'pair', label: '2. 验证与配对' }] as const
const shellHAAgentId = ref('')
const shellHAPairingVisited = ref(false)
const haGuideProfile = ref({ id: '', name: '' })
const guideServerUrl = localStorage.getItem('serverDomain') || window.location.origin
const openOxiDNSHAGuide = (agent: Agent) => {
  installationMode.value = 'ordinary'
  if (agent.deployment_method === 'docker') {
    scriptForm.value.installType = 'docker'
    scriptForm.value.dockerMode = 'oxidns'
    onDockerModeChange()
  } else {
    scriptForm.value.installType = 'shell'
    scriptForm.value.shellMode = 'single'
    scriptForm.value.type = 'oxidns'
    onServiceTypeChange()
  }
  oxidnsInstallMode.value = 'ha'
  const profileId = agent.profile_id || defaultProfileId.value
  haGuideProfile.value = { id: profileId, name: profileStore.profileName(profileId) }
  shellHAAgentId.value = agent.id
  if (agent.deployment_method !== 'docker') {
    scriptForm.value.name = agent.name
    scriptForm.value.agent_ip = agent.host
    scriptForm.value.port = agent.port
    shellHAInstallCore.value = false
  }
  shellHAStep.value = 'pair'
  scriptDialogVisible.value = true
}
const gatewayInstallContext = ref<{ profileId: string; profileName: string } | null>(null)
const openGatewayInstaller = () => {
  gatewayInstallContext.value = { profileId: profileStore.activeProfileId.value, profileName: activeProfileName.value }
  scriptDialogVisible.value = false
}
const closeGatewayInstaller = () => {
  gatewayInstallContext.value = null
  void loadAgents()
}
const dockerUpdateAgent = ref<Agent | null>(null)
const lifecyclePanelOpen = ref(false)
const installHelpOpen = ref(false)
const coreOnlyPanelOpen = ref(false)
const dockerOptionsOpen = ref(false)
const logsDialogVisible = ref(false)
const metricsDialogVisible = ref(false)
const installCommandSection = ref<HTMLElement | null>(null)
const installScript = ref('')
const scriptLoading = ref(false)
let scriptRequest = 0
const INSTALLER_DOWNLOADERS = ['curl', 'wget'] as const
const installerDownloader = ref<InstallerDownloader>('curl')
const installScriptUrl = ref('')
const installLinkExpiry = ref('')
const installCommand = computed(() => installScriptUrl.value
  ? buildInstallCommand(installScriptUrl.value, installerDownloader.value)
  : '')
const coreInstallScriptUrl = ref('')
const coreInstallLinkExpiry = ref('')
const CORE_INSTALL_TARGETS = [
  { value: 'both', label: 'Mihomo + OxiDNS' },
  { value: 'mihomo', label: '仅 Mihomo' },
  { value: 'oxidns', label: '仅 OxiDNS' },
] as const
const coreInstallTarget = ref<CoreInstallTarget>('both')
const coreInstallCommand = computed(() => coreInstallScriptUrl.value
  ? buildInstallCommand(coreInstallScriptUrl.value, installerDownloader.value)
  : '')
const coreInstallLabel = computed(() => coreInstallTarget.value === 'both' ? '两个核心' : coreInstallTarget.value === 'mihomo' ? 'Mihomo' : 'OxiDNS')
const coreInstallScript = ref('')
const coreInstallLoading = ref(false)
let coreInstallRequest = 0
const dockerComposeContent = ref('')
const dockerRunCommand = ref('')
const logs = ref('')
const logsNotice = ref('')
const logSources = ref<AgentLogSource[]>([])
const logSourcesLoading = ref(false)
const logsLoading = ref(false)
const selectedLogSource = ref('')
const customLogPath = ref('')
const currentAgent = ref<Agent | null>(null)
const currentMetricsAgent = ref<Agent | null>(null)
const metricsHistory = ref<any[]>([])
const metricsLoading = ref(false)
const logsPaneRef = ref<InstanceType<typeof AgentLogLines> | null>(null)

const createScriptForm = () => ({
  installType: 'shell',
  shellMode: 'single',
  installCore: false,
  skipIpv4Forwarding: false,
  dockerMode: 'mihomo', // mihomo, mosdns, oxidns, aio
  dockerDNSHA: false,
  haNetworkMode: 'host',
  haHostIdPath: '/etc/machine-id',
  haParent: '',
  haSubnet: '',
  haGateway: '',
  name: '',
  type: 'mihomo',
  port: 8080,
  agent_ip: '',
  config_path: '/etc/mihomo/config.yaml',
  restart_command: '',
  service_manager: 'auto',
  service_unit: '',
  service_binary: '',
  stop_command: '',
  start_command: '',
  status_command: '',
  dockerImage: '',
  containerName: 'configflow-agent',
  serviceContainerName: '',
  networkMode: 'bridge',
  // 多服务 Agent 端口配置
  mihomoAgentPort: 8080,
  mosdnsAgentPort: 8081,
  oxidnsAgentPort: 8082,
  mihomoConfigPath: '/etc/mihomo/config.yaml',
  oxidnsConfigPath: '/etc/oxidns/config.yaml',
  mihomoServiceUnit: 'mihomo',
  oxidnsServiceUnit: 'oxidns',
  mihomoServiceBinary: 'mihomo',
  oxidnsServiceBinary: 'oxidns'
})
const scriptForm = ref(createScriptForm())
const isDockerOxiDNS = computed(() => scriptForm.value.installType === 'docker' && scriptForm.value.dockerMode === 'oxidns')
const isShellOxiDNS = computed(() => scriptForm.value.installType === 'shell' && scriptForm.value.shellMode === 'single' && scriptForm.value.type === 'oxidns')
const isOxiDNSInstallation = computed(() => isDockerOxiDNS.value || isShellOxiDNS.value)
const showDockerHAGuide = computed(() => installationMode.value === 'ordinary' && isDockerOxiDNS.value && oxidnsInstallMode.value === 'ha')
const showShellHAGuide = computed(() => installationMode.value === 'ordinary' && isShellOxiDNS.value && oxidnsInstallMode.value === 'ha')
const showInstallForm = computed(() => installationMode.value === 'ordinary' && !showDockerHAGuide.value && !(showShellHAGuide.value && shellHAStep.value === 'pair'))
watch([showShellHAGuide, shellHAStep], ([shown, step]) => {
  if (!shown) shellHAPairingVisited.value = false
  else if (step === 'pair') shellHAPairingVisited.value = true
}, { flush: 'sync' })
watch([() => scriptForm.value.installType, isOxiDNSInstallation], ([type, selected], [previousType]) => {
  if (!selected || type !== previousType) oxidnsInstallMode.value = 'standard'
}, { flush: 'sync' })
watch(oxidnsInstallMode, () => {
  shellHAStep.value = 'install'
  shellHAAgentId.value = ''
  haGuideProfile.value = { id: profileStore.activeProfileId.value, name: profileStore.profileName(profileStore.activeProfileId.value) }
}, { flush: 'sync' })
const dockerDNSHA = computed(() => scriptForm.value.installType === 'docker' && scriptForm.value.dockerMode === 'oxidns' && scriptForm.value.dockerDNSHA)
const shellAio = computed(() => scriptForm.value.installType === 'shell' && scriptForm.value.shellMode === 'aio')
const singleCoreSupported = computed(() => scriptForm.value.installType === 'shell' && scriptForm.value.shellMode === 'single' && ['mihomo', 'oxidns'].includes(scriptForm.value.type))
// HA has its own opt-in so returning to ordinary installation restores the user's choice.
const shellHAInstallCore = ref(true)
watch(showShellHAGuide, shown => { if (shown) shellHAInstallCore.value = true }, { flush: 'sync' })
const installCoreChoice = computed({
  get: () => showShellHAGuide.value ? shellHAInstallCore.value : scriptForm.value.installCore,
  set: (value: boolean) => { if (showShellHAGuide.value) shellHAInstallCore.value = value; else scriptForm.value.installCore = value },
})
const singleCoreInstall = computed(() => singleCoreSupported.value && installCoreChoice.value)
const aioCoreInstall = computed(() => shellAio.value && scriptForm.value.installCore)
const combinedCoreInstall = computed(() => singleCoreInstall.value || aioCoreInstall.value)
// The displayed defaults do not overwrite the user's existing-service settings.
const singleServiceSettings = computed(() => singleCoreInstall.value ? {
  ...scriptForm.value,
  config_path: `/etc/${scriptForm.value.type}/config.yaml`, restart_command: '',
  service_manager: 'auto', service_unit: scriptForm.value.type,
  service_binary: `/usr/local/bin/${scriptForm.value.type}`,
  stop_command: '', start_command: '', status_command: ''
} : scriptForm.value)
const aioServiceSettings = computed(() => aioCoreInstall.value ? {
  ...scriptForm.value,
  mihomoConfigPath: '/etc/mihomo/config.yaml', oxidnsConfigPath: '/etc/oxidns/config.yaml',
  mihomoServiceUnit: 'mihomo', oxidnsServiceUnit: 'oxidns',
  mihomoServiceBinary: '/usr/local/bin/mihomo', oxidnsServiceBinary: '/usr/local/bin/oxidns'
} : scriptForm.value)
const shellServiceSettings = computed(() => shellAio.value ? aioServiceSettings.value : singleServiceSettings.value)
// Same rule as the server: a custom lifecycle (explicit commands, or a non-standard restart under auto-detection).
const usesCustomLifecycle = (form: { service_unit: string; type: string; restart_command: string; service_manager: string }) => {
  const unit = (form.service_unit.trim() || form.type).replace(/\.service$/, '')
  const restart = form.restart_command.trim()
  const standardRestart = !restart || [
    `systemctl restart ${unit}`, `systemctl restart ${unit}.service`, `rc-service ${unit} restart`
  ].includes(restart)
  return form.service_manager === 'command' || (form.service_manager === 'auto' && !standardRestart)
}
// Standalone Mihomo installs enable and persist IPv4 forwarding by default; AIO, Docker, OxiDNS and MosDNS never send it.
const singleMihomoForwarding = computed(() => scriptForm.value.installType === 'shell'
  && scriptForm.value.shellMode === 'single' && scriptForm.value.type === 'mihomo')
const forwardingCustomLifecycle = computed(() => singleMihomoForwarding.value && usesCustomLifecycle(singleServiceSettings.value))
watch(() => [scriptForm.value.type, scriptForm.value.installType, scriptForm.value.shellMode], () => {
  scriptForm.value.installCore = false
  scriptForm.value.skipIpv4Forwarding = false
}, { flush: 'sync' })

const resetCoreInstallCommand = () => {
  coreInstallRequest += 1
  coreInstallScriptUrl.value = ''
  coreInstallLinkExpiry.value = ''
  coreInstallScript.value = ''
  coreInstallLoading.value = false
}

watch([shellAio, aioCoreInstall, scriptDialogVisible, coreInstallTarget], resetCoreInstallCommand, { flush: 'sync' })

const installCommandUpdateMessage = '当前服务不支持短安装链接，请更新 ConfigFlow 后重试'

const readInstallCommand = (data: unknown, baseUrl: URL) => {
  if (!data || typeof data !== 'object') throw new Error(installCommandUpdateMessage)
  const { script, download_path: path, expires_at: expiresAt } = data as Record<string, unknown>
  if (typeof script !== 'string' || !script.startsWith('#!')) throw new Error(installCommandUpdateMessage)
  if (typeof path !== 'string' || !/^\/api\/agents\/i\/[A-Za-z0-9_-]{22}$/.test(path)) {
    throw new Error('服务返回的安装链接无效，请更新 ConfigFlow 后重试')
  }
  if (typeof expiresAt !== 'number' || !Number.isSafeInteger(expiresAt) || expiresAt <= Date.now() / 1000) {
    throw new Error('安装链接已失效，请重新生成')
  }
  const url = new URL(baseUrl)
  url.pathname = `${url.pathname.replace(/\/+$/, '')}${path}`
  return {
    script, url: url.toString(),
    expiry: new Date(expiresAt * 1000).toLocaleString('zh-CN', {
      month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false
    })
  }
}

const installCommandError = (error: any, fallback: string) => [404, 405].includes(error.response?.status)
  ? installCommandUpdateMessage
  : error.response?.data?.message || error.message || fallback

const generateCoreInstallCommand = async () => {
  if (coreInstallLoading.value) return
  const request = ++coreInstallRequest
  coreInstallLoading.value = true
  try {
    let baseUrl: URL
    try {
      baseUrl = new URL((localStorage.getItem('serverDomain') || window.location.origin).trim())
    } catch {
      throw new Error('服务地址无效，请先设置完整的 HTTP / HTTPS 地址')
    }
    if (!['http:', 'https:'].includes(baseUrl.protocol) || baseUrl.search || baseUrl.hash || baseUrl.username || baseUrl.password) {
      throw new Error('请将服务地址设置为不含账号、查询参数或片段的 HTTP / HTTPS 地址')
    }
    const response = await agentApi.generateCoreInstallCommand(coreInstallTarget.value)
    if (request !== coreInstallRequest || !shellAio.value || aioCoreInstall.value || !scriptDialogVisible.value) return
    const result = readInstallCommand(response.data, baseUrl)
    if (result.script.split('\n', 2)[1] !== CORE_INSTALL_INTERFACE) {
      throw new Error('安装脚本版本不支持单核心选择，请更新 ConfigFlow 后重试')
    }
    coreInstallScript.value = result.script
    coreInstallScriptUrl.value = result.url
    coreInstallLinkExpiry.value = result.expiry
    notify.success('核心安装命令已生成，请以 root 身份在目标机器执行')
  } catch (error: any) {
    if (request === coreInstallRequest) notify.error(installCommandError(error, '生成核心安装命令失败'))
  } finally {
    if (request === coreInstallRequest) coreInstallLoading.value = false
  }
}

const serviceTypeLabels: Record<Agent['service_type'], string> = {
  mihomo: 'Mihomo',
  surge: 'Surge',
  mosdns: 'MosDNS',
  oxidns: 'OxiDNS'
}

// 服务类型选项
const serviceTypeOptions = [
  { label: 'Mihomo', value: 'mihomo' },
  { label: 'MosDNS', value: 'mosdns' },
  { label: 'OxiDNS', value: 'oxidns' }
]

// 网络模式选项
const networkModeOptions = [
  { label: 'bridge (桥接)', value: 'bridge' },
  { label: 'host (主机网络)', value: 'host' }
]

// 定时刷新
let refreshTimer: number | null = null

// 生成随机端口号（范围：10000-60000）
const generateRandomPort = (): number => {
  return Math.floor(Math.random() * (60000 - 10000 + 1)) + 10000
}

// 统计数据
const onlineCount = computed(() => {
  return agents.value.filter(a => a.status === 'online').length
})

const offlineCount = computed(() => {
  return agents.value.filter(a => a.status === 'offline').length
})

// CPU 使用率图表配置
const chartPalette = useChartPalette()

const cpuChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.cpu?.usage_percent?.toFixed(1) || 0)

  return {
    title: {
      text: 'CPU 使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>CPU: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: 'CPU',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: withAlpha(chartPalette.value.primary, 0.3) },
            { offset: 1, color: withAlpha(chartPalette.value.primary, 0.05) }
          ]
        }
      },
      lineStyle: { color: chartPalette.value.primary },
      itemStyle: { color: chartPalette.value.primary }
    }]
  }
})

// 内存使用率图表配置
const memoryChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.memory?.used_percent?.toFixed(1) || 0)

  return {
    title: {
      text: '内存使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>内存: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: '内存',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: withAlpha(chartPalette.value.success, 0.3) },
            { offset: 1, color: withAlpha(chartPalette.value.success, 0.05) }
          ]
        }
      },
      lineStyle: { color: chartPalette.value.success },
      itemStyle: { color: chartPalette.value.success }
    }]
  }
})

// 磁盘使用率图表配置
const diskChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.disk?.used_percent?.toFixed(1) || 0)

  return {
    title: {
      text: '磁盘使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>磁盘: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: '磁盘',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: withAlpha(chartPalette.value.warning, 0.3) },
            { offset: 1, color: withAlpha(chartPalette.value.warning, 0.05) }
          ]
        }
      },
      lineStyle: { color: chartPalette.value.warning },
      itemStyle: { color: chartPalette.value.warning }
    }]
  }
})

// 网络速度图表配置
const networkChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const uploadData = metricsHistory.value.map(m => ((m.network?.speed_sent || 0) / 1024).toFixed(2))
  const downloadData = metricsHistory.value.map(m => ((m.network?.speed_recv || 0) / 1024).toFixed(2))

  return {
    title: {
      text: '网络速度',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        return `${params[0].name}<br/>
          上传: ${params[0].value} KB/s<br/>
          下载: ${params[1].value} KB/s`
      }
    },
    legend: {
      data: ['上传', '下载'],
      bottom: 0,
      textStyle: { fontSize: 11 }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '12%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        formatter: '{value} KB/s',
        fontSize: 11
      }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        smooth: true,
        data: uploadData,
        lineStyle: { color: chartPalette.value.primary },
        itemStyle: { color: chartPalette.value.primary }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        lineStyle: { color: chartPalette.value.success },
        itemStyle: { color: chartPalette.value.success }
      }
    ]
  }
})

// 流量统计图表配置
const trafficChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const uploadData = metricsHistory.value.map(m => ((m.network?.bytes_sent || 0) / (1024 * 1024 * 1024)).toFixed(3))
  const downloadData = metricsHistory.value.map(m => ((m.network?.bytes_recv || 0) / (1024 * 1024 * 1024)).toFixed(3))

  return {
    title: {
      text: '流量统计',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const upload = parseFloat(params[0].value).toFixed(2)
        const download = parseFloat(params[1].value).toFixed(2)
        return `${params[0].name}<br/>
          上传: ${upload} GB<br/>
          下载: ${download} GB`
      }
    },
    legend: {
      data: ['上传', '下载'],
      bottom: 0,
      textStyle: { fontSize: 11 }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '12%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        formatter: '{value} GB',
        fontSize: 11
      }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        smooth: true,
        data: uploadData,
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: withAlpha(chartPalette.value.primary, 0.3) },
              { offset: 1, color: withAlpha(chartPalette.value.primary, 0.05) }
            ]
          }
        },
        lineStyle: { color: chartPalette.value.primary },
        itemStyle: { color: chartPalette.value.primary }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: withAlpha(chartPalette.value.success, 0.3) },
              { offset: 1, color: withAlpha(chartPalette.value.success, 0.05) }
            ]
          }
        },
        lineStyle: { color: chartPalette.value.success },
        itemStyle: { color: chartPalette.value.success }
      }
    ]
  }
})

// 加载 Agent 列表
let agentsLoadGeneration = 0
const agentsLoaded = ref(false)
const trafficNow = ref(Date.now())
const loadAgents = async () => {
  trafficNow.value = Date.now()
  const generation = ++agentsLoadGeneration
  try {
    const { data } = await agentApi.getAll()
    if (generation !== agentsLoadGeneration) return
    trafficNow.value = Date.now()
    agents.value = data
    agentsLoaded.value = true
    for (const agent of agents.value) {
      if (agent.latest_upgrade) trackUpgrade(agent.id, agent.latest_upgrade)
      if (agent.latest_deployment && !deploymentBusy(agent.id)) trackDeployment(agent.id, agent.latest_deployment, false)
    }
  } catch (error) {
    if (generation !== agentsLoadGeneration) return
    notify.error('加载 Agent 列表失败')
  }
}

const profileBindingBlocked = (agentId: string) => {
  if (upgradeBusy(agentId)) return true
  const task = deployments[agentId] || agents.value.find(agent => agent.id === agentId)?.latest_deployment
  return !!task && !['succeeded', 'failed', 'rolled_back'].includes(task.status)
}

const bindAgentProfile = async (agent: Agent, profileId: string): Promise<boolean> => {
  if (profileBindingBlocked(agent.id)) {
    notify.warning('请先完成发布或恢复服务，再更改绑定配置空间')
    return false
  }
  const previousProfileId = agent.profile_id || defaultProfileId.value
  if (profileId === previousProfileId) return true
  bindingAgentId.value = agent.id
  try {
    await agentApi.bindProfile(agent.id, profileId)
    agent.profile_id = profileId
    // The previous comparison belongs to the previous binding.
    agent.content_sync = { status: 'checking' }
    void loadAgents()
    notify.success('Agent 绑定的配置空间已更新')
    return true
  } catch (error: any) {
    notify.error(error.response?.data?.message || 'Agent 绑定配置空间失败')
    return false
  } finally {
    bindingAgentId.value = null
  }
}

/* bindAgentProfile 失败时不写回 agent.profile_id，Select 会自动回到原值 */
const handleAgentProfileChange = async (agent: Agent, profileId: string) => {
  if (!profileId || profileId === (agent.profile_id || defaultProfileId.value)) return
  await bindAgentProfile(agent, profileId)
}

// 格式化时间
const formatTime = (timeStr: string) => {
  if (!timeStr) return 'N/A'
  try {
    const date = new Date(timeStr)
    const now = new Date()
    const diff = Math.floor((now.getTime() - date.getTime()) / 1000)

    if (diff < 60) return `${diff} 秒前`
    if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
    if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
    return `${Math.floor(diff / 86400)} 天前`
  } catch (e) {
    return 'N/A'
  }
}

// 判断心跳是否超过5分钟（允许删除记录）
const isHeartbeatExpired = (agent: Agent) => {
  if (!agent.last_heartbeat) return true
  try {
    const date = new Date(agent.last_heartbeat)
    const now = new Date()
    return (now.getTime() - date.getTime()) > 5 * 60 * 1000
  } catch {
    return true
  }
}

// 格式化百分比
const formatPercent = (value: number | undefined) => {
  if (value === undefined || value === null) return 'N/A'
  return `${value.toFixed(1)}%`
}

// 格式化字节数
const formatBytes = (bytes: number | undefined) => {
  if (bytes === undefined || bytes === null) return 'N/A'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`
}

// 格式化网络速度
const formatSpeed = (bytesPerSec: number | undefined) => {
  if (bytesPerSec === undefined || bytesPerSec === null) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let speed = bytesPerSec
  let unitIndex = 0
  while (speed >= 1024 && unitIndex < units.length - 1) {
    speed /= 1024
    unitIndex++
  }
  return `${speed.toFixed(1)} ${units[unitIndex]}`
}

// 格式化网络速度（别名）
const formatNetworkSpeed = formatSpeed

// 获取进度条颜色
const getProgressColor = (percent: number | undefined) => {
  if (percent === undefined || percent === null) return chartPalette.value.primary
  if (percent < 60) return chartPalette.value.success // 绿色
  if (percent < 80) return chartPalette.value.warning // 橙色
  return chartPalette.value.danger // 红色
}

/* ---------- 新 UI 的派生数据 ---------- */

const chipClass = (active: boolean): string =>
  [
    'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors',
    active
      ? 'border-primary-accent bg-primary-soft text-primary-accent dark:border-primary-accent/40 dark:bg-primary-soft/50 dark:text-foreground'
      : 'border-input bg-card text-muted-foreground hover:border-border-strong dark:border-border/50 dark:bg-background/40'
  ].join(' ')

const INSTALL_TYPES = [
  { value: 'shell', label: 'Shell 安装' },
  { value: 'docker', label: 'Docker 容器' }
]

const SHELL_MODES = [
  { value: 'single', label: '单服务' },
  { value: 'aio', label: 'Mihomo + OxiDNS 同机' }
]

const DOCKER_MODES = [
  { value: 'mihomo', label: 'Mihomo (Clash)' },
  { value: 'mosdns', label: 'MosDNS' },
  { value: 'oxidns', label: 'OxiDNS' },
  { value: 'aio', label: 'All-in-One（三合一）' }
]

const serviceBinaryCopy = optionCopy('指向现有服务使用的核心程序，默认可留空。', '接管已有核心，或程序不在默认搜索位置时填写。', '可用程序名或绝对路径，必须与现有服务实际使用的核心一致。留空按程序名查找；同时安装核心时使用默认路径，无需自行填写。')

const installTypeCopy = computed(() =>
  scriptForm.value.installType === 'shell'
    ? optionCopy('直接在 Linux 上安装，也可管理已有核心服务。', '目标使用 Linux，且希望直接管理系统中的核心服务。', '将 Agent 安装为系统服务，可接管已有核心。Mihomo 和 OxiDNS 可按需勾选补装；是否同机管理两项服务，由下方 Shell 模式决定。')
    : optionCopy('用容器同时运行 Agent 和所选核心服务。', '目标设备能运行 Docker，且希望使用容器部署。', '镜像内置所选核心，无需另装 Mihomo、MosDNS 或 OxiDNS。按下方模式选择单项核心或三合一，并核对网络模式和访问端口。')
)

const dockerModeCopy = computed(
  () =>
    ({
      mihomo: optionCopy('需要代理服务时选用，容器已内置 Mihomo。', '只需要代理核心及其管理 Agent，希望通过容器运行。', '同一容器运行 Agent 和 Mihomo，不必另装核心。可接收和更新配置；不会同时部署独立的 OxiDNS 或 MosDNS 服务。'),
      oxidns: optionCopy('需要域名解析服务时选用，容器已内置 OxiDNS。', '希望通过容器运行 OxiDNS，并由 Agent 管理配置。', '同一容器包含 Agent 和 OxiDNS，无需另装核心。它提供 DNS 解析服务；是否需要代理核心，取决于实际分流方案。'),
      mosdns: optionCopy('使用 MosDNS 解析方案时选用，容器内置核心。', '现有配置面向 MosDNS，且希望通过容器部署。', '同一容器运行 Agent 和 MosDNS，可接收配置并管理服务。无需在宿主机另装 MosDNS，不会自动附带 Mihomo 代理核心。'),
      aio: optionCopy('代理和 DNS 同机运行，一个容器即可部署。', '希望在同一容器内同时运行 Mihomo 与 OxiDNS。', '同一容器运行 Agent + Mihomo + OxiDNS。不会自动完成宿主机网关转发；如需 Linux 网关，请使用专用安装向导。')
    })[scriptForm.value.dockerMode as 'mihomo' | 'oxidns' | 'mosdns' | 'aio']
)

const DOCKER_NOTES: Record<string, { title: string; lines: string[] }> = {
  mihomo: {
    title: 'Docker Mihomo Agent 使用说明',
    lines: [
      '安装后即可使用 Mihomo 代理服务',
      'Agent 与 Mihomo 在同一容器运行，无需另外安装 Mihomo',
      '可自动获取和更新配置，也可在此页面重启服务',
      '代理端口：7890 (HTTP)、7891 (SOCKS5)、9090 (API)'
    ]
  },
  oxidns: {
    title: 'Docker OxiDNS Agent 使用说明',
    lines: [
      'Agent 与 OxiDNS 在同一容器运行',
      '支持完整规则包发布、原生配置检查与失败回滚',
      'DNS 端口：53 (TCP/UDP)；Agent 端口：8082',
      '如果与 MosDNS 同机运行，请使用不同 DNS 监听端口或不同宿主机端口映射'
    ]
  },
  mosdns: {
    title: 'Docker MosDNS Agent 使用说明',
    lines: [
      '安装后即可使用 MosDNS 解析域名',
      'Agent 与 MosDNS 在同一容器运行，无需另外安装 MosDNS',
      '可自动获取和更新配置，也可在此页面重启服务',
      'DNS 端口：53 (TCP/UDP)'
    ]
  },
  aio: {
    title: 'Docker All-in-One Agent 使用说明',
    lines: [
      'Mihomo 负责代理，OxiDNS 负责 DNS 解析',
      'Agent 默认端口：Mihomo 8080，OxiDNS 8082',
      '可通过环境变量 ENABLE_MIHOMO / ENABLE_OXIDNS 控制启用哪些服务',
      '配置保存在 ./aio_data/mihomo 与 ./aio_data/oxidns 目录'
    ]
  }
}

const dockerNote = computed(
  () => DOCKER_NOTES[scriptForm.value.dockerMode as string] ?? DOCKER_NOTES.mihomo
)

const scriptPanelOpen = ref(false)

/** 生成结果按安装方式给出可复制的命令块，避免模板里重复四段几乎相同的结构 */
const commandBlocks = computed(() => {
  if (scriptForm.value.installType === 'shell') {
    return [{
      title: 'Linux（systemd / OpenRC）',
      note: combinedCoreInstall.value ? '以 root 执行；完整下载成功后，先安装核心，再安装 Agent。' : '以 root 执行；完整下载成功后安装 Agent。',
      value: installCommand.value, rows: 3, copy: copyCommand
    }]
  }
  return [
    {
      title: 'Docker Run 命令',
      note: '推荐',
      value: dockerRunCommand.value,
      rows: 8,
      copy: copyDockerRun
    },
    {
      title: 'docker-compose.yml',
      note: '可选，保存后执行 docker compose up -d',
      value: dockerComposeContent.value,
      rows: 15,
      copy: copyDockerCompose
    }
  ]
})

/** 卡片内的资源占用条 */
const metricsOf = (agent: any) => {
  const m = agent.system_metrics || {}
  return [
    {
      label: 'CPU',
      percent: m.cpu?.usage_percent || 0,
      text: formatPercent(m.cpu?.usage_percent),
      color: getProgressColor(m.cpu?.usage_percent),
      detail: ''
    },
    {
      label: '内存',
      percent: m.memory?.used_percent || 0,
      text: formatPercent(m.memory?.used_percent),
      color: getProgressColor(m.memory?.used_percent),
      detail: `${formatBytes(m.memory?.used)} / ${formatBytes(m.memory?.total)}`
    },
    {
      label: '磁盘',
      percent: m.disk?.used_percent || 0,
      text: formatPercent(m.disk?.used_percent),
      color: getProgressColor(m.disk?.used_percent),
      detail: `${formatBytes(m.disk?.used)} / ${formatBytes(m.disk?.total)}`
    }
  ]
}

const metricsSummary = computed(() => {
  const m = currentMetricsAgent.value?.system_metrics
  if (!m) return []
  return [
    {
      label: 'CPU 使用率',
      value: formatPercent(m.cpu?.usage_percent),
      color: getProgressColor(m.cpu?.usage_percent),
      detail: `${m.cpu?.core_count ?? '—'} 核`
    },
    {
      label: '内存使用率',
      value: formatPercent(m.memory?.used_percent),
      color: getProgressColor(m.memory?.used_percent),
      detail: `${formatBytes(m.memory?.used)} / ${formatBytes(m.memory?.total)}`
    },
    {
      label: '磁盘使用率',
      value: formatPercent(m.disk?.used_percent),
      color: getProgressColor(m.disk?.used_percent),
      detail: `${formatBytes(m.disk?.used)} / ${formatBytes(m.disk?.total)}`
    },
    {
      label: '网络速度',
      value: `↑ ${formatNetworkSpeed(m.network?.speed_sent)}`,
      color: 'var(--primary-accent)',
      detail: `↓ ${formatNetworkSpeed(m.network?.speed_recv)}`
    },
    {
      label: '总流量',
      value: `↑ ${formatBytes(m.network?.bytes_sent)}`,
      color: 'var(--success-accent)',
      detail: `↓ ${formatBytes(m.network?.bytes_recv)}`
    }
  ]
})

const metricsCharts = computed(() => [
  { key: 'cpu', option: cpuChartOption.value },
  { key: 'memory', option: memoryChartOption.value },
  { key: 'network', option: networkChartOption.value },
  { key: 'traffic', option: trafficChartOption.value },
  { key: 'disk', option: diskChartOption.value }
])

// 显示监控详情
const showMetricsDetail = async (agent: any) => {
  currentMetricsAgent.value = agent
  metricsDialogVisible.value = true
  metricsLoading.value = true

  try {
    // 获取最近 24 小时的历史数据
    const response = await api.get(`/agents/${agent.id}/metrics/history?hours=24`)
    if (response.data.success) {
      metricsHistory.value = response.data.data.history || []
    } else {
      notify.error('获取监控历史数据失败')
      metricsHistory.value = []
    }
  } catch (error) {
    console.error('Error fetching metrics history:', error)
    notify.error('获取监控历史数据失败')
    metricsHistory.value = []
  } finally {
    metricsLoading.value = false
  }
}

// 安装类型变化时重置相关字段
const onInstallTypeChange = () => {
  scriptForm.value.dockerDNSHA = false
  // 清空之前生成的命令
  installScriptUrl.value = ''
  installLinkExpiry.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''

  if (scriptForm.value.installType === 'docker') {
    // 切换到 Docker 模式时，确保有默认值
    if (!scriptForm.value.containerName) {
      scriptForm.value.containerName = 'configflow-agent'
    }
    if (!scriptForm.value.networkMode) {
      scriptForm.value.networkMode = 'bridge'
    }

    // 触发 docker mode change 以设置正确的默认值
    onDockerModeChange()
  }
}

// Docker 模式变化时
const onDockerModeChange = () => {
  scriptForm.value.dockerDNSHA = false
  installScriptUrl.value = ''
  installLinkExpiry.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''

  if (scriptForm.value.dockerMode === 'mihomo') {
    // mihomo 模式
    scriptForm.value.type = 'mihomo'
  } else if (scriptForm.value.dockerMode === 'oxidns') {
    scriptForm.value.type = 'oxidns'
  } else if (scriptForm.value.dockerMode === 'mosdns') {
    // mosdns 模式
    scriptForm.value.type = 'mosdns'
  } else {
    // aio 模式
    scriptForm.value.type = 'mihomo'
  }
}

// 服务类型变化时更新默认配置路径和重启命令
const onServiceTypeChange = () => {
  Object.assign(scriptForm.value, {
    service_manager: 'auto', service_unit: '', service_binary: '',
    stop_command: '', start_command: '', status_command: ''
  })
  if (scriptForm.value.type === 'mihomo') {
    scriptForm.value.config_path = '/etc/mihomo/config.yaml'
    scriptForm.value.restart_command = ''
    // 如果是 Docker 模式，更新默认服务容器名称
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'mihomo'
    }
  } else if (scriptForm.value.type === 'oxidns') {
    scriptForm.value.config_path = '/etc/oxidns/config.yaml'
    scriptForm.value.restart_command = ''
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'oxidns'
    }
  } else if (scriptForm.value.type === 'mosdns') {
    scriptForm.value.config_path = '/etc/mosdns/config.yaml'
    scriptForm.value.restart_command = ''
    // 如果是 Docker 模式，更新默认服务容器名称
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'mosdns'
    }
  }
}

// 处理生成脚本按钮点击
const handleGenerateScript = () => {
  showGenerateScriptDialog()
}

// 显示生成脚本对话框
const showGenerateScriptDialog = () => {
  installationMode.value = 'ordinary'
  oxidnsInstallMode.value = 'standard'
  shellHAStep.value = 'install'
  shellHAAgentId.value = ''
  // 清空其他值
  installScript.value = ''
  installScriptUrl.value = ''
  installLinkExpiry.value = ''
  installerDownloader.value = 'curl'
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  scriptPanelOpen.value = false

  // Reset every installation field, including both Docker Agent ports.
  scriptForm.value = { ...createScriptForm(), port: generateRandomPort() }
  lifecyclePanelOpen.value = false
  installHelpOpen.value = false
  coreOnlyPanelOpen.value = false
  dockerOptionsOpen.value = false

  // 打开对话框 - v-if 会确保表单完全重新渲染
  scriptDialogVisible.value = true
}

// 重置表单
const resetForm = () => {
  scriptRequest += 1
  scriptLoading.value = false
  installScript.value = ''
  installScriptUrl.value = ''
  installLinkExpiry.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  scriptPanelOpen.value = false
}

// Moving to pairing invalidates unfinished generation but retains any completed command for returning.
watch(shellHAStep, () => { scriptRequest++; scriptLoading.value = false }, { flush: 'sync' })
watch([installationMode, oxidnsInstallMode, shellHAInstallCore], resetForm, { flush: 'sync' })
watch(scriptForm, resetForm, { deep: true, flush: 'sync' })
watch(scriptDialogVisible, resetForm, { flush: 'sync' })
watch([installScriptUrl, dockerComposeContent, dockerRunCommand], async (commands) => {
  if (!commands.some(Boolean)) return
  await nextTick()
  installCommandSection.value?.scrollIntoView?.({ block: 'start', behavior: 'auto' })
})

// 生成安装脚本
const generateScript = async () => {
  if (scriptLoading.value) return
  if (!scriptForm.value.name.trim()) {
    notify.warning('请输入 Agent 名称')
    return
  }
  if (dockerDNSHA.value) {
    const fields = scriptForm.value.haNetworkMode === 'host'
      ? [scriptForm.value.agent_ip] : [scriptForm.value.agent_ip, scriptForm.value.haParent, scriptForm.value.haSubnet]
    if (!fields.every(value => value.trim())) {
      notify.warning(scriptForm.value.haNetworkMode === 'host' ? '请填写宿主机固定 IPv4' : '请填写容器固定 IPv4、宿主机网卡和局域网网段')
      return
    }
  }

  // Shell 安装时检查配置文件路径是否为文件而非目录
  if (scriptForm.value.installType === 'shell') {
    if (!/^[A-Za-z0-9_-]+$/.test(scriptForm.value.name.trim())) {
      notify.warning('Agent 名称只能包含字母、数字、短横线或下划线')
      return
    }
    const form = shellServiceSettings.value
    const configPaths = shellAio.value ? [form.mihomoConfigPath, form.oxidnsConfigPath] : [form.config_path]
    if (configPaths.some(path => path.trim() && !path.trim().match(/\.\w+$/))) {
      lifecyclePanelOpen.value = true
      notify.warning('配置文件路径应指向一个文件（如 config.yaml），而不是文件夹')
      return
    }
    if (shellAio.value) {
      if (![form.mihomoAgentPort, form.oxidnsAgentPort].every(port => Number.isInteger(port) && port >= 1024 && port <= 65535)) {
        notify.warning('Agent 端口应为 1024 到 65535 之间的整数')
        return
      }
      if (form.mihomoAgentPort === form.oxidnsAgentPort) {
        notify.warning('两个 Agent 必须使用不同的端口')
        return
      }
      if (aioCoreInstall.value && [form.mihomoAgentPort, form.oxidnsAgentPort].includes(1053)) {
        notify.warning('Mihomo DNS 使用 1053，请为 Agent 选择其他端口')
        return
      }
    } else {
      if (singleCoreInstall.value && (!Number.isInteger(form.port) || form.port < 1024 || form.port > 65535)) {
        notify.warning('Agent 端口应为 1024 到 65535 之间的整数')
        return
      }
      if (singleCoreInstall.value && form.type === 'mihomo' && form.port === 1053) {
        notify.warning('Mihomo DNS 使用 1053，请为 Agent 选择其他端口')
        return
      }
      if (usesCustomLifecycle(form) && ![form.stop_command, form.start_command, form.status_command].every(value => value.trim())) {
        lifecyclePanelOpen.value = true
        notify.warning('自定义服务需要填写停止、启动和状态检查命令，才能在发布失败时回滚')
        return
      }
    }
  }

  const request = ++scriptRequest
  const includeCore = combinedCoreInstall.value
  const prepareDNSHA = showShellHAGuide.value
  scriptLoading.value = true
  const loadingToast = notify.loading(scriptForm.value.installType === 'docker' ? '正在生成 Docker 部署命令...' : '正在生成安装命令...')

  try {
    if (scriptForm.value.installType === 'docker') {
      // 生成 Docker Compose 和 Docker Run
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin

      const dockerParams = {
        server_url: serverUrl,
        agent_name: scriptForm.value.containerName || scriptForm.value.name,
        agent_ip: scriptForm.value.agent_ip || '',
        network_mode: dockerDNSHA.value ? scriptForm.value.haNetworkMode : scriptForm.value.networkMode,
        ...(dockerDNSHA.value ? {
          enable_dns_ha: true,
          ha_host_id_path: scriptForm.value.haHostIdPath.trim(),
          ...(scriptForm.value.haNetworkMode === 'macvlan' ? {
            ha_parent: scriptForm.value.haParent.trim(),
            ha_subnet: scriptForm.value.haSubnet.trim(),
            ha_gateway: scriptForm.value.haGateway.trim(),
          } : {}),
        } : {}),
        enable_mihomo: scriptForm.value.dockerMode === 'mihomo' || scriptForm.value.dockerMode === 'aio',
        enable_oxidns: scriptForm.value.dockerMode === 'oxidns' || scriptForm.value.dockerMode === 'aio',
        oxidns_port: scriptForm.value.oxidnsAgentPort,
        enable_mosdns: scriptForm.value.dockerMode === 'mosdns',
        mihomo_port: scriptForm.value.mihomoAgentPort,
        mosdns_port: scriptForm.value.mosdnsAgentPort,
        // 根据模式设置数据目录
        data_dir: scriptForm.value.dockerMode === 'aio' ? './aio_data' :
                  (scriptForm.value.dockerMode === 'oxidns' ? './oxidns_data' : scriptForm.value.dockerMode === 'mosdns' ? './mosdns_data' : './mihomo_data')
      }

      // 使用统一的 Docker API
      const [composeResponse, runResponse] = await Promise.all([
        api.get('/agents/docker-agent-compose', { params: dockerParams }),
        api.get('/agents/docker-agent-run', { params: dockerParams })
      ])

      if (request !== scriptRequest || !scriptDialogVisible.value) return
      dockerComposeContent.value = composeResponse.data
      dockerRunCommand.value = runResponse.data
      notify.success('Docker 部署命令已生成，请复制到目标机器执行')
    } else {
      let serverUrl = localStorage.getItem('serverDomain') || window.location.origin
      let parsed: URL
      try { parsed = new URL(serverUrl.trim()) } catch { throw new Error('服务地址无效，请先设置完整的 HTTP / HTTPS 地址') }
      if (!['http:', 'https:'].includes(parsed.protocol) || parsed.search || parsed.hash || parsed.username || parsed.password) {
        throw new Error('请将服务地址设置为不含账号、查询参数或片段的 HTTP / HTTPS 地址')
      }
      serverUrl = parsed.toString().replace(/\/+$/, '')
      const form = { ...shellServiceSettings.value }
      const lifecycleParams: Record<string, string> = {}
      if (form.service_manager !== 'auto') lifecycleParams.service_manager = form.service_manager
      for (const key of ['service_unit', 'service_binary', 'stop_command', 'start_command', 'status_command'] as const) {
        const value = form[key].trim()
        if (value) lifecycleParams[key] = value
      }
      const shellParams: AgentInstallScriptParams = shellAio.value ? {
        name: form.name.trim(),
        type: 'aio',
        agent_ip: form.agent_ip.trim(),
        server_url: serverUrl,
        mihomo_agent_port: form.mihomoAgentPort,
        oxidns_agent_port: form.oxidnsAgentPort,
        mihomo_config_path: form.mihomoConfigPath.trim() || '/etc/mihomo/config.yaml',
        oxidns_config_path: form.oxidnsConfigPath.trim() || '/etc/oxidns/config.yaml',
        mihomo_service_unit: form.mihomoServiceUnit.trim() || 'mihomo',
        oxidns_service_unit: form.oxidnsServiceUnit.trim() || 'oxidns',
        mihomo_service_binary: form.mihomoServiceBinary.trim() || 'mihomo',
        oxidns_service_binary: form.oxidnsServiceBinary.trim() || 'oxidns',
        ...(includeCore ? { install_core: true } : {})
      } : {
        name: form.name.trim(),
        type: form.type,
        port: form.port,
        agent_ip: form.agent_ip.trim(),
        config_path: form.config_path.trim(),
        restart_command: form.restart_command.trim(),
        server_url: serverUrl,
        ...lifecycleParams,
        ...(includeCore ? { install_core: true } : {}),
        ...(prepareDNSHA ? { prepare_dns_ha: true } : {}),
        ...(singleMihomoForwarding.value && scriptForm.value.skipIpv4Forwarding && !forwardingCustomLifecycle.value ? { ipv4_forwarding: false } : {})
      }
      const response = await agentApi.generateInstallCommand(shellParams)
      if (request !== scriptRequest || !scriptDialogVisible.value) return
      const result = readInstallCommand(response.data, parsed)
      if (includeCore && result.script.split('\n', 2)[1] !== AGENT_CORE_INSTALL_INTERFACE) {
        throw new Error('安装脚本版本不支持同时安装核心，请更新 ConfigFlow 后重试')
      }

      if (prepareDNSHA && !result.script.split('\n').slice(1, 4).includes(DNS_HA_PREPARE_INTERFACE)) {
        throw new Error('安装脚本版本不支持自动准备高可用环境，请更新 ConfigFlow 后重试')
      }

      // The preview and download are the same immutable server-generated snapshot.
      installScript.value = result.script
      installScriptUrl.value = result.url
      installLinkExpiry.value = result.expiry

      notify.success('安装命令已生成，请复制到目标机器执行')
    }
  } catch (error: any) {
    if (request === scriptRequest) {
      const errorMsg = scriptForm.value.installType === 'shell'
        ? installCommandError(error, '生成脚本失败')
        : error.response?.data?.message || error.message || '生成脚本失败'
      notify.error(errorMsg)
    }
  } finally {
    if (request === scriptRequest) scriptLoading.value = false
    notify.dismiss(loadingToast)
  }
}

const copyAgentContent = async (text: string, message: string) => {
  if (!text) return
  try {
    await copyText(text)
    notify.success(message)
  } catch {
    notify.error('复制失败，请手动复制')
  }
}

const copyCommand = () => copyAgentContent(installCommand.value, '命令已复制到剪贴板')
const copyDockerCompose = () => copyAgentContent(dockerComposeContent.value, 'Docker Compose 已复制到剪贴板')
const copyDockerRun = () => copyAgentContent(dockerRunCommand.value, 'Docker Run 命令已复制到剪贴板')

// 推送配置
const submissionFailures = ref<Record<string, Partial<AgentDeployment>>>({})
const pushingIds = ref<Set<string>>(new Set())
const pushingAll = ref(false)

type PushOutcome = 'submitted' | 'done' | 'failed' | 'busy'

/**
 * 推送：mihomo / mosdns / oxidns 走事务发布，结果由 useAgentDeployments 跟踪并提示，
 * 这里只处理「提交」本身的成败。
 */
const pushOne = async (agent: Agent, quiet = false): Promise<PushOutcome> => {
  if (deploymentBusy(agent.id)) return 'busy'
  delete submissionFailures.value[agent.id]
  const deploymentId = createDeploymentId()
  const transactional = agent.service_type !== 'surge'
  if (transactional && !beginSubmission(agent.id, deploymentId)) return 'busy'
  pushingIds.value = new Set([...pushingIds.value, agent.id])
  try {
    const { data } = await agentApi.pushConfig(agent.id, deploymentId)
    if (data.deployment_id === deploymentId && typeof data.status === 'string') {
      endSubmission(agent.id, deploymentId, data)
      return 'submitted'
    }
    if (transactional) {
      endSubmission(agent.id, deploymentId, 'unknown')
      return 'submitted'
    }
    if (data?.success === false || data?.restart?.success === false) {
      submissionFailures.value[agent.id] = { ...data, error: data.restart?.success === false ? `配置已写入，但重启失败：${data.restart.message || '服务未确认启动。'}` : data.error || data.message, error_details: data.restart?.error_details || data.error_details }
      if (!quiet) notify.error(`「${agent.name}」配置发布失败`, deploymentErrorSummary(submissionFailures.value[agent.id]))
      return 'failed'
    }
    return 'done'
  } catch (error: any) {
    if (error.response?.data?.deployment_id) {
      if (error.response.data.deployment_id === deploymentId) {
        endSubmission(agent.id, deploymentId, error.response.data)
        return 'submitted'
      }
      endSubmission(agent.id, deploymentId, 'rejected')
      trackDeployment(agent.id, error.response.data)
      return 'submitted'
    }
    if (transactional && (!error.response || error.response.status >= 500)) {
      endSubmission(agent.id, deploymentId, 'unknown')
      return 'submitted'
    }
    if (transactional && endSubmission(agent.id, deploymentId, 'rejected')) return 'submitted'
    submissionFailures.value[agent.id] = { ...error.response?.data, error: error.response?.data?.error || error.response?.data?.message || '发布请求失败，请重试。' }
    if (!quiet) notify.error(`「${agent.name}」配置发布失败`, deploymentErrorSummary(submissionFailures.value[agent.id]))
    return 'failed'
  } finally {
    const next = new Set(pushingIds.value)
    next.delete(agent.id)
    pushingIds.value = next
  }
}

// 卡片「推送」与「待推送的变更」窗口里的「推送」共用同一可用条件。
const pushBlocked = (agent: Agent) => pushingIds.value.has(agent.id) || upgradeBusy(agent.id) || deploymentBusy(agent.id)
  || deployments[agent.id]?.status === 'rollback_failed'
const pushDiffAgentId = ref<string | null>(null)
const pushDiffAgent = computed(() => agents.value.find(agent => agent.id === pushDiffAgentId.value))
// 关闭窗口后沿用卡片的推送流程，进度显示在卡片上。
const pushFromDiff = (agent: Agent) => {
  if (agent.status !== 'online' || pushBlocked(agent)) return
  pushDiffAgentId.value = null
  void pushConfig(agent)
}

const pushConfig = async (agent: Agent) => {
  if (deploymentBusy(agent.id)) return
  if ((await pushOne(agent)) === 'done') {
    notify.success(`已推送至「${agent.name}」`)
    void loadAgents()
  }
}

const pushAll = async () => {
  const online = agents.value.filter(a => a.status === 'online' && !deploymentBusy(a.id))
  if (!online.length || pushingAll.value) return
  pushingAll.value = true
  const results = await submitAgentRequests(uniquePublicationTargets(online), agent => pushOne(agent, true))
  pushingAll.value = false
  const sent = results.filter(r => r === 'submitted' || r === 'done').length
  const failed = results.filter(r => r === 'failed').length
  const skipped = agents.value.length - online.length
  const tail = `${failed ? ` · ${failed} 台失败` : ''}${skipped ? ` · ${skipped} 台离线或发布中跳过` : ''}`
  if (failed) notify.warning(`已提交 ${sent} 项发布${tail}`)
  else notify.success(`已提交 ${sent} 项发布${tail}`)
  void loadAgents()
}

// 重启 Agent
const restartAgent = async (agent: Agent) => {
  if (deployments[agent.id]?.status === 'ready') {
    await activateDeployment(agent.id)
    return
  }
  const ok = await confirm('确定要重启此 Agent 的服务吗？服务将会短暂中断。', {
    title: '重启服务',
    confirmText: '重启'
  })
  if (!ok) return

  try {
    const loadingToast = notify.loading('正在重启服务...')

    try {
      await agentApi.restart(agent.id)
      notify.success('服务重启成功')
    } finally {
      notify.dismiss(loadingToast)
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || '服务重启失败')
  }
}

const updateAgent = async (agent: Agent) => {
  if (agent.deployment_method === 'docker') {
    dockerUpdateAgent.value = agent
    return
  }
  if (upgradeBusy(agent.id) || deploymentBusy(agent.id)) return
  const ok = await confirm('更新将保留身份和核心配置，自动完成迁移并重启 Agent。网页会持续查询最终结果。', {
    title: '更新 Agent', confirmText: '立即更新'
  })
  if (!ok) return
  try {
    await startUpgrade(agent.id)
  } catch (error: any) {
    notify.error(error.response?.data?.message || '更新请求未确认，请查询更新结果')
    void loadAgents()
  }
}

// 卸载 Agent
const uninstallAgent = async (agent: Agent) => {
  const ok = await confirmDanger(
    `确定要卸载远程服务器上的 Agent「${agent.name}」吗？\n\n此操作将：\n• 停止 Agent 服务\n• 删除 Agent 程序文件\n• 删除服务配置（systemd/OpenRC）\n• 从管理列表中移除\n\n此操作不可恢复。`,
    { title: '卸载 Agent', confirmText: '确定卸载' }
  )
  if (!ok) return

  try {
    const loadingToast = notify.loading('正在卸载 Agent...')

    try {
      await agentApi.uninstall(agent.id)
      notify.success('Agent 卸载命令已发送，远程服务器正在执行卸载...')
      // 等待几秒后刷新列表
      setTimeout(() => {
        loadAgents()
      }, 3000)
    } finally {
      notify.dismiss(loadingToast)
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || '卸载失败')
  }
}

// 删除 Agent 记录
const deleteAgent = async (agent: Agent) => {
  const ok = await confirmDanger(
    `确定删除 Agent「${agent.name}」的管理记录吗？记录删除后无法恢复，但不会卸载远程机器上的 Agent 程序。如需移除程序，请使用「卸载 Agent」。`,
    { title: '删除记录', confirmText: '确定删除' }
  )
  if (!ok) return

  try {
    await agentApi.delete(agent.id)
    notify.success('记录删除成功')
    loadAgents()
  } catch (error: any) {
    notify.error(error.response?.data?.message || '删除失败')
  }
}

// 旧版 Agent 不提供日志来源列表时，退回由 Agent 自动检测
const LEGACY_LOG_SOURCE: AgentLogSource = { id: 'auto', label: '自动检测', kind: 'auto', clearable: false }

const currentLogSource = computed(() => logSources.value.find(source => source.id === selectedLogSource.value))

const currentLogLocation = computed(() => {
  const source = currentLogSource.value
  if (!source) return ''
  if (source.kind === 'journal') return `journalctl -u ${source.unit}`
  return source.path || ''
})

const canClearLog = computed(() => {
  if (selectedLogSource.value === 'custom') return Boolean(customLogPath.value)
  return Boolean(currentLogSource.value?.clearable)
})

// 查看日志
const viewLogs = async (agent: Agent) => {
  currentAgent.value = agent
  logs.value = ''
  logsNotice.value = ''
  logSources.value = []
  selectedLogSource.value = ''
  customLogPath.value = ''
  logsDialogVisible.value = true
  await loadLogSources()
  // 来源列表读取失败时保留失败原因，不被「请输入自定义日志路径」覆盖
  if (!logsNotice.value) await loadLogs()
}

// 读取 Agent 解析出的日志来源，默认选中第一个可读来源
const loadLogSources = async () => {
  if (!currentAgent.value) return
  logSourcesLoading.value = true
  try {
    const { data } = await agentApi.getLogSources(currentAgent.value.id)
    const sources: AgentLogSource[] = data.legacy ? [LEGACY_LOG_SOURCE] : data.sources || []
    logSources.value = sources
    selectedLogSource.value = (sources.find(source => source.kind !== 'unavailable') || sources[0])?.id || 'custom'
  } catch (error: any) {
    logSources.value = []
    selectedLogSource.value = 'custom'
    logsNotice.value = `获取日志来源失败: ${error.response?.data?.message || error.message || '未知错误'}`
  } finally {
    logSourcesLoading.value = false
  }
}

// 加载日志
const loadLogs = async () => {
  if (!currentAgent.value || !selectedLogSource.value) return

  const source = currentLogSource.value
  logsNotice.value = ''
  if (selectedLogSource.value === 'custom' && !customLogPath.value) {
    logsNotice.value = '请输入自定义日志路径'
    return
  }
  if (source?.kind === 'unavailable') {
    logsNotice.value = source.path ? `${source.note}\n${source.path}` : source.note || '该日志不可读'
    return
  }

  logsLoading.value = true
  try {
    const params = selectedLogSource.value === 'custom'
      ? { logPath: customLogPath.value }
      : source?.kind === 'auto' ? {} : { source: selectedLogSource.value }
    const { data } = await agentApi.getLogs(currentAgent.value.id, 200, params)
    if (data.success) {
      logs.value = [data.logs, data.error_logs].filter(Boolean).join('\n')
    } else {
      logsNotice.value = `获取日志失败: ${data.message}`
    }

    // 等待DOM更新后滚动到底部
    await nextTick()
    scrollToBottom()
  } catch (error: any) {
    logsNotice.value = `获取日志失败: ${error.response?.data?.message || error.message || '未知错误'}`
  } finally {
    logsLoading.value = false
  }
}

// 滚动到日志底部
const scrollToBottom = () => logsPaneRef.value?.scrollToBottom()

// 刷新日志
const refreshLogs = async () => {
  await loadLogs()
  if (!logsNotice.value) notify.success('日志已刷新')
}

// 清空日志
const clearLogs = async () => {
  if (!currentAgent.value || !canClearLog.value) return

  const ok = await confirmDanger('确定要清空该日志文件吗？此操作不可恢复。', {
    title: '清空日志',
    confirmText: '确定清空'
  })
  if (!ok) return

  const loadingToast = notify.loading('正在清空日志...')

  try {
    const target = selectedLogSource.value === 'custom'
      ? { logPath: customLogPath.value }
      : { source: selectedLogSource.value }
    const { data } = await agentApi.clearLog(currentAgent.value.id, target)
    if (data.success) {
      notify.success('日志已清空')
      await loadLogs()
    } else {
      notify.error(data.message || '清空日志失败')
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || '清空日志失败')
  } finally {
    notify.dismiss(loadingToast)
  }
}

// 日志来源切换
const onLogSourceChange = async (value: string) => {
  selectedLogSource.value = value
  logs.value = ''
  logsNotice.value = ''
  if (value !== 'custom') {
    customLogPath.value = ''
    await loadLogs()
  }
}

// 启动定时刷新
const startAutoRefresh = () => {
  // 每 10 秒刷新一次
  refreshTimer = window.setInterval(() => {
    loadAgents()
  }, 10000)
}

// 停止定时刷新
const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

/* ---------- 配置版本 ---------- */
const profileStore = useProfileStore()

const revisionOf = (profileId?: string) => {
  const revision = profiles.value.find(p => p.id === (profileId || defaultProfileId.value))?.revision
  return typeof revision === 'number' && Number.isSafeInteger(revision) && revision >= 0 ? revision : undefined
}
const activeProfileName = computed(
  () => profileStore.activeProfile.value?.name || profileStore.activeProfileId.value
)

const syncOf = (agent: Agent) => getAgentConfigSync(agent, revisionOf(agent.profile_id), deployments[agent.id])
const syncTone = (agent: Agent) => {
  const tone = syncOf(agent).tone
  return tone === 'success' ? 'text-success-accent' : tone === 'warning' ? 'text-warning-accent' : 'text-muted-foreground'
}

/* 推送与跨页动作 */
const router = useRouter()
const runPending = () => {
  if (consumeAction(router, 'push-all')) pushAll()
}
watch(() => router?.currentRoute.value.query.run, run => run === 'push-all' && runPending())
useSettingLink({
  keys: ['agent', 'profile', 'section', 'deployment'], ready: () => agentsLoaded.value,
  matches: query => !!queryText(query.agent) && ['deployment', 'rule-sync', 'dns-follow'].includes(queryText(query.section)),
  open: async query => {
    const agent = agents.value.find(item => item.id === queryText(query.agent))
    if (!agent) { notify.warning('对应设备不存在或已删除。'); return }
    if (queryText(query.profile) && queryText(query.profile) !== (agent.profile_id || defaultProfileId.value)) {
      notify.warning('设备所属配置空间已变化，请从 Agent 页面确认后再操作。'); return
    }
    if (agentSettings.value || scriptDialogVisible.value || logsDialogVisible.value || metricsDialogVisible.value) {
      notify.warning('请先保存或关闭当前编辑窗口。'); return
    }
    await nextTick()
    const card = Array.from(document.querySelectorAll<HTMLElement>('[data-notification-agent]'))
      .find(element => element.dataset.notificationAgent === agent.id)
    if (query.section === 'deployment') {
      notificationDeploymentId.value = queryText(query.deployment)
      deploymentHistoryAgentId.value = agent.id
    } else if (query.section === 'dns-follow') {
      dnsFollowRefs.get(agent.id)?.showSettings()
    } else {
      await focusSetting(card?.querySelector<HTMLElement>('[aria-label="规则同步"]'))
    }
  },
})

onMounted(async () => {
  await Promise.all([loadAgents(), refreshProfiles().catch(() => undefined)])
  startAutoRefresh()
  runPending()
})

onUnmounted(() => {
  resetCoreInstallCommand()
  settingsPageActive = false
  stopAutoRefresh()
})
</script>

<style scoped>
.agent-profile-select {
  color: var(--foreground);
  background: var(--popover);
  border-color: var(--border-strong);
  cursor: pointer;
  transition: background-color 200ms var(--ease-tech), border-color 200ms var(--ease-tech);
}

.agent-profile-select :deep(svg) {
  color: var(--primary-accent);
  opacity: 1;
}

.agent-profile-select:not(:disabled):hover,
.agent-profile-select:not(:disabled)[data-state='open'] {
  background: var(--primary-soft);
  border-color: var(--primary-accent);
}

.agent-profile-select:focus-visible {
  border-color: var(--primary-accent);
  outline: 2px solid var(--primary-accent);
  outline-offset: 2px;
  box-shadow: none;
}

.agent-profile-select:disabled {
  color: var(--muted-foreground);
  background: var(--muted);
  cursor: not-allowed;
}

.agent-profile-select:disabled :deep(svg) {
  color: var(--muted-foreground);
}

@media (width < 640px) {
  .agents-page-header :deep(.cf-page-actions) {
    width: 100%;
    margin-left: 0;
    gap: var(--cf-sp-1);
  }

  .agents-page-header :deep(.cf-page-actions > button:first-child) {
    flex: 0 0 var(--cf-touch);
  }
}

.agent-pulse {
  animation: agent-ping 2s infinite;
}

@keyframes agent-ping {
  0% {
    box-shadow: 0 0 0 0 oklch(from var(--success-accent) l c h / 60%);
  }
  100% {
    box-shadow: 0 0 0 8px transparent;
  }
}
</style>
