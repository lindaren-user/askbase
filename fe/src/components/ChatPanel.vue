<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { ArrowUp, ChevronRight, Square, TriangleAlert } from "lucide-vue-next";
import type { ApiCitation } from "@/services/api";
import { renderAssistantMarkdown, renderMarkdown } from "@/lib/markdown";
import { nameInitial } from "@/lib/name";
import assistantAvatar from "@/assets/golang.webp";
import readBookImage from "@/assets/read-book.webp";

/** 面板消息：流式中的临时消息 id 为字符串 */
export interface PanelMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  createdAt?: string;
  citations?: ApiCitation[];
}

const props = withDefaults(
  defineProps<{
    datasetName: string;
    sessionTitle: string;
    datasetDeleted?: boolean;
    messages: PanelMessage[];
    draft: string;
    isStreaming: boolean;
    canSend: boolean;
    userName: string;
    /** 自定义模型名称；空表示服务端默认模型 */
    modelLabel?: string;
  }>(),
  { modelLabel: "", datasetDeleted: false }
);

const emit = defineEmits<{
  "update:draft": [value: string];
  send: [];
  stop: [];
}>();

const scroller = ref<HTMLElement | null>(null);
const composer = ref<HTMLTextAreaElement | null>(null);
const hoverCitation = ref<{
  messageId: string;
  citeN: number;
  top: number;
  aboveTop: number;
  left: number;
} | null>(null);
const popoverRef = ref<HTMLElement | null>(null);
const citationPreviewImage = ref("");
const popoverPos = ref<{
  top: number;
  left: number;
  maxWidth: number;
  visible: boolean;
} | null>(null);
const CITE_HIDE_MS = 220;
let hideCitationTimer = 0;

const hasStartedChat = computed(() => props.messages.length > 0);
const userInitial = computed(() => nameInitial(props.userName));

function cancelHideCitation() {
  if (hideCitationTimer) {
    window.clearTimeout(hideCitationTimer);
    hideCitationTimer = 0;
  }
}

function scheduleHideCitation() {
  cancelHideCitation();
  hideCitationTimer = window.setTimeout(() => {
    hoverCitation.value = null;
    hideCitationTimer = 0;
  }, CITE_HIDE_MS);
}

const hoverCitationData = computed<ApiCitation | null>(() => {
  const hc = hoverCitation.value;
  if (!hc) return null;
  const msg = props.messages.find((m) => m.id === hc.messageId);
  if (!msg?.citations) return null;
  return msg.citations.find((c) => c.n === hc.citeN) ?? null;
});

// 卡片定位：先不可见渲染测量高度，再决定放下方/上方
watch(
  hoverCitation,
  async (hc) => {
    if (!hc) {
      popoverPos.value = null;
      return;
    }
    const margin = 12;
    const gap = 8;
    const maxWidth = Math.min(380, window.innerWidth - margin * 2);
    let left = hc.left;
    if (left + maxWidth > window.innerWidth - margin) {
      left = window.innerWidth - maxWidth - margin;
    }
    left = Math.max(margin, left);
    popoverPos.value = { top: -9999, left, maxWidth, visible: false };
    await nextTick();
    if (!popoverRef.value) return;
    const h = popoverRef.value.getBoundingClientRect().height;
    let top = hc.top + gap;
    if (top + h > window.innerHeight - margin) {
      top = hc.aboveTop - h - gap;
      if (top < margin) top = margin;
    }
    popoverPos.value = { top, left, maxWidth, visible: true };
  },
  { flush: "post" }
);

watch(
  () => props.messages.map((m) => m.content).join(""),
  async () => {
    await nextTick();
    if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight;
  }
);

function onDraftInput(e: Event) {
  emit("update:draft", (e.target as HTMLTextAreaElement).value);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    if (props.canSend && props.draft.trim()) emit("send");
  }
}

function renderContent(msg: PanelMessage): string {
  return renderAssistantMarkdown(msg.content, msg.citations?.map((citation) => citation.n));
}

function closestCite(node: EventTarget | null): HTMLElement | null {
  if (!(node instanceof Element)) return null;
  const el = node.closest(".cite-ref");
  if (el instanceof HTMLElement) return el;
  return null;
}

function showCitationPopover(messageId: string, citeEl: HTMLElement) {
  const citeN = Number(citeEl.dataset.cite);
  if (!citeN) return;
  cancelHideCitation();
  const rect = citeEl.getBoundingClientRect();
  hoverCitation.value = {
    messageId,
    citeN,
    top: rect.bottom,
    aboveTop: rect.top,
    left: rect.left,
  };
}

function onMessageMouseOver(e: MouseEvent, messageId: string) {
  const cite = closestCite(e.target);
  if (cite) showCitationPopover(messageId, cite);
}

function onMessageMouseOut(e: MouseEvent) {
  if (!closestCite(e.target)) return;
  const to = e.relatedTarget;
  if (
    to instanceof HTMLElement &&
    (to.closest(".cite-ref") || to.closest(".cite-popover"))
  ) {
    return;
  }
  scheduleHideCitation();
}

function isStreamTail(msg: PanelMessage) {
  return (
    props.isStreaming &&
    msg.role === "assistant" &&
    msg.id === props.messages[props.messages.length - 1]?.id
  );
}

function formatMessageTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return "";
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

// focusComposer 把光标放到提问框。
function focusComposer() {
  void nextTick(() => composer.value?.focus());
}

onMounted(() => document.addEventListener("click", cancelHideCitation));
onUnmounted(() => {
  document.removeEventListener("click", cancelHideCitation);
  cancelHideCitation();
});

defineExpose({ focusComposer });
</script>

<template>
  <section
    class="relative flex h-full min-w-0 flex-1 flex-col bg-gray-50 dark:bg-gray-900"
  >
    <header
      class="flex h-14 shrink-0 items-center border-b border-gray-200 bg-white pl-6 pr-14 dark:border-gray-800 dark:bg-gray-950"
    >
      <div class="flex min-w-0 items-center gap-1.5 text-sm">
        <TriangleAlert
          v-if="datasetDeleted"
          class="size-4 shrink-0 text-amber-500"
          title="所属知识库已删除"
        />
        <span class="truncate text-gray-500 dark:text-gray-400">{{
          datasetDeleted ? "知识库已删除" : datasetName || "未选择知识库"
        }}</span>
        <ChevronRight
          v-if="sessionTitle"
          class="size-4 shrink-0 text-gray-400"
        />
        <span
          v-if="sessionTitle"
          class="truncate font-medium text-gray-900 dark:text-white"
          >{{ sessionTitle }}</span
        >
      </div>
      <span
        v-if="modelLabel"
        class="ml-auto flex shrink-0 items-center gap-1 rounded-full bg-blue-50 px-2.5 py-1 text-xs text-blue-600 dark:bg-blue-950 dark:text-blue-400"
        title="当前使用自定义模型（本机 API Key）"
      >
        {{ modelLabel }}
      </span>
    </header>

    <div class="flex min-h-0 flex-1 flex-col">
      <div
        v-show="hasStartedChat"
        ref="scroller"
        class="min-h-0 flex-1 overflow-y-auto px-6 py-6"
      >
        <div class="mx-auto max-w-3xl space-y-6">
          <article
            v-for="msg in messages"
            :key="msg.id"
            class="message-enter flex gap-3"
            :data-message-id="msg.id"
          >
            <div
              v-if="msg.role === 'user'"
              class="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-gray-900 text-xs font-medium text-white dark:bg-gray-100 dark:text-gray-900"
            >
              {{ userInitial }}
            </div>
            <img
              v-else
              :src="assistantAvatar"
              alt=""
              class="mt-0.5 size-8 shrink-0 object-contain"
            />
            <div class="min-w-0 flex-1">
              <div
                class="mb-2 flex items-baseline gap-2 text-xs font-semibold text-gray-400"
              >
                <span>{{ msg.role === "user" ? "你" : "AskBase 助手" }}</span>
                <span
                  v-if="msg.createdAt"
                  class="font-normal text-gray-300 dark:text-gray-600"
                >
                  {{ formatMessageTime(msg.createdAt) }}
                </span>
              </div>

              <div
                v-if="msg.role === 'user'"
                class="rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm leading-6 text-gray-800 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-100"
              >
                {{ msg.content }}
              </div>
              <template v-else>
                <div
                  @mouseover="onMessageMouseOver($event, msg.id)"
                  @mouseout="onMessageMouseOut"
                >
                  <div
                    class="md-body text-sm leading-7 text-gray-800 dark:text-gray-100"
                    :class="{ 'is-streaming': isStreamTail(msg) }"
                    v-html="renderContent(msg)"
                  />
                  <div
                    v-if="msg.citations?.length"
                    class="mt-2 flex flex-wrap gap-1.5"
                  >
                    <span class="text-xs text-gray-400">来源：</span>
                    <button
                      v-for="cite in msg.citations"
                      :key="cite.n"
                      type="button"
                      class="cite-ref rounded border border-gray-200 px-1.5 py-0.5 text-xs text-gray-500 hover:border-blue-400 hover:text-blue-600 dark:border-gray-700 dark:text-gray-400"
                      :data-cite="cite.n"
                    >
                      [{{ cite.n }}] {{ cite.documentName }}
                    </button>
                  </div>
                </div>
              </template>
            </div>
          </article>
        </div>
      </div>

      <!-- 输入区 -->
      <div
        class="composer-slot relative px-6"
        :class="
          hasStartedChat
            ? 'shrink-0 pb-5 pt-2'
            : 'flex flex-1 flex-col items-center justify-center pb-16'
        "
      >
        <div class="relative mx-auto w-full max-w-3xl">
          <img
            v-if="!hasStartedChat"
            :src="readBookImage"
            alt=""
            aria-hidden="true"
            class="pointer-events-none absolute -top-6 right-8 z-0 w-36 select-none sm:right-10 sm:w-40"
          />
          <div class="relative z-10">
            <div v-if="!hasStartedChat" class="mb-6 text-center">
              <h2 class="text-2xl font-semibold text-gray-900 dark:text-white">
                {{
                  datasetDeleted
                    ? "所属知识库已删除"
                    : `向「${datasetName || "知识库"}」提问`
                }}
              </h2>
              <p
                class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400"
              >
                基于向量检索增强生成，答案附引用溯源
              </p>
            </div>
            <div
              class="flex w-full items-center gap-2 rounded-[28px] border border-gray-200 bg-white px-4 py-2.5 shadow-sm dark:border-gray-800 dark:bg-gray-950"
            >
              <textarea
                ref="composer"
                :value="draft"
                rows="1"
                class="max-h-40 min-h-[24px] flex-1 resize-none bg-transparent py-0 text-sm leading-6 text-gray-800 outline-none dark:text-gray-100"
                :placeholder="
                  datasetDeleted
                    ? '所属知识库已删除，无法继续提问'
                    : '输入问题，Enter 发送，Shift+Enter 换行'
                "
                :disabled="!canSend"
                @input="onDraftInput"
                @keydown="onKeydown"
              />
              <button
                v-if="!isStreaming"
                type="button"
                class="flex size-9 shrink-0 items-center justify-center rounded-full bg-gray-900 text-white transition-colors hover:bg-gray-800 disabled:opacity-40 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
                :disabled="!draft.trim() || !canSend"
                aria-label="发送"
                @click="emit('send')"
              >
                <ArrowUp class="size-4" />
              </button>
              <button
                v-else
                type="button"
                class="flex size-9 shrink-0 items-center justify-center rounded-full bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900"
                aria-label="停止生成"
                @click="emit('stop')"
              >
                <Square class="size-3.5" fill="currentColor" />
              </button>
            </div>
            <p class="mt-2 text-center text-xs text-gray-400">
              答案由大模型基于检索片段生成，可能存在偏差，请核对引用原文
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- 引用悬浮卡片 -->
    <Teleport to="body">
      <div
        v-if="hoverCitation && popoverPos"
        ref="popoverRef"
        class="cite-popover fixed z-50 w-max p-2 -m-2"
        :style="{
          top: `${popoverPos.top}px`,
          left: `${popoverPos.left}px`,
          maxWidth: `${popoverPos.maxWidth}px`,
          opacity: popoverPos.visible ? 1 : 0,
          pointerEvents: popoverPos.visible ? 'auto' : 'none',
        }"
        @mouseenter="cancelHideCitation"
        @mouseleave="scheduleHideCitation"
      >
        <div
          class="rounded-lg border border-gray-200 bg-white px-3 py-2 shadow-lg dark:border-gray-700 dark:bg-gray-900"
        >
          <template v-if="hoverCitationData">
            <div class="text-sm font-medium text-gray-900 dark:text-gray-100">
              [{{ hoverCitationData.n }}] {{ hoverCitationData.documentName }}
            </div>
            <div class="mt-0.5 text-xs text-gray-400">
              相似度 {{ hoverCitationData.score.toFixed(3) }}
            </div>
            <img
              v-if="hoverCitationData.imageUrl"
              :src="hoverCitationData.imageUrl"
              alt=""
              class="mt-2 max-h-32 w-full cursor-zoom-in rounded object-contain"
              @click="citationPreviewImage = hoverCitationData.imageUrl || ''"
            />
            <div
              class="md-body chunk-markdown cite-scroll mt-2 max-h-56 overflow-y-auto break-words text-sm leading-6 text-gray-600 dark:text-gray-300"
              v-html="renderMarkdown(hoverCitationData.content)"
            />
          </template>
          <p
            v-else
            class="max-w-[16rem] text-sm leading-5 text-gray-500 dark:text-gray-400"
          >
            没有对应检索来源，模型标了未提供的编号。
          </p>
        </div>
      </div>
    </Teleport>

    <!-- 引用图片预览 -->
    <Teleport to="body">
      <div
        v-if="citationPreviewImage"
        class="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 p-8"
        @click="citationPreviewImage = ''"
      >
        <img
          :src="citationPreviewImage"
          alt=""
          class="max-h-full max-w-full object-contain"
        />
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.composer-slot {
  transition: padding 480ms cubic-bezier(0.22, 1, 0.36, 1),
    flex-grow 480ms cubic-bezier(0.22, 1, 0.36, 1);
}

.md-body :deep(.cite-ref) {
  cursor: pointer;
}

.cite-scroll {
  scrollbar-width: thin;
  scrollbar-color: rgb(156 163 175 / 0.45) transparent;
}
.cite-scroll::-webkit-scrollbar {
  width: 3px;
}
.cite-scroll::-webkit-scrollbar-track {
  background: transparent;
}
.cite-scroll::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: rgb(156 163 175 / 0.45);
}

.md-body.is-streaming:empty::after,
.md-body.is-streaming:deep(> :last-child)::after {
  content: "▍";
  margin-left: 0.12em;
  animation: stream-caret 1s steps(1) infinite;
}

@keyframes stream-caret {
  50% {
    opacity: 0;
  }
}
</style>
