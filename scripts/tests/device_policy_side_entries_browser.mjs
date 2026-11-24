// Run only on the test server, after building frontend/. Uses synthetic data in a temporary database.
import assert from 'node:assert/strict'
import { execFile, spawn } from 'node:child_process'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'

import { pathToFileURL } from "node:url"
import { promisify } from "node:util"

const root = process.cwd()
const { parse, stringify } = await import(pathToFileURL(resolve(root, 'frontend/node_modules/yaml/dist/index.js')))
const { chromium } = await import(process.env.CF_PLAYWRIGHT || 'playwright')
const port = process.env.CF_SIDE_ENTRIES_PORT || '15947'
const apiPort = process.env.CF_SIDE_ENTRIES_API_PORT || '15948'
const base = `http://127.0.0.1:${port}`
const api = `http://127.0.0.1:${apiPort}`
const scratch = await mkdtemp(join(tmpdir(), 'cf-side-entries-'))
const shots = resolve(root, process.env.CF_SHOTS || 'doc/img/guide')
const evidence = resolve(root, '.ci-results/device-policy-side-entries')
let server, browser
let logs = ''

async function request(path, method, body) {
  const response = await fetch(api + path, {
    method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  })
  assert.ok(response.ok, `${method} ${path}: ${response.status} ${await response.clone().text()}`)
  return response.json()
}

function chain(prefix, listen, upstream, recorder = false) {
  const rules = recorder ? [{ exec: '$query_recorder' }] : []
  rules.push({ exec: '$' + prefix + '_forward' }, { matches: ['!has_resp'], exec: 'reject 2' }, { exec: 'accept' })
  return [
    { tag: prefix + '_forward', type: 'forward', args: { upstreams: [{ addr: upstream }] } },
    { tag: prefix + '_sequence', type: 'sequence', args: rules },
    ...['udp', 'tcp'].map(proto => ({
      tag: prefix + '_' + proto, type: proto + '_server', args: { listen, entry: prefix + '_sequence' },
    })),
  ]
}

try {
  await mkdir(evidence, { recursive: true })
  await mkdir(shots, { recursive: true })
  const binary = process.env.CF_SIDE_ENTRIES_SERVER_BIN || join(scratch, 'configflow')
  if (!process.env.CF_SIDE_ENTRIES_SERVER_BIN) {
    await promisify(execFile)('go', ['build', '-o', binary, './cmd/configflow'], { cwd: resolve(root, 'server') })
  }
  server = spawn(binary, ['-frontend', resolve(root, 'frontend/dist')], {
    cwd: root,
    env: { ...process.env, DATA_DIR: join(scratch, 'data'), ADMIN_USERNAME: '', ADMIN_PASSWORD: '',
      LOG_FILE: join(scratch, 'panel.log'), CONFIGFLOW_BACKEND_HOST: '127.0.0.1',
      CONFIGFLOW_BACKEND_PORT: apiPort, CONFIGFLOW_WEB_PORT: port },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  server.stdout.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
  server.stderr.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
  let ready = false
  for (let i = 0; i < 80; i++) {
    if (server.exitCode !== null) throw new Error(logs)
    try { if ((await fetch(api + '/api/auth/status')).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  assert.ok(ready, 'Isolated preview did not start')

  await request('/api/nodes/latency/settings', 'POST', { dns_servers: ['198.51.100.2:15353'] })
  const { settings } = await request('/api/oxidns/editor', 'POST', { settings: { query_recorder: { enabled: true } } })
  const document = parse(settings.native_config)
  document.plugins.push(...chain('node_test_real', '198.51.100.2:15353', 'udp://198.51.100.53', true))
  document.plugins.push(...chain('debug_dns', '198.51.100.2:5300', 'udp://198.51.100.54'))
  settings.native_config = stringify(document)
  await request('/api/oxidns/settings', 'POST', settings)
  for (const [ip, name, mode] of [['198.51.100.21', '客厅电视', 'direct'], ['198.51.100.22', '书房电脑', 'smart']]) {
    await request('/api/client-devices?profile=default', 'PUT', {
      profile_id: 'default', ip, name, mode, previous_name: '', previous_mode: '', base_url: base,
    })
  }
  const state = await request('/api/client-devices?profile=default', 'GET')
  assert.equal(state.editable, true, state.reason)
  assert.deepEqual(state.side_entries, [{ tags: ['debug_dns_udp', 'debug_dns_tcp'], listen: '198.51.100.2:5300', entry: 'debug_dns_sequence' }])

  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 }, reducedMotion: 'reduce' })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(String(error)))
  await page.goto(`${base}/client-devices`)
  const note = page.locator('[data-side-entries]')
  await note.waitFor()
  const text = await note.innerText()
  assert.match(text, /以下入口没有经过主处理链，设备策略对它们不生效/)
  assert.match(text, /debug_dns_udp、debug_dns_tcp（198\.51\.100\.2:5300）/)
  assert.doesNotMatch(text, /node_test_real/)
  assert.equal(await page.locator('[aria-label="198.51.100.21 DNS 策略"]').first().isDisabled(), false)
  await page.screenshot({ path: join(shots, 'client-devices-side-entries.png'), animations: 'disabled' })

  await page.emulateMedia({ colorScheme: 'dark' })
  await page.setViewportSize({ width: 375, height: 812 })
  await page.reload()
  await note.waitFor()
  await page.screenshot({ path: join(evidence, 'mobile-dark.png'), animations: 'disabled' })
  const overflow = await page.evaluate(() => document.scrollingElement.scrollWidth > window.innerWidth)
  assert.equal(overflow, false, 'Mobile page must not overflow horizontally')
  assert.deepEqual(errors, [])
  console.log('PASS: side entry note, node-test entry hidden, policies editable, mobile without overflow')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await browser?.close()
  if (server && server.exitCode === null && server.signalCode === null) {
    server.kill('SIGTERM')
    await new Promise(resolve => server.once('exit', resolve))
  }
  await rm(scratch, { recursive: true, force: true })
}
