<template>
  <div class="logs-container">
    <!-- 页面标题 -->
    <PageHeader title="日志" description="运行记录与错误排查，支持关键词搜索、级别过滤与自动刷新" />

    <!-- 顶部工具栏 -->
    <el-card class="toolbar-card" shadow="never">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="searchKeyword"
            placeholder="搜索关键词"
            clearable
            style="width: 200px"
            @change="loadLogs"
          >
            <template #prefix>
              <el-icon><Search /></el-icon>
            </template>
          </el-input>

          <el-select
            v-model="logLevel"
            placeholder="日志级别"
            clearable
            style="width: 130px"
            @change="loadLogs"
          >
            <el-option label="全部" value="" />
            <el-option label="DEBUG" value="DEBUG" />
            <el-option label="INFO" value="INFO" />
            <el-option label="WARNING" value="WARNING" />
            <el-option label="ERROR" value="ERROR" />
            <el-option label="CRITICAL" value="CRITICAL" />
          </el-select>

          <el-select
            v-model="logLines"
            placeholder="显示行数"
            style="width: 130px"
            @change="loadLogs"
          >
            <el-option label="100 行" :value="100" />
            <el-option label="200 行" :value="200" />
            <el-option label="500 行" :value="500" />
            <el-option label="1000 行" :value="1000" />
            <el-option label="全部" :value="10000" />
          </el-select>
        </div>

        <div class="toolbar-right">
          <el-switch
            v-model="autoRefresh"
            active-text="自动刷新"
            @change="toggleAutoRefresh"
          />

          <el-button @click="loadLogs" :loading="loading">
            <el-icon><Refresh /></el-icon>
            刷新
          </el-button>

          <el-button type="primary" @click="scrollToBottom">
            <el-icon><Bottom /></el-icon>
            到底部
          </el-button>

          <el-button type="danger" @click="clearLogs">
            <el-icon><Delete /></el-icon>
            清空日志
          </el-button>
        </div>
      </div>

      <!-- 日志信息 -->
      <div class="log-info" v-if="logInfo">
        <el-tag size="small" type="info">
          文件: {{ logInfo.path }}
        </el-tag>
        <el-tag size="small" type="success">
          大小: {{ logInfo.size_mb }} MB
        </el-tag>
        <el-tag size="small">
          总行数: {{ totalLines }}
        </el-tag>
        <el-tag size="small" v-if="filteredLines !== totalLines">
          过滤后: {{ filteredLines }} 行
        </el-tag>
      </div>
    </el-card>

    <!-- 日志内容 -->
    <el-card class="log-card" shadow="never">
      <div class="log-content" ref="logContainer" v-loading="loading">
        <div v-if="logs.length === 0" class="empty-logs">
          <el-empty description="暂无日志" />
        </div>
        <div v-else class="log-lines">
          <div
            v-for="(log, index) in logs"
            :key="index"
            class="log-line"
          >
            <span class="line-number">{{ index + 1 }}</span>
            <span class="line-content" v-html="formatLogLine(log)"></span>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/shell/PageHeader.vue'
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, Delete, Bottom } from '@element-plus/icons-vue'
import api from '@/api'

// 日志数据
const logs = ref<string[]>([])
const logInfo = ref<any>(null)
const totalLines = ref(0)
const filteredLines = ref(0)

// 过滤选项
const searchKeyword = ref('')
const logLevel = ref('')
const logLines = ref(100)

// 自动刷新
const autoRefresh = ref(false)
const refreshTimer = ref<number | null>(null)
const refreshInterval = 5000 // 5秒刷新一次

// 加载状态
const loading = ref(false)

// DOM引用
const logContainer = ref<HTMLElement>()

// 加载日志
const loadLogs = async (scrollToEnd = false) => {
  loading.value = true
  try {
    const params: any = {
      lines: logLines.value
    }

    if (searchKeyword.value) {
      params.search = searchKeyword.value
    }

    if (logLevel.value) {
      params.level = logLevel.value
    }

    const response = await api.get('/logs/tail', { params })

    if (response.data.success) {
      logs.value = response.data.logs
      totalLines.value = response.data.total_lines
      filteredLines.value = response.data.filtered_lines

      // 滚动到底部
      if (scrollToEnd) {
        await nextTick()
        scrollToBottom()
      }
    } else {
      ElMessage.error(response.data.message || '加载日志失败')
    }
  } catch (error: any) {
    console.error('Failed to load logs:', error)
    ElMessage.error(error.response?.data?.error || '加载日志失败')
  } finally {
    loading.value = false
  }
}

// 加载日志信息
const loadLogInfo = async () => {
  try {
    const response = await api.get('/logs/info')
    if (response.data.success && response.data.exists) {
      logInfo.value = response.data
    }
  } catch (error) {
    console.error('Failed to load log info:', error)
  }
}

// 滚动到底部
const scrollToBottom = () => {
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}

// 切换自动刷新
const toggleAutoRefresh = (enabled: boolean) => {
  if (enabled) {
    // 启动自动刷新
    refreshTimer.value = window.setInterval(() => {
      loadLogs(true) // 自动刷新时滚动到底部
    }, refreshInterval)
    ElMessage.success('已启用自动刷新（每5秒）')
  } else {
    // 停止自动刷新
    if (refreshTimer.value) {
      clearInterval(refreshTimer.value)
      refreshTimer.value = null
    }
    ElMessage.info('已停止自动刷新')
  }
}

// 清空日志
const clearLogs = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要清空日志文件吗？此操作不可恢复。',
      '警告',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const response = await api.post('/logs/clear')
    if (response.data.success) {
      ElMessage.success('日志已清空')
      logs.value = []
      totalLines.value = 0
      filteredLines.value = 0
      loadLogInfo()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('Failed to clear logs:', error)
      ElMessage.error('清空日志失败')
    }
  }
}

// 格式化日志行，添加颜色高亮
const formatLogLine = (log: string): string => {
  // 转义HTML特殊字符
  const escapeHtml = (text: string) => {
    const div = document.createElement('div')
    div.textContent = text
    return div.innerHTML
  }

  const escapedLog = escapeHtml(log)

  // 高亮时间戳 (YYYY-MM-DD HH:MM:SS,mmm)
  let formatted = escapedLog.replace(
    /(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2},\d{3})/g,
    '<span class="log-timestamp">$1</span>'
  )

  // 高亮日志级别
  formatted = formatted.replace(
    /\s-\s(DEBUG|INFO|WARNING|ERROR|CRITICAL)\s-\s/g,
    (match, level) => {
      const levelClass = `log-level-${level.toLowerCase()}`
      return ` - <span class="${levelClass}">${level}</span> - `
    }
  )

  // 高亮日志来源 (logger name)
  formatted = formatted.replace(
    /\s-\s([a-zA-Z0-9_.]+)\s-\s/,
    ' - <span class="log-source">$1</span> - '
  )

  // 高亮特殊符号
  formatted = formatted.replace(/✅/g, '<span class="emoji-success">✅</span>')
  formatted = formatted.replace(/⚠️/g, '<span class="emoji-warning">⚠️</span>')
  formatted = formatted.replace(/❌/g, '<span class="emoji-error">❌</span>')

  return formatted
}

onMounted(async () => {
  await loadLogs(true) // 初次加载时滚动到底部
  await loadLogInfo()
})

onUnmounted(() => {
  // 清理定时器
  if (refreshTimer.value) {
    clearInterval(refreshTimer.value)
  }
})
</script>

<style scoped>
.logs-container {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 28px 32px 40px;
  background: var(--cf-bg);
  min-height: calc(100vh - 64px);
  box-sizing: border-box;
}

/* 页面标题 */
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 16px;
  margin-bottom: 4px;
  /* 固定顶部 */
  position: sticky;
  top: 0;
  z-index: 100;
  background: var(--cf-bg);
  margin: -28px -32px 4px -32px;
  padding: 28px 32px;
}

.title-block h2 {
  font-size: 26px;
  font-weight: 700;
  color: var(--cf-fg);
  margin: 0 0 6px 0;
  letter-spacing: -0.5px;
}

.title-block p {
  font-size: 14px;
  color: var(--cf-fg-2);
  margin: 0;
  line-height: 1.6;
}

/* 工具栏卡片 */
.toolbar-card {
  border-radius: 24px;
  background: var(--cf-s1);
  border: 1px solid rgba(107, 115, 255, 0.1);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.toolbar-card:hover {
  box-shadow: 0 20px 40px rgba(65, 80, 180, 0.16);
  border-color: rgba(107, 115, 255, 0.25);
  transform: translateY(-2px);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

/* 输入框和选择器样式 */
.toolbar-left :deep(.el-input__wrapper) {
  border-radius: 12px;
  box-shadow: 0 2px 8px rgba(91, 109, 255, 0.06);
  transition: all 0.3s ease;
}

.toolbar-left :deep(.el-input__wrapper:hover) {
  box-shadow: 0 4px 12px rgba(91, 109, 255, 0.12);
}

.toolbar-left :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 4px 16px rgba(91, 109, 255, 0.18);
  border-color: var(--cf-primary);
}

.toolbar-left :deep(.el-select .el-input__wrapper) {
  border-radius: 12px;
}

/* 自定义按钮样式 */
.toolbar-right :deep(.el-button) {
  border-radius: 14px;
  font-weight: 500;
  padding: 0 18px;
  height: 38px;
  font-size: 14px;
  transition: all 0.3s ease;
}

.toolbar-right :deep(.el-button--primary) {
  border: none;
  box-shadow: 0 8px 20px rgba(91, 109, 255, 0.25);
}

.toolbar-right :deep(.el-button--primary:hover) {
  transform: translateY(-2px);
  box-shadow: 0 12px 28px rgba(91, 109, 255, 0.35);
}

.toolbar-right :deep(.el-button--danger) {
  border: none;
  box-shadow: 0 8px 20px rgba(255, 87, 87, 0.25);
}

.toolbar-right :deep(.el-button--danger:hover) {
  transform: translateY(-2px);
  box-shadow: 0 12px 28px rgba(255, 87, 87, 0.35);
}

.toolbar-right :deep(.el-button:not(.el-button--primary):not(.el-button--danger)) {
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.2);
  color: var(--cf-primary);
}

.toolbar-right :deep(.el-button:not(.el-button--primary):not(.el-button--danger):hover) {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.3);
  transform: translateY(-1px);
}

/* 信息标签 - Meta Pill 样式 */
.log-info {
  margin-top: 20px;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.log-info :deep(.el-tag) {
  border-radius: 16px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  transition: all 0.2s ease;
}

.log-info :deep(.el-tag--info) {
  background: rgba(139, 143, 255, 0.12);
  color: var(--cf-primary);
}

.log-info :deep(.el-tag--success) {
  background: rgba(103, 194, 58, 0.12);
  color: var(--cf-success);
}

.log-info :deep(.el-tag) {
  background: rgba(107, 115, 255, 0.08);
  color: var(--cf-primary);
}

/* 日志内容卡片 */
.log-card {
  flex: 1;
  min-height: 0;
  border-radius: 24px;
  display: flex;
  flex-direction: column;
  background: var(--cf-s1);
  border: 1px solid rgba(107, 115, 255, 0.1);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.log-card:hover {
  box-shadow: 0 20px 40px rgba(65, 80, 180, 0.16);
  border-color: rgba(107, 115, 255, 0.25);
}

.log-card :deep(.el-card__body) {
  flex: 1;
  min-height: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}

.log-content {
  flex: 1;
  overflow-y: auto;
  background: #1e1e1e;
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', 'source-code-pro', monospace;
  font-size: 13px;
  line-height: 1.6;
  color: #d4d4d4;
  padding: 20px;
  border-radius: 16px;
  margin: 4px;
}

.empty-logs {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 400px;
}

.log-lines {
  display: flex;
  flex-direction: column;
}

.log-line {
  display: flex;
  padding: 2px 0;
  transition: background-color 0.2s;
}

.log-line:hover {
  background-color: rgba(255, 255, 255, 0.05);
}

.line-number {
  flex-shrink: 0;
  width: 60px;
  /* 控制台背景恒为深色，行号与时间戳不跟随主题，否则浅色下会看不见 */
  color: #8a8f98;
  text-align: right;
  margin-right: 16px;
  user-select: none;
}

.line-content {
  flex: 1;
  word-break: break-all;
  white-space: pre-wrap;
}

/* 日志时间戳样式 */
:deep(.log-timestamp) {
  /* 同上：控制台内的配色独立于应用主题 */
  color: #8a8f98;
  font-weight: 500;
}

/* 日志来源样式 */
:deep(.log-source) {
  color: #9cdcfe;
  font-style: italic;
}

/* 日志级别样式 */
:deep(.log-level-critical) {
  color: var(--cf-danger);
  font-weight: bold;
  background: rgba(255, 68, 68, 0.1);
  padding: 2px 6px;
  border-radius: 3px;
}

:deep(.log-level-error) {
  color: #f48771;
  font-weight: bold;
  background: rgba(244, 135, 113, 0.1);
  padding: 2px 6px;
  border-radius: 3px;
}

:deep(.log-level-warning) {
  color: #dcdcaa;
  font-weight: 600;
  background: rgba(220, 220, 170, 0.1);
  padding: 2px 6px;
  border-radius: 3px;
}

:deep(.log-level-info) {
  color: #4ec9b0;
  font-weight: 500;
  background: rgba(78, 201, 176, 0.1);
  padding: 2px 6px;
  border-radius: 3px;
}

:deep(.log-level-debug) {
  color: var(--cf-fg-3);
  font-weight: 400;
}

/* Emoji 样式 */
:deep(.emoji-success) {
  font-size: 14px;
}

:deep(.emoji-warning) {
  font-size: 14px;
}

:deep(.emoji-error) {
  font-size: 14px;
}

/* 滚动条样式 */
.log-content::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

.log-content::-webkit-scrollbar-track {
  background: #2d2d2d;
  border-radius: 4px;
}

.log-content::-webkit-scrollbar-thumb {
  background: var(--cf-fg-3);
  border-radius: 4px;
}

.log-content::-webkit-scrollbar-thumb:hover {
  background: var(--cf-fg-3);
}

/* 响应式布局 */
@media (max-width: 768px) {
  .logs-container {
    padding: 20px 16px 32px;
  }

  .page-header {
    margin: -20px -16px 4px -16px;
    padding: 20px 16px;
  }

  .title-block h2 {
    font-size: 22px;
  }

  .title-block p {
    font-size: 13px;
  }

  .toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-left,
  .toolbar-right {
    width: 100%;
    justify-content: flex-start;
  }

  .toolbar-left :deep(.el-input),
  .toolbar-left :deep(.el-select) {
    flex: 1;
    min-width: 0;
  }

  .toolbar-right :deep(.el-button) {
    flex: 1;
  }

  .log-info {
    gap: 8px;
  }

  .log-info :deep(.el-tag) {
    font-size: 12px;
    padding: 5px 12px;
  }

  .line-number {
    width: 40px;
    margin-right: 12px;
  }

  .log-content {
    font-size: 12px;
    padding: 16px;
  }

  .toolbar-card,
  .log-card {
    border-radius: 20px;
  }
}

/* 超小屏幕适配 */
@media (max-width: 480px) {
  .logs-container {
    padding: 16px;
  }

  .title-block h2 {
    font-size: 20px;
  }

  .title-block p {
    font-size: 13px;
  }

  .toolbar-card,
  .log-card {
    border-radius: 18px;
    padding: 16px;
  }

  .log-content {
    font-size: 11px;
    padding: 14px;
  }
}
</style>
