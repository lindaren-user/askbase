<script setup lang="ts">
import { CircleAlert, CircleCheck, Info, X } from 'lucide-vue-next'
import { useToast, type ToastKind } from '@/composables/useToast'

const { items, dismiss } = useToast()

const ICONS: Record<ToastKind, typeof CircleCheck> = {
  success: CircleCheck,
  error: CircleAlert,
  info: Info,
}

const TONE: Record<ToastKind, string> = {
  success: 'text-emerald-600 dark:text-emerald-400',
  error: 'text-red-600 dark:text-red-400',
  info: 'text-gray-500 dark:text-gray-400',
}
</script>

<template>
  <Teleport to="body">
    <div class="pointer-events-none fixed right-4 top-4 z-[200] flex w-[min(22rem,calc(100vw-2rem))] flex-col gap-2">
      <TransitionGroup name="toast">
        <div
          v-for="item in items"
          :key="item.id"
          class="pointer-events-auto flex items-start gap-2.5 rounded-xl border border-gray-200 bg-white px-3.5 py-3 shadow-lg dark:border-gray-700 dark:bg-gray-900"
        >
          <component :is="ICONS[item.kind]" class="mt-0.5 size-4 shrink-0" :class="TONE[item.kind]" />
          <p class="min-w-0 flex-1 text-sm leading-5 text-gray-800 dark:text-gray-100">{{ item.message }}</p>
          <button
            type="button"
            class="shrink-0 rounded-md p-0.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
            aria-label="关闭"
            @click="dismiss(item.id)"
          >
            <X class="size-3.5" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>
