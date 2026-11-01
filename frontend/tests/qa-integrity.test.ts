import {beforeAll,beforeEach,afterEach,describe,it,expect,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import Nodes from '@/views/Nodes.vue'
import Subscriptions from '@/views/Subscriptions.vue'
import Rules from '@/views/Rules.vue'
import Profiles from '@/views/Profiles.vue'
import ProxyGroups from '@/views/ProxyGroups.vue'
import ConfirmHost from '@/components/feedback/ConfirmHost.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import api,{nodeApi,subscriptionApi,ruleApi,ruleSetApi,profileApi,proxyGroupApi} from '@/api'
import {notify,settleConfirm} from '@/lib/feedback'
import {setActiveProfileId} from '@/profileContext'
vi.mock('@/api',()=>{
 const crud=()=>({getAll:vi.fn(),list:vi.fn(),create:vi.fn(),update:vi.fn(),delete:vi.fn(),fetch:vi.fn()})
 return {default:{get:vi.fn(),put:vi.fn(),post:vi.fn(),delete:vi.fn()},nodeApi:{...crud(),previewDeletion:vi.fn(),deleteWithReferences:vi.fn()},subscriptionApi:crud(),ruleApi:crud(),ruleSetApi:crud(),profileApi:crud(),proxyGroupApi:crud()}
})
let nodes:any[],groups:any[],rules:any[],subs:any[],profiles:any[]
const wrappers:any[]=[]
const clone=(x:any)=>JSON.parse(JSON.stringify(x))
beforeAll(()=>{Object.defineProperty(Element.prototype,'scrollIntoView',{configurable:true,value:vi.fn()});Object.defineProperty(window,'matchMedia',{configurable:true,value:vi.fn(()=>({matches:false,addEventListener:vi.fn(),removeEventListener:vi.fn(),addListener:vi.fn(),removeListener:vi.fn()}))})})
beforeEach(()=>{
 vi.resetAllMocks();window.sessionStorage.clear();setActiveProfileId('default')
 nodes=[{id:'n1',name:'Alpha',enabled:true,proxy_string:'http://alpha.test:80'},{id:'n2',name:'Beta',enabled:true,proxy_string:'http://beta.test:80'}]
 groups=[{id:'g1',name:'Old',type:'select',enabled:true,manual_nodes:['DIRECT'],proxies_order:[]},{id:'g2',name:'Other',type:'select',enabled:true,manual_nodes:['REJECT']}]
 rules=[{id:'r1',itemType:'rule',rule_type:'DOMAIN-SUFFIX',value:'example.test',policy:'Old',enabled:true}]
 subs=[{id:'s1',name:'Feed',url:'https://feed.test',enabled:true,type:'universal',interval:86400}]
 profiles=[{id:'default',name:'Default'},{id:'target',name:'Target'}]
 for(const [obj,data] of [[nodeApi,()=>nodes],[proxyGroupApi,()=>groups],[ruleApi,()=>rules],[ruleSetApi,()=>[]],[subscriptionApi,()=>subs],[profileApi,()=>profiles]] as any){obj.getAll.mockImplementation(async()=>({data:clone(data())}));obj.list.mockImplementation(async()=>({data:clone(data())}));for(const op of ['create','update','delete','fetch'])obj[op].mockResolvedValue({data:{}})}
 vi.mocked(api.get).mockImplementation(async(path:any)=>({data:clone(path==='/rules'?rules:path==='/proxy-groups'?groups:path==='/subscriptions'?subs:path==='/aggregations'?[{id:'a1',name:'On',enabled:true},{id:'a2',name:'Off',enabled:false}]:[])}))
 for(const op of ['put','post','delete'])vi.mocked(api[op]).mockResolvedValue({data:{}})
 vi.spyOn(notify,'error').mockImplementation(()=>0);vi.spyOn(notify,'warning').mockImplementation(()=>0);vi.spyOn(notify,'success').mockImplementation(()=>0)
})
afterEach(()=>{wrappers.splice(0).forEach(w=>w.unmount());settleConfirm('cancel');document.body.innerHTML='';vi.restoreAllMocks()})
async function render(c:any){const w=mount(c,{attachTo:document.body});wrappers.push(w);await flushPromises();return w}
async function click(text:string){await flushPromises();const b=[...document.querySelectorAll('button')].find(x=>x.textContent?.trim()===text);expect(b,`button ${text}`).toBeTruthy();b!.click();await flushPromises()}
async function fill(id:string,value:string){const e=document.querySelector(id) as HTMLInputElement;expect(e,id).toBeTruthy();e.value=value;e.dispatchEvent(new Event('input',{bubbles:true}));await flushPromises()}
async function attr(selector:string){const e=document.querySelector(selector) as HTMLElement;expect(e,selector).toBeTruthy();e.click();await flushPromises()}
const dialog=()=>document.querySelector('[role="dialog"]')
describe('QA integrity regressions',()=>{
 it('referenced group disable rejection restores enabled state and displays guidance',async()=>{
  await render(ProxyGroups);let submitted:any;vi.mocked(proxyGroupApi.update).mockImplementation(async(_id,data)=>{submitted=clone(data);throw {response:{data:{message:'请先修改引用'}}}});await attr('[aria-label="停用 Old"]');expect(submitted.enabled).toBe(false);expect(notify.error).toHaveBeenCalledWith('请先修改引用');expect(document.querySelector('[aria-label="停用 Old"]')).not.toBeNull();expect(api.put).not.toHaveBeenCalled()
 })
 it('rejected group deletion leaves dependent rules untouched and reports server guidance',async()=>{
  await render(ProxyGroups);await render(ConfirmHost);vi.mocked(proxyGroupApi.delete).mockRejectedValue({response:{data:{message:'请先修改引用'}}});await click('删除');const confirmButton=[...document.querySelectorAll('[role="alertdialog"] button')].find(b=>b.textContent?.trim()==='删除') as HTMLButtonElement;expect(confirmButton).toBeTruthy();confirmButton.click();await flushPromises();expect(proxyGroupApi.delete).toHaveBeenCalledWith('g1','default');expect(api.delete).not.toHaveBeenCalled();expect(notify.error).toHaveBeenCalledWith('请先修改引用')
 })
 it('rejected group rename never writes dependent resources before primary save',async()=>{
  await render(ProxyGroups);await click('编辑');await fill('#group-name','NewName');vi.mocked(proxyGroupApi.update).mockRejectedValue({response:{data:{message:'引用冲突'}}});await click('保存');expect(proxyGroupApi.update).toHaveBeenCalledWith('g1',expect.objectContaining({name:'NewName'}),'default');expect(api.put).not.toHaveBeenCalled();expect(api.delete).not.toHaveBeenCalled();expect(dialog()).not.toBeNull();expect(notify.error).toHaveBeenCalledWith('引用冲突')
 })
 it.each(['', '   '])('subscription create and edit require nonblank name and URL: %j',async(blank)=>{
  await render(Subscriptions);await click('添加订阅');await fill('#sub-name',blank);await fill('#sub-url','https://valid.test');await click('保存');expect(subscriptionApi.create).not.toHaveBeenCalled();await fill('#sub-name','Valid');await fill('#sub-url',blank);await click('保存');expect(subscriptionApi.create).not.toHaveBeenCalled();await click('取消');await attr('[aria-label="编辑 Feed"]');await fill('#sub-name',blank);await click('保存');expect(subscriptionApi.update).not.toHaveBeenCalled();await fill('#sub-name','Feed');await fill('#sub-url',blank);await click('保存');expect(subscriptionApi.update).not.toHaveBeenCalled();expect(dialog()).not.toBeNull();await fill('#sub-url','ss://valid-existing-protocol');await click('保存');expect(subscriptionApi.update).toHaveBeenCalledWith('s1',expect.objectContaining({name:'Feed',url:'ss://valid-existing-protocol'}))
 })
 it('filtered select-all deletes only visible rows after explicit confirmation',async()=>{
  const w=await render(Nodes);await render(ConfirmHost);await w.find('input').setValue('Alpha');await flushPromises();expect(w.text()).not.toContain('Beta');vi.mocked(nodeApi.previewDeletion).mockImplementation(async(ids:string[])=>({data:{nodes:nodes.filter(n=>ids.includes(n.id)).map(n=>({id:n.id,name:n.name})),usages:[],groups:[],blockers:[],empty_sources:[],confirmation:'confirm-n1'}}) as any);vi.mocked(nodeApi.deleteWithReferences).mockResolvedValue({data:{success:true,deleted_ids:['n1']}} as any);await click('全选');await click('删除 1 项');expect(nodeApi.previewDeletion).toHaveBeenCalledWith(['n1']);const dialog=document.querySelector('[role="dialog"]')!;expect(dialog.textContent).toContain('Alpha');expect(dialog.textContent).not.toContain('Beta');await click('删除节点');expect(nodeApi.deleteWithReferences).toHaveBeenCalledWith(['n1'],'confirm-n1');expect(nodeApi.delete).not.toHaveBeenCalled()
 })
 it('visible deselection preserves explicitly selected hidden rows',async()=>{
  const w=await render(Nodes);await click('全选');await w.find('input').setValue('Alpha');await flushPromises();await click('取消全选');expect(w.text()).toContain('删除 1 项');await w.find('input').setValue('Beta');await flushPromises();await click('取消全选');expect(w.text()).not.toContain('删除 1 项')
 })
})
