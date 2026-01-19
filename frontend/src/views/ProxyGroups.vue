<template>
  <div class="proxy-groups-page">
    <div class="page-header">
      <div class="title-block">
        <h2>策略管理</h2>
        <p>配置策略组的来源与输出节点</p>
      </div>
      <div class="header-actions">
        <el-button class="action-btn action-primary" @click="showAddDialog">
          <el-icon><Plus /></el-icon>
          添加策略组
        </el-button>
      </div>
    </div>

    <div class="groups-grid" ref="groupsContainer">
      <div
        v-for="group in proxyGroups"
        :key="group.id || group.name"
        class="group-card"
        :class="{ disabled: !group.enabled }"
        :data-name="group.name"
      >
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-drag-handle">
              <el-icon><DCaret /></el-icon>
            </div>
            <div class="card-title">
              <span class="group-icon" v-if="getGroupIcon(group.name)">{{ getGroupIcon(group.name) }}</span>
              <span class="group-name">{{ getGroupNameWithoutIcon(group.name) }}</span>
            </div>
          </div>
          <div class="card-meta">
            <span v-if="group.follow_group" class="meta-pill follow-pill">跟随</span>
            <span v-else class="meta-pill type-pill" :class="`type-${group.type}`">{{ getGroupTypeLabel(group.type) }}</span>
            <button
              type="button"
              class="status-toggle-btn compact"
              :class="{ active: group.enabled, loading: group.id ? savingStatus[group.id] : false }"
              @click="handleToggle(group)"
              :disabled="group.id ? savingStatus[group.id] : false"
            >
              <el-icon><View /></el-icon>
            </button>
          </div>
        </div>

        <div class="card-body">
          <!-- 始终显示：简要来源信息 -->
          <div class="card-section compact">
            <div class="section-content summary-text">{{ getSourceSummary(group) }}</div>
            <button
              class="expand-toggle-btn"
              @click.stop="toggleCardExpand(group.id || group.name)"
              :title="isCardExpanded(group.id || group.name) ? '收起详情' : '展开详情'"
            >
              <el-icon :class="{ rotated: isCardExpanded(group.id || group.name) }">
                <ArrowDown />
              </el-icon>
            </button>
          </div>

          <!-- 展开时显示：详细信息 -->
          <template v-if="isCardExpanded(group.id || group.name)">
            <div v-if="group.follow_group" class="card-section">
              <div class="section-label">跟随策略</div>
              <div class="section-content">{{ getFollowGroupName(group.follow_group) || '-' }}</div>
            </div>

            <template v-if="!group.follow_group">
              <div v-if="hasAggregations(group)" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>聚合来源</div>
                <div class="section-content tag-list">
                  <el-tag
                    v-for="aggName in getAggregationsList(group)"
                    :key="aggName"
                    size="small"
                    type="warning"
                    class="data-tag"
                  >
                    {{ aggName }}
                  </el-tag>
                  <span v-if="getAggregationsList(group).length === 0" class="empty-text">无</span>
                </div>
              </div>

              <div v-if="hasAggregations(group) && getAggregationSubscriptions(group)" class="card-section">
                <div class="section-label"><el-icon><Link /></el-icon>包含订阅</div>
                <div class="section-content">{{ getAggregationSubscriptions(group) }}</div>
              </div>

              <div v-if="hasAggregations(group) && getAggregationNodes(group)" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>包含节点</div>
                <div class="section-content">{{ getAggregationNodes(group) }}</div>
              </div>

              <div v-if="group.aggregation_regex" class="card-section">
                <div class="section-label"><el-icon><Filter /></el-icon>聚合正则</div>
                <div class="section-content code-box small">{{ group.aggregation_regex }}</div>
              </div>

              <div v-if="hasSubscriptions(group) && !hasAggregations(group)" class="card-section">
                <div class="section-label"><el-icon><Link /></el-icon>订阅来源</div>
                <div class="section-content">{{ getSubscriptionDisplay(group) }}</div>
              </div>

              <div v-if="group.regex && hasSubscriptions(group) && !hasAggregations(group)" class="card-section">
                <div class="section-label"><el-icon><Filter /></el-icon>订阅正则</div>
                <div class="section-content code-box small">{{ group.regex }}</div>
              </div>

              <div v-if="hasManualNodes(group) && !hasAggregations(group)" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>节点来源</div>
                <div class="section-content">{{ getManualNodesDisplay(group) }}</div>
              </div>

              <div v-if="hasIncludeGroups(group)" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>策略组来源</div>
                <div class="section-content tag-list">
                  <el-tag
                    v-for="name in getIncludeGroupsList(group)"
                    :key="name"
                    size="small"
                    type="info"
                    class="data-tag"
                  >
                    {{ name }}
                  </el-tag>
                </div>
              </div>

              <div v-if="hasManualNodes(group) && hasIncludeGroups(group)" class="card-section">
                <div class="section-label"><el-icon><Rank /></el-icon>节点顺序</div>
                <div class="section-content">{{ getProxyOrderLabel(group.proxy_order) }}</div>
              </div>

              <div v-if="group.type !== 'select'" class="card-section">
                <div class="section-label"><el-icon><Link /></el-icon>测试 URL</div>
                <div class="section-content">{{ group.url || '-' }}</div>
              </div>

              <div v-if="group.type !== 'select'" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>测试间隔</div>
                <div class="section-content">{{ group.interval || '-' }} 秒</div>
              </div>

              <div v-if="group.type === 'load-balance' && group.strategy" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>负载策略</div>
                <div class="section-content">{{ getStrategyLabel(group.strategy) }}</div>
              </div>

              <div v-if="group.type === 'load-balance' && group.lazy !== undefined" class="card-section">
                <div class="section-label"><el-icon><Connection /></el-icon>懒加载</div>
                <div class="section-content">{{ group.lazy ? '是' : '否' }}</div>
              </div>
            </template>
          </template>

        </div>

        <div class="card-footer">
          <el-button class="card-btn ghost" size="small" @click="editGroup(group)">
            <el-icon><Edit /></el-icon>
            编辑
          </el-button>
          <el-button class="card-btn danger" size="small" @click="deleteGroup(group)">
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </div>
      </div>
    </div>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑策略组' : '添加策略组'" width="720px" class="groups-dialog">
      <el-form :model="form" label-width="100px" class="groups-form">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="请输入策略组名称" />
        </el-form-item>
        <el-form-item label="类型" v-if="!enabledSources.includes('follow')">
          <el-select v-model="form.type" placeholder="请选择策略组类型">
            <el-option label="手动选择 (Select)" value="select" />
            <el-option label="自动测速 (URL-Test)" value="url-test" />
            <el-option label="故障转移 (Fallback)" value="fallback" />
            <el-option label="负载均衡 (Load-Balance)" value="load-balance" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点来源">
          <el-checkbox-group v-model="enabledSources">
            <el-checkbox label="subscription" :disabled="enabledSources.includes('follow')" v-if="!enabledSources.includes('follow')">订阅</el-checkbox>
            <el-checkbox label="node" :disabled="enabledSources.includes('follow')" v-if="!enabledSources.includes('follow')">节点</el-checkbox>
            <el-checkbox label="aggregation" :disabled="enabledSources.includes('follow')" v-if="!enabledSources.includes('follow')">
              聚合
            </el-checkbox>
            <el-checkbox label="strategy" :disabled="enabledSources.includes('follow')" v-if="!enabledSources.includes('follow')">策略</el-checkbox>
            <el-checkbox label="follow" @change="handleFollowChange">跟随</el-checkbox>
          </el-checkbox-group>
        </el-form-item>

        <!-- 跟随策略组 -->
        <el-form-item label="跟随策略" v-if="enabledSources.includes('follow')">
          <el-select
            v-model="form.follow_group"
            clearable
            placeholder="选择要跟随的策略组"
            style="width: 100%"
          >
            <el-option
              v-for="group in availableStrategies"
              :key="group.id"
              :label="group.name"
              :value="group.id"
            />
          </el-select>
          <div style="font-size: 12px; color: #909399; margin-top: 4px;">
            跟随模式：将完全复制被跟随策略组的所有配置（类型、节点来源、测试参数等），只保留自己的名称
          </div>
        </el-form-item>

        <!-- 订阅筛选 -->
        <template v-if="enabledSources.includes('subscription')">
          <el-form-item label="订阅筛选">
            <el-select
              v-model="form.subscriptions"
              multiple
              clearable
              placeholder="选择订阅（自动包含订阅的所有节点）"
              style="width: 100%"
            >
              <el-option
                v-for="sub in subscriptions"
                :key="sub.id"
                :label="sub.name"
                :value="sub.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="正则过滤" v-if="form.subscriptions && form.subscriptions.length > 0">
            <el-input
              v-model="form.regex"
              clearable
              placeholder="输入正则表达式（可选，过滤订阅节点名称）"
            />
          </el-form-item>
        </template>

        <!-- 手动节点 -->
        <el-form-item label="包含节点" v-if="enabledSources.includes('node')">
          <el-select
            v-model="form.manual_nodes"
            multiple
            filterable
            clearable
            placeholder="手动选择节点"
            style="width: 100%"
          >
            <el-option
              v-for="node in availableNodes"
              :key="node.id"
              :label="node.name"
              :value="node.id"
            />
          </el-select>
        </el-form-item>

        <!-- 引用聚合 -->
        <el-form-item label="引用聚合" v-if="enabledSources.includes('aggregation')">
          <el-select
            v-model="form.aggregations"
            multiple
            filterable
            clearable
            placeholder="选择订阅聚合"
            style="width: 100%"
          >
            <el-option
              v-for="agg in availableAggregations"
              :key="agg.id"
              :label="agg.name"
              :value="agg.id"
            >
              <div style="display: flex; justify-content: space-between; align-items: center; width: 100%;">
                <span>{{ agg.name }}</span>
                <el-tag v-if="agg.regex_filter" type="info" size="small" style="margin-left: 8px;">
                  {{ agg.regex_filter }}
                </el-tag>
              </div>
            </el-option>
          </el-select>
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            聚合中的所有订阅和节点都会被包含
          </div>
        </el-form-item>
        <!-- 聚合正则过滤 -->
        <el-form-item label="正则过滤" v-if="enabledSources.includes('aggregation') && form.aggregations && form.aggregations.length > 0">
          <el-input
            v-model="form.aggregation_regex"
            clearable
            placeholder="输入正则表达式（可选，过滤聚合节点名称）"
          />
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            此正则过滤将应用于聚合中的节点，不使用聚合自带的正则过滤器
          </div>
        </el-form-item>

        <!-- 引用策略 -->
        <el-form-item label="引用策略" v-if="enabledSources.includes('strategy')">
          <el-select
            v-model="form.include_groups"
            multiple
            filterable
            clearable
            placeholder="选择已有策略组"
            style="width: 100%"
          >
            <el-option
              v-for="group in availableStrategies"
              :key="group.id"
              :label="group.name"
              :value="group.id"
            />
          </el-select>
        </el-form-item>
        <!-- 已选择的节点和策略排序 -->
        <el-form-item
          label="顺序调整"
          v-if="orderedProxiesList.length > 0"
        >
          <el-collapse v-model="activeCollapsePanel" style="width: 100%">
            <el-collapse-item name="sorting">
              <template #title>
                <span style="font-size: 13px; color: #606266;">拖拽调整顺序（点击展开/收起）</span>
              </template>
              <div class="ordered-proxies-container">
                <div ref="orderedProxiesRef" class="ordered-proxies-list">
                  <div
                    v-for="(item, index) in orderedProxiesList"
                    :key="`${item.type}-${item.id}`"
                    class="ordered-proxy-item"
                    :data-index="index"
                  >
                    <el-icon class="drag-handle"><DCaret /></el-icon>
                    <el-tag
                      :type="item.type === 'node' ? 'success' : item.type === 'aggregation' ? 'warning' : 'info'"
                      size="small"
                    >
                      {{ item.type === 'node' ? '节点' : item.type === 'aggregation' ? '聚合' : '策略' }}
                    </el-tag>
                    <span class="proxy-name">{{ item.name }}</span>
                  </div>
                </div>
              </div>
            </el-collapse-item>
          </el-collapse>
        </el-form-item>
        <el-form-item label="测试URL" v-if="needsUrl && !enabledSources.includes('follow')">
          <el-input
            v-model="form.url"
            placeholder="http://www.gstatic.com/generate_204"
          />
        </el-form-item>
        <el-form-item label="测试间隔(秒)" v-if="needsUrl && !enabledSources.includes('follow')">
          <el-input-number v-model="form.interval" :min="60" :max="3600" style="width: 100%" />
        </el-form-item>
        <!-- 负载均衡特有字段 -->
        <el-form-item label="负载策略" v-if="form.type === 'load-balance' && !enabledSources.includes('follow')">
          <el-select v-model="form.strategy" clearable placeholder="请选择负载策略（可选）" style="width: 100%">
            <el-option label="轮询 (round-robin)" value="round-robin" />
            <el-option label="一致性哈希 (consistent-hashing)" value="consistent-hashing" />
            <el-option label="会话保持 (sticky-sessions)" value="sticky-sessions" />
          </el-select>
        </el-form-item>
        <el-form-item label="懒加载" v-if="form.type === 'load-balance' && !enabledSources.includes('follow')">
          <el-select v-model="form.lazy" clearable placeholder="请选择是否启用懒加载（可选）" style="width: 100%">
            <el-option label="是" :value="true" />
            <el-option label="否" :value="false" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveGroup">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, DCaret, View, Edit, Delete, Link, Connection, Filter, Rank, ArrowDown } from '@element-plus/icons-vue'
import { proxyGroupApi, nodeApi } from '@/api'
import type { ProxyGroup, ProxyNode, Subscription } from '@/types'
import api from '@/api'
import Sortable from 'sortablejs'

const proxyGroups = ref<ProxyGroup[]>([])
const nodes = ref<ProxyNode[]>([])
const subscriptions = ref<Subscription[]>([])
const aggregations = ref<any[]>([])
const savingStatus = ref<Record<string, boolean>>({})
const dialogVisible = ref(false)
const isEdit = ref(false)
const enabledSources = ref<string[]>([])

const groupsContainer = ref<HTMLElement | null>(null)
const orderedProxiesRef = ref<HTMLElement | null>(null)
const activeCollapsePanel = ref<string[]>([]) // 默认收起，空数组表示没有展开的面板
let orderedProxiesSortable: any = null
const originalGroupName = ref<string>('') // 保存原始策略组名称，用于检测名称变化
const expandedCards = ref<Set<string>>(new Set()) // 展开的卡片ID集合
const form = ref<Partial<ProxyGroup>>({
  name: '',
  type: 'select',
  url: 'http://www.gstatic.com/generate_204',
  interval: 300,
  subscriptions: [],
  regex: '',
  aggregation_regex: '',
  manual_nodes: [],
  aggregations: [],
  include_groups: [],
  proxies_order: []
})

const availableNodes = computed(() => {
  // 返回所有节点（包含 id 和 name），默认包含DIRECT和REJECT
  const nodeOptions = nodes.value.map(n => ({ id: n.id, name: n.name }))
  // DIRECT 和 REJECT 使用名称作为 ID
  return [
    { id: 'DIRECT', name: 'DIRECT' },
    { id: 'REJECT', name: 'REJECT' },
    ...nodeOptions
  ]
})

const availableStrategies = computed(() => {
  // 排除当前正在编辑的策略组，避免循环引用
  return proxyGroups.value
    .filter(g => g.id !== form.value.id)
    .map(g => ({ id: g.id, name: g.name }))
})

const availableAggregations = computed(() => {
  // 只返回已开启的聚合,包含正则过滤器信息
  return aggregations.value
    .filter(a => a.enabled !== false)
    .map(a => ({ id: a.id, name: a.name, regex_filter: a.regex_filter }))
})

const needsUrl = computed(() => {
  return ['url-test', 'fallback', 'load-balance'].includes(form.value.type || '')
})

// 已排序的代理列表（用于显示）
const orderedProxiesList = computed(() => {
  const result: Array<{type: string, id: string, name: string}> = []
  const proxiesOrder = form.value.proxies_order || []

  // 如果有排序数据，使用排序数据
  if (proxiesOrder.length > 0) {
    proxiesOrder.forEach(item => {
      if (item.type === 'node') {
        const node = availableNodes.value.find(n => n.id === item.id)
        if (node) {
          result.push({ type: 'node', id: item.id, name: node.name })
        }
      } else if (item.type === 'strategy') {
        const strategy = availableStrategies.value.find(s => s.id === item.id)
        if (strategy) {
          result.push({ type: 'strategy', id: item.id, name: strategy.name })
        }
      } else if (item.type === 'aggregation') {
        const aggregation = availableAggregations.value.find(a => a.id === item.id)
        if (aggregation) {
          result.push({ type: 'aggregation', id: item.id, name: aggregation.name })
        }
      }
    })
  } else {
    // 没有排序数据时，自动生成（节点在前，聚合在中间，策略在后）
    const manualNodes = form.value.manual_nodes || []
    const aggregations = form.value.aggregations || []
    const includeGroups = form.value.include_groups || []

    manualNodes.forEach(nodeId => {
      const node = availableNodes.value.find(n => n.id === nodeId)
      if (node) {
        result.push({ type: 'node', id: nodeId, name: node.name })
      }
    })

    aggregations.forEach(aggId => {
      const aggregation = availableAggregations.value.find(a => a.id === aggId)
      if (aggregation) {
        result.push({ type: 'aggregation', id: aggId, name: aggregation.name })
      }
    })

    includeGroups.forEach(groupId => {
      const strategy = availableStrategies.value.find(s => s.id === groupId)
      if (strategy) {
        result.push({ type: 'strategy', id: groupId, name: strategy.name })
      }
    })
  }

  return result
})

// 查找上一个同类型策略组的配置
const findPreviousSameTypeConfig = (type: string) => {
  // 在现有策略组中查找最后一个同类型的策略组
  const sameTypeGroups = proxyGroups.value.filter(g => g.type === type)
  if (sameTypeGroups.length > 0) {
    // 返回最后一个同类型策略组的配置
    const lastGroup = sameTypeGroups[sameTypeGroups.length - 1]
    return {
      url: lastGroup.url || 'http://www.gstatic.com/generate_204',
      interval: lastGroup.interval || 300
    }
  }
  // 如果没有找到，返回默认值
  return {
    url: 'http://www.gstatic.com/generate_204',
    interval: 300
  }
}

// 监听策略组类型变化
watch(() => form.value.type, (newType, oldType) => {
  if (!oldType) return

  const needsUrlTypes = ['url-test', 'fallback', 'load-balance']
  const oldNeedsUrl = needsUrlTypes.includes(oldType)
  const newNeedsUrl = needsUrlTypes.includes(newType || '')

  // 从需要测速的类型切换到 select，删除测速参数
  if (oldNeedsUrl && newType === 'select') {
    delete form.value.url
    delete form.value.interval
  }
  // 从 select 切换到需要测速的类型，自动获取上一个同类型的配置
  else if (oldType === 'select' && newNeedsUrl && newType) {
    const config = findPreviousSameTypeConfig(newType)
    form.value.url = config.url
    form.value.interval = config.interval
  }
  // 从一个需要测速的类型切换到另一个需要测速的类型
  else if (oldNeedsUrl && newNeedsUrl && newType && oldType !== newType) {
    const config = findPreviousSameTypeConfig(newType)
    form.value.url = config.url
    form.value.interval = config.interval
  }

  // 从负载均衡切换到其他类型，删除负载均衡特有字段
  if (oldType === 'load-balance' && newType !== 'load-balance') {
    delete form.value.strategy
    delete form.value.lazy
  }
})

// 处理跟随模式切换
const handleFollowChange = (checked: boolean) => {
  if (checked) {
    // 选择跟随时，清除其他来源
    enabledSources.value = ['follow']
    form.value.manual_nodes = []
    form.value.aggregations = []
    form.value.include_groups = []
    form.value.subscriptions = []
    form.value.regex = ''
  } else {
    // 取消跟随时，清除跟随策略
    form.value.follow_group = undefined
  }
}

// 监听节点来源勾选状态变化
watch(enabledSources, (newSources) => {
  // 如果取消勾选"节点"，清空手动节点列表
  if (!newSources.includes('node')) {
    form.value.manual_nodes = []
  }
  // 如果取消勾选"聚合"，清空聚合列表和聚合正则
  if (!newSources.includes('aggregation')) {
    form.value.aggregations = []
    form.value.aggregation_regex = ''
  }
  // 如果取消勾选"策略"，清空引用策略列表
  if (!newSources.includes('strategy')) {
    form.value.include_groups = []
  }
  // 如果取消勾选"订阅"，清空订阅列表
  if (!newSources.includes('subscription')) {
    form.value.subscriptions = []
    form.value.regex = ''
  }
  // 如果取消勾选"跟随"，清空跟随策略
  if (!newSources.includes('follow')) {
    form.value.follow_group = undefined
  }
}, { deep: true })

// 监听节点、聚合和策略组选择变化，自动同步排序列表
watch([() => form.value.manual_nodes, () => form.value.aggregations, () => form.value.include_groups], ([newNodes, newAggregations, newGroups]) => {
  const currentOrder = form.value.proxies_order || []
  const newOrder: Array<{type: string, id: string}> = []

  // 保留已有的顺序
  currentOrder.forEach(item => {
    if (item.type === 'node' && newNodes?.includes(item.id)) {
      newOrder.push(item)
    } else if (item.type === 'aggregation' && newAggregations?.includes(item.id)) {
      newOrder.push(item)
    } else if (item.type === 'strategy' && newGroups?.includes(item.id)) {
      newOrder.push(item)
    }
  })

  // 添加新选择的节点（不在原顺序中的）
  newNodes?.forEach(nodeId => {
    if (!newOrder.find(item => item.type === 'node' && item.id === nodeId)) {
      newOrder.push({ type: 'node', id: nodeId })
    }
  })

  // 添加新选择的聚合（不在原顺序中的）
  newAggregations?.forEach(aggId => {
    if (!newOrder.find(item => item.type === 'aggregation' && item.id === aggId)) {
      newOrder.push({ type: 'aggregation', id: aggId })
    }
  })

  // 添加新选择的策略组（不在原顺序中的）
  newGroups?.forEach(groupId => {
    if (!newOrder.find(item => item.type === 'strategy' && item.id === groupId)) {
      newOrder.push({ type: 'strategy', id: groupId })
    }
  })

  form.value.proxies_order = newOrder
}, { deep: true })

const getGroupTypeLabel = (type: string) => {
  const labels: Record<string, string> = {
    'select': '手动选择',
    'url-test': '自动测速',
    'fallback': '故障转移',
    'load-balance': '负载均衡'
  }
  return labels[type] || type
}

const getTypeTagType = (type: string) => {
  const types: Record<string, string> = {
    'select': 'primary',
    'url-test': 'success',
    'fallback': 'warning',
    'load-balance': 'danger'
  }
  return types[type] || 'primary'
}

const getStrategyLabel = (strategy: string) => {
  const labels: Record<string, string> = {
    'round-robin': '轮询',
    'consistent-hashing': '一致性哈希',
    'sticky-sessions': '会话保持'
  }
  return labels[strategy] || strategy
}

const getProxyOrderLabel = (order?: string) => {
  const labels: Record<string, string> = {
    'nodes_first': '节点优先',
    'strategies_first': '策略优先'
  }
  return labels[order || 'nodes_first'] || '节点优先'
}

const getGroupIcon = (name: string) => {
  // 从名称中提取emoji图标（支持更广泛的emoji范围，包括国旗）
  // 国旗emoji由两个区域指示符组成
  const flagMatch = name.match(/^[\u{1F1E6}-\u{1F1FF}]{2}/u)
  if (flagMatch) return flagMatch[0]

  // 其他常见emoji
  const emojiMatch = name.match(/^[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1F000}-\u{1F02F}\u{1F0A0}-\u{1F0FF}\u{1F100}-\u{1F64F}]/u)
  return emojiMatch ? emojiMatch[0] : null
}

const getGroupNameWithoutIcon = (name: string) => {
  // 去掉名称开头的emoji图标（包括国旗），只返回文字部分
  // 先移除国旗emoji（两个区域指示符）
  let result = name.replace(/^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u, '')
  // 再移除其他emoji
  result = result.replace(/^[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1F000}-\u{1F02F}\u{1F0A0}-\u{1F0FF}\u{1F100}-\u{1F64F}]\s*/u, '')
  return result.trim()
}

const getSubscriptionNames = (subIds: string[]) => {
  return subIds
    .map(id => {
      const sub = subscriptions.value.find(s => s.id === id)
      return sub ? sub.name : id
    })
    .filter(Boolean)
}

// 判断是否有订阅来源
const hasSubscriptions = (group: ProxyGroup) => {
  if (group.subscriptions && group.subscriptions.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'subscription' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 判断是否有手动节点
const hasManualNodes = (group: ProxyGroup) => {
  if (group.manual_nodes && group.manual_nodes.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'node' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 判断是否有引用聚合
const hasAggregations = (group: ProxyGroup) => {
  if (group.aggregations && group.aggregations.length > 0) {
    return true
  }
  return false
}

// 判断是否有引用策略组
const hasIncludeGroups = (group: ProxyGroup) => {
  if (group.include_groups && group.include_groups.length > 0) {
    return true
  }
  // 兼容旧格式
  if (group.source === 'strategy' && group.proxies && group.proxies.length > 0) {
    return true
  }
  return false
}

// 获取订阅显示文本
const getSubscriptionDisplay = (group: ProxyGroup) => {
  const subIds = group.subscriptions || []
  const subNames = getSubscriptionNames(subIds)
  return subNames.join('、')
}

// 获取手动节点显示文本
const getManualNodesDisplay = (group: ProxyGroup) => {
  // 新格式：将节点 ID 转换为名称
  if (group.manual_nodes && group.manual_nodes.length > 0) {
    const nodeNames = group.manual_nodes.map(nodeId => {
      // DIRECT 和 REJECT 直接返回
      if (nodeId === 'DIRECT' || nodeId === 'REJECT') {
        return nodeId
      }
      // 根据 ID 查找节点名称
      const node = nodes.value.find(n => n.id === nodeId)
      return node ? node.name : nodeId
    })
    return nodeNames.join('、')
  }
  // 兼容旧格式
  if (group.source === 'node' && group.proxies && group.proxies.length > 0) {
    return group.proxies.join('、')
  }
  return '-'
}

// 获取引用聚合列表
const getAggregationsList = (group: ProxyGroup) => {
  // 将聚合 ID 转换为名称
  if (group.aggregations && group.aggregations.length > 0) {
    return group.aggregations.map(aggId => {
      // 根据 ID 查找聚合名称
      const agg = aggregations.value.find(a => a.id === aggId)
      return agg ? agg.name : aggId
    })
  }
  return []
}

// 获取聚合包含的订阅信息
const getAggregationSubscriptions = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allSubNames: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.subscriptions && agg.subscriptions.length > 0) {
      const subNames = agg.subscriptions.map((subId: string) => {
        const sub = subscriptions.value.find(s => s.id === subId)
        return sub ? sub.name : subId
      })
      allSubNames.push(...subNames)
    }
  })

  return allSubNames.length > 0 ? allSubNames.join('、') : ''
}

// 获取聚合包含的节点信息
const getAggregationNodes = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allNodeNames: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.nodes && agg.nodes.length > 0) {
      const nodeNames = agg.nodes.map((nodeId: string) => {
        if (nodeId === 'DIRECT' || nodeId === 'REJECT') {
          return nodeId
        }
        const node = nodes.value.find(n => n.id === nodeId)
        return node ? node.name : nodeId
      })
      allNodeNames.push(...nodeNames)
    }
  })

  return allNodeNames.length > 0 ? allNodeNames.join('、') : ''
}

// 获取聚合的正则过滤器
const getAggregationRegex = (group: ProxyGroup) => {
  if (!group.aggregations || group.aggregations.length === 0) {
    return ''
  }

  const allRegex: string[] = []
  group.aggregations.forEach(aggId => {
    const agg = aggregations.value.find(a => a.id === aggId)
    if (agg && agg.regex_filter && agg.regex_filter.trim()) {
      allRegex.push(agg.regex_filter.trim())
    }
  })

  return allRegex.length > 0 ? allRegex.join(' | ') : ''
}

// 获取引用策略组列表
const getIncludeGroupsList = (group: ProxyGroup) => {
  // 新格式：将策略组 ID 转换为名称
  if (group.include_groups && group.include_groups.length > 0) {
    return group.include_groups.map(groupId => {
      // 根据 ID 查找策略组名称
      const refGroup = proxyGroups.value.find(g => g.id === groupId)
      return refGroup ? refGroup.name : groupId
    })
  }
  // 兼容旧格式
  if (group.source === 'strategy' && group.proxies && group.proxies.length > 0) {
    return group.proxies
  }
  return []
}

// 获取跟随策略组名称
const getFollowGroupName = (groupId: string) => {
  const refGroup = proxyGroups.value.find(g => g.id === groupId)
  return refGroup ? refGroup.name : groupId
}

// 切换卡片展开/收起状态
const toggleCardExpand = (groupId: string) => {
  if (expandedCards.value.has(groupId)) {
    expandedCards.value.delete(groupId)
  } else {
    expandedCards.value.add(groupId)
  }
}

// 判断卡片是否展开
const isCardExpanded = (groupId: string) => {
  return expandedCards.value.has(groupId)
}

// 获取卡片的简要来源信息（用于折叠状态）
const getSourceSummary = (group: ProxyGroup) => {
  const sources: string[] = []

  if (group.follow_group) {
    const followName = getFollowGroupName(group.follow_group)
    return `跟随: ${followName}`
  }

  if (hasAggregations(group)) {
    const aggList = getAggregationsList(group)
    sources.push(`聚合(${aggList.length})`)
  }

  if (hasSubscriptions(group)) {
    const subIds = group.subscriptions || []
    sources.push(`订阅(${subIds.length})`)
  }

  if (hasManualNodes(group)) {
    const nodeCount = (group.manual_nodes || []).length
    sources.push(`节点(${nodeCount})`)
  }

  if (hasIncludeGroups(group)) {
    const groupCount = (group.include_groups || []).length
    sources.push(`策略(${groupCount})`)
  }

  return sources.length > 0 ? sources.join(' + ') : '无来源'
}

const loadProxyGroups = async () => {
  try {
    const { data } = await proxyGroupApi.getAll()

    // 修复缺少 ID 的策略组
    let needsSave = false
    const fixedData = data.map((group: ProxyGroup, index: number) => {
      if (!group.id) {
        needsSave = true
        return {
          ...group,
          id: `group_${Date.now()}_${index}`
        }
      }
      return group
    })

    proxyGroups.value = fixedData

    proxyGroups.value.forEach(group => {
      if (group.id && savingStatus.value[group.id] === undefined) {
        savingStatus.value[group.id] = false
      }
    })

    // 如果有修复的数据，保存回后端
    if (needsSave) {
      try {
        await api.post('/proxy-groups/reorder', { groups: fixedData })
        console.log('已自动修复缺少ID的策略组')
      } catch (error) {
        console.error('保存修复后的策略组失败:', error)
      }
    }
  } catch (error) {
    ElMessage.error('加载策略组列表失败')
  }
}

const loadNodes = async () => {
  try {
    const { data } = await nodeApi.getAll()
    nodes.value = data
  } catch (error) {
    ElMessage.error('加载节点列表失败')
  }
}

const loadSubscriptions = async () => {
  try {
    const { data } = await api.get('/subscriptions')
    subscriptions.value = data
  } catch (error) {
    ElMessage.error('加载订阅列表失败')
  }
}

const loadAggregations = async () => {
  try {
    const { data } = await api.get('/aggregations')
    aggregations.value = data
  } catch (error) {
    ElMessage.error('加载聚合列表失败')
  }
}

const showAddDialog = () => {
  isEdit.value = false
  enabledSources.value = []
  // select 类型不需要 url 和 interval
  form.value = {
    id: `group_${Date.now()}`,
    name: '',
    type: 'select',
    enabled: true,
    subscriptions: [],
    regex: '',
    aggregation_regex: '',
    manual_nodes: [],
    aggregations: [],
    include_groups: [],
    proxies_order: []
  }
  dialogVisible.value = true
}

const handleToggle = (group: ProxyGroup) => {
  group.enabled = !group.enabled
  toggleGroupEnabled(group)
}

const toggleGroupEnabled = async (group: ProxyGroup) => {
  if (!group.id) return
  const previous = !group.enabled
  savingStatus.value[group.id] = true
  try {
    await proxyGroupApi.update(group.id, group)
    ElMessage.success(group.enabled ? '已启用' : '已禁用')
  } catch (error) {
    ElMessage.error('更新状态失败')
    group.enabled = previous
    loadProxyGroups()
  } finally {
    savingStatus.value[group.id] = false
  }
}

const editGroup = (row: ProxyGroup) => {
  isEdit.value = true

  // 保存原始策略组名称，用于检测名称变化
  originalGroupName.value = row.name

  // 数据迁移：处理旧格式数据
  let manual_nodes: string[] = []
  let group_aggregations: string[] = []
  let include_groups: string[] = []
  let subscriptions: string[] = row.subscriptions ? [...row.subscriptions] : []

  if (row.manual_nodes || row.aggregations || row.include_groups) {
    // 新格式数据
    manual_nodes = row.manual_nodes ? [...row.manual_nodes] : []
    group_aggregations = row.aggregations ? [...row.aggregations] : []
    include_groups = row.include_groups ? [...row.include_groups] : []

    // 数据清理:如果策略组有聚合,需要从subscriptions和manual_nodes中过滤掉聚合包含的订阅/节点
    if (group_aggregations.length > 0 && aggregations.value.length > 0) {
      // 收集所有聚合中的订阅和节点ID
      const aggregationSubIds = new Set<string>()
      const aggregationNodeIds = new Set<string>()

      group_aggregations.forEach(aggId => {
        // 使用响应式变量 aggregations.value 查找聚合定义
        const agg = aggregations.value.find((a: any) => a.id === aggId)
        if (agg) {
          // 收集聚合的订阅ID
          if (agg.subscriptions && Array.isArray(agg.subscriptions)) {
            agg.subscriptions.forEach((subId: string) => aggregationSubIds.add(subId))
          }
          // 收集聚合的节点ID
          if (agg.nodes && Array.isArray(agg.nodes)) {
            agg.nodes.forEach((nodeId: string) => aggregationNodeIds.add(nodeId))
          }
        }
      })

      // 从策略组的subscriptions中过滤掉聚合的订阅
      subscriptions = subscriptions.filter(subId => !aggregationSubIds.has(subId))
      // 从策略组的manual_nodes中过滤掉聚合的节点
      manual_nodes = manual_nodes.filter(nodeId => !aggregationNodeIds.has(nodeId))
    }
  } else if (row.proxies && row.proxies.length > 0) {
    // 旧格式数据，根据 source 判断
    const source = row.source || 'subscription'
    if (source === 'node') {
      manual_nodes = [...row.proxies]
    } else if (source === 'strategy') {
      include_groups = [...row.proxies]
    }
  }

  // 根据实际数据初始化 enabledSources
  const sources: string[] = []
  if (row.follow_group) {
    sources.push('follow')
  } else {
    if (subscriptions.length > 0) {
      sources.push('subscription')
    }
    if (manual_nodes.length > 0) {
      sources.push('node')
    }
    if (group_aggregations.length > 0) {
      sources.push('aggregation')
    }
    if (include_groups.length > 0) {
      sources.push('strategy')
    }
  }
  enabledSources.value = sources

  // 修复 proxies_order：确保包含所有已选择的节点、聚合和策略组
  let proxies_order = row.proxies_order || []
  const existingIds = new Set(proxies_order.map((item: any) => `${item.type}:${item.id}`))

  // 添加缺失的节点
  manual_nodes.forEach(nodeId => {
    if (!existingIds.has(`node:${nodeId}`)) {
      proxies_order.push({ type: 'node', id: nodeId })
    }
  })

  // 添加缺失的聚合
  group_aggregations.forEach(aggId => {
    if (!existingIds.has(`aggregation:${aggId}`)) {
      proxies_order.push({ type: 'aggregation', id: aggId })
    }
  })

  // 添加缺失的策略组
  include_groups.forEach(groupId => {
    if (!existingIds.has(`strategy:${groupId}`)) {
      proxies_order.push({ type: 'strategy', id: groupId })
    }
  })

  // 清理 proxies_order 中已被删除的项
  proxies_order = proxies_order.filter((item: any) => {
    if (item.type === 'node') return manual_nodes.includes(item.id)
    if (item.type === 'aggregation') return group_aggregations.includes(item.id)
    if (item.type === 'strategy') return include_groups.includes(item.id)
    return false
  })

  // 构建表单数据，select 类型不需要 url 和 interval
  const formData: any = {
    id: row.id,
    name: row.name,
    type: row.type,
    enabled: row.enabled !== undefined ? row.enabled : true,
    subscriptions,
    regex: row.regex || '',
    aggregation_regex: row.aggregation_regex || '',
    manual_nodes,
    aggregations: group_aggregations,
    include_groups,
    proxies_order,
    follow_group: row.follow_group
  }

  // 只有需要测速的类型才添加 url 和 interval
  if (row.type !== 'select') {
    formData.url = row.url
    formData.interval = row.interval
  }

  // 负载均衡类型特有字段
  if (row.type === 'load-balance') {
    if (row.strategy) {
      formData.strategy = row.strategy
    }
    if (row.lazy !== undefined && row.lazy !== null) {
      formData.lazy = row.lazy
    } else {
      // 默认值为 true
      formData.lazy = true
    }
  }

  form.value = formData
  dialogVisible.value = true
}

const saveGroup = async () => {
  if (!form.value.name) {
    ElMessage.warning('请输入策略组名称')
    return
  }

  // 验证至少选择了一种来源
  const hasSubscriptions = form.value.subscriptions && form.value.subscriptions.length > 0
  const hasManualNodes = form.value.manual_nodes && form.value.manual_nodes.length > 0
  const hasAggregations = form.value.aggregations && form.value.aggregations.length > 0
  const hasIncludeGroups = form.value.include_groups && form.value.include_groups.length > 0
  const hasFollowGroup = form.value.follow_group !== undefined && form.value.follow_group !== null && form.value.follow_group !== ''

  if (!hasSubscriptions && !hasManualNodes && !hasAggregations && !hasIncludeGroups && !hasFollowGroup) {
    ElMessage.warning('请至少选择一种节点来源（订阅、节点、聚合、策略或跟随）')
    return
  }

  // 跟随模式验证
  if (hasFollowGroup && !form.value.follow_group) {
    ElMessage.warning('跟随模式下请选择要跟随的策略组')
    return
  }

  try {
    // 准备保存的数据
    const saveData = { ...form.value }

    // 根据 enabledSources 清理未选中来源的数据，避免产生脏数据
    if (!enabledSources.value.includes('subscription')) {
      saveData.subscriptions = []
      saveData.regex = ''
    }
    if (!enabledSources.value.includes('node')) {
      saveData.manual_nodes = []
    }
    if (!enabledSources.value.includes('aggregation')) {
      saveData.aggregations = []
      saveData.aggregation_regex = ''
    }
    if (!enabledSources.value.includes('strategy')) {
      saveData.include_groups = []
    }
    if (!enabledSources.value.includes('follow')) {
      delete saveData.follow_group
    }

    // 调试日志
    console.log('[策略组保存] 保存数据:', {
      name: saveData.name,
      enabledSources: enabledSources.value,
      subscriptions: saveData.subscriptions,
      aggregations: saveData.aggregations,
      manual_nodes: saveData.manual_nodes,
      include_groups: saveData.include_groups,
      proxies_order: saveData.proxies_order
    })

    // select 类型不需要 url 和 interval
    if (saveData.type === 'select') {
      delete saveData.url
      delete saveData.interval
    }

    // 如果负载策略和懒加载未选择，删除这些字段
    if (saveData.type === 'load-balance') {
      if (!saveData.strategy) {
        delete saveData.strategy
      }
      if (saveData.lazy === undefined || saveData.lazy === null) {
        delete saveData.lazy
      }
    }

    if (isEdit.value) {
      // 检查策略组名称是否发生变化
      const nameChanged = originalGroupName.value !== form.value.name

      if (nameChanged) {
        // 名称发生变化，需要同步更新所有引用该策略的规则配置
        try {
          // 获取所有规则配置
          const { data: allRules } = await api.get('/rules')

          // 找到所有引用该策略的规则（单条规则和规则集）
          const relatedRules = allRules.filter(
            (item: any) => item.policy === originalGroupName.value
          )

          if (relatedRules.length > 0) {
            // 批量更新规则的 policy 字段
            const updatePromises = relatedRules.map((rule: any) => {
              const updatedRule = { ...rule, policy: form.value.name }
              if (rule.itemType === 'rule') {
                return api.put(`/rules/${rule.id}`, updatedRule)
              } else if (rule.itemType === 'ruleset') {
                return api.put(`/rule-sets/${rule.id}`, updatedRule)
              }
              return Promise.resolve()
            })

            await Promise.all(updatePromises)
            console.log(`已同步更新 ${relatedRules.length} 个规则配置的策略引用`)
          }
        } catch (error) {
          console.error('同步更新规则配置失败:', error)
          ElMessage.warning('策略组名称已更新，但部分规则配置同步失败，请手动检查')
        }
      }

      // 使用 id 进行API调用
      await proxyGroupApi.update(saveData.id!, saveData)
      ElMessage.success('更新成功')
    } else {
      await proxyGroupApi.create(saveData)
      ElMessage.success('添加成功')
    }
    dialogVisible.value = false
    loadProxyGroups()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const deleteGroup = async (row: ProxyGroup) => {
  if (!row.id) {
    ElMessage.error('策略组缺少ID，无法删除')
    console.error('策略组数据异常，缺少ID字段:', row)
    return
  }

  try {
    // 先检查是否有规则配置引用了这个策略组
    const { data: allRules } = await api.get('/rules')
    const relatedRules = allRules.filter(
      (item: any) => item.policy === row.name
    )

    if (relatedRules.length > 0) {
      // 有关联的规则配置，询问用户是否一起删除
      try {
        await ElMessageBox.confirm(
          `该策略组被 ${relatedRules.length} 个规则配置引用，是否一起删除这些规则配置？`,
          '删除确认',
          {
            confirmButtonText: '一起删除',
            cancelButtonText: '仅删除策略组',
            distinguishCancelAndClose: true,
            type: 'warning'
          }
        )

        // 用户选择一起删除，先删除关联的规则配置
        for (const rule of relatedRules) {
          try {
            if (rule.itemType === 'rule') {
              await api.delete(`/rules/${rule.id}`)
            } else if (rule.itemType === 'ruleset') {
              await api.delete(`/rule-sets/${rule.id}`)
            }
          } catch (error) {
            console.error(`删除规则配置失败:`, error)
          }
        }

        // 然后删除策略组
        await proxyGroupApi.delete(row.id)
        ElMessage.success(`已删除策略组及 ${relatedRules.length} 个关联的规则配置`)
        loadProxyGroups()
      } catch (action) {
        if (action === 'cancel') {
          // 用户选择仅删除策略组
          await proxyGroupApi.delete(row.id)
          ElMessage.success('已删除策略组，关联的规则配置保留')
          loadProxyGroups()
        } else {
          // 用户点击了关闭按钮，取消删除
          return
        }
      }
    } else {
      // 没有关联的规则配置，直接删除
      await ElMessageBox.confirm(
        '确定要删除该策略组吗？',
        '删除确认',
        {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning'
        }
      )

      await proxyGroupApi.delete(row.id)
      ElMessage.success('删除成功')
      loadProxyGroups()
    }
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除失败')
      console.error('删除策略组失败:', error)
    }
  }
}

const initSortable = () => {
  nextTick(() => {
    if (groupsContainer.value) {
      Sortable.create(groupsContainer.value, {
        animation: 150,
        handle: '.card-drag-handle',
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        dragClass: 'sortable-drag',
        onEnd: async (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex) return

          // 更新本地数据顺序
          const movedItem = proxyGroups.value.splice(oldIndex, 1)[0]
          proxyGroups.value.splice(newIndex, 0, movedItem)

          // 保存新顺序到后端
          try {
            await saveProxyGroupsOrder()
            ElMessage.success('排序已更新')
          } catch (error) {
            ElMessage.error('保存排序失败')
            loadProxyGroups()
          }
        }
      })
    }
  })
}

const saveProxyGroupsOrder = async () => {
  await api.post('/proxy-groups/reorder', {
    groups: proxyGroups.value
  })
}

// 初始化已排序代理列表的拖拽功能
const initOrderedProxiesSortable = () => {
  nextTick(() => {
    if (orderedProxiesRef.value) {
      // 销毁已有实例
      if (orderedProxiesSortable) {
        orderedProxiesSortable.destroy()
      }

      orderedProxiesSortable = Sortable.create(orderedProxiesRef.value, {
        animation: 150,
        handle: '.drag-handle',
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        dragClass: 'sortable-drag',
        onEnd: (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex || oldIndex === undefined || newIndex === undefined) return

          // 更新 proxies_order 顺序
          const proxiesOrder = [...(form.value.proxies_order || [])]
          const movedItem = proxiesOrder.splice(oldIndex, 1)[0]
          proxiesOrder.splice(newIndex, 0, movedItem)
          form.value.proxies_order = proxiesOrder
        }
      })
    }
  })
}

// 监听对话框显示状态，初始化排序功能
watch(dialogVisible, (visible) => {
  if (visible) {
    // 对话框打开时，延迟初始化Sortable（等待DOM渲染）
    setTimeout(() => {
      initOrderedProxiesSortable()
    }, 100)
  } else {
    // 对话框关闭时，销毁Sortable实例
    if (orderedProxiesSortable) {
      orderedProxiesSortable.destroy()
      orderedProxiesSortable = null
    }
  }
})

// 监听排序列表变化，重新初始化Sortable
watch(orderedProxiesList, () => {
  if (dialogVisible.value && orderedProxiesList.value.length > 0) {
    initOrderedProxiesSortable()
  }
}, { deep: true })

// 监听折叠面板展开状态，展开时初始化Sortable
watch(activeCollapsePanel, (newVal) => {
  if (newVal.includes('sorting') && orderedProxiesList.value.length > 0) {
    // 延迟初始化，等待DOM渲染完成
    setTimeout(() => {
      initOrderedProxiesSortable()
    }, 50)
  }
})

onMounted(async () => {
  loadProxyGroups().then(() => {
    initSortable()
  })
  loadNodes()
  loadSubscriptions()

  loadAggregations()
})

onUnmounted(() => {
})
</script>


<style scoped>
.proxy-groups-page {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --group-radius-xl: 40px;
  --group-radius-lg: 24px;
  --group-radius-md: 16px;
  --group-radius-sm: 12px;
  --group-radius-pill: 999px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
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
  border-radius: var(--group-radius-md, 16px);
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

.groups-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  margin-top: 24px;
}

.group-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px 20px 18px;
  border-radius: var(--group-radius-lg, 24px);
  background: #fff;
  border: 1px solid rgba(107, 115, 255, 0.12);
  box-shadow: 0 20px 40px rgba(91, 112, 255, 0.16);
  transition: transform 0.25s ease, box-shadow 0.25s ease;
}

.group-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 24px 48px rgba(91, 112, 255, 0.2);
}

.group-card.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
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

.card-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.group-icon {
  font-size: 18px;
}

.group-name {
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
  border-radius: var(--group-radius-pill);
  font-size: 12px;
  font-weight: 500;
}

.follow-pill {
  background: rgba(107, 115, 255, 0.16);
  color: #4e5eff;
}

.type-pill {
  background: rgba(91, 112, 255, 0.12);
  color: #3f4ffa !important;
}

.type-pill.type-select {
  background: rgba(91, 112, 255, 0.12) !important;
  color: #3f4ffa !important;
}

.type-pill.type-url-test {
  background: rgba(139, 143, 255, 0.16) !important;
  color: #8b8fff;
}

.type-pill.type-fallback {
  background: rgba(107, 115, 255, 0.16) !important;
  color: #6b73ff !important;
}

.type-pill.type-load-balance {
  background: rgba(78, 94, 255, 0.18) !important;
  color: #4e5eff !important;
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
  background: linear-gradient(135deg, #8b8fff 0%, #6b7dff 100%);
  color: #fff;
}

.status-toggle-btn.loading {
  opacity: 0.6;
  cursor: progress;
}

.status-toggle-btn:disabled {
  cursor: not-allowed;
}


.groups-dialog :deep(.el-dialog),
:deep(.el-overlay-dialog .groups-dialog) {
  border-radius: var(--group-radius-xl, 40px) !important;
  overflow: hidden;
  background: rgba(252, 253, 255, 0.97);
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  backdrop-filter: blur(20px);
  --el-dialog-border-radius: var(--group-radius-xl, 40px);
}
.card-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
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

.card-section.compact {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  border-radius: var(--group-radius-md, 16px);
  background: rgba(107, 115, 255, 0.06);
  border: 1px solid rgba(107, 115, 255, 0.1);
  margin-bottom: 4px;
}

.summary-text {
  flex: 1;
  font-size: 13px;
  font-weight: 500;
  color: #48506c;
}

.expand-toggle-btn {
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: none;
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.25s ease;
}

.expand-toggle-btn:hover {
  background: rgba(107, 115, 255, 0.2);
  transform: scale(1.08);
}

.expand-toggle-btn .el-icon {
  font-size: 16px;
  transition: transform 0.25s ease;
}

.expand-toggle-btn .el-icon.rotated {
  transform: rotate(180deg);
}

.section-label {
  font-size: 13px;
  font-weight: 600;
  color: #7d88af;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.section-content {
  font-size: 13px;
  color: #48506c;
  line-height: 1.5;
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

.code-box {
  padding: 12px 14px;
  border-radius: var(--group-radius-md, 16px);
  background: #f4f6ff;
  color: #1f2d3d;
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  border: 1px solid rgba(107, 115, 255, 0.12);
}

.code-box.small {
  max-height: 160px;
  overflow: auto;
}

.empty-text {
  font-size: 12px;
  color: #a0a8c2;
}

.card-footer {
  display: flex;
  gap: 12px;
  margin-top: auto;
}

.card-btn.el-button {
  flex: 1;
  border-radius: var(--group-radius-md, 16px);
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
  border-radius: var(--group-radius-lg, 24px);
  padding: 30px 28px 26px;
  box-shadow: 0 18px 30px rgba(91, 112, 255, 0.12);
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.groups-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

:deep(.groups-form .el-form-item__label) {
  font-weight: 600;
  font-size: 13px;
  color: #6c74a0;
}

:deep(.groups-form .el-input__wrapper),
:deep(.groups-form .el-select .el-input__wrapper),
:deep(.groups-form .el-textarea__inner) {
  border-radius: var(--group-radius-md, 16px);
  border: none;
  box-shadow: 0 0 0 1px rgba(107, 115, 255, 0.14);
  background-color: #f9faff;
  transition: box-shadow 0.2s ease, transform 0.2s ease;
}

:deep(.groups-form .el-input__wrapper.is-focus),
:deep(.groups-form .el-select .el-input__wrapper.is-focus),
:deep(.groups-form .el-textarea__inner:focus) {
  box-shadow: 0 0 0 2px rgba(107, 115, 255, 0.32);
  transform: translateY(-1px);
  background-color: #fff;
}

.status-toggle-row {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border-radius: var(--group-radius-pill);
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  font-weight: 600;
}

.status-toggle-row span {
  font-size: 13px;
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
  border-radius: var(--group-radius-md, 16px);
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

.groups-helper-dialog :deep(.el-dialog) {
  border-radius: var(--group-radius-lg, 24px);
}

.dialog-alert {
  margin-bottom: 16px;
}

.empty-helper {
  text-align: center;
  padding: 40px 0;
  color: #909399;
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
  border-radius: var(--group-radius-md, 16px);
  background: rgba(107, 115, 255, 0.08);
  transition: background 0.2s ease;
}

.subscription-count-item.clickable {
  cursor: pointer;
}

.subscription-count-item.clickable:hover {
  background: rgba(107, 115, 255, 0.14);
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
  gap: 10px;
}

.node-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
  border-bottom: 1px solid #eef1f8;
  color: #4b5678;
}

.node-item:last-child {
  border-bottom: none;
}

@media (max-width: 1024px) {
  .proxy-groups-page {
    padding: 24px;
    --group-radius-xl: 32px;
    --group-radius-lg: 22px;
    --group-radius-md: 14px;
  }

  .groups-grid {
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  }
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .proxy-groups-page {
    padding: 20px;
    --group-radius-xl: 28px;
    --group-radius-lg: 20px;
    --group-radius-md: 14px;
  }

  .groups-grid {
    grid-template-columns: 1fr;
  }

  .card-actions {
    flex-direction: column;
  }

  .card-btn.el-button {
    width: 100%;
  }
}

@media (max-width: 480px) {
  .proxy-groups-page {
    padding: 16px;
    --group-radius-xl: 24px;
    --group-radius-lg: 18px;
    --group-radius-md: 12px;
  }

  .action-btn {
    width: 100%;
    justify-content: center;
  }
}
</style>
