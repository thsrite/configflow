# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| G11 / log/off/False/draft | `POST /api/oxidns/render` | open | — | 28e6e0c12 | 200 / 200 |
| G11 / log/off/True/draft | `POST /api/profiles/<profile_id>/oxidns/render` | open | — | 28e6e0c12 | 200 / 200 |
| G12 / False/conflict-current | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/conflict-template | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/merge-independent-fields | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/no-op-comments | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/prepare-template | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/remove-standard-cache | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/rename/False | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/rename/True | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/resolved-baseline-is-idempotent | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / False/restore-standard-cache | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/conflict-current | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/conflict-template | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/merge-independent-fields | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/no-op-comments | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/prepare-template | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/remove-standard-cache | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/rename/False | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/rename/True | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/resolved-baseline-is-idempotent | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G12 / True/restore-standard-cache | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| G13 / expired/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / expired/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / same-name-different-definition/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| G13 / same-name-identical/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| H1061 / actual-YAML-syntax-error | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | open | — | 28e6e0c12 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/<sub_id>/proxies` | open | — | 28e6e0c12 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/proxies` | open | — | 28e6e0c12 | 400 / 400 |
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 28e6e0c12 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 28e6e0c12 | 400 / 200 |
| H123 / original-and-mirror-both-fail-through-loopback-proxy | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 502 / 502 |
| H128 / sequence-root | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H128 / sequence-root | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / duplicate-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / duplicate-payload | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / mapping-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / mapping-payload | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / missing-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H129 / missing-payload | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H130 / alias | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H130 / alias | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H130 / anchor | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H130 / anchor | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / block-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / block-entry | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / nested-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / nested-entry | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / non-string-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H131 / non-string-entry | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H132 / duplicate-extra-flow-key | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H132 / duplicate-extra-flow-key | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H133 / multiline-extra-flow-value | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H133 / multiline-extra-flow-value | `GET /api/rules/local/<name>` | open | — | 28e6e0c12 | 200 / 200 |
| H470 / native-normalization/duplicate-key | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N7-3d](behaviour-changes.md#n7-3d-share--oxidns-readers) | 28e6e0c12 | 400 / 400 |
| H470 / native-normalization/duplicate-key | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N7-3d](behaviour-changes.md#n7-3d-share--oxidns-readers) | 28e6e0c12 | 400 / 400 |
| H474 / native-normalization/recursive-alias | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N7-3d](behaviour-changes.md#n7-3d-share--oxidns-readers) | 28e6e0c12 | 400 / 400 |
| H474 / native-normalization/recursive-alias | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N7-3d](behaviour-changes.md#n7-3d-share--oxidns-readers) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/cache_dump_interval/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/cache_lazy_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/cache_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/dynamic_domain_set.args.batch_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/dynamic_domain_set.args.flush_interval_ms/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/dynamic_domain_set.args.queue_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/fallback_concurrent/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/fallback_threshold/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ip_selector.args.max_parallel_probes/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ip_selector.args.max_wait/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ip_selector.args.probe_stagger/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ip_selector.args.probe_timeout/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ip_selector.args.top_n/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/local_concurrent/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/log.max_files/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/probe_cache_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.batch_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.cleanup_interval_hours/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.flush_interval_ms/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.memory_tail/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.queue_size/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.reader_concurrency/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/query_recorder.args.retention_days/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/remote_concurrent/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/remote_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ros_address_list.args.max_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ros_address_list.args.min_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ros_route.args.max_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/ros_route.args.min_ttl/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/oxidns/worker_threads/nan | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/cache_dump_interval/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/cache_lazy_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/cache_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.batch_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.flush_interval_ms/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.queue_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/fallback_concurrent/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/fallback_threshold/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.max_parallel_probes/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.max_wait/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.probe_stagger/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.probe_timeout/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.top_n/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/local_concurrent/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/log.max_files/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/probe_cache_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.batch_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.cleanup_interval_hours/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.flush_interval_ms/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.memory_tail/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.queue_size/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.reader_concurrency/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.retention_days/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/remote_concurrent/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/remote_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ros_address_list.args.max_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ros_address_list.args.min_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ros_route.args.max_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/ros_route.args.min_ttl/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H511 / /api/profiles/<profile_id>/oxidns/worker_threads/nan | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · C29 / N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 400 / 400 |
| H911 / actual-public-catalog-key-is-bound-to-DNS-follow | `PUT /api/rules/dns` | open | — | 28e6e0c12 | 400 / 400 |
| H916 / retained-native-issue-moves-after-removing-real-pinned-helpers | `PUT /api/rules/<rule_id>` | open | — | 28e6e0c12 | 409 / 409 |
| R007-S02 /  | `GET /api/aggregations/<agg_id>/preview` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R008-S05 / failed-source-valid-cache | `GET /api/aggregations/<agg_id>/provider` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R011-F06 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 400 |
| R011-F07 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 400 |
| R011-F08 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 415 |
| R012-F01 /  | `GET /api/auth/status` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 405 / 405 |
| R015-B01 /  | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R015-B02 /  | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R015-B03 /  | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R015-B04 /  | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R015-F08 / empty | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F09 / array | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F10 / unknown | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F11 / text-type | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F12 / auto-type | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F13 / keep-negative | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F14 / keep-large | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F15 / daily-large | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-F16 / keep-bool | `POST /api/backup/config` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R015-S04 /  | `POST /api/backup/config` | accepted | [behaviour-changes.md · C35空密码保留](behaviour-changes.md#c35webdav-凭据安全与表单密码保留) | 28e6e0c12 | 200 / 200 |
| R016-B02 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-B03 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-B90 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-B91 / Asia/Shanghai/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-B91 / Etc/GMT+12/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-B91 / Etc/GMT-14/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-F08 / array | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F09 / empty | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F10 / unknown | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F11 / text-type | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F12 / auto-type | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F13 / daily-large | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F13 / keep-bool | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F13 / keep-large | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F13 / keep-negative | `POST /api/backup/now` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R016-F15 /  | `POST /api/backup/now` | closed | abdf47ef, d01fdf32, f9963a7f, 93369229b；93369229b为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 28e6e0c12 | 500 / 500 |
| R016-F15 / real-MKCOL-403-before-PUT | `POST /api/backup/now` | closed | 93369229b；93369229b为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 28e6e0c12 | 400 / 400 |
| R016-F16 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-S01 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-S02 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R016-S03 /  | `POST /api/backup/now` | open | — | 28e6e0c12 | 200 / 200 |
| R017-F08 / array | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F09 / empty | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F10 / unknown | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F11 / text-type | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F12 / auto-type | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F13 / daily-large | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F13 / keep-bool | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F13 / keep-large | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F13 / keep-negative | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F14 /  | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F15 /  | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F16 /  | `POST /api/backup/test` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 400 / 400 |
| R017-F18 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 93369229b；93369229b为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 28e6e0c12 | 400 / 400 |
| R017-F19 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 93369229b；93369229b为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 28e6e0c12 | 500 / 500 |
| R055-B01 / disabled/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R055-S02 / new/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-B01 / name-collision/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-B02 / before-MATCH/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S01 / direct/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S01 / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | open | — | 28e6e0c12 | 200 / 200 |
| R056-S02 / proxy/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S03 / both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S03 / empty-means-both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S04 / repeat/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R056-S04 / repeat/1 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 28e6e0c12 | 200 / 200 |
| R058-B03 / stored-legacy-extension | `PUT /api/domain-discovery/settings` | accepted | [behaviour-changes.md · R058-B03](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 200 / 200 |
| R075-F08 / real-stat-EACCES-with-database-still-readable | `GET /api/logs/info` | open | — | 28e6e0c12 | 500 / 200 |
| R087-B04 / text/B04 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 422 / 422 |
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 200 / 502 |
| R087-F13 / text/F13 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 422 / 422 |
| R087-F16 / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 500 / 400 |
| R087-S90 / declared-no-cache-download-failure | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 502 / 502 |
| R087-S91 /  | `GET /api/mosdns/rule-proxy` | open | — | 28e6e0c12 | 200 / 200 |
| R090-F10 / SQLite-trigger-rejects-real-editor-commit | `POST /api/mosdns/settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| R103-F08 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R103-F09 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R103-F10 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R103-F11 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R103-F14 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 400 |
| R107-B01 /  | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B02 / empty-list | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B03 / draft-only | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B04 / P3/native-draft-selection | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B05 / P4/native-draft-selection | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B06 / P5/native-draft-selection | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-B07 / P6/native-draft-selection | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-F11 / native-structure | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-S01 / cache | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-S02 / upstream | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-S03 / late-routing/existing-action-before-selector-conflict | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-S03 / routing-clear | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R107-S04 / adguard | `POST /api/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-B01 /  | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-B03 / P3/native-draft-selection | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-B04 / P4/native-draft-selection | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-B05 / P5/native-draft-selection | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-B06 / P6/native-draft-selection | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / common-applies-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / common-primitive-guards-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / common-reads-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / editor-conversion | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / graph-guard/actual-URL-rule | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / graph-guard/custom-match-before-actual-rename | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / graph-guard/no-assets | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / native-apply-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / native-dependency-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / native-lifecycle/live-download | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / native-lifecycle/no-assets | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / native-lifecycle/real-Hosts-source | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / new-native-issue/real-pinned-upstream | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / retained-Hosts-content-has-an-independent-consumer | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / retained-empty-rule-URL/no-selected-rules | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / routing-late-guards-two-adjacent-rules | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / structure-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / template-preview-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R108-S01 / template-resource-precondition | `POST /api/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-B04 / P3/native-draft-selection | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-B05 / P4/native-draft-selection | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-B06 / P5/native-draft-selection | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-B07 / P6/native-draft-selection | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-S02 /  | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-S02 / graph-guard/actual-recorded-managed-rename | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-S03 /  | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R109-S04 /  | `POST /api/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R112-S02 / new-native-issue/real-save-with-retained-invalid-arguments | `POST /api/oxidns/settings` | open | — | 28e6e0c12 | 200 / 200 |
| R113-B01 /  | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-B02 /  | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-B03 / P3/native-draft-selection | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-B04 / P4/native-draft-selection | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-B05 / P5/native-draft-selection | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-B06 / P6/native-draft-selection | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-F11 / native-structure | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-S01 /  | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-S02 /  | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R113-S03 /  | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R122-S05 / failed-source-valid-cache | `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider` | closed | a05dd1270；a05dd1270为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 28e6e0c12 | 200 / 200 |
| R138-B04 / text/B04 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 422 / 422 |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 28e6e0c12 | 200 / 502 |
| R138-F13 / text/F13 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 422 / 422 |
| R138-F16 / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 28e6e0c12 | 500 / 400 |
| R138-S90 / declared-no-cache-download-failure | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 28e6e0c12 | 502 / 502 |
| R138-S91 /  | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 28e6e0c12 | 200 / 200 |
| R140-B01 /  | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-B02 / empty-list | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-B03 / draft-only | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-B04 / P3/native-draft-selection | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-F11 / native-structure | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-S01 / cache | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-S02 / upstream | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-S03 / late-routing/existing-action-before-selector-conflict | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-S03 / routing-clear | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R140-S04 / adguard | `POST /api/profiles/<profile_id>/oxidns/common-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-B01 /  | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-B03 / P3/native-draft-selection | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / common-applies-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / common-primitive-guards-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / common-reads-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / editor-conversion | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / graph-guard/actual-URL-rule | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / graph-guard/custom-match-before-actual-rename | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / graph-guard/no-assets | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / native-apply-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / native-dependency-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / native-lifecycle/live-download | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / native-lifecycle/no-assets | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / native-lifecycle/real-Hosts-source | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / retained-Hosts-content-has-an-independent-consumer | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / retained-empty-rule-URL/no-selected-rules | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / routing-late-guards-two-adjacent-rules | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / structure-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / template-preview-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R141-S01 / template-resource-precondition | `POST /api/profiles/<profile_id>/oxidns/editor` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R142-B04 / P3/native-draft-selection | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R142-S02 /  | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R142-S02 / graph-guard/actual-recorded-managed-rename | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R142-S03 /  | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R142-S04 /  | `POST /api/profiles/<profile_id>/oxidns/native-settings/apply` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-B01 /  | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-B02 /  | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-B03 / P3/native-draft-selection | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-F11 / native-structure | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-S01 /  | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-S02 /  | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R146-S03 /  | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R151-S01 /  | `GET /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R151-S02 /  | `GET /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-B01 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-B02 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-B03 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F08 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F09 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F10 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F11 / DOMAIN | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F11 / IP-CIDR | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F12 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F13 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F14 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-F15 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R152-S01 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-S02 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-S03 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R152-S04 /  | `POST /api/profiles/<profile_id>/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R154-B03 / ?format=unknown | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-B04 / URL-over-query-header | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-B90 / Asia/Shanghai | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-B90 / UTC | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-B91 / HEAD/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F01 / open/provider-D1 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F02 / open/provider-D2 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F03 / open/provider-D3 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F06 / open/provider-D6 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F07 / open/provider-D7 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F08 / open/named-provider-authorization-guard | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-F17 / last-good-provider-output-path-is-directory | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| R154-S01 / GET/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-S01 / default | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-S01 / login/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-S01 / open/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-S01 / token/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R154-S06 /  | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R179-B01 /  | `POST /api/rule-sets` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R179-F08 /  | `POST /api/rule-sets` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| R179-F09 /  | `POST /api/rule-sets` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R179-F10 /  | `POST /api/rule-sets` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R179-F11 /  | `POST /api/rule-sets` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R179-S01 /  | `POST /api/rule-sets` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R180-B01 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 404 / 404 |
| R180-B02 / P3/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R180-B03 / P4/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R180-B04 / P5/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R180-B05 / P6/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R180-F08 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 404 / 404 |
| R180-F09 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 404 / 404 |
| R180-S01 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R180-S01 / mosdns-selector | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R181-B01 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R181-F08 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R181-F09 /  | `PUT /api/rule-sets/<rule_set_id>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| R181-F10 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R181-S01 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R182-S01 /  | `GET /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R182-S02 /  | `GET /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-B01 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-B02 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-B03 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F08 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F09 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F10 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F11 / DOMAIN | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F11 / IP-CIDR | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F12 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F13 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F14 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-F15 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R183-S01 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-S02 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-S03 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R183-S04 /  | `POST /api/rules` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R184-B01 /  | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-B02 / P3/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-B03 / P4/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-B04 / P5/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-B05 / P6/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-F08 /  | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 404 / 404 |
| R184-S01 /  | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-S01 / dns-cleanup/False | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-S01 / mosdns-selector | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R184-S02 / dns-cleanup/True | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| R185-B01 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-B02 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-B03 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-F08 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F09 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F10 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F11 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F12 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F13 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F14 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F15 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-F16 /  | `PUT /api/rules/<rule_id>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| R185-F18 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R185-S01 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-S02 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-S03 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-S03 / clear-route-with-type | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R185-S04 /  | `PUT /api/rules/<rule_id>` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R186-B01 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R186-B02 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R186-B03 / concurrent-allocation | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | None / None |
| R186-F08 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-F09 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-F10 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-F11 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-F12 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-F13 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R186-S01 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R186-S02 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R186-S03 /  | `POST /api/rules/batch` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-B01 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-B02 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-B03 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-B04 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-B05 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S01 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S02 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S03 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S04 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S05 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R187-S06 /  | `POST /api/rules/compose` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R188-S01 / real-DNS-follow-binding-catalog | `GET /api/rules/dns` | open | — | 28e6e0c12 | 200 / 200 |
| R189-B01 /  | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-B02 / native | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-B02 / template | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-F08 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F08 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F09 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F09 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F10 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F10 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F11 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F11 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F12 / native/'unknown' | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F12 / native/1 | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F12 / template/'unknown' | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F12 / template/1 | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F13 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F13 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-F15 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 400 / 400 |
| R189-F15 / template/ | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R189-S01 / cleanup-prepare/False | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / cleanup-prepare/True | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/blackhole | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/direct | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/nodata | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/nxdomain | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/proxy | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / native/refused | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/blackhole | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/direct | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/nodata | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/nxdomain | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/proxy | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / template/refused | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S01 / type-guard-prepare | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S02 / native/follow | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S02 / template/follow | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S03 / native/default | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S03 / template/default | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R189-S04 / native | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R189-S04 / template | `PUT /api/rules/dns` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R191-B01 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R191-B02 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R191-B03 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R191-F08 / [] | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R191-F08 / {} | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R191-F09 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R191-F10 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R191-F11 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R191-F12 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 404 / 404 |
| R191-S01 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R191-S02 /  | `POST /api/rules/group-name` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-B01 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-B02 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-B03 / explicit-DNS-does-not-follow-policy-edit | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-F08 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F09 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F10 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F11 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F12 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F13 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F14 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R194-F15 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 404 / 404 |
| R194-S01 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-S02 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-S03 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R194-S04 /  | `PUT /api/rules/policy` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R195-B01 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R195-B02 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R195-B03 / real-recognized-native-provider-order-rebuild | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| R195-F08 / [] | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R195-F08 / {} | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R195-F09 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R195-F10 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R195-F11 /  | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| R195-F12 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 400 / 400 |
| R195-F13 / {'ids': [1]} | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| R195-F13 / {'ids': [[1]]} | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| R195-S01 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R195-S02 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R195-S03 /  | `POST /api/rules/reorder` | closed | 838f7739f；838f7739f为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 28e6e0c12 | 200 / 200 |
| R217-S02 /  | `PUT /api/subscriptions/<sub_id>` | closed | e9187b9f | 28e6e0c12 | 200 / 200 |
| R218-F09 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | 28e6e0c12 | 500 / 500 |
| R218-F10 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 400 / 400 |
| R218-F11 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 400 |
| R218-F12 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 8f3d36ad | 28e6e0c12 | 500 / 500 |
| R218-F13 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 3ab22d36 | 28e6e0c12 | 500 / 500 |
| R218-S03 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | 28e6e0c12 | 200 / 200 |
| R220-B03 / ?format=unknown | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-B90 / Asia/Shanghai | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-B90 / UTC | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-B91 / HEAD/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F01 / open/provider-D1 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F02 / open/provider-D2 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F03 / open/provider-D3 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F06 / open/provider-D6 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F07 / open/provider-D7 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F08 / open/named-provider-authorization-guard | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-F17 / last-good-provider-output-path-is-directory | `GET /api/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| R220-S01 / GET/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-S01 / default | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-S01 / login/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-S01 / open/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-S01 / token/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R220-S06 /  | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-B02 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-B90 / Asia/Shanghai | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-B90 / UTC | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-F11 / invalid-source-no-fallback | `GET /api/subscriptions/proxies` | closed | cb0cc9fa | 28e6e0c12 | 400 / 400 |
| R222-S01 / different-definitions-no-chain | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-S01 / identical-crossfeed | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-S01 / two | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-S02 / disabled | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-S03 / http403-cache-fallback | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R222-S04 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| R223-F08 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R223-F09 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R223-F10 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 400 |
| R223-F12 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 400 |
| R228-F01 /  | `GET /api/version` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 405 / 405 |
| T06 / 12-second-last-good-and-background-finish | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| T06 / 20-clients-one-flight | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] / [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] |
| T06 / next-poll-is-fresh | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 28e6e0c12 | 200 / 200 |
| T25 / rules/operation-5 | `DELETE /api/rules/<rule_id>` | open | — | 28e6e0c12 | 200 / 200 |
| T27 / real-tick/due-and-backoff | `startup` | closed | 8f3d36ad | 28e6e0c12 | None / None |
| X01 / /api/oxidns/render | `POST /api/oxidns/render` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| X01 / /api/oxidns/template-preview | `POST /api/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| X01 / /api/profiles/<profile_id>/oxidns/render | `POST /api/profiles/<profile_id>/oxidns/render` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| X01 / /api/profiles/<profile_id>/oxidns/template-preview | `POST /api/profiles/<profile_id>/oxidns/template-preview` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 28e6e0c12 | 200 / 200 |
| X04 / login/empty | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 400 |
| X04 / login/ignored-extra/-Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 200 / 400 |
| X04 / login/ignored-extra/Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 200 / 400 |
| X04 / login/ignored-extra/NaN | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 200 / 400 |
| X04 / login/null | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 400 |
| X04 / login/plain | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 28e6e0c12 | 500 / 415 |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 400 |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 400 |
| X06 / encoded-slash/GET /api/profiles/<profile_id>/rules/local/<name> | `GET /api/profiles/<profile_id>/rules/local/<name>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / encoded-slash/GET /api/rules/local/<name> | `GET /api/rules/local/<name>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / head//api/aggregations | `GET /api/aggregations` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| X06 / head//api/mosdns/api-settings | `GET /api/mosdns/api-settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| X06 / head//api/nodes | `GET /api/nodes` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| X06 / head//api/oxidns/settings | `GET /api/oxidns/settings` | accepted | [behaviour-changes.md · C29 HEAD只读](behaviour-changes.md#head-只读c29--c31--c32) | 28e6e0c12 | 400 / 200 |
| X06 / head//api/rule-sets | `GET /api/rule-sets` | accepted | [behaviour-changes.md · C32](behaviour-changes.md#c32--head-规则接口只读python-bug-修正) | 28e6e0c12 | 500 / 200 |
| X06 / head//api/rules | `GET /api/rules` | accepted | [behaviour-changes.md · C32](behaviour-changes.md#c32--head-规则接口只读python-bug-修正) | 28e6e0c12 | 500 / 200 |
| X06 / head//api/subscriptions | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 500 / 500 |
| X06 / login/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / login/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / open/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / open/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X06 / open/unknown-anonymous | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 28e6e0c12 | 404 / 404 |
| X08 / retired-node-test-dns-migration | `startup` | separate | — | 28e6e0c12 | None / None |

## R180-B01 /

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |
| `db.fields.profile/default/mosdns.revision` | value | 1 | 9 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 9 |

## R180-B02 / P3/successful-rule-group-write

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R180-B03 / P4/successful-rule-group-write

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R180-B04 / P5/successful-rule-group-write

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/other/mosdns.revision` | value | 3 | 6 |
| `db.fields.profile/other/oxidns.revision` | value | 3 | 6 |

## R180-B05 / P6/successful-rule-group-write

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R180-F08 /

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |
| `db.fields.profile/default/mosdns.revision` | value | 1 | 9 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 9 |

## R180-F09 /

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |
| `db.fields.profile/default/mosdns.revision` | value | 1 | 9 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 9 |

## R180-S01 /

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 9 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 9 |

## R180-S01 / mosdns-selector

路由：`DELETE /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.revision` | value | 1 | 5 |

## R184-B01 /

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 5 |
| `db.fields.profile/default/oxidns.revision` | value | 2 | 5 |

## R184-B02 / P3/successful-rule-group-write

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R184-B03 / P4/successful-rule-group-write

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R184-B04 / P5/successful-rule-group-write

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/other/mosdns.revision` | value | 3 | 6 |
| `db.fields.profile/other/oxidns.revision` | value | 3 | 6 |

## R184-B05 / P6/successful-rule-group-write

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 6 |

## R184-F08 /

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |
| `db.fields.profile/default/mosdns.revision` | value | 1 | 11 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 11 |

## R184-S01 /

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 11 |
| `db.fields.profile/default/oxidns.revision` | value | 1 | 11 |

## R184-S01 / dns-cleanup/False

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 6 |

## R184-S01 / mosdns-selector

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.revision` | value | 1 | 5 |

## R184-S02 / dns-cleanup/True

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 7 |
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## T25 / rules/operation-5

路由：`DELETE /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/mosdns.revision` | value | 1 | 10 |

## X06 / head//api/aggregations

路由：`GET /api/aggregations`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## R012-F01 /

路由：`GET /api/auth/status`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>The method is not allowed for the requested URL.</p>'] | {'message': 'The method is not allowed for the requested URL.', 'success': False} |

## R075-F08 / real-stat-EACCES-with-database-still-readable

路由：`GET /api/logs/info`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `log.stat-permission.assigned_path` | value | True | False |
| `log.stat-permission.permission_error` | value | True | False |
| `status` | value | 500 | 200 |
| `body.error` | missing-in-go | "[Errno 13] Permission denied: '/tmp/configdiff-5q7oa1jy/denied-log/synthetic.log'" | None |
| `body.exists` | extra-in-go | None | False |
| `body.path` | extra-in-go | None | '/tmp/configdiff-5q7oa1jy/denied-log/synthetic.log' |
| `body.success` | value | False | True |

## X06 / head//api/mosdns/api-settings

路由：`GET /api/mosdns/api-settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.internal-redirect-boundary.destination_requests` | length | 0 | 1 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 1 | 2 |
| `rule.internal-redirect-boundary.source_paths` | length | 1 | 2 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H123 / original-and-mirror-both-fail-through-loopback-proxy

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests[0].user_agent` | value | '' | 'Go-http-client/1.1' |

## R087-B91 / absolute-query-dispatches-internal-content-endpoint

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 200 | 502 |
| `headers.content-type` | value | 'text/plain; charset=utf-8' | 'application/json' |
| `body` | type | ['full:inline.example'] | {'code': 'RULE_SOURCE_UNAVAILABLE', 'message': '规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。', 'success': False} |

## R087-F16 / receipt-path-is-directory

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | value | 'Failed to process rule proxy request' | 'Invalid remote URL' |

## R087-S91 /

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests[0].host` | value | '' | 'raw.githubusercontent.com:443' |
| `subscription.source.requests[0].request_line` | value | 'CONNECT raw.githubusercontent.com:443 HTTP/1.0' | 'CONNECT raw.githubusercontent.com:443 HTTP/1.1' |
| `subscription.source.requests[0].user_agent` | value | '' | 'Go-http-client/1.1' |

## X06 / head//api/nodes

路由：`GET /api/nodes`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## X06 / head//api/oxidns/settings

路由：`GET /api/oxidns/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 400 | 200 |
| `headers.etag` | value | '' | '"734b369f86f81bd42f5ed9008d5d318dfa5fadfcaee8e7615405a213eb36455b"' |

## H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.internal-redirect-boundary.destination_requests` | length | 0 | 1 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 1 | 2 |
| `rule.internal-redirect-boundary.source_paths` | length | 1 | 2 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## R138-B91 / absolute-query-dispatches-internal-content-endpoint

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 200 | 502 |
| `headers.content-type` | value | 'text/plain; charset=utf-8' | 'application/json' |
| `body` | type | ['full:inline.example'] | {'code': 'RULE_SOURCE_UNAVAILABLE', 'message': '规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。', 'success': False} |

## R138-F16 / receipt-path-is-directory

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | value | 'Failed to process rule proxy request' | 'Invalid remote URL' |

## R138-S91 /

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests[0].host` | value | '' | 'raw.githubusercontent.com:443' |
| `subscription.source.requests[0].request_line` | value | 'CONNECT raw.githubusercontent.com:443 HTTP/1.0' | 'CONNECT raw.githubusercontent.com:443 HTTP/1.1' |
| `subscription.source.requests[0].user_agent` | value | '' | 'Go-http-client/1.1' |

## H128 / sequence-root

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / duplicate-payload

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / mapping-payload

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / missing-payload

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H130 / alias

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H130 / anchor

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / block-entry

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / nested-entry

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / non-string-entry

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H132 / duplicate-extra-flow-key

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H133 / multiline-extra-flow-value

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |
| `body.original-bytes` | value | {'bytes_length': 51, 'bytes_sha256': '0553098a22d5ebc8812f786e0728cd7534123cf3b2fbc4fdd19609e34d4389b7', 'bytes_prefix_hex': '7b7061796c6f61643a205b2b2e6578616d706c652e636f6d5d2c2065787472613a202266697273740a207365636f6e64227d0a'} | {'bytes_length': 49, 'bytes_sha256': '08a84b1b6d47972542fa244d1f321473cf7c199ae4b0058dae4834b46666b00f', 'bytes_prefix_hex': '7061796c6f61643a0a20202d202b2e6578616d706c652e636f6d0a65787472613a20226669727374207365636f6e64220a'} |

## X06 / encoded-slash/GET /api/profiles/<profile_id>/rules/local/<name>

路由：`GET /api/profiles/<profile_id>/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

## R154-F17 / last-good-provider-output-path-is-directory

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## X06 / head//api/rule-sets

路由：`GET /api/rule-sets`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 200 |
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## X06 / head//api/rules

路由：`GET /api/rules`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 200 |
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## R188-S01 / real-DNS-follow-binding-catalog

路由：`GET /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.system//_revision.revision` | value | 5 | 4 |
| `db.fields.system//_revision.value` | value | 3 | 2 |
| `db.fields.system//agents.revision` | value | 5 | 4 |
| `db.fields.system//agents.value[0].dns_follow_state` | missing-in-go | {'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'updated_at': '2026-11-17T22:07:38.548104Z', 'events': [{'at': '2026-11-17T22:07:38.548045Z', 'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试'}]} | None |
| `db.meta.generation` | value | 5 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:dns_follow:agent-synthetic': {'source': 'task', 'title': '合成 Agent 🧪 · DNS 联动', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': 'default', 'scope': 'profile', 'profile_name': '默认配置', 'target': {'path': '/agents', 'query': {'agent': 'agent-synthetic', 'profile': 'default', 'section': 'dns-follow'}, 'label': '查看同步设置'}, 'checked_at': '2026-11-17T22:07:38Z', 'failure_count': 1, 'id': 'task:dns_follow:agent-synthetic', 'state': 'pending', 'read': False, 'version': '2026-11-17T22:07:38.752416+00:00', 'first_seen_at': '2026-11-17T22:07: | None |
| `files.task-status.json` | missing-in-go | {'mode': 384, 'content': {'dns_follow:agent-synthetic': {'last_run_at': '2026-11-17T22:07:38Z', 'last_finished_at': '2026-11-17T22:07:38Z', 'last_status': 'failed', 'last_duration_ms': 0, 'last_trigger': 'event', 'last_error': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'run_count': 1, 'failure_count': 1, 'consecutive_failures': 1}}} | None |

## H128 / sequence-root

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / duplicate-payload

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / mapping-payload

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H129 / missing-payload

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H130 / alias

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H130 / anchor

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / block-entry

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / nested-entry

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H131 / non-string-entry

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H132 / duplicate-extra-flow-key

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |

## H133 / multiline-extra-flow-value

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.delivery-warning` | length | 1 | 0 |
| `body.original-bytes` | value | {'bytes_length': 51, 'bytes_sha256': '0553098a22d5ebc8812f786e0728cd7534123cf3b2fbc4fdd19609e34d4389b7', 'bytes_prefix_hex': '7b7061796c6f61643a205b2b2e6578616d706c652e636f6d5d2c2065787472613a202266697273740a207365636f6e64227d0a'} | {'bytes_length': 49, 'bytes_sha256': '08a84b1b6d47972542fa244d1f321473cf7c199ae4b0058dae4834b46666b00f', 'bytes_prefix_hex': '7061796c6f61643a0a20202d202b2e6578616d706c652e636f6d0a65787472613a20226669727374207365636f6e64220a'} |

## X06 / encoded-slash/GET /api/rules/local/<name>

路由：`GET /api/rules/local/<name>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## X06 / head//api/subscriptions

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

## X06 / login/trailing-slash

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## X06 / login/unknown-admin

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## X06 / open/trailing-slash

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## X06 / open/unknown-admin

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## X06 / open/unknown-anonymous

路由：`GET /api/subscriptions`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.</p>'] | {'message': 'The requested URL was not found on the server. If you entered the URL manually please check your spelling and try again.', 'success': False} |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

## R220-F17 / last-good-provider-output-path-is-directory

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

## R228-F01 /

路由：`GET /api/version`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>The method is not allowed for the requested URL.</p>'] | {'message': 'The method is not allowed for the requested URL.', 'success': False} |

## R011-F06 /

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | internal-error-prefix | "登录失败。原始错误：'list' object has no attribute 'get'" | 'The browser (or proxy) sent a request that this server could not understand.' |

## R011-F07 /

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | internal-error-prefix | '登录失败。原始错误：400 Bad Request: The browser (or proxy) sent a request that this server could not understand.' | 'The browser (or proxy) sent a request that this server could not understand.' |

## R011-F08 /

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 415 |
| `body.message` | internal-error-prefix | "登录失败。原始错误：415 Unsupported Media Type: Did not attempt to load JSON data because the request Content-Type was not 'application/json'." | "Did not attempt to load JSON data because the request Content-Type was not 'application/json'." |

## X04 / login/empty

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | internal-error-prefix | '登录失败。原始错误：400 Bad Request: The browser (or proxy) sent a request that this server could not understand.' | 'The browser (or proxy) sent a request that this server could not understand.' |

## X04 / login/ignored-extra/-Infinity

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.token` | invalid-Go-JWT | '<valid-HS256>' | '<invalid>' |
| `status` | value | 200 | 400 |
| `body.expiresIn` | missing-in-go | 86400 | None |
| `body.message` | extra-in-go | None | 'The browser (or proxy) sent a request that this server could not understand.' |
| `body.success` | value | True | False |
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzg0OTgsImlhdCI6MTc5NDk1MjA5OH0.AMYujHB1znGGadahnPxNDXaZMM5YKVZZSmcNrDCPJ2Q' | None |
| `body.username` | missing-in-go | 'diff-admin' | None |

## X04 / login/ignored-extra/Infinity

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.token` | invalid-Go-JWT | '<valid-HS256>' | '<invalid>' |
| `status` | value | 200 | 400 |
| `body.expiresIn` | missing-in-go | 86400 | None |
| `body.message` | extra-in-go | None | 'The browser (or proxy) sent a request that this server could not understand.' |
| `body.success` | value | True | False |
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzg0OTgsImlhdCI6MTc5NDk1MjA5OH0.AMYujHB1znGGadahnPxNDXaZMM5YKVZZSmcNrDCPJ2Q' | None |
| `body.username` | missing-in-go | 'diff-admin' | None |

## X04 / login/ignored-extra/NaN

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.token` | invalid-Go-JWT | '<valid-HS256>' | '<invalid>' |
| `status` | value | 200 | 400 |
| `body.expiresIn` | missing-in-go | 86400 | None |
| `body.message` | extra-in-go | None | 'The browser (or proxy) sent a request that this server could not understand.' |
| `body.success` | value | True | False |
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzg0OTgsImlhdCI6MTc5NDk1MjA5OH0.AMYujHB1znGGadahnPxNDXaZMM5YKVZZSmcNrDCPJ2Q' | None |
| `body.username` | missing-in-go | 'diff-admin' | None |

## X04 / login/null

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | internal-error-prefix | "登录失败。原始错误：'NoneType' object has no attribute 'get'" | 'The browser (or proxy) sent a request that this server could not understand.' |

## X04 / login/plain

路由：`POST /api/auth/login`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 415 |
| `body.message` | internal-error-prefix | "登录失败。原始错误：415 Unsupported Media Type: Did not attempt to load JSON data because the request Content-Type was not 'application/json'." | "Did not attempt to load JSON data because the request Content-Type was not 'application/json'." |

## R015-S04 /

路由：`POST /api/backup/config`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.system//backup.revision` | value | 8 | 6 |
| `db.fields.system//backup.value.webdav_password` | value | '' | 'synthetic-pass' |

## R016-B02 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B03 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[10].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B90 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[10].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B91 / Asia/Shanghai/previous-local-day-crosses-UTC-midnight

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B91 / Etc/GMT+12/previous-local-day-crosses-UTC-midnight

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B91 / Etc/GMT-14/previous-local-day-crosses-UTC-midnight

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-F16 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S01 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S02 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S03 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## X04 / token/literal/"\ud800"

路由：`POST /api/config-token`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | non-ascii-internal-error-text | "'utf-8' codec can't encode character '\\ud800' in position 238: surrogates not allowed" | '请求内容包含无效的 Unicode 字符' |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.shared/rules/by-id/57dd3a99c8f85e438e5b63f912f9efaa2a1b944f33845873aee9364fcd0d832f.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R090-F10 / SQLite-trigger-rejects-real-editor-commit

路由：`POST /api/mosdns/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## R103-F08 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下节点 id 不存在: ['ids 必须是数组']" | 'ids 必须是数组' |

## R103-F09 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下节点 id 不存在: ['id 必须是互不重复的字符串']" | 'id 必须是互不重复的字符串' |

## R103-F10 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下节点 id 不存在: ['id 必须是互不重复的字符串']" | 'id 必须是互不重复的字符串' |

## R103-F11 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下节点 id 不存在: ['nodes 必须是对象数组']" | 'nodes 必须是对象数组' |

## R103-F14 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | non-ascii-internal-error-text | "'list' object has no attribute 'get'" | '请求体必须是 JSON 对象' |

## R107-B01 /

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R107-B02 / empty-list

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R107-B03 / draft-only

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R107-B04 / P3/native-draft-selection

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R107-B05 / P4/native-draft-selection

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R107-B06 / P5/native-draft-selection

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R107-B07 / P6/native-draft-selection

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R107-F11 / native-structure

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'plugins:\n- tag: dup\n  type: sequence\n  args: []\n- tag: dup\n  type: sequence\n  args: []\nruntime:\n  worker_threads: 3\n' | 'plugins:\n  - {tag: dup, type: sequence, args: []}\n  - {tag: dup, type: sequence, args: []}\nruntime:\n  worker_threads: 3\n' |

## R107-S01 / cache

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |

## R107-S02 / upstream

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://192.0.2.53\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns- | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://192.0.2.53\n  - tag: forward_remote\n    type: forward\n    args:\n      concurr |

## R107-S03 / late-routing/existing-action-before-selector-conflict

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1_2\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: rules_domain_2\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1_2\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: rules_domain_2\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurren |

## R107-S03 / routing-clear

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R107-S04 / adguard

路由：`POST /api/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## G12 / False/prepare-template

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## R108-B01 /

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-B03 / P3/native-draft-selection

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-B04 / P4/native-draft-selection

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-B05 / P5/native-draft-selection

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-B06 / P6/native-draft-selection

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / common-applies-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / common-primitive-guards-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / common-reads-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / editor-conversion

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / graph-guard/actual-URL-rule

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / graph-guard/custom-match-before-actual-rename

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / graph-guard/no-assets

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / native-apply-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / native-dependency-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: dynamic_domains\n  type: dyn | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: dynamic_domains\n  type: dyn | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / native-lifecycle/live-download

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / native-lifecycle/no-assets

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / native-lifecycle/real-Hosts-source

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## R108-S01 / new-native-issue/real-pinned-upstream

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    exps:\n    - full:example.com\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      exps:\n        - full:example.com\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n    |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    exps:\n    - full:example.com\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      exps:\n        - full:example.com\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n    |

## R108-S01 / retained-Hosts-content-has-an-independent-consumer

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## R108-S01 / retained-empty-rule-URL/no-selected-rules

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R108-S01 / routing-late-guards-two-adjacent-rules

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forwar | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: u |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forwar | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: u |

## R108-S01 / structure-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / template-preview-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R108-S01 / template-resource-precondition

路由：`POST /api/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## G12 / False/remove-standard-cache

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forwar |

## G12 / False/rename/False

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / False/rename/True

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 987\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 987\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n     |

## G12 / False/restore-standard-cache

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forwar |

## R109-B04 / P3/native-draft-selection

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'runtime:\n  worker_threads: 7\nplugins: []\n' | 'runtime: {worker_threads: 7}\nplugins: []\n' |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R109-B05 / P4/native-draft-selection

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'runtime:\n  worker_threads: 7\nplugins: []\n' | 'runtime: {worker_threads: 7}\nplugins: []\n' |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R109-B06 / P5/native-draft-selection

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'runtime:\n  worker_threads: 7\nplugins: []\n' | 'runtime: {worker_threads: 7}\nplugins: []\n' |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R109-B07 / P6/native-draft-selection

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'runtime:\n  worker_threads: 7\nplugins: []\n' | 'runtime: {worker_threads: 7}\nplugins: []\n' |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R109-S02 /

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams: | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    t |

## R109-S02 / graph-guard/actual-recorded-managed-rename

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequen | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: h |

## R109-S03 /

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: htt |

## R109-S04 /

路由：`POST /api/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: htt |

## G11 / log/off/False/draft

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.config.log.level` | type | 'off' | False |

## H470 / native-normalization/duplicate-key

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'OxiDNS YAML 包含重复字段' | 'YAML 包含重复键 "plugins"（第 2 行）' |

## H474 / native-normalization/recursive-alias

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'OxiDNS YAML 不支持循环别名' | 'YAML 包含递归引用（第 1 行）' |

## H511 / /api/oxidns/cache_dump_interval/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_dump_interval 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/cache_lazy_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_lazy_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/cache_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/dynamic_domain_set.args.batch_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'batch_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/dynamic_domain_set.args.flush_interval_ms/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'flush_interval_ms 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/dynamic_domain_set.args.queue_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'queue_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/fallback_concurrent/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'fallback_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/fallback_threshold/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'fallback_threshold 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ip_selector.args.max_parallel_probes/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_parallel_probes 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ip_selector.args.max_wait/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_wait 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ip_selector.args.probe_stagger/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_stagger 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ip_selector.args.probe_timeout/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_timeout 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ip_selector.args.top_n/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'top_n 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/local_concurrent/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'local_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/log.max_files/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_files 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/probe_cache_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_cache_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.batch_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'batch_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.cleanup_interval_hours/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cleanup_interval_hours 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.flush_interval_ms/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'flush_interval_ms 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.memory_tail/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'memory_tail 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.queue_size/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'queue_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.reader_concurrency/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'reader_concurrency 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/query_recorder.args.retention_days/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'retention_days 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/remote_concurrent/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'remote_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/remote_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'remote_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ros_address_list.args.max_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ros_address_list.args.min_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'min_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ros_route.args.max_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/ros_route.args.min_ttl/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'min_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/oxidns/worker_threads/nan

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'worker_threads 必须为 1–256 的整数' | '请提供 settings 配置对象' |

## X01 / /api/oxidns/render

路由：`POST /api/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R112-S02 / new-native-issue/real-save-with-retained-invalid-arguments

路由：`POST /api/oxidns/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/updated_at.value` | value | '2026-11-17T22:05:02.670058Z' | '2026-11-17T22:05:02.680903Z' |

## G12 / False/conflict-current

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / False/conflict-template

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / False/merge-independent-fields

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 321\n    lazy_cache_ttl: 600\n    short_circuit: true\n    experimental:\n      n: 0\n      enabled: false\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent:  | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 321\n      lazy_cache_ttl: 600\n      short_circuit: true\n      experimental:\n        n: 0\n        enabled: false\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n       |

## G12 / False/no-op-comments

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / False/resolved-baseline-is-idempotent

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## R113-B01 /

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |

## R113-B02 /

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R113-B03 / P3/native-draft-selection

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R113-B04 / P4/native-draft-selection

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R113-B05 / P5/native-draft-selection

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R113-B06 / P6/native-draft-selection

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R113-F11 / native-structure

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R113-S01 /

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R113-S02 /

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R113-S03 /

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## X01 / /api/oxidns/template-preview

路由：`POST /api/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R140-B01 /

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R140-B02 / empty-list

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R140-B03 / draft-only

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R140-B04 / P3/native-draft-selection

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R140-F11 / native-structure

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'plugins:\n- tag: dup\n  type: sequence\n  args: []\n- tag: dup\n  type: sequence\n  args: []\nruntime:\n  worker_threads: 3\n' | 'plugins:\n  - {tag: dup, type: sequence, args: []}\n  - {tag: dup, type: sequence, args: []}\nruntime:\n  worker_threads: 3\n' |

## R140-S01 / cache

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |

## R140-S02 / upstream

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://192.0.2.53\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns- | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://192.0.2.53\n  - tag: forward_remote\n    type: forward\n    args:\n      concurr |

## R140-S03 / late-routing/existing-action-before-selector-conflict

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1_2\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: rules_domain_2\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1_2\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: rules_domain_2\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurren |

## R140-S03 / routing-clear

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R140-S04 / adguard

路由：`POST /api/profiles/<profile_id>/oxidns/common-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## G12 / True/prepare-template

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## R141-B01 /

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-B03 / P3/native-draft-selection

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / common-applies-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / common-primitive-guards-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / common-reads-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / editor-conversion

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / graph-guard/actual-URL-rule

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / graph-guard/custom-match-before-actual-rename

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / graph-guard/no-assets

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / native-apply-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / native-dependency-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: dynamic_domains\n  type: dyn | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: dynamic_domains\n  type: dyn | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / native-lifecycle/live-download

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / native-lifecycle/no-assets

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / native-lifecycle/real-Hosts-source

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## R141-S01 / retained-Hosts-content-has-an-independent-consumer

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## R141-S01 / retained-empty-rule-URL/no-selected-rules

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R141-S01 / routing-late-guards-two-adjacent-rules

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forwar | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: u |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n    - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forwar | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n        - ./rules/rule_bb14ac9bdd0b3225_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: u |

## R141-S01 / structure-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / template-preview-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R141-S01 / template-resource-precondition

路由：`POST /api/profiles/<profile_id>/oxidns/editor`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: custom_hosts\n  type: hosts\n  args:\n    files:\n    - ./rules/custom_hosts.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1. | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: custom_hosts\n    type: hosts\n    args:\n      files:\n        - ./rules/custom_hosts.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n  |

## G12 / True/remove-standard-cache

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forwar |

## G12 / True/rename/False

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / True/rename/True

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 987\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 987\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n     |

## G12 / True/restore-standard-cache

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forwar |

## R142-B04 / P3/native-draft-selection

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | 'runtime:\n  worker_threads: 7\nplugins: []\n' | 'runtime: {worker_threads: 7}\nplugins: []\n' |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R142-S02 /

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams: | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    t |

## R142-S02 / graph-guard/actual-recorded-managed-rename

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: renamed_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequen | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: renamed_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: h |

## R142-S03 /

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: htt |

## R142-S04 /

路由：`POST /api/profiles/<profile_id>/oxidns/native-settings/apply`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: htt |

## G11 / log/off/True/draft

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.config.log.level` | type | 'off' | False |

## H470 / native-normalization/duplicate-key

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'OxiDNS YAML 包含重复字段' | 'YAML 包含重复键 "plugins"（第 2 行）' |

## H474 / native-normalization/recursive-alias

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'OxiDNS YAML 不支持循环别名' | 'YAML 包含递归引用（第 1 行）' |

## H511 / /api/profiles/<profile_id>/oxidns/cache_dump_interval/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_dump_interval 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/cache_lazy_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_lazy_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/cache_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cache_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.batch_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'batch_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.flush_interval_ms/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'flush_interval_ms 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/dynamic_domain_set.args.queue_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'queue_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/fallback_concurrent/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'fallback_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/fallback_threshold/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'fallback_threshold 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.max_parallel_probes/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_parallel_probes 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.max_wait/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_wait 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.probe_stagger/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_stagger 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.probe_timeout/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_timeout 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ip_selector.args.top_n/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'top_n 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/local_concurrent/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'local_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/log.max_files/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_files 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/probe_cache_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'probe_cache_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.batch_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'batch_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.cleanup_interval_hours/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'cleanup_interval_hours 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.flush_interval_ms/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'flush_interval_ms 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.memory_tail/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'memory_tail 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.queue_size/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'queue_size 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.reader_concurrency/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'reader_concurrency 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/query_recorder.args.retention_days/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'retention_days 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/remote_concurrent/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'remote_concurrent 必须为 1–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/remote_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'remote_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ros_address_list.args.max_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ros_address_list.args.min_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'min_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ros_route.args.max_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'max_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/ros_route.args.min_ttl/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'min_ttl 必须为 0–2147483647 的整数' | '请提供 settings 配置对象' |

## H511 / /api/profiles/<profile_id>/oxidns/worker_threads/nan

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | 'worker_threads 必须为 1–256 的整数' | '请提供 settings 配置对象' |

## X01 / /api/profiles/<profile_id>/oxidns/render

路由：`POST /api/profiles/<profile_id>/oxidns/render`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## G12 / True/conflict-current

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / True/conflict-template

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / True/merge-independent-fields

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 321\n    lazy_cache_ttl: 600\n    short_circuit: true\n    experimental:\n      n: 0\n      enabled: false\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent:  | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 321\n      lazy_cache_ttl: 600\n      short_circuit: true\n      experimental:\n        n: 0\n        enabled: false\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    | "runtime:\n  worker_threads: 9\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n       |

## G12 / True/no-op-comments

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## G12 / True/resolved-baseline-is-idempotent

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_1fd327ea9ee548a9_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n   | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_1fd327ea9ee548a9_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      |

## R146-B01 /

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 300\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 300\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type:  |

## R146-B02 /

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R146-B03 / P3/native-draft-selection

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R146-F11 / native-structure

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R146-S01 /

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R146-S02 /

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## R146-S03 /

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.native_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1\n  type: domain_set\n  args:\n    files:\n    - ./rules/rule_19bb23c03f0cc731_domain.txt\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n  | "runtime:\n  worker_threads: 7\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1\n    type: domain_set\n    args:\n      files:\n        - ./rules/rule_19bb23c03f0cc731_domain.txt\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type |

## X01 / /api/profiles/<profile_id>/oxidns/template-preview

路由：`POST /api/profiles/<profile_id>/oxidns/template-preview`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.settings.template_base_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n    concurrent: 1\n    upstreams:\n    - addr: https://1.1.1.1/dns-query\n    - addr: https://8.8.8.8/dns-query\n- tag: china_dns\n  type: sequence\ | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag: forward_remote\n    type: forward\n    args:\n      concurrent: 1\n      upstreams:\n        - addr: https://1.1.1.1/dns-query\n        - addr: http |

## R179-F08 /

路由：`POST /api/rule-sets`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## R195-B03 / real-recognized-native-provider-order-rebuild

路由：`POST /api/rules/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: rules_domain_1_2\n  type: domain_set\n  args:\n    exps:\n    - full:second.example\n    - full:example.com\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstre | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: rules_domain_1_2\n    type: domain_set\n    args:\n      exps:\n        - full:second.example\n        - full:example.com\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forwa |

## R195-F11 /

路由：`POST /api/rules/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>以下规则 id 不存在: [&#39;missing&#39;]</p>'] | {'message': "以下规则 id 不存在: ['missing']", 'success': False} |

## R195-F13 / {'ids': [1]}

路由：`POST /api/rules/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>以下规则 id 不存在: [1]</p>'] | {'message': '以下规则 id 不存在: [1]', 'success': False} |

## R195-F13 / {'ids': [[1]]}

路由：`POST /api/rules/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## X04 / new_domain/literal/"\ud800"

路由：`POST /api/server-domain`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | non-ascii-internal-error-text | "'utf-8' codec can't encode character '\\ud800' in position 18: surrogates not allowed" | '请求内容包含无效的 Unicode 字符' |

## R218-F10 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>400 Bad Request</title>', '<h1>Bad Request</h1>', '<p>The browser (or proxy) sent a request that this server could not understand.</p>'] | {'message': 'The browser (or proxy) sent a request that this server could not understand.', 'success': False} |

## R218-F11 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': '请求体必须是 JSON 对象', 'success': False} |

## R223-F08 /

路由：`POST /api/subscriptions/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下订阅 id 不存在: ['id 必须是互不重复的字符串']" | 'id 必须是互不重复的字符串' |

## R223-F09 /

路由：`POST /api/subscriptions/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下订阅 id 不存在: ['id 必须是互不重复的字符串']" | 'id 必须是互不重复的字符串' |

## R223-F10 /

路由：`POST /api/subscriptions/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 404 | 400 |
| `body.message` | value | "以下订阅 id 不存在: ['ids 必须是数组']" | 'ids 必须是数组' |

## R223-F12 /

路由：`POST /api/subscriptions/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | value | '订阅操作失败' | '请求体必须是 JSON 对象' |

## R058-B03 / stored-legacy-extension

路由：`PUT /api/domain-discovery/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/domain_discovery.value.synthetic_legacy_extension` | extra-in-go | None | {'flag': False, 'values': []} |

## R181-F09 /

路由：`PUT /api/rule-sets/<rule_set_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |

## H916 / retained-native-issue-moves-after-removing-real-pinned-helpers

路由：`PUT /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/updated_at.value` | value | '2026-11-17T22:05:02.670058Z' | '2026-11-17T22:05:02.680903Z' |

## R185-F16 /

路由：`PUT /api/rules/<rule_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>', '<h1>Not Found</h1>', '<p>Rule not found</p>'] | {'message': 'Rule not found', 'success': False} |

## H911 / actual-public-catalog-key-is-bound-to-DNS-follow

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.system//_revision.revision` | value | 5 | 4 |
| `db.fields.system//_revision.value` | value | 3 | 2 |
| `db.fields.system//agents.revision` | value | 5 | 4 |
| `db.fields.system//agents.value[0].dns_follow_state` | missing-in-go | {'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'updated_at': '2026-11-17T22:07:38.548104Z', 'events': [{'at': '2026-11-17T22:07:38.548045Z', 'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试'}]} | None |
| `db.meta.generation` | value | 5 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:dns_follow:agent-synthetic': {'source': 'task', 'title': '合成 Agent 🧪 · DNS 联动', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': 'default', 'scope': 'profile', 'profile_name': '默认配置', 'target': {'path': '/agents', 'query': {'agent': 'agent-synthetic', 'profile': 'default', 'section': 'dns-follow'}, 'label': '查看同步设置'}, 'checked_at': '2026-11-17T22:07:38Z', 'failure_count': 1, 'id': 'task:dns_follow:agent-synthetic', 'state': 'pending', 'read': False, 'version': '2026-11-17T22:07:38.752416+00:00', 'first_seen_at': '2026-11-17T22:07: | None |
| `files.task-status.json` | missing-in-go | {'mode': 384, 'content': {'dns_follow:agent-synthetic': {'last_run_at': '2026-11-17T22:07:38Z', 'last_finished_at': '2026-11-17T22:07:38Z', 'last_status': 'failed', 'last_duration_ms': 0, 'last_trigger': 'event', 'last_error': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'run_count': 1, 'failure_count': 1, 'consecutive_failures': 1}}} | None |

## R189-B02 / native

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F08 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F09 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F10 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F11 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F12 / native/'unknown'

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F12 / native/1

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F13 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-F15 / native/

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / cleanup-prepare/True

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/blackhole

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | 'runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/direct

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/nodata

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/nxdomain

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/proxy

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S01 / native/refused

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S02 / native/follow

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S03 / native/default

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## R189-S04 / native

路由：`PUT /api/rules/dns`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/oxidns.value.native_config` | value | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n- tag: lazy_cache\n  type: cache\n  args:\n    size: 10240\n    lazy_cache_ttl: 3600\n    short_circuit: true\n- tag: local_probe_cache\n  type: cache\n  args:\n    size: 2048\n    short_circuit: false\n- tag: forward_local\n  type: forward\n  args:\n    concurrent: 2\n    upstreams:\n    - addr: udp://223.5.5.5\n    - addr: udp://119.29.29.29\n- tag: forward_remote\n  type: forward\n  args:\n | "runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: synthetic-agent/token+01234567890123456789\n      password: synthetic-current-proxy/token+0123456789\nlog:\n  level: info\nplugins:\n  - tag: lazy_cache\n    type: cache\n    args:\n      size: 10240\n      lazy_cache_ttl: 3600\n      short_circuit: true\n  - tag: local_probe_cache\n    type: cache\n    args:\n      size: 2048\n      short_circuit: false\n  - tag: forward_local\n    type: forward\n    args:\n      concurrent: 2\n      upstreams:\n        - addr: udp://223.5.5.5\n        - addr: udp://119.29.29.29\n  - tag:  |

## X08 / retired-node-test-dns-migration

路由：`startup`；状态：O12-separate

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/_revision.revision` | value | 5 | 2 |
| `db.fields.profile/default/_revision.value` | value | 2 | 1 |
| `db.fields.profile/default/updated_at.revision` | value | 5 | 1 |
| `db.fields.system//_revision.revision` | value | 6 | 4 |
| `db.fields.system//_revision.value` | value | 3 | 2 |
| `db.fields.system//agents.revision` | value | 6 | 4 |
| `db.fields.system//agents.value[0].node_test_dns_endpoints` | extra-in-go | None | {'legacy-synthetic': {'endpoint': '192.0.2.53:15353', 'profile_id': 'default', 'status': 'installed', 'plugins': [], 'installed': True}} |
| `db.fields.system//system_config.revision` | value | 6 | 4 |
| `db.fields.system//system_config.value.node_test_dns` | missing-in-go | ['https://dns.example/dns-query'] | None |
| `db.fields.system//system_config.value.node_test_dns_config.servers` | length | 1 | 2 |
| `db.fields.system//system_config.value.node_test_dns_config.servers[0]` | value | 'https://dns.example/dns-query' | '192.0.2.53:15353' |
| `db.meta.generation` | value | 6 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T21:28:57.462534+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T21:28:57.462585+00:00', 'first_seen_at': '2026-11-17T21:28:57.462585+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
