<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Mail, X } from 'lucide-vue-next'
import TurnstileWidget from './TurnstileWidget.vue'
import { getTurnstileConfig, loginWithCode, sendAuthCode, type ApiUser, type TurnstileConfig } from '@/services/api'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  success: [user: ApiUser]
}>()

const email = ref('')
const code = ref('')
const turnstileToken = ref('')
const turnstileConfig = ref<TurnstileConfig | null>(null)
const turnstileWidget = ref<{ reset: () => void } | null>(null)
const loading = ref(false)
const sendingCode = ref(false)
const codeCountdown = ref(0)
let countdownTimer: ReturnType<typeof setTimeout> | null = null
const toast = useToast()

const submitLabel = computed(() => (loading.value ? '处理中...' : '登录'))

const turnstileEnabled = computed(
  () => Boolean(turnstileConfig.value?.enabled && turnstileConfig.value.siteKey),
)

async function loadTurnstileConfig() {
  try {
    turnstileConfig.value = await getTurnstileConfig()
  } catch {
    turnstileConfig.value = { siteKey: '', enabled: false }
  }
}

watch(
  () => props.open,
  (open) => {
    turnstileToken.value = ''
    if (!open) return
    void loadTurnstileConfig()
  },
)

function clearCountdown() {
  codeCountdown.value = 0
  if (countdownTimer) {
    clearTimeout(countdownTimer)
    countdownTimer = null
  }
}

function tickCountdown() {
  codeCountdown.value--
  if (codeCountdown.value > 0) {
    countdownTimer = setTimeout(tickCountdown, 1000)
  }
}

function startCountdown() {
  clearCountdown()
  codeCountdown.value = 60
  countdownTimer = setTimeout(tickCountdown, 1000)
}

function isValidEmail(value: string): boolean {
  return Boolean(value.trim() && value.includes('@'))
}

function requireValidEmail(): boolean {
  if (isValidEmail(email.value)) return true
  toast.error('请输入正确的邮箱地址')
  return false
}

async function handleSendCode() {
  if (!requireValidEmail()) return
  if (sendingCode.value || codeCountdown.value > 0) return
  if (turnstileEnabled.value && !turnstileToken.value) {
    toast.error('请先完成人机验证')
    return
  }
  sendingCode.value = true
  try {
    await sendAuthCode(email.value.trim(), turnstileToken.value)
    startCountdown()
    toast.success('验证码已发送')
  } catch (error) {
    toast.fromError(error, '发送验证码失败')
  } finally {
    sendingCode.value = false
    if (turnstileEnabled.value) {
      // Turnstile 令牌只能校验一次；请求结束后必须重置，避免重发时复用旧令牌。
      turnstileToken.value = ''
      turnstileWidget.value?.reset()
    }
  }
}

async function handleSubmit() {
  if (!requireValidEmail()) return
  if (!code.value.trim()) {
    toast.error('请输入验证码')
    return
  }
  loading.value = true
  try {
    const apiUser = await loginWithCode(email.value.trim(), code.value.replace(/\D/g, ''), turnstileToken.value)
    toast.success('登录成功')
    emit('success', apiUser)
  } catch (error) {
    toast.fromError(error, '登录失败')
  } finally {
    loading.value = false
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') void handleSubmit()
}
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="fixed inset-0 z-[100] flex items-center justify-center">
        <div class="absolute inset-0 z-0 cursor-pointer bg-black/50" @click="emit('close')" />
        <div
          class="relative z-10 w-[420px] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900"
        >
          <div
            class="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-800"
          >
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">登录 AskBase</h3>
            <button
              class="rounded-lg p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
              aria-label="关闭"
              @click="emit('close')"
            >
              <X class="size-5" />
            </button>
          </div>

          <div class="p-6">
            <div class="space-y-4">
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">
                  邮箱
                </label>
                <div class="relative">
                  <Mail
                    class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400"
                  />
                  <input
                    v-model="email"
                    type="email"
                    class="w-full rounded-lg border border-gray-200 bg-white py-2.5 pl-10 pr-3 text-sm text-gray-700 outline-none transition-colors focus:border-gray-400 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300 dark:focus:border-gray-500"
                    placeholder="请输入邮箱"
                    @keydown="handleKeydown"
                  />
                </div>
              </div>

              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">
                  验证码
                </label>
                <div class="flex gap-2">
                  <input
                    v-model="code"
                    type="text"
                    maxlength="6"
                    class="flex-1 rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm text-gray-700 outline-none transition-colors focus:border-gray-400 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300 dark:focus:border-gray-500"
                    placeholder="请输入6位验证码"
                    @keydown="handleKeydown"
                  />
                  <button
                    type="button"
                    class="shrink-0 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors"
                    :class="
                      codeCountdown > 0 || sendingCode
                        ? 'cursor-not-allowed bg-gray-200 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
                        : 'bg-gray-900 text-white hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200'
                    "
                    :disabled="codeCountdown > 0 || sendingCode"
                    @click="handleSendCode"
                  >
                    {{ sendingCode ? '发送中...' : codeCountdown > 0 ? `${codeCountdown}s` : '发送验证码' }}
                  </button>
                </div>
              </div>

              <TurnstileWidget
                v-if="turnstileEnabled"
                ref="turnstileWidget"
                :sitekey="turnstileConfig!.siteKey"
                action="login"
                @verified="turnstileToken = $event"
                @error="toast.error($event)"
                @expired="turnstileToken = ''"
              />
            </div>

            <button
              class="mt-6 w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-gray-800 active:scale-[0.99] dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
              :disabled="loading"
              @click="handleSubmit"
            >
              {{ submitLabel }}
            </button>

            <p class="mt-4 text-center text-sm text-gray-500 dark:text-gray-400">
              未注册用户登录即注册
            </p>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
