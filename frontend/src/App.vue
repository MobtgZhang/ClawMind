<script setup lang="ts">
import { onMounted, ref } from "vue";
import Sidebar from "./components/Sidebar.vue";
import ChatMessage from "./components/ChatMessage.vue";
import Composer from "./components/Composer.vue";
import SettingsPanel from "./components/SettingsPanel.vue";
import { useChatStore } from "./stores/chat";

const store = useChatStore();
const settingsOpen = ref(false);
const listRef = ref<HTMLElement | null>(null);

onMounted(() => {
  void store.bootstrap();
});
</script>

<template>
  <div class="flex h-full overflow-hidden">
    <Sidebar @open-settings="settingsOpen = true" />
    <main class="flex min-w-0 flex-1 flex-col">
      <header
        class="flex h-12 shrink-0 items-center justify-center border-b border-ink/10 px-3 md:px-4"
      >
        <div class="mx-auto w-4/5 max-w-full truncate px-4 md:px-6">
          <h1 class="truncate text-sm font-medium text-ink">
            {{ store.currentSession?.title ?? "ClawMind" }}
          </h1>
        </div>
      </header>
      <div ref="listRef" class="flex-1 overflow-y-auto bg-surface-muted/30 dark:bg-surface/50">
        <div
          v-if="store.bootState === 'loading'"
          class="flex h-full items-center justify-center text-ink-muted"
        >
          正在加载…
        </div>
        <div
          v-else-if="store.bootState === 'error'"
          class="flex h-full flex-col items-center justify-center gap-4 px-6 text-center"
        >
          <p class="max-w-md text-sm text-ink-muted">
            {{ store.bootError }}
          </p>
          <p class="max-w-md text-xs text-ink-muted/80">
            请确认后端已启动（例如在项目根目录执行 <code class="rounded bg-surface-muted px-1">make run</code>），并用浏览器访问
            <code class="rounded bg-surface-muted px-1">http://127.0.0.1:5173</code>，不要直接打开 dist 文件。
          </p>
          <button
            type="button"
            class="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white hover:opacity-90"
            @click="store.bootstrap()"
          >
            重试连接
          </button>
        </div>
        <template v-else>
          <div class="mx-auto w-4/5 max-w-full px-4 pb-8 pt-2 md:px-6">
            <ChatMessage v-for="m in store.visiblePathMessages" :key="m.id" :message="m" />
          </div>
        </template>
      </div>
      <Composer />
    </main>
    <SettingsPanel v-model:open="settingsOpen" />
  </div>
</template>
