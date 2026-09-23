import { onMounted, onUnmounted, ref } from 'vue'

/** 第一次点击进入待确认态；同 key 再点确认；点击外部取消。 */
export function usePendingConfirm<T extends string | number>(keepInsideSelector: string) {
  const pending = ref<T | null>(null)

  function clear(): void {
    pending.value = null
  }

  function armOrConfirm(key: T, onConfirm: () => void): void {
    if (pending.value === key) {
      pending.value = null
      onConfirm()
      return
    }
    pending.value = key
  }

  function onDocumentClick(e: MouseEvent): void {
    const target = e.target
    if (!(target instanceof Element)) return
    if (pending.value !== null && !target.closest(keepInsideSelector)) {
      pending.value = null
    }
  }

  onMounted(() => document.addEventListener('click', onDocumentClick))
  onUnmounted(() => document.removeEventListener('click', onDocumentClick))

  return { pending, clear, armOrConfirm }
}
