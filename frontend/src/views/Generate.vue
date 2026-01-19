<template>
  <div class="generate">
    <div class="page-header">
      <div class="title-block">
        <h2>生成配置</h2>
        <p>生成和管理您的配置文件</p>
      </div>
    </div>

    <div class="config-section">
      <el-row :gutter="20">
        <el-col :xs="24" :sm="12" :md="8">
          <el-card shadow="hover">
            <template #header>
              <h4>Mihomo (Clash Meta)</h4>
            </template>
            <p style="margin-bottom: 16px">生成适用于 Mihomo/Clash Meta 的 YAML 配置文件</p>

            <!-- URL 展示区域 -->
            <div class="config-url-box">
              <el-input
                :model-value="mihomoUrlDisplay"
                readonly
                                size="small"
                placeholder="配置 URL"
              >
                <template #append>
                  <el-button @click="copyUrl(mihomoUrl, 'Mihomo')" size="small">
                    <el-icon><CopyDocument /></el-icon>                  </el-button>
                </template>
              </el-input>
              <div class="url-hint">点击复制，可在客户端中直接订阅此 URL</div>
            </div>

            <el-row :gutter="10">
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="showCustomConfigDialog('mihomo')"
                  style="width: 100%"
                  size="small"
                >
                  <el-icon><Edit /></el-icon>
                  <span>基础</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="previewConfig('mihomo')"
                  :loading="mihomoPreviewLoading"
                  style="width: 100%"
                  size="small"
                >
                  <el-icon><View /></el-icon>
                  <span>预览</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  type="primary"
                  @click="generateMihomo"
                  :loading="mihomoLoading"
                  style="width: 100%"
                  size="small"
                >
                  <el-icon><Download /></el-icon>
                  <span>下载</span>
                </el-button>
              </el-col>
            </el-row>
          </el-card>
        </el-col>

        <el-col :xs="24" :sm="12" :md="8">
          <el-card shadow="hover">
            <template #header>
              <h4>Surge</h4>
            </template>
            <p style="margin-bottom: 16px">生成适用于 Surge 的 .conf 配置文件（INI 格式）</p>

            <!-- URL 展示区域 -->
            <div class="config-url-box">
              <el-input
                :model-value="surgeUrlDisplay"
                readonly
                                size="small"
                placeholder="配置 URL"
              >
                <template #append>
                  <el-button @click="copyUrl(surgeUrl, 'Surge')" size="small">
                    <el-icon><CopyDocument /></el-icon>                  </el-button>
                </template>
              </el-input>
              <div class="url-hint">点击复制，可在客户端中直接订阅此 URL</div>
            </div>

            <el-row :gutter="10">
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="handleSurgeCustomConfig"
                  style="width: 100%"
                  size="small"
                                  >
                  <el-icon><Edit /></el-icon>
                  <span>基础</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="handleSurgePreview"
                  :loading="surgePreviewLoading"
                  style="width: 100%"
                  size="small"
                                  >
                  <el-icon><View /></el-icon>
                  <span>预览</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  type="primary"
                  @click="generateSurge"
                  :loading="surgeLoading"
                  style="width: 100%"
                  size="small"
                >
                  <el-icon><Download /></el-icon>
                  <span>下载</span>
                </el-button>
              </el-col>
            </el-row>
          </el-card>
        </el-col>

        <el-col :xs="24" :sm="12" :md="8">
          <el-card shadow="hover">
            <template #header>
              <h4>MosDNS</h4>
            </template>
            <p style="margin-bottom: 16px">生成适用于 MosDNS 的 YAML 配置文件</p>

            <!-- URL 展示区域 -->
            <div class="config-url-box">
              <el-input
                :model-value="mosdnsUrlDisplay"
                readonly
                                size="small"
                placeholder="配置 URL"
              >
                <template #append>
                  <el-button @click="copyUrl(mosdnsUrl, 'MosDNS')" size="small">
                    <el-icon><CopyDocument /></el-icon>                  </el-button>
                </template>
              </el-input>
              <div class="url-hint">点击复制，可在客户端中直接订阅此 URL</div>
            </div>

            <el-row :gutter="10">
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="showMosdnsSettingsDialog"
                  style="width: 100%"
                  size="small"
                  title="基础设置"
                >
                  <el-icon><Setting /></el-icon>
                  <span>设置</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  @click="previewConfig('mosdns')"
                  :loading="mosdnsPreviewLoading"
                  style="width: 100%"
                  size="small"
                  title="预览配置"
                >
                  <el-icon><View /></el-icon>
                  <span>预览</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8">
                <el-button
                  type="primary"
                  @click="generateMosdns"
                  :loading="mosdnsLoading"
                  style="width: 100%"
                  size="small"
                  title="下载配置"
                >
                  <el-icon><Download /></el-icon>
                  <span>下载</span>
                </el-button>
              </el-col>
            </el-row>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <el-divider />

    <div class="config-section">
      <el-row :gutter="20" class="config-row">
        <el-col :xs="24" :sm="12" :md="8">
          <el-card shadow="hover" class="equal-height-card">
            <template #header>
              <h4>服务配置</h4>
            </template>
            <el-form label-width="100px">
              <el-form-item label="服务域名">
                <el-input
                  v-model="serverDomain"
                  placeholder="http://example.com:5001"
                  @blur="onServerDomainBlur"
                  size="small"
                >
                  <template #append>
                    <el-button @click="resetServerDomain" size="small">
                      <el-icon><Refresh /></el-icon>
                    </el-button>
                  </template>
                </el-input>
                <div class="server-domain-hint">
                  此域名将用于：<br />
                  • 规则仓库内容 URL<br />
                  • MosDNS 规则转换接口<br />
                  • Agent 安装脚本<br />
                  • 配置订阅 URL
                </div>
              </el-form-item>
              <el-form-item label="订阅聚合">
                <div style="display: flex; align-items: center; gap: 8px;">
                  <el-switch
                    v-model="subscriptionAggregationEnabled"
                    @change="onSubscriptionAggregationChange"
                    size="small"
                  />
                </div>
                <div class="server-domain-hint">
                  开启后将在节点管理菜单下显示"订阅聚合"功能，可组合订阅和节点
                </div>
              </el-form-item>
              <el-form-item label="令牌">
                <el-input
                  v-model="configToken"
                  placeholder="留空表示不启用令牌保护，可手动输入或点击生成"
                  size="small"
                                    clearable
                  @clear="onClearToken"
                  @blur="onTokenBlur"
                >
                  <template #append>
                    <el-button @click="generateToken" size="small" title="生成随机令牌">
                      <el-icon><Refresh /></el-icon>
                    </el-button>
                  </template>
                </el-input>
                <div class="server-domain-hint">
                  配置令牌后，外部访问配置 URL 需要添加 ?token=xxx 参数
                </div>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>

        <el-col :xs="24" :sm="12" :md="8">
          <el-card shadow="hover" class="equal-height-card">
            <template #header>
              <h4>配置管理</h4>
            </template>
            <el-row :gutter="8">
              <el-col :xs="24" :sm="12" :md="8" :lg="6">
                <el-button @click="exportConfig" style="width: 100%" size="small">
                  <el-icon><Upload /></el-icon>
                  <span>导出</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8" :lg="6">
                <el-button @click="exportConfigDesensitized" style="width: 100%" size="small">
                  <el-icon><Upload /></el-icon>
                  <span>脱敏</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="8" :lg="6">
                <el-upload
                  :show-file-list="false"
                  :before-upload="importConfig"
                  accept=".json"
                  style="width: 100%"
                >
                  <el-button style="width: 100%" size="small">
                    <el-icon><Download /></el-icon>
                    <span>导入</span>
                  </el-button>
                </el-upload>
              </el-col>
              <el-col :xs="24" :sm="12" :md="12" :lg="6">
                <el-button @click="handleBackup" type="primary" style="width: 100%" size="small">
                  <el-icon><FolderOpened /></el-icon>
                  <span>备份</span>
                </el-button>
              </el-col>
              <el-col :xs="24" :sm="12" :md="12" :lg="6" style="margin-top: 12px">
                <el-button @click="resetConfig" type="danger" style="width: 100%" size="small">
                  <el-icon><RefreshLeft /></el-icon>
                  <span>重置</span>
                </el-button>
              </el-col>
            </el-row>
            <p style="margin-top: 12px; color: #999; font-size: 12px; line-height: 1.4">
              导出配置将保存所有订阅、节点、规则和策略组设置。<br/>
              脱敏导出将隐藏敏感信息。<br/>
              导入配置将覆盖当前所有设置。<br/>
              备份将配置自动上传到远程存储（如 WebDAV）。<br/>
              重置将恢复为默认配置，清空所有数据。
            </p>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <!-- 自定义配置对话框 -->
    <el-dialog
      v-model="customConfigDialogVisible"
      :title="getCustomConfigDialogTitle()"
      width="70%"
      :close-on-click-modal="false"
    >
      <el-alert
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <p>{{ getCustomConfigDialogDesc() }}</p>
        <p style="margin-top: 10px">留空则使用默认基础配置。{{ currentConfigType === 'surge' ? '支持 INI 格式语法' : '支持 YAML 语法高亮' }}</p>
      </el-alert>

      <YamlEditor
        v-model="customConfigContent"
        :placeholder="getCustomConfigPlaceholder()"
      />

      <template #footer>
        <el-button @click="customConfigDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveCustomConfig" :loading="savingCustomConfig">保存</el-button>
      </template>
    </el-dialog>

    <!-- 预览配置对话框 -->
    <el-dialog
      v-model="previewDialogVisible"
      :title="getPreviewDialogTitle()"
      width="80%"
      :close-on-click-modal="false"
    >
      <YamlEditor
        v-model="previewContent"
        :readOnly="true"
      />

      <template #footer>
        <el-button @click="previewDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyPreviewContent">复制到剪贴板</el-button>
      </template>
    </el-dialog>

    <!-- MosDNS 设置对话框 -->
    <el-dialog
      v-model="mosdnsSettingsDialogVisible"
      title="MosDNS 设置"
      width="700px"
      :close-on-click-modal="false"
      class="mosdns-dialog"
    >
      <el-tabs v-model="mosdnsActiveTab">
        <!-- 自定义配置 Tab - 暂时隐藏 -->
        <!-- <el-tab-pane label="自定义配置" name="custom">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>在此编辑 MosDNS 的基础配置。使用 YAML 格式，主要包含 log、data_providers、plugins、servers 等部分。</p>
            <p style="margin-top: 10px">留空则使用默认基础配置。注意插件初始化顺序。</p>
          </el-alert>

          <YamlEditor
            v-model="mosdnsCustomConfig"
            :placeholder="getMosdnsCustomConfigPlaceholder()"
          />
        </el-tab-pane> -->

        <!-- 规则配置 Tab -->
        <el-tab-pane label="规则配置" name="rules">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>选择哪些规则/规则集使用直连 DNS（国内），哪些使用代理 DNS（国外）</p>
            <p style="margin-top: 10px">只有选择的规则和规则集会包含在 MosDNS 配置中</p>
          </el-alert>

          <el-form label-width="120px">
            <el-divider content-position="left">直连规则配置</el-divider>

            <el-form-item label="直连规则集">
              <el-select
                v-model="mosdnsDirectRulesets"
                multiple
                filterable
                placeholder="选择使用国内 DNS 的规则集"
                style="width: 100%"
              >
                <el-option
                  v-for="ruleset in availableRuleSets"
                  :key="ruleset.id"
                  :label="ruleset.name"
                  :value="ruleset.id"
                  :disabled="mosdnsProxyRulesets.includes(ruleset.id)"
                />
              </el-select>
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                这些规则集将使用国内 DNS（与代理规则集互斥）
              </div>
            </el-form-item>

            <el-form-item label="直连规则">
              <el-select
                v-model="mosdnsDirectRules"
                multiple
                filterable
                placeholder="选择使用国内 DNS 的规则"
                style="width: 100%"
              >
                <el-option
                  v-for="rule in availableRules"
                  :key="rule.id"
                  :label="`${rule.rule_type}: ${rule.value} → ${rule.policy}`"
                  :value="rule.id"
                  :disabled="mosdnsProxyRules.includes(rule.id)"
                />
              </el-select>
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                这些单条规则将使用国内 DNS（与代理规则互斥）
              </div>
            </el-form-item>

            <el-divider content-position="left">代理规则配置</el-divider>

            <el-form-item label="代理规则集">
              <el-select
                v-model="mosdnsProxyRulesets"
                multiple
                filterable
                placeholder="选择使用国外 DNS 的规则集"
                style="width: 100%"
              >
                <el-option
                  v-for="ruleset in availableRuleSets"
                  :key="ruleset.id"
                  :label="ruleset.name"
                  :value="ruleset.id"
                  :disabled="mosdnsDirectRulesets.includes(ruleset.id)"
                />
              </el-select>
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                这些规则集将使用国外 DNS（与直连规则集互斥）
              </div>
            </el-form-item>

            <el-form-item label="代理规则">
              <el-select
                v-model="mosdnsProxyRules"
                multiple
                filterable
                placeholder="选择使用国外 DNS 的规则"
                style="width: 100%"
              >
                <el-option
                  v-for="rule in availableRules"
                  :key="rule.id"
                  :label="`${rule.rule_type}: ${rule.value} → ${rule.policy}`"
                  :value="rule.id"
                  :disabled="mosdnsDirectRules.includes(rule.id)"
                />
              </el-select>
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                这些单条规则将使用国外 DNS（与直连规则互斥）
              </div>
            </el-form-item>

            <el-divider content-position="left">自定义 Match</el-divider>

            <el-form-item label="插入位置">
              <div style="width: 100%">
                <el-select v-model="mosdnsCustomMatchPosition" style="width: 100%">
                  <el-option label="优先匹配（在规则匹配之前执行）" value="head" />
                  <el-option label="尾部匹配（在规则匹配之后执行）" value="tail" />
                </el-select>
                <div style="margin-top: 8px; color: #909399; font-size: 12px">
                  选择自定义 match 在自动生成的规则匹配之前或之后执行
                </div>
              </div>
            </el-form-item>

            <el-form-item label="自定义 Match">
              <div style="width: 100%">
                <el-button
                  type="primary"
                  size="small"
                  @click="addMosdnsCustomMatch"
                  style="margin-bottom: 12px"
                >
                  添加匹配项
                </el-button>

                <div
                  v-if="mosdnsCustomMatches.length === 0"
                  style="color: #909399; font-size: 12px; padding: 12px; background: #f5f7fa; border-radius: 4px"
                >
                  暂无自定义 match，可点击上方按钮添加
                </div>

                <div
                  v-for="(item, index) in mosdnsCustomMatches"
                  :key="item.id"
                  class="custom-match-card"
                >
                  <div class="custom-match-header">
                    <div class="custom-match-title">
                      <span>匹配项 {{ index + 1 }}</span>
                      <el-switch v-model="item.enabled" size="small" active-text="启用" inactive-text="禁用" />
                    </div>
                    <div class="custom-match-actions">
                      <el-button-group>
                        <el-button
                          size="small"
                          @click="moveMosdnsCustomMatch(index, 'up')"
                          :disabled="index === 0"
                        >
                          上移
                        </el-button>
                        <el-button
                          size="small"
                          @click="moveMosdnsCustomMatch(index, 'down')"
                          :disabled="index === mosdnsCustomMatches.length - 1"
                        >
                          下移
                        </el-button>
                      </el-button-group>
                      <el-button
                        size="small"
                        type="danger"
                        @click="removeMosdnsCustomMatch(item.id)"
                      >
                        删除
                      </el-button>
                    </div>
                  </div>

                  <div class="custom-match-body">
                    <div class="custom-match-field">
                      <div class="custom-match-label">匹配条件</div>
                      <el-input
                        v-model="item.matchesText"
                        type="textarea"
                        :rows="3"
                        placeholder="每行一个 match 表达式，例如：\nqname $自定义规则集\nresp_ip $国内IP段"
                      />
                      <div class="custom-match-hint">
                        支持 MosDNS `sequence` 的 matches 格式，自动忽略空行
                      </div>
                    </div>

                    <div class="custom-match-field">
                      <div class="custom-match-label">执行动作</div>
                      <el-input
                        v-model="item.exec"
                        placeholder="例如 goto china_dns 或 $proxy_dns"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- DNS 服务器配置 Tab -->
        <el-tab-pane label="DNS 服务器" name="dns">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>配置国内和国外的 DNS 服务器地址</p>
            <p style="margin-top: 10px">支持 UDP、TCP、DoH、DoT 等多种协议格式，每个条目可使用简单模式或 YAML 模式</p>
          </el-alert>

          <el-form label-width="120px">
            <!-- 国内 DNS -->
            <el-form-item label="国内 DNS">
              <div style="width: 100%">
                <el-button
                  type="primary"
                  size="small"
                  @click="addDnsEntry('local')"
                  style="margin-bottom: 12px"
                >
                  添加 DNS 条目
                </el-button>

                <div v-if="mosdnsLocalDnsEntries.length === 0" style="color: #909399; font-size: 12px; padding: 12px; background: #f5f7fa; border-radius: 4px">
                  暂无 DNS 条目，点击上方按钮添加
                </div>

                <div ref="localDnsListRef">
                  <div v-for="(entry, index) in mosdnsLocalDnsEntries" :key="entry.id" class="dns-entry-card">
                  <div class="dns-entry-header">
                    <div class="dns-entry-left">
                      <el-icon class="drag-handle" :size="16" title="拖拽调整顺序">
                        <DCaret />
                      </el-icon>
                      <span class="dns-entry-index">条目 {{ index + 1 }}</span>
                    </div>
                    <div class="dns-entry-actions">
                      <el-button
                        size="small"
                        @click="toggleDnsEntryMode('local', entry.id)"
                      >
                        {{ entry.mode === 'simple' ? '切换到 YAML' : '切换到简单模式' }}
                      </el-button>
                      <el-button
                        size="small"
                        type="danger"
                        @click="removeDnsEntry('local', entry.id)"
                      >
                        删除
                      </el-button>
                    </div>
                  </div>

                  <!-- 简单模式 -->
                  <div v-if="entry.mode === 'simple'" class="dns-entry-content">
                    <el-form-item label="地址" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.addr"
                        placeholder="https://dns.alidns.com/dns-query 或 223.5.5.5"
                        size="small"
                      />
                    </el-form-item>
                    <el-form-item label="Bootstrap" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.bootstrap"
                        placeholder="223.5.5.5（可选）"
                        size="small"
                        :disabled="!isDomainAddr(entry.addr)"
                      />
                      <div v-if="!isDomainAddr(entry.addr) && entry.addr" style="margin-top: 4px; color: #909399; font-size: 11px">
                        仅域名地址或 DoH/DoT 地址需要 Bootstrap
                      </div>
                    </el-form-item>
                    <el-form-item label="Pipeline" label-width="80px" style="margin-bottom: 0">
                      <el-checkbox v-model="entry.enable_pipeline">启用 Pipeline</el-checkbox>
                    </el-form-item>
                  </div>

                  <!-- YAML 模式 -->
                  <div v-else class="dns-entry-content">
                    <el-input
                      v-model="entry.yaml_config"
                      type="textarea"
                      :rows="4"
                      placeholder="- addr: 192.168.1.1:53&#10;  bootstrap: 223.5.5.5&#10;  enable_pipeline: false"
                      size="small"
                      @input="validateYaml(entry)"
                      :class="{ 'yaml-error': entry.yaml_error }"
                    />
                    <div v-if="entry.yaml_error" class="yaml-error-message">
                      {{ entry.yaml_error }}
                    </div>
                    <div v-else-if="entry.yaml_config && entry.yaml_config.trim()" class="yaml-success-message">
                      ✓ YAML 语法正确
                    </div>
                  </div>
                  </div>
                </div>

                <div style="margin-top: 8px; color: #909399; font-size: 12px">
                  直连规则使用的 DNS 服务器
                </div>
              </div>
            </el-form-item>

            <!-- 国外 DNS -->
            <el-form-item label="国外 DNS">
              <div style="width: 100%">
                <el-button
                  type="primary"
                  size="small"
                  @click="addDnsEntry('remote')"
                  style="margin-bottom: 12px"
                >
                  添加 DNS 条目
                </el-button>

                <div v-if="mosdnsRemoteDnsEntries.length === 0" style="color: #909399; font-size: 12px; padding: 12px; background: #f5f7fa; border-radius: 4px">
                  暂无 DNS 条目，点击上方按钮添加
                </div>

                <div ref="remoteDnsListRef">
                  <div v-for="(entry, index) in mosdnsRemoteDnsEntries" :key="entry.id" class="dns-entry-card">
                  <div class="dns-entry-header">
                    <div class="dns-entry-left">
                      <el-icon class="drag-handle" :size="16" title="拖拽调整顺序">
                        <DCaret />
                      </el-icon>
                      <span class="dns-entry-index">条目 {{ index + 1 }}</span>
                    </div>
                    <div class="dns-entry-actions">
                      <el-button
                        size="small"
                        @click="toggleDnsEntryMode('remote', entry.id)"
                      >
                        {{ entry.mode === 'simple' ? '切换到 YAML' : '切换到简单模式' }}
                      </el-button>
                      <el-button
                        size="small"
                        type="danger"
                        @click="removeDnsEntry('remote', entry.id)"
                      >
                        删除
                      </el-button>
                    </div>
                  </div>

                  <!-- 简单模式 -->
                  <div v-if="entry.mode === 'simple'" class="dns-entry-content">
                    <el-form-item label="地址" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.addr"
                        placeholder="https://1.1.1.1/dns-query 或 1.1.1.1"
                        size="small"
                      />
                    </el-form-item>
                    <el-form-item label="Bootstrap" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.bootstrap"
                        placeholder="223.5.5.5（可选）"
                        size="small"
                        :disabled="!isDomainAddr(entry.addr)"
                      />
                      <div v-if="!isDomainAddr(entry.addr) && entry.addr" style="margin-top: 4px; color: #909399; font-size: 11px">
                        仅域名地址或 DoH/DoT 地址需要 Bootstrap
                      </div>
                    </el-form-item>
                    <el-form-item label="Pipeline" label-width="80px" style="margin-bottom: 0">
                      <el-checkbox v-model="entry.enable_pipeline">启用 Pipeline</el-checkbox>
                    </el-form-item>
                  </div>

                  <!-- YAML 模式 -->
                  <div v-else class="dns-entry-content">
                    <el-input
                      v-model="entry.yaml_config"
                      type="textarea"
                      :rows="4"
                      placeholder="- addr: https://1.1.1.1/dns-query&#10;  bootstrap: 223.5.5.5&#10;  enable_pipeline: true"
                      size="small"
                      @input="validateYaml(entry)"
                      :class="{ 'yaml-error': entry.yaml_error }"
                    />
                    <div v-if="entry.yaml_error" class="yaml-error-message">
                      {{ entry.yaml_error }}
                    </div>
                    <div v-else-if="entry.yaml_config && entry.yaml_config.trim()" class="yaml-success-message">
                      ✓ YAML 语法正确
                    </div>
                  </div>
                  </div>
                </div>

                <div style="margin-top: 8px; color: #909399; font-size: 12px">
                  代理规则使用的主 DNS 服务器
                </div>
              </div>
            </el-form-item>

            <!-- Fallback DNS -->
            <el-form-item label="Fallback DNS">
              <div style="width: 100%">
                <el-button
                  type="primary"
                  size="small"
                  @click="addDnsEntry('fallback')"
                  style="margin-bottom: 12px"
                >
                  添加 DNS 条目
                </el-button>

                <div v-if="mosdnsFallbackDnsEntries.length === 0" style="color: #909399; font-size: 12px; padding: 12px; background: #f5f7fa; border-radius: 4px">
                  暂无 DNS 条目，点击上方按钮添加（留空则使用国内 DNS）
                </div>

                <div ref="fallbackDnsListRef">
                  <div v-for="(entry, index) in mosdnsFallbackDnsEntries" :key="entry.id" class="dns-entry-card">
                  <div class="dns-entry-header">
                    <div class="dns-entry-left">
                      <el-icon class="drag-handle" :size="16" title="拖拽调整顺序">
                        <DCaret />
                      </el-icon>
                      <span class="dns-entry-index">条目 {{ index + 1 }}</span>
                    </div>
                    <div class="dns-entry-actions">
                      <el-button
                        size="small"
                        @click="toggleDnsEntryMode('fallback', entry.id)"
                      >
                        {{ entry.mode === 'simple' ? '切换到 YAML' : '切换到简单模式' }}
                      </el-button>
                      <el-button
                        size="small"
                        type="danger"
                        @click="removeDnsEntry('fallback', entry.id)"
                      >
                        删除
                      </el-button>
                    </div>
                  </div>

                  <!-- 简单模式 -->
                  <div v-if="entry.mode === 'simple'" class="dns-entry-content">
                    <el-form-item label="地址" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.addr"
                        placeholder="https://dns.alidns.com/dns-query 或 223.5.5.5"
                        size="small"
                      />
                    </el-form-item>
                    <el-form-item label="Bootstrap" label-width="80px" style="margin-bottom: 12px">
                      <el-input
                        v-model="entry.bootstrap"
                        placeholder="223.5.5.5（可选）"
                        size="small"
                        :disabled="!isDomainAddr(entry.addr)"
                      />
                      <div v-if="!isDomainAddr(entry.addr) && entry.addr" style="margin-top: 4px; color: #909399; font-size: 11px">
                        仅域名地址或 DoH/DoT 地址需要 Bootstrap
                      </div>
                    </el-form-item>
                    <el-form-item label="Pipeline" label-width="80px" style="margin-bottom: 0">
                      <el-checkbox v-model="entry.enable_pipeline">启用 Pipeline</el-checkbox>
                    </el-form-item>
                  </div>

                  <!-- YAML 模式 -->
                  <div v-else class="dns-entry-content">
                    <el-input
                      v-model="entry.yaml_config"
                      type="textarea"
                      :rows="4"
                      placeholder="- addr: 192.168.1.1:53&#10;  bootstrap: 223.5.5.5&#10;  enable_pipeline: false"
                      size="small"
                      @input="validateYaml(entry)"
                      :class="{ 'yaml-error': entry.yaml_error }"
                    />
                    <div v-if="entry.yaml_error" class="yaml-error-message">
                      {{ entry.yaml_error }}
                    </div>
                    <div v-else-if="entry.yaml_config && entry.yaml_config.trim()" class="yaml-success-message">
                      ✓ YAML 语法正确
                    </div>
                  </div>
                  </div>
                </div>

                <div style="margin-top: 8px; color: #909399; font-size: 12px">
                  当国外 DNS 超时时使用的备用 DNS 服务器，留空则复用国内 DNS 配置
                </div>
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 默认转发配置 Tab -->
        <el-tab-pane label="默认转发" name="default">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>当所有规则都不匹配时，使用的默认 DNS 服务器</p>
            <p style="margin-top: 10px">推荐使用"国外 DNS"以避免污染</p>
          </el-alert>

          <el-form label-width="120px">
            <el-form-item label="默认转发">
              <el-radio-group v-model="mosdnsDefaultForward">
                <el-radio value="forward_remote">
                  <span style="font-weight: 500">国外 DNS</span>
                  <span style="margin-left: 8px; color: #909399; font-size: 12px">
                    (推荐) 使用国外 DNS 服务器，避免 DNS 污染
                  </span>
                </el-radio>
                <el-radio value="forward_local" style="margin-top: 12px">
                  <span style="font-weight: 500">国内 DNS</span>
                  <span style="margin-left: 8px; color: #909399; font-size: 12px">
                    使用国内 DNS 服务器，解析速度更快
                  </span>
                </el-radio>
              </el-radio-group>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 自定义 Host 配置 Tab -->
        <el-tab-pane label="自定义 Host" name="hosts">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>配置自定义域名解析，优先级最高</p>
            <p style="margin-top: 10px">格式：每行一个映射，域名在前，IP地址在后，用空格分隔</p>
          </el-alert>

          <el-form label-width="120px">
            <el-form-item label="Hosts 记录">
              <el-input
                v-model="mosdnsCustomHosts"
                type="textarea"
                :rows="10"
                placeholder="每行一个 Host 记录&#10;例如：&#10;localhost 127.0.0.1&#10;myserver.local 192.168.1.100&#10;dns.google 8.8.8.8&#10;cloudflare-dns.com 1.1.1.1"
              />
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                自定义 Host 记录会在所有规则之前优先匹配
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 日志设置 Tab -->
        <el-tab-pane label="日志设置" name="log">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>配置 MosDNS 日志输出级别和文件路径</p>
            <p style="margin-top: 10px">可以控制日志详细程度，帮助排查问题</p>
          </el-alert>

          <el-form label-width="120px">
            <el-form-item label="启用日志">
              <div>
                <el-switch v-model="mosdnsLogEnabled" />
                <div style="margin-top: 8px; color: #909399; font-size: 12px; line-height: 1.5;">
                  关闭日志可以提高性能，但不利于问题排查
                </div>
              </div>
            </el-form-item>

            <el-form-item label="日志级别" v-if="mosdnsLogEnabled">
              <el-select v-model="mosdnsLogLevel" style="width: 100%">
                <el-option label="Debug（调试）" value="debug">
                  <div>
                    <div style="font-weight: 500">Debug</div>
                    <div style="font-size: 12px; color: #909399">最详细的日志，包含所有调试信息</div>
                  </div>
                </el-option>
                <el-option label="Info（信息）" value="info">
                  <div>
                    <div style="font-weight: 500">Info</div>
                    <div style="font-size: 12px; color: #909399">一般信息日志，包含重要操作记录</div>
                  </div>
                </el-option>
                <el-option label="Warn（警告）" value="warn">
                  <div>
                    <div style="font-weight: 500">Warn</div>
                    <div style="font-size: 12px; color: #909399">仅记录警告和错误信息</div>
                  </div>
                </el-option>
                <el-option label="Error（错误）" value="error">
                  <div>
                    <div style="font-weight: 500">Error</div>
                    <div style="font-size: 12px; color: #909399">仅记录错误信息</div>
                  </div>
                </el-option>
              </el-select>
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                推荐使用 Info 级别，调试时可使用 Debug 级别
              </div>
            </el-form-item>

            <el-form-item label="日志文件路径" v-if="mosdnsLogEnabled">
              <el-input
                v-model="mosdnsLogFile"
                placeholder="./mosdns.log"
              />
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                日志文件的保存路径，相对于 MosDNS 配置文件目录
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- API 设置 Tab -->
        <el-tab-pane label="API 设置" name="api">
          <el-alert
            type="info"
            :closable="false"
            style="margin-bottom: 20px"
          >
            <p>配置 MosDNS API 接口，用于监控和管理 MosDNS 服务</p>
            <p style="margin-top: 10px">API 接口可以查询 MosDNS 运行状态和统计信息</p>
          </el-alert>

          <el-form label-width="120px">
            <el-form-item label="启用 API">
              <div>
                <el-switch v-model="mosdnsApiEnabled" />
                <div style="margin-top: 8px; color: #909399; font-size: 12px; line-height: 1.5;">
                  关闭 API 可以减少资源占用，但无法通过 API 查询状态
                </div>
              </div>
            </el-form-item>

            <el-form-item label="API 监听地址" v-if="mosdnsApiEnabled">
              <el-input
                v-model="mosdnsApiAddress"
                placeholder="0.0.0.0:8338"
              />
              <div style="margin-top: 8px; color: #909399; font-size: 12px">
                格式：IP:端口，例如 0.0.0.0:8338 或 127.0.0.1:8338
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>
      </el-tabs>

      <template #footer>
        <el-button @click="mosdnsSettingsDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveMosdnsSettings" :loading="savingMosdnsSettings">保存</el-button>
      </template>
    </el-dialog>

    <!-- 备份配置对话框 -->
    <el-dialog
      v-model="backupDialogVisible"
      title="配置备份"
      width="600px"
      :close-on-click-modal="false"
    >
      <el-alert
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <p>配置 WebDAV 远程备份，自动将配置文件上传到远程存储</p>
        <p style="margin-top: 10px">支持坚果云、Nextcloud 等 WebDAV 服务</p>
      </el-alert>

      <el-form :model="backupForm" label-width="120px">
        <el-form-item label="WebDAV 地址">
          <el-input
            v-model="backupForm.webdav_url"
            placeholder="https://dav.jianguoyun.com/dav/"
          />
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            WebDAV 服务器地址，例如坚果云：https://dav.jianguoyun.com/dav/
          </div>
        </el-form-item>

        <el-form-item label="用户名">
          <el-input
            v-model="backupForm.webdav_username"
            placeholder="WebDAV 用户名/邮箱"
          />
        </el-form-item>

        <el-form-item label="密码">
          <el-input
            v-model="backupForm.webdav_password"
            type="password"
            show-password
            placeholder="WebDAV 密码/应用密码"
          />
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            坚果云需要使用应用密码，不是登录密码
          </div>
        </el-form-item>

        <el-form-item label="备份路径">
          <el-input
            v-model="backupForm.webdav_path"
            placeholder="/config-flow-backup/"
          />
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            远程存储路径，默认为 /config-flow-backup/
          </div>
        </el-form-item>

        <el-form-item label="自动备份">
          <el-switch v-model="backupForm.auto_backup" />
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            开启后每次配置变更时自动备份
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <div style="display: flex; justify-content: space-between; width: 100%">
          <div>
            <el-button @click="testWebDAVConnection" :loading="testingConnection">
              测试连接
            </el-button>
            <el-button @click="backupNow" type="primary" :loading="backingUp">
              立即备份
            </el-button>
          </div>
          <div>
            <el-button @click="backupDialogVisible = false">取消</el-button>
            <el-button type="primary" @click="saveBackupConfig" :loading="savingBackup">
              保存配置
            </el-button>
          </div>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { DCaret, RefreshLeft } from '@element-plus/icons-vue'
import { generateApi, configApi, customConfigApi, subscriptionApi, nodeApi, ruleApi, ruleSetApi, proxyGroupApi, agentApi, serverDomainApi, configTokenApi } from '@/api'
import YamlEditor from '@/components/YamlEditor.vue'
import api from '@/api'
import type { RuleSet } from '@/types'
import * as yaml from 'js-yaml'
import Sortable from 'sortablejs'

// DNS 条目接口定义
interface DnsEntry {
  id: string
  mode: 'simple' | 'yaml'
  addr: string
  bootstrap: string
  enable_pipeline: boolean
  yaml_config: string
  yaml_error?: string  // YAML 验证错误信息
}

interface MosdnsCustomMatchItem {
  id: string
  enabled: boolean
  exec: string
  matchesText: string
}

// 生成唯一 ID
const generateId = (): string => {
  return `dns-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

const generateMatchId = (): string => {
  return `mosdns-match-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

// 判断地址是否需要 Bootstrap
// DoH/DoT/DoQ 等加密 DNS 协议即使使用 IP 地址也需要 Bootstrap
const isDomainAddr = (addr: string): boolean => {
  if (!addr || !addr.trim()) {
    return false
  }

  const trimmedAddr = addr.trim()

  // 如果地址包含 http://, https://, tls://, quic:// 等协议前缀
  // 说明是 DoH/DoT/DoQ 等加密 DNS，需要 Bootstrap
  if (/^(https?|tls|quic):\/\//.test(trimmedAddr)) {
    return true
  }

  // 提取地址中的主机部分（去除协议、端口、路径等）
  let host = trimmedAddr

  // 去除协议前缀
  host = host.replace(/^(https?|tls|quic):\/\//, '')

  // 去除路径部分
  host = host.split('/')[0]

  // 去除端口号
  host = host.split(':')[0]

  // 检查是否为 IPv4 地址（如 1.1.1.1）
  const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/
  if (ipv4Regex.test(host)) {
    return false
  }

  // 检查是否为 IPv6 地址（简化判断，包含多个冒号或方括号）
  if (host.includes('[') || (host.match(/:/g) || []).length > 1) {
    return false
  }

  // 其他情况视为域名（包含字母或以点分隔的多个部分）
  return /[a-zA-Z]/.test(host)
}

// YAML 验证函数
const validateYaml = (entry: DnsEntry): void => {
  if (entry.mode !== 'yaml' || !entry.yaml_config || !entry.yaml_config.trim()) {
    entry.yaml_error = undefined
    return
  }

  try {
    // 尝试解析 YAML
    const parsed = yaml.load(entry.yaml_config)

    // 验证格式：必须是对象（单个DNS条目）或数组（多个DNS条目）
    if (typeof parsed === 'object' && parsed !== null) {
      // 如果是数组，检查第一个元素
      if (Array.isArray(parsed)) {
        if (parsed.length > 0) {
          const firstItem = parsed[0]
          if (typeof firstItem !== 'object' || !firstItem.addr) {
            entry.yaml_error = 'YAML 格式错误：数组项必须包含 addr 字段'
            return
          }
        }
      } else {
        // 如果是对象，必须有 addr 字段
        if (!parsed.addr) {
          entry.yaml_error = 'YAML 格式错误：必须包含 addr 字段'
          return
        }
      }
    } else {
      entry.yaml_error = 'YAML 格式错误：必须是对象或数组'
      return
    }

    entry.yaml_error = undefined
  } catch (error: any) {
    // 提供更有帮助的错误信息
    let errorMsg = error.message || 'YAML 语法错误'

    // 检查常见错误：缺少缩进
    const lines = entry.yaml_config.trim().split('\n')
    if (lines.length > 1 && lines[0].trim().startsWith('-')) {
      // 检查后续行是否缺少缩进
      for (let i = 1; i < lines.length; i++) {
        const line = lines[i]
        if (line.trim() && !line.trim().startsWith('-') && !line.startsWith(' ') && !line.startsWith('\t')) {
          errorMsg = 'YAML 格式错误：第 ' + (i + 1) + ' 行缺少缩进（应该以 2 个空格开头）'
          break
        }
      }
    }

    entry.yaml_error = errorMsg
  }
}

// 文本 → DNS 条目数组
const parseDnsText = (text: string): DnsEntry[] => {
  if (!text || !text.trim()) {
    return []
  }

  const entries: DnsEntry[] = []
  const lines = text.trim().split('\n')
  let currentYamlLines: string[] = []
  let inYamlBlock = false

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const trimmedLine = line.trim()

    // 跳过空行
    if (!trimmedLine) {
      continue
    }

    // 检测 YAML 格式（以 - 开头）
    if (trimmedLine.startsWith('-')) {
      inYamlBlock = true
      currentYamlLines = [line]

      // 收集 YAML 块的所有行（缩进的行属于同一块）
      for (let j = i + 1; j < lines.length; j++) {
        const nextLine = lines[j]
        const nextTrimmed = nextLine.trim()

        // 如果遇到新的 - 开头或非缩进行，停止收集
        if (nextTrimmed.startsWith('-') || (nextTrimmed && !nextLine.startsWith(' ') && !nextLine.startsWith('\t'))) {
          break
        }

        // 如果是空行或缩进行，加入当前块
        if (!nextTrimmed || nextLine.startsWith(' ') || nextLine.startsWith('\t')) {
          currentYamlLines.push(nextLine)
          i = j
        }
      }

      // 创建 YAML 模式条目
      entries.push({
        id: generateId(),
        mode: 'yaml',
        addr: '',
        bootstrap: '',
        enable_pipeline: false,
        yaml_config: currentYamlLines.join('\n')
      })
    } else {
      // 简单格式：addr bootstrap=xxx enable_pipeline=true
      const parts = trimmedLine.split(/\s+/)
      const addr = parts[0] || ''
      let bootstrap = ''
      let enable_pipeline = false

      // 解析参数
      for (let j = 1; j < parts.length; j++) {
        const part = parts[j]
        if (part.startsWith('bootstrap=')) {
          bootstrap = part.substring('bootstrap='.length)
        } else if (part === 'enable_pipeline=true' || part === 'enable_pipeline:true') {
          enable_pipeline = true
        } else if (part === 'enable_pipeline=false' || part === 'enable_pipeline:false') {
          enable_pipeline = false
        }
      }

      entries.push({
        id: generateId(),
        mode: 'simple',
        addr,
        bootstrap,
        enable_pipeline,
        yaml_config: ''
      })
    }
  }

  return entries
}

// DNS 条目数组 → 文本
const dnsEntriesToText = (entries: DnsEntry[]): string => {
  if (!entries || entries.length === 0) {
    return ''
  }

  return entries.map(entry => {
    if (entry.mode === 'yaml') {
      return entry.yaml_config
    } else {
      // 简单模式转换为文本
      const parts = [entry.addr]
      // 只有域名才添加 bootstrap
      if (entry.bootstrap && isDomainAddr(entry.addr)) {
        parts.push(`bootstrap=${entry.bootstrap}`)
      }
      if (entry.enable_pipeline) {
        parts.push('enable_pipeline=true')
      }
      return parts.join(' ')
    }
  }).join('\n')
}

const addMosdnsCustomMatch = () => {
  mosdnsCustomMatches.value.push({
    id: generateMatchId(),
    enabled: true,
    exec: '',
    matchesText: ''
  })
}

const removeMosdnsCustomMatch = (id: string) => {
  mosdnsCustomMatches.value = mosdnsCustomMatches.value.filter(item => item.id !== id)
}

const moveMosdnsCustomMatch = (index: number, direction: 'up' | 'down') => {
  const targetIndex = direction === 'up' ? index - 1 : index + 1
  if (targetIndex < 0 || targetIndex >= mosdnsCustomMatches.value.length) {
    return
  }

  const list = [...mosdnsCustomMatches.value]
  const [item] = list.splice(index, 1)
  list.splice(targetIndex, 0, item)
  mosdnsCustomMatches.value = list
}

const mihomoLoading = ref(false)
const surgeLoading = ref(false)
const mosdnsLoading = ref(false)

const mihomoPreviewLoading = ref(false)
const surgePreviewLoading = ref(false)
const mosdnsPreviewLoading = ref(false)

const customConfigDialogVisible = ref(false)
const customConfigContent = ref('')
const savingCustomConfig = ref(false)
const currentConfigType = ref<'mihomo' | 'surge' | 'mosdns'>('mihomo')

const previewDialogVisible = ref(false)
const previewContent = ref('')
const currentPreviewType = ref<'mihomo' | 'surge' | 'mosdns'>('mihomo')

const mosdnsSettingsDialogVisible = ref(false)
const mosdnsActiveTab = ref('rules')
const mosdnsCustomConfig = ref('')
const mosdnsDirectRulesets = ref<string[]>([])
const mosdnsProxyRulesets = ref<string[]>([])
const mosdnsDirectRules = ref<string[]>([])
const mosdnsProxyRules = ref<string[]>([])
const mosdnsCustomMatches = ref<MosdnsCustomMatchItem[]>([])
const mosdnsCustomMatchPosition = ref<'head' | 'tail'>('tail')
const mosdnsLocalDns = ref('')
const mosdnsRemoteDns = ref('')
const mosdnsFallbackDns = ref('')
// DNS 条目数组（新的数据结构）
const mosdnsLocalDnsEntries = ref<DnsEntry[]>([])
const mosdnsRemoteDnsEntries = ref<DnsEntry[]>([])
const mosdnsFallbackDnsEntries = ref<DnsEntry[]>([])
// DNS 列表容器的 ref
const localDnsListRef = ref<HTMLElement | null>(null)
const remoteDnsListRef = ref<HTMLElement | null>(null)
const fallbackDnsListRef = ref<HTMLElement | null>(null)
const mosdnsDefaultForward = ref('forward_remote')
const mosdnsCustomHosts = ref('')
const mosdnsLogEnabled = ref(true)
const mosdnsLogLevel = ref('info')
const mosdnsLogFile = ref('./mosdns.log')
const mosdnsApiEnabled = ref(true)
const mosdnsApiAddress = ref('0.0.0.0:8338')
const availableRuleSets = ref<RuleSet[]>([])
const availableRules = ref<any[]>([])
const savingMosdnsSettings = ref(false)

// 备份配置
const backupDialogVisible = ref(false)
const backupForm = ref({
  webdav_url: '',
  webdav_username: '',
  webdav_password: '',
  webdav_path: '/config-flow-backup/',
  auto_backup: false
})
const testingConnection = ref(false)
const backingUp = ref(false)
const savingBackup = ref(false)

// 服务域名配置
const serverDomain = ref(localStorage.getItem('serverDomain') || window.location.origin)

// 配置令牌
const configToken = ref('')

// 订阅聚合开关
const subscriptionAggregationEnabled = ref(false)

// 处理按钮点击
const handleSurgeCustomConfig = () => {
  showCustomConfigDialog('surge')
}

const handleSurgePreview = () => {
  previewConfig('surge')
}

const handleBackup = () => {
  showBackupDialog()
}

// 重置服务域名为当前浏览器地址
const resetServerDomain = async () => {
  try {
    await ElMessageBox.confirm(
      '是否将服务域名重置为当前浏览器地址？',
      '确认重置',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const newDomain = window.location.origin

    await serverDomainApi.update({
      new_domain: newDomain
    })

    serverDomain.value = newDomain
    localStorage.setItem('serverDomain', newDomain)

    ElMessage.success('服务域名已重置为当前地址')
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('重置服务域名失败:', error)
      ElMessage.error('重置失败')
    }
  }
}

// 输入框失去焦点时保存
const onServerDomainBlur = async () => {
  if (!serverDomain.value) {
    return
  }

  // 保存到 localStorage
  localStorage.setItem('serverDomain', serverDomain.value)

  try {
    await serverDomainApi.update({
      new_domain: serverDomain.value
    })

    ElMessage.success(`服务域名已更新为：${serverDomain.value}`)
  } catch (error: any) {
    console.error('更新服务域名失败:', error)
    ElMessage.error('更新失败')
  }
}

// 监听服务域名变化并保存到 localStorage
const saveServerDomain = () => {
  localStorage.setItem('serverDomain', serverDomain.value)
}

// 订阅聚合开关变化处理
const onSubscriptionAggregationChange = async (value: boolean) => {
  try {
    // 保存到 localStorage
    localStorage.setItem('subscriptionAggregationEnabled', value.toString())

    // 保存到后端
    await api.post('/settings/subscription-aggregation', {
      enabled: value
    })

    ElMessage.success(value ? '订阅聚合已开启' : '订阅聚合已关闭')

    // 触发自定义事件，通知其他组件更新
    window.dispatchEvent(new CustomEvent('subscription-aggregation-changed', {
      detail: { enabled: value }
    }))
  } catch (error: any) {
    console.error('更新订阅聚合开关失败:', error)
    ElMessage.error('更新失败')
    // 失败时恢复原值
    subscriptionAggregationEnabled.value = !value
  }
}

// 配置令牌相关函数
const loadConfigToken = async () => {
  try {
    const response = await configTokenApi.get()
    configToken.value = response.data.config_token || ''
  } catch (error: any) {
    console.error('加载配置令牌失败:', error)
  }
}

const generateToken = async () => {
  try {
    const response = await configTokenApi.update({ generate: true })
    configToken.value = response.data.config_token
    ElMessage.success('令牌已生成并保存')
  } catch (error: any) {
    console.error('生成令牌失败:', error)
    ElMessage.error('生成令牌失败')
  }
}

const saveToken = async () => {
  try {
    if (!configToken.value || configToken.value.trim() === '') {
      ElMessage.warning('令牌不能为空，如需清空请点击清除按钮')
      return
    }
    await configTokenApi.update({ token: configToken.value })
    ElMessage.success('令牌已保存')
  } catch (error: any) {
    console.error('保存令牌失败:', error)
    ElMessage.error('保存令牌失败')
  }
}

const onTokenBlur = async () => {
  // 如果输入框为空，不保存
  if (!configToken.value || configToken.value.trim() === '') {
    return
  }

  try {
    await configTokenApi.update({ token: configToken.value })
    ElMessage.success('令牌已保存')
  } catch (error: any) {
    console.error('保存令牌失败:', error)
    ElMessage.error('保存令牌失败')
  }
}

const onClearToken = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要清除配置令牌吗？清除后配置 URL 将不再需要令牌验证。',
      '确认清除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
    await configTokenApi.delete()
    configToken.value = ''
    ElMessage.success('令牌已清除')
  } catch (error: any) {
    if (error !== 'cancel') {
      console.error('清除令牌失败:', error)
      ElMessage.error('清除令牌失败')
    }
  }
}

// 计算配置 URL - 使用 serverDomain 代替固定的 window.location.origin
const baseUrl = computed(() => {
  return serverDomain.value
})

const mihomoUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/mihomo`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})
const surgeUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/surge`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})
const mosdnsUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/mosdns`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})

// URL显示
const mihomoUrlDisplay = computed(() => mihomoUrl.value)
const surgeUrlDisplay = computed(() => surgeUrl.value)
const mosdnsUrlDisplay = computed(() => mosdnsUrl.value)

// 复制 URL 到剪贴板
const copyUrl = (url: string, configType: string) => {
  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(url).then(() => {
      ElMessage.success(`${configType} 配置 URL 已复制到剪贴板`)
    }).catch(() => {
      fallbackCopyUrl(url, configType)
    })
  } else {
    // 降级到传统方法
    fallbackCopyUrl(url, configType)
  }
}

// 降级复制方法
const fallbackCopyUrl = (text: string, configType: string) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    ElMessage.success(`${configType} 配置 URL 已复制到剪贴板`)
  } catch (err) {
    ElMessage.error('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

const generateMihomo = async () => {
  try {
    mihomoLoading.value = true
    const response = await generateApi.mihomo()

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/x-yaml' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'mihomo.yaml'
    link.click()
    window.URL.revokeObjectURL(url)

    ElMessage.success('Mihomo 配置已生成')
  } catch (error) {
    ElMessage.error('生成失败')
  } finally {
    mihomoLoading.value = false
  }
}

const generateSurge = async () => {
  try {
    surgeLoading.value = true
    const response = await generateApi.surge()

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'text/plain' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'surge.conf'
    link.click()
    window.URL.revokeObjectURL(url)

    ElMessage.success('Surge 配置已生成')
  } catch (error) {
    ElMessage.error('生成失败')
  } finally {
    surgeLoading.value = false
  }
}

const generateMosdns = async () => {
  try {
    mosdnsLoading.value = true
    const response = await generateApi.mosdns()

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'mosdns-config.zip'
    link.click()
    window.URL.revokeObjectURL(url)

    ElMessage.success('MosDNS 配置已生成')
  } catch (error) {
    ElMessage.error('生成失败')
  } finally {
    mosdnsLoading.value = false
  }
}

const exportConfig = async () => {
  try {
    const response = await configApi.export()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config.json'
    link.click()
    window.URL.revokeObjectURL(url)

    ElMessage.success('配置已导出')
  } catch (error) {
    ElMessage.error('导出失败')
  }
}

const exportConfigDesensitized = async () => {
  try {
    const response = await configApi.exportDesensitized()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config_desensitized.json'
    link.click()
    window.URL.revokeObjectURL(url)

    ElMessage.success('脱敏配置已导出')
  } catch (error) {
    ElMessage.error('导出失败')
  }
}

const importConfig = async (file: File) => {
  try {
    const text = await file.text()
    const config = JSON.parse(text)

    await configApi.import(config)
    ElMessage.success('配置导入成功，请刷新页面')
  } catch (error) {
    ElMessage.error('导入失败，请检查文件格式')
  }

  return false // 阻止自动上传
}

const resetConfig = async () => {
  try {
    await ElMessageBox.confirm(
      '重置配置将清空所有订阅、节点、规则和策略组设置，恢复为默认配置。此操作不可撤销！',
      '确认重置配置',
      {
        confirmButtonText: '确认重置',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger'
      }
    )

    await api.post('/config/reset')
    ElMessage.success('配置已重置为默认值，请刷新页面')

    // 刷新统计
    setTimeout(() => {
      // 刷新页面以加载新配置
      window.location.reload()
    }, 500)
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('重置失败')
    }
  }
}

const getCustomConfigDialogTitle = () => {
  const titles = {
    mihomo: '自定义 Mihomo 基础配置',
    surge: '自定义 Surge 基础配置',
    mosdns: '自定义 MosDNS 基础配置'
  }
  return titles[currentConfigType.value]
}

const getCustomConfigDialogDesc = () => {
  const descs = {
    mihomo: '在此编辑 Mihomo 的基础配置（如 mixed-port、dns、tun 等），生成配置时会自动合并 proxies、proxy-groups、rules 等配置。使用 YAML 格式。',
    surge: '在此编辑 Surge 的基础配置。使用 INI 风格的配置格式，包含 [General]、[Proxy]、[Proxy Group]、[Rule] 等部分。规则格式：TYPE,VALUE,POLICY（如 DOMAIN-SUFFIX,google.com,Proxy）',
    mosdns: '在此编辑 MosDNS 的基础配置。使用 YAML 格式，主要包含 log、data_providers、plugins、servers 等部分。注意插件初始化顺序。'
  }
  return descs[currentConfigType.value]
}

const getCustomConfigPlaceholder = () => {
  const placeholders = {
    mihomo: '输入自定义 YAML 配置，例如：\nmixed-port: 7890\nallow-lan: true\nmode: rule\nlog-level: info\nexternal-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 0.0.0.0:53\n  enhanced-mode: fake-ip',
    surge: '输入自定义配置，例如：\n[General]\nloglevel = notify\ninternet-test-url = http://www.gstatic.com/generate_204\nproxy-test-url = http://www.gstatic.com/generate_204\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8\n\n# 代理将自动生成在 [Proxy] 部分\n# 策略组将自动生成在 [Proxy Group] 部分\n# 规则将自动生成在 [Rule] 部分',
    mosdns: '输入自定义 YAML 配置，例如：\nlog:\n  level: info\n  file: ./mosdns.log\n\nservers:\n  - addr: 127.0.0.1:53\n    protocol: udp\n\n# data_providers 和 plugins 将自动生成'
  }
  return placeholders[currentConfigType.value]
}

const getPreviewDialogTitle = () => {
  const titles = {
    mihomo: '预览 Mihomo 配置',
    surge: '预览 Surge 配置',
    mosdns: '预览 MosDNS 配置'
  }
  return titles[currentPreviewType.value]
}

const showCustomConfigDialog = async (type: 'mihomo' | 'surge' | 'mosdns') => {
  currentConfigType.value = type
  try {
    const apiMap = {
      mihomo: customConfigApi.getMihomo,
      surge: customConfigApi.getSurge,
      mosdns: customConfigApi.getMosdns
    }
    const response = await apiMap[type]()
    customConfigContent.value = response.data.config || ''
    customConfigDialogVisible.value = true
  } catch (error) {
    ElMessage.error('加载自定义配置失败')
  }
}

const saveCustomConfig = async () => {
  try {
    savingCustomConfig.value = true
    const apiMap = {
      mihomo: customConfigApi.saveMihomo,
      surge: customConfigApi.saveSurge,
      mosdns: customConfigApi.saveMosdns
    }
    await apiMap[currentConfigType.value]({ config: customConfigContent.value })
    ElMessage.success('自定义配置已保存')
    customConfigDialogVisible.value = false
  } catch (error) {
    ElMessage.error('保存失败')
  } finally {
    savingCustomConfig.value = false
  }
}

const previewConfig = async (type: 'mihomo' | 'surge' | 'mosdns') => {
  currentPreviewType.value = type
  const loadingMap = {
    mihomo: mihomoPreviewLoading,
    surge: surgePreviewLoading,
    mosdns: mosdnsPreviewLoading
  }

  try {
    loadingMap[type].value = true

    const apiMap = {
      mihomo: generateApi.previewMihomo,
      surge: generateApi.previewSurge,
      mosdns: generateApi.previewMosdns
    }
    const response = await apiMap[type]()
    previewContent.value = response.data.content || response.data
    previewDialogVisible.value = true
  } catch (error) {
    ElMessage.error('生成预览失败')
  } finally {
    loadingMap[type].value = false
  }
}

const copyPreviewContent = () => {
  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(previewContent.value).then(() => {
      ElMessage.success('已复制到剪贴板')
    }).catch(() => {
      fallbackCopyPreview()
    })
  } else {
    // 降级到传统方法
    fallbackCopyPreview()
  }
}

// 降级复制预览内容
const fallbackCopyPreview = () => {
  const textarea = document.createElement('textarea')
  textarea.value = previewContent.value
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    ElMessage.success('已复制到剪贴板')
  } catch (err) {
    ElMessage.error('复制失败')
  }
  document.body.removeChild(textarea)
}

// ===== DNS 条目管理函数 =====

// 添加 DNS 条目
const addDnsEntry = (type: 'local' | 'remote' | 'fallback') => {
  const newEntry: DnsEntry = {
    id: generateId(),
    mode: 'simple',
    addr: '',
    bootstrap: '',
    enable_pipeline: false,
    yaml_config: ''
  }

  if (type === 'local') {
    mosdnsLocalDnsEntries.value.push(newEntry)
  } else if (type === 'remote') {
    mosdnsRemoteDnsEntries.value.push(newEntry)
  } else {
    mosdnsFallbackDnsEntries.value.push(newEntry)
  }
}

// 删除 DNS 条目
const removeDnsEntry = (type: 'local' | 'remote' | 'fallback', id: string) => {
  if (type === 'local') {
    mosdnsLocalDnsEntries.value = mosdnsLocalDnsEntries.value.filter(entry => entry.id !== id)
  } else if (type === 'remote') {
    mosdnsRemoteDnsEntries.value = mosdnsRemoteDnsEntries.value.filter(entry => entry.id !== id)
  } else {
    mosdnsFallbackDnsEntries.value = mosdnsFallbackDnsEntries.value.filter(entry => entry.id !== id)
  }
}

// 切换 DNS 条目模式
const toggleDnsEntryMode = (type: 'local' | 'remote' | 'fallback', id: string) => {
  const entries = type === 'local' ? mosdnsLocalDnsEntries.value
                : type === 'remote' ? mosdnsRemoteDnsEntries.value
                : mosdnsFallbackDnsEntries.value

  const entry = entries.find(e => e.id === id)
  if (entry) {
    if (entry.mode === 'simple') {
      // 切换到 YAML 模式，生成初始 YAML 配置
      const yamlParts = [`- addr: ${entry.addr || ''}`]
      // 只有域名才添加 bootstrap
      if (entry.bootstrap && isDomainAddr(entry.addr)) {
        yamlParts.push(`  bootstrap: ${entry.bootstrap}`)
      }
      if (entry.enable_pipeline) {
        yamlParts.push(`  enable_pipeline: true`)
      }
      entry.yaml_config = yamlParts.join('\n')
      entry.mode = 'yaml'
    } else {
      // 切换到简单模式，尝试解析 YAML
      try {
        const lines = entry.yaml_config.trim().split('\n')
        let addr = ''
        let bootstrap = ''
        let enable_pipeline = false

        for (const line of lines) {
          const trimmed = line.trim()
          if (trimmed.startsWith('addr:') || trimmed.startsWith('- addr:')) {
            addr = trimmed.replace(/^-?\s*addr:\s*/, '')
          } else if (trimmed.startsWith('bootstrap:')) {
            bootstrap = trimmed.replace(/^bootstrap:\s*/, '')
          } else if (trimmed.includes('enable_pipeline') && trimmed.includes('true')) {
            enable_pipeline = true
          }
        }

        entry.addr = addr
        // 如果地址不是域名，清空 bootstrap
        entry.bootstrap = isDomainAddr(addr) ? bootstrap : ''
        entry.enable_pipeline = enable_pipeline
        entry.mode = 'simple'
      } catch (error) {
        ElMessage.warning('YAML 解析失败，已清空字段')
        entry.addr = ''
        entry.bootstrap = ''
        entry.enable_pipeline = false
        entry.mode = 'simple'
      }
    }
  }
}

// 初始化 DNS 条目拖拽功能
const initDnsSortable = () => {
  nextTick(() => {
    // 初始化国内 DNS 拖拽
    if (localDnsListRef.value) {
      Sortable.create(localDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsLocalDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsLocalDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }

    // 初始化国外 DNS 拖拽
    if (remoteDnsListRef.value) {
      Sortable.create(remoteDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsRemoteDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsRemoteDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }

    // 初始化 Fallback DNS 拖拽
    if (fallbackDnsListRef.value) {
      Sortable.create(fallbackDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsFallbackDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsFallbackDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }
  })
}

const loadRuleSets = async () => {
  try {
    const response = await ruleSetApi.getAll()
    availableRuleSets.value = response.data
  } catch (error) {
    console.error('加载规则集列表失败', error)
  }
}

const loadRules = async () => {
  try {
    const response = await ruleApi.getAll()
    // 筛选出单条规则（itemType 为 'rule'）
    availableRules.value = response.data.filter((item: any) => item.itemType === 'rule')
  } catch (error) {
    console.error('加载规则列表失败', error)
  }
}

const getMosdnsCustomConfigPlaceholder = () => {
  return '输入自定义 YAML 配置，例如：\nlog:\n  level: info\n  file: ./mosdns.log\n\nservers:\n  - addr: 127.0.0.1:5335\n    protocol: udp\n\n# data_providers 和 plugins 将自动生成'
}

const showMosdnsSettingsDialog = async () => {
  try {
    // 加载规则集列表和规则列表
    await Promise.all([loadRuleSets(), loadRules()])

    // 加载自定义配置
    const customConfigResponse = await customConfigApi.getMosdns()
    mosdnsCustomConfig.value = customConfigResponse.data.config || ''

    // 加载规则集配置
    const rulesetResponse = await api.get('/mosdns/rulesets')
    mosdnsDirectRulesets.value = rulesetResponse.data.direct_rulesets || []
    mosdnsProxyRulesets.value = rulesetResponse.data.proxy_rulesets || []
    mosdnsDirectRules.value = rulesetResponse.data.direct_rules || []
    mosdnsProxyRules.value = rulesetResponse.data.proxy_rules || []

    // 加载自定义 match 配置
    const customMatchResponse = await api.get('/mosdns/custom-matches')
    const fetchedMatches = Array.isArray(customMatchResponse.data?.custom_matches)
      ? customMatchResponse.data.custom_matches
      : []
    const fetchedPosition = customMatchResponse.data?.position
    mosdnsCustomMatchPosition.value = fetchedPosition === 'head' ? 'head' : 'tail'
    mosdnsCustomMatches.value = fetchedMatches.map((item: any) => ({
      id: item.id || generateMatchId(),
      enabled: item.enabled !== undefined ? Boolean(item.enabled) : true,
      exec: item.exec || '',
      matchesText: Array.isArray(item.matches)
        ? item.matches.join('\n')
        : (item.matches || '')
    }))

    // 加载 DNS 服务器配置
    const dnsResponse = await api.get('/mosdns/dns-servers')
    mosdnsLocalDns.value = dnsResponse.data.local_dns || ''
    mosdnsRemoteDns.value = dnsResponse.data.remote_dns || ''
    mosdnsFallbackDns.value = dnsResponse.data.fallback_dns || ''
    // 解析 DNS 文本为条目数组
    mosdnsLocalDnsEntries.value = parseDnsText(mosdnsLocalDns.value)
    mosdnsRemoteDnsEntries.value = parseDnsText(mosdnsRemoteDns.value)
    mosdnsFallbackDnsEntries.value = parseDnsText(mosdnsFallbackDns.value)
    mosdnsDefaultForward.value = dnsResponse.data.default_forward || 'forward_remote'
    mosdnsCustomHosts.value = dnsResponse.data.custom_hosts || ''

    // 加载日志配置
    const logResponse = await api.get('/mosdns/log-settings')
    mosdnsLogEnabled.value = logResponse.data.log_enabled !== undefined ? logResponse.data.log_enabled : true
    mosdnsLogLevel.value = logResponse.data.log_level || 'info'
    mosdnsLogFile.value = logResponse.data.log_file || './mosdns.log'

    // 加载 API 配置
    const apiResponse = await api.get('/mosdns/api-settings')
    mosdnsApiEnabled.value = apiResponse.data.api_enabled !== undefined ? apiResponse.data.api_enabled : true
    mosdnsApiAddress.value = apiResponse.data.api_address || '0.0.0.0:8338'

    // 重置到第一个 tab
    mosdnsActiveTab.value = 'rules'

    // 显示对话框
    mosdnsSettingsDialogVisible.value = true

    // 初始化拖拽功能
    initDnsSortable()
  } catch (error) {
    console.error('加载 MosDNS 设置失败', error)
    ElMessage.error('加载设置失败')
  }
}

const saveMosdnsSettings = async () => {
  try {
    savingMosdnsSettings.value = true

    // 保存自定义配置
    await customConfigApi.saveMosdns({ config: mosdnsCustomConfig.value })

    // 保存规则集和规则配置
    await api.post('/mosdns/rulesets', {
      direct_rulesets: mosdnsDirectRulesets.value,
      proxy_rulesets: mosdnsProxyRulesets.value,
      direct_rules: mosdnsDirectRules.value,
      proxy_rules: mosdnsProxyRules.value
    })

    const customMatchPayload = mosdnsCustomMatches.value.map(item => ({
      id: item.id,
      enabled: item.enabled,
      exec: item.exec,
      matches: item.matchesText
    }))

    await api.post('/mosdns/custom-matches', {
      custom_matches: customMatchPayload,
      position: mosdnsCustomMatchPosition.value
    })

    // 将 DNS 条目数组转换为文本
    const localDnsText = dnsEntriesToText(mosdnsLocalDnsEntries.value)
    const remoteDnsText = dnsEntriesToText(mosdnsRemoteDnsEntries.value)
    const fallbackDnsText = dnsEntriesToText(mosdnsFallbackDnsEntries.value)

    // 保存 DNS 服务器配置
    await api.post('/mosdns/dns-servers', {
      local_dns: localDnsText,
      remote_dns: remoteDnsText,
      fallback_dns: fallbackDnsText,
      default_forward: mosdnsDefaultForward.value,
      custom_hosts: mosdnsCustomHosts.value
    })

    // 同步更新文本字段（保持兼容性）
    mosdnsLocalDns.value = localDnsText
    mosdnsRemoteDns.value = remoteDnsText
    mosdnsFallbackDns.value = fallbackDnsText

    // 保存日志配置
    await api.post('/mosdns/log-settings', {
      log_enabled: mosdnsLogEnabled.value,
      log_level: mosdnsLogLevel.value,
      log_file: mosdnsLogFile.value
    })

    // 保存 API 配置
    await api.post('/mosdns/api-settings', {
      api_enabled: mosdnsApiEnabled.value,
      api_address: mosdnsApiAddress.value
    })

    ElMessage.success('MosDNS 设置已保存')
    mosdnsSettingsDialogVisible.value = false
  } catch (error) {
    console.error('保存 MosDNS 设置失败', error)
    ElMessage.error('保存失败')
  } finally {
    savingMosdnsSettings.value = false
  }
}

// 备份相关方法
const showBackupDialog = async () => {
  try {
    // 加载备份配置
    const response = await api.get('/backup/config')
    if (response.data) {
      backupForm.value = {
        webdav_url: response.data.webdav_url || '',
        webdav_username: response.data.webdav_username || '',
        webdav_password: response.data.webdav_password || '',
        webdav_path: response.data.webdav_path || '/config-flow-backup/',
        auto_backup: response.data.auto_backup || false
      }
    }
    backupDialogVisible.value = true
  } catch (error) {
    console.error('加载备份配置失败', error)
    backupDialogVisible.value = true
  }
}

const testWebDAVConnection = async () => {
  if (!backupForm.value.webdav_url) {
    ElMessage.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    ElMessage.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    ElMessage.warning('请输入密码')
    return
  }

  try {
    testingConnection.value = true
    await api.post('/backup/test', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path
    })
    ElMessage.success('连接测试成功')
  } catch (error: any) {
    console.error('测试连接失败', error)
    const errorMsg = error.response?.data?.message || '连接测试失败，请检查配置'
    ElMessage.error(errorMsg)
  } finally {
    testingConnection.value = false
  }
}

const backupNow = async () => {
  if (!backupForm.value.webdav_url) {
    ElMessage.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    ElMessage.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    ElMessage.warning('请输入密码')
    return
  }

  try {
    backingUp.value = true
    await api.post('/backup/now', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path
    })
    ElMessage.success('备份成功')
  } catch (error: any) {
    console.error('备份失败', error)
    const errorMsg = error.response?.data?.message || '备份失败，请检查配置'
    ElMessage.error(errorMsg)
  } finally {
    backingUp.value = false
  }
}

const saveBackupConfig = async () => {
  try {
    savingBackup.value = true
    await api.post('/backup/config', backupForm.value)
    ElMessage.success('备份配置已保存')
    backupDialogVisible.value = false
  } catch (error: any) {
    console.error('保存备份配置失败', error)
    ElMessage.error('保存失败')
  } finally {
    savingBackup.value = false
  }
}

// 监听服务域名变化并自动保存
watch(serverDomain, (newValue) => {
  if (newValue) {
    localStorage.setItem('serverDomain', newValue)
  }
})

onMounted(async () => {
  // 从后端加载服务域名配置
  try {
    const response = await serverDomainApi.get()
    const backendDomain = response.data.server_domain

    // 如果后端有配置，优先使用后端的配置
    if (backendDomain && backendDomain.trim()) {
      serverDomain.value = backendDomain
      localStorage.setItem('serverDomain', backendDomain)
    } else {
      // 后端没有配置，使用 localStorage 或当前地址
      const localDomain = localStorage.getItem('serverDomain') || window.location.origin
      serverDomain.value = localDomain
    }
  } catch (error) {
    console.error('加载服务域名失败:', error)
    // 加载失败，使用 localStorage 或当前地址
    const fallbackDomain = localStorage.getItem('serverDomain') || window.location.origin
    serverDomain.value = fallbackDomain
  }

  // 加载配置令牌
  await loadConfigToken()

  // 从后端加载订阅聚合开关配置
  try {
    const response = await api.get('/settings/subscription-aggregation')
    const enabled = response.data.enabled || false
    subscriptionAggregationEnabled.value = enabled
    localStorage.setItem('subscriptionAggregationEnabled', enabled.toString())
  } catch (error) {
    console.error('加载订阅聚合开关失败:', error)
    // 加载失败，使用 localStorage
    const localEnabled = localStorage.getItem('subscriptionAggregationEnabled') === 'true'
    subscriptionAggregationEnabled.value = localEnabled
  }

})

onUnmounted(() => {
})
</script>

<style scoped>
.generate {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --gen-radius-xl: 40px;
  --gen-radius-lg: 24px;
  --gen-radius-md: 16px;
  --gen-radius-pill: 999px;
}

.page-header {
  margin-bottom: 28px;
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

.config-section {
  margin-bottom: 24px;
}

.el-card {
  border-radius: var(--gen-radius-lg, 24px);
  border: 1px solid rgba(107, 115, 255, 0.1);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.el-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 20px 40px rgba(65, 80, 180, 0.16);
  border-color: rgba(107, 115, 255, 0.25);
}

h4 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  color: transparent;
  display: flex;
  align-items: center;
  gap: 8px;
}

h4 .el-tag {
  -webkit-text-fill-color: var(--el-color-warning) !important;
  color: var(--el-color-warning) !important;
  background: var(--el-color-warning-light-9) !important;
  background-clip: padding-box !important;
  -webkit-background-clip: padding-box !important;
  border-color: var(--el-color-warning-light-5) !important;
}

/* URL 展示框样式 */
.config-url-box {
  margin-bottom: 16px;
  padding: 14px;
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.08) 0%, rgba(0, 13, 255, 0.08) 100%);
  border-radius: var(--gen-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.2);
  transition: all 0.2s ease;
}

.config-url-box:hover {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.12) 0%, rgba(0, 13, 255, 0.12) 100%);
  border-color: rgba(107, 115, 255, 0.3);
}

.url-hint {
  margin-top: 8px;
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}

/* 等高卡片容器 */
.config-row {
  margin: 0 -10px !important;
}

.config-row :deep(.el-col) {
  display: flex !important;
  padding: 0 10px !important;
}

/* 等高卡片样式 */
.equal-height-card {
  height: 100%;
  width: 100%;
  display: flex;
  flex-direction: column;
}

.equal-height-card :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
}

/* 服务域名提示文字 - 减小行间距 */
.server-domain-hint {
  margin-top: 8px;
  color: #909399;
  font-size: 12px;
  line-height: 1.4;
}

/* 生成器卡片特殊样式 */
:deep(.el-card) {
  position: relative;
  overflow: hidden;
}

/* 按钮组优化 */
:deep(.el-row) {
  margin: 0 !important;
}

:deep(.el-col) {
  padding: 0 5px;
}

:deep(.el-col:first-child) {
  padding-left: 0;
}

:deep(.el-col:last-child) {
  padding-right: 0;
}

/* 统计卡片样式 */
:deep(.el-descriptions) {
  --el-descriptions-item-bordered-label-background: linear-gradient(135deg, rgba(107, 115, 255, 0.05) 0%, rgba(0, 13, 255, 0.05) 100%);
}

:deep(.el-descriptions__label) {
  font-weight: 600;
  color: #6B73FF;
}

:deep(.el-descriptions__content) {
  font-weight: 600;
  color: #4a5568;
}

/* 配置提示样式 */
:deep(.el-alert) {
  border: none;
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.1) 0%, rgba(0, 13, 255, 0.1) 100%);
  border-left: 4px solid #6B73FF;
}

/* 上传按钮样式 */
:deep(.el-upload) {
  width: 100%;
}

:deep(.el-upload .el-button) {
  width: 100%;
}

/* Divider 样式 */
:deep(.el-divider) {
  border-top: 2px dashed rgba(107, 115, 255, 0.15);
  margin: 32px 0;
}

/* 按钮样式优化 */
:deep(.el-button) {
  border-radius: var(--gen-radius-md, 16px);
  font-weight: 600;
  transition: all 0.2s ease;
}

:deep(.el-button:not(.is-disabled):hover) {
  transform: translateY(-1px);
}

:deep(.el-button--primary) {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  border: none;
  box-shadow: 0 8px 16px rgba(87, 104, 255, 0.25);
}

:deep(.el-button--primary:hover) {
  box-shadow: 0 10px 20px rgba(87, 104, 255, 0.35);
}

/* 输入框样式 */
:deep(.el-input__wrapper) {
  border-radius: var(--gen-radius-md, 16px);
  transition: all 0.2s ease;
}

:deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px rgba(107, 115, 255, 0.2) inset;
}

:deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px #000dff inset, 0 0 0 3px rgba(107, 115, 255, 0.1) !important;
}

/* 对话框样式 */
:deep(.el-dialog) {
  border-radius: var(--gen-radius-xl, 40px);
  overflow: hidden;
  box-shadow: 0 36px 80px rgba(65, 80, 180, 0.28);
  border: 1px solid rgba(107, 115, 255, 0.16);
  background: rgba(252, 253, 255, 0.97);
  backdrop-filter: blur(20px);
}

:deep(.el-dialog__header) {
  padding: 24px 32px;
  margin: 0;
  border-bottom: 1px solid rgba(107, 115, 255, 0.1);
  background: #f7f8ff;
}

:deep(.el-dialog__title) {
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

:deep(.el-dialog__body) {
  padding: 28px 32px;
  background: #f7f8ff;
}

:deep(.el-dialog__footer) {
  padding: 20px 32px;
  border-top: 1px solid rgba(107, 115, 255, 0.1);
  background: #f7f8ff;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .generate {
    padding: 20px 16px 32px;
    min-height: calc(100vh - 64px);
  }

  .title-block h2 {
    font-size: 22px;
  }

  .title-block p {
    font-size: 13px;
  }

  /* 卡片网格单列布局 */
  .config-section :deep(.el-row) {
    flex-direction: column;
    margin: 0 !important;
  }

  .config-section :deep(.el-col) {
    width: 100% !important;
    max-width: 100% !important;
    padding: 0 !important;
    margin-bottom: 12px;
  }

  .config-section :deep(.el-col:last-child) {
    margin-bottom: 0;
  }

  .config-row {
    margin: 0 !important;
  }

  .config-row :deep(.el-col) {
    padding: 0 !important;
  }

  /* 卡片内容调整 */
  :deep(.el-card) {
    margin-bottom: 0;
  }

  :deep(.el-card__header) {
    padding: 12px 14px;
  }

  :deep(.el-card__body) {
    padding: 14px;
  }

  h3, h4 {
    font-size: 16px;
  }

  /* 按钮布局调整 */
  :deep(.el-button) {
    font-size: 12px;
    padding: 8px 10px;
  }

  :deep(.el-button .el-icon) {
    font-size: 14px;
  }

  /* 配置管理按钮 */
  :deep(.el-space) {
    display: flex;
    flex-direction: column;
    width: 100%;
  }

  :deep(.el-space .el-space__item) {
    width: 100%;
    margin: 0 0 8px 0 !important;
  }

  :deep(.el-space .el-button),
  :deep(.el-upload) {
    width: 100%;
  }

  /* MosDNS 设置对话框 */
  :deep(.mosdns-dialog .el-dialog) {
    width: 95vw !important;
    max-width: 95vw !important;
    margin: 0 !important;
    max-height: 92vh;
  }

  :deep(.mosdns-dialog .el-dialog__body) {
    padding: 18px 16px 14px;
  }

  :deep(.mosdns-dialog .el-dialog__footer) {
    padding: 16px;
  }

  :deep(.mosdns-dialog .el-tabs__header) {
    margin-bottom: 16px;
  }

  :deep(.mosdns-dialog .el-tabs__nav-wrap) {
    overflow: hidden;
  }

  :deep(.mosdns-dialog .el-tabs__nav-scroll) {
    overflow-x: auto;
    scrollbar-width: none;
  }

  :deep(.mosdns-dialog .el-tabs__nav-scroll::-webkit-scrollbar) {
    display: none;
  }

  :deep(.mosdns-dialog .el-tabs__nav) {
    display: flex;
    flex-wrap: nowrap;
    gap: 8px;
    min-width: 100%;
  }

  :deep(.mosdns-dialog .el-tabs__item) {
    flex: 0 0 auto;
    min-width: 120px;
    text-align: center;
    border-radius: 999px;
    padding: 10px 16px;
    white-space: nowrap;
  }

  :deep(.mosdns-dialog .el-form) {
    gap: 16px;
  }

  :deep(.mosdns-dialog .el-form-item) {
    flex-direction: column;
    align-items: stretch;
  }

  :deep(.mosdns-dialog .el-form-item__label) {
    width: 100% !important;
    text-align: left;
    line-height: 1.4;
    padding-bottom: 6px;
  }

  :deep(.mosdns-dialog .el-form-item__content) {
    width: 100%;
  }

  :deep(.mosdns-dialog .el-select),
  :deep(.mosdns-dialog .el-input),
  :deep(.mosdns-dialog .el-textarea),
  :deep(.mosdns-dialog .el-input-number) {
    width: 100%;
  }

  :deep(.mosdns-dialog .el-button[size='small']) {
    width: 100%;
    justify-content: center;
  }

  :deep(.mosdns-dialog .custom-match-header) {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }

  :deep(.mosdns-dialog .custom-match-actions) {
    width: 100%;
    flex-wrap: wrap;
    gap: 8px;
  }

  :deep(.mosdns-dialog .custom-match-actions .el-button),
  :deep(.mosdns-dialog .custom-match-actions .el-button-group .el-button) {
    flex: 1 1 calc(50% - 4px);
    min-width: 48%;
  }

  .custom-match-card {
    padding: 14px;
  }

  .dns-entry-card {
    padding: 14px;
  }

  .dns-entry-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }

  .dns-entry-actions {
    width: 100%;
    flex-wrap: wrap;
  }

  .dns-entry-actions :deep(.el-button) {
    flex: 1 1 calc(50% - 4px);
    min-width: 48%;
  }

  .custom-match-actions :deep(.el-button-group) {
    width: 100%;
  }

  /* 统计信息 */
  :deep(.el-descriptions) {
    font-size: 12px;
  }

  :deep(.el-descriptions__label),
  :deep(.el-descriptions__content) {
    padding: 8px 10px !important;
    font-size: 12px;
  }

  /* Divider 间距 */
  :deep(.el-divider) {
    margin: 20px 0;
  }

  /* 说明文字 */
  p {
    font-size: 12px;
  }
}

/* 超小屏幕适配 */
@media (max-width: 480px) {
  .header h3 {
    font-size: 16px;
  }

  h3, h4 {
    font-size: 14px;
  }

  :deep(.el-card__header) {
    padding: 10px 12px;
  }

  :deep(.el-card__body) {
    padding: 12px;
  }

  :deep(.el-descriptions) {
    font-size: 11px;
  }

  :deep(.el-descriptions__label),
  :deep(.el-descriptions__content) {
    padding: 6px 8px !important;
    font-size: 11px;
  }
}

/* DNS 条目卡片样式 */
.dns-entry-card {
  margin-bottom: 16px;
  padding: 18px;
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-radius: var(--gen-radius-lg, 24px);
  background: #ffffff;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  cursor: move;
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.06);
}

.dns-entry-card:hover {
  border-color: rgba(107, 115, 255, 0.35);
  box-shadow: 0 12px 28px rgba(107, 115, 255, 0.2);
  transform: translateY(-2px);
}

/* 拖拽时的样式 */
.sortable-ghost {
  opacity: 0.4;
  background: #f5f7fa;
}

.dns-entry-card:last-child {
  margin-bottom: 0;
}

.dns-entry-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid #ebeef5;
}

.dns-entry-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.drag-handle {
  color: #909399;
  cursor: grab;
  transition: all 0.3s;
  transform: rotate(90deg);
}

.drag-handle:hover {
  color: #6B73FF;
}

.drag-handle:active {
  cursor: grabbing;
}

.dns-entry-index {
  font-weight: 600;
  font-size: 14px;
  color: #303133;
}

.dns-entry-actions {
  display: flex;
  gap: 8px;
}

.dns-entry-content {
  padding-top: 4px;
}

/* YAML 验证样式 */
.yaml-error :deep(.el-textarea__inner) {
  border-color: #f56c6c !important;
}

.yaml-error-message {
  margin-top: 8px;
  padding: 8px 12px;
  background: #fef0f0;
  border: 1px solid #fde2e2;
  border-radius: 4px;
  color: #f56c6c;
  font-size: 12px;
  line-height: 1.5;
}

.yaml-success-message {
  margin-top: 8px;
  padding: 6px 12px;
  background: #f0f9ff;
  border: 1px solid #d1e7ff;
  border-radius: 4px;
  color: #8b8fff;
  font-size: 12px;
  line-height: 1.5;
}

/* 自定义 Match 卡片样式 */
.custom-match-card {
  margin-bottom: 14px;
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-radius: var(--gen-radius-md, 16px);
  padding: 16px;
  background: #fff;
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.06);
  transition: all 0.2s ease;
}

.custom-match-card:hover {
  border-color: rgba(107, 115, 255, 0.25);
  box-shadow: 0 8px 20px rgba(65, 80, 180, 0.12);
}

.custom-match-card:last-child {
  margin-bottom: 0;
}

.custom-match-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid #ebeef5;
}

.custom-match-title {
  display: flex;
  align-items: center;
  gap: 12px;
  font-weight: 600;
  color: #303133;
}

.custom-match-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.custom-match-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.custom-match-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.custom-match-label {
  font-size: 13px;
  font-weight: 600;
  color: #606266;
}

.custom-match-hint {
  font-size: 12px;
  color: #909399;
  line-height: 1.4;
}
</style>
