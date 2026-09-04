<template>
  <div class="subscriptions-page" :class="{ 'cf-reordering': reorder.active.value }">
    <ScopeBanner scope="resource" :profile-name="cfProfileName" description="订阅源按配置空间隔离，切换配置空间会看到各自的列表" />

    <PageHeader title="订阅来源" description="订阅拉取后的节点进入共享节点库">
      <template #actions>
        <el-button :loading="isRefreshing" :disabled="reorder.active.value" @click="handleFetchAll">
          <el-icon><RefreshRight /></el-icon>
          批量更新
        </el-button>
        <el-button type="primary" :disabled="reorder.active.value" @click="showAddDialog">
          <el-icon><Plus /></el-icon>
          添加订阅
        </el-button>
      </template>
    </PageHeader>

    <div class="cf-toolbar">
      <el-input
        v-model="keyword"
        class="cf-toolbar__search"
        placeholder="搜索订阅名称或地址"
        clearable
        :disabled="reorder.active.value"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-button v-if="!reorder.active.value" :disabled="subscriptions.length < 2" @click="reorder.enter">
        <el-icon><Sort /></el-icon>
        调整顺序
      </el-button>
    </div>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <el-empty v-if="visibleSubscriptions.length === 0" :description="emptyText" />

    <div class="subscriptions-grid" ref="subscriptionsContainer">
      <div
        v-for="(sub, index) in visibleSubscriptions"
        :key="sub.id"
        class="subscription-card"
        :class="{ disabled: !sub.enabled }"
        :data-id="sub.id"
        data-reorder-item
      >
        <div class="card-header">
          <span v-if="reorder.active.value" class="card-order cf-num">{{ index + 1 }}</span>
          <span class="card-dot" :class="dotClass(sub)" aria-hidden="true"></span>
          <div class="card-title">{{ sub.name }}</div>

          <DragHandle
            v-if="reorder.active.value"
            :label="sub.name"
            :index="index"
            :total="subscriptions.length"
            :position="reorder.positionLabel(index)"
            :grabbed="reorder.grabbedIndex.value === index"
            @up="reorder.moveUp(index)"
            @down="reorder.moveDown(index)"
            @keydown="reorder.onHandleKeydown($event, index)"
          />
          <button
            v-else
            class="status-toggle cf-reorder-mute"
            :class="{ active: sub.enabled }"
            :aria-label="sub.enabled ? `停用 ${sub.name}` : `启用 ${sub.name}`"
            @click="handleToggle(sub)"
          >
            <el-icon v-if="sub.enabled"><View /></el-icon>
            <el-icon v-else><Hide /></el-icon>
          </button>
        </div>

        <template v-if="!reorder.active.value">
          <div class="card-meta">
            <span class="meta-pill" :class="getTypeClass(sub.type)">{{ getTypeLabel(sub.type) }}</span>
            <span class="meta-pill" :class="getNodeStatusClass(subscriptionStatus[sub.id])">
              <template v-if="subscriptionStatus[sub.id]?.status === 'loading'">
                <el-icon class="spin"><Loading /></el-icon>
                获取中…
              </template>
              <template v-else-if="subscriptionStatus[sub.id]?.status === 'success'">
                {{ subscriptionStatus[sub.id]?.count || 0 }} 个节点
              </template>
              <template v-else-if="subscriptionStatus[sub.id]?.status === 'error'">获取失败</template>
              <template v-else>未获取</template>
            </span>
            <span class="meta-text">更新周期 {{ formatInterval(sub.interval) }}</span>
          </div>

          <div class="card-url">
            <span class="card-url__value cf-mono">{{ getDisplayUrl(sub) }}</span>
            <el-link type="primary" class="card-url__toggle" @click="toggleUrlReveal(sub.id)">
              {{ revealedUrls[sub.id] ? '隐藏' : '显示原文' }}
            </el-link>
          </div>

          <div class="card-actions">
            <el-button size="small" @click="handleFetchSubscription(sub)">
              <el-icon><Connection /></el-icon>
              获取节点
            </el-button>
            <el-button size="small" @click="editSubscription(sub)">
              <el-icon><EditPen /></el-icon>
              编辑
            </el-button>
            <el-button size="small" text class="danger-text" @click="deleteSubscription(sub)">
              <el-icon><Delete /></el-icon>
              删除
            </el-button>
          </div>
        </template>
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
            <el-form-item label="健康检查" class="form-item">
              <el-input
                v-model="form.health_check_url"
                placeholder="留空使用默认（http://www.gstatic.com/generate_204）"
              />
              <div class="form-tip">回家 / 内网订阅从国内出网，境外地址会被误判为失活，建议填 http://www.baidu.com</div>
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
import { useProfileStore } from '@/stores/profile'
import { computed, ref, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElLoading, ElMessageBox } from 'element-plus'
import { Plus, Connection, Loading, RefreshRight, View, Hide, EditPen, Delete, Close, ArrowDown, Search, Sort } from '@element-plus/icons-vue'
import { subscriptionApi, subStoreUrlApi } from '@/api'
import type { Subscription } from '@/types'
import api from '@/api'
import yaml from 'js-yaml'
import PageHeader from '@/components/shell/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
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
  interval: 86400,
  health_check_url: ''
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

/* ---------- 搜索与排序 ---------- */
const reorder = useReorder<Subscription>({
  items: subscriptions,
  container: subscriptionsContainer,
  labelOf: sub => sub.name,
  // 按 id 提交：后端 utils/reorder.py 的首选契约，服务端在存量数据上重排，
  // 避免把列表接口的计算字段（cached_node_count 等）回写进配置
  persist: async items => {
    await api.post('/subscriptions/reorder', {
      ids: items.map(item => item.id),
      position: 'top'
    })
  }
})


const keyword = ref('')

// 排序模式下必须展示完整列表，否则筛选会让保存的顺序丢条目
const visibleSubscriptions = computed(() => {
  if (reorder.active.value) return subscriptions.value
  const q = keyword.value.trim().toLowerCase()
  if (!q) return subscriptions.value
  return subscriptions.value.filter(
    sub =>
      sub.name?.toLowerCase().includes(q) || sub.url?.toLowerCase().includes(q)
  )
})

const emptyText = computed(() =>
  subscriptions.value.length === 0 ? '还没有订阅来源' : '没有匹配的订阅'
)

const dotClass = (sub: Subscription): string => {
  if (!sub.enabled) return 'is-off'
  return subscriptionStatus.value[sub.id]?.status === 'error' ? 'is-warn' : 'is-ok'
}

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    ElMessage.success('顺序已保存，所有配置空间生效')
  } catch (error) {
    ElMessage.error('保存顺序失败，顺序已还原')
  }
}

onMounted(async () => {
  await loadSubscriptions()
})

onUnmounted(() => {
})
</script>

<style scoped>
/* 页面只做布局与局部语义，颜色/圆角/间距全部来自全局 token */
.subscriptions-page {
  display: flex;
  flex-direction: column;
}

.cf-toolbar {
  display: flex;
  gap: var(--cf-sp-2);
  margin-bottom: var(--cf-sp-3);
}

.cf-toolbar__search {
  max-width: 320px;
}

/* ---------- 卡片列表 ---------- */
.subscriptions-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: var(--cf-sp-3);
}

.subscription-card {
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  padding: var(--cf-sp-3) var(--cf-sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--cf-sp-2);
}

.subscription-card.disabled {
  opacity: 0.65;
}

.card-header {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  min-height: var(--cf-ctrl-h);
}

.card-order {
  width: 22px;
  font-size: 12px;
  color: var(--cf-fg-3);
  flex: 0 0 auto;
}

.card-dot {
  width: 8px;
  height: 8px;
  border-radius: var(--cf-r-pill);
  flex: 0 0 auto;
}
.card-dot.is-ok {
  background: var(--cf-success);
  box-shadow: 0 0 0 3px var(--cf-success-soft);
}
.card-dot.is-warn {
  background: var(--cf-warning);
  box-shadow: 0 0 0 3px var(--cf-warning-soft);
}
.card-dot.is-off {
  background: var(--cf-fg-3);
}

.card-title {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--cf-fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-toggle {
  width: var(--cf-ctrl-h);
  height: var(--cf-ctrl-h);
  border-radius: var(--cf-r-md);
  border: 1px solid var(--cf-bd);
  background: var(--cf-s2);
  color: var(--cf-fg-3);
  display: grid;
  place-items: center;
  cursor: pointer;
  flex: 0 0 auto;
}
.status-toggle.active {
  color: var(--cf-success);
  border-color: color-mix(in srgb, var(--cf-success) 35%, transparent);
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--cf-sp-2);
}

.meta-pill {
  font-size: 11px;
  font-weight: 650;
  padding: 3px 8px;
  border-radius: 7px;
  background: var(--cf-s3);
  color: var(--cf-fg-2);
  border: 1px solid var(--cf-bd);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}
.meta-pill.status-success {
  background: var(--cf-success-soft);
  color: var(--cf-success);
  border-color: transparent;
}
.meta-pill.status-error {
  background: var(--cf-danger-soft);
  color: var(--cf-danger);
  border-color: transparent;
}
.meta-pill.status-loading {
  background: var(--cf-primary-soft);
  color: var(--cf-primary);
  border-color: transparent;
}

.meta-text {
  font-size: 12px;
  color: var(--cf-fg-2);
}

.card-url {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  min-width: 0;
}

.card-url__value {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11.5px;
  color: var(--cf-fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-url__toggle {
  flex: 0 0 auto;
  font-size: 12px;
}

.card-actions {
  display: flex;
  gap: var(--cf-sp-2);
  flex-wrap: wrap;
  margin-top: var(--cf-sp-1);
}

.danger-text {
  color: var(--cf-danger);
}

.spin {
  animation: cf-spin 1s linear infinite;
}
@keyframes cf-spin {
  to {
    transform: rotate(360deg);
  }
}

/* ---------- 对话框 ---------- */
.dialog-header {
  display: flex;
  align-items: flex-start;
  gap: var(--cf-sp-3);
}

.dialog-title-group h3 {
  margin: 0;
  font-size: 17px;
  font-weight: 650;
  color: var(--cf-fg);
}

.dialog-title-group p {
  margin: 3px 0 0;
  font-size: 12.5px;
  color: var(--cf-fg-2);
}

.dialog-close-btn {
  margin-left: auto;
  width: var(--cf-ctrl-h);
  height: var(--cf-ctrl-h);
  border-radius: var(--cf-r-md);
  border: 1px solid var(--cf-bd);
  background: var(--cf-s2);
  color: var(--cf-fg-2);
  display: grid;
  place-items: center;
  cursor: pointer;
  flex: 0 0 auto;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 0 var(--cf-sp-4);
}

.interval-box {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  width: 100%;
}

.interval-hint,
.form-tip {
  font-size: 12px;
  color: var(--cf-fg-3);
}

.form-tip {
  margin-top: var(--cf-sp-1);
  line-height: 1.45;
}

.status-toggle-row {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  color: var(--cf-fg-2);
  font-size: 13px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--cf-sp-2);
}

/* ---------- 节点预览 ---------- */
.preview-count {
  font-size: 13px;
  color: var(--cf-fg-2);
}

.node-item {
  padding: var(--cf-sp-3) 0;
  border-bottom: 1px solid var(--cf-bd);
  cursor: pointer;
}
.node-item:last-child {
  border-bottom: none;
}

.node-name {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  font-size: 14px;
  font-weight: 550;
  color: var(--cf-fg);
}

.expand-arrow {
  margin-left: auto;
  color: var(--cf-fg-3);
  transition: transform var(--cf-dur) var(--cf-ease);
}
.expand-arrow.expanded {
  transform: rotate(180deg);
}

.node-details {
  display: flex;
  align-items: center;
  gap: var(--cf-sp-2);
  margin-top: var(--cf-sp-1);
}

.node-server {
  font-family: var(--cf-mono);
  font-size: 12px;
  color: var(--cf-fg-2);
}

.code-box {
  margin: var(--cf-sp-2) 0 0;
  padding: var(--cf-sp-3);
  background: var(--cf-s3);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-md);
  font-family: var(--cf-mono);
  font-size: 12px;
  color: var(--cf-fg-2);
  overflow-x: auto;
  white-space: pre;
}

@media (max-width: 640px) {
  .subscriptions-grid {
    grid-template-columns: 1fr;
  }
  .cf-toolbar__search {
    max-width: none;
  }
}
</style>
