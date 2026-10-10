import { createRouter, createWebHistory } from 'vue-router'
import api from '@/api'
import { checkNavigationAuth, invalidateAuthSession } from '@/authSession'

// 扩展 RouteMeta 接口以支持自定义属性
declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresSubscriptionAggregation?: boolean
  }
}

const routes = [
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
    path: '/agents',
    name: 'Agents',
    component: () => import('@/views/Agents.vue')
  },
  {
    path: '/logs',
    name: 'Logs',
    component: () => import('@/views/Logs.vue')
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 路由守卫
router.beforeEach(async (to) => {
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

export default router
