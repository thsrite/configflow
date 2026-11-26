# Artifact 三入口 HR 复核（fresh Opus，非作者）

范围：`71e2f9abb`、`cb3848319`、`a51fb5f20`（交接 `772b17de8`，分支 `go-port-artifacts`）。
入口：`POST /api/generate/<service>/artifact`、`POST /api/profiles/<profile_id>/generate/<service>/artifact`、
`GET /api/generate/artifacts/<preview_id>/<action>`。对照 Python `backend/routes/generate.py`
`generate_artifact`/`read_artifact`、`backend/routes/profiles.py`、`backend/routes/agents.py`
`download_final_agent_config`/`read_final_agent_file`。按 WORKFLOW「Leaner verification」：未重跑作者整套，
只读代码、定向测试、一个负控。

**结论：high 0 / medium 0 / low 3。C34 artifact 部分改 `verified`。**

## 发现

| # | 级别 | 发现 | 处理 |
| --- | --- | --- | --- |
| L1 | low | `readPrivate` 先 `Lstat` 后 `Open`（跟随 symlink），`load` 对 root/folder 也是先 Lstat 后按路径打开：在检查与打开之间把文件或中间目录换成 symlink 仍可被跟随（TOCTOU）。前提是攻击者能写数据目录；而 `record.json` 本身没有签名/MAC，能写该目录就能直接伪造一条合法记录，所以 symlink 防护只是纵深防御。作者把根目录 symlink 评为 medium 偏高，同属此类。 | 不修。若以后要做，用 `O_NOFOLLOW` 打开 + `f.Stat()`，目录级需 openat 语义（`os.Root` 不能拒绝根内 symlink）。L2 后项。 |
| L2 | low | 手动 artifact 与 Agent 发布预览共用同一 `.generated-previews` 配额（24 个 / 512 MiB，按 mtime 驱逐）。短时间连续手动导出 ≥24 次会驱逐尚未推送的 Agent 预览，推送时得到 `preview_changed`。与 Python 行为一致（同一 `previews.save`）。 | 不修，保持 parity；切换后如有反馈再分开配额。 |
| L3 | low（测试缺口） | 比较器 `artifact_policy.py` 把 `body.content` 换成已证明主文件的结构化投影，但自检只有“附加文件改变会报差异”的负控，没有“主配置语义不同会报差异”的负控，投影可能吞掉主配置差异而不被发现。 | 已补负控 `test_main_config_semantic_difference_is_reported`：Go 侧主配置 `dns.enable` 改为 false，断言出现 `body.content*` 差异且不是 proof 失败。通过，说明投影没有吞差异。 |

另补 Go 定向测试 `TestArtifactPreviewIDVariantsNeverSelectPlantedRecords`：对大写、尾随 `\n`、31/33 位、`.` 前缀
五种 ID 变体，在**正好同名**的目录下放一份除 id 外完全合法的记录和归档，GET file/download 必须 409
`preview_changed`；规范 ID 正控 200。负控：在服务器副本上把 `previewIDPattern` 放宽为 `(?i)`，该测试失败（随后同步恢复）。

Python bug（Go 已修，非差异项）：Python `generate_artifact` 在生成**之后**才读 `repository.config_revision`，
并发保存时记录的 revision 可能比实际生成用的配置新；Go `configgen.Capture` 用 `GetPublishSnapshot`
一次取得配置与 revision。

## 核对过、无问题的项

1. **路径与文件**：`preview_id` 只接受 `^[a-f0-9]{32}$`（RE2 的 `$` 不匹配尾随换行）；`action` 白名单在加载前判断；
   `file` 的 `path` 经 `RelativePath` 规范化且必须等于原值，再在 manifest 内容 map 中查找，不拼磁盘路径；
   `../`、绝对路径、`a/../b`、空串、缺失均 404（作者测试 + 与 Python KeyError→404 一致）。下载 ZIP 的条目名来自
   已由 `Pack`/`Open` 校验过的 manifest，无 zip slip；生成侧对 MosDNS/OxiDNS 归档逐成员 `RelativePath`、去重、
   文件数和单个/总大小上限，读取用 `LimitReader(limit+1)`，压缩炸弹有界。ZIP 成员 `0600`（与 Python writestr 一致，
   真 Flask 对照）。临时文件 `atomicWrite` defer 删除，Save 失败删除整个目录；目录 0700。
   symlink：root、artifact 目录、record、archive 的静态 symlink 均拒绝（作者测试）；竞态替换见 L1。
2. **归属/TTL**：读取固定 `agentID="manual"`，其他 Agent 的预览 409；过期按 `ExpiresAt<=now` 拒绝；Previews 按
   Manager 区分，另一仓库读同 ID 被拒（作者测试）。Agent 推送路径以真实 agent id 加载，Agent ID 由服务器生成
   （`agent_<ms>_<hex>`），不会等于 `manual`，手动 artifact 不能被推送到设备。`/api/agents/manual/generated-config/...`
   能读手动预览，与 Python 相同，均需管理员认证。显式 profile 路径不被 `?profile_id=` 覆盖（作者测试）；
   冻结内容在之后改配置、切默认 profile 后不变。ID 为 128 位随机能力标识，不会被并发请求替换。
3. **与 A14 边界**：`GenerateArtifact` 只用 `RunDetached`、`Pack`、`previewsFor(m).Save`；不建 reservation，不调发布、
   `FinalizeBundle`、上传或激活。成员校验与 `Pack` 部分重叠，但属于 ZIP→成员的入口适配，不是复制发布逻辑。
   锁：GenerateArtifact 自身不持锁，`Save` 只持叶锁 L4 且锁内不进 repo；等待预览锁期间关机（真实 Runtime、生产宽限）
   不写预览、不改已提交 generated 文件；关机前/断连五引擎都有真实 Runtime 用例。
4. **认证与头**：三入口都包 `auth.Require`，auth 无 generate 前缀豁免。`download`：`application/zip`、
   `attachment; filename=<service_type>-final-config.zip`，`service_type` 来自已校验引擎，无文件名注入；`file`：
   `application/octet-stream` 无 Content-Disposition（同 Python）；成功响应 `no-store`，409 不带（同 Python）。
5. **删除的 34 条 baseline**：逐条是 POST 两前缀 ×15 字段（503 时的 missing/extra/status）+ GET 4 条（503 的 code/message/
   Cache-Control/status）。接线后 POST 由 `artifact_policy` 在逐侧证明记录、manifest、tar、逐文件摘要后，仅替换
   id/deployment_id/expires_at 和 size/sha256；路径、角色、顺序、EOL、主配置及全部成员结构化比较，
   `subscription_hosts/dialers` 原样比较。GET 在 proddiff 中只有无效/外来 ID 的 409 两侧一致，成功读取由
   真 Flask difftest 逐字节覆盖。没有对 artifact 路由的 normalize 规则。作者报告最终 compare `new=0 stale=0`
   （未重跑，按 Leaner verification）。负控见 L3。
6. **Go 风格**：新消息常量在 `messages.go`；错误不既记日志又返回；无新 panic/裸 goroutine；`ctx` 首参。

## L2 / 切换后项

- L1 TOCTOU 加固（全 deploy 存储共用，非本模块独有）。
- L2 手动/Agent 预览分配额（如有需要）。
- 真实浏览器下载（文件名、解压权限）随 L2 实机验收确认。

## 运行的命令（.32）

```sh
RT_HOST=sunbroke@<.32> rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/generate)" && echo gofmt-ok && go vet ./internal/api/generate && go test -race -count=1 -timeout=6m ./internal/api/generate -run "^TestArtifact" && cd .. && "$HOME/cf-test/.venv/bin/python" -m unittest discover -q -s server/tools/proddiff -p "test_artifact_policy.py"'
```

末行：`gofmt-ok`；`ok configflow/internal/api/generate 5.511s`；`Ran 7 tests in 0.017s / OK`。
负控（服务器副本放宽 ID 正则）：`--- FAIL: TestArtifactPreviewIDVariantsNeverSelectPlantedRecords`，随后重新同步恢复。
