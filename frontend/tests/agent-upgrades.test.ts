import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { agentApi } from '@/api'
import { useAgentUpgrades } from '@/composables/useAgentUpgrades'
vi.mock('@/api', () => ({ agentApi: { getUpgrade: vi.fn(), update: vi.fn() } }))
const wrappers: ReturnType<typeof mount>[] = []
function setup() {
 let flow!: ReturnType<typeof useAgentUpgrades>
 const success = vi.fn()
 wrappers.push(mount(defineComponent({setup(){flow=useAgentUpgrades(success);return()=>null}})))
 return {flow,success}
}
const state = (status: string) => ({update_id:'same-upgrade',target_version:'1.3.0-go',previous_version:'1.0.8-go',status})
beforeEach(()=>{vi.useFakeTimers();vi.resetAllMocks()})
afterEach(()=>{wrappers.splice(0).forEach(w=>w.unmount());vi.useRealTimers()})
it('acceptance and temporary disconnection never imply success', async()=>{
 const {flow,success}=setup()
 vi.mocked(agentApi.update).mockResolvedValue({data:state('queued')} as any)
 vi.mocked(agentApi.getUpgrade).mockRejectedValueOnce(new Error('restart')).mockResolvedValueOnce({data:state('succeeded')} as any)
 await flow.start('a');expect(success).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(0);expect(flow.busy('a')).toBe(true);expect(success).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(1500);expect(success).toHaveBeenCalledTimes(1);expect(flow.busy('a')).toBe(false)
 flow.track('a',state('checking'));expect(flow.upgrades.a.status).toBe('succeeded')
})
it('restores a persisted task and keeps failed rollback blocked', async()=>{
 const {flow,success}=setup()
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('rollback_failed')} as any)
 flow.track('a',state('unknown'))
 await vi.advanceTimersByTimeAsync(0)
 expect(flow.busy('a')).toBe(true);expect(success).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(10000);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(1)
})
it('an unconfirmed result is polled slowly after the first query', async()=>{
 const {flow}=setup()
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('unknown')} as any)
 flow.track('a',state('unknown'))
 await vi.advanceTimersByTimeAsync(0);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(1)
 await vi.advanceTimersByTimeAsync(1500);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(1)
 await vi.advanceTimersByTimeAsync(13500);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(2)
})
it('closing the page stops polling without submitting another update',async()=>{
 const {flow}=setup();flow.track('a',state('checking'))
 wrappers.splice(0).forEach(w=>w.unmount())
 await vi.advanceTimersByTimeAsync(10000)
 expect(agentApi.getUpgrade).not.toHaveBeenCalled();expect(agentApi.update).not.toHaveBeenCalled()
})
it('prevents duplicate submission while the first request is pending', async()=>{
 const {flow}=setup()
 let resolve!: (value: any)=>void
 vi.mocked(agentApi.update).mockImplementation(()=>new Promise(r=>{resolve=r}))
 const first=flow.start('a');await flow.start('a')
 expect(agentApi.update).toHaveBeenCalledTimes(1);expect(flow.busy('a')).toBe(true)
 expect(flow.upgrades.a.status).toBe('submitting')
 resolve({data:state('queued')});await first
 expect(flow.busy('a')).toBe(true)
})
it('keeps the last reported stage on disconnection and clears it after reconnecting', async()=>{
 const {flow}=setup()
 const events=[{at:'2026-10-18T12:00:00Z',status:'restarting',message:'正在重启 Agent 1.8.21-go'}]
 flow.track('a',{...state('restarting'),events})
 vi.mocked(agentApi.getUpgrade).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({data:{...state('checking'),events}} as any)
 await vi.advanceTimersByTimeAsync(0)
 expect(flow.upgrades.a).toMatchObject({status:'restarting',query_error:true,events})
 await vi.advanceTimersByTimeAsync(1500)
 expect(flow.upgrades.a).toMatchObject({status:'checking',query_error:false,events})
})
it('rejects older same-task snapshots without ordering stages by a guessed sequence',()=>{
 const {flow}=setup()
 const events=[{at:'2026-10-18T12:00:01Z',status:'restarting',message:'重启 Agent'}]
 flow.track('a',{...state('restarting'),updated_at:101,events})
 flow.track('a',{...state('checking'),updated_at:100})
 expect(flow.upgrades.a.status).toBe('restarting')
 expect(flow.upgrades.a.events).toEqual(events)
 flow.track('a',{...state('checking'),updated_at:102})
 expect(flow.upgrades.a.status).toBe('checking')
 expect(flow.upgrades.a.events).toEqual(events)
})
it('does not mistake an old success for a newly submitted update', async()=>{
 const {flow,success}=setup()
 flow.track('a',state('succeeded'));success.mockClear()
 let reject!: (error:any)=>void
 vi.mocked(agentApi.update).mockImplementation(()=>new Promise((_r,no)=>{reject=no}))
 const first=flow.start('a')
 flow.track('a',state('succeeded'))
 expect(flow.upgrades.a.status).toBe('submitting')
 reject(new Error('timeout'));await expect(first).rejects.toThrow('timeout')
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('succeeded')} as any)
 await vi.advanceTimersByTimeAsync(0)
 expect(flow.upgrades.a.status).toBe('unknown');expect(success).not.toHaveBeenCalled()
})
it('explicit reconciliation finds a new task without sending another update', async()=>{
 const {flow}=setup()
 flow.track('a',state('succeeded'))
 vi.mocked(agentApi.update).mockRejectedValueOnce(new Error('timeout'))
 await expect(flow.start('a')).rejects.toThrow()
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:{...state('downloading'),update_id:'new-task'}} as any)
 const confirmRetry=vi.fn().mockResolvedValue(true)
 await flow.retryUnknown('a',confirmRetry)
 expect(flow.upgrades.a.update_id).toBe('new-task')
 expect(agentApi.update).toHaveBeenCalledTimes(1);expect(confirmRetry).not.toHaveBeenCalled()
})
it.each(['old','missing'])('rechecks and asks before retrying an unrecorded update (%s)', async condition=>{
 const {flow}=setup()
 flow.track('a',state('succeeded'))
 vi.mocked(agentApi.update).mockRejectedValueOnce(new Error('timeout')).mockResolvedValueOnce({data:{...state('queued'),update_id:'new-task'}} as any)
 await expect(flow.start('a')).rejects.toThrow()
 if(condition==='old')vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('succeeded')} as any)
 else vi.mocked(agentApi.getUpgrade).mockRejectedValue({response:{status:404}})
 const confirmRetry=vi.fn().mockResolvedValue(true)
 await flow.retryUnknown('a',confirmRetry)
 expect(confirmRetry).toHaveBeenCalledOnce();expect(agentApi.update).toHaveBeenCalledTimes(2)
 expect(flow.upgrades.a.update_id).toBe('new-task')
})
it('manual query returns the latest state even while background polling is pending', async()=>{
 const {flow}=setup()
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('failed')} as any)
 flow.track('a',state('unknown'))
 const pending=flow.query('a');expect(flow.querying.has('a')).toBe(true)
 expect((await pending)?.status).toBe('failed')
 expect(flow.querying.has('a')).toBe(false);expect(flow.busy('a')).toBe(false)
 await vi.advanceTimersByTimeAsync(20000);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(1)
})
it('manual query surfaces request errors and resumes polling', async()=>{
 const {flow}=setup()
 vi.mocked(agentApi.getUpgrade).mockRejectedValueOnce(new Error('offline')).mockResolvedValue({data:state('unknown')} as any)
 flow.track('a',state('unknown'))
 await expect(flow.query('a')).rejects.toThrow('offline')
 expect(flow.querying.has('a')).toBe(false)
 await vi.advanceTimersByTimeAsync(15000);expect(agentApi.getUpgrade).toHaveBeenCalledTimes(2)
})
it('a task restored on page load is queried immediately', async()=>{
 const {flow}=setup()
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('failed')} as any)
 flow.track('a',state('unknown'))
 await vi.advanceTimersByTimeAsync(0)
 expect(agentApi.getUpgrade).toHaveBeenCalledTimes(1);expect(flow.busy('a')).toBe(false)
})
it('a task already succeeded when first seen is not announced; one watched to success is', async()=>{
 const {flow,success}=setup()
 // 首次载入列表：每台设备上一次升级早已成功，页面不需要为它们再读列表
 flow.track('a',state('succeeded'));flow.track('b',state('succeeded'))
 expect(success).not.toHaveBeenCalled();expect(flow.busy('a')).toBe(false)
 flow.track('a',state('succeeded'));expect(success).not.toHaveBeenCalled()
 // 载入时还在进行，之后看着它成功
 vi.mocked(agentApi.getUpgrade).mockResolvedValue({data:state('succeeded')} as any)
 flow.track('c',state('installing'));expect(success).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(0);expect(success).toHaveBeenCalledTimes(1)
 flow.track('c',state('succeeded'));expect(success).toHaveBeenCalledTimes(1)
})
