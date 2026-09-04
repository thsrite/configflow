/** 全站导航的唯一来源：按作用域分组，桌面 rail 与移动端底栏共用。 */

export type NavScope = 'overview' | 'resource' | 'profile' | 'system'

export interface NavItem {
  path: string
  label: string
  icon: string
  /** 功能开关控制的入口，关闭时不渲染 */
  flag?: 'subscriptionAggregation'
}

export interface NavGroup {
  scope: NavScope
  /** 移动端底栏标签 */
  tabLabel: string
  tabIcon: string
  /** 桌面 rail 分组标题；overview 无标题 */
  title?: string
  /** 分组作用域说明，供作用域横幅使用 */
  hint?: string
  items: NavItem[]
}

export const NAV_GROUPS: NavGroup[] = [
  {
    scope: 'overview',
    tabLabel: '总览',
    tabIcon: 'DataAnalysis',
    items: [{ path: '/dashboard', label: '数据统计', icon: 'DataAnalysis' }]
  },
  {
    scope: 'resource',
    tabLabel: '资源',
    tabIcon: 'Files',
    title: '资源',
    // 注意：这些资源当前仍按配置空间隔离，尚未真正共享。
    // 在存储层实现共享之前，此处不得宣称「所有配置空间共用」。
    hint: '订阅、节点与规则集',
    items: [
      { path: '/subscriptions', label: '订阅来源', icon: 'Link' },
      { path: '/nodes', label: '节点库', icon: 'Connection' },
      {
        path: '/subscription-aggregation',
        label: '订阅聚合',
        icon: 'Share',
        flag: 'subscriptionAggregation'
      },
      { path: '/rule-library', label: '规则库', icon: 'FolderOpened' }
    ]
  },
  {
    scope: 'profile',
    tabLabel: '当前配置',
    tabIcon: 'Postcard',
    title: '当前配置',
    hint: '仅属于本配置空间',
    items: [
      { path: '/proxy-groups', label: '策略组', icon: 'Grid' },
      { path: '/rules', label: '策略规则', icon: 'Document' },
      { path: '/generate', label: '配置生成', icon: 'Download' }
    ]
  },
  {
    scope: 'system',
    tabLabel: '系统',
    tabIcon: 'Setting',
    title: '系统',
    items: [
      { path: '/profiles', label: '配置空间', icon: 'Setting' },
      { path: '/agents', label: 'Agent', icon: 'Monitor' },
      { path: '/logs', label: '日志', icon: 'Tickets' }
    ]
  }
]

const PATH_SCOPE = new Map<string, NavScope>(
  NAV_GROUPS.flatMap(group => group.items.map(item => [item.path, group.scope] as const))
)

export const scopeOfPath = (path: string): NavScope | undefined => PATH_SCOPE.get(path)

export const groupOfScope = (scope: NavScope): NavGroup | undefined =>
  NAV_GROUPS.find(group => group.scope === scope)

export const labelOfPath = (path: string): string | undefined =>
  NAV_GROUPS.flatMap(group => group.items).find(item => item.path === path)?.label
