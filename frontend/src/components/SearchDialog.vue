<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import type { Session } from "@/types/chat";
import Icon from "./Icon.vue";

const store = useChatStore();
const open = defineModel<boolean>("open", { default: false });

const q = ref("");
const sel = ref(0);
const input = ref<HTMLInputElement | null>(null);

/** 后端没有会话搜索端点，这里在已加载的 sessions 上做客户端过滤（标题 + 项目名）。 */
const results = computed<Session[]>(() => {
  const list = store.sessions ?? [];
  const needle = q.value.trim().toLowerCase();
  if (!needle) return list;
  const title = (s: Session) => (s.title || "未命名").toLowerCase();
  const proj = (s: Session) =>
    (store.projects ?? []).find((p) => p.id === s.projectId)?.title.toLowerCase() ?? "";
  return list.filter((s) => title(s).includes(needle) || proj(s).includes(needle));
});

watch(open, (v) => {
  if (!v) return;
  q.value = "";
  sel.value = 0;
  void nextTick(() => input.value?.focus());
});

watch(q, () => {
  sel.value = 0;
});

function go(s: Session) {
  open.value = false;
  void store.selectSession(s.id);
}

function newChat() {
  open.value = false;
  store.newSession();
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    open.value = false;
  } else if (e.key === "ArrowDown") {
    e.preventDefault();
    sel.value = Math.min(sel.value + 1, results.value.length - 1);
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    sel.value = Math.max(sel.value - 1, 0);
  } else if (e.key === "Enter") {
    e.preventDefault();
    const hit = results.value[sel.value];
    if (hit) go(hit);
    else if (q.value.trim()) newChat();
  }
}

function projectOf(s: Session): string {
  return (store.projects ?? []).find((p) => p.id === s.projectId)?.title ?? "";
}

function when(s: Session): string {
  const t = Date.parse(s.updatedAt);
  if (Number.isNaN(t)) return "";
  const diff = Date.now() - t;
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return new Date(t).toLocaleDateString();
}

onMounted(() => {
  /* no-op: focus happens when opened */
});
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="dialog-scrim" @click.self="open = false">
      <div
        class="dialog search"
        role="dialog"
        aria-modal="true"
        aria-label="搜索对话"
        @click.stop
        @keydown="onKeydown"
      >
        <div class="search__bar">
          <Icon name="search" />
          <input
            ref="input"
            v-model="q"
            class="search__input"
            placeholder="搜索对话"
            aria-label="搜索对话"
            role="combobox"
            aria-expanded="true"
            aria-controls="cm-search-results"
            :aria-activedescendant="results[sel] ? `cm-sr-${sel}` : undefined"
          />
          <button
            type="button"
            class="icon-btn icon-btn--sm"
            title="关闭"
            aria-label="关闭"
            @click="open = false"
          >
            <Icon name="close" size="sm" />
          </button>
        </div>

        <div id="cm-search-results" class="search__results" role="listbox" aria-label="对话">
          <button
            v-if="!q.trim()"
            type="button"
            class="search__item search__item--new"
            @click="newChat"
          >
            <Icon name="new-chat" />
            <span>新对话</span>
          </button>

          <button
            v-for="(s, i) in results"
            :id="`cm-sr-${i}`"
            :key="s.id"
            type="button"
            class="search__item"
            :class="{ 'is-sel': i === sel }"
            role="option"
            :aria-selected="i === sel"
            @click="go(s)"
            @pointermove="sel = i"
          >
            <Icon name="chat" />
            <span class="search__text">
              <b>{{ s.title || "未命名" }}</b>
              <span>{{ [projectOf(s), when(s)].filter(Boolean).join(" · ") || "无项目" }}</span>
            </span>
          </button>

          <p v-if="!results.length" class="search__none">
            {{ q.trim() ? "没有匹配的对话。" : "还没有对话。" }}
          </p>
        </div>
      </div>
    </div>
  </Teleport>
</template>
