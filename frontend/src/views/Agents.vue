<template>
  <div>
    <ScopeBanner
      scope="system"
      description="Agent 列表为所有配置空间共有；每个 Agent 各自绑定一个配置空间，推送时使用它绑定的那份配置"
    />

    <PageHeader
      eyebrow="System"
      title="Agent"
      description="Agent 注册、配置推送与运行状态。"
    >
      <template #actions>
        <Button variant="outline" class="border-border/60 bg-background/40" @click="loadAgents">
          <RefreshCw class="size-4" />
          刷新
        </Button>
        <Button class="shadow-glow" @click="handleGenerateScript">
          <FileText class="size-4" />
          生成安装脚本
        </Button>
      </template>
    </PageHeader>

    <!-- 统计 -->
    <div class="mb-4 grid grid-cols-3 gap-3 max-[640px]:grid-cols-1">
      <StatTile label="总 Agent 数" :value="agents.length" :icon="Server" tone="primary" />
      <StatTile label="在线" :value="onlineCount" :icon="CircleCheck" tone="success" />
      <StatTile label="离线" :value="offlineCount" :icon="TriangleAlert" tone="warning" />
    </div>

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

    <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(360px,1fr))] gap-3 max-md:grid-cols-1">
      <Motion
        v-for="(agent, index) in agents"
        :key="agent.id"
        v-bind="listItem(index)"
        class="hairline edge-light relative flex flex-col gap-3.5 overflow-hidden rounded-xl border border-border/35 bg-card/55 p-4 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft"
      >
        <header class="flex flex-wrap items-center gap-2">
          <StatusDot
            :tone="agent.status === 'online' ? 'success' : 'muted'"
            :pulse="agent.status === 'online'"
          />
          <p class="m-0 min-w-0 flex-1 truncate text-[14px] font-semibold text-foreground">
            {{ agent.name }}
          </p>
          <Badge :variant="agent.service_type === 'mihomo' ? 'brand' : 'info'" class="text-[10.5px]">
            {{ agent.service_type === 'mihomo' ? 'Mihomo' : 'MosDNS' }}
          </Badge>
          <Badge v-if="agent.deployment_method" variant="outline" class="text-[10.5px]">
            {{ agent.deployment_method === 'shell' ? 'Shell' : agent.deployment_method === 'docker' ? 'Docker' : agent.deployment_method }}
          </Badge>
        </header>

        <dl class="m-0 grid grid-cols-[76px_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[12.5px]">
          <dt class="text-muted-foreground">地址</dt>
          <dd class="m-0 truncate font-mono text-foreground">{{ agent.host }}:{{ agent.port }}</dd>
          <dt class="text-muted-foreground">配置版本</dt>
          <dd class="m-0 truncate font-mono text-foreground">{{ agent.config_version || 'N/A' }}</dd>
          <dt class="text-muted-foreground">最后心跳</dt>
          <dd class="m-0 truncate text-foreground">{{ formatTime(agent.last_heartbeat) }}</dd>
          <dt class="text-muted-foreground">Agent 版本</dt>
          <dd class="m-0 truncate font-mono text-foreground">{{ agent.version || 'N/A' }}</dd>
        </dl>

        <FormField label="绑定配置空间">
          <Select
            :model-value="agent.profile_id || 'default'"
            :disabled="bindingAgentId === agent.id"
            @update:model-value="value => handleAgentProfileChange(agent, String(value))"
          >
            <SelectTrigger class="h-8 w-full bg-background/50 text-[12.5px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent class="glass-strong">
              <SelectItem v-for="profile in profiles" :key="profile.id" :value="profile.id">
                {{ profile.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </FormField>

        <!-- 系统监控指标 -->
        <section
          v-if="agent.system_metrics"
          class="flex flex-col gap-2.5 rounded-lg border border-border/40 bg-background/40 p-3"
        >
          <div class="flex items-center gap-2">
            <span class="text-[11px] font-semibold tracking-[0.08em] text-muted-foreground uppercase">
              系统监控
            </span>
            <Button variant="ghost" size="sm" class="ml-auto" @click="showMetricsDetail(agent)">
              <ChartLine class="size-3.5" />
              详情
            </Button>
          </div>

          <div v-for="metric in metricsOf(agent)" :key="metric.label" class="flex flex-col gap-1">
            <div class="flex items-baseline gap-2 text-[12px]">
              <span class="text-muted-foreground">{{ metric.label }}</span>
              <span class="num ml-auto font-medium" :style="{ color: metric.color }">
                {{ metric.text }}
              </span>
            </div>
            <!-- 用原生进度条而不是引第三方组件：只需要一条带阈值配色的细条 -->
            <div class="h-1.5 overflow-hidden rounded-full bg-border/60">
              <div
                class="h-full rounded-full transition-[width] duration-500 ease-[cubic-bezier(0.32,0.72,0,1)]"
                :style="{ width: `${Math.min(100, metric.percent)}%`, backgroundColor: metric.color }"
              />
            </div>
            <p v-if="metric.detail" class="num m-0 text-[11px] text-muted-foreground">
              {{ metric.detail }}
            </p>
          </div>

          <div class="grid grid-cols-2 gap-2 border-0 border-t border-border/40 pt-2.5">
            <div v-for="flow in flowsOf(agent)" :key="flow.label" class="min-w-0">
              <p class="m-0 text-[11px] text-muted-foreground">{{ flow.label }}</p>
              <p class="num m-0 truncate text-[12.5px] font-medium text-foreground">{{ flow.value }}</p>
            </div>
          </div>
        </section>

        <footer class="mt-auto flex flex-wrap items-center gap-1 border-0 border-t border-border/50 pt-3">
          <Button variant="ghost" size="sm" @click="pushConfig(agent)">
            <Upload class="size-3.5" />
            推送
          </Button>
          <Button variant="ghost" size="sm" @click="restartAgent(agent)">
            <RotateCw class="size-3.5" />
            重启
          </Button>
          <Button variant="ghost" size="sm" @click="viewLogs(agent)">
            <ScrollText class="size-3.5" />
            日志
          </Button>
          <Button v-if="agent.has_update" variant="ghost" size="sm" class="text-primary-accent" @click="updateAgent(agent)">
            <Download class="size-3.5" />
            更新
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger as-child>
              <Button variant="ghost" size="icon-sm" class="ml-auto" title="更多操作" aria-label="更多操作">
                <MoreHorizontal class="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" class="glass-strong">
              <DropdownMenuItem class="text-destructive-accent" @select="uninstallAgent(agent)">
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

    <!-- ===== 生成安装脚本 ===== -->
    <Dialog v-model:open="scriptDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[760px] border-border/50">
        <DialogHeader>
          <DialogTitle>生成 Agent 安装脚本</DialogTitle>
          <DialogDescription>填写部署参数，生成可直接在目标机器执行的一键安装命令。</DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[62dvh] flex-col gap-4 overflow-y-auto pr-1">
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

          <FormField label="Agent 名称" html-for="agent-name">
            <Input id="agent-name" v-model="scriptForm.name" class="bg-background/50" placeholder="例如：香港服务器" />
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
            <FormField label="配置文件路径" html-for="agent-config-path" hint="Agent 拉取配置后保存的文件路径。">
              <Input
                id="agent-config-path"
                v-model="scriptForm.config_path"
                class="bg-background/50 font-mono"
                placeholder="/etc/mihomo/config.yaml"
              />
            </FormField>

            <FormField
              label="重启命令"
              html-for="agent-restart"
              hint="用于完全重启服务，会短暂中断。支持命令方式（systemctl restart mihomo）或 URL 方式（http://127.0.0.1:9090/restart）。"
            >
              <Input
                id="agent-restart"
                v-model="scriptForm.restart_command"
                class="bg-background/50 font-mono"
                placeholder="命令或 URL"
              />
            </FormField>
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

          <Button v-else class="w-full shadow-glow" @click="generateScript">
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
      <DialogContent class="glass-strong hairline max-w-[900px] border-border/50">
        <DialogHeader>
          <DialogTitle>Agent 日志 · {{ currentAgent?.name }}</DialogTitle>
          <DialogDescription>选择日志文件后查看内容，可清空或刷新。</DialogDescription>
        </DialogHeader>

        <div class="flex flex-col gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <Select v-model="selectedLogPath" @update:model-value="value => onLogPathChange(String(value))">
              <SelectTrigger class="h-9 w-[260px] bg-background/50 text-[13px]">
                <SelectValue placeholder="选择日志文件" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
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
                验证
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
      <DialogContent class="glass-strong hairline max-w-[1100px] border-border/50">
        <DialogHeader>
          <DialogTitle>系统监控 · {{ currentMetricsAgent?.name }}</DialogTitle>
          <DialogDescription>最近 24 小时的监控数据，共 {{ metricsHistory.length }} 个数据点。</DialogDescription>
        </DialogHeader>

        <LoadingRows v-if="metricsLoading" :rows="4" />

        <EmptyState
          v-else-if="metricsHistory.length === 0"
          :icon="ChartLine"
          title="暂无监控数据"
          description="Agent 上报心跳后，监控数据会在这里按时间聚合展示。"
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
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
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
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

// Register ECharts components
use([LineChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent, CanvasRenderer])

const agents = ref<Agent[]>([])
const { profiles, refreshProfiles } = useProfileStore()
const bindingAgentId = ref<string | null>(null)
const scriptDialogVisible = ref(false)
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

const scriptForm = ref({
  installType: 'shell',
  dockerMode: 'mihomo', // mihomo, mosdns, aio
  name: '',
  type: 'mihomo',
  port: 8080,
  agent_ip: '',
  config_path: '/etc/mihomo/config.yaml',
  restart_command: 'systemctl restart mihomo',
  dockerImage: '',
  containerName: 'configflow-agent',
  serviceContainerName: '',
  networkMode: 'bridge',
  // Docker Agent 端口配置
  mihomoAgentPort: 8080,
  mosdnsAgentPort: 8081
})

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
            { offset: 0, color: 'rgba(64, 158, 255, 0.3)' },
            { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#409EFF' },
      itemStyle: { color: '#409EFF' }
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
            { offset: 0, color: 'rgba(103, 194, 58, 0.3)' },
            { offset: 1, color: 'rgba(103, 194, 58, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#67C23A' },
      itemStyle: { color: '#67C23A' }
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
            { offset: 0, color: 'rgba(230, 162, 60, 0.3)' },
            { offset: 1, color: 'rgba(230, 162, 60, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#E6A23C' },
      itemStyle: { color: '#E6A23C' }
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
        lineStyle: { color: '#409EFF' },
        itemStyle: { color: '#409EFF' }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        lineStyle: { color: '#67C23A' },
        itemStyle: { color: '#67C23A' }
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
              { offset: 0, color: 'rgba(64, 158, 255, 0.3)' },
              { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
            ]
          }
        },
        lineStyle: { color: '#409EFF' },
        itemStyle: { color: '#409EFF' }
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
              { offset: 0, color: 'rgba(103, 194, 58, 0.3)' },
              { offset: 1, color: 'rgba(103, 194, 58, 0.05)' }
            ]
          }
        },
        lineStyle: { color: '#67C23A' },
        itemStyle: { color: '#67C23A' }
      }
    ]
  }
})

// 加载 Agent 列表
const loadAgents = async () => {
  try {
    const { data } = await agentApi.getAll()
    agents.value = data
  } catch (error) {
    notify.error('加载 Agent 列表失败')
  }
}

const bindAgentProfile = async (agent: Agent, profileId: string): Promise<boolean> => {
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
  if (percent === undefined || percent === null) return '#409EFF'
  if (percent < 60) return '#67C23A' // 绿色
  if (percent < 80) return '#E6A23C' // 橙色
  return '#F56C6C' // 红色
}

/* ---------- 新 UI 的派生数据 ---------- */

const chipClass = (active: boolean): string =>
  [
    'flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors',
    active
      ? 'border-primary-accent/40 bg-primary-soft/50 text-foreground'
      : 'border-border/50 bg-background/40 text-muted-foreground hover:border-border-strong'
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
    ? '将 Agent 直接安装到系统服务。'
    : '使用 Docker 容器运行 Agent，内置 Mihomo / MosDNS 服务。'
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
      '镜像内置 Mihomo，Agent 与 Mihomo 在同一容器中运行',
      '无需单独部署 Mihomo 服务，一个容器即可完成',
      '支持自动配置拉取、更新和服务重启',
      '代理端口：7890 (HTTP)、7891 (SOCKS5)、9090 (API)'
    ]
  },
  mosdns: {
    title: 'Docker MosDNS Agent 使用说明',
    lines: [
      '镜像内置 MosDNS，Agent 与 MosDNS 在同一容器中运行',
      '无需单独部署 MosDNS 服务，一个容器即可完成',
      '支持自动配置拉取、更新和服务重启',
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
  if (scriptForm.value.type === 'mihomo') {
    scriptForm.value.config_path = '/etc/mihomo/config.yaml'
    scriptForm.value.restart_command = 'systemctl restart mihomo'
    // 如果是 Docker 模式，更新默认服务容器名称
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'mihomo'
    }
  } else if (scriptForm.value.type === 'mosdns') {
    scriptForm.value.config_path = '/etc/mosdns/config.yaml'
    scriptForm.value.restart_command = 'systemctl restart mosdns'
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

  // 重置表单值
  scriptForm.value = {
    installType: 'shell',
    dockerMode: 'mihomo',
    name: '',
    type: 'mihomo',
    port: generateRandomPort(),
    agent_ip: '',
    config_path: '/etc/mihomo/config.yaml',
    restart_command: 'systemctl restart mihomo',
    dockerImage: '',
    containerName: 'configflow-agent',
    serviceContainerName: '',
    networkMode: 'bridge'
  }


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
  if (!scriptForm.value.name) {
    notify.warning('请输入 Agent 名称')
    return
  }

  // Shell 安装时检查配置文件路径是否为文件而非目录
  if (scriptForm.value.installType === 'shell') {
    const configPath = scriptForm.value.config_path.trim()
    if (configPath && !configPath.match(/\.\w+$/)) {
      notify.warning('配置文件路径应指向一个文件（如 config.yaml），而不是文件夹')
      return
    }
  }

  const loadingToast = notify.loading(scriptForm.value.installType === 'docker' ? '正在生成 Docker 部署命令...' : '正在生成安装命令...')

  try {
    console.log('开始生成脚本，参数：', scriptForm.value)

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
      notify.success('Docker 部署命令生成成功！')
    } else {
      // 生成 Shell 脚本
      const response = await agentApi.generateScript({
        name: scriptForm.value.name,
        type: scriptForm.value.type,
        port: scriptForm.value.port,
        agent_ip: scriptForm.value.agent_ip,
        config_path: scriptForm.value.config_path,
        restart_command: scriptForm.value.restart_command
      })

      console.log('API 响应：', response)

      // 保存完整脚本
      installScript.value = response.data

      // 获取服务域名配置（优先使用配置的域名，否则使用当前访问地址）
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin

      // 生成一键安装命令
      const params = new URLSearchParams({
        name: scriptForm.value.name,
        type: scriptForm.value.type,
        port: scriptForm.value.port.toString(),
        config_path: scriptForm.value.config_path,
        restart_command: scriptForm.value.restart_command,
        server_url: serverUrl  // 传递完整的服务器URL给后端
      })

      // 如果用户输入了 agent_ip，则添加到参数中
      if (scriptForm.value.agent_ip && scriptForm.value.agent_ip.trim()) {
        params.set('agent_ip', scriptForm.value.agent_ip.trim())
      }

      const scriptUrl = `${serverUrl}/api/agents/install-script?${params.toString()}`

      // 生成一键命令 - 标准 Linux
      installCommand.value = `curl -sSL "${scriptUrl}" | sudo bash`

      // 生成一键命令 - Alpine Linux
      installCommandAlpine.value = `curl -sSL "${scriptUrl}" | sh`

      notify.success('安装命令生成成功！')
    }
  } catch (error: any) {
    console.error('生成脚本失败，错误详情：', error)
    console.error('错误响应：', error.response)
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
const pushConfig = async (agent: Agent) => {
  const loadingToast = notify.loading('正在推送配置...')

  try {
    await agentApi.pushConfig(agent.id)
    notify.success('配置推送成功')
    loadAgents()
  } catch (error: any) {
    notify.error(error.response?.data?.message || '配置推送失败')
  } finally {
    notify.dismiss(loadingToast)
  }
}

// 重启 Agent
const restartAgent = async (agent: Agent) => {
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
  const ok = await confirm('检测到新版本可用，是否立即更新 Agent？更新过程中 Agent 将会重启。', {
    title: '更新 Agent',
    confirmText: '立即更新'
  })
  if (!ok) return

  try {
    const loadingToast = notify.loading('正在更新 Agent，请稍候...')

    try {
      const response = await agentApi.update(agent.id)
      notify.dismiss(loadingToast)

      notify.success('Agent 更新已启动，请等待重启完成')

      // 3秒后刷新列表
      setTimeout(() => {
        loadAgents()
      }, 3000)
    } catch (error: any) {
      notify.dismiss(loadingToast)
      notify.error(error.response?.data?.message || 'Agent 更新失败')
    }
  } catch (error: any) {
    notify.error(error.response?.data?.message || 'Agent 更新失败')
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
    `确定要删除 Agent「${agent.name}」的管理记录吗？\n\n注意：此操作仅删除管理记录，不会卸载远程服务器上的 Agent 程序。如需完全卸载，请使用「卸载 Agent」。`,
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
      notify.error('设置失败')
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

onMounted(() => {
  loadAgents()
  refreshProfiles().catch(() => undefined)
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

