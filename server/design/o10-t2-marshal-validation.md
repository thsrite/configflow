# O10-T2：`model` 编解码只校验一次

状态：实现于 `go-perf-t2`（2026-11-21）。来源：`server/parity/o10-performance.md` 热点 2 与任务 T2。

## 现状调用链

编码（以一份 Agent 列表为例）：

```
model.Marshal([]Agent)                      acyclic(整棵) + marshalValue + validJSON(整棵)
 └ Agent.MarshalJSON → EncodeRecord
    └ 每个字段 Marshal(field)               acyclic(字段子树) + validJSON(字段子树)
       └ Map[Deployment].MarshalJSON → 每个值 Marshal(...) → Deployment.MarshalJSON → EncodeRecord → ...
    └ objectJSON(...)                       validJSON(整条记录)
```

每一层记录、Map、字段都把自己的整个子树重新 `acyclic` 一遍、`validJSON` 一遍（`objectJSON` 末尾还有一遍），
嵌套 depth 层就重复 depth（实际约 2×depth）次。`validJSON` 本身是 `utf8.Valid` + `json.Valid` + 用
`json.Decoder.Token` 逐 token 再走一遍（每个字符串、数字都分配），O10 负载期它占全部分配的 39%。

解码：`DecodeRecord` → `parseObject` 先 `validJSON`，再用 `json.Decoder` 第三次扫描切分成员；每个已声明成员
`decodeInto` → `json.Unmarshal`（再扫描 + 拷贝）→ 子记录 `UnmarshalJSON` → `DecodeRecord` → 又一轮。每个字段的
基线 `Marshal(field)` 同样带整棵校验。

## 改法

1. **校验器单次扫描。** `validJSON` = `utf8.Valid` + `jsontext.Value.IsValid(AllowInvalidUTF8(true))`：标准库
   `encoding/json/jsontext` 一次扫描同时检查语法、嵌套深度（10000）和**每一层**的重复成员名（按解码后的名字比较）。
   只有失败时才走慢路径分类：UTF-8 不合法或 `json.Valid` 不通过 → 原来的 `ErrJSON`；否则只剩重复名 → 原来的
   `ErrJSON: duplicate member`。错误值与消息与旧实现逐字相同。
2. **`parseObject` 两遍变一遍切分。** 校验之后用 `jsontext.Decoder`（直接读 `bytes.Buffer`，不拷贝）读出顶层成员的
   偏移；成员的 key/leading/separator/value/trailing 切片按旧算法从同样的偏移取，`jsonObject` 结构不变。
3. **编码“单元”只在含外来字节时校验。** 新的内部函数 `encodeUnit(v)` 取代 `model` 内部对 `Marshal` 的调用
   （`EncodeRecord` 每个字段、`Map` 每个值、解码基线、`SetMemberJSON` 基线、`TextLines`、`Config`）。它做的事和旧
   `Marshal` 一样，只是：
   - `acyclic` 只在类型图可能成环时运行（类型含接口或递归类型，结果按类型缓存）；不可能成环的类型旧实现的
     `acyclic` 必然通过，跳过不改变结果。
   - 编码时，`model` 包自己的类型（记录、`Map`、`Fields`、`Dynamic`、`TextLines`、`MosDNSMatch`、`Config`、
     `Document`）的 `MarshalJSON` 输出视为可信；其他 `json.Marshaler`（`json.RawMessage`、其他包的类型）的输出
     标记为“外来”。单元结束时只有含外来字节才 `validJSON` 一次；Go map 组合内含外来字节时也在组合处校验一次
     （与旧 `objectJSON` 的校验点一致）。
   - `objectJSON` 不再在末尾整段校验。
4. **深度上限不满足“组合即合法”。** 两个各自合法、深 10000 层的值放进记录/列表/Map 后会超过 `encoding/json` 与
   `jsontext` 的 10000 层上限，旧实现在每个 `objectJSON` 与每个单元的 `validJSON` 处拒绝（`ErrJSON`）。因此这些旧校验点
   保留一个**只数括号**的检查 `tooDeep`：不分配，字符串内容用 `bytes.IndexByte` 跳过；输出不足 20002 字节时
   （不可能深于 10000 层）直接跳过，所以普通记录零开销，只有大输出（如整份 Agent 列表的上几层）各扫一遍。
   失败位置、顺序与错误值（`ErrJSON`）与旧实现相同——参照语料里有 9995–10000 层逐层越界的用例，最初漏掉这一点的
   实现正是被它们抓到的。
5. **对外 `Marshal` 在最终字节上校验一次**（纵深防御）；`EncodeRecord`、`Map/Fields.MarshalJSON` 不再整段校验，
   因为它们既是对外入口又是嵌套路径，无法区分；它们的输出按下面的不变量“构造即合法”。
6. **解码不再经 `encoding/json` 转一手。** `decodeInto` 的切片分支用 `jsontext` 切元素（不再 `json.Unmarshal`
   到 `[]json.RawMessage`）；目标是 `model` 包里实现了 `UnmarshalJSON` 的类型时直接调用它（数据已由上层校验，
   去掉首尾空白，与 `encoding/json` 传给 `UnmarshalJSON` 的字面量相同）。每层自己的 `parseObject` 仍完整校验一次
   （它也是外部 `json.Unmarshal` 的入口，不能信任调用方）。

## 不变量（为什么少校验仍等价）

`model` 的编码器只把下列字节拼成输出：(a) 解码时已校验过的原始成员字节与空白片段；(b) `Fields.Set`、
`SetMemberJSON`、`Dynamic.UnmarshalJSON` 入口校验过的字节；(c) `encoding/json` 编码的标量；(d) `model` 编码器自己的
输出；(e) 外来 `Marshaler` 的输出——它们按旧粒度（所在单元 / Go map）校验。对象组合在 `objectJSON` 里检查成员名
唯一（`seen`），`EncodeRecord` 拒绝遮蔽已声明成员的 opaque 成员。由归纳，任何 `model` 编码器的输出都是合法、
UTF-8 正确、无重复名的 JSON（深度另由第 4 条在原校验点检查）；因此去掉的那些校验在旧实现里也必然通过，错误只可能出现在 (e)，而 (e) 的校验点与
旧实现相同，报错的值、消息、发生顺序不变。输出字节不受校验影响。

保留的语义：输出字节、错误类型与消息、未知成员保留、大整数（`json.Number`/`Int`）、R5（错误类型的成员
保持原字节并给出 FieldIssue）、R2 基线比较、成员顺序；`Marshal` 仍拒绝环（`cyclic value`）。

## 风险

| 风险 | 处理 |
|---|---|
| 将来有人在 `model` 里写一个拼接未校验字节的 `MarshalJSON`，而它被当作可信 | 文档注释写明不变量；对外 `Marshal` 仍有最终校验；fuzz 目标检查“`MarshalJSON` 成功则输出合法” |
| `jsontext` 与旧 `json.Valid`+`Decoder.Token` 在边角（孤立代理项转义、深度上限、空白）上判定不同 | `FuzzValidJSONReference` / `FuzzParseObjectReference` 以测试内保留的旧实现为参照逐字节比较 |
| 跳过 `acyclic` 漏掉环 | 只对类型图无接口、无递归的类型跳过——这些类型的值不可能成环；语料含切片/映射自环 |
| 直接调用 `UnmarshalJSON` 与 `encoding/json` 的差别（null、空白、指针） | 只在非指针目标、`model` 包类型上走捷径，先去掉 JSON 空白；指针与其他类型仍走 `json.Unmarshal` |

## 验收

- `testdata/o10t2_reference.txt`：旧实现对 9249 个确定性合成用例（Agent 文档的解码/编码/编辑/损坏、
  `SetMemberJSON`、各类片段/重复名/坏 UTF-8/深度 10000–10001、环、Config、Document）记录的结果（错误文本或输出
  摘要 + FieldIssue）；新实现逐行一致（`TestO10T2ReferenceCorpus`）。
- fuzz：`FuzzValidJSONReference`、`FuzzParseObjectReference`（新旧实现对照），`FuzzMarshalOutputValid`
  （解码→编辑→编码：`Marshal` 与 `MarshalJSON` 字节相同且合法、无重复名）。
- 基准 `BenchmarkO10T2AgentList`（Agent 列表编码/解码，分配与时间）；`tools/o10perf` 前后对比。
