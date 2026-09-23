<script setup lang="ts">
import { ArrowRight, Github, Quote, Sparkles } from 'lucide-vue-next'
import logoImage from '@/assets/logo.webp'
import searchImage from '@/assets/golang-search.webp'

const GITHUB_REPO_URL = 'https://github.com/lindaren-user/askbase'
const currentYear = new Date().getFullYear()

defineProps<{
  loggedIn: boolean
}>()

const emit = defineEmits<{
  login: []
  workbench: []
}>()
</script>

<template>
  <div class="home-page min-h-screen overflow-x-hidden bg-[#f7f7f5] text-[#171717]">
    <header class="fixed inset-x-0 top-0 z-50 border-b border-black/5 bg-[#f7f7f5]/90 backdrop-blur-xl">
      <nav class="mx-auto flex h-16 max-w-7xl items-center justify-between px-5 sm:px-8 lg:px-10" aria-label="首页导航">
        <a href="#/" aria-label="AskBase 首页">
          <img :src="logoImage" alt="AskBase" class="h-auto w-44 object-contain sm:w-[216px]" />
        </a>
        <div class="flex items-center gap-2 sm:gap-3">
          <button
            v-if="!loggedIn"
            class="h-9 px-3 text-sm font-medium text-gray-600 transition-colors hover:text-black sm:px-4"
            @click="emit('login')"
          >
            登录
          </button>
          <span v-else class="hidden items-center gap-1.5 text-sm text-gray-500 sm:flex">
            <span class="size-1.5 rounded-full bg-emerald-500"></span>
            已登录
          </span>
          <button
            class="flex h-9 items-center gap-2 rounded-md bg-[#171717] px-3.5 text-sm font-medium text-white transition-transform hover:-translate-y-0.5 hover:bg-black sm:px-4"
            @click="emit('workbench')"
          >
            工作台
            <ArrowRight class="size-4" />
          </button>
        </div>
      </nav>
    </header>

    <main>
      <section class="relative mx-auto flex min-h-[min(860px,92vh)] max-w-7xl items-center px-5 pb-24 pt-28 sm:px-8 lg:px-10 lg:pt-24">
        <div class="grid w-full items-center gap-10 lg:grid-cols-[0.9fr_1.1fr] lg:gap-16">
          <div class="relative z-10 home-reveal">
            <div class="mb-7 inline-flex items-center gap-2 border-b border-gray-300 pb-2 text-xs font-semibold text-gray-500">
              <Sparkles class="size-3.5 text-blue-600" />
              从文档到可信答案
            </div>
            <h1 class="max-w-2xl text-5xl font-semibold leading-[1.08] text-[#111] sm:text-6xl lg:text-7xl">
              AskBase
            </h1>
            <p class="mt-5 max-w-xl text-2xl font-medium leading-snug text-gray-700 sm:text-3xl">
              让每一份文档，<br />都成为答案的一部分。
            </p>
            <p class="mt-6 max-w-lg text-base leading-7 text-gray-500 sm:text-lg">
              上传、解析、分块、混合检索与引用回答在一条链路内完成，为你建立清晰、可验证的个人知识入口。
            </p>
            <div class="mt-9 flex items-center">
              <button
                class="flex h-11 items-center gap-2 rounded-md bg-[#171717] px-5 text-sm font-medium text-white transition-transform hover:-translate-y-0.5 hover:bg-black"
                @click="emit('workbench')"
              >
                {{ loggedIn ? '进入工作台' : '开始使用' }}
                <ArrowRight class="size-4" />
              </button>
            </div>
          </div>

          <div class="home-reveal home-reveal-delay relative min-h-[390px] sm:min-h-[500px]">
            <div class="absolute inset-x-0 top-4 h-[88%] overflow-hidden rounded-md bg-[#151515] shadow-2xl shadow-black/15">
              <div class="flex h-11 items-center justify-between border-b border-white/10 px-4 text-[11px] text-white/50">
                <span>ASKBASE / KNOWLEDGE</span>
                <span class="flex items-center gap-1.5"><span class="size-1.5 rounded-full bg-emerald-400"></span>READY</span>
              </div>
              <div class="grid h-[calc(100%-2.75rem)] grid-cols-[72px_1fr] sm:grid-cols-[96px_1fr]">
                <div class="border-r border-white/10 p-3 sm:p-4">
                  <div class="mb-4 size-8 rounded-md bg-white/10"></div>
                  <div class="space-y-3">
                    <div class="h-2 w-full bg-white/20"></div>
                    <div class="h-2 w-4/5 bg-white/10"></div>
                    <div class="h-2 w-3/5 bg-white/10"></div>
                  </div>
                </div>
                <div class="relative overflow-hidden p-5 sm:p-8">
                  <p class="text-xs text-white/45">在知识库中提问</p>
                  <p class="mt-3 max-w-md text-lg font-medium leading-relaxed text-white sm:text-2xl">
                    如何让检索结果既理解语义，<br class="hidden sm:block" />又准确命中关键字？
                  </p>
                  <div class="mt-8 max-w-[65%] border-l-2 border-blue-400 pl-4 sm:max-w-md">
                    <p class="text-xs text-blue-300">基于 3 个相关分块</p>
                    <p class="mt-2 text-sm leading-6 text-white/70 sm:text-base">
                      同时执行向量与全文检索，再通过融合排序选出最相关的上下文……
                    </p>
                  </div>
                  <div class="absolute bottom-7 right-8 hidden items-center gap-2 text-[11px] text-white/35 sm:flex">
                    <Quote class="size-3.5" />
                    引用可追溯
                  </div>
                </div>
              </div>
            </div>
            <img
              :src="searchImage"
              alt="AskBase 知识检索形象"
              class="home-hero-image absolute -bottom-2 -right-5 h-40 w-auto object-contain drop-shadow-2xl sm:h-72 lg:-right-10 lg:h-80"
            />
          </div>
        </div>
      </section>
    </main>

    <footer class="fixed inset-x-0 bottom-0 z-40 border-t border-black/5 bg-[#f7f7f5]/95 backdrop-blur-xl">
      <div class="mx-auto flex h-12 max-w-7xl items-center justify-between px-5 text-xs text-gray-500 sm:px-8 lg:px-10">
        <div class="flex min-w-0 items-center gap-3">
          <img :src="logoImage" alt="" class="h-auto w-24 shrink-0 translate-y-px object-contain" />
          <span class="hidden truncate sm:inline">让文档成为可检索、可追溯的知识</span>
        </div>
        <div class="flex shrink-0 items-center gap-4">
          <span>© {{ currentYear }}</span>
          <a
            :href="GITHUB_REPO_URL"
            target="_blank"
            rel="noopener noreferrer"
            class="flex items-center gap-1.5 transition-colors hover:text-gray-900"
          >
            <Github class="size-3.5" />
            GitHub
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.home-page {
  letter-spacing: 0;
}

.home-reveal {
  opacity: 0;
  animation: home-reveal-in 700ms ease forwards;
}

.home-reveal-delay {
  animation-delay: 120ms;
}

.home-hero-image {
  animation: home-float 5s ease-in-out infinite;
}

@keyframes home-float {
  0%, 100% { transform: translateY(0) rotate(0deg); }
  50% { transform: translateY(-10px) rotate(1deg); }
}

@keyframes home-reveal-in {
  from { opacity: 0; transform: translateY(28px); }
  to { opacity: 1; transform: translateY(0); }
}

@media (prefers-reduced-motion: reduce) {
  .home-reveal {
    opacity: 1;
    transform: none;
    animation: none;
  }

  .home-hero-image {
    animation: none;
  }
}
</style>
