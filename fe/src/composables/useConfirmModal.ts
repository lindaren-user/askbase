import { ref } from 'vue'

export function useConfirmModal() {
  const open = ref(false)
  const title = ref('')
  const message = ref('')
  let action: (() => void | Promise<void>) | null = null

  function ask(opts: { title: string; message: string; onConfirm: () => void | Promise<void> }): void {
    title.value = opts.title
    message.value = opts.message
    action = opts.onConfirm
    open.value = true
  }

  function confirm(): void {
    const fn = action
    action = null
    open.value = false
    void fn?.()
  }

  function cancel(): void {
    action = null
    open.value = false
  }

  return { open, title, message, ask, confirm, cancel }
}
