# L6 实验网 runbook：24 小时浸泡 + 切换演练

- 准备：L6 prep agent，2026-11-23，分支 `go-l6-prep`（基于 `go@595972901`）。本文件只描述环境与步骤；**浸泡和切换演练都还没有开始**。
- 依据：[o14-switch-plan.md](../o14-switch-plan.md) 第 A.3、A.5、C 节，WORKFLOW「VM lab tests」，[L1](L1.md)–[L5](L5.md) 的搭建经验，HANDOFF 里留给 L6 的实机项（主备失联自动补完、s6 Agent 镜像、Docker 主备）。
- 文中只出现实验网地址（10.97.0.0/24）、文档地址（192.0.2.0/24）和实验专用账号；生产地址、域名、token 一律不写。生产快照副本只在实验 guest 和测试服务器的私有目录里出现。
- 脚本都在 [`l6/`](l6/)：测试服务器上跑的在 `l6/`，PVE 上跑的在 `l6/pve/`（复制到 PVE 的 `/root/cf-gwip-lab/l6/`）。

## 0. 安全规则（每一步都适用）

1. 只在 PVE 实验宿主上操作，从测试服务器 `ssh pve` 进入；只动 owner 文件 `/root/cf-gwip-lab/l6-owner.json` 里登记的 guest（250–259）。`lab.conf` 的 `PROTECTED` 列出了不许碰的 ID（生产 guest 与其他工作的 CT 130、CT 240），所有脚本经 `require_registered` 检查。
2. 隔离：`inet cfgt` 丢弃实验网发往 192.168.0.0/16、10.0.0.0/8、172.16.0.0/12 的流量（宿主原有规则，脚本从不修改）。`labnet.sh up` 另加：实验网发往外部的明文 DNS（:53）一律 DNAT 到实验解析器，DoT（:853）丢弃——否则家里路由器会把明文 DNS 劫持到生产 DNS。
3. 生产快照副本在**任何面板启动之前**先跑 `sanitize-snapshot.py apply`，并且 `scan` 必须为 0（见第 4 节）。面板 CT 的 `panel.sh data` 会拒绝没有 `.l6-sanitize.json` 的数据包。
4. 报告、截图里只写数量和结果；快照里的真实地址、名称、域名、token 不进入任何提交、报告或 Agent 提示。`scan --values` 只在操作者终端看。
5. 测试服务器到实验网不通（`inet cfgt` 只放行实验网发起的连接）；所有操作经 `ssh pve` → `pct exec`/`qm`。
6. PVE 的 root shell 是 zsh：不要在 `ssh pve "…"` 里写 `pkill -f <模式>`（会匹配并杀掉 ssh 会话本身，本次准备时踩过一次）；长任务用 `nohup … &` 并轮询日志。

## 1. Guest 清单（已建好，2026-11-23）

| ID | 名称 | 系统 | 地址 | 内存 | 角色 |
|---|---|---|---|---|---|
| CT 250 | cf-l6-panel | Alpine 3.24 + Docker 29.9.3 + Compose 5.7.2 | 10.97.0.150 | 2048 MB | 面板容器 `cf-panel`（host 网络，:80）。先跑最后一个 Python 镜像，再换 Go 镜像；数据在 `/srv/cf/data`（700） |
| CT 251 | cf-l6-infra | Alpine 3.24 | 10.97.0.151 | 768 MB | 实验解析器 labdns（mosdns 5.3.3，上游 DoH-by-IP，查询日志 `/var/log/labdns-queries.log`）；镜像仓库 `:5000`（registry:3，HTTP）；静态 HTTP `:8090`（GitHub release 镜像 `gh/`、合成订阅 `sub/lab.yaml`、探测目标 `ok.txt`）；WebDAV `:8091`（rclone，账号 `l6dav`） |
| CT 252 | cf-l6-gw-deb | Debian 12，systemd | 10.97.0.152 | 512 MB | 旁路网关 Mihomo Agent（试点），`/dev/net/tun` |
| CT 253 | cf-l6-gw-alp | Alpine 3.24，OpenRC | 10.97.0.153 | 512 MB | 旁路网关 Mihomo Agent，`/dev/net/tun` |
| CT 254 | cf-l6-dns-a | Debian 12，systemd | 10.97.0.154 | 384 MB | DNS HA 成员 A：OxiDNS + keepalived |
| CT 255 | cf-l6-dns-b | Alpine 3.24，OpenRC | 10.97.0.155 | 384 MB | DNS HA 成员 B：OxiDNS + keepalived |
| VM 256 | cf-l6-chr | RouterOS CHR 7.25.1 | 10.97.0.156 | 512 MB | 主路由角色：REST over TLS（自签证书 `l6-srv`），用户 `cf`/`L6Cf-2611`，admin/`L6Chr-2611`，`www` 关闭 |
| CT 257 | cf-l6-client | Debian 12 | 10.97.0.157 | 256 MB | 客户端探测：`dig` 循环查两个 VIP，`curl` 经试点网关代理 |
| VM 258 | cf-l6-dock-a | Debian 12 云镜像 + Docker CE 29.10.0 + Compose 5.8.0 | 10.97.0.158 | 1024 MB | Docker 主备成员 A（s6 Agent 镜像） |
| VM 259 | cf-l6-dock-b | 同上 | 10.97.0.159 | 1024 MB | Docker 主备成员 B |

- VIP：原生 DNS HA 对 `10.97.0.249`（CT 254/255），Docker HA 对 `10.97.0.248`（VM 258/259）。两个 Docker 成员用 VM 而不是 LXC：同一 PVE 上的 LXC 共用内核 boot ID，会被「相同宿主」检查拒绝。
- 所有 guest `onboot=0`，搜索域 `lab.invalid`（PVE 默认会把宿主的真实搜索域写进 guest 的 hosts，已覆盖）。CT 的实验 SSH 钥匙在 PVE `/root/cf-gwip-lab/l6/id_l6`；VM 用 cloud-init 注入同一把钥匙（`root@`）。
- 软件源：`mirrors.aliyun.com`（TUNA、USTC 当天返回 403）；Alpine 用 HTTP（见 L6P-2）。镜像站地址在 PVE 上解析后写进 guest 的 `/etc/hosts`，所以实验解析器起来前也能装包。安装命令都有超时与重试（apk 在连接卡死时不会自己超时）。
- GitHub release 文件：Agent/安装脚本的核心下载经 `l6-ghshim`（`/usr/local/bin/curl`、`wget` 的包装）改到 `http://10.97.0.151:8090/gh/…`，安装脚本仍校验自己写死的 SHA256。当前镜像：Mihomo v1.19.32（amd64-compatible）、OxiDNS v1.6.1（x86_64 musl）、mosdns v5.3.3。安装器要了没镜像的文件时（404），在测试服务器上 `fetch-assets.sh <URL>` 后重新 `push-to-lab.sh` 和 `setup-infra.sh base`。
- PVE 资源：建完 10 台后 PVE 可用内存约 4.3 GB；Docker VM 设了 balloon 512 MB。只做原生部分时可先 `qm shutdown 258 259`。
- 已做的冒烟（空数据，做完即删）：CT 250 里 Go 镜像 2 s 健康、`/api/version` = `2.0.0-rc.l6+fe126f1ca`，Python 镜像 3 s 健康、v2.2.0；`soak.sh` 采到 Go 面板空载 RSS ≈ 37 MB、8 线程、18 fd；252/253 经 shim 从实验镜像下载 Mihomo/OxiDNS 成功（SHA256 与 release 一致）；`labnet.sh check` 全 PASS；`setup-guests.sh check` 每台都能到 infra、DNS 经实验解析器。
- 已知采集限制：LXC 里 `docker stats` 的内存列是 0B（嵌套 cgroup 读不到），以 `/proc/<pid>` 的 RSS 为准。

## 2. 镜像（当前构建，供环境验证；浸泡前要用最终代码重建）

在 `.64` 上用 `build-images.sh` 构建（`L6_COMMIT=fe126f1ca472`，即本分支修好 arm64 构建后的提交），`docker save | gzip -1` 后推到 PVE 并校验 SHA256，再推进实验仓库：

| 用途 | 实验仓库引用 | 镜像 ID / 来源摘要 | tar.gz SHA256 |
|---|---|---|---|
| Go 面板（runtime，`VERSION=2.0.0-rc.l6+fe126f1ca`，`AGENT_IMAGE` 指向下一行） | `10.97.0.151:5000/config-flow:2.0.0-rc.l6-fe126f1ca` | `sha256:aab665c1bde0dc44902b294824455657d76c8252c5b6bb4526a8ebb0730d7bbc` | `e20908a6b418ef8a96f8222ee50b1775e2a77a4a47afdc4ee50208a5d092e8a0` |
| s6 Agent 镜像（Agent 1.9.3-go） | `10.97.0.151:5000/config-flow-agent:1.9.3-go-fe126f1ca` | `sha256:b1d5a7c8c4093148561330e1ef4e96936805ebd227549ff4ef2b9f47e3c92877` | `81d4370004808772deea5d9ea4d6c97dae02814b5496741df7e13ffc1521a72d` |
| 最后一个 Python 面板（当日 `thsrite/config-flow:latest`，`/api/version` = v2.2.0） | `10.97.0.151:5000/config-flow:python-final` | 索引摘要 `sha256:2c96d675a841d16ef1bed08442ac3453a0ae5f874763d76161402311acae6ea3` | `04c772b704ae97dd0ba78bdf0c5d39c1b975472875dba476b684c65704cb72e8` |
| Python 时代 Docker Agent（当日 `thsrite/config-flow-agent:latest`，Supervisor + Python） | `10.97.0.151:5000/config-flow-agent:python-final` | 索引摘要 `sha256:384ef418a72912f9bfb0115b50667783d120fec8d1da00cfeb37f8541b07ef60` | `a5ebbb1fc22b163dc4c8204a88a323400f3531680faf30adc5bd8966e90e9302` |
| 仓库本身 | `cf-l6/registry:3`（只在 CT 251 本地） | 索引摘要 `sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c` | `61fb2d383f700973c62584181a48d415bdc11a24b946062af94d9b40f89900b9` |

- 构建时发现并修复了一个发布阻断：`go@595972901` 的面板镜像**无法构建**——`backend/agents/go-agent/svc/exec.go` 用了 `syscall.Dup2`，linux/arm64 没有这个系统调用，而面板镜像要交叉编译 arm64/armv7 的 Agent（Agent 镜像的 arm64 变体同样失败）。改为 `golang.org/x/sys/unix.Dup2`（提交 `fe126f1ca`）。门禁的 Agent 阶段只在 amd64 上 vet/test，没有覆盖交叉编译，见第 9 节。
- 面板生成的 Docker Agent 命令/Compose 使用构建时注入的 `AGENT_IMAGE`，这里指向实验仓库，所以 Go 面板生成的 Docker 安装会从实验网内拉取配套的 s6 镜像（与发布时「面板与 Agent 镜像配对」的机制相同，只是仓库不同）。
- Python 面板生成的 Docker 命令用 `thsrite/config-flow-agent:latest`：Docker VM 上已把 `python-final` 重新打成这个标签，`docker compose up` 不会去 Docker Hub。
- 所有 Docker 客户端（CT 250、CT 251、VM 258/259）的 `daemon.json` 都信任 `10.97.0.151:5000`（HTTP）。

**用最终代码重建（浸泡必须跑发布的代码）**，在任务 worktree 里：

```sh
# 测试服务器（.64 或 .32），不在本机
RT_HOST=sunbroke@192.168.0.64 rt -e L6_COMMIT=$(git rev-parse --short=12 HEAD) -- bash server/parity/lab/l6/build-images.sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'bash server/parity/lab/l6/fetch-assets.sh && bash server/parity/lab/l6/push-to-lab.sh <commit12>'
ssh sunbroke@192.168.0.64 'ssh pve "cd /root/cf-gwip-lab/l6 && bash setup-infra.sh images <commit12> && bash setup-guests.sh panel"'
```

`L6_PY_PANEL`/`L6_PY_AGENT` 可换成维护者指定的「最后一个 Python 版本」标签（D2：Go 合入 main 前最后一次 Python 发布）。

## 3. 网络

- 网桥 `cfgt` 10.97.0.0/24，网关 10.97.0.1（PVE），经 vmbr0 MASQUERADE 出网。实验网没有 IPv6 互联网。
- `labnet.sh up`（已执行，运行时规则，PVE 重启后要重跑）：`inet cfgt` prerouting 把 `daddr != 10.97.0.0/24` 的 :53 DNAT 到 10.97.0.151；forward 放行这类重定向及其回包、丢弃 cfgt→vmbr0 的 :53 和 :853；postrouting 对重定向流量做 masquerade（保证解析器经宿主回包）。规则都带注释 `l6-labdns`，`labnet.sh down` 只删这些。
  - 这个重定向作用于整个 cfgt（包括其他工作的 CT 130）：按协调者 2026-11-22 的裁定，实验 DoH 解析器 + DNAT 是 cfgt 的常设基础设施。
- `labnet.sh check`：确认私网丢弃规则在、重定向在、DoT 丢弃在；从一台运行中的 L6 CT 向 223.5.5.5 发明文查询，必须出现在实验解析器的查询日志里；访问家庭网段必须失败。
- `labnet.sh egress-lock <id>…` / `egress-open`：按需切断某些 guest 的外网（注释 `l6-egress-<id>`）。默认不锁：快照脱敏后，订阅、手动节点、EasyTier 都已换成合成或 TEST-NET 地址（第 4 节），面板仍需要外网刷新公开规则源。若协调者希望浸泡期间网关完全不出网，`egress-lock 252 253`（Mihomo 的 DIRECT 解析与健康检查会失败，HTTP 探测改为直连目标仍可用）。
- RouterOS CHR 只有一个口（ether1 在 cfgt）。快照里路由器的规则同步、强制 DNS、IPv6 验收会针对这台 CHR 执行；IPv6 验收在实验网里会给出「未就绪」类结果——Python 基线和 Go 切换后在同一环境下比较，期望两者一致即可（不是要它通过）。

## 4. 生产快照副本：取得与脱敏（C.0）

**取快照由协调者做**（只读，按 WORKFLOW「Production snapshot refresh」：数据目录 tar（不含日志）+ 容器内 SQLite 在线备份，700 权限，放在测试服务器 `~/cf-prod-snapshot/<日期>/data`）。本准备**没有**访问生产 NAS。

脱敏在测试服务器上、对副本做（不在快照目录本身，脚本会拒绝）：

```sh
S=~/cf-prod-snapshot/<日期>/data            # 协调者取来的快照（不改）
W=~/.cache/cf-l6/snap && rm -rf $W && mkdir -p $W && chmod 700 $W && cp -a $S $W/data
python3 server/parity/lab/l6/sanitize-snapshot.py template --auto $W/data $W/map.json   # 私有映射，600
#   检查 map.json：agents 每台映射到哪台实验 guest（自动规则见下）、routers、replace；必要时手改
python3 server/parity/lab/l6/sanitize-snapshot.py apply --dry-run $W/data $W/map.json
python3 server/parity/lab/l6/sanitize-snapshot.py apply $W/data $W/map.json        # integrity_check 必须 ok
python3 server/parity/lab/l6/sanitize-snapshot.py scan $W/data $W/map.json         # 必须 0、退出码 0
# 两个面板都要能接受这份副本（无网络）：
sudo docker run --rm --network none -v $W/data:/data <Go 面板镜像> /usr/local/bin/configflow -check
tar -C $W -cf $W/data.tar data && scp $W/data.tar pve:/root/cf-gwip-lab/l6/data.tar
```

`apply` 做的事（每项都会在 `<副本>/.l6-sanitize.json` 里记数量，不记值）：

| 对象 | 处理 |
|---|---|
| Agent 记录 | 映射到实验 guest 的改 host/port 并保持启用；其余禁用并把地址改到 TEST-NET-1（192.0.2.x，不可路由）；HA 发布记录里的成员地址同步改 |
| 路由器 | 第一台 RouterOS → 实验 CHR 的 endpoint/账号（`verify_tls=false`）；其余 endpoint 改到 TEST-NET-1、关闭自动规则同步 |
| OxiDNS 的 `ros_address_list`/`ros_route` 插件（当前与模板） | 地址与账号改为实验 CHR |
| WebDAV | 关闭自动备份，清空地址/账号/密码（或在 map 里填实验 WebDAV：`http://10.97.0.151:8091`，`l6dav`/`L6-dav-2611`，用来测轮转） |
| 订阅（面板与内置 Sub-Store） | URL 改为 `http://10.97.0.151:8090/sub/lab.yaml`（合成，TEST-NET 节点），保留启用与刷新间隔，因此定时刷新照常运行但只访问实验 HTTP；删除订阅缓存、聚合 provider、Sub-Store 缓存、同步产物与分享 token |
| 手动节点 | 改为合成 ss 节点（TEST-NET 地址，名称保留） |
| EasyTier 出站 | 关闭，网络名/密钥/私钥替换（否则实验网关会用真实密钥加入真实网络） |
| system_config | `server_domain` → `http://10.97.0.150`；外部 `sub_store_url`、`rule_fetch_proxy` 清空 |
| 全库字符串与 JSON/YAML 状态文件 | `replace` 映射：旧 Agent 地址、VIP、面板地址 → 实验地址；订阅/Sub-Store/WebDAV 主机名 → `*.invalid` 占位名（它们也出现在规则同步回执、预览、HA 发布缓存里）。IPv4 只按整段匹配 |
| 旧版 JSON（config.json、system.json、profiles/*/config.json 及 .bak） | 同样的规则（它们只在数据库损坏重建时读取，而且 SHA256 不再匹配，不会被用上；保留在原位是为了切换演练与生产目录形态一致） |
| SQLite | 改动的行 revision = 新 generation，写新 commit_id（与 `internal/store.Commit` 相同），`integrity_check` 必须 ok |

`--auto` 的映射规则：HA 成员优先；原生 Mihomo → 252、253；原生 OxiDNS → 254、255；Docker OxiDNS → 258、259；更多的同类设备禁用；HA 的 VIP → 10.97.0.249（原生）/ 10.97.0.248（Docker）；第一台 RouterOS → CHR。`scan` 只在「template 时记录的生产目标」（Agent/路由器/VIP/ROS 插件/WebDAV/订阅/Sub-Store/面板地址，`_deny`）仍出现时失败；私网地址只计数（规则列表、客户端网段里本来就有）。

已在测试服务器现有的旧快照副本上验证：`apply` 后 integrity ok，`scan` 0 个残留；Go 镜像 `-check` 通过；Python v2.2.0 镜像在 `--network none` 下启动健康（只有「无法连接上游」的规则刷新警告）。临时副本已删除。

**脱敏之后、面板启动之前**：`panel.sh data /root/cf-gwip-lab/l6/data.tar`（CT 250，700 权限）。

## 5. 步骤

所有 PVE 侧命令都在 `ssh pve` 后的 `/root/cf-gwip-lab/l6/` 里执行。

### 5.1 开工检查（每次开工、PVE 重启后）

```sh
bash mkguests.sh status            # 10 台都在
bash labnet.sh up && bash labnet.sh check     # 全 PASS
bash setup-guests.sh check         # 每台能到 infra、DNS 走实验解析器
pct exec 251 -- sh -c 'wget -qO- http://10.97.0.151:5000/v2/_catalog'
nft list chain inet cfgt forward | grep -c 'ip daddr 192.168.0.0/16 drop'   # 1
```

### 5.2 Python 起点（C.2）

1. `panel.sh data …`，`panel.sh python` → `/api/version` = Python 版本。登录用实验账号（`panel.env`：admin / `L6-panel-2611`，与生产一样开启登录）。
2. **在任何 Agent 安装之前**，在面板上确认：Agent 页没有一台能连上的生产设备（全部是 TEST-NET 或实验地址）、路由器页只有 CHR、设置页 WebDAV 已关、订阅地址是实验地址。截图只拍数量。
3. 安装 Agent（Python 面板生成的命令，CT 里用 `pct exec <id> -- env SHELL=/bin/sh PATH=/usr/local/bin:…`，安装器经 shim 从实验镜像下载核心）：
   - 252/253：Mihomo（252 是试点）；254/255：OxiDNS，勾选「准备 DNS 高可用」；258/259：Docker HA 安装引导（compose 或 docker run，见 5.7）。
   - 让实验 guest 接管快照里对应的 Agent 记录：`agent-identity.sh <ct> <快照 Agent ID>`（从面板数据里读 token，改 guest 上 `/opt/configflow-agent/config-<类型>.json` 的 `agent_id/token/server_url`，重启 Agent），然后在 Agent 页删除安装时新注册的那条重复记录。也可以先 `agent-identity.sh --seed <ct> <ID> <类型>` 再运行安装命令，看安装器是否沿用已有身份（L1 S2 显示同一链接重跑会沿用；对「预置身份」没有实测过，第一次做时记录结果）。
4. 每台全量推送一次，使设备运行 **Python 生成的配置**。
5. 建立 C.2-3 的其余状态：DNS HA 配对（254+255，VIP 10.97.0.249）、DNS 跟随绑定（Mihomo → OxiDNS）、CHR 规则同步基准、IPv6 验收记录、一个 OxiDNS ready 未激活候选、一条已忽略通知、一个未提交的固定 IP 预览、一个进行中的设置向导、（可选）WebDAV 指向实验 WebDAV。
6. 记录 A.1-4 基线：`soak.sh start py-baseline <Mihomo 代理端口>` 跑至少 1 小时；导出配置（设置 → 导出）存到 PVE `/root/cf-gwip-lab/l6/runs/`；CHR 规则导出 `curl -sk -u cf:L6Cf-2611 https://10.97.0.156/rest/ip/firewall/address-list | wc -c` 等。

### 5.3 切换演练（C.3，按 A.3 第 1–8 步）

```sh
bash soak.sh stop py-baseline
bash panel.sh go 2.0.0-rc.l6-<sha>     # 打印 停止耗时 / 健康耗时 / 停机时长（目标 ≤ 2 min）与首启 WARN/ERROR 行数
bash soak.sh start go-switch <Mihomo 代理端口>
```

- A.2 影子启动：先在测试服务器上对同一份脱敏副本跑 Go 镜像（`--network none`），首启日志与实验面板首启对比。
- D5 裁定是**切换后保持自动同步开启**：首启后立即观察第一轮自动同步（路由器、Agent 规则同步）——`agents.tsv`、路由器页 paused 数、CHR 上的条目数。异常按 A.5 先停发布。
- 全量重推顺序（D11）：试点 252 → 253 → Docker 对 → 原生 DNS HA 对（按配对流程，最后做）。每台记录是否出现一次 `changed`、推送耗时、核心是重启还是热重载、content sync 到 `synced` 的耗时。
- 每一步记开始/结束时间（`date -Is`），填进 C.4 表。

### 5.4 停损演练（C.5 抽样）

| 演练 | 命令/做法 | 通过标准 |
|---|---|---|
| F5 | 给 252 推一个核心加载不了的配置（例如把 mixed-port 改成已被占用的端口） | Agent 自动回滚，252 继续服务（`probe-http.log` 只在重启瞬间失败），面板显示失败，253 不受影响 |
| F6 | 原生 HA 对发布中、主机让位后：`pct exec 250 -- docker kill -s KILL cf-panel`，再 `docker start cf-panel`（或 `panel.sh kill`） | 至少一台持有 VIP，`probe-dns-native.log` 中断在 L3 测得范围内；重启后按同一任务继续或安全失败 |
| F9 | 推送完成后 `panel.sh stop`；`panel.sh restore pre-switch`（切换前 `panel.sh backup pre-switch` 的冷备份）；`panel.sh go <tag>` | 启动正常；面板提示需要重推；重推后收敛 |
| F12 | 第二轮演练：恢复切换前备份后再切一次，不做任何手动操作，记录首启到手动推送之间的自动写入（D5 已定保持开启，这里只记录数字） | 记录实际写入次数与内容 |
| 重启 | `panel.sh restart` | 健康、排空正常（ExitCode 0）、状态不丢、不产生新的 `changed` |

切换前务必 `panel.sh stop && panel.sh backup pre-switch && panel.sh python`（冷备份需要停机几秒）。

### 5.5 主备失联自动补完（maintainer 2026-11-22 批准的行为，L6 实机核对）

前提：原生 HA 对（254/255）已配对且完整发布过一次；规则自动同步开关**关**（验证补完不依赖它）。

1. 改一项 OxiDNS 配置（例如 `cache_size` +1，记为 X），向 HA 对发起完整主备发布。
2. 在「已更新的备机持有 VIP、等待主机发布」这一阶段，让主机失联：`pct exec 254 -- nft add table inet l6lost; pct exec 254 -- nft add chain inet l6lost in '{ type filter hook input priority -10; }'; pct exec 254 -- nft add rule inet l6lost in ip saddr 10.97.0.150 drop`（只断面板→主机，VRRP 不受影响；也可以 `pct stop 254` 做断电变体）。
3. 等 10 分钟以上：任务应为 `failed`，消息「主备设备连接中断超过 10 分钟，本次主备发布已停止；设备恢复后会自动完成本次主备发布。设备无法恢复时可以强制停用高可用」，HA 页显示「等待设备恢复后自动完成发布」。这期间再改一项配置（Y，不发布），用来证明补完发的是**中断时的内容**。
4. 恢复：`pct exec 254 -- nft delete table inet l6lost`（或 `pct start 254`）。两次健康心跳后应出现「设备已恢复，正在自动完成本次主备发布」，发布记录带「自动补完」标签；结束为 `succeeded`。
5. 核对：两台 `config.yaml` 的 sha256 相同且只含 X，不含 Y；补完只尝试一次（再断一次不会再自动补）；`probe-dns-native.log` 全程失败数；VIP 最终在主机。
6. 变体（可选）：断的是备机；或在等待期间手动发布一次（应取代补完，补完不再发生）。

### 5.6 Docker 主备（s6 Agent 镜像，L6 实机核对）

VM 258/259 已装 Docker CE 29.10.0 + Compose 5.8.0，信任实验仓库，已预拉 `config-flow-agent:1.9.3-go-<sha>` 和 `python-final`（另打标签 `thsrite/config-flow-agent:latest`）。

1. **Python 起点**：在 Python 面板的「Docker 主备安装引导」生成两台的命令（host 或 macvlan；macvlan 的父接口是 `eth0`，网段 10.97.0.0/24，给容器留 10.97.0.200–239），在 258/259 上执行（`ssh -i /root/cf-gwip-lab/l6/id_l6 root@10.97.0.158`）。镜像是 Python 时代的 Supervisor 镜像。配对，VIP 10.97.0.248，完整发布一次。
2. **切换后**，按「Docker Agent 更新步骤」把两台换成 Go 面板配套的 s6 镜像（`10.97.0.151:5000/config-flow-agent:1.9.3-go-<sha>`），沿用原数据卷重建容器。核对：
   - 日志里从旧 journal（`service_manager` 为 `supervisor`）恢复，HA 不需要重新开启；`docker exec … ps` 里 PID 1 是 s6（`s6-svscan`），没有 python3/supervisord 进程；`supervisorctl`/`python3` 只是 shell 兼容层。
   - `docker kill -s KILL` 掉容器内的 OxiDNS 进程（`docker exec <c> sh -c 'kill -9 $(pidof oxidns)'`）→ 由 s6 拉起；连续快速退出后进入 FATAL 并停止重启，修复后可恢复。
   - `docker stop`：先释放 keepalived（VIP 迁到另一台，`probe-dns-docker.log` 只丢 1–2 个查询），容器退出码 0，无僵尸进程；`docker start` 后按 journal 恢复。
   - host 模式额外核对 HA companion：控制 socket、keepalived 由 Agent 在 companion 里启停。
   - 新装：在 Go 面板生成一次 Docker 安装命令，镜像引用必须是实验仓库的 s6 标签（`AGENT_IMAGE` 注入），注册成功（P13 的实验网版本）。
3. 主备失联补完的 Docker 变体（可选）：把 5.5 的第 2 步换成断 258。

### 5.7 24 小时浸泡（O10 + C.5「24 h」）

```sh
bash soak.sh start go-24h <Mihomo 代理端口>   # 切换演练结束后立即开始
# 24 小时后
bash soak.sh stop go-24h && bash soak.sh summary go-24h
```

浸泡期间不做人工操作，让定时任务（订阅刷新、规则刷新、规则同步、指标落盘、清理）都至少跑一轮；可每 6 小时 `soak.sh summary` 看趋势。

## 6. 采集的指标

| 类别 | 指标 | 来源 | 期望 |
|---|---|---|---|
| 面板 | 停机时长 | `panel.sh go` 输出 | ≤ 2 min |
| 面板 | 首启耗时、启动日志 WARN/ERROR 数量与类别 | `panel.sh go` 输出、`soak/<run>/errors.log` | 与影子启动一致；ERROR 均可解释 |
| 面板 | RSS、线程数、fd 数、CPU（每 60 s） | `soak/<run>/panel.tsv`（`/proc/<pid>`、`docker stats`） | 24 h 内平稳无单调增长；低于 Python 基线（生产 Python 146 MB RSS） |
| 面板 | Sub-Store（node）RSS | `panel.tsv` 最后一列 | 按需启动、空闲后退出 |
| 面板 | goroutine | 发布镜像没有 pprof，本采集不含；需要时用 o10perf 的 pprof overlay 另跑一轮 | — |
| 日志 | 每 10 min 的 ERROR/WARN/总行数 | `logs.tsv` | 无持续增长的错误类别 |
| 发布 | 每台 Agent 的 status、最近发布状态、config_version、Agent 版本（每 10 min） | `agents.tsv`（面板 API） | 全部 online；最近发布 succeeded；切换后每台只出现一次 config_version 变化 |
| 设备 | Agent、Mihomo、OxiDNS、keepalived 的 RSS（每 5 min）；Docker 容器内存 | `devices.tsv` | 平稳 |
| DNS HA | VIP 查询失败数与最长中断（10 次/s） | `probe-dns-native.log`、`probe-dns-docker.log`（失败带毫秒时间戳 + 每分钟汇总） | 在 L3 范围内；规则热更新 0 丢包 |
| 代理 | 经试点网关代理的 HTTP 失败（1 次/s） | `probe-http.log` | 只在核心重启瞬间失败 |
| DNS HA | 两台最终配置摘要 | `pct exec 254/255 -- sha256sum <OxiDNS 配置>` | 相同 |
| 路由器 | paused 数、预览差异条数、发布后规则条数 | 面板 + CHR REST 导出 | 差异只在资源标签/哈希 |
| 数据 | `integrity_check`、切换前后导出结构差异 | `sqlite3`（CT 250 里 `apk add sqlite`）、导出 JSON | ok；只有随机 ID/时间差异 |
| 备份 | WebDAV 首次自动备份、轮转 | CT 251 `/srv/webdav` | 按 A.1-6 |
| 一次性事项 | 通知重现数、固定 IP 预览 409、设置向导提示、OxiDNS 候选激活被拒 | 面板 | 按 O12 描述 |
| M16 | 各 Mihomo 内核上报的 coreVersion 原始字符串 | Agent 页 / `agents.tsv` | 记录；`alpha-*` 时核对 EasyTier 生成被拒的提示 |

所有原始数据留在 PVE `/root/cf-gwip-lab/l6/soak/<run>/`；报告 `server/parity/lab/L6.md` 只写统计值。

## 7. 清理

```sh
bash soak.sh stop <run>
bash panel.sh stop
pct exec 250 -- rm -rf /srv/cf            # 快照副本与冷备份
rm -f /root/cf-gwip-lab/l6/data.tar
for id in 250 251 252 253 254 255 257; do pct stop $id; pct destroy $id --purge; done
for id in 256 258 259; do qm stop $id; qm destroy $id --purge; done
bash labnet.sh down                        # 或保留（常设基础设施，见第 3 节）；保留时解析器 CT 251 也要保留
# owner 文件把每台标为 destroyed（或「留用」并写明给谁）
```

测试服务器上：删除 `~/.cache/cf-l6/snap`（脱敏副本与私有 map）；`~/.cache/cf-l6/images/<commit>` 可保留到发布。PVE `/root/cf-gwip-lab/l6/xfer/` 里的镜像 tar 可删。

**注意**：如果销毁 CT 251 而保留 `labnet.sh up` 的重定向，整个 cfgt 的明文 DNS 都会失败（包括 CT 130）。两者要一起留或一起撤。

## 8. 需要协调者/维护者确认的事项

1. **取快照**：由协调者从生产 NAS 只读取一份新的快照（本准备没有访问生产），然后按第 4 节脱敏；建议取在最终代码构建之后、演练前 24 小时内。
2. **最终代码与镜像**：浸泡必须跑发布的代码；按第 2 节用最终提交重建两份镜像，并确认「最后一个 Python 版本」用哪个标签（目前是当日的 `latest` = v2.2.0）。
3. **`fe126f1ca` 的修复**（`unix.Dup2`）是 Agent 产品代码改动，需要进批次门禁；建议门禁的 Agent 阶段加上 `GOARCH=arm64/arm` 的 `go build`/`go vet`（这次是镜像构建才暴露）。
4. **Agent 接管快照记录**的方式：本准备给了 `agent-identity.sh`（改 guest 上的 Agent 配置后删除重复记录）。「预置身份后再运行安装命令」能否避免重复注册没有实测，第一次做时确认。
5. **出网策略**：默认不锁网关出网（脱敏后已无真实订阅/节点/EasyTier）；如需浸泡期间网关完全不出网，用 `labnet.sh egress-lock 252 253`。
6. **快照里设备数多于实验 guest 时**：`--auto` 会把多出来的禁用（TEST-NET 地址）。若快照有 iKuai，本准备没有建 iKuai VM（C.1 标为可选；L5 的方法可复用 `/var/lib/vz/template/iso/ikuai-4.0.314.iso`）。
7. **CT 251 与 DNS 重定向**是否在 L6 之后作为 cfgt 常设基础设施保留（协调者 11-22 的裁定倾向保留）。

## 9. 本次准备中的发现

| ID | 严重程度 | 摘要 | 处理 |
|---|---|---|---|
| L6P-1 | **高（发布阻断）** | `go@595972901` 面板镜像构建失败：`svc/exec.go` 的 `syscall.Dup2` 在 linux/arm64 不存在；Agent 镜像的 arm64 变体同样失败。门禁 Agent 阶段只在 amd64 上 vet/test，没覆盖 | 已修（`fe126f1ca`，`unix.Dup2`）；linux amd64/arm64/arm/386 build + vet、`go test ./svc` 通过。建议门禁加交叉编译 |
| L6P-2 | 观察 | Alpine guest 里 busybox `wget`（`ssl_client`）和 apk 自带的下载器对镜像站的 HTTPS 大文件会中途卡死，且 apk 不会自己超时；同一 CT 里 curl 走 HTTPS 正常（~6 MB/s），Debian CT 里 curl ~20 MB/s；`inet cfgt` 没有丢包（new/invalid/untracked 计数为 0）。这也可能是以往「实验网下载 GitHub 很慢」的原因之一 | Alpine 软件源改用 HTTP（apk 包有签名校验）；安装命令加超时与重试；大文件优先在测试服务器下载后推入 |
| L6P-3 | 观察 | PVE 会把宿主的真实搜索域写进 CT 的 `/etc/hosts` | 所有 L6 guest 设 `--searchdomain lab.invalid` |
| L6P-4 | 观察 | TUNA、USTC 镜像站当天对实验出口返回 403 | 改用 aliyun |
