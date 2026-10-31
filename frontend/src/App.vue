<script setup lang="ts">
import { nextTick, onMounted, onBeforeUnmount, ref, watch, computed } from "vue";
import Sidebar from "./components/Sidebar.vue";
import ChatMessage from "./components/ChatMessage.vue";
import Composer from "./components/Composer.vue";
import SettingsPanel from "./components/SettingsPanel.vue";
import HarnessPanel from "./components/HarnessPanel.vue";
import PluginsPanel from "./components/PluginsPanel.vue";
import CodePanel from "./components/CodePanel.vue";
import SearchDialog from "./components/SearchDialog.vue";
import Icon from "./components/Icon.vue";
import { useChatStore } from "./stores/chat";
import { useCodeStore } from "./stores/code";

const store = useChatStore();
const code = useCodeStore();
const settingsOpen = ref(false);
const harnessOpen = ref(false);
const pluginsOpen = ref(false);
const codeOpen = ref(false);
const searchOpen = ref(false);
const listRef = ref<HTMLElement | null>(null);
const showJump = ref(false);
const modelMenuOpen = ref(false);

/** 当前模型配置（聊天页左上角）：谁在回答、按什么采样 */
const modelName = computed(
  () => store.serverSettings.openaiModel?.trim() || store.currentSession?.model || "未配置模型"
);
const presetLabel = computed(() => {
  const t = Number(store.serverSettings.temperature ?? 0.7);
  if (t <= 0.35) return "精确";
  if (t >= 0.9) return "创意";
  return "平衡";
});

/** 图循环：工作区分级决定的是单遍还是图循环 */
const loopLabel = computed(() => (code.view?.loop === "graph" ? "图循环" : "单遍"));

const messageCount = computed(() => store.visiblePathMessages.length);

onMounted(() => {
  void store.bootstrap();
  void code.refresh();
});

/** 新消息到达时贴底；用户上翻后不打扰（Maya 的行为） */
watch(messageCount, async () => {
  await nextTick();
  const el = listRef.value;
  if (!el) return;
  const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 240;
  if (nearBottom) el.scrollTop = el.scrollHeight;
});

/** 流式输出时跟随滚动 */
watch(
  () => store.streamingPartTexts[store.streamingMessageId ?? ""] ?? {},
  async () => {
    await nextTick();
    const el = listRef.value;
    if (!el) return;
    if (showJump.value) return;
    el.scrollTop = el.scrollHeight;
  },
  { deep: true }
);

function onScroll() {
  const el = listRef.value;
  if (!el) return;
  showJump.value = el.scrollHeight - el.scrollTop - el.clientHeight > 320;
}

function jumpBottom() {
  const el = listRef.value;
  if (!el) return;
  el.scrollTop = el.scrollHeight;
  showJump.value = false;
}

/** 任何弹层打开时，收起左上角的模型菜单（否则会盖在弹层之上） */
watch(
  [settingsOpen, harnessOpen, pluginsOpen, codeOpen, searchOpen],
  (values) => {
    if (values.some(Boolean)) modelMenuOpen.value = false;
  }
);

/** 点击空白处或按 Esc 关闭模型菜单 */
onMounted(() => {
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") modelMenuOpen.value = false;
  };
  const onPointer = (e: PointerEvent) => {
    const anchor = document.querySelector(".anchor");
    if (anchor && e.target instanceof Node && !anchor.contains(e.target)) {
      modelMenuOpen.value = false;
    }
  };
  window.addEventListener("keydown", onKey);
  window.addEventListener("pointerdown", onPointer);
  onBeforeUnmount(() => {
    window.removeEventListener("keydown", onKey);
    window.removeEventListener("pointerdown", onPointer);
  });
});

/** 空会话的问候语建议：直接把任务放进输入框 */
const IDEAS: Array<{ icon: string; text: string }> = [
  { icon: "search", text: "先画像这个仓库：语言分布、模块依赖和最大的影响面在哪" },
  { icon: "check", text: "跑一遍测试，定位失败原因并修复" },
  { icon: "boxes", text: "查一下这个符号的调用者，评估改动的影响面" },
  { icon: "edit", text: "把这个模块的重复逻辑抽出来，并用测试证明没破坏行为" },
];

function useIdea(text: string) {
  void store.sendMessage(text);
}
</script>

<template>
  <div class="shell">
    <Sidebar
      @open-settings="settingsOpen = true"
      @open-harness="harnessOpen = true"
      @open-plugins="pluginsOpen = true"
      @open-code="codeOpen = true"
      @open-search="searchOpen = true"
    />
    <main class="chat">
      <header class="chat-head">
        <div class="chat-head__left">
          <!-- 左上角：当前模型配置（Maya-Project 同款） -->
          <div class="anchor">
            <button
              type="button"
              class="model-btn"
              :class="{ 'is-open': modelMenuOpen }"
              title="模型配置"
              aria-label="模型配置"
              @click="modelMenuOpen = !modelMenuOpen"
            >
              <span>{{ modelName }}</span>
              <span class="badge badge--muted">{{ presetLabel }}</span>
              <Icon name="chevron" size="sm" />
            </button>
            <div v-if="modelMenuOpen" class="menu model-menu" @click.stop>
              <div class="model-menu__card">
                <span class="cm-mark h-9 w-9"><Icon name="cpu" size="sm" /></span>
                <span>
                  <b>{{ modelName }}</b>
                  <span>采样档：{{ presetLabel }} · T={{ store.serverSettings.temperature ?? 0.7 }}</span>
                </span>
              </div>
              <div class="menu__sep" />
              <button
                type="button"
                class="menu__item"
                @click="modelMenuOpen = false; settingsOpen = true"
              >
                <Icon name="sliders" size="sm" />
                <span class="menu__label">
                  <span>模型与采样设置</span>
                  <span class="menu__sub">名称、密钥、Base URL、温度</span>
                </span>
              </button>
              <button
                type="button"
                class="menu__item"
                @click="modelMenuOpen = false; codeOpen = true"
              >
                <Icon name="gauge" size="sm" />
                <span class="menu__label">
                  <span>代码工作区</span>
                  <span class="menu__sub">{{ code.view?.workspace ?? "尚未扫描" }}</span>
                </span>
              </button>
            </div>
          </div>
          <button
            type="button"
            class="chat-title"
            :title="store.currentSession?.title ?? 'ClawMind'"
            @click="settingsOpen = true"
          >
            <span>{{ store.currentSession?.title ?? "ClawMind" }}</span>
            <Icon name="chevron" size="sm" class="text-ink-muted" />
          </button>
        </div>
        <div class="chat-head__right">
          <!-- 右上角：图循环 -->
          <button
            v-if="code.view"
            type="button"
            class="badge cursor-pointer"
            :class="code.view.loop === 'graph' ? 'badge--accent' : 'badge--muted'"
            :title="code.view.loop === 'graph' ? '图循环模式' : '单遍模式'"
            @click="codeOpen = true"
          >
            <span class="badge__dot" />
            {{ loopLabel }}
          </button>
          <button
            type="button"
            class="icon-btn"
            title="代码工作区"
            aria-label="代码工作区"
            @click="codeOpen = true"
          >
            <Icon name="code" />
          </button>
          <button
            type="button"
            class="icon-btn"
            title="Harness（自进化）"
            aria-label="Harness（自进化）"
            @click="harnessOpen = true"
          >
            <Icon name="bolt" />
          </button>
          <button
            type="button"
            class="icon-btn"
            title="插件层与记忆"
            aria-label="插件层与记忆"
            @click="pluginsOpen = true"
          >
            <Icon name="layers" />
          </button>
        </div>
      </header>

      <div ref="listRef" class="chat__scroll" @scroll.passive="onScroll">
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
          <p class="max-w-md text-sm text-ink-muted">{{ store.bootError }}</p>
          <p class="max-w-md text-xs text-ink-muted">
            请确认后端已启动（例如在项目根目录执行 <code class="mono rounded bg-surface-2 px-1">make run</code>），并用浏览器访问
            <code class="mono rounded bg-surface-2 px-1">http://127.0.0.1:5173</code>。
          </p>
          <button type="button" class="btn btn--primary" @click="store.bootstrap()">重试连接</button>
        </div>
        <template v-else-if="messageCount === 0">
          <!-- 空会话：问候 + 工作区卡片 + 代码工程建议 -->
          <div class="greeting">
            <h1>今天要改哪个仓库？</h1>
            <div v-if="code.view" class="ws-card">
              <span class="cm-mark h-10 w-10 text-lg"><Icon name="code" /></span>
              <div class="ws-card__text">
                <div class="flex flex-wrap items-center gap-2">
                  <b class="text-base font-semibold text-ink">当前工作区</b>
                  <span
                    class="badge"
                    :class="code.view.class === 'complex' ? 'badge--accent' : 'badge--ok'"
                  >
                    {{ code.view.class === "complex" ? "complex · 图循环" : "simple · 单遍" }}
                  </span>
                  <span v-if="code.view.profile?.branch" class="badge badge--muted">
                    <Icon name="swap" size="sm" />{{ code.view.profile.branch }}
                  </span>
                </div>
                <div class="ws-card__facts">
                  <span class="mono">{{ code.view.workspace }}</span>
                  <span>文件<b class="num">{{ code.view.graph?.shape.files ?? code.view.profile?.scanned ?? "—" }}</b></span>
                  <span>符号<b class="num">{{ code.view.graph?.shape.symbols ?? "—" }}</b></span>
                  <span>测试文件<b class="num">{{ code.view.profile?.testFiles ?? 0 }}</b></span>
                </div>
              </div>
              <button type="button" class="btn btn--secondary btn--sm" @click="codeOpen = true">
                详情
              </button>
            </div>
            <div class="ideas">
              <button
                v-for="idea in IDEAS"
                :key="idea.text"
                type="button"
                class="idea"
                @click="useIdea(idea.text)"
              >
                <Icon :name="idea.icon" />
                {{ idea.text }}
              </button>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="thread">
            <ChatMessage v-for="m in store.visiblePathMessages" :key="m.id" :message="m" />
          </div>
        </template>
      </div>

      <button v-if="showJump" type="button" class="jump" title="回到底部" aria-label="回到底部" @click="jumpBottom">
        <Icon name="arrow-down" />
      </button>

      <Composer />
    </main>
    <SettingsPanel v-model:open="settingsOpen" />
    <HarnessPanel v-model:open="harnessOpen" />
    <PluginsPanel v-model:open="pluginsOpen" />
    <CodePanel v-model:open="codeOpen" />
    <SearchDialog v-model:open="searchOpen" />
  </div>
</template>
