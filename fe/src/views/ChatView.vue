<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import SessionSidebar from '@/components/SessionSidebar.vue'
import ChatPanel, { type PanelMessage } from '@/components/ChatPanel.vue'
import {
  createSession,
  listDatasets,
  listMessages,
  listSessions,
  streamChat,
  updateSession,
  type ApiDataset,
  type ApiSession,
} from '@/services/api'
import { useAppState } from '@/composables/useAppState'
import { useModelSettings } from '@/composables/useModelSettings'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  refreshToken?: number
}>()

const { user, route, navigate } = useAppState()
const { chatModelChoice, activeModelLabel } = useModelSettings()
const toast = useToast()

const datasets = ref<ApiDataset[]>([])
const sessions = ref<ApiSession[]>([])
const selectedDatasetId = ref(0)
const activeSessionId = ref(0)
const messages = ref<PanelMessage[]>([])
const draft = ref('')
const isStreaming = ref(false)
const chatPanel = ref<InstanceType<typeof ChatPanel> | null>(null)
let abortController: AbortController | null = null

const activeSession = computed(() => sessions.value.find((s) => s.id === activeSessionId.value) ?? null)
const selectedDataset = computed(() => datasets.value.find((d) => d.id === selectedDatasetId.value) ?? null)
const datasetDeleted = computed(() => Boolean(activeSession.value?.datasetDeleted))
const canSend = computed(() => Boolean(selectedDataset.value) && !datasetDeleted.value && !isStreaming.value)

function appendAssistantError(message: PanelMessage, errorMessage: string): void {
  const warning = `⚠ ${errorMessage}`
  message.content = message.content ? `${message.content}\n\n${warning}` : warning
}

async function loadDatasets() {
  try {
    datasets.value = await listDatasets()
    const stillThere = datasets.value.some((d) => d.id === selectedDatasetId.value)
    if (!stillThere) {
      selectedDatasetId.value = datasets.value[0]?.id ?? 0
    }
  } catch (e) {
    toast.fromError(e, '加载知识库失败')
  }
}

async function loadSessions() {
  try {
    const items = await listSessions()
    if (!selectedDatasetId.value) {
      sessions.value = items
      return
    }
    sessions.value = items.filter(
      (s) => s.datasetId === selectedDatasetId.value || s.datasetDeleted,
    )
  } catch (e) {
    toast.fromError(e, '加载会话失败')
  }
}

async function loadMessages(sessionId: number) {
  try {
    const items = await listMessages(sessionId)
    messages.value = items.map((m) => ({
      id: String(m.id),
      role: m.role,
      content: m.content,
      createdAt: m.createdAt,
      citations: m.citations,
    }))
  } catch (e) {
    toast.fromError(e, '加载消息失败')
  }
}

async function selectDataset(id: number) {
  selectedDatasetId.value = id
  activeSessionId.value = 0
  messages.value = []
  await loadSessions()
}

async function selectSession(id: number) {
  if (isStreaming.value) return
  activeSessionId.value = id
  const session = sessions.value.find((s) => s.id === id)
  if (session && !session.datasetDeleted) {
    selectedDatasetId.value = session.datasetId
  }
  navigate({ view: 'chat', sessionId: id })
  await loadMessages(id)
}

async function createAndSelect() {
  if (!selectedDatasetId.value) return
  try {
    const session = await createSession(selectedDatasetId.value)
    await loadSessions()
    await selectSession(session.id)
    chatPanel.value?.focusComposer()
  } catch (e) {
    toast.fromError(e, '新建会话失败')
  }
}

async function renameSession(id: number, title: string) {
  try {
    await updateSession(id, { title })
    const item = sessions.value.find((s) => s.id === id)
    if (item) item.title = title
  } catch (e) {
    toast.fromError(e, '重命名失败')
  }
}

async function archiveSession(id: number) {
  try {
    await updateSession(id, { status: 'archived' })
    sessions.value = sessions.value.filter((s) => s.id !== id)
    toast.success('会话已归档')
    if (activeSessionId.value === id) {
      activeSessionId.value = 0
      messages.value = []
      navigate({ view: 'chat' })
    }
  } catch (e) {
    toast.fromError(e, '归档失败')
  }
}

async function send() {
  const content = draft.value.trim()
  if (!content || isStreaming.value || !selectedDatasetId.value) return
  draft.value = ''

  // 无激活会话时先创建
  let sessionId = activeSessionId.value
  if (!sessionId) {
    try {
      const session = await createSession(selectedDatasetId.value)
      await loadSessions()
      sessionId = session.id
      activeSessionId.value = session.id
      navigate({ view: 'chat', sessionId })
    } catch (e) {
      toast.fromError(e, '创建会话失败')
      return
    }
  }

  const now = new Date().toISOString()
  messages.value.push({ id: `u-${Date.now()}`, role: 'user', content, createdAt: now })
  const assistantId = `a-${Date.now()}`
  messages.value.push({ id: assistantId, role: 'assistant', content: '', createdAt: now })

  isStreaming.value = true
  abortController = new AbortController()

  const assistant = messages.value[messages.value.length - 1]
  try {
    await streamChat(
      { sessionId, content, model: chatModelChoice.value },
      {
        signal: abortController.signal,
        onMeta: () => {},
        onDelta: (text) => {
          assistant.content += text
        },
        onDone: (result) => {
          if (result.content && !assistant.content) assistant.content = result.content
          assistant.citations = result.citations ?? []
        },
        onError: (message) => {
          appendAssistantError(assistant, message)
        },
      },
    )
  } catch (e) {
    if (!abortController.signal.aborted) {
      appendAssistantError(assistant, e instanceof Error ? e.message : '生成回复失败')
    }
  } finally {
    isStreaming.value = false
    abortController = null
    void loadSessions()
  }
}

function stop() {
  abortController?.abort()
}

// 路由中的 sessionId 变化时加载对应会话
watch(
  () => route.value.sessionId,
  async (id) => {
    if (route.value.view !== 'chat') return
    if (id && id !== activeSessionId.value) {
      activeSessionId.value = id
      await loadMessages(id)
    }
  },
)

watch(
  () => props.refreshToken,
  () => {
    void loadSessions()
  },
)

onMounted(async () => {
  await loadDatasets()
  await loadSessions()
  const sid = route.value.sessionId
  if (sid) {
    activeSessionId.value = sid
    await loadMessages(sid)
  }
})
</script>

<template>
  <div class="flex h-full min-w-0">
    <SessionSidebar
      :datasets="datasets"
      :selected-dataset-id="selectedDatasetId"
      :sessions="sessions"
      :active-session-id="activeSessionId"
      @select-dataset="selectDataset"
      @select-session="selectSession"
      @create-session="createAndSelect"
      @rename-session="renameSession"
      @archive-session="archiveSession"
    />
    <div class="flex min-w-0 flex-1 flex-col">
      <ChatPanel
        ref="chatPanel"
        :dataset-name="selectedDataset?.name || activeSession?.datasetName || ''"
        :session-title="activeSession?.title || ''"
        :dataset-deleted="datasetDeleted"
        :messages="messages"
        :draft="draft"
        :is-streaming="isStreaming"
        :can-send="canSend"
        :user-name="user?.nickname || user?.email || ''"
        :model-label="activeModelLabel"
        @update:draft="draft = $event"
        @send="send"
        @stop="stop"
      />
    </div>
  </div>
</template>
