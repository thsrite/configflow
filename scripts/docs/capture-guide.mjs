#!/usr/bin/env node
// Run ONLY on the test server, from the repository root, after cf-test-remote
// has prepared frontend/node_modules. No installed Agent or real config is used.
// Example: rt --back doc/img/guide -- node scripts/docs/capture-guide.mjs
// PLAYWRIGHT_MODULE may point to another server-side Playwright installation.
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { createServer as tcpServer } from 'node:net'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { pathToFileURL } from 'node:url'

const root = process.cwd()
const webPort = Number(process.env.DOCS_WEB_PORT || 15671)
const apiPort = Number(process.env.DOCS_API_PORT || 15672)
const python = process.env.DOCS_PYTHON || '/home/sunbroke/cf-test/.venv/bin/python'
const playwrightPath = process.env.PLAYWRIGHT_MODULE
// CAPTURE_ONLY=name[,name] re-captures just those images and updates their report entries.
const only = new Set((process.env.CAPTURE_ONLY || '').split(',').map(name => name.trim()).filter(Boolean))
const output = resolve(root, 'doc/img/guide')
const temporary = await mkdtemp(join(tmpdir(), 'configflow-docs-'))
const captures = []
const browserErrors = []
let backend, vite, browser
let backendLog = ''
async function assertPortFree(port) {
  await new Promise((yes, no) => {
    const server = tcpServer()
    server.once('error', no)
    server.listen(port, '0.0.0.0', () => server.close(yes))
  })
}
async function waitFor(url) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try { if ((await fetch(url)).ok) return } catch {}
    if (backend?.exitCode !== null) throw new Error(`Backend exited: ${backendLog}`)
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  throw new Error(`Timed out waiting for ${url}\n${backendLog}`)
}
try {
  await assertPortFree(webPort)
  await assertPortFree(apiPort)
  await mkdir(output, { recursive: true })
  const launcher = `from pathlib import Path
import json, os
from backend.common.config import get_default_config, DATA_DIR
config = get_default_config()
config['system_config']['server_domain'] = 'http://192.168.1.10:5010'
Path(DATA_DIR, 'config.json').write_text(json.dumps(config), encoding='utf-8')
from backend.app import app
app.run(host='0.0.0.0', port=int(os.environ['DOCS_API_PORT']), debug=False, use_reloader=False)
`
  backend = spawn(python, ['-c', launcher], {
    cwd: root,
    env: { ...process.env, DATA_DIR: temporary, LOG_FILE: join(temporary, 'api.log'),
      LOG_LEVEL: 'WARNING', ADMIN_USERNAME: '', ADMIN_PASSWORD: '',
      JWT_SECRET_KEY: 'isolated-documentation-demo-only', DOCS_API_PORT: String(apiPort),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  backend.stdout.on('data', chunk => { backendLog += chunk })
  backend.stderr.on('data', chunk => { backendLog += chunk })
  await waitFor(`http://127.0.0.1:${apiPort}/api/auth/status`)
  // The SQLite-era backend ignores the legacy config.json above; set the demo address through the API.
  const domain = await fetch(`http://127.0.0.1:${apiPort}/api/server-domain`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ new_domain: 'http://192.168.1.10:5010' }),
  })
  if (!domain.ok) throw new Error(`server-domain: ${domain.status} ${await domain.text()}`)
  const { createServer } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/vite/dist/node/index.js')))
  vite = await createServer({
    root: resolve(root, 'frontend'), configFile: resolve(root, 'frontend/vite.config.mts'),
    server: { host: '0.0.0.0', port: webPort, strictPort: true,
      proxy: { '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true } } },
  })
  await vite.listen()
  let chromium
  try {
    ({ chromium } = await (playwrightPath ? import(pathToFileURL(playwrightPath)) : import('playwright')))
  } catch (error) {
    throw new Error('无法加载 Playwright。请在测试服务器安装 playwright@1.64.1 和 Chromium，并通过 PLAYWRIGHT_MODULE 指定其 index.mjs；完整命令见 doc/img/guide/README.md。', { cause: error })
  }
  browser = await chromium.launch({ headless: true })
  const page = await browser.newPage({ viewport: { width: 1280, height: 1700 }, deviceScaleFactor: 1 })
  page.on('pageerror', error => browserErrors.push(error.message))
  await page.addInitScript(() => { localStorage.setItem('configflow-theme', 'light') })
  const base = `http://127.0.0.1:${webPort}`
  async function capture(name, scene, locator) {
    if (only.size && !only.has(name)) return
    await page.evaluate(() => document.fonts.ready)
    await page.evaluate(() => document.activeElement?.blur())
    await page.waitForTimeout(350)
    const path = join(output, `${name}.jpg`)
    await (locator || page).screenshot({ path, type: 'jpeg', quality: 82, animations: 'disabled' })
    captures.push({ file: `${name}.jpg`, scene, viewport: page.viewportSize(), actualUI: true, mock: false,
      note: '隔离临时数据库；地址与名称为演示填写；未执行设备安装、配置发布或 HA 配对。' })
    console.log(`Captured ${name}`)
  }
  await page.goto(`${base}/setup`)
  await page.locator('#setup-name').waitFor()
  await capture('panel-header-docs', '管理面板顶栏：右侧查看文档入口', page.locator('header').first())
  await capture('first-run-router', '首次向导：默认 Mihomo + OxiDNS 分流', page.locator('main .max-w-5xl'))
  await page.locator('input[name="setup-goal"][value="gateway"]').check()
  await capture('first-run-gateway', '首次向导：选择 Linux 网关', page.locator('main .max-w-5xl'))

  await page.goto(`${base}/system-settings`)
  await page.locator('#server-domain').waitFor()
  const serviceCard = page.locator('section').filter({ has: page.locator('#server-domain') })
  await capture('panel-service-settings', '系统设置：演示服务域名；独立开发服务不支持容器监听端口设置', serviceCard)

  await page.goto(`${base}/agents`)
  await page.getByRole('button', { name: '生成安装脚本', exact: true }).first().click()
  const dialog = page.getByRole('dialog')
  await page.locator('#agent-name').fill('demo-mihomo')
  await page.locator('#agent-port').fill('18080')
  await capture('agent-shell-single-default', '普通 Shell 单独 Mihomo：两个勾选的默认状态', dialog)
  await page.locator('#agent-install-core').click()
  await page.locator('#agent-skip-forwarding').click()
  await capture('agent-shell-single-choices', '普通 Shell 单独 Mihomo：安装核心并跳过管理 IPv4 转发', dialog)
  await dialog.getByText('Mihomo + OxiDNS 同机', { exact: true }).click()
  await capture('agent-shell-aio', 'Shell 同机双服务：核心安装默认为未勾选', dialog)

  await dialog.getByText('Docker 容器', { exact: true }).click()
  await dialog.getByRole('button', { name: /Docker 高级配置/ }).click()
  await capture('agent-docker-single', 'Docker 单独 Mihomo：默认 bridge 与 Agent 端口', dialog)
  await dialog.getByText('All-in-One（三合一）', { exact: true }).click()
  await capture('agent-docker-aio', 'Docker 同机 Mihomo + OxiDNS：两个 Agent 端口', dialog)
  await dialog.getByText('OxiDNS', { exact: true }).click()
  await dialog.getByText('高可用安装', { exact: true }).click()
  await page.setViewportSize({ width: 1280, height: 2400 })
  const dockerHA = page.locator('[aria-label="OxiDNS Docker HA 安装引导"]')
  await capture('oxidns-docker-ha-prepare', 'Docker HA：准备环境与默认 Host 模式', dockerHA)
  await dockerHA.getByRole('button', { name: '2. 主机 1', exact: true }).click()
  await page.locator('#docker-ha-host-1-ip').fill('192.168.1.21')
  await page.locator('#docker-ha-host-1-data').fill('/volume1/docker/oxidns-a')
  await capture('oxidns-docker-ha-host', 'Docker HA：Host 主机字段，示例地址与目录', dockerHA)
  await dockerHA.getByRole('button', { name: '1. 准备环境', exact: true }).click()
  await dockerHA.getByText('macvlan 独立地址', { exact: true }).click()
  await dockerHA.getByRole('button', { name: '2. 主机 1', exact: true }).click()
  await page.locator('#docker-ha-host-1-ip').fill('192.168.1.31')
  await page.locator('#docker-ha-host-1-parent').fill('eth0')
  await page.locator('#docker-ha-host-1-subnet').fill('192.168.1.0/24')
  await page.locator('#docker-ha-host-1-gateway').fill('192.168.1.1')
  await capture('oxidns-docker-ha-macvlan', 'Docker HA：macvlan 网络字段，示例地址与网卡', dockerHA)
  await dockerHA.getByRole('button', { name: '5. 验证与配对', exact: true }).click()
  await capture('oxidns-ha-pair-empty', 'Docker HA：尚无设备时的验证与配对页', dockerHA)

  await dialog.getByText('Shell 安装', { exact: true }).click()
  await dialog.getByText('单服务', { exact: true }).click()
  await dialog.getByText('OxiDNS', { exact: true }).click()
  await dialog.getByText('高可用安装', { exact: true }).click()
  await page.locator('#agent-name').fill('demo-dns-a')
  await page.locator('#agent-port').fill('18082')
  await capture('oxidns-shell-ha-prepare', 'Shell OxiDNS HA：准备安装，安装核心默认勾选', dialog)
  let report = { capturedAt: new Date().toISOString(), source: '当前仓库真实 Vue 前端 + Flask API',
    mock: false, captures, browserErrors }
  if (only.size) {
    const missing = [...only].filter(name => !captures.some(item => item.file === `${name}.jpg`))
    if (missing.length) throw new Error(`CAPTURE_ONLY names not captured: ${missing.join(', ')}`)
    report = JSON.parse(await readFile(join(output, 'capture-report.json'), 'utf8'))
    const at = new Date().toISOString()
    report.captures = report.captures.map(item => {
      const fresh = captures.find(entry => entry.file === item.file)
      return fresh ? { ...fresh, recapturedAt: at } : item
    })
    report.browserErrors = [...(report.browserErrors || []), ...browserErrors]
  }
  await writeFile(join(output, 'capture-report.json'), JSON.stringify(report, null, 2) + '\n')
  if (browserErrors.length) throw new Error(`Browser errors: ${browserErrors.join('; ')}`)
} catch (error) {
  console.error(backendLog.slice(-4000))
  throw error
} finally {
  await browser?.close()
  await vite?.close()
  if (backend && backend.exitCode === null) {
    backend.kill('SIGTERM')
    await new Promise(resolve => backend.once('exit', resolve))
  }
  await rm(temporary, { recursive: true, force: true })
}
