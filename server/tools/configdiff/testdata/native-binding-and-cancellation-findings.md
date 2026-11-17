# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| H1061 / actual-YAML-syntax-error | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | open | — | 93fdcc62 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/<sub_id>/proxies` | open | — | 93fdcc62 | 400 / 400 |
| H1061 / actual-YAML-syntax-error | `GET /api/subscriptions/proxies` | open | — | 93fdcc62 | 400 / 400 |
| R011-F06 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 400 |
| R011-F07 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 400 |
| R011-F08 /  | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 415 |
| R012-F01 /  | `GET /api/auth/status` | accepted | [behaviour-changes.md · Step B #3](behaviour-changes.md#config-step-b--coordinator-decisions) | 93fdcc62 | 405 / 405 |
| X04 / login/empty | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 400 |
| X04 / login/ignored-extra/-Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 200 / 400 |
| X04 / login/ignored-extra/Infinity | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 200 / 400 |
| X04 / login/ignored-extra/NaN | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 200 / 400 |
| X04 / login/null | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 400 |
| X04 / login/plain | `POST /api/auth/login` | accepted | [behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses) | 93fdcc62 | 500 / 415 |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 93fdcc62 | 500 / 400 |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | accepted | [behaviour-changes.md · Step B #2](behaviour-changes.md#config-step-b--coordinator-decisions) | 93fdcc62 | 500 / 400 |

## R012-F01 /

路由：`GET /api/auth/status`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>The method is not allowed for the requested URL.</p>'] | {'message': 'The method is not allowed for the requested URL.', 'success': False} |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

## H1061 / actual-YAML-syntax-error

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | "订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 2 行，第 1 列\n出错内容：[文件末尾]\n原始诊断：while parsing a flow node: expected the node content, but found '<stream end>'" | '订阅「订阅甲🧪」准备失败：Provider YAML 内容无效\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 1 列\n出错内容：proxies: [\n原始诊断：YAML 语法错误（第 1 行）：yaml: line 1: did not find expected node content' |

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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMjQ4NTIsImlhdCI6MTc5NDkzODQ1Mn0.zkqvfvru7K5R2oimgAhZ-UABXjd853V0IaLllrtTX2s' | None |
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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMjQ4NTIsImlhdCI6MTc5NDkzODQ1Mn0.zkqvfvru7K5R2oimgAhZ-UABXjd853V0IaLllrtTX2s' | None |
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
| `body.token` | missing-in-go | 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4iLCJleHAiOjE3OTUwMjQ4NTIsImlhdCI6MTc5NDkzODQ1Mn0.zkqvfvru7K5R2oimgAhZ-UABXjd853V0IaLllrtTX2s' | None |
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

## X04 / token/literal/"\ud800"

路由：`POST /api/config-token`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | non-ascii-internal-error-text | "'utf-8' codec can't encode character '\\ud800' in position 238: surrogates not allowed" | '请求内容包含无效的 Unicode 字符' |

## X04 / new_domain/literal/"\ud800"

路由：`POST /api/server-domain`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | non-ascii-internal-error-text | "'utf-8' codec can't encode character '\\ud800' in position 18: surrogates not allowed" | '请求内容包含无效的 Unicode 字符' |
