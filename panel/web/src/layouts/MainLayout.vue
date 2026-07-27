<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { systemApi } from '@/api/system'
import { loadPanelSettings as loadCachedPanelSettings } from '@/utils/panelSettings'
import { useResponsive } from '@/composables/useResponsive'
import { preloadPanelRoutes, preloadRouteByPath } from '@/router'
import {
  Bell,
  Box,
  Connection,
  Document,
  Download,
  Expand,
  Fold,
  Key,
  Moon,
  Odometer,
  Operation,
  Setting,
  SetUp,
  Sunny,
  Tickets,
  Timer,
  User,
  UserFilled,
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const themeStore = useThemeStore()
const { isMobile } = useResponsive()
const isCollapsed = ref(false)
const drawerVisible = ref(false)
const panelTitle = ref('呆呆面板')
const panelIcon = ref('')
const panelVersion = ref('')
const routeCacheMax = 14

const roleLevel: Record<string, number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
}

function hasRole(minRole: string) {
  const currentRole = authStore.user?.role
  if (!currentRole) return false
  return (roleLevel[currentRole] || 0) >= (roleLevel[minRole] || 0)
}

const canAccessAdmin = computed(() => hasRole('admin'))

const workspaceItems = [
  { index: '/dashboard', title: '仪表板', icon: Odometer, minRole: 'viewer' },
  { index: '/tasks', title: '定时任务', icon: Timer, minRole: 'viewer' },
  { index: '/subscriptions', title: '订阅管理', icon: Download, minRole: 'operator' },
  { index: '/envs', title: '环境变量', icon: Setting, minRole: 'operator' },
  { index: '/config-file', title: '配置文件', icon: Document, minRole: 'admin' },
  { index: '/logs', title: '执行日志', icon: Tickets, minRole: 'viewer' },
  { index: '/scripts', title: '脚本管理', icon: Document, minRole: 'operator' },
  { index: '/deps', title: '依赖管理', icon: Box, minRole: 'admin' },
  { index: '/docs/api', title: '接口文档', icon: Connection, minRole: 'viewer' },
  { index: '/profile', title: '个人设置', icon: User, minRole: 'viewer' },
]

const adminItems = [
  { index: '/admin/settings', title: '系统设置', icon: SetUp, minRole: 'admin' },
  { index: '/admin/notifications', title: '通知渠道', icon: Bell, minRole: 'admin' },
  { index: '/admin/users', title: '用户管理', icon: UserFilled, minRole: 'admin' },
  { index: '/admin/open-api', title: 'Open API', icon: Key, minRole: 'admin' },
]

const filteredWorkspaceItems = computed(() =>
  workspaceItems.filter(item => hasRole(item.minRole))
)

const filteredAdminItems = computed(() =>
  canAccessAdmin.value ? adminItems : []
)

const activeMenu = computed(() => route.path)

const preloadableMenuPaths = computed(() =>
  [...filteredWorkspaceItems.value, ...filteredAdminItems.value]
    .map(item => item.index)
    .filter(index => index !== route.path)
)

const breadcrumb = computed(() => {
  const matched = [...route.matched].reverse().find(item => item.meta.section)
  const section = matched?.meta.section === 'admin' ? '管理后台' : '工作台'
  const title = route.meta.title as string || ''
  return { section, title }
})

const themeIcon = computed(() => (themeStore.isDark ? Sunny : Moon))

onMounted(() => {
  loadPanelSettings()
  loadVersion()
  if (authStore.isLoggedIn && !authStore.user) {
    authStore.fetchUser()
  }
  scheduleMenuPreload()
})

watch(isMobile, (mobile) => {
  if (mobile) {
    isCollapsed.value = true
    return
  }
  drawerVisible.value = false
}, { immediate: true })

watch(() => authStore.user?.role, () => {
  scheduleMenuPreload()
})

function handleMenuSelect(index: string) {
  preloadPage(index)
  router.push(index)
  if (isMobile.value) drawerVisible.value = false
}

function preloadPage(index: string) {
  void preloadRouteByPath(index)
}

function scheduleMenuPreload() {
  // 首屏先让当前页稳定渲染；空闲时再分批预加载菜单页，减少首次切页等待 chunk 下载导致的白屏。
  preloadPanelRoutes(preloadableMenuPaths.value)
}

function toggleSidebar() {
  if (isMobile.value) {
    drawerVisible.value = !drawerVisible.value
  } else {
    isCollapsed.value = !isCollapsed.value
  }
}

async function handleLogout() {
  await authStore.logout()
}

async function loadPanelSettings() {
  try {
    const settings = await loadCachedPanelSettings()
    if (settings?.panel_title) panelTitle.value = settings.panel_title
    if (settings?.panel_icon) panelIcon.value = settings.panel_icon
    const routeTitle = route.meta.title as string | undefined
    document.title = routeTitle ? `${panelTitle.value} - ${routeTitle}` : panelTitle.value
  } catch {}
}

async function loadVersion() {
  try {
    const res = await systemApi.version() as any
    if (res.data?.version) panelVersion.value = res.data.version
  } catch {}
}

</script>

<template>
  <el-container class="layout-container">
    <!-- Desktop Sidebar -->
    <aside v-if="!isMobile" class="layout-aside" :class="{ 'is-collapsed': isCollapsed }">
      <!-- Logo -->
      <div class="sidebar-logo" :class="{ 'is-collapsed': isCollapsed }">
        <div class="logo-inner">
          <div class="logo-icon-wrap">
            <img :src="panelIcon || '/favicon.svg'" alt="logo" class="logo-icon" />
          </div>
          <template v-if="!isCollapsed">
            <span class="logo-title">{{ panelTitle }}</span>
            <span v-if="panelVersion" class="logo-version">v{{ panelVersion }}</span>
          </template>
        </div>
      </div>

      <!-- Scrollable navigation area -->
      <div class="sidebar-nav">
        <el-menu
          :default-active="activeMenu"
          :collapse="isCollapsed"
          :collapse-transition="false"
          background-color="transparent"
          @select="handleMenuSelect"
        >
          <el-menu-item
            v-for="item in filteredWorkspaceItems"
            :key="item.index"
            :index="item.index"
            @mouseenter="preloadPage(item.index)"
            @focusin="preloadPage(item.index)"
          >
            <el-icon><component :is="item.icon" /></el-icon>
            <template #title>{{ item.title }}</template>
          </el-menu-item>
        </el-menu>

        <!-- Admin section -->
        <template v-if="filteredAdminItems.length > 0">
          <div v-if="!isCollapsed" class="nav-section-label">管理后台</div>
          <div v-else class="nav-section-divider"></div>
          <el-menu
            :default-active="activeMenu"
            :collapse="isCollapsed"
            :collapse-transition="false"
            background-color="transparent"
            @select="handleMenuSelect"
          >
            <el-menu-item
              v-for="item in filteredAdminItems"
              :key="item.index"
              :index="item.index"
              @mouseenter="preloadPage(item.index)"
              @focusin="preloadPage(item.index)"
            >
              <el-icon><component :is="item.icon" /></el-icon>
              <template #title>{{ item.title }}</template>
            </el-menu-item>
          </el-menu>
        </template>
      </div>

      <!-- User card -->
      <div class="sidebar-user" :class="{ 'is-collapsed': isCollapsed }">
        <div class="user-card-inner" @click="router.push('/profile')">
          <img
            v-if="authStore.user?.avatar_url"
            :src="authStore.user.avatar_url"
            alt=""
            class="user-avatar"
          />
          <div v-else class="user-avatar user-avatar--placeholder">
            {{ (authStore.user?.username || 'U').charAt(0).toUpperCase() }}
          </div>
          <template v-if="!isCollapsed">
            <div class="user-info">
              <span class="user-name">{{ authStore.user?.username || 'User' }}</span>
              <span class="user-role">{{ authStore.user?.role === 'admin' ? '管理员' : '用户' }}</span>
            </div>
          </template>
        </div>
      </div>

      <!-- Collapse toggle -->
      <button class="sidebar-collapse-btn" @click="toggleSidebar">
        <el-icon><component :is="isCollapsed ? Expand : Fold" /></el-icon>
        <span v-if="!isCollapsed" class="collapse-text">收起菜单</span>
      </button>
    </aside>

    <!-- Mobile Drawer -->
    <el-drawer
      v-if="isMobile"
      v-model="drawerVisible"
      direction="ltr"
      :size="260"
      :with-header="false"
      :show-close="false"
    >
      <div class="sidebar-logo mobile-logo">
        <div class="logo-inner">
          <div class="logo-icon-wrap">
            <img :src="panelIcon || '/favicon.svg'" alt="logo" class="logo-icon" />
          </div>
          <span class="logo-title">{{ panelTitle }}</span>
          <span v-if="panelVersion" class="logo-version">v{{ panelVersion }}</span>
        </div>
      </div>

      <div class="sidebar-nav mobile-nav">
        <el-menu
          :default-active="activeMenu"
          background-color="transparent"
          @select="handleMenuSelect"
        >
          <el-menu-item
            v-for="item in filteredWorkspaceItems"
            :key="item.index"
            :index="item.index"
            @mouseenter="preloadPage(item.index)"
            @focusin="preloadPage(item.index)"
          >
            <el-icon><component :is="item.icon" /></el-icon>
            <template #title>{{ item.title }}</template>
          </el-menu-item>
        </el-menu>

        <template v-if="filteredAdminItems.length > 0">
          <div class="nav-section-label">管理后台</div>
          <el-menu
            :default-active="activeMenu"
            background-color="transparent"
            @select="handleMenuSelect"
          >
            <el-menu-item
              v-for="item in filteredAdminItems"
              :key="item.index"
              :index="item.index"
              @mouseenter="preloadPage(item.index)"
              @focusin="preloadPage(item.index)"
            >
              <el-icon><component :is="item.icon" /></el-icon>
              <template #title>{{ item.title }}</template>
            </el-menu-item>
          </el-menu>
        </template>
      </div>

      <div class="sidebar-user">
        <div class="user-card-inner" @click="router.push('/profile'); drawerVisible = false">
          <img
            v-if="authStore.user?.avatar_url"
            :src="authStore.user.avatar_url"
            alt=""
            class="user-avatar"
          />
          <div v-else class="user-avatar user-avatar--placeholder">
            {{ (authStore.user?.username || 'U').charAt(0).toUpperCase() }}
          </div>
          <div class="user-info">
            <span class="user-name">{{ authStore.user?.username || 'User' }}</span>
            <span class="user-role">{{ authStore.user?.role === 'admin' ? '管理员' : '用户' }}</span>
          </div>
        </div>
      </div>
    </el-drawer>

    <!-- Main content area -->
    <el-container class="layout-main-wrap">
      <!-- Header -->
      <header class="layout-header">
        <div class="header-left">
          <button class="header-toggle-btn" @click="toggleSidebar">
            <el-icon :size="18"><Operation /></el-icon>
          </button>
          <nav class="header-breadcrumb">
            <span class="breadcrumb-sep">›</span>
            <span class="breadcrumb-section">{{ breadcrumb.section }}</span>
            <template v-if="breadcrumb.title">
              <span class="breadcrumb-sep">›</span>
              <span class="breadcrumb-current">{{ breadcrumb.title }}</span>
            </template>
          </nav>
        </div>

        <div class="header-center"></div>

        <div class="header-right">
          <button class="header-icon-btn theme-toggle" @click="themeStore.toggleTheme">
            <el-icon :size="18"><component :is="themeIcon" /></el-icon>
          </button>
          <el-dropdown trigger="click">
            <div class="header-user">
              <img
                v-if="authStore.user?.avatar_url"
                :src="authStore.user.avatar_url"
                alt=""
                class="header-user-avatar"
              />
              <div v-else class="header-user-avatar header-user-avatar--placeholder">
                {{ (authStore.user?.username || 'U').charAt(0).toUpperCase() }}
              </div>
              <span v-if="!isMobile" class="header-user-name">{{ authStore.user?.username || 'User' }}</span>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="router.push('/profile')">个人设置</el-dropdown-item>
                <el-dropdown-item v-if="canAccessAdmin" @click="router.push('/admin/settings')">系统设置</el-dropdown-item>
                <el-dropdown-item divided @click="handleLogout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <!-- Page content -->
      <main class="layout-main">
        <div class="route-shell">
          <router-view v-slot="{ Component, route: viewRoute }">
            <transition name="page-shell">
              <keep-alive :max="routeCacheMax">
                <component :is="Component" :key="viewRoute.name || viewRoute.path" />
              </keep-alive>
            </transition>
          </router-view>
        </div>
      </main>

      <!-- Footer -->
      <footer class="layout-footer">
        <span>© {{ new Date().getFullYear() }} {{ panelTitle }}</span>
        <span class="footer-sep">·</span>
        <span>开源自动化任务调度面板</span>
        <span class="footer-sep">·</span>
        <a href="javascript:void(0)" class="footer-link">文档中心</a>
        <span class="footer-sep">·</span>
        <a href="javascript:void(0)" class="footer-link">使用条款</a>
        <span class="footer-sep">·</span>
        <a href="javascript:void(0)" class="footer-link">隐私政策</a>
      </footer>
    </el-container>
  </el-container>
</template>

<style scoped lang="scss">
// ==================== 悬浮圆角外壳 ====================
// 桌面：在最外层包一层留白 + 圆角 + 阴影 + 描边的「悬浮外壳」，包住侧栏 + 主区。
// 外壳自身用表面色，留白处露出 #app 的页面底色；overflow: hidden 用于圆角裁切。
// 注意：内部滚动链（layout-main / route-shell / dd-*-page）保持不动，外壳只在最外层加壳。
.layout-container {
  margin: var(--dd-shell-margin);
  // 高度扣掉上下两侧留白，保证外壳整体在视口内、不溢出
  height: calc(100dvh - var(--dd-shell-margin) * 2);
  border-radius: var(--dd-radius-shell);
  box-shadow: var(--dd-shadow-shell);
  border: 1px solid var(--el-border-color-light);
  background: var(--el-bg-color);
  overflow: hidden;
}

// ==================== Sidebar ====================
.layout-aside {
  width: 220px;
  box-shadow: 8px 0 24px rgba(15, 23, 42, 0.03);
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color);
  border-right: 1px solid var(--el-border-color-light);
  transition: width 0.25s cubic-bezier(0.25, 0.46, 0.45, 0.94);
  overflow: hidden;
  will-change: width;
  z-index: 10;

  &.is-collapsed {
    width: 64px;
  }
}

// Logo
.sidebar-logo {
  height: 60px;
  display: flex;
  align-items: center;
  padding: 8px 10px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--el-border-color-light);
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--el-color-primary-light-9) 48%, white) 0%,
    var(--el-bg-color) 100%
  );

  &.is-collapsed {
    justify-content: center;
    padding: 8px;
  }
}

.logo-inner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-radius: 14px;
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--el-bg-color) 88%, white) 0%,
    var(--el-bg-color) 100%
  );
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 10%, var(--el-border-color-lighter));
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.04);
  transition: box-shadow 0.3s, border-color 0.3s;
  width: 100%;
  min-height: 42px;

  &:hover {
    box-shadow: 0 6px 16px rgba(15, 23, 42, 0.06);
    border-color: color-mix(in srgb, var(--el-color-primary) 18%, var(--el-border-color-lighter));
  }
}

.is-collapsed .logo-inner {
  width: 40px;
  min-height: 40px;
  padding: 6px;
  justify-content: center;
}

.logo-icon-wrap {
  width: 28px;
  height: 28px;
  border-radius: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  background: linear-gradient(145deg, var(--el-color-primary-light-8), var(--el-color-primary-light-9));
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 18%, transparent);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.55), 0 4px 10px rgba(64, 158, 255, 0.12);
  transition: transform 0.3s, box-shadow 0.3s;

  .logo-inner:hover & {
    transform: translateY(-1px);
  }
}

.logo-icon {
  width: 16px;
  height: 16px;
}

.logo-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
  min-width: 0;
}

.logo-version {
  flex-shrink: 0;
  padding: 3px 8px;
  border-radius: 999px;
  font-family: var(--dd-font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--el-color-primary-dark-2);
  background: color-mix(in srgb, var(--el-color-primary) 10%, white);
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 18%, transparent);
}

// Navigation
.sidebar-nav {
  flex: 1;
  position: relative;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 6px 0;

  &::-webkit-scrollbar {
    width: 3px;
  }

  &::-webkit-scrollbar-thumb {
    background: transparent;
    border-radius: 3px;
  }

  &:hover::-webkit-scrollbar-thumb {
    background: var(--el-border-color);
  }
}

// 收起态：Element Plus 把菜单项内容包进 tooltip 触发层，其默认 padding: 0 20px 会把
// 图标整体右推，导致与已居中的 logo 不在同一竖直轴上。清掉左右内距并居中，让收起态
// 导航图标与 logo / 用户头像 / 收起按钮统一居中对齐。
.sidebar-nav :deep(.el-menu--collapse .el-menu-item .el-menu-tooltip__trigger) {
  padding-left: 0;
  padding-right: 0;
  justify-content: center;
}

.nav-section-label {
  padding: 16px 20px 6px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--el-text-color-placeholder);
  user-select: none;
}

.nav-section-divider {
  height: 1px;
  margin: 8px 16px;
  background: var(--el-border-color-lighter);
}

// Quick Environments
.sidebar-envs {
  flex-shrink: 0;
  padding: 0 12px;
  border-top: 1px solid var(--el-border-color-lighter);
  margin-top: 4px;
}

.envs-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 4px 4px;
}

.envs-title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: var(--el-text-color-placeholder);
  text-transform: uppercase;
}

.envs-add-btn {
  width: 20px;
  height: 20px;
  border-radius: 4px;
  border: 1px solid var(--el-border-color-lighter);
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--el-text-color-placeholder);
  transition:
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    color var(--dd-motion-fast) var(--dd-ease-standard),
    border-color var(--dd-motion-fast) var(--dd-ease-standard),
    transform var(--dd-motion-fast) var(--dd-ease-emphasized),
    box-shadow var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }
}

.envs-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-bottom: 8px;
}

.env-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 6px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.2s;

  &:hover {
    background: var(--el-fill-color-light);
    transform: translateY(-1px);
    box-shadow: 0 6px 18px rgba(15, 23, 42, 0.08);
  }
}

.env-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}

.env-info {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.env-name {
  font-size: 12px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  line-height: 1.3;
}

.env-key {
  font-size: 10px;
  color: var(--el-text-color-placeholder);
  font-family: var(--dd-font-mono);
}

// User card
.sidebar-user {
  flex-shrink: 0;
  padding: 8px 12px;
  border-top: 1px solid var(--el-border-color-lighter);

  &.is-collapsed {
    padding: 8px;
    display: flex;
    justify-content: center;
  }
}

.user-card-inner {
  display: flex;
  position: relative;
  overflow: hidden;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 10px;
  cursor: pointer;
  transition: background 0.2s;

  &:hover {
    background: var(--el-fill-color-light);
    transform: translateY(-1px);
    box-shadow: 0 8px 18px rgba(15, 23, 42, 0.08);
  }

  &::after {
    content: "";
    position: absolute;
    inset: -50% 0 auto;
    height: 75%;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.28), transparent);
    opacity: 0;
    transform: translateY(-8px);
    transition: opacity 180ms ease, transform 180ms ease;
    pointer-events: none;
  }

  &:hover::after {
    opacity: 1;
    transform: translateY(0);
  }

  .is-collapsed & {
    padding: 6px;
    justify-content: center;
  }
}

.user-avatar {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  object-fit: cover;
  flex-shrink: 0;
}

.user-avatar--placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--el-color-primary-light-5), var(--el-color-primary));
  color: #fff;
  font-size: 14px;
  font-weight: 700;
}

.user-info {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.user-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-role {
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  line-height: 1.3;
}

// Collapse button
.sidebar-collapse-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 40px;
  margin: 4px 12px 10px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  background: transparent;
  cursor: pointer;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  transition:
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    color var(--dd-motion-fast) var(--dd-ease-standard),
    transform var(--dd-motion-fast) var(--dd-ease-emphasized),
    box-shadow var(--dd-motion-fast) var(--dd-ease-standard);

  &:hover {
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
    border-color: var(--el-color-primary-light-7);
    transform: translateY(-1px);
  }
}

.collapse-text {
  font-size: 12px;
  white-space: nowrap;
}

// ==================== Header ====================
.layout-header {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 20px;
  background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-light);
  position: sticky;
  top: 0;
  z-index: 20;
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.header-toggle-btn {
  width: 34px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.5);
  height: 34px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--el-text-color-regular);
  transition: all 0.2s;
  flex-shrink: 0;

  &:hover {
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
    border-color: var(--el-color-primary-light-7);
    transform: translateY(-1px);
    box-shadow: 0 8px 18px rgba(15, 23, 42, 0.08);
  }
}

.header-breadcrumb {
  display: flex;
  padding: 6px 10px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--el-fill-color-light) 76%, transparent);
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  min-width: 0;
}

.breadcrumb-sep {
  color: var(--el-text-color-placeholder);
  font-weight: 300;
  font-size: 15px;
}

.breadcrumb-section {
  white-space: nowrap;
  color: var(--el-text-color-secondary);
}

.breadcrumb-current {
  white-space: nowrap;
  letter-spacing: 0.1px;
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.header-center {
  flex: 1;
  max-width: 480px;
  display: flex;
  justify-content: center;
}

.header-search {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 36px;
  padding: 0 14px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
  border: 1px solid transparent;
  cursor: pointer;
  transition: all 0.2s;

  &:hover {
    background: var(--el-fill-color);
    border-color: var(--el-border-color-lighter);
  }
}

.search-icon {
  color: var(--el-text-color-placeholder);
  flex-shrink: 0;
}

.search-placeholder {
  flex: 1;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.search-kbd {
  flex-shrink: 0;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 11px;
  font-family: var(--dd-font-mono);
  color: var(--el-text-color-placeholder);
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  line-height: 1.2;
}

.header-right {
  display: flex;
  padding: 4px 6px;
  border-radius: 14px;
  background: color-mix(in srgb, var(--el-fill-color-light) 82%, transparent);
  align-items: center;
  gap: 6px;
}

.header-icon-btn {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  border: none;
  background: transparent;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--el-text-color-regular);
  transition:
    background-color var(--dd-motion-fast) var(--dd-ease-standard),
    color var(--dd-motion-fast) var(--dd-ease-standard),
    transform var(--dd-motion-fast) var(--dd-ease-emphasized),
    box-shadow var(--dd-motion-fast) var(--dd-ease-standard);
  position: relative;

  &:hover {
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
  }
}

.theme-toggle {
  &:hover {
    transform: translateY(-1px) scale(1.03);
  }
}

.notification-dot {
  position: absolute;
  top: 7px;
  right: 7px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #f56c6c;
  border: 1.5px solid var(--el-bg-color);
}

.header-user {
  display: flex;
  position: relative;
  overflow: hidden;
  align-items: center;
  gap: 8px;
  padding: 5px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
  outline: none;

  &:hover {
    background: var(--el-fill-color-light);
    transform: translateY(-1px);
    box-shadow: 0 10px 20px rgba(15, 23, 42, 0.08);
  }

  &::after {
    content: "";
    position: absolute;
    inset: -50% 0 auto;
    height: 70%;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.28), transparent);
    opacity: 0;
    transform: translateY(-10px);
    transition: opacity 180ms ease, transform 180ms ease;
    pointer-events: none;
  }

  &:hover::after {
    opacity: 1;
    transform: translateY(0);
  }
}

.header-user-avatar {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  object-fit: cover;
  flex-shrink: 0;
}

.header-user-avatar--placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, var(--el-color-primary-light-5), var(--el-color-primary));
  color: #fff;
  font-size: 12px;
  font-weight: 700;
}

.header-user-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}

// ==================== Main ====================
.layout-main-wrap {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  width: 0;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.layout-main {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 20px;
  background: var(--el-bg-color-page);
}

.route-shell {
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
  contain: layout style;

  > :deep(*) {
    min-width: 0;
  }

  > :deep(.dd-fixed-page) {
    flex: 1 1 auto;
    width: 100%;
    min-height: 0;
  }

  > :deep(.dd-scroll-page) {
    flex: 1 1 auto;
    width: 100%;
    min-height: 0;
  }
}

// ==================== Footer ====================
.layout-footer {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 12px 20px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  background: var(--el-bg-color-page);
  border-top: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
}

@media screen and (max-height: 820px) and (min-width: 769px) {
  // 矮屏：外壳四周留白收小，给内容腾出更多垂直空间
  .layout-container {
    --dd-shell-margin: 10px;
  }

  .layout-header {
    height: 52px;
    padding: 0 16px;
  }

  .layout-main {
    padding: 14px 16px;
  }

  .layout-footer {
    padding: 8px 16px;
    font-size: 11px;
  }
}

.footer-sep {
  color: var(--el-border-color);
}

.footer-link {
  color: var(--el-text-color-secondary);
  transition: color 0.2s;

  &:hover {
    color: var(--el-color-primary);
  }
}

// ==================== Page transition ====================
// 切页只保留透明度 + 极轻缩放，避免 blur、复杂位移和长动画抢主线程。
// 这里不再使用 out-in：旧页面绝对定位短暂淡出，新页面立即进入，避免先卸载旧页后等待新页导致白屏。
.page-shell-enter-active {
  position: relative;
  z-index: 1;
  animation: dd-page-shell-enter 180ms var(--dd-ease-decelerate) both;
  will-change: opacity, transform;
}

.page-shell-leave-active {
  position: absolute;
  inset: 0;
  z-index: 0;
  width: 100%;
  pointer-events: none;
  animation: dd-page-shell-leave 80ms var(--dd-ease-standard) both;
  will-change: opacity;
}

@keyframes dd-page-shell-enter {
  from {
    opacity: 0;
    transform: scale3d(0.992, 0.992, 1);
  }
  to {
    opacity: 1;
    transform: scale3d(1, 1, 1);
  }
}

@keyframes dd-page-shell-leave {
  from {
    opacity: 1;
  }
  to {
    opacity: 0;
  }
}

// ==================== Mobile drawer ====================
.mobile-logo {
  border-bottom: 1px solid var(--el-border-color-light);
}

.mobile-nav {
  flex: 1;
}

// ==================== Mobile responsive ====================
@media screen and (max-width: 768px) {
  // 移动端：取消悬浮外壳，回到全屏，避免小屏留白浪费；drawer 行为不变
  .layout-container {
    margin: 0;
    height: 100dvh;
    border-radius: 0;
    box-shadow: none;
    border: none;
  }

  .layout-header {
    height: 54px;
    padding: 0 12px;
    gap: 8px;
  }

  .layout-main {
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    padding: max(12px, env(safe-area-inset-top)) 12px calc(16px + env(safe-area-inset-bottom));
  }

  // 移动端 .route-shell 必须能被子内容撑高，否则被 flex column 容器压缩到等于 .layout-main 的可视高度，
  // 子页面 overflow visible 的内容画在容器外但不计入 box 尺寸，.layout-main 看不到滚动需求。
  // 桌面端依赖 flex: 1 + min-height: 0 + overflow: hidden 实现内嵌滚动；移动端反过来：禁止收缩 + 无 contain。
  .route-shell {
    flex: 1 0 auto;
    min-height: 0;
    overflow: visible;
    contain: none;

    > :deep(.dd-fixed-page),
    > :deep(.dd-scroll-page) {
      flex: 1 0 auto;
      min-height: 0;
      overflow: visible;
    }
  }

  .header-center {
    display: none;
  }

  .header-breadcrumb {
    font-size: 12px;
  }

  .header-right {
    gap: 2px;
  }

  .layout-footer {
    padding: 10px 12px;
    font-size: 11px;
  }
}
</style>
