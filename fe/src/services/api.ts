/** 全局鉴权事件：API 收到 401 时派发，前端弹出登录框 */
export const AUTH_REQUIRED_EVENT = 'askbase:auth-required'

export function emitAuthRequired() {
  window.dispatchEvent(new CustomEvent(AUTH_REQUIRED_EVENT))
}

export interface ApiEnvelope<T> {
  code: number
  msg?: string
  data?: T
}

export interface TurnstileConfig {
  siteKey: string
  enabled: boolean
}

export interface ApiUser {
  id: number
  email: string
  nickname: string
}

export interface ApiDataset {
  id: number
  userId: number
  name: string
  description: string
  chunkStrategy: string
  embedModel: string
  embedModelId: number
  visionModel?: string
  visionModelId?: number
  documentCount?: number
  chunkCount?: number
  createdAt: string
  updatedAt: string
}

export interface ApiDocument {
  id: number
  datasetId: number
  name: string
  fileHash: string
  sizeBytes: number
  status: 'pending' | 'parsing' | 'chunking' | 'embedding' | 'done' | 'failed' | 'cancelling' | 'cancelled'
  progress: number
  parseError?: string
  enabled: boolean
  chunkCount?: number
  createdAt: string
  updatedAt: string
}

export interface ApiChunk {
  id: string
  documentId: number
  datasetId: number
  chunkIndex: number
  content: string
  tokenNum: number
  enabled: boolean
  imageUrl?: string
  elementType: 'text' | 'title' | 'table' | 'formula' | 'image' | 'chart' | 'code' | 'list'
  pageNumber: number
  bbox: number[]
  parserName: string
  createdAt: string
}

export interface ApiSession {
  id: number
  userId: number
  datasetId: number
  title: string
  status: number
  datasetName?: string
  datasetDeleted?: boolean
  createdAt: string
  updatedAt: string
}

export interface ApiMessage {
  id: number
  sessionId: number
  role: 'user' | 'assistant'
  content: string
  citations?: ApiCitation[]
  createdAt: string
}

export interface ApiCitation {
  n: number
  chunkId: string
  documentId: number
  documentName: string
  content: string
  score: number
  imageUrl?: string
}

export interface ApiFileUploadToken {
  key: string
  url: string
  uploadUrl: string
  method: string
  headers: Record<string, string>
  expiresAt: string
}

export interface ApiRetrievalHit {
  chunkId: string
  documentId: number
  documentName: string
  content: string
  score: number
  imageUrl?: string
}

export interface ApiDocumentProgress {
  documentId: number
  status: ApiDocument['status']
  progress: number
  stage: string
  parseError?: string
}

export function formatRelativeTime(iso: string) {
  const time = new Date(iso).getTime()
  if (!Number.isFinite(time)) return ''
  const delta = Date.now() - time
  if (delta < 60_000) return '刚刚'
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)} 分钟前`
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)} 小时前`
  if (delta < 7 * 86_400_000) return `${Math.floor(delta / 86_400_000)} 天前`
  return new Date(iso).toLocaleDateString('zh-CN')
}

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/**
 * fetch 封装：走 Vite 代理访问后端，统一处理 401。
 */
export async function apiFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
  options?: { silent401?: boolean },
) {
  const res = await fetch(input, {
    credentials: 'include',
    ...init,
  })
  if (res.status === 401 && !options?.silent401) {
    emitAuthRequired()
  }
  return res
}

export async function apiJSON<T>(
  input: RequestInfo | URL,
  init?: RequestInit,
  options?: { silent401?: boolean },
) {
  const headers = new Headers(init?.headers)
  if (!headers.has('Content-Type') && init?.body) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await apiFetch(input, { ...init, headers }, options)
  const body = (await res.json()) as ApiEnvelope<T>
  if (!res.ok || body.code !== 0) {
    throw new Error(body.msg || '请求失败')
  }
  return body.data as T
}

// ============================== 认证 ==============================

export async function getTurnstileConfig() {
  return apiJSON<TurnstileConfig>('/api/v1/auth/turnstile-config')
}

export async function sendAuthCode(email: string, turnstileToken: string) {
  await apiJSON<void>('/api/v1/auth/send-code', {
    method: 'POST',
    body: JSON.stringify({ email, turnstileToken }),
  })
}

export async function loginWithCode(email: string, code: string, turnstileToken: string) {
  const data = await apiJSON<{ user: ApiUser }>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, code, turnstileToken }),
  })
  return data.user
}

export async function fetchMe(silent401 = true) {
  const data = await apiJSON<{ user: ApiUser }>('/api/v1/auth/me', undefined, { silent401 })
  return data.user
}

export async function logoutSession() {
  await apiJSON<void>(
    '/api/v1/auth/logout',
    { method: 'POST' },
    { silent401: true },
  )
}

/** 注销账号：级联删除全部知识库/文档/分块/会话后清除登录态。 */
export async function deleteAccount() {
  await apiJSON<void>('/api/v1/auth/account', { method: 'DELETE' })
}

// ============================== 知识库 ==============================

export async function listDatasets() {
  return apiJSON<ApiDataset[]>('/api/v1/datasets')
}

export async function createDataset(payload: {
  name: string
  description?: string
  chunkStrategy?: string
  embedModelId: number
  visionModelId?: number
}) {
  return apiJSON<ApiDataset>('/api/v1/datasets', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function updateDataset(
  id: number,
  payload: { name?: string; description?: string; chunkStrategy?: string; visionModelId?: number },
) {
  await apiJSON<void>(`/api/v1/datasets/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function deleteDataset(id: number) {
  await apiJSON<void>(`/api/v1/datasets/${id}`, { method: 'DELETE' })
}

// ============================== 文档 ==============================

/** 计算文件 SHA-256，用于上传去重（幂等登记）。 */
export async function hashFile(file: File): Promise<string> {
  const buf = await file.arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', buf)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

export async function createUploadUrl(payload: {
  datasetId: number
  filename: string
  contentType: string
  size: number
  fileHash: string
}) {
  return apiJSON<ApiFileUploadToken>('/api/v1/documents/upload-url', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

/** 按预签名凭证把文件 PUT 到对象存储。 */
export async function putToObjectStorage(token: ApiFileUploadToken, file: File) {
  const response = await fetch(token.uploadUrl, {
    method: token.method || 'PUT',
    headers: token.headers || {},
    body: file,
  })
  if (!response.ok) {
    throw new Error('上传到对象存储失败')
  }
}

export async function registerDocument(
  datasetId: number,
  payload: { name: string; r2Key: string; fileHash: string; sizeBytes: number },
) {
  return apiJSON<{ document: ApiDocument; created: boolean }>(`/api/v1/datasets/${datasetId}/documents`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function getDocument(id: number) {
  return apiJSON<ApiDocument>(`/api/v1/documents/${id}`)
}

export async function listDocuments(datasetId: number) {
  return apiJSON<ApiDocument[]>(`/api/v1/datasets/${datasetId}/documents`)
}

export async function updateDocument(
  id: number,
  payload: { name?: string; enabled?: boolean },
) {
  await apiJSON<void>(`/api/v1/documents/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function deleteDocument(id: number) {
  await apiJSON<void>(`/api/v1/documents/${id}`, { method: 'DELETE' })
}

export async function parseDocument(id: number) {
  await apiJSON<void>(`/api/v1/documents/${id}/parse`, { method: 'POST' })
}

export async function stopDocument(id: number) {
  await apiJSON<void>(`/api/v1/documents/${id}/stop`, { method: 'POST' })
}

export async function batchDocumentStatus(ids: number[], enabled: boolean) {
  await apiJSON<void>('/api/v1/documents/batch-status', {
    method: 'POST',
    body: JSON.stringify({ ids, enabled }),
  })
}

export interface ApiDocumentPreview {
  kind: 'text' | 'markdown' | 'pdf' | 'image'
  content?: string
  url?: string
  name: string
}

export async function previewDocument(id: number) {
  return apiJSON<ApiDocumentPreview>(`/api/v1/documents/${id}/preview`)
}

export async function downloadDocument(id: number) {
  return apiJSON<{ url: string; filename: string }>(`/api/v1/documents/${id}/download`)
}

export async function getDocumentProgress(id: number) {
  return apiJSON<ApiDocumentProgress>(`/api/v1/documents/${id}/progress`)
}

// ============================== 分块 ==============================

export async function listChunks(documentId: number) {
  return apiJSON<ApiChunk[]>(`/api/v1/documents/${documentId}/chunks`)
}

export async function updateChunk(id: string, payload: { enabled?: boolean; content?: string }): Promise<void> {
  await apiJSON<void>(`/api/v1/chunks/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

// ============================== 检索测试 ==============================

export async function retrievalTest(
  datasetId: number,
  payload: { query: string; topK?: number; minScore?: number },
) {
  return apiJSON<ApiRetrievalHit[]>('/api/v1/retrieval/test', {
    method: 'POST',
    body: JSON.stringify({ datasetId, ...payload }),
  })
}

// ============================== 会话与对话 ==============================

export async function listSessions(datasetId?: number) {
  const query = datasetId ? `?datasetId=${datasetId}` : ''
  return apiJSON<ApiSession[]>(`/api/v1/sessions${query}`)
}

export async function listArchivedSessions() {
  return apiJSON<ApiSession[]>('/api/v1/sessions?status=archived')
}

export async function createSession(datasetId: number, title = '') {
  return apiJSON<ApiSession>('/api/v1/sessions', {
    method: 'POST',
    body: JSON.stringify({ datasetId, title }),
  })
}

export async function updateSession(id: number, payload: { title?: string; status?: 'active' | 'archived' }) {
  await apiJSON<void>(`/api/v1/sessions/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export async function listMessages(sessionId: number) {
  return apiJSON<ApiMessage[]>(`/api/v1/sessions/${sessionId}/messages`)
}

export interface ChatMeta {
  sessionId: number
  userMessageId: number
  assistantMessageId: number
}

/** 对话模型覆盖：official 或不传用服务端默认；custom 为本机自备模型（BYOK）。 */
export interface ChatModelChoice {
  source: 'official' | 'custom'
  modelId: string
  apiUrl: string
  apiKey: string
}

export async function testCustomModel(payload: { modelId: string; apiUrl: string; apiKey: string }) {
  return apiJSON<{ ok: boolean; message: string }>('/api/v1/models/test', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function testEmbedModel(payload: { modelId: string; apiUrl: string; apiKey: string }) {
  return apiJSON<{ ok: boolean; message: string }>('/api/v1/models/test-embed', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export interface ApiEmbedModel {
  id: number
  name: string
  modelId: string
  apiUrl: string
  apiKeyMasked: string
}

export async function listEmbedModels() {
  return apiJSON<ApiEmbedModel[]>('/api/v1/embed-models')
}

export async function createEmbedModel(payload: {
  name: string
  modelId: string
  apiUrl: string
  apiKey: string
}) {
  return apiJSON<ApiEmbedModel>('/api/v1/embed-models', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function updateEmbedModel(
  id: number,
  payload: { name?: string; modelId?: string; apiUrl?: string; apiKey?: string },
) {
  await apiJSON<void>(`/api/v1/embed-models/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export async function deleteEmbedModel(id: number) {
  await apiJSON<void>(`/api/v1/embed-models/${id}`, { method: 'DELETE' })
}

export type ApiVisionModel = ApiEmbedModel

export async function listVisionModels() {
  return apiJSON<ApiVisionModel[]>('/api/v1/vision-models')
}

export async function createVisionModel(payload: {
  name: string
  modelId: string
  apiUrl: string
  apiKey: string
}) {
  return apiJSON<ApiVisionModel>('/api/v1/vision-models', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export async function deleteVisionModel(id: number) {
  await apiJSON<void>(`/api/v1/vision-models/${id}`, { method: 'DELETE' })
}

export async function testVisionModel(payload: { modelId: string; apiUrl: string; apiKey: string }) {
  return apiJSON<{ ok: boolean; message: string }>('/api/v1/models/test-vision', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export interface ChatDone {
  finishReason: string
  citations?: ApiCitation[]
  content?: string
}

export interface ChatHandlers {
  onMeta: (meta: ChatMeta) => void
  onDelta: (text: string) => void
  onDone?: (result: ChatDone) => void
  onError: (message: string) => void
  signal?: AbortSignal
}

/** SSE 流式对话：POST /chat/completions，按事件流回调。 */
export async function streamChat(
  payload: { sessionId: number; content: string; model?: ChatModelChoice },
  handlers: ChatHandlers,
) {
  const res = await apiFetch('/api/v1/chat/completions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal: handlers.signal,
  })
  if (!res.ok) {
    let message = '生成回复失败'
    try {
      const body = (await res.json()) as ApiEnvelope<unknown>
      if (body.msg) message = body.msg
    } catch {
      // 非 JSON 错误
    }
    throw new Error(message)
  }
  if (!res.body) {
    throw new Error('浏览器不支持流式响应')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let eventName = 'message'
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    buffer = buffer.replace(/\r\n/g, '\n')
    while (buffer.includes('\n\n')) {
      const idx = buffer.indexOf('\n\n')
      const raw = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      eventName = dispatchSSEBlock(raw, eventName, handlers)
    }
  }
  if (buffer.trim()) {
    dispatchSSEBlock(buffer, eventName, handlers)
  }
}

function dispatchSSEBlock(raw: string, prevEvent: string, handlers: ChatHandlers): string {
  let eventName = prevEvent
  const dataLines: string[] = []
  for (const line of raw.split('\n')) {
    if (line.startsWith('event:')) {
      eventName = line.slice(6).trim()
      continue
    }
    if (line.startsWith('data:')) {
      dataLines.push(line.slice(5).trimStart())
    }
  }
  if (!dataLines.length) return 'message'
  const data = dataLines.join('\n')
  try {
    const parsed = JSON.parse(data) as Record<string, unknown>
    switch (eventName) {
      case 'meta':
        handlers.onMeta(parsed as unknown as ChatMeta)
        break
      case 'delta':
        handlers.onDelta(String(parsed.content ?? ''))
        break
      case 'done':
        handlers.onDone?.({
          finishReason: String(parsed.finishReason ?? 'stop'),
          citations: Array.isArray(parsed.citations)
            ? (parsed.citations as ApiCitation[])
            : [],
          content: typeof parsed.content === 'string' ? parsed.content : undefined,
        })
        break
      case 'error':
        handlers.onError(String(parsed.message ?? '生成回复失败'))
        break
      default:
        break
    }
  } catch {
    // 忽略无法解析的片段
  }
  return 'message'
}
