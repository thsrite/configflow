# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| G13 / expired/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / expired/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / expired/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / fresh/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / fresh/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / interval-zero/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / interval-zero/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / same-name-different-definition/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| G13 / same-name-identical/merged | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| H123 / original-and-mirror-both-fail-through-loopback-proxy | `GET /api/mosdns/rule-proxy` | open | — | 7521bb5c | 502 / 502 |
| R016-B02 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-B03 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-B90 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-F15 /  | `POST /api/backup/now` | open | — | aeff9d75 | 500 / 500 |
| R016-F16 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-S01 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-S02 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R016-S03 /  | `POST /api/backup/now` | open | — | aeff9d75 | 200 / 200 |
| R017-F18 /  | `POST /api/backup/test` | open | — | aeff9d75 | 400 / 400 |
| R017-F19 /  | `POST /api/backup/test` | open | — | aeff9d75 | 500 / 500 |
| R055-B01 / disabled/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R055-S02 / new/domain/example.com | `POST /api/domain-discovery/region/route` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-B01 / name-collision/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-B02 / before-MATCH/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S01 / direct/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S01 / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | open | — | aeff9d75 | 200 / 200 |
| R056-S02 / proxy/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S03 / both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S03 / empty-means-both/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S04 / repeat/0 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R056-S04 / repeat/1 | `POST /api/domain-discovery/rulesets/init` | closed | 041ab5a6 | aeff9d75 | 200 / 200 |
| R058-B03 / stored-legacy-extension | `PUT /api/domain-discovery/settings` | accepted | [behaviour-changes.md · R058-B03](behaviour-changes.md#config-step-b--coordinator-decisions) | aeff9d75 | 200 / 200 |
| R075-F08 / real-stat-EACCES-with-database-still-readable | `GET /api/logs/info` | open | — | 7521bb5c | 500 / 200 |
| R087-B04 / text/B04 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 7521bb5c | 422 / 422 |
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | open | — | aeff9d75 | 200 / 502 |
| R087-F13 / text/F13 | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | 7521bb5c | 422 / 422 |
| R087-F16 / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | open | — | aeff9d75 | 500 / 400 |
| R087-S90 / declared-no-cache-download-failure | `GET /api/mosdns/rule-proxy` | closed | 8f3d36ad | aeff9d75 | 502 / 502 |
| R087-S91 /  | `GET /api/mosdns/rule-proxy` | open | — | aeff9d75 | 200 / 200 |
| R090-F10 / SQLite-trigger-rejects-real-editor-commit | `POST /api/mosdns/settings` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 7521bb5c | 500 / 500 |
| R138-B04 / text/B04 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 7521bb5c | 422 / 422 |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | aeff9d75 | 200 / 502 |
| R138-F13 / text/F13 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | 7521bb5c | 422 / 422 |
| R138-F16 / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | aeff9d75 | 500 / 400 |
| R138-S90 / declared-no-cache-download-failure | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | closed | 8f3d36ad | aeff9d75 | 502 / 502 |
| R138-S91 /  | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | aeff9d75 | 200 / 200 |
| R154-B03 / ?format=unknown | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-B04 / URL-over-query-header | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-B90 / Asia/Shanghai | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-B90 / UTC | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-B91 / HEAD/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F01 / open/provider-D1 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F02 / open/provider-D2 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F03 / open/provider-D3 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F06 / open/provider-D6 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F07 / open/provider-D7 | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-F08 / open/named-provider-authorization-guard | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S01 / GET/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S01 / default | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S01 / login/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S01 / open/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S01 / token/authorized-provider | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R154-S06 /  | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R217-S02 /  | `PUT /api/subscriptions/<sub_id>` | closed | e9187b9f | aeff9d75 | 200 / 200 |
| R218-F09 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | aeff9d75 | 500 / 500 |
| R218-F10 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | aeff9d75 | 400 / 400 |
| R218-F11 /  | `POST /api/subscriptions/<sub_id>/fetch` | accepted | [behaviour-changes.md · Step B #1](behaviour-changes.md#config-step-b--coordinator-decisions) | aeff9d75 | 500 / 400 |
| R218-F12 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 8f3d36ad | aeff9d75 | 500 / 500 |
| R218-F13 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 0313e086, 3ab22d36 | aeff9d75 | 500 / 500 |
| R218-S03 /  | `POST /api/subscriptions/<sub_id>/fetch` | closed | 8f3d36ad | aeff9d75 | 200 / 200 |
| R220-B03 / ?format=unknown | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-B90 / Asia/Shanghai | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-B90 / UTC | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-B91 / HEAD/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F01 / open/provider-D1 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F02 / open/provider-D2 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F03 / open/provider-D3 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F06 / open/provider-D6 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F07 / open/provider-D7 | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-F08 / open/named-provider-authorization-guard | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S01 / GET/first-download | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S01 / default | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S01 / login/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S01 / open/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S01 / token/authorized-provider | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R220-S06 /  | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-B02 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-B90 / Asia/Shanghai | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-B90 / UTC | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-F11 / invalid-source-no-fallback | `GET /api/subscriptions/proxies` | open | — | aeff9d75 | 400 / 400 |
| R222-S01 / different-definitions-no-chain | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-S01 / identical-crossfeed | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-S01 / two | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-S02 / disabled | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-S03 / http403-cache-fallback | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R222-S04 / none | `GET /api/subscriptions/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| R228-F01 /  | `GET /api/version` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 7521bb5c | 405 / 405 |
| T06 / 12-second-last-good-and-background-finish | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| T06 / 20-clients-one-flight | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] / [200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200] |
| T06 / next-poll-is-fresh | `GET /api/subscriptions/<sub_id>/proxies` | closed | 7b904725 | aeff9d75 | 200 / 200 |
| T27 / real-tick/due-and-backoff | `startup` | closed | 8f3d36ad | aeff9d75 | None / None |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | aeff9d75 | 500 / 400 |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | aeff9d75 | 500 / 400 |
| X08 / retired-node-test-dns-migration | `startup` | separate | — | aeff9d75 | None / None |

## R075-F08 / real-stat-EACCES-with-database-still-readable

路由：`GET /api/logs/info`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `log.stat-permission.assigned_path` | value | True | False |
| `log.stat-permission.permission_error` | value | True | False |
| `status` | value | 500 | 200 |
| `body.error` | missing-in-go | "[Errno 13] Permission denied: '/tmp/configdiff-41i0wm5i/denied-log/synthetic.log'" | None |
| `body.exists` | extra-in-go | None | False |
| `body.path` | extra-in-go | None | '/tmp/configdiff-41i0wm5i/denied-log/synthetic.log' |
| `body.success` | value | False | True |

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

## R222-F11 / invalid-source-no-fallback

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '订阅「订阅甲🧪」准备失败：Provider 代理必须是含非空 name 的对象\n字段：proxies[0]\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 11 列\n出错内容：proxies: [bad]' | '订阅「订阅甲🧪」准备失败：Provider 代理必须是含非空 name 的对象\n字段：proxies[0]\n内容来源：本次订阅返回的内容' |

## R228-F01 /

路由：`GET /api/version`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>The method is not allowed for the requested URL.</p>'] | {'message': 'The method is not allowed for the requested URL.', 'success': False} |

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

## R016-F15 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '备份失败：NotEnoughSpace' | '备份失败：HTTP 507' |
| `files.notifications.json.content.items.task:webdav_backup.message` | value | 'NotEnoughSpace' | 'HTTP 507' |
| `files.task-status.json.content.webdav_backup.last_error` | value | 'NotEnoughSpace' | 'HTTP 507' |

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

## R017-F18 /

路由：`POST /api/backup/test`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '无法创建目录 /one/：ResponseErrorCode HTTP 403' | '无法创建目录 /one/：HTTP 403' |

## R017-F19 /

路由：`POST /api/backup/test`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '连接测试失败：NoConnection' | '连接测试失败：network request failed' |

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
| `files.shared/rules/by-id/4041f17d5c0fdd2b085350804ce17233c8aaee66326b570e4c54146b778a3c67.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R090-F10 / SQLite-trigger-rejects-real-editor-commit

路由：`POST /api/mosdns/settings`；状态：different

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

## R058-B03 / stored-legacy-extension

路由：`PUT /api/domain-discovery/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/domain_discovery.value.synthetic_legacy_extension` | extra-in-go | None | {'flag': False, 'values': []} |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T06:36:36.485419+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T06:36:36.485487+00:00', 'first_seen_at': '2026-11-17T06:36:36.485487+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
