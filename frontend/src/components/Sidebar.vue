<script setup lang="ts">
import { ref } from "vue";
import { useChatStore } from "@/stores/chat";
import Icon from "./Icon.vue";

const store = useChatStore();
const skillsOpen = ref(false);
const skillFileInput = ref<HTMLInputElement | null>(null);

defineEmits<{
  (e: "open-settings"): void;
  (e: "open-harness"): void;
  (e: "open-plugins"): void;
  (e: "open-code"): void;
  (e: "open-search"): void;
}>();

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

function filterRow(label: string, value: string) {
  return { label, value, active: store.projectFilter === value };
}
const filters = [
  filterRow("全部对话", "all"),
  filterRow("未归类", "unassigned"),
];
</script>

<template>
  <aside class="side">
    <div class="side__head">
      <span class="side__brand">
        <span class="cm-mark h-7 w-7"><Icon name="code" size="sm" /></span>
        ClawMind
      </span>
    </div>

    <input
      ref="skillFileInput"
      type="file"
      accept="application/json,.json"
      class="hidden"
      @change="onSkillFile"
    />

    <button type="button" class="nav-item" @click="store.newSession()">
      <Icon name="new-chat" />
      <span>新对话</span>
    </button>
    <button type="button" class="nav-item" @click="$emit('open-search')">
      <Icon name="search" />
      <span>搜索聊天</span>
    </button>
    <button type="button" class="nav-item" @click="$emit('open-settings')">
      <Icon name="settings" />
      <span>设置</span>
    </button>
    <button type="button" class="nav-item" @click="$emit('open-plugins')">
      <Icon name="layers" />
      <span>插件设置</span>
    </button>
    <button type="button" class="nav-item" @click="$emit('open-harness')">
      <Icon name="bolt" />
      <span>自进化 Harness</span>
    </button>
    <button type="button" class="nav-item" @click="triggerSkillImport">
      <Icon name="attach" />
      <span>导入技能</span>
    </button>
    <button type="button" class="nav-item" @click="$emit('open-code')">
      <Icon name="code" />
      <span>代码工作区</span>
    </button>

    <div class="side__list">
      <!-- 项目筛选 -->
      <div class="side__label">
        项目
        <button
          type="button"
          class="icon-btn icon-btn--sm"
          title="新建项目"
          aria-label="新建项目"
          @click="onNewProject"
        >
          <Icon name="plus" size="sm" />
        </button>
      </div>
      <div
        v-for="f in filters"
        :key="f.value"
        class="row"
        :class="{ 'is-current': store.projectFilter === f.value }"
      >
        <button type="button" class="row__open" @click="store.setProjectFilter(f.value)">
          {{ f.label }}
        </button>
      </div>
      <div
        v-for="p in store.projects ?? []"
        :key="p.id"
        class="row"
        :class="{ 'is-current': store.projectFilter === p.id }"
      >
        <button type="button" class="row__open" :title="p.title" @click="store.setProjectFilter(p.id)">
          <Icon name="folder" size="sm" class="mr-1 inline-block align-[-2px]" />
          {{ p.title }}
        </button>
      </div>

      <!-- 技能 -->
      <div class="side__label">
        技能
        <button
          type="button"
          class="icon-btn icon-btn--sm"
          title="新建技能"
          aria-label="新建技能"
          @click="onNewSkill"
        >
          <Icon name="plus" size="sm" />
        </button>
      </div>
      <button type="button" class="nav-item" @click="skillsOpen = !skillsOpen">
        <Icon name="box" />
        <span>已安装 {{ (store.skills ?? []).length }}</span>
        <Icon name="chevron" size="sm" class="chev" :class="{ 'is-closed': !skillsOpen }" />
      </button>
      <template v-if="skillsOpen">
        <p class="side__note">
          导入为 OpenAI 格式 JSON（含 <code class="mono">tools</code> 数组）；新建写入
          <code class="mono">.clawmind/skills.json</code>。内置原子工具始终可用。
        </p>
        <div
          v-for="sk in store.skills ?? []"
          :key="sk.name"
          class="row px-2 py-1"
          :title="sk.description"
        >
          <div class="min-w-0 px-2">
            <div class="truncate text-sm font-medium text-ink">{{ sk.name }}</div>
            <div v-if="sk.description" class="truncate text-xs text-ink-muted">
              {{ sk.description }}
            </div>
          </div>
        </div>
        <p v-if="(store.skills ?? []).length === 0" class="side__note">暂无扩展技能</p>
      </template>

      <!-- 历史对话 -->
      <div class="side__label">对话</div>
      <div
        v-for="s in store.sessions ?? []"
        :key="s.id"
        class="row"
        :class="{ 'is-current': s.id === store.currentSessionId }"
      >
        <span
          v-if="s.id === store.currentSessionId && store.sending"
          class="row__live"
          aria-hidden="true"
        />
        <button type="button" class="row__open" :title="s.title" @click="store.selectSession(s.id)">
          {{ s.title || "未命名" }}
        </button>
        <button
          type="button"
          class="row__more"
          title="删除对话"
          aria-label="删除对话"
          @click.prevent.stop="store.deleteSession(s.id)"
        >
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>

    <div class="side__foot">
      <button
        type="button"
        class="status-card"
        title="打开设置"
        @click="$emit('open-settings')"
      >
        <span class="cm-mark h-8 w-8"><Icon name="cpu" size="sm" /></span>
        <span class="status-card__text">
          <b>{{ store.serverSettings.openaiModel || "未配置模型" }}</b>
          <span>设置模型与密钥</span>
        </span>
        <Icon name="sliders" size="sm" class="ml-auto text-ink-muted" />
      </button>
      <div class="flex items-center justify-between px-1 pt-1">
        <button
          type="button"
          class="icon-btn"
          :title="store.darkMode ? '切换为浅色' : '切换为深色'"
          :aria-label="store.darkMode ? '切换为浅色' : '切换为深色'"
          @click="store.setDarkMode(!store.darkMode)"
        >
          <Icon :name="store.darkMode ? 'sun' : 'moon'" />
        </button>
        <button
          type="button"
          class="icon-btn"
          title="设置"
          aria-label="设置"
          @click="$emit('open-settings')"
        >
          <Icon name="settings" />
        </button>
      </div>
    </div>
  </aside>
</template>
