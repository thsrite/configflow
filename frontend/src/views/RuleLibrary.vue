<template>
  <div class="rule-library-page" :class="{ 'cf-reordering': reorder.active.value }">
    <ScopeBanner scope="resource" :profile-name="cfProfileName" description="规则集来源与缓存，按配置空间隔离" />
    <PageHeader title="规则库" description="集中维护规则集来源与缓存，所有配置空间共同使用">
      <template #actions>
        <el-button v-if="!reorder.active.value" :disabled="ruleLibrary.length < 2" @click="reorder.enter">
          <el-icon><Sort /></el-icon>
          调整顺序
        </el-button>
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
         
          @click="showProxyConfigDialog"
        >
          <el-icon><Setting /></el-icon>
          GitHub 代理
        </el-button>
        <el-button
         
          @click="handleBatchTest"
          :disabled="ruleLibrary.length === 0"
          :loading="testing"
        >
          <el-icon><Connection /></el-icon>
          {{ testing ? '测试中...' : '批量测试' }}        </el-button>
        <el-button
         
          @click="handleBatchCache"
          :disabled="selectedRules.length === 0"
          :loading="caching"
        >
          <el-icon><Download /></el-icon>
          {{ caching ? '缓存中...' : '批量缓存' }}        </el-button>
        <el-button
          v-if="selectedRules.length > 0"
          type="danger" plain
          @click="batchDeleteRules"
        >
          <el-icon><Delete /></el-icon>
          批量删除 ({{ selectedRules.length }})
        </el-button>
        <el-button
         
          @click="showBatchImportDialog"
        >
          <el-icon><Upload /></el-icon>
          批量导入
        </el-button>
        <el-button
          type="primary"
          @click="showAddDialog"
        >
          <el-icon><Plus /></el-icon>
          添加规则集
        </el-button>
      </template>
    </PageHeader>

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
      <ReorderBar
        :active="reorder.active.value"
        :saving="reorder.saving.value"
        :announcement="reorder.announcement.value"
        @cancel="reorder.cancel"
        @save="handleSaveOrder"
      />

      <div v-if="viewMode === 'list'" class="rules-list" ref="rulesContainer">
      <div
        v-for="(rule, cfIndex) in ruleLibrary"
        :key="rule.id"
        class="list-item"
        :class="{ disabled: !rule.enabled }"
        :data-id="rule.id"
        data-reorder-item
      >
        <div class="list-item-checkbox">
          <el-checkbox
            :model-value="selectedRules.includes(rule.id)"
            @change="toggleRuleSelection(rule.id)"
          />
        </div>
        <div class="list-item-drag">
            <DragHandle
              v-if="reorder.active.value"
              :label="rule.name || rule.id"
              :index="cfIndex"
              :total="ruleLibrary.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
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
        v-for="(rule, cfIndex) in ruleLibrary"
        :key="rule.id"
        class="rule-card"
        :class="{ disabled: !rule.enabled }"
        :data-id="rule.id"
        data-reorder-item
      >
        <div class="card-header">
          <div class="card-title-group">
            <el-checkbox
              :model-value="selectedRules.includes(rule.id)"
              @change="toggleRuleSelection(rule.id)"
              style="margin-right: 8px"
            />
            <DragHandle
              v-if="reorder.active.value"
              :label="rule.name || rule.id"
              :index="cfIndex"
              :total="ruleLibrary.length"
              :position="reorder.positionLabel(cfIndex)"
              :grabbed="reorder.grabbedIndex.value === cfIndex"
              @up="reorder.moveUp(cfIndex)"
              @down="reorder.moveDown(cfIndex)"
              @keydown="reorder.onHandleKeydown($event, cfIndex)"
            />
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
          <el-form-item v-if="currentRuleSet?.behavior === 'classical'" label="规则类型">
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
              {{ addRuleToSetHelperText }}
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
              <code style="background: var(--cf-s2); padding: 2px 6px; border-radius: 3px;">
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
import { useProfileStore } from '@/stores/profile'
import ReorderBar from '@/components/shell/ReorderBar.vue'
import DragHandle from '@/components/shell/DragHandle.vue'
import { useReorder } from '@/composables/useReorder'
import PageHeader from '@/components/shell/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
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
  CopyDocument, Sort } from '@element-plus/icons-vue'
import api from '@/api'
import { activeProfileId } from '@/profileContext'


const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
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

const addRuleToSetHelperText = computed(() => {
  switch (currentRuleSet.value?.behavior) {
    case 'domain':
      return '每行一个域名，例如 example.com 或 baidu.com。'
    case 'ipcidr':
      return '每行一个 CIDR，例如 1.1.1.0/24 或 2001:db8::/32。'
    default:
      return '每行一个值，例如 example.com、baidu.com 或 192.168.1.0/24。'
  }
})

const loadRuleLibrary = async () => {
  try {
    const { data } = await api.get('/rule-library')
    ruleLibrary.value = data
    nextTick(() => {
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
    rule_type: row.behavior === 'ipcidr' ? 'IP-CIDR' : 'DOMAIN-SUFFIX',
    value: ''
  }
  addRuleToSetDialogVisible.value = true
}

const formatRuleValueForRuleSet = (value: string) => {
  const trimmedValue = value.trim()

  if (currentRuleSet.value?.behavior === 'classical') {
    return `${addRuleToSetForm.value.rule_type},${trimmedValue}`
  }

  return trimmedValue
}

const isValidIpv4Cidr = (value: string) => {
  const match = value.match(/^(\d{1,3})(?:\.(\d{1,3})){3}\/(\d{1,2})$/)
  if (!match) return false

  const [address, prefix] = value.split('/')
  const octets = address.split('.').map(Number)
  const prefixNumber = Number(prefix)

  return octets.every(octet => octet >= 0 && octet <= 255) && prefixNumber >= 0 && prefixNumber <= 32
}

const isValidIpv6Cidr = (value: string) => {
  const match = value.match(/^([0-9a-fA-F:]+)\/(\d{1,3})$/)
  if (!match || !match[1].includes(':')) return false

  const prefixNumber = Number(match[2])
  return prefixNumber >= 0 && prefixNumber <= 128
}

const isValidCidr = (value: string) => isValidIpv4Cidr(value) || isValidIpv6Cidr(value)

const isLikelyDomainValue = (value: string) => {
  if (value.includes(',') || value.includes('/')) return false
  return /^[a-zA-Z0-9+*_.-]+$/.test(value)
}

const getInvalidRuleValue = (values: string[]) => {
  switch (currentRuleSet.value?.behavior) {
    case 'domain':
      return values.find(value => !isLikelyDomainValue(value.trim()))
    case 'ipcidr':
      return values.find(value => !isValidCidr(value.trim()))
    default:
      return undefined
  }
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
  const invalidValue = getInvalidRuleValue(values)
  if (invalidValue) {
    const behavior = currentRuleSet.value.behavior
    const expectedText = behavior === 'ipcidr' ? 'CIDR，例如 1.1.1.0/24' : '域名，例如 example.com'
    ElMessage.warning(`"${invalidValue.trim()}" 不符合 ${currentRuleSet.value.name} 的规则集类型，请输入${expectedText}`)
    return
  }

  const newRules = values.map(formatRuleValueForRuleSet).join('\n')

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
    return `${baseUrl}/api/profiles/${encodeURIComponent(activeProfileId.value)}/rule-library/content/${rule.id}`
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
  })
})

/* ---------- 统一拖动排序 ---------- */
const reorder = useReorder<any>({
  items: ruleLibrary,
  container: rulesContainer,
  labelOf: item => item.name || item.id,
  // 按 id 提交，服务端在存量数据上重排，不回写列表接口的计算字段
  persist: async items => {
    await api.post('/rule-library/reorder', {
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
  loadRuleLibrary()
  loadProxyDomains()
})

onUnmounted(() => {
})
</script>

<style scoped>
.rule-library-page {
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
  background: var(--cf-bg);
  margin: -28px -32px 28px -32px;
  padding: 28px 32px;
}

.title-block h2 {
  margin: 0;
  font-size: 26px;
  font-weight: 700;
  color: var(--cf-fg);
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
  color: var(--cf-fg-2);
  transition: all 0.2s ease;
}

.toggle-btn:hover {
  background: rgba(107, 115, 255, 0.12);
  color: var(--cf-primary);
}

.toggle-btn.active {
  background: var(--cf-primary-fill);
  color: var(--cf-primary-fg);
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
  background: var(--cf-s1);
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
  background: var(--cf-s1);
  border-radius: var(--rule-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.06);
}

.selection-count {
  font-size: 13px;
  color: var(--cf-primary);
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
  background: var(--cf-s1);
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
  color: var(--cf-fg);
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
  color: var(--cf-primary);
  text-transform: capitalize;
}

.meta-badge.source {
  background: rgba(144, 147, 153, 0.12);
  color: var(--cf-fg-2);
}

.list-item-content {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--cf-fg);
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
  color: var(--cf-fg-2);
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
  color: var(--cf-primary);
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
  color: var(--cf-primary-hover);
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
  background: var(--cf-s1);
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
  color: var(--cf-fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: move;
  transition: all 0.2s ease;
}

.card-drag-handle:hover {
  background: rgba(107, 115, 255, 0.15);
  color: var(--cf-primary);
}

.card-title {
  font-size: 17px;
  font-weight: 700;
  color: var(--cf-fg);
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
  color: var(--cf-primary);
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
  background: var(--cf-primary-fill);
  color: var(--cf-primary-fg);
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
  color: var(--cf-primary);
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.type-pill {
  text-transform: capitalize;
}

.source-pill {
  background: rgba(144, 147, 153, 0.12);
  border-color: rgba(144, 147, 153, 0.18);
  color: var(--cf-fg-2);
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
  color: var(--cf-fg-2);
  font-weight: 600;
}

.section-label .el-icon {
  font-size: 16px;
  color: var(--cf-primary);
}

.url-box,
.content-box {
  background: rgba(107, 115, 255, 0.06);
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-radius: var(--rule-radius-md, 16px);
  padding: 12px 14px;
  font-size: 13px;
  color: var(--cf-fg);
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
  color: var(--cf-primary);
}

.card-btn.ghost:hover {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  border: 1px solid rgba(155, 143, 255, 0.28);
  color: var(--cf-primary-hover);
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
  background: var(--cf-s2);
}

:deep(.rule-dialog .el-dialog__title) {
  font-size: 20px;
  font-weight: 700;
  color: var(--cf-fg);
  }

:deep(.rule-dialog .el-dialog__body) {
  padding: 28px 32px;
  background: var(--cf-s2);
}

:deep(.rule-dialog .el-dialog__footer) {
  padding: 20px 32px;
  border-top: 1px solid rgba(107, 115, 255, 0.1);
  background: var(--cf-s2);
}

.dialog-card {
  background: var(--cf-s1);
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
  color: var(--cf-fg-2);
}

.helper-text {
  margin-top: 8px;
  color: var(--cf-fg-2);
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
  color: var(--cf-fg-2);
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
  color: var(--cf-primary);
}

.footer-btn.primary {
  background: var(--cf-s2);
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
