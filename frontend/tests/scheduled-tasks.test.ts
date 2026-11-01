import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ScheduledTasks from '@/components/settings/ScheduledTasks.vue'
import { Select } from '@/components/ui/select'
import { routerApi, tasksApi } from '@/api'
import { notify } from '@/lib/feedback'
import {
  CATEGORY_LABELS, cacheNote, intervalText, isOverdue, relativeTo, sourceIntervalOptions, taskIntervalOptions,
  taskStatusView, taskSummary, type ScheduledTask
} from '@/lib/scheduledTasks'

vi.mock('@/api', () => ({
  tasksApi: { list: vi.fn(), run: vi.fn(), setInterval: vi.fn() },
  routerApi: { saveRuleSync: vi.fn() },
}))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn() } }))

const NOW = Date.parse('2026-10-24T08:00:00Z')
const task = (changes: Partial<ScheduledTask> = {}): ScheduledTask => ({
  key: 'rule_source:r1', category: 'rule_source', name: 'Streaming', detail: 'rules.test', resource_id: 'r1',
  profile_id: null, profile_name: null, enabled: true, interval_seconds: 86400, schedule_text: '',
  editable: true, can_run: true, run_hint: '', running: false, next_run_at: '2026-10-24T11:00:00Z',
  state_message: '', link: '/rule-library', last_run_at: '2026-10-23T11:00:00Z', last_status: 'success',
  last_error: '', last_duration_ms: 1200, last_trigger: 'schedule', last_success_at: '2026-10-23T11:00:00Z',
  run_count: 42, failure_count: 3, cache_retained: null, cache_updated_at: '2026-10-23T11:00:00Z', ...changes
})
const metricsFlush = (changes: Partial<ScheduledTask> = {}) => task({
  key: 'metrics_history_flush', category: 'metrics_history', name: '监控历史保存', detail: 'Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录', resource_id: '',
  interval_seconds: 1800, link: '/agents', next_run_at: '2026-10-24T08:20:00Z', last_run_at: '2026-10-24T07:50:00Z',
  last_trigger: 'shutdown', run_count: 5, failure_count: 0, cache_updated_at: null, ...changes
})
const failed = task({
  last_status: 'failed', last_error: '连接上游超时', cache_retained: true,
  cache_updated_at: '2026-10-22T11:00:00Z', next_run_at: '2026-10-24T08:05:00Z',
})

describe('scheduled task helpers', () => {
  it('formats intervals, relative times and summaries', () => {
    expect(intervalText(86400)).toBe('每 1 天')
    expect(intervalText(6 * 3600)).toBe('每 6 小时')
    expect(intervalText(1800)).toBe('每 30 分钟')
    expect(intervalText(0)).toBe('仅手动')
    expect(relativeTo('2026-10-24T11:00:00Z', NOW)).toBe('3 小时后')
    expect(relativeTo('2026-10-23T08:00:00Z', NOW)).toBe('1 天前')
    expect(relativeTo('2026-10-24T08:00:20Z', NOW)).toBe('即将运行')
    expect(relativeTo(null, NOW)).toBe('—')
    expect(sourceIntervalOptions(7200)).toContain(7200)
    expect(taskSummary([task(), failed, task({ enabled: false, last_status: 'failed' }), task({ running: true })]))
      .toEqual({ total: 4, enabled: 3, failed: 1, running: 1 })
  })

  it('offers fixed intervals for monitoring history and routers, and source intervals otherwise', () => {
    expect(CATEGORY_LABELS.metrics_history).toBe('监控历史')
    expect(taskIntervalOptions(metricsFlush())).toEqual([
      { value: '300', label: '每 5 分钟' }, { value: '600', label: '每 10 分钟' },
      { value: '1800', label: '每 30 分钟' }, { value: '3600', label: '每 1 小时' },
    ])
    expect(taskIntervalOptions(task({ category: 'router_rule_sync', interval_seconds: 1800 })).map(option => option.value))
      .toEqual(['900', '1800', '3600', '10800', '21600', '43200', '86400'])
    expect(taskIntervalOptions(task())[0]).toEqual({ value: '0', label: '关闭自动更新（仅手动）' })
  })

  it('colours a failure by whether the previous cache is still in use', () => {
    expect(taskStatusView(task())).toEqual({ label: '成功', chip: 'chip-ok' })
    expect(taskStatusView(failed)).toEqual({ label: '失败 · 使用旧缓存', chip: 'chip-warn' })
    expect(taskStatusView(task({ last_status: 'failed', cache_retained: false }))).toEqual({ label: '失败', chip: 'chip-bad' })
    expect(taskStatusView(task({ last_status: 'skipped' }))).toEqual({ label: '已跳过', chip: 'chip-warn' })
    expect(taskStatusView(task({ running: true }))).toEqual({ label: '运行中', chip: 'chip-acc' })
    expect(taskStatusView(task({ last_status: null }))).toBeNull()
    expect(cacheNote(failed)).toMatch(/^继续使用 .+（.+）更新的缓存。$/)
    expect(cacheNote(task({ last_status: 'failed', cache_retained: false }))).toBe('没有可用的有效缓存，请修正来源后重新运行。')
    expect(cacheNote(task())).toBe('')
    expect(isOverdue(task({ next_run_at: '2026-10-24T07:00:00Z' }), NOW)).toBe(true)
    expect(isOverdue(task({ next_run_at: '2026-10-24T07:58:00Z' }), NOW)).toBe(false)
    expect(isOverdue(task({ next_run_at: '2026-10-24T07:00:00Z', enabled: false }), NOW)).toBe(false)
  })
})

const wrappers: ReturnType<typeof mount>[] = []
async function render(tasks: ScheduledTask[]) {
  vi.mocked(tasksApi.list).mockResolvedValue({ data: { success: true, tasks } } as any)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:path(.*)*', component: { render: () => null } }] })
  const wrapper = mount(ScheduledTasks, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval', 'clearTimeout', 'clearInterval', 'Date'] })
  vi.setSystemTime(NOW)
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('ScheduledTasks', () => {
  it('shows each task with its schedule, results and always-visible failure reason', async () => {
    const wrapper = await render([task(), failed, task({
      key: 'router_rule_sync:p:ros', category: 'router_rule_sync', name: 'ROS', profile_id: 'p', profile_name: 'Home',
      detail: '', interval_seconds: 1800, last_status: 'failed', last_error: '请先手动预览发布', cache_retained: null,
      state_message: '已暂停，需要在上游路由手动预览发布', can_run: false, run_hint: '同步尚未完成或需要人工确认',
    }), task({ key: 'webdav_backup', category: 'webdav_backup', name: 'WebDAV 自动备份', interval_seconds: null,
      schedule_text: '配置变更后自动备份', next_run_at: null, editable: false, link: '/system-settings', last_status: null, last_run_at: null, run_count: 0 })])
    expect(wrapper.get('[data-task-summary]').text()).toBe('已启用 4 / 4')
    expect(wrapper.get('[data-task-failed]').text()).toBe('2 个失败')
    const row = wrapper.get('[data-task="rule_source:r1"]')
    expect(row.text()).toContain('Streaming')
    expect(row.text()).toContain('rules.test')
    expect(row.text()).toContain('每 1 天')
    expect(row.text()).toContain('3 小时后')
    expect(row.text()).toContain('运行 42 次 · 失败 3 次')
    expect(row.get('[data-task-status]').classes()).toContain('chip-ok')
    const failure = wrapper.findAll('[data-task="rule_source:r1"]')[1]!
    expect(failure.get('[data-task-status]').text()).toBe('失败 · 使用旧缓存')
    expect(failure.get('[data-task-error]').text()).toBe('连接上游超时')
    expect(failure.get('[data-task-error]').classes()).toContain('text-warning-accent')
    expect(failure.get('[data-task-cache]').text()).toContain('更新的缓存')
    const ros = wrapper.get('[data-task="router_rule_sync:p:ros"]')
    expect(ros.text()).toContain('Home')
    expect(ros.get('[data-task-error]').classes()).toContain('text-destructive-accent')
    expect(ros.text()).toContain('已暂停，需要在上游路由手动预览发布')
    expect(ros.get('button[aria-label="立即运行「ROS」"]').attributes('disabled')).toBeDefined()
    expect(ros.get('button[aria-label="立即运行「ROS」"]').attributes('title')).toBe('同步尚未完成或需要人工确认')
    const backup = wrapper.get('[data-task="webdav_backup"]')
    expect(backup.text()).toContain('配置变更后自动备份')
    expect(backup.text()).toContain('按需')
    expect(backup.text()).toContain('尚未运行')
    expect(backup.find('button[aria-label^="编辑"]').exists()).toBe(false)
    expect(backup.get('a').attributes('href')).toBe('/system-settings')
  })

  it('filters by failure and category', async () => {
    const wrapper = await render([task({ key: 'subscription:s', category: 'subscription', name: 'Feed' }), failed])
    const chip = (text: string) => wrapper.findAll('button.chip').find(item => item.text().startsWith(text))!
    await chip('失败').trigger('click')
    expect(wrapper.findAll('tbody tr').map(row => row.attributes('data-task'))).toEqual(['rule_source:r1'])
    await chip('订阅更新').trigger('click')
    expect(wrapper.findAll('tbody tr').map(row => row.attributes('data-task'))).toEqual(['subscription:s'])
    expect(chip('订阅更新').attributes('aria-pressed')).toBe('true')
  })

  it('runs a task, then polls quickly until it finishes', async () => {
    const wrapper = await render([task()])
    vi.mocked(tasksApi.run).mockResolvedValue({ data: { success: true, message: '已开始运行，结果会显示在列表中' } } as any)
    vi.mocked(tasksApi.list).mockResolvedValueOnce({ data: { success: true, tasks: [task({ running: true })] } } as any)
    await wrapper.get('button[aria-label="立即运行「Streaming」"]').trigger('click')
    await flushPromises()
    expect(tasksApi.run).toHaveBeenCalledWith('rule_source:r1')
    expect(notify.success).toHaveBeenCalledWith('「Streaming」：已开始运行，结果会显示在列表中')
    expect(wrapper.get('[data-task-status]').text()).toBe('运行中')
    expect(wrapper.text()).toContain('运行中…')
    vi.mocked(tasksApi.list).mockResolvedValue({ data: { success: true, tasks: [task({ run_count: 43 })] } } as any)
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain('运行 43 次')
    expect(wrapper.text()).not.toContain('运行中…')
  })

  it('pauses polling while its settings tab is hidden and reads again only once due', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue({ data: { success: true, tasks: [task({ running: true })] } } as any)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:path(.*)*', component: { render: () => null } }] })
    const wrapper = mount(ScheduledTasks, { props: { active: true }, attachTo: document.body, global: { plugins: [router] } })
    wrappers.push(wrapper)
    await flushPromises()
    expect(tasksApi.list).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ active: false })
    await vi.advanceTimersByTimeAsync(60000)
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(tasksApi.list).toHaveBeenCalledTimes(1)
    // 隐藏期间已经过了轮询时间，切回立即读取一次
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(tasksApi.list).toHaveBeenCalledTimes(2)
    // 刚读过就切走再切回，不重复读取，按剩余时间继续轮询
    await wrapper.setProps({ active: false })
    await vi.advanceTimersByTimeAsync(1000)
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(tasksApi.list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(2000)
    expect(tasksApi.list).toHaveBeenCalledTimes(3)
  })

  it('reports a run that cannot start', async () => {
    const wrapper = await render([task()])
    vi.mocked(tasksApi.run).mockRejectedValue({ isAxiosError: true, response: { data: { message: '这个任务正在运行，请稍后查看结果' } } })
    await wrapper.get('button[aria-label="立即运行「Streaming」"]').trigger('click')
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('这个任务正在运行，请稍后查看结果')
  })

  it('keeps the last list and says so when a later read fails', async () => {
    const wrapper = await render([failed])
    vi.mocked(tasksApi.list).mockRejectedValue(new Error('网络错误'))
    await wrapper.get('button[aria-label="刷新任务列表"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('网络错误，保留上次读取的结果。')
    expect(wrapper.find('[data-task="rule_source:r1"]').exists()).toBe(true)
  })

  it('edits a source interval, including turning automatic updates off', async () => {
    const wrapper = await render([task()])
    vi.mocked(tasksApi.setInterval).mockResolvedValue({ data: { success: true, task: null } } as any)
    await wrapper.get('button[aria-label="编辑「Streaming」的间隔"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('服务端按此间隔下载并校验规则集')
    wrapper.findAllComponents(Select)[0]!.vm.$emit('update:modelValue', '0')
    await flushPromises()
    document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(tasksApi.setInterval).toHaveBeenCalledWith('rule_source:r1', 0)
    expect(notify.success).toHaveBeenCalledWith('已更新「Streaming」')
  })

  it('shows the monitoring history row and edits its save interval', async () => {
    const wrapper = await render([metricsFlush()])
    const row = wrapper.get('[data-task="metrics_history_flush"]')
    expect(row.text()).toContain('监控历史保存')
    // 写明保存的是哪个软件的数据
    expect(row.text()).toContain('Mihomo 流量记录')
    expect(row.text()).toContain('监控历史')
    expect(row.text()).toContain('每 30 分钟')
    expect(row.text()).toContain('20 分钟后')
    expect(row.text()).toContain('服务停止时')
    expect(row.get('a').attributes('href')).toBe('/agents')
    vi.mocked(tasksApi.setInterval).mockResolvedValue({ data: { success: true, task: null } } as any)
    await wrapper.get('button[aria-label="编辑「监控历史保存」的间隔"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[role="dialog"] label[for="task-interval"]')?.textContent).toBe('保存间隔')
    expect(document.body.textContent).toContain('服务正常停止时也会保存一次')
    expect(document.querySelector('#task-interval')?.textContent).toContain('每 30 分钟')
    wrapper.findAllComponents(Select)[0]!.vm.$emit('update:modelValue', '300')
    await flushPromises()
    document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(tasksApi.setInterval).toHaveBeenCalledWith('metrics_history_flush', 300)
    expect(routerApi.saveRuleSync).not.toHaveBeenCalled()
    expect(notify.success).toHaveBeenCalledWith('已更新「监控历史保存」')
  })

  it('edits a router through its own rule sync settings and keeps errors in the dialog', async () => {
    const ros = task({ key: 'router_rule_sync:p:ros', category: 'router_rule_sync', name: 'ROS', resource_id: 'ros',
      profile_id: 'p', interval_seconds: 1800 })
    const wrapper = await render([ros])
    vi.mocked(routerApi.saveRuleSync).mockRejectedValueOnce({ isAxiosError: true, response: { data: { message: '路由器设置已变化，请刷新后重试' } } })
    await wrapper.get('button[aria-label="编辑「ROS」的间隔"]').trigger('click')
    await flushPromises()
    wrapper.findAllComponents(Select)[0]!.vm.$emit('update:modelValue', '3600')
    await flushPromises()
    document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(routerApi.saveRuleSync).toHaveBeenCalledWith('ros', { auto_rule_sync: true, rule_sync_interval: 60 }, 'p')
    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe('路由器设置已变化，请刷新后重试')
  })
})
