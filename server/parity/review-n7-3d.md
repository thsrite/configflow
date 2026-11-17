# N7-3d 独立复核

复核者未参与作者实现；起点 `go@a08c4cc27`，范围 `f90a156e..a08c4cc2`。
分支 `go-review-n7-3d`。仅用 rt 测试；新增用例为合成数据；无生产访问、push 或子 agent。
状态：复核及最终门禁进行中。

## 发现

- **F1 / medium：provider 和 gateway 返回错误丢失原始 reader 原因链。**
  providerSourceError 把原始错误仅格式化为字符串，providerContext 再次重建 Error；
  gateway.BaseDocument/yamlPayloadStrings 直接替换成固定错误。七种合成拒绝
  （syntax、D1、tag shape、merge、explicit scalar、递归、depth40 alias bomb）
  全部拒绝，但 `errors.Is` 无法辨别原始原因。修复前两个新增回归共14个场景均失败。
  修复保留原因链，仍能匹配原验证错误类；中文展示文本不变，完整原生诊断、原生行列
  通过 `errors.As(*yamlx.SyntaxError)` 保留。gateway 不新增显示诊断，以遵守原有中文合同。
  `TestReviewN73DProviderReaderCauses` / `TestReviewN73DGatewayReaderCauses` 通过；
  原235条 provider 完整显示参照也通过（0位置差）。修复提交：`117f785f0`。
- **F2 / medium：AST 门漏扫 cmd/tools 对 yamlxtest 的运行时导入，并放过 yamlx 任意子包。**
  原门仅限制 internal 下运行时 helper 导入，且 WalkDir 跳过整个 yamlx 目录。
  合成违规文件在 cmd 导入 yamlxtest、在 yamlx/bypass 直接引用 yaml.Unmarshal，旧门均退出0；
  修复门均退出1。原有 raw Unmarshal、runtime Clone、internal helper 负控旧/新均退出1。
  现扫描全部子包，仅 yamlx 本包和 yamlxtest 本包可直接读 YAML；两个固定 oracle 工具和
  testdata recorder 可以导入测试 helper，其余 cmd/tools/internal 的非测试文件均禁止。
  11个永久正/负控覆盖 import alias、函数引用、dot import、Clone、helper、子包，全部通过。
  修复提交：`02afae340`。
- **F3 / low：冻结档案缺少独立全来源执行门。**
  原有 Classify 按调用者选中的来源运行；convert 的3837来源只跑注入值/Parse audit，
  不调用 Classify。档案条数/SHA固定不能保证全部21343来源均在每次测试实际执行。
  新增 `TestAllFrozenReaderSources` 对每条来源调用 Classify，逐条校验当前完整图或完整错误，
  不恢复旧实现；执行21343，unknown=0，类别计数与作者报告一致。修复提交：`02afae340`。

- **F4 / low：deploy.SourceError 把原生未知坐标显示为第0行/第0列。**
  NUL 控制字符的原生解析错误没有位置；未闭合 flow list 只有行号。原实现无条件把0
  转为位置指针，向用户显示不存在的行列。新增三种真实 Parse 合成控制，修复前前两种
  失败；现在仅附加原生确有的正行号/列号，保留原始原因和诊断。provider hook 的历史
  展示合同不变，235条冻结完整显示参照继续通过。修复提交：本次坐标修复提交，最终补列 SHA。

## 验证（迭代）

所有下列命令由 `RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; …'` 执行。

- 修复前：`go test -count=1 -run "^TestReviewN73D.*ReaderCauses$" ./internal/gateway ./internal/provider` → exit1，14个场景丢失 cause。
- 修复后：`go test -count=1 -run "^TestReviewN73D.*ReaderCauses$|^TestN73NativeDiagnosticInterface$" ./internal/gateway ./internal/provider` → gateway0.009s/provider0.043s PASS；随后两个包 vet 通过。
- `go test -count=1 -v -run "^TestYAMLFacade|^TestAllFrozenReaderSources$" ./internal/yamlx ./internal/yamlx/yamlxtest` → yamlx0.572s/yamlxtest4.948s PASS；21343条真实冻结来源实际执行。

最终逐调用点、分类抽样、用户补充条件和完整门禁结果待补齐。

补验：`go test -count=1 -run "^TestReviewN73DSourceErrorUnknownCoordinates$|^TestN73NativeDiagnosticInterface$" ./internal/deploy ./internal/provider` → deploy0.022s/provider0.078s PASS。
`go test -p=4 -count=1 ./internal/rules ./internal/deploy/...` → rules3.607s/deploy24.485s/contentsync0.642s/pushdiff7.947s PASS；随后相关包 vet 通过（规则明细恢复前）。
