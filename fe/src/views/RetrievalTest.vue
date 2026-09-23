<script setup lang="ts">
import { ref } from 'vue'
import { ArrowLeft, ChevronRight, Search } from 'lucide-vue-next'
import { retrievalTest, type ApiRetrievalHit } from '@/services/api'
import { useAppState } from '@/composables/useAppState'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  datasetId: number
  datasetName: string
}>()

const { navigate } = useAppState()
const toast = useToast()

const query = ref('')
const topK = ref(10)
const minScore = ref(0.2)
const hits = ref<ApiRetrievalHit[]>([])
const searching = ref(false)
const searched = ref(false)
const expanded = ref<Set<string>>(new Set())

async function run() {
  const q = query.value.trim()
  if (!q) {
    toast.error('请输入检索内容')
    return
  }
  if (searching.value) return
  searching.value = true
  try {
    hits.value = await retrievalTest(props.datasetId, {
      query: q,
      topK: topK.value,
      minScore: minScore.value,
    })
    searched.value = true
  } catch (e) {
    toast.fromError(e, '检索失败')
  } finally {
    searching.value = false
  }
}

function toggleExpand(id: string): void {
  if (expanded.value.has(id)) expanded.value.delete(id)
  else expanded.value.add(id)
  expanded.value = new Set(expanded.value)
}

function scoreClass(score: number) {
  if (score >= 0.6) return 'text-green-600 dark:text-green-400'
  if (score >= 0.4) return 'text-blue-600 dark:text-blue-400'
  return 'text-gray-500 dark:text-gray-400'
}

function scoreBarClass(score: number) {
  if (score >= 0.6) return 'bg-green-500'
  if (score >= 0.4) return 'bg-blue-500'
  return 'bg-gray-400'
}
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden bg-white dark:bg-gray-950">
    <header class="mx-auto flex w-full max-w-5xl shrink-0 items-center px-8 py-4">
      <div class="flex min-w-0 items-center gap-1.5 text-sm">
        <button
          class="flex items-center gap-1 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
          @click="navigate({ view: 'documents', datasetId, datasetName: props.datasetName })"
        >
          <ArrowLeft class="size-4" />{{ datasetName }}
        </button>
        <ChevronRight class="size-4 shrink-0 text-gray-300 dark:text-gray-600" />
        <span class="truncate font-medium text-gray-900 dark:text-white">检索测试</span>
      </div>
    </header>

    <div class="mx-auto min-h-0 w-full max-w-5xl flex-1 overflow-y-auto px-8 py-6">
      <div class="mb-6 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
        <div class="flex gap-2">
          <input
            v-model="query"
            type="text"
            class="flex-1 rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm outline-none transition-colors focus:border-gray-400 dark:border-gray-600 dark:bg-gray-800 dark:focus:border-gray-500"
            placeholder="输入测试问题，只召回分块，不调用大模型"
            @keydown.enter="run"
          />
          <button
            class="flex items-center gap-1.5 rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-gray-800 disabled:opacity-50 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
            :disabled="searching || !query.trim()"
            @click="run"
          >
            <Search class="size-4" />{{ searching ? '检索中...' : '检索' }}
          </button>
        </div>
        <div class="mt-4 flex items-center gap-6 text-xs text-gray-500 dark:text-gray-400">
          <label class="flex items-center gap-2">
            TopK
            <input v-model.number="topK" type="range" min="1" max="20" class="w-28" />
            <span class="w-6 text-center font-medium text-gray-700 dark:text-gray-200">{{ topK }}</span>
          </label>
          <label class="flex items-center gap-2">
            相似度阈值
            <input v-model.number="minScore" type="range" min="0" max="0.9" step="0.05" class="w-28" />
            <span class="w-8 text-center font-medium text-gray-700 dark:text-gray-200">{{ minScore.toFixed(2) }}</span>
          </label>
        </div>
        <p class="mt-3 text-xs text-gray-400">仅召回解析完成且已启用的文档</p>
      </div>

      <div v-if="searched && !hits.length" class="rounded-xl border border-gray-200 py-16 text-center text-sm text-gray-400 dark:border-gray-800">
        无命中分块，可降低相似度阈值或检查文档是否完成解析
      </div>

      <div class="space-y-2">
        <div
          v-for="hit in hits"
          :key="hit.chunkId"
          class="rounded-xl border border-gray-200 bg-white px-5 py-4 dark:border-gray-800 dark:bg-gray-900"
        >
          <div class="flex items-center gap-3 text-xs">
            <span class="font-mono font-semibold" :class="scoreClass(hit.score)">
              {{ hit.score.toFixed(3) }}
            </span>
            <span class="truncate text-gray-500 dark:text-gray-400">{{ hit.documentName }}</span>
            <span class="ml-auto text-gray-300 dark:text-gray-600">chunk #{{ hit.chunkId }}</span>
          </div>
          <div class="mt-1.5 h-1 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
            <div
              class="h-full rounded-full"
              :class="scoreBarClass(hit.score)"
              :style="{ width: `${Math.min(100, hit.score * 100)}%` }"
            />
          </div>
          <img
            v-if="hit.imageUrl"
            :src="hit.imageUrl"
            alt=""
            class="mt-3 max-h-40 w-full rounded-lg object-contain bg-gray-50 dark:bg-gray-950"
          />
          <p
            class="mt-2 cursor-pointer whitespace-pre-wrap break-words text-sm leading-6 text-gray-600 dark:text-gray-300"
            :class="{ 'line-clamp-3': !expanded.has(hit.chunkId) }"
            @click="toggleExpand(hit.chunkId)"
          >
            {{ hit.content }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
