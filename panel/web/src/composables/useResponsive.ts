import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const MOBILE_BREAKPOINT = 768
const TABLET_BREAKPOINT = 1024

export function useResponsive() {
  const width = ref(typeof window !== 'undefined' ? window.innerWidth : TABLET_BREAKPOINT)
  const height = ref(typeof window !== 'undefined' ? window.innerHeight : 0)

  function updateViewport() {
    if (typeof window === 'undefined') return
    // 最小化/切后台守卫：Edge/Chromium 在窗口最小化时会派发 innerWidth===0 的 resize 事件。
    // 若把 0 写进 width，会让 isMobile/dialogFullscreen 误翻为 true，导致弹窗全屏、侧栏收起、全站切移动端。
    // 这类零尺寸不是真实布局态，直接跳过，等窗口恢复后的真实 resize 再更新；真实移动端/缩放（innerWidth>0）行为不变。
    if (window.innerWidth === 0 || document.hidden) return
    width.value = window.innerWidth
    height.value = window.innerHeight
  }

  onMounted(() => {
    updateViewport()
    window.addEventListener('resize', updateViewport, { passive: true })
  })

  onBeforeUnmount(() => {
    if (typeof window === 'undefined') return
    window.removeEventListener('resize', updateViewport)
  })

  const isMobile = computed(() => width.value <= MOBILE_BREAKPOINT)
  const isTablet = computed(() => width.value <= TABLET_BREAKPOINT)
  const dialogFullscreen = computed(() => isMobile.value)

  return {
    width,
    height,
    isMobile,
    isTablet,
    dialogFullscreen,
    updateViewport,
  }
}
