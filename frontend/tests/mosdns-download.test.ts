import { Blob as NodeBlob } from 'node:buffer'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import api, { generateApi } from '@/api'
import Generate from '@/views/Generate.vue'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import { notify } from '@/lib/feedback'
import { scopedRequests, setActiveProfileId } from '@/profileContext'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

const originalAdapter = api.defaults.adapter
const wrappers: ReturnType<typeof shallowMount>[] = []
const response = (config: InternalAxiosRequestConfig, data: unknown = {}): AxiosResponse => ({
  config, data, status: 200, statusText: 'OK', headers: {}
})

beforeEach(() => {
  // jsdom's Blob lacks text(); use the browser-compatible Node implementation.
  vi.stubGlobal('Blob', NodeBlob)
  setActiveProfileId('download-profile')
  scopedRequests.value = 0
  api.defaults.adapter = async config => response(config)
  vi.spyOn(notify, 'error').mockImplementation(() => 0)
  vi.spyOn(notify, 'success').mockImplementation(() => 0)
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  api.defaults.adapter = originalAdapter
  scopedRequests.value = 0
  sessionStorage.clear()
  localStorage.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function renderDownload() {
  const wrapper = shallowMount(Generate)
  wrappers.push(wrapper)
  await flushPromises()
  const action = () => wrapper.getComponent(GenerateStudio).props('targets')
    .find((target: any) => target.key === 'mosdns')!.actions
    .find((item: any) => item.label === '下载')!
  return action
}

describe('MosDNS ZIP download feedback', () => {
  it('uses a one-minute ZIP timeout without changing other downloads', async () => {
    const requests: InternalAxiosRequestConfig[] = []
    api.defaults.adapter = async config => {
      requests.push(config)
      return response(config)
    }
    await generateApi.mosdns('download-profile')
    await generateApi.mihomo('download-profile')
    expect(requests[0]).toMatchObject({
      url: '/profiles/download-profile/generate/mosdns', responseType: 'blob', timeout: 60000
    })
    expect(requests[1].timeout).toBe(30000)
  })

  it('shows the JSON Blob error and restores the download action after failure', async () => {
    const action = await renderDownload()
    let rejectDownload!: () => void
    api.defaults.adapter = config => new Promise((_resolve, reject) => {
      rejectDownload = () => reject({
        config,
        response: {
          status: 500,
          data: new Blob([JSON.stringify({ message: '规则「Example」下载失败，请检查规则源' })], { type: 'application/json' })
        }
      })
    })
    const downloading = action().run()
    await flushPromises()
    expect(action().loading).toBe(true)
    rejectDownload()
    await downloading
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('生成 MosDNS 配置失败：规则「Example」下载失败，请检查规则源')
    expect(action().loading).toBe(false)
    expect(scopedRequests.value).toBe(0)
    expect(notify.success).not.toHaveBeenCalled()
  })

  it.each(['ECONNABORTED', 'ETIMEDOUT'])('explains %s as a timeout and allows retry', async code => {
    const action = await renderDownload()
    api.defaults.adapter = async config => { throw { config, code } }
    await action().run()
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('生成 MosDNS 配置超时，请稍后重试')
    expect(action().loading).toBe(false)
  })

  it.each([
    ['text/html', '<html><body>502 Bad Gateway</body></html>'],
    ['application/json', '{invalid json'],
    ['application/json', JSON.stringify({ message: null })]
  ])('does not display unreadable %s gateway responses', async (type, body) => {
    const action = await renderDownload()
    api.defaults.adapter = async config => {
      throw { config, response: { status: 502, data: new Blob([body], { type }) } }
    }
    await action().run()
    await flushPromises()
    expect(notify.error).toHaveBeenCalledWith('生成 MosDNS 配置失败')
    expect(action().loading).toBe(false)
  })
})
