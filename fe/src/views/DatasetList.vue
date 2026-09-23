<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ChevronDown, Database, FileText, Layers, Pencil, Plus, Trash2, X } from 'lucide-vue-next'
import {
  createDataset,
  createEmbedModel,
  createVisionModel,
  deleteDataset,
  deleteEmbedModel,
  deleteVisionModel,
  listDatasets,
  listEmbedModels,
  listVisionModels,
  testEmbedModel,
  testVisionModel,
  updateDataset,
  formatRelativeTime,
  type ApiDataset,
  type ApiEmbedModel,
  type ApiVisionModel,
} from '@/services/api'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { useAppState } from '@/composables/useAppState'
import { useConfirmModal } from '@/composables/useConfirmModal'
import { useConnectionTest } from '@/composables/useConnectionTest'
import { useToast } from '@/composables/useToast'

const FIELD =
  'w-full rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm outline-none focus:border-gray-400 dark:border-gray-600 dark:bg-gray-800'
const FIELD_DISABLED =
  'w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-2.5 text-sm text-gray-500 dark:border-gray-600 dark:bg-gray-800'
const MENU_BTN =
  'flex w-full items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-left text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-800'
const MENU_PANEL =
  'absolute left-0 top-full z-30 mt-1 w-full rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-800'
const MENU_ITEM =
  'w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700'
const MENU_ITEM_ON = 'bg-gray-100 dark:bg-gray-700'

const STRATEGIES = [
  { value: 'general', label: '通用文档', description: '普通文本、Markdown 与常规办公文档' },
  { value: 'book', label: '书籍', description: '按章、节组织长篇书籍与教材' },
  { value: 'paper', label: '学术论文', description: '保留摘要、章节、表格与公式结构' },
  { value: 'resume', label: '简历', description: '按信息、经历、教育与技能组织' },
  { value: 'qa', label: '问答对', description: '一组问题与答案生成一个独立分块' },
] as const

const OFFICIAL_NAME = '服务端默认'
const OFFICIAL_ID = 0
const VISION_UNUSED_ID = -1

type ModelKind = 'embed' | 'vision'

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

const items = ref<ApiDataset[]>([])
const embedModels = ref<ApiEmbedModel[]>([])
const visionModels = ref<ApiVisionModel[]>([])
const loading = ref(true)

const embedName = ref('')
const embedModelId = ref('')
const embedApiUrl = ref('')
const embedApiKey = ref('')
const embedSaving = ref(false)
const embedFormOpen = ref(false)
const modelFormKind = ref<ModelKind>('embed')

const MODEL_PANELS: {
  kind: ModelKind
  title: string
  hint: string
  empty: string
}[] = [
  {
    kind: 'embed',
    title: '嵌入模型',
    hint: '登记后绑定到知识库，创建后不可更换。',
    empty: '还没有嵌入模型，请先添加后再建知识库。',
  },
  {
    kind: 'vision',
    title: '视觉模型',
    hint: '用于识别文档中的图片，生成文字描述后参与检索；可随时更换。',
    empty: '还没有视觉模型。',
  },
]

const {
  status: embedTestStatus,
  passed: embedTestPassed,
  invalidate: resetEmbedTest,
  fail: failEmbedTest,
  run: runEmbedTest,
} = useConnectionTest()

const editorOpen = ref(false)
const editingId = ref<number | null>(null)
const editorName = ref('')
const editorDesc = ref('')
const editorStrategy = ref('general')
const editorEmbedId = ref(0)
const boundEmbedName = ref('')
const editorVisionId = ref(0)
const saving = ref(false)
const strategyMenuOpen = ref(false)
const embedMenuOpen = ref(false)
const visionMenuOpen = ref(false)

function isOfficial(m: { id?: number; name: string }) {
  return m.id === OFFICIAL_ID || m.name === OFFICIAL_NAME
}

function officialVisionId(): number {
  if (visionModels.value.some((m) => isOfficial(m))) return OFFICIAL_ID
  return VISION_UNUSED_ID
}

function modelsOf(kind: ModelKind): { id: number; name: string; modelId: string }[] {
  if (kind === 'embed') return embedModels.value
  return visionModels.value
}

function closeEditorMenus() {
  strategyMenuOpen.value = false
  embedMenuOpen.value = false
  visionMenuOpen.value = false
}

const strategyLabel = computed(() => {
  const found = STRATEGIES.find((s) => s.value === editorStrategy.value)
  if (found) return found.label
  return STRATEGIES[0].label
})

function normalizeStrategy(value: string): string {
  if (value === 'recursive' || value === 'fixed' || !value) return 'general'
  return value
}

function strategyName(value: string): string {
  return STRATEGIES.find((item) => item.value === normalizeStrategy(value))?.label ?? '通用文档'
}

const selectedEmbedLabel = computed(() => {
  const found = embedModels.value.find((m) => m.id === editorEmbedId.value)
  if (found) return found.name
  return '请选择'
})

const selectedVisionLabel = computed(() => {
  if (editorVisionId.value < 0) return '不使用'
  const found = visionModels.value.find((m) => m.id === editorVisionId.value)
  if (found) return found.name
  return '不使用'
})

function pickStrategy(value: string): void {
  editorStrategy.value = value
  strategyMenuOpen.value = false
}

function pickEmbed(id: number) {
  editorEmbedId.value = id
  embedMenuOpen.value = false
}

function pickVision(id: number) {
  editorVisionId.value = id
  visionMenuOpen.value = false
}

function onDocumentClick(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  if (!target?.closest('[data-strategy-menu]')) strategyMenuOpen.value = false
  if (!target?.closest('[data-embed-menu]')) embedMenuOpen.value = false
  if (!target?.closest('[data-vision-menu]')) visionMenuOpen.value = false
}

async function reloadModels(): Promise<void> {
  const [em, vm] = await Promise.all([listEmbedModels(), listVisionModels()])
  embedModels.value = em
  visionModels.value = vm
}

async function reload() {
  loading.value = true
  try {
    const [ds, em, vm] = await Promise.all([listDatasets(), listEmbedModels(), listVisionModels()])
    items.value = ds
    embedModels.value = em
    visionModels.value = vm
  } catch (e) {
    toast.fromError(e, '加载知识库失败')
  } finally {
    loading.value = false
  }
}

function openEditor(ds: ApiDataset | null) {
  closeEditorMenus()
  if (!ds) {
    editingId.value = null
    editorName.value = ''
    editorDesc.value = ''
    editorStrategy.value = 'general'
    editorEmbedId.value = embedModels.value[0]?.id ?? OFFICIAL_ID
    editorVisionId.value = officialVisionId()
    boundEmbedName.value = ''
  } else {
    editingId.value = ds.id
    editorName.value = ds.name
    editorDesc.value = ds.description
    editorStrategy.value = normalizeStrategy(ds.chunkStrategy)
    editorEmbedId.value = ds.embedModelId
    editorVisionId.value = ds.visionModelId ?? VISION_UNUSED_ID
    boundEmbedName.value = ds.embedModel
  }
  editorOpen.value = true
}

async function saveEditor() {
  const name = editorName.value.trim()
  if (!name) {
    toast.error('请输入知识库名称')
    return
  }
  if (!editingId.value && !embedModels.value.some((m) => m.id === editorEmbedId.value)) {
    toast.error('请选择嵌入模型')
    return
  }
  await persistEditor()
}

async function persistEditor() {
  saving.value = true
  try {
    if (editingId.value) {
      await updateDataset(editingId.value, {
        name: editorName.value.trim(),
        description: editorDesc.value.trim(),
        visionModelId: editorVisionId.value,
      })
      toast.success('知识库已更新')
    } else {
      await createDataset({
        name: editorName.value.trim(),
        description: editorDesc.value.trim(),
        chunkStrategy: editorStrategy.value,
        embedModelId: editorEmbedId.value,
        visionModelId: editorVisionId.value,
      })
      toast.success('知识库已创建')
    }
    editorOpen.value = false
    await reload()
  } catch (e) {
    toast.fromError(e, '保存失败')
  } finally {
    saving.value = false
  }
}

function modelFormPayload(): { modelId: string; apiUrl: string; apiKey: string } {
  return {
    modelId: embedModelId.value.trim(),
    apiUrl: embedApiUrl.value.trim(),
    apiKey: embedApiKey.value.trim(),
  }
}

function openModelForm(kind: ModelKind): void {
  modelFormKind.value = kind
  embedName.value = ''
  embedModelId.value = ''
  embedApiUrl.value = ''
  embedApiKey.value = ''
  resetEmbedTest()
  embedFormOpen.value = true
}

const modelFormTitle = computed(() => {
  if (modelFormKind.value === 'vision') return '添加视觉模型'
  return '添加嵌入模型'
})

const modelIdPlaceholder = computed(() => {
  if (modelFormKind.value === 'vision') return '例如 google/gemma-4-31b-it:free'
  return '例如 BAAI/bge-m3'
})

const editorBindHint = computed(() => {
  if (editingId.value) return '解析模板与嵌入模型创建后不可更换'
  return '知识库中的全部文档将使用所选解析模板'
})

async function testEmbedConnection() {
  if (!embedModelId.value.trim() || !embedApiUrl.value.trim()) {
    failEmbedTest('请先填写模型 ID 与接口前缀')
    return
  }
  const payload = modelFormPayload()
  if (modelFormKind.value === 'vision') {
    await runEmbedTest(() => testVisionModel(payload))
    return
  }
  await runEmbedTest(() => testEmbedModel(payload))
}

async function saveEmbedModel() {
  if (!embedTestPassed.value) {
    failEmbedTest('请先测试连接成功后再保存')
    return
  }
  embedSaving.value = true
  const payload = {
    name: embedName.value.trim() || embedModelId.value.trim(),
    ...modelFormPayload(),
  }
  try {
    if (modelFormKind.value === 'vision') {
      await createVisionModel(payload)
    } else {
      await createEmbedModel(payload)
    }
    embedFormOpen.value = false
    await reloadModels()
    toast.success(modelFormKind.value === 'vision' ? '视觉模型已添加' : '嵌入模型已添加')
  } catch (e) {
    toast.fromError(e, '保存失败')
  } finally {
    embedSaving.value = false
  }
}

function requestDeleteModel(kind: ModelKind, m: { id: number; name: string }): void {
  const isVision = kind === 'vision'
  askDelete({
    title: isVision ? '删除视觉模型' : '删除嵌入模型',
    message: `确定删除「${m.name}」吗？此操作不可恢复。`,
    onConfirm: async () => {
      try {
        if (isVision) {
          await deleteVisionModel(m.id)
        } else {
          await deleteEmbedModel(m.id)
        }
        await reloadModels()
        toast.success(isVision ? '视觉模型已删除' : '嵌入模型已删除')
      } catch (e) {
        toast.fromError(e, isVision ? '删除视觉模型失败' : '删除嵌入模型失败')
      }
    },
  })
}

function requestDeleteDataset(ds: ApiDataset): void {
  askDelete({
    title: '删除知识库',
    message: `确定删除「${ds.name}」吗？其中的文档与分块将一并删除，且不可恢复。`,
    onConfirm: async () => {
      try {
        await deleteDataset(ds.id)
        await reload()
        toast.success('知识库已删除')
      } catch (e) {
        toast.fromError(e, '删除失败')
      }
    },
  })
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  void reload()
})
onUnmounted(() => document.removeEventListener('click', onDocumentClick))
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="mx-auto max-w-5xl px-8 py-8">
      <div class="mb-6 flex items-center justify-between">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">知识库</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            上传文档构建知识库，基于向量检索智能问答
          </p>
        </div>
        <button
          class="flex items-center gap-1.5 rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-gray-800 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
          @click="openEditor(null)"
        >
          <Plus class="size-4" />
          新建知识库
        </button>
      </div>

      <div v-if="!loading" class="mb-6 grid gap-4 sm:grid-cols-2">
        <div
          v-for="panel in MODEL_PANELS"
          :key="panel.kind"
          class="rounded-xl border border-gray-200 p-4 dark:border-gray-800"
        >
          <div class="mb-3 flex items-center justify-between">
            <div>
              <h2 class="text-sm font-medium text-gray-900 dark:text-white">{{ panel.title }}</h2>
              <p class="mt-0.5 text-xs text-gray-500">{{ panel.hint }}</p>
            </div>
            <button
              class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300"
              @click="openModelForm(panel.kind)"
            >
              添加模型
            </button>
          </div>
          <p v-if="!modelsOf(panel.kind).length" class="text-xs text-gray-400">{{ panel.empty }}</p>
          <div v-else class="flex flex-wrap gap-2">
            <div
              v-for="m in modelsOf(panel.kind)"
              :key="m.id"
              class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-1.5 text-xs dark:border-gray-700"
            >
              <span class="font-medium text-gray-800 dark:text-gray-200">{{ m.name }}</span>
              <span class="text-gray-400">{{ m.modelId }}</span>
              <button
                v-if="!isOfficial(m)"
                class="rounded p-0.5 text-gray-400 transition-colors hover:text-red-500"
                title="删除"
                @click.stop="requestDeleteModel(panel.kind, m)"
              >
                <Trash2 class="size-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="loading" class="py-20 text-center text-sm text-gray-400">加载中...</div>

      <div
        v-else-if="!items.length"
        class="flex flex-col items-center gap-3 rounded-xl border border-dashed border-gray-300 py-20 text-gray-400 dark:border-gray-700"
      >
        <Database class="size-10" />
        <p class="text-sm">还没有知识库，点击右上角新建</p>
      </div>

      <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div
          v-for="ds in items"
          :key="ds.id"
          class="group flex min-w-0 cursor-pointer flex-col rounded-xl border border-gray-200 bg-white p-5 transition-shadow hover:shadow-md dark:border-gray-800 dark:bg-gray-900"
          @click="navigate({ view: 'documents', datasetId: ds.id, datasetName: ds.name })"
        >
          <div class="mb-3 flex items-start justify-between">
            <div class="flex size-10 items-center justify-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-400">
              <Database class="size-5" />
            </div>
            <div
              class="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100"
              @click.stop
            >
              <button
                class="rounded-md p-1.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
                title="重命名"
                @click="openEditor(ds)"
              >
                <Pencil class="size-4" />
              </button>
              <button
                class="rounded-md p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
                title="删除"
                @click="requestDeleteDataset(ds)"
              >
                <Trash2 class="size-4" />
              </button>
            </div>
          </div>

          <h3 class="mb-1 truncate text-sm font-semibold text-gray-900 dark:text-white">
            {{ ds.name }}
          </h3>
          <p class="mb-4 line-clamp-2 min-h-5 text-xs text-gray-500 dark:text-gray-400">
            {{ ds.description || '暂无描述' }}
          </p>

          <div class="mt-auto space-y-1.5 text-xs text-gray-400">
            <div class="flex items-center gap-3 whitespace-nowrap">
              <span class="flex items-center gap-1">
                <FileText class="size-3.5 shrink-0" />{{ ds.documentCount ?? 0 }} 文档
              </span>
              <span class="flex items-center gap-1">
                <Layers class="size-3.5 shrink-0" />{{ ds.chunkCount ?? 0 }} 分块
              </span>
              <span class="truncate">{{ strategyName(ds.chunkStrategy) }}</span>
            </div>
            <div class="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-3">
              <div class="flex min-w-0 items-center gap-1.5">
                <span class="shrink-0 text-gray-500 dark:text-gray-300">嵌入</span>
                <span class="min-w-0 truncate" :title="ds.embedModel || '未绑定'">
                  {{ ds.embedModel || '未绑定' }}
                </span>
              </div>
              <div class="flex min-w-0 items-center gap-1.5">
                <span class="shrink-0 text-gray-500 dark:text-gray-300">视觉</span>
                <span class="min-w-0 truncate" :title="ds.visionModel || '未使用'">
                  {{ ds.visionModel || '未使用' }}
                </span>
              </div>
              <span class="shrink-0">{{ formatRelativeTime(ds.updatedAt) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="modal">
        <div v-if="editorOpen" class="fixed inset-0 z-[100] flex items-center justify-center">
          <div class="absolute inset-0 z-0 cursor-pointer bg-black/50" @click="editorOpen = false" />
          <div class="relative z-10 w-[520px] overflow-visible rounded-xl bg-white shadow-2xl dark:bg-gray-900">
            <div class="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-800">
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ editingId ? '编辑知识库' : '新建知识库' }}
              </h3>
              <button
                class="rounded-lg p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
                aria-label="关闭"
                @click="editorOpen = false"
              >
                <X class="size-5" />
              </button>
            </div>
            <div class="space-y-4 p-6">
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">名称</label>
                <input
                  v-model="editorName"
                  type="text"
                  maxlength="128"
                  :class="FIELD"
                  placeholder="例如：产品手册"
                  @keydown.enter="saveEditor"
                />
              </div>
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">描述</label>
                <textarea
                  v-model="editorDesc"
                  rows="3"
                  :class="['resize-none', FIELD]"
                  placeholder="可选"
                />
              </div>
              <div class="flex gap-3">
                <div class="min-w-0 flex-1">
                  <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">解析模板</label>
                  <div v-if="!editingId" data-strategy-menu class="relative">
                    <button type="button" :class="MENU_BTN" @click.stop="strategyMenuOpen = !strategyMenuOpen">
                      <span class="min-w-0 flex-1 truncate">{{ strategyLabel }}</span>
                      <ChevronDown class="size-4 shrink-0 text-gray-400" />
                    </button>
                    <div v-if="strategyMenuOpen" :class="MENU_PANEL">
                      <button
                        v-for="item in STRATEGIES"
                        :key="item.value"
                        type="button"
                        :class="[MENU_ITEM, editorStrategy === item.value ? MENU_ITEM_ON : '']"
                        @click="pickStrategy(item.value)"
                      >
                        <span class="block font-medium">{{ item.label }}</span>
                        <span class="mt-0.5 block text-xs text-gray-400">{{ item.description }}</span>
                      </button>
                    </div>
                  </div>
                  <input
                    v-else
                    :value="strategyLabel"
                    disabled
                    :class="FIELD_DISABLED"
                  />
                </div>
                <div class="min-w-0 flex-1">
                  <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">嵌入模型</label>
                  <div v-if="!editingId" data-embed-menu class="relative">
                    <button type="button" :class="MENU_BTN" @click.stop="embedMenuOpen = !embedMenuOpen">
                      <span class="min-w-0 flex-1 truncate">{{ selectedEmbedLabel }}</span>
                      <ChevronDown class="size-4 shrink-0 text-gray-400" />
                    </button>
                    <div v-if="embedMenuOpen" :class="[MENU_PANEL, 'max-h-48 overflow-y-auto']">
                      <button
                        v-for="m in embedModels"
                        :key="m.id"
                        type="button"
                        :class="[MENU_ITEM, 'truncate', editorEmbedId === m.id ? MENU_ITEM_ON : '']"
                        @click="pickEmbed(m.id)"
                      >
                        {{ m.name }}
                      </button>
                      <p v-if="!embedModels.length" class="px-3 py-2 text-sm text-gray-400">请先添加嵌入模型</p>
                    </div>
                  </div>
                  <input
                    v-else
                    :value="boundEmbedName"
                    disabled
                    :class="FIELD_DISABLED"
                  />
                </div>
              </div>
              <p class="text-xs text-gray-400">{{ editorBindHint }}</p>
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">视觉模型</label>
                <div data-vision-menu class="relative">
                  <button type="button" :class="MENU_BTN" @click.stop="visionMenuOpen = !visionMenuOpen">
                    <span class="min-w-0 flex-1 truncate">{{ selectedVisionLabel }}</span>
                    <ChevronDown class="size-4 shrink-0 text-gray-400" />
                  </button>
                  <div v-if="visionMenuOpen" :class="[MENU_PANEL, 'max-h-48 overflow-y-auto']">
                    <button
                      type="button"
                      :class="[MENU_ITEM, editorVisionId === VISION_UNUSED_ID ? MENU_ITEM_ON : '']"
                      @click="pickVision(VISION_UNUSED_ID)"
                    >
                      不使用
                    </button>
                    <button
                      v-for="m in visionModels"
                      :key="m.id"
                      type="button"
                      :class="[MENU_ITEM, 'truncate', editorVisionId === m.id ? MENU_ITEM_ON : '']"
                      @click="pickVision(m.id)"
                    >
                      {{ m.name }}
                    </button>
                  </div>
                </div>
                <p class="mt-1.5 text-xs text-gray-400">仅用于识别图片内容，可随时更换</p>
              </div>
              <button
                class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-gray-800 disabled:opacity-50 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-200"
                :disabled="saving"
                @click="saveEditor"
              >
                {{ saving ? '保存中...' : '保存' }}
              </button>
            </div>
          </div>
        </div>
      </Transition>

      <Transition name="modal">
        <div v-if="embedFormOpen" class="fixed inset-0 z-[110] flex items-center justify-center">
          <div class="absolute inset-0 z-0 cursor-pointer bg-black/50" @click="embedFormOpen = false" />
          <div class="relative z-10 w-[440px] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900">
            <div class="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-800">
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ modelFormTitle }}</h3>
              <button
                class="rounded-lg p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800"
                aria-label="关闭"
                @click="embedFormOpen = false"
              >
                <X class="size-5" />
              </button>
            </div>
            <div class="space-y-3 p-6">
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">名称</label>
                <input v-model="embedName" :class="FIELD" placeholder="展示名" />
              </div>
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">模型 ID</label>
                <input
                  v-model="embedModelId"
                  :class="FIELD"
                  :placeholder="modelIdPlaceholder"
                />
              </div>
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">接口前缀</label>
                <input v-model="embedApiUrl" :class="FIELD" placeholder="例如 https://api.example.com/v1" />
              </div>
              <div>
                <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">API Key</label>
                <input v-model="embedApiKey" type="password" :class="FIELD" placeholder="sk-…" />
              </div>
              <div class="flex items-center gap-2 pt-1">
                <div class="flex-1" />
                <button
                  type="button"
                  class="rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-gray-600"
                  :disabled="embedTestStatus === 'testing'"
                  @click="testEmbedConnection"
                >
                  {{ embedTestStatus === 'testing' ? '测试中...' : '测试连通性' }}
                </button>
                <button
                  type="button"
                  class="rounded-lg bg-gray-900 px-3 py-2 text-sm text-white disabled:opacity-50 dark:bg-white dark:text-gray-900"
                  :disabled="!embedTestPassed || embedSaving"
                  @click="saveEmbedModel"
                >
                  {{ embedSaving ? '保存中...' : '保存' }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </Transition>
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
