<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { usePluginStore } from "@/stores/plugins";
import type { PluginView } from "@/types/plugins";
import Icon from "./Icon.vue";

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
    <div v-if="open" class="dialog-scrim" @click.self="open = false">
      <div class="dialog dialog--wide" role="dialog" aria-label="插件层与记忆" @click.stop>
        <div class="dialog__head">
          <h2>插件层与记忆分层</h2>
          <button type="button" class="icon-btn" title="关闭" aria-label="关闭" @click="open = false">
            <Icon name="close" />
          </button>
        </div>
        <div class="dialog__body">
          <p class="mt-0 text-xs text-ink-muted">
            每个部件都是一个插件：层顺序决定谁生效，注册在卸载时撤销。插件层只能收紧安全策略——添加 deny /
            high-risk 模式、降低上限、收窄工作区根，内置地板不可移除。
          </p>

          <div class="field mt-4 max-w-xs">
            <label class="field__label" for="plug-project">项目（记忆作用域）</label>
            <select id="plug-project" v-model="projectId" class="input">
              <option value="">全局（未分配项目）</option>
              <option v-for="p in chat.projects" :key="p.id" :value="p.id">{{ p.title }}</option>
            </select>
          </div>

          <div v-if="plugins.lastError" class="callout callout--danger mt-3">
            <Icon name="warning" size="sm" />
            <span>{{ plugins.lastError }}</span>
          </div>

          <div class="mt-4 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
            <span class="badge">挂载 {{ mounted.length }} / {{ plugins.plugins.length }}</span>
            <span v-if="plugins.config" class="badge badge--accent">层 {{ plugins.config.layers }}</span>
            <span v-if="plugins.config" class="mono truncate">{{ plugins.config.home }}</span>
          </div>

          <ul class="mt-3 space-y-2">
            <li
              v-for="p in plugins.plugins"
              :key="p.id"
              class="rounded-xl border border-line bg-surface-2/50 p-3"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="text-sm font-medium text-ink">{{ p.name || p.id }}</span>
                    <span class="badge badge--muted">层 {{ p.layer || 0 }}</span>
                    <span v-if="p.version" class="text-xs text-ink-muted">v{{ p.version }}</span>
                    <span class="badge" :class="p.enabled ? 'badge--ok' : 'badge--muted'">
                      {{ p.enabled ? "已挂载" : "未挂载" }}
                    </span>
                  </div>
                  <p v-if="p.description" class="mt-1 text-xs text-ink-muted">{{ p.description }}</p>
                  <div class="mt-1.5 flex flex-wrap gap-1">
                    <span v-for="s in p.seams" :key="s" class="tag">{{ s }}</span>
                  </div>
                  <div class="mt-1.5 text-xs text-ink-muted">
                    工具 {{ (p.tools ?? []).length }} · 技能 {{ (p.skills ?? []).length }} · 子代理
                    {{ (p.subagents ?? []).length }} · hook {{ hookTotal(p) }} · patch {{ (p.patch ?? []).length }}
                  </div>
                  <ul v-if="p.problems?.length" class="callout callout--warn mt-2 block !gap-1 text-xs">
                    <li v-for="problem in p.problems" :key="problem" class="ml-4 list-disc">
                      {{ problem }}
                    </li>
                  </ul>
                </div>
                <button
                  type="button"
                  class="btn btn--secondary btn--sm shrink-0"
                  @click="onToggle(p)"
                >
                  {{ p.enabled ? "卸载" : "挂载" }}
                </button>
              </div>
            </li>
            <li v-if="!plugins.plugins.length" class="text-xs text-ink-muted">
              没有插件目录。在
              <code class="mono rounded bg-surface-2 px-1">.clawmind/plugins/&lt;id&gt;/plugin.json</code>
              写入一个 manifest 后刷新。
            </li>
          </ul>

          <div v-if="plugins.config" class="mt-5">
            <h3 class="text-sm font-semibold text-ink">解析后的层配置</h3>
            <p class="mt-1 text-xs text-ink-muted">只有运行时真正读取的 key 会出现在这里。</p>
            <div class="mt-2 space-y-1 text-xs">
              <div v-for="seam in seamOrder" :key="seam" class="flex gap-2">
                <span class="mono w-16 shrink-0 font-medium text-ink-muted">{{ seam }}</span>
                <span class="min-w-0 break-all text-ink-muted">
                  <template v-if="Object.keys(plugins.config.config[seam] ?? {}).length">
                    <span v-for="(value, key) in plugins.config.config[seam]" :key="key" class="mr-2">
                      <code class="mono rounded bg-surface-2 px-1">{{ key }}</code>={{ value }}
                    </span>
                  </template>
                  <span v-else>—</span>
                </span>
              </div>
            </div>
          </div>

          <div class="mt-5">
            <h3 class="text-sm font-semibold text-ink">记忆分层（L0 → L3）</h3>
            <p class="mt-1 text-xs text-ink-muted">
              达到上限的层会把其余上下文挤出去，所以 census 是值得一看的东西。
            </p>
            <div v-if="plugins.census?.note" class="mt-2 text-xs text-ink-muted">
              {{ plugins.census.note }}
            </div>
            <ul v-if="plugins.census" class="mt-2 space-y-1.5 text-xs">
              <li v-for="row in plugins.census.layers" :key="row.layer" class="flex items-center gap-3">
                <span class="w-28 shrink-0 font-medium text-ink">{{ row.label }} {{ row.name }}</span>
                <span class="meter-bar flex-1">
                  <span
                    :class="{ 'is-full': row.count >= row.cap }"
                    :style="{ width: `${Math.min(100, row.cap ? (row.count / row.cap) * 100 : 0)}%` }"
                  />
                </span>
                <span class="num w-20 shrink-0 text-right text-ink-muted">
                  {{ row.count }} / {{ row.cap }}
                </span>
              </li>
            </ul>
            <div v-if="plugins.census?.budget" class="mt-2 flex flex-wrap gap-2 text-xs text-ink-muted">
              <span class="badge">条目 {{ plugins.census.budget.MaxItems }}</span>
              <span class="badge">字符 {{ plugins.census.budget.CharBudget }}</span>
              <span class="badge">超时 {{ timeoutMs(plugins.census.budget.Timeout / 1e6) }}</span>
            </div>
          </div>
        </div>
        <div class="dialog__actions">
          <button type="button" class="btn btn--secondary" @click="open = false">关闭</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
