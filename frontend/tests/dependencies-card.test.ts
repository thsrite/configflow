import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DependenciesCard from '@/components/settings/DependenciesCard.vue'
import { dependenciesApi, type DependencyStatus } from '@/api'

vi.mock('@/api', () => ({ dependenciesApi: { setSubStoreMode: vi.fn() } }))
vi.mock('@/lib/feedback', () => ({ notify: { success: vi.fn(), error: vi.fn() } }))
const wrappers: ReturnType<typeof mount>[] = []
const dependency = () => ({key:'sub-store',name:'Sub-Store',description:'订阅解析与节点格式转换',homepage:'https://example.com',running:false,
  current_version:'2.44.2',runtime:'v24.22.1',latest_version:'2.44.2',release_url:'https://example.com/releases/2.44.2',check_error:'',has_update:false,updatable:true,
  lifecycle:{managed:true,mode:'on_demand',state:'sleeping',idle_seconds:300,error:''},update:{state:'idle',message:''}
} as DependencyStatus)
function render(dep = dependency()) {
  const wrapper = mount(DependenciesCard, {props:{dependencies:[dep],checking:false,updatingKey:''},attachTo:document.body})
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('ResizeObserver',class {observe() {} disconnect() {} unobserve() {}})
})
afterEach(() => {wrappers.splice(0).forEach(wrapper=>wrapper.unmount());vi.unstubAllGlobals()})

describe('third-party dependency table',()=>{
  it('keeps idle status normal and shows a single current version with its release link',()=>{
    const wrapper=render()
    expect(wrapper.text()).toContain('休眠中')
    expect(wrapper.text()).not.toContain('无法连接')
    expect(wrapper.text().match(/2\.44\.2/g)).toHaveLength(1)
    expect(wrapper.findAll('a').find(link=>link.text()==='已是最新')?.attributes('href')).toBe('https://example.com/releases/2.44.2')
    expect(dependenciesApi.setSubStoreMode).not.toHaveBeenCalled()
  })
  it('keeps updating separate from selecting the runtime mode',async()=>{
    const dep=dependency();dep.has_update=true;dep.latest_version='2.45.0'
    const wrapper=render(dep)
    const update=wrapper.findAll('button').find(button=>button.text()==='更新到 2.45.0')!
    await update.trigger('click')
    expect(wrapper.emitted('update')?.[0]).toEqual([dep])
    expect(dependenciesApi.setSubStoreMode).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('已是最新')
  })
  it('keeps check, update and startup failures visible in their respective fields',()=>{
    const dep=dependency();dep.check_error='版本检查连接超时';dep.update={...dep.update,state:'failed',message:'更新失败，已回滚'}
    dep.lifecycle={...dep.lifecycle!,state:'failed',error:'启动未就绪，请检查容器日志'}
    const wrapper=render(dep)
    expect(wrapper.text()).toContain('版本检查连接超时')
    expect(wrapper.text()).toContain('更新失败，已回滚')
    expect(wrapper.text()).toContain('无法连接')
    expect(wrapper.text()).toContain('启动未就绪')
    expect(wrapper.text()).not.toContain('已是最新')
  })
  it('shows active update progress and locks the mode selector',()=>{
    const dep=dependency();dep.update={...dep.update,state:'running',message:'正在安装新版本'}
    dep.lifecycle={...dep.lifecycle!,state:'updating'}
    const wrapper=render(dep)
    expect(wrapper.text()).toContain('正在安装新版本')
    expect(wrapper.text()).toContain('更新中')
    expect(wrapper.get('[role="combobox"]').attributes('disabled')).toBeDefined()
  })
})
