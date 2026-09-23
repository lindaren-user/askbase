<script setup lang="ts">
import { Archive, MessageSquare, TriangleAlert } from 'lucide-vue-next'
import { formatRelativeTime, type ApiSession } from '@/services/api'

defineProps<{
  items: ApiSession[]
}>()

const emit = defineEmits<{
  restore: [sessionId: number]
}>()
</script>

<template>
  <div class="flex min-h-full flex-col">
    <div
      v-if="items.length === 0"
      class="flex flex-1 flex-col items-center justify-center text-gray-400 dark:text-gray-500"
    >
      <Archive class="size-10" />
      <p class="mt-2 text-sm">暂无归档会话</p>
    </div>
    <div v-else class="space-y-1">
      <div
        v-for="item in items"
        :key="item.id"
        class="flex items-center gap-3 rounded-lg px-3 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/50"
      >
        <MessageSquare class="size-5 shrink-0 text-gray-400" />
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900 dark:text-white">
            {{ item.title || '新会话' }}
          </p>
          <p class="flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400">
            <TriangleAlert
              v-if="item.datasetDeleted || !item.datasetName"
              class="size-3.5 shrink-0 text-amber-500"
              title="所属知识库已删除"
            />
            <span>{{ item.datasetName || '知识库已删除' }} · {{ formatRelativeTime(item.updatedAt) }}</span>
          </p>
        </div>
        <button
          type="button"
          class="rounded-lg px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
          @click="emit('restore', item.id)"
        >
          恢复
        </button>
      </div>
    </div>
  </div>
</template>
