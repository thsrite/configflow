# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| G13 / expired/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / expired/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / same-name-different-definition/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| G13 / same-name-identical/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| H1061 / actual-YAML-syntax-error | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | open | — | c14763b3 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/<sub_id>/proxies` | open | — | c14763b3 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/proxies` | open | — | c14763b3 | 400 / 400 |
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | c14763b3 | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | c14763b3 | 400 / 200 |
| H123 / original-and-mirror-both-fail-through-loopback-proxy | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 502 / 502 |
| H128 / sequence-root | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H128 / sequence-root | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / duplicate-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / duplicate-payload | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / mapping-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / mapping-payload | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / missing-payload | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H129 / missing-payload | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H130 / alias | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H130 / alias | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H130 / anchor | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H130 / anchor | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / block-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / block-entry | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / nested-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / nested-entry | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / non-string-entry | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H131 / non-string-entry | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H132 / duplicate-extra-flow-key | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H132 / duplicate-extra-flow-key | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H133 / multiline-extra-flow-value | `GET /api/profiles/<profile_id>/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H133 / multiline-extra-flow-value | `GET /api/rules/local/<name>` | open | — | 15173872f | 200 / 200 |
| H911 / actual-public-catalog-key-is-bound-to-DNS-follow | `PUT /api/rules/dns` | open | — | 15173872f | 400 / 400 |
| R007-S02 /  | `GET /api/aggregations/<agg_id>/preview` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R008-S05 / failed-source-valid-cache | `GET /api/aggregations/<agg_id>/provider` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R011-F06 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 400 |
| R011-F07 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 400 |
| R011-F08 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 415 |
| R012-F01 /  | `GET /api/auth/status` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 405 / 405 |
| R015-B01 /  | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R015-B02 /  | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R015-B03 /  | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R015-B04 /  | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R015-F08 / empty | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F09 / array | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F10 / unknown | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F11 / text-type | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F12 / auto-type | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F13 / keep-negative | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F14 / keep-large | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F15 / daily-large | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-F16 / keep-bool | `POST /api/backup/config` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R015-S04 /  | `POST /api/backup/config` | accepted | [behaviour-changes.md · C35空密码保留](behaviour-changes.md#c35webdav-凭据安全与表单密码保留) | c14763b3 | 200 / 200 |
| R016-B02 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-B03 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-B90 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-B91 / Asia/Shanghai/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-B91 / Etc/GMT+12/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-B91 / Etc/GMT-14/previous-local-day-crosses-UTC-midnight | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-F08 / array | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F09 / empty | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F10 / unknown | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F11 / text-type | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F12 / auto-type | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F13 / daily-large | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F13 / keep-bool | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F13 / keep-large | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F13 / keep-negative | `POST /api/backup/now` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R016-F15 /  | `POST /api/backup/now` | closed | abdf47ef, d01fdf32, f9963a7f, 25959d1cf；25959d1cf为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | c14763b3 | 500 / 500 |
| R016-F15 / real-MKCOL-403-before-PUT | `POST /api/backup/now` | closed | 25959d1cf；25959d1cf为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | c14763b3 | 400 / 400 |
| R016-F16 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-S01 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-S02 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R016-S03 /  | `POST /api/backup/now` | open | — | c14763b3 | 200 / 200 |
| R017-F08 / array | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F09 / empty | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F10 / unknown | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F11 / text-type | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F12 / auto-type | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F13 / daily-large | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F13 / keep-bool | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F13 / keep-large | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F13 / keep-negative | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F14 /  | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F15 /  | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F16 /  | `POST /api/backup/test` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 400 / 400 |
| R017-F18 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 25959d1cf；25959d1cf为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | c14763b3 | 400 / 400 |
| R017-F19 /  | `POST /api/backup/test` | closed | abdf47ef, d01fdf32, f9963a7f, 25959d1cf；25959d1cf为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较 | c14763b3 | 500 / 500 |
| R055-B01 / disabled/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R055-S02 / new/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-B01 / name-collision/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-B02 / before-MATCH/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S01 / direct/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S01 / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | open | — | c14763b3 | 200 / 200 |
| R056-S02 / proxy/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S03 / both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S03 / empty-means-both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S04 / repeat/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R056-S04 / repeat/1 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | c14763b3 | 200 / 200 |
| R058-B03 / stored-legacy-extension | `PUT /api/domain-discovery/settings` | accepted | [behaviour-changes.md · R058-B03](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 200 / 200 |
| R075-F08 / real-stat-EACCES-with-database-still-readable | `GET /api/logs/info` | open | — | c14763b3 | 500 / 200 |
| R087-B04 / text/B04 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 422 / 422 |
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 200 / 502 |
| R087-F13 / text/F13 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 422 / 422 |
| R087-F16 / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 500 / 400 |
| R087-S90 / declared-no-cache-download-failure | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 502 / 502 |
| R087-S91 /  | `GET /api/mosdns/rule-proxy` | open | — | c14763b3 | 200 / 200 |
| R090-F10 / SQLite-trigger-rejects-real-editor-commit | `POST /api/mosdns/settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 500 |
| R103-F08 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R103-F09 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R103-F10 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R103-F11 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R103-F14 /  | `POST /api/nodes/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 400 |
| R122-S05 / failed-source-valid-cache | `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider` | closed | ac2df5d5e；ac2df5d5e为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复 | c14763b3 | 200 / 200 |
| R138-B04 / text/B04 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 422 / 422 |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | c14763b3 | 200 / 502 |
| R138-F13 / text/F13 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 422 / 422 |
| R138-F16 / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | c14763b3 | 500 / 400 |
| R138-S90 / declared-no-cache-download-failure | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | c14763b3 | 502 / 502 |
| R138-S91 /  | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | c14763b3 | 200 / 200 |
| R151-S01 /  | `GET /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R151-S02 /  | `GET /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-B01 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-B02 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-B03 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F08 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F09 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F10 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F11 / DOMAIN | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F11 / IP-CIDR | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F12 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F13 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F14 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-F15 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R152-S01 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-S02 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-S03 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R152-S04 /  | `POST /api/profiles/<profile_id>/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R154-B03 / ?format=unknown | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-B04 / URL-over-query-header | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-B90 / Asia/Shanghai | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-B90 / UTC | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-B91 / HEAD/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F01 / open/provider-D1 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F02 / open/provider-D2 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F03 / open/provider-D3 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F06 / open/provider-D6 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F07 / open/provider-D7 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F08 / open/named-provider-authorization-guard | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-F17 / last-good-provider-output-path-is-directory | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 500 |
| R154-S01 / GET/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-S01 / default | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-S01 / login/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-S01 / open/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-S01 / token/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R154-S06 /  | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R179-B01 /  | `POST /api/rule-sets` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R179-F08 /  | `POST /api/rule-sets` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| R179-F09 /  | `POST /api/rule-sets` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R179-F10 /  | `POST /api/rule-sets` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R179-F11 /  | `POST /api/rule-sets` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R179-S01 /  | `POST /api/rule-sets` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R180-B01 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 404 / 404 |
| R180-B02 / P3/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R180-B03 / P4/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R180-B04 / P5/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R180-B05 / P6/successful-rule-group-write | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R180-F08 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 404 / 404 |
| R180-F09 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 404 / 404 |
| R180-S01 /  | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R180-S01 / mosdns-selector | `DELETE /api/rule-sets/<rule_set_id>` | open | — | 895f5e21 | 200 / 200 |
| R181-B01 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R181-F08 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R181-F09 /  | `PUT /api/rule-sets/<rule_set_id>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| R181-F10 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R181-S01 /  | `PUT /api/rule-sets/<rule_set_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R182-S01 /  | `GET /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R182-S02 /  | `GET /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-B01 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-B02 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-B03 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F08 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F09 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F10 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F11 / DOMAIN | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F11 / IP-CIDR | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F12 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F13 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F14 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-F15 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R183-S01 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-S02 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-S03 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R183-S04 /  | `POST /api/rules` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R184-B01 /  | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-B02 / P3/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-B03 / P4/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-B04 / P5/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-B05 / P6/successful-rule-group-write | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-F08 /  | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 404 / 404 |
| R184-S01 /  | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-S01 / dns-cleanup/False | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-S01 / mosdns-selector | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R184-S02 / dns-cleanup/True | `DELETE /api/rules/<rule_id>` | open | — | 895f5e21 | 200 / 200 |
| R185-B01 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-B02 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-B03 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-F08 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F09 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F10 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F11 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F12 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F13 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F14 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F15 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-F16 /  | `PUT /api/rules/<rule_id>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| R185-F18 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R185-S01 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-S02 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-S03 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-S03 / clear-route-with-type | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R185-S04 /  | `PUT /api/rules/<rule_id>` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R186-B01 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R186-B02 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R186-B03 / concurrent-allocation | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | None / None |
| R186-F08 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-F09 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-F10 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-F11 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-F12 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-F13 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R186-S01 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R186-S02 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R186-S03 /  | `POST /api/rules/batch` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-B01 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-B02 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-B03 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-B04 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-B05 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S01 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S02 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S03 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S04 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S05 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R187-S06 /  | `POST /api/rules/compose` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R188-S01 / real-DNS-follow-binding-catalog | `GET /api/rules/dns` | open | — | 15173872f | 200 / 200 |
| R189-B01 /  | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-B02 / native | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-B02 / template | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-F08 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F08 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F09 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F09 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F10 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F10 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F11 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F11 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F12 / native/'unknown' | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F12 / native/1 | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F12 / template/'unknown' | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F12 / template/1 | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F13 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F13 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-F15 / native/ | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 400 / 400 |
| R189-F15 / template/ | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R189-S01 / cleanup-prepare/False | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / cleanup-prepare/True | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/blackhole | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/direct | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/nodata | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/nxdomain | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/proxy | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / native/refused | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S01 / template/blackhole | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / template/direct | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / template/nodata | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / template/nxdomain | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / template/proxy | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / template/refused | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S01 / type-guard-prepare | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S02 / native/follow | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S02 / template/follow | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S03 / native/default | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S03 / template/default | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R189-S04 / native | `PUT /api/rules/dns` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R189-S04 / template | `PUT /api/rules/dns` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R191-B01 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R191-B02 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R191-B03 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R191-F08 / [] | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R191-F08 / {} | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R191-F09 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R191-F10 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R191-F11 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R191-F12 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 404 / 404 |
| R191-S01 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R191-S02 /  | `POST /api/rules/group-name` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-B01 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-B02 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-B03 / explicit-DNS-does-not-follow-policy-edit | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-F08 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F09 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F10 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F11 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F12 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F13 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F14 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R194-F15 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 404 / 404 |
| R194-S01 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-S02 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-S03 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R194-S04 /  | `PUT /api/rules/policy` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R195-B01 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R195-B02 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R195-B03 / real-recognized-native-provider-order-rebuild | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正) | 895f5e21 | 200 / 200 |
| R195-F08 / [] | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R195-F08 / {} | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R195-F09 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R195-F10 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R195-F11 /  | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| R195-F12 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 400 / 400 |
| R195-F13 / {'ids': [1]} | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| R195-F13 / {'ids': [[1]]} | `POST /api/rules/reorder` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| R195-S01 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R195-S02 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R195-S03 /  | `POST /api/rules/reorder` | closed | d7bb7ca45；d7bb7ca45为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比 | 895f5e21 | 200 / 200 |
| R217-S02 /  | `PUT /api/subscriptions/<sub_id>` | closed | e9187b9f | c14763b3 | 200 / 200 |
| R218-F09 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | c14763b3 | 500 / 500 |
| R218-F10 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 400 / 400 |
| R218-F11 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 400 |
| R218-F12 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 8f3d36ad | c14763b3 | 500 / 500 |
| R218-F13 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 3ab22d36 | c14763b3 | 500 / 500 |
| R218-S03 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | c14763b3 | 200 / 200 |
| R220-B03 / ?format=unknown | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-B90 / Asia/Shanghai | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-B90 / UTC | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-B91 / HEAD/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F01 / open/provider-D1 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F02 / open/provider-D2 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F03 / open/provider-D3 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F06 / open/provider-D6 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F07 / open/provider-D7 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F08 / open/named-provider-authorization-guard | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-F17 / last-good-provider-output-path-is-directory | `GET /api/subscriptions/<sub_id>/proxies` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 500 |
| R220-S01 / GET/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-S01 / default | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-S01 / login/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-S01 / open/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-S01 / token/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R220-S06 /  | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-B02 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-B90 / Asia/Shanghai | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-B90 / UTC | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-F11 / invalid-source-no-fallback | `GET /api/subscriptions/proxies` | closed | cb0cc9fa | c14763b3 | 400 / 400 |
| R222-S01 / different-definitions-no-chain | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-S01 / identical-crossfeed | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-S01 / two | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-S02 / disabled | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-S03 / http403-cache-fallback | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R222-S04 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| R223-F08 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R223-F09 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R223-F10 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 404 / 400 |
| R223-F12 /  | `POST /api/subscriptions/reorder` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 400 |
| R228-F01 /  | `GET /api/version` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 405 / 405 |
| T06 / 12-second-last-good-and-background-finish | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| T06 / 20-clients-one-flight | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] / [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] |
| T06 / next-poll-is-fresh | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | c14763b3 | 200 / 200 |
| T27 / real-tick/due-and-backoff | `startup` | closed | 8f3d36ad | c14763b3 | None / None |
| X04 / login/empty | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 400 |
| X04 / login/ignored-extra/-Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 200 / 400 |
| X04 / login/ignored-extra/Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 200 / 400 |
| X04 / login/ignored-extra/NaN | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 200 / 400 |
| X04 / login/null | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 400 |
| X04 / login/plain | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | c14763b3 | 500 / 415 |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 400 |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | c14763b3 | 500 / 400 |
| X06 / encoded-slash/GET /api/profiles/<profile_id>/rules/local/<name> | `GET /api/profiles/<profile_id>/rules/local/<name>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / encoded-slash/GET /api/rules/local/<name> | `GET /api/rules/local/<name>` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / head//api/aggregations | `GET /api/aggregations` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| X06 / head//api/mosdns/api-settings | `GET /api/mosdns/api-settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| X06 / head//api/nodes | `GET /api/nodes` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| X06 / head//api/rule-sets | `GET /api/rule-sets` | accepted | [behaviour-changes.md · C32](behaviour-changes.md#c32--head-规则接口只读python-bug-修正) | 895f5e21 | 500 / 200 |
| X06 / head//api/rules | `GET /api/rules` | accepted | [behaviour-changes.md · C32](behaviour-changes.md#c32--head-规则接口只读python-bug-修正) | 895f5e21 | 500 / 200 |
| X06 / head//api/subscriptions | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 500 / 500 |
| X06 / login/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / login/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / open/trailing-slash | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / open/unknown-admin | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X06 / open/unknown-anonymous | `GET /api/subscriptions` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 895f5e21 | 404 / 404 |
| X08 / retired-node-test-dns-migration | `startup` | separate | — | c14763b3 | None / None |

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
| `body.error` | missing-in-go | "[Errno 13] Permission denied: '/tmp/configdiff-z1bu7_52/denied-log/synthetic.log'" | None |
| `body.exists` | extra-in-go | None | False |
| `body.path` | extra-in-go | None | '/tmp/configdiff-z1bu7_52/denied-log/synthetic.log' |
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
| `db.fields.system//agents.value[0].dns_follow_state` | missing-in-go | {'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'updated_at': '2026-11-17T20:43:44.369907Z', 'events': [{'at': '2026-11-17T20:43:44.369850Z', 'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试'}]} | None |
| `db.meta.generation` | value | 5 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:dns_follow:agent-synthetic': {'source': 'task', 'title': '合成 Agent 🧪 · DNS 联动', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': 'default', 'scope': 'profile', 'profile_name': '默认配置', 'target': {'path': '/agents', 'query': {'agent': 'agent-synthetic', 'profile': 'default', 'section': 'dns-follow'}, 'label': '查看同步设置'}, 'checked_at': '2026-11-17T20:43:44Z', 'failure_count': 1, 'id': 'task:dns_follow:agent-synthetic', 'state': 'pending', 'read': False, 'version': '2026-11-17T20:43:44.538075+00:00', 'first_seen_at': '2026-11-17T20:43: | None |
| `files.task-status.json` | missing-in-go | {'mode': 384, 'content': {'dns_follow:agent-synthetic': {'last_run_at': '2026-11-17T20:43:44Z', 'last_finished_at': '2026-11-17T20:43:44Z', 'last_status': 'failed', 'last_duration_ms': 0, 'last_trigger': 'event', 'last_error': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'run_count': 1, 'failure_count': 1, 'consecutive_failures': 1}}} | None |

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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzE1ODMsImlhdCI6MTc5NDk0NTE4M30.l4vH9DIuqSH1853wrULXOdHyfG9pn4cTClFBvC2P3Xs' | None |
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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzE1ODMsImlhdCI6MTc5NDk0NTE4M30.l4vH9DIuqSH1853wrULXOdHyfG9pn4cTClFBvC2P3Xs' | None |
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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMzE1ODMsImlhdCI6MTc5NDk0NTE4M30.l4vH9DIuqSH1853wrULXOdHyfG9pn4cTClFBvC2P3Xs' | None |
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
| `files.shared/rules/by-id/dd768124ddf4f2ff3c1aa671c567aaba80ed75b8edf3d75fd3d5a8223f96151d.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

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
| `db.fields.system//agents.value[0].dns_follow_state` | missing-in-go | {'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'updated_at': '2026-11-17T20:43:44.369907Z', 'events': [{'at': '2026-11-17T20:43:44.369850Z', 'status': 'failed', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试'}]} | None |
| `db.meta.generation` | value | 5 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:dns_follow:agent-synthetic': {'source': 'task', 'title': '合成 Agent 🧪 · DNS 联动', 'message': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': 'default', 'scope': 'profile', 'profile_name': '默认配置', 'target': {'path': '/agents', 'query': {'agent': 'agent-synthetic', 'profile': 'default', 'section': 'dns-follow'}, 'label': '查看同步设置'}, 'checked_at': '2026-11-17T20:43:44Z', 'failure_count': 1, 'id': 'task:dns_follow:agent-synthetic', 'state': 'pending', 'read': False, 'version': '2026-11-17T20:43:44.538075+00:00', 'first_seen_at': '2026-11-17T20:43: | None |
| `files.task-status.json` | missing-in-go | {'mode': 384, 'content': {'dns_follow:agent-synthetic': {'last_run_at': '2026-11-17T20:43:44Z', 'last_finished_at': '2026-11-17T20:43:44Z', 'last_status': 'failed', 'last_duration_ms': 0, 'last_trigger': 'event', 'last_error': 'DNS 联动暂不可用：联动处理异常，请检查 Agent 连接后重试', 'run_count': 1, 'failure_count': 1, 'consecutive_failures': 1}}} | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T19:34:19.600935+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T19:34:19.600983+00:00', 'first_seen_at': '2026-11-17T19:34:19.600983+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
