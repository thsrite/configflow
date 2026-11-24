#!/usr/bin/env node
// Run ONLY on the test server, from the repository root, after frontend/node_modules
// is prepared (e.g. cf-test typecheck). Starts the Go panel (server/cmd/configflow)
// with an isolated temporary DATA_DIR and synthetic demo data; no real config or
// installed Agent is used.
// Example: rt --no-sync -e PLAYWRIGHT_MODULE=... --back doc/img/guide -- node scripts/docs/capture-guide.mjs
// Optional env:
//   DOCS_WEB_PORT      fixed Vite port (default: random free port on 0.0.0.0)
//   DOCS_API_PORT      fixed Go panel port (default: random free port on 0.0.0.0)
//   DOCS_SERVER_BIN    pre-built configflow binary (default: go build into temp dir)
//   PLAYWRIGHT_MODULE  path to playwright/index.mjs
//   CAPTURE_ONLY       comma-separated screenshot names (without .jpg)
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises'
import { spawn, execFile } from 'node:child_process'
import { createServer as tcpServer } from 'node:net'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

const root = process.cwd()
const playwrightPath = process.env.PLAYWRIGHT_MODULE
// CAPTURE_ONLY=name[,name] re-captures just those images and updates their report entries.
const only = new Set((process.env.CAPTURE_ONLY || '').split(',').map(name => name.trim()).filter(Boolean))
const output = resolve(root, 'doc/img/guide')
const temporary = await mkdtemp(join(tmpdir(), 'configflow-docs-'))
const dataDir = join(temporary, 'data')
await mkdir(dataDir, { recursive: true })
const captures = []
const browserErrors = []
let backend, vite, browser
let backendLog = ''

async function pickFreePort() {
  return await new Promise((yes, no) => {
    const server = tcpServer()
    server.once('error', no)
    server.listen(0, '0.0.0.0', () => {
      const addr = server.address()
      const port = typeof addr === 'object' && addr ? addr.port : 0
      server.close(err => (err ? no(err) : yes(port)))
    })
  })
}

async function assertPortFree(port) {
  await new Promise((yes, no) => {
    const server = tcpServer()
    server.once('error', no)
    server.listen(port, '0.0.0.0', () => server.close(yes))
  })
}

async function resolvePort(envName) {
  const raw = process.env[envName]
  if (raw && raw.trim() !== '') {
    const port = Number(raw)
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error(`Invalid ${envName}: ${raw}`)
    }
    await assertPortFree(port)
    return port
  }
  return await pickFreePort()
}

async function buildGoServer() {
  if (process.env.DOCS_SERVER_BIN) {
    return resolve(process.env.DOCS_SERVER_BIN)
  }
  const binary = join(temporary, 'configflow')
  await execFileAsync('go', ['build', '-o', binary, './cmd/configflow'], {
    cwd: resolve(root, 'server'),
    env: process.env,
    maxBuffer: 10 * 1024 * 1024,
  })
  return binary
}

async function waitFor(url) {
  for (let attempt = 0; attempt < 150; attempt++) {
    try { if ((await fetch(url)).ok) return } catch {}
    if (backend?.exitCode !== null) throw new Error(`Backend exited (${backend.exitCode}): ${backendLog}`)
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  throw new Error(`Timed out waiting for ${url}\n${backendLog}`)
}

const demoServerDomain = 'http://192.0.2.10:5010'

try {
  const webPort = await resolvePort('DOCS_WEB_PORT')
  const apiPort = await resolvePort('DOCS_API_PORT')
  await mkdir(output, { recursive: true })

  const serverBin = await buildGoServer()
  backend = spawn(serverBin, [], {
    cwd: resolve(root, 'server'),
    env: {
      ...process.env,
      DATA_DIR: dataDir,
      LOG_FILE: join(temporary, 'api.log'),
      LOG_LEVEL: 'WARNING',
      ADMIN_USERNAME: '',
      ADMIN_PASSWORD: '',
      JWT_SECRET_KEY: 'isolated-documentation-demo-only',
      CONFIGFLOW_BACKEND_HOST: '0.0.0.0',
      CONFIGFLOW_BACKEND_PORT: String(apiPort),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  backend.stdout.on('data', chunk => { backendLog += chunk })
  backend.stderr.on('data', chunk => { backendLog += chunk })
  await waitFor(`http://127.0.0.1:${apiPort}/api/auth/status`)

  const domain = await fetch(`http://127.0.0.1:${apiPort}/api/server-domain`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ new_domain: demoServerDomain }),
  })
  if (!domain.ok) throw new Error(`server-domain: ${domain.status} ${await domain.text()}`)

  const { createServer } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/vite/dist/node/index.js')))
  vite = await createServer({
    root: resolve(root, 'frontend'),
    configFile: resolve(root, 'frontend/vite.config.mts'),
    server: {
      host: '0.0.0.0',
      port: webPort,
      strictPort: true,
      proxy: { '/api': { target: `http://127.0.0.1:${apiPort}`, changeOrigin: true } },
    },
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

  async function captureOn(targetPage, name, scene, locator, extra = {}) {
    if (only.size && !only.has(name)) return
    await targetPage.evaluate(() => document.fonts.ready)
    await targetPage.evaluate(() => document.activeElement?.blur())
    await targetPage.waitForTimeout(350)
    const path = join(output, `${name}.jpg`)
    await (locator || targetPage).screenshot({ path, type: 'jpeg', quality: 82, animations: 'disabled', ...extra })
    captures.push({
      file: `${name}.jpg`,
      scene,
      viewport: targetPage.viewportSize(),
      actualUI: true,
      mock: false,
      note: '隔离临时数据库（Go 面板）；地址与名称为合成演示数据；未执行设备安装、配置发布或 HA 配对。',
    })
    console.log(`Captured ${name}`)
  }

  const capture = (name, scene, locator, extra) => captureOn(page, name, scene, locator, extra)

  await page.goto(`${base}/setup`)
  await page.locator('#setup-name').waitFor()
  await capture('panel-header-docs', '管理面板顶栏：右侧查看文档入口', page.locator('header').first())
  await capture('first-run-router', '首次向导：默认 Mihomo + OxiDNS 分流', page.locator('main .max-w-5xl'))
  await page.locator('input[name="setup-goal"][value="gateway"]').check()
  await capture('first-run-gateway', '首次向导：选择 Linux 网关', page.locator('main .max-w-5xl'))

  await page.goto(`${base}/system-settings`)
  await page.locator('#server-domain').waitFor()
  const serviceCard = page.locator('section').filter({ has: page.locator('#server-domain') })
  await capture('panel-service-settings', '系统设置：演示服务域名（RFC 5737 地址）；独立开发服务不支持容器监听端口设置', serviceCard)

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

  // Import a configuration-only backup containing synthetic demo Agents (no run records)
  // to trigger the restore banner and per-card "待重新发布" markers.
  const needRestored = !only.size || only.has('agents-backup-restored-desktop-light') || only.has('agents-backup-restored-mobile-dark')
  if (needRestored) {
    const exportedRes = await fetch(`http://127.0.0.1:${apiPort}/api/config/export`)
    if (!exportedRes.ok) throw new Error(`config/export: ${exportedRes.status} ${await exportedRes.text()}`)
    const exported = await exportedRes.json()
    const profileIds = Object.keys(exported.profiles || {})
    const defaultProfileId = profileIds[0] || 'default'
    exported.system = exported.system || {}
    exported.system.agents = [
      {
        id: 'demo-agent-mihomo',
        name: 'demo-mihomo',
        host: '192.0.2.21',
        port: 18080,
        service_type: 'mihomo',
        deployment_method: 'shell',
        enabled: true,
        profile_id: defaultProfileId,
        token: 'demo-synthetic-token-mihomo',
      },
      {
        id: 'demo-agent-oxidns',
        name: 'demo-oxidns',
        host: '192.0.2.22',
        port: 18081,
        service_type: 'oxidns',
        deployment_method: 'docker',
        enabled: true,
        profile_id: defaultProfileId,
        token: 'demo-synthetic-token-oxidns',
      },
    ]
    const importedRes = await fetch(`http://127.0.0.1:${apiPort}/api/config/import`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(exported),
    })
    const importedText = await importedRes.text()
    if (!importedRes.ok) throw new Error(`config/import: ${importedRes.status} ${importedText}`)
    if (!importedText.includes('运行记录未包含在备份里')) {
      throw new Error(`config/import did not report restore marker: ${importedText}`)
    }

    await page.setViewportSize({ width: 1280, height: 1000 })
    await page.goto(`${base}/agents`)
    await page.locator('[data-state-restored]').waitFor()
    await page.locator('[data-agent-state-restored]').first().waitFor()
    await capture('agents-backup-restored-desktop-light', 'Agent 页：从不含运行记录的备份恢复后（桌面浅色），顶部横幅与卡片待重新发布标记', page.locator('main'))

    const mobilePage = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 })
    mobilePage.on('pageerror', error => browserErrors.push(error.message))
    await mobilePage.addInitScript(() => { localStorage.setItem('configflow-theme', 'dark') })
    try {
      await mobilePage.goto(`${base}/agents`)
      await mobilePage.locator('[data-state-restored]').waitFor()
      await mobilePage.locator('[data-agent-state-restored]').first().waitFor()
      await captureOn(mobilePage, 'agents-backup-restored-mobile-dark', 'Agent 页：从不含运行记录的备份恢复后（手机深色），顶部横幅与卡片待重新发布标记')
    } finally {
      await mobilePage.close()
    }
  }

  let report = {
    capturedAt: new Date().toISOString(),
    source: '当前仓库真实 Vue 前端 + Go 面板（server/cmd/configflow）',
    mock: false,
    captures,
    browserErrors,
  }
  if (only.size) {
    const missing = [...only].filter(name => !captures.some(item => item.file === `${name}.jpg`))
    if (missing.length) throw new Error(`CAPTURE_ONLY names not captured: ${missing.join(', ')}`)
    report = JSON.parse(await readFile(join(output, 'capture-report.json'), 'utf8'))
    report.source = '当前仓库真实 Vue 前端 + Go 面板（server/cmd/configflow）'
    const at = new Date().toISOString()
    const seen = new Set()
    report.captures = report.captures.map(item => {
      const fresh = captures.find(entry => entry.file === item.file)
      if (!fresh) return item
      seen.add(fresh.file)
      return { ...fresh, recapturedAt: at }
    })
    for (const fresh of captures) {
      if (!seen.has(fresh.file)) report.captures.push({ ...fresh, recapturedAt: at })
    }
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
