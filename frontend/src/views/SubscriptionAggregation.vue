<template>
  <div class="aggregation-page">
    <div class="page-header">
      <div class="title-block">
        <h2>订阅聚合</h2>
        <p>整合多个订阅和节点，构建统一输出</p>
      </div>
      <div class="header-actions">
        <el-button class="action-btn action-primary" @click="showAddDialog">
          <el-icon><Plus /></el-icon>
          添加聚合
        </el-button>
      </div>
    </div>

    <div v-if="aggregations.length === 0" class="empty-state">
      <el-empty description="暂无聚合，点击右上角添加">
        <el-button class="action-btn action-primary" type="primary" @click="showAddDialog">
          <el-icon><Plus /></el-icon>
          添加聚合
        </el-button>
      </el-empty>
    </div>

    <div v-else class="aggregation-grid" ref="cardContainer">
      <div
        v-for="aggregation in aggregations"
        :key="aggregation.id"
        class="aggregation-card"
        :data-id="aggregation.id"
        :class="{ disabled: !aggregation.enabled }"
      >
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-drag-handle">
              <el-icon><Rank /></el-icon>
            </div>
            <div class="card-title">
              <span class="card-name">{{ aggregation.name }}</span>
            </div>
          </div>
          <div class="card-meta">
            <span class="meta-pill">订阅 {{ aggregation.subscriptions.length }}</span>
            <span class="meta-pill">节点 {{ aggregation.nodes.length }}</span>
            <button
              type="button"
              class="status-toggle-btn compact"
              :class="{ active: aggregation.enabled, loading: aggregation.id ? savingStatus[aggregation.id] : false }"
              @click="handleToggle(aggregation)"
              :disabled="aggregation.id ? savingStatus[aggregation.id] : false"
            >
              <el-icon><View /></el-icon>
            </button>
          </div>
        </div>

        <div v-if="aggregation.description" class="card-description">
          {{ aggregation.description }}
        </div>

        <div class="card-section">
          <div class="section-label">
            <el-icon><Link /></el-icon>
            包含订阅
          </div>
          <div class="tag-list">
            <el-tag
              v-for="subId in aggregation.subscriptions"
              :key="subId"
              size="small"
              class="data-tag"
            >
              {{ getSubscriptionName(subId) }}
            </el-tag>
            <span v-if="aggregation.subscriptions.length === 0" class="empty-text">无</span>
          </div>
        </div>

        <div class="card-section">
          <div class="section-label">
            <el-icon><Connection /></el-icon>
            包含节点
          </div>
          <div class="tag-list">
            <el-tag
              v-for="nodeId in aggregation.nodes"
              :key="nodeId"
              size="small"
              type="success"
              class="data-tag"
            >
              {{ getNodeName(nodeId) }}
            </el-tag>
            <span v-if="aggregation.nodes.length === 0" class="empty-text">无</span>
          </div>
        </div>

        <div v-if="aggregation.regex_filter" class="card-section">
          <div class="section-label">
            <el-icon><Filter /></el-icon>
            正则过滤
          </div>
          <div class="code-box small">{{ aggregation.regex_filter }}</div>
        </div>

        <div class="card-section inline">
          <div class="section-label">节点统计</div>
          <el-tag
            v-if="aggregation.loading_count"
            type="info"
            size="large"
            class="count-tag"
          >
            <el-icon class="is-loading"><Loading /></el-icon>
            <span style="margin-left: 4px">加载中...</span>
          </el-tag>
          <el-tag
            v-else
            type="info"
            size="large"
            class="count-tag"
          >
            {{ aggregation.node_count !== undefined ? aggregation.node_count : '-' }} 个节点
          </el-tag>
        </div>

        <div class="card-footer">
          <div class="card-actions">
            <el-button class="card-btn ghost" size="small" @click="handlePreviewNodes(aggregation)">
              <el-icon><View /></el-icon>
              预览节点
            </el-button>
            <el-button class="card-btn ghost" size="small" @click="editAggregation(aggregation)">
              <el-icon><Edit /></el-icon>
              编辑
            </el-button>
            <el-button class="card-btn danger" size="small" @click="deleteAggregation(aggregation)">
              <el-icon><Delete /></el-icon>
              删除
            </el-button>
          </div>
        </div>
      </div>
    </div>

    <el-dialog
      v-model="dialogVisible"
      class="aggregation-dialog"
      width="760px"
      :close-on-click-modal="false"
      :destroy-on-close="true"
    >
      <template #header="{ close }">
        <div class="dialog-header">
          <div class="dialog-title-group">
            <h3>{{ isEdit ? '编辑聚合' : '添加聚合' }}</h3>
            <p>选择订阅或节点，构建一个新的聚合输出</p>
          </div>
          <button class="dialog-close-btn" type="button" @click="close">
            <el-icon><Close /></el-icon>
          </button>
        </div>
      </template>
      <div class="dialog-card">
        <el-form :model="form" label-position="top" class="aggregation-form">
          <el-form-item label="聚合名称" required>
            <el-input v-model="form.name" placeholder="请输入聚合名称" />
          </el-form-item>

          <el-form-item label="选择订阅">
            <el-select
              v-model="form.subscriptions"
              multiple
              filterable
              placeholder="选择要包含的订阅"
              style="width: 100%"
            >
              <el-option
                v-for="sub in subscriptions"
                :key="sub.id"
                :label="sub.name"
                :value="sub.id"
              />
            </el-select>
            <p class="helper-text">选择的订阅中的所有节点都会被包含在聚合中</p>
          </el-form-item>

          <el-form-item label="选择节点">
            <el-select
              v-model="form.nodes"
              multiple
              filterable
              placeholder="选择要包含的节点"
              style="width: 100%"
            >
              <el-option
                v-for="node in nodes"
                :key="node.id"
                :label="node.name"
                :value="node.id"
              />
            </el-select>
            <p class="helper-text">可额外加入独立节点，与订阅节点一起输出</p>
          </el-form-item>

          <el-form-item label="正则过滤">
            <el-input
              v-model="form.regex_filter"
              placeholder="可选：使用正则表达式过滤节点名称，例如 香港|HK"
            />
          </el-form-item>

          <el-form-item label="描述">
            <el-input
              v-model="form.description"
              type="textarea"
              :rows="3"
              placeholder="可选：说明聚合用途"
            />
          </el-form-item>

          <el-form-item label="启用状态">
            <div class="status-toggle-row">
              <el-switch v-model="form.enabled" />
              <span>{{ form.enabled ? '聚合启用中' : '聚合已停用' }}</span>
            </div>
            <p class="helper-text">停用后，该聚合不会出现在策略组的选择列表中</p>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="dialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveAggregation" :loading="saving">
            保存
          </el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog
      v-model="previewDialogVisible"
      class="aggregation-helper-dialog"
      title="节点预览"
      width="650px"
      :close-on-click-modal="false"
    >
      <div v-loading="previewLoading">
        <el-alert
          v-if="previewNodes.length > 0"
          :title="`共 ${previewNodes.length} 个节点`"
          type="success"
          :closable="false"
          class="dialog-alert"
        />

        <!-- 订阅统计信息 -->
        <div v-if="Object.keys(previewSubscriptionCounts).length > 0" class="preview-section">
          <div class="section-title">订阅统计</div>
          <div class="subscription-count-list">
            <div
              v-for="(count, subId) in previewSubscriptionCounts"
              :key="subId"
              class="subscription-count-item clickable"
              @click="showSubscriptionNodes({ id: subId, name: getSubscriptionName(subId) })"
            >
              <div class="subscription-info">
                <el-icon style="color: #6B73FF;"><Postcard /></el-icon>
                <span class="subscription-name">{{ getSubscriptionName(subId) }}</span>
              </div>
              <el-tag type="primary" size="large">{{ count }} 个节点</el-tag>
            </div>
          </div>
        </div>

        <!-- 所有节点列表 -->
        <div v-if="previewNodes.length > 0" class="preview-section">
          <div class="section-title">所有节点</div>
          <el-scrollbar max-height="400px">
            <div class="node-list">
              <div
                v-for="(node, index) in previewNodes"
                :key="index"
                class="node-item clickable-node"
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
            </div>
          </el-scrollbar>
        </div>

        <div v-if="previewNodes.length === 0 && !previewLoading" class="empty-helper">
          暂无节点
        </div>
      </div>
    </el-dialog>

    <el-dialog
      v-model="subscriptionNodesDialogVisible"
      class="aggregation-helper-dialog"
      :title="`${currentSubscription.name} - 节点列表`"
      width="600px"
      :close-on-click-modal="false"
    >
      <div v-loading="subscriptionNodesLoading">
        <el-alert
          v-if="subscriptionNodes.length > 0"
          :title="`共 ${subscriptionNodes.length} 个节点`"
          type="success"
          :closable="false"
          class="dialog-alert"
        />
        <div v-if="subscriptionNodes.length === 0 && !subscriptionNodesLoading" class="empty-helper">
          暂无节点
        </div>
        <el-scrollbar max-height="400px">
          <div class="node-list">
            <div v-for="node in subscriptionNodes" :key="node.id" class="node-item">
              <el-icon><Connection /></el-icon>
              <span>{{ node.name }}</span>
              <el-tag size="small" type="info" style="margin-left: auto;">{{ node.type }}</el-tag>
            </div>
          </div>
        </el-scrollbar>
      </div>
      <template #footer>
        <el-button @click="subscriptionNodesDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Rank, Edit, Delete, View, Connection, Loading, Postcard, Close, Link, Filter, ArrowDown } from '@element-plus/icons-vue'
import api from '@/api'
import Sortable from 'sortablejs'
import * as yaml from 'js-yaml'

interface Subscription {
  id: string
  name: string
}

interface Node {
  id: string
  name: string
}

interface Aggregation {
  id?: string
  name: string
  subscriptions: string[]
  nodes: string[]
  description?: string
  enabled?: boolean
  regex_filter?: string
  created_at?: string
  updated_at?: string
  node_count?: number
  loading_count?: boolean
}

const aggregations = ref<Aggregation[]>([])
const subscriptions = ref<Subscription[]>([])
const nodes = ref<Node[]>([])
const savingStatus = ref<Record<string, boolean>>({})

const dialogVisible = ref(false)
const isEdit = ref(false)
const saving = ref(false)
const cardContainer = ref<HTMLElement | null>(null)

const previewDialogVisible = ref(false)
const previewLoading = ref(false)
const previewNodes = ref<any[]>([])
const previewSubscriptionCounts = ref<Record<string, number>>({})
const expandedPreviewNodes = ref<Set<number>>(new Set())

const subscriptionNodesDialogVisible = ref(false)
const subscriptionNodesLoading = ref(false)
const subscriptionNodes = ref<any[]>([])
const currentSubscription = ref<{ id: string; name: string }>({ id: '', name: '' })

const form = ref<Aggregation>({
  name: '',
  subscriptions: [],
  nodes: [],
  description: '',
  enabled: true,
  regex_filter: ''
})

const loadAggregationNodeCount = async (aggregation: Aggregation) => {
  if (!aggregation.id) return

  try {
    aggregation.loading_count = true
    const response = await api.get(`/aggregations/${aggregation.id}/count`)
    if (response.data.success) {
      aggregation.node_count = response.data.total_count
    }
  } catch (error) {
    console.error('Failed to load node count:', error)
    aggregation.node_count = 0
  } finally {
    aggregation.loading_count = false
  }
}

const loadAggregations = async () => {
  try {
    const response = await api.get('/aggregations')
    aggregations.value = response.data

    aggregations.value.forEach(agg => {
      if (agg.id && savingStatus.value[agg.id] === undefined) {
        savingStatus.value[agg.id] = false
      }
      // 从本地缓存加载节点数量
      loadAggregationNodeCount(agg)
    })
  } catch (error) {
    console.error('Failed to load aggregations:', error)
    ElMessage.error('加载聚合失败')
  }
}

const loadSubscriptions = async () => {
  try {
    const response = await api.get('/subscriptions')
    subscriptions.value = response.data.filter((sub: any) => sub.enabled !== false)
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
  }
}

const loadNodes = async () => {
  try {
    const response = await api.get('/nodes')
    nodes.value = response.data.filter((node: any) => node.enabled !== false)
  } catch (error) {
    console.error('Failed to load nodes:', error)
  }
}

const getSubscriptionName = (subId: string) => {
  const sub = subscriptions.value.find(s => s.id === subId)
  return sub ? sub.name : subId
}

const getNodeName = (nodeId: string) => {
  const node = nodes.value.find(n => n.id === nodeId)
  return node ? node.name : nodeId
}

const formatDate = (dateString: string) => {
  if (!dateString) return '-'
  const date = new Date(dateString)
  return date.toLocaleString('zh-CN')
}

const showAddDialog = () => {
  isEdit.value = false
  form.value = {
    name: '',
    subscriptions: [],
    nodes: [],
    description: '',
    enabled: true,
    regex_filter: ''
  }
  dialogVisible.value = true
}

const handleToggle = (aggregation: Aggregation) => {
  aggregation.enabled = !aggregation.enabled
  toggleAggregationEnabled(aggregation)
}

const toggleAggregationEnabled = async (aggregation: Aggregation) => {
  if (!aggregation.id) return
  const previous = !aggregation.enabled
  savingStatus.value[aggregation.id] = true
  try {
    await api.put(`/aggregations/${aggregation.id}`, aggregation)
    ElMessage.success(aggregation.enabled ? '已启用' : '已禁用')
  } catch (error) {
    ElMessage.error('更新状态失败')
    aggregation.enabled = previous
    loadAggregations()
  } finally {
    savingStatus.value[aggregation.id] = false
  }
}

const editAggregation = (aggregation: Aggregation) => {
  isEdit.value = true
  form.value = { ...aggregation }
  dialogVisible.value = true
}

const saveAggregation = async () => {
  if (!form.value.name) {
    ElMessage.warning('请输入聚合名称')
    return
  }

  if (form.value.subscriptions.length === 0 && form.value.nodes.length === 0) {
    ElMessage.warning('请至少选择一个订阅或节点')
    return
  }

  try {
    saving.value = true

    if (isEdit.value && form.value.id) {
      await api.put(`/aggregations/${form.value.id}`, form.value)
      ElMessage.success('聚合已更新')
    } else {
      await api.post('/aggregations', form.value)
      ElMessage.success('聚合已创建')
    }

    dialogVisible.value = false
    await loadAggregations()
  } catch (error) {
    console.error('Failed to save aggregation:', error)
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

const deleteAggregation = async (aggregation: Aggregation) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除聚合 "${aggregation.name}" 吗？`,
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await api.delete(`/aggregations/${aggregation.id}`)
    ElMessage.success('聚合已删除')

    if (aggregation.id) {
      delete savingStatus.value[aggregation.id]
    }

    await loadAggregations()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      console.error('Failed to delete aggregation:', error)
      ElMessage.error('删除失败')
    }
  }
}

const togglePreviewExpand = (index: number) => {
  if (expandedPreviewNodes.value.has(index)) {
    expandedPreviewNodes.value.delete(index)
  } else {
    expandedPreviewNodes.value.add(index)
  }
  expandedPreviewNodes.value = new Set(expandedPreviewNodes.value)
}

const formatNodeToYaml = (node: any) => {
  return yaml.dump(node, { indent: 2, lineWidth: -1 }).trim()
}

const handlePreviewNodes = async (aggregation: Aggregation) => {
  if (!aggregation.id) return
  previewDialogVisible.value = true
  previewLoading.value = true
  expandedPreviewNodes.value = new Set()

  try {
    const response = await api.get(`/aggregations/${aggregation.id}/preview`)
    if (response.data.success) {
      previewNodes.value = response.data.nodes || []
      previewSubscriptionCounts.value = response.data.subscription_node_counts || {}

      // 预览后更新列表中的节点数量（因为预览会更新各订阅的本地缓存）
      aggregation.node_count = response.data.count || 0
    } else {
      ElMessage.error(response.data.message || '获取节点失败')
    }
  } catch (error) {
    console.error('Failed to preview nodes:', error)
    ElMessage.error('获取节点失败')
  } finally {
    previewLoading.value = false
  }
}

const showSubscriptionNodes = async (subscription: { id: string; name: string }) => {
  currentSubscription.value = subscription
  subscriptionNodesDialogVisible.value = true
  subscriptionNodesLoading.value = true
  subscriptionNodes.value = []

  try {
    const response = await api.post(`/subscriptions/${subscription.id}/fetch`, { preview: true })
    if (response.data.success) {
      subscriptionNodes.value = response.data.nodes || []
    } else {
      ElMessage.error(response.data.message || '获取节点列表失败')
    }
  } catch (error) {
    console.error('Failed to get subscription nodes:', error)
    ElMessage.error('获取节点列表失败')
  } finally {
    subscriptionNodesLoading.value = false
  }
}

const initSortable = () => {
  nextTick(() => {
    if (cardContainer.value) {
      Sortable.create(cardContainer.value, {
        animation: 150,
        handle: '.card-drag-handle',
        onEnd: async ({ oldIndex, newIndex }) => {
          if (oldIndex === newIndex) return

          const moved = aggregations.value.splice(oldIndex, 1)[0]
          aggregations.value.splice(newIndex, 0, moved)

          try {
            await api.post('/aggregations/reorder', {
              aggregations: aggregations.value
            })
            ElMessage.success('排序已更新')
          } catch (error) {
            console.error('Failed to save order:', error)
            ElMessage.error('保存排序失败')
            loadAggregations()
          }
        }
      })
    }
  })
}

onMounted(async () => {
  await loadAggregations()
  await Promise.all([loadSubscriptions(), loadNodes()])
  initSortable()
})
</script>

<style scoped>
.aggregation-page {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --agg-radius-xl: 40px;
  --agg-radius-lg: 24px;
  --agg-radius-md: 16px;
  --agg-radius-sm: 12px;
  --agg-radius-pill: 999px;
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
  -webkit-text-fill-color: transparent;
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
  border-radius: var(--agg-radius-md, 16px);
  font-weight: 600;
  font-size: 14px;
  border: none;
  background: rgba(107, 115, 255, 0.15);
  color: #4a5bff;
  transition: all 0.2s ease;
}

.action-btn .el-icon {
  font-size: 16px;
}

.action-btn.action-primary {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  color: #fff;
  box-shadow: 0 12px 30px rgba(87, 104, 255, 0.25);
}

.action-btn.action-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 16px 36px rgba(87, 104, 255, 0.28);
}

.empty-state {
  margin-top: 32px;
}

.aggregation-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  margin-top: 24px;
}

.aggregation-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 28px 26px 24px;
  border-radius: var(--agg-radius-lg, 24px);
  background: #fff;
  border: 1px solid rgba(107, 115, 255, 0.12);
  box-shadow: 0 20px 40px rgba(91, 112, 255, 0.16);
  transition: transform 0.25s ease, box-shadow 0.25s ease;
}

.aggregation-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 24px 48px rgba(91, 112, 255, 0.2);
}

.aggregation-card.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
}

.card-drag-handle {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(107, 115, 255, 0.14);
  color: #616bff;
  cursor: grab;
  transition: background 0.2s ease, color 0.2s ease;
}

.card-drag-handle:hover {
  background: rgba(107, 115, 255, 0.22);
  color: #3f4ffa;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.card-title-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.card-name {
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.card-meta {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  padding: 4px 12px;
  border-radius: var(--agg-radius-pill);
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  font-size: 12px;
  font-weight: 500;
}

.status-toggle-btn {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  border: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(107, 115, 255, 0.18);
  color: #4e5eff;
  cursor: pointer;
  transition: all 0.2s ease;
  box-shadow: 0 12px 26px rgba(87, 104, 255, 0.18);
}

.status-toggle-btn.compact {
  width: 40px;
  height: 40px;
}

.status-toggle-btn .el-icon {
  font-size: 18px;
}

.status-toggle-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 16px 30px rgba(87, 104, 255, 0.22);
}

.status-toggle-btn.active {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  color: #fff;
}

.status-toggle-btn.loading {
  opacity: 0.6;
  cursor: progress;
}

.status-toggle-btn:disabled {
  cursor: not-allowed;
}

.card-description {
  font-size: 13px;
  color: #6c74a0;
  background: rgba(107, 115, 255, 0.08);
  padding: 12px 14px;
  border-radius: var(--agg-radius-md, 16px);
}

.card-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.card-section.inline {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
}

.section-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #7d88af;
  font-weight: 600;
}

.section-label .el-icon {
  font-size: 16px;
  color: #4e5eff;
}

.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.data-tag {
  background: rgba(107, 115, 255, 0.12);
  border: none;
  color: #4e5eff;
}

.empty-text {
  font-size: 12px;
  color: #a0a8c2;
}

.code-box {
  padding: 12px 14px;
  border-radius: var(--agg-radius-md, 16px);
  background: #f4f6ff;
  color: #1f2d3d;
  font-size: 13px;
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  border: 1px solid rgba(107, 115, 255, 0.12);
}

.code-box.small {
  max-height: 160px;
  overflow: auto;
}

.count-tag {
  cursor: pointer;
  border-radius: var(--agg-radius-pill);
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-top: auto;
}

.time-text {
  font-size: 12px;
  color: #9097b5;
}

.card-actions {
  display: flex;
  gap: 10px;
}

.card-btn.el-button {
  border-radius: var(--agg-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  padding: 0 16px;
}

.card-btn.ghost {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.25);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  color: #9b8fff;
  border: 1px solid rgba(155, 143, 255, 0.28);
}

.aggregation-dialog :deep(.el-dialog),
:deep(.el-overlay-dialog .aggregation-dialog) {
  border-radius: var(--agg-radius-xl, 40px) !important;
  overflow: hidden;
  background: rgba(252, 253, 255, 0.97);
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  backdrop-filter: blur(20px);
  --el-dialog-border-radius: var(--agg-radius-xl, 40px);
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
  border-radius: var(--agg-radius-lg, 24px);
  padding: 30px 28px 26px;
  box-shadow: 0 18px 30px rgba(91, 112, 255, 0.12);
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.aggregation-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

:deep(.aggregation-form .el-form-item__label) {
  font-weight: 600;
  font-size: 13px;
  color: #6c74a0;
}

:deep(.aggregation-form .el-input__wrapper),
:deep(.aggregation-form .el-select .el-input__wrapper),
:deep(.aggregation-form .el-textarea__inner) {
  border-radius: var(--agg-radius-md, 16px);
  border: none;
  box-shadow: 0 0 0 1px rgba(107, 115, 255, 0.14);
  background-color: #f9faff;
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}

:deep(.aggregation-form .el-input__wrapper.is-focus),
:deep(.aggregation-form .el-select .el-input__wrapper.is-focus),
:deep(.aggregation-form .el-textarea__inner:focus) {
  box-shadow: 0 0 0 2px rgba(107, 115, 255, 0.32);
  transform: translateY(-1px);
  background-color: #fff;
}

.helper-text {
  margin: 6px 0 0;
  font-size: 12px;
  color: #9099bf;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.footer-btn {
  min-width: 118px;
  height: 42px;
  border-radius: var(--agg-radius-md, 16px);
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

.aggregation-helper-dialog :deep(.el-dialog) {
  border-radius: var(--agg-radius-lg, 24px);
}

.dialog-alert {
  margin-bottom: 16px;
}

.empty-helper {
  text-align: center;
  padding: 40px 0;
  color: #909399;
}

.preview-section {
  margin-top: 20px;
}

.preview-section:first-child {
  margin-top: 0;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #404a67;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid #eef1f8;
}

.subscription-count-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.subscription-count-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-radius: var(--agg-radius-md, 16px);
  background: rgba(107, 115, 255, 0.08);
  transition: all 0.2s ease;
}

.subscription-count-item.clickable {
  cursor: pointer;
}

.subscription-count-item.clickable:hover {
  background: rgba(107, 115, 255, 0.14);
  transform: translateX(4px);
}

.subscription-info {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-weight: 600;
  color: #404a67;
}

.node-list {
  display: flex;
  flex-direction: column;
}

.node-item {
  padding: 12px 16px;
  border-bottom: 1px solid #eef1f8;
  color: #4b5678;
}

.node-item.clickable-node {
  cursor: pointer;
  transition: background 0.2s ease;
}

.node-item.clickable-node:hover {
  background: #f7f8ff;
}

.node-item:last-child {
  border-bottom: none;
}

.node-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.node-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.node-name .el-icon {
  color: #4e5eff;
  font-size: 16px;
}

.expand-arrow {
  font-size: 14px;
  color: #7d88af;
  margin-left: auto;
  transition: transform 0.2s ease;
  flex-shrink: 0;
}

.expand-arrow.expanded {
  transform: rotate(180deg);
}

.node-details {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: #7d88af;
}

.node-server {
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  color: #7d88af;
}

.code-box {
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

@media (max-width: 1024px) {
  .aggregation-page {
    padding: 24px;
    --agg-radius-xl: 32px;
    --agg-radius-lg: 22px;
    --agg-radius-md: 14px;
  }

  .aggregation-grid {
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  }

  .aggregation-dialog :deep(.el-dialog__body),
  .aggregation-dialog :deep(.el-dialog__footer) {
    padding: 0 24px 24px;
  }
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    margin: -20px -16px 20px -16px;
    padding: 20px 16px;
  }

  .header-actions {
    width: 100%;
    flex-direction: column;
    justify-content: flex-start;
    gap: 10px;
    align-items: stretch;
  }

  :deep(.header-actions .el-button + .el-button) {
    margin-left: 0;
  }

  .action-btn {
    width: 100%;
    justify-content: center;
    box-sizing: border-box;
  }

  .aggregation-page {
    padding: 20px;
    --agg-radius-xl: 28px;
    --agg-radius-lg: 20px;
    --agg-radius-md: 14px;
  }

  .aggregation-grid {
    grid-template-columns: 1fr;
  }

  .card-actions {
    flex-direction: column;
    flex-wrap: nowrap;
    align-items: stretch;
    gap: 10px;
    width: 100%;
  }

  :deep(.card-actions .el-button + .el-button) {
    margin-left: 0;
  }

  .card-btn.el-button {
    width: 100%;
    flex: unset;
    display: flex;
    box-sizing: border-box;
    justify-content: center;
  }

  .aggregation-dialog :deep(.el-dialog__body),
  .aggregation-dialog :deep(.el-dialog__footer) {
    padding: 0 24px 24px;
  }
}

@media (max-width: 480px) {
  .aggregation-page {
    padding: 16px;
    --agg-radius-xl: 24px;
    --agg-radius-lg: 18px;
    --agg-radius-md: 12px;
  }

  .action-btn {
    width: 100%;
    justify-content: center;
  }

  .aggregation-dialog :deep(.el-dialog__body),
  .aggregation-dialog :deep(.el-dialog__footer) {
    padding: 0 20px 20px;
  }

  .dialog-card {
    padding: 24px 18px 20px;
  }
}
</style>
