# O10 正式测量机（PVE CT 240）与 Go vs Python 基线（2026-11-21）

之后 O10 的验收都以这台 CT 为准。本文只有汇总数字；快照内容、真实主机名/IP/token/域名不出现在这里。

## 结论（先看这里）

1. **batch-21（go c2113cce0）在正式测量机上空闲已接近 Python，但两项仍未达标；读路由一半仍慢于 Python。**
   两轮高度可重复（各项差 ≤ 10%）：
   - 空闲 CPU 平均 **1.91–1.97%**（Python 1.45–1.57%，×1.3）；10 s 窗口中位 **1.1%**（Python 0.6–0.8%）；
     p10 **0.9–1.0%**（Python 0.4–0.5%，×2）。尖峰 41%，与 Python 的 40–43% 相同，> 10% 的窗口 5 个（Python 4 个）。
     剩下的是稳态底线：空闲分配稳态中位 3.0 MB/s，30 min 约 510 次 GC。
   - RSS：空闲结束 **91–93 MB**（Python 117–118），最大 125–142 MB（Python 118），负载后 132–140 MB（Python 141–144）。
   - **快于或等于 Python**：通知（39–47 ms vs 60–83）、Mihomo（236–274 ms vs 1.0–1.4 s）、provider（313–334 ms vs 1.0–1.5 s）、
     surge/loon/mosdns/oxidns、心跳、Agent rule-sync。
   - **仍慢于 Python**：`summary=1` ×7–9、`/api/agents/<id>` ×5–7、`/api/profiles` ×5、路由器 policy ×4–5、
     policy-settings ×3–4、`/api/routers` ×2.5、`history=0` ×2.5、`/api/agents` ×2–6、路由器 rule-sync ×2、
     upstream-routers ×1–2。分配仍是百 MB 级：Agent 列表 220–230 MB、详情 139 MB、provider 206 MB、Mihomo 171 MB。
     这些正是 `o10-rootcause.md` 里 S1–S3 要解决的部分。
2. **go tip 57af72520（batch-21 之前）远未达标**：空闲平均 42%、窗口中位 11–12%、通知 4.1 s / 2.2–2.4 GB 一次。
   它不含 O10 修复，对应 `o10-performance.md` 的「修复前」列，量级与那里一致。保留在下文作为“修复前”基线。
3. **测试服务器的数字可信：分配跨机器一致，延迟/CPU 同比放大。** batch-21 与 .64 run3（T1+T2）在 T3 未触及的路由上
   分配几乎逐字一致（Agent 列表 221 vs 219–221 MB、详情 138 vs 138–139、Mihomo 170 vs 171、provider 206 vs 206、
   surge 12 vs 12.2、`summary=1` 33 vs 33）。延迟在 PVE 上对 Go 和 Python 都慢约 1.3–2.5 倍（N305 低频 E 核、CT 2 核），
   所以 Go/Python 倍数可以在测试服务器上看，**绝对值和“≤ Python”的验收必须在这台机器上同机测**。
4. **验收的绝对目标要按本机重定（需要裁定）。** Python 自己在这里空闲平均 1.45–1.57%、窗口中位 0.6–0.8%、p10 0.4–0.5%，
   在 .64 上定的「平均 ≤ 1%、底线 ≤ 0.5%」Python 也过不了。建议改成“同机同快照不高于 Python”（平均、窗口中位、p10），
   延迟“≤ Python”同样按同机数字判。

## 机器与 CT

| 项 | 值 |
|---|---|
| 宿主 | PVE 192.168.0.6，Intel Core i3-N305（8 核 8 线程，E 核，最高 3.8 GHz），16 GB，PVE 9.2.25，内核 7.0.14-24-pve |
| 宿主同时运行 | 生产来宾（主路由、DNS、mihomo 等）和实验网 CT 220–229；6 轮测量期间（10:36–14:21 UTC）每分钟采样 1 分钟 load：221 个采样，最小 0.37、中位 1.49、p90 2.88、最大 4.02（一次） |
| CT | **VMID 240，`cf-o10-perf`，10.97.0.140/24 @ cfgt**（不出网，见下） |
| CT 模板 | debian-13-standard_13.1-2 |
| CT 限额 | unprivileged，`features: nesting=1`，cores 2，memory 2048 MB，swap 512 MB，cpuunits 50（生产优先），onboot 0，rootfs local-lvm 8 GB |
| Python | 3.11.17（uv 管理，装在 `/opt/o10`），包与测试服务器 `~/cf-test/.venv` 完全相同（`uv pip freeze` → install），waitress 3.0.2 |
| Go | 在测试服务器构建：go1.27.4，`CGO_ENABLED=0 -trimpath -ldflags='-s -w'`（与 docker/Dockerfile 相同）+ o10perf 的 pprof overlay |
| 登记 | `/root/cf-gwip-lab/o10-owner.json`（用途、创建时间、创建者 O10、驱动脚本） |

## 隔离（出口阻断）与验证

CT 本身没有网络：

- `net0` 带 `link_down=1`：宿主侧 veth `DOWN`，CT 里 `eth0` 是 `NO-CARRIER`，路由都是 `linkdown`。
- 宿主新增独立的 nft 表 `inet o10ct`（不改已有规则），只针对 10.97.0.140：forward 方向源/目的都 drop，input 方向源 drop，
  优先级 `filter - 10`。这张表是运行时规则（宿主重启后消失），`pve.sh` 每次运行前都会检查并补上。
- 依赖安装不需要放行公网：Python 环境在测试服务器上构建好，用 `pct push` 推进去；Go 二进制同样 `pct push`。
- o10perf 自己还在只有 lo 的 user/net/mount/pid namespace 里跑（`run.sh`），快照副本以只读 bind 挂载。
  CT 为 unprivileged + nesting=1 时 `unshare --user --map-root-user --net --mount --pid --mount-proc` 可用，不需要 keyctl。
- 每次运行开头 `perf.py` 做出口证明（接口只有 lo、DNS 解析失败、到 192.0.2.1 / 2001:db8::1 的连接不可达），通过才开始。

已验证（`pve.sh check`）：宿主 veth `DOWN`；o10ct 3 条 drop 规则在；CT 内连 10.97.0.1:22 失败（“no egress to gateway”）；
namespace 里只剩 1 个接口。6 轮正式测量（外加 3 次冒烟）的出口证明全部通过（run.sh 在证明失败时会直接退出）。

快照：每次运行 `pct push` 一份私有副本到 CT 的 `/root/cf-prod-snapshot/data`（700），运行结束后连同输出一起删除；
宿主中转目录 `/root/cf-o10` 也在每步后清空。私有输出（profile、日志、逐请求数据）只取回到测试服务器
`~/cf-prod-snapshot/runs/pve-<时间戳>`。

## 怎么重跑（一条命令）

在要测的那棵 worktree 里（它的 `server/tools/o10perf/pve.sh` 存在即可）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash server/tools/o10perf/pve.sh run --servers go       # 测本树的 Go
RT_HOST=sunbroke@192.168.0.32 rt -- bash server/tools/o10perf/pve.sh run --servers python   # 测本树的 Python
```

- 其余参数原样交给 `perf.py`（默认空闲 30 min + 全部路由负载，各 10 次）；`--src DIR` 可指定测试服务器上的另一份源码树
  （例如 `git archive <commit> backend server | ssh <server> tar x -C DIR`）。
- 脚本会：按 Dockerfile 构建 + overlay → 推送 → 等宿主 load ≤ 4 → 后台运行（ssh 断开不影响）→ 取回 → 删除 CT 内快照与输出 → 关机。
  只打印 `report.json`。一轮 Go 约 40 min，Python 约 35 min。**Go 和 Python 分开依次跑**，不要并发（CT 只有 2 核）。
- 一次性准备：`pve.sh create`（建 CT）、`pve.sh python`（在测试服务器 `/opt/o10` 构建 Python 环境并推进 CT；
  换测试服务器时先在那台跑一次）、`pve.sh check`（隔离检查）。
- 汇总数字：perf.py 的 `report.json` 给平均与尖峰；窗口中位 / p10 / p90 和“5 分钟后”的稳态数从 `idle-series.json` 算
  （10 s 一个窗口）。

## 方法

- 被测版本：(1) go tip 57af72520 的 Go 服务器；(2) 同一 commit 的 Python backend（`python -m backend.startup`，
  与生产一样经 waitress 启动）；(3) batch-21 合入后的 go c2113cce0（只测 Go）。同一生产快照（20261116），各自私有副本。
- 顺序：Go1 → Py1 → Go2 → Py2 →（batch-21）Go1 → Go2，每轮单独一个进程，CT 内没有其它负载。 batch-21 在测量途中合入 go，所以在原计划 4 轮之后追加。
- o10perf 默认：启动后空闲 30 min（10 s 采样 `/proc` 与 Go `MemStats`），然后每个路由 1 次预热 + 10 次计时，最后静置 120 s。
  每路由的数字是该标签所有实例里的最大值（perf.py 的汇总口径；`/api/agents/<id>` 等有 3 个实例）。
- 所有后台循环都开启；断网下规则源/订阅刷新会失败，每 300 s 重试一次，是空闲尖峰的主要来源（见 `o10-performance.md`）。

## 结果：go tip 57af72520（batch-21 之前，两轮）

### 空闲 30 min

| 指标 | Go 轮 1 | Go 轮 2 | Python 轮 1 | Python 轮 2 |
|---|---:|---:|---:|---:|
| 启动到可服务 | 2.13 s | 2.17 s | 1.44 s | 1.44 s |
| 启动后 RSS | 84 MB | 85 MB | 75 MB | 69 MB |
| 空闲 CPU 平均（单核 %） | **42.1%** | **41.6%** | 1.45% | 1.57% |
| 5 分钟后平均 | 33.7% | 33.3% | 1.00% | 1.12% |
| 10 s 窗口中位（底线） | **11.0%** | **12.0%** | 0.6% | 0.8% |
| 窗口 p10 | 8.8% | 8.4% | 0.4% | 0.5% |
| 窗口 p90 | 190% | 148% | 0.9% | 1.1% |
| 尖峰（10 s 窗口，CT 上限 200%） | 201% | 171% | 43% | 40% |
| > 10% 的窗口 / 总窗口 | 107 / 179 | 106 / 179 | 4 / 179 | 4 / 179 |
| 空闲分配（平均 / 5 分钟后窗口中位） | 162 / 37.9 MB/s | 162 / 37.8 MB/s | — | — |
| GC 次数（30 min） | 5397 | 4222 | — | — |
| GC 后存活堆 | 17.4 MB | 24.7 MB | — | — |
| 空闲 RSS 结束 / 最大 | 166 / 361 MB | 204 / 435 MB | 118 / 118 MB | 117 / 118 MB |
| 负载后 RSS 即刻 / 120 s 后 | 290 / 262 MB | 318 / 280 MB | 144 / 145 MB | 141 / 143 MB |
| 负载后 GC 存活堆 | 35.8 MB | 43.0 MB | — | — |
| goroutine（始/末/最大） | 15 / 15 / 22 | 15 / 15 / 27 | 线程 22 | 线程 22 |
| fd 结束 | 13 | 13 | 12 | 12 |

### 每路由（中位延迟，10 次；两轮用 “/” 分开）

| 路由 | Go 延迟 ms | Go CPU ms/次 | Go 分配 MB/次 | Py 延迟 ms | Py CPU ms/次 | 响应 KB Go / Py |
|---|---:|---:|---:|---:|---:|---:|
| GET /api/notifications | 4123 / 4147 | 6573 / 6051 | **2416 / 2222** | 83 / 60 | 71 / 61 | 23 / 27 |
| GET /api/agents | 1392 / 1409 | 2052 / 1996 | 740 / 710 | 220 / 61 | 182 / 59 | 329 / 469 |
| GET /api/agents?history=0 | 1482 / 1464 | 2136 / 2094 | 760 / 762 | 187 / 152 | 154 / 221 | 22 / 27 |
| GET /api/agents?summary=1 | 143 / 131 | 246 / 183 | 73 / 57 | 9.7 / 7.4 | 8 / 8 | 22 / 27 |
| GET /api/agents/<id>（最大者） | 1377 / 1380 | 2413 / 2821 | 873 / 1059 | 53 / 38 | 50 / 39 | 238 / 349 |
| GET /api/agents/<id>/rule-sync | 397 / 404 | 844 / 819 | 305 / 301 | 21 / 15 | 18 / 15 | <1 |
| POST …/heartbeat | 36 / 18 | 77 / 36 | 27 / 14 | 6.7 / 5.5 | 5 / 5 | <1 |
| GET /api/profiles | 88 / 63 | 175 / 89 | 61 / 30 | 3.1 / 4.3 | 2 / 4 | <1 |
| GET /api/config/<p>/surge | 274 / 364 | 394 / 416 | 128 / 142 | 29 / 20 | 23 / 20 | 13 |
| GET /api/config/<p>/loon | 273 / 347 | 415 / 401 | 138 / 136 | 29 / 19 | 24 / 23 | 13 |
| GET /api/config/<p>/mosdns | 266 / 354 | 417 / 390 | 139 / 138 | 40 / 32 | 33 / 32 | 13 |
| GET /api/config/<p>/oxidns | 363 / 446 | 525 / 472 | 173 / 160 | 78 / 64 | 70 / 67 | 18 / 17 |
| GET /api/config/<p>/mihomo | 686 / 941 | 1039 / 1025 | 364 / 370 | 1373 / 1005 | 1107 / 1012 | 22 / 21 |
| GET …/aggregations/<agg>/provider | 1559 / 2296 | 2441 / 2549 | 847 / 875 | 1460 / 1049 | 1183 / 1050 | 79 / 75 |
| GET /api/routers | 331 / 353 | 507 / 574 | 178 / 203 | 12 / 10 | 11 / 10 | <1 |
| GET /api/routers/<id>/policy | 819 / 877 | 1268 / 1230 | 450 / 440 | 28 / 22 | 28 / 25 | <1 |
| GET /api/routers/<id>/policy-settings | 1112 / 1218 | 1754 / 1754 | 621 / 630 | 42 / 28 | 35 / 30 | 6 / 7 |
| GET /api/routers/<id>/rule-sync | 340 / 350 | 596 / 575 | 207 / 207 | 16 / 10 | 12 / 9 | <1 |
| GET /api/agents/<id>/upstream-routers | 1471 / 1459 | 2373 / 2215 | 810 / 797 | 125 / 56 | 101 / 78 | <1 |

所有请求返回 200。Python 的 `/api/agents` 与详情响应比 Go 大（469 vs 329 KB）；原因未追查，可能是 30 min 空闲后两端记录里的历史条数不同。Go 两轮之间分配差 ≤ 10%
（`/api/profiles`、心跳、agents/<id> 例外，在 20–50% 之间，都是单实例小值或实例取最大带来的波动）。

## 结果：batch-21（go c2113cce0，只测 Go，两轮）

Python 对照用上一节的两轮（backend 在两个 commit 之间未变；同一快照）。

### 空闲 30 min

| 指标 | Go 轮 1 | Go 轮 2 | Python（两轮） |
|---|---:|---:|---:|
| 启动到可服务 | 0.42 s | 0.42 s | 1.44 s |
| 启动后 RSS | 82 MB | 101 MB | 69–75 MB |
| 空闲 CPU 平均 | **1.97%** | **1.91%** | 1.45–1.57% |
| 5 分钟后平均 | 1.41% | 1.37% | 1.00–1.12% |
| 10 s 窗口中位 | **1.1%** | **1.1%** | 0.6–0.8% |
| 窗口 p10 | 0.99% | 0.89% | 0.4–0.5% |
| 窗口 p90 | 1.4% | 1.3% | 0.9–1.1% |
| 尖峰 | 41.6% | 40.8% | 40–43% |
| > 10% 的窗口 | 5 / 179 | 5 / 179 | 4 / 179 |
| 空闲分配（平均 / 5 分钟后中位） | 6.9 / 3.0 MB/s | 6.9 / 3.0 MB/s | — |
| GC 次数 / GC 后存活堆 | 517 / 16.7 MB | 508 / 16.7 MB | — |
| 空闲 RSS 结束 / 最大 | **93** / 125 MB | **91** / 142 MB | 117–118 / 118 MB |
| 负载后 RSS 即刻 / 120 s 后 | 140 / 155 MB | 132 / 141 MB | 141–144 / 143–145 MB |
| 负载后 GC 存活堆 | 42 MB | 35 MB | — |
| goroutine（始/末/最大） | 17 / 15 / 19 | 17 / 15 / 19 | 线程 22 |

### 每路由

| 路由 | Go 延迟 ms | Go CPU ms/次 | Go 分配 MB/次 | Py 延迟 ms | Go/Py 延迟 |
|---|---:|---:|---:|---:|---:|
| GET /api/notifications | 47 / 39 | 49 / 50 | 19 / 20 | 83 / 60 | **×0.6** |
| GET /api/agents | 400 / 395 | 495 / 487 | 221 / 219 | 220 / 61 | ×1.8–6.5 |
| GET /api/agents?history=0 | 454 / 407 | 530 / 513 | 230 / 225 | 187 / 152 | ×2.4–3.0 |
| GET /api/agents?summary=1 | 68 / 69 | 86 / 85 | 33 / 33 | 9.7 / 7.4 | ×7–9 |
| GET /api/agents/<id>（最大者） | 266 / 255 | 320 / 312 | 139 / 138 | 53 / 38 | ×5–7 |
| GET /api/agents/<id>/rule-sync | 23 / 14 | 45 / 18 | 22 / 6.5 | 21 / 15 | ×1 |
| POST …/heartbeat | 5.6 / 5.2 | 13 / 11 | 5.8 / 4.2 | 6.7 / 5.5 | ×0.9 |
| GET /api/profiles | 19 / 19 | 24 / 22 | 11 / 11 | 3.1 / 4.3 | ×4–6 |
| GET /api/config/<p>/surge | 22 / 21 | 28 / 27 | 12 / 12 | 29 / 20 | ×0.8–1.1 |
| GET /api/config/<p>/loon | 22 / 25 | 29 / 29 | 12 / 12 | 29 / 19 | ×0.8–1.3 |
| GET /api/config/<p>/mosdns | 26 / 22 | 32 / 28 | 14 / 14 | 40 / 32 | ×0.7 |
| GET /api/config/<p>/oxidns | 53 / 40 | 57 / 51 | 21 / 21 | 78 / 64 | ×0.6–0.7 |
| GET /api/config/<p>/mihomo | 274 / 236 | 359 / 346 | 171 / 171 | 1373 / 1005 | **×0.2** |
| GET …/aggregations/<agg>/provider | 334 / 313 | 441 / 440 | 206 / 206 | 1460 / 1049 | **×0.3** |
| GET /api/routers | 30 / 27 | 35 / 49 | 15 / 25 | 12 / 10 | ×2.5–2.7 |
| GET /api/routers/<id>/policy | 121 / 104 | 184 / 132 | 93 / 70 | 28 / 22 | ×4–5 |
| GET /api/routers/<id>/policy-settings | 131 / 118 | 252 / 201 | 125 / 102 | 42 / 28 | ×3–4 |
| GET /api/routers/<id>/rule-sync | 26 / 27 | 33 / 34 | 15 / 15 | 16 / 10 | ×1.6–2.7 |
| GET /api/agents/<id>/upstream-routers | 121 / 127 | 169 / 158 | 85 / 78 | 125 / 56 | ×1–2.3 |

所有请求返回 200。`GET /api/routers` 第 2 轮分配 25 MB（第 1 轮 15 MB），与 `o10-rootcause.md` 浅拷贝实验里记录的
同一路由分配翻倍现象相似，未追查。

## 与测试服务器数字的对比

两组同代码（或代码差异已知）的对照：

- **batch-21（PVE）vs `o10-rootcause.md` run3（.64，Ryzen 7 9700X，T1+T2）**：batch-21 = T1+T2+T3 及其它合入，
  T3 只改通知/空闲刷新，所以其余路由应当一致。
- **go tip 57af72520（PVE）vs `o10-performance.md`「修复前」（.32，i5-12500）**：两者都不含 O10 修复。

| 路由 / 项 | .64 run3 | PVE batch-21 | 比值 | .32 修复前 | PVE go tip | 比值 |
|---|---:|---:|---:|---:|---:|---:|
| /api/agents 分配 | 221 MB | 219–221 MB | **×1.0** | 688 MB | 710–740 MB | ×1.05 |
| /api/agents/<id> 分配 | 138 MB | 138–139 MB | **×1.0** | 386 MB | 873–1059 MB | ×2.5¹ |
| summary=1 分配 | 33 MB | 33 MB | ×1.0 | 54 MB | 57–73 MB | ×1.2 |
| /api/profiles 分配 | 11 MB | 11 MB | ×1.0 | 31 MB | 30–61 MB | ×1–2 |
| surge 分配 | 12 MB | 12.2 MB | ×1.0 | 137 MB | 128–142 MB | ×1.0 |
| mihomo / provider 分配 | 170 / 206 MB | 171 / 206 MB | ×1.0 | 137 / 390 MB | 364–370 / 847–875 MB | ×2.2–2.7¹ |
| 路由器 policy / policy-settings 分配 | 70 / 76 MB | 70–93 / 102–125 MB | ×1.0–1.6 | — | — | — |
| /api/agents 延迟 | 239 ms | 395–400 ms | ×1.65 | 0.91 s | 1.4 s | ×1.5 |
| summary=1 延迟 | 38 ms | 68–69 ms | ×1.8 | 89 ms | 131–143 ms | ×1.5 |
| mihomo 延迟 | 149 ms | 236–274 ms | ×1.6–1.8 | 0.67 s | 0.69–0.94 s | ×1.0–1.4 |
| provider 延迟 | 221 ms | 313–334 ms | ×1.4–1.5 | 1.0 s | 1.6–2.3 s | ×1.6–2.3 |
| 路由器 policy 延迟 | 71 ms | 104–121 ms | ×1.5–1.7 | — | — | — |
| Python notifications | 38 ms | 60–83 ms | ×1.6–2.2 | 41 ms | 同左 | ×1.5–2 |
| Python agents/<id> | 23 ms | 38–53 ms | ×1.7–2.3 | 25 ms | 同左 | ×1.5–2 |
| Python mihomo | 544 ms | 1.0–1.4 s | ×1.8–2.5 | 0.63 s | 同左 | ×1.6–2.2 |
| Python 空闲平均 / 窗口中位 | 1.07% / 0.3% | 1.45–1.57% / 0.6–0.8% | ×1.4 / ×2–2.7 | 0.9% / ≈0.4% | 同左 | ×1.6–2 |
| Go 空闲稳态 底线 / 分配 | 0.7% / 4.2 MB/s（T1+T2） | 1.1% / 3.0 MB/s（含 T3） | — | ≈9% / 36 MB/s | 11–12% / 37.8 MB/s | ×1.3 / ×1.0 |
| Go 空闲 RSS 结束 | 107 MB | 91–93 MB | — | 289 MB | 166–204 MB | 见下 |

¹ .32「修复前」与 go tip 之间 go 又合了 batch-20 等改动，且 perf.py 对多实例标签取最大值，这几项差异是代码/口径，不是机器。

解读：

- **分配是硬数据**：同代码在 .64 与 PVE 上几乎逐字一致（含 2 核限制、不同 CPU），两轮之间也 ≤ 10%。测试服务器上的
  分配数字和 allocs profile 可以直接作为证据。
- **延迟/CPU 同比放大**：PVE 上 Go 慢 1.4–1.8 倍、Python 慢 1.6–2.5 倍（Python 单线程对低频 E 核更敏感）。倍数相近但不相同：同一路由的 Go/Python 比值在 PVE 上比测试服务器约有利于 Go 20–30%。所以测试服务器上的比值可作估计，“≤ Python”的验收只按本机同机数字判。
- **空闲 CPU 百分比**放大约 1.5–2.5 倍，同上，绝对阈值必须在本机重定（见结论 4）。
- **RSS** 受 GC 节奏影响（2 核下 GC 更频繁），go tip 结束值反而比 .32 低、最大值更高；batch-21 的存活堆两边都是 17 MB。
  RSS 跨机器只能看量级。
- **尖峰**：CT 只有 2 核，go tip 的尖峰被封顶在 200% 并拉长；batch-21 已无此问题（41%，与 Python 相同）。

## 私有输出

测试服务器 .32 `~/cf-prod-snapshot/runs/`：`pve-20261121-103612`（Go1）、`pve-20261121-111508`（Py1）、
`pve-20261121-115001`（Go2）、`pve-20261121-123001`（Py2）、`pve-20261121-130509` / `pve-20261121-134110`（batch-21 Go1 / Go2）。不提交。

## 结果：go 77c4c4538（R1/R2/R4/R6/R7/R10 合入后，只测 Go，一轮，2026-11-22）

Python 列取本文前面同一 CT、同一快照两轮中**较快**的一轮（口径从严）。窗口统计由 idle-series.json 按 10 s 窗口计算。

| 空闲 | Go | Python |
|---|---:|---:|
| CPU 平均 | **1.05%** | 1.45–1.57% |
| 10 s 窗口中位 | **0.40%** | 0.6–0.8% |
| 窗口 p10 | **0.20%** | 0.4–0.5% |
| 窗口 p90 | 0.60% | 0.9–1.1% |
| RSS 结束 / 最大 | **90 / 129 MB** | 117–118 MB |

| 路由 | Go 延迟 ms | Go 分配 MB/次 | Py 延迟 ms |
|---|---:|---:|---:|
| GET /api/notifications | 30.0 | 14.5 | 60 |
| GET /api/agents | 11.2 | 5.4 | 61 |
| GET /api/agents?history=0 | 17.3 | 17.5 | 152 |
| GET /api/agents?summary=1 | 3.7 | 0.5 | 7.4 |
| GET /api/agents/<id>（最大者） | 8.1 | 3.5 | 38 |
| GET /api/agents/<id>/rule-sync | 6.6 | 2.0 | 15 |
| POST …/heartbeat | 2.9 | 0.3 | 5.5 |
| GET /api/profiles | 2.4 | 0.1 | 3.1 |
| GET /api/config/<p>/surge | 13.1 | 6.1 | 20 |
| GET /api/config/<p>/loon | 13.5 | 6.1 | 19 |
| GET /api/config/<p>/mosdns | 15.1 | 8.2 | 32 |
| GET /api/config/<p>/oxidns | 31.8 | 14.6 | 64 |
| GET /api/config/<p>/mihomo | 231 | 156 | 1005 |
| GET …/aggregations/<agg>/provider | 295 | 179 | 1049 |
| GET /api/routers | 3.9 | 0.6 | 10 |
| GET /api/routers/<id>/policy | 8.1 | 3.2 | 22 |
| GET /api/routers/<id>/policy-settings | 20.1 | 9.2 | 28 |
| GET /api/routers/<id>/rule-sync | 3.1 | 0.5 | 10 |
| GET /api/agents/<id>/upstream-routers | 31.7 | 30.1 | 56 |

结论：这一轮所有空闲指标和全部 19 条路由都优于 Python（Agent 视图缓存默认开启）。正式验收还需：第二轮重复、
O18 T1a–T1d 与空闲收尾合入后复测、R12（GC/RSS）后复测，以及同日重测一轮 Python。
