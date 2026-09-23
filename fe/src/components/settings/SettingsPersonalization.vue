<script setup lang="ts">
import { Check, Monitor, Moon, Sun } from 'lucide-vue-next'
import { useColorMode } from '@/composables/useColorMode'

const { preference } = useColorMode()

const options = [
  { value: 'system', label: '跟随系统', icon: Monitor, tip: '读取系统深色/浅色偏好；19:00-07:00 自动深色' },
  { value: 'light', label: '浅色模式', icon: Sun, tip: '' },
  { value: 'dark', label: '深色模式', icon: Moon, tip: '' },
] as const
</script>

<template>
  <div class="space-y-8">
    <div>
      <h4 class="font-medium text-gray-900 dark:text-white">主题模式</h4>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">选择适合你的界面主题</p>
      <div class="mt-4 grid grid-cols-3 gap-4">
        <button
          v-for="option in options"
          :key="option.value"
          type="button"
          class="relative flex flex-col items-center gap-2 rounded-lg border px-4 py-3 transition-colors"
          :title="option.tip || option.label"
          :class="
            preference === option.value
              ? 'border-gray-900 bg-white text-gray-900 dark:border-white dark:bg-gray-800 dark:text-white'
              : 'border-gray-200 bg-gray-50 text-gray-600 hover:bg-white dark:border-gray-700 dark:bg-gray-800/60 dark:text-gray-400 dark:hover:bg-gray-800'
          "
          @click="preference = option.value"
        >
          <component :is="option.icon" class="size-6 text-gray-600 dark:text-gray-400" />
          <span class="text-sm text-gray-700 dark:text-gray-300">{{ option.label }}</span>
          <Check v-if="preference === option.value" class="absolute right-2 top-2 size-4" />
        </button>
      </div>
    </div>
  </div>
</template>
