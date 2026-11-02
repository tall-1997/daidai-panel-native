import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import {
  applyPanelAppearance,
  applyThemeMode,
  readThemeMode,
  systemPrefersDark,
  type ThemeMode,
} from '@/utils/panelAppearance'

export const useThemeStore = defineStore('theme', () => {
  // v3.3.2（issue #145）起主题是三档：明亮 / 暗夜 / 跟随系统。
  // 持久化仍落在 localStorage 的裸 `theme` 键上，读写全部收敛到 utils/panelAppearance.ts，
  // 这个 store 只负责把「档位 + 系统当前明暗」合成对外的 isDark。
  const mode = ref<ThemeMode>(readThemeMode())
  const systemDark = ref(systemPrefersDark())

  // change 监听挂在 store 工厂体内而【不是】 onMounted：Pinia 的 setup 工厂不在组件上下文里，
  // onMounted 根本不会执行；而且这个 store 可能在组件之外（路由守卫、工具函数）先被用到。
  // 监听跟着 store 活一辈子，不需要摘 —— store 本身就是整页生命周期的单例。
  if (typeof window !== 'undefined') {
    window.matchMedia?.('(prefers-color-scheme: dark)')?.addEventListener('change', (event) => {
      systemDark.value = event.matches
    })
  }

  // 对外的只读出口，语义与名字都不变：CodeMirror / Monaco / 图表 / applyPanelAppearance
  // 全都依赖它，三档改造对它们必须完全无感。
  const isDark = computed(() =>
    mode.value === 'system' ? systemDark.value : mode.value === 'dark'
  )

  function setThemeMode(next: ThemeMode) {
    // applyThemeMode 会写 localStorage 并立即改 <html> 上的 class，全站即时生效、无需刷新
    mode.value = applyThemeMode(next)
  }

  // 顶栏与登录页那颗图标按钮的语义：点一下 = 脱离「跟随系统」，落到与当前观感相反的显式档位。
  // 第三档「跟随系统」只在个人设置页能选回来，避免顶栏一颗按钮转三态、误触后不知道自己在哪一档。
  function toggleTheme() {
    setThemeMode(isDark.value ? 'light' : 'dark')
  }

  watch(isDark, (val) => {
    document.documentElement.classList.toggle('dark', val)
    // ⚠️ 这里【不】写 localStorage：三档的持久化归 applyThemeMode 管。
    //    v3.3.2 之前这行是 setItem('theme', val ? 'dark' : 'light')，留着的话「跟随系统」
    //    会在首次 watch（immediate）时就被自解成 dark/light 写回存储，用户下次打开便不再跟随。
    // 保留这条 watch 的唯一理由是 applyPanelAppearance()：它是重推编辑器/日志底色默认值、
    // 并派发 PANEL_APPEARANCE_CHANGE_EVENT 让已挂载的编辑器换主题的唯一触发点。
    applyPanelAppearance()
  }, { immediate: true })

  return { isDark, mode, setThemeMode, toggleTheme }
})
