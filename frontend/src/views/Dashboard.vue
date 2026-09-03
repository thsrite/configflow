<template>
  <div class="dashboard-page">
    <PageHeader title="数据统计" :description="scopeSummary" />

    <!-- 统计卡片区域 -->
    <div class="stats-grid">
      <StatCard
        v-for="stat in statsData"
        :key="stat.label"
        :label="stat.label"
        :value="stat.value"
        :scope="stat.scope"
        :hint="stat.hint"
        :route="stat.route"
      />
    </div>

    <!-- Agent 状态区域 -->
    <div class="agent-status-section" v-if="agents.length > 0">
      <AgentStatus :agents="agents" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { statsApi, agentApi } from '@/api'
import { ElMessage } from 'element-plus'
import StatCard from '@/components/StatCard.vue'
import AgentStatus from '@/components/AgentStatus.vue'
import PageHeader from '@/components/shell/PageHeader.vue'
import { useProfileStore } from '@/stores/profile'

interface Agent {
  id: string
  name: string
  host: string
  port: number
  service_type: string
  status: string
  config_version?: string
  last_heartbeat?: string
  version?: string
  deployment_method?: string
}

// 当前时间
const currentTime = ref('')

// 更新时间
const updateTime = () => {
  const now = new Date()
  const hours = now.getHours().toString().padStart(2, '0')
  const minutes = now.getMinutes().toString().padStart(2, '0')
  const date = now.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'long'
  })
  currentTime.value = `${date} ${hours}:${minutes}`
}

// 统计数据
type StatScope = 'shared' | 'profile' | 'system'

interface StatItem {
  label: string
  value: number
  scope: StatScope
  hint: string
  route: string
}

const statsData = ref<StatItem[]>([
  { label: '订阅来源', value: 0, scope: 'shared', hint: '共享资源', route: '/subscriptions' },
  { label: '节点库', value: 0, scope: 'shared', hint: '共享资源', route: '/nodes' },
  { label: '策略组', value: 0, scope: 'profile', hint: '当前配置', route: '/proxy-groups' },
  { label: '策略规则', value: 0, scope: 'profile', hint: '当前配置', route: '/rules' }
])

// Agent 列表
const agents = ref<Agent[]>([])

const profileStore = useProfileStore()

// 页面说明同时点出两类作用域，让用户立刻分辨共享与私有
const scopeSummary = computed(() => {
  const name = profileStore.activeProfile.value?.name || profileStore.activeProfileId.value
  return `共享资源 · 4 类 · 当前配置「${name}」`
})

// 加载 Agent 列表
const loadAgents = async () => {
  try {
    const response = await agentApi.getAll()
    agents.value = response.data || []
  } catch (error) {
    // 静默失败，不显示错误消息
    console.error('加载 Agent 列表失败:', error)
    agents.value = []
  }
}

// 加载总览统计数据
const loadOverview = async () => {
  try {
    const response = await statsApi.getOverview()
    if (response.data.success) {
      const data = response.data.data

      // 更新统计卡片数据（顺序：订阅、节点、策略组、规则）
      statsData.value[0].value = data.subscriptions.total
      statsData.value[1].value = data.nodes.total
      statsData.value[2].value = data.proxyGroups.total
      statsData.value[3].value = data.rules.total
    }
  } catch (error) {
    console.error('加载统计数据失败:', error)
  }
}

// 加载所有数据
const loadAllData = async () => {
  // 并行加载所有数据
  await Promise.all([
    loadOverview(),
    loadAgents()
  ])
}

let timeInterval: number
let dataInterval: number

onMounted(async () => {
  updateTime()

  // 加载所有数据
  await loadAllData()

  // 定时更新时间
  timeInterval = setInterval(updateTime, 60000)

  // 定时刷新数据（每30秒）
  dataInterval = setInterval(loadAllData, 30000)
})

onUnmounted(() => {
  if (timeInterval) {
    clearInterval(timeInterval)
  }
  if (dataInterval) {
    clearInterval(dataInterval)
  }
})
</script>

<style scoped>
.dashboard-page {
  display: flex;
  flex-direction: column;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--cf-sp-3);
  margin-bottom: var(--cf-sp-5);
}

.agent-status-section {
  margin-bottom: var(--cf-sp-4);
}

@media (max-width: 640px) {
  .stats-grid {
    grid-template-columns: 1fr 1fr;
    gap: var(--cf-sp-2);
  }
}
</style>
