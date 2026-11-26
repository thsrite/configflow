import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DockerAgentUpdateDialog from '@/components/agents/DockerAgentUpdateDialog.vue'
import { notify } from '@/lib/feedback'
import { systemApi } from '@/api'

vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@/api', () => ({ systemApi: { getVersion: vi.fn() } }))
let wrapper: ReturnType<typeof mount>
const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const execCommand = Object.getOwnPropertyDescriptor(document, 'execCommand')
const writeText = vi.fn()
beforeEach(() => {
 vi.clearAllMocks()
 vi.mocked(systemApi.getVersion).mockResolvedValue({ data: { version: '3.0.0', agent_image: 'thsrite/config-flow-agent:3.0.0' } } as any)
 writeText.mockResolvedValue(undefined)
 Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
 wrapper = mount(DockerAgentUpdateDialog, {
  props: { agent: { id: 'docker-agent', name: 'Docker Agent', host: '192.0.2.10' } as any },
  attachTo: document.body
 })
})
afterEach(() => {
 wrapper.unmount()
 document.body.innerHTML = ''
 if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
 else delete (navigator as any).clipboard
 if (execCommand) Object.defineProperty(document, 'execCommand', execCommand)
 else delete (document as any).execCommand
})
async function click(label: string) {
 await flushPromises()
 const button = Array.from(document.querySelectorAll('button')).find(b => b.textContent?.trim() === label)
 expect(button).toBeTruthy()
 button!.click()
 await flushPromises()
}
async function input(id: string, value: string) {
 const element = document.getElementById(id) as HTMLInputElement
 element.value = value
 element.dispatchEvent(new Event('input', { bubbles: true }))
 await flushPromises()
}

it('copies commands scoped to the chosen service, and refuses shell fragments', async () => {
 await flushPromises()
 await input('docker-update-service', 'my-dns')
 await click('复制 Compose 命令')
 expect(writeText).toHaveBeenCalledWith("docker compose pull 'my-dns' &&\ndocker compose up -d --no-deps 'my-dns'")
 for (const invalid of ['', 'agent; echo injected', '$(whoami)', '--all', 'agent other']) {
  await input('docker-update-service', invalid)
  expect(document.querySelector('[role="alert"]')).not.toBeNull()
  expect(document.body.textContent).not.toContain('复制 Compose 命令')
 }
 expect(writeText).toHaveBeenCalledTimes(1)
})

it('Docker Run offers an editable pull command and preserves manual reconstruction requirements', async () => {
 await flushPromises()
 const option = document.querySelector('[role="radio"][value="run"]') as HTMLElement
 option.click()
 await flushPromises()
 expect(document.body.textContent).toContain('必须复用原宿主机目录或原数据卷')
 await input('docker-update-image', 'registry.example:5000/team/agent:v2')
 await click('复制拉取命令')
 expect(writeText).toHaveBeenCalledWith("docker pull 'registry.example:5000/team/agent:v2'")
 expect(document.querySelector('pre')?.textContent).not.toContain('docker run')
 await input('docker-update-image', 'agent:latest; rm -rf /')
 expect(document.body.textContent).not.toContain('复制拉取命令')
})

it('supports clipboard fallback on HTTP deployments and reports copy failure honestly', async () => {
 writeText.mockRejectedValue(new Error('HTTP clipboard unavailable'))
 const fallback = vi.fn().mockReturnValueOnce(true).mockReturnValueOnce(false)
 Object.defineProperty(document, 'execCommand', { configurable: true, value: fallback })
 await click('复制 Compose 命令')
 expect(fallback).toHaveBeenCalledWith('copy')
 expect(notify.success).toHaveBeenCalledTimes(1)
 await click('复制 Compose 命令')
 expect(notify.success).toHaveBeenCalledTimes(1)
 expect(notify.error).toHaveBeenCalledWith('复制失败，请手动选择并复制命令')
 expect(document.querySelector('textarea')).toBeNull()
})

it('copies within the real dialog focus scope when the Clipboard API is absent', async () => {
 Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
 const fallback = vi.fn(() => {
  const field = document.activeElement as HTMLTextAreaElement
  expect(field.closest('[role="dialog"]')).not.toBeNull()
  expect(field.value.slice(field.selectionStart, field.selectionEnd)).toContain("docker compose pull 'agent'")
  return true
 })
 Object.defineProperty(document, 'execCommand', { configurable: true, value: fallback })
 await click('复制 Compose 命令')
 expect(fallback).toHaveBeenCalledOnce()
 expect(notify.success).toHaveBeenCalledWith('命令已复制，请到 Docker 宿主机执行')
 expect(notify.error).not.toHaveBeenCalled()
})

async function chooseRun() {
 await flushPromises()
 ;(document.querySelector('[role="radio"][value="run"]') as HTMLElement).click()
 await flushPromises()
}

it('defaults the target image to the Agent paired with this panel, never the frozen latest', async () => {
 await chooseRun()
 expect((document.getElementById('docker-update-image') as HTMLInputElement).value).toBe('thsrite/config-flow-agent:3.0.0')
 await click('复制拉取命令')
 expect(writeText).toHaveBeenCalledWith("docker pull 'thsrite/config-flow-agent:3.0.0'")
})

it('falls back to the major-version tag when the panel version cannot be read', async () => {
 wrapper.unmount()
 vi.mocked(systemApi.getVersion).mockRejectedValue(new Error('offline'))
 wrapper = mount(DockerAgentUpdateDialog, {
  props: { agent: { id: 'docker-agent', name: 'Docker Agent', host: '192.0.2.10' } as any },
  attachTo: document.body
 })
 await chooseRun()
 expect((document.getElementById('docker-update-image') as HTMLInputElement).value).toBe('thsrite/config-flow-agent:3')
})
