import { ref } from 'vue'

export type ToastKind = 'success' | 'error' | 'info'

export interface ToastItem {
  id: number
  kind: ToastKind
  message: string
}

const MAX_VISIBLE = 5
const DURATION_MS: Record<ToastKind, number> = {
  success: 3200,
  error: 5600,
  info: 4000,
}

const items = ref<ToastItem[]>([])
let seq = 0
const timers = new Map<number, ReturnType<typeof setTimeout>>()

function dismiss(id: number): void {
  const timer = timers.get(id)
  if (timer) {
    clearTimeout(timer)
    timers.delete(id)
  }
  items.value = items.value.filter((t) => t.id !== id)
}

function push(kind: ToastKind, message: string): void {
  const text = message.trim()
  if (!text) return
  const id = ++seq
  items.value = [...items.value.slice(-(MAX_VISIBLE - 1)), { id, kind, message: text }]
  timers.set(id, setTimeout(() => dismiss(id), DURATION_MS[kind]))
}

function asMsg(e: unknown, fallback: string): string {
  if (e instanceof Error) return e.message
  return fallback
}

function success(message: string): void {
  push('success', message)
}

function error(message: string): void {
  push('error', message)
}

function info(message: string): void {
  push('info', message)
}

function fromError(e: unknown, fallback: string): void {
  error(asMsg(e, fallback))
}

export function useToast() {
  return { items, success, error, info, dismiss, fromError }
}
