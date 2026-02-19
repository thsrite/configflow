<template>
  <div class="rule-library-page">
    <div class="page-header">
      <div class="title-block">
        <h2>规则仓库</h2>
        <p>管理您的规则集和配置</p>
      </div>
      <div class="header-actions">
        <el-button-group class="view-toggle">
          <el-button
            :class="['toggle-btn', { active: viewMode === 'list' }]"
            @click="viewMode = 'list'"
            title="列表视图"
          >
            <el-icon><List /></el-icon>
          </el-button>
          <el-button
            :class="['toggle-btn', { active: viewMode === 'grid' }]"
            @click="viewMode = 'grid'"
            title="卡片视图"
          >
            <el-icon><Grid /></el-icon>
          </el-button>
        </el-button-group>
        <el-button
          class="action-btn action-secondary"
          @click="showProxyConfigDialog"
        >
          <el-icon><Setting /></el-icon>
          GitHub 代理
        </el-button>
        <el-button
          class="action-btn action-secondary"
          @click="handleBatchTest"
          :disabled="ruleLibrary.length === 0"
          :loading="testing"
        >
          <el-icon><Connection /></el-icon>
          {{ testing ? '测试中...' : '批量测试' }}        </el-button>
        <el-button
          class="action-btn action-secondary"
          @click="handleBatchCache"
          :disabled="selectedRules.length === 0"
          :loading="caching"
        >
          <el-icon><Download /></el-icon>
          {{ caching ? '缓存中...' : '批量缓存' }}        </el-button>
        <el-button
          v-if="selectedRules.length > 0"
          class="action-btn danger"
          @click="batchDeleteRules"
        >
          <el-icon><Delete /></el-icon>
          批量删除 ({{ selectedRules.length }})
        </el-button>
        <el-button
          class="action-btn action-secondary"
          @click="showBatchImportDialog"
        >
          <el-icon><Upload /></el-icon>
          批量导入
        </el-button>
        <el-button
          type="primary"
          class="action-btn action-primary"
          @click="showAddDialog"
        >
          <el-icon><Plus /></el-icon>
          添加规则集
        </el-button>
      </div>
    </div>

    <div v-if="ruleLibrary.length === 0" class="empty-state">
      <el-empty description="暂无规则，请添加规则集" />
    </div>

    <template v-else>
      <!-- 选择控制栏 -->
      <div class="selection-bar">
        <el-checkbox
          :model-value="allSelected"
          :indeterminate="someSelected && !allSelected"
          @change="toggleSelectAll"
        >
          全选
        </el-checkbox>
        <span v-if="selectedRules.length > 0" class="selection-count">
          已选择 {{ selectedRules.length }} 项
        </span>
      </div>

      <!-- 列表视图 -->
      <div v-if="viewMode === 'list'" class="rules-list" ref="rulesContainer">
      <div
        v-for="rule in ruleLibrary"
        :key="rule.id"
        class="list-item"
        :class="{ disabled: !rule.enabled }"
        :data-id="rule.id"
      >
        <div class="list-item-checkbox">
          <el-checkbox
            :model-value="selectedRules.includes(rule.id)"
            @change="toggleRuleSelection(rule.id)"
          />
        </div>
        <div class="list-item-drag">
          <button class="card-drag-handle" type="button" aria-label="拖动排序">
            <el-icon><DCaret /></el-icon>
          </button>
        </div>
        <div class="list-item-info">
          <div class="list-item-name">{{ rule.name }}</div>
          <div class="list-item-meta">
            <span class="meta-badge">{{ rule.behavior }}</span>
            <span class="meta-badge source">{{ rule.source_type === 'content' ? '规则内容' : 'URL地址' }}</span>
          </div>
        </div>
        <div class="list-item-content">
          <div v-if="rule.source_type === 'content'" class="content-preview">
            {{ getContentPreview(rule.content) }}
          </div>
          <div v-else class="url-preview">
            <el-link :href="rule.url" target="_blank" type="primary" :underline="false">
              {{ rule.url }}
            </el-link>
          </div>
        </div>
        <div class="list-item-actions">
          <button class="status-toggle" :class="{ active: rule.enabled }" @click="handleToggle(rule)">
            <el-icon v-if="rule.enabled"><View /></el-icon>
            <el-icon v-else><Hide /></el-icon>
          </button>
          <el-button
            v-if="rule.source_type === 'content'"
            class="list-btn"
            size="small"
            @click="showAddRuleToSetDialog(rule)"
            title="添加规则"
          >
            <el-icon><Plus /></el-icon>
          </el-button>
          <el-button
            class="list-btn"
            size="small"
            @click="copyRuleUrl(rule)"
            title="复制下载URL"
          >
            <el-icon><CopyDocument /></el-icon>
          </el-button>
          <el-button
            class="list-btn"
            size="small"
            @click="editRule(rule)"
            title="编辑"
          >
            <el-icon><Edit /></el-icon>
          </el-button>
          <el-button
            class="list-btn danger"
            size="small"
            @click="deleteRule(rule)"
            title="删除"
          >
            <el-icon><Delete /></el-icon>
          </el-button>
        </div>
      </div>
    </div>

      <!-- 卡片视图 -->
      <div v-else class="rules-grid" ref="rulesContainer">
      <div
        v-for="rule in ruleLibrary"
        :key="rule.id"
        class="rule-card"
        :class="{ disabled: !rule.enabled }"
        :data-id="rule.id"
      >
        <div class="card-header">
          <div class="card-title-group">
            <el-checkbox
              :model-value="selectedRules.includes(rule.id)"
              @change="toggleRuleSelection(rule.id)"
              style="margin-right: 8px"
            />
            <button class="card-drag-handle" type="button" aria-label="拖动排序">
              <el-icon><DCaret /></el-icon>
            </button>
            <div class="card-title">{{ rule.name }}</div>
          </div>
          <button class="status-toggle" :class="{ active: rule.enabled }" @click="handleToggle(rule)">
            <el-icon v-if="rule.enabled"><View /></el-icon>
            <el-icon v-else><Hide /></el-icon>
          </button>
        </div>

        <div class="card-meta">
          <span class="meta-pill type-pill">
            {{ rule.behavior }}
          </span>
          <span class="meta-pill source-pill">
            {{ rule.source_type === 'content' ? '规则内容' : 'URL地址' }}
          </span>
        </div>

        <div class="card-section">
          <div class="section-label">
            <el-icon v-if="rule.source_type === 'content'"><Document /></el-icon>
            <el-icon v-else><Link /></el-icon>
            {{ rule.source_type === 'content' ? '规则内容' : '规则地址' }}
          </div>
          <div v-if="rule.source_type === 'content'" class="content-box">
            {{ getContentPreview(rule.content) }}
          </div>
          <div v-else class="url-box">
            <el-link :href="rule.url" target="_blank" type="primary" :underline="false">
              {{ rule.url }}
            </el-link>
          </div>
        </div>

        <div class="card-actions">
          <el-button
            v-if="rule.source_type === 'content'"
            class="card-btn ghost"
            size="small"
            @click="showAddRuleToSetDialog(rule)"
          >
            <el-icon><Plus /></el-icon>
            添加规则
          </el-button>
          <el-button
            class="card-btn ghost"
            size="small"
            @click="copyRuleUrl(rule)"
          >
            <el-icon><CopyDocument /></el-icon>
            复制URL
          </el-button>
          <el-button
            class="card-btn ghost"
            size="small"
            @click="editRule(rule)"
          >
            <el-icon><Edit /></el-icon>
            编辑
          </el-button>
          <el-button
            class="card-btn danger"
            size="small"
            @click="deleteRule(rule)"
          >
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </div>
      </div>
    </div>
    </template>

    <!-- 添加/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑规则' : '添加规则'"
      width="700px"
      class="rule-dialog"
    >
      <div class="dialog-card">
        <el-form :model="form" label-width="100px" class="rule-form">
          <el-form-item label="规则名">
            <el-input v-model="form.name" placeholder="请输入规则名称" />
          </el-form-item>
          <el-form-item label="来源类型">
            <el-radio-group v-model="form.source_type">
              <el-radio value="url">URL 地址</el-radio>
              <el-radio value="content">规则内容</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="规则地址" v-if="form.source_type === 'url'">
            <el-input v-model="form.url" placeholder="请输入规则集URL地址" />
          </el-form-item>
          <el-form-item label="规则内容" v-if="form.source_type === 'content'">
            <el-input
              v-model="form.content"
              type="textarea"
              :rows="10"
              :placeholder="ruleContentPlaceholder"
            />
            <div class="helper-text">
              {{ ruleContentHelperText }}
            </div>
          </el-form-item>
          <el-form-item label="类型">
            <el-select v-model="form.behavior" style="width: 100%">
              <el-option label="Domain" value="domain" />
              <el-option label="IP CIDR" value="ipcidr" />
              <el-option label="Classical" value="classical" />
            </el-select>
          </el-form-item>
          <el-form-item label="状态">
            <div class="status-toggle-row">
              <el-switch v-model="form.enabled" @change="handleFormStatusChange" />
              <span>{{ form.enabled ? '规则启用中' : '规则已停用' }}</span>
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="dialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveRule">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 给规则集添加规则对话框 -->
    <el-dialog
      v-model="addRuleToSetDialogVisible"
      :title="`添加规则到 ${currentRuleSet?.name}`"
      width="600px"
      class="rule-dialog"
    >
      <div class="dialog-card">
        <el-form :model="addRuleToSetForm" label-width="100px" class="rule-form">
          <el-form-item label="规则类型">
            <el-select v-model="addRuleToSetForm.rule_type" placeholder="选择规则类型" style="width: 100%">
              <el-option label="DOMAIN" value="DOMAIN" />
              <el-option label="DOMAIN-SUFFIX" value="DOMAIN-SUFFIX" />
              <el-option label="DOMAIN-KEYWORD" value="DOMAIN-KEYWORD" />
              <el-option label="IP-CIDR" value="IP-CIDR" />
              <el-option label="IP-CIDR6" value="IP-CIDR6" />
              <el-option label="IP-SUFFIX" value="IP-SUFFIX" />
              <el-option label="DST-PORT" value="DST-PORT" />
            </el-select>
          </el-form-item>
          <el-form-item label="值">
            <el-input
              v-model="addRuleToSetForm.value"
              placeholder="域名、IP或规则集名称"
              :rows="5"
              type="textarea"
            />
            <div class="helper-text">
              每行一个值。例如：<br>
              example.com<br>
              baidu.com<br>
              192.168.1.0/24
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="addRuleToSetDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveRuleToSet">添加</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 批量导入对话框 -->
    <el-dialog
      v-model="batchImportDialogVisible"
      title="批量导入规则"
      width="700px"
      class="rule-dialog"
    >
      <div class="dialog-card">
        <el-alert
          title="粘贴 YAML 格式的 rule-providers 配置"
          type="info"
          :closable="false"
          style="margin-bottom: 16px"
        >
          <template #default>
            <div style="font-size: 13px; margin-top: 8px;">
              示例格式：<br>
              <code style="background: #f5f7fa; padding: 2px 6px; border-radius: 3px;">
                private_block: { type: http, behavior: classical, url: "https://...", ... }
              </code>
            </div>
          </template>
        </el-alert>
        <el-input
          v-model="batchImportText"
          type="textarea"
          :rows="12"
          placeholder="粘贴 rule-providers 配置内容..."
        />
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="batchImportDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="processBatchImport">
            <el-icon><Upload /></el-icon>
            导入
          </el-button>
        </div>
      </template>
    </el-dialog>

    <!-- GitHub 代理域名配置对话框 -->
    <el-dialog
      v-model="proxyConfigDialogVisible"
      title="GitHub 代理域名配置"
      width="500px"
      class="rule-dialog"
    >
      <div class="dialog-card">
        <el-alert
          title="配置后将在连通性测试、Mihomo生成、MosDNS转换时自动使用代理域名"
          type="info"
          :closable="false"
          style="margin-bottom: 16px"
        />
        <el-form :model="proxyDomains" label-width="120px" class="rule-form">
          <el-form-item label="GitHub 代理">
            <el-input
              v-model="proxyDomains.proxy"
              placeholder="例如: ghproxy.com 或 https://ghproxy.com"
              clearable
            />
            <div class="helper-text">
              支持的域名：github.com、raw.githubusercontent.com、gist.githubusercontent.com、api.github.com
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="proxyConfigDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="handleSaveProxyDomains">保存配置</el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus,
  Edit,
  Delete,
  Upload,
  Download,
  DCaret,
  Connection,
  Setting,
  View,
  Hide,
  Link,
  Document,
  List,
  Grid,
  CopyDocument
} from '@element-plus/icons-vue'
import api from '@/api'
import Sortable from 'sortablejs'

interface RuleLibraryItem {
  id: string
  name: string
  url: string
  behavior: string
  enabled: boolean
  source_type?: 'url' | 'content'
  content?: string
}

const ruleLibrary = ref<RuleLibraryItem[]>([])
const dialogVisible = ref(false)
const batchImportDialogVisible = ref(false)
const addRuleToSetDialogVisible = ref(false)
const proxyConfigDialogVisible = ref(false)
const batchImportText = ref('')
const isEdit = ref(false)
const testing = ref(false)
const caching = ref(false) // 批量缓存加载状态
const testResults = ref<Record<string, boolean>>({})
const currentRuleSet = ref<RuleLibraryItem | null>(null)
const rulesContainer = ref<HTMLElement | null>(null)
const viewMode = ref<'list' | 'grid'>('list') // 默认列表视图
const selectedRules = ref<string[]>([]) // 选中的规则ID列表
let sortableInstance: any = null // Sortable实例

// 专业功能开关
// 处理按钮点击
const handleBatchTest = () => {
  batchTestConnectivity()
}

const handleBatchCache = () => {
  batchCacheRules()
}

const proxyDomains = ref({
  proxy: ''
})

const form = ref<Partial<RuleLibraryItem>>({
  name: '',
  url: '',
  behavior: 'classical',
  enabled: true,
  source_type: 'url',
  content: ''
})

const ruleContentPlaceholder = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return [
        '请输入域名规则，每行一条规则',
        '示例：',
        'DOMAIN-SUFFIX,example.com',
        'DOMAIN,github.com',
        'DOMAIN-KEYWORD,google'
      ].join('\n')
    case 'ipcidr':
      return [
        '请输入 IP 规则，每行一条规则',
        '示例：',
        'IP-CIDR,192.168.0.0/16',
        'IP-CIDR,10.0.0.0/8',
        'IP-CIDR6,2001:db8::/32'
      ].join('\n')
    default:
      return [
        '请输入规则内容，每行一条规则',
        '示例：',
        'RULE-SET,ads',
        'GEOSITE,category-ads-all',
        'PROCESS-NAME,Google Chrome'
      ].join('\n')
  }
})

const ruleContentHelperText = computed(() => {
  const behavior = form.value.behavior || 'classical'
  switch (behavior) {
    case 'domain':
      return '支持 DOMAIN、DOMAIN-SUFFIX、DOMAIN-KEYWORD 等域名规则，每行一条。'
    case 'ipcidr':
      return '支持 IPv4/IPv6 CIDR 写法，例如 192.168.0.0/16 或 2001:db8::/32。'
    default:
      return '可混合使用 DOMAIN、GEOIP、RULE-SET 等规则前缀，请确保格式符合 Clash/Mihomo 要求。'
  }
})

const addRuleToSetForm = ref({
  rule_type: 'DOMAIN-SUFFIX',
  value: ''
})

const loadRuleLibrary = async () => {
  try {
    const { data } = await api.get('/rule-library')
    ruleLibrary.value = data
    nextTick(() => {
      initSortable()
    })
  } catch (error) {
    ElMessage.error('加载规则仓库失败')
  }
}

const showAddDialog = () => {
  isEdit.value = false
  form.value = {
    id: `lib_${Date.now()}`,
    name: '',
    url: '',
    behavior: 'classical',
    enabled: true,
    source_type: 'url',
    content: ''
  }
  dialogVisible.value = true
}

const editRule = (row: RuleLibraryItem) => {
  isEdit.value = true
  form.value = {
    ...row,
    source_type: row.source_type || 'url',
    content: row.content || ''
  }
  dialogVisible.value = true
}

const isFullUrl = (url: string): boolean => {
  if (!url) return false
  return url.startsWith('http://') || url.startsWith('https://')
}

const testSingleRule = async (url: string): Promise<boolean> => {
  try {
    if (!isFullUrl(url)) {
      return true
    }

    const { data } = await api.post('/rule-library/test-single', { url })
    if (data.success) {
      return data.available
    }
    return false
  } catch (error) {
    return false
  }
}

const handleFormStatusChange = async (enabled: boolean) => {
  if (!enabled) {
    return
  }

  if (form.value.source_type === 'url' && form.value.url && isFullUrl(form.value.url)) {
    const loading = ElMessage({
      message: '正在测试规则连通性...',
      duration: 0,
      type: 'info'
    })

    const isAvailable = await testSingleRule(form.value.url)
    loading.close()

    if (!isAvailable) {
      ElMessage.error('规则地址无法访问，无法开启')
      form.value.enabled = false
      return
    }
  }
}

const saveRule = async () => {
  if (!form.value.name) {
    ElMessage.warning('请输入规则名称')
    return
  }

  if (form.value.source_type === 'url') {
    if (!form.value.url) {
      ElMessage.warning('请输入规则地址')
      return
    }
  } else if (form.value.source_type === 'content') {
    if (!form.value.content || !form.value.content.trim()) {
      ElMessage.warning('请输入规则内容')
      return
    }
  }

  let isAvailable = true

  if (form.value.source_type === 'url' && form.value.url) {
    const loading = ElMessage({
      message: '正在测试规则连通性...',
      duration: 0,
      type: 'info'
    })

    isAvailable = await testSingleRule(form.value.url)
    loading.close()

    if (!isAvailable) {
      try {
        await ElMessageBox.confirm(
          '该规则地址无法访问，规则将被添加但状态为关闭。是否继续？',
          '连通性测试失败',
          {
            confirmButtonText: '继续添加',
            cancelButtonText: '取消',
            type: 'warning'
          }
        )
        form.value.enabled = false
      } catch {
        return
      }
    }
  }

  try {
    if (isEdit.value) {
      const { data } = await api.put(`/rule-library/${form.value.id}`, form.value)

      // 显示同步信息
      if (data.synced_count > 0) {
        ElMessage.success({
          message: `更新成功，并同步${form.value.enabled ? '启用' : '禁用'}了 ${data.synced_count} 个关联的规则配置`,
          duration: 3000
        })
      } else {
        ElMessage.success('更新成功')
      }
    } else {
      await api.post('/rule-library', form.value)
      ElMessage.success(isAvailable ? '添加成功' : '添加成功（规则已关闭）')
    }
    dialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const disableRelatedRuleConfigs = async (libraryRuleId: string) => {
  try {
    const { data: allRules } = await api.get('/rules')

    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && item.library_rule_id === libraryRuleId
    )

    for (const ruleSet of relatedRuleSets) {
      if (ruleSet.enabled) {
        ruleSet.enabled = false
        await api.put(`/rule-sets/${ruleSet.id}`, ruleSet)
      }
    }

    if (relatedRuleSets.length > 0) {
      ElMessage.info(`已同时关闭 ${relatedRuleSets.length} 个关联的规则配置`)
    }
  } catch (error) {
    console.error('关闭关联规则配置失败:', error)
  }
}

const deleteRule = async (row: RuleLibraryItem) => {
  try {
    const { data: allRules } = await api.get('/rules')
    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && item.library_rule_id === row.id
    )

    if (relatedRuleSets.length > 0) {
      try {
        await ElMessageBox.confirm(
          `该规则被 ${relatedRuleSets.length} 个规则配置引用，是否一起删除这些规则配置？`,
          '删除确认',
          {
            confirmButtonText: '一起删除',
            cancelButtonText: '仅删除规则仓库',
            distinguishCancelAndClose: true,
            type: 'warning'
          }
        )

        const deletedRuleSetIds: string[] = []
        for (const ruleSet of relatedRuleSets) {
          try {
            await api.delete(`/rule-sets/${ruleSet.id}`)
            deletedRuleSetIds.push(ruleSet.id)
          } catch (error) {
            console.error(`删除规则配置 ${ruleSet.name} 失败:`, error)
          }
        }

        if (deletedRuleSetIds.length > 0) {
          try {
            const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

            const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter(
              (id: string) => !deletedRuleSetIds.includes(id)
            )
            const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter(
              (id: string) => !deletedRuleSetIds.includes(id)
            )

            if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
                updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
              await api.post('/mosdns/rulesets', {
                direct_rulesets: updatedDirectRulesets,
                proxy_rulesets: updatedProxyRulesets,
                direct_rules: mosdnsConfig.direct_rules,
                proxy_rules: mosdnsConfig.proxy_rules
              })
              console.log('已同步更新 MosDNS 配置，移除了对已删除规则集的引用')
            }
          } catch (error) {
            console.error('同步更新 MosDNS 配置失败:', error)
            ElMessage.warning('规则配置已删除，但 MosDNS 配置同步失败，请手动检查')
          }
        }

        await api.delete(`/rule-library/${row.id}`)
        ElMessage.success(`已删除规则仓库及 ${relatedRuleSets.length} 个关联的规则配置`)
        loadRuleLibrary()
      } catch (action) {
        if (action === 'cancel') {
          await api.delete(`/rule-library/${row.id}`)
          ElMessage.success('已删除规则仓库，关联的规则配置保留')
          loadRuleLibrary()
        } else {
          return
        }
      }
    } else {
      await ElMessageBox.confirm(
        '确定要删除该规则吗？',
        '删除确认',
        {
          confirmButtonText: '确定',
          cancelButtonText: '取消',
          type: 'warning'
        }
      )

      await api.delete(`/rule-library/${row.id}`)
      ElMessage.success('删除成功')
      loadRuleLibrary()
    }
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除失败')
      console.error('删除规则失败:', error)
    }
  }
}

const handleToggle = async (rule: RuleLibraryItem) => {
  rule.enabled = !rule.enabled
  await toggleEnabled(rule)
}

const toggleEnabled = async (row: RuleLibraryItem) => {
  if (row.enabled && row.source_type === 'url' && isFullUrl(row.url)) {
    const loading = ElMessage({
      message: '正在测试规则连通性...',
      duration: 0,
      type: 'info'
    })

    const isAvailable = await testSingleRule(row.url)
    loading.close()

    if (!isAvailable) {
      ElMessage.error('规则地址无法访问，无法开启')
      row.enabled = false
      return
    }
  }

  try {
    const { data } = await api.put(`/rule-library/${row.id}`, row)

    // 显示同步信息
    if (data.synced_count > 0) {
      ElMessage.success({
        message: `${row.enabled ? '已开启' : '已关闭'}，并同步${row.enabled ? '启用' : '禁用'}了 ${data.synced_count} 个关联的规则配置`,
        duration: 3000
      })
    } else {
      ElMessage.success(row.enabled ? '已开启' : '已关闭')
    }
  } catch (error) {
    ElMessage.error('更新失败')
    row.enabled = !row.enabled
  }
}

const showAddRuleToSetDialog = (row: RuleLibraryItem) => {
  currentRuleSet.value = row
  addRuleToSetForm.value = {
    rule_type: 'DOMAIN-SUFFIX',
    value: ''
  }
  addRuleToSetDialogVisible.value = true
}

const saveRuleToSet = async () => {
  if (!currentRuleSet.value) {
    return
  }

  if (!addRuleToSetForm.value.value || !addRuleToSetForm.value.value.trim()) {
    ElMessage.warning('请输入规则值')
    return
  }

  const values = addRuleToSetForm.value.value.trim().split('\n').filter(line => line.trim())
  const newRules = values.map(value => `${addRuleToSetForm.value.rule_type},${value.trim()}`).join('\n')

  const existingContent = currentRuleSet.value.content || ''
  const updatedContent = existingContent ? `${existingContent}\n${newRules}` : newRules

  try {
    const updatedRule = {
      ...currentRuleSet.value,
      content: updatedContent
    }

    await api.put(`/rule-library/${currentRuleSet.value.id}`, updatedRule)
    ElMessage.success('添加成功')
    addRuleToSetDialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const showBatchImportDialog = () => {
  batchImportText.value = ''
  batchImportDialogVisible.value = true
}

const processBatchImport = async () => {
  const text = batchImportText.value.trim()
  if (!text) {
    ElMessage.warning('请粘贴配置内容')
    return
  }

  try {
    const rules = parseRuleProviders(text)
    if (rules.length === 0) {
      ElMessage.warning('未解析到有效的规则配置')
      return
    }

    let successCount = 0
    for (const rule of rules) {
      try {
        await api.post('/rule-library', rule)
        successCount++
      } catch (error) {
        console.error(`导入规则 ${rule.name} 失败:`, error)
      }
    }

    ElMessage.success(`成功导入 ${successCount} 条规则`)
    batchImportDialogVisible.value = false
    loadRuleLibrary()
  } catch (error) {
    ElMessage.error('解析配置失败，请检查格式')
  }
}

const parseRuleProviders = (text: string): RuleLibraryItem[] => {
  const rules: RuleLibraryItem[] = []

  text = text.replace(/^rule-providers:\s*$/m, '')

  const lines = text.split('\n')

  let counter = 0
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue

    const match = trimmed.match(/^([a-zA-Z0-9_-]+):\s*\{(.+)\}/)
    if (!match) continue

    const name = match[1]
    const content = match[2]

    const urlMatch = content.match(/url:\s*["']([^"']+)["']/)
    if (!urlMatch) continue
    const url = urlMatch[1]

    const behaviorMatch = content.match(/behavior:\s*([a-z]+)/)
    const behavior = behaviorMatch ? behaviorMatch[1] : 'classical'

    rules.push({
      id: `lib_${Date.now()}_${counter++}`,
      name,
      url,
      behavior,
      enabled: true
    })
  }

  return rules
}

const initSortable = () => {
  nextTick(() => {
    // 先销毁旧实例
    if (sortableInstance) {
      sortableInstance.destroy()
      sortableInstance = null
    }

    if (rulesContainer.value) {
      sortableInstance = Sortable.create(rulesContainer.value, {
        animation: 150,
        handle: '.card-drag-handle',
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        dragClass: 'sortable-drag',
        onEnd: async (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex) return

          const movedItem = ruleLibrary.value.splice(oldIndex, 1)[0]
          ruleLibrary.value.splice(newIndex, 0, movedItem)

          try {
            await saveRuleLibraryOrder()
            ElMessage.success('排序已更新')
          } catch (error) {
            ElMessage.error('保存排序失败')
            loadRuleLibrary()
          }
        }
      })
    }
  })
}

const saveRuleLibraryOrder = async () => {
  await api.post('/rule-library/reorder', {
    rules: ruleLibrary.value
  })
}

const getContentPreview = (content?: string) => {
  if (!content) return '无内容'

  const lines = content.split('\n').filter(line => line.trim())
  const totalLines = lines.length

  const previewLines = lines.slice(0, 3)
  const preview = previewLines.join('\n')

  if (totalLines > 3) {
    return `${preview}\n... (共 ${totalLines} 条规则)`
  }

  return preview
}

const batchTestConnectivity = async () => {
  try {
    await ElMessageBox.confirm(
      '即将测试所有规则地址的连通性，不可用的规则将被自动关闭。是否继续？',
      '批量测试连通性',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    testing.value = true
    testResults.value = {}

    const { data } = await api.post('/rule-library/test')

    if (data.success) {
      data.results.forEach((result: any) => {
        testResults.value[result.id] = result.available
      })

      const failedCount = data.failed_count
      const totalCount = data.total_count
      const successCount = totalCount - failedCount

      const failedResults = data.results.filter((result: any) => !result.available)
      for (const failedRule of failedResults) {
        await disableRelatedRuleConfigs(failedRule.id)
      }

      if (failedCount > 0) {
        ElMessage.warning({
          message: `测试完成！成功: ${successCount}，失败: ${failedCount}。不可用的规则和关联的规则配置已自动关闭。`,
          duration: 5000,
          showClose: true
        })
      } else {
        ElMessage.success({
          message: `测试完成！所有 ${totalCount} 条规则均可用。`,
          duration: 3000
        })
      }

      await loadRuleLibrary()
    } else {
      ElMessage.error('测试失败：' + data.message)
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('测试失败')
      console.error('测试连通性失败:', error)
    }
  } finally {
    testing.value = false
  }
}

const batchCacheRules = async () => {
  try {
    await ElMessageBox.confirm(
      `即将缓存选中的 ${selectedRules.value.length} 条规则到本地，缓存失败的规则将被自动关闭。是否继续？`,
      '批量缓存规则',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    caching.value = true

    const { data } = await api.post('/rule-library/cache', {
      rule_ids: selectedRules.value
    })

    if (data.success) {
      const successCount = data.success_count
      const failedCount = data.failed_count
      const totalCount = data.total_count

      // 清空选择
      selectedRules.value = []

      if (failedCount > 0) {
        ElMessage.warning({
          message: `缓存完成！成功: ${successCount}，失败: ${failedCount}。缓存失败的规则和关联的规则配置已自动关闭。`,
          duration: 5000,
          showClose: true
        })
      } else {
        ElMessage.success({
          message: `缓存完成！成功缓存 ${totalCount} 条规则。`,
          duration: 3000
        })
      }

      await loadRuleLibrary()
    } else {
      ElMessage.error('缓存失败：' + data.message)
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error('缓存失败')
      console.error('批量缓存失败:', error)
    }
  } finally {
    caching.value = false
  }
}

const loadProxyDomains = async () => {
  try {
    const { data } = await api.get('/rule-library/proxy-domains')
    proxyDomains.value = {
      proxy: data.proxy_domains || ''
    }
  } catch (error) {
    console.error('加载代理域名配置失败:', error)
  }
}

const showProxyConfigDialog = () => {
  proxyConfigDialogVisible.value = true
}

const handleSaveProxyDomains = async () => {
  try {
    const proxyValue = proxyDomains.value.proxy?.trim() || ''

    await api.post('/rule-library/proxy-domains', {
      proxy_domains: proxyValue
    })

    ElMessage.success('代理域名配置已保存')
    proxyConfigDialogVisible.value = false
  } catch (error) {
    ElMessage.error('保存代理域名配置失败')
    console.error('保存代理域名配置失败:', error)
  }
}

// 生成规则的可下载 URL
const getRuleDownloadUrl = (rule: RuleLibraryItem): string => {
  if (rule.source_type === 'content') {
    const baseUrl = `${window.location.protocol}//${window.location.host}`
    return `${baseUrl}/api/rule-library/content/${rule.id}`
  }
  return rule.url || ''
}

// 复制 URL 到剪贴板
const copyRuleUrl = async (rule: RuleLibraryItem) => {
  const url = getRuleDownloadUrl(rule)
  if (!url) {
    ElMessage.warning('该规则没有可用的URL')
    return
  }

  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('URL已复制到剪贴板')
  } catch (err) {
    // 降级方案
    const input = document.createElement('input')
    input.value = url
    document.body.appendChild(input)
    input.select()
    document.execCommand('copy')
    document.body.removeChild(input)
    ElMessage.success('URL已复制到剪贴板')
  }
}

// 选择相关的计算属性
const allSelected = computed(() => {
  return ruleLibrary.value.length > 0 && selectedRules.value.length === ruleLibrary.value.length
})

const someSelected = computed(() => {
  return selectedRules.value.length > 0 && selectedRules.value.length < ruleLibrary.value.length
})

// 切换单个规则的选择状态
const toggleRuleSelection = (ruleId: string) => {
  const index = selectedRules.value.indexOf(ruleId)
  if (index > -1) {
    selectedRules.value.splice(index, 1)
  } else {
    selectedRules.value.push(ruleId)
  }
}

// 切换全选/取消全选
const toggleSelectAll = () => {
  if (allSelected.value) {
    selectedRules.value = []
  } else {
    selectedRules.value = ruleLibrary.value.map(rule => rule.id)
  }
}

// 批量删除规则
const batchDeleteRules = async () => {
  if (selectedRules.value.length === 0) return

  try {
    // 获取所有规则配置，检查关联
    const { data: allRules } = await api.get('/rules')
    const relatedRuleSets = allRules.filter(
      (item: any) => item.itemType === 'ruleset' && selectedRules.value.includes(item.library_rule_id)
    )

    let confirmMessage = `确定要删除选中的 ${selectedRules.value.length} 条规则吗？`
    if (relatedRuleSets.length > 0) {
      confirmMessage = `选中的规则被 ${relatedRuleSets.length} 个规则配置引用，是否一起删除这些规则配置？`
    }

    await ElMessageBox.confirm(
      confirmMessage,
      '批量删除确认',
      {
        confirmButtonText: relatedRuleSets.length > 0 ? '一起删除' : '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    // 删除关联的规则配置
    const deletedRuleSetIds: string[] = []
    if (relatedRuleSets.length > 0) {
      for (const ruleSet of relatedRuleSets) {
        try {
          await api.delete(`/rule-sets/${ruleSet.id}`)
          deletedRuleSetIds.push(ruleSet.id)
        } catch (error) {
          console.error(`删除规则配置 ${ruleSet.name} 失败:`, error)
        }
      }

      // 同步更新 MosDNS 配置
      if (deletedRuleSetIds.length > 0) {
        try {
          const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')
          const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter(
            (id: string) => !deletedRuleSetIds.includes(id)
          )
          const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter(
            (id: string) => !deletedRuleSetIds.includes(id)
          )

          if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
              updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
            await api.post('/mosdns/rulesets', {
              direct_rulesets: updatedDirectRulesets,
              proxy_rulesets: updatedProxyRulesets,
              direct_rules: mosdnsConfig.direct_rules,
              proxy_rules: mosdnsConfig.proxy_rules
            })
          }
        } catch (error) {
          console.error('同步更新 MosDNS 配置失败:', error)
        }
      }
    }

    // 批量删除规则仓库
    let successCount = 0
    for (const ruleId of selectedRules.value) {
      try {
        await api.delete(`/rule-library/${ruleId}`)
        successCount++
      } catch (error) {
        console.error(`删除规则 ${ruleId} 失败:`, error)
      }
    }

    // 清空选择
    selectedRules.value = []

    if (relatedRuleSets.length > 0) {
      ElMessage.success(`已删除 ${successCount} 条规则及 ${deletedRuleSetIds.length} 个关联的规则配置`)
    } else {
      ElMessage.success(`已删除 ${successCount} 条规则`)
    }

    loadRuleLibrary()
  } catch (error: any) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('批量删除失败')
      console.error('批量删除失败:', error)
    }
  }
}

// 监听视图模式切换，重新初始化拖拽
watch(viewMode, () => {
  nextTick(() => {
    initSortable()
  })
})

onMounted(() => {
  loadRuleLibrary()
  loadProxyDomains()
})

onUnmounted(() => {
})
</script>

<style scoped>
.rule-library-page {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --rule-radius-xl: 40px;
  --rule-radius-lg: 24px;
  --rule-radius-md: 16px;
  --rule-radius-sm: 12px;
  --rule-radius-pill: 999px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
  margin-bottom: 28px;
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
  border-radius: var(--rule-radius-md, 16px);
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

.action-btn.danger {
  background: rgba(245, 108, 108, 0.12);
  border: 1px solid rgba(245, 108, 108, 0.35);
  color: #f56c6c;
}

.action-btn.danger:hover {
  background: rgba(245, 108, 108, 0.18);
  border-color: rgba(245, 108, 108, 0.5);
  transform: translateY(-1px);
  box-shadow: 0 8px 20px rgba(245, 108, 108, 0.2);
}

.action-btn:not([disabled]):hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 24px rgba(87, 104, 255, 0.25);
}

:deep(.action-btn .el-icon) {
  font-size: 16px;
}

.view-toggle {
  margin-right: 8px;
}

.toggle-btn {
  width: 40px;
  height: 40px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.2);
  color: #7c86ae;
  transition: all 0.2s ease;
}

.toggle-btn:hover {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
}

.toggle-btn.active {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  color: #fff;
  border-color: transparent;
}

.toggle-btn .el-icon {
  font-size: 18px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 400px;
  background: #fff;
  border-radius: var(--rule-radius-lg, 24px);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
}

/* 选择控制栏样式 */
.selection-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 20px;
  margin-bottom: 16px;
  background: #fff;
  border-radius: var(--rule-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.06);
}

.selection-count {
  font-size: 13px;
  color: #4e5eff;
  font-weight: 600;
  padding: 4px 12px;
  background: rgba(107, 115, 255, 0.12);
  border-radius: var(--rule-radius-pill, 999px);
}

.list-item-checkbox {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}

/* 列表视图样式 */
.rules-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.list-item {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: #fff;
  border-radius: var(--rule-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.1);
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.06);
  transition: all 0.2s ease;
}

.list-item:hover {
  transform: translateX(4px);
  box-shadow: 0 8px 20px rgba(65, 80, 180, 0.12);
  border-color: rgba(107, 115, 255, 0.25);
}

.list-item.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
}

.list-item-drag {
  flex-shrink: 0;
}

.list-item-info {
  flex: 0 0 200px;
  min-width: 0;
}

.list-item-name {
  font-size: 15px;
  font-weight: 600;
  color: #30354d;
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.list-item-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.meta-badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: var(--rule-radius-pill, 999px);
  font-size: 11px;
  font-weight: 600;
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  text-transform: capitalize;
}

.meta-badge.source {
  background: rgba(144, 147, 153, 0.12);
  color: #606266;
}

.list-item-content {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: #4b5678;
}

.content-preview,
.url-preview {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.content-preview {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: #606266;
}

.list-item-actions {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.list-btn {
  width: 32px;
  height: 32px;
  padding: 0;
  border-radius: 50%;
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.2);
  color: #4e5eff;
  transition: all 0.2s ease;
}

.list-btn:hover {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.35);
  transform: scale(1.08);
}

.list-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  border-color: rgba(155, 143, 255, 0.25);
  color: #9b8fff;
}

.list-btn.danger:hover {
  background: rgba(155, 143, 255, 0.18);
  border-color: rgba(155, 143, 255, 0.35);
}

.rules-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
}

.rule-card {
  background: #fff;
  border-radius: var(--rule-radius-lg, 24px);
  padding: 24px;
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.rule-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 20px 40px rgba(65, 80, 180, 0.16);
  border-color: rgba(107, 115, 255, 0.25);
}

.rule-card.disabled {
  opacity: 0.5;
  filter: grayscale(0.4);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.card-title-group {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}

.card-drag-handle {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: none;
  background: rgba(107, 115, 255, 0.08);
  color: #7c86ae;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: move;
  transition: all 0.2s ease;
}

.card-drag-handle:hover {
  background: rgba(107, 115, 255, 0.15);
  color: #4e5eff;
}

.card-title {
  font-size: 17px;
  font-weight: 700;
  color: #30354d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-toggle {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: none;
  background: rgba(107, 115, 255, 0.18);
  color: #4e5eff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.status-toggle:hover {
  transform: scale(1.05);
}

.status-toggle.active {
  background: linear-gradient(135deg, #8b8fff 0%, #6b7dff 100%);
  color: #fff;
  box-shadow: 0 12px 28px rgba(87, 104, 255, 0.3);
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 6px 12px;
  border-radius: var(--rule-radius-pill, 999px);
  font-size: 12px;
  font-weight: 600;
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.type-pill {
  text-transform: capitalize;
}

.source-pill {
  background: rgba(144, 147, 153, 0.12);
  border-color: rgba(144, 147, 153, 0.18);
  color: #606266;
}

.card-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
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

.url-box,
.content-box {
  background: rgba(107, 115, 255, 0.06);
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-radius: var(--rule-radius-md, 16px);
  padding: 12px 14px;
  font-size: 13px;
  color: #4b5678;
  word-break: break-all;
  transition: all 0.2s ease;
}

.url-box:hover {
  background: rgba(107, 115, 255, 0.1);
  border-color: rgba(107, 115, 255, 0.25);
}

.content-box {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  max-height: 120px;
  overflow-y: auto;
}

.card-actions {
  display: flex;
  gap: 8px;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid rgba(107, 115, 255, 0.08);
}

.card-btn {
  flex: 1;
  height: 36px;
  border-radius: var(--rule-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}

.card-btn.ghost {
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.25);
  color: #4e5eff;
}

.card-btn.ghost:hover {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  border: 1px solid rgba(155, 143, 255, 0.28);
  color: #9b8fff;
}

.card-btn.danger:hover {
  background: rgba(155, 143, 255, 0.18);
  border-color: rgba(155, 143, 255, 0.35);
  transform: translateY(-1px);
}

:deep(.rule-dialog) {
  border-radius: var(--rule-radius-xl, 40px) !important;
  overflow: hidden;
  background: rgba(252, 253, 255, 0.97);
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  backdrop-filter: blur(20px);
}

:deep(.rule-dialog .el-dialog__header) {
  padding: 24px 32px;
  margin: 0;
  border-bottom: 1px solid rgba(107, 115, 255, 0.1);
  background: #f7f8ff;
}

:deep(.rule-dialog .el-dialog__title) {
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

:deep(.rule-dialog .el-dialog__body) {
  padding: 28px 32px;
  background: #f7f8ff;
}

:deep(.rule-dialog .el-dialog__footer) {
  padding: 20px 32px;
  border-top: 1px solid rgba(107, 115, 255, 0.1);
  background: #f7f8ff;
}

.dialog-card {
  background: #fff;
  border-radius: var(--rule-radius-lg, 24px);
  padding: 24px;
  box-shadow: 0 8px 20px rgba(91, 112, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.rule-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.rule-form :deep(.el-form-item__label) {
  font-weight: 600;
  font-size: 13px;
  color: #6c74a0;
}

.helper-text {
  margin-top: 8px;
  color: #909399;
  font-size: 12px;
  line-height: 1.6;
}

.status-toggle-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.status-toggle-row span {
  font-size: 14px;
  color: #606266;
}

.dialog-footer {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
}

.footer-btn {
  min-width: 100px;
  height: 40px;
  border-radius: var(--rule-radius-md, 16px);
  font-weight: 600;
}

.footer-btn.ghost {
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.25);
  color: #4e5eff;
}

.footer-btn.primary {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  border: none;
  box-shadow: 0 8px 16px rgba(87, 104, 255, 0.25);
}

.sortable-ghost {
  opacity: 0.4;
}

.sortable-chosen {
  opacity: 0.8;
}

.sortable-drag {
  cursor: move !important;
}

@media (max-width: 768px) {
  .rule-library-page {
    padding: 20px 16px 32px;
  }

  .page-header {
    flex-direction: column;
    align-items: flex-start;
    margin: -20px -16px 20px -16px;
    padding: 20px 16px;
  }

  .header-actions {
    width: 100%;
    flex-wrap: wrap;
  }

  .view-toggle {
    order: -1;
    margin-bottom: 8px;
  }

  .action-btn {
    flex: 1;
    min-width: calc(50% - 6px);
    justify-content: center;
  }

  .rules-grid {
    grid-template-columns: 1fr;
  }

  .list-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .list-item-info {
    flex: 1;
    width: 100%;
  }

  .list-item-content {
    width: 100%;
  }

  .list-item-actions {
    width: 100%;
    justify-content: flex-end;
  }

  :deep(.rule-dialog) {
    width: 95vw !important;
    max-width: 95vw !important;
  }
}
</style>
