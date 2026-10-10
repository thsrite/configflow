import { createRouter, createWebHistory } from 'vue-router'
import api from '@/api'

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
router.beforeEach(async (to, from, next) => {
  // 登录页面直接放行
  if (to.path === '/login') {
    next()
    return
  }

  try {
    // 检查是否启用了认证
    const response = await api.get('/auth/status')
    const authEnabled = response.data.authEnabled

    // 如果没有启用认证，直接放行（仅检查订阅聚合权限）
    if (!authEnabled) {
      // 如果需要订阅聚合权限，检查订阅聚合是否启用
      if (to.meta.requiresSubscriptionAggregation) {
        try {
          const aggResponse = await api.get('/settings/subscription-aggregation')
          const subscriptionAggregationEnabled = aggResponse.data.enabled || false
          if (!subscriptionAggregationEnabled) {
            console.warn('访问被拒绝：订阅聚合功能未启用')
            next('/subscriptions')
            return
          }
        } catch (error) {
          console.error('Failed to check subscription aggregation status:', error)
          // 检查失败，使用 localStorage 备份
          const localEnabled = localStorage.getItem('subscriptionAggregationEnabled') === 'true'
          if (!localEnabled) {
            next('/subscriptions')
            return
          }
        }
      }

      next()
      return
    }

    // 检查是否有 token
    const token = localStorage.getItem('token')
    if (!token) {
      // 没有 token，跳转到登录页
      next('/login')
      return
    }

    // 验证 token 是否有效
    try {
      await api.get('/auth/verify')

      // token 有效，检查订阅聚合权限
      if (to.meta.requiresSubscriptionAggregation) {
        try {
          const aggResponse = await api.get('/settings/subscription-aggregation')
          const subscriptionAggregationEnabled = aggResponse.data.enabled || false
          if (!subscriptionAggregationEnabled) {
            console.warn('访问被拒绝：订阅聚合功能未启用')
            next('/subscriptions')
            return
          }
        } catch (error) {
          console.error('Failed to check subscription aggregation status:', error)
          // 检查失败，使用 localStorage 备份
          const localEnabled = localStorage.getItem('subscriptionAggregationEnabled') === 'true'
          if (!localEnabled) {
            next('/subscriptions')
            return
          }
        }
      }

      // 所有检查通过，允许访问
      next()
    } catch (error) {
      // token 无效，清除 token 并跳转到登录页
      localStorage.removeItem('token')
      localStorage.removeItem('username')
      next('/login')
    }
  } catch (error) {
    // 检查认证状态失败，可能是服务器错误，直接放行
    console.error('Failed to check auth status:', error)
    next()
  }
})

export default router
