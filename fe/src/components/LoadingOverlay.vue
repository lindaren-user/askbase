<script setup lang="ts">
import { LoaderCircle } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    message?: string
  }>(),
  {
    message: '加载中...',
  },
)
</script>

<template>
  <div
    class="absolute inset-0 z-10 flex cursor-wait flex-col items-center justify-center bg-white/90 text-gray-700 dark:bg-gray-900/90 dark:text-gray-200"
    role="status"
    aria-live="polite"
    @click.prevent
  >
    <LoaderCircle class="mb-2 size-7 animate-spin" />
    <p class="text-sm font-medium">{{ message }}</p>
  </div>
</template>
