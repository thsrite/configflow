// Test server only. Real UI/API and an isolated database; the Agent transport is synthetic.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const root = process.cwd()
const { chromium } = await import(process.env.CF_PLAYWRIGHT || '/home/sunbroke/rt/private__tmp__configflow-real-env-rebuild__browser-runtime/node_modules/playwright/index.mjs')
const port = '15958'
const base = `http://127.0.0.1:${port}`
const scratch = await mkdtemp(join(tmpdir(), 'cf-final-preview-'))
const evidence = resolve(root, '.ci-results/final-config')
await mkdir(evidence, { recursive: true })
await mkdir(resolve(root, 'doc/img/guide'), { recursive: true })
const source = String.raw`
import hashlib, io, os, tarfile
from pathlib import Path
import yaml
from flask import jsonify, send_from_directory
from werkzeug.serving import run_simple
from backend.common.agent_manager import get_agent_manager
from backend.test_qa_integrity import make_app

app, repo = make_app(Path(os.environ['DATA_DIR']))
repo.update_shared_transaction(lambda shared: shared.update(subscriptions=[], nodes=[], rule_library=[
    {'id': 'local', 'name': '家庭直连', 'source_type': 'content', 'behavior': 'domain', 'format': 'text', 'content': '+.home.example\n+.lan.example', 'enabled': True}]))
repo.update_profile_transaction('default', lambda profile: profile.update(
    mihomo={'custom_config': 'mixed-port: 7890\nmode: rule\nlog-level: info\ndns: {enable: false}\n'},
    proxy_groups=[], rule_configs=[{'id': 'direct', 'name': '家庭直连', 'itemType': 'ruleset', 'library_rule_id': 'local', 'policy': 'DIRECT', 'enabled': True}]))
manager = get_agent_manager()
ident = manager.register_agent({'name': '客厅网关 · Mihomo', 'host': '192.0.2.10', 'service_type': 'mihomo', 'profile_id': 'default', 'version': '1.8.53-go'})['id']
uploads = []
def transport(agent, method, path, **kwargs):
    if path == 'capabilities':
        return {'success': True, 'deployment_protocols': [1], 'final_config_protocols': [1], 'service_type': 'mihomo', 'config_path': 'config.yaml', 'mihomo_resources': []}
    if path == 'config/final-preview':
        document = yaml.safe_load(kwargs['json']['content'])
        document['profile'] = {'store-selected': True}
        for provider in document.get('rule-providers', {}).values():
            provider['path'] = provider['path'].removeprefix('./')
        return {'success': True, 'content': yaml.safe_dump(document, allow_unicode=True)}
    if path == 'deployments':
        with tarfile.open(fileobj=io.BytesIO(kwargs['data']), mode='r:gz') as archive:
            uploads.append({name.removeprefix('files/'): hashlib.sha256(archive.extractfile(name).read()).hexdigest() for name in archive.getnames() if name.startswith('files/')})
    return {'success': True, 'status': 'succeeded', 'config_version': '12345678'}
manager._deployment_request = transport
@app.route('/lab/edit', methods=['POST'])
def edit():
    repo.update_profile_transaction('default', lambda profile: profile['mihomo'].update(custom_config=profile['mihomo']['custom_config'].replace('info', 'debug')))
    return jsonify(success=True)
@app.route('/lab/uploads')
def uploaded():
    return jsonify(uploads)
@app.route('/lab/add-agent', methods=['POST'])
def add_agent():
    return jsonify(manager.register_agent({'name': '办公网关 · Mihomo', 'host': '192.0.2.11', 'service_type': 'mihomo', 'profile_id': 'default', 'version': '1.8.53-go'}))
@app.route('/lab/long-config', methods=['POST'])
def long_config():
    def edit(profile):
        config = yaml.safe_load(profile['mihomo']['custom_config'])
        config['hosts'] = {f'host-{index}.test': '192.0.2.1' for index in range(200)}
        profile['mihomo']['custom_config'] = yaml.safe_dump(config)
    repo.update_profile_transaction('default', edit)
    return jsonify(success=True)
frontend = Path.cwd() / 'frontend/dist'
@app.route('/')
@app.route('/<path:path>')
def index(path=''):
    return send_from_directory(frontend, path if (frontend / path).is_file() else 'index.html')
run_simple('127.0.0.1', ${port}, app, threaded=True)
`
const server = spawn('/home/sunbroke/cf-test/.venv/bin/python', ['-c', source], { cwd: root, env: { ...process.env, DATA_DIR: scratch }, stdio: ['ignore', 'pipe', 'pipe'] })
let logs = '', browser
server.stdout.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
server.stderr.on('data', chunk => { logs = (logs + chunk).slice(-10000) })
const errors = []
try {
  for (let i = 0; i < 80; i++) {
    if (server.exitCode !== null) throw new Error(logs)
    try { if ((await fetch(`${base}/api/agents`)).ok) break } catch {}
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  browser = await chromium.launch({ headless: true })
  async function open(viewport, theme, { chooseTarget = true, motion = false, reducedMotion = true, record = false } = {}) {
    const context = await browser.newContext({ viewport, reducedMotion: reducedMotion ? 'reduce' : 'no-preference', colorScheme: theme,
      ...(record ? { recordVideo: { dir: join(evidence, 'video'), size: viewport } } : {}) })
    await context.addInitScript(mode => localStorage.setItem('configflow-theme', mode), theme)
    if (motion) await context.addInitScript(() => localStorage.setItem('configflow-preferences', JSON.stringify({ motion: true })))
    const page = await context.newPage()
    page.on('pageerror', err => errors.push(err.message))
    await page.goto(`${base}/generate`)
    await page.waitForFunction(() => {
      const selector = document.querySelector('#generate-destination')
      return selector && !selector.textContent.includes('正在读取')
    })
    if (chooseTarget) {
      await page.getByRole('combobox', { name: '配置用途' }).click()
      await page.getByRole('option', { name: '客厅网关 · Mihomo', exact: true }).click()
    }
    return page
  }
  async function generate(page) {
    const response = page.waitForResponse(res => res.url().endsWith('/generate-config'))
    await page.getByRole('button', { name: '生成', exact: true }).click()
    const res = await response
    const result = await res.json()
    assert.equal(res.status(), 200, JSON.stringify(result))
    await page.locator('.gen-code').filter({ hasText: 'store-selected' }).waitFor()
    await page.waitForFunction(() => [...document.querySelectorAll('button')].some(button => button.textContent.trim() === '推送' && !button.disabled))
    return result
  }
  const page = await open({ width: 1280, height: 900 }, 'light', { chooseTarget: false })
  assert.match(await page.getByRole('combobox', { name: '配置用途' }).textContent(), /客厅网关/)
  const first = await generate(page)
  assert.ok(first.files.length > 1, 'Rule file is in the generated artifact')
  await page.locator('[data-sonner-toast]').first().waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
  await page.screenshot({ path: resolve(root, 'doc/img/guide/final-agent-config.png'), animations: 'disabled' })
  await page.getByRole('combobox', { name: '预览文件' }).click()
  const rule = first.files.find(f => f.role === 'rule')
  await page.getByRole('option').filter({ hasText: rule.path }).click()
  await page.locator('.gen-code').filter({ hasText: 'home.example' }).waitFor()
  await fetch(`${base}/lab/edit`, { method: 'POST' })
  await page.getByRole('button', { name: '推送', exact: true }).click()
  await page.getByRole('alert').filter({ hasText: '重新生成' }).first().waitFor()
  assert.equal(await page.getByRole('button', { name: '推送', exact: true }).isEnabled(), false)
  assert.deepEqual(await (await fetch(`${base}/lab/uploads`)).json(), [])
  const current = await generate(page)
  const uploaded = page.waitForResponse(res => res.url().endsWith('/push-config'))
  await page.getByRole('button', { name: '推送', exact: true }).click()
  assert.equal((await uploaded).status(), 200)
  assert.deepEqual((await (await fetch(`${base}/lab/uploads`)).json())[0], Object.fromEntries(current.files.map(file => [file.path, file.sha256])))
  const second = await (await fetch(`${base}/lab/add-agent`, { method: 'POST' })).json()
  const multiple = await open({ width: 1280, height: 900 }, 'light', { chooseTarget: false })
  assert.match(await multiple.getByRole('combobox', { name: '配置用途' }).textContent(), /请选择/)
  assert.equal(await multiple.getByRole('button', { name: '生成', exact: true }).isEnabled(), false)
  await multiple.getByRole('combobox', { name: '配置用途' }).click()
  await multiple.getByRole('option', { name: '办公网关 · Mihomo', exact: true }).waitFor()
  await multiple.screenshot({ path: resolve(root, 'doc/img/guide/generate-destination-choice.png'), animations: 'disabled' })
  await multiple.getByRole('option', { name: '办公网关 · Mihomo', exact: true }).click()
  assert.equal((await generate(multiple)).agent_id, second.id)
  await multiple.reload()
  await multiple.waitForFunction(() => document.querySelector('#generate-destination')?.textContent.includes('办公网关'))
  assert.equal(await multiple.getByRole('button', { name: '生成', exact: true }).isEnabled(), true)

  await fetch(`${base}/lab/long-config`, { method: 'POST' })
  // Reproduce the default appearance: no saved motion opt-in, no OS reduction.
  const animated = await open({ width: 1440, height: 1050 }, 'light', { reducedMotion: false, record: true })
  assert.equal(await animated.evaluate(() => document.documentElement.dataset.motion), 'off')
  assert.equal(await animated.evaluate(() => localStorage.getItem('configflow-preferences')), null)
  await animated.evaluate(() => {
    window.replaySamples = []
    window.replayTimer = setInterval(() => {
      const steps = [...document.querySelectorAll('.gen-step')]
      window.replaySamples.push({ stage: steps.findIndex(step => step.classList.contains('run')),
        shown: document.querySelectorAll('.gen-line').length, done: steps.filter(step => step.classList.contains('done')).length })
    }, 16)
  })
  const response = animated.waitForResponse(res => res.url().endsWith('/generate-config'))
  await animated.getByRole('button', { name: '生成', exact: true }).click()
  const rendered = await (await response).json()
  const total = rendered.content.trimEnd().split('\n').length
  await animated.waitForFunction(count => {
    const shown = document.querySelectorAll('.gen-code .gen-line').length
    return shown > 0 && shown < count
  }, total)
  assert.equal(await animated.getByRole('button', { name: '推送', exact: true }).isEnabled(), false)
  await animated.waitForFunction(() => document.querySelectorAll('.gen-step.done').length === 2)
  assert.match(await animated.locator('[data-result-replay]').textContent(), /配置已生成/)
  await animated.screenshot({ path: resolve(root, 'doc/img/guide/generate-result-replay.png') })
  await animated.waitForFunction(() => [...document.querySelectorAll('button')].some(button => button.textContent.trim() === '推送' && !button.disabled))
  assert.equal(await animated.locator('.gen-line').count(), total)
  const samples = await animated.evaluate(() => { clearInterval(window.replayTimer); return window.replaySamples })
  assert.deepEqual([...new Set(samples.filter(item => item.stage >= 0).map(item => item.stage))], [0, 1, 2, 3, 4])
  assert.ok(samples.some(item => item.shown > 0 && item.shown < total && item.done < 4), 'Code appears while earlier steps are replaying')
  await writeFile(join(evidence, 'replay-samples.json'), JSON.stringify(samples, null, 2))
  const animatedUpload = animated.waitForResponse(res => res.url().endsWith('/push-config'))
  await animated.getByRole('button', { name: '推送', exact: true }).click()
  assert.equal((await animatedUpload).status(), 200)
  assert.deepEqual((await (await fetch(`${base}/lab/uploads`)).json()).at(-1), Object.fromEntries(rendered.files.map(file => [file.path, file.sha256])))
  // Skip a second playback and confirm the exact final file is shown immediately.
  await animated.getByRole('button', { name: '生成', exact: true }).click()
  await animated.getByRole('button', { name: '立即显示', exact: true }).click()
  await animated.waitForFunction(() => document.querySelectorAll('.gen-step.done').length === 5)
  assert.equal(await animated.locator('.gen-line').count(), total)
  const video = animated.video()
  await animated.context().close()
  await video.saveAs(join(evidence, 'generation-replay.webm'))
  for (const theme of ['light', 'dark']) {
    const phone = await open({ width: 375, height: 812 }, theme)
    await generate(phone)
    await phone.locator('[data-sonner-toast]').first().waitFor({ state: 'detached', timeout: 15000 }).catch(() => {})
    assert.equal(await phone.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    await phone.screenshot({ path: join(evidence, `mobile-${theme}.png`), animations: 'disabled' })
  }
  assert.deepEqual(errors, [])
  await writeFile(join(evidence, 'result.json'), JSON.stringify({ passed: true, checks: ['single Agent default', 'multiple Agents require selection', 'remember Agent after reload', 'default static appearance replays all five steps', 'progressive final-result text', 'skip playback', 'system reduced motion', 'final content', 'rule file preview', 'stale rejection', 'upload file hashes', '375px light/dark'], files: current.files.map(f => f.path) }, null, 2))
  console.log('PASS: destination defaults/restoration, animation, exact upload and mobile light/dark')
} catch (error) {
  console.error(logs.split('\n').slice(-20).join('\n'))
  throw error
} finally {
  await browser?.close()
  server.kill('SIGTERM')
  await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve))
  await rm(scratch, { recursive: true, force: true })
}
