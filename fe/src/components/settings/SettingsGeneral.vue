<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Check, Trash2 } from 'lucide-vue-next'
import ConfirmModal from '@/components/ConfirmModal.vue'
import { testCustomModel } from '@/services/api'
import { useConnectionTest } from '@/composables/useConnectionTest'
import type { CustomModel, ModelSelection } from '@/composables/useModelSettings'

const props = defineProps<{
  officialName: string
  customModels: CustomModel[]
  selection: ModelSelection
}>()

const emit = defineEmits<{
  'select-official': []
  'select-custom': [id: string]
  'save-custom': [model: Omit<CustomModel, 'id'>]
  'delete-custom': [id: string]
}>()

/** 厂商预设：仅作接口前缀快捷填充 */
const PROVIDERS = [
  { label: 'DeepSeek', apiUrl: 'https://api.deepseek.com' },
  { label: '豆包 Ark', apiUrl: 'https://ark.cn-beijing.volces.com/api/v3' },
  { label: '通义千问', apiUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
  { label: 'OpenAI', apiUrl: 'https://api.openai.com/v1' },
] as const

const customDraftOpen = ref(false)
const draftName = ref('')
const draftModelId = ref('')
const draftApiUrl = ref<string>(PROVIDERS[0].apiUrl)
const draftApiKey = ref('')
const deleteDialogOpen = ref(false)

const {
  status: testStatus,
  passed: hasPassedTest,
  invalidate: resetTest,
  fail: failTest,
  succeed: succeedTest,
  resetSoon: resetTestSoon,
  run: runChatTest,
} = useConnectionTest()

const selectedMode = computed(() => {
  if (customDraftOpen.value || props.selection.source === 'custom') return 'custom'
  return 'official'
})

const selectedCustom = computed(
  () => props.customModels.find((m) => m.id === props.selection.customId) ?? null,
)

function selectOfficial() {
  customDraftOpen.value = false
  emit('select-official')
}

function startDraft() {
  draftName.value = ''
  draftModelId.value = ''
  draftApiUrl.value = PROVIDERS[0].apiUrl
  draftApiKey.value = ''
  resetTest()
  customDraftOpen.value = true
}

function enterCustomMode() {
  if (props.customModels.length > 0) {
    customDraftOpen.value = false
    emit('select-custom', props.customModels[0].id)
    return
  }
  startDraft()
}

function pickCustom(id: string) {
  customDraftOpen.value = false
  emit('select-custom', id)
}

watch([draftModelId, draftApiUrl, draftApiKey], () => {
  if (customDraftOpen.value) resetTest()
})

async function testConnection() {
  if (!draftModelId.value.trim() || !draftApiUrl.value.trim() || !draftApiKey.value.trim()) {
    failTest('请先填写模型 ID、接口前缀和 API Key')
    resetTestSoon()
    return
  }
  await runChatTest(() =>
    testCustomModel({
      modelId: draftModelId.value.trim(),
      apiUrl: draftApiUrl.value.trim(),
      apiKey: draftApiKey.value.trim(),
    }),
  )
  resetTestSoon()
}

function save() {
  if (!hasPassedTest.value) {
    failTest('请先测试连接成功后再保存')
    return
  }
  emit('save-custom', {
    name: draftName.value.trim() || draftModelId.value.trim(),
    modelId: draftModelId.value.trim(),
    apiUrl: draftApiUrl.value.trim(),
    apiKey: draftApiKey.value.trim(),
  })
  customDraftOpen.value = false
  succeedTest('已保存并启用')
  resetTestSoon()
}

function confirmDelete() {
  if (props.selection.customId) emit('delete-custom', props.selection.customId)
  deleteDialogOpen.value = false
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-3">
      <h4 class="text-sm font-medium text-gray-900 dark:text-white">问答模型</h4>
      <div class="flex gap-1">
        <button
          type="button"
          class="rounded-md border px-2.5 py-1 text-xs transition-colors"
          :class="
            selectedMode === 'official'
              ? 'border-gray-900 bg-white text-gray-900 dark:border-white dark:bg-gray-800 dark:text-white'
              : 'border-gray-200 text-gray-500 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400'
          "
          @click="selectOfficial"
        >
          默认
          <Check v-if="selectedMode === 'official'" class="ml-1 inline size-3" />
        </button>
        <button
          type="button"
          class="rounded-md border px-2.5 py-1 text-xs transition-colors"
          :class="
            selectedMode === 'custom'
              ? 'border-gray-900 bg-white text-gray-900 dark:border-white dark:bg-gray-800 dark:text-white'
              : 'border-gray-200 text-gray-500 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400'
          "
          @click="enterCustomMode"
        >
          自定义
          <Check v-if="selectedMode === 'custom'" class="ml-1 inline size-3" />
        </button>
      </div>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">仅用于生成回答；检索嵌入由知识库绑定的模型完成。</p>

    <div
      v-if="selectedMode === 'custom'"
      class="space-y-3 rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-gray-700 dark:bg-gray-800/50"
    >
      <div v-if="customModels.length > 0" class="flex flex-wrap gap-1.5">
        <button
          v-for="model in customModels"
          :key="model.id"
          type="button"
          class="rounded-md border px-2 py-1 text-xs transition-colors"
          :class="
            selection.customId === model.id && !customDraftOpen
              ? 'border-gray-900 bg-white text-gray-900 dark:border-white dark:bg-gray-800 dark:text-white'
              : 'border-gray-200 bg-white text-gray-600 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400'
          "
          @click="pickCustom(model.id)"
        >
          {{ model.name }}
        </button>
        <button
          type="button"
          class="rounded-md border border-dashed border-gray-300 px-2 py-1 text-xs text-gray-500 dark:border-gray-600"
          @click="startDraft"
        >
          新增
        </button>
      </div>

      <template v-if="customDraftOpen">
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-xs text-gray-500">名称</span>
          <input
            v-model="draftName"
            type="text"
            placeholder="仅方便自己查看"
            class="flex-1 rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm outline-none dark:border-gray-600 dark:bg-gray-800"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-xs text-gray-500">厂商</span>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="provider in PROVIDERS"
              :key="provider.label"
              type="button"
              class="rounded-full border px-2 py-0.5 text-xs"
              :class="
                draftApiUrl === provider.apiUrl
                  ? 'border-gray-900 text-gray-900 dark:border-white dark:text-white'
                  : 'border-gray-200 text-gray-500 dark:border-gray-700 dark:text-gray-400'
              "
              @click="draftApiUrl = provider.apiUrl"
            >
              {{ provider.label }}
            </button>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-xs text-gray-500">模型 ID</span>
          <input
            v-model="draftModelId"
            type="text"
            placeholder="例如 deepseek-chat"
            class="flex-1 rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm outline-none dark:border-gray-600 dark:bg-gray-800"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-xs text-gray-500">接口前缀</span>
          <input
            v-model="draftApiUrl"
            type="text"
            class="flex-1 rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm outline-none dark:border-gray-600 dark:bg-gray-800"
          />
        </div>
        <div class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-xs text-gray-500">API Key</span>
          <input
            v-model="draftApiKey"
            type="password"
            placeholder="sk-…"
            class="flex-1 rounded-md border border-gray-200 bg-white px-2 py-1.5 text-sm outline-none dark:border-gray-600 dark:bg-gray-800"
          />
        </div>
      </template>

      <template v-else-if="selectedCustom">
        <p class="text-xs text-gray-500">{{ selectedCustom.modelId }} · {{ selectedCustom.apiUrl }}</p>
      </template>

      <div class="flex items-center justify-end gap-2">
        <button
          v-if="customDraftOpen"
          type="button"
          class="rounded-md border border-gray-200 px-3 py-1 text-xs dark:border-gray-600"
          :disabled="testStatus === 'testing'"
          @click="testConnection"
        >
          {{ testStatus === 'testing' ? '测试中...' : '测试连接' }}
        </button>
        <button
          v-if="customDraftOpen"
          type="button"
          class="rounded-md bg-gray-900 px-3 py-1 text-xs text-white disabled:opacity-50 dark:bg-white dark:text-gray-900"
          :disabled="!hasPassedTest"
          @click="save"
        >
          保存并启用
        </button>
        <button
          v-if="!customDraftOpen && selection.source === 'custom'"
          type="button"
          class="rounded-md border border-red-200 px-3 py-1 text-xs text-red-600"
          @click="deleteDialogOpen = true"
        >
          <span class="inline-flex items-center gap-1">
            <Trash2 class="size-3" />
            删除
          </span>
        </button>
      </div>
    </div>

    <ConfirmModal
      :open="deleteDialogOpen"
      title="删除自定义模型"
      message="删除后需重新填写，且只从本机移除。"
      confirm-text="删除"
      danger
      @confirm="confirmDelete"
      @cancel="deleteDialogOpen = false"
    />
  </div>
</template>
