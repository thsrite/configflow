# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| G13 / expired/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / expired/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / fresh/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / fresh/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / same-name-different-definition/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| G13 / same-name-identical/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| H123 / original-and-mirror-both-fail-through-loopback-proxy | `GET /api/mosdns/rule-proxy` | open | — | 038b52ae | 502 / 502 |
| R007-S02 /  | `GET /api/aggregations/<agg_id>/preview` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R008-S05 / failed-source-valid-cache | `GET /api/aggregations/<agg_id>/provider` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R012-F01 /  | `GET /api/auth/status` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 405 / 405 |
| R015-B01 /  | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R015-B02 /  | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R015-B03 /  | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R015-B04 /  | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R015-F08 / empty | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F09 / array | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F10 / unknown | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F11 / text-type | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F12 / auto-type | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F13 / keep-negative | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F14 / keep-large | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F15 / daily-large | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-F16 / keep-bool | `POST /api/backup/config` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R015-S04 /  | `POST /api/backup/config` | accepted | [behaviour-changes.md · C35空密码保留](behaviour-changes.md#c35webdav-凭据安全与表单密码保留) | 038b52ae | 200 / 200 |
| R016-B02 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-B03 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-B90 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-B91 / Asia/Shanghai/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-B91 / Etc/GMT+12/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-B91 / Etc/GMT-14/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-F08 / array | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F09 / empty | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F10 / unknown | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F11 / text-type | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F12 / auto-type | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F13 / daily-large | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F13 / keep-bool | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F13 / keep-large | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F13 / keep-negative | `POST /api/backup/now` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R016-F15 /  | `POST /api/backup/now` | closed | abdf47ef, d01fdf32, f9963a7f, 9b421107；9b421107为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 038b52ae | 500 / 500 |
| R016-F15 / real-MKCOL-403-before-PUT | `POST /api/backup/now` | closed | 9b421107；9b421107为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 038b52ae | 400 / 400 |
| R016-F16 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-S01 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-S02 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R016-S03 /  | `POST /api/backup/now` | open | — | 038b52ae | 200 / 200 |
| R017-F08 / array | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F09 / empty | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F10 / unknown | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F11 / text-type | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F12 / auto-type | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F13 / daily-large | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F13 / keep-bool | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F13 / keep-large | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F13 / keep-negative | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F14 /  | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F15 /  | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F16 /  | `POST /api/backup/test` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 400 / 400 |
| R017-F18 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 9b421107；9b421107为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 038b52ae | 400 / 400 |
| R017-F19 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 9b421107；9b421107为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | 038b52ae | 500 / 500 |
| R055-B01 / disabled/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R055-S02 / new/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-B01 / name-collision/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-B02 / before-MATCH/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S01 / direct/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S01 / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | open | — | 038b52ae | 200 / 200 |
| R056-S02 / proxy/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S03 / both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S03 / empty-means-both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S04 / repeat/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R056-S04 / repeat/1 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | 038b52ae | 200 / 200 |
| R058-B03 / stored-legacy-extension | `PUT /api/domain-discovery/settings` | accepted | [behaviour-changes.md · R058-B03](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 200 / 200 |
| R075-F08 / real-stat-EACCES-with-database-still-readable | `GET /api/logs/info` | open | — | 038b52ae | 500 / 200 |
| R087-B04 / text/B04 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 422 / 422 |
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | open | — | 038b52ae | 200 / 502 |
| R087-F13 / text/F13 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 422 / 422 |
| R087-F16 / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | open | — | 038b52ae | 500 / 400 |
| R087-S90 / declared-no-cache-download-failure | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 502 / 502 |
| R087-S91 /  | `GET /api/mosdns/rule-proxy` | open | — | 038b52ae | 200 / 200 |
| R090-F10 / SQLite-trigger-rejects-real-editor-commit | `POST /api/mosdns/settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| R103-F08 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R103-F09 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R103-F10 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R103-F11 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R103-F14 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 400 |
| R122-S05 / failed-source-valid-cache | `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider` | closed | 5e5dd8ee；5e5dd8ee为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | 038b52ae | 200 / 200 |
| R138-B04 / text/B04 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 422 / 422 |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 038b52ae | 200 / 502 |
| R138-F13 / text/F13 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 422 / 422 |
| R138-F16 / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 038b52ae | 500 / 400 |
| R138-S90 / declared-no-cache-download-failure | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 038b52ae | 502 / 502 |
| R138-S91 /  | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 038b52ae | 200 / 200 |
| R154-B03 / ?format=unknown | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-B04 / URL-over-query-header | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-B90 / Asia/Shanghai | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-B90 / UTC | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-B91 / HEAD/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F01 / open/provider-D1 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F02 / open/provider-D2 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F03 / open/provider-D3 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F06 / open/provider-D6 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F07 / open/provider-D7 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F08 / open/named-provider-authorization-guard | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-F17 / last-good-provider-output-path-is-directory | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| R154-S01 / GET/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-S01 / default | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-S01 / login/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-S01 / open/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-S01 / token/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R154-S06 /  | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R217-S02 /  | `PUT /api/subscriptions/<sub_id>` | closed | e9187b9f | 038b52ae | 200 / 200 |
| R218-F09 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | 038b52ae | 500 / 500 |
| R218-F10 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 400 / 400 |
| R218-F11 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 400 |
| R218-F12 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 8f3d36ad | 038b52ae | 500 / 500 |
| R218-F13 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 3ab22d36 | 038b52ae | 500 / 500 |
| R218-S03 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | 038b52ae | 200 / 200 |
| R220-B03 / ?format=unknown | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-B90 / Asia/Shanghai | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-B90 / UTC | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-B91 / HEAD/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F01 / open/provider-D1 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F02 / open/provider-D2 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F03 / open/provider-D3 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F06 / open/provider-D6 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F07 / open/provider-D7 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F08 / open/named-provider-authorization-guard | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-F17 / last-good-provider-output-path-is-directory | `GET /api/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| R220-S01 / GET/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-S01 / default | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-S01 / login/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-S01 / open/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-S01 / token/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R220-S06 /  | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-B02 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-B90 / Asia/Shanghai | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-B90 / UTC | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-F11 / invalid-source-no-fallback | `GET /api/subscriptions/proxies` | closed | cb0cc9fa | 038b52ae | 400 / 400 |
| R222-S01 / different-definitions-no-chain | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-S01 / identical-crossfeed | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-S01 / two | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-S02 / disabled | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-S03 / http403-cache-fallback | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R222-S04 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| R223-F08 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R223-F09 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R223-F10 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 400 |
| R223-F12 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 400 |
| R228-F01 /  | `GET /api/version` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 405 / 405 |
| T06 / 12-second-last-good-and-background-finish | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| T06 / 20-clients-one-flight | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] / [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] |
| T06 / next-poll-is-fresh | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | 038b52ae | 200 / 200 |
| T27 / real-tick/due-and-backoff | `startup` | open | — | 038b52ae | None / None |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 400 |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 400 |
| X06 / head//api/aggregations | `GET /api/aggregations` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| X06 / head//api/mosdns/api-settings | `GET /api/mosdns/api-settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| X06 / head//api/nodes | `GET /api/nodes` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| X06 / head//api/subscriptions | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 500 / 500 |
| X06 / login/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 404 |
| X06 / login/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 404 |
| X06 / open/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 404 |
| X06 / open/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 404 |
| X06 / open/unknown-anonymous | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 038b52ae | 404 / 404 |
| X08 / retired-node-test-dns-migration | `startup` | separate | — | 038b52ae | None / None |

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
| `body.error` | missing-in-go | "[Errno 13] Permission denied: '/tmp/configdiff-6iafvp2z/denied-log/synthetic.log'" | None |
| `body.exists` | extra-in-go | None | False |
| `body.path` | extra-in-go | None | '/tmp/configdiff-6iafvp2z/denied-log/synthetic.log' |
| `body.success` | value | False | True |

## X06 / head//api/mosdns/api-settings

路由：`GET /api/mosdns/api-settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |

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

## R154-F17 / last-good-provider-output-path-is-directory

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

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

## R220-F17 / last-good-provider-output-path-is-directory

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'message': 'The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.', 'success': False} |

## R228-F01 /

路由：`GET /api/version`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>The method is not allowed for the requested URL.</p>'] | {'message': 'The method is not allowed for the requested URL.', 'success': False} |

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
| `files.shared/rules/by-id/3cd891c8bab62d2d1963aa78431b5e3f07cc33490a42e19b6d8e13d8bb23612a.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

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

## T27 / real-tick/due-and-backoff

路由：`startup`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `scheduler.requests./cache-url-mismatch` | missing-in-go | [{'method': 'GET', 'path': '/cache-url-mismatch', 'authorization': '', 'user_agent': 'clash.meta'}] | None |
| `scheduler.requests./due` | missing-in-go | [{'method': 'GET', 'path': '/due', 'authorization': '', 'user_agent': 'clash.meta'}] | None |
| `scheduler.requests./retry-capped` | missing-in-go | [{'method': 'GET', 'path': '/retry-capped', 'authorization': '', 'user_agent': 'clash.meta'}] | None |
| `scheduler.requests./retry-due` | missing-in-go | [{'method': 'GET', 'path': '/retry-due', 'authorization': '', 'user_agent': 'clash.meta'}] | None |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:backoff': {'source': 'task', 'title': '合成-backoff · 订阅更新', 'message': '任务未完成，请查看任务详情。', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'backoff', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T14:11:00Z', 'failure_count': 3, 'id': 'task:subscription:backoff', 'state': 'pending', 'read': False, 'version': '2026-11-17T14:19:49.987058+00:00', 'first_seen_at': '2026-11-17T14:19:49.987058+00:00', 'last_seen_at': '2026-11-17T14:11:00Z', ' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.metadata.fetched_at` | missing-in-go | '2026-11-17T14:19:50.009154+00:00' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.metadata.subscription_name` | missing-in-go | '合成-cache-url-mismatch' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.metadata.url` | value | 'http://127.0.0.1:34577/cache-url-mismatch' | 'http://other-source.example/feed' |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].id` | missing-in-go | 'node_6b7f5c27' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].name` | value | 'synthetic-node' | 'cached-node' |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].params` | missing-in-go | {} | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].server` | value | '192.0.2.1' | '192.0.2.2' |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].subscription_id` | missing-in-go | 'cache-url-mismatch' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.nodes[0].subscription_name` | missing-in-go | '合成-cache-url-mismatch' | None |
| `files.shared/subscribes/cache-url-mismatch.json.content.updated_at` | time-format | '2026-11-17T14:19:50.009154+00:00' | '2026-11-17T14:19:20Z' |
| `files.shared/subscribes/cache-url-mismatch.json.mode` | value | 384 | 420 |
| `files.shared/subscribes/cache-url-mismatch.json.bak` | missing-in-go | {'mode': 384, 'content': {'subscription_id': 'cache-url-mismatch', 'updated_at': '2026-11-17T14:19:20Z', 'count': 1, 'nodes': [{'name': 'cached-node', 'type': 'socks5', 'server': '192.0.2.2', 'port': 1080}], 'metadata': {'url': 'http://other-source.example/feed'}}} | None |
| `files.shared/subscribes/due.json` | missing-in-go | {'mode': 384, 'content': {'subscription_id': 'due', 'updated_at': '2026-11-17T14:19:49.980717+00:00', 'count': 1, 'nodes': [{'name': 'synthetic-node', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080, 'params': {}, 'subscription_id': 'due', 'subscription_name': '合成-due', 'id': 'node_e4734f40'}], 'metadata': {'subscription_name': '合成-due', 'url': 'http://127.0.0.1:34577/due', 'fetched_at': '2026-11-17T14:19:49.980717+00:00'}}} | None |
| `files.shared/subscribes/retry-capped.json` | missing-in-go | {'mode': 384, 'content': {'subscription_id': 'retry-capped', 'updated_at': '2026-11-17T14:19:50.000865+00:00', 'count': 1, 'nodes': [{'name': 'synthetic-node', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080, 'params': {}, 'subscription_id': 'retry-capped', 'subscription_name': '合成-retry-capped', 'id': 'node_cd4da658'}], 'metadata': {'subscription_name': '合成-retry-capped', 'url': 'http://127.0.0.1:34577/retry-capped', 'fetched_at': '2026-11-17T14:19:50.000865+00:00'}}} | None |
| `files.shared/subscribes/retry-due.json` | missing-in-go | {'mode': 384, 'content': {'subscription_id': 'retry-due', 'updated_at': '2026-11-17T14:19:49.990905+00:00', 'count': 1, 'nodes': [{'name': 'synthetic-node', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080, 'params': {}, 'subscription_id': 'retry-due', 'subscription_name': '合成-retry-due', 'id': 'node_5d00ec5c'}], 'metadata': {'subscription_name': '合成-retry-due', 'url': 'http://127.0.0.1:34577/retry-due', 'fetched_at': '2026-11-17T14:19:49.990905+00:00'}}} | None |
| `files.shared/subscription-downloads/4ca3086265fcfb3ddaa5af68fcf00fd73f5edfa03eced32d923352db00822324.json` | missing-in-go | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n', 'fetched_at': '2026-11-17T14:19:50.000865+00:00', 'traffic': None}} | None |
| `files.shared/subscription-downloads/0b21ee0679579346eebb15229ccccb3d57413e19880e85f06a4a78854431714e.json` | missing-in-go | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n', 'fetched_at': '2026-11-17T14:19:49.990905+00:00', 'traffic': None}} | None |
| `files.shared/subscription-downloads/e44d682be26592602f014ce69f2b6f84955ccded371d3f64f28eda1457e138c7.json` | missing-in-go | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n', 'fetched_at': '2026-11-17T14:19:50.009154+00:00', 'traffic': None}} | None |
| `files.shared/subscription-downloads/a8ec51bcc05dca342a9e7332838ae6b442848f96eaec72e49b9f17f87b2a4a46.json` | missing-in-go | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n', 'fetched_at': '2026-11-17T14:19:49.980717+00:00', 'traffic': None}} | None |
| `files.shared/subscription_health.json` | missing-in-go | {'mode': 384, 'content': {'due': {'history': [{'at': '2026-11-17T14:19:49.984134Z', 'status': 'ok', 'count': 1}]}, 'retry-due': {'history': [{'at': '2026-11-17T14:19:49.992261Z', 'status': 'ok', 'count': 1}]}, 'retry-capped': {'history': [{'at': '2026-11-17T14:19:50.001857Z', 'status': 'ok', 'count': 1}]}, 'cache-url-mismatch': {'history': [{'at': '2026-11-17T14:19:50.010220Z', 'status': 'ok', 'count': 1}]}}} | None |
| `files.shared/subscription_health.json.bak` | missing-in-go | {'mode': 384, 'content': {'due': {'history': [{'at': '2026-11-17T14:19:49.984134Z', 'status': 'ok', 'count': 1}]}, 'retry-due': {'history': [{'at': '2026-11-17T14:19:49.992261Z', 'status': 'ok', 'count': 1}]}, 'retry-capped': {'history': [{'at': '2026-11-17T14:19:50.001857Z', 'status': 'ok', 'count': 1}]}}} | None |
| `files.task-status.json.content.subscription:cache-url-mismatch` | missing-in-go | {'last_run_at': '2026-11-17T14:19:49Z', 'last_finished_at': '2026-11-17T14:19:50Z', 'last_status': 'success', 'last_duration_ms': 22, 'last_trigger': 'schedule', 'last_error': '', 'run_count': 1, 'last_success_at': '2026-11-17T14:19:50Z', 'consecutive_failures': 0, 'cache_updated_at': '2026-11-17T14:19:50.009154+00:00'} | None |
| `files.task-status.json.content.subscription:due` | missing-in-go | {'last_run_at': '2026-11-17T14:19:49Z', 'last_finished_at': '2026-11-17T14:19:49Z', 'last_status': 'success', 'last_duration_ms': 7, 'last_trigger': 'schedule', 'last_error': '', 'run_count': 1, 'last_success_at': '2026-11-17T14:19:49Z', 'consecutive_failures': 0, 'cache_updated_at': '2026-11-17T14:19:49.980717+00:00'} | None |
| `files.task-status.json.content.subscription:retry-capped.cache_updated_at` | missing-in-go | '2026-11-17T14:19:50.000865+00:00' | None |
| `files.task-status.json.content.subscription:retry-capped.consecutive_failures` | value | 0 | 10 |
| `files.task-status.json.content.subscription:retry-capped.last_duration_ms` | missing-in-go | 24 | None |
| `files.task-status.json.content.subscription:retry-capped.last_error` | missing-in-go | '' | None |
| `files.task-status.json.content.subscription:retry-capped.last_finished_at` | missing-in-go | '2026-11-17T14:19:50Z' | None |
| `files.task-status.json.content.subscription:retry-capped.last_run_at` | invalid-time-window | '2026-11-17T14:19:49Z' | '2026-11-17T14:18:19Z' |
| `files.task-status.json.content.subscription:retry-capped.last_status` | value | 'success' | 'failed' |
| `files.task-status.json.content.subscription:retry-capped.last_success_at` | missing-in-go | '2026-11-17T14:19:50Z' | None |
| `files.task-status.json.content.subscription:retry-capped.last_trigger` | missing-in-go | 'schedule' | None |
| `files.task-status.json.content.subscription:retry-capped.run_count` | missing-in-go | 1 | None |
| `files.task-status.json.content.subscription:retry-due.cache_updated_at` | missing-in-go | '2026-11-17T14:19:49.990905+00:00' | None |
| `files.task-status.json.content.subscription:retry-due.consecutive_failures` | value | 0 | 3 |
| `files.task-status.json.content.subscription:retry-due.last_duration_ms` | missing-in-go | 15 | None |
| `files.task-status.json.content.subscription:retry-due.last_error` | missing-in-go | '' | None |
| `files.task-status.json.content.subscription:retry-due.last_finished_at` | missing-in-go | '2026-11-17T14:19:49Z' | None |
| `files.task-status.json.content.subscription:retry-due.last_run_at` | invalid-time-window | '2026-11-17T14:19:49Z' | '2026-11-17T13:57:40Z' |
| `files.task-status.json.content.subscription:retry-due.last_status` | value | 'success' | 'failed' |
| `files.task-status.json.content.subscription:retry-due.last_success_at` | missing-in-go | '2026-11-17T14:19:49Z' | None |
| `files.task-status.json.content.subscription:retry-due.last_trigger` | missing-in-go | 'schedule' | None |
| `files.task-status.json.content.subscription:retry-due.run_count` | missing-in-go | 1 | None |
| `files.task-status.json.mode` | value | 384 | 420 |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T14:20:22.969729+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T14:20:22.969764+00:00', 'first_seen_at': '2026-11-17T14:20:22.969764+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
