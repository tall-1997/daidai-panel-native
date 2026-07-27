import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { applyPanelAppearance } from '@/utils/panelAppearance'

export const useThemeStore = defineStore('theme', () => {
  const isDark = ref(localStorage.getItem('theme') === 'dark')

  function toggleTheme() {
    isDark.value = !isDark.value
  }

  watch(isDark, (val) => {
    document.documentElement.classList.toggle('dark', val)
    localStorage.setItem('theme', val ? 'dark' : 'light')
    applyPanelAppearance()
  }, { immediate: true })

  return { isDark, toggleTheme }
})
