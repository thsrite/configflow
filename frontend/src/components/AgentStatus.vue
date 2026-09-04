<template>
  <div class="agent-status">
    <div class="agents-list" v-if="agents.length > 0">
      <div
        v-for="agent in agents"
        :key="agent.id"
        class="agent-row"
        @click="goToAgentManagement"
      >
        <!-- 左侧：Agent 信息 -->
        <div class="agent-info-section">
          <div class="agent-header">
            <div class="agent-title">
              <h3 class="agent-name">{{ agent.name }}</h3>
              <div class="status-indicator" :class="agent.status === 'online' ? 'status-online' : 'status-offline'">
                <span class="status-dot"></span>
                <span class="status-text">{{ agent.status === 'online' ? '在线' : '离线' }}</span>
              </div>
            </div>
            <div class="agent-badges">
              <span class="badge badge-type" :class="'type-' + agent.service_type">
                {{ agent.service_type === 'mihomo' ? 'Mihomo' : 'MosDNS' }}
              </span>
              <span v-if="agent.deployment_method" class="badge badge-deploy" :class="'deploy-' + agent.deployment_method">
                {{ agent.deployment_method === 'shell' ? 'Shell' : agent.deployment_method === 'docker' ? 'Docker' : agent.deployment_method }}
              </span>
            </div>
          </div>

          <div class="agent-meta">
            <div class="meta-item">
              <el-icon class="meta-icon"><Connection /></el-icon>
              <span class="meta-value">{{ agent.host }}:{{ agent.port }}</span>
            </div>
            <div class="meta-item">
              <el-icon class="meta-icon"><InfoFilled /></el-icon>
              <span class="meta-value">{{ agent.version || 'N/A' }}</span>
            </div>
            <div class="meta-item">
              <el-icon class="meta-icon"><Clock /></el-icon>
              <span class="meta-value">{{ formatTime(agent.last_heartbeat) }}</span>
            </div>
          </div>
        </div>

        <!-- 右侧：监控图表 -->
        <div class="agent-metrics-section" v-if="agent.system_metrics">
          <!-- 1. CPU -->
          <div class="metric-item">
            <div class="metric-header">
              <span class="metric-name">CPU</span>
              <span class="metric-value" :class="getMetricClass(agent.system_metrics.cpu?.usage_percent)">
                {{ formatPercent(agent.system_metrics.cpu?.usage_percent) }}
              </span>
            </div>
            <div class="metric-chart">
              <v-chart
                v-if="metricsHistory[agent.id]?.length > 0"
                :option="getCpuChartOption(agent.id)"
                :autoresize="true"
                style="height: 110px; width: 100%;"
              />
              <div v-else class="no-data">等待数据...</div>
            </div>
          </div>

          <!-- 2. 内存 -->
          <div class="metric-item">
            <div class="metric-header">
              <span class="metric-name">内存</span>
              <span class="metric-value" :class="getMetricClass(agent.system_metrics.memory?.used_percent)">
                {{ formatPercent(agent.system_metrics.memory?.used_percent) }}
              </span>
            </div>
            <div class="metric-chart">
              <v-chart
                v-if="metricsHistory[agent.id]?.length > 0"
                :option="getMemoryChartOption(agent.id)"
                :autoresize="true"
                style="height: 110px; width: 100%;"
              />
              <div v-else class="no-data">等待数据...</div>
            </div>
          </div>

          <!-- 3. 网络速率 -->
          <div class="metric-item">
            <div class="metric-header metric-header-network">
              <span class="metric-name">网络速率</span>
              <span class="metric-value metric-network">
                <span class="upload">↑{{ formatSpeed(agent.system_metrics.network?.speed_sent) }}</span>
                <span class="download">↓{{ formatSpeed(agent.system_metrics.network?.speed_recv) }}</span>
              </span>
            </div>
            <div class="metric-chart">
              <v-chart
                v-if="metricsHistory[agent.id]?.length > 0"
                :option="getNetworkChartOption(agent.id)"
                :autoresize="true"
                style="height: 110px; width: 100%;"
              />
              <div v-else class="no-data">等待数据...</div>
            </div>
          </div>

          <!-- 4. 流量统计 -->
          <div class="metric-item" v-if="trafficStats[agent.id]">
            <div class="metric-header metric-header-network">
              <span class="metric-name">流量统计</span>
              <span class="metric-value metric-network">
                <span class="upload">↑{{ formatBytes(trafficStats[agent.id].total?.bytes_sent) }}</span>
                <span class="download">↓{{ formatBytes(trafficStats[agent.id].total?.bytes_recv) }}</span>
              </span>
            </div>
            <div class="metric-chart" v-if="trafficTrend[agent.id]?.length > 0">
              <v-chart
                :option="getTrafficTrendChartOption(agent.id)"
                :autoresize="true"
                style="height: 110px; width: 100%;"
              />
            </div>
          </div>

          <!-- 5. 磁盘 -->
          <div class="metric-item">
            <div class="metric-header">
              <span class="metric-name">磁盘</span>
              <span class="metric-value" :class="getMetricClass(agent.system_metrics.disk?.used_percent)">
                {{ formatPercent(agent.system_metrics.disk?.used_percent) }}
              </span>
            </div>
            <div class="metric-chart">
              <v-chart
                v-if="metricsHistory[agent.id]?.length > 0"
                :option="getDiskChartOption(agent.id)"
                :autoresize="true"
                style="height: 110px; width: 100%;"
              />
              <div v-else class="no-data">等待数据...</div>
            </div>
          </div>
        </div>

        <!-- 无监控数据的占位 -->
        <div class="agent-metrics-placeholder" v-else>
          <span class="placeholder-text">暂无监控数据</span>
        </div>
      </div>
    </div>

    <div class="empty-state" v-else>
      <p>暂无 Agent</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { Connection, Clock, InfoFilled } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import api from '@/api'

// Register ECharts components
use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

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
  system_metrics?: {
    cpu?: { usage_percent: number; core_count: number }
    memory?: { total: number; used: number; available: number; used_percent: number }
    disk?: { total: number; used: number; free: number; used_percent: number }
    network?: { bytes_sent: number; bytes_recv: number; speed_sent: number; speed_recv: number }
    collected_at?: string
  }
}

interface Props {
  agents: Agent[]
}

const props = defineProps<Props>()
const router = useRouter()

// 存储每个 Agent 的历史数据
const metricsHistory = ref<Record<string, any[]>>({})

// 存储每个 Agent 的流量统计数据
const trafficStats = ref<Record<string, any>>({})

// 存储每个 Agent 的流量趋势数据
const trafficTrend = ref<Record<string, any[]>>({})

// 跳转到 Agent 管理页面
const goToAgentManagement = () => {
  router.push('/agents')
}

// 加载 Agent 的历史监控数据
const loadMetricsHistory = async (agentId: string) => {
  try {
    const response = await api.get(`/agents/${agentId}/metrics/history?hours=1`)
    if (response.data.success && response.data.data.history) {
      metricsHistory.value[agentId] = response.data.data.history.slice(-20) // 最近20个数据点
    }
  } catch (error) {
    console.error(`加载 Agent ${agentId} 监控历史失败:`, error)
  }
}

// 加载 Agent 的流量统计数据
const loadTrafficStats = async (agentId: string) => {
  try {
    // 加载多个周期的统计数据
    const [totalRes, todayRes, hours24Res] = await Promise.all([
      api.get(`/agents/${agentId}/traffic/stats?period=total`),
      api.get(`/agents/${agentId}/traffic/stats?period=today`),
      api.get(`/agents/${agentId}/traffic/stats?period=hours_24`)
    ])

    trafficStats.value[agentId] = {
      total: totalRes.data.data?.stats || {},
      today: todayRes.data.data?.stats || {},
      hours_24: hours24Res.data.data?.stats || {}
    }
  } catch (error) {
    console.error(`加载 Agent ${agentId} 流量统计失败:`, error)
  }
}

// 加载 Agent 的流量趋势数据
const loadTrafficTrend = async (agentId: string) => {
  try {
    const response = await api.get(`/agents/${agentId}/traffic/trend?hours=24&interval=5`)
    if (response.data.success && response.data.data.trend) {
      trafficTrend.value[agentId] = response.data.data.trend
    }
  } catch (error) {
    console.error(`加载 Agent ${agentId} 流量趋势失败:`, error)
  }
}

// 加载所有 Agent 的历史数据
const loadAllMetricsHistory = async () => {
  for (const agent of props.agents) {
    if (agent.system_metrics) {
      await Promise.all([
        loadMetricsHistory(agent.id),
        loadTrafficStats(agent.id),
        loadTrafficTrend(agent.id)
      ])
    }
  }
}

const chartTooltipBase = {
  appendToBody: true,
  confine: false,
  extraCssText: 'z-index: 9999; pointer-events: none;'
}

const formatTrafficAxisLabel = (value: number) => {
  if (value >= 1024) {
    const tb = value / 1024
    return `${tb >= 10 ? tb.toFixed(0) : tb.toFixed(1)} TB`
  }
  return `${value.toFixed(0)} GB`
}

// CPU 图表配置
const getCpuChartOption = (agentId: string) => {
  const history = metricsHistory.value[agentId] || []
  const data = history.map(m => m.cpu?.usage_percent || 0)

  return {
    grid: { left: 0, right: 0, top: 5, bottom: 5 },
    xAxis: { type: 'category', show: false, boundaryGap: false },
    yAxis: { type: 'value', show: false, max: 100 },
    tooltip: {
      ...chartTooltipBase,
      trigger: 'axis',
      formatter: '{c}%',
      axisPointer: { type: 'line' }
    },
    series: [{
      type: 'line',
      data: data,
      smooth: true,
      symbol: 'none',
      lineStyle: { width: 2.5, color: '#409EFF' },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(64, 158, 255, 0.35)' },
            { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
          ]
        }
      }
    }]
  }
}

// 内存图表配置
const getMemoryChartOption = (agentId: string) => {
  const history = metricsHistory.value[agentId] || []
  const data = history.map(m => m.memory?.used_percent || 0)

  return {
    grid: { left: 0, right: 0, top: 5, bottom: 5 },
    xAxis: { type: 'category', show: false, boundaryGap: false },
    yAxis: { type: 'value', show: false, max: 100 },
    tooltip: {
      ...chartTooltipBase,
      trigger: 'axis',
      formatter: '{c}%',
      axisPointer: { type: 'line' }
    },
    series: [{
      type: 'line',
      data: data,
      smooth: true,
      symbol: 'none',
      lineStyle: { width: 2.5, color: '#67C23A' },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(103, 194, 58, 0.35)' },
            { offset: 1, color: 'rgba(103, 194, 58, 0.05)' }
          ]
        }
      }
    }]
  }
}

// 磁盘图表配置
const getDiskChartOption = (agentId: string) => {
  const history = metricsHistory.value[agentId] || []
  const data = history.map(m => m.disk?.used_percent || 0)

  return {
    grid: { left: 0, right: 0, top: 5, bottom: 5 },
    xAxis: { type: 'category', show: false, boundaryGap: false },
    yAxis: { type: 'value', show: false, max: 100 },
    tooltip: {
      ...chartTooltipBase,
      trigger: 'axis',
      formatter: '{c}%',
      axisPointer: { type: 'line' }
    },
    series: [{
      type: 'line',
      data: data,
      smooth: true,
      symbol: 'none',
      lineStyle: { width: 2.5, color: '#E6A23C' },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(230, 162, 60, 0.35)' },
            { offset: 1, color: 'rgba(230, 162, 60, 0.05)' }
          ]
        }
      }
    }]
  }
}

// 网络图表配置
const getNetworkChartOption = (agentId: string) => {
  const history = metricsHistory.value[agentId] || []
  const uploadData = history.map(m => ((m.network?.speed_sent || 0) / 1024).toFixed(1))
  const downloadData = history.map(m => ((m.network?.speed_recv || 0) / 1024).toFixed(1))

  return {
    grid: { left: 0, right: 0, top: 5, bottom: 5 },
    xAxis: { type: 'category', show: false, boundaryGap: false },
    yAxis: { type: 'value', show: false },
    tooltip: {
      ...chartTooltipBase,
      trigger: 'axis',
      formatter: (params: any) => `↑${params[0].value} KB/s<br/>↓${params[1].value} KB/s`,
      axisPointer: { type: 'line' }
    },
    series: [
      {
        type: 'line',
        data: uploadData,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: '#409EFF' }
      },
      {
        type: 'line',
        data: downloadData,
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: '#67C23A' }
      }
    ]
  }
}

// 流量趋势图表配置
const getTrafficTrendChartOption = (agentId: string) => {
  const trend = trafficTrend.value[agentId] || []
  const stats = trafficStats.value[agentId]

  const timestamps = trend.map(t => {
    const date = new Date(t.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })

  // 将累计流量转换为 GB 单位
  const uploadData = trend.map(t => ((t.bytes_sent || 0) / (1024 * 1024 * 1024)).toFixed(3))
  const downloadData = trend.map(t => ((t.bytes_recv || 0) / (1024 * 1024 * 1024)).toFixed(3))

  // 今日流量
  const todayUpload = stats?.today?.bytes_sent_delta || 0
  const todayDownload = stats?.today?.bytes_recv_delta || 0

  return {
    grid: { left: 8, right: 10, top: 8, bottom: 0, containLabel: true },
    xAxis: {
      type: 'category',
      data: timestamps,
      axisLabel: { fontSize: 10, color: '#666' },
      axisLine: { lineStyle: { color: '#ddd' } }
    },
    yAxis: {
      type: 'value',
      min: 0,
      splitNumber: 4,
      axisLabel: {
        fontSize: 10,
        color: '#666',
        margin: 8,
        formatter: formatTrafficAxisLabel
      },
      splitLine: { lineStyle: { color: '#f0f0f0' } }
    },
    tooltip: {
      ...chartTooltipBase,
      trigger: 'axis',
      formatter: (params: any) => {
        const time = params[0].axisValue
        const upload = parseFloat(params[0].value).toFixed(2)
        const download = parseFloat(params[1].value).toFixed(2)

        // 格式化今日流量
        const formatBytesInTooltip = (bytes: number) => {
          const gb = bytes / (1024 * 1024 * 1024)
          return gb >= 1 ? `${gb.toFixed(2)} GB` : `${(bytes / (1024 * 1024)).toFixed(0)} MB`
        }

        return `${time}<br/>` +
               `累计: ↑${upload} GB ↓${download} GB<br/>` +
               `今日: ↑${formatBytesInTooltip(todayUpload)} ↓${formatBytesInTooltip(todayDownload)}`
      },
      axisPointer: { type: 'line' }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        data: uploadData,
        smooth: true,
        symbol: 'circle',
        symbolSize: 4,
        lineStyle: { width: 2, color: '#409EFF' },
        areaStyle: { color: 'rgba(64, 158, 255, 0.1)' }
      },
      {
        name: '下载',
        type: 'line',
        data: downloadData,
        smooth: true,
        symbol: 'circle',
        symbolSize: 4,
        lineStyle: { width: 2, color: '#67C23A' },
        areaStyle: { color: 'rgba(103, 194, 58, 0.1)' }
      }
    ]
  }
}

// 格式化时间
const formatTime = (timeStr?: string) => {
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

// 格式化百分比
const formatPercent = (value: number | undefined) => {
  if (value === undefined || value === null) return 'N/A'
  return `${value.toFixed(1)}%`
}

// 格式化速度
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

// 格式化字节数
const formatBytes = (bytes: number | undefined) => {
  if (bytes === undefined || bytes === null) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

// 获取监控指标的样式类
const getMetricClass = (percent: number | undefined) => {
  if (percent === undefined || percent === null) return ''
  if (percent < 60) return 'metric-good'
  if (percent < 80) return 'metric-warning'
  return 'metric-danger'
}

// 定时刷新历史数据
let refreshInterval: number

onMounted(async () => {
  await loadAllMetricsHistory()
  // 每30秒刷新一次
  refreshInterval = setInterval(loadAllMetricsHistory, 30000)
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
})
</script>

<style scoped>
.agent-status {
  width: 100%;
  height: 100%;
}

.agents-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.agent-row {
  display: flex;
  align-items: stretch;
  gap: 16px;
  background: var(--cf-s2);
  backdrop-filter: blur(20px);
  border-radius: 20px;
  padding: 24px 28px;
  box-shadow: 0 4px 24px rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.06);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: pointer;
  position: relative;
  overflow: visible;
}

.agent-row::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  width: 4px;
  height: 100%;
  background: var(--cf-s2);
  opacity: 0;
  transition: opacity 0.3s;
  border-radius: 20px 0 0 20px;
}

.agent-row:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 40px rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.12);
}

.agent-row:hover::before {
  opacity: 1;
}

/* 左侧信息区域 */
.agent-info-section {
  flex: 0 0 140px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.agent-header {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.agent-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.agent-name {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: var(--cf-fg);
  letter-spacing: -0.3px;
}

.status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: 600;
}

.status-online {
  background: rgba(76, 209, 55, 0.1);
  color: var(--cf-success);
}

.status-offline {
  background: rgba(245, 108, 108, 0.1);
  color: var(--cf-danger);
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: pulse 2s infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}

.agent-badges {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.badge {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.2px;
}

.badge-type.type-mihomo {
  background: rgba(107, 115, 255, 0.1);
  color: var(--cf-primary);
}

.badge-type.type-mosdns {
  background: rgba(139, 143, 255, 0.1);
  color: var(--cf-primary-hover);
}

.badge-deploy.deploy-shell {
  background: rgba(250, 140, 22, 0.1);
  color: var(--cf-warning);
}

.badge-deploy.deploy-docker {
  background: rgba(24, 144, 255, 0.1);
  color: var(--cf-primary);
}

.agent-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.meta-icon {
  font-size: 14px;
  color: var(--cf-primary);
}

.meta-value {
  font-size: 12px;
  font-weight: 500;
  color: var(--cf-fg);
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}

/* 右侧监控图表区域 */
.agent-metrics-section {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 12px;
  min-width: 0;
}

.metric-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.metric-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 24px;
  margin-bottom: 0;
  min-width: 0;
}

.metric-name {
  flex: 0 0 auto;
  font-size: 12px;
  font-weight: 700;
  color: var(--cf-fg-2);
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.metric-value {
  flex: 0 1 auto;
  font-size: 14px;
  font-weight: 700;
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
  line-height: 20px;
  padding: 2px 8px;
  border-radius: 6px;
  white-space: nowrap;
}

.metric-value.metric-network {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-height: 24px;
  padding: 0;
  font-size: 11px;
}

.metric-network .upload {
  color: var(--cf-primary);
}

.metric-network .download {
  color: var(--cf-success);
}

.metric-good {
  color: var(--cf-success);
  background: rgba(103, 194, 58, 0.12);
}

.metric-warning {
  color: var(--cf-warning);
  background: rgba(230, 162, 60, 0.12);
}

.metric-danger {
  color: var(--cf-danger);
  background: rgba(245, 108, 108, 0.12);
}

.metric-chart {
  background: rgba(107, 115, 255, 0.02);
  border-radius: 8px;
  border: 1px solid rgba(107, 115, 255, 0.06);
  overflow: visible;
}

.no-data {
  height: 110px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  color: var(--cf-fg-3);
}

.agent-metrics-placeholder {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(107, 115, 255, 0.03);
  border-radius: 12px;
  border: 1px dashed rgba(107, 115, 255, 0.15);
}

.placeholder-text {
  font-size: 13px;
  color: var(--cf-fg-3);
  font-weight: 500;
}

.empty-state {
  text-align: center;
  padding: 48px 20px;
  color: var(--cf-fg-3);
  font-size: 14px;
  background: rgba(107, 115, 255, 0.03);
  border-radius: 16px;
  border: 1px dashed rgba(107, 115, 255, 0.15);
}

.empty-state p {
  margin: 0;
  font-weight: 500;
}

/* 响应式 */
@media (max-width: 1600px) {
  .agent-metrics-section {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 1200px) {
  .agent-metrics-section {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 1024px) {
  .agent-row {
    flex-direction: column;
  }

  .agent-info-section {
    flex: none;
  }

  .agent-metrics-section {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 768px) {
  .agent-metrics-section {
    grid-template-columns: repeat(2, 1fr);
    gap: 16px 12px;
  }

  .agent-row {
    padding: 20px;
  }

  .metric-header {
    gap: 4px 8px;
  }

  .metric-header-network {
    align-items: flex-start;
    flex-direction: column;
    justify-content: flex-start;
    min-height: 44px;
  }

  .metric-header-network .metric-value.metric-network {
    align-items: flex-start;
    flex-wrap: wrap;
    justify-content: flex-start;
    gap: 2px 8px;
    line-height: 16px;
    min-height: 0;
    width: 100%;
  }

  .metric-header-network .upload,
  .metric-header-network .download {
    white-space: nowrap;
  }
}
</style>
