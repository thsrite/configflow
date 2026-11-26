# #40 发布镜像复核（fresh Opus，非作者）

- 范围：`go-opus-review-image-40` 上 `6bfbf509a`（相对 `go`）：Dockerfile、supervisord.conf、configflow-start.sh、
  configflow-healthcheck.sh、auto_update.py、docker.yml、文档，以及 `cmd/configflow -check`、`repo.CheckStartup`、
  `weblistener.CheckConfig`、各 `stat_linux.go` 时间戳换算。
- 实测：全部在 `.64` 上用 `docker buildx` 构建 runtime（amd64、arm64）和 autoupdate（amd64）镜像，只用合成数据目录与
  本地 git 源；未连生产、实验网或外部更新源。
- 结论：无 high。2 条 medium 已在本分支修复并复测；1 条 medium 需要维护者/协调者决定（未改）；其余 low。

## 发现

| # | 级别 | 内容 | 处理 |
|---|---|---|---|
| M1 | medium | `docker/metadata-action` 默认 `flavor: latest=auto`：推**正式** semver 标签（如 `v2.0.0`）时会自动加 `latest`，绕过 `PUBLISH_LATEST` 开关，违反 D3（观察期内不改 `latest`）。预发布标签不受影响。 | 已修：`flavor: latest=false`，`latest` 只由 raw 规则决定。只做了 YAML 解析检查，CI 未实跑（依据 metadata-action 文档的 latest 规则）。 |
| M2 | medium | autoupdate 从 Python 镜像换到 Go 镜像时，`/data/.auto-update/manifest.json` 是 Python 时期写的（没有 `server` 键）。远端 sha 未变（"already built"）、离线、缺工具链三条路径都会直接 `install(manifest)`：Go server 用镜像自带的，前端/Agent 用旧 manifest 里另一提交的构建，违反 M11。实测 S4：版本显示镜像版本，但安装的是 manifest 记录提交的组件。 | 已修：`load_manifest` 遇到有 `sha` 但没有 `server` 键的旧 manifest 时丢弃并打日志，按新镜像重新判定。复测：打印 `ignoring build … recorded by the Python image`，随后正常更新并写入新 manifest，再次重启为 `already built`。 |
| M3 | medium（需决定） | **旧 Python autoupdate 容器**跑的是镜像里固化的旧 `auto_update.py`，新脚本的 D6 保护对它们不起作用。Go 合入 `main` 后只要 `backend/` 的 Python 源码还在，旧脚本会照常安装 `main` 的 `backend/` 并重建前端 → 用户继续跑"冻结的 Python 后端 + Go 时期前端"，之后前端为 Go 新增的接口会逐步失配；而 CI（docker.yml 只构建 `target: runtime`）从不发布 `autoupdate` 标签，这些用户拿不到 Go 版 autoupdate 镜像，除非维护者手动推送。 | 维护者已采纳（O14 B.3.1）：CI 增加 autoupdate 构建与发布（`<版本>-autoupdate`；浮动 `autoupdate` 与 `latest` 同一开关），actionlint 通过。原建议：Go 合入 `main` 的同一提交删除 `backend/` Python 源码（旧脚本 `install_requirements`/`replace_dir` 会失败并回退到最后一次 Python 构建，不会跑半新半旧组合），同时手动发布 Go 版 `autoupdate` 标签（或在 CI 增加该 target），并写入升级说明。 |
| L1 | low | 被拒的旧数据目录（`-check` 或正常启动）里会新建 `app.log`（logx 默认 `DATA_DIR/app.log`），而提示语和文档写"数据目录未改动／不修改目录内容"。配置文件本身确实未动；Python 同样会写日志文件。 | 已改（维护者裁定）：提示语改为「配置数据未改动」，install.md 与 O14 B.1/B.4 写明只新建 `app.log`。 |
| L2 | low | `ADD --chmod=644` 同时把它创建的上级目录 `/opt`、`/opt/sub-store` 设成 `0644`（旧镜像同样如此）。容器以 root 运行所以无影响；非 root 运行会失败。 | 已修：sub-store 阶段 `chmod 755` 两个目录；复测为 `drwxr-xr-x`。 |
| L3 | low | `stat_linux.go` 改为 `Timespec.Nano()`：在 amd64/arm64 上与原 `Sec*1e9+Nsec` 完全等价（都是 int64）；它修好了 32 位（arm/386）编译。但 `upgrades/artifacts_test.go` 仍有同样写法，`GOARCH=arm go vet` 失败。 | 已修测试；`GOARCH=arm go vet ./internal/agents/upgrades` 通过。面板镜像不含 armv7（D4），只影响 autoupdate 在 armv7 主机上的编译。 |
| L4 | low | Python 镜像的 supervisord 给 Flask 强制 `DATA_DIR=/data`，容器里另设的 `DATA_DIR` 实际被后端忽略；Go 镜像按容器环境的 `DATA_DIR` 读写。若有用户把 `DATA_DIR` 设成别的路径但卷仍挂在 `/data`，首启会看到空配置（原数据不受影响）。 | 不改行为；已写入 install.md 卷说明与 O14 B.4（维护者裁定）。 |
| L5 | low | 关停：supervisord 先停 configflow（stopwaitsecs 9）再停 Sub-Store（5）。理论最坏 >10 s，但 Go 在宽限 8 s 内结束、Sub-Store 毫秒级退出。实测见下。 | 无需改。 |
| L6 | low | autoupdate 构建期间 Go 尚未启动，HEALTHCHECK（start-period 15 s + 3×30 s）可能显示 unhealthy；只影响状态显示，Docker 不会因此重启。 | 未改。 |
| L7 | low | 构建失败不做负缓存，每次启动都重试同一失败提交；不可达的 git 源会让启动多等 30 s（`ls-remote` 超时）。与 Python 时期一致。 | 未改。 |
| L8 | low | autoupdate 的 server 构建按源码树指纹缓存，复用时版本号保留首次构建它的提交（实测运行 `b1d69d3` 时显示 `main-a0d68f9`，两者 server 源码相同）。仍是唯一版本，能定位源码。 | 未改。 |
| L9 | low | 自定义 git 源：更新等于执行该源的代码（`npm ci` 生命周期脚本、Go 构建），下载只靠 HTTPS，无签名校验；`CONFIGFLOW_UPDATE_REPO` 是容器所有者自己设置的变量，不跨越权限边界。本地路径源若不属 root，会被 git `safe.directory` 拒绝（文档未提）。构建产物先写 `.tmp` 再 `os.replace`，二进制用临时文件 + rename 原子替换；目录在服务启动前删除后复制，失败回到镜像副本。缓存都在 `/data/.auto-update`（实测约 1.5 GB，主要是 go-mod/go-cache）。 | 未改；建议文档写明自定义源须可信、缓存占用。 |
| L10 | low | 运行镜像仍带 Python 3.14（约 45 MB），因为 Alpine 的 `supervisor` 依赖它，`auto_update.py` 也用它；B.3"去掉 Python 依赖"只能在换掉 supervisord 后实现。 | 未改。 |

## 逐项核对（无问题的项）

- **O13 第 5 类**：`ADMIN_*`/`JWT_SECRET_KEY`、`DATA_DIR`、`LOG_*`、`CONFIGFLOW_BACKEND_*`、`AGENTS_STATIC_DIR`（默认 `/opt/configflow/static/agents`，镜像里有 amd64/arm64/armv7 三个 Agent）、
  `SUB_STORE_*`（`SUB_STORE_BUILTIN_DIR=/opt/sub-store`、`supervisorctl -c /etc/supervisor/conf.d/supervisord.conf … sub-store` 与 Go 常量一致）、
  node-probe 路径 `/opt/configflow/node-probe/`、端口 80、卷 `/data`、`CONFIGFLOW_FRONTEND_DIR`、`.gz` 预压缩（实测返回 `Content-Encoding: gzip` 与 immutable 缓存头）。
- **Alpine/musl**：Go server、Agent、node-probe 都是 `CGO_ENABLED=0` 静态二进制；Node 24.22.3（Alpine 包），`node:sqlite` 在 amd64 与 arm64（QEMU）可加载；
  `tzdata` + `TZ=Asia/Shanghai`（`date` 显示 CST，日志时间为本地时间）；`/etc/ssl/certs/ca-certificates.crt` 存在。Go 只会执行 `node`、`supervisorctl`、node-probe，
  不依赖 bash/curl（镜像里有 busybox `sh`、`wget`、`ip`、`ping`、`nslookup`）。UID 仍为 root。
- **镜像大小**：runtime 355 MB（压缩 97.8 MB），autoupdate 748 MB（压缩 179 MB）。
- **M11**：`/api/version` = `v9.0.0-rc.1+6bfbf509ab48`；Agent 与 server 来自同一构建上下文。**D4**：docker.yml 只 `linux/amd64,linux/arm64`，Agent 仍含 armv7。**D8**：`TZ=Asia/Shanghai`。**D3**：见 M1。
- **D6**：新 autoupdate 遇旧目录跳过更新并恢复镜像版本，随后 `-check` 拒绝并退出 1（`restart: unless-stopped` 下会循环重启并重复打印提示，可接受：与 Python 时期 web-listener prepare 失败的行为相同）。
- **-check**：不取锁、不建库；全新空目录和不存在的子目录都通过且不写任何文件；只在报错时写 `app.log`（L1）。正常启动路径只复用了提示常量，行为不变。测试 `cmd/configflow/check_test.go` 8 例覆盖空目录、已有库、两类旧文件、Web/后端端口、宽限时间。

## 实测（.64，`docker buildx`）

```
T1 -check 空目录：rc=0，目录文件数 0
T2 全新首启：health=healthy after 6s；GET / 200；/api/version {"version":"v9.0.0-rc.1+6bfbf509ab48"}；sub-store STOPPED（按需）
T3 Sub-Store mode=always → RUNNING；mode=on_demand 正常返回
T4 docker stop：空闲 465 ms；Sub-Store 运行 + 一个未写完请求体的挂起请求 7205 ms，exit 0，非 SIGKILL
T5 旧目录（config.json + profiles/p1/config.json）：中文提示原文、rc=1；只新增 app.log（L1）
T6 CONFIGFLOW_WEB_PORT=8088：healthy，8088 返回 200
T7 设置页切到 8090：{"port":8090,"source":"saved",...}；8090=200，80 关闭；HEALTHCHECK 读 web-listener.json 仍 healthy；重启后 8090=200
T8 CONFIGFLOW_WEB_PORT=abc / =3001：中文错误，rc=1
arm64（QEMU）：uname aarch64，node v24.22.3，node:sqlite ok，-check ok，不写文件
autoupdate（本地 git 源）：
  S1 源码与镜像相同 → 不重建，12 s 就绪
  S2 server 改动 → 只重建 server，29 s 就绪，版本 main-a0d68f9
  S3 server 编译失败 → "update failed, keeping a0d68f9"，仍运行 main-a0d68f9
  S4 Python 时期 manifest（修复前）→ "already built"，混用镜像 server（M2）；修复后 → 丢弃旧 manifest、重新判定、保存
  S5 旧目录 → 跳过提示 + -check 拒绝，rc=1
  S6 源不可达 → 30 s 超时后用上一构建启动
修复后重建：/opt、/opt/sub-store 为 drwxr-xr-x
go test -race -count=1 ./cmd/configflow ./internal/repo ./internal/weblistener ./internal/jsonstore \
  ./internal/agents/trafficstore ./internal/agents/upgrades ./internal/api/logs ./internal/store  → 全部 ok
GOOS=linux GOARCH={amd64,arm64,arm,386} go vet（改动的 stat 包与 cmd/configflow）：修复后全部通过
```
