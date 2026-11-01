import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, markRaw, nextTick, type Ref } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import { Select } from '@/components/ui/select'
import { agentApi, generateApi } from '@/api'
import { copyText } from '@/lib/clipboard'
import { notify } from '@/lib/feedback'
import { usePreferences } from '@/stores/preferences'

const live = vi.hoisted(() => ({ agents: undefined as unknown as Ref<any[]>, loaded: undefined as unknown as Ref<boolean> }))
vi.mock('@/stores/live', async () => {
  const { ref } = await import('vue')
  live.agents = ref([])
  live.loaded = ref(false)
  return { useLive: () => ({ loaded: live.loaded, boundAgents: live.agents,
    isOnline: (agent: any) => agent.status === 'online' && agent.enabled !== false,
    start: vi.fn(), stop: vi.fn(), refresh: vi.fn() }) }
})
vi.mock('@/api', () => ({
  agentApi: { generateConfig: vi.fn(), pushConfig: vi.fn(), getDeployment: vi.fn() },
  generateApi: { artifact: vi.fn() },
  statsApi: { getOverview: vi.fn(async () => ({ data: { success: true, data: {} } })) },
  ruleSetApi: { getAll: vi.fn(async () => ({ data: [] })) },
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }))
vi.mock('@/lib/clipboard', () => ({ copyText: vi.fn(async () => {}) }))
const icon = markRaw(defineComponent({ template: '<i />' }))
const targets = ['mihomo', 'mosdns', 'loon'].map(key => ({ key, title: key, icon, desc: '', url: '', urlDisplay: '', actions: [] })) as any
const wrappers: VueWrapper[] = []
const button = (wrapper: VueWrapper, text: string) => wrapper.findAll('button').find(b => b.text() === text)!
const selection = (wrapper: VueWrapper) => wrapper.findAllComponents(Select)[1]
const choose = async (wrapper: VueWrapper, id: string) => {
  selection(wrapper).vm.$emit('update:modelValue', id)
  await flushPromises()
}
const agent = (id: string, service_type = 'mihomo', profile_id = 'default', status = 'online') => ({ id, name: id, service_type, profile_id, status, enabled: true })
const content = Array.from({ length: 120 }, (_, i) => `key${i}: value${i}`).join('\n') + '\n'
const artifact = (id: string, profile = 'default', service = 'mihomo') => ({ data: {
  id: `preview-${id}`, agent_id: id, profile_id: profile, service_type: service, deployment_id: `deployment-${id}`,
  content, config_path: 'config.yaml', files: [{ path: 'config.yaml', role: 'config', size: content.length, sha256: 'hash' }], target_ids: [id],
} }) as any
let reduced = false

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  live.agents.value = []
  live.loaded.value = true
  reduced = true
  usePreferences().prefs.value.motion = false
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('prefers-reduced-motion') && reduced,
    media: query, addEventListener: vi.fn(), removeEventListener: vi.fn(), addListener: vi.fn(), removeListener: vi.fn() }))
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.mocked(agentApi.generateConfig).mockImplementation(async (id, profile, service) => artifact(id, profile, service))
  vi.mocked(agentApi.pushConfig).mockImplementation(async (_id, deployment_id) => ({ data: { deployment_id, status: 'succeeded' } }) as any)
  vi.mocked(generateApi.artifact).mockResolvedValue(artifact('manual'))
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  usePreferences().prefs.value.motion = false
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})
async function render(query = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: icon }, { path: '/agents', component: icon }] })
  await router.push({ path: '/', query })
  const wrapper = mount(GenerateStudio, { props: { targets, profileId: 'default' }, global: { plugins: [router] }, attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('Generation destination', () => {
  it('waits for the first device list, then generates for the sole matching Agent', async () => {
    live.loaded.value = false
    const wrapper = await render()
    expect(button(wrapper, '生成').attributes('disabled')).toBeDefined()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true }))
    expect(generateApi.artifact).not.toHaveBeenCalled()
    expect(agentApi.generateConfig).not.toHaveBeenCalled()
    live.agents.value = [agent('home'), agent('dns', 'mosdns'), agent('elsewhere', 'mihomo', 'other'), { ...agent('disabled'), enabled: false }]
    live.loaded.value = true
    await flushPromises()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(agentApi.generateConfig).toHaveBeenCalledWith('home', 'default', 'mihomo')
    expect(generateApi.artifact).not.toHaveBeenCalled()
  })

  it('defaults to manual export only when no Agent matches, and keeps it available explicitly', async () => {
    live.agents.value = [agent('dns', 'mosdns')]
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(generateApi.artifact).toHaveBeenCalledWith('mihomo', 'default')
    await button(wrapper, 'mosdns').trigger('click')
    await choose(wrapper, 'manual')
    live.agents.value = [...live.agents.value, agent('dns-two', 'mosdns')]
    await flushPromises()
    expect(selection(wrapper).props('modelValue')).toBe('manual')
  })

  it('requires a choice for multiple Agents and restores the chosen Agent on a later visit', async () => {
    live.agents.value = [agent('home'), agent('office')]
    const wrapper = await render()
    expect(button(wrapper, '生成').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#generate-destination').text()).toContain('请选择')
    await choose(wrapper, 'office')
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(agentApi.generateConfig).toHaveBeenCalledWith('office', 'default', 'mihomo')
    wrapper.unmount()
    const restored = await render()
    expect(selection(restored).props('modelValue')).toBe('office')
    expect(button(restored, '生成').attributes('disabled')).toBeUndefined()
  })

  it('remembers choices separately for each profile and software', async () => {
    live.agents.value = [agent('a'), agent('b'), agent('dns-a', 'mosdns'), agent('dns-b', 'mosdns'),
      agent('other-a', 'mihomo', 'other'), agent('other-b', 'mihomo', 'other')]
    const wrapper = await render()
    await choose(wrapper, 'b')
    await button(wrapper, 'mosdns').trigger('click')
    expect(selection(wrapper).props('modelValue')).toBeUndefined()
    await choose(wrapper, 'dns-a')
    await button(wrapper, 'mihomo').trigger('click')
    expect(selection(wrapper).props('modelValue')).toBe('b')
    await wrapper.setProps({ profileId: 'other' })
    expect(selection(wrapper).props('modelValue')).toBeUndefined()
    await choose(wrapper, 'other-b')
    await wrapper.setProps({ profileId: 'default' })
    expect(selection(wrapper).props('modelValue')).toBe('b')
  })

  it('does not replace a selected Agent when it is removed or rebound', async () => {
    live.agents.value = [agent('home'), agent('office')]
    const wrapper = await render()
    await choose(wrapper, 'office')
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    live.agents.value = [agent('home'), agent('office', 'mihomo', 'other')]
    await flushPromises()
    expect(button(wrapper, '生成').attributes('disabled')).toBeDefined()
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
    expect(wrapper.get('.gen-code').text()).toBe('')
    expect(agentApi.generateConfig).toHaveBeenCalledTimes(1)
  })

  it('ignores an unavailable remembered Agent when multiple alternatives remain', async () => {
    live.agents.value = [agent('home'), agent('office')]
    const first = await render()
    await choose(first, 'office')
    first.unmount()
    live.agents.value = [agent('home'), agent('new')]
    const wrapper = await render()
    expect(selection(wrapper).props('modelValue')).toBeUndefined()
    expect(button(wrapper, '生成').attributes('disabled')).toBeDefined()
  })

  it('keeps the sole offline Agent selected and waits for it to come online', async () => {
    live.agents.value = [agent('home', 'mihomo', 'default', 'offline')]
    const wrapper = await render()
    expect(selection(wrapper).props('modelValue')).toBe('home')
    expect(wrapper.text()).toContain('目标设备离线')
    expect(button(wrapper, '生成').attributes('disabled')).toBeDefined()
    live.agents.value[0].status = 'online'
    await nextTick()
    expect(button(wrapper, '生成').attributes('disabled')).toBeUndefined()
  })

  it('can choose and generate when local storage is unavailable', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    live.agents.value = [agent('home'), agent('office')]
    const wrapper = await render()
    await choose(wrapper, 'office')
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(agentApi.generateConfig).toHaveBeenCalledWith('office', 'default', 'mihomo')
  })

  it('defers a quick action until the device list resolves without exporting a generic config', async () => {
    live.loaded.value = false
    await render({ run: 'generate-mihomo' })
    expect(generateApi.artifact).not.toHaveBeenCalled()
    live.agents.value = [agent('home')]
    live.loaded.value = true
    await flushPromises()
    expect(agentApi.generateConfig).toHaveBeenCalledExactlyOnceWith('home', 'default', 'mihomo')
  })

  it('allows Loon manual export even while the Agent list is loading', async () => {
    live.loaded.value = false
    const wrapper = await render()
    await button(wrapper, 'loon').trigger('click')
    expect(selection(wrapper).props('modelValue')).toBe('manual')
    expect(button(wrapper, '生成').attributes('disabled')).toBeUndefined()
  })
})

describe('Fixed-result generation animation', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(performance.now()), 16))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id))
    reduced = false
    live.agents.value = [agent('home')]
  })

  it.each([false, true])('replays every step and the fixed result with appearance motion=%s', async motion => {
    usePreferences().prefs.value.motion = motion
    let resolve!: (value: any) => void
    vi.mocked(agentApi.generateConfig).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('正在生成最终配置')
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(0)
    expect(wrapper.findAll('.gen-line')).toHaveLength(0)
    resolve(artifact('home'))
    await flushPromises()
    expect(wrapper.findAll('.gen-line').length).toBeGreaterThan(0)
    expect(wrapper.findAll('.gen-line').length).toBeLessThan(120)
    expect(wrapper.get('[data-result-replay]').text()).toContain('配置已生成')
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(0)
    expect(wrapper.findAll('.gen-step')[0].classes()).toContain('run')
    let previousLines = wrapper.findAll('.gen-line').length
    for (let stage = 1; stage <= 4; stage++) {
      await vi.advanceTimersByTimeAsync(370)
      expect(wrapper.findAll('.gen-step.done')).toHaveLength(stage)
      expect(wrapper.findAll('.gen-step')[stage].classes()).toContain('run')
      expect(wrapper.findAll('.gen-line').length).toBeGreaterThan(previousLines)
      previousLines = wrapper.findAll('.gen-line').length
    }
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()
    expect(wrapper.findAll('.gen-line')).toHaveLength(120)
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(5)
    expect(wrapper.find('[data-result-replay]').exists()).toBe(false)
    await button(wrapper, '复制').trigger('click')
    expect(copyText).toHaveBeenCalledWith(content)
    await button(wrapper, '推送').trigger('click')
    expect(agentApi.pushConfig).toHaveBeenCalledWith('home', 'deployment-home', { generation_id: 'preview-home' })
  })

  it('cancels a partially revealed result when the profile changes', async () => {
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.gen-line').length).toBeGreaterThan(0)
    await wrapper.setProps({ profileId: 'other' })
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.findAll('.gen-line')).toHaveLength(0)
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
  })

  it('respects system reduced motion even when appearance motion is enabled', async () => {
    usePreferences().prefs.value.motion = true
    reduced = true
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.gen-line')).toHaveLength(120)
    expect(button(wrapper, '推送').attributes('disabled')).toBeUndefined()
  })

  it('lets the user finish playback immediately without changing the final file', async () => {
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    await button(wrapper, '立即显示').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.gen-line')).toHaveLength(120)
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(5)
    expect(button(wrapper, '推送').attributes('disabled')).toBeUndefined()
    await button(wrapper, '复制').trigger('click')
    expect(copyText).toHaveBeenCalledWith(content)
    await vi.advanceTimersByTimeAsync(3000)
    expect(notify.success).toHaveBeenCalledTimes(2) // Generated, then copied.
  })

  it('keeps short files and high-refresh displays from finishing all steps in a few frames', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => setTimeout(() => cb(performance.now()), 4))
    const result = artifact('home')
    result.data.content = 'mode: rule\nrules:\n  - MATCH,DIRECT\n'
    vi.mocked(agentApi.generateConfig).mockResolvedValueOnce(result)
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(400)
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(1)
    expect(wrapper.findAll('.gen-line')).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1500)
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(5)
    expect(wrapper.findAll('.gen-line')).toHaveLength(3)
  })

  it.each(['hidden', 'reduced'])('finishes a partial result when the page becomes %s', async mode => {
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    if (mode === 'hidden') {
      vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
      document.dispatchEvent(new Event('visibilitychange'))
    } else {
      reduced = true
      await vi.advanceTimersByTimeAsync(16)
    }
    await flushPromises()
    expect(wrapper.findAll('.gen-line')).toHaveLength(120)
    expect(button(wrapper, '推送').attributes('disabled')).toBeUndefined()
  })

  it('limits a long preview while copying all original bytes', async () => {
    const result = artifact('home')
    result.data.content = Array.from({ length: 2500 }, (_, i) => `key${i}: value`).join('\n') + '\n'
    vi.mocked(agentApi.generateConfig).mockResolvedValueOnce(result)
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2700)
    expect(wrapper.findAll('.gen-line')).toHaveLength(2000)
    expect(wrapper.text()).toContain('仅显示前 2000 行')
    await button(wrapper, '复制').trigger('click')
    expect(copyText).toHaveBeenCalledWith(result.data.content)
  })

  it('cancels playback on unmount without reporting success', async () => {
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(3000)
    expect(notify.success).not.toHaveBeenCalled()
  })

  it('never replays completed steps when generation fails', async () => {
    vi.mocked(agentApi.generateConfig).mockRejectedValueOnce({ response: { data: { message: '目标设备未就绪' } } })
    const wrapper = await render()
    await button(wrapper, '生成').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.findAll('.gen-step.done')).toHaveLength(0)
    expect(wrapper.find('[data-result-replay]').exists()).toBe(false)
    expect(wrapper.text()).toContain('目标设备未就绪')
    expect(button(wrapper, '推送').attributes('disabled')).toBeDefined()
  })
})
