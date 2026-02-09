<template>
  <div class="subscriptions-page">
    <div class="page-header">
      <div class="title-block">
        <h2>订阅管理</h2>
        <p>管理您的订阅源和配置</p>
      </div>
      <div class="header-actions">
        <el-button
          class="action-btn action-secondary"
          @click="handleFetchAll"
                    :loading="isRefreshing"
        >
          <el-icon><RefreshRight /></el-icon>
          批量更新        </el-button>
        <el-button
          type="primary"
          class="action-btn action-primary"
          @click="showAddDialog"
        >
          <el-icon><Plus /></el-icon>
          添加订阅
        </el-button>
      </div>
    </div>

    <div class="subscriptions-grid" ref="subscriptionsContainer">
      <div
        v-for="sub in subscriptions"
        :key="sub.id"
        class="subscription-card"
        :class="{ disabled: !sub.enabled }"
        :data-id="sub.id"
      >
        <div class="card-header">
          <div class="card-title-group">
            <button class="card-drag-handle" type="button" aria-label="拖动排序">
              <el-icon><DCaret /></el-icon>
            </button>
            <div class="card-title">{{ sub.name }}</div>
          </div>
          <button class="status-toggle" :class="{ active: sub.enabled }" @click="handleToggle(sub)">
            <el-icon v-if="sub.enabled"><View /></el-icon>
            <el-icon v-else><Hide /></el-icon>
          </button>
        </div>
        <div class="card-meta">
          <span class="meta-pill type-pill" :class="getTypeClass(sub.type)">
            {{ getTypeLabel(sub.type) }}
          </span>
          <span
            class="meta-pill nodes-pill"
            :class="getNodeStatusClass(subscriptionStatus[sub.id])"
          >
            <template v-if="subscriptionStatus[sub.id]?.status === 'loading'">
              <el-icon class="spin"><Loading /></el-icon>
              获取中...
            </template>
            <template v-else-if="subscriptionStatus[sub.id]?.status === 'success'">
              <el-icon><CircleCheck /></el-icon>
              {{ subscriptionStatus[sub.id]?.count || 0 }} 个节点
            </template>
            <template v-else-if="subscriptionStatus[sub.id]?.status === 'error'">
              <el-icon><CircleClose /></el-icon>
              获取失败
            </template>
            <template v-else>
              <el-icon><Minus /></el-icon>
              未获取
            </template>
          </span>
        </div>
        <div class="card-section">
          <div class="section-label-row">
            <div class="section-label">
              <el-icon><Link /></el-icon>
              订阅URL
            </div>
            <el-link type="primary" class="url-toggle" @click="toggleUrlReveal(sub.id)">
              {{ revealedUrls[sub.id] ? '隐藏原文' : '显示原文' }}
            </el-link>
          </div>
          <div class="url-box">{{ getDisplayUrl(sub) }}</div>
        </div>
        <div class="card-section inline">
          <div class="section-label">
            <el-icon><Calendar /></el-icon>
            更新周期
          </div>
          <div class="section-value">{{ formatInterval(sub.interval) }}</div>
        </div>
        <div class="card-actions">
          <el-button
            class="card-btn ghost"
            size="small"
                        @click="handleFetchSubscription(sub)"
          >
            <el-icon><Connection /></el-icon>
            获取节点
          </el-button>
          <el-button
            class="card-btn primary"
            size="small"
            @click="editSubscription(sub)"
          >
            <el-icon><EditPen /></el-icon>
            编辑
          </el-button>
          <el-button
            class="card-btn danger"
            size="small"
            @click="deleteSubscription(sub)"
          >
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </div>
      </div>
    </div>

    <!-- 添加/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      width="640px"
      class="subscription-dialog"
      :close-on-click-modal="false"
      :destroy-on-close="true"
    >
      <template #header="{ close }">
        <div class="dialog-header">
          <div class="dialog-title-group">
            <h3>{{ isEdit ? '编辑订阅' : '添加订阅' }}</h3>
            <p>配置订阅名称、链接与同步策略以保持节点数据最新</p>
          </div>
          <button class="dialog-close-btn" type="button" @click="close" aria-label="关闭">
            <el-icon><Close /></el-icon>
          </button>
        </div>
      </template>
      <div class="dialog-card">
        <el-form :model="form" label-position="top" class="subscription-form">
          <div class="form-grid">
            <el-form-item label="订阅名称" class="form-item">
              <el-input v-model="form.name" placeholder="请输入订阅名称" />
            </el-form-item>
            <el-form-item label="订阅类型" class="form-item">
              <el-select v-model="form.type" placeholder="请选择订阅类型" class="type-select">
                <el-option label="通用" value="universal" />
                <el-option label="Mihomo" value="mihomo" />
                <el-option label="Surge" value="surge" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="订阅链接" class="form-item">
            <el-input
              v-model="form.url"
              type="textarea"
              :rows="3"
              placeholder="请输入订阅URL"
            />
          </el-form-item>
          <div class="form-grid interval-row">
            <el-form-item label="更新间隔" class="form-item">
              <div class="interval-box">
                <el-input-number
                  v-model="form.interval"
                  :min="60"
                  :max="604800"
                  :step="3600"
                  class="interval-input"
                />
                <span class="interval-hint">秒（建议 86400 = 1 天）</span>
              </div>
            </el-form-item>
            <el-form-item label="启用状态" class="form-item status-item">
              <div class="status-toggle-row">
                <el-switch v-model="form.enabled" />
                <span>{{ form.enabled ? '订阅启用中' : '订阅已停用' }}</span>
              </div>
            </el-form-item>
          </div>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="dialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveSubscription">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 节点预览对话框 -->
    <el-dialog
      v-model="nodesPreviewVisible"
      title="节点预览"
      width="800px"
      class="nodes-preview-dialog"
    >
      <div class="preview-header">
        <el-text class="preview-count">共 {{ previewNodes.length }} 个节点</el-text>
      </div>
      <div class="nodes-list">
        <el-scrollbar max-height="500px">
          <div
            v-for="(node, index) in previewNodes"
            :key="node.id"
            class="node-item"
            @click="togglePreviewExpand(index)"
          >
            <div class="node-info">
              <div class="node-name">
                <el-icon><Connection /></el-icon>
                <span>{{ node.name }}</span>
                <el-icon class="expand-arrow" :class="{ expanded: expandedPreviewNodes.has(index) }"><ArrowDown /></el-icon>
              </div>
              <div class="node-details">
                <el-tag size="small" type="primary">{{ node.type?.toUpperCase() || 'UNKNOWN' }}</el-tag>
                <span class="node-server">{{ node.server }}:{{ node.port }}</span>
              </div>
            </div>
            <pre v-show="expandedPreviewNodes.has(index)" class="code-box" @click.stop>{{ formatNodeToYaml(node) }}</pre>
          </div>
          <el-empty v-if="previewNodes.length === 0" description="暂无节点" />
        </el-scrollbar>
      </div>
      <template #footer>
        <el-button @click="nodesPreviewVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElLoading, ElMessageBox } from 'element-plus'
import { Plus, DCaret, Connection, Loading, CircleCheck, CircleClose, Minus, RefreshRight, View, Hide, Link, Calendar, EditPen, Delete, Close, ArrowDown } from '@element-plus/icons-vue'
import { subscriptionApi, subStoreUrlApi } from '@/api'
import type { Subscription } from '@/types'
import Sortable from 'sortablejs'
import api from '@/api'
import yaml from 'js-yaml'

const subscriptions = ref<Subscription[]>([])
const subscriptionsContainer = ref<HTMLElement | null>(null)
const dialogVisible = ref(false)
const isEdit = ref(false)
const isRefreshing = ref(false)
// 使用构建时常量控制专业功能
// 处理按钮点击
const handleFetchAll = () => {
  fetchAllSubscriptionsInBackground()
}

const handleFetchSubscription = (sub: Subscription) => {
  fetchSubscription(sub)
}

const form = ref<Partial<Subscription>>({
  name: '',
  url: '',
  type: 'universal',
  enabled: true,
  interval: 86400
})

// 节点预览相关
const nodesPreviewVisible = ref(false)
const previewNodes = ref<any[]>([])
const expandedPreviewNodes = ref<Set<number>>(new Set())
const revealedUrls = ref<Record<string, boolean>>({})

// 订阅状态管理
interface SubscriptionStatusItem {
  status: 'idle' | 'loading' | 'success' | 'error'
  count?: number
  error?: string
  updatedAt?: string | null
}
const subscriptionStatus = ref<Record<string, SubscriptionStatusItem>>({})

// 获取类型显示标签
const getTypeLabel = (type: string) => {
  const labels: Record<string, string> = {
    'mihomo': 'Mihomo',
    'surge': 'Surge',
    'universal': '通用'
  }
  return labels[type] || type
}

// 卡片类型样式
const typeClassMap: Record<string, string> = {
  'mihomo': 'type-mihomo',
  'surge': 'type-surge',
  'universal': 'type-universal'
}
const getTypeClass = (type: string) => typeClassMap[type] || 'type-default'

// 节点状态 pill 样式
const getNodeStatusClass = (status?: SubscriptionStatusItem) => {
  if (!status || status.status === 'idle') return 'status-idle'
  if (status.status === 'loading') return 'status-loading'
  if (status.status === 'success') return 'status-success'
  return 'status-error'
}

// 格式化更新间隔
const formatInterval = (seconds: number) => {
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)} 分钟`
  } else if (seconds < 86400) {
    return `${Math.floor(seconds / 3600)} 小时`
  } else {
    return `${Math.floor(seconds / 86400)} 天`
  }
}

// 切换预览节点展开/收起
const togglePreviewExpand = (index: number) => {
  if (expandedPreviewNodes.value.has(index)) {
    expandedPreviewNodes.value.delete(index)
  } else {
    expandedPreviewNodes.value.add(index)
  }
  expandedPreviewNodes.value = new Set(expandedPreviewNodes.value)
}

// 将节点数据格式化为 YAML
const formatNodeToYaml = (node: any) => {
  const { id, ...rest } = node
  const proxy: Record<string, any> = {
    name: rest.name,
    type: rest.type,
    server: rest.server,
    port: rest.port,
    ...rest.params
  }
  return yaml.dump(proxy, { indent: 2, lineWidth: -1 }).trim()
}

const loadSubscriptions = async () => {
  try {
    const { data } = await subscriptionApi.getAll()
    subscriptions.value = data
    // 初始化订阅状态
    subscriptions.value.forEach(sub => {
      if (revealedUrls.value[sub.id] === undefined) {
        revealedUrls.value[sub.id] = false
      }
      const cachedCount = typeof sub.cached_node_count === 'number' ? sub.cached_node_count : null
      const updatedAt = sub.cached_updated_at ?? null
      if (cachedCount !== null) {
        subscriptionStatus.value[sub.id] = {
          status: 'success',
          count: cachedCount,
          updatedAt
        }
      } else {
        subscriptionStatus.value[sub.id] = { status: 'idle' }
      }
    })
  } catch (error) {
    ElMessage.error('加载订阅列表失败')
  }
}

// 后台获取所有订阅的节点数量
const fetchAllSubscriptionsInBackground = async () => {
  if (isRefreshing.value) return

  isRefreshing.value = true
  try {
    // 并发获取所有订阅的节点预览
    const promises = subscriptions.value.map(async (sub) => {
      // 跳过已禁用的订阅
      if (!sub.enabled) {
        return
      }

      try {
        const previous = subscriptionStatus.value[sub.id]
        subscriptionStatus.value[sub.id] = {
          status: 'loading',
          count: previous?.count,
          updatedAt: previous?.updatedAt
        }
        const { data } = await subscriptionApi.fetch(sub.id, true)

        if (data.success) {
          const cachedCount = data.cached_count ?? (data.nodes?.length || 0)
          const updatedAt = data.cached_updated_at ?? null
          sub.cached_node_count = cachedCount
          sub.cached_updated_at = updatedAt
          subscriptionStatus.value[sub.id] = {
            status: 'success',
            count: cachedCount,
            updatedAt
          }
        } else {
          subscriptionStatus.value[sub.id] = {
            status: 'error',
            error: data.message
          }
        }
      } catch (error: any) {
        subscriptionStatus.value[sub.id] = {
          status: 'error',
          error: error.response?.data?.message || '获取失败'
        }
      }
    })

    await Promise.allSettled(promises)
  } finally {
    isRefreshing.value = false
  }
}

const checkSubStoreUrl = async (): Promise<boolean> => {
  try {
    const response = await subStoreUrlApi.get()
    const url = response.data?.sub_store_url || ''
    if (!url) {
      await ElMessageBox.confirm(
        '尚未配置 Sub-Store URL，订阅解析和节点格式转换功能将不可用。请前往「生成配置」页面配置 Sub-Store 地址。',
        '提示',
        {
          confirmButtonText: '继续添加',
          cancelButtonText: '取消',
          type: 'warning'
        }
      )
    }
    return true
  } catch (error: any) {
    if (error === 'cancel' || error?.toString?.().includes('cancel')) {
      return false
    }
    return true
  }
}

const showAddDialog = async () => {
  if (!await checkSubStoreUrl()) return
  isEdit.value = false
  form.value = {
    id: `sub_${Date.now()}`,
    name: '',
    url: '',
    type: 'universal',
    enabled: true,
    interval: 86400
  }
  dialogVisible.value = true
}

const editSubscription = (row: Subscription) => {
  isEdit.value = true
  form.value = { ...row }
  dialogVisible.value = true
}

const saveSubscription = async () => {
  try {
    if (isEdit.value) {
      await subscriptionApi.update(form.value.id!, form.value)
      ElMessage.success('更新成功')
    } else {
      await subscriptionApi.create(form.value)
      ElMessage.success('添加成功')
    }
    dialogVisible.value = false
    loadSubscriptions()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const deleteSubscription = async (row: Subscription) => {
  try {
    // 确认删除
    await ElMessageBox.confirm(
      '确定要删除该订阅吗？删除后将同步清理策略组中对该订阅的引用。',
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    // 先删除订阅
    await subscriptionApi.delete(row.id)

    // 获取所有策略组，清理引用
    const { data: proxyGroups } = await api.get('/proxy-groups')
    let updatedCount = 0

    // 查找引用了该订阅的策略组
    for (const group of proxyGroups) {
      if (group.subscriptions && group.subscriptions.includes(row.id)) {
        // 从订阅列表中移除该订阅ID
        group.subscriptions = group.subscriptions.filter((id: string) => id !== row.id)

        try {
          await api.put(`/proxy-groups/${group.id}`, group)
          updatedCount++
        } catch (error) {
          console.error(`更新策略组 ${group.name} 失败:`, error)
        }
      }
    }

    if (updatedCount > 0) {
      ElMessage.success(`删除成功，已同步清理 ${updatedCount} 个策略组中的引用`)
    } else {
      ElMessage.success('删除成功')
    }
    loadSubscriptions()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除失败')
      console.error('删除订阅失败:', error)
    }
  }
}

const fetchSubscription = async (row: Subscription) => {
  const loading = ElLoading.service({
    lock: true,
    text: '正在解析节点...',
    background: 'rgba(0, 0, 0, 0.7)'
  })
  try {
    // 使用预览模式
    const { data } = await subscriptionApi.fetch(row.id, true)

    if (data.success) {
      // 显示节点预览
      previewNodes.value = data.nodes || []
      expandedPreviewNodes.value = new Set()
      nodesPreviewVisible.value = true
      const cachedCount = data.cached_count ?? previewNodes.value.length
      const updatedAt = data.cached_updated_at ?? null
      row.cached_node_count = cachedCount
      row.cached_updated_at = updatedAt
      subscriptionStatus.value[row.id] = {
        status: 'success',
        count: cachedCount,
        updatedAt
      }

      if (previewNodes.value.length === 0) {
        ElMessage.warning('未解析到任何节点')
      }
    } else {
      ElMessage.error(data.message || '解析节点失败')
    }
  } catch (error: any) {
    console.error('获取节点失败:', error)
    const message = error.response?.data?.message || '解析节点失败'
    subscriptionStatus.value[row.id] = {
      status: 'error',
      error: message,
      count: subscriptionStatus.value[row.id]?.count,
      updatedAt: subscriptionStatus.value[row.id]?.updatedAt
    }
    ElMessage.error(message)
  } finally {
    loading.close()
  }
}

const handleToggle = async (sub: Subscription) => {
  sub.enabled = !sub.enabled
  await toggleSubscriptionEnabled(sub)
}

const toggleUrlReveal = (id: string) => {
  revealedUrls.value[id] = !revealedUrls.value[id]
}

const getDisplayUrl = (sub: Subscription) => {
  if (revealedUrls.value[sub.id]) {
    return sub.url
  }

  const url = sub.url || ''
  if (url.length <= 12) return url

  try {
    const parsed = new URL(url)
    const maskedSearchParams = new URLSearchParams(parsed.search)

    maskedSearchParams.forEach((value, key) => {
      if (value.length > 8) {
        maskedSearchParams.set(key, `${value.slice(0, 3)}****${value.slice(-3)}`)
      }
    })

    parsed.search = maskedSearchParams.toString() ? `?${maskedSearchParams.toString()}` : ''
    return parsed.toString()
  } catch (error) {
    // 对于非标准 URL，使用通用掩码
    return `${url.slice(0, 12)}****${url.slice(-6)}`
  }
}

const toggleSubscriptionEnabled = async (sub: Subscription) => {
  try {
    await subscriptionApi.update(sub.id, sub)
    ElMessage.success(sub.enabled ? '已启用' : '已禁用')
  } catch (error) {
    ElMessage.error('更新状态失败')
    // 回滚状态
    sub.enabled = !sub.enabled
    loadSubscriptions()
  }
}

const initSortable = () => {
  nextTick(() => {
    if (subscriptionsContainer.value) {
      Sortable.create(subscriptionsContainer.value, {
        animation: 150,
        handle: '.card-drag-handle',
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        dragClass: 'sortable-drag',
        onEnd: async (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex) return

          // 更新本地数据顺序
          const movedItem = subscriptions.value.splice(oldIndex, 1)[0]
          subscriptions.value.splice(newIndex, 0, movedItem)

          // 保存新顺序到后端
          try {
            await saveSubscriptionsOrder()
            ElMessage.success('排序已更新')
          } catch (error) {
            ElMessage.error('保存排序失败')
            loadSubscriptions()
          }
        }
      })
    }
  })
}

const saveSubscriptionsOrder = async () => {
  // 批量更新订阅顺序
  await api.post('/subscriptions/reorder', {
    subscriptions: subscriptions.value
  })
}

onMounted(async () => {
  await loadSubscriptions()
  initSortable()
})

onUnmounted(() => {
})
</script>

<style scoped>
.subscriptions-page {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --sub-radius-xl: 40px;
  --sub-radius-lg: 24px;
  --sub-radius-md: 16px;
  --sub-radius-sm: 12px;
  --sub-radius-pill: 999px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
  /* 固定顶部 */
  position: sticky;
  top: 0;
  z-index: 100;
  background: #f5f7ff;
  margin: -28px -32px 28px -32px;
  padding: 28px 32px;
}

.title-block h2 {
  margin: 0;
  font-size: 26px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  color: transparent;
}

.title-block p {
  margin: 6px 0 0;
  font-size: 14px;
  color: #7f87af;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: flex-end;
}

.action-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0 20px;
  height: 40px;
  border-radius: var(--sub-radius-md, 16px);
  font-weight: 600;
  font-size: 14px;
  border: none;
  background: rgba(107, 115, 255, 0.15);
  color: #4a5bff;
  transition: all 0.2s ease;
}

.action-btn.action-secondary {
  border: 1px solid rgba(107, 115, 255, 0.35);
}

.action-btn.action-primary {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  color: #fff;
  box-shadow: 0 12px 30px rgba(87, 104, 255, 0.25);
}

.action-btn:not([disabled]):hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 24px rgba(87, 104, 255, 0.25);
}

.action-btn:focus {
  outline: none;
}

:deep(.action-btn .el-icon) {
  font-size: 16px;
}

.subscriptions-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  margin-top: 28px;
}

:deep(.subscription-dialog),
:deep(.el-overlay-dialog .subscription-dialog) {
  border-radius: var(--sub-radius-xl, 40px) !important;
  overflow: hidden;
  background: rgba(252, 253, 255, 0.97);
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  backdrop-filter: blur(20px);
  --el-dialog-border-radius: var(--sub-radius-xl, 40px);
}

:deep(.subscription-dialog .el-dialog__header) {
  padding: 20px 32px 0;
  margin: 0;
  border-bottom: none;
}

:deep(.subscription-dialog .el-dialog__body) {
  padding: 0 32px 28px;
  background: #f7f8ff;
}

:deep(.subscription-dialog .el-dialog__footer) {
  padding: 0 32px 28px;
  border-top: none;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  padding: 8px 0 18px;
  color: #30354d;
}

.dialog-title-group h3 {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.dialog-title-group p {
  margin: 8px 0 0;
  font-size: 13px;
  color: #7c86ae;
}

.dialog-close-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 1px solid rgba(124, 134, 174, 0.35);
  background: transparent;
  color: #7c86ae;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.dialog-close-btn:hover {
  background: rgba(107, 115, 255, 0.12);
  border-color: rgba(107, 115, 255, 0.35);
  color: #4e5eff;
}

.dialog-card {
  background: #fff;
  border-radius: var(--sub-radius-lg, 24px);
  padding: 30px 28px 26px;
  box-shadow: 0 18px 30px rgba(91, 112, 255, 0.12);
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.subscription-form {
  display: flex;
  flex-direction: column;
  gap: 22px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 18px;
}

.interval-row {
  align-items: start;
}

.form-item {
  margin: 0;
}

.subscription-form :deep(.el-form-item__label) {
  font-weight: 600;
  font-size: 13px;
  color: #6c74a0;
  margin-bottom: 8px;
}

.subscription-form :deep(.el-input__wrapper),
.subscription-form :deep(.el-select .el-input__wrapper),
.subscription-form :deep(.el-textarea__inner),
.subscription-form :deep(.el-input-number .el-input__wrapper) {
  border-radius: var(--sub-radius-md, 16px);
  border: none;
  box-shadow: 0 0 0 1px rgba(107, 115, 255, 0.14);
  transition: box-shadow 0.2s ease, transform 0.2s ease;
  background-color: #f9faff;
}

.subscription-form :deep(.el-textarea__inner) {
  padding: 14px 16px;
}

.subscription-form :deep(.el-input__wrapper.is-focus),
.subscription-form :deep(.el-select .el-input__wrapper.is-focus),
.subscription-form :deep(.el-input-number.is-active .el-input__wrapper),
.subscription-form :deep(.el-input-number:hover .el-input__wrapper),
.subscription-form :deep(.el-textarea__inner:focus) {
  box-shadow: 0 0 0 2px rgba(107, 115, 255, 0.32);
  transform: translateY(-1px);
  background-color: #fff;
}

.dialog-card :deep(.el-input-number .el-input__wrapper) {
  height: 46px;
}

.interval-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.interval-input {
  width: 100%;
}

.interval-hint {
  font-size: 12px;
  color: #8a93c1;
}

.status-toggle-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-radius: 999px;
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  font-weight: 600;
}

.status-toggle-row span {
  font-size: 13px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.footer-btn {
  min-width: 118px;
  height: 42px;
  border-radius: var(--sub-radius-md, 16px);
  font-weight: 600;
  font-size: 14px;
}

.footer-btn.ghost {
  background: transparent;
  color: #5460d7;
  border: 1px solid rgba(107, 115, 255, 0.3);
}

.footer-btn.ghost:hover {
  background: rgba(107, 115, 255, 0.08);
}

.footer-btn.primary {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  border: none;
  color: #fff;
  box-shadow: 0 12px 24px rgba(87, 104, 255, 0.28);
}

.footer-btn.primary:hover {
  transform: translateY(-1px);
}

.subscription-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 28px 24px 24px;
  border-radius: var(--sub-radius-lg, 24px);
  background: #fff;
  border: 1px solid rgba(107, 115, 255, 0.08);
  box-shadow: 0 18px 40px rgba(91, 112, 255, 0.16);
  transition: transform 0.25s ease, box-shadow 0.25s ease;
}

.subscription-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 22px 48px rgba(91, 112, 255, 0.2);
}

.subscription-card.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.card-title-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-drag-handle {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: rgba(107, 115, 255, 0.12);
  color: #5a66ff;
  cursor: grab;
  transition: background 0.2s ease, color 0.2s ease, transform 0.2s ease;
  box-shadow: 0 8px 20px rgba(91, 112, 255, 0.18);
}

.card-drag-handle:hover {
  background: rgba(107, 115, 255, 0.22);
  color: #3f4ffa;
  transform: translateY(-1px);
}

.card-drag-handle:focus {
  outline: none;
}

.card-title {
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.status-toggle {
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 50%;
  background: rgba(107, 115, 255, 0.16);
  color: #5a66ff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.status-toggle.active {
  background: linear-gradient(135deg, #8b8fff 0%, #6b7dff 100%);
  color: #fff;
  box-shadow: 0 12px 28px rgba(87, 104, 255, 0.3);
}

.status-toggle:hover {
  transform: scale(1.05);
}

.status-toggle:focus {
  outline: none;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
}

.type-mihomo {
  background: rgba(107, 115, 255, 0.16);
  color: #4e5eff;
}

.type-surge {
  background: rgba(139, 143, 255, 0.16);
  color: #8b8fff;
}

.type-universal {
  background: rgba(129, 139, 173, 0.16);
  color: #5e6887;
}

.type-default {
  background: rgba(140, 149, 186, 0.18);
  color: #626c90;
}

.nodes-pill {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
}

.nodes-pill.status-success {
  background: rgba(139, 143, 255, 0.16);
  color: #8b8fff;
}

.nodes-pill.status-error {
  background: rgba(255, 131, 131, 0.2);
  color: #eb4d4d;
}

.nodes-pill.status-loading {
  background: rgba(107, 115, 255, 0.18);
  color: #4e5eff;
}

.nodes-pill.status-idle {
  background: rgba(136, 148, 185, 0.16);
  color: #6c7496;
}

.nodes-pill .spin {
  animation: rotate 1s linear infinite;
}

@keyframes rotate {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

.card-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-section.inline {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
}

.section-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.section-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #7d88af;
}

.url-toggle {
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.url-toggle:focus-visible {
  outline: none;
}

.url-toggle:focus-visible span {
  text-decoration: underline;
}

.section-value {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.url-box {
  padding: 12px 14px;
  border-radius: var(--sub-radius-md, 16px);
  background: #f4f6ff;
  color: #1f2d3d;
  font-size: 13px;
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  word-break: break-all;
}

.card-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: auto;
}

:deep(.card-btn.el-button) {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 40px;
  border-radius: var(--sub-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  padding: 0 16px;
  border: none;
}

.card-btn.ghost {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.25);
}

.card-btn.primary {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.32);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  color: #9b8fff;
  border: 1px solid rgba(155, 143, 255, 0.28);
}

.card-btn:hover {
  box-shadow: 0 10px 24px rgba(87, 104, 255, 0.15);
}

.card-btn.danger:hover {
  box-shadow: 0 10px 24px rgba(155, 143, 255, 0.25);
}

.sortable-ghost {
  opacity: 0.6;
  transform: scale(0.98);
}

.sortable-chosen {
  box-shadow: 0 18px 40px rgba(91, 112, 255, 0.2);
}

.sortable-drag {
  cursor: grabbing !important;
}

@media (max-width: 1024px) {
  .subscriptions-page {
    padding: 24px;
    --sub-radius-xl: 32px;
    --sub-radius-lg: 22px;
    --sub-radius-md: 14px;
  }

  .subscriptions-grid {
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 20px;
  }

  :deep(.subscription-dialog .el-dialog__header) {
    padding: 20px 24px 0;
  }

  :deep(.subscription-dialog .el-dialog__body),
  :deep(.subscription-dialog .el-dialog__footer) {
    padding: 0 24px 24px;
  }

  :deep(.subscription-dialog),
  :deep(.el-overlay-dialog .subscription-dialog) {
    border-radius: var(--sub-radius-xl, 32px) !important;
    --el-dialog-border-radius: var(--sub-radius-xl, 32px);
  }

  .dialog-card {
    padding: 28px 24px 24px;
  }
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    margin: -20px -20px 20px -20px;
    padding: 20px;
  }

  .header-actions {
    width: 100%;
    flex-direction: column;
    gap: 10px;
    align-items: stretch;
  }

  :deep(.action-btn.el-button) {
    width: 100%;
    justify-content: center;
    display: flex;
    box-sizing: border-box;
  }

  .action-btn {
    width: 100%;
    display: flex;
    box-sizing: border-box;
  }

  :deep(.header-actions .el-button + .el-button) {
    margin-left: 0;
  }

  .card-actions {
    width: 100%;
    flex-direction: column;
    flex-wrap: nowrap;
    align-items: stretch;
    gap: 10px;
  }

  :deep(.card-actions .el-button + .el-button) {
    margin-left: 0;
  }

  .action-btn.action-secondary {
    border: 1px solid rgba(107, 115, 255, 0.35);
  }

  .action-btn.action-primary {
    border: 1px solid transparent;
  }

  .subscriptions-page {
    padding: 20px;
    --sub-radius-xl: 28px;
    --sub-radius-lg: 20px;
    --sub-radius-md: 14px;
  }

  .subscriptions-grid {
    grid-template-columns: 1fr;
  }

  .subscription-card {
    padding: 20px;
  }

  :deep(.card-btn.el-button) {
    flex: unset;
    width: 100%;
    display: flex;
    box-sizing: border-box;
    justify-content: center;
  }

  .dialog-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .dialog-close-btn {
    align-self: flex-end;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }

  .interval-row {
    grid-template-columns: 1fr;
    gap: 16px;
  }

  :deep(.subscription-dialog),
  :deep(.el-overlay-dialog .subscription-dialog) {
    border-radius: var(--sub-radius-xl, 28px) !important;
    --el-dialog-border-radius: var(--sub-radius-xl, 28px);
  }

}

@media (max-width: 480px) {
  .subscriptions-page {
    padding: 16px;
    --sub-radius-xl: 24px;
    --sub-radius-lg: 18px;
    --sub-radius-md: 12px;
  }

  .card-title {
    font-size: 16px;
  }

  :deep(.action-btn.el-button) {
    width: 100%;
    justify-content: center;
    display: flex;
    box-sizing: border-box;
  }

  .action-btn {
    width: 100%;
    display: flex;
    box-sizing: border-box;
  }

  .action-btn.action-secondary {
    border: 1px solid rgba(107, 115, 255, 0.35);
  }

  .action-btn.action-primary {
    border: 1px solid transparent;
  }

  .card-actions {
    gap: 8px;
  }

  :deep(.card-btn.el-button) {
    width: 100%;
    justify-content: center;
  }

  .dialog-footer {
    flex-direction: column;
    gap: 10px;
  }

  .footer-btn {
    width: 100%;
  }

  :deep(.subscription-dialog),
  :deep(.el-overlay-dialog .subscription-dialog) {
    border-radius: var(--sub-radius-xl, 24px) !important;
    --el-dialog-border-radius: var(--sub-radius-xl, 24px);
  }

  :deep(.subscription-dialog .el-dialog__body),
  :deep(.subscription-dialog .el-dialog__footer) {
    padding: 0 20px 20px;
  }

  .dialog-card {
    padding: 24px 18px 20px;
  }
}

/* 节点预览对话框 */
.nodes-preview-dialog .preview-header {
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid #ebedf5;
}

.nodes-preview-dialog .preview-count {
  font-size: 14px;
  color: #65708f;
  font-weight: 500;
}

.nodes-preview-dialog .nodes-list {
  margin: 0 -20px;
}

.nodes-preview-dialog .node-item {
  padding: 12px 20px;
  border-bottom: 1px solid #eef1f8;
  transition: background 0.2s ease;
  cursor: pointer;
}

.nodes-preview-dialog .node-item:hover {
  background: #f7f8ff;
}

.nodes-preview-dialog .node-item .expand-arrow {
  font-size: 14px;
  color: #7d88af;
  margin-left: auto;
  transition: transform 0.2s ease;
  flex-shrink: 0;
}

.nodes-preview-dialog .node-item .expand-arrow.expanded {
  transform: rotate(180deg);
}

.nodes-preview-dialog .code-box {
  margin-top: 10px;
  padding: 14px 16px;
  border-radius: 12px;
  background: #f4f6ff;
  color: #1f2d3d;
  font-size: 13px;
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 260px;
  overflow: auto;
  border: 1px solid rgba(107, 115, 255, 0.1);
  cursor: text;
}

.nodes-preview-dialog .node-item:last-child {
  border-bottom: none;
}

.nodes-preview-dialog .node-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nodes-preview-dialog .node-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.nodes-preview-dialog .node-name .el-icon {
  color: #4e5eff;
  font-size: 16px;
}

.nodes-preview-dialog .node-details {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: #7d88af;
}

.nodes-preview-dialog .node-server {
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  color: #7d88af;
}

.nodes-preview-dialog :deep(.el-dialog) {
  border-radius: var(--sub-radius-lg, 24px);
  overflow: hidden;
}

@media (max-width: 768px) {
  .nodes-preview-dialog :deep(.el-dialog) {
    width: 95vw !important;
    max-width: 95vw !important;
  }

  .nodes-preview-dialog .nodes-list {
    margin: 0 -16px;
  }
}

@media (max-width: 480px) {
  .nodes-preview-dialog :deep(.el-dialog) {
    width: 100vw !important;
    max-width: 100vw !important;
    margin: 0 !important;
  }
}
</style>
