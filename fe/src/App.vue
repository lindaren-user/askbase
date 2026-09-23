<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Database, Github, MessageSquare, Settings } from 'lucide-vue-next'
import brandLogo from '@/assets/golang-search.webp'
import AuthModal from '@/components/AuthModal.vue'
import SettingsModal from '@/components/SettingsModal.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import ToastHost from '@/components/ToastHost.vue'
import DatasetList from '@/views/DatasetList.vue'
import DocumentList from '@/views/DocumentList.vue'
import ChunkList from '@/views/ChunkList.vue'
import RetrievalTest from '@/views/RetrievalTest.vue'
import ChatView from '@/views/ChatView.vue'
import HomeView from '@/views/HomeView.vue'
import { useAppState } from '@/composables/useAppState'
import { useColorMode } from '@/composables/useColorMode'
import { useModelSettings } from '@/composables/useModelSettings'
import { useToast } from '@/composables/useToast'
import { AUTH_REQUIRED_EVENT, deleteAccount, listArchivedSessions, updateSession, type ApiSession } from '@/services/api'

const GITHUB_REPO_URL = 'https://github.com/lindaren-user/askbase'

const {
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
} = useAppState()

const toast = useToast()
useColorMode()

const showHome = computed(() => route.value.view === 'home' || !isLoggedIn.value)
const showWorkbench = computed(
  () => booted.value && isLoggedIn.value && route.value.view !== 'home',
)

const {
  customModels,
  selection: modelSelection,
  selectOfficial,
  selectCustom,
  saveCustom,
  deleteCustom,
} = useModelSettings()

const logoutConfirmOpen = ref(false)

function confirmLogout(): void {
  logoutConfirmOpen.value = false
  settingsOpen.value = false
  void logout()
  toast.success('已退出登录')
}

function openWorkbench(): void {
  if (isLoggedIn.value) {
    navigate({ view: 'datasets' })
    return
  }
  openAuthModal()
}

const deletingAccount = ref(false)

async function confirmDeleteAccount(): Promise<void> {
  if (deletingAccount.value) return
  deletingAccount.value = true
  try {
    await deleteAccount()
    settingsOpen.value = false
    await logout()
    toast.success('账号已注销')
  } catch (e) {
    toast.fromError(e, '注销账号失败')
  } finally {
    deletingAccount.value = false
  }
}

const archivedItems = ref<ApiSession[]>([])
const chatRefreshToken = ref(0)

watch(settingsOpen, (open) => {
  if (open && isLoggedIn.value) void loadArchivedSessions()
})

async function loadArchivedSessions(): Promise<void> {
  try {
    archivedItems.value = await listArchivedSessions()
  } catch (e) {
    archivedItems.value = []
    toast.fromError(e, '加载归档失败')
  }
}

async function restoreSession(sessionId: number): Promise<void> {
  try {
    await updateSession(sessionId, { status: 'active' })
    archivedItems.value = archivedItems.value.filter((s) => s.id !== sessionId)
    chatRefreshToken.value += 1
    toast.success('会话已恢复')
  } catch (e) {
    toast.fromError(e, '恢复会话失败')
  }
}

onMounted(() => {
  applyLocation()
  window.addEventListener(AUTH_REQUIRED_EVENT, handleUnauthorized)
  window.addEventListener('popstate', applyLocation)

  void bootstrapAuth()
})

onUnmounted(() => {
  window.removeEventListener(AUTH_REQUIRED_EVENT, handleUnauthorized)
  window.removeEventListener('popstate', applyLocation)
})
</script>

<template>
  <div
    class="relative flex bg-white text-gray-900 dark:bg-gray-950 dark:text-white"
    :class="showHome ? 'min-h-screen' : 'h-screen'"
  >
    <HomeView
      v-if="booted && showHome"
      class="flex-1"
      :logged-in="isLoggedIn"
      @login="openAuthModal"
      @workbench="openWorkbench"
    />
    <a
      v-if="showWorkbench"
      :href="GITHUB_REPO_URL"
      target="_blank"
      rel="noopener noreferrer"
      class="absolute top-3 right-3 z-40 flex size-10 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
      title="GitHub"
    >
      <Github class="size-5" />
    </a>
    <template v-if="showWorkbench">
      <!-- 应用级左侧导航 -->
      <aside
        class="flex w-16 flex-col items-center border-r border-gray-200 py-4 dark:border-gray-800"
      >
        <button
          class="mb-6 flex size-10 items-center justify-center"
          title="返回首页"
          @click="navigate({ view: 'home' })"
        >
          <img :src="brandLogo" alt="AskBase" class="size-9 object-contain" />
        </button>
        <nav class="flex flex-1 flex-col items-center gap-2">
          <button
            class="flex size-10 items-center justify-center rounded-lg transition-colors"
            :class="
              route.view !== 'chat'
                ? 'bg-gray-100 text-gray-900 dark:bg-gray-800 dark:text-white'
                : 'text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300'
            "
            title="知识库"
            @click="navigate({ view: 'datasets' })"
          >
            <Database class="size-5" />
          </button>
          <button
            class="flex size-10 items-center justify-center rounded-lg transition-colors"
            :class="
              route.view === 'chat'
                ? 'bg-gray-100 text-gray-900 dark:bg-gray-800 dark:text-white'
                : 'text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300'
            "
            title="对话"
            @click="navigate({ view: 'chat' })"
          >
            <MessageSquare class="size-5" />
          </button>
        </nav>
        <div
          class="mb-2 flex size-10 items-center justify-center rounded-full bg-blue-100 text-sm font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-200"
          :title="user?.email"
        >
          {{ (user?.nickname || user?.email || '?').slice(0, 1).toUpperCase() }}
        </div>
        <button
          class="flex size-10 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
          title="设置"
          @click="settingsOpen = true"
        >
          <Settings class="size-5" />
        </button>
      </aside>

      <!-- 主内容区 -->
      <main class="flex-1 overflow-hidden">
        <DatasetList v-if="route.view === 'datasets'" />
        <DocumentList
          v-else-if="route.view === 'documents'"
          :key="`docs-${route.datasetId}`"
          :dataset-id="route.datasetId"
          :dataset-name="route.datasetName"
        />
        <ChunkList
          v-else-if="route.view === 'chunks'"
          :key="`chunks-${route.documentId}`"
          :document-id="route.documentId"
        />
        <RetrievalTest
          v-else-if="route.view === 'retrieval'"
          :key="`retr-${route.datasetId}`"
          :dataset-id="route.datasetId"
          :dataset-name="route.datasetName"
        />
        <ChatView v-else-if="route.view === 'chat'" :refresh-token="chatRefreshToken" />
      </main>
    </template>

    <AuthModal :open="authOpen" @success="applyLoggedInUser" @close="authOpen = false" />

    <SettingsModal
      :open="settingsOpen"
      :user="user"
      official-model-name="服务端配置模型"
      :custom-models="customModels"
      :selection="modelSelection"
      :archived-items="archivedItems"
      :deleting-account="deletingAccount"
      @close="settingsOpen = false"
      @logout="logoutConfirmOpen = true"
      @delete-account="confirmDeleteAccount"
      @restore="restoreSession"
      @select-official-model="selectOfficial"
      @select-custom-model="selectCustom"
      @save-custom-model="saveCustom"
      @delete-custom-model="deleteCustom"
    />

    <ConfirmModal
      :open="logoutConfirmOpen"
      title="退出登录"
      message="确定要退出当前账号吗？未发送的内容不会保存。"
      confirm-text="退出"
      danger
      @confirm="confirmLogout"
      @cancel="logoutConfirmOpen = false"
    />

    <ToastHost />
  </div>
</template>
