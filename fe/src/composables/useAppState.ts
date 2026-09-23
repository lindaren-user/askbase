import { computed, ref } from 'vue'
import {
  fetchMe,
  logoutSession,
  type ApiUser,
} from '@/services/api'

/** 视图路由：hash 驱动，刷新/前进后退可恢复 */
export type ViewName =
  | 'home'       // 产品首页
  | 'datasets'   // 知识库列表
  | 'documents'  // 文档管理
  | 'chunks'     // 分块管理
  | 'retrieval'  // 检索测试
  | 'chat'       // 对话页

export interface RouteState {
  view: ViewName
  datasetId: number
  datasetName: string
  documentId: number
  sessionId: number
}

const user = ref<ApiUser | null>(null)
const authOpen = ref(false)
const settingsOpen = ref(false)
const booted = ref(false)

const route = ref<RouteState>({ view: 'home', datasetId: 0, datasetName: '', documentId: 0, sessionId: 0 })

function parseHash(): RouteState {
  const hash = window.location.hash.replace(/^#\/?/, '')
  const [pathPart, query = ''] = hash.split('?')
  const parts = pathPart.split('/').filter(Boolean)
  const params = new URLSearchParams(query)
  const state: RouteState = {
    view: 'home',
    datasetId: 0,
    datasetName: params.get('name') ?? '',
    documentId: 0,
    sessionId: 0,
  }
  if (parts[0] === 'datasets') {
    state.view = 'datasets'
    if (parts[1]) {
      state.datasetId = Number(parts[1]) || 0
      if (parts[2] === 'retrieval') state.view = 'retrieval'
      else state.view = 'documents'
    }
  } else if (parts[0] === 'documents' && parts[1] && parts[2] === 'chunks') {
    state.view = 'chunks'
    state.documentId = Number(parts[1]) || 0
  } else if (parts[0] === 'chat') {
    state.view = 'chat'
    state.sessionId = Number(parts[1]) || 0
  }
  return state
}

function toHash(state: RouteState): string {
  switch (state.view) {
    case 'home':
      return '#/'
    case 'documents':
    case 'retrieval':
      const name = state.datasetName ? `?name=${encodeURIComponent(state.datasetName)}` : ''
      return `#/datasets/${state.datasetId}/${state.view}${name}`
    case 'chunks':
      return `#/documents/${state.documentId}/chunks`
    case 'chat':
      return state.sessionId ? `#/chat/${state.sessionId}` : '#/chat'
    default:
      return '#/datasets'
  }
}

export function useAppState() {
  const isLoggedIn = computed(() => Boolean(user.value))

  function navigate(next: Partial<RouteState> & { view: ViewName }) {
    const merged: RouteState = {
      view: next.view,
      datasetId: next.datasetId ?? 0,
      datasetName: next.datasetName ?? '',
      documentId: next.documentId ?? 0,
      sessionId: next.sessionId ?? 0,
    }
    const hash = toHash(merged)
    if (window.location.hash !== hash) {
      window.location.hash = hash
    }
    route.value = merged
  }

  function applyLocation() {
    route.value = parseHash()
  }

  async function bootstrapAuth() {
    try {
      user.value = await fetchMe()
    } catch {
      user.value = null
    } finally {
      booted.value = true
    }
  }

  function openAuthModal() {
    authOpen.value = true
  }

  function handleUnauthorized() {
    user.value = null
    authOpen.value = true
  }

  function applyLoggedInUser(next: ApiUser) {
    user.value = next
    authOpen.value = false
    navigate({ view: 'datasets' })
  }

  async function logout() {
    try {
      await logoutSession()
    } finally {
      user.value = null
      settingsOpen.value = false
      navigate({ view: 'home' })
    }
  }

  return {
    user,
    isLoggedIn,
    authOpen,
    settingsOpen,
    booted,
    route,
    navigate,
    applyLocation,
    bootstrapAuth,
    openAuthModal,
    handleUnauthorized,
    applyLoggedInUser,
    logout,
  }
}
