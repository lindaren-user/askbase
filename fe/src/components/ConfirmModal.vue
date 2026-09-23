<script setup lang="ts">
import { X } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    open: boolean
    title: string
    message: string
    confirmText?: string
    danger?: boolean
  }>(),
  { confirmText: '确认', danger: false },
)

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="fixed inset-0 z-[110] flex items-center justify-center">
        <div class="absolute inset-0 z-0 bg-black/50" @click="emit('cancel')" />
        <div class="relative z-10 w-[380px] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900">
          <div class="flex items-center justify-between border-b border-gray-200 px-5 py-3.5 dark:border-gray-800">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ title }}</h3>
            <button
              class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
              aria-label="关闭"
              @click="emit('cancel')"
            >
              <X class="size-4" />
            </button>
          </div>
          <div class="px-5 py-4">
            <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ message }}</p>
            <div class="mt-5 flex justify-end gap-2">
              <button
                class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 transition-colors hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
                @click="emit('cancel')"
              >
                取消
              </button>
              <button
                class="rounded-lg px-4 py-2 text-sm font-medium text-white transition-colors"
                :class="danger ? 'bg-red-600 hover:bg-red-700' : 'bg-gray-900 hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200'"
                @click="emit('confirm')"
              >
                {{ confirmText }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
