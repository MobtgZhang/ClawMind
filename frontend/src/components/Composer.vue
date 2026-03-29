<script setup lang="ts">
import { ref, watch, nextTick } from "vue";
import { useChatStore } from "@/stores/chat";

const store = useChatStore();
const text = ref("");
const ta = ref<HTMLTextAreaElement | null>(null);

watch(
  () => store.currentSessionId,
  () => {
    text.value = "";
  }
);

watch(text, () => {
  if (text.value && store.lastError) store.clearLastError();
});

function resize() {
  const el = ta.value;
  if (!el) return;
  el.style.height = "auto";
  el.style.height = Math.min(el.scrollHeight, 200) + "px";
}

async function submit() {
  const t = text.value.trim();
  if (!t || store.sending) return;
  text.value = "";
  await nextTick();
  resize();
  await store.sendMessage(t);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    submit();
  }
}

const showStop = () => store.sending && !!store.streamingMessageId;
const showSpinner = () => store.sending && !store.streamingMessageId;
</script>

<template>
  <div class="border-t border-ink/10 bg-surface px-4 py-4 md:px-6">
    <div
      v-if="store.lastError && store.bootState === 'ready'"
      class="mx-auto mb-2 w-4/5 max-w-full rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-2 text-left text-xs text-red-700 dark:text-red-300 md:px-6"
    >
      {{ store.lastError }}
    </div>
    <div class="mx-auto flex w-4/5 max-w-full items-end gap-2 px-4 md:px-6">
      <div
        class="flex flex-1 items-end gap-2 rounded-3xl border border-ink/12 bg-surface-muted px-4 py-2 shadow-sm dark:border-ink/20"
      >
        <textarea
          ref="ta"
          v-model="text"
          rows="1"
          class="max-h-[200px] min-h-[44px] w-full resize-none bg-transparent py-2.5 text-[15px] text-ink outline-none placeholder:text-ink-muted"
          placeholder="发送消息…"
          :disabled="!store.currentSessionId || store.sending"
          @input="resize"
          @keydown="onKeydown"
        />
        <!-- 停止：红色方形图标 -->
        <button
          v-if="showStop()"
          type="button"
          class="mb-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-red-600 text-white shadow-md hover:bg-red-700"
          title="停止生成"
          aria-label="停止生成"
          @click="store.stopGeneration()"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <rect x="6" y="6" width="12" height="12" rx="1" />
          </svg>
        </button>
        <!-- 发送中（尚未挂上流）：转圈 -->
        <div
          v-else-if="showSpinner()"
          class="mb-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-ink/10"
          title="正在处理…"
        >
          <svg
            class="h-5 w-5 animate-spin text-ink-muted"
            viewBox="0 0 24 24"
            fill="none"
            aria-hidden="true"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            />
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
        </div>
        <!-- 发送：蓝色圆形 + 向上箭头 -->
        <button
          v-else
          type="button"
          class="mb-1 flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-blue-600 text-white shadow-sm hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-blue-600/35 disabled:shadow-none dark:bg-blue-500 dark:hover:bg-blue-600"
          :disabled="!store.currentSessionId || store.sending || !text.trim()"
          title="发送"
          aria-label="发送"
          @click="submit"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.25" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 19V5m0 0l-7 7m7-7l7 7" />
          </svg>
        </button>
      </div>
    </div>
    <p class="mx-auto mt-2 w-4/5 max-w-full px-4 text-center text-xs text-ink-muted md:px-6">
      内容由 AI 生成。模型与密钥见服务端 <code class="rounded bg-surface-muted px-1">.clawmind/config.json</code>；Agent 工具在仓库目录下执行（可用
      <code class="rounded bg-surface-muted px-1">AGENT_WORKSPACE</code> 指定工作区）。
    </p>
  </div>
</template>
