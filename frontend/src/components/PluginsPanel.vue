<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { usePluginStore } from "@/stores/plugins";
import type { PluginView } from "@/types/plugins";

const chat = useChatStore();
const plugins = usePluginStore();
const open = defineModel<boolean>("open", { default: false });

const projectId = ref("");

watch(
  () => open.value,
  (v) => {
    if (!v) return;
    projectId.value = chat.currentSession?.projectId ?? "";
    void plugins.refresh(projectId.value);
  }
);

watch(projectId, () => {
  if (open.value) void plugins.refresh(projectId.value);
});

const mounted = computed(() => plugins.plugins.filter((p) => p.enabled));
const seamOrder = ["llm", "tools", "memory", "session", "sandbox", "prompt", "hooks"];

function hookTotal(p: PluginView): number {
  return Object.values(p.hooks ?? {}).reduce((sum, n) => sum + n, 0);
}

function timeoutMs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
}

async function onToggle(plugin: PluginView) {
  try {
    await plugins.setEnabled(plugin.id, !plugin.enabled);
  } catch (err) {
    window.alert((err as Error).message || "切换插件失败");
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      @click.self="open = false"
    >
      <div
        class="max-h-[92vh] w-full max-w-3xl overflow-y-auto rounded-2xl border border-ink/15 bg-surface p-6 shadow-xl"
        @click.stop
      >
        <h2 class="text-lg font-semibold text-ink">插件层与记忆分层</h2>
        <p class="mt-1 text-xs text-ink-muted">
          每个部件都是一个插件：层顺序决定谁生效，注册在卸载时撤销。插件层只能收紧安全策略——添加 deny /
          high-risk 模式、降低上限、收窄工作区根，内置地板不可移除。
        </p>

        <label class="mt-4 block text-xs font-medium text-ink-muted">项目（记忆作用域）</label>
        <select
          v-model="projectId"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        >
          <option value="">全局（未分配项目）</option>
          <option v-for="p in chat.projects" :key="p.id" :value="p.id">{{ p.title }}</option>
        </select>

        <div v-if="plugins.lastError" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          {{ plugins.lastError }}
        </div>

        <div class="mt-4 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
          <span class="rounded-lg bg-surface-muted px-2 py-1">挂载 {{ mounted.length }} / {{ plugins.plugins.length }}</span>
          <span v-if="plugins.config" class="rounded-lg bg-surface-muted px-2 py-1">层 {{ plugins.config.layers }}</span>
          <span v-if="plugins.config" class="truncate">{{ plugins.config.home }}</span>
        </div>

        <ul class="mt-3 space-y-2">
          <li
            v-for="p in plugins.plugins"
            :key="p.id"
            class="rounded-xl border border-ink/12 bg-surface-muted/40 p-3"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-sm font-medium text-ink">{{ p.name || p.id }}</span>
                  <span class="rounded bg-ink/10 px-1.5 py-0.5 text-xs text-ink-muted">层 {{ p.layer || 0 }}</span>
                  <span v-if="p.version" class="text-xs text-ink-muted">v{{ p.version }}</span>
                  <span
                    class="rounded px-1.5 py-0.5 text-xs"
                    :class="p.enabled ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300' : 'bg-ink/10 text-ink-muted'"
                  >
                    {{ p.enabled ? "已挂载" : "未挂载" }}
                  </span>
                </div>
                <p v-if="p.description" class="mt-1 text-xs text-ink-muted">{{ p.description }}</p>
                <div class="mt-1 flex flex-wrap gap-1 text-xs text-ink-muted">
                  <span v-for="s in p.seams" :key="s" class="rounded bg-surface px-1.5 py-0.5">{{ s }}</span>
                </div>
                <div class="mt-1 text-xs text-ink-muted">
                  工具 {{ (p.tools ?? []).length }} · 技能 {{ (p.skills ?? []).length }} · 子代理
                  {{ (p.subagents ?? []).length }} · hook {{ hookTotal(p) }} · patch {{ (p.patch ?? []).length }}
                </div>
                <ul v-if="p.problems?.length" class="mt-1 list-disc pl-4 text-xs text-amber-700 dark:text-amber-300">
                  <li v-for="problem in p.problems" :key="problem">{{ problem }}</li>
                </ul>
              </div>
              <button
                type="button"
                class="shrink-0 rounded-lg border border-ink/15 px-3 py-1.5 text-xs font-medium text-ink transition hover:bg-ink/10"
                @click="onToggle(p)"
              >
                {{ p.enabled ? "卸载" : "挂载" }}
              </button>
            </div>
          </li>
          <li v-if="!plugins.plugins.length" class="text-xs text-ink-muted">
            没有插件目录。在 <code class="rounded bg-surface-muted px-1">.clawmind/plugins/&lt;id&gt;/plugin.json</code>
            写入一个 manifest 后刷新。
          </li>
        </ul>

        <div v-if="plugins.config" class="mt-4">
          <h3 class="text-sm font-semibold text-ink">解析后的层配置</h3>
          <p class="mt-1 text-xs text-ink-muted">只有运行时真正读取的 key 会出现在这里。</p>
          <div class="mt-2 space-y-1 text-xs">
            <div v-for="seam in seamOrder" :key="seam" class="flex gap-2">
              <span class="w-16 shrink-0 font-medium text-ink-muted">{{ seam }}</span>
              <span class="min-w-0 break-all text-ink-muted">
                <template v-if="Object.keys(plugins.config.config[seam] ?? {}).length">
                  <span v-for="(value, key) in plugins.config.config[seam]" :key="key" class="mr-2">
                    <code class="rounded bg-surface px-1">{{ key }}</code>={{ value }}
                  </span>
                </template>
                <span v-else>—</span>
              </span>
            </div>
          </div>
        </div>

        <div class="mt-4">
          <h3 class="text-sm font-semibold text-ink">记忆分层（L0 → L3）</h3>
          <p class="mt-1 text-xs text-ink-muted">
            达到上限的层会把其余上下文挤出去，所以 census 是值得一看的东西。
          </p>
          <div v-if="plugins.census?.note" class="mt-2 text-xs text-ink-muted">{{ plugins.census.note }}</div>
          <ul v-if="plugins.census" class="mt-2 space-y-1 text-xs">
            <li v-for="row in plugins.census.layers" :key="row.layer" class="flex items-center gap-2">
              <span class="w-24 shrink-0 font-medium text-ink">{{ row.label }} {{ row.name }}</span>
              <span class="h-1.5 w-24 shrink-0 rounded bg-ink/10">
                <span
                  class="block h-full rounded"
                  :class="row.count >= row.cap ? 'bg-red-500/70' : 'bg-accent/70'"
                  :style="{ width: `${Math.min(100, row.cap ? (row.count / row.cap) * 100 : 0)}%` }"
                />
              </span>
              <span class="text-ink-muted">{{ row.count }} / {{ row.cap }}</span>
            </li>
          </ul>
          <div v-if="plugins.census?.budget" class="mt-2 flex flex-wrap gap-2 text-xs text-ink-muted">
            <span class="rounded bg-surface-muted px-2 py-1">条目 {{ plugins.census.budget.MaxItems }}</span>
            <span class="rounded bg-surface-muted px-2 py-1">字符 {{ plugins.census.budget.CharBudget }}</span>
            <span class="rounded bg-surface-muted px-2 py-1">超时 {{ timeoutMs(plugins.census.budget.Timeout / 1e6) }}</span>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
