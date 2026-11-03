import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SubscriptionDialersSummary from '@/components/generate/SubscriptionDialersSummary.vue'
import type { SubscriptionDialersReport } from '@/types'

const report = (overrides: Partial<SubscriptionDialersReport> = {}): SubscriptionDialersReport => ({
  groups: [{ subscription: 'Airport', target: 'Relay', group: 'Airport/Relay', members: ['Relay-KR'], copied: false }],
  skipped: [],
  skipped_count: 0,
  ...overrides,
})
const text = (wrapper: ReturnType<typeof mount>) => wrapper.text().replace(/\s+/g, ' ')

describe('subscription dialers summary', () => {
  it('counts the generated relay groups without a warning when nothing was skipped', () => {
    const wrapper = mount(SubscriptionDialersSummary, { props: { report: report() } })
    expect(text(wrapper.find('[data-subscription-dialers-counts]'))).toBe('自动补上中转分组 1 个')
    expect(wrapper.find('ul').exists()).toBe(false)
  })

  it('lists skipped nodes with what to do and counts the ones not listed', () => {
    const wrapper = mount(SubscriptionDialersSummary, { props: { report: report({
      groups: [],
      skipped: [
        { subscription: 'Airport', node: '🇰🇷 韩国 01', target: 'Relay', reason: 'refresh' },
        { subscription: 'Airport', node: '🇭🇰 香港 02', target: 'Relay-HK', reason: 'missing' },
      ],
      skipped_count: 5,
    }) } })
    expect(text(wrapper.find('[data-subscription-dialers-counts]'))).toBe('自动补上中转分组 0 个，未能补上的节点 5 个')
    const items = wrapper.findAll('li').map(item => item.text())
    expect(items).toEqual([
      'Airport「🇰🇷 韩国 01」：找不到中转「Relay」，订阅缓存还没有记录机场的分组，在「订阅来源」刷新这个订阅后会自动补上，刷新前发布会报错',
      'Airport「🇭🇰 香港 02」：找不到中转「Relay-HK」，机场的这个分组没法补上，本次跳过这个节点',
      '另有 3 个，见服务端日志',
    ])
  })
})
