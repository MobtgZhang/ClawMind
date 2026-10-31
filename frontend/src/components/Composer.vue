<script setup lang="ts">
import { ref, watch, nextTick } from "vue";
import { useChatStore } from "@/stores/chat";
import Icon from "./Icon.vue";

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
  el.style.height = Math.min(el.scrollHeight, 240) + "px";
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
    void submit();
  }
}

const showStop = () => store.sending && !!store.streamingMessageId;
const showSpinner = () => store.sending && !store.streamingMessageId;
</script>

<template>
  <div class="chat__dock">
    <div class="composer-wrap">
      <div v-if="store.lastError && store.bootState === 'ready'" class="msg-error">
        {{ store.lastError }}
      </div>
      <div class="composer">
        <textarea
          ref="ta"
          v-model="text"
          rows="1"
          class="composer__input"
          placeholder="描述要改的代码，Enter 发送 / Shift+Enter 换行…"
          :disabled="!store.currentSessionId || store.sending"
          @input="resize"
          @keydown="onKeydown"
        />
        <!-- 停止 -->
        <button
          v-if="showStop()"
          type="button"
          class="send send--stop"
          title="停止生成"
          aria-label="停止生成"
          @click="store.stopGeneration()"
        >
          <Icon name="stop" />
        </button>
        <!-- 发送中（尚未挂流）：转圈 -->
        <span
          v-else-if="showSpinner()"
          class="dot-pulse mb-2"
          aria-hidden="true"
          title="正在处理…"
        />
        <!-- 发送 -->
        <button
          v-else
          type="button"
          class="send"
          :disabled="!store.currentSessionId || store.sending || !text.trim()"
          title="发送"
          aria-label="发送"
          @click="submit"
        >
          <Icon name="send" />
        </button>
      </div>
      <p class="composer-note">
        内容由 AI 生成。改动后会自动跑推断的测试命令验证；模型与密钥见设置。
      </p>
    </div>
  </div>
</template>
