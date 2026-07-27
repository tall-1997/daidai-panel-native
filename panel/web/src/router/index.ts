import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getCachedPanelTitle, loadPanelSettings } from '@/utils/panelSettings'

const roleLevel: Record<string, number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
}

function hasRequiredRole(role: string | undefined, minRole: string | undefined) {
  if (!minRole) return true
  if (!role) return false
  return (roleLevel[role] || 0) >= (roleLevel[minRole] || 0)
}

const legacyRouteMap: Record<string, string> = {
  '/notifications': '/admin/notifications',
  '/users': '/admin/users',
  '/open-api': '/admin/open-api',
  '/admin/deps': '/deps',
  '/api-docs': '/docs/api',
}

const routeComponents = {
  login: () => import('@/views/login/index.vue'),
  layout: () => import('@/layouts/MainLayout.vue'),
  dashboard: () => import('@/views/dashboard/index.vue'),
  tasks: () => import('@/views/tasks/index.vue'),
  scripts: () => import('@/views/scripts/index.vue'),
  envs: () => import('@/views/envs/index.vue'),
  configFile: () => import('@/views/config-file/index.vue'),
  subscriptions: () => import('@/views/subscriptions/index.vue'),
  logs: () => import('@/views/logs/index.vue'),
  deps: () => import('@/views/deps/index.vue'),
  notifications: () => import('@/views/notifications/index.vue'),
  users: () => import('@/views/users/index.vue'),
  profile: () => import('@/views/profile/index.vue'),
  apiDocs: () => import('@/views/api-docs/index.vue'),
  settings: () => import('@/views/settings/index.vue'),
  openApi: () => import('@/views/open-api/index.vue'),
}

const routePreloaders: Record<string, () => Promise<unknown>> = {
  '/dashboard': routeComponents.dashboard,
  '/tasks': routeComponents.tasks,
  '/scripts': routeComponents.scripts,
  '/envs': routeComponents.envs,
  '/config-file': routeComponents.configFile,
  '/subscriptions': routeComponents.subscriptions,
  '/logs': routeComponents.logs,
  '/deps': routeComponents.deps,
  '/notifications': routeComponents.notifications,
  '/users': routeComponents.users,
  '/profile': routeComponents.profile,
  '/docs/api': routeComponents.apiDocs,
  '/admin/settings': routeComponents.settings,
  '/admin/notifications': routeComponents.notifications,
  '/admin/users': routeComponents.users,
  '/admin/open-api': routeComponents.openApi,
}

const preloadedRoutes = new Set<string>()

function normalizePreloadPath(path: string) {
  const clean = path.split(/[?#]/)[0] || '/'
  return clean.length > 1 ? clean.replace(/\/$/, '') : clean
}

export function preloadRouteByPath(path: string) {
  const normalizedPath = normalizePreloadPath(path)
  const loader = routePreloaders[normalizedPath]
  if (!loader || preloadedRoutes.has(normalizedPath)) return Promise.resolve()

  preloadedRoutes.add(normalizedPath)
  return loader().catch((error) => {
    // 预加载失败不能影响用户正常切页；下次点击时允许重新走 Vue Router 的懒加载。
    preloadedRoutes.delete(normalizedPath)
    console.warn('页面预加载失败', normalizedPath, error)
  })
}

export function preloadPanelRoutes(paths: string[]) {
  if (typeof window === 'undefined') return

  const queue = [...new Set(paths.map(normalizePreloadPath))]
    .filter((path) => routePreloaders[path] && !preloadedRoutes.has(path))

  const scheduleNext = () => {
    if (queue.length === 0) return

    const idleWindow = window as Window & {
      requestIdleCallback?: (
        callback: (deadline: { timeRemaining: () => number; didTimeout?: boolean }) => void,
        options?: { timeout: number },
      ) => number
    }

    const run = (deadline?: { timeRemaining: () => number; didTimeout?: boolean }) => {
      // 每个空闲片段只预加载少量页面，避免后台下载/解析 chunk 反过来抢占切页主线程。
      let count = 0
      // requestIdleCallback 超时触发时 timeRemaining 可能为 0；此时至少推进一小批，避免队列一直空转。
      const shouldForceRun = !deadline || deadline.didTimeout
      while (queue.length > 0 && count < 2 && (shouldForceRun || deadline.timeRemaining() > 8)) {
        void preloadRouteByPath(queue.shift()!)
        count += 1
      }
      if (queue.length > 0) scheduleNext()
    }

    if (idleWindow.requestIdleCallback) {
      idleWindow.requestIdleCallback(run, { timeout: 1800 })
      return
    }

    window.setTimeout(() => run(), 500)
  }

  scheduleNext()
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: routeComponents.login,
      meta: { requiresAuth: false }
    },
    {
      path: '/',
      component: routeComponents.layout,
      meta: { requiresAuth: true, section: 'workspace' },
      children: [
        {
          path: '',
          redirect: '/dashboard'
        },
        {
          path: 'dashboard',
          name: 'Dashboard',
          component: routeComponents.dashboard,
          meta: { title: '仪表板', icon: 'Odometer', minRole: 'viewer' }
        },
        {
          path: 'tasks',
          name: 'Tasks',
          component: routeComponents.tasks,
          meta: { title: '定时任务', icon: 'Timer', minRole: 'viewer' }
        },
        {
          path: 'scripts',
          name: 'Scripts',
          component: routeComponents.scripts,
          meta: { title: '脚本管理', icon: 'Document', minRole: 'operator' }
        },
        {
          path: 'envs',
          name: 'Envs',
          component: routeComponents.envs,
          meta: { title: '环境变量', icon: 'Setting', minRole: 'operator' }
        },
        {
          path: 'config-file',
          name: 'ConfigFile',
          component: routeComponents.configFile,
          meta: { title: '配置文件', icon: 'Document', minRole: 'admin' }
        },
        {
          path: 'subscriptions',
          name: 'Subscriptions',
          component: routeComponents.subscriptions,
          meta: { title: '订阅管理', icon: 'Download', minRole: 'operator' }
        },
        {
          path: 'logs',
          name: 'Logs',
          component: routeComponents.logs,
          meta: { title: '执行日志', icon: 'Tickets', minRole: 'viewer' }
        },
        {
          path: 'deps',
          name: 'Deps',
          component: routeComponents.deps,
          meta: { title: '依赖管理', icon: 'Box', minRole: 'admin' }
        },
        {
          path: 'notifications',
          name: 'Notifications',
          component: routeComponents.notifications,
          meta: { title: '通知渠道', icon: 'Bell', minRole: 'admin' }
        },
        {
          path: 'users',
          name: 'Users',
          component: routeComponents.users,
          meta: { title: '用户管理', icon: 'UserFilled', minRole: 'admin' }
        },
        {
          path: 'profile',
          name: 'Profile',
          component: routeComponents.profile,
          meta: { title: '个人设置', icon: 'User', minRole: 'viewer' }
        },
        {
          path: 'docs/api',
          name: 'ApiDocs',
          component: routeComponents.apiDocs,
          meta: { title: '接口文档', icon: 'Connection', minRole: 'viewer' }
        }
      ]
    },
    {
      path: '/admin',
      component: routeComponents.layout,
      meta: { requiresAuth: true, section: 'admin' },
      children: [
        {
          path: '',
          redirect: '/admin/settings'
        },
        {
          path: 'settings',
          name: 'AdminSettings',
          component: routeComponents.settings,
          meta: { title: '系统设置', icon: 'SetUp', minRole: 'admin' }
        },
        {
          path: 'notifications',
          name: 'AdminNotifications',
          component: routeComponents.notifications,
          meta: { title: '通知渠道', icon: 'Bell', minRole: 'admin' }
        },
        {
          path: 'users',
          name: 'AdminUsers',
          component: routeComponents.users,
          meta: { title: '用户管理', icon: 'UserFilled', minRole: 'admin' }
        },
        {
          path: 'open-api',
          name: 'AdminOpenAPI',
          component: routeComponents.openApi,
          meta: { title: 'Open API', icon: 'Key', minRole: 'admin' }
        }
      ]
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/'
    }
  ]
})

router.beforeEach(async (to, _from, next) => {
  const authStore = useAuthStore()

  if (to.meta.requiresAuth === false) {
    if (authStore.isLoggedIn && to.name === 'Login') {
      next('/')
      return
    }
    next()
    return
  }

  if (!authStore.isLoggedIn) {
    next('/login')
    return
  }

  if (!authStore.user) {
    try {
      await authStore.fetchUser()
    } catch {
      authStore.clearAuth()
      next('/login')
      return
    }
  }

  if (to.path === '/settings') {
    next(hasRequiredRole(authStore.user?.role, 'admin') ? '/admin/settings' : '/profile')
    return
  }

  const legacyRedirect = legacyRouteMap[to.path]
  if (legacyRedirect) {
    if (!hasRequiredRole(authStore.user?.role, 'admin')) {
      next('/dashboard')
      return
    }
    next(legacyRedirect)
    return
  }

  const minRole = to.meta.minRole as string | undefined
  if (!hasRequiredRole(authStore.user?.role, minRole)) {
    next('/dashboard')
    return
  }

  next()
})

router.afterEach((to) => {
  const title = to.meta.title as string | undefined
  const panelTitle = getCachedPanelTitle()
  document.title = title ? `${panelTitle} - ${title}` : panelTitle
})

void loadPanelSettings().then(() => {
  const currentRoute = router.currentRoute.value
  const title = currentRoute.meta.title as string | undefined
  const panelTitle = getCachedPanelTitle()
  document.title = title ? `${panelTitle} - ${title}` : panelTitle
})

export default router
