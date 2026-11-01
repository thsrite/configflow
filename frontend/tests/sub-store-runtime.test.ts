import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SubStoreRuntime from '@/components/settings/SubStoreRuntime.vue'
import { Select } from '@/components/ui/select'
import { dependenciesApi, type DependencyStatus } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({ dependenciesApi: { setSubStoreMode: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn() } }))
const wrappers: ReturnType<typeof mount>[] = []
const dependency = (extra = {}) => ({ key: 'sub-store', running: false,
  lifecycle: { managed: true, mode: 'on_demand', state: 'sleeping', idle_seconds: 300, error: '' },
  update: { state: 'idle' }, ...extra } as DependencyStatus)
function render(dep = dependency()) {
  const wrapper = mount(SubStoreRuntime, { props: { dependency: dep }, attachTo: document.body })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.unstubAllGlobals() })

describe('Sub-Store runtime settings', () => {
  it('shows the saved mode without waking the service on mount', () => {
    const wrapper = render()
    expect(wrapper.get('[role="combobox"]').attributes('aria-label')).toBe('Sub-Store 运行方式')
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('on_demand')
    expect(wrapper.text()).toContain('空闲 5 分钟')
    expect(dependenciesApi.setSubStoreMode).not.toHaveBeenCalled()
  })

  it('waits for a successful mode change and prevents duplicate saves', async () => {
    let done!: () => void
    vi.mocked(dependenciesApi.setSubStoreMode).mockReturnValue(new Promise(resolve => { done = () => resolve({ data: { success: true } } as any) }))
    const wrapper = render()
    const select = wrapper.getComponent(Select)
    select.vm.$emit('update:modelValue', 'always')
    await flushPromises()
    expect(dependenciesApi.setSubStoreMode).toHaveBeenCalledWith('always')
    expect(wrapper.text()).toContain('正在保存')
    expect(select.props('modelValue')).toBe('on_demand')
    expect(wrapper.get('[role="combobox"]').attributes('disabled')).toBeDefined()
    select.vm.$emit('update:modelValue', 'always')
    expect(dependenciesApi.setSubStoreMode).toHaveBeenCalledTimes(1)
    done(); await flushPromises()
    expect(select.props('modelValue')).toBe('always')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(notify.success).toHaveBeenCalled()
  })

  it('retains the saved mode when switching fails', async () => {
    vi.mocked(dependenciesApi.setSubStoreMode).mockRejectedValue(new Error('unavailable'))
    const wrapper = render()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'always')
    await flushPromises()
    expect(wrapper.getComponent(Select).props('modelValue')).toBe('on_demand')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(notify.error).toHaveBeenCalled()
  })

  it('prevents mode changes while updating', async () => {
    const wrapper = render(dependency({ update: { state: 'running' } }))
    expect(wrapper.get('[role="combobox"]').attributes('disabled')).toBeDefined()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'always')
    await flushPromises()
    expect(dependenciesApi.setSubStoreMode).not.toHaveBeenCalled()
  })

  it('does not offer local process controls for an external server', () => {
    const dep = dependency()
    dep.lifecycle = { ...dep.lifecycle!, managed: false, mode: 'external', state: 'running' }
    const wrapper = render(dep)
    expect(wrapper.text()).toContain('由外部服务管理')
    expect(wrapper.find('[role="combobox"]').exists()).toBe(false)
  })
})
