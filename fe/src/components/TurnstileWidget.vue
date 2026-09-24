<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  sitekey: string
  action?: string
}>()

const emit = defineEmits<{
  verified: [token: string]
  error: [message: string]
  expired: []
}>()

const container = ref<HTMLElement | null>(null)
const widgetId = ref<string | null>(null)

declare global {
  interface Window {
    turnstile?: {
      render: (el: HTMLElement, options: Record<string, unknown>) => string
      remove: (id: string) => void
      reset: (id?: string) => void
    }
  }
}

function loadScript(): Promise<void> {
  if (window.turnstile) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>('script[data-turnstile]')
    if (existing) {
      if (existing.dataset.ready === '1' || window.turnstile) {
        resolve()
        return
      }
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('加载 Turnstile 失败')), { once: true })
      return
    }
    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    script.async = true
    script.defer = true
    script.dataset.turnstile = '1'
    script.onload = () => {
      script.dataset.ready = '1'
      resolve()
    }
    script.onerror = () => reject(new Error('加载 Turnstile 失败'))
    document.head.appendChild(script)
  })
}

// reset 丢弃已经提交过的单次令牌，并让 Turnstile 生成新的验证结果。
function reset() {
  if (widgetId.value && window.turnstile) {
    window.turnstile.reset(widgetId.value)
  }
  emit('expired')
}

defineExpose({ reset })

async function mountWidget() {
  if (!props.sitekey || !container.value) return
  try {
    await loadScript()
    if (!window.turnstile || !container.value) return
    if (widgetId.value) {
      window.turnstile.remove(widgetId.value)
      widgetId.value = null
    }
    widgetId.value = window.turnstile.render(container.value, {
      sitekey: props.sitekey,
      action: props.action || 'login',
      theme: 'auto',
      size: 'flexible',
      callback: (token: string) => emit('verified', token),
      'error-callback': () => emit('error', '人机验证失败，请重试'),
      'expired-callback': () => emit('expired'),
    })
  } catch (e) {
    emit('error', e instanceof Error ? e.message : '人机验证不可用')
  }
}

onMounted(() => {
  void mountWidget()
})

watch(
  () => props.sitekey,
  () => {
    void mountWidget()
  },
)

onBeforeUnmount(() => {
  if (widgetId.value && window.turnstile) {
    window.turnstile.remove(widgetId.value)
  }
})
</script>

<template>
  <div ref="container" class="min-h-[65px] w-full" />
</template>
