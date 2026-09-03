<template>
  <div class="nodes-page" :class="{ 'cf-reordering': reorder.active.value }">
    <ScopeBanner scope="shared" />
    <PageHeader title="节点库" description="订阅拉取与手动录入的节点集中在此，供所有配置空间引用">
      <template #actions>
        <el-button v-if="!reorder.active.value" :disabled="nodes.length < 2" @click="reorder.enter">
          <el-icon><Sort /></el-icon>
          调整顺序
        </el-button>
        <el-button @click="showBatchAddDialog">
          <el-icon><DocumentAdd /></el-icon>
          批量添加
        </el-button>
        <el-button type="primary" @click="showAddDialog">
          <el-icon><Plus /></el-icon>
          添加节点
        </el-button>
        <el-button
          v-if="nodes.length > 0"
         
          @click="toggleSelectAll"
        >
          {{ isAllSelected ? '取消全选' : '全选' }}
        </el-button>
        <el-button
          v-if="selectedNodeIds.size > 0"
          type="danger" plain
          @click="batchDeleteNodes"
        >
          <el-icon><Delete /></el-icon>
          批量删除 ({{ selectedNodeIds.size }})
        </el-button>
      </template>
    </PageHeader>

    <div v-if="selectedNodeIds.size > 0" class="selection-tip">
      已选择 <strong>{{ selectedNodeIds.size }}</strong> 个节点
    </div>

    <ReorderBar
      :active="reorder.active.value"
      :saving="reorder.saving.value"
      :announcement="reorder.announcement.value"
      @cancel="reorder.cancel"
      @save="handleSaveOrder"
    />

    <div class="nodes-grid" ref="nodesContainer">
      <div
        v-for="(node, cfIndex) in nodes"
        :key="node.id || node.name"
        class="node-card"
        :class="{ 'node-selected': selectedNodeIds.has(node.id), disabled: !node.enabled }"
        :data-name="node.name"
        data-reorder-item
      >
        <div class="card-header">
          <div class="card-title-group">
            <DragHandle
              v-if="reorder.active.value"
              :label="node.name || node.id"
              :index="cfIndex"
              :total="nodes.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
            <el-checkbox
              :model-value="selectedNodeIds.has(node.id)"
              @change="toggleNodeSelection(node.id)"
              class="node-checkbox"
            />
            <div class="card-title">
              <span class="node-icon">🌐</span>
              <div class="node-name-group">
                <span class="node-name" :title="node.name">{{ node.name }}</span>
                <span v-if="node.remark" class="node-remark" :title="node.remark">{{ node.remark }}</span>
              </div>
            </div>
          </div>
          <div class="card-meta">
            <span class="meta-pill protocol-pill" :class="`protocol-${getProtocol(node.proxy_string).toLowerCase()}`">
              {{ getProtocol(node.proxy_string) }}
            </span>
            <span
              v-if="node.subscription_name"
              class="meta-pill source-pill"
            >
              {{ node.subscription_name }}
            </span>
            <button
              type="button"
              class="status-toggle-btn compact"
              :class="{ active: node.enabled, loading: savingStatus[node.id] }"
              @click="handleToggle(node)"
              :disabled="savingStatus[node.id]"
            >
              <el-icon><View /></el-icon>
            </button>
          </div>
        </div>

        <div class="card-section">
          <div class="section-label-row">
            <div class="section-label">
              <el-icon><Link /></el-icon>
              节点字符串
            </div>
            <button
              type="button"
              class="expand-toggle-btn"
              @click="toggleNodeExpand(node.id)"
            >
              <el-icon v-if="expandedNodes.has(node.id)"><ArrowUp /></el-icon>
              <el-icon v-else><ArrowDown /></el-icon>
            </button>
          </div>
          <pre v-show="expandedNodes.has(node.id)" class="code-box">{{ formatProxyStringForDisplay(node.proxy_string) }}</pre>
        </div>

        <div class="card-actions">
          <el-button class="card-btn ghost" size="small" @click="editNode(node)">
            <el-icon><EditPen /></el-icon>
            编辑
          </el-button>
          <el-button class="card-btn danger" size="small" @click="deleteNode(node)">
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </div>
      </div>
    </div>

    <el-dialog
      v-model="dialogVisible"
      class="node-dialog"
      width="720px"
      :close-on-click-modal="false"
      :destroy-on-close="true"
    >
      <template #header="{ close }">
        <div class="dialog-header">
          <div class="dialog-title-group">
            <h3>{{ isEdit ? '编辑节点' : '添加节点' }}</h3>
            <p>填写节点名称与连接字符串，支持 URI / JSON / YAML 格式</p>
          </div>
          <button class="dialog-close-btn" type="button" @click="close">
            <el-icon><Close /></el-icon>
          </button>
        </div>
      </template>
      <div class="dialog-card">
        <el-form :model="form" label-position="top" class="nodes-form">
          <el-form-item label="节点名称">
            <el-input v-model="form.name" placeholder="请输入节点名称，例如：香港节点 01" />
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="form.remark" placeholder="可选，添加备注信息" />
          </el-form-item>
          <el-form-item label="节点字符串">
            <el-input
              v-model="form.proxy_string"
              type="textarea"
              :rows="12"
              class="code-textarea"
              placeholder="支持 URI、JSON、YAML 等格式"
            />
          </el-form-item>
          <el-form-item label="启用状态">
            <div class="status-toggle-row">
              <el-switch v-model="form.enabled" />
              <span>{{ form.enabled ? '节点启用中' : '节点已停用' }}</span>
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="dialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveNode">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <el-dialog
      v-model="batchDialogVisible"
      class="node-dialog"
      width="780px"
      :close-on-click-modal="false"
      :destroy-on-close="true"
    >
      <template #header="{ close }">
        <div class="dialog-header">
          <div class="dialog-title-group">
            <h3>批量添加节点</h3>
            <p>粘贴多个节点链接或配置，系统会自动识别格式并导入</p>
          </div>
          <button class="dialog-close-btn" type="button" @click="close">
            <el-icon><Close /></el-icon>
          </button>
        </div>
      </template>
      <div class="dialog-card">
        <el-form :model="batchForm" label-position="top" class="nodes-form">
          <el-form-item label="节点链接或配置">
            <el-input
              v-model="batchForm.nodes_text"
              type="textarea"
              :rows="16"
              class="code-textarea large"
              placeholder="支持 URI、JSON、YAML 多种格式，自动忽略空行和 // 注释"
            />
          </el-form-item>
          <el-form-item label="默认状态">
            <div class="status-toggle-row">
              <el-switch v-model="batchForm.enabled" />
              <span>{{ batchForm.enabled ? '导入后默认启用节点' : '导入后默认禁用节点' }}</span>
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="batchDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveBatchNodes">批量添加</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import PageHeader from '@/components/shell/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import { ref, computed, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { DCaret, Plus, DocumentAdd, Delete, EditPen, Close, Link, View, ArrowUp, ArrowDown, Sort } from '@element-plus/icons-vue'
import { nodeApi, subStoreUrlApi } from '@/api'
import type { ProxyNode } from '@/types'
import api from '@/api'
import * as yaml from 'js-yaml'

const nodes = ref<ProxyNode[]>([])
const savingStatus = ref<Record<string, boolean>>({})
const nodesContainer = ref<HTMLElement | null>(null)
const dialogVisible = ref(false)
const isEdit = ref(false)
const form = ref<Partial<ProxyNode>>({
  name: '',
  proxy_string: '',
  enabled: true,
  remark: ''
})

// 节点字符串展开/收起状态
const expandedNodes = ref<Set<string>>(new Set())

// 批量添加相关
const batchDialogVisible = ref(false)
const batchForm = ref({
  nodes_text: '',
  enabled: true
})

// 批量删除相关
const selectedNodeIds = ref<Set<string>>(new Set())
const isAllSelected = computed(() => {
  return nodes.value.length > 0 && selectedNodeIds.value.size === nodes.value.length
})
const isSomeSelected = computed(() => {
  return selectedNodeIds.value.size > 0 && selectedNodeIds.value.size < nodes.value.length
})

// 从节点字符串中提取协议类型
const getProtocol = (proxyString: string) => {
  if (!proxyString) return 'Unknown'

  // 去除 YAML 列表标记
  let str = proxyString.trim()
  if (str.startsWith('- ')) {
    str = str.substring(2).trim()
  }

  // 检查是否是单行 JSON 对象格式
  if (str.startsWith('{') && str.endsWith('}') && !str.includes('\n')) {
    try {
      // 尝试解析为 JSON
      const obj = JSON.parse(str)
      if (obj && obj.type) {
        return obj.type.toUpperCase()
      }
    } catch {
      // JSON 解析失败，尝试正则提取
      try {
        const typeMatch = str.match(/"type":\s*"([a-z0-9]+)"|type:\s*([a-z0-9]+)/i)
        if (typeMatch) {
          return (typeMatch[1] || typeMatch[2]).toUpperCase()
        }
      } catch {
        // 忽略解析错误
      }
    }
  }

  // 检查是否是多行 YAML/JSON 格式
  if (str.includes('\n') || (str.startsWith('{') && str.endsWith('}'))) {
    try {
      // 尝试解析为 JSON（多行）
      const obj = JSON.parse(str)
      if (obj && obj.type) {
        return obj.type.toUpperCase()
      }
    } catch {
      // JSON 解析失败，尝试正则提取 type 字段
      const typeMatch = str.match(/["\']?type["\']?\s*:\s*["\']?([a-z0-9]+)["\']?/i)
      if (typeMatch) {
        return typeMatch[1].toUpperCase()
      }
    }
  }

  // 检查 URI 格式
  const match = str.match(/^([a-z0-9]+):\/\//)
  if (match) {
    return match[1].toUpperCase()
  }
  return 'Unknown'
}

// 获取协议标签颜色
const getProtocolTagType = (proxyString: string) => {
  const protocol = getProtocol(proxyString).toLowerCase()
  const types: Record<string, string> = {
    'ss': 'primary',
    'vmess': 'success',
    'vless': 'success',
    'trojan': 'warning',
    'hysteria2': 'danger',
    'wireguard': 'info',
    'http': '',
    'https': 'info'
  }
  return types[protocol] || ''
}

// 格式化节点字符串用于显示（多行格式化）
const formatProxyStringForDisplay = (str: string) => {
  if (!str) return ''

  const trimmed = str.trim()

  // 去除 YAML 列表标记
  let content = trimmed
  if (content.startsWith('- ')) {
    content = content.substring(2).trim()
  }

  // 检查是否是单行 JSON 对象格式
  if (content.startsWith('{') && content.endsWith('}') && !content.includes('\n')) {
    try {
      // 尝试解析为 JSON
      const obj = JSON.parse(content)
      if (obj && typeof obj === 'object') {
        // 返回格式化的 JSON（带缩进）
        return JSON.stringify(obj, null, 2)
      }
    } catch {
      // JSON 解析失败，返回原字符串
    }
  }

  // 如果已经是格式化的多行内容（YAML 或 JSON），直接返回
  if (content.includes('\n')) {
    return content
  }

  return str
}

// 截断字符串
const truncateString = (str: string, maxLength: number) => {
  if (!str) return ''
  if (str.length <= maxLength) return str
  return str.substring(0, maxLength) + '...'
}

const loadNodes = async () => {
  try {
    const { data } = await nodeApi.getAll()
    nodes.value = data
  } catch (error) {
    ElMessage.error('加载节点列表失败')
  }
}

const checkSubStoreUrl = async (): Promise<boolean> => {
  try {
    const response = await subStoreUrlApi.get()
    const url = response.data?.sub_store_url || ''
    if (!url) {
      await ElMessageBox.confirm(
        '尚未配置 Sub-Store URL，节点格式转换功能将不可用。请前往「生成配置」页面配置 Sub-Store 地址。',
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
    name: '',
    proxy_string: '',
    enabled: true,
    remark: ''
  }
  dialogVisible.value = true
}

const showBatchAddDialog = async () => {
  if (!await checkSubStoreUrl()) return
  batchForm.value = {
    nodes_text: '',
    enabled: true
  }
  batchDialogVisible.value = true
}

// 切换节点选中状态
const toggleNodeSelection = (nodeId: string) => {
  if (selectedNodeIds.value.has(nodeId)) {
    selectedNodeIds.value.delete(nodeId)
  } else {
    selectedNodeIds.value.add(nodeId)
  }
  // 触发响应式更新
  selectedNodeIds.value = new Set(selectedNodeIds.value)
}

// 切换节点字符串展开/收起
const toggleNodeExpand = (nodeId: string) => {
  if (expandedNodes.value.has(nodeId)) {
    expandedNodes.value.delete(nodeId)
  } else {
    expandedNodes.value.add(nodeId)
  }
  // 触发响应式更新
  expandedNodes.value = new Set(expandedNodes.value)
}

// 全选/取消全选
const toggleSelectAll = () => {
  if (isAllSelected.value) {
    selectedNodeIds.value.clear()
  } else {
    selectedNodeIds.value = new Set(nodes.value.map(n => n.id))
  }
}

// 批量删除
const batchDeleteNodes = async () => {
  if (selectedNodeIds.value.size === 0) {
    ElMessage.warning('请先选择要删除的节点')
    return
  }

  try {
    // 确认删除
    await ElMessageBox.confirm(
      `确定要删除选中的 ${selectedNodeIds.value.size} 个节点吗？删除后将同步清理策略组中对这些节点的引用。`,
      '批量删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const nodeIdsToDelete = Array.from(selectedNodeIds.value)
    let successCount = 0
    let failCount = 0

    // 批量删除节点
    for (const nodeId of nodeIdsToDelete) {
      try {
        await nodeApi.delete(nodeId)
        successCount++
      } catch (error) {
        failCount++
        console.error(`删除节点 ${nodeId} 失败:`, error)
      }
    }

    // 获取所有策略组，清理引用
    try {
      const { data: proxyGroups } = await api.get('/proxy-groups')
      let updatedGroupCount = 0

      for (const group of proxyGroups) {
        let groupModified = false

        if (group.manual_nodes && group.manual_nodes.length > 0) {
          const originalLength = group.manual_nodes.length
          group.manual_nodes = group.manual_nodes.filter(
            (id: string) => !nodeIdsToDelete.includes(id)
          )
          if (group.manual_nodes.length !== originalLength) {
            groupModified = true
          }
        }

        if (group.proxies_order && group.proxies_order.length > 0) {
          const originalLength = group.proxies_order.length
          group.proxies_order = group.proxies_order.filter(
            (item: any) => !(item.type === 'node' && nodeIdsToDelete.includes(item.id))
          )
          if (group.proxies_order.length !== originalLength) {
            groupModified = true
          }
        }

        if (groupModified) {
          try {
            await api.put(`/proxy-groups/${group.id}`, group)
            updatedGroupCount++
          } catch (error) {
            console.error(`更新策略组 ${group.name} 失败:`, error)
          }
        }
      }

      // 显示结果
      if (failCount === 0) {
        if (updatedGroupCount > 0) {
          ElMessage.success(`批量删除成功！已删除 ${successCount} 个节点，清理了 ${updatedGroupCount} 个策略组中的引用`)
        } else {
          ElMessage.success(`批量删除成功！已删除 ${successCount} 个节点`)
        }
      } else {
        ElMessage.warning(`批量删除完成！成功 ${successCount} 个，失败 ${failCount} 个`)
      }
    } catch (error) {
      console.error('清理策略组引用失败:', error)
      ElMessage.warning(`已删除 ${successCount} 个节点，但清理策略组引用时出现错误`)
    }

    // 清空选择并刷新列表
    selectedNodeIds.value.clear()
    loadNodes()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('批量删除失败')
      console.error('批量删除节点失败:', error)
    }
  }
}

const handleToggle = (node: ProxyNode) => {
  node.enabled = !node.enabled
  toggleNodeEnabled(node)
}

const toggleNodeEnabled = async (node: ProxyNode) => {
  const previous = !node.enabled
  savingStatus.value[node.id] = true
  try {
    await nodeApi.update(node.id, node)
    ElMessage.success(node.enabled ? '已启用' : '已禁用')
  } catch (error) {
    ElMessage.error('更新状态失败')
    node.enabled = previous
    loadNodes()
  } finally {
    savingStatus.value[node.id] = false
  }
}

const editNode = (row: ProxyNode) => {
  isEdit.value = true
  form.value = { ...row }

  // 自动格式化对象格式的节点字符串
  if (form.value.proxy_string) {
    form.value.proxy_string = formatProxyString(form.value.proxy_string)
  }

  dialogVisible.value = true
}

// 格式化节点字符串
const formatProxyString = (str: string) => {
  if (!str) return str

  const trimmed = str.trim()

  // 去除 YAML 列表标记
  let content = trimmed
  if (content.startsWith('- ')) {
    content = content.substring(2).trim()
  }

  // 如果是多行 YAML 格式（不以 { 开头），直接返回
  if (content.includes('\n') && !content.startsWith('{')) {
    return content
  }

  // 如果已经是格式化的 JSON，直接返回
  if (content.startsWith('{') && content.includes('\n')) {
    try {
      // 验证是否是有效的 JSON
      JSON.parse(content)
      return content
    } catch {
      // 不是有效的 JSON，继续处理
    }
  }

  // 检查是否是单行 JSON 对象格式
  if (content.startsWith('{') && content.endsWith('}') && !content.includes('\n')) {
    try {
      // 尝试直接解析为 JSON
      const obj = JSON.parse(content)
      return JSON.stringify(obj, null, 2)
    } catch {
      // JSON 解析失败，尝试作为 YAML 对象解析
      try {
        // 移除首尾的大括号
        let inner = content.substring(1, content.length - 1).trim()

        // 分割键值对
        const pairs: string[] = []
        let currentPair = ''
        let depth = 0

        for (let i = 0; i < inner.length; i++) {
          const char = inner[i]

          if (char === '{' || char === '[') {
            depth++
          } else if (char === '}' || char === ']') {
            depth--
          } else if (char === ',' && depth === 0) {
            pairs.push(currentPair.trim())
            currentPair = ''
            continue
          }

          currentPair += char
        }

        if (currentPair.trim()) {
          pairs.push(currentPair.trim())
        }

        // 构建 JSON 对象
        const obj: Record<string, any> = {}

        for (const pair of pairs) {
          const colonIndex = pair.indexOf(':')
          if (colonIndex > 0) {
            let key = pair.substring(0, colonIndex).trim()
            let value = pair.substring(colonIndex + 1).trim()

            // 移除键周围的引号（如果有）
            if ((key.startsWith('"') && key.endsWith('"')) ||
                (key.startsWith("'") && key.endsWith("'"))) {
              key = key.substring(1, key.length - 1)
            }

            // 移除值周围的引号（如果有）
            if ((value.startsWith('"') && value.endsWith('"')) ||
                (value.startsWith("'") && value.endsWith("'"))) {
              value = value.substring(1, value.length - 1)
            }

            // 尝试解析值
            if (value === 'true') {
              obj[key] = true
            } else if (value === 'false') {
              obj[key] = false
            } else if (!isNaN(Number(value)) && value !== '' && !/^0\d+/.test(value)) {
              obj[key] = Number(value)
            } else {
              obj[key] = value
            }
          }
        }

        // 转换为格式化的 JSON
        return JSON.stringify(obj, null, 2)
      } catch (e) {
        // 解析失败，返回原字符串
        console.warn('Failed to format proxy string:', e)
      }
    }
  }

  return str
}

const saveNode = async () => {
  try {
    // 在保存前格式化节点字符串
    if (form.value.proxy_string) {
      form.value.proxy_string = formatProxyString(form.value.proxy_string)
    }

    if (isEdit.value) {
      await nodeApi.update(form.value.id!, form.value)
      ElMessage.success('更新成功')
    } else {
      await nodeApi.create(form.value)
      ElMessage.success('添加成功')
    }
    dialogVisible.value = false
    loadNodes()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

// 从节点链接中提取名称（通常在#后面）
const extractNodeName = (proxyString: string): string | undefined => {
  try {
    const trimmed = proxyString.trim()

    // 检查是否包含 #
    const hashIndex = trimmed.indexOf('#')
    if (hashIndex === -1) {
      return undefined
    }

    // 提取 # 后面的部分
    let name = trimmed.substring(hashIndex + 1).trim()

    // URL 解码（节点名称可能是编码的）
    try {
      name = decodeURIComponent(name)
    } catch {
      // 解码失败，使用原始名称
    }

    // 如果名称为空或只包含空格，返回 undefined
    if (!name || name.length === 0) {
      return undefined
    }

    return name
  } catch {
    return undefined
  }
}

const saveBatchNodes = async () => {
  console.log('[批量添加] 版本: v2.0 - 支持 YAML 格式')
  try {
    const text = batchForm.value.nodes_text.trim()
    if (!text) {
      ElMessage.warning('请输入节点链接')
      return
    }

    console.log('[批量添加] 输入文本长度:', text.length, '字符')
    console.log('[批量添加] 输入文本前100字符:', text.substring(0, 100))

    let successCount = 0
    let failCount = 0
    const errors: string[] = []
    let autoNameCounter = 1 // 自动命名计数器

    // 检查是否是完整的 YAML 格式（包含 proxies: 或以 - name:/- type: 开头的列表）
    const hasProxiesKey = text.includes('proxies:')
    const hasYamlList = /^[\s]*-[\s]+(name|type):/m.test(text)
    const isYamlFormat = hasProxiesKey || hasYamlList

    console.log('[批量添加] 格式检测 - proxies:', hasProxiesKey, ', YAML列表:', hasYamlList, ', 判定为YAML:', isYamlFormat)

    if (isYamlFormat) {
      console.log('[批量添加] 检测到 YAML 格式，开始解析')
      try {
        // 尝试解析为 YAML
        const parsed = yaml.load(text)
        console.log('[批量添加] YAML 解析结果:', parsed)
        let proxies: any[] = []

        // 如果包含 proxies 字段，提取 proxies 数组
        if (parsed && typeof parsed === 'object' && 'proxies' in parsed) {
          proxies = Array.isArray(parsed.proxies) ? parsed.proxies : []
          console.log('[批量添加] 从 proxies 字段提取到', proxies.length, '个节点')
        }
        // 如果直接是数组（以 - 开头的 YAML 列表）
        else if (Array.isArray(parsed)) {
          proxies = parsed
          console.log('[批量添加] 直接解析为数组，包含', proxies.length, '个节点')
        }

        if (proxies.length === 0) {
          ElMessage.warning('未找到有效的节点定义')
          console.warn('[批量添加] 未找到有效的节点定义')
          return
        }

        // 批量创建节点
        for (let i = 0; i < proxies.length; i++) {
          const proxy = proxies[i]
          try {
            // 将节点对象转换为 YAML 字符串
            const proxyYaml = yaml.dump(proxy, { indent: 2, lineWidth: -1 })

            // 使用节点中的 name 字段作为名称
            const nodeName = proxy.name || `节点_${autoNameCounter++}`

            // 创建节点
            const nodeData: any = {
              name: nodeName,
              proxy_string: proxyYaml.trim(),
              enabled: batchForm.value.enabled
            }

            await nodeApi.create(nodeData)
            successCount++
          } catch (error: any) {
            failCount++
            const errorMsg = error?.response?.data?.detail || error?.message || '未知错误'
            errors.push(`节点 ${i + 1} (${proxy.name || 'unnamed'}): ${errorMsg}`)
          }
        }
      } catch (yamlError: any) {
        ElMessage.error(`YAML 解析失败: ${yamlError.message}`)
        return
      }
    } else {
      // 原有的按行处理逻辑（用于 URI 格式和单行 JSON）
      console.log('[批量添加] 使用按行处理模式')
      const lines = text.split('\n')
        .map(line => line.trim())
        .filter(line => line && !line.startsWith('//'))

      console.log('[批量添加] 过滤后的行数:', lines.length)

      if (lines.length === 0) {
        ElMessage.warning('没有有效的节点链接')
        return
      }

      // 批量处理每一行
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        try {
          // 格式化节点字符串
          const formattedProxyString = formatProxyString(line)

          // 提取节点名称
          let nodeName = extractNodeName(line)

          // 如果提取不到名称，自动生成一个
          if (!nodeName) {
            nodeName = `节点_${autoNameCounter}`
            autoNameCounter++
          }

          // 创建节点
          const nodeData: any = {
            name: nodeName,
            proxy_string: formattedProxyString,
            enabled: batchForm.value.enabled
          }

          await nodeApi.create(nodeData)
          successCount++
        } catch (error: any) {
          failCount++
          const errorMsg = error?.response?.data?.detail || error?.message || '未知错误'
          errors.push(`第 ${i + 1} 行: ${errorMsg}`)
        }
      }
    }

    // 显示结果摘要
    if (failCount === 0) {
      ElMessage.success(`批量添加完成！成功添加 ${successCount} 个节点`)
    } else if (successCount === 0) {
      ElMessage.error(`批量添加失败！所有 ${failCount} 个节点都添加失败`)
      if (errors.length > 0) {
        console.error('批量添加错误详情:', errors)
      }
    } else {
      ElMessage.warning(`批量添加完成！成功 ${successCount} 个，失败 ${failCount} 个`)
      if (errors.length > 0 && errors.length <= 5) {
        // 如果错误不多，显示错误详情
        setTimeout(() => {
          errors.forEach(err => ElMessage.error(err))
        }, 500)
      } else if (errors.length > 5) {
        console.error('批量添加错误详情:', errors)
        ElMessage.info('查看控制台了解详细错误信息')
      }
    }

    // 如果有成功的，关闭对话框并刷新列表
    if (successCount > 0) {
      batchDialogVisible.value = false
      loadNodes()
    }
  } catch (error) {
    ElMessage.error('批量添加失败')
    console.error('批量添加错误:', error)
  }
}

const deleteNode = async (row: ProxyNode) => {
  try {
    // 确认删除
    await ElMessageBox.confirm(
      '确定要删除该节点吗？删除后将同步清理策略组中对该节点的引用。',
      '删除确认',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    // 先删除节点
    await nodeApi.delete(row.id)

    // 获取所有策略组，清理引用
    const { data: proxyGroups } = await api.get('/proxy-groups')
    let updatedCount = 0

    // 查找引用了该节点的策略组
    for (const group of proxyGroups) {
      if (group.manual_nodes && group.manual_nodes.includes(row.id)) {
        // 从节点列表中移除该节点ID
        group.manual_nodes = group.manual_nodes.filter((id: string) => id !== row.id)

        // 同步更新 proxies_order
        if (group.proxies_order && group.proxies_order.length > 0) {
          group.proxies_order = group.proxies_order.filter(
            (item: any) => !(item.type === 'node' && item.id === row.id)
          )
        }

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
    loadNodes()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除失败')
      console.error('删除节点失败:', error)
    }
  }
}



/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: nodes,
  container: nodesContainer,
  labelOf: item => item.name || item.id,
  // 按 id 提交，服务端在存量数据上重排，不回写列表接口的计算字段
  persist: async items => {
    await api.post('/nodes/reorder', {
      ids: items.map(item => item.id),
      position: 'top'
    })
  }
})

const handleSaveOrder = async () => {
  try {
    await reorder.save()
    ElMessage.success('顺序已保存，所有配置空间生效')
  } catch (error) {
    ElMessage.error('保存顺序失败，顺序已还原')
  }
}

onMounted(() => {
  loadNodes().then(() => {
  })
})
</script>

<style scoped>
.nodes-page {
  --node-radius-xl: 40px;
  --node-radius-lg: 24px;
  --node-radius-md: 16px;
  --node-radius-sm: 12px;
  --node-radius-pill: 999px;
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
  background: var(--cf-bg);
  margin: -28px -32px 28px -32px;
  padding: 28px 32px;
}

.title-block h2 {
  margin: 0;
  font-size: 26px;
  font-weight: 700;
  color: var(--cf-fg);
  color: transparent;
}

.title-block p {
  margin: 6px 0 0;
  font-size: 14px;
  color: var(--cf-fg-2);
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: flex-end;
}

.selection-tip {
  margin-top: 16px;
  margin-bottom: -4px;
  padding: 10px 16px;
  border-radius: var(--node-radius-md, 16px);
  background: rgba(107, 115, 255, 0.12);
  color: var(--cf-primary);
  font-size: 13px;
  font-weight: 600;
  display: inline-flex;
  gap: 6px;
  align-items: center;
}

.selection-tip strong {
  font-weight: 700;
}

.nodes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  margin-top: 24px;
}

.node-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 28px 26px 24px;
  border-radius: var(--node-radius-lg, 24px);
  background: var(--cf-s1);
  border: 1px solid rgba(107, 115, 255, 0.12);
  box-shadow: 0 20px 40px rgba(91, 112, 255, 0.16);
  transition: transform 0.25s ease, box-shadow 0.25s ease, border-color 0.25s ease;
  min-height: 100%;
}

.node-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 24px 48px rgba(91, 112, 255, 0.2);
}

.node-card.node-selected {
  border-color: rgba(78, 95, 255, 0.6);
  box-shadow: 0 20px 50px rgba(78, 95, 255, 0.25);
}

.node-card.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
}

.card-drag-handle {
  position: relative;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(107, 115, 255, 0.14);
  color: var(--cf-primary);
  cursor: grab;
  transition: background 0.2s ease, color 0.2s ease;
  z-index: 2;
}

.card-drag-handle:hover {
  background: rgba(107, 115, 255, 0.22);
  color: var(--cf-primary);
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
  min-width: 0;
  flex: 1;
  overflow: hidden;
}

.node-checkbox {
  display: inline-flex;
  align-items: center;
}

:deep(.node-checkbox .el-checkbox__inner) {
  border-radius: 6px;
  width: 18px;
  height: 18px;
}

:deep(.node-checkbox .el-checkbox__inner::after) {
  left: 5px;
  top: 1px;
}

:deep(.node-checkbox .el-checkbox__input.is-checked .el-checkbox__inner) {
  background: var(--cf-s2);
  border: none;
  box-shadow: 0 6px 16px rgba(87, 104, 255, 0.35);
}

:deep(.node-checkbox .el-checkbox__input.is-checked .el-checkbox__inner::after) {
  border-color: var(--cf-s1);
  left: 4px;
}

:deep(.node-checkbox .el-checkbox__inner:hover) {
  border-color: rgba(107, 115, 255, 0.6);
}

.card-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
  overflow: hidden;
}

.node-icon {
  font-size: 18px;
  flex-shrink: 0;
}

.node-name-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  flex: 1;
  overflow: hidden;
}

.node-name {
  font-size: 17px;
  font-weight: 600;
  color: var(--cf-fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-remark {
  font-size: 12px;
  color: var(--cf-fg-2);
  font-weight: 400;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
  flex-shrink: 0;
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: var(--node-radius-pill);
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
  color: var(--cf-primary);
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
  background: var(--cf-primary);
  color: var(--cf-primary-fg);
}

.status-toggle-btn.loading {
  opacity: 0.6;
  cursor: progress;
}

.status-toggle-btn:disabled {
  cursor: not-allowed;
}

.protocol-pill {
  background: rgba(107, 115, 255, 0.16);
  color: var(--cf-primary) !important;
}

.protocol-pill.protocol-ss {
  background: rgba(107, 115, 255, 0.16);
  color: var(--cf-primary);
}

.protocol-pill.protocol-vmess,
.protocol-pill.protocol-vless {
  background: rgba(139, 143, 255, 0.16);
  color: var(--cf-primary-hover);
}

.protocol-pill.protocol-trojan {
  background: rgba(107, 115, 255, 0.16);
  color: var(--cf-primary);
}

.protocol-pill.protocol-hysteria2 {
  background: rgba(78, 94, 255, 0.18);
  color: var(--cf-primary);
}

.protocol-pill.protocol-wireguard {
  background: rgba(107, 115, 255, 0.18);
  color: var(--cf-primary);
}

.protocol-pill.protocol-http,
.protocol-pill.protocol-https {
  background: rgba(130, 143, 178, 0.18);
  color: var(--cf-fg);
}

.protocol-pill.protocol-unknown {
  background: rgba(162, 170, 206, 0.16);
  color: var(--cf-fg-2);
}

.source-pill {
  background: rgba(255, 255, 255, 0.7);
  color: var(--cf-fg);
  border: 1px dashed rgba(107, 115, 255, 0.25);
}

.status-pill {
  background: rgba(139, 143, 255, 0.16);
  color: var(--cf-primary-hover);
}

.status-pill.disabled {
  background: rgba(162, 170, 206, 0.16);
  color: var(--cf-fg-2);
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
  color: var(--cf-fg-2);
}

.section-label .el-icon {
  font-size: 16px;
  color: var(--cf-primary);
}

.expand-toggle-btn {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid rgba(107, 115, 255, 0.25);
  background: rgba(107, 115, 255, 0.08);
  color: var(--cf-primary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
  flex-shrink: 0;
}

.expand-toggle-btn:hover {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.35);
  transform: scale(1.05);
}

 .section-value {
  font-size: 14px;
  font-weight: 600;
  color: var(--cf-fg);
}

.code-box {
  padding: 14px 16px;
  border-radius: var(--node-radius-md, 16px);
  background: var(--cf-s2);
  color: var(--cf-fg);
  font-size: 13px;
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 220px;
  overflow: auto;
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.card-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: auto;
}

.card-btn.el-button {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 40px;
  border-radius: var(--node-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  padding: 0 16px;
  border: none;
}

.card-btn.ghost {
  background: rgba(107, 115, 255, 0.12);
  color: var(--cf-primary);
  border: 1px solid rgba(107, 115, 255, 0.25);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  color: var(--cf-primary-hover);
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
  box-shadow: 0 20px 50px rgba(91, 112, 255, 0.2);
}

.sortable-drag {
  cursor: grabbing !important;
}

.node-card::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  box-shadow: inset 0 0 0 1px rgba(107, 115, 255, 0.08);
}

.node-card.node-selected::before {
  box-shadow: inset 0 0 0 2px rgba(78, 95, 255, 0.35);
}

.nodes-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.status-toggle-row {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border-radius: var(--node-radius-pill);
  background: rgba(107, 115, 255, 0.12);
  color: var(--cf-primary);
  font-weight: 600;
}

.status-toggle-row span {
  font-size: 13px;
}

:deep(.node-dialog),
:deep(.el-overlay-dialog .node-dialog) {
  border-radius: var(--node-radius-xl, 40px) !important;
  overflow: hidden;
  background: rgba(252, 253, 255, 0.97);
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  backdrop-filter: blur(20px);
  --el-dialog-border-radius: var(--node-radius-xl, 40px);
}

:deep(.node-dialog .el-dialog__header) {
  padding: 20px 32px 0;
  margin: 0;
  border-bottom: none;
}

:deep(.node-dialog .el-dialog__body) {
  padding: 0 32px 28px;
  background: var(--cf-s2);
}

:deep(.node-dialog .el-dialog__footer) {
  padding: 0 32px 28px;
  border-top: none;
}

.dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  padding: 8px 0 18px;
  color: var(--cf-fg);
}

.dialog-title-group h3 {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: var(--cf-fg);
  }

.dialog-title-group p {
  margin: 8px 0 0;
  font-size: 13px;
  color: var(--cf-fg-2);
}

.dialog-close-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 1px solid rgba(124, 134, 174, 0.35);
  background: transparent;
  color: var(--cf-fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.dialog-close-btn:hover {
  background: rgba(107, 115, 255, 0.12);
  border-color: rgba(107, 115, 255, 0.35);
  color: var(--cf-primary);
}

.dialog-card {
  background: var(--cf-s1);
  border-radius: var(--node-radius-lg, 24px);
  padding: 30px 28px 26px;
  box-shadow: 0 18px 30px rgba(91, 112, 255, 0.12);
  border: 1px solid rgba(107, 115, 255, 0.1);
}

:deep(.node-dialog .el-form-item__label) {
  font-weight: 600;
  font-size: 13px;
  color: var(--cf-fg-2);
}

:deep(.node-dialog .el-input__wrapper),
:deep(.node-dialog .el-select .el-input__wrapper),
:deep(.node-dialog .el-textarea__inner),
:deep(.node-dialog .el-input-number .el-input__wrapper) {
  border-radius: var(--node-radius-md, 16px);
  border: none;
  box-shadow: 0 0 0 1px rgba(107, 115, 255, 0.14);
  transition: box-shadow 0.2s ease, transform 0.2s ease;
  background-color: var(--cf-s2);
}

:deep(.node-dialog .el-input__wrapper.is-focus),
:deep(.node-dialog .el-select .el-input__wrapper.is-focus),
:deep(.node-dialog .el-input-number.is-active .el-input__wrapper),
:deep(.node-dialog .el-input-number:hover .el-input__wrapper),
:deep(.node-dialog .el-textarea__inner:focus) {
  box-shadow: 0 0 0 2px rgba(107, 115, 255, 0.32);
  transform: translateY(-1px);
  background-color: var(--cf-s1);
}

.code-textarea :deep(.el-textarea__inner) {
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  min-height: 200px;
}

.code-textarea.large :deep(.el-textarea__inner) {
  min-height: 320px;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.footer-btn {
  min-width: 118px;
  height: 42px;
  border-radius: var(--node-radius-md, 16px);
  font-weight: 600;
  font-size: 14px;
}

.footer-btn.ghost {
  background: transparent;
  color: var(--cf-primary);
  border: 1px solid rgba(107, 115, 255, 0.3);
}

.footer-btn.ghost:hover {
  background: rgba(107, 115, 255, 0.08);
}

.footer-btn.primary {
  background: var(--cf-primary);
  border: none;
  color: var(--cf-primary-fg);
  box-shadow: 0 12px 24px rgba(87, 104, 255, 0.28);
}

.footer-btn.primary:hover {
  transform: translateY(-1px);
}

.nodes-preview-dialog :deep(.el-dialog) {
  border-radius: var(--node-radius-lg, 24px);
  overflow: hidden;
}

.nodes-preview-dialog .preview-header {
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--cf-s3);
}

.nodes-preview-dialog .preview-count {
  font-size: 14px;
  color: var(--cf-fg-2);
  font-weight: 500;
}

.nodes-preview-dialog .nodes-list {
  margin: 0 -20px;
}

.nodes-preview-dialog .node-item {
  padding: 12px 20px;
  border-bottom: 1px solid var(--cf-s3);
  transition: background 0.2s ease;
}

.nodes-preview-dialog .node-item:hover {
  background: var(--cf-s2);
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
  color: var(--cf-fg);
}

.nodes-preview-dialog .node-name .el-icon {
  color: var(--cf-primary);
  font-size: 16px;
}

.nodes-preview-dialog .node-details {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: var(--cf-fg-2);
}

.nodes-preview-dialog .node-server {
  font-family: 'SFMono-Regular', 'Consolas', 'Monaco', monospace;
  color: var(--cf-fg-2);
}

@media (max-width: 1024px) {
  .nodes-page {
    padding: 24px;
    --node-radius-xl: 32px;
    --node-radius-lg: 22px;
    --node-radius-md: 14px;
  }

  .nodes-grid {
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  }

  :deep(.node-dialog .el-dialog__body),
  :deep(.node-dialog .el-dialog__footer) {
    padding: 0 24px 24px;
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

  .nodes-page {
    padding: 20px;
    --node-radius-xl: 28px;
    --node-radius-lg: 20px;
    --node-radius-md: 14px;
  }

  .nodes-grid {
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

  :deep(.card-btn.el-button) {
    width: 100%;
    flex: unset;
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

  :deep(.node-dialog .el-dialog__body),
  :deep(.node-dialog .el-dialog__footer) {
    padding: 0 24px 24px;
  }

  :deep(.node-dialog),
  :deep(.el-overlay-dialog .node-dialog) {
    border-radius: var(--node-radius-xl, 28px) !important;
    --el-dialog-border-radius: var(--node-radius-xl, 28px);
  }
}

@media (max-width: 480px) {
  .nodes-page {
    padding: 16px;
    --node-radius-xl: 24px;
    --node-radius-lg: 18px;
    --node-radius-md: 12px;
  }

  .card-title {
    gap: 8px;
  }

  .action-btn {
    width: 100%;
    justify-content: center;
  }

  :deep(.node-dialog .el-dialog__body),
  :deep(.node-dialog .el-dialog__footer) {
    padding: 0 20px 20px;
  }

  :deep(.node-dialog),
  :deep(.el-overlay-dialog .node-dialog) {
    border-radius: var(--node-radius-xl, 24px) !important;
    --el-dialog-border-radius: var(--node-radius-xl, 24px);
  }

  .dialog-card {
    padding: 24px 18px 20px;
  }
}
</style>
