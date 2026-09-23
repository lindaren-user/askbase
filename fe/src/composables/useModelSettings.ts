import { computed, ref, watch } from 'vue'
import type { ChatModelChoice } from '@/services/api'

/** 自定义对话模型（BYOK）：仅存本机 localStorage */
export interface CustomModel {
  id: string
  name: string
  modelId: string
  apiUrl: string
  apiKey: string
}

export interface ModelSelection {
  source: 'official' | 'custom'
  customId: string
}

const STORAGE_KEY = 'askbase.model-settings.v1'

interface Persisted {
  customModels: CustomModel[]
  selection: ModelSelection
}

const customModels = ref<CustomModel[]>([])
const selection = ref<ModelSelection>({ source: 'official', customId: '' })
let loaded = false

function persist() {
  localStorage.setItem(
    STORAGE_KEY,
    JSON.stringify({
      customModels: customModels.value,
      selection: selection.value,
    } satisfies Persisted),
  )
}

function normalizeSelection(
  raw: Partial<ModelSelection> | undefined,
  list: Array<{ id: string }>,
): ModelSelection {
  const next: ModelSelection = {
    source: raw?.source === 'custom' ? 'custom' : 'official',
    customId: String(raw?.customId ?? ''),
  }
  if (next.source === 'custom' && !list.some((m) => m.id === next.customId)) {
    return { source: 'official', customId: '' }
  }
  return next
}

function load() {
  if (loaded) return
  loaded = true
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return
    const data = JSON.parse(raw) as Partial<Persisted>
    if (Array.isArray(data.customModels)) customModels.value = data.customModels
    selection.value = normalizeSelection(data.selection, customModels.value)
  } catch {
    // 本地数据损坏时忽略
  }
}

export function useModelSettings() {
  load()

  const activeCustom = computed(
    () => customModels.value.find((m) => m.id === selection.value.customId) ?? null,
  )

  const chatModelChoice = computed<ChatModelChoice | undefined>(() => {
    if (selection.value.source !== 'custom' || !activeCustom.value) return undefined
    return {
      source: 'custom',
      modelId: activeCustom.value.modelId,
      apiUrl: activeCustom.value.apiUrl,
      apiKey: activeCustom.value.apiKey,
    }
  })

  const activeModelLabel = computed(() => {
    if (selection.value.source === 'custom' && activeCustom.value) {
      return activeCustom.value.name || activeCustom.value.modelId
    }
    return ''
  })

  function selectOfficial() {
    selection.value = { source: 'official', customId: '' }
  }

  function selectCustom(id: string) {
    selection.value = { source: 'custom', customId: id }
  }

  function saveCustom(model: Omit<CustomModel, 'id'>) {
    const item: CustomModel = { ...model, id: `m-${Date.now().toString(36)}` }
    customModels.value = [...customModels.value, item]
    selection.value = { source: 'custom', customId: item.id }
  }

  function deleteCustom(id: string) {
    customModels.value = customModels.value.filter((m) => m.id !== id)
    if (selection.value.customId === id) {
      selection.value = { source: 'official', customId: '' }
    }
  }

  watch([customModels, selection], persist, { deep: true })

  return {
    customModels,
    selection,
    activeCustom,
    activeModelLabel,
    chatModelChoice,
    selectOfficial,
    selectCustom,
    saveCustom,
    deleteCustom,
  }
}
