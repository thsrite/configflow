# N7-3c per-input migration gate

Source: `.64`, `go test -p=4 -count=1 -v -run "MigrationGate|TestN73NativeDepthBoundary|TestN73PlainDepthBoundary"` on the seven scoped packages. Counts are **input occurrences**, not unique texts; each row gets one primary category. Timestamp and non-JSON tags are classified by the old constructed type versus the new Node contract even if re-dumping both would hide that distinction. Boundary outputs are verified separately.

| Category | yamlx core | config | routers | agents |
|---|---:|---:|---:|---:|
| D1-duplicate | 8 | 29 | 7 | 1 |
| YAML1.1/timestamp | 58 | 586 | 43 | 0 |
| equal | 1694 | 15400 | 3168 | 8242 |
| explicit-scalar-native | 4 | 0 | 0 | 0 |
| native-diagnostics | 6 | 2965 | 141 | 1 |
| reader-policy | 373 | 64 | 9 | 0 |
| tagged-collections | 81 | 556 | 33 | 0 |
| unsafe-alias | 4 | 54 | 6 | 0 |
| 原生解析器语法范围 | 8 | 1255 | 0 | 0 |
| 复合映射键：Node 保留 | 5 | 6 | 8 | 1 |
| 未知标签：Python 拒绝 → Go 接受 | 84 | 57 | 19 | 1 |
| 深层普通节点：Go 接受 | 1 | 0 | 4 | 0 |
| **Total** | 2326 | 20972 | 3438 | 8246 |

## Every caller / fixture group

- `yamlx / yamlx/corpus.jsonl`: 1591 — equal=1591.
- `yamlx / yamlx/review.jsonl`: 735 — D1-duplicate=8, YAML1.1/timestamp=58, equal=103, explicit-scalar-native=4, native-diagnostics=6, reader-policy=373, tagged-collections=81, unsafe-alias=4, 原生解析器语法范围=8, 复合映射键：Node 保留=5, 未知标签：Python 拒绝 → Go 接受=84, 深层普通节点：Go 接受=1.
- `config / testdata/corpus.jsonl.gz/payload`: 3186 — D1-duplicate=26, YAML1.1/timestamp=46, equal=2704, native-diagnostics=224, reader-policy=23, tagged-collections=110, unsafe-alias=29, 未知标签：Python 拒绝 → Go 接受=24.
- `config / testdata/corpus.jsonl.gz/upstreams`: 5413 — YAML1.1/timestamp=243, equal=3387, native-diagnostics=1047, tagged-collections=161, 原生解析器语法范围=575.
- `config / testdata/corpus.jsonl.gz/custom/upstreams`: 8191 — YAML1.1/timestamp=294, equal=5640, native-diagnostics=1232, reader-policy=29, tagged-collections=273, unsafe-alias=19, 原生解析器语法范围=677, 未知标签：Python 拒绝 → Go 接受=27.
- `config / testdata/recorded.jsonl.gz/custom/upstreams`: 364 — equal=360, native-diagnostics=4.
- `config / testdata/recorded.jsonl.gz/payload`: 100 — equal=100.
- `config / testdata/recorded.jsonl.gz/upstreams`: 45 — equal=45.
- `config / mosdns/archive.checkReferences`: 87 — equal=87.
- `config / mosdns/C21-review`: 20 — equal=5, tagged-collections=9, unsafe-alias=6.
- `config / provider/corpus`: 1415 — D1-duplicate=3, YAML1.1/timestamp=3, equal=1279, native-diagnostics=100, reader-policy=12, tagged-collections=3, 原生解析器语法范围=3, 复合映射键：Node 保留=6, 未知标签：Python 拒绝 → Go 接受=6.
- `config / provider/recorded`: 2151 — equal=1793, native-diagnostics=358.
- `routers / routers/FakeIPNetwork`: 613 — D1-duplicate=3, YAML1.1/timestamp=1, equal=531, native-diagnostics=76, reader-policy=1, tagged-collections=1.
- `routers / testdata/r02_recorded.jsonl.gz/check_generation`: 1207 — equal=1207.
- `routers / testdata/r02_recorded.jsonl.gz/full_tun_base_yaml`: 11 — equal=10, native-diagnostics=1.
- `routers / testdata/r02_recorded.jsonl.gz/router_split_state`: 74 — equal=73, native-diagnostics=1.
- `routers / testdata/r02_recorded.jsonl.gz/mode_from_context`: 61 — equal=61.
- `routers / testdata/r02_recorded.jsonl.gz/base_is_full_tun`: 23 — equal=23.
- `routers / testdata/r02_corpus.jsonl.gz/full_tun_base_yaml`: 324 — YAML1.1/timestamp=4, equal=301, native-diagnostics=14, tagged-collections=1, 未知标签：Python 拒绝 → Go 接受=4.
- `routers / testdata/r02_corpus.jsonl.gz/check_generation`: 225 — YAML1.1/timestamp=3, equal=207, native-diagnostics=10, tagged-collections=2, 未知标签：Python 拒绝 → Go 接受=3.
- `routers / testdata/r02_corpus.jsonl.gz/base_is_full_tun`: 236 — YAML1.1/timestamp=3, equal=219, native-diagnostics=13, 未知标签：Python 拒绝 → Go 接受=1.
- `routers / testdata/r02_corpus.jsonl.gz/mode_from_context`: 226 — YAML1.1/timestamp=3, equal=211, native-diagnostics=9, tagged-collections=1, 未知标签：Python 拒绝 → Go 接受=2.
- `routers / testdata/r02_corpus.jsonl.gz/router_split_state`: 96 — YAML1.1/timestamp=1, equal=89, native-diagnostics=5, 未知标签：Python 拒绝 → Go 接受=1.
- `routers / testdata/r02_edge.jsonl.gz/check_generation`: 35 — equal=35.
- `routers / testdata/r02_edge.jsonl.gz/mode_from_context`: 105 — D1-duplicate=1, YAML1.1/timestamp=7, equal=76, native-diagnostics=3, reader-policy=2, tagged-collections=9, unsafe-alias=2, 复合映射键：Node 保留=2, 未知标签：Python 拒绝 → Go 接受=2, 深层普通节点：Go 接受=1.
- `routers / testdata/r02_edge.jsonl.gz/full_tun_base_yaml`: 60 — D1-duplicate=1, YAML1.1/timestamp=7, equal=41, native-diagnostics=3, reader-policy=2, tagged-collections=1, 复合映射键：Node 保留=2, 未知标签：Python 拒绝 → Go 接受=2, 深层普通节点：Go 接受=1.
- `routers / testdata/r02_edge.jsonl.gz/base_is_full_tun`: 71 — D1-duplicate=1, YAML1.1/timestamp=7, equal=42, native-diagnostics=3, reader-policy=2, tagged-collections=9, unsafe-alias=2, 复合映射键：Node 保留=2, 未知标签：Python 拒绝 → Go 接受=2, 深层普通节点：Go 接受=1.
- `routers / testdata/r02_edge.jsonl.gz/router_split_state`: 71 — D1-duplicate=1, YAML1.1/timestamp=7, equal=42, native-diagnostics=3, reader-policy=2, tagged-collections=9, unsafe-alias=2, 复合映射键：Node 保留=2, 未知标签：Python 拒绝 → Go 接受=2, 深层普通节点：Go 接受=1.
- `agents / installer/readCompose+annotateCompose`: 8239 — equal=8239.
- `agents / discovery/customPorts`: 7 — D1-duplicate=1, equal=3, native-diagnostics=1, 复合映射键：Node 保留=1, 未知标签：Python 拒绝 → Go 接受=1.
