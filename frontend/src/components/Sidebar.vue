<script setup lang="ts">
import { ref } from "vue";
import { useChatStore } from "@/stores/chat";

const store = useChatStore();
const projectsOpen = ref(true);
const skillsOpen = ref(true);
const chatsOpen = ref(true);
const skillFileInput = ref<HTMLInputElement | null>(null);

defineEmits<{ (e: "open-settings"): void }>();

function onNewProject() {
  const t = window.prompt("项目名称", "新项目");
  if (t === null) return;
  void store.createProject(t);
}

function triggerSkillImport() {
  skillFileInput.value?.click();
}

async function onSkillFile(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  try {
    await store.importSkillsFile(file);
  } catch (err) {
    window.alert((err as Error).message || "导入失败");
  }
}

function onNewSkill() {
  const name = window.prompt("技能名称（function name）", "");
  if (name === null || !name.trim()) return;
  const desc = window.prompt("技能描述", "自定义技能") ?? "";
  void store.createSkill(name.trim(), desc).catch((e) => {
    window.alert((e as Error).message || "创建失败");
  });
}
</script>

<template>
  <aside
    class="flex h-full w-[272px] shrink-0 flex-col border-r border-ink/10 bg-surface-muted/80"
  >
    <div class="flex items-center gap-2.5 px-3 py-3">
      <img
        src="/logo.svg"
        width="32"
        height="32"
        class="h-8 w-8 shrink-0 rounded-lg"
        alt="ClawMind"
      />
      <span class="font-semibold tracking-tight text-ink">ClawMind</span>
    </div>

    <input
      ref="skillFileInput"
      type="file"
      accept="application/json,.json"
      class="hidden"
      @change="onSkillFile"
    />

    <div class="flex-1 overflow-y-auto px-2 pb-2">
      <!-- 项目：仅新建与筛选 -->
      <button
        type="button"
        class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-xs font-semibold uppercase tracking-wide text-ink-muted hover:bg-ink/5"
        @click="projectsOpen = !projectsOpen"
      >
        项目
        <span class="text-ink-muted/60">{{ projectsOpen ? "−" : "+" }}</span>
      </button>
      <div v-show="projectsOpen" class="mb-3 mt-1 space-y-0.5 pl-1">
        <button
          type="button"
          class="w-full truncate rounded-lg px-3 py-2 text-left text-sm transition-colors"
          :class="
            store.projectFilter === 'all'
              ? 'bg-ink/10 text-ink'
              : 'text-ink-muted hover:bg-ink/5 hover:text-ink'
          "
          @click="store.setProjectFilter('all')"
        >
          全部对话
        </button>
        <button
          type="button"
          class="w-full truncate rounded-lg px-3 py-2 text-left text-sm transition-colors"
          :class="
            store.projectFilter === 'unassigned'
              ? 'bg-ink/10 text-ink'
              : 'text-ink-muted hover:bg-ink/5 hover:text-ink'
          "
          @click="store.setProjectFilter('unassigned')"
        >
          未归类
        </button>
        <button
          type="button"
          class="mb-1 w-full rounded-lg border border-dashed border-ink/20 px-3 py-2 text-left text-xs text-ink-muted hover:bg-ink/5 hover:text-ink"
          @click="onNewProject"
        >
          + 新建项目
        </button>
        <ul class="space-y-0.5">
          <li v-for="p in store.projects ?? []" :key="p.id">
            <button
              type="button"
              class="w-full truncate rounded-lg px-3 py-2 text-left text-sm transition-colors"
              :class="
                store.projectFilter === p.id
                  ? 'bg-ink/10 text-ink'
                  : 'text-ink-muted hover:bg-ink/5 hover:text-ink'
              "
              :title="p.title"
              @click="store.setProjectFilter(p.id)"
            >
              {{ p.title }}
            </button>
          </li>
        </ul>
      </div>

      <!-- 技能：导入 + 新建 -->
      <button
        type="button"
        class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-xs font-semibold uppercase tracking-wide text-ink-muted hover:bg-ink/5"
        @click="skillsOpen = !skillsOpen"
      >
        技能
        <span class="text-ink-muted/60">{{ skillsOpen ? "−" : "+" }}</span>
      </button>
      <div v-show="skillsOpen" class="mb-3 mt-1 space-y-1 pl-1">
        <div class="flex gap-1 px-1">
          <button
            type="button"
            class="flex-1 rounded-lg border border-ink/15 px-2 py-1.5 text-xs text-ink hover:bg-ink/5"
            @click="triggerSkillImport"
          >
            导入
          </button>
          <button
            type="button"
            class="flex-1 rounded-lg border border-ink/15 px-2 py-1.5 text-xs text-ink hover:bg-ink/5"
            @click="onNewSkill"
          >
            新建
          </button>
        </div>
        <p class="px-2 text-[10px] leading-snug text-ink-muted">
          导入为 OpenAI 格式 JSON（含 <code class="rounded bg-surface-muted px-0.5">tools</code> 数组）；新建写入
          <code class="rounded bg-surface-muted px-0.5">.clawmind/skills.json</code>。内置原子工具始终可用。
        </p>
        <p v-if="(store.skills ?? []).length === 0" class="px-3 py-1 text-xs text-ink-muted">暂无扩展技能</p>
        <ul v-else class="max-h-48 space-y-1 overflow-y-auto">
          <li
            v-for="sk in store.skills ?? []"
            :key="sk.name"
            class="rounded-lg px-3 py-2 text-sm text-ink-muted"
            :title="sk.description"
          >
            <span class="font-medium text-ink">{{ sk.name }}</span>
            <span v-if="sk.description" class="mt-0.5 line-clamp-2 block text-xs opacity-80">{{
              sk.description
            }}</span>
          </li>
        </ul>
      </div>

      <!-- 历史对话 -->
      <button
        type="button"
        class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-xs font-semibold uppercase tracking-wide text-ink-muted hover:bg-ink/5"
        @click="chatsOpen = !chatsOpen"
      >
        历史对话
        <span class="text-ink-muted/60">{{ chatsOpen ? "−" : "+" }}</span>
      </button>
      <div v-show="chatsOpen" class="mt-1 space-y-0.5">
        <button
          type="button"
          class="mb-1 flex w-full items-center gap-2 rounded-lg border border-ink/15 bg-surface px-3 py-2.5 text-left text-sm text-ink hover:bg-surface-muted"
          @click="store.newSession()"
        >
          <span class="text-lg leading-none">+</span>
          新对话
        </button>
        <ul class="space-y-0.5">
          <li v-for="s in store.sessions ?? []" :key="s.id">
            <div class="flex items-center gap-1">
              <button
                type="button"
                class="min-w-0 flex-1 truncate rounded-lg px-3 py-2 text-left text-sm transition-colors"
                :class="
                  s.id === store.currentSessionId
                    ? 'bg-ink/10 text-ink'
                    : 'text-ink-muted hover:bg-ink/5 hover:text-ink'
                "
                :title="s.title"
                @click="store.selectSession(s.id)"
              >
                {{ s.title || "未命名" }}
              </button>
              <button
                type="button"
                class="shrink-0 rounded p-1.5 text-base leading-none text-ink-muted/80 hover:bg-ink/10 hover:text-ink"
                title="删除对话"
                @click.prevent.stop="store.deleteSession(s.id)"
              >
                ×
              </button>
            </div>
          </li>
        </ul>
      </div>
    </div>

    <div class="border-t border-ink/10 p-2">
      <div class="flex w-full items-center justify-between px-1">
        <button
          type="button"
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-ink-muted transition hover:bg-ink/10 hover:text-ink"
          title="设置"
          aria-label="设置"
          @click="$emit('open-settings')"
        >
          <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
            />
            <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
        </button>
        <button
          type="button"
          class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-ink-muted transition hover:bg-ink/10 hover:text-ink"
          :title="store.darkMode ? '切换为浅色' : '切换为深色'"
          :aria-label="store.darkMode ? '切换为浅色' : '切换为深色'"
          @click="store.setDarkMode(!store.darkMode)"
        >
          <!-- 深色模式时显示太阳 → 点一下切浅色 -->
          <svg
            v-if="store.darkMode"
            class="h-5 w-5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="4" />
            <path
              stroke-linecap="round"
              d="M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32l1.41 1.41M2 12h2m16 0h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"
            />
          </svg>
          <!-- 浅色模式时显示月亮 → 点一下切深色 -->
          <svg
            v-else
            class="h-5 w-5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z"
            />
          </svg>
        </button>
      </div>
    </div>
  </aside>
</template>
