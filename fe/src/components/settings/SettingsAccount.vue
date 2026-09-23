<script setup lang="ts">
import { AlertTriangle, LogOut, X } from 'lucide-vue-next'
import { ref } from 'vue'
import type { ApiUser } from '@/services/api'
import { nameInitial } from '@/lib/name'
import { useToast } from '@/composables/useToast'

const props = defineProps<{
  user: ApiUser | null
  deleting?: boolean
}>()

const emit = defineEmits<{
  logout: []
  'delete-account': []
}>()

const toast = useToast()

const deleteDialogOpen = ref(false)
const deleteConfirm = ref('')

function openDeleteDialog() {
  deleteConfirm.value = ''
  deleteDialogOpen.value = true
}

function handleDeleteAccount() {
  const expected = (props.user?.email || '').trim().toLowerCase()
  if (!expected) {
    toast.error('当前账户没有邮箱，无法注销')
    return
  }
  if (deleteConfirm.value.trim().toLowerCase() !== expected) {
    toast.error('请输入当前邮箱以确认注销')
    return
  }
  deleteDialogOpen.value = false
  emit('delete-account')
}
</script>

<template>
  <div class="space-y-8">
    <!-- 资料卡 -->
    <div class="flex items-center gap-4">
      <div
        class="flex size-16 shrink-0 items-center justify-center rounded-full bg-gray-900 text-xl font-medium text-white dark:bg-gray-100 dark:text-gray-900"
      >
        {{ nameInitial(user?.nickname || user?.email || '?') }}
      </div>
      <div class="min-w-0 flex-1">
        <h4 class="font-medium text-gray-900 dark:text-white">
          {{ user?.nickname || '未设置昵称' }}
        </h4>
        <p class="mt-0.5 truncate text-sm text-gray-500 dark:text-gray-400">
          {{ user?.email || '未绑定邮箱' }}
        </p>
      </div>
    </div>

    <!-- 危险区：注销账号 -->
    <div class="rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950/30">
      <div class="flex items-center gap-2 text-red-600 dark:text-red-400">
        <AlertTriangle class="size-5" />
        <h4 class="font-medium">注销用户</h4>
      </div>
      <p class="mt-2 text-sm text-red-600/80 dark:text-red-300/80">
        注销后将立即删除账号及全部知识库、文档、分块与会话，对象存储中的文件同步清理。该操作不可恢复。
      </p>
      <button
        type="button"
        class="mt-4 w-full rounded-lg bg-red-600 px-4 py-2 text-sm text-white hover:bg-red-700 disabled:opacity-50"
        :disabled="deleting"
        @click="openDeleteDialog"
      >
        {{ deleting ? '注销中...' : '注销用户' }}
      </button>
    </div>

    <!-- 退出登录 -->
    <button
      type="button"
      class="flex w-full items-center justify-center gap-2 rounded-lg border border-gray-200 px-4 py-3 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
      @click="emit('logout')"
    >
      <LogOut class="size-4" />
      退出登录
    </button>
  </div>

  <!-- 注销确认：输入当前邮箱 -->
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="deleteDialogOpen" class="fixed inset-0 z-[80] flex items-center justify-center">
        <div class="absolute inset-0 z-0 bg-black/50" @click="deleteDialogOpen = false" />
        <div class="relative z-10 w-[400px] overflow-hidden rounded-xl bg-white shadow-2xl dark:bg-gray-900">
          <div class="flex items-center justify-between border-b border-red-100 px-6 py-4 dark:border-red-900/60">
            <h3 class="text-lg font-semibold text-red-600 dark:text-red-300">注销用户</h3>
            <button
              type="button"
              class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-gray-800 dark:hover:text-gray-300"
              @click="deleteDialogOpen = false"
            >
              <X class="size-5" />
            </button>
          </div>
          <div class="space-y-4 p-6">
            <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">
              请输入当前邮箱以确认注销 AskBase 账户。
            </p>
            <div>
              <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">
                当前邮箱
              </label>
              <input
                v-model="deleteConfirm"
                type="email"
                class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2.5 text-sm text-gray-700 outline-none focus:border-gray-400 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-300"
                placeholder="请输入邮箱"
                @keydown.enter="handleDeleteAccount"
              />
            </div>
            <div class="flex gap-3 pt-2">
              <button
                type="button"
                class="flex-1 rounded-lg border border-gray-200 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
                @click="deleteDialogOpen = false"
              >
                取消
              </button>
              <button
                type="button"
                class="flex-1 rounded-lg bg-red-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-red-700"
                @click="handleDeleteAccount"
              >
                确认注销
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
