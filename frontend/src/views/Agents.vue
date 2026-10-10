<template>
  <div>

    <PageHeader
      title="Agent"
    >
      <template #actions>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="loadAgents">
          <RefreshCw class="size-4" />
          刷新
        </Button>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="handleGenerateScript">
          <FileText class="size-4" />
          生成安装脚本
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
      <!-- 拓扑：ConfigFlow → 各 Agent，每台设备一条下发通道 -->
      <SectionCard :padded="false" class="mb-3.5" role="region" aria-label="下发拓扑">
        <FlowMap
          ref="topology"
          :columns="topoColumns"
          :edges="topoEdges"
          :height="Math.max(260, agents.length * 72)"
          :min-width="isNarrow ? 560 : 640"
          columns-template="minmax(0,1fr) minmax(0,1.2fr)"
          :gap-x="isNarrow ? 90 : 200"
        >
          <template #node="{ node }">
            <span v-if="node.kind === 'hub'" class="group flex flex-col items-start p-[18px]">
              <BrandMark class="size-10 text-primary" :busy="pushingAll" />
              <span class="font-display mt-2.5 text-[22px] font-medium">ConfigFlow</span>
              <span class="mt-1 text-[10.5px] text-muted-foreground" :title="`当前配置修订号：${activeRevision === undefined ? '暂未获取' : `v${activeRevision}`}`">
                配置空间 · {{ activeProfileName }}
              </span>
            </span>
          </template>
        </FlowMap>
      </SectionCard>

      <div class="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-3.5 max-md:grid-cols-1">
        <Motion
          v-for="(agent, index) in agents"
          :key="agent.id"
          v-bind="listItem(index)"
          :class="cn(
            'relative flex flex-col overflow-hidden rounded-[18px] border border-border bg-card/90 p-[18px] shadow-surface',
            agent.status !== 'online' && 'opacity-80'
          )"
        >
          <header class="flex items-center gap-2.5">
            <span
              :class="cn('size-[9px] shrink-0 rounded-full', agent.status === 'online' ? 'agent-pulse bg-success-accent' : 'bg-muted-foreground/60')"
              :title="agent.status === 'online' ? '在线' : '离线'"
            />
            <p class="font-display m-0 min-w-0 flex-1 truncate text-[19px] font-medium" :title="agent.name">{{ agent.name }}</p>
            <Badge variant="outline" :class="cn('chip font-mono', agent.service_type === 'mosdns' && 'chip-sky')">{{ serviceTypeLabels[agent.service_type] || agent.service_type }}</Badge>
            <span class="font-mono text-[11px] text-muted-foreground">{{ agent.version || '' }}</span>
          </header>
          <p class="mt-1 mb-0 truncate font-mono text-[11.5px] text-muted-foreground">
            {{ agent.host }}:{{ agent.port }}
            · {{ agent.deployment_method === 'shell' ? 'shell' : agent.deployment_method === 'docker' ? 'docker' : agent.deployment_method || '—' }}
            · 心跳 {{ formatTime(agent.last_heartbeat) }}
          </p>

          <!-- 资源占用：CPU / 内存 / 磁盘 -->
          <div class="my-4 grid gap-2.5">
            <div
              v-for="metric in metricsOf(agent)"
              :key="metric.label"
              class="grid grid-cols-[44px_1fr_44px] items-center gap-2.5 text-[11.5px] text-muted-foreground"
              :title="metric.detail || undefined"
            >
              {{ metric.label }}
              <i class="block h-1.5 overflow-hidden rounded-full bg-accent">
                <span
                  class="block h-full rounded-full transition-[width] duration-1000 ease-(--ease-flow)"
                  :style="{ width: `${agent.system_metrics ? Math.min(100, metric.percent) : 0}%`, background: metric.color }"
                />
              </i>
              <b class="text-right font-mono font-medium text-foreground/80">{{ agent.system_metrics ? metric.text : '—' }}</b>
            </div>
            <div v-if="agent.system_metrics" class="flex gap-4 font-mono text-[11px] text-muted-foreground">
              <span>↑ {{ flowsOf(agent)[0].value }}</span>
              <span>↓ {{ flowsOf(agent)[1].value }}</span>
              <Button variant="ghost" size="sm" class="-my-1 ml-auto h-6 px-1.5 text-[11px]" @click="showMetricsDetail(agent)">
                <ChartLine class="size-3" />
                监控
              </Button>
            </div>
          </div>

          <div class="mb-3.5 flex items-center gap-2 text-[11.5px] text-muted-foreground">
            配置空间
            <Select
              :model-value="agent.profile_id || 'default'"
              :disabled="bindingAgentId === agent.id || profileBindingBlocked(agent.id)"
              @update:model-value="value => handleAgentProfileChange(agent, String(value))"
            >
              <SelectTrigger class="h-7 min-w-0 flex-1 bg-background/50 text-[12px]" :aria-label="`${agent.name} 绑定的配置空间`">
                <SelectValue />
              </SelectTrigger>
              <SelectContent class="glass-strong">
                <SelectItem v-for="profile in profiles" :key="profile.id" :value="profile.id">
                  {{ profile.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- 事务发布与在线更新的进度：结果由 useAgentDeployments / useAgentUpgrades 轮询 -->
          <section
            v-if="deployments[agent.id]"
            class="mb-3 rounded-xl border border-border bg-background/50 p-3 text-[12.5px]"
            aria-live="polite"
          >
            <p class="m-0 flex items-center gap-2 font-medium">
              <Loader2 v-if="deploymentBusy(agent.id)" class="size-3.5 animate-spin text-primary-accent" />
              {{ deploymentLabels[deployments[agent.id].status] || deployments[agent.id].status }}
            </p>
            <p v-if="deployments[agent.id].not_recorded" class="mt-1 mb-0 text-muted-foreground">后端暂未记录此发布，正在继续查询。可使用原发布编号重新提交。</p>
            <p v-else-if="deployments[agent.id].query_error" class="mt-1 mb-0 text-muted-foreground">暂时无法确认结果，正在重新查询。</p>
            <p v-if="deployments[agent.id].error || deployments[agent.id].rollback_error" class="mt-1 mb-0 break-words text-destructive-accent">
              {{ deployments[agent.id].error }} {{ deployments[agent.id].rollback_error }}
            </p>
            <Button v-if="deployments[agent.id].status === 'ready'" variant="outline" size="sm" class="mt-2" @click="activateDeployment(agent.id)">应用并启动</Button>
            <Button v-if="deployments[agent.id].status === 'rollback_failed'" variant="outline" size="sm" class="mt-2" @click="refreshDeployment(agent.id)">查询恢复状态</Button>
            <Button v-if="deployments[agent.id].not_recorded || deployments[agent.id].retrying" variant="outline" size="sm" class="mt-2" :disabled="deployments[agent.id].retrying" @click="retryDeployment(agent.id)">{{ deployments[agent.id].retrying ? '正在重新提交…' : '重试同一次发布' }}</Button>
          </section>

          <section
            v-if="upgrades[agent.id]"
            class="mb-3 rounded-xl border border-border bg-background/50 p-3 text-[12.5px]"
            aria-live="polite"
          >
            <p class="m-0 font-medium">{{ upgradeLabels[upgrades[agent.id].status] || upgrades[agent.id].status }} · {{ upgrades[agent.id].target_version }}</p>
            <p v-if="upgrades[agent.id].error" class="mt-1 mb-0 break-words text-destructive-accent">{{ upgrades[agent.id].error }}</p>
            <Button
              v-if="upgradeBusy(agent.id)"
              variant="outline"
              size="sm"
              class="mt-2"
              :disabled="upgradeQuerying.has(agent.id)"
              @click="queryUpgrade(agent.id)"
            >
              <Loader2 v-if="upgradeQuerying.has(agent.id)" class="size-3.5 animate-spin" />
              {{ upgradeQuerying.has(agent.id) ? '正在查询…' : '查询更新结果' }}
            </Button>
          </section>

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

          <footer class="mt-auto flex items-center gap-2 border-t border-border pt-3.5">
            <span
              :class="cn('mr-auto min-w-0 text-[12px]', syncOf(agent).tone === 'success' ? 'text-success-accent' : syncOf(agent).tone === 'warning' ? 'text-warning-accent' : 'text-muted-foreground')"
              :title="syncOf(agent).title"
            >
              {{ syncOf(agent).label }}
            </span>
            <Button
              v-if="agent.status === 'online'"
              variant="outline"
              size="sm"
              class="h-[30px]"
              :disabled="pushingIds.has(agent.id) || upgradeBusy(agent.id) || deploymentBusy(agent.id) || deployments[agent.id]?.status === 'rollback_failed'"
              @click="pushConfig(agent)"
            >
              <Loader2 v-if="pushingIds.has(agent.id) || deploymentBusy(agent.id)" class="size-3.5 animate-spin" />
              <Send v-else class="size-3.5" />
              推送
            </Button>
            <span v-else class="chip">离线 {{ formatTime(agent.last_heartbeat) }}</span>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <Button variant="ghost" size="icon-sm" class="size-[30px]" :aria-label="`${agent.name} 的更多操作`">
                  <MoreHorizontal class="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" class="glass-strong min-w-[160px]">
                <DropdownMenuItem @select="viewLogs(agent)">
                  <ScrollText class="size-4" />
                  日志
                </DropdownMenuItem>
                <DropdownMenuItem v-if="agent.system_metrics" @select="showMetricsDetail(agent)">
                  <ChartLine class="size-4" />
                  监控详情
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
        </Motion>
      </div>
    </template>

    <DockerAgentUpdateDialog v-if="dockerUpdateAgent" :agent="dockerUpdateAgent" @close="dockerUpdateAgent = null" @refresh="loadAgents" />

    <!-- ===== 生成安装脚本 ===== -->
    <Dialog v-model:open="scriptDialogVisible">
      <DialogContent class="max-w-[760px]">
        <DialogHeader>
          <DialogTitle>生成 Agent 安装脚本</DialogTitle>
          <DialogDescription>选择安装方式并填写机器信息，然后复制生成的命令到目标机器执行。</DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[62dvh] flex-col gap-4 overflow-y-auto">
          <FormField label="安装类型" :hint="installTypeHint">
            <RadioGroup v-model="scriptForm.installType" class="flex gap-2" @update:model-value="onInstallTypeChange">
              <label
                v-for="option in INSTALL_TYPES"
                :key="option.value"
                :class="chipClass(scriptForm.installType === option.value)"
              >
                <RadioGroupItem :value="option.value" />
                {{ option.label }}
              </label>
            </RadioGroup>
          </FormField>

          <FormField v-if="scriptForm.installType === 'docker'" label="Docker 模式" :hint="dockerModeHint">
            <RadioGroup v-model="scriptForm.dockerMode" class="flex flex-wrap gap-2" @update:model-value="onDockerModeChange">
              <label
                v-for="option in DOCKER_MODES"
                :key="option.value"
                :class="chipClass(scriptForm.dockerMode === option.value)"
              >
                <RadioGroupItem :value="option.value" />
                {{ option.label }}
              </label>
            </RadioGroup>
          </FormField>

          <FormField label="Agent 名称" html-for="agent-name" hint="使用字母、数字、短横线或下划线。">
            <Input id="agent-name" v-model="scriptForm.name" class="bg-background/50" placeholder="例如：hong-kong-server" />
          </FormField>

          <FormField v-if="scriptForm.installType !== 'docker'" label="服务类型">
            <RadioGroup v-model="scriptForm.type" class="flex flex-wrap gap-2" @update:model-value="onServiceTypeChange">
              <label
                v-for="option in serviceTypeOptions"
                :key="option.value"
                :class="chipClass(scriptForm.type === option.value)"
              >
                <RadioGroupItem :value="option.value" />
                {{ option.label }}
              </label>
            </RadioGroup>
          </FormField>

          <FormField v-if="scriptForm.installType !== 'docker'" label="Agent 端口" html-for="agent-port">
            <Input
              id="agent-port"
              v-model.number="scriptForm.port"
              type="number"
              :min="1024"
              :max="65535"
              class="w-44 bg-background/50"
            />
          </FormField>

          <FormField v-else label="Agent 端口">
            <div class="flex flex-col gap-2">
              <div
                v-if="scriptForm.dockerMode === 'mihomo' || scriptForm.dockerMode === 'aio'"
                class="flex items-center gap-2"
              >
                <span class="w-32 shrink-0 text-[12.5px] text-muted-foreground">Mihomo Agent</span>
                <Input
                  v-model.number="scriptForm.mihomoAgentPort"
                  type="number"
                  :min="1024"
                  :max="65535"
                  class="w-36 bg-background/50"
                />
              </div>
              <div
                v-if="scriptForm.dockerMode === 'mosdns' || scriptForm.dockerMode === 'aio'"
                class="flex items-center gap-2"
              >
                <span class="w-32 shrink-0 text-[12.5px] text-muted-foreground">MosDNS Agent</span>
                <Input
                  v-model.number="scriptForm.mosdnsAgentPort"
                  type="number"
                  :min="1024"
                  :max="65535"
                  class="w-36 bg-background/50"
                />
              </div>
            </div>
          </FormField>

          <FormField label="Agent IP" html-for="agent-ip" hint="可选：指定 Agent 的 IP 地址，留空则脚本会自动获取。">
            <Input
              id="agent-ip"
              v-model="scriptForm.agent_ip"
              class="bg-background/50 font-mono"
              placeholder="留空则自动获取"
            />
          </FormField>

          <template v-if="scriptForm.installType === 'shell'">
            <FormField label="配置文件路径" html-for="agent-config-path" hint="填写配置在目标机器上的保存位置，须包含文件名。">
              <Input
                id="agent-config-path"
                v-model="scriptForm.config_path"
                class="bg-background/50 font-mono"
                placeholder="/etc/mihomo/config.yaml"
              />
            </FormField>

            <FormField
              label="重启命令（可选）"
              html-for="agent-restart"
              hint="留空由安装脚本识别 systemd / OpenRC。使用自定义命令时，请在高级配置中提供停止、启动和状态检查方式。"
            >
              <Input
                id="agent-restart"
                v-model="scriptForm.restart_command"
                class="bg-background/50 font-mono"
                placeholder="留空自动识别"
              />
            </FormField>

            <Collapsible v-model:open="lifecyclePanelOpen" class="rounded-lg border border-border/70">
              <CollapsibleTrigger class="w-full px-3 py-2 text-left text-[13px] font-medium">
                服务管理高级配置（可选）
              </CollapsibleTrigger>
              <CollapsibleContent class="space-y-4 border-t border-border/60 p-3">
                <FormField label="服务管理方式" hint="默认自动识别；自行启动的服务可选择自定义命令。">
                  <Select v-model="scriptForm.service_manager">
                    <SelectTrigger class="bg-background/50"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="auto">自动识别 systemd / OpenRC</SelectItem>
                      <SelectItem value="systemd">systemd</SelectItem>
                      <SelectItem value="openrc">OpenRC</SelectItem>
                      <SelectItem value="command">自定义命令</SelectItem>
                    </SelectContent>
                  </Select>
                </FormField>
                <FormField label="服务名称" html-for="agent-service-unit" hint="留空使用 mihomo 或 mosdns；可填写自定义 systemd 单元或 OpenRC 服务名。">
                  <Input id="agent-service-unit" v-model="scriptForm.service_unit" class="bg-background/50 font-mono" :placeholder="scriptForm.type" />
                </FormField>
                <FormField label="内核程序路径" html-for="agent-service-binary" hint="用于配置检查，应指向正在运行的同一个 Mihomo / MosDNS 程序；留空按程序名查找。">
                  <Input id="agent-service-binary" v-model="scriptForm.service_binary" class="bg-background/50 font-mono" :placeholder="`/usr/local/bin/${scriptForm.type}`" />
                </FormField>
                <template v-if="scriptForm.service_manager === 'command' || scriptForm.restart_command.trim()">
                  <FormField label="停止命令" html-for="agent-service-stop" hint="应等待服务完全停止后再退出。">
                    <Input id="agent-service-stop" v-model="scriptForm.stop_command" class="bg-background/50 font-mono" placeholder="例如：/opt/service/control stop" />
                  </FormField>
                  <FormField label="启动命令" html-for="agent-service-start">
                    <Input id="agent-service-start" v-model="scriptForm.start_command" class="bg-background/50 font-mono" placeholder="例如：/opt/service/control start" />
                  </FormField>
                  <FormField label="状态检查命令" html-for="agent-service-status" hint="服务运行时退出码为 0，未运行时为非 0。">
                    <Input id="agent-service-status" v-model="scriptForm.status_command" class="bg-background/50 font-mono" placeholder="例如：/opt/service/control status" />
                  </FormField>
                </template>
              </CollapsibleContent>
            </Collapsible>
          </template>

          <template v-if="scriptForm.installType === 'docker'">
            <InfoNote>
              <p><strong class="text-foreground">{{ dockerNote.title }}</strong></p>
              <p v-for="line in dockerNote.lines" :key="line">• {{ line }}</p>
            </InfoNote>

            <FormField label="Docker 镜像" html-for="docker-image" hint="可选：留空则使用默认官方镜像。">
              <Input
                id="docker-image"
                v-model="scriptForm.dockerImage"
                class="bg-background/50 font-mono"
                placeholder="默认使用官方镜像"
              />
            </FormField>

            <FormField label="Agent 容器名" html-for="docker-container" hint="Agent 的 Docker 容器名称。">
              <Input
                id="docker-container"
                v-model="scriptForm.containerName"
                class="bg-background/50 font-mono"
                placeholder="configflow-agent"
              />
            </FormField>

            <FormField label="网络模式" hint="host 模式可直接访问主机网络，bridge 模式需要端口映射。">
              <RadioGroup v-model="scriptForm.networkMode" class="flex flex-wrap gap-2">
                <label
                  v-for="option in networkModeOptions"
                  :key="option.value"
                  :class="chipClass(scriptForm.networkMode === option.value)"
                >
                  <RadioGroupItem :value="option.value" />
                  {{ option.label }}
                </label>
              </RadioGroup>
            </FormField>
          </template>

          <LabeledDivider label="一键安装命令" />

          <template v-if="installCommand || dockerComposeContent || dockerRunCommand">
            <div v-for="block in commandBlocks" :key="block.title" class="flex flex-col gap-2">
              <div class="flex items-baseline gap-2">
                <span class="text-[12.5px] font-semibold text-foreground">{{ block.title }}</span>
                <span class="text-[11.5px] text-muted-foreground">{{ block.note }}</span>
              </div>
              <Textarea
                :model-value="block.value"
                readonly
                :rows="block.rows"
                class="bg-background/50 font-mono text-[11.5px]"
              />
              <Button variant="outline" class="border-border/60 bg-background/40" @click="block.copy">
                <Copy class="size-4" />
                复制
              </Button>
            </div>

            <Collapsible v-model:open="scriptPanelOpen">
              <CollapsibleTrigger as-child>
                <button
                  type="button"
                  class="flex w-full cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-2 text-left text-[13px] text-muted-foreground transition-colors hover:border-border-strong"
                >
                  查看完整安装脚本
                  <ChevronDown
                    class="ml-auto size-3.5 transition-transform duration-200"
                    :class="scriptPanelOpen && 'rotate-180'"
                  />
                </button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <Textarea
                  :model-value="installScript"
                  readonly
                  :rows="15"
                  class="mt-2 bg-background/50 font-mono text-[11.5px]"
                />
              </CollapsibleContent>
            </Collapsible>
          </template>

          <Button v-else class="w-full" @click="generateScript">
            <FileText class="size-4" />
            {{ scriptForm.installType === 'docker' ? '生成 Docker 部署命令' : '生成安装命令' }}
          </Button>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="scriptDialogVisible = false">关闭</Button>
          <Button v-if="installCommand" variant="outline" @click="resetForm">重新生成</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== Agent 日志 ===== -->
    <Dialog v-model:open="logsDialogVisible">
      <DialogContent class="max-w-[900px]">
        <DialogHeader>
          <DialogTitle>Agent 日志 · {{ currentAgent?.name }}</DialogTitle>
          <DialogDescription>选择此 Agent 机器上的日志文件查看内容。清空会删除所选文件中的全部日志，且无法恢复。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <Select v-model="selectedLogPath" @update:model-value="value => onLogPathChange(String(value))">
              <SelectTrigger class="h-9 w-[260px] bg-background/50 text-[13px]">
                <SelectValue placeholder="选择日志文件" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="opt in logPathOptions" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </SelectItem>
                <SelectItem value="custom">自定义路径…</SelectItem>
              </SelectContent>
            </Select>

            <template v-if="selectedLogPath === 'custom'">
              <Input
                v-model="customLogPath"
                class="w-[300px] bg-background/50 font-mono"
                placeholder="/var/log/your-file.log"
                @keyup.enter="validateAndLoadCustomPath"
              />
              <Button
                variant="outline"
                class="border-border/60 bg-background/40"
                :disabled="validatingPath"
                @click="validateAndLoadCustomPath"
              >
                <Loader2 v-if="validatingPath" class="size-4 animate-spin" />
                查看日志
              </Button>
            </template>
          </div>

          <pre
            ref="logsPaneRef"
            class="m-0 max-h-[46dvh] overflow-auto rounded-lg border border-border/50 bg-background/45 p-3 font-mono text-[11.5px] leading-[1.7] whitespace-pre-wrap text-foreground/85"
          >{{ logs || '暂无日志' }}</pre>
        </div>

        <DialogFooter class="sm:justify-between">
          <label
            v-if="isMainAgentLog"
            class="flex items-center gap-2 text-[12.5px] text-muted-foreground"
          >
            <Switch
              :model-value="loggingEnabled"
              :disabled="togglingLogging"
              @update:model-value="value => toggleLogging(Boolean(value))"
            />
            {{ loggingEnabled ? '日志已启用' : '日志已禁用' }}
          </label>
          <div class="flex items-center gap-2">
            <Button variant="outline" @click="logsDialogVisible = false">关闭</Button>
            <Button
              variant="outline"
              class="border-destructive-accent/30 bg-destructive-soft/40 text-destructive-accent"
              @click="clearLogs"
            >
              <Trash2 class="size-4" />
              清空
            </Button>
            <Button @click="refreshLogs">
              <RefreshCw class="size-4" />
              刷新
            </Button>
          </div>
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
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import BrandMark from '@/components/common/BrandMark.vue'
import FlowMap, { type FlowColumn, type FlowEdge, type FlowTone } from '@/components/dashboard/FlowMap.vue'
import { consumeAction } from '@/lib/actions'
import { cn } from '@/lib/utils'
import { getAgentConfigSync } from '@/lib/agentConfigSync'
import DockerAgentUpdateDialog from '@/components/agents/DockerAgentUpdateDialog.vue'
import { Motion } from 'motion-v'
import {
  ChartLine,
  ChevronDown,
  CircleCheck,
  Copy,
  Download,
  FileText,
  Loader2,
  MoreHorizontal,
  RefreshCw,
  RotateCw,
  ScrollText,
  Send,
  Server,
  Trash2,
  TriangleAlert,
  Upload,
  X
} from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import EmptyState from '@/components/common/EmptyState.vue'
import FormField from '@/components/common/FormField.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import LabeledDivider from '@/components/common/LabeledDivider.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import StatTile from '@/components/common/StatTile.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { agentApi } from '@/api'
import api from '@/api'
import type { Agent } from '@/types'
import { useProfileStore } from '@/stores/profile'
import { createDeploymentId, deploymentLabels, useAgentDeployments } from '@/composables/useAgentDeployments'
import { upgradeLabels, useAgentUpgrades } from '@/composables/useAgentUpgrades'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

// Register ECharts components
use([LineChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent, CanvasRenderer])

const agents = ref<Agent[]>([])
const { deployments, busy: deploymentBusy, track: trackDeployment, refresh: refreshDeployment, activate: activateDeployment, retryPublish: retryDeployment } = useAgentDeployments(() => { void loadAgents() })
const {
  upgrades, busy: upgradeBusy, querying: upgradeQuerying, track: trackUpgrade, query: queryAgentUpgrade, start: startUpgrade
} = useAgentUpgrades(() => { void loadAgents() })

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
const { profiles, refreshProfiles } = useProfileStore()
const bindingAgentId = ref<string | null>(null)
const scriptDialogVisible = ref(false)
const dockerUpdateAgent = ref<Agent | null>(null)
const lifecyclePanelOpen = ref(false)
const logsDialogVisible = ref(false)
const metricsDialogVisible = ref(false)
const installScript = ref('')
const installCommand = ref('')
const installCommandAlpine = ref('')
const dockerComposeContent = ref('')
const dockerRunCommand = ref('')
const logs = ref('')
const selectedLogPath = ref('/var/log/configflow-agent.log')
const customLogPath = ref('')
const loggingEnabled = ref(true)
const togglingLogging = ref(false)
const validatingPath = ref(false)
const currentAgent = ref<Agent | null>(null)
const currentMetricsAgent = ref<Agent | null>(null)
const metricsHistory = ref<any[]>([])
const metricsLoading = ref(false)
const logsPaneRef = ref<HTMLElement | null>(null)

const createScriptForm = () => ({
  installType: 'shell',
  dockerMode: 'mihomo', // mihomo, mosdns, aio
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
  // Docker Agent 端口配置
  mihomoAgentPort: 8080,
  mosdnsAgentPort: 8081
})
const scriptForm = ref(createScriptForm())

const serviceTypeLabels: Record<Agent['service_type'], string> = {
  mihomo: 'Mihomo',
  surge: 'Surge',
  mosdns: 'MosDNS'
}

// 服务类型选项
const serviceTypeOptions = [
  { label: 'Mihomo', value: 'mihomo' },
  { label: 'MosDNS', value: 'mosdns' }
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
const loadAgents = async () => {
  const generation = ++agentsLoadGeneration
  try {
    const { data } = await agentApi.getAll()
    if (generation !== agentsLoadGeneration) return
    agents.value = data
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
  const previousProfileId = agent.profile_id || 'default'
  if (profileId === previousProfileId) return true
  bindingAgentId.value = agent.id
  try {
    await agentApi.bindProfile(agent.id, profileId)
    agent.profile_id = profileId
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
  if (!profileId || profileId === (agent.profile_id || 'default')) return
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

// 格式化字节数（短格式，用于卡片显示）
const formatBytesShort = (bytes: number | undefined) => {
  if (bytes === undefined || bytes === null) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  // 使用较少的小数位使显示更紧凑
  return `${size.toFixed(1)} ${units[unitIndex]}`
}

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

const DOCKER_MODES = [
  { value: 'mihomo', label: 'Mihomo (Clash)' },
  { value: 'mosdns', label: 'MosDNS' },
  { value: 'aio', label: 'All-in-One（三合一）' }
]

const installTypeHint = computed(() =>
  scriptForm.value.installType === 'shell'
    ? '将 Agent 安装为目标机器上的系统服务。'
    : '用 Docker 运行 Agent，无需另行安装 Mihomo / MosDNS。'
)

const dockerModeHint = computed(
  () =>
    ({
      mihomo: '内置 Mihomo，一个容器运行 Agent + Mihomo。',
      mosdns: '内置 MosDNS，一个容器运行 Agent + MosDNS。',
      aio: '内置 Mihomo + MosDNS，一个容器同时运行三者。'
    })[scriptForm.value.dockerMode as string] ?? ''
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
      '镜像内置 Mihomo 与 MosDNS，可同时运行多个服务',
      'Mihomo Agent 端口 8080，MosDNS Agent 端口 8081',
      '可通过环境变量 ENABLE_MIHOMO / ENABLE_MOSDNS 控制启用哪些服务',
      '会自动创建 ./mihomo 与 ./mosdns 目录存储配置文件'
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
    return [
      {
        title: 'Ubuntu / Debian / CentOS',
        note: '使用 systemd',
        value: installCommand.value,
        rows: 3,
        copy: copyCommand
      },
      {
        title: 'Alpine Linux',
        note: '使用 OpenRC',
        value: installCommandAlpine.value,
        rows: 3,
        copy: copyCommandAlpine
      }
    ]
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

/** 卡片内的三条资源占用条 */
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

/** 卡片内的网络速率与累计流量 */
const flowsOf = (agent: any) => {
  const net = agent.system_metrics?.network || {}
  return [
    { label: '↑ 上传', value: formatSpeed(net.speed_sent) },
    { label: '↓ 下载', value: formatSpeed(net.speed_recv) },
    { label: '↑ 已发送', value: formatBytesShort(net.bytes_sent) },
    { label: '↓ 已接收', value: formatBytesShort(net.bytes_recv) }
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
  // 清空之前生成的命令
  installCommand.value = ''
  installCommandAlpine.value = ''
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
  installCommand.value = ''
  installCommandAlpine.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''

  if (scriptForm.value.dockerMode === 'mihomo') {
    // mihomo 模式
    scriptForm.value.type = 'mihomo'
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
  // 清空其他值
  installScript.value = ''
  installCommand.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  scriptPanelOpen.value = false

  // Reset every installation field, including both Docker Agent ports.
  scriptForm.value = { ...createScriptForm(), port: generateRandomPort() }
  lifecyclePanelOpen.value = false
  installCommandAlpine.value = ''

  // 打开对话框 - v-if 会确保表单完全重新渲染
  scriptDialogVisible.value = true
}

// 重置表单
const resetForm = () => {
  installScript.value = ''
  installCommand.value = ''
  installCommandAlpine.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  scriptPanelOpen.value = false
}

// 生成安装脚本
const generateScript = async () => {
  if (!scriptForm.value.name.trim()) {
    notify.warning('请输入 Agent 名称')
    return
  }

  // Shell 安装时检查配置文件路径是否为文件而非目录
  if (scriptForm.value.installType === 'shell') {
    if (!/^[A-Za-z0-9_-]+$/.test(scriptForm.value.name.trim())) {
      notify.warning('Agent 名称只能包含字母、数字、短横线或下划线')
      return
    }
    const configPath = scriptForm.value.config_path.trim()
    if (configPath && !configPath.match(/\.\w+$/)) {
      notify.warning('配置文件路径应指向一个文件（如 config.yaml），而不是文件夹')
      return
    }
    const form = scriptForm.value
    const unit = (form.service_unit.trim() || form.type).replace(/\.service$/, '')
    const restart = form.restart_command.trim()
    const standardRestart = !restart || [
      `systemctl restart ${unit}`, `systemctl restart ${unit}.service`, `rc-service ${unit} restart`
    ].includes(restart)
    const customLifecycle = form.service_manager === 'command' || (form.service_manager === 'auto' && !standardRestart)
    if (customLifecycle && ![form.stop_command, form.start_command, form.status_command].every(value => value.trim())) {
      lifecyclePanelOpen.value = true
      notify.warning('自定义服务需要填写停止、启动和状态检查命令，才能在发布失败时回滚')
      return
    }
  }

  const loadingToast = notify.loading(scriptForm.value.installType === 'docker' ? '正在生成 Docker 部署命令...' : '正在生成安装命令...')

  try {
    if (scriptForm.value.installType === 'docker') {
      // 生成 Docker Compose 和 Docker Run
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin

      const dockerParams = {
        server_url: serverUrl,
        agent_name: scriptForm.value.containerName || scriptForm.value.name,
        agent_ip: scriptForm.value.agent_ip || '',
        network_mode: scriptForm.value.networkMode,
        enable_mihomo: scriptForm.value.dockerMode === 'mihomo' || scriptForm.value.dockerMode === 'aio',
        enable_mosdns: scriptForm.value.dockerMode === 'mosdns' || scriptForm.value.dockerMode === 'aio',
        mihomo_port: scriptForm.value.mihomoAgentPort,
        mosdns_port: scriptForm.value.mosdnsAgentPort,
        // 根据模式设置数据目录
        data_dir: scriptForm.value.dockerMode === 'aio' ? './aio_data' :
                  (scriptForm.value.dockerMode === 'mosdns' ? './mosdns_data' : './mihomo_data')
      }

      // 使用统一的 Docker API
      const [composeResponse, runResponse] = await Promise.all([
        api.get('/agents/docker-agent-compose', { params: dockerParams }),
        api.get('/agents/docker-agent-run', { params: dockerParams })
      ])

      dockerComposeContent.value = composeResponse.data
      dockerRunCommand.value = runResponse.data
      notify.success('Docker 部署命令已生成，请复制到目标机器执行')
    } else {
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin
      const form = scriptForm.value
      const lifecycleParams: Record<string, string> = {}
      if (form.service_manager !== 'auto') lifecycleParams.service_manager = form.service_manager
      for (const key of ['service_unit', 'service_binary', 'stop_command', 'start_command', 'status_command'] as const) {
        const value = form[key].trim()
        if (value) lifecycleParams[key] = value
      }
      const shellParams = {
        name: form.name.trim(),
        type: form.type,
        port: form.port,
        agent_ip: form.agent_ip.trim(),
        config_path: form.config_path.trim(),
        restart_command: form.restart_command.trim(),
        server_url: serverUrl,
        ...lifecycleParams
      }
      const response = await agentApi.generateScript(shellParams)
      installScript.value = response.data

      // Preview and one-click installation use exactly the same parameters.
      const params = new URLSearchParams()
      for (const [key, value] of Object.entries(shellParams)) {
        if (value !== '') params.set(key, String(value))
      }
      // An explicit empty restart command requests lifecycle auto-detection.
      params.set('restart_command', shellParams.restart_command)

      const scriptUrl = `${serverUrl}/api/agents/install-script?${params.toString()}`

      // 生成一键命令 - 标准 Linux
      installCommand.value = `curl -sSL "${scriptUrl}" | sudo bash`

      // 生成一键命令 - Alpine Linux
      installCommandAlpine.value = `curl -sSL "${scriptUrl}" | sh`

      notify.success('安装命令已生成，请复制到目标机器执行')
    }
  } catch (error: any) {
    const errorMsg = error.response?.data?.message || error.message || '生成脚本失败'
    notify.error(errorMsg)
  } finally {
    notify.dismiss(loadingToast)
  }
}

// 复制命令
const copyCommand = () => {
  if (!installCommand.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(installCommand.value).then(() => {
      notify.success('命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(installCommand.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(installCommand.value)
  }
}

// 复制 Alpine 命令
const copyCommandAlpine = () => {
  if (!installCommandAlpine.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(installCommandAlpine.value).then(() => {
      notify.success('Alpine 命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(installCommandAlpine.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(installCommandAlpine.value)
  }
}

// 复制 Docker Compose
const copyDockerCompose = () => {
  if (!dockerComposeContent.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(dockerComposeContent.value).then(() => {
      notify.success('Docker Compose 已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(dockerComposeContent.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(dockerComposeContent.value)
  }
}

// 复制 Docker Run 命令
const copyDockerRun = () => {
  if (!dockerRunCommand.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(dockerRunCommand.value).then(() => {
      notify.success('Docker Run 命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(dockerRunCommand.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(dockerRunCommand.value)
  }
}

// 降级复制方法
const fallbackCopy = (text: string) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    notify.success('内容已复制到剪贴板')
  } catch (err) {
    notify.error('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

// 推送配置
const pushingIds = ref<Set<string>>(new Set())
const pushingAll = ref(false)

type PushOutcome = 'submitted' | 'done' | 'failed' | 'busy'

/**
 * 推送：数据包沿拓扑通道送出；mihomo / mosdns 走事务发布，结果由 useAgentDeployments 跟踪并提示，
 * 这里只处理「提交」本身的成败。
 */
const pushOne = async (agent: Agent, quiet = false): Promise<PushOutcome> => {
  if (deploymentBusy(agent.id)) return 'busy'
  const deploymentId = createDeploymentId()
  deployments[agent.id] = { deployment_id: deploymentId, status: 'preparing' }
  pushingIds.value = new Set([...pushingIds.value, agent.id])
  topology.value?.burst(`agent:${agent.id}`, 16)
  try {
    const { data } = await agentApi.pushConfig(agent.id, deploymentId)
    if (data.deployment_id) {
      trackDeployment(agent.id, data)
      return 'submitted'
    }
    delete deployments[agent.id]
    return 'done'
  } catch (error: any) {
    if (error.response?.data?.deployment_id) {
      trackDeployment(agent.id, error.response.data)
      return 'submitted'
    }
    if (!error.response) {
      trackDeployment(agent.id, { deployment_id: deploymentId, status: 'unknown' })
      return 'submitted'
    }
    delete deployments[agent.id]
    if (!quiet) notify.error(`「${agent.name}」配置发布失败`, error.response?.data?.message)
    return 'failed'
  } finally {
    const next = new Set(pushingIds.value)
    next.delete(agent.id)
    pushingIds.value = next
  }
}

const pushConfig = async (agent: Agent) => {
  if (deploymentBusy(agent.id)) return
  const loadingToast = notify.loading('正在准备并上传发布文件...')
  try {
    if ((await pushOne(agent)) === 'done') {
      notify.success(`已推送至「${agent.name}」`)
      void loadAgents()
    }
  } finally {
    notify.dismiss(loadingToast)
  }
}

const pushAll = async () => {
  const online = agents.value.filter(a => a.status === 'online' && !deploymentBusy(a.id))
  if (!online.length || pushingAll.value) return
  pushingAll.value = true
  const results = await Promise.all(online.map(agent => pushOne(agent, true)))
  pushingAll.value = false
  const sent = results.filter(r => r === 'submitted' || r === 'done').length
  const failed = results.filter(r => r === 'failed').length
  const skipped = agents.value.length - online.length
  const tail = `${failed ? ` · ${failed} 台失败` : ''}${skipped ? ` · ${skipped} 台离线或发布中跳过` : ''}`
  if (failed) notify.warning(`已向 ${sent} 台 Agent 提交推送${tail}`)
  else notify.success(`已向 ${sent} 台 Agent 提交推送${tail}`)
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
    notify.info('更新任务已创建，正在确认执行结果')
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

// 根据 Agent 类型动态生成日志选项
const logPathOptions = computed(() => {
  const common = [
    { label: '主 Agent 日志', value: '/var/log/configflow-agent.log' },
    { label: 'Supervisor 主日志', value: '/var/log/supervisor/supervisord.log' },
  ]
  const mihomo = [
    { label: 'Mihomo 输出日志', value: '/var/log/supervisor/mihomo.log' },
    { label: 'Mihomo 错误日志', value: '/var/log/supervisor/mihomo.err.log' },
  ]
  const mosdns = [
    { label: 'MosDNS 错误日志', value: '/etc/mosdns/mosdns.err.log' },
  ]
  const type = currentAgent.value?.service_type
  if (type === 'mihomo') return [...common, ...mihomo]
  if (type === 'mosdns') return [...common, ...mosdns]
  return [...common, ...mihomo, ...mosdns]
})

// 判断是否为主 Agent 日志
const isMainAgentLog = computed(() => {
  return selectedLogPath.value === '/var/log/configflow-agent.log' && customLogPath.value === ''
})

// 查看日志
const viewLogs = async (agent: Agent) => {
  currentAgent.value = agent
  selectedLogPath.value = '/var/log/configflow-agent.log'
  customLogPath.value = ''
  logsDialogVisible.value = true
  await loadLoggingConfig()
  await loadLogs()
}

// 加载日志
const loadLogs = async () => {
  if (!currentAgent.value) return

  try {
    // 确定要读取的日志路径
    let logPath = ''
    if (selectedLogPath.value === 'custom') {
      if (!customLogPath.value) {
        logs.value = '请输入自定义日志路径'
        return
      }
      logPath = customLogPath.value
    } else {
      logPath = selectedLogPath.value
    }

    // 调用 API 获取日志
    const { data } = await agentApi.getLogs(currentAgent.value.id, 200, logPath)
    if (data.success) {
      logs.value = data.logs || '暂无日志'
    } else {
      logs.value = `获取日志失败: ${data.message}`
    }

    // 等待DOM更新后滚动到底部
    await nextTick()
    scrollToBottom()
  } catch (error: any) {
    logs.value = `获取日志失败: ${error.response?.data?.message || error.message || '未知错误'}`
    notify.error('获取日志失败')
  }
}

// 滚动到日志底部
const scrollToBottom = () => {
  const pane = logsPaneRef.value
  if (pane) pane.scrollTop = pane.scrollHeight
}

// 刷新日志
const refreshLogs = async () => {
  const loadingToast = notify.loading('正在刷新日志...')

  try {
    await loadLogs()
    notify.success('日志刷新成功')
  } finally {
    notify.dismiss(loadingToast)
  }
}

// 清空日志
const clearLogs = async () => {
  if (!currentAgent.value) return

  let logPath = selectedLogPath.value === 'custom' ? customLogPath.value : selectedLogPath.value
  if (!logPath) {
    notify.warning('请先选择日志文件')
    return
  }

  const ok = await confirmDanger('确定要清空该日志文件吗？此操作不可恢复。', {
    title: '清空日志',
    confirmText: '确定清空'
  })
  if (!ok) return

  const loadingToast = notify.loading('正在清空日志...')

  try {
    const { data } = await agentApi.clearLog(currentAgent.value.id, logPath)
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

// 日志路径切换
const onLogPathChange = async (newPath: string) => {
  // 确保 selectedLogPath 已更新
  selectedLogPath.value = newPath
  if (newPath !== 'custom') {
    customLogPath.value = ''
    await loadLogs()
  }
}

// 验证并加载自定义路径
const validateAndLoadCustomPath = async () => {
  if (!currentAgent.value || !customLogPath.value) {
    notify.warning('请输入日志路径')
    return
  }

  validatingPath.value = true
  try {
    const { data } = await agentApi.validateLogPath(currentAgent.value.id, customLogPath.value)

    if (data.success && data.valid) {
      notify.success('路径验证成功')
      await loadLogs()
    } else {
      notify.error(data.error || '路径验证失败')
      logs.value = `路径验证失败: ${data.error || '未知错误'}`
    }
  } catch (error: any) {
    notify.error('路径验证失败')
    logs.value = `路径验证失败: ${error.response?.data?.message || error.message}`
  } finally {
    validatingPath.value = false
  }
}

// 加载日志配置状态
const loadLoggingConfig = async () => {
  if (!currentAgent.value) return

  try {
    const { data } = await agentApi.getLoggingConfig(currentAgent.value.id)
    if (data.success) {
      loggingEnabled.value = data.enabled !== false // 默认为 true
    }
  } catch (error: any) {
    console.error('获取日志配置失败:', error)
  }
}

// 切换日志开关
const toggleLogging = async (enabled: boolean) => {
  if (!currentAgent.value) return

  togglingLogging.value = true
  try {
    const { data } = await agentApi.setLoggingConfig(currentAgent.value.id, enabled)

    if (data.success) {
      notify.success(enabled ? '日志已启用' : '日志已禁用')
    } else {
      notify.error('修改日志开关失败')
      loggingEnabled.value = !enabled // 回滚状态
    }
  } catch (error: any) {
    notify.error('设置日志开关失败')
    loggingEnabled.value = !enabled // 回滚状态
  } finally {
    togglingLogging.value = false
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

/* ---------- 拓扑与配置版本 ---------- */
const topology = ref<InstanceType<typeof FlowMap> | null>(null)
const isNarrow = useMediaQuery('(max-width: 640px)')
const profileStore = useProfileStore()

const revisionOf = (profileId?: string) => {
  const revision = profiles.value.find(p => p.id === (profileId || 'default'))?.revision
  return typeof revision === 'number' && Number.isSafeInteger(revision) && revision >= 0 ? revision : undefined
}
const activeRevision = computed(() => revisionOf(profileStore.activeProfileId.value))
const activeProfileName = computed(
  () => profileStore.activeProfile.value?.name || profileStore.activeProfileId.value
)

const syncOf = (agent: Agent) => getAgentConfigSync(agent, revisionOf(agent.profile_id), deployments[agent.id])

const topoColumns = computed<FlowColumn[]>(() => [
  { key: 'hub', title: '配置中心', nodes: [{ id: 'hub', title: 'ConfigFlow', kind: 'hub', fit: true }] },
  {
    key: 'agents',
    title: 'Agent',
    nodes: agents.value.map(a => ({
      id: `agent:${a.id}`,
      title: a.name,
      meta: `${a.host}:${a.port}`,
      icon: Server,
      status: (a.status === 'online' ? 'online' : 'offline') as 'online' | 'offline'
    }))
  }
])

const topoEdges = computed<FlowEdge[]>(() =>
  agents.value.map(a => ({
    from: 'hub',
    to: `agent:${a.id}`,
    weight: 0.7,
    tone: (a.service_type === 'mosdns' ? 'info' : 'success') as FlowTone,
    dead: a.status !== 'online'
  }))
)

/* 推送与跨页动作 */
const router = useRouter()
const runPending = () => {
  if (consumeAction(router, 'push-all')) pushAll()
}
watch(() => router?.currentRoute.value.query.run, run => run === 'push-all' && runPending())

onMounted(async () => {
  await Promise.all([loadAgents(), refreshProfiles().catch(() => undefined)])
  startAutoRefresh()
  runPending()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
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
