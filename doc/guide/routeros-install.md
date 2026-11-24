---
outline: 2
---

# 在 RouterOS 上安装 Mihomo 和 OxiDNS

这套方案把管理面板装在 NAS 或另一台电脑上，把 Mihomo 和 OxiDNS 装进 RouterOS 容器。Mihomo 负责代理；OxiDNS 负责查询网站地址，并把需要代理的查询交给 Mihomo。

完成后，手机和电脑的**网关仍是 RouterOS，DNS 改用 OxiDNS**。先接入一台设备测试，成功后再让其他设备使用。

<a id="prepare"></a>
## 第 1 步：准备设备和地址

开始前需要：

- 已能运行容器的 RouterOS 7：安装了匹配版本的 Container 软件包、启用了容器功能，并有可写磁盘。未准备好时先按 [RouterOS 容器说明](https://help.mikrotik.com/docs/spaces/ROS/pages/84901929/Container) 完成。
- 一台运行 ConfigFlow 面板的 NAS 或电脑，以及可用的代理订阅。
- 两个独立容器需要两个空闲的内网 IP；一个 AIO 容器只需一个。都应避开 DHCP 自动分配范围，防止地址冲突。

先在 WinBox 的 **Files → Backup** 保存路由器备份并下载到电脑。在 **IP → DHCP Server → Networks** 打开当前网段，记下原来的 **DNS Servers**；原来为空也要记下来，恢复时会用到。

下面是一套完整示例。**先换成自己的地址，再开始填写。**

| 要准备什么 | 本文示例 | 后面用在哪里 |
| --- | --- | --- |
| RouterOS 地址 | `198.51.100.254` | 容器网关、客户端网关、路由器管理地址 |
| 面板地址 | `http://198.51.100.100:5010` | 服务域名、容器的 `SERVER_URL` |
| Mihomo 容器 IP | `198.51.100.28` | 代理设备地址、代理路由的下一跳 |
| OxiDNS 容器 IP | `198.51.100.251` | DNS 设备地址、客户端 DNS |
| 测试设备 IP | `198.51.100.50` | 首次选择需要分流的设备 |
| 内网网段 | `198.51.100.0/24` | 容器地址中的 `/24` 要与实际内网一致 |
| LAN 网桥 | `bridge1` | 把容器接到内网 |
| 可写磁盘目录 | `disk1/configflow` | 保存镜像、配置和设备身份 |

### 两个容器还是一个容器？

| 方式 | 推荐情况 | 选择后的区别 |
| --- | --- | --- |
| **两个独立容器** | 首次安装推荐，正文按此方式填写 | 各有一个 IP，可以分别重启、升级和排查 |
| 一个 AIO 容器 | 希望减少容器数量，接受两项服务一起启停 | 两个核心共用 IP；DNS 和 Agent 的端口必须分开，见第 4 步的 [AIO 页签](#routeros-aio) |

AIO 就是把多个程序装在同一个容器里。以上两种都只有一份 OxiDNS，不会在设备故障时自动切换到另一台；需要该能力时看 [OxiDNS 高可用](oxidns-ha.md#combinations)。

<a id="panel"></a>
## 第 2 步：让容器能找到面板

1. 按 [安装管理面板](install.md) 完成 NAS 或 Docker 部署。
2. 从另一台内网设备打开面板，例如 `http://198.51.100.100:5010`。
3. 进入「系统 → 系统设置 → 常规 → 服务配置」，将「服务域名」填成这个完整地址。
4. 需要保护配置下载链接时生成「访问令牌」，以后从页面复制完整链接，不要漏掉令牌。

**不要填 `localhost`。** 对容器来说，它表示容器自己，找不到 NAS 上的面板。面板的 `data` 目录要长期保留，后面给 Agent 准备的目录不能代替它。

<a id="image"></a>
## 第 3 步：下载并上传镜像

镜像是容器启动所需的程序包。先确认 RouterOS 设备的架构，再在**装有 Docker 的电脑或 NAS**上执行命令，不要在 RouterOS Terminal 执行：

| 设备架构 | 命令中填写 |
| --- | --- |
| x86 / x86_64 | `linux/amd64` |
| ARM64 | `linux/arm64` |

下面按 x86 举例；ARM64 要把**两条命令**中的平台都改成 `linux/arm64`。导出使用的 `--platform` 选项需要 Docker API 1.48 或以上；旧版 Docker 需先升级。使用 ConfigFlow `v3.0.0` 及以上版本时，请将下面示例中的 `latest` 替换为与当前面板相同的版本号标签（例如 `thsrite/config-flow-agent:3.0.0`），确保内置 Agent 版本与面板配套；若面板已开启登录，新装容器还需在环境变量中配置与面板一致的 `AGENT_REGISTRATION_KEY`（或使用面板生成的注册凭据）。

```sh
docker pull --platform linux/amd64 thsrite/config-flow-agent:3.0.0
docker save --platform linux/amd64 -o configflow-agent.tar thsrite/config-flow-agent:3.0.0
```

完成后，在 WinBox 的 **Files** 打开实际磁盘，本例是 `disk1`，新建 `configflow` 文件夹，把 `configflow-agent.tar` 上传进去。最终应能看到：

```text
disk1/configflow/configflow-agent.tar
```

两个独立容器和 AIO 都用这个镜像，不需要重复下载。

<a id="container-install"></a>
## 第 4 步：创建并启动容器

先选两个独立容器或一个 AIO 容器，只创建所选方案。首次安装推荐两个独立容器，便于分别重启和排查。

<DocsTabs id="routeros-container-layout" label="容器组合" :tabs="[{ id: 'routeros-two-containers', label: '两个独立容器（推荐）' }, { id: 'routeros-aio', label: '一个 AIO 容器' }]">

<template #routeros-two-containers>

两项服务各有一个 IP。再选一种操作方式：习惯在 WinBox 点选就用第一项，习惯终端命令就用第二项；不要重复执行两种方式。

<DocsTabs id="routeros-create-method" label="操作方式" :tabs="[{ id: 'routeros-winbox', label: 'WinBox 操作' }, { id: 'routeros-command', label: '命令行操作' }]">

<template #routeros-winbox>

#### 4.1 给每个容器接一张网卡

在 **Interfaces → VETH** 点击添加。VETH 是容器的虚拟网卡，可以理解为给容器接了一根网线。

| 字段 | Mihomo | OxiDNS |
| --- | --- | --- |
| Name | `cf-main` | `cf-dns` |
| Address | `198.51.100.28/24` | `198.51.100.251/24` |
| Gateway | `198.51.100.254` | `198.51.100.254` |

再到 **Bridge → Ports**，分别添加这两张网卡，**Bridge 都选实际的 LAN 网桥**，本例是 `bridge1`。填错网桥或网关时，容器可能无法联系面板或外网。

#### 4.2 准备长期保留的目录

在 **Files** 创建下表的 Src 目录，再到 **Container → Mounts** 逐项添加。“挂载”就是把路由器磁盘上的目录交给容器使用。

| List（挂载名称） | Src（路由器上的目录） | Dst（容器内目录） | 保存什么 |
| --- | --- | --- | --- |
| `cf-main-config` | `disk1/configflow/main-config` | `/etc/mihomo` | Mihomo 配置 |
| `cf-main-state` | `disk1/configflow/main-state` | `/opt/configflow-agent` | Mihomo Agent 身份和运行资料 |
| `cf-dns-config` | `disk1/configflow/dns-config` | `/etc/oxidns` | OxiDNS 配置 |
| `cf-dns-state` | `disk1/configflow/dns-state` | `/opt/configflow-agent` | OxiDNS Agent 身份和运行资料 |

**四个 Src 目录都要保留，并且不能互相混用。** 两项 Dst 都写 `/opt/configflow-agent` 没问题，因为位于不同容器。不要把另一台 Agent 的身份目录复制过来，否则可能被面板当成同一台设备。

以后重建容器仍挂载原目录。只保留镜像文件，不能保留这些配置和身份。

#### 4.3 填写启动设置

到 **Container → Envs** 添加下表内容。环境变量就是容器启动时读取的设置：List 选对应容器的列表，Key 填名称，Value 填值。

| Key | List 为 `cf-main` 时的 Value | List 为 `cf-dns` 时的 Value | 作用 |
| --- | --- | --- | --- |
| `SERVER_URL` | `http://198.51.100.100:5010` | 同左 | 面板的完整地址 |
| `SERVICE_TYPE` | `mihomo` | `oxidns` | 决定启动哪个核心 |
| `AGENT_NAME` | `mihomo-main` | `oxidns` | 面板中显示的设备名称 |
| `AGENT_IP` | `198.51.100.28` | `198.51.100.251` | 面板联系它的 IP，与对应网卡一致 |
| `AGENT_PORT` | `8080` | `8082` | Agent 管理端口，按本文填写即可 |

`8080 / 8082` 不是 DNS 端口，不填到手机的 DNS 设置中。单服务容器不需要额外添加 `ENABLE_MIHOMO` 等组合开关。

#### 4.4 添加并启动两个容器

在 **Container** 点击添加，分别填写：

| 字段 | Mihomo | OxiDNS |
| --- | --- | --- |
| File | `disk1/configflow/configflow-agent.tar` | 同左 |
| Name / Hostname | `cf-main` | `cf-dns` |
| Interface | `cf-main` | `cf-dns` |
| Root Dir（程序解压目录） | `disk1/configflow/main-root` | `disk1/configflow/dns-root` |
| Envlist / Envlists（启动设置列表） | `cf-main` | `cf-dns` |
| Mountlists（挂载列表） | `cf-main-config,cf-main-state` | `cf-dns-config,cf-dns-state` |
| Start On Boot（开机启动） | **先不勾选** | **先不勾选** |

启动命令保持镜像默认值。等镜像解压完成、状态显示 `stopped` 后，先选 Mihomo 点 **Start**，确认变成 `running`，再启动 OxiDNS。

安装测试阶段先手动启动；第 7 步验证后再勾选开机启动，RouterOS 重启后才会自动启动它们。**容器运行了，还需要第 5 步发布核心配置。**

</template>

<template #routeros-command>

先完成第 3 步的镜像上传。以下只适用于新装，尚未用 WinBox 创建过这些对象。在 RouterOS Terminal 或 SSH 会话中先检查，确认没有同名对象，也没有旧服务使用这些目录：

```text
/container print detail
/interface veth print detail
/container mounts print detail
/container envs print detail
/file print
```

修改脚本开头的网桥、网关、面板地址、两个容器 IP、网段大小和磁盘路径，再把包括最外层 `{ }` 的整段一起粘贴执行：

```text
{
    :local lanBridge "bridge1"
    :local gateway "198.51.100.254"
    :local serverUrl "http://198.51.100.100:5010"
    :local mainIP "198.51.100.28"
    :local dnsIP "198.51.100.251"
    :local prefix "/24"
    :local base "disk1/configflow"
    :local imageFile ($base . "/configflow-agent.tar")

    /interface veth add name=cf-main address=($mainIP . $prefix) gateway=$gateway
    /interface veth add name=cf-dns address=($dnsIP . $prefix) gateway=$gateway
    :foreach name in={"cf-main";"cf-dns"} do={
        /interface bridge port add bridge=$lanBridge interface=$name
    }

    :foreach dir in={"main-config";"main-state";"dns-config";"dns-state"} do={
        /file add name=($base . "/" . $dir) type=directory
    }
    /container mounts add list=cf-main-config src=($base . "/main-config") dst=/etc/mihomo
    /container mounts add list=cf-main-state src=($base . "/main-state") dst=/opt/configflow-agent
    /container mounts add list=cf-dns-config src=($base . "/dns-config") dst=/etc/oxidns
    /container mounts add list=cf-dns-state src=($base . "/dns-state") dst=/opt/configflow-agent

    /container envs add list=cf-main key=SERVER_URL value=$serverUrl
    /container envs add list=cf-main key=SERVICE_TYPE value="mihomo"
    /container envs add list=cf-main key=AGENT_NAME value="mihomo-main"
    /container envs add list=cf-main key=AGENT_IP value=$mainIP
    /container envs add list=cf-main key=AGENT_PORT value="8080"

    /container envs add list=cf-dns key=SERVER_URL value=$serverUrl
    /container envs add list=cf-dns key=SERVICE_TYPE value="oxidns"
    /container envs add list=cf-dns key=AGENT_NAME value="oxidns"
    /container envs add list=cf-dns key=AGENT_IP value=$dnsIP
    /container envs add list=cf-dns key=AGENT_PORT value="8082"

    /container add file=$imageFile name=cf-main hostname=cf-main \
        interface=cf-main envlist=cf-main \
        mountlists=cf-main-config,cf-main-state \
        root-dir=($base . "/main-root") start-on-boot=no
    /container add file=$imageFile name=cf-dns hostname=cf-dns \
        interface=cf-dns envlist=cf-dns \
        mountlists=cf-dns-config,cf-dns-state \
        root-dir=($base . "/dns-root") start-on-boot=no
}
```

中途报错时，先检查已创建的对象，不要整段重跑。设备不支持 `/file add type=directory` 时，可先在 WinBox Files 创建同名目录，再跳过脚本中的建目录循环。

等下面的检查显示镜像解压完成、容器处于 `stopped`：

```text
/container print detail where name~"^cf-"
```

逐条启动，每次确认对应容器变成 `running` 再继续：

```text
/container start [find where name="cf-main"]
/container start [find where name="cf-dns"]
/container print detail where name~"^cf-"
```

</template>

</DocsTabs>

</template>

<template #routeros-aio>

首次安装只创建一个 `cf-aio` 容器，不创建 `cf-main`、`cf-dns` 两个独立容器。它们共用一个 IP，本例使用 `198.51.100.28`，但管理端口和 DNS 端口仍分开。

使用 WinBox：先在 **Interfaces → VETH** 新建网卡，并在 **Bridge → Ports** 把它加入实际 LAN 网桥；再到 **Container** 添加容器。各项按下表填写：

| 项目 | AIO 怎么填 |
| --- | --- |
| VETH | Name 填 `cf-aio`，Address 填 `198.51.100.28/24`，Gateway 填 `198.51.100.254`；加入 LAN 网桥 `bridge1` |
| Container 的 File | 仍用 `disk1/configflow/configflow-agent.tar` |
| Name / Hostname / Interface | 都填 `cf-aio` |
| Root Dir | `disk1/configflow/aio-root` |
| Envlist / Envlists | `cf-aio` |
| Start On Boot | 先不勾选，验证后再开启 |

在 **Files** 新建以下 Src 目录，在 **Container → Mounts** 添加对应挂载。编辑 `cf-aio` 容器时，Mountlists 选择这三项：

| List | Src | Dst |
| --- | --- | --- |
| `cf-aio-mihomo` | `disk1/configflow/aio-mihomo` | `/etc/mihomo` |
| `cf-aio-oxidns` | `disk1/configflow/aio-oxidns` | `/etc/oxidns` |
| `cf-aio-state` | `disk1/configflow/aio-state` | `/opt/configflow-agent` |

在 **Container → Envs** 使用 List `cf-aio` 添加：

| Key | Value | 说明 |
| --- | --- | --- |
| `SERVER_URL` | `http://198.51.100.100:5010` | 面板地址 |
| `AGENT_IP` | `198.51.100.28` | 两个 Agent 共用容器 IP |
| `ENABLE_MIHOMO` | `true` | 启动 Mihomo；不启用就没有代理服务 |
| `ENABLE_OXIDNS` | `true` | 启动 OxiDNS；不启用就没有本方案的 DNS 服务 |
| `ENABLE_MOSDNS` | `false` | 不启动 MosDNS，避免与 OxiDNS 争用 DNS 端口 |
| `AGENT_MIHOMO_NAME` | `mihomo-main` | Mihomo 卡片名称 |
| `AGENT_MIHOMO_PORT` | `8080` | Mihomo Agent 管理端口 |
| `AGENT_OXIDNS_NAME` | `oxidns` | OxiDNS 卡片名称 |
| `AGENT_OXIDNS_PORT` | `8082` | OxiDNS Agent 管理端口 |

**不要添加 `SERVICE_TYPE`**，否则会进入单服务模式。AIO 使用上表的分服务名称和端口，不使用单服务表中的 `AGENT_NAME`、`AGENT_PORT`。

保留默认启动命令，解压完成后点击 **Start**。后面的操作仍要分别发布两个核心：

- Mihomo DNS 用 `1053`，OxiDNS DNS 用 `53`，不能都占用 `53`。
- OxiDNS 代理上游仍填 `198.51.100.28:1053`。
- 路由下一跳、客户端 DNS 都改用 `198.51.100.28`；后文的 OxiDNS 地址 `198.51.100.251` 不再适用。
- 面板应出现两张 Agent 卡片，IP 相同，端口分别是 `8080` 和 `8082`。
- 重启或停止这个容器，会同时影响代理和 DNS。三项挂载目录都要保留，不与其他容器共用。

</template>

</DocsTabs>

<a id="publish"></a>
<a id="services"></a>
## 第 5 步：上线并发布两项配置

Agent 是容器里的小助手，负责收配置和报状态。在「系统 → Agent」点击 **刷新**，应该看到 `mihomo-main`、`oxidns` 两张在线卡片。

看不到时先检查容器是否运行、面板地址是否正确、两端能否互相访问。**在线只代表管理连接正常，还要发布核心配置；此时先不修改全网 DNS。**

### 准备共用的配置空间

1. 新建并选择一个 [配置空间](../manual/profiles.md)，可以把它理解为一整套节点和分流设置。
2. 在 [订阅来源](../manual/subscriptions.md) 添加并更新订阅，或在 [节点库](../manual/nodes.md) 导入节点。
3. 在 [策略组](../manual/proxy-groups.md) 选节点，在 [策略规则](../manual/rules.md) 设置各类网站的去向，把兜底的 `MATCH` 放在末尾。
4. 将两张 Agent 卡片的 **配置空间** 都选为这个空间。绑定只是决定后续用哪套设置，不会立即发布。

### 先设置并发布 Mihomo

在 `mihomo-main` 卡片点 **设置**，打开基础配置。这里使用 YAML 文本，按原有缩进修改对应字段；不要把下表当作完整文件粘贴。

| 字段 | 本文建议值 | 设置后的作用 |
| --- | --- | --- |
| `dns.enable` | `true` | 开启 Mihomo DNS，OxiDNS 才能把代理查询交给它 |
| `dns.listen` | `0.0.0.0:1053` | 让 OxiDNS 能从内网访问这个 DNS 端口 |
| `dns.enhanced-mode` | `fake-ip` | 用临时地址接管需要代理的网站连接 |
| `dns.fake-ip-range` | `198.18.0.1/16` | 默认示例对应路由网段 `198.18.0.0/16`；不能与已有 LAN、VPN 重叠 |
| `tun.enable`、`tun.auto-route` | 均为 `true` | 开启接收转交流量的通道并准备容器内路由；关闭后不能仅靠主路由转发完成本方案 |
| `tun.auto-detect-interface` | `true` | 自动选择容器向外联网的网卡 |

已有配置用其他 Fake-IP 范围时，以实际值为准，后续路由也要一致。其他字段和编辑方法见 [Mihomo 基础配置](../manual/core-settings.md#mihomo-基础配置)。

点击 **保存并发布**，确认目标是 `mihomo-main`；如果只保存了草稿，还需在卡片点 **推送**。出现 **应用并启动** 时点击，等待发布成功和核心状态正常。

### 再设置并发布 OxiDNS

在 `oxidns` 卡片点 **设置**：

| 设置 | 本文建议 | 原因 |
| --- | --- | --- |
| UDP、TCP 监听 | 均为 `0.0.0.0:53` | 让内网设备通过普通 DNS 端口查询 |
| 直连上游 | 可用的公共 DNS，例如 `https://223.5.5.5/dns-query` | 不需要代理的网站交给它查询 |
| 代理上游 | `198.51.100.28:1053` | 填 **Mihomo IP + DNS 端口**，不是面板或 Agent 端口 |
| 测试设备策略 | `198.51.100.50` 选「智能分流」 | 先验证这一台，不要直接设置整个内网 |

设备策略位置见 [客户端设备](../manual/client-devices.md)，上游位置见 [OxiDNS 基础与上游](../manual/core-settings.md#oxidns-基础与上游)。

保存并发布到 `oxidns`。两项核心均成功后，继续接入 RouterOS。

<a id="router"></a>
<a id="router-access"></a>
## 第 6 步：接入 RouterOS，只验证一台设备

### 让面板有权管理 RouterOS

在 WinBox 中准备一个专用账号：

1. **IP → Services**：推荐启用 `www-ssl`、配置可用证书，以 HTTPS 连接；仅使用 HTTP 时，需 RouterOS 7.9 或以上，并启用 `www`。
2. **System → Users → Groups**：新建组，勾选 `read`、`write`、`api`、`rest-api`。
3. **System → Users**：新建账号、设置密码、选择该组，允许来源限制为 NAS 的实际地址。
4. 确认内网防火墙允许 NAS 访问该管理端口。

### 添加路由器并发布

在 ConfigFlow 切到刚才的空间，进入「上游路由 → 添加路由器」：

| 字段 | 本次怎么填 | 结果与注意事项 |
| --- | --- | --- |
| 类型、名称 | RouterOS；名称自定 | 用于识别并选择对应管理接口 |
| 管理地址、账号 | `https://198.51.100.254` 和专用账号；非默认端口也填上 | 访问路由器后台，不填代理或 DNS 端口 |
| 校验 HTTPS 证书 | 默认开，有可信证书时保留 | 验证失败会阻止连接；自签证书仅在确认地址正确且属于可信内网时按需关闭 |
| 代理网关 | `mihomo-main` | 把需要代理的连接送给 Mihomo 容器 |
| 自动配置回流 | 本文同网段方案开启 | 帮助回复按本方案返回设备；其他网络先看 [上游路由](../manual/routers.md) |
| 使用静态路由的 IP 规则集 | 先留空 | 仍自动包含 Fake-IP 网段 |
| 使用策略路由的 IP 规则集 | 需要时选 Telegram 等 IP 清单 | 只对设置为智能分流的来源设备生效，不需要时留空 |
| DHCP DNS 服务器 | **暂选「不关联」** | 这次先不改变全网设备获得的 DNS |

**下一跳填 Mihomo 容器 IP，本例是 `198.51.100.28`，不是 NAS 或 RouterOS 自己的地址。**

依次点击：

1. **保存 → 探测**，确认可以连接。到这里还没写入路由。
2. 点该路由器的 **策略路由**，在 **来源设备** 中确认 `198.51.100.50` 为「智能分流」。
3. 在 **发布预览** 勾选本次 OxiDNS 目标，点 **保存并预览**。
4. 核对测试设备、Mihomo 地址、所选 IP 清单，并确认 DHCP DNS 本次跳过。
5. **确认发布**，等待 OxiDNS 和 RouterOS 都成功。出现冲突、失败或结果待确认时，先查看原因，不要连续重复提交。

“策略路由”就是按来源设备和目标地址决定连接走哪条路。本文用它处理 Telegram 等可能直接连接 IP 的应用；普通域名分流不必额外选所有真实 IP。其余按钮、IPv4 范围和冲突处理见 [上游路由](../manual/routers.md)。

<a id="manual-network"></a>
不用自动下发时，见 [Fake-IP 与 Telegram 手工路由表](first-run.md#manual-routes)，下一跳换成本页的 Mihomo 容器 IP。自动发布成功后，不要再手动添加一遍。

<a id="verify"></a>
### 修改测试设备并验证

只改测试设备：**网关保持 `198.51.100.254`，DNS 改为 `198.51.100.251`**。AIO 的 DNS 改为 `198.51.100.28`。

关闭这台设备上其他代理、VPN 和浏览器安全 DNS，检查：

- 常用直连网站和需要代理的网站都能打开。
- 选了 Telegram IP 清单时，Telegram 能连接。
- [OxiDNS 查询记录](../manual/oxidns-panel.md) 能看到这台设备的 IP。
- [Mihomo 面板](../manual/mihomo-panel.md) 能看到对应代理连接。

没有 DNS 记录时，先查设备真正使用的 DNS；有记录却打不开代理网站时，查节点、核心状态和路由发布结果。**这一台还没成功，就不要全网启用。**

<a id="clients"></a>
## 第 7 步：全网启用并设置开机启动

1. 编辑 RouterOS 接入设置，将 **DHCP DNS 服务器** 选为 `oxidns`，保存。
2. 到 **策略路由 → 来源设备** 添加其他设备或网段，需要分流的选「智能分流」，不需要的选「直连」。长期按设备设置时建议固定 IP。
3. **发布预览** 勾选 OxiDNS，点击 **保存并预览**。
4. 核对实际 DHCP 网段、设备范围，以及 DNS 将改为 `198.51.100.251`（AIO 为 `198.51.100.28`），再确认发布。
5. 让客户端断开网络再连接，查看实际取得的 DNS；手动写死 DNS 的设备单独修改。

发布会**替换目标网段原来的 DNS 列表**，不会保留旧备用地址。找不到唯一匹配网段时会阻止发布，应核对页面提示，不要猜。客户端网关仍保持 RouterOS。

最后在 WinBox 的 **Container** 分别编辑两个容器，勾选 **Start On Boot**；AIO 只改它自己的容器。不勾选时当前可以运行，但 RouterOS 重启后不会自动启动。

<details>
<summary>用命令行开启两个独立容器的开机启动</summary>

在 RouterOS Terminal 先确认每个名称只对应一个运行中的容器，再执行。AIO 使用实际的 `cf-aio` 名称，不执行下面两条独立容器命令。

```text
/container print detail where name~"^cf-"
/container set [find where name="cf-main"] start-on-boot=yes
/container set [find where name="cf-dns"] start-on-boot=yes
```

</details>

<a id="dns-guard"></a>
“强制局域网 DNS 走 OxiDNS”先保持关闭。需要统一处理设备自行指定的公共 DNS 时，再看 [强制 DNS 的开关和限制](../manual/routers.md#dhcp-dns-和强制-dns)；它不会接管所有 IPv6 或加密 DNS。

<a id="changes"></a>
## 以后怎么改

| 想改什么 | 去哪里改 | 改完还要做什么 |
| --- | --- | --- |
| 节点、网站去向 | 订阅来源、策略组、策略规则 | 保存并发布到对应核心，仅保存不改变现网 |
| DNS 上游或端口 | OxiDNS / Mihomo 设置 | 保存并发布；Mihomo DNS 地址变动时，同步修改 OxiDNS 代理上游 |
| 分流设备或 IP 清单 | 客户端设备、上游路由 | 保存、预览、核对目标并确认发布 |
| 容器 IP | 已有实例建议保留原 IP；确需更换时，先安排地址迁移 | 修改 VETH 和 `AGENT_IP` 并重启容器，不会自动更新面板已登记的 Agent 地址。还需处理面板中的设备地址，以及上游、路由网关、DHCP DNS 等旧地址引用，不能当成一次普通改值 |
| 原面板更换访问地址 | 修改服务域名、各容器的 `SERVER_URL`，再停止并启动容器 | 容器启动时会更新它使用的面板地址并保留 Agent 身份。原面板须保留原 `data`；若换成全新面板，不能假定原身份仍有效。随后检查在线状态和实际推送是否成功 |

已有 Agent 的心跳不会自动改写面板里的连接地址，因此“重新在线”不能证明面板已能通过新 IP 管理它。不要为更换地址直接删除身份目录；先保留可用旧地址和数据，按完整迁移安排切换。

核心发布失败时，到 [Agent 发布记录](../manual/agents.md#history) 看最终结果；需要退回时使用「回滚到发布前」。卡片在线或请求已提交，不等于核心已经恢复。

<a id="replace-container"></a>
### 已有容器怎样换镜像、失败怎样退回

以下适用于**保留原服务类型**的升级或重建。把 MosDNS 换成 OxiDNS，还涉及核心配置与 Agent 类型迁移，不能只换镜像或改一个名称。

1. 记下旧容器的 File、Root Dir、Interface、Envlist、Mountlists 和 Start On Boot。保留旧镜像文件和原 Root Dir，不删除旧容器。
2. 安排一次短暂停服，每次只更换一个容器；AIO 会同时暂停代理和 DNS。先取消旧容器 **Start On Boot**，点 **Stop**，等待完全停止。
3. 停止后，完整备份它使用的配置目录和身份目录。配置、规则和身份要来自同一次备份，不能拿无关旧文件拼起来。
4. 上传与设备架构匹配的新镜像，创建一个新名称的容器，使用**新的 Root Dir**，继续使用原 Interface、环境变量和挂载列表。新容器 **Start On Boot 先关闭**。
5. 等新镜像解压完成再点 **Start**。确认原有 Agent 恢复在线、没有多出重复设备，再按第 6 步验证 DNS、代理和实际客户端访问。
6. 成功后开启新容器的 **Start On Boot**，旧容器保持停止。确认稳定前保留旧文件和备份。

失败时先 **Stop 新容器**，确认停止后再 **Start 旧容器**，恢复旧容器的开机启动。**不能让新旧容器同时使用同一 IP、网卡或身份目录运行。** 如果期间还发布了新配置，先按发布记录处理回滚，不能以为换回镜像就会恢复配置；必须恢复备份时，在容器停止状态下成套恢复对应数据。

旧容器启动后仍要重新检查 DNS 和代理。未恢复前不要继续更换其他容器，也不要为了清理报错而删除旧 Root Dir 或身份目录。

<a id="restore-dns"></a>
## 暂停使用或上网异常时，怎样恢复

先恢复客户端 DNS，再处理代理规则，最后决定是否停止容器。不要先停掉全家正在使用的 DNS。

1. 编辑 ConfigFlow 的 RouterOS 接入设置，把 **DHCP DNS 服务器** 改为「不关联」。开过 **强制局域网 DNS 走 OxiDNS** 时，也将其关闭，再保存。
2. 开过强制 DNS 时，到 **策略路由 → 发布预览**，取消全部 OxiDNS 目标，点击 **保存并预览**。确认本次只发布 RouterOS，并包含移除强制 DNS 的改动，再 **确认发布**。没有相关规则和变更时不用重复发布；OxiDNS 已离线时也不要继续勾选它作为发布目标。
3. 在 WinBox 的 **IP → DHCP Server → Networks** 打开自己的网段，将 **DNS Servers** 改回第 1 步记录的原值；原来为空就恢复为空。**Gateway 保持 RouterOS 地址。**
4. 手动改过 DNS 的客户端也恢复原值，断开重连，检查实际 DNS 和普通上网。
5. 要停用代理分流时，将来源设备改为「直连」，取消此前选择的代理 IP 规则集，保存、预览并确认发布。检查主路由上仍指向 Mihomo 的手工路由，手工添加的需自行停用。Fake-IP 路由可能仍保留；先保留 Mihomo 运行，确认客户端不再使用旧 Fake-IP 或通过它连接后，再停止容器。

**「不关联」不会自动恢复旧 DNS。** 删除 Agent 或路由器接入记录，也不会替你撤销已经写入的路由。恢复后先确认客户端能上网，继续保留配置和身份目录，方便重新启用。
