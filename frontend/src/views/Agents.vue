<template>
  <div class="agents-page">
    <div class="page-header">
      <div class="title-block">
        <h2>Agent 管理</h2>
        <p>管理您的远程 Agent 和服务状态</p>
      </div>
      <div class="header-actions">
        <el-button
          class="action-btn action-secondary"
          @click="loadAgents"
        >
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
        <el-button
          type="primary"
          class="action-btn action-primary"
          @click="handleGenerateScript"
        >
          <el-icon><Document /></el-icon>
          生成安装脚本
        </el-button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon" style="background: #e6f4ff; color: #1890ff;">
              <el-icon><Monitor /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ agents.length }}</div>
              <div class="stat-label">总 Agent 数</div>
            </div>
          </div>
        </el-card>
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon" style="background: rgba(139, 143, 255, 0.12); color: #8b8fff;">
              <el-icon><SuccessFilled /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ onlineCount }}</div>
              <div class="stat-label">在线</div>
            </div>
          </div>
        </el-card>
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon" style="background: #fff7e6; color: #fa8c16;">
              <el-icon><WarningFilled /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-value">{{ offlineCount }}</div>
              <div class="stat-label">离线</div>
            </div>
          </div>
        </el-card>
      </div>

    <!-- Agent 列表 -->
    <div v-if="agents.length === 0" class="empty-state">
      <el-empty description="暂无 Agent，请生成安装脚本部署" />
    </div>

    <div v-else class="agents-grid">
      <div
        v-for="agent in agents"
        :key="agent.id"
        class="agent-card"
      >
        <div class="card-header">
          <div class="card-title-group">
            <div class="card-title">{{ agent.name }}</div>
          </div>
          <div class="card-meta">
            <span class="meta-pill type-pill" :class="agent.service_type === 'mihomo' ? 'type-mihomo' : 'type-mosdns'">
              {{ agent.service_type === 'mihomo' ? 'Mihomo' : 'MosDNS' }}
            </span>
            <span class="meta-pill status-pill" :class="agent.status === 'online' ? 'status-online' : 'status-offline'">
              {{ agent.status === 'online' ? '在线' : '离线' }}
            </span>
            <span v-if="agent.deployment_method" class="meta-pill deploy-pill" :class="'deploy-' + agent.deployment_method">
              {{ agent.deployment_method === 'shell' ? 'Shell' : agent.deployment_method === 'docker' ? 'Docker' : agent.deployment_method }}
            </span>
          </div>
        </div>

        <div class="card-section">
          <div class="section-label">
            <el-icon><Connection /></el-icon>
            地址
          </div>
          <div class="section-value">{{ agent.host }}:{{ agent.port }}</div>
        </div>

        <div class="card-section inline">
          <div class="section-label">
            <el-icon><Document /></el-icon>
            配置版本
          </div>
          <div class="section-value">{{ agent.config_version || 'N/A' }}</div>
        </div>

        <div class="card-section inline">
          <div class="section-label">
            <el-icon><Clock /></el-icon>
            最后心跳
          </div>
          <div class="section-value">{{ formatTime(agent.last_heartbeat) }}</div>
        </div>

        <div class="card-section inline">
          <div class="section-label">
            <el-icon><InfoFilled /></el-icon>
            Agent 版本
          </div>
          <div class="section-value">{{ agent.version || 'N/A' }}</div>
        </div>

        <!-- 系统监控指标 -->
        <div v-if="agent.system_metrics" class="metrics-section">
          <div class="metrics-header">
            <span>系统监控</span>
            <el-button text size="small" @click="showMetricsDetail(agent)">
              <el-icon><TrendCharts /></el-icon>
              详情
            </el-button>
          </div>

          <div class="metric-item">
            <div class="metric-label">
              <span>CPU</span>
              <span class="metric-value">{{ formatPercent(agent.system_metrics.cpu?.usage_percent) }}</span>
            </div>
            <el-progress
              :percentage="agent.system_metrics.cpu?.usage_percent || 0"
              :stroke-width="6"
              :color="getProgressColor(agent.system_metrics.cpu?.usage_percent)"
              :show-text="false"
            />
          </div>

          <div class="metric-item">
            <div class="metric-label">
              <span>内存</span>
              <span class="metric-value">{{ formatPercent(agent.system_metrics.memory?.used_percent) }}</span>
            </div>
            <el-progress
              :percentage="agent.system_metrics.memory?.used_percent || 0"
              :stroke-width="6"
              :color="getProgressColor(agent.system_metrics.memory?.used_percent)"
              :show-text="false"
            />
            <div class="metric-detail">{{ formatBytes(agent.system_metrics.memory?.used) }} / {{ formatBytes(agent.system_metrics.memory?.total) }}</div>
          </div>

          <div class="metric-item">
            <div class="metric-label">
              <span>磁盘</span>
              <span class="metric-value">{{ formatPercent(agent.system_metrics.disk?.used_percent) }}</span>
            </div>
            <el-progress
              :percentage="agent.system_metrics.disk?.used_percent || 0"
              :stroke-width="6"
              :color="getProgressColor(agent.system_metrics.disk?.used_percent)"
              :show-text="false"
            />
            <div class="metric-detail">{{ formatBytes(agent.system_metrics.disk?.used) }} / {{ formatBytes(agent.system_metrics.disk?.total) }}</div>
          </div>

          <div class="metric-item network">
            <div class="metric-label">
              <span>网络速率</span>
            </div>
            <div class="network-speeds">
              <div class="speed-item">
                <span class="speed-label">↑ 上传</span>
                <span class="speed-value">{{ formatSpeed(agent.system_metrics.network?.speed_sent) }}</span>
              </div>
              <div class="speed-item">
                <span class="speed-label">↓ 下载</span>
                <span class="speed-value">{{ formatSpeed(agent.system_metrics.network?.speed_recv) }}</span>
              </div>
            </div>
          </div>

          <div class="metric-item traffic-total" v-if="agent.system_metrics.network">
            <div class="metric-label">
              <span>总流量</span>
            </div>
            <div class="network-speeds">
              <div class="speed-item">
                <span class="speed-label">↑ 已发送</span>
                <span class="speed-value">{{ formatBytesShort(agent.system_metrics.network?.bytes_sent) }}</span>
              </div>
              <div class="speed-item">
                <span class="speed-label">↓ 已接收</span>
                <span class="speed-value">{{ formatBytesShort(agent.system_metrics.network?.bytes_recv) }}</span>
              </div>
            </div>
          </div>
        </div>

        <div class="card-actions">
          <el-button class="card-btn ghost" size="small" @click="pushConfig(agent)">
            <el-icon><Upload /></el-icon>
            推送配置
          </el-button>
          <el-button class="card-btn warning" size="small" @click="restartAgent(agent)">
            <el-icon><RefreshRight /></el-icon>
            重启
          </el-button>
          <el-button class="card-btn success" size="small" @click="viewLogs(agent)">
            <el-icon><View /></el-icon>
            日志
          </el-button>
        </div>
        <div class="card-actions" style="margin-top: 8px;">
          <el-button v-if="agent.has_update" class="card-btn primary" size="small" @click="updateAgent(agent)">
            <el-icon><Download /></el-icon>
            更新
          </el-button>
          <el-button class="card-btn danger" size="small" @click="uninstallAgent(agent)">
            <el-icon><Delete /></el-icon>
            卸载
          </el-button>
          <el-button class="card-btn info" size="small" :disabled="!isHeartbeatExpired(agent)" @click="deleteAgent(agent)">
            <el-icon><Close /></el-icon>
            删除记录
          </el-button>
        </div>
      </div>
    </div>

    <!-- 生成安装脚本对话框 -->
    <el-dialog
      v-model="scriptDialogVisible"
      title="生成 Agent 安装脚本"
      width="700px"
      :close-on-click-modal="false"
      append-to-body
      destroy-on-close
      class="script-dialog"
    >
      <el-form v-if="scriptDialogVisible" :key="formKey" :model="scriptForm" label-width="120px">
        <el-form-item label="安装类型">
          <div>
            <el-radio-group v-model="scriptForm.installType" @change="onInstallTypeChange">
              <el-radio label="shell">Shell 安装</el-radio>
              <el-radio label="docker">Docker 容器</el-radio>
              <el-radio label="docker-mihomo">Docker (Mihomo内置)</el-radio>
              <el-radio label="docker-mosdns">Docker (mosdns内置)</el-radio>
              <el-radio label="docker-aio">Docker (All-in-One)</el-radio>
            </el-radio-group>
            <div style="margin-top: 8px; color: #909399; font-size: 12px; line-height: 1.5;">
              <template v-if="scriptForm.installType === 'shell'">
                Shell 安装：将 Agent 直接安装到系统服务
              </template>
              <template v-else-if="scriptForm.installType === 'docker'">
                Docker 容器：使用 Docker 容器运行 Agent
              </template>
              <template v-else-if="scriptForm.installType === 'docker-mihomo'">
                Docker (Mihomo内置)：内置 Mihomo，一个容器运行 Agent+Mihomo
              </template>
              <template v-else-if="scriptForm.installType === 'docker-mosdns'">
                Docker (mosdns内置)：内置 mosdns，一个容器运行 Agent+mosdns
              </template>
              <template v-else>
                Docker (三合一)：内置 Mihomo+mosdns，一个容器运行 Agent+Mihomo+mosdns
              </template>
            </div>
          </div>
        </el-form-item>

        <el-form-item label="Agent 名称">
          <el-input v-model="scriptForm.name" placeholder="例如：香港服务器" />
        </el-form-item>

        <el-form-item v-if="scriptForm.installType !== 'docker-aio'" label="服务类型">
          <el-radio-group
            v-model="scriptForm.type"
            @change="onServiceTypeChange"
            :disabled="scriptForm.installType === 'docker-mihomo' || scriptForm.installType === 'docker-mosdns'"
          >
            <el-radio
              v-for="option in serviceTypeOptions"
              :key="option.value"
              :label="option.value"
            >
              {{ option.label }}
            </el-radio>
          </el-radio-group>
          <div style="margin-top: 8px; color: #909399; font-size: 12px" v-if="scriptForm.installType === 'docker-mihomo'">
            Docker Mihomo 版本仅支持 Mihomo 服务
          </div>
          <div style="margin-top: 8px; color: #909399; font-size: 12px" v-if="scriptForm.installType === 'docker-mosdns'">
            Docker mosdns 版本仅支持 mosdns 服务
          </div>
        </el-form-item>

        <el-form-item label="Agent 端口" v-if="scriptForm.installType !== 'docker-aio'">
          <el-input-number v-model="scriptForm.port" :min="1024" :max="65535" />
        </el-form-item>

        <el-form-item label="Agent 端口" v-else>
          <div style="color: #909399; font-size: 14px; line-height: 1.8;">
            <div>• Mihomo Agent: <span style="color: #409EFF; font-weight: 600;">8080</span></div>
            <div>• MosDNS Agent: <span style="color: #67C23A; font-weight: 600;">8081</span></div>
            <div style="margin-top: 4px; font-size: 12px;">All-in-One 版本使用固定端口</div>
          </div>
        </el-form-item>

        <el-form-item label="Agent IP">
          <el-input v-model="scriptForm.agent_ip" placeholder="可选，留空则自动获取">
            <template #append>
              <el-tooltip content="可选：指定 Agent 的 IP 地址，留空则脚本会自动获取" placement="top">
                <el-icon><QuestionFilled /></el-icon>
              </el-tooltip>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item label="配置文件路径" v-if="scriptForm.installType === 'shell'">
          <el-input v-model="scriptForm.config_path" placeholder="/etc/mihomo/config.yaml">
            <template #append>
              <el-tooltip content="Agent 拉取配置后保存的文件路径" placement="top">
                <el-icon><QuestionFilled /></el-icon>
              </el-tooltip>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item label="重启命令" v-if="scriptForm.installType === 'shell'">
          <el-input v-model="scriptForm.restart_command" placeholder="命令或URL">
            <template #append>
              <el-tooltip content="用于完全重启服务，会短暂中断" placement="top">
                <el-icon><QuestionFilled /></el-icon>
              </el-tooltip>
            </template>
          </el-input>
          <div style="margin-top: 8px; color: #909399; font-size: 12px; line-height: 1.5;">
            支持两种方式：<br/>
            • 命令方式：systemctl restart mihomo<br/>
            • URL方式：http://127.0.0.1:9090/restart
          </div>
        </el-form-item>

        <!-- Docker 特有字段 -->
        <template v-if="scriptForm.installType === 'docker' || scriptForm.installType === 'docker-mihomo' || scriptForm.installType === 'docker-mosdns' || scriptForm.installType === 'docker-aio'">
          <el-alert
            type="warning"
            :closable="false"
            style="margin-bottom: 16px;"
            v-if="scriptForm.installType === 'docker'"
          >
            <template #title>
              <div style="line-height: 1.6; font-size: 13px;">
                <strong>Docker Agent 使用说明：</strong><br/>
                • Docker Agent 只能管理通过 Docker 部署的 Mihomo/MosDNS 服务<br/>
                • 需要指定服务容器名称才能实现重启功能<br/>
                • Agent 容器需要挂载 Docker socket 才能控制其他容器
              </div>
            </template>
          </el-alert>

          <el-alert
            type="success"
            :closable="false"
            style="margin-bottom: 16px;"
            v-if="scriptForm.installType === 'docker-mihomo'"
          >
            <template #title>
              <div style="line-height: 1.6; font-size: 13px;">
                <strong>Docker Mihomo Agent 使用说明：</strong><br/>
                • 该镜像内置了 Mihomo，Agent 和 Mihomo 在同一个容器中运行<br/>
                • 无需单独部署 Mihomo 服务，一个容器即可完成<br/>
                • 支持自动配置拉取、更新和服务重启<br/>
                • 适合快速部署和测试环境
              </div>
            </template>
          </el-alert>

          <el-alert
            type="success"
            :closable="false"
            style="margin-bottom: 16px;"
            v-if="scriptForm.installType === 'docker-mosdns'"
          >
            <template #title>
              <div style="line-height: 1.6; font-size: 13px;">
                <strong>Docker mosdns Agent 使用说明：</strong><br/>
                • 该镜像内置了 mosdns，Agent 和 mosdns 在同一个容器中运行<br/>
                • 无需单独部署 mosdns 服务，一个容器即可完成<br/>
                • 支持自动配置拉取、更新和服务重启<br/>
                • 适合快速部署和测试环境
              </div>
            </template>
          </el-alert>

          <el-alert
            type="success"
            :closable="false"
            style="margin-bottom: 16px;"
            v-if="scriptForm.installType === 'docker-aio'"
          >
            <template #title>
              <div style="line-height: 1.6; font-size: 13px;">
                <strong>Docker All-in-One Agent 使用说明：</strong><br/>
                • 该镜像内置了 Mihomo 和 mosdns，可同时运行多个服务<br/>
                • Mihomo Agent (端口 8080) + mosdns Agent (端口 8081)<br/>
                • 支持通过环境变量启用/禁用任一服务<br/>
                • 适合需要同时部署 Mihomo 和 mosdns 的场景
              </div>
            </template>
          </el-alert>

          <el-form-item label="Docker 镜像">
            <el-input v-model="scriptForm.dockerImage" placeholder="默认使用官方镜像">
              <template #append>
                <el-tooltip content="可选：指定自定义 Docker 镜像，留空则使用默认镜像" placement="top">
                  <el-icon><QuestionFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>

          <el-form-item label="Agent 容器名">
            <el-input v-model="scriptForm.containerName" placeholder="configflow-agent">
              <template #append>
                <el-tooltip content="Agent 的 Docker 容器名称" placement="top">
                  <el-icon><QuestionFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>

          <el-form-item label="服务容器名" v-if="scriptForm.installType === 'docker'">
            <el-input v-model="scriptForm.serviceContainerName" placeholder="mihomo">
              <template #append>
                <el-tooltip content="要管理的 Mihomo/MosDNS 服务的容器名称，用于重启功能" placement="top">
                  <el-icon><QuestionFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              填写后 Agent 可以通过 Docker 重启该服务容器
            </div>
          </el-form-item>

          <el-form-item label="网络模式">
            <el-radio-group v-model="scriptForm.networkMode">
              <el-radio
                v-for="option in networkModeOptions"
                :key="option.value"
                :label="option.value"
              >
                {{ option.label }}
              </el-radio>
            </el-radio-group>
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              host 模式可直接访问主机网络，bridge 模式需要端口映射
            </div>
          </el-form-item>
        </template>
      </el-form>

      <el-divider content-position="left">
        <span style="font-weight: 600; color: #6B73FF;">一键安装命令</span>
      </el-divider>

      <div v-if="installCommand || dockerComposeContent || dockerRunCommand" class="install-command-box">
        <el-alert
          type="success"
          :closable="false"
          style="margin-bottom: 16px;"
          v-if="scriptForm.installType === 'shell'"
        >
          <template #title>
            <div style="line-height: 1.6;">
              在远程服务器上执行以下命令即可自动安装 Agent<br/>
              <span style="font-size: 12px; color: #67C23A;">
                ✓ 支持 Ubuntu、Debian、CentOS、Alpine Linux 等系统<br/>
                ✓ 自动检测并配置 systemd 或 OpenRC 服务管理器
              </span>
            </div>
          </template>
        </el-alert>

        <el-alert
          type="info"
          :closable="false"
          style="margin-bottom: 16px;"
          v-if="scriptForm.installType === 'docker' || scriptForm.installType === 'docker-mihomo' || scriptForm.installType === 'docker-mosdns' || scriptForm.installType === 'docker-aio'"
        >
          <template #title>
            <div style="line-height: 1.6;">
              <template v-if="scriptForm.installType === 'docker'">
                使用 Docker Compose 快速部署 Agent<br/>
                <span style="font-size: 12px; color: #409EFF;">
                  ✓ 轻量级容器化部署，隔离性好<br/>
                  ✓ 支持自动重启和健康检查
                </span>
              </template>
              <template v-else-if="scriptForm.installType === 'docker-mihomo'">
                使用 Docker 快速部署 Mihomo Agent<br/>
                <span style="font-size: 12px; color: #409EFF;">
                  ✓ 内置 Mihomo，一个容器运行 Agent+Mihomo<br/>
                  ✓ 支持自动配置拉取、更新和服务重启
                </span>
              </template>
              <template v-else-if="scriptForm.installType === 'docker-mosdns'">
                使用 Docker 快速部署 mosdns Agent<br/>
                <span style="font-size: 12px; color: #409EFF;">
                  ✓ 内置 mosdns，一个容器运行 Agent+mosdns<br/>
                  ✓ 支持自动配置拉取、更新和服务重启
                </span>
              </template>
              <template v-else>
                使用 Docker 快速部署 All-in-One Agent<br/>
                <span style="font-size: 12px; color: #409EFF;">
                  ✓ 内置 Mihomo 和 mosdns，一个容器同时运行两个服务<br/>
                  ✓ 支持通过环境变量控制启用哪些服务
                </span>
              </template>
            </div>
          </template>
        </el-alert>

        <!-- Shell 安装命令 -->
        <template v-if="scriptForm.installType === 'shell'">
          <!-- Ubuntu/Debian/CentOS 命令 -->
          <div class="command-section">
            <div class="command-label">
              <span style="font-weight: 600; color: #409EFF;">Ubuntu / Debian / CentOS</span>
              <span style="font-size: 12px; color: #909399; margin-left: 8px;">(使用 systemd)</span>
            </div>
            <div class="command-container">
              <el-input
                v-model="installCommand"
                type="textarea"
                :rows="3"
                readonly
                class="command-input"
              />
              <el-button
                type="primary"
                size="large"
                @click="copyCommand"
                style="margin-top: 12px; width: 100%;"
              >
                <el-icon><DocumentCopy /></el-icon>
                复制命令
              </el-button>
            </div>
          </div>

          <!-- Alpine Linux 命令 -->
          <div class="command-section" style="margin-top: 20px;">
            <div class="command-label">
              <span style="font-weight: 600; color: #67C23A;">Alpine Linux</span>
              <span style="font-size: 12px; color: #909399; margin-left: 8px;">(使用 OpenRC)</span>
            </div>
            <div class="command-container">
              <el-input
                v-model="installCommandAlpine"
                type="textarea"
                :rows="3"
                readonly
                class="command-input alpine"
              />
              <el-button
                type="success"
                size="large"
                @click="copyCommandAlpine"
                style="margin-top: 12px; width: 100%;"
              >
                <el-icon><DocumentCopy /></el-icon>
                复制命令
              </el-button>
            </div>
          </div>
        </template>

        <!-- Docker Compose 内容 -->
        <template v-if="scriptForm.installType === 'docker' || scriptForm.installType === 'docker-mihomo' || scriptForm.installType === 'docker-mosdns' || scriptForm.installType === 'docker-aio'">
          <!-- Docker Run 命令 -->
          <div class="command-section">
            <div class="command-label">
              <span style="font-weight: 600; color: #409EFF;">Docker Run 命令</span>
              <span style="font-size: 12px; color: #909399; margin-left: 8px;">(推荐)</span>
            </div>
            <div class="command-container">
              <el-input
                v-model="dockerRunCommand"
                type="textarea"
                :rows="8"
                readonly
                class="command-input docker-run"
              />
              <el-button
                type="primary"
                size="large"
                @click="copyDockerRun"
                style="margin-top: 12px; width: 100%;"
              >
                <el-icon><DocumentCopy /></el-icon>
                复制 Docker Run 命令
              </el-button>
            </div>
          </div>

          <!-- Docker Compose 文件 -->
          <div class="command-section" style="margin-top: 20px;">
            <div class="command-label">
              <span style="font-weight: 600; color: #67C23A;">docker-compose.yml</span>
              <span style="font-size: 12px; color: #909399; margin-left: 8px;">(可选)</span>
            </div>
            <div class="command-container">
              <el-input
                v-model="dockerComposeContent"
                type="textarea"
                :rows="15"
                readonly
                class="command-input docker-compose"
              />
              <el-button
                type="success"
                size="large"
                @click="copyDockerCompose"
                style="margin-top: 12px; width: 100%;"
              >
                <el-icon><DocumentCopy /></el-icon>
                复制 docker-compose.yml
              </el-button>
            </div>
          </div>

          <el-alert
            type="info"
            :closable="false"
            style="margin-top: 16px;"
          >
            <template #title>
              <div style="line-height: 1.6; font-size: 13px;">
                <strong>部署方式：</strong><br/>
                <br/>
                <strong style="color: #409EFF;">方式一：使用 Docker Run（推荐）</strong><br/>
                直接在服务器上执行上面的 Docker Run 命令即可<br/>
                <br/>
                <strong style="color: #67C23A;">方式二：使用 Docker Compose</strong><br/>
                1. 保存 docker-compose.yml 文件<br/>
                2. 在同目录执行: <code style="background: #f5f7fa; padding: 2px 6px; border-radius: 3px;">docker-compose up -d</code><br/>
                3. 查看日志: <code style="background: #f5f7fa; padding: 2px 6px; border-radius: 3px;">docker-compose logs -f</code><br/>
                <br/>
                <strong style="color: #E6A23C;">重要提示：</strong><br/>
                <template v-if="scriptForm.installType === 'docker'">
                  • 如果配置了服务容器名称，Agent 将能够通过 Docker 重启该服务<br/>
                  • 确保服务容器与 Agent 在同一 Docker 网络或主机上<br/>
                  • Agent 需要访问 Docker socket 才能控制其他容器
                </template>
                <template v-else-if="scriptForm.installType === 'docker-mihomo'">
                  • 该镜像内置了 Mihomo，Agent 和 Mihomo 在同一容器中运行<br/>
                  • 会自动创建 ./mihomo 目录存储配置文件<br/>
                  • 代理端口：7890 (HTTP)、7891 (SOCKS5)、9090 (API)
                </template>
                <template v-else-if="scriptForm.installType === 'docker-mosdns'">
                  • 该镜像内置了 mosdns，Agent 和 mosdns 在同一容器中运行<br/>
                  • 会自动创建 ./mosdns 目录存储配置文件<br/>
                  • DNS 端口：53 (TCP/UDP)
                </template>
                <template v-else>
                  • 该镜像内置了 Mihomo 和 mosdns，可同时运行两个服务<br/>
                  • 会自动创建 ./mihomo 和 ./mosdns 目录存储配置文件<br/>
                  • Mihomo Agent: 8080，mosdns Agent: 8081<br/>
                  • 可通过环境变量 ENABLE_MIHOMO/ENABLE_MOSDNS 控制启用哪些服务
                </template>
              </div>
            </template>
          </el-alert>
        </template>

        <el-divider />

        <el-collapse v-model="activeCollapse" style="margin-top: 16px;">
          <el-collapse-item title="查看完整安装脚本" name="script">
            <el-input
              v-model="installScript"
              type="textarea"
              :rows="15"
              readonly
              class="script-textarea"
            />
          </el-collapse-item>
        </el-collapse>
      </div>

      <el-button
        v-else
        type="primary"
        size="large"
        @click="generateScript"
        style="width: 100%;"
      >
        <el-icon><Document /></el-icon>
        {{ (scriptForm.installType === 'docker' || scriptForm.installType === 'docker-mihomo' || scriptForm.installType === 'docker-mosdns' || scriptForm.installType === 'docker-aio') ? '生成 Docker 部署命令' : '生成安装命令' }}
      </el-button>

      <template #footer>
        <el-button @click="scriptDialogVisible = false">关闭</el-button>
        <el-button
          v-if="installCommand"
          @click="resetForm"
        >
          重新生成
        </el-button>
      </template>
    </el-dialog>

    <!-- 查看日志对话框 -->
    <el-dialog
      v-model="logsDialogVisible"
      :title="`Agent 日志 - ${currentAgent?.name}`"
      width="900px"
      :close-on-click-modal="false"
    >
      <div class="logs-config-section">
        <el-form :inline="true" size="default">
          <el-form-item label="日志文件">
            <el-select
              v-model="selectedLogPath"
              placeholder="选择日志文件"
              style="width: 280px"
              @change="onLogPathChange"
            >
              <el-option
                v-for="opt in logPathOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
              <el-option label="自定义路径..." value="custom" />
            </el-select>
          </el-form-item>

          <el-form-item v-if="selectedLogPath === 'custom'" label="">
            <el-input
              v-model="customLogPath"
              placeholder="/var/log/your-file.log"
              style="width: 300px"
              @keyup.enter="validateAndLoadCustomPath"
            >
              <template #append>
                <el-button @click="validateAndLoadCustomPath" :loading="validatingPath">
                  验证
                </el-button>
              </template>
            </el-input>
          </el-form-item>
        </el-form>
      </div>

      <div class="logs-container">
        <el-input
          ref="logsTextareaRef"
          v-model="logs"
          type="textarea"
          :rows="18"
          readonly
          class="logs-textarea"
          placeholder="暂无日志"
        />
      </div>

      <template #footer>
        <div class="logs-footer">
          <div class="logs-footer-left">
            <el-switch
              v-if="isMainAgentLog"
              v-model="loggingEnabled"
              @change="toggleLogging"
              active-text="日志已启用"
              inactive-text="日志已禁用"
              :loading="togglingLogging"
            />
          </div>
          <div class="logs-footer-right">
            <el-button @click="logsDialogVisible = false">关闭</el-button>
            <el-button type="danger" plain @click="clearLogs">
              <el-icon><Delete /></el-icon>
              清空
            </el-button>
            <el-button type="primary" @click="refreshLogs">
              <el-icon><Refresh /></el-icon>
              刷新
            </el-button>
          </div>
        </div>
      </template>
    </el-dialog>

    <!-- 监控详情对话框 -->
    <el-dialog
      v-model="metricsDialogVisible"
      :title="`系统监控 - ${currentMetricsAgent?.name}`"
      width="90%"
      :close-on-click-modal="false"
      top="5vh"
    >
      <div v-loading="metricsLoading" class="metrics-dialog-content">
        <div v-if="metricsHistory.length === 0 && !metricsLoading" class="empty-state">
          <el-empty description="暂无监控数据" />
        </div>

        <div v-else class="metrics-charts">
          <!-- 最新数据摘要 -->
          <div v-if="currentMetricsAgent?.system_metrics" class="metrics-summary-header">
            <el-descriptions :column="5" border size="small">
              <el-descriptions-item label="CPU 使用率">
                <span :style="{ color: getProgressColor(currentMetricsAgent.system_metrics.cpu?.usage_percent) }">
                  {{ formatPercent(currentMetricsAgent.system_metrics.cpu?.usage_percent) }}
                </span>
                <span style="color: #909399; font-size: 11px; margin-left: 8px;">
                  ({{ currentMetricsAgent.system_metrics.cpu?.core_count }} 核)
                </span>
              </el-descriptions-item>
              <el-descriptions-item label="内存使用率">
                <span :style="{ color: getProgressColor(currentMetricsAgent.system_metrics.memory?.used_percent) }">
                  {{ formatPercent(currentMetricsAgent.system_metrics.memory?.used_percent) }}
                </span>
                <span style="color: #909399; font-size: 11px; margin-left: 8px;">
                  {{ formatBytes(currentMetricsAgent.system_metrics.memory?.used) }} / {{ formatBytes(currentMetricsAgent.system_metrics.memory?.total) }}
                </span>
              </el-descriptions-item>
              <el-descriptions-item label="网络速度">
                <span style="color: #409EFF;">↑ {{ formatNetworkSpeed(currentMetricsAgent.system_metrics.network?.speed_sent) }}</span>
                <span style="color: #67C23A; margin-left: 8px;">↓ {{ formatNetworkSpeed(currentMetricsAgent.system_metrics.network?.speed_recv) }}</span>
              </el-descriptions-item>
              <el-descriptions-item label="总流量">
                <span style="color: #409EFF;">↑ {{ formatBytes(currentMetricsAgent.system_metrics.network?.bytes_sent) }}</span>
                <span style="color: #67C23A; margin-left: 8px;">↓ {{ formatBytes(currentMetricsAgent.system_metrics.network?.bytes_recv) }}</span>
              </el-descriptions-item>
              <el-descriptions-item label="磁盘使用率">
                <span :style="{ color: getProgressColor(currentMetricsAgent.system_metrics.disk?.used_percent) }">
                  {{ formatPercent(currentMetricsAgent.system_metrics.disk?.used_percent) }}
                </span>
                <span style="color: #909399; font-size: 11px; margin-left: 8px;">
                  {{ formatBytes(currentMetricsAgent.system_metrics.disk?.used) }} / {{ formatBytes(currentMetricsAgent.system_metrics.disk?.total) }}
                </span>
              </el-descriptions-item>
            </el-descriptions>
          </div>

          <!-- 图表网格 -->
          <div class="charts-grid">
            <div class="chart-card">
              <v-chart :option="cpuChartOption" :autoresize="true" style="height: 280px;" />
            </div>
            <div class="chart-card">
              <v-chart :option="memoryChartOption" :autoresize="true" style="height: 280px;" />
            </div>
            <div class="chart-card">
              <v-chart :option="networkChartOption" :autoresize="true" style="height: 280px;" />
            </div>
            <div class="chart-card">
              <v-chart :option="trafficChartOption" :autoresize="true" style="height: 280px;" />
            </div>
            <div class="chart-card">
              <v-chart :option="diskChartOption" :autoresize="true" style="height: 280px;" />
            </div>
          </div>

          <!-- 数据信息 -->
          <div class="metrics-info">
            <el-text size="small" type="info">
              显示最近 24 小时的监控数据，共 {{ metricsHistory.length }} 个数据点
            </el-text>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button @click="metricsDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="showMetricsDetail(currentMetricsAgent)">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox, ElLoading } from 'element-plus'
import { Document, Refresh, Monitor, SuccessFilled, WarningFilled, DocumentCopy, QuestionFilled, Connection, Clock, InfoFilled, Upload, RefreshRight, View, Delete, Close, Download, TrendCharts } from '@element-plus/icons-vue'
import { agentApi } from '@/api'
import api from '@/api'
import type { Agent } from '@/types'
import { use } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent, TitleComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

// Register ECharts components
use([LineChart, GridComponent, TooltipComponent, LegendComponent, TitleComponent, CanvasRenderer])

const agents = ref<Agent[]>([])
const scriptDialogVisible = ref(false)
const logsDialogVisible = ref(false)
const metricsDialogVisible = ref(false)
const installScript = ref('')
const installCommand = ref('')
const installCommandAlpine = ref('')
const dockerComposeContent = ref('')
const dockerRunCommand = ref('')
const activeCollapse = ref<string[]>([])
const logs = ref('')
const selectedLogPath = ref('/var/log/configflow-agent.log')
const customLogPath = ref('')
const loggingEnabled = ref(true)
const togglingLogging = ref(false)
const validatingPath = ref(false)
const currentAgent = ref<Agent | null>(null)
const currentMetricsAgent = ref<Agent | null>(null)
const metricsHistory = ref<any[]>([])
const metricsLoading = ref(false)
const logsTextareaRef = ref()
const formKey = ref(0) // 用于强制重新渲染表单

const scriptForm = ref({
  installType: 'shell',
  name: '',
  type: 'mihomo',
  port: 8080,
  agent_ip: '',
  config_path: '/etc/mihomo/config.yaml',
  restart_command: 'systemctl restart mihomo',
  dockerImage: '',
  containerName: 'configflow-agent',
  serviceContainerName: '',
  networkMode: 'bridge'
})

// 服务类型选项
const serviceTypeOptions = [
  { label: 'Mihomo', value: 'mihomo' },
  { label: 'MosDNS', value: 'mosdns' }
]

// 网络模式选项
const networkModeOptions = [
  { label: 'bridge (桥接)', value: 'bridge' },
  { label: 'host (主机网络)', value: 'host' }
]

// 定时刷新
let refreshTimer: number | null = null

// 生成随机端口号（范围：10000-60000）
const generateRandomPort = (): number => {
  return Math.floor(Math.random() * (60000 - 10000 + 1)) + 10000
}

// 统计数据
const onlineCount = computed(() => {
  return agents.value.filter(a => a.status === 'online').length
})

const offlineCount = computed(() => {
  return agents.value.filter(a => a.status === 'offline').length
})

// CPU 使用率图表配置
const cpuChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.cpu?.usage_percent?.toFixed(1) || 0)

  return {
    title: {
      text: 'CPU 使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>CPU: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: 'CPU',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(64, 158, 255, 0.3)' },
            { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#409EFF' },
      itemStyle: { color: '#409EFF' }
    }]
  }
})

// 内存使用率图表配置
const memoryChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.memory?.used_percent?.toFixed(1) || 0)

  return {
    title: {
      text: '内存使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>内存: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: '内存',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(103, 194, 58, 0.3)' },
            { offset: 1, color: 'rgba(103, 194, 58, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#67C23A' },
      itemStyle: { color: '#67C23A' }
    }]
  }
})

// 磁盘使用率图表配置
const diskChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const data = metricsHistory.value.map(m => m.disk?.used_percent?.toFixed(1) || 0)

  return {
    title: {
      text: '磁盘使用率',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const item = params[0]
        return `${item.name}<br/>磁盘: ${item.value}%`
      }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '3%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      max: 100,
      axisLabel: {
        formatter: '{value}%',
        fontSize: 11
      }
    },
    series: [{
      name: '磁盘',
      type: 'line',
      smooth: true,
      data: data,
      areaStyle: {
        color: {
          type: 'linear',
          x: 0, y: 0, x2: 0, y2: 1,
          colorStops: [
            { offset: 0, color: 'rgba(230, 162, 60, 0.3)' },
            { offset: 1, color: 'rgba(230, 162, 60, 0.05)' }
          ]
        }
      },
      lineStyle: { color: '#E6A23C' },
      itemStyle: { color: '#E6A23C' }
    }]
  }
})

// 网络速度图表配置
const networkChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const uploadData = metricsHistory.value.map(m => ((m.network?.speed_sent || 0) / 1024).toFixed(2))
  const downloadData = metricsHistory.value.map(m => ((m.network?.speed_recv || 0) / 1024).toFixed(2))

  return {
    title: {
      text: '网络速度',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        return `${params[0].name}<br/>
          上传: ${params[0].value} KB/s<br/>
          下载: ${params[1].value} KB/s`
      }
    },
    legend: {
      data: ['上传', '下载'],
      bottom: 0,
      textStyle: { fontSize: 11 }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '12%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        formatter: '{value} KB/s',
        fontSize: 11
      }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        smooth: true,
        data: uploadData,
        lineStyle: { color: '#409EFF' },
        itemStyle: { color: '#409EFF' }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        lineStyle: { color: '#67C23A' },
        itemStyle: { color: '#67C23A' }
      }
    ]
  }
})

// 流量统计图表配置
const trafficChartOption = computed(() => {
  const timestamps = metricsHistory.value.map(m => {
    const date = new Date(m.timestamp)
    return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
  })
  const uploadData = metricsHistory.value.map(m => ((m.network?.bytes_sent || 0) / (1024 * 1024 * 1024)).toFixed(3))
  const downloadData = metricsHistory.value.map(m => ((m.network?.bytes_recv || 0) / (1024 * 1024 * 1024)).toFixed(3))

  return {
    title: {
      text: '流量统计',
      left: 'center',
      top: 10,
      textStyle: { fontSize: 14 }
    },
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const upload = parseFloat(params[0].value).toFixed(2)
        const download = parseFloat(params[1].value).toFixed(2)
        return `${params[0].name}<br/>
          上传: ${upload} GB<br/>
          下载: ${download} GB`
      }
    },
    legend: {
      data: ['上传', '下载'],
      bottom: 0,
      textStyle: { fontSize: 11 }
    },
    grid: {
      left: '3%',
      right: '4%',
      top: '15%',
      bottom: '12%',
      containLabel: true
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisLabel: { fontSize: 11 }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        formatter: '{value} GB',
        fontSize: 11
      }
    },
    series: [
      {
        name: '上传',
        type: 'line',
        smooth: true,
        data: uploadData,
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(64, 158, 255, 0.3)' },
              { offset: 1, color: 'rgba(64, 158, 255, 0.05)' }
            ]
          }
        },
        lineStyle: { color: '#409EFF' },
        itemStyle: { color: '#409EFF' }
      },
      {
        name: '下载',
        type: 'line',
        smooth: true,
        data: downloadData,
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(103, 194, 58, 0.3)' },
              { offset: 1, color: 'rgba(103, 194, 58, 0.05)' }
            ]
          }
        },
        lineStyle: { color: '#67C23A' },
        itemStyle: { color: '#67C23A' }
      }
    ]
  }
})

// 加载 Agent 列表
const loadAgents = async () => {
  try {
    const { data } = await agentApi.getAll()
    agents.value = data
  } catch (error) {
    ElMessage.error('加载 Agent 列表失败')
  }
}

// 格式化时间
const formatTime = (timeStr: string) => {
  if (!timeStr) return 'N/A'
  try {
    const date = new Date(timeStr)
    const now = new Date()
    const diff = Math.floor((now.getTime() - date.getTime()) / 1000)

    if (diff < 60) return `${diff} 秒前`
    if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
    if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
    return `${Math.floor(diff / 86400)} 天前`
  } catch (e) {
    return 'N/A'
  }
}

// 判断心跳是否超过5分钟（允许删除记录）
const isHeartbeatExpired = (agent: Agent) => {
  if (!agent.last_heartbeat) return true
  try {
    const date = new Date(agent.last_heartbeat)
    const now = new Date()
    return (now.getTime() - date.getTime()) > 5 * 60 * 1000
  } catch {
    return true
  }
}

// 格式化百分比
const formatPercent = (value: number | undefined) => {
  if (value === undefined || value === null) return 'N/A'
  return `${value.toFixed(1)}%`
}

// 格式化字节数
const formatBytes = (bytes: number | undefined) => {
  if (bytes === undefined || bytes === null) return 'N/A'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`
}

// 格式化网络速度
const formatSpeed = (bytesPerSec: number | undefined) => {
  if (bytesPerSec === undefined || bytesPerSec === null) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let speed = bytesPerSec
  let unitIndex = 0
  while (speed >= 1024 && unitIndex < units.length - 1) {
    speed /= 1024
    unitIndex++
  }
  return `${speed.toFixed(1)} ${units[unitIndex]}`
}

// 格式化网络速度（别名）
const formatNetworkSpeed = formatSpeed

// 格式化字节数（短格式，用于卡片显示）
const formatBytesShort = (bytes: number | undefined) => {
  if (bytes === undefined || bytes === null) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  // 使用较少的小数位使显示更紧凑
  return `${size.toFixed(1)} ${units[unitIndex]}`
}

// 获取进度条颜色
const getProgressColor = (percent: number | undefined) => {
  if (percent === undefined || percent === null) return '#409EFF'
  if (percent < 60) return '#67C23A' // 绿色
  if (percent < 80) return '#E6A23C' // 橙色
  return '#F56C6C' // 红色
}

// 显示监控详情
const showMetricsDetail = async (agent: any) => {
  currentMetricsAgent.value = agent
  metricsDialogVisible.value = true
  metricsLoading.value = true

  try {
    // 获取最近 24 小时的历史数据
    const response = await api.get(`/agents/${agent.id}/metrics/history?hours=24`)
    if (response.data.success) {
      metricsHistory.value = response.data.data.history || []
    } else {
      ElMessage.error('获取监控历史数据失败')
      metricsHistory.value = []
    }
  } catch (error) {
    console.error('Error fetching metrics history:', error)
    ElMessage.error('获取监控历史数据失败')
    metricsHistory.value = []
  } finally {
    metricsLoading.value = false
  }
}

// 安装类型变化时重置相关字段
const onInstallTypeChange = () => {
  // 清空之前生成的命令
  installCommand.value = ''
  installCommandAlpine.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''

  if (scriptForm.value.installType === 'docker' || scriptForm.value.installType === 'docker-mihomo' || scriptForm.value.installType === 'docker-mosdns' || scriptForm.value.installType === 'docker-aio') {
    // 切换到 Docker 模式时，确保有默认值
    if (!scriptForm.value.containerName) {
      scriptForm.value.containerName = 'configflow-agent'
    }
    if (!scriptForm.value.networkMode) {
      scriptForm.value.networkMode = 'bridge'
    }

    // docker-mihomo 模式强制使用 mihomo 类型
    if (scriptForm.value.installType === 'docker-mihomo') {
      scriptForm.value.type = 'mihomo'
      // docker-mihomo 不需要服务容器名（Mihomo 在同一容器内）
      scriptForm.value.serviceContainerName = ''
    } else if (scriptForm.value.installType === 'docker-mosdns') {
      // docker-mosdns 模式强制使用 mosdns 类型
      scriptForm.value.type = 'mosdns'
      // docker-mosdns 不需要服务容器名（mosdns 在同一容器内）
      scriptForm.value.serviceContainerName = ''
    } else if (scriptForm.value.installType === 'docker-aio') {
      // docker-aio 模式不需要选择服务类型（同时支持两种服务）
      scriptForm.value.type = 'mihomo'  // 默认设置为 mihomo，但实际不使用
      scriptForm.value.serviceContainerName = ''
    } else {
      // 根据服务类型设置默认的服务容器名称
      if (!scriptForm.value.serviceContainerName) {
        scriptForm.value.serviceContainerName = scriptForm.value.type
      }
    }
  }
}

// 服务类型变化时更新默认配置路径和重启命令
const onServiceTypeChange = () => {
  if (scriptForm.value.type === 'mihomo') {
    scriptForm.value.config_path = '/etc/mihomo/config.yaml'
    scriptForm.value.restart_command = 'systemctl restart mihomo'
    // 如果是 Docker 模式，更新默认服务容器名称
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'mihomo'
    }
  } else if (scriptForm.value.type === 'mosdns') {
    scriptForm.value.config_path = '/etc/mosdns/config.yaml'
    scriptForm.value.restart_command = 'systemctl restart mosdns'
    // 如果是 Docker 模式，更新默认服务容器名称
    if (scriptForm.value.installType === 'docker' && !scriptForm.value.serviceContainerName) {
      scriptForm.value.serviceContainerName = 'mosdns'
    }
  }
}

// 处理生成脚本按钮点击
const handleGenerateScript = () => {
  showGenerateScriptDialog()
}

// 显示生成脚本对话框
const showGenerateScriptDialog = () => {
  // 清空其他值
  installScript.value = ''
  installCommand.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  activeCollapse.value = []

  // 重置表单值
  scriptForm.value = {
    installType: 'shell',
    name: '',
    type: 'mihomo',
    port: generateRandomPort(),
    agent_ip: '',
    config_path: '/etc/mihomo/config.yaml',
    restart_command: 'systemctl restart mihomo',
    dockerImage: '',
    containerName: 'configflow-agent',
    serviceContainerName: '',
    networkMode: 'bridge'
  }

  // 递增 formKey 强制重新渲染
  formKey.value++

  // 打开对话框 - v-if 会确保表单完全重新渲染
  scriptDialogVisible.value = true
}

// 重置表单
const resetForm = () => {
  installScript.value = ''
  installCommand.value = ''
  installCommandAlpine.value = ''
  dockerComposeContent.value = ''
  dockerRunCommand.value = ''
  activeCollapse.value = []
}

// 生成安装脚本
const generateScript = async () => {
  if (!scriptForm.value.name) {
    ElMessage.warning('请输入 Agent 名称')
    return
  }

  // Shell 安装时检查配置文件路径是否为文件而非目录
  if (scriptForm.value.installType === 'shell') {
    const configPath = scriptForm.value.config_path.trim()
    if (configPath && !configPath.match(/\.\w+$/)) {
      ElMessage.warning('配置文件路径应指向一个文件（如 config.yaml），而不是文件夹')
      return
    }
  }

  const loading = ElLoading.service({
    lock: true,
    text: (scriptForm.value.installType === 'docker' || scriptForm.value.installType === 'docker-mihomo' || scriptForm.value.installType === 'docker-mosdns' || scriptForm.value.installType === 'docker-aio') ? '正在生成 Docker 部署命令...' : '正在生成安装命令...',
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    console.log('开始生成脚本，参数：', scriptForm.value)

    if (scriptForm.value.installType === 'docker' || scriptForm.value.installType === 'docker-mihomo' || scriptForm.value.installType === 'docker-mosdns' || scriptForm.value.installType === 'docker-aio') {
      // 生成 Docker Compose 和 Docker Run
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin

      const dockerParams = {
        name: scriptForm.value.name,
        type: scriptForm.value.type,
        port: scriptForm.value.port,
        agent_ip: scriptForm.value.agent_ip || '',
        docker_image: scriptForm.value.dockerImage || '',
        container_name: scriptForm.value.containerName,
        service_container_name: scriptForm.value.serviceContainerName || '',
        network_mode: scriptForm.value.networkMode,
        server_url: serverUrl,
        install_type: scriptForm.value.installType  // 传递安装类型
      }

      // 根据安装类型选择不同的 API
      let composeResponse, runResponse
      if (scriptForm.value.installType === 'docker-mihomo') {
        // 使用 Docker Mihomo 专用 API
        [composeResponse, runResponse] = await Promise.all([
          api.get('/agents/docker-mihomo-compose', { params: dockerParams }),
          api.get('/agents/docker-mihomo-run', { params: dockerParams })
        ])
      } else if (scriptForm.value.installType === 'docker-mosdns') {
        // 使用 Docker mosdns 专用 API
        [composeResponse, runResponse] = await Promise.all([
          api.get('/agents/docker-mosdns-compose', { params: dockerParams }),
          api.get('/agents/docker-mosdns-run', { params: dockerParams })
        ])
      } else if (scriptForm.value.installType === 'docker-aio') {
        // 使用 Docker AIO 专用 API
        [composeResponse, runResponse] = await Promise.all([
          api.get('/agents/docker-aio-compose', { params: dockerParams }),
          api.get('/agents/docker-aio-run', { params: dockerParams })
        ])
      } else {
        // 使用原来的 Docker API
        [composeResponse, runResponse] = await Promise.all([
          agentApi.generateDockerCompose(dockerParams),
          agentApi.generateDockerRun(dockerParams)
        ])
      }

      dockerComposeContent.value = composeResponse.data
      dockerRunCommand.value = runResponse.data
      ElMessage.success('Docker 部署命令生成成功！')
    } else {
      // 生成 Shell 脚本
      const response = await agentApi.generateScript({
        name: scriptForm.value.name,
        type: scriptForm.value.type,
        port: scriptForm.value.port,
        agent_ip: scriptForm.value.agent_ip,
        config_path: scriptForm.value.config_path,
        restart_command: scriptForm.value.restart_command
      })

      console.log('API 响应：', response)

      // 保存完整脚本
      installScript.value = response.data

      // 获取服务域名配置（优先使用配置的域名，否则使用当前访问地址）
      const serverUrl = localStorage.getItem('serverDomain') || window.location.origin

      // 生成一键安装命令
      const params = new URLSearchParams({
        name: scriptForm.value.name,
        type: scriptForm.value.type,
        port: scriptForm.value.port.toString(),
        config_path: scriptForm.value.config_path,
        restart_command: scriptForm.value.restart_command,
        server_url: serverUrl  // 传递完整的服务器URL给后端
      })

      // 如果用户输入了 agent_ip，则添加到参数中
      if (scriptForm.value.agent_ip && scriptForm.value.agent_ip.trim()) {
        params.set('agent_ip', scriptForm.value.agent_ip.trim())
      }

      const scriptUrl = `${serverUrl}/api/agents/install-script?${params.toString()}`

      // 生成一键命令 - 标准 Linux
      installCommand.value = `curl -sSL "${scriptUrl}" | sudo bash`

      // 生成一键命令 - Alpine Linux
      installCommandAlpine.value = `curl -sSL "${scriptUrl}" | sh`

      ElMessage.success('安装命令生成成功！')
    }
  } catch (error: any) {
    console.error('生成脚本失败，错误详情：', error)
    console.error('错误响应：', error.response)
    const errorMsg = error.response?.data?.message || error.message || '生成脚本失败'
    ElMessage.error(errorMsg)
  } finally {
    loading.close()
  }
}

// 复制命令
const copyCommand = () => {
  if (!installCommand.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(installCommand.value).then(() => {
      ElMessage.success('命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(installCommand.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(installCommand.value)
  }
}

// 复制 Alpine 命令
const copyCommandAlpine = () => {
  if (!installCommandAlpine.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(installCommandAlpine.value).then(() => {
      ElMessage.success('Alpine 命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(installCommandAlpine.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(installCommandAlpine.value)
  }
}

// 复制 Docker Compose
const copyDockerCompose = () => {
  if (!dockerComposeContent.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(dockerComposeContent.value).then(() => {
      ElMessage.success('Docker Compose 已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(dockerComposeContent.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(dockerComposeContent.value)
  }
}

// 复制 Docker Run 命令
const copyDockerRun = () => {
  if (!dockerRunCommand.value) return

  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(dockerRunCommand.value).then(() => {
      ElMessage.success('Docker Run 命令已复制到剪贴板')
    }).catch(() => {
      fallbackCopy(dockerRunCommand.value)
    })
  } else {
    // 降级到传统方法
    fallbackCopy(dockerRunCommand.value)
  }
}

// 降级复制方法
const fallbackCopy = (text: string) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    ElMessage.success('内容已复制到剪贴板')
  } catch (err) {
    ElMessage.error('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

// 推送配置
const pushConfig = async (agent: Agent) => {
  const loading = ElLoading.service({
    lock: true,
    text: '正在推送配置...',
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    await agentApi.pushConfig(agent.id)
    ElMessage.success('配置推送成功')
    loadAgents()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '配置推送失败')
  } finally {
    loading.close()
  }
}

// 重启 Agent
const restartAgent = async (agent: Agent) => {
  try {
    await ElMessageBox.confirm(
      '确定要重启此 Agent 的服务吗？服务将会短暂中断。',
      '警告',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const loading = ElLoading.service({
      lock: true,
      text: '正在重启服务...',
      background: 'rgba(0, 0, 0, 0.7)'
    })

    try {
      await agentApi.restart(agent.id)
      ElMessage.success('服务重启成功')
    } finally {
      loading.close()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || '服务重启失败')
    }
  }
}

const updateAgent = async (agent: Agent) => {
  try {
    await ElMessageBox.confirm(
      `检测到新版本可用，是否立即更新 Agent？更新过程中 Agent 将会重启。`,
      '更新确认',
      {
        confirmButtonText: '立即更新',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const loading = ElLoading.service({
      lock: true,
      text: '正在更新 Agent，请稍候...',
      background: 'rgba(0, 0, 0, 0.7)'
    })

    try {
      const response = await agentApi.update(agent.id)
      loading.close()

      ElMessage.success('Agent 更新已启动，请等待重启完成')

      // 3秒后刷新列表
      setTimeout(() => {
        loadAgents()
      }, 3000)
    } catch (error: any) {
      loading.close()
      ElMessage.error(error.response?.data?.message || 'Agent 更新失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || 'Agent 更新失败')
    }
  }
}

// 卸载 Agent
const uninstallAgent = async (agent: Agent) => {
  try {
    await ElMessageBox.confirm(
      `确定要卸载远程服务器上的 Agent "${agent.name}" 吗？\n\n此操作将：\n• 停止 Agent 服务\n• 删除 Agent 程序文件\n• 删除服务配置（systemd/OpenRC）\n• 从管理列表中移除\n\n⚠️ 此操作不可恢复！`,
      '卸载 Agent',
      {
        confirmButtonText: '确定卸载',
        cancelButtonText: '取消',
        type: 'error',
        dangerouslyUseHTMLString: false
      }
    )

    const loading = ElLoading.service({
      lock: true,
      text: '正在卸载 Agent...',
      background: 'rgba(0, 0, 0, 0.7)'
    })

    try {
      await agentApi.uninstall(agent.id)
      ElMessage.success({
        message: 'Agent 卸载命令已发送，远程服务器正在执行卸载...',
        duration: 5000
      })
      // 等待几秒后刷新列表
      setTimeout(() => {
        loadAgents()
      }, 3000)
    } finally {
      loading.close()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || '卸载失败')
    }
  }
}

// 删除 Agent 记录
const deleteAgent = async (agent: Agent) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除 Agent "${agent.name}" 的管理记录吗？\n\n⚠️ 注意：此操作仅删除管理记录，不会卸载远程服务器上的 Agent 程序。\n如需完全卸载，请使用"卸载"按钮。`,
      '删除记录',
      {
        confirmButtonText: '确定删除',
        cancelButtonText: '取消',
        type: 'warning',
        dangerouslyUseHTMLString: false
      }
    )

    await agentApi.delete(agent.id)
    ElMessage.success('记录删除成功')
    loadAgents()
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.response?.data?.message || '删除失败')
    }
  }
}

// 根据 Agent 类型动态生成日志选项
const logPathOptions = computed(() => {
  const common = [
    { label: '主 Agent 日志', value: '/var/log/configflow-agent.log' },
    { label: 'Supervisor 主日志', value: '/var/log/supervisor/supervisord.log' },
  ]
  const mihomo = [
    { label: 'Mihomo 输出日志', value: '/var/log/supervisor/mihomo.log' },
    { label: 'Mihomo 错误日志', value: '/var/log/supervisor/mihomo.err.log' },
  ]
  const mosdns = [
    { label: 'MosDNS 错误日志', value: '/etc/mosdns/mosdns.err.log' },
  ]
  const type = currentAgent.value?.service_type
  if (type === 'mihomo') return [...common, ...mihomo]
  if (type === 'mosdns') return [...common, ...mosdns]
  return [...common, ...mihomo, ...mosdns]
})

// 判断是否为主 Agent 日志
const isMainAgentLog = computed(() => {
  return selectedLogPath.value === '/var/log/configflow-agent.log' && customLogPath.value === ''
})

// 查看日志
const viewLogs = async (agent: Agent) => {
  currentAgent.value = agent
  selectedLogPath.value = '/var/log/configflow-agent.log'
  customLogPath.value = ''
  logsDialogVisible.value = true
  await loadLoggingConfig()
  await loadLogs()
}

// 加载日志
const loadLogs = async () => {
  if (!currentAgent.value) return

  try {
    // 确定要读取的日志路径
    let logPath = ''
    if (selectedLogPath.value === 'custom') {
      if (!customLogPath.value) {
        logs.value = '请输入自定义日志路径'
        return
      }
      logPath = customLogPath.value
    } else {
      logPath = selectedLogPath.value
    }

    // 调用 API 获取日志
    const { data } = await agentApi.getLogs(currentAgent.value.id, 200, logPath)
    if (data.success) {
      logs.value = data.logs || '暂无日志'
    } else {
      logs.value = `获取日志失败: ${data.message}`
    }

    // 等待DOM更新后滚动到底部
    await nextTick()
    scrollToBottom()
  } catch (error: any) {
    logs.value = `获取日志失败: ${error.response?.data?.message || error.message || '未知错误'}`
    ElMessage.error('获取日志失败')
  }
}

// 滚动到日志底部
const scrollToBottom = () => {
  if (logsTextareaRef.value) {
    const textarea = logsTextareaRef.value.$el.querySelector('textarea')
    if (textarea) {
      textarea.scrollTop = textarea.scrollHeight
    }
  }
}

// 刷新日志
const refreshLogs = async () => {
  const loading = ElLoading.service({
    lock: true,
    text: '正在刷新日志...',
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    await loadLogs()
    ElMessage.success('日志刷新成功')
  } finally {
    loading.close()
  }
}

// 清空日志
const clearLogs = async () => {
  if (!currentAgent.value) return

  let logPath = selectedLogPath.value === 'custom' ? customLogPath.value : selectedLogPath.value
  if (!logPath) {
    ElMessage.warning('请先选择日志文件')
    return
  }

  try {
    await ElMessageBox.confirm('确定要清空该日志文件吗？此操作不可恢复。', '清空日志', {
      confirmButtonText: '确定清空',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }

  const loading = ElLoading.service({
    lock: true,
    text: '正在清空日志...',
    background: 'rgba(0, 0, 0, 0.7)'
  })

  try {
    const { data } = await agentApi.clearLog(currentAgent.value.id, logPath)
    if (data.success) {
      ElMessage.success('日志已清空')
      await loadLogs()
    } else {
      ElMessage.error(data.message || '清空日志失败')
    }
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '清空日志失败')
  } finally {
    loading.close()
  }
}

// 日志路径切换
const onLogPathChange = async (newPath: string) => {
  // 确保 selectedLogPath 已更新
  selectedLogPath.value = newPath
  if (newPath !== 'custom') {
    customLogPath.value = ''
    await loadLogs()
  }
}

// 验证并加载自定义路径
const validateAndLoadCustomPath = async () => {
  if (!currentAgent.value || !customLogPath.value) {
    ElMessage.warning('请输入日志路径')
    return
  }

  validatingPath.value = true
  try {
    const { data } = await agentApi.validateLogPath(currentAgent.value.id, customLogPath.value)

    if (data.success && data.valid) {
      ElMessage.success('路径验证成功')
      await loadLogs()
    } else {
      ElMessage.error(data.error || '路径验证失败')
      logs.value = `路径验证失败: ${data.error || '未知错误'}`
    }
  } catch (error: any) {
    ElMessage.error('路径验证失败')
    logs.value = `路径验证失败: ${error.response?.data?.message || error.message}`
  } finally {
    validatingPath.value = false
  }
}

// 加载日志配置状态
const loadLoggingConfig = async () => {
  if (!currentAgent.value) return

  try {
    const { data } = await agentApi.getLoggingConfig(currentAgent.value.id)
    if (data.success) {
      loggingEnabled.value = data.enabled !== false // 默认为 true
    }
  } catch (error: any) {
    console.error('获取日志配置失败:', error)
  }
}

// 切换日志开关
const toggleLogging = async (enabled: boolean) => {
  if (!currentAgent.value) return

  togglingLogging.value = true
  try {
    const { data } = await agentApi.setLoggingConfig(currentAgent.value.id, enabled)

    if (data.success) {
      ElMessage.success(enabled ? '日志已启用' : '日志已禁用')
    } else {
      ElMessage.error('设置失败')
      loggingEnabled.value = !enabled // 回滚状态
    }
  } catch (error: any) {
    ElMessage.error('设置日志开关失败')
    loggingEnabled.value = !enabled // 回滚状态
  } finally {
    togglingLogging.value = false
  }
}

// 启动定时刷新
const startAutoRefresh = () => {
  // 每 10 秒刷新一次
  refreshTimer = window.setInterval(() => {
    loadAgents()
  }, 10000)
}

// 停止定时刷新
const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

onMounted(() => {
  loadAgents()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.agents-page {
  padding: 28px 32px 40px;
  background: #f5f7ff;
  min-height: calc(100vh - 64px);
  --agent-radius-xl: 40px;
  --agent-radius-lg: 24px;
  --agent-radius-md: 16px;
  --agent-radius-sm: 12px;
  --agent-radius-pill: 999px;
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
  border-radius: var(--agent-radius-md, 16px);
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

.stats-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 20px;
  margin-bottom: 28px;
}

.stat-card {
  border-radius: var(--agent-radius-lg, 24px);
  border: 1px solid rgba(107, 115, 255, 0.1);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.stat-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 20px 40px rgba(65, 80, 180, 0.16);
  border-color: rgba(107, 115, 255, 0.25);
}

.stat-content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
}

.stat-info {
  flex: 1;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: #303133;
  line-height: 1;
}

.stat-label {
  font-size: 14px;
  color: #909399;
  margin-top: 4px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 400px;
  background: #fff;
  border-radius: var(--agent-radius-lg, 24px);
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  margin-top: 24px;
}

.agents-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  margin-top: 24px;
}

.agent-card {
  background: #fff;
  border-radius: var(--agent-radius-lg, 24px);
  padding: 24px;
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.agent-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 22px 48px rgba(91, 112, 255, 0.2);
  border-color: rgba(107, 115, 255, 0.25);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.card-title-group {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}

.card-title {
  font-size: 17px;
  font-weight: 700;
  color: #30354d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 6px 12px;
  border-radius: var(--agent-radius-pill, 999px);
  font-size: 12px;
  font-weight: 600;
}

.type-pill.type-mihomo {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.type-pill.type-mosdns {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.18);
}

.status-pill.status-online {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.18);
}

.status-pill.status-offline {
  background: rgba(245, 108, 108, 0.12);
  color: #f56c6c;
  border: 1px solid rgba(245, 108, 108, 0.18);
}

.deploy-pill.deploy-shell {
  background: rgba(103, 194, 58, 0.12);
  color: #67c23a;
  border: 1px solid rgba(103, 194, 58, 0.18);
}

.deploy-pill.deploy-docker {
  background: rgba(64, 158, 255, 0.12);
  color: #409eff;
  border: 1px solid rgba(64, 158, 255, 0.18);
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

.section-value {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
  font-family: 'Courier New', Consolas, monospace;
}

/* 监控指标样式 */
.metrics-section {
  padding: 16px 0;
  margin: 16px 0;
  border-top: 1px solid rgba(107, 115, 255, 0.08);
  border-bottom: 1px solid rgba(107, 115, 255, 0.08);
}

.metrics-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-weight: 600;
  font-size: 13px;
  color: #2d3748;
}

.metrics-header .el-button {
  padding: 4px 8px;
  height: auto;
  font-size: 12px;
}

.metric-item {
  margin-bottom: 12px;
}

.metric-item:last-child {
  margin-bottom: 0;
}

.metric-label {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
  font-size: 12px;
  color: #606266;
}

.metric-value {
  font-weight: 600;
  color: #2d3748;
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}

.metric-detail {
  margin-top: 4px;
  font-size: 11px;
  color: #909399;
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}

.metric-item.network {
  padding: 8px 0;
}

.network-speeds {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 4px;
}

.speed-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 12px;
  background: rgba(107, 115, 255, 0.05);
  border-radius: 8px;
  border: 1px solid rgba(107, 115, 255, 0.1);
}

.speed-label {
  font-size: 11px;
  color: #909399;
  font-weight: 500;
}

.speed-value {
  font-size: 13px;
  font-weight: 700;
  color: #2d3748;
  font-family: 'SF Mono', 'Monaco', 'Consolas', monospace;
}

.metric-item.traffic-total {
  padding: 8px 0;
  border-top: 1px dashed rgba(107, 115, 255, 0.1);
  margin-top: 8px;
}

.metric-item.traffic-total .speed-item {
  background: linear-gradient(135deg, rgba(64, 158, 255, 0.08) 0%, rgba(103, 194, 58, 0.08) 100%);
  border: 1px solid rgba(107, 115, 255, 0.15);
}

.metric-item.traffic-total .speed-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.metric-item.traffic-total .speed-value {
  font-size: 14px;
  font-weight: 800;
  background: linear-gradient(135deg, #409EFF 0%, #67C23A 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.card-actions {
  display: flex;
  gap: 8px;
  margin-top: auto;
}

.card-btn.el-button {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 36px;
  border-radius: var(--agent-radius-md, 16px);
  font-size: 13px;
  font-weight: 600;
  padding: 0 12px;
  border: none;
  transition: all 0.2s ease;
}

.card-btn.ghost {
  background: rgba(107, 115, 255, 0.12);
  color: #4e5eff;
  border: 1px solid rgba(107, 115, 255, 0.25);
}

.card-btn.ghost:hover {
  background: rgba(107, 115, 255, 0.18);
  border-color: rgba(107, 115, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.warning {
  background: rgba(255, 176, 103, 0.12);
  color: #e6a23c;
  border: 1px solid rgba(255, 176, 103, 0.25);
}

.card-btn.warning:hover {
  background: rgba(255, 176, 103, 0.18);
  border-color: rgba(255, 176, 103, 0.35);
  transform: translateY(-1px);
}

.card-btn.success {
  background: rgba(139, 143, 255, 0.12);
  color: #8b8fff;
  border: 1px solid rgba(139, 143, 255, 0.25);
}

.card-btn.success:hover {
  background: rgba(139, 143, 255, 0.18);
  border-color: rgba(139, 143, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.primary {
  background: rgba(64, 158, 255, 0.12);
  color: #409eff;
  border: 1px solid rgba(64, 158, 255, 0.25);
}

.card-btn.primary:hover {
  background: rgba(64, 158, 255, 0.18);
  border-color: rgba(64, 158, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.danger {
  background: rgba(155, 143, 255, 0.12);
  color: #9b8fff;
  border: 1px solid rgba(155, 143, 255, 0.28);
}

.card-btn.danger:hover {
  background: rgba(155, 143, 255, 0.18);
  border-color: rgba(155, 143, 255, 0.35);
  transform: translateY(-1px);
}

.card-btn.info {
  background: rgba(144, 147, 153, 0.12);
  color: #909399;
  border: 1px solid rgba(144, 147, 153, 0.25);
}

.card-btn.info:hover {
  background: rgba(144, 147, 153, 0.18);
  border-color: rgba(144, 147, 153, 0.35);
  transform: translateY(-1px);
}

/* 对话框样式 */
:deep(.el-dialog) {
  border-radius: var(--agent-radius-lg, 24px);
  box-shadow: 0 24px 64px rgba(65, 80, 180, 0.18);
}

:deep(.el-dialog__header) {
  padding: 24px 32px 20px;
  border-bottom: 1px solid rgba(107, 115, 255, 0.1);
}

:deep(.el-dialog__title) {
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  color: transparent;
}

:deep(.el-dialog__body) {
  padding: 28px 32px;
}

:deep(.el-dialog__footer) {
  padding: 16px 32px 24px;
  border-top: 1px solid rgba(107, 115, 255, 0.1);
}

:deep(.el-dialog__close) {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: rgba(107, 115, 255, 0.08);
  transition: all 0.2s ease;
}

:deep(.el-dialog__close:hover) {
  background: rgba(107, 115, 255, 0.15);
  transform: rotate(90deg);
}

/* 表单样式 */
:deep(.el-form-item) {
  margin-bottom: 24px;
}

:deep(.el-form-item__label) {
  font-size: 14px;
  font-weight: 600;
  color: #30354d;
  padding-bottom: 8px;
}

/* 输入框样式 - 只影响非select的input */
:deep(.el-form-item:not(.no-custom-style) .el-input:not(.el-select .el-input) .el-input__wrapper) {
  border-radius: var(--agent-radius-md, 16px);
  box-shadow: 0 2px 8px rgba(65, 80, 180, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.15);
  transition: all 0.2s ease;
  padding: 8px 16px;
  background-color: #fff;
}

:deep(.el-form-item:not(.no-custom-style) .el-input:not(.el-select .el-input) .el-input__wrapper:hover) {
  border-color: rgba(107, 115, 255, 0.35);
}

:deep(.el-form-item:not(.no-custom-style) .el-input:not(.el-select .el-input) .el-input__wrapper.is-focus) {
  border-color: #000dff;
  box-shadow: 0 4px 16px rgba(107, 115, 255, 0.15);
}

:deep(.el-form-item:not(.no-custom-style) .el-input:not(.el-select .el-input) .el-input__inner) {
  color: #30354d;
  font-size: 14px;
}

/* el-select 使用最小化样式 - 不包括no-custom-style */
:deep(.el-form-item:not(.no-custom-style) .el-select) {
  width: 100%;
}

:deep(.el-form-item:not(.no-custom-style) .el-select .el-input__wrapper) {
  border-radius: var(--agent-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  box-shadow: 0 2px 8px rgba(65, 80, 180, 0.08);
}

:deep(.el-form-item:not(.no-custom-style) .el-select .el-input__wrapper:hover) {
  border-color: rgba(107, 115, 255, 0.35);
}

:deep(.el-form-item:not(.no-custom-style) .el-select .el-input__wrapper.is-focus) {
  border-color: #000dff;
  box-shadow: 0 4px 16px rgba(107, 115, 255, 0.15);
}

/* 确保 el-select 内部的文本颜色正确显示 - Element Plus 2.5.x */
:deep(.el-select) {
  width: 100%;
  --el-text-color-regular: #30354d;
  --el-text-color-placeholder: #909399;
  --el-fill-color-blank: #fff;
}

:deep(.el-select__wrapper) {
  background-color: #fff !important;
  color: #30354d !important;
}

:deep(.el-select .el-input__inner) {
  color: #30354d !important;
  font-size: 14px !important;
}

:deep(.el-select input) {
  color: #30354d !important;
  font-size: 14px !important;
}

:deep(.el-select .el-select__selected) {
  color: #30354d !important;
  font-size: 14px !important;
}

:deep(.el-select .el-select__selected-item) {
  color: #30354d !important;
  font-size: 14px !important;
}

/* 强制覆盖 placeholder 类的颜色 - 更高优先级 */
:deep(.el-select__selection .el-select__selected-item.el-select__placeholder) {
  color: #30354d !important;
}

:deep(div.el-select__selected-item.el-select__placeholder) {
  color: #30354d !important;
}

:deep(.el-select__selected-item.el-select__placeholder span) {
  color: #30354d !important;
}

/* 真正的空 placeholder */
:deep(.el-select .el-select__placeholder:not(.el-select__selected-item)) {
  color: #909399 !important;
}

:deep(.el-select .el-select__input) {
  color: #30354d !important;
}

:deep(.el-select span) {
  color: #30354d !important;
}

:deep(.el-select .el-select__suffix) {
  color: #909399 !important;
}

/* Agent select popper 样式已移除（使用 teleported=false） */

:deep(.el-input-number) {
  width: 100%;
}

:deep(.el-input-number .el-input__wrapper) {
  padding: 0;
}

:deep(.el-input-number__decrease),
:deep(.el-input-number__increase) {
  width: 36px;
  border-radius: var(--agent-radius-sm, 12px);
  background: rgba(107, 115, 255, 0.08);
  border: none;
  transition: all 0.2s ease;
}

:deep(.el-input-number__decrease:hover),
:deep(.el-input-number__increase:hover) {
  background: rgba(107, 115, 255, 0.15);
  color: #000dff;
}

/* 单选按钮样式 */
:deep(.el-radio-group) {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
}

:deep(.el-radio) {
  margin-right: 0;
  padding: 10px 16px;
  border-radius: var(--agent-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  background: rgba(107, 115, 255, 0.05);
  transition: all 0.2s ease;
  white-space: nowrap;
}

:deep(.el-radio:hover) {
  border-color: rgba(107, 115, 255, 0.35);
  background: rgba(107, 115, 255, 0.08);
}

:deep(.el-radio.is-checked) {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.12) 0%, rgba(0, 13, 255, 0.12) 100%);
  border-color: #000dff;
}

:deep(.el-radio__input) {
  margin-right: 8px;
}

:deep(.el-radio__input.is-checked .el-radio__inner) {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  border-color: #000dff;
}

:deep(.el-radio__label) {
  font-size: 14px;
  font-weight: 600;
  color: #30354d;
}

/* Alert 样式 */
:deep(.el-alert) {
  border-radius: var(--agent-radius-md, 16px);
  border: none;
  padding: 16px;
}

:deep(.el-alert--warning) {
  background: rgba(255, 176, 103, 0.12);
  border-left: 3px solid #e6a23c;
}

:deep(.el-alert--success) {
  background: rgba(139, 143, 255, 0.12);
  border-left: 3px solid #8b8fff;
}

:deep(.el-alert--info) {
  background: rgba(107, 115, 255, 0.12);
  border-left: 3px solid #000dff;
}

:deep(.el-alert__title) {
  font-size: 13px;
  line-height: 1.6;
  color: #30354d;
}

/* Divider 样式 */
:deep(.el-divider) {
  margin: 28px 0;
  border-color: rgba(107, 115, 255, 0.1);
}

:deep(.el-divider__text) {
  font-weight: 600;
  color: #000dff;
  background: #fff;
  padding: 0 16px;
}

/* Collapse 样式 */
:deep(.el-collapse) {
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-radius: var(--agent-radius-md, 16px);
  overflow: hidden;
}

:deep(.el-collapse-item__header) {
  font-weight: 600;
  color: #30354d;
  background: rgba(107, 115, 255, 0.05);
  padding: 14px 20px;
  border-bottom: 1px solid rgba(107, 115, 255, 0.1);
}

:deep(.el-collapse-item__content) {
  padding: 20px;
  background: #fff;
}

/* 按钮样式增强 */
:deep(.el-button) {
  border-radius: var(--agent-radius-md, 16px);
  font-weight: 600;
  transition: all 0.2s ease;
  border: none;
}

:deep(.el-button--primary) {
  background: linear-gradient(135deg, #6b7dff 0%, #5b6dff 100%);
  box-shadow: 0 8px 20px rgba(107, 115, 255, 0.25);
}

:deep(.el-button--primary:hover) {
  transform: translateY(-1px);
  box-shadow: 0 12px 28px rgba(107, 115, 255, 0.35);
}

:deep(.el-button--success) {
  background: linear-gradient(135deg, #8b8fff 0%, #6b7dff 100%);
  color: #fff;
  box-shadow: 0 8px 20px rgba(139, 143, 255, 0.25);
}

:deep(.el-button--success:hover) {
  transform: translateY(-1px);
  box-shadow: 0 12px 28px rgba(139, 143, 255, 0.35);
}

:deep(.el-button--default) {
  background: rgba(107, 115, 255, 0.08);
  color: #000dff;
  border: 1px solid rgba(107, 115, 255, 0.15);
}

:deep(.el-button--default:hover) {
  background: rgba(107, 115, 255, 0.15);
  border-color: rgba(107, 115, 255, 0.25);
  transform: translateY(-1px);
}

:deep(.el-button.is-disabled) {
  opacity: 0.5;
}

:deep(.el-button--large) {
  padding: 12px 24px;
  font-size: 15px;
}

/* Tooltip 样式 */
:deep(.el-tooltip__trigger) {
  display: inline-flex;
  align-items: center;
}

:deep(.el-input-group__append) {
  background: rgba(107, 115, 255, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.15);
  border-left: none;
  border-radius: 0 var(--agent-radius-md, 16px) var(--agent-radius-md, 16px) 0;
  padding: 0 12px;
  transition: all 0.2s ease;
}

:deep(.el-input-group__append:hover) {
  background: rgba(107, 115, 255, 0.12);
}

:deep(.el-input-group__append .el-icon) {
  color: #000dff;
}

/* 描述文本样式 */
:deep(.el-form-item__content > div[style*="color"]) {
  font-size: 13px;
  line-height: 1.6;
  color: #7d88af !important;
}

:deep(.el-form-item__content > div[style*="margin-top"]) {
  margin-top: 10px !important;
}

/* Select 下拉样式 */
:deep(.el-select-dropdown) {
  border-radius: var(--agent-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  box-shadow: 0 12px 32px rgba(65, 80, 180, 0.12);
  background: #fff;
  padding: 4px 0;
}

:deep(.el-select-dropdown .el-select-dropdown__list) {
  padding: 4px;
}

:deep(.el-select-dropdown__item) {
  padding: 10px 16px;
  border-radius: var(--agent-radius-sm, 12px);
  margin: 2px 4px;
  transition: all 0.2s ease;
  color: #30354d;
  font-size: 14px;
}

:deep(.el-select-dropdown__item:hover) {
  background: rgba(107, 115, 255, 0.08);
  color: #000dff;
}

:deep(.el-select-dropdown__item.selected) {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.12) 0%, rgba(0, 13, 255, 0.12) 100%);
  color: #000dff;
  font-weight: 600;
}

:deep(.el-select-dropdown__item.is-disabled) {
  color: #c0c4cc;
  cursor: not-allowed;
}

/* 安装命令样式 */
.install-command-box {
  margin-top: 20px;
}

.command-section {
  margin-bottom: 20px;
}

.command-label {
  margin-bottom: 14px;
  padding-left: 4px;
  display: flex;
  align-items: center;
  font-weight: 600;
  font-size: 15px;
  color: #30354d;
}

.command-container {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.04) 0%, rgba(0, 13, 255, 0.02) 100%);
  padding: 20px;
  border-radius: var(--agent-radius-md, 16px);
  border: 1px solid rgba(107, 115, 255, 0.15);
  transition: all 0.3s ease;
  box-shadow: 0 4px 12px rgba(65, 80, 180, 0.05);
}

.command-container:hover {
  background: linear-gradient(135deg, rgba(107, 115, 255, 0.08) 0%, rgba(0, 13, 255, 0.04) 100%);
  border-color: rgba(107, 115, 255, 0.25);
  box-shadow: 0 6px 18px rgba(65, 80, 180, 0.1);
  transform: translateY(-2px);
}

.command-input :deep(.el-textarea__inner) {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 14px;
  font-weight: 500;
  background: #fff;
  color: #2c3e50;
  border: 2px solid rgba(107, 115, 255, 0.25);
  border-radius: var(--agent-radius-md, 16px);
  padding: 16px;
  line-height: 1.7;
  transition: all 0.3s ease;
  box-shadow: 0 2px 8px rgba(65, 80, 180, 0.06);
}

.command-input :deep(.el-textarea__inner):hover {
  border-color: rgba(107, 115, 255, 0.4);
}

.command-input :deep(.el-textarea__inner):focus {
  border-color: #000dff;
  box-shadow: 0 4px 16px rgba(107, 115, 255, 0.15);
  outline: none;
}

.command-input.alpine :deep(.el-textarea__inner) {
  border-color: rgba(139, 143, 255, 0.35);
  background: linear-gradient(135deg, rgba(139, 143, 255, 0.02) 0%, rgba(139, 143, 255, 0.01) 100%);
}

.command-input.alpine :deep(.el-textarea__inner):hover {
  border-color: rgba(139, 143, 255, 0.5);
}

.command-input.alpine :deep(.el-textarea__inner):focus {
  border-color: #8b8fff;
  box-shadow: 0 4px 16px rgba(139, 143, 255, 0.15);
}

.command-input.docker-run :deep(.el-textarea__inner) {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 13px;
  background: linear-gradient(135deg, rgba(64, 158, 255, 0.04) 0%, rgba(64, 158, 255, 0.02) 100%);
  color: #2c3e50;
  border: 2px solid rgba(64, 158, 255, 0.35);
  border-radius: var(--agent-radius-md, 16px);
  padding: 14px;
  line-height: 1.7;
  font-weight: 500;
  box-shadow: 0 2px 8px rgba(64, 158, 255, 0.08);
}

.command-input.docker-run :deep(.el-textarea__inner):hover {
  border-color: rgba(64, 158, 255, 0.5);
}

.command-input.docker-run :deep(.el-textarea__inner):focus {
  border-color: #409eff;
  box-shadow: 0 4px 16px rgba(64, 158, 255, 0.18);
}

.command-input.docker-compose :deep(.el-textarea__inner) {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 13px;
  background: #282c34;
  color: #abb2bf;
  border: 2px solid rgba(139, 143, 255, 0.4);
  border-radius: var(--agent-radius-md, 16px);
  padding: 14px;
  line-height: 1.7;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
}

.command-input.docker-compose :deep(.el-textarea__inner):hover {
  border-color: rgba(139, 143, 255, 0.6);
}

.command-input.docker-compose :deep(.el-textarea__inner):focus {
  border-color: #8b8fff;
  box-shadow: 0 6px 20px rgba(139, 143, 255, 0.2);
}

.script-textarea :deep(.el-textarea__inner) {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 12px;
  background: #282c34;
  color: #abb2bf;
  border: 2px solid rgba(139, 143, 255, 0.3);
  border-radius: var(--agent-radius-md, 16px);
  line-height: 1.6;
  padding: 14px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
}

.script-textarea :deep(.el-textarea__inner):focus {
  border-color: #8b8fff;
  box-shadow: 0 6px 20px rgba(139, 143, 255, 0.2);
}

/* 日志样式 */
.logs-config-section {
  margin-bottom: 12px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 8px;
}

.logs-container {
  margin-top: 8px;
}

.logs-textarea :deep(.el-textarea__inner) {
  font-family: 'Courier New', Consolas, monospace;
  font-size: 12px;
  background: #1e1e1e;
  color: #d4d4d4;
  border: 2px solid #3e3e3e;
  border-radius: var(--agent-radius-md, 16px);
  line-height: 1.6;
  padding: 14px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
  transition: all 0.3s ease;
}

.logs-textarea :deep(.el-textarea__inner):hover {
  border-color: #525252;
}

.logs-textarea :deep(.el-textarea__inner):focus {
  border-color: #000dff;
  box-shadow: 0 6px 20px rgba(107, 115, 255, 0.2);
}

.logs-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.logs-footer-left {
  flex: 1;
}

.logs-footer-right {
  display: flex;
  gap: 8px;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .agents-page {
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

  .stats-cards {
    grid-template-columns: 1fr;
    gap: 12px;
  }

  .stat-card :deep(.el-card__body) {
    padding: 14px;
  }

  .stat-icon {
    width: 40px;
    height: 40px;
    font-size: 20px;
  }

  .stat-value {
    font-size: 20px;
  }

  .stat-label {
    font-size: 13px;
  }

  .agents-grid {
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

  :deep(.script-dialog .el-dialog__body) {
    padding: 20px 16px 12px;
  }

  :deep(.script-dialog .el-form) {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  :deep(.script-dialog .el-form-item) {
    margin-bottom: 12px;
    flex-direction: column;
    align-items: flex-start;
  }

  :deep(.script-dialog .el-form-item__label) {
    width: 100% !important;
    text-align: left;
    line-height: 1.4;
    padding-bottom: 6px;
  }

  :deep(.script-dialog .el-form-item__content) {
    width: 100%;
  }

  :deep(.script-dialog .el-radio-group) {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  :deep(.script-dialog .el-radio) {
    margin-right: 0;
    width: 100%;
    display: inline-flex;
    align-items: center;
    justify-content: flex-start;
    gap: 8px;
    padding: 10px 14px;
    border-radius: 14px;
    background: rgba(107, 115, 255, 0.06);
  }

  /* 移动端单选按钮组可换行 */
  :deep(.el-radio-group) {
    flex-wrap: wrap;
    gap: 8px;
  }

  /* 对话框移动端优化 */
  :deep(.el-dialog) {
    width: 95vw !important;
    max-width: 95vw !important;
    margin: 0 !important;
    max-height: 90vh;
    border-radius: var(--agent-radius-md, 16px) !important;
  }

  :deep(.el-dialog__header) {
    padding: 18px 20px 16px;
  }

  :deep(.el-dialog__title) {
    font-size: 18px;
  }

  :deep(.el-dialog__body) {
    padding: 20px 16px;
    overflow-y: auto;
    max-height: calc(90vh - 140px);
  }

  :deep(.el-dialog__footer) {
    padding: 14px 16px 18px;
  }

  :deep(.el-form-item) {
    margin-bottom: 18px;
  }

  :deep(.el-form-item__label) {
    font-size: 13px;
    width: 90px !important;
  }

  :deep(.el-input-number) {
    width: 100%;
  }

  :deep(.el-radio) {
    padding: 8px 14px;
  }

  :deep(.el-radio__label) {
    font-size: 13px;
  }

  :deep(.el-alert) {
    padding: 12px;
  }

  :deep(.el-alert__title) {
    font-size: 12px;
  }

  :deep(.el-divider) {
    margin: 20px 0;
  }

  .command-container {
    padding: 14px;
  }

  .command-label {
    font-size: 14px;
  }

  .command-input :deep(.el-textarea__inner) {
    font-size: 12px;
    padding: 12px;
  }

  :deep(.el-button) {
    font-size: 13px;
  }

  :deep(.el-button--large) {
    padding: 10px 20px;
    font-size: 14px;
  }

  .logs-textarea :deep(.el-textarea__inner) {
    font-size: 11px;
    padding: 12px;
  }

  .script-textarea :deep(.el-textarea__inner) {
    font-size: 11px;
    padding: 12px;
  }
}

/* 超小屏幕适配 */
@media (max-width: 480px) {
  .agents-page {
    padding: 16px;
    --agent-radius-xl: 24px;
    --agent-radius-lg: 18px;
    --agent-radius-md: 12px;
  }

  .title-block h2 {
    font-size: 20px;
  }

  .title-block p {
    font-size: 13px;
  }

  .stats-cards {
    grid-template-columns: 1fr;
    gap: 10px;
  }

  .stat-card :deep(.el-card__body) {
    padding: 12px;
  }

  .stat-content {
    gap: 12px;
  }

  .stat-icon {
    width: 36px;
    height: 36px;
    font-size: 18px;
  }

  .stat-value {
    font-size: 18px;
  }

  .stat-label {
    font-size: 12px;
  }

  .agent-card {
    padding: 16px;
  }

  .card-title {
    font-size: 15px;
  }

  .card-actions {
    gap: 6px;
  }

  .card-btn.el-button {
    font-size: 12px;
    padding: 0 10px;
    height: 32px;
  }

  :deep(.el-dialog) {
    width: 100vw !important;
    max-width: 100vw !important;
    margin: 0 !important;
    border-radius: 0 !important;
    max-height: 100vh;
  }

  :deep(.el-dialog__header) {
    padding: 16px 14px 14px;
  }

  :deep(.el-dialog__title) {
    font-size: 16px;
  }

  :deep(.el-dialog__body) {
    max-height: calc(100vh - 130px);
    padding: 18px 14px;
  }

  :deep(.el-dialog__footer) {
    padding: 12px 14px 16px;
  }

  :deep(.el-form-item) {
    margin-bottom: 16px;
  }

  :deep(.el-form-item__label) {
    font-size: 12px;
    width: 80px !important;
  }

  :deep(.el-radio) {
    padding: 7px 12px;
  }

  :deep(.el-radio__label) {
    font-size: 12px;
  }

  :deep(.el-alert) {
    padding: 10px;
  }

  :deep(.el-alert__title) {
    font-size: 11px;
  }

  :deep(.el-divider) {
    margin: 16px 0;
  }

  :deep(.el-button) {
    font-size: 12px;
  }

  :deep(.el-button--large) {
    padding: 8px 16px;
    font-size: 13px;
  }

  .command-container {
    padding: 12px;
  }

  .command-label {
    font-size: 13px;
  }

  .command-input :deep(.el-textarea__inner) {
    font-size: 11px;
    padding: 10px;
  }

  .logs-textarea :deep(.el-textarea__inner) {
    font-size: 10px;
    padding: 10px;
  }

  .script-textarea :deep(.el-textarea__inner) {
    font-size: 10px;
    padding: 10px;
  }

  /* 监控详情对话框样式 */
  .metrics-dialog-content {
    min-height: 400px;
  }

  .metrics-summary-header {
    margin-bottom: 20px;
  }

  .metrics-summary-header :deep(.el-descriptions__label) {
    font-size: 12px;
    font-weight: 500;
  }

  .metrics-summary-header :deep(.el-descriptions__content) {
    font-size: 13px;
  }

  .charts-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 20px;
    margin-bottom: 20px;
  }

  .chart-card {
    background: #f5f7fa;
    border-radius: 8px;
    padding: 16px;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
  }

  .metrics-info {
    text-align: center;
    padding: 12px;
    background: #f5f7fa;
    border-radius: 6px;
  }

  .empty-state {
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 300px;
  }

  /* 响应式布局 */
  @media (max-width: 1200px) {
    .charts-grid {
      grid-template-columns: 1fr;
    }
  }
}
</style>
