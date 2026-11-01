<template>
  <div class="min-w-0 [overflow-wrap:anywhere]">
    <PageHeader title="系统设置">
      <template #actions><Button as-child variant="outline" class="min-h-11"><RouterLink to="/setup">首次使用向导</RouterLink></Button></template>
    </PageHeader>
    <Tabs keep-visited :model-value="tab" class="min-w-0" @update:model-value="changeTab(String($event))">
      <TabsList class="mb-4 min-h-11 max-sm:mb-2 max-sm:grid max-sm:h-auto max-sm:w-full max-sm:grid-cols-2">
        <TabsTrigger value="general" class="min-h-11 px-4 max-sm:px-2">常规</TabsTrigger>
        <TabsTrigger value="tasks" class="min-h-11 px-4 max-sm:px-2">
          定时任务
          <span v-if="failedTasks" class="chip chip-bad num ml-1.5 px-1.5" :aria-label="`${failedTasks} 个任务失败`">{{ failedTasks }}</span>
        </TabsTrigger>
      </TabsList>
      <TabsContent value="tasks" class="mt-0 min-w-0">
        <ScheduledTasks />
      </TabsContent>
      <TabsContent value="general" class="mt-0 min-w-0">
    <!-- ===== 服务配置 / 配置管理 ===== -->
    <div class="grid grid-cols-2 gap-3 max-[1100px]:grid-cols-1">
      <SectionCard title="服务配置" :icon="Settings" class="min-w-0">
        <div class="flex flex-col gap-4">
          <FormField
            label="服务域名"
            html-for="server-domain"
            hint="填写设备能访问的服务地址，用于生成链接。"
            :help="optionHelp('客户端或 Agent 需要通过链接访问本服务时设置。', '规则、配置订阅、规则转换和安装脚本链接都会使用此地址。填写设备实际能访问的完整地址；仅本机可用的地址无法供其他设备使用。')"
          >
            <div class="flex items-center gap-1.5">
              <Input
                id="server-domain"
                v-model="serverDomain"
                class="min-w-0 bg-background/50 font-mono"
                placeholder="http://192.168.1.10:5010"
                @blur="onServerDomainBlur"
              />
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                title="恢复默认"
                aria-label="恢复默认服务域名"
                @click="resetServerDomain"
              >
                <RefreshCw class="size-4" />
              </Button>
            </div>
          </FormField>

          <WebPortSettings />

          <FormField
            label="规则下载代理"
            html-for="rule-fetch-proxy"
            hint="规则下载受限时填写代理，留空直接连接。"
            :help="optionHelp('面板下载规则、Geo 数据或测试连通性需要代理时填写。', '支持含账号密码的 HTTP/HTTPS 代理，原始及 GitHub 备用地址均使用它；留空直连。移开输入焦点即保存，不修改设备代理。')"
          >
            <div class="flex items-center gap-1.5">
              <Input
                id="rule-fetch-proxy"
                v-model="ruleFetchProxy"
                :type="showRuleFetchProxy ? 'text' : 'password'"
                autocomplete="off"
                :spellcheck="false"
                class="min-w-0 bg-background/50 font-mono"
                placeholder="http://192.168.0.3:7890"
                @blur="onRuleFetchProxyBlur"
              />
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                :aria-label="showRuleFetchProxy ? '隐藏规则下载代理' : '显示规则下载代理'"
                :title="showRuleFetchProxy ? '隐藏规则下载代理' : '显示规则下载代理'"
                @click="showRuleFetchProxy = !showRuleFetchProxy"
              >
                <EyeOff v-if="showRuleFetchProxy" class="size-4" />
                <Eye v-else class="size-4" />
              </Button>
            </div>
          </FormField>

          <FormField hint="开启后将在资源分组下显示「订阅聚合」，可组合订阅和节点。">
            <div class="flex items-center gap-2.5">
              <Switch
                id="agg-enabled"
                v-model="subscriptionAggregationEnabled"
                @update:model-value="value => onSubscriptionAggregationChange(Boolean(value))"
              />
              <Label for="agg-enabled" class="text-[13px] text-muted-foreground">订阅聚合</Label>
            </div>
          </FormField>

          <FormField
            label="访问令牌"
            persistent-hint
            html-for="config-token"
            hint="修改后需更新客户端链接；清空会取消保护。"
            :help="optionHelp('需要限制配置订阅链接的访问者时设置，所有配置共用。', '链接需携带 ?token=xxx，修改令牌后旧链接失效。此令牌同时授予 MCP 管理权限，不应公开分享；清空后取消令牌保护。')"
          >
            <div class="flex items-center gap-1.5">
              <Input
                id="config-token"
                v-model="configToken"
                class="min-w-0 bg-background/50 font-mono"
                placeholder="可手动输入或点击生成"
                @blur="onTokenBlur"
              />
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                title="生成随机令牌"
                aria-label="生成随机令牌"
                @click="generateToken"
              >
                <RefreshCw class="size-4" />
              </Button>
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                title="清除令牌"
                aria-label="清除令牌"
                @click="onClearToken"
              >
                <Trash2 class="size-4" />
              </Button>
            </div>
          </FormField>

          <FormField
            label="MCP"
            persistent-hint
            html-for="mcp-url"
            hint="连接 AI 管理工具；令牌含管理权限，请勿公开。"
            :help="optionHelp('希望通过支持 MCP 的 AI 客户端管理本服务时使用。', '填写此地址，以 Authorization: Bearer 加访问令牌认证。与订阅共用的令牌拥有管理权限，请勿公开令牌或含令牌的订阅链接。')"
          >
            <div class="flex items-center gap-1.5">
              <Input id="mcp-url" :model-value="mcpUrl" readonly class="min-w-0 bg-background/50 font-mono" />
              <Button variant="outline" size="icon" class="shrink-0 border-border/60 bg-background/40" title="复制 MCP 地址" aria-label="复制 MCP 地址" @click="copyMcpUrl">
                <Copy class="size-4" />
              </Button>
            </div>
          </FormField>
        </div>
      </SectionCard>

      <SectionCard title="备份与恢复" :icon="Archive" class="min-w-0">
        <div class="flex flex-col gap-4">
          <div class="grid grid-cols-2 gap-2 max-sm:[&_button]:min-h-11 max-sm:[&_button]:gap-1.5 max-sm:[&_button]:px-2 [&_button]:h-auto [&_button]:min-h-9 [&_button]:min-w-0 [&_button]:whitespace-normal">
            <Button variant="outline" class="border-border/60 bg-background/40" @click="exportConfig">
              <Upload class="size-4" />
              导出全部数据
            </Button>
            <Button
              variant="outline"
              class="border-border/60 bg-background/40"
              @click="exportConfigDesensitized"
            >
              <ShieldCheck class="size-4" />
              脱敏导出
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" @click="pickImportFile">
              <Download class="size-4" />
              导入全部数据
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" @click="handleBackup">
              <CloudUpload class="size-4" />
              WebDAV 备份
            </Button>
            <input
              ref="importInput"
              type="file"
              accept=".json"
              hidden
              @change="onImportFileChange"
            />
          </div>

          <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
            导出和 WebDAV 备份包含共享资源、所有配置、系统设置及 Agent。脱敏导出隐藏敏感信息，仅用于分享。导入会覆盖上述全部数据，不仅影响当前配置，且无法撤销，请先备份。完整备份含敏感信息，请妥善保管。
          </p>

          <!-- 重置会清空全部数据，与常规操作分区并降低视觉权重，避免误触 -->
          <div
            class="mt-auto flex flex-wrap items-center gap-3 rounded-lg border border-destructive-accent/25 bg-destructive-soft/30 p-3"
          >
            <div class="min-w-0 flex-[1_1_12rem]">
              <p class="m-0 text-[13px] font-semibold text-destructive-accent">重置全部数据</p>
              <p class="mt-0.5 mb-0 text-[12px] text-muted-foreground">
                清空共享资源、所有配置及 Agent，恢复默认系统设置，不可撤销。
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              class="shrink-0 border-destructive-accent/40 bg-transparent text-destructive-accent"
              @click="resetConfig"
            >
              <RotateCcw class="size-3.5" />
              重置
            </Button>
          </div>
        </div>
      </SectionCard>
    </div>

    <!-- ===== 第三方依赖 ===== -->
    <DependenciesCard
      class="mt-3"
      :dependencies="dependencies"
      :checking="checkingDependencies"
      :updating-key="updatingKey"
      @check="loadDependencies(true)"
      @update="updateDependency"
      @changed="loadDependencies()"
    />
      </TabsContent>
    </Tabs>

    <!-- ===== 配置备份 ===== -->
    <Dialog v-model:open="backupDialogVisible">
      <DialogContent class="max-w-[620px] [overflow-wrap:anywhere]">
        <DialogHeader>
          <DialogTitle>备份全部数据</DialogTitle>
          <DialogDescription>
            通过 WebDAV 备份共享资源、所有配置、系统设置及 Agent。支持坚果云、Nextcloud 等服务。
          </DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[56dvh] flex-col gap-4 overflow-y-auto">
          <FormField
            label="WebDAV 地址"
            html-for="webdav-url"
            hint="例如坚果云：https://dav.jianguoyun.com/dav/"
          >
            <Input
              id="webdav-url"
              v-model="backupForm.webdav_url"
              class="bg-background/50 font-mono"
              placeholder="https://dav.jianguoyun.com/dav/"
            />
          </FormField>

          <FormField label="用户名" html-for="webdav-user">
            <Input
              id="webdav-user"
              v-model="backupForm.webdav_username"
              autocomplete="username"
              class="bg-background/50"
              placeholder="WebDAV 用户名 / 邮箱"
            />
          </FormField>

          <FormField label="密码" html-for="webdav-pass" hint="坚果云需要使用应用密码，不是登录密码。">
            <Input
              id="webdav-pass"
              v-model="backupForm.webdav_password"
              type="password"
              autocomplete="current-password"
              class="bg-background/50"
              placeholder="WebDAV 密码 / 应用密码"
            />
          </FormField>

          <FormField label="备份路径" html-for="webdav-path" hint="备份在 WebDAV 中的保存位置，默认为 /config-flow-backup/">
            <Input
              id="webdav-path"
              v-model="backupForm.webdav_path"
              class="bg-background/50 font-mono"
              placeholder="/config-flow-backup/"
            />
          </FormField>

          <div class="grid gap-4 sm:grid-cols-2">
            <FormField label="保留最近份数" html-for="backup-keep-latest" hint="每次备份后删除更早的备份，0 表示不清理。">
              <Input
                id="backup-keep-latest"
                v-model.number="backupForm.keep_latest"
                type="number"
                inputmode="numeric"
                :min="0"
                :max="1000"
                :step="1"
                class="num bg-background/50"
              />
            </FormField>
            <FormField label="每日保留天数" html-for="backup-keep-daily" hint="另保留最近几天每天最后一份，0 表示不额外保留。">
              <Input
                id="backup-keep-daily"
                v-model.number="backupForm.keep_daily"
                type="number"
                inputmode="numeric"
                :min="0"
                :max="3650"
                :step="1"
                class="num bg-background/50"
              />
            </FormField>
          </div>
          <p class="-mt-2 text-[12px] text-muted-foreground">只清理备份路径中由 ConfigFlow 生成的 config_backup_*.json 文件。</p>

          <FormField hint="保存配置后在后台备份全部数据，短时间内的多次修改会合并备份。">
            <div class="flex items-center gap-2.5">
              <Switch id="auto-backup" v-model="backupForm.auto_backup" />
              <Label for="auto-backup" class="text-[13px] text-muted-foreground">自动备份</Label>
            </div>
          </FormField>
        </div>

        <DialogFooter class="gap-2 sm:flex-wrap sm:justify-between [&_button]:h-auto [&_button]:min-h-9 [&_button]:min-w-0 [&_button]:whitespace-normal">
          <div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
            <Button
              variant="outline"
              class="border-border/60 bg-background/40"
              :disabled="testingConnection"
              @click="testWebDAVConnection"
            >
              <Loader2 v-if="testingConnection" class="size-4 animate-spin" />
              测试连接
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" :disabled="backingUp" @click="backupNow">
              <Loader2 v-if="backingUp" class="size-4 animate-spin" />
              立即备份
            </Button>
          </div>
          <div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
            <Button variant="outline" @click="backupDialogVisible = false">取消</Button>
            <Button :disabled="savingBackup" @click="saveBackupConfig">
              <Loader2 v-if="savingBackup" class="size-4 animate-spin" />
              保存配置
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSettingLink, focusSetting } from '@/lib/settingLink'
import { isAxiosError } from 'axios'
import { Archive, CloudUpload, Copy, Download, Eye, EyeOff, Loader2, RefreshCw, RotateCcw, Settings, ShieldCheck, Trash2, Upload } from '@lucide/vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import FormField from '@/components/common/FormField.vue'
import DependenciesCard from '@/components/settings/DependenciesCard.vue'
import ScheduledTasks from '@/components/settings/ScheduledTasks.vue'
import WebPortSettings from '@/components/settings/WebPortSettings.vue'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { optionHelp } from '@/lib/optionCopy'
import { usePageReloadGuard } from '@/lib/pageReloadGuard'
import api, { configApi, serverDomainApi, configTokenApi, ruleFetchProxyApi, dependenciesApi, tasksApi } from '@/api'
import { taskSummary } from '@/lib/scheduledTasks'
import type { DependencyStatus } from '@/api'

// 「常规 / 定时任务」标签与地址栏 ?tab= 同步，可直接分享或从其他页面跳转。
const route = useRoute()
const router = useRouter()
const tab = ref(route.query.tab === 'tasks' ? 'tasks' : 'general')
watch(() => route.query.tab, value => { tab.value = value === 'tasks' ? 'tasks' : 'general' })
const changeTab = (value: string) => {
  tab.value = value === 'tasks' ? 'tasks' : 'general'
  router.replace({ query: { ...route.query, tab: tab.value === 'tasks' ? 'tasks' : undefined } })
}
// 常规页也提示失败的任务数，读取失败时不打扰。
const failedTasks = ref(0)
tasksApi.list().then(({ data }) => { failedTasks.value = taskSummary(data.tasks || []).failed }).catch(() => {})

// 备份配置
const backupDialogVisible = ref(false)
const BACKUP_RETENTION_LIMITS = { keep_latest: 1000, keep_daily: 3650 } as const
const backupForm = ref<{
  webdav_url: string
  webdav_username: string
  webdav_password: string
  webdav_path: string
  auto_backup: boolean
  keep_latest: number | string
  keep_daily: number | string
}>({
  webdav_url: '',
  webdav_username: '',
  webdav_password: '',
  webdav_path: '/config-flow-backup/',
  auto_backup: false,
  keep_latest: 30,
  keep_daily: 30
})

// 空输入或越界时提示，不把非法值交给后端。
const backupRetentionValid = () => {
  for (const [key, limit] of Object.entries(BACKUP_RETENTION_LIMITS) as [keyof typeof BACKUP_RETENTION_LIMITS, number][]) {
    const value = backupForm.value[key]
    if (typeof value !== 'number' || !Number.isInteger(value) || value < 0 || value > limit) {
      notify.warning(key === 'keep_latest' ? '保留最近份数须为 0–1000 的整数' : '每日保留天数须为 0–3650 的整数')
      return false
    }
  }
  return true
}
const testingConnection = ref(false)
const backingUp = ref(false)
const savingBackup = ref(false)

const ruleFetchProxy = ref('')
const savedRuleFetchProxy = ref('')
const showRuleFetchProxy = ref(false)

// 订阅聚合开关
const subscriptionAggregationEnabled = ref(false)

// 第三方依赖
const dependencies = ref<DependencyStatus[]>([])
const checkingDependencies = ref(false)
const updatingKey = ref('')
let dependenciesDisposed = false
let dependencyPollTimer: ReturnType<typeof setTimeout> | undefined

const loadDependencies = async (refresh = false) => {
  checkingDependencies.value = true
  try {
    const response = await dependenciesApi.list(refresh)
    dependencies.value = response.data.dependencies || []
    if (refresh) {
      const failed = dependencies.value.find(dep => dep.check_error)
      if (failed) notify.error(failed.check_error)
      else notify.success(dependencies.value.some(dep => dep.has_update) ? '发现新版本' : '所有依赖均为最新版本')
    }
  } catch {
    notify.error('加载第三方依赖失败')
  } finally {
    checkingDependencies.value = false
    scheduleDependencyPoll()
  }
}

// 仅此页面可见时刷新；检测状态不会唤醒 Sub-Store 或推迟休眠。
const scheduleDependencyPoll = () => {
  if (dependenciesDisposed) return
  clearTimeout(dependencyPollTimer)
  dependencyPollTimer = setTimeout(async () => {
    if (document.hidden) { scheduleDependencyPoll(); return }
    try {
      const response = await dependenciesApi.list()
      dependencies.value = response.data.dependencies || []
    } catch {
      // 更新期间 Sub-Store 重启，后端偶发失败时继续轮询
    }
    const target = dependencies.value.find(dep => dep.key === updatingKey.value)
    if (dependencies.value.some(dep => dep.update.state === 'running')) {
      scheduleDependencyPoll()
      return
    }
    if (target) {
      if (target.update.state === 'success') notify.success(target.update.message)
      else if (target.update.state === 'failed') notify.error(target.update.message)
    }
    updatingKey.value = ''
    scheduleDependencyPoll()
  }, dependencies.value.some(dep => dep.update.state === 'running') ? 2000 : 15000)
}

const updateDependency = async (dep: DependencyStatus) => {
  const ok = await confirm(
    `将 ${dep.name} 从 ${dep.current_version || '当前版本'} 更新到 ${dep.latest_version}。更新期间约十几秒内订阅解析和节点转换不可用，失败会自动回滚。`,
    { title: `更新 ${dep.name}`, confirmText: '更新' }
  )
  if (!ok) return
  updatingKey.value = dep.key
  try {
    await dependenciesApi.update(dep.key)
    dep.update = { ...dep.update, state: 'running', message: `准备更新到 ${dep.latest_version}…` }
    scheduleDependencyPoll()
  } catch (error) {
    updatingKey.value = ''
    notify.error(isAxiosError(error) ? error.response?.data?.message || '启动更新失败' : '启动更新失败')
  }
}

onBeforeUnmount(() => { dependenciesDisposed = true; clearTimeout(dependencyPollTimer) })

const serverDomain = ref(window.location.origin)
const configToken = ref('')
const savedServerDomain = ref(serverDomain.value)
const savedConfigToken = ref('')
const savingSettings = ref(0)
usePageReloadGuard(() => savingSettings.value > 0
  || serverDomain.value !== savedServerDomain.value
  || configToken.value !== savedConfigToken.value
  || ruleFetchProxy.value.trim() !== savedRuleFetchProxy.value)
const mcpUrl = computed(() => `${serverDomain.value.replace(/\/+$/, '')}/mcp`)

const copyMcpUrl = async () => {
  try {
    await copyText(mcpUrl.value)
    notify.success('MCP 地址已复制')
  } catch {
    notify.error('复制失败，请手动复制')
  }
}

const loadConfigToken = async () => {
  try {
    const response = await configTokenApi.get()
    configToken.value = response.data.config_token || ''
    savedConfigToken.value = configToken.value
  } catch {
    notify.error('加载访问令牌失败')
  }
}

const handleBackup = () => {
  showBackupDialog()
}

// 重置服务域名为当前浏览器地址
const resetServerDomain = async () => {
  const ok = await confirm('是否将服务域名重置为当前浏览器地址？', { title: '重置服务域名' })
  if (!ok) return

  savingSettings.value++
  try {
    const newDomain = window.location.origin

    await serverDomainApi.update({
      new_domain: newDomain
    })

    serverDomain.value = newDomain
    savedServerDomain.value = newDomain
    localStorage.setItem('serverDomain', newDomain)

    notify.success('服务域名已重置为当前地址')
  } catch (error) {
    console.error('重置服务域名失败:', error)
    notify.error('重置服务域名失败')
  } finally {
    savingSettings.value--
  }
}

// 输入框失去焦点时保存
const onServerDomainBlur = async () => {
  if (!serverDomain.value) {
    return
  }

  const submitted = serverDomain.value
  savingSettings.value++
  try {
    await serverDomainApi.update({
      new_domain: submitted
    })
    savedServerDomain.value = submitted
    localStorage.setItem('serverDomain', serverDomain.value)

    notify.success(`服务域名已更新为：${serverDomain.value}`)
  } catch (error) {
    console.error('更新服务域名失败:', error)
    notify.error('更新服务域名失败')
  } finally {
    savingSettings.value--
  }
}

// 订阅聚合开关变化处理
const onSubscriptionAggregationChange = async (value: boolean) => {
  savingSettings.value++
  try {
    // 保存到后端
    await api.post('/settings/subscription-aggregation', {
      enabled: value
    })
    localStorage.setItem('subscriptionAggregationEnabled', value.toString())

    notify.success(value ? '订阅聚合已开启' : '订阅聚合已关闭')

    // 触发自定义事件，通知其他组件更新
    window.dispatchEvent(new CustomEvent('subscription-aggregation-changed', {
      detail: { enabled: value }
    }))
  } catch (error) {
    console.error('更新订阅聚合开关失败:', error)
    notify.error('更新订阅聚合设置失败')
    // 失败时恢复原值
    subscriptionAggregationEnabled.value = !value
  } finally {
    savingSettings.value--
  }
}

const generateToken = async () => {
  savingSettings.value++
  try {
    const response = await configTokenApi.update({ generate: true })
    configToken.value = response.data.config_token
    savedConfigToken.value = configToken.value
    notify.success('令牌已生成并保存')
  } catch (error) {
    console.error('生成令牌失败:', error)
    notify.error('生成令牌失败')
  } finally {
    savingSettings.value--
  }
}

const onTokenBlur = async () => {
  // 如果输入框为空，不保存
  if (!configToken.value || configToken.value.trim() === '') {
    return
  }

  const submitted = configToken.value
  savingSettings.value++
  try {
    await configTokenApi.update({ token: submitted })
    savedConfigToken.value = submitted
    notify.success('令牌已保存')
  } catch (error) {
    console.error('保存令牌失败:', error)
    notify.error('保存令牌失败')
  } finally {
    savingSettings.value--
  }
}

const onClearToken = async () => {
  const ok = await confirm('确定清除访问令牌吗？清除后，所有配置的订阅链接都不再需要令牌验证，MCP 也不再受此令牌保护。', {
    title: '清除令牌',
    confirmText: '清除'
  })
  if (!ok) return

  savingSettings.value++
  try {
    await configTokenApi.delete()
    configToken.value = ''
    savedConfigToken.value = ''
    notify.success('令牌已清除')
  } catch (error) {
    console.error('清除令牌失败:', error)
    notify.error('清除令牌失败')
  } finally {
    savingSettings.value--
  }
}

const loadRuleFetchProxy = async () => {
  try {
    const response = await ruleFetchProxyApi.get()
    ruleFetchProxy.value = response.data.rule_fetch_proxy || ''
    savedRuleFetchProxy.value = ruleFetchProxy.value
  } catch {
    notify.error('加载规则下载代理失败')
  }
}

const onRuleFetchProxyBlur = async () => {
  const value = ruleFetchProxy.value.trim()
  if (value === savedRuleFetchProxy.value) return
  savingSettings.value++
  try {
    const response = await ruleFetchProxyApi.update({ rule_fetch_proxy: value })
    savedRuleFetchProxy.value = response.data.rule_fetch_proxy
    ruleFetchProxy.value = savedRuleFetchProxy.value
    notify.success(value ? '规则下载代理已保存' : '规则下载代理已清除，将使用现有网络')
  } catch (error) {
    // Axios errors contain the submitted URL, which may include credentials.
    const invalid = isAxiosError(error) && error.response?.status === 400
    notify.error(invalid ? '代理地址无效，请填写 HTTP/HTTPS 代理地址或留空' : '保存规则下载代理失败')
  } finally {
    savingSettings.value--
  }
}

const exportConfig = async () => {
  try {
    const response = await configApi.export()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config.json'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('全部数据已导出，请妥善保管备份文件')
  } catch (error) {
    notify.error('导出失败')
  }
}

const exportConfigDesensitized = async () => {
  try {
    const response = await configApi.exportDesensitized()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config_desensitized.json'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('脱敏数据已导出，仅用于分享')
  } catch (error) {
    notify.error('导出失败')
  }
}

const importInput = ref<HTMLInputElement | null>(null)

const pickImportFile = () => importInput.value?.click()

const onImportFileChange = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  try {
    const config = JSON.parse(await file.text())
    const ok = await confirmDanger(
      '导入将覆盖共享资源、所有配置、系统设置及 Agent，不仅影响当前配置，且无法撤销。请先导出备份。确定继续吗？',
      { title: '导入全部数据', confirmText: '确认覆盖并导入' }
    )
    if (!ok) return
    await configApi.import(config)
    notify.success('全部数据已导入，正在刷新页面')
    localStorage.removeItem('serverDomain')
    localStorage.removeItem('subscriptionAggregationEnabled')
    window.location.reload()
  } catch (error) {
    console.error('导入配置失败:', error)
    notify.error('导入失败，请检查文件格式')
  } finally {
    input.value = ''
  }
}

const resetConfig = async () => {
  const ok = await confirmDanger(
    '重置将清空共享资源、所有配置及 Agent，并恢复默认系统设置。不仅影响当前配置，且无法撤销。请先导出备份。',
    { title: '重置全部数据', confirmText: '确认重置' }
  )
  if (!ok) return

  try {
    await api.post('/config/reset')
    notify.success('全部数据已重置，正在刷新页面')
    localStorage.removeItem('serverDomain')
    localStorage.removeItem('subscriptionAggregationEnabled')

    // 刷新统计
    setTimeout(() => {
      // 刷新页面以加载新配置
      window.location.reload()
    }, 500)
  } catch (error) {
    if (error !== 'cancel') {
      notify.error('重置失败')
    }
  }
}

// 备份相关方法
const showBackupDialog = async () => {
  try {
    // 加载备份配置
    const response = await api.get('/backup/config')
    if (response.data) {
      backupForm.value = {
        webdav_url: response.data.webdav_url || '',
        webdav_username: response.data.webdav_username || '',
        webdav_password: response.data.webdav_password || '',
        webdav_path: response.data.webdav_path || '/config-flow-backup/',
        auto_backup: response.data.auto_backup || false,
        keep_latest: response.data.keep_latest ?? 30,
        keep_daily: response.data.keep_daily ?? 30
      }
    }
    backupDialogVisible.value = true
  } catch (error) {
    console.error('加载备份配置失败', error)
    notify.error('加载备份配置失败')
  }
}

useSettingLink({
  keys: ['section'], ready: () => true,
  matches: query => query.section === 'backup',
  open: async (_query, current) => {
    if (backupDialogVisible.value) return
    await showBackupDialog()
    if (!current()) return
    await focusSetting(document.getElementById('webdav-url'))
  },
})

const testWebDAVConnection = async () => {
  if (!backupForm.value.webdav_url) {
    notify.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    notify.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    notify.warning('请输入密码')
    return
  }

  try {
    testingConnection.value = true
    await api.post('/backup/test', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path
    })
    notify.success('连接测试成功')
  } catch (error) {
    console.error('测试连接失败', error)
    const errorMsg = isAxiosError<{ message?: string }>(error) ? error.response?.data?.message : undefined
    notify.error(errorMsg || '连接失败，请检查 WebDAV 地址、用户名和密码')
  } finally {
    testingConnection.value = false
  }
}

const backupNow = async () => {
  if (!backupForm.value.webdav_url) {
    notify.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    notify.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    notify.warning('请输入密码')
    return
  }
  if (!backupRetentionValid()) return

  try {
    backingUp.value = true
    const response = await api.post('/backup/now', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path,
      keep_latest: backupForm.value.keep_latest,
      keep_daily: backupForm.value.keep_daily
    })
    notify.success(response.data?.message || '全部数据已备份到 WebDAV')
  } catch (error) {
    console.error('备份失败', error)
    const errorMsg = isAxiosError<{ message?: string }>(error) ? error.response?.data?.message : undefined
    notify.error(errorMsg || '备份失败，请检查 WebDAV 连接和备份路径')
  } finally {
    backingUp.value = false
  }
}

const saveBackupConfig = async () => {
  if (!backupRetentionValid()) return
  try {
    savingBackup.value = true
    await api.post('/backup/config', backupForm.value)
    notify.success('备份配置已保存')
    backupDialogVisible.value = false
  } catch (error) {
    console.error('保存备份配置失败', error)
    notify.error('保存备份设置失败')
  } finally {
    savingBackup.value = false
  }
}

onMounted(async () => {
  await Promise.all([
    loadConfigToken(),
    loadRuleFetchProxy(),
    loadDependencies(),
    (async () => {
      try {
        const response = await serverDomainApi.get()
        serverDomain.value = response.data.server_domain?.trim() || window.location.origin
        savedServerDomain.value = serverDomain.value
        localStorage.setItem('serverDomain', serverDomain.value)
      } catch {
        notify.error('加载服务域名失败')
      }
    })(),
    (async () => {
      try {
        const response = await api.get('/settings/subscription-aggregation')
        subscriptionAggregationEnabled.value = Boolean(response.data.enabled)
        localStorage.setItem('subscriptionAggregationEnabled', String(subscriptionAggregationEnabled.value))
      } catch {
        notify.error('加载订阅聚合开关失败')
      }
    })()
  ])
})
</script>
