<template>
  <div class="rules-page">
    <div class="page-header">
      <div class="title-block">
        <h2>规则配置</h2>
        <p>管理您的规则和规则集</p>
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
          class="action-btn action-primary"
          @click="showAddRuleDialog"
        >
          <el-icon><Plus /></el-icon>
          添加规则
        </el-button>
        <el-button
          class="action-btn action-primary"
          @click="showAddRuleSetDialog"
        >
          <el-icon><Plus /></el-icon>
          添加规则集
        </el-button>
        <el-button
          type="primary"
          class="action-btn action-secondary"
          @click="handleShowRuleIndex"
        >
          <el-icon><Search /></el-icon>
          规则索引
        </el-button>
      </div>
    </div>

    <div v-if="allRulesAndSets.length === 0" class="empty-state">
      <el-empty description="暂无规则，请添加规则或规则集" />
    </div>

    <!-- 列表视图 -->
    <div v-else-if="viewMode === 'list'" class="rules-list" id="sortable-rules" ref="rulesContainer">
      <div
        v-for="item in allRulesAndSets"
        :key="item.uniqueId"
        class="list-item-wrapper"
        :data-id="item.uniqueId"
      >
        <!-- 分组列表项（收起状态） -->
        <div
          v-if="item.isGroup"
          class="list-item group-item"
          @click="toggleGroup(item.groupId)"
        >
          <div class="list-item-drag">
            <button class="card-drag-handle" type="button" @click.stop>
              <el-icon><DCaret /></el-icon>
            </button>
          </div>
          <div class="list-item-info">
            <div class="list-item-name">{{ item.groupName || `${item.count} 个规则集` }}</div>
            <div class="list-item-meta">
              <span class="meta-badge group">规则集组</span>
              <span v-if="item.groupName" class="meta-badge count">{{ item.count }} 个</span>
              <span class="meta-badge policy">{{ item.policy }}</span>
            </div>
          </div>
          <div class="list-item-actions">
            <el-button class="list-btn" size="small" @click.stop="showGroupRenameDialog(item)" title="重命名">
              <el-icon><Edit /></el-icon>
            </el-button>
            <button class="expand-btn" @click.stop="toggleGroup(item.groupId)">
              <el-icon><ArrowDown /></el-icon>
            </button>
          </div>
        </div>

        <!-- 普通列表项 -->
        <div
          v-else
          :class="[
            'list-item',
            item.itemType === 'rule' ? 'rule-type' : 'ruleset-type',
            item.isExpandedGroupItem ? 'expanded-group-item' : '',
            { disabled: !item.enabled }
          ]"
        >
          <div class="list-item-drag">
            <button class="card-drag-handle" type="button">
              <el-icon><DCaret /></el-icon>
            </button>
          </div>
          <div class="list-item-type">
            <span class="type-badge" :class="item.itemType">
              {{ item.itemType === 'rule' ? '规则' : '规则集' }}
            </span>
            <!-- 展开组的第一个项显示收起按钮 -->
            <el-button
              v-if="item.isExpandedGroupItem && item.isFirstInGroup"
              class="collapse-btn"
              size="small"
              link
              @click="toggleGroup(item.groupId)"
            >
              <el-icon><ArrowUp /></el-icon>
              收起
            </el-button>
          </div>
          <div class="list-item-content">
            <!-- 规则：规则类型和值 -->
            <template v-if="item.itemType === 'rule'">
              <span class="rule-type-text">{{ item.rule_type }}</span>
              <span class="rule-value-text" :title="item.value">{{ item.value }}</span>
              <span v-if="item.remark" class="rule-remark-badge" :title="item.remark">
                <el-icon><ChatLineSquare /></el-icon>
                {{ item.remark }}
              </span>
            </template>
            <!-- 规则集：规则名 -->
            <template v-else>
              <span class="ruleset-name-text" :title="`类型: ${item.behavior}\nURL: ${item.url}`">{{ item.name }}</span>
              <el-tag v-if="item.library_rule_id" size="small" type="warning" effect="plain" class="library-badge">
                <el-icon><FolderOpened /></el-icon>
              </el-tag>
              <span v-if="item.remark" class="rule-remark-badge" :title="item.remark">
                <el-icon><ChatLineSquare /></el-icon>
                {{ item.remark }}
              </span>
            </template>
          </div>
          <div class="list-item-policy">
            <span class="policy-tag">{{ item.policy }}</span>
          </div>
          <div class="list-item-actions">
            <button class="status-toggle" :class="{ active: item.enabled }" @click="toggleItemStatus(item)">
              <el-icon v-if="item.enabled"><View /></el-icon>
              <el-icon v-else><Hide /></el-icon>
            </button>
            <el-button class="list-btn" size="small" @click="editItem(item)" title="编辑">
              <el-icon><Edit /></el-icon>
            </el-button>
            <el-button class="list-btn danger" size="small" @click="deleteItem(item)" title="删除">
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 卡片视图 -->
    <div v-else class="rules-grid" id="sortable-rules" ref="rulesContainer">
      <div
        v-for="item in allRulesAndSets"
        :key="item.uniqueId"
        class="rule-card-wrapper"
        :data-id="item.uniqueId"
      >
        <!-- 分组卡片（收起状态） -->
        <div
          v-if="item.isGroup"
          class="rule-card group-card"
          @click="toggleGroup(item.groupId)"
        >
          <div class="card-header">
            <div class="card-title-group">
              <button class="card-drag-handle" type="button" @click.stop>
                <el-icon><DCaret /></el-icon>
              </button>
              <div class="card-title">{{ item.groupName || `${item.count} 个规则集` }}</div>
            </div>
            <button class="expand-btn" @click.stop="toggleGroup(item.groupId)">
              <el-icon><ArrowDown /></el-icon>
            </button>
          </div>

          <div class="card-meta">
            <span class="meta-pill group-pill">规则集组</span>
            <span v-if="item.groupName" class="meta-pill count-pill">{{ item.count }} 个</span>
            <span class="meta-pill policy-pill">{{ item.policy }}</span>
          </div>

          <div class="card-actions">
            <el-button class="card-btn" size="small" @click.stop="showGroupRenameDialog(item)">
              <el-icon><Edit /></el-icon>
              重命名
            </el-button>
            <el-button class="card-btn primary" size="small" @click.stop="toggleGroup(item.groupId)">
              查看全部
            </el-button>
          </div>
        </div>

        <!-- 普通单个卡片 -->
        <div
          v-else
          :class="[
            'rule-card',
            item.itemType === 'rule' ? 'rule-type' : 'ruleset-type',
            item.isExpandedGroupItem ? 'expanded-group-item' : '',
            { disabled: !item.enabled }
          ]"
        >
          <!-- 卡片头部 -->
          <div class="card-header">
            <div class="card-title-group">
              <button class="card-drag-handle" type="button">
                <el-icon><DCaret /></el-icon>
              </button>
              <span class="card-type-badge" :class="item.itemType">
                {{ item.itemType === 'rule' ? '规则' : '规则集' }}
              </span>
              <!-- 展开组的第一个项显示收起按钮 -->
              <el-button
                v-if="item.isExpandedGroupItem && item.isFirstInGroup"
                class="collapse-btn"
                size="small"
                link
                @click="toggleGroup(item.groupId)"
              >
                <el-icon><ArrowUp /></el-icon>
                收起
              </el-button>
            </div>
            <button class="status-toggle" :class="{ active: item.enabled }" @click="toggleItemStatus(item)">
              <el-icon v-if="item.enabled"><View /></el-icon>
              <el-icon v-else><Hide /></el-icon>
            </button>
          </div>

          <!-- 卡片主体内容 -->
          <div class="card-body">
            <!-- 规则：规则类型和值在一行显示 -->
            <template v-if="item.itemType === 'rule'">
              <div class="rule-content">
                <span class="rule-type-inline">{{ item.rule_type }}</span>
                <span class="rule-value-inline" :title="item.value">{{ item.value }}</span>
              </div>
              <div v-if="item.remark" class="rule-remark" :title="item.remark">
                <el-icon><ChatLineSquare /></el-icon>
                {{ item.remark }}
              </div>
            </template>

            <!-- 规则集：显示规则名 -->
            <template v-else>
              <div class="ruleset-info">
                <div class="ruleset-name" :title="`类型: ${item.behavior}\nURL: ${item.url}`">
                  {{ item.name }}
                </div>
                <el-tag v-if="item.library_rule_id" size="small" type="warning" effect="plain" class="library-badge">
                  <el-icon><FolderOpened /></el-icon>
                </el-tag>
              </div>
              <div v-if="item.remark" class="rule-remark" :title="item.remark">
                <el-icon><ChatLineSquare /></el-icon>
                {{ item.remark }}
              </div>
            </template>
          </div>

          <!-- 卡片底部 -->
          <div class="card-footer">
            <div class="footer-left">
              <span class="policy-tag">{{ item.policy }}</span>
            </div>
            <div class="footer-actions">
              <el-button class="card-btn ghost" size="small" @click="editItem(item)">
                <el-icon><Edit /></el-icon>
              </el-button>
              <el-button class="card-btn danger" size="small" @click="deleteItem(item)">
                <el-icon><Delete /></el-icon>
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 添加/编辑规则对话框 -->
    <el-dialog v-model="ruleDialogVisible" :title="isEditRule ? '编辑规则' : '添加规则'" width="500px" class="rule-dialog">
      <div class="dialog-card">
        <el-form :model="ruleForm" label-width="80px" class="rule-form">
          <el-form-item label="规则类型">
            <el-select v-model="ruleForm.rule_type">
              <el-option label="DOMAIN" value="DOMAIN" />
              <el-option label="DOMAIN-SUFFIX" value="DOMAIN-SUFFIX" />
              <el-option label="DOMAIN-KEYWORD" value="DOMAIN-KEYWORD" />
              <el-option label="IP-CIDR" value="IP-CIDR" />
              <el-option label="IP-CIDR6" value="IP-CIDR6" />
              <el-option label="IP-SUFFIX" value="IP-SUFFIX" />
              <el-option label="DST-PORT" value="DST-PORT" />
              <el-option label="SRC-PORT" value="SRC-PORT" />
              <el-option label="GEOIP" value="GEOIP" />
              <el-option label="GEOSITE" value="GEOSITE" />
              <el-option label="RULE-SET" value="RULE-SET" />
              <el-option label="AND" value="AND" />
              <el-option label="OR" value="OR" />
              <el-option label="NOT" value="NOT" />
              <el-option label="MATCH" value="MATCH" />
            </el-select>
          </el-form-item>
          <el-form-item label="值">
            <el-input v-model="ruleForm.value" placeholder="域名、IP或规则集名称" />
          </el-form-item>
          <el-form-item label="策略">
            <el-select v-model="ruleForm.policy" placeholder="选择策略" style="width: 100%">
              <el-option
                v-for="policy in availablePolicies"
                :key="policy"
                :label="policy"
                :value="policy"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="ruleForm.remark" placeholder="可选，添加备注说明" />
          </el-form-item>
          <el-form-item label="no-resolve" v-if="isIpRuleType">
            <div class="switch-with-tip">
              <el-switch v-model="ruleForm.no_resolve" />
              <span class="form-tip">IP 类规则建议开启</span>
            </div>
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="ruleForm.enabled" />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="ruleDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveRule">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 添加/编辑规则集对话框 -->
    <el-dialog v-model="ruleSetDialogVisible" :title="isEditRuleSet ? '编辑规则集' : '添加规则集'" width="500px" class="rule-dialog">
      <div class="dialog-card">
        <el-form :model="ruleSetForm" label-width="80px" class="rule-form">
          <el-form-item label="选择规则">
            <el-select
              v-model="selectedLibraryRule"
              placeholder="从规则仓库选择（可选）"
              style="width: 100%"
              filterable
              clearable
              @change="onLibraryRuleSelect"
              @clear="onLibraryRuleClear"
            >
              <el-option
                v-for="rule in enabledLibraryRules"
                :key="rule.id"
                :label="rule.name"
                :value="rule.id"
              >
                <div style="display: flex; justify-content: space-between;">
                  <span>{{ rule.name }}</span>
                  <el-tag size="small" type="info">{{ rule.behavior }}</el-tag>
                </div>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item label="名称">
            <el-input v-model="ruleSetForm.name" placeholder="规则集名称" :disabled="!!selectedLibraryRule" />
          </el-form-item>
          <el-form-item label="URL">
            <el-input v-model="ruleSetForm.url" placeholder="规则集URL地址" :disabled="!!selectedLibraryRule" />
          </el-form-item>
          <el-form-item label="类型">
            <el-select v-model="ruleSetForm.behavior" style="width: 100%" :disabled="!!selectedLibraryRule">
              <el-option label="Domain" value="domain" />
              <el-option label="IP CIDR" value="ipcidr" />
              <el-option label="Classical" value="classical" />
            </el-select>
          </el-form-item>
          <el-form-item label="策略">
            <el-select v-model="ruleSetForm.policy" placeholder="选择策略" style="width: 100%">
              <el-option
                v-for="policy in availablePolicies"
                :key="policy"
                :label="policy"
                :value="policy"
              />
            </el-select>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="ruleSetForm.remark" placeholder="可选，添加备注说明" />
          </el-form-item>
          <el-form-item label="no-resolve" v-if="ruleSetForm.behavior === 'ipcidr'">
            <div class="switch-with-tip">
              <el-switch v-model="ruleSetForm.no_resolve" />
              <span class="form-tip">IP CIDR 类规则集建议开启</span>
            </div>
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="ruleSetForm.enabled" @change="handleRuleSetStatusChange" />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="ruleSetDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveRuleSet">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 规则集组重命名对话框 -->
    <el-dialog v-model="groupRenameDialogVisible" title="重命名规则集组" width="450px" class="rule-dialog">
      <div class="dialog-card">
        <el-form :model="groupRenameForm" label-width="70px" class="rule-form">
          <el-form-item label="组名称">
            <el-input
              v-model="groupRenameForm.groupName"
              placeholder="输入名称，留空显示数量"
              clearable
            />
          </el-form-item>
        </el-form>
        <div class="rename-tip">
          <el-icon><InfoFilled /></el-icon>
          <span>留空将显示默认名称</span>
        </div>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="groupRenameDialogVisible = false">取消</el-button>
          <el-button class="footer-btn primary" type="primary" @click="saveGroupName">保存</el-button>
        </div>
      </template>
    </el-dialog>

    <!-- 规则索引对话框 -->
    <el-dialog v-model="ruleIndexDialogVisible" title="规则索引" width="600px" class="rule-dialog">
      <div class="dialog-card">
        <el-alert
          title="规则索引"
          type="info"
          :closable="false"
          style="margin-bottom: 16px"
        >
          <div style="font-size: 13px; margin-top: 8px;">
            输入域名或IP地址，查询会匹配到哪条规则
          </div>
        </el-alert>

        <el-form label-width="80px">
          <el-form-item label="查询内容">
            <el-input
              v-model="ruleIndexQuery"
              placeholder="请输入域名（如：google.com）或IP（如：192.168.1.1）"
              @keyup.enter="performRuleIndexQuery"
              clearable
            />
          </el-form-item>
        </el-form>

        <div v-if="ruleIndexResult" style="margin-top: 20px;">
          <!-- 匹配成功 -->
          <el-result
            v-if="ruleIndexResult.matched"
            icon="success"
            title="匹配成功"
            style="padding: 20px 0;"
          >
            <template #sub-title>
              <div style="font-size: 14px; line-height: 1.8;">
                <div style="margin-bottom: 12px;">
                  <strong style="font-size: 16px; color: #303133;">{{ ruleIndexResult.rule_name }}</strong>
                </div>
                <div style="display: flex; flex-direction: column; gap: 8px; text-align: left; max-width: 500px; margin: 0 auto;">
                  <div>
                    <span style="color: #909399;">规则类型：</span>
                    <el-tag :type="ruleIndexResult.rule_type === 'rule' ? 'primary' : 'success'" size="small">
                      {{ ruleIndexResult.rule_type === 'rule' ? '直接规则' : '规则集' }}
                    </el-tag>
                  </div>
                  <div>
                    <span style="color: #909399;">匹配规则：</span>
                    <el-tag type="info" size="small" style="font-family: monospace;">
                      {{ ruleIndexResult.matched_line }}
                    </el-tag>
                  </div>
                  <div>
                    <span style="color: #909399;">执行策略：</span>
                    <el-tag
                      :type="ruleIndexResult.policy === 'DIRECT' ? 'success' : ruleIndexResult.policy === 'REJECT' ? 'danger' : 'primary'"
                      size="small"
                    >
                      {{ ruleIndexResult.policy }}
                    </el-tag>
                  </div>
                  <div>
                    <span style="color: #909399;">规则来源：</span>
                    <span>{{ ruleIndexResult.source }}</span>
                  </div>
                  <div>
                    <span style="color: #909399;">优先级：</span>
                    <span>第 {{ ruleIndexResult.priority }} 条规则</span>
                  </div>
                  <div>
                    <span style="color: #909399;">Behavior：</span>
                    <el-tag type="info" size="small">{{ ruleIndexResult.behavior }}</el-tag>
                  </div>
                  <div v-if="ruleIndexResult.elapsed_time !== undefined">
                    <span style="color: #909399;">索引耗时：</span>
                    <el-tag type="warning" size="small">{{ ruleIndexResult.elapsed_time }} ms</el-tag>
                  </div>
                </div>
              </div>
            </template>
          </el-result>

          <!-- 未匹配 -->
          <el-result
            v-else
            icon="warning"
            title="未匹配任何规则"
            style="padding: 20px 0;"
          >
            <template #sub-title>
              <div style="font-size: 14px; line-height: 1.8;">
                <div style="margin-bottom: 8px;">{{ ruleIndexResult.message }}</div>
                <div v-if="ruleIndexResult.elapsed_time !== undefined" style="color: #909399;">
                  索引耗时：<el-tag type="warning" size="small">{{ ruleIndexResult.elapsed_time }} ms</el-tag>
                </div>
              </div>
            </template>
          </el-result>
        </div>
      </div>
      <template #footer>
        <div class="dialog-footer">
          <el-button class="footer-btn ghost" @click="ruleIndexDialogVisible = false">关闭</el-button>
          <el-button class="footer-btn primary" type="primary" @click="performRuleIndexQuery" :loading="ruleIndexLoading">
            查询
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, onActivated, computed, nextTick, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { DCaret, Edit, Delete, FolderOpened, ArrowUp, ArrowDown, Search, Plus, View, Hide, InfoFilled, List, Grid, ChatLineSquare } from '@element-plus/icons-vue'
import { ruleApi, ruleSetApi, proxyGroupApi } from '@/api'
import type { Rule, RuleSet, ProxyGroup } from '@/types'
import Sortable from 'sortablejs'
import api from '@/api'

const allRules = ref<any[]>([])  // 包含规则和规则集的合并数组
const proxyGroups = ref<ProxyGroup[]>([])
const ruleLibrary = ref<any[]>([])  // 规则仓库
const selectedLibraryRule = ref('')  // 选中的规则仓库项ID
const ruleDialogVisible = ref(false)
const ruleSetDialogVisible = ref(false)
const isEditRule = ref(false)
const isEditRuleSet = ref(false)
const viewMode = ref<'list' | 'grid'>('grid')  // 默认卡片视图
const rulesContainer = ref<HTMLElement | null>(null)

// 处理按钮点击
const handleShowRuleIndex = () => {
  showRuleIndexDialog()
}

// 规则索引相关
const ruleIndexDialogVisible = ref(false)
const ruleIndexQuery = ref('')
const ruleIndexResult = ref<any>(null)
const ruleIndexLoading = ref(false)
const isSavingOrder = ref(false) // 防止并发保存排序

const ruleForm = ref<Partial<Rule>>({
  rule_type: 'DOMAIN-SUFFIX',
  value: '',
  policy: 'DIRECT',
  enabled: true,
  remark: '',
  no_resolve: false
})

const ruleSetForm = ref<Partial<RuleSet>>({
  name: '',
  url: '',
  behavior: 'classical',
  policy: 'DIRECT',
  enabled: true,
  library_rule_id: '',  // 关联的规则仓库ID
  remark: '',
  no_resolve: false
})

// 可用的策略选项：DIRECT、REJECT和所有策略组
const availablePolicies = computed(() => {
  const policies = ['DIRECT', 'REJECT']
  const groupNames = proxyGroups.value.map(g => g.name)
  return [...policies, ...groupNames]
})

// 判断当前规则类型是否为 IP 类型（需要 no-resolve）
const isIpRuleType = computed(() => {
  return ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP'].includes(ruleForm.value.rule_type || '')
})

// 展开的组ID集合
const expandedGroups = ref<Set<string>>(new Set())

// 切换组的展开/收起状态
const toggleGroup = (groupId: string) => {
  if (expandedGroups.value.has(groupId)) {
    expandedGroups.value.delete(groupId)
  } else {
    expandedGroups.value.add(groupId)
  }
}

// 规则集组重命名相关
const groupRenameDialogVisible = ref(false)
const groupRenameForm = ref({
  groupId: '',
  groupName: '',
  items: [] as any[]
})

// 显示重命名对话框
const showGroupRenameDialog = (group: any) => {
  groupRenameForm.value = {
    groupId: group.groupId,
    groupName: group.groupName || '',
    items: group.items
  }
  groupRenameDialogVisible.value = true
}

// 保存规则集组名称
const saveGroupName = async () => {
  try {
    const newGroupName = groupRenameForm.value.groupName.trim()
    // 更新组内所有规则集的 group_name 字段
    for (const item of groupRenameForm.value.items) {
      const updatedItem = { ...item, group_name: newGroupName }
      delete updatedItem.uniqueId // 移除前端添加的字段
      await ruleSetApi.update(item.id, updatedItem)
    }
    ElMessage.success('重命名成功')
    groupRenameDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    ElMessage.error('重命名失败')
  }
}

// 分组并合并连续相同策略的规则
const allRulesAndSets = computed(() => {
  const result: any[] = []
  let currentGroup: any = null

  allRules.value.forEach((item, index) => {
    const uniqueId = `${item.itemType}-${item.id}`
    const itemWithId = { ...item, uniqueId }

    // 只对规则集（ruleset）进行分组
    if (item.itemType === 'ruleset') {
      // 检查是否可以与当前组合并（相同策略）
      if (currentGroup && currentGroup.policy === item.policy) {
        // 添加到当前组（groupId保持不变，基于第一个成员）
        currentGroup.items.push(itemWithId)
        currentGroup.count++
      } else {
        // 如果有未完成的组，先添加到结果
        if (currentGroup) {
          result.push(currentGroup)
        }
        // 创建新组，使用成员ID作为groupId而不是索引
        const groupId = `group_${item.id}_${item.policy}`
        currentGroup = {
          isGroup: true,
          groupId: groupId,
          policy: item.policy,
          groupName: item.group_name || '',  // 使用第一个规则集的 group_name 作为组名称
          count: 1,
          items: [itemWithId],
          uniqueId: groupId
        }
      }
    } else {
      // 规则（rule）不参与分组
      // 如果有未完成的组，先添加到结果
      if (currentGroup) {
        result.push(currentGroup)
        currentGroup = null
      }
      // 直接添加规则
      result.push(itemWithId)
    }
  })

  // 添加最后一个组（如果有）
  if (currentGroup) {
    result.push(currentGroup)
  }

  // 处理分组展开：如果组已展开，则将组内项目展开成独立卡片
  const finalResult: any[] = []
  result.forEach(item => {
    if (item.isGroup) {
      // 如果组只有1个规则集，直接显示为单个卡片
      if (item.count === 1) {
        finalResult.push(item.items[0])
      }
      // 如果组已展开，将所有项目展开
      else if (expandedGroups.value.has(item.groupId)) {
        // 给展开的项目添加分组信息，用于显示收起按钮
        item.items.forEach((subItem: any, index: number) => {
          finalResult.push({
            ...subItem,
            isExpandedGroupItem: true,
            groupId: item.groupId,
            groupPolicy: item.policy,
            isFirstInGroup: index === 0,
            isLastInGroup: index === item.items.length - 1
          })
        })
      }
      // 否则显示为折叠的组
      else {
        finalResult.push(item)
      }
    } else {
      finalResult.push(item)
    }
  })

  return finalResult
})

// 过滤出已启用的规则仓库项，并排除已被使用的
const enabledLibraryRules = computed(() => {
  // 获取所有已使用的规则仓库ID（规则集中关联的）
  const usedLibraryIds = new Set(
    allRules.value
      .filter(item => item.itemType === 'ruleset' && item.library_rule_id)
      .map(item => item.library_rule_id)
  )

  // 编辑模式下，当前规则关联的ID应该保留（允许保持当前选择）
  if (isEditRuleSet.value && ruleSetForm.value.library_rule_id) {
    usedLibraryIds.delete(ruleSetForm.value.library_rule_id)
  }

  // 过滤：启用的 + 未被使用的
  return ruleLibrary.value.filter(rule =>
    rule.enabled && !usedLibraryIds.has(rule.id)
  )
})

const loadAllRules = async () => {
  try {
    const { data } = await ruleApi.getAll()
    allRules.value = data
  } catch (error) {
    ElMessage.error('加载规则列表失败')
  }
}

const loadProxyGroups = async () => {
  try {
    const { data } = await proxyGroupApi.getAll()
    proxyGroups.value = data
  } catch (error) {
    ElMessage.error('加载策略组列表失败')
  }
}

const loadRuleLibrary = async () => {
  try {
    const { data } = await api.get('/rule-library')
    ruleLibrary.value = data
  } catch (error) {
    ElMessage.error('加载规则仓库失败')
  }
}

const showAddRuleDialog = () => {
  isEditRule.value = false
  ruleForm.value = {
    id: `rule_${Date.now()}`,
    rule_type: 'DOMAIN-SUFFIX',
    value: '',
    policy: 'DIRECT',
    enabled: true,
    remark: '',
    no_resolve: false,  // DOMAIN-SUFFIX 不需要 no-resolve
    itemType: 'rule'
  }
  ruleDialogVisible.value = true
}

const editRule = (row: Rule) => {
  isEditRule.value = true
  ruleForm.value = { ...row }
  // 如果没有 no_resolve 字段，根据规则类型设置默认值
  if (ruleForm.value.no_resolve === undefined || ruleForm.value.no_resolve === null) {
    const isIpType = ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP'].includes(ruleForm.value.rule_type || '')
    ruleForm.value.no_resolve = isIpType
  }
  ruleDialogVisible.value = true
}

const saveRule = async () => {
  try {
    const ruleData = { ...ruleForm.value, itemType: 'rule' }
    if (isEditRule.value) {
      await ruleApi.update(ruleData.id!, ruleData)
      ElMessage.success('更新成功')
    } else {
      await ruleApi.create(ruleData)
      ElMessage.success('添加成功')
    }
    ruleDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const deleteRule = async (row: any) => {
  try {
    await ruleApi.delete(row.id)

    // 同步更新 MosDNS 配置，移除对该规则的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

      // 从 direct_rules 和 proxy_rules 中移除该规则 ID
      const updatedDirectRules = mosdnsConfig.direct_rules.filter((id: string) => id !== row.id)
      const updatedProxyRules = mosdnsConfig.proxy_rules.filter((id: string) => id !== row.id)

      // 如果有变化，保存更新
      if (updatedDirectRules.length !== mosdnsConfig.direct_rules.length ||
          updatedProxyRules.length !== mosdnsConfig.proxy_rules.length) {
        await api.post('/mosdns/rulesets', {
          direct_rulesets: mosdnsConfig.direct_rulesets,
          proxy_rulesets: mosdnsConfig.proxy_rulesets,
          direct_rules: updatedDirectRules,
          proxy_rules: updatedProxyRules
        })
        console.log('已同步更新 MosDNS 配置，移除了对规则的引用')
      }
    } catch (error) {
      console.error('同步更新 MosDNS 配置失败:', error)
      // 不阻断删除操作，只记录警告
      ElMessage.warning('规则已删除，但 MosDNS 配置同步失败，请手动检查')
    }

    ElMessage.success('删除成功')
    loadAllRules()
  } catch (error) {
    ElMessage.error('删除失败')
  }
}

const showAddRuleSetDialog = () => {
  isEditRuleSet.value = false
  selectedLibraryRule.value = ''  // 清空选中的规则
  ruleSetForm.value = {
    id: `ruleset_${Date.now()}`,
    name: '',
    url: '',
    behavior: 'classical',
    policy: 'DIRECT',
    enabled: true,
    library_rule_id: '',
    remark: '',
    no_resolve: false,  // classical 不需要 no-resolve
    itemType: 'ruleset'
  }
  ruleSetDialogVisible.value = true
}

// 规则仓库选择处理
const onLibraryRuleSelect = (libraryRuleId: string) => {
  const selectedRule = ruleLibrary.value.find(r => r.id === libraryRuleId)
  if (selectedRule) {
    ruleSetForm.value.name = selectedRule.name
    ruleSetForm.value.behavior = selectedRule.behavior
    ruleSetForm.value.library_rule_id = libraryRuleId  // 保存规则仓库ID
    // 根据 behavior 自动设置 no_resolve
    ruleSetForm.value.no_resolve = selectedRule.behavior === 'ipcidr'

    // 根据规则来源类型设置 URL
    if (selectedRule.source_type === 'content') {
      // 规则内容类型，使用内容接口
      const baseUrl = `${window.location.protocol}//${window.location.host}`
      ruleSetForm.value.url = `${baseUrl}/api/rule-library/content/${libraryRuleId}`
    } else {
      // URL 类型，使用原始 URL
      ruleSetForm.value.url = selectedRule.url
    }
  }
}

// 规则仓库清除处理
const onLibraryRuleClear = () => {
  // 清除后允许手动输入
  ruleSetForm.value.name = ''
  ruleSetForm.value.url = ''
  ruleSetForm.value.behavior = 'classical'
  ruleSetForm.value.library_rule_id = ''  // 清除关联
  ruleSetForm.value.no_resolve = false  // classical 不需要 no-resolve
}

const editRuleSet = (row: RuleSet) => {
  isEditRuleSet.value = true
  ruleSetForm.value = { ...row }
  // 如果没有 no_resolve 字段，根据 behavior 设置默认值
  if (ruleSetForm.value.no_resolve === undefined || ruleSetForm.value.no_resolve === null) {
    ruleSetForm.value.no_resolve = ruleSetForm.value.behavior === 'ipcidr'
  }

  // 如果有关联的规则仓库ID，则反显
  if (row.library_rule_id) {
    selectedLibraryRule.value = row.library_rule_id
  } else {
    selectedLibraryRule.value = ''
  }

  ruleSetDialogVisible.value = true
}

const saveRuleSet = async () => {
  try {
    const ruleSetData = { ...ruleSetForm.value, itemType: 'ruleset' }
    if (isEditRuleSet.value) {
      // 检查策略是否发生变化，如果变化则清除 group_name
      const originalItem = allRules.value.find(r => r.id === ruleSetData.id && r.itemType === 'ruleset')
      if (originalItem && originalItem.policy !== ruleSetData.policy) {
        // 策略变化，清除组名称
        ruleSetData.group_name = ''
      }
      await ruleSetApi.update(ruleSetData.id!, ruleSetData)
      ElMessage.success('更新成功')
    } else {
      await ruleSetApi.create(ruleSetData)
      ElMessage.success('添加成功')
    }
    ruleSetDialogVisible.value = false
    loadAllRules()
  } catch (error) {
    ElMessage.error('保存失败')
  }
}

const deleteRuleSet = async (row: any) => {
  try {
    await ruleSetApi.delete(row.id)

    // 同步更新 MosDNS 配置，移除对该规则集的引用
    try {
      const { data: mosdnsConfig } = await api.get('/mosdns/rulesets')

      // 从 direct_rulesets 和 proxy_rulesets 中移除该规则集 ID
      const updatedDirectRulesets = mosdnsConfig.direct_rulesets.filter((id: string) => id !== row.id)
      const updatedProxyRulesets = mosdnsConfig.proxy_rulesets.filter((id: string) => id !== row.id)

      // 如果有变化，保存更新
      if (updatedDirectRulesets.length !== mosdnsConfig.direct_rulesets.length ||
          updatedProxyRulesets.length !== mosdnsConfig.proxy_rulesets.length) {
        await api.post('/mosdns/rulesets', {
          direct_rulesets: updatedDirectRulesets,
          proxy_rulesets: updatedProxyRulesets,
          direct_rules: mosdnsConfig.direct_rules,
          proxy_rules: mosdnsConfig.proxy_rules
        })
        console.log('已同步更新 MosDNS 配置，移除了对规则集的引用')
      }
    } catch (error) {
      console.error('同步更新 MosDNS 配置失败:', error)
      // 不阻断删除操作，只记录警告
      ElMessage.warning('规则集已删除，但 MosDNS 配置同步失败，请手动检查')
    }

    ElMessage.success('删除成功')
    loadAllRules()
  } catch (error) {
    ElMessage.error('删除失败')
  }
}

// 统一的编辑方法
const editItem = (row: any) => {
  if (row.itemType === 'rule') {
    editRule(row)
  } else {
    editRuleSet(row)
  }
}

// 统一的删除方法
const deleteItem = (row: any) => {
  if (row.itemType === 'rule') {
    deleteRule(row)
  } else {
    deleteRuleSet(row)
  }
}

// 判断是否为完整URL（http/https开头）
const isFullUrl = (url: string): boolean => {
  if (!url) return false
  return url.startsWith('http://') || url.startsWith('https://')
}

// 测试URL的连通性
const testUrlConnectivity = async (url: string): Promise<boolean> => {
  try {
    // 只测试完整URL，排除相对路径
    if (!isFullUrl(url)) {
      return true  // 相对路径不测试，默认认为可用
    }

    // 调用后端测试接口
    const { data } = await api.post('/rule-library/test-single', { url })
    if (data.success) {
      return data.available
    }
    return false
  } catch (error) {
    return false
  }
}

// 测试单个规则库规则的连通性
const testLibraryRuleConnectivity = async (libraryRuleId: string): Promise<boolean> => {
  try {
    // 查找规则库中的规则
    const libraryRule = ruleLibrary.value.find(r => r.id === libraryRuleId)
    if (!libraryRule) {
      return false
    }

    // 只测试完整URL
    return await testUrlConnectivity(libraryRule.url)
  } catch (error) {
    return false
  }
}

// 处理编辑对话框中规则集状态切换
const handleRuleSetStatusChange = async (enabled: boolean) => {
  // 如果是关闭，直接允许
  if (!enabled) {
    return
  }

  // 如果是开启，需要测试连通性（仅测试完整URL）
  if (ruleSetForm.value.url && isFullUrl(ruleSetForm.value.url)) {
    const loading = ElMessage({
      message: '正在测试规则连通性...',
      duration: 0,
      type: 'info'
    })

    const isAvailable = await testUrlConnectivity(ruleSetForm.value.url)
    loading.close()

    if (!isAvailable) {
      ElMessage.error('规则地址无法访问，无法开启')
      // 自动关闭开关
      ruleSetForm.value.enabled = false
      return
    }
  }
}

// 切换启用/禁用状态
const toggleItemStatus = async (item: any) => {
  // 找到原始数据中的对应项
  const originalItem = allRules.value.find(r => r.id === item.id && r.itemType === item.itemType)
  if (!originalItem) {
    ElMessage.error('未找到对应的规则项')
    return
  }

  // 保存旧状态用于回滚
  const oldEnabled = originalItem.enabled

  // 切换状态
  item.enabled = !item.enabled
  originalItem.enabled = !originalItem.enabled

  // 如果是规则集且正在开启，需要先测试连通性（只测试完整URL）
  if (item.itemType === 'ruleset' && item.enabled) {
    // 如果有URL且是完整URL，进行测试
    if (item.url && isFullUrl(item.url)) {
      const loading = ElMessage({
        message: '正在测试规则连通性...',
        duration: 0,
        type: 'info'
      })

      const isAvailable = await testUrlConnectivity(item.url)
      loading.close()

      if (!isAvailable) {
        ElMessage.error('规则地址无法访问，无法开启')
        // 回滚状态
        item.enabled = oldEnabled
        originalItem.enabled = oldEnabled

        // 如果关联了规则仓库，也更新规则仓库的状态
        if (item.library_rule_id) {
          const libraryRule = ruleLibrary.value.find(r => r.id === item.library_rule_id)
          if (libraryRule) {
            libraryRule.enabled = false
            try {
              await api.put(`/rule-library/${item.library_rule_id}`, libraryRule)
            } catch (error) {
              console.error('更新规则仓库状态失败:', error)
            }
          }
        }

        return
      }
    }
  }

  try {
    if (item.itemType === 'rule') {
      await ruleApi.update(item.id, originalItem)
    } else {
      await ruleSetApi.update(item.id, originalItem)
    }
    ElMessage.success('状态已更新')
  } catch (error) {
    ElMessage.error('更新状态失败')
    // 失败后恢复状态
    item.enabled = oldEnabled
    originalItem.enabled = oldEnabled
  }
}

const initSortable = () => {
  nextTick(() => {
    const container = document.querySelector('#sortable-rules')
    if (container) {
      Sortable.create(container as HTMLElement, {
        handle: '.card-drag-handle',
        animation: 150,
        ghostClass: 'sortable-ghost',
        onEnd: async (evt: any) => {
          const { oldIndex, newIndex } = evt
          if (oldIndex === newIndex) return

          // 获取被拖动的元素对应的数据项（从渲染的列表中）
          const draggedDisplayItem = allRulesAndSets.value[oldIndex]
          const targetDisplayItem = allRulesAndSets.value[newIndex]

          // 检测是否拖动的是展开的规则组成员 - 展开状态下拖动单个成员
          if (draggedDisplayItem.isExpandedGroupItem) {
            // 展开状态下，只拖动单个项目
            const draggedItemInAllRules = allRules.value.findIndex(
              item => item.id === draggedDisplayItem.id && item.itemType === draggedDisplayItem.itemType
            )

            let targetIndexInAllRules = 0
            if (targetDisplayItem) {
              if (targetDisplayItem.isExpandedGroupItem) {
                // 目标是展开组的成员
                targetIndexInAllRules = allRules.value.findIndex(
                  item => item.id === targetDisplayItem.id && item.itemType === targetDisplayItem.itemType
                )
                if (newIndex > oldIndex) {
                  targetIndexInAllRules++
                }
              } else if (targetDisplayItem.isGroup) {
                // 目标是折叠的组
                const targetGroupFirstId = targetDisplayItem.items[0].id
                targetIndexInAllRules = allRules.value.findIndex(item => item.id === targetGroupFirstId)

                if (newIndex > oldIndex) {
                  const targetGroupLastId = targetDisplayItem.items[targetDisplayItem.items.length - 1].id
                  const targetGroupLastIdx = allRules.value.findIndex(item => item.id === targetGroupLastId)
                  targetIndexInAllRules = targetGroupLastIdx + 1
                }
              } else {
                // 目标是普通项
                targetIndexInAllRules = allRules.value.findIndex(
                  item => item.id === targetDisplayItem.id && item.itemType === targetDisplayItem.itemType
                )
                if (newIndex > oldIndex) {
                  targetIndexInAllRules++
                }
              }
            } else {
              targetIndexInAllRules = allRules.value.length
            }

            // 移除并插入单个项目
            const movedItem = allRules.value.splice(draggedItemInAllRules, 1)[0]
            if (draggedItemInAllRules < targetIndexInAllRules) {
              targetIndexInAllRules--
            }
            allRules.value.splice(targetIndexInAllRules, 0, movedItem)
          } else {
            // 处理普通项或折叠的组卡片的拖动

            // 检查拖动的是否是折叠的组
            if (draggedDisplayItem.isGroup) {
              // 拖动折叠的组，需要移动整个组的所有成员
              const groupMembers = draggedDisplayItem.items.map((item: any) => ({
                id: item.id,
                itemType: item.itemType
              }))

              // 在 allRules 中找到这些成员的起始和结束索引
              let groupStartIdx = -1
              let groupEndIdx = -1
              for (let i = 0; i < allRules.value.length; i++) {
                const match = groupMembers.some(
                  member => member.id === allRules.value[i].id && member.itemType === allRules.value[i].itemType
                )
                if (match) {
                  if (groupStartIdx === -1) groupStartIdx = i
                  groupEndIdx = i
                }
              }

              // 提取整个组
              const groupItems = allRules.value.slice(groupStartIdx, groupEndIdx + 1)

              // 计算目标位置
              let targetIndexInAllRules = 0
              if (targetDisplayItem) {
                if (targetDisplayItem.isExpandedGroupItem) {
                  // 目标是展开组的成员
                  const targetGroupFirstId = allRulesAndSets.value
                    .find(item => item.isExpandedGroupItem && item.groupId === targetDisplayItem.groupId)?.id
                  targetIndexInAllRules = allRules.value.findIndex(item => item.id === targetGroupFirstId)

                  if (newIndex > oldIndex) {
                    const targetGroupLastId = allRulesAndSets.value
                      .reverse()
                      .find(item => item.isExpandedGroupItem && item.groupId === targetDisplayItem.groupId)?.id
                    const targetGroupLastIdx = allRules.value.findIndex(item => item.id === targetGroupLastId)
                    targetIndexInAllRules = targetGroupLastIdx + 1
                    allRulesAndSets.value.reverse()
                  }
                } else if (targetDisplayItem.isGroup) {
                  // 目标也是折叠的组
                  const targetGroupFirstId = targetDisplayItem.items[0].id
                  targetIndexInAllRules = allRules.value.findIndex(item => item.id === targetGroupFirstId)

                  if (newIndex > oldIndex) {
                    const targetGroupLastId = targetDisplayItem.items[targetDisplayItem.items.length - 1].id
                    const targetGroupLastIdx = allRules.value.findIndex(item => item.id === targetGroupLastId)
                    targetIndexInAllRules = targetGroupLastIdx + 1
                  }
                } else {
                  // 目标是普通项
                  targetIndexInAllRules = allRules.value.findIndex(
                    item => item.id === targetDisplayItem.id && item.itemType === targetDisplayItem.itemType
                  )
                  if (newIndex > oldIndex) {
                    targetIndexInAllRules++
                  }
                }
              } else {
                targetIndexInAllRules = allRules.value.length
              }

              // 从原位置移除组
              allRules.value.splice(groupStartIdx, groupItems.length)

              // 重新计算插入位置（如果原位置在目标位置之前，需要调整）
              if (groupStartIdx < targetIndexInAllRules) {
                targetIndexInAllRules -= groupItems.length
              }

              // 插入到新位置
              allRules.value.splice(targetIndexInAllRules, 0, ...groupItems)
            } else {
              // 拖动普通项（单个规则或规则集）
              const draggedItemInAllRules = allRules.value.findIndex(
                item => item.id === draggedDisplayItem.id && item.itemType === draggedDisplayItem.itemType
              )

              let targetIndexInAllRules = 0
              if (targetDisplayItem) {
                if (targetDisplayItem.isExpandedGroupItem) {
                  // 目标是展开组的成员，找到该组在 allRules 中的位置
                  const targetGroupFirstId = allRulesAndSets.value
                    .find(item => item.isExpandedGroupItem && item.groupId === targetDisplayItem.groupId)?.id
                  targetIndexInAllRules = allRules.value.findIndex(item => item.id === targetGroupFirstId)

                  if (newIndex > oldIndex) {
                    const targetGroupLastId = allRulesAndSets.value
                      .reverse()
                      .find(item => item.isExpandedGroupItem && item.groupId === targetDisplayItem.groupId)?.id
                    const targetGroupLastIdx = allRules.value.findIndex(item => item.id === targetGroupLastId)
                    targetIndexInAllRules = targetGroupLastIdx + 1
                    allRulesAndSets.value.reverse()
                  }
                } else if (targetDisplayItem.isGroup) {
                  // 目标是折叠的组，找到该组第一个成员在 allRules 中的位置
                  const targetGroupFirstId = targetDisplayItem.items[0].id
                  targetIndexInAllRules = allRules.value.findIndex(item => item.id === targetGroupFirstId)

                  if (newIndex > oldIndex) {
                    const targetGroupLastId = targetDisplayItem.items[targetDisplayItem.items.length - 1].id
                    const targetGroupLastIdx = allRules.value.findIndex(item => item.id === targetGroupLastId)
                    targetIndexInAllRules = targetGroupLastIdx + 1
                  }
                } else {
                  // 目标是普通项
                  targetIndexInAllRules = allRules.value.findIndex(
                    item => item.id === targetDisplayItem.id && item.itemType === targetDisplayItem.itemType
                  )
                  if (newIndex > oldIndex) {
                    targetIndexInAllRules++
                  }
                }
              } else {
                targetIndexInAllRules = allRules.value.length
              }

              // 移除并插入
              const movedItem = allRules.value.splice(draggedItemInAllRules, 1)[0]
              if (draggedItemInAllRules < targetIndexInAllRules) {
                targetIndexInAllRules--
              }
              allRules.value.splice(targetIndexInAllRules, 0, movedItem)
            }
          }

          // 保存新顺序到后端
          try {
            const saved = await saveRulesOrder()
            // 只有在真正执行了保存才显示成功消息
            if (saved) {
              ElMessage.success('排序已更新')
            }
          } catch (error) {
            console.error('保存排序失败:', error)
            ElMessage.error('保存排序失败')
            // 重新加载数据
            loadAllRules()
          }
        }
      })
    }
  })
}

const saveRulesOrder = async (): Promise<boolean> => {
  // 防止并发请求
  if (isSavingOrder.value) {
    console.log('排序保存中，跳过本次请求')
    return false
  }

  // 验证数据
  if (!allRules.value || allRules.value.length === 0) {
    console.warn('没有规则数据，跳过保存')
    return false
  }

  try {
    isSavingOrder.value = true
    // 批量更新规则和规则集顺序（使用合并数组）
    await api.post('/rules/reorder', {
      rule_configs: allRules.value
    })
    return true
  } finally {
    isSavingOrder.value = false
  }
}

// 规则索引相关方法
const showRuleIndexDialog = () => {
  ruleIndexQuery.value = ''
  ruleIndexResult.value = null
  ruleIndexDialogVisible.value = true
}

const performRuleIndexQuery = async () => {
  const query = ruleIndexQuery.value.trim()
  if (!query) {
    ElMessage.warning('请输入域名或IP地址')
    return
  }

  ruleIndexLoading.value = true
  ruleIndexResult.value = null

  try {
    // 规则索引需要更长的超时时间（2分钟），因为需要获取和解析规则集内容
    const { data } = await api.post('/rules/match-test', { query }, {
      timeout: 120000  // 2分钟超时
    })
    if (data.success) {
      ruleIndexResult.value = data
    } else {
      ElMessage.error(data.message || '查询失败')
    }
  } catch (error: any) {
    console.error('Rule index query failed:', error)
    if (error.response?.status === 404) {
      ElMessage.error('该功能需要专业版')
    } else if (error.code === 'ECONNABORTED') {
      ElMessage.error('查询超时，请检查规则集配置是否正确')
    } else {
      ElMessage.error(error.response?.data?.message || '查询失败，请稍后重试')
    }
  } finally {
    ruleIndexLoading.value = false
  }
}

// 监听视图模式切换，重新初始化拖拽
watch(viewMode, () => {
  nextTick(() => {
    initSortable()
  })
})

// 监听规则类型变化，自动设置 no_resolve 默认值
watch(() => ruleForm.value.rule_type, (newType) => {
  // 切换规则类型时，自动设置 no_resolve（IP 类型默认开启）
  const isIpType = ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP'].includes(newType || '')
  ruleForm.value.no_resolve = isIpType
})

// 监听规则集类型变化，自动设置 no_resolve 默认值
watch(() => ruleSetForm.value.behavior, (newBehavior) => {
  // 切换 behavior 时，自动设置 no_resolve（ipcidr 类型默认开启）
  ruleSetForm.value.no_resolve = newBehavior === 'ipcidr'
})

onMounted(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()]).then(() => {
    initSortable()
  })
})

onUnmounted(() => {
})

// 页面激活时重新加载数据（从其他页面返回时）
onActivated(() => {
  Promise.all([loadAllRules(), loadProxyGroups(), loadRuleLibrary()])
})
</script>

<style scoped>
.rules-page {
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

.action-btn:not([disabled]):hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 24px rgba(87, 104, 255, 0.25);
}

:deep(.action-btn .el-icon) {
  font-size: 16px;
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

.rules-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 24px;
}

.rule-card-wrapper {
  cursor: move;
}

.rule-card {
  background: #fff;
  border-radius: var(--rule-radius-lg, 24px);
  padding: 20px;
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 140px;
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

.rule-card.group-card {
  background: linear-gradient(135deg, rgba(139, 143, 255, 0.08) 0%, rgba(139, 143, 255, 0.02) 100%);
  border-color: rgba(139, 143, 255, 0.25);
  cursor: pointer;
}

.rule-card.group-card:hover {
  border-color: rgba(139, 143, 255, 0.45);
  box-shadow: 0 20px 40px rgba(139, 143, 255, 0.2);
}

.rule-card.expanded-group-item {
  border-left: 3px solid #8b8fff;
  background: linear-gradient(90deg, rgba(139, 143, 255, 0.05) 0%, rgba(139, 143, 255, 0.01) 100%);
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
  font-size: 16px;
  font-weight: 700;
  color: #30354d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-type-badge {
  padding: 4px 12px;
  border-radius: var(--rule-radius-pill, 999px);
  font-size: 12px;
  font-weight: 600;
}

.card-type-badge.rule {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.card-type-badge.ruleset {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.18);
}

.collapse-btn {
  font-size: 12px;
  padding: 2px 8px;
  color: #8b8fff;
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

.expand-btn {
  flex-shrink: 0;
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

.expand-btn:hover {
  background: rgba(107, 115, 255, 0.12);
  border-color: rgba(107, 115, 255, 0.35);
  color: #4e5eff;
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
}

.group-pill {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.18);
}

.policy-pill {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.count-pill {
  background: rgba(103, 194, 58, 0.12);
  color: #67c23a;
  border: 1px solid rgba(103, 194, 58, 0.18);
}

.rename-tip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  background: rgba(107, 115, 255, 0.06);
  border-radius: 8px;
  font-size: 13px;
  color: #7f87af;
  margin-top: 8px;
}

.rename-tip .el-icon {
  color: #6b73ff;
  font-size: 16px;
}

.card-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: hidden;
}

.rule-content {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.rule-type-inline {
  font-weight: 600;
  color: #4e5eff;
  white-space: nowrap;
  flex-shrink: 0;
}

.rule-value-inline {
  color: #30354d;
  word-break: break-all;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.rule-remark {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #909399;
  padding: 4px 8px;
  background: rgba(144, 147, 153, 0.08);
  border-radius: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.rule-remark .el-icon {
  font-size: 14px;
  flex-shrink: 0;
}

.ruleset-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ruleset-name {
  font-size: 15px;
  font-weight: 700;
  color: #30354d;
  word-break: break-all;
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}

.library-badge {
  flex-shrink: 0;
}

.card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 12px;
  border-top: 1px solid rgba(107, 115, 255, 0.08);
}

.footer-left {
  display: flex;
  gap: 8px;
  align-items: center;
  flex: 1;
}

.policy-tag {
  padding: 4px 12px;
  border-radius: var(--rule-radius-pill, 999px);
  font-size: 12px;
  font-weight: 600;
  background: rgba(144, 147, 153, 0.12);
  color: #606266;
}

.footer-actions {
  display: flex;
  gap: 8px;
}

.card-actions {
  display: flex;
  gap: 8px;
  margin-top: auto;
  padding-top: 8px;
}

.card-btn {
  flex: 1;
  height: 32px;
  border-radius: var(--rule-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  transition: all 0.2s ease;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: none;
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

.card-btn.primary {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  color: #fff;
  box-shadow: 0 8px 16px rgba(87, 104, 255, 0.25);
}

.card-btn.primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 20px rgba(87, 104, 255, 0.35);
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

.switch-with-tip {
  display: flex;
  align-items: center;
  gap: 12px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
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

/* 视图切换按钮样式 */
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

/* 列表视图样式 */
.rules-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.list-item-wrapper {
  cursor: move;
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

.list-item.group-item {
  background: linear-gradient(135deg, rgba(139, 143, 255, 0.08) 0%, rgba(139, 143, 255, 0.02) 100%);
  border-color: rgba(139, 143, 255, 0.25);
  cursor: pointer;
}

.list-item.group-item:hover {
  border-color: rgba(139, 143, 255, 0.45);
}

.list-item.expanded-group-item {
  border-left: 3px solid #8b8fff;
  background: linear-gradient(90deg, rgba(139, 143, 255, 0.05) 0%, rgba(139, 143, 255, 0.01) 100%);
}

.list-item-drag {
  flex-shrink: 0;
}

.list-item-type {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 80px;
}

.type-badge {
  padding: 4px 12px;
  border-radius: var(--rule-radius-pill, 999px);
  font-size: 12px;
  font-weight: 600;
}

.type-badge.rule {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.type-badge.ruleset {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.18);
}

.list-item-info {
  flex: 1;
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
}

.meta-badge.group {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
}

.meta-badge.policy {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
}

.meta-badge.count {
  background: rgba(103, 194, 58, 0.12);
  color: #67c23a;
}

.list-item-content {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.rule-type-text {
  font-weight: 600;
  color: #4e5eff;
  white-space: nowrap;
  flex-shrink: 0;
}

.rule-value-text {
  color: #30354d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ruleset-name-text {
  font-size: 14px;
  font-weight: 600;
  color: #30354d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rule-remark-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: #909399;
  padding: 2px 8px;
  background: rgba(144, 147, 153, 0.1);
  border-radius: var(--rule-radius-pill, 999px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 150px;
  flex-shrink: 0;
}

.rule-remark-badge .el-icon {
  font-size: 12px;
  flex-shrink: 0;
}

.list-item-policy {
  flex-shrink: 0;
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

@media (max-width: 768px) {
  .rules-page {
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
    gap: 10px;
  }

  .view-toggle {
    order: -1;
    margin-bottom: 8px;
  }

  :deep(.header-actions .el-button + .el-button) {
    margin-left: 0;
  }

  .action-btn {
    flex: 1;
    min-width: calc(50% - 6px);
    justify-content: center;
    box-sizing: border-box;
  }

  .rules-grid {
    grid-template-columns: 1fr;
  }

  .list-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .list-item-type {
    width: 100%;
  }

  .list-item-content {
    width: 100%;
    flex-wrap: wrap;
  }

  .list-item-policy {
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
