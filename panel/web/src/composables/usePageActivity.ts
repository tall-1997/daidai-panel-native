import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref } from 'vue'

function getDocumentVisible() {
  return typeof document === 'undefined' || document.visibilityState !== 'hidden'
}

export function usePageActivity() {
  const isDocumentVisible = ref(getDocumentVisible())
  const isViewActive = ref(true)

  const handleVisibilityChange = () => {
    isDocumentVisible.value = getDocumentVisible()
  }

  onMounted(() => {
    isViewActive.value = true
    isDocumentVisible.value = getDocumentVisible()
    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', handleVisibilityChange)
    }
  })

  onActivated(() => {
    isViewActive.value = true
  })

  onDeactivated(() => {
    isViewActive.value = false
  })

  onBeforeUnmount(() => {
    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
  })

  return {
    isDocumentVisible,
    isViewActive,
    isPageActive: computed(() => isViewActive.value && isDocumentVisible.value),
  }
}
