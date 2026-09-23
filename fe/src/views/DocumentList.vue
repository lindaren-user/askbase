<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  ArrowLeft,
  Check,
  ChevronRight,
  CircleAlert,
  Download,
  Eye,
  FileText,
  FlaskConical,
  Layers,
  Play,
  Power,
  RefreshCw,
  Square,
  Trash2,
  Upload,
  X,
} from 'lucide-vue-next'
import {
  batchDocumentStatus,
  createUploadUrl,
  deleteDocument,
  downloadDocument,
  formatBytes,
  formatRelativeTime,
  getDocumentProgress,
  hashFile,
  listDocuments,
  parseDocument,
  previewDocument,
  putToObjectStorage,
  registerDocument,
  stopDocument,
  updateDocument,
  type ApiDocument,
  type ApiDocumentPreview,
} from '@/services/api'
import ConfirmModal from '@/components/ConfirmModal.vue'
import LoadingOverlay from '@/components/LoadingOverlay.vue'
import { useAppState } from '@/composables/useAppState'
import { useConfirmModal } from '@/composables/useConfirmModal'
import { useToast } from '@/composables/useToast'
import { renderMarkdown } from '@/lib/markdown'

const props = defineProps<{
  datasetId: number
  datasetName: string
}>()

const { navigate } = useAppState()
const toast = useToast()
const {
  open: deleteOpen,
  title: deleteTitle,
  message: deleteMessage,
  ask: askDelete,
  confirm: confirmDelete,
  cancel: cancelDelete,
} = useConfirmModal()

const PIPELINE = ['pending', 'parsing', 'chunking', 'embedding', 'cancelling']

const items = ref<ApiDocument[]>([])
const loading = ref(true)
const dragOver = ref(false)
const uploading = ref(false)

const uploadAreaClass = computed((): string => {
  if (uploading.value) return 'cursor-wait border-gray-300 dark:border-gray-700'
  if (dragOver.value) return 'border-blue-400 bg-blue-50 dark:border-blue-600 dark:bg-blue-950/40'
  return 'cursor-pointer border-gray-300 text-gray-400 hover:border-gray-400 dark:border-gray-700 dark:hover:border-gray-500'
})

function inPipeline(status: string) {
  return PIPELINE.includes(status)
}

const selected = ref<Set<number>>(new Set())
const preview = ref<ApiDocumentPreview | null>(null)
const previewLoading = ref(false)
const errorTooltip = ref<{
  documentId: number
  message: string
  left: string
  top: string
  width: string
  maxHeight: string
  transform: string
} | null>(null)
let errorTooltipHideTimer: ReturnType<typeof setTimeout> | null = null

const previewMarkdown = computed((): string => {
  const item = preview.value
  if (item?.kind !== 'markdown' || !item.content) return ''
  return renderMarkdown(item.content)
})

const allSelected = computed(
  () => items.value.length > 0 && items.value.every((d) => selected.value.has(d.id)),
)

function toggleSelect(id: number) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function toggleSelectAll() {
  if (allSelected.value) {
    selected.value = new Set()
    return
  }
  selected.value = new Set(items.value.map((d) => d.id))
}

function showErrorTooltip(event: MouseEvent | FocusEvent, doc: ApiDocument): void {
  if (!doc.parseError) return

  clearErrorTooltipHideTimer()

  const target = event.currentTarget as HTMLElement
  if (target.scrollWidth <= target.clientWidth) {
    errorTooltip.value = null
    return
  }

  const rect = target.getBoundingClientRect()
  const viewportPadding = 16
  const tooltipWidth = Math.min(480, window.innerWidth - viewportPadding * 2)
  const left = Math.min(
    Math.max(rect.left, viewportPadding),
    window.innerWidth - tooltipWidth - viewportPadding,
  )
  const availableAbove = rect.top - viewportPadding - 8
  const availableBelow = window.innerHeight - rect.bottom - viewportPadding - 8
  const placeAbove = availableBelow < 320 && availableAbove > availableBelow
  const maxHeight = Math.min(320, placeAbove ? availableAbove : availableBelow)

  errorTooltip.value = {
    documentId: doc.id,
    message: doc.parseError,
    left: `${left}px`,
    top: `${placeAbove ? rect.top - 8 : rect.bottom + 8}px`,
    width: `${tooltipWidth}px`,
    maxHeight: `${Math.max(maxHeight, 0)}px`,
    transform: placeAbove ? 'translateY(-100%)' : 'none',
  }
}

function clearErrorTooltipHideTimer(): void {
  if (!errorTooltipHideTimer) return
  clearTimeout(errorTooltipHideTimer)
  errorTooltipHideTimer = null
}

function hideErrorTooltip(documentId?: number): void {
  if (documentId !== undefined && errorTooltip.value?.documentId !== documentId) return
  clearErrorTooltipHideTimer()
  errorTooltip.value = null
}

function scheduleErrorTooltipHide(documentId: number): void {
  clearErrorTooltipHideTimer()
  errorTooltipHideTimer = setTimeout(() => hideErrorTooltip(documentId), 120)
}

function handleViewportResize(): void {
  hideErrorTooltip()
}

// 解析进度轮询：有进行中文档时 1.5s 一轮
let pollTimer: ReturnType<typeof setTimeout> | null = null
const progressMap = ref<Record<number, { status: string; progress: number; stage: string }>>({})

const hasActive = computed(() => items.value.some((d) => inPipeline(d.status)))

async function reload() {
  try {
    hideErrorTooltip()
    items.value = await listDocuments(props.datasetId)
    const next: Record<number, { status: string; progress: number; stage: string }> = {}
    for (const doc of items.value) {
      if (!inPipeline(doc.status)) continue
      if (progressMap.value[doc.id]) next[doc.id] = progressMap.value[doc.id]
    }
    progressMap.value = next
  } catch (e) {
    toast.fromError(e, '加载文档失败')
  } finally {
    loading.value = false
  }
}

async function pollProgress() {
  const active = items.value.filter((d) => inPipeline(d.status))
  for (const doc of active) {
    try {
      const p = await getDocumentProgress(doc.id)
      progressMap.value[doc.id] = { status: p.status, progress: p.progress, stage: p.stage }
      if (p.status === 'done' || p.status === 'failed' || p.status === 'cancelled') {
        await reload()
        break
      }
    } catch {
      // 单条失败忽略
    }
  }
}

function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer)
  // TODO: 停止流程改为前端立即展示终态、worker 后台协作退出后，移除 cancelling 的高频轮询。
  const cancelling = items.value.some((d) => statusOf(d).status === 'cancelling')
  pollTimer = setTimeout(async () => {
    await pollProgress()
    if (hasActive.value) schedulePoll()
  }, cancelling ? 400 : 1500)
}

async function uploadFile(file: File): Promise<void> {
  try {
    const fileHash = await hashFile(file)
    const token = await createUploadUrl({
      datasetId: props.datasetId,
      filename: file.name,
      contentType: file.type || 'application/octet-stream',
      size: file.size,
      fileHash,
    })
    await putToObjectStorage(token, file)
    const result = await registerDocument(props.datasetId, {
      name: file.name,
      r2Key: token.key,
      fileHash,
      sizeBytes: file.size,
    })
    if (result.created) {
      toast.success(`「${file.name}」已上传`)
      return
    }
    toast.info(`「${file.name}」内容已存在，未重复解析`)
  } catch (e) {
    toast.fromError(e, `上传 ${file.name} 失败`)
  }
}

async function handleFiles(files: FileList | File[]): Promise<void> {
  if (uploading.value || files.length === 0) return
  uploading.value = true
  dragOver.value = false
  try {
    for (const file of files) {
      await uploadFile(file)
    }
    await reload()
    schedulePoll()
  } finally {
    uploading.value = false
  }
}

function onDragOver(): void {
  if (!uploading.value) dragOver.value = true
}

function onDrop(e: DragEvent): void {
  dragOver.value = false
  if (uploading.value) return
  if (e.dataTransfer?.files?.length) void handleFiles(e.dataTransfer.files)
}

function onPick(e: Event): void {
  const input = e.target as HTMLInputElement
  if (uploading.value) return
  if (input.files?.length) void handleFiles(input.files)
  input.value = ''
}

async function toggleEnabled(doc: ApiDocument): Promise<void> {
  try {
    await updateDocument(doc.id, { enabled: !doc.enabled })
    doc.enabled = !doc.enabled
    toast.success(doc.enabled ? '文档已启用' : '文档已停用')
  } catch (e) {
    toast.fromError(e, '更新失败')
  }
}

function requestDelete(doc: ApiDocument): void {
  askDelete({
    title: '删除文档',
    message: `确定删除「${doc.name}」吗？相关分块与存储文件将一并删除，且不可恢复。`,
    onConfirm: async () => {
      try {
        await deleteDocument(doc.id)
        await reload()
        toast.success('文档已删除')
      } catch (e) {
        toast.fromError(e, '删除失败')
      }
    },
  })
}

async function onRetry(doc: ApiDocument) {
  try {
    await parseDocument(doc.id)
    doc.status = 'pending'
    doc.progress = 0
    doc.parseError = undefined
    schedulePoll()
    toast.success('已重新解析')
  } catch (e) {
    toast.fromError(e, '重试失败')
  }
}

async function onStop(doc: ApiDocument) {
  const fromPending = statusOf(doc).status === 'pending'
  try {
    await stopDocument(doc.id)
    doc.status = fromPending ? 'cancelled' : 'cancelling'
    if (!fromPending) schedulePoll()
    toast.success(fromPending ? '已取消解析' : '正在停止解析')
  } catch (e) {
    toast.fromError(e, '停止失败')
  }
}

async function onBatchEnabled(enabled: boolean): Promise<void> {
  const ready = items.value.filter((d) => selected.value.has(d.id) && isParseDone(d))
  if (!ready.length) {
    toast.info('仅解析完成的文档可启停检索')
    return
  }
  try {
    await batchDocumentStatus(ready.map((d) => d.id), enabled)
    for (const doc of ready) doc.enabled = enabled
    toast.success(enabled ? '已批量启用' : '已批量停用')
  } catch (e) {
    toast.fromError(e, '批量更新失败')
  }
}

async function onPreview(doc: ApiDocument) {
  hideErrorTooltip()
  previewLoading.value = true
  try {
    preview.value = await previewDocument(doc.id)
  } catch (e) {
    preview.value = null
    toast.fromError(e, '预览失败')
  } finally {
    previewLoading.value = false
  }
}

async function onDownload(doc: ApiDocument) {
  try {
    const { url } = await downloadDocument(doc.id)
    window.open(url, '_blank', 'noopener')
  } catch (e) {
    toast.fromError(e, '下载失败')
  }
}

function statusOf(doc: ApiDocument) {
  const p = progressMap.value[doc.id]
  if (p && inPipeline(p.status)) {
    return { status: p.status, progress: p.progress, stage: p.stage }
  }
  return { status: doc.status, progress: doc.progress, stage: '' }
}

function isParseDone(doc: ApiDocument): boolean {
  return statusOf(doc).status === 'done'
}

const statusBadge: Record<string, { text: string; cls: string }> = {
  pending: { text: '排队中', cls: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300' },
  parsing: { text: '解析中', cls: 'bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-400' },
  chunking: { text: '分块中', cls: 'bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-400' },
  embedding: { text: '向量化', cls: 'bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-400' },
  done: { text: '已完成', cls: 'bg-green-50 text-green-600 dark:bg-green-950 dark:text-green-400' },
  failed: { text: '失败', cls: 'bg-red-50 text-red-600 dark:bg-red-950 dark:text-red-400' },
  cancelling: { text: '正在停止', cls: 'bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-400' },
  cancelled: { text: '已停止', cls: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300' },
}

onMounted(() => {
  window.addEventListener('resize', handleViewportResize)
  void reload().then(() => {
    if (hasActive.value) schedulePoll()
  })
})

onUnmounted(() => {
  window.removeEventListener('resize', handleViewportResize)
  clearErrorTooltipHideTimer()
  if (pollTimer) clearTimeout(pollTimer)
})
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden bg-white dark:bg-gray-950">
    <header class="mx-auto flex w-full max-w-5xl shrink-0 items-center justify-between px-8 py-4">
        <div class="flex min-w-0 items-center gap-1.5 text-sm">
          <button
            class="flex items-center gap-1 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
            @click="navigate({ view: 'datasets' })"
          >
            <ArrowLeft class="size-4" />知识库
          </button>
          <ChevronRight class="size-4 shrink-0 text-gray-300 dark:text-gray-600" />
          <span class="truncate font-medium text-gray-900 dark:text-white">{{ datasetName }}</span>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <button
            class="flex items-center gap-1.5 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
            @click="navigate({ view: 'retrieval', datasetId, datasetName: props.datasetName })"
          >
            <FlaskConical class="size-4" />检索测试
          </button>
        </div>
    </header>

    <div
      class="mx-auto min-h-0 w-full max-w-5xl flex-1 overflow-y-auto px-8 py-6"
      @scroll="hideErrorTooltip()"
    >
      <label
        class="relative mb-6 block overflow-hidden rounded-xl border border-dashed px-6 py-8 text-center transition-colors"
        :class="uploadAreaClass"
        @dragover.prevent="onDragOver"
        @dragleave.prevent="dragOver = false"
        @drop.prevent="onDrop"
      >
        <input
          type="file"
          multiple
          :disabled="uploading"
          accept=".txt,.csv,.md,.markdown,.pdf,.docx,.xlsx,.jpg,.jpeg,.png,.webp,.gif"
          class="hidden"
          @change="onPick"
        />
        <Upload class="mx-auto mb-2 size-8" />
        <p class="text-sm">拖拽 txt / csv / md / pdf / docx / xlsx / 图片到此处，或点击上传</p>
        <p class="mt-1 text-xs">文件经预签名地址直传对象存储，按内容哈希去重</p>
        <LoadingOverlay v-if="uploading" message="努力上传中..." />
      </label>

      <div v-if="loading" class="py-20 text-center text-sm text-gray-400">加载中...</div>

      <div
        v-else-if="!items.length"
        class="rounded-xl border border-gray-200 py-16 text-center text-sm text-gray-400 dark:border-gray-800"
      >
        暂无文档
      </div>

      <!-- 文档列表 -->
      <div v-else class="space-y-2">
        <label class="mb-1 flex items-center gap-2 px-1 text-xs text-gray-400">
          <input type="checkbox" :checked="allSelected" @change="toggleSelectAll" />
          全选
        </label>
        <div
          v-for="doc in items"
          :key="doc.id"
          class="rounded-xl border border-gray-200 bg-white px-5 py-4 dark:border-gray-800 dark:bg-gray-900"
        >
          <div class="flex items-center gap-3">
            <input type="checkbox" :checked="selected.has(doc.id)" @change="toggleSelect(doc.id)" />
            <FileText class="size-5 shrink-0 text-gray-400" />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ doc.name }}</span>
                <span
                  class="shrink-0 rounded-full px-2 py-0.5 text-xs"
                  :class="statusBadge[statusOf(doc).status]?.cls"
                >
                  {{ statusOf(doc).stage || statusBadge[statusOf(doc).status]?.text }}
                </span>
                <span v-if="isParseDone(doc) && !doc.enabled" class="shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-gray-800">
                  已停用
                </span>
              </div>
              <div class="mt-1 flex min-w-0 items-center gap-3 text-xs text-gray-400">
                <span class="shrink-0">{{ formatBytes(doc.sizeBytes) }}</span>
                <span class="shrink-0">{{ doc.chunkCount ?? 0 }} 分块</span>
                <span class="shrink-0">{{ formatRelativeTime(doc.createdAt) }}</span>
                <span
                  v-if="doc.parseError"
                  class="min-w-0 flex-1 cursor-help truncate rounded-sm text-red-400 outline-none focus-visible:ring-2 focus-visible:ring-red-300 dark:focus-visible:ring-red-700"
                  tabindex="0"
                  :aria-describedby="`document-error-${doc.id}`"
                  @pointerenter="showErrorTooltip($event, doc)"
                  @pointerleave="scheduleErrorTooltipHide(doc.id)"
                  @focus="showErrorTooltip($event, doc)"
                  @blur="hideErrorTooltip(doc.id)"
                >
                  {{ doc.parseError }}
                </span>
              </div>
              <!-- 解析进度条 -->
              <div
                v-if="inPipeline(statusOf(doc).status)"
                class="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800"
              >
                <div
                  class="h-full rounded-full bg-blue-500 transition-all duration-500"
                  :style="{ width: `${statusOf(doc).progress}%` }"
                />
              </div>
            </div>

            <div class="flex shrink-0 items-center gap-1">
              <button
                v-if="inPipeline(statusOf(doc).status) && statusOf(doc).status !== 'cancelling'"
                class="rounded-md p-2 text-gray-400 hover:bg-gray-100 hover:text-amber-600 dark:hover:bg-gray-800"
                title="停止解析"
                @click="onStop(doc)"
              >
                <Square class="size-4" />
              </button>
              <button
                v-if="statusOf(doc).status === 'cancelled'"
                class="flex items-center gap-1 rounded-md px-2 py-1.5 text-xs text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-950"
                title="继续解析"
                @click="onRetry(doc)"
              >
                <Play class="size-3.5 fill-current" />继续
              </button>
              <button
                v-if="statusOf(doc).status === 'failed'"
                class="rounded-md p-2 text-gray-400 hover:bg-gray-100 hover:text-blue-600 dark:hover:bg-gray-800"
                title="重试解析"
                @click="onRetry(doc)"
              >
                <RefreshCw class="size-4" />
              </button>
              <button
                class="rounded-md p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800"
                title="预览"
                @click="onPreview(doc)"
              >
                <Eye class="size-4" />
              </button>
              <button
                class="rounded-md p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800"
                title="下载"
                @click="onDownload(doc)"
              >
                <Download class="size-4" />
              </button>
              <button
                v-if="isParseDone(doc)"
                class="rounded-md p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800"
                title="查看分块"
                @click="navigate({ view: 'chunks', documentId: doc.id })"
              >
                <Layers class="size-4" />
              </button>
              <!-- 启停开关 -->
              <button
                v-if="isParseDone(doc)"
                class="relative h-5 w-9 shrink-0 rounded-full transition-colors"
                :class="doc.enabled ? 'bg-blue-500' : 'bg-gray-300 dark:bg-gray-600'"
                :title="doc.enabled ? '停用后不参与检索' : '启用后参与检索'"
                @click="toggleEnabled(doc)"
              >
                <span
                  class="absolute top-0.5 size-4 rounded-full bg-white shadow transition-all"
                  :class="doc.enabled ? 'left-[18px]' : 'left-0.5'"
                />
              </button>
              <button
                class="rounded-md p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
                title="删除"
                @click="requestDelete(doc)"
              >
                <Trash2 class="size-4" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="errorTooltip"
        :id="`document-error-${errorTooltip.documentId}`"
        role="tooltip"
        class="fixed z-[90] overflow-y-auto rounded-lg border border-red-200 bg-white px-3 py-2.5 shadow-lg dark:border-red-900 dark:bg-gray-900"
        :style="{
          left: errorTooltip.left,
          top: errorTooltip.top,
          width: errorTooltip.width,
          maxHeight: errorTooltip.maxHeight,
          transform: errorTooltip.transform,
        }"
        @pointerenter="clearErrorTooltipHideTimer"
        @pointerleave="scheduleErrorTooltipHide(errorTooltip.documentId)"
      >
        <div class="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-red-600 dark:text-red-400">
          <CircleAlert class="size-3.5 shrink-0" />
          完整错误信息
        </div>
        <p class="break-words text-xs leading-5 text-gray-700 dark:text-gray-200">
          {{ errorTooltip.message }}
        </p>
      </div>
    </Teleport>

    <Teleport to="body">
      <Transition name="modal">
        <div
          v-if="selected.size"
          class="pointer-events-none fixed inset-x-0 bottom-6 z-[60] flex justify-center px-4"
        >
          <div
            class="pointer-events-auto flex items-center gap-3 rounded-full border border-gray-200 bg-white/95 px-4 py-2.5 shadow-xl backdrop-blur dark:border-gray-700 dark:bg-gray-900/95"
          >
            <span class="px-1 text-sm text-gray-600 dark:text-gray-300">已选 {{ selected.size }} 项</span>
            <button
              class="flex items-center gap-1.5 rounded-full bg-blue-50 px-3 py-1.5 text-sm text-blue-700 hover:bg-blue-100 dark:bg-blue-950 dark:text-blue-300 dark:hover:bg-blue-900"
              @click="onBatchEnabled(true)"
            >
              <Check class="size-4" />启用
            </button>
            <button
              class="flex items-center gap-1.5 rounded-full bg-gray-100 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
              @click="onBatchEnabled(false)"
            >
              <Power class="size-4" />停用
            </button>
            <button
              class="flex size-8 items-center justify-center rounded-full text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
              title="取消选择"
              @click="selected = new Set()"
            >
              <X class="size-4" />
            </button>
          </div>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <div
        v-if="preview || previewLoading"
        class="fixed inset-0 z-[100] flex items-center justify-center"
      >
        <div class="absolute inset-0 bg-black/50" @click="preview = null" />
        <div class="relative z-10 flex h-[80vh] w-[720px] max-w-[92vw] flex-col overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900">
          <div class="flex items-center justify-between border-b border-gray-200 px-5 py-3 dark:border-gray-800">
            <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
              {{ preview?.name || '预览' }}
            </h3>
            <button
              class="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
              @click="preview = null"
            >
              <X class="size-4" />
            </button>
          </div>
          <p v-if="previewLoading" class="py-16 text-center text-sm text-gray-400">加载中...</p>
          <div
            v-else-if="previewMarkdown"
            class="md-body min-h-0 flex-1 overflow-y-auto overscroll-contain p-5 text-sm leading-7 text-gray-800 dark:text-gray-100"
            v-html="previewMarkdown"
          />
          <div
            v-else-if="preview?.kind === 'text'"
            class="min-h-0 flex-1 overflow-y-auto overscroll-contain p-5"
          >
            <pre class="whitespace-pre-wrap break-words text-sm leading-6 text-gray-700 dark:text-gray-200">{{ preview.content }}</pre>
          </div>
          <iframe
            v-else-if="preview?.kind === 'pdf' && preview.url"
            :src="preview.url"
            title="PDF 预览"
            class="min-h-0 w-full flex-1 border-0"
          />
          <div
            v-else-if="preview?.kind === 'image' && preview.url"
            class="flex min-h-0 flex-1 items-center justify-center overflow-auto bg-gray-50 p-4 dark:bg-gray-950"
          >
            <img :src="preview.url" :alt="preview.name" class="max-h-full max-w-full object-contain" />
          </div>
        </div>
      </div>
    </Teleport>

    <ConfirmModal
      :open="deleteOpen"
      :title="deleteTitle"
      :message="deleteMessage"
      confirm-text="删除"
      danger
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </div>
</template>
