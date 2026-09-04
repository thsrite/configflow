<template>
  <div class="dashboard-page">
    <PageHeader title="数据统计" description="概览运行状态与资源健康，快速掌握代理配置体系情况。" />

    <!-- 作用域说明：共享资源与当前配置并置，让用户立刻分辨数据归属 -->
    <div class="cf-scopes">
      <span class="cf-scopes__chip is-shared">
        <el-icon><Files /></el-icon>
        共享资源 · {{ sharedKinds }} 类
      </span>
      <span class="cf-scopes__chip is-profile">
        <el-icon><Postcard /></el-icon>
        当前配置 · {{ profileName }}
      </span>
    </div>

    <div class="cf-dash">
      <div class="cf-dash__main">
        <KpiPanel :items="kpis" />
        <ActivityList v-model:active="activeTab" :rows="visibleActivity" :tabs="activityTabs" />
      </div>

      <aside class="cf-dash__side">
        <HealthPanel
          :rows="healthRows"
          :checked-at="checkedAt"
          :loading="loading"
          @refresh="loadAllData"
        />
        <QuickActions :actions="quickActions" @run="runQuickAction" />
      </aside>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { statsApi, agentApi, profileApi, subscriptionApi } from '@/api'
import api from '@/api'
import PageHeader from '@/components/shell/PageHeader.vue'
import KpiPanel, { type KpiItem } from '@/components/dashboard/KpiPanel.vue'
import HealthPanel, { type HealthRow } from '@/components/dashboard/HealthPanel.vue'
import QuickActions, { type QuickAction } from '@/components/dashboard/QuickActions.vue'
import ActivityList, { type ActivityRow } from '@/components/dashboard/ActivityList.vue'
import { useProfileStore } from '@/stores/profile'

const router = useRouter()
const profileStore = useProfileStore()

const loading = ref(false)
const checkedAt = ref('')

const counts = ref({ subscriptions: 0, nodes: 0, proxyGroups: 0, rules: 0 })
const profileCount = ref(0)
const agents = ref<any[]>([])
const subscriptions = ref<any[]>([])
const activity = ref<ActivityRow[]>([])

const sharedKinds = 4

const profileName = computed(
  () => profileStore.activeProfile.value?.name || profileStore.activeProfileId.value
)

/* ---------- KPI：与概念图一致的四项 ---------- */
const kpis = computed<KpiItem[]>(() => [
  { label: '配置空间', value: profileCount.value, icon: 'Setting', scope: 'system', route: '/profiles' },
  { label: '订阅来源', value: counts.value.subscriptions, icon: 'Link', scope: 'shared', route: '/subscriptions' },
  { label: '节点', value: counts.value.nodes, icon: 'Connection', scope: 'shared', route: '/nodes' },
  { label: 'Agent', value: agents.value.length, icon: 'Monitor', scope: 'system', route: '/agents' }
])

/* ---------- 配置健康：全部由真实数据推导，不展示无法计算的项 ---------- */
const healthRows = computed<HealthRow[]>(() => {
  const rows: HealthRow[] = []

  const total = subscriptions.value.length
  const enabled = subscriptions.value.filter(s => s.enabled).length
  rows.push({
    label: '订阅来源',
    value: total ? `${enabled}/${total} 启用` : '未配置',
    level: total === 0 ? 'warn' : enabled === total ? 'ok' : 'warn'
  })

  rows.push({
    label: '节点库',
    value: `${counts.value.nodes} 个节点`,
    level: counts.value.nodes > 0 ? 'ok' : 'warn'
  })

  rows.push({
    label: '策略组',
    value: `${counts.value.proxyGroups} 组`,
    level: counts.value.proxyGroups > 0 ? 'ok' : 'warn'
  })

  rows.push({
    label: '策略规则',
    value: `${counts.value.rules} 条`,
    level: counts.value.rules > 0 ? 'ok' : 'warn'
  })

  const online = agents.value.filter(a => a.status === 'online').length
  rows.push({
    label: 'Agent 状态',
    value: agents.value.length ? `${online}/${agents.value.length} 在线` : '未注册',
    level: agents.value.length === 0 ? 'warn' : online === agents.value.length ? 'ok' : 'err'
  })

  return rows
})

/* ---------- 快速操作：一个视图只有一个主操作 ---------- */
const quickActions = computed<QuickAction[]>(() => [
  { label: '生成配置', icon: 'Download', primary: true, route: '/generate' },
  { label: '更新订阅', icon: 'Refresh', route: '/subscriptions' },
  { label: '推送到 Agent', icon: 'Promotion', route: '/agents', disabled: agents.value.length === 0 },
  { label: '查看运行日志', icon: 'Tickets', route: '/logs' }
])

const runQuickAction = (action: QuickAction) => {
  if (action.route) router.push(action.route)
}

/* ---------- 运行状态：从真实日志解析，不编造记录 ---------- */
const activityTabs = ['全部', '订阅', '配置生成', 'Agent']
const activeTab = ref('全部')

const visibleActivity = computed(() => {
  if (activeTab.value === '全部') return activity.value
  return activity.value.filter(row => row.task.includes(activeTab.value))
})

const LOG_LINE = /^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})[,.]?\d*\s+-\s+([\w.]+)\s+-\s+(\w+)\s+-\s+(.*)$/

const classifyTask = (logger: string, message: string): string => {
  const text = `${logger} ${message}`.toLowerCase()
  if (text.includes('subscription') || text.includes('订阅')) return '订阅'
  if (text.includes('generate') || text.includes('生成')) return '配置生成'
  if (text.includes('agent')) return 'Agent'
  return '系统'
}

const parseLogs = (lines: string[]): ActivityRow[] =>
  lines
    .map(line => {
      const m = LOG_LINE.exec(line)
      if (!m) return null
      const [, time, logger, level, message] = m
      const lv = level === 'ERROR' ? 'err' : level === 'WARNING' ? 'warn' : 'ok'
      return {
        task: classifyTask(logger, message),
        detail: message,
        status: lv === 'ok' ? '成功' : lv === 'warn' ? '警告' : '失败',
        level: lv as ActivityRow['level'],
        time: time.slice(5)
      }
    })
    .filter((row): row is ActivityRow => row !== null)
    .reverse()

/* ---------- 数据加载 ---------- */
const loadAllData = async () => {
  loading.value = true
  try {
    const results = await Promise.allSettled([
      statsApi.getOverview(),
      agentApi.getAll(),
      profileApi.list(),
      subscriptionApi.getAll(),
      api.get('/logs/tail', { params: { lines: 40 } })
    ])

    const [stats, agentRes, profileRes, subRes, logRes] = results

    if (stats.status === 'fulfilled' && stats.value.data?.success) {
      const d = stats.value.data.data
      counts.value = {
        subscriptions: d.subscriptions?.total ?? 0,
        nodes: d.nodes?.total ?? 0,
        proxyGroups: d.proxyGroups?.total ?? 0,
        rules: d.rules?.total ?? 0
      }
    }
    if (agentRes.status === 'fulfilled') agents.value = agentRes.value.data || []
    if (profileRes.status === 'fulfilled') {
      const data = profileRes.value.data
      profileCount.value = (Array.isArray(data) ? data : data?.profiles || []).length
    }
    if (subRes.status === 'fulfilled') subscriptions.value = subRes.value.data || []
    if (logRes.status === 'fulfilled') {
      activity.value = parseLogs(logRes.value.data?.logs || []).slice(0, 12)
    }

    checkedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } finally {
    loading.value = false
  }
}

let timer: number

onMounted(async () => {
  await loadAllData()
  timer = setInterval(loadAllData, 30000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.dashboard-page {
  display: flex;
  flex-direction: column;
}

.cf-scopes {
  display: flex;
  gap: var(--cf-sp-2);
  flex-wrap: wrap;
  margin-bottom: var(--cf-sp-4);
}

.cf-scopes__chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border-radius: var(--cf-r-md);
  border: 1px solid var(--cf-bd);
  font-size: 12.5px;
  font-weight: 600;
  color: var(--cf-fg);
}

.cf-scopes__chip.is-shared {
  background: var(--cf-shared-soft);
  border-color: color-mix(in srgb, var(--cf-shared) 28%, transparent);
}
.cf-scopes__chip.is-shared .el-icon {
  color: var(--cf-shared);
}

.cf-scopes__chip.is-profile {
  background: var(--cf-primary-soft);
  border-color: color-mix(in srgb, var(--cf-primary) 30%, transparent);
}
.cf-scopes__chip.is-profile .el-icon {
  color: var(--cf-primary);
}

/* 概念图：主内容 + 右侧窄辅助列 */
.cf-dash {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: var(--cf-sp-4);
  align-items: start;
}

.cf-dash__main {
  min-width: 0;
}

@media (max-width: 1100px) {
  .cf-dash {
    grid-template-columns: minmax(0, 1fr);
  }
  /* 窄屏下辅助列降为次要区块置于内容之后，而非消失 */
  .cf-dash__side {
    order: 2;
  }
}
</style>
