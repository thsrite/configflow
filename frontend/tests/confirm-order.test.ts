import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import ConfirmHost from '@/components/feedback/ConfirmHost.vue'
import ProfileSwitcher from '@/components/ProfileSwitcher.vue'
import { Select } from '@/components/ui/select'
import * as feedback from '@/lib/feedback'
import { useProfileStore } from '@/stores/profile'
import { activeProfileId, setActiveProfileId } from '@/profileContext'

// Only the network boundary is replaced: no production requests or profile data.
vi.mock('@/api', () => ({ profileApi: { list: vi.fn(async () => ({ data: [
 { id: 'default', name: 'Original' }, { id: 'target', name: 'Target' }
] })) } }))
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => {
 wrappers.splice(0).forEach(w => w.unmount())
 feedback.settleConfirm('cancel')
 vi.restoreAllMocks()
 vi.unstubAllGlobals()
 document.body.innerHTML = ''
 sessionStorage.clear()
})
function host() {
 const w = mount(ConfirmHost, { attachTo: document.body })
 wrappers.push(w)
 return w
}
async function clickText(text: string) {
 await flushPromises()
 const button = Array.from(document.querySelectorAll('button')).find(b => b.textContent?.trim() === text)
 expect(button, `real button ${text} must exist`).toBeTruthy()
 button!.click()
 await flushPromises()
}
describe('confirmation choices and profile switching', () => {
 it('shows an optional external link that opens in a new tab without settling the dialog', async () => {
  host()
  const result = feedback.confirm('New version', { confirmText: 'Update', link: { href: 'https://example.com/notes', text: 'Release notes' } })
  await flushPromises()
  const link = Array.from(document.querySelectorAll('a')).find(a => a.textContent?.includes('Release notes'))!
  expect(link.getAttribute('href')).toBe('https://example.com/notes')
  expect(link.getAttribute('target')).toBe('_blank')
  expect(link.getAttribute('rel')).toContain('noopener')
  expect(feedback.confirmState.open).toBe(true)
  await clickText('Update')
  await expect(result).resolves.toBe(true)
 })
 it('drops a link that is not http(s)', async () => {
  host()
  void feedback.confirm('x', { link: { href: 'javascript:alert(1)', text: 'bad' } })
  await flushPromises()
  expect(Array.from(document.querySelectorAll('a')).some(a => a.textContent?.includes('bad'))).toBe(false)
 })
 it('uninstrumented confirmation click must resolve true', async () => {
  host()
  const result = feedback.confirm('Switch profile?', { confirmText: 'Switch' })
  await clickText('Switch')
  expect(await result).toBe(true)
 })
 it('cancel click resolves false (negative control)', async () => {
  host()
  const result = feedback.confirm('Switch profile?', { confirmText: 'Switch' })
  await clickText('取消')
  expect(await result).toBe(false)
 })
 it('alt click must resolve alt', async () => {
  host()
  const result = feedback.choose('Choose', { altText: 'Alternative' })
  await clickText('Alternative')
  const actual = await result
  expect(actual).toBe('alt')
 })
 it('confirmed profile change must keep Target and persist target', async () => {
  setActiveProfileId('default')
  await useProfileStore().refreshProfiles()
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/')
  await router.isReady()
  const w = mount(ProfileSwitcher, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(w)
  host()
  await flushPromises()
  // Emit the real Select's public selection event to isolate the confirmation boundary.
  w.findComponent(Select).vm.$emit('update:modelValue', 'target')
  await flushPromises()
  await clickText('切换')
  expect(w.text()).toContain('Target')
  expect(activeProfileId.value).toBe('target')
  expect(sessionStorage.getItem('configflow.activeProfile')).toBe('target')
 })
 it('cancelled profile change keeps Original', async () => {
  setActiveProfileId('default')
  await useProfileStore().refreshProfiles()
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/')
  await router.isReady()
  const w = mount(ProfileSwitcher, { attachTo: document.body, global: { plugins: [router] } })
  wrappers.push(w)
  host()
  await flushPromises()
  w.findComponent(Select).vm.$emit('update:modelValue', 'target')
  await flushPromises()
  await clickText('取消')
  expect(w.text()).toContain('Original')
  expect(activeProfileId.value).toBe('default')
  expect(sessionStorage.getItem('configflow.activeProfile')).toBe('default')
 })
})
