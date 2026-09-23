<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'
import { Archive, Check, ChevronDown, MessageSquare, Pencil, Plus, TriangleAlert } from 'lucide-vue-next'
import type { ApiDataset, ApiSession } from '@/services/api'
import { formatRelativeTime } from '@/services/api'
import { usePendingConfirm } from '@/composables/usePendingConfirm'

const props = defineProps<{
  datasets: ApiDataset[]
  selectedDatasetId: number
  sessions: ApiSession[]
  activeSessionId: number
}>()

const emit = defineEmits<{
  'select-dataset': [datasetId: number]
  'select-session': [sessionId: number]
  'create-session': []
  'rename-session': [sessionId: number, title: string]
  'archive-session': [sessionId: number]
}>()

const datasetMenuOpen = ref(false)
const datasetMenuRef = ref<HTMLElement | null>(null)
const sidebarRef = ref<HTMLElement | null>(null)
const sidebarWidth = ref<number | null>(null)
const editingId = ref<number | null>(null)
const editDraft = ref('')

const MIN_SIDEBAR_WIDTH = 180
const MAX_SIDEBAR_WIDTH = 420
const MIN_CHAT_WIDTH = 320

let resizeOrigin: { x: number; width: number } | null = null
let previousCursor = ''
let previousUserSelect = ''

const { pending, armOrConfirm } = usePendingConfirm<number>('[data-session-item]')

function selectedDataset() {
  return props.datasets.find((d) => d.id === props.selectedDatasetId) ?? null
}

function pickDataset(id: number) {
  emit('select-dataset', id)
  datasetMenuOpen.value = false
}

async function startRename(session: ApiSession) {
  editingId.value = session.id
  editDraft.value = session.title
  await nextTick()
  const input = document.querySelector<HTMLInputElement>('[data-rename-input]')
  input?.focus()
  input?.select()
}

function commitRename() {
  const id = editingId.value
  const title = editDraft.value.trim()
  editingId.value = null
  editDraft.value = ''
  if (!id || !title) return
  emit('rename-session', id, title)
}

function onRenameKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    commitRename()
  }
  if (e.key === 'Escape') {
    editingId.value = null
  }
}

function onDocumentClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!datasetMenuRef.value?.contains(target)) datasetMenuOpen.value = false
}

function constrainedWidth(width: number): number {
  const containerWidth = sidebarRef.value?.parentElement?.clientWidth ?? window.innerWidth
  const availableWidth = Math.max(MIN_SIDEBAR_WIDTH, containerWidth - MIN_CHAT_WIDTH)
  const maximumWidth = Math.min(MAX_SIDEBAR_WIDTH, availableWidth)
  return Math.min(Math.max(width, MIN_SIDEBAR_WIDTH), maximumWidth)
}

function stopResize() {
  if (!resizeOrigin) return
  resizeOrigin = null
  window.removeEventListener('pointermove', resizeSidebar)
  window.removeEventListener('pointerup', stopResize)
  window.removeEventListener('pointercancel', stopResize)
  document.body.style.cursor = previousCursor
  document.body.style.userSelect = previousUserSelect
}

function resizeSidebar(event: PointerEvent) {
  if (!resizeOrigin) return
  sidebarWidth.value = constrainedWidth(resizeOrigin.width + event.clientX - resizeOrigin.x)
}

function startResize(event: PointerEvent) {
  if (!sidebarRef.value || resizeOrigin) return
  event.preventDefault()
  resizeOrigin = {
    x: event.clientX,
    width: sidebarRef.value.getBoundingClientRect().width,
  }
  previousCursor = document.body.style.cursor
  previousUserSelect = document.body.style.userSelect
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
  window.addEventListener('pointermove', resizeSidebar)
  window.addEventListener('pointerup', stopResize)
  window.addEventListener('pointercancel', stopResize)
}

function resizeWithKeyboard(event: KeyboardEvent) {
  if (!sidebarRef.value) return
  const currentWidth = sidebarRef.value.getBoundingClientRect().width
  let nextWidth: number
  switch (event.key) {
    case 'Home':
      nextWidth = MIN_SIDEBAR_WIDTH
      break
    case 'End':
      nextWidth = MAX_SIDEBAR_WIDTH
      break
    case 'ArrowLeft':
      nextWidth = currentWidth - 12
      break
    case 'ArrowRight':
      nextWidth = currentWidth + 12
      break
    default:
      return
  }
  event.preventDefault()
  sidebarWidth.value = constrainedWidth(nextWidth)
}

function resetSidebarWidth() {
  sidebarWidth.value = null
}

onBeforeUnmount(stopResize)
</script>

<template>
  <aside
    ref="sidebarRef"
    class="relative flex w-[clamp(8.5rem,28%,16rem)] min-w-0 shrink-0 flex-col overflow-hidden border-r border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-950"
    :style="sidebarWidth === null ? undefined : { width: `${sidebarWidth}px` }"
    @click="onDocumentClick"
  >
    <!-- 知识库选择器 -->
    <div class="border-b border-gray-100 p-3 dark:border-gray-800">
      <div ref="datasetMenuRef" class="relative">
        <button
          type="button"
          class="flex w-full items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-800"
          @click.stop="datasetMenuOpen = !datasetMenuOpen"
        >
          <span class="min-w-0 flex-1 truncate text-left font-medium text-gray-900 dark:text-white">
            {{ selectedDataset()?.name || '选择知识库' }}
          </span>
          <ChevronDown class="size-4 shrink-0 text-gray-400" />
        </button>
        <div
          v-if="datasetMenuOpen"
          class="absolute left-0 top-full z-30 mt-1 w-full rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-800"
        >
          <button
            v-for="ds in datasets"
            :key="ds.id"
            type="button"
            class="flex w-full items-center justify-between px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
            @click="pickDataset(ds.id)"
          >
            <span class="truncate">{{ ds.name }}</span>
            <Check v-if="ds.id === selectedDatasetId" class="size-4 shrink-0 text-blue-600" />
          </button>
          <p v-if="!datasets.length" class="px-3 py-2 text-xs text-gray-400">
            请先在知识库页创建
          </p>
        </div>
      </div>

      <button
        type="button"
        class="mt-2 flex w-full items-center justify-center gap-1.5 rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-gray-800 disabled:opacity-40 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
        :disabled="!selectedDatasetId"
        @click="emit('create-session')"
      >
        <Plus class="size-4" />新建会话
      </button>
    </div>

    <!-- 会话列表 -->
    <div class="min-h-0 flex-1 overflow-y-auto p-2">
      <p v-if="!sessions.length" class="px-3 py-8 text-center text-xs text-gray-400">
        暂无会话，发送问题即自动创建
      </p>
      <div
        v-for="session in sessions"
        :key="session.id"
        data-session-item
        class="group mb-0.5 flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 transition-colors"
        :class="
          session.id === activeSessionId
            ? 'bg-gray-100 dark:bg-gray-800'
            : 'hover:bg-gray-50 dark:hover:bg-gray-800/60'
        "
        @click="emit('select-session', session.id)"
      >
        <MessageSquare class="size-4 shrink-0 text-gray-400" />
        <div class="min-w-0 flex-1">
          <input
            v-if="editingId === session.id"
            v-model="editDraft"
            data-rename-input
            class="w-full rounded border border-blue-400 bg-white px-1 py-0.5 text-sm outline-none dark:bg-gray-900"
            @keydown="onRenameKeydown"
            @blur="commitRename"
            @click.stop
          />
          <template v-else>
            <p class="flex items-center gap-1 truncate text-sm text-gray-800 dark:text-gray-100">
              <TriangleAlert
                v-if="session.datasetDeleted"
                class="size-3.5 shrink-0 text-amber-500"
                title="所属知识库已删除"
              />
              <span class="truncate">{{ session.title || '新会话' }}</span>
            </p>
            <p class="text-[11px] text-gray-400">{{ formatRelativeTime(session.updatedAt) }}</p>
          </template>
        </div>
        <div class="hidden shrink-0 items-center group-hover:flex" @click.stop>
          <button
            class="rounded p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            title="重命名"
            @click="startRename(session)"
          >
            <Pencil class="size-3.5" />
          </button>
          <button
            class="rounded p-1 transition-colors"
            :class="pending === session.id ? 'bg-red-50 text-red-600 dark:bg-red-950' : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'"
            :title="pending === session.id ? '再点一次确认归档' : '归档'"
            @click="armOrConfirm(session.id, () => emit('archive-session', session.id))"
          >
            <Archive class="size-3.5" />
          </button>
        </div>
      </div>
    </div>

    <div
      role="separator"
      aria-label="调整会话列表宽度"
      aria-orientation="vertical"
      :aria-valuemin="MIN_SIDEBAR_WIDTH"
      :aria-valuemax="MAX_SIDEBAR_WIDTH"
      :aria-valuenow="sidebarWidth ?? undefined"
      tabindex="0"
      class="group/resize absolute inset-y-0 right-0 z-20 w-2 cursor-col-resize touch-none outline-none"
      title="拖动调整宽度，双击恢复默认"
      @pointerdown="startResize"
      @keydown="resizeWithKeyboard"
      @dblclick="resetSidebarWidth"
    >
      <span
        class="absolute inset-y-0 right-0 w-0.5 bg-transparent transition-colors group-hover/resize:bg-blue-500 group-focus/resize:bg-blue-500"
      />
    </div>
  </aside>
</template>
