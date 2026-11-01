import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import NodeDeleteDialog from '@/components/nodes/NodeDeleteDialog.vue'
import { nodeApi, type NodeDeletionPreview } from '@/api'
import { notify } from '@/lib/feedback'

vi.mock('@/api', () => ({ nodeApi: { previewDeletion: vi.fn(), deleteWithReferences: vi.fn() } }))

const plan: NodeDeletionPreview = {
  nodes: [{ id: 'one', name: '香港节点' }, { id: 'two', name: '日本节点' }],
  usages: [{ kind: 'aggregation', id: 'agg', name: '常用聚合' }, { kind: 'profile', id: 'other', name: '另一个空间' }],
  groups: [{ id: 'group', name: '自动选择', profile_id: 'other', profile_name: '另一个空间' }],
  blockers: [], empty_sources: [], confirmation: 'reviewed',
}
let wrapper: VueWrapper<InstanceType<typeof NodeDeleteDialog>>

beforeEach(() => {
  vi.resetAllMocks()
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
  vi.mocked(nodeApi.previewDeletion).mockResolvedValue({ data: structuredClone(plan) } as any)
  vi.mocked(nodeApi.deleteWithReferences).mockResolvedValue({ data: { success: true, deleted_ids: ['one', 'two'] } } as any)
  wrapper = mount(NodeDeleteDialog, { attachTo: document.body })
})
afterEach(() => {
  wrapper.unmount()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

function button(label: string) {
  const found = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')]
    .find(button => button.textContent?.trim() === label)
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  button(label).click()
  await flushPromises()
}
async function show(ids = ['one', 'two']) {
  wrapper.vm.show(ids)
  await flushPromises()
}

describe('节点删除确认', () => {
  it('列出跨空间影响，确认后一次提交整批节点并报告实际删除结果', async () => {
    await show()
    expect(nodeApi.previewDeletion).toHaveBeenCalledWith(['one', 'two'])
    expect(document.body.textContent).toContain('常用聚合')
    expect(document.body.textContent).toContain('另一个空间 → 策略组「自动选择」')
    expect(document.body.textContent).toContain('删除后无法恢复')
    expect(nodeApi.deleteWithReferences).not.toHaveBeenCalled()
    await click('解除引用并删除')
    expect(nodeApi.deleteWithReferences).toHaveBeenCalledTimes(1)
    expect(nodeApi.deleteWithReferences).toHaveBeenCalledWith(['one', 'two'], 'reviewed')
    expect(wrapper.emitted('deleted')).toEqual([[['one', 'two']]])
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('取消不会解除任何引用', async () => {
    await show()
    await click('取消')
    expect(nodeApi.deleteWithReferences).not.toHaveBeenCalled()
    expect(wrapper.emitted('deleted')).toBeUndefined()
  })

  it('代理链阻断时显示原因，禁止提交整批删除', async () => {
    vi.mocked(nodeApi.previewDeletion).mockResolvedValue({ data: { ...plan, blockers: ['前置节点仍被代理链使用'] } } as any)
    await show()
    expect(document.body.textContent).toContain('前置节点仍被代理链使用')
    expect(button('解除引用并删除').disabled).toBe(true)
    await click('解除引用并删除')
    expect(nodeApi.deleteWithReferences).not.toHaveBeenCalled()
  })

  it('空聚合或空策略组的影响在确认前可见', async () => {
    vi.mocked(nodeApi.previewDeletion).mockResolvedValue({ data: { ...plan, empty_sources: ['聚合「常用聚合」'] } } as any)
    await show()
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('将没有已选择的来源')
    expect(button('解除引用并删除').disabled).toBe(false)
  })

  it('读取失败时不能删除，允许重新读取', async () => {
    vi.mocked(nodeApi.previewDeletion).mockRejectedValueOnce(new Error('网络连接失败'))
    await show()
    expect(document.body.textContent).toContain('网络连接失败')
    expect(button('删除节点').disabled).toBe(true)
    await click('重新读取影响')
    expect(button('解除引用并删除').disabled).toBe(false)
  })

  it('引用变化后必须重新读取和确认，不自动重试删除', async () => {
    vi.mocked(nodeApi.deleteWithReferences).mockRejectedValueOnce({ response: { status: 409, data: {
      code: 'node_deletion_changed', message: '节点或引用关系已变化，请重新查看删除影响并确认',
    } } })
    await show()
    await click('解除引用并删除')
    expect(document.body.textContent).toContain('节点或引用关系已变化')
    expect(button('删除节点').disabled).toBe(true)
    vi.mocked(nodeApi.previewDeletion).mockResolvedValueOnce({ data: { ...plan, confirmation: 'new-review' } } as any)
    await click('重新读取影响')
    expect(nodeApi.deleteWithReferences).toHaveBeenCalledTimes(1)
    await click('解除引用并删除')
    expect(nodeApi.deleteWithReferences).toHaveBeenLastCalledWith(['one', 'two'], 'new-review')
  })

  it('删除处理中不能重复提交或关闭', async () => {
    let finish!: (response: any) => void
    vi.mocked(nodeApi.deleteWithReferences).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await show()
    await click('解除引用并删除')
    expect(button('正在删除…').disabled).toBe(true)
    expect(button('取消').disabled).toBe(true)
    wrapper.vm.show(['different'])
    await flushPromises()
    expect(nodeApi.previewDeletion).toHaveBeenCalledTimes(1)
    finish({ data: { deleted_ids: ['one', 'two'] } })
    await flushPromises()
    expect(wrapper.emitted('deleted')).toEqual([[['one', 'two']]])
  })

  it('取消后重新打开时忽略旧预览响应', async () => {
    let finish!: (response: any) => void
    vi.mocked(nodeApi.previewDeletion).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    await show(['one'])
    await click('取消')
    vi.mocked(nodeApi.previewDeletion).mockResolvedValueOnce({ data: { ...plan, nodes: [{ id: 'two', name: '最新节点' }], confirmation: 'new-review' } } as any)
    await show(['two'])
    finish({ data: { ...plan, nodes: [{ id: 'one', name: '旧节点' }] } })
    await flushPromises()
    expect(document.body.textContent).toContain('最新节点')
    expect(document.body.textContent).not.toContain('旧节点')
    await click('解除引用并删除')
    expect(nodeApi.deleteWithReferences).toHaveBeenCalledWith(['two'], 'new-review')
  })
})
