<script setup lang="ts">
import { ref } from 'vue'
import { Archive, Palette, SlidersHorizontal, User, X } from 'lucide-vue-next'
import SettingsGeneral from '@/components/settings/SettingsGeneral.vue'
import SettingsPersonalization from '@/components/settings/SettingsPersonalization.vue'
import SettingsAccount from '@/components/settings/SettingsAccount.vue'
import SettingsArchive from '@/components/settings/SettingsArchive.vue'
import type { ApiUser, ApiSession } from '@/services/api'
import type { CustomModel, ModelSelection } from '@/composables/useModelSettings'

type SettingsTab = 'general' | 'personalization' | 'archive' | 'account'

withDefaults(
  defineProps<{
    open: boolean
    user: ApiUser | null
    officialModelName: string
    customModels: CustomModel[]
    selection: ModelSelection
    archivedItems?: ApiSession[]
    deletingAccount?: boolean
  }>(),
  { deletingAccount: false, archivedItems: () => [] },
)

const emit = defineEmits<{
  close: []
  logout: []
  'delete-account': []
  restore: [sessionId: number]
  'select-official-model': []
  'select-custom-model': [id: string]
  'save-custom-model': [model: Omit<CustomModel, 'id'>]
  'delete-custom-model': [id: string]
}>()

const tab = ref<SettingsTab>('general')

const tabs: Array<{ id: SettingsTab; label: string; icon: typeof SlidersHorizontal }> = [
  { id: 'general', label: '通用', icon: SlidersHorizontal },
  { id: 'personalization', label: '个性化', icon: Palette },
  { id: 'archive', label: '归档', icon: Archive },
  { id: 'account', label: '账户', icon: User },
]

const tabTitles: Record<SettingsTab, string> = {
  general: '通用',
  personalization: '个性化',
  archive: '归档',
  account: '账户',
}
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="fixed inset-0 z-[70] flex items-center justify-center">
        <div class="absolute inset-0 bg-black/50" @click="emit('close')" />

        <div
          class="relative z-10 flex h-[480px] w-[640px] max-w-[92vw] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900"
        >
          <!-- 左侧标签栏 -->
          <div class="w-[180px] border-r border-gray-200 bg-gray-50 p-4 dark:border-gray-800 dark:bg-gray-950">
            <h2 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">设置</h2>
            <nav class="space-y-1">
              <button
                v-for="item in tabs"
                :key="item.id"
                type="button"
                class="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors"
                :class="
                  tab === item.id
                    ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-800 dark:text-white'
                    : 'text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800'
                "
                @click="tab = item.id"
              >
                <component :is="item.icon" class="size-5" />
                {{ item.label }}
              </button>
            </nav>
          </div>

          <!-- 右侧内容 -->
          <div class="flex flex-1 flex-col overflow-hidden">
            <div class="flex items-center justify-between border-b border-gray-200 px-6 py-4 dark:border-gray-800">
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ tabTitles[tab] }}</h3>
              <button
                type="button"
                class="rounded-lg p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
                @click="emit('close')"
              >
                <X class="size-5" />
              </button>
            </div>

            <div class="min-h-0 flex-1 overflow-y-auto p-6">
              <SettingsGeneral
                v-if="tab === 'general'"
                :official-name="officialModelName"
                :custom-models="customModels"
                :selection="selection"
                @select-official="emit('select-official-model')"
                @select-custom="emit('select-custom-model', $event)"
                @save-custom="emit('save-custom-model', $event)"
                @delete-custom="emit('delete-custom-model', $event)"
              />
              <SettingsPersonalization v-else-if="tab === 'personalization'" />
              <SettingsArchive
                v-else-if="tab === 'archive'"
                :items="archivedItems"
                @restore="emit('restore', $event)"
              />
              <SettingsAccount
                v-else
                :user="user"
                :deleting="deletingAccount"
                @logout="emit('logout')"
                @delete-account="emit('delete-account')"
              />
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
