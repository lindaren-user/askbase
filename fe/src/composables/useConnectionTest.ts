import { onUnmounted, ref } from 'vue'
import { useToast } from '@/composables/useToast'

export type TestStatus = 'idle' | 'testing' | 'success' | 'error'

export interface TestResult {
  ok: boolean
  message: string
}

export function useConnectionTest() {
  const toast = useToast()
  const status = ref<TestStatus>('idle')
  const passed = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null

  function clearTimer(): void {
    if (!timer) return
    clearTimeout(timer)
    timer = null
  }

  function invalidate(): void {
    passed.value = false
    if (status.value === 'success') status.value = 'idle'
  }

  function fail(msg: string): void {
    status.value = 'error'
    toast.error(msg)
  }

  function succeed(msg: string): void {
    status.value = 'success'
    toast.success(msg)
  }

  function resetSoon(ms = 4000): void {
    clearTimer()
    timer = setTimeout(() => {
      status.value = 'idle'
    }, ms)
  }

  async function run(test: () => Promise<TestResult>): Promise<TestResult | null> {
    clearTimer()
    status.value = 'testing'
    try {
      const result = await test()
      passed.value = result.ok
      let text = result.message
      if (!text) {
        text = result.ok ? '连接成功' : '连接失败'
      }
      if (result.ok) succeed(text)
      else fail(text)
      return result
    } catch (e) {
      passed.value = false
      status.value = 'error'
      toast.fromError(e, '连接失败')
      return null
    }
  }

  onUnmounted(clearTimer)

  return { status, passed, invalidate, fail, succeed, resetSoon, run }
}
