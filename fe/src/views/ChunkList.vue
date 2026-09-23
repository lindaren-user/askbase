<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ArrowLeft, ChevronRight } from 'lucide-vue-next'
import { useAppState } from '@/composables/useAppState'
import { useToast } from '@/composables/useToast'
import { renderMarkdown } from '@/lib/markdown'
import { getDocument, listChunks, updateChunk, type ApiChunk } from '@/services/api'

const props = defineProps<{
  documentId: number
}>()

const { navigate } = useAppState()
const toast = useToast()

const items = ref<ApiChunk[]>([])
const documentName = ref('')
const datasetId = ref(0)
const loading = ref(true)
const expanded = ref<Set<string>>(new Set())
const previewImage = ref('')
const editingId = ref<string | null>(null)
const editDraft = ref('')
const saving = ref(false)
const elementTypeLabels: Record<ApiChunk['elementType'], string> = {
  text: '文本',
  title: '标题',
  table: '表格',
  formula: '公式',
  image: '图片',
  chart: '图表',
  code: '代码',
  list: '列表',
}

async function reload(): Promise<void> {
  try {
    const doc = await getDocument(props.documentId)
    documentName.value = doc.name
    datasetId.value = doc.datasetId
    items.value = await listChunks(props.documentId)
  } catch (e) {
    toast.fromError(e, '加载分块失败')
  } finally {
    loading.value = false
  }
}

async function toggleEnabled(chunk: ApiChunk): Promise<void> {
  try {
    await updateChunk(chunk.id, { enabled: !chunk.enabled })
    chunk.enabled = !chunk.enabled
    toast.success(chunk.enabled ? '分块已启用' : '分块已停用')
  } catch (e) {
    toast.fromError(e, '更新失败')
  }
}

function toggleExpand(id: string): void {
  if (expanded.value.has(id)) expanded.value.delete(id)
  else expanded.value.add(id)
  expanded.value = new Set(expanded.value)
}

function isStructuredChunk(chunk: ApiChunk): boolean {
  if (chunk.elementType !== 'text') return true
  return /^\s*(?:<table\b|\|[^\n]*\|\s*\n\|)/i.test(chunk.content)
}

function toggleChunk(chunk: ApiChunk): void {
  if (!isStructuredChunk(chunk)) toggleExpand(chunk.id)
}

function startEdit(chunk: ApiChunk): void {
  editingId.value = chunk.id
  editDraft.value = chunk.content
  if (!expanded.value.has(chunk.id)) {
    expanded.value = new Set(expanded.value).add(chunk.id)
  }
}

async function saveEdit(chunk: ApiChunk): Promise<void> {
  const content = editDraft.value.trim()
  if (!content) {
    toast.error('分块内容不能为空')
    return
  }
  if (content === chunk.content) {
    editingId.value = null
    return
  }
  saving.value = true
  try {
    await updateChunk(chunk.id, { content })
    chunk.content = content
    editingId.value = null
    toast.success('分块已保存')
  } catch (e) {
    toast.fromError(e, '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(reload)
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden bg-white dark:bg-gray-950">
    <header class="mx-auto flex w-full max-w-4xl shrink-0 items-center gap-1.5 px-8 py-4 text-sm">
        <button
          class="flex shrink-0 items-center gap-1 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
          @click="navigate({ view: 'documents', datasetId })"
        >
          <ArrowLeft class="size-4" />文档
        </button>
        <ChevronRight class="size-4 shrink-0 text-gray-300 dark:text-gray-600" />
        <span class="truncate font-medium text-gray-900 dark:text-white">{{ documentName }} 的分块</span>
    </header>

    <div class="mx-auto min-h-0 w-full max-w-4xl flex-1 overflow-y-auto px-8 py-6">
      <div v-if="loading" class="py-20 text-center text-sm text-gray-400">加载中...</div>
      <div
        v-else-if="!items.length"
        class="rounded-xl border border-gray-200 py-16 text-center text-sm text-gray-400 dark:border-gray-800"
      >
        暂无分块
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="chunk in items"
          :key="chunk.id"
          class="rounded-xl border border-gray-200 bg-white px-5 py-4 transition-colors dark:border-gray-800 dark:bg-gray-900"
          :class="{ 'opacity-60': !chunk.enabled }"
        >
          <div class="flex items-center gap-3">
            <span class="flex size-7 shrink-0 items-center justify-center rounded-md bg-gray-100 text-xs font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300">
              {{ chunk.chunkIndex }}
            </span>
            <div class="flex items-center gap-3 text-xs text-gray-400">
              <span>约 {{ chunk.tokenNum }} tokens</span>
              <span v-if="chunk.elementType !== 'text'">{{ elementTypeLabels[chunk.elementType] }}</span>
              <span v-if="chunk.pageNumber > 0">第 {{ chunk.pageNumber }} 页</span>
              <span v-if="!chunk.enabled" class="text-gray-400">不参与检索</span>
            </div>
            <button
              class="ml-auto rounded-md px-2 py-1 text-xs text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
              @click="startEdit(chunk)"
            >
              编辑
            </button>
            <button
              class="relative h-5 w-9 shrink-0 rounded-full transition-colors"
              :class="chunk.enabled ? 'bg-blue-500' : 'bg-gray-300 dark:bg-gray-600'"
              :title="chunk.enabled ? '点击禁用，禁用后不参与检索' : '点击启用'"
              @click="toggleEnabled(chunk)"
            >
              <span
                class="absolute top-0.5 size-4 rounded-full bg-white shadow transition-all"
                :class="chunk.enabled ? 'left-[18px]' : 'left-0.5'"
              />
            </button>
          </div>
          <img
            v-if="chunk.imageUrl"
            :src="chunk.imageUrl"
            alt=""
            class="mb-2 max-h-48 w-full cursor-zoom-in rounded-lg object-contain bg-gray-50 dark:bg-gray-950"
            @click="previewImage = chunk.imageUrl || ''"
          />
          <textarea
            v-if="editingId === chunk.id"
            v-model="editDraft"
            rows="6"
            class="mt-2 w-full resize-y rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm leading-6 outline-none dark:border-gray-700 dark:bg-gray-800 dark:text-gray-200"
          />
          <div
            v-else
            class="md-body chunk-markdown mt-2 break-words text-sm leading-6 text-gray-600 dark:text-gray-300"
            :class="{
              'max-h-[4.5rem] cursor-pointer overflow-hidden': !isStructuredChunk(chunk) && !expanded.has(chunk.id),
              'cursor-pointer': !isStructuredChunk(chunk),
            }"
            @click="toggleChunk(chunk)"
            v-html="renderMarkdown(chunk.content)"
          />
          <div v-if="editingId === chunk.id" class="mt-2 flex justify-end gap-2">
            <button
              class="rounded-lg px-3 py-1.5 text-xs text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800"
              @click="editingId = null"
            >
              取消
            </button>
            <button
              class="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50 dark:bg-white dark:text-gray-900"
              :disabled="saving"
              @click="saveEdit(chunk)"
            >
              {{ saving ? '保存中...' : '保存并重算向量' }}
            </button>
          </div>
        </div>
      </div>
    </div>
    <Teleport to="body">
      <div
        v-if="previewImage"
        class="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 p-8"
        @click="previewImage = ''"
      >
        <img :src="previewImage" alt="" class="max-h-full max-w-full object-contain" />
      </div>
    </Teleport>
  </div>
</template>
