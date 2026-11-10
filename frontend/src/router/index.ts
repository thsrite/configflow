import { createRouter, createWebHistory, isNavigationFailure, NavigationFailureType, type RouteLocationNormalized } from 'vue-router'
import api from '@/api'
import { checkNavigationAuth, invalidateAuthSession } from '@/authSession'
import { notify } from '@/lib/feedback'
import { clearRecoveryParameter, recoverPage, type RecoveryResult } from '@/lib/pageRecovery'
import { oxidnsMonitorPath, rememberedOxiDNSDevice } from '@/lib/oxidnsDevice'

// 扩展 RouteMeta 接口以支持自定义属性
declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresSubscriptionAggregation?: boolean
  }
}

const routes = [
  { path: '/setup', name: 'Setup', component: () => import('@/views/Setup.vue') },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    redirect: '/dashboard'
  },
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/views/Dashboard.vue')
  },
  {
    path: '/oxidns',
    name: 'OxiDNSMonitorEntry',
    component: () => import('@/views/OxiDNSMonitorEntry.vue'),
    // 记得上次看的设备就直接打开它，不先读设备列表；设备已不存在时面板会退回这里重新选择
    beforeEnter: (to: RouteLocationNormalized) => {
      const id = rememberedOxiDNSDevice()
      return id ? { path: oxidnsMonitorPath(id), query: to.query } : true
    }
  },
  {
    path: '/mihomo',
    name: 'MihomoPanel',
    component: () => import('@/views/MihomoPanel.vue')
  },
  {
    path: '/profiles',
    name: 'Profiles',
    component: () => import('@/views/Profiles.vue')
  },
  {
    path: '/subscriptions',
    name: 'Subscriptions',
    component: () => import('@/views/Subscriptions.vue')
  },
  {
    path: '/nodes',
    name: 'Nodes',
    component: () => import('@/views/Nodes.vue')
  },
  {
    path: '/subscription-aggregation',
    name: 'SubscriptionAggregation',
    component: () => import('@/views/SubscriptionAggregation.vue'),
    meta: { requiresSubscriptionAggregation: true }
  },
  {
    path: '/rule-library',
    name: 'RuleLibrary',
    component: () => import('@/views/RuleLibrary.vue')
  },
  {
    path: '/rules',
    name: 'Rules',
    component: () => import('@/views/Rules.vue')
  },
  {
    path: '/client-devices',
    name: 'ClientDevices',
    component: () => import('@/views/ClientDevices.vue')
  },
  {
    path: '/domain-discovery',
    name: 'DomainDiscovery',
    component: () => import('@/views/DomainDiscovery.vue')
  },
  {
    path: '/proxy-groups',
    name: 'ProxyGroups',
    component: () => import('@/views/ProxyGroups.vue')
  },
  {
    path: '/system-settings',
    name: 'SystemSettings',
    component: () => import('@/views/SystemSettings.vue')
  },
  {
    path: '/generate',
    name: 'Generate',
    component: () => import('@/views/Generate.vue')
  },
  {
    path: '/routers',
    name: 'Routers',
    component: () => import('@/views/Routers.vue')
  },
  {
    path: '/agents',
    name: 'Agents',
    component: () => import('@/views/Agents.vue')
  },
  {
    path: '/agents/:agentId/oxidns',
    name: 'OxiDNSMonitor',
    component: () => import('@/views/OxiDNSMonitor.vue')
  },
  {
    path: '/logs',
    name: 'Logs',
    component: () => import('@/views/Logs.vue')
  }
]

clearRecoveryParameter()
const router = createRouter({
  history: createWebHistory(),
  routes
})

// Keep real waits (initial authentication, feature checks, lazy page imports)
// visible without flashing a loading message on an ordinary page switch.
let pendingNavigation: { to: RouteLocationNormalized; timer: ReturnType<typeof setTimeout>; toast?: string | number } | undefined
let navigationVersion = 0
export let pageLoadRecovery: Promise<RecoveryResult> | undefined
const finishNavigation = (to?: RouteLocationNormalized) => {
  if (!pendingNavigation || (to && pendingNavigation.to !== to)) return
  clearTimeout(pendingNavigation.timer)
  if (pendingNavigation.toast !== undefined) notify.dismiss(pendingNavigation.toast)
  pendingNavigation = undefined
}
const startNavigation = (to: RouteLocationNormalized) => {
  navigationVersion++
  finishNavigation()
  const navigation = {
    to,
    timer: setTimeout(() => {
      if (pendingNavigation === navigation) navigation.toast = notify.loading('正在打开页面…')
    }, 200),
    toast: undefined as string | number | undefined,
  }
  pendingNavigation = navigation
}

// 路由守卫
router.beforeEach(async (to) => {
  startNavigation(to)
  // 登录页面直接放行
  if (to.path === '/login') {
    invalidateAuthSession()
    return
  }

  try {
    if (!await checkNavigationAuth(api)) return '/login'
  } catch (error: any) {
    if (error.response?.status === 401) return '/login'
    // 临时网络或服务端故障不等于登录失效。后续业务 API 仍由服务端鉴权。
    console.error('Failed to check authentication:', error)
  }

  if (to.meta.requiresSubscriptionAggregation) {
    try {
      const { data } = await api.get('/settings/subscription-aggregation')
      if (!data.enabled) return '/subscriptions'
    } catch (error) {
      console.error('Failed to check subscription aggregation status:', error)
      if (localStorage.getItem('subscriptionAggregationEnabled') !== 'true') return '/subscriptions'
    }
  }
})

router.afterEach((to, _from, failure) => {
  // Component leave guards run before beforeEach. Even an aborted new click
  // retires a version check started for an earlier destination.
  if (isNavigationFailure(failure, NavigationFailureType.aborted)) navigationVersion++
  // Clicking the current page cancels an outstanding navigation, but Vue Router
  // skips beforeEach for that duplicated target. Retire the superseded wait now
  // so a late import result cannot leave a spinner or report an obsolete error.
  if (isNavigationFailure(failure, NavigationFailureType.duplicated)) {
    navigationVersion++
    finishNavigation()
  }
  else finishNavigation(to)
})
router.onError((error, to) => {
  if (pendingNavigation?.to !== to) return
  const version = navigationVersion
  finishNavigation(to)
  pageLoadRecovery = recoverPage(error, to.fullPath, () => version === navigationVersion)
  void pageLoadRecovery.then(result => {
    if (version !== navigationVersion || result === 'reloading' || result === 'stale') return
    if (result === 'blocked') {
      notify.error('页面已更新，当前内容已保留', '请先保存正在编辑的内容或关闭编辑窗口，再重新打开目标页面。')
    } else {
      notify.error('页面打开失败，请重试', '可能正在更新或网络暂时不稳定，当前页面内容已保留。')
    }
  })
})

export default router
