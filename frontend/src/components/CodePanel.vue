<script setup lang="ts">
import { computed, watch } from "vue";
import { useCodeStore } from "@/stores/code";
import Icon from "./Icon.vue";

const open = defineModel<boolean>("open", { default: false });
const code = useCodeStore();

watch(open, (v) => {
  if (v && !code.view && !code.loading) void code.refresh();
});

const view = computed(() => code.view);

/** 语言分布条：按 bytes 占比 */
const langRows = computed(() => {
  const langs = view.value?.profile?.languages ?? [];
  const total = langs.reduce((s, l) => s + l.bytes, 0) || 1;
  return [...langs]
    .sort((a, b) => b.bytes - a.bytes)
    .map((l) => ({ ...l, pct: Math.max(2, Math.round((l.bytes / total) * 100)) }));
});

const commands = computed(() => {
  const c = view.value?.profile?.commands ?? {};
  return [
    { key: "test", label: "test", cmd: c.test },
    { key: "build", label: "build", cmd: c.build },
    { key: "lint", label: "lint", cmd: c.lint },
    { key: "format", label: "format", cmd: c.format },
  ].filter((r) => r.cmd);
});

function fmtBytes(n: number): string {
  if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MB";
  if (n >= 1024) return (n / 1024).toFixed(1) + " KB";
  return n + " B";
}

async function copyCmd(cmd: string) {
  try {
    await navigator.clipboard.writeText(cmd);
  } catch {
    /* ignore */
  }
}

const classBadge = computed(() =>
  view.value?.class === "complex"
    ? { cls: "badge--accent", text: "complex · 图循环" }
    : { cls: "badge--ok", text: "simple · 单遍" }
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="dialog-scrim" @click.self="open = false">
      <div class="dialog dialog--wide" role="dialog" aria-label="代码工作区" @click.stop>
        <div class="dialog__head">
          <h2>代码工作区</h2>
          <div class="flex items-center gap-1">
            <button
              type="button"
              class="icon-btn"
              title="重建画像与符号图索引"
              aria-label="重建索引"
              :disabled="code.refreshing"
              @click="code.reindex()"
            >
              <Icon name="refresh" :class="code.refreshing ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="icon-btn" title="关闭" aria-label="关闭" @click="open = false">
              <Icon name="close" />
            </button>
          </div>
        </div>

        <div class="dialog__body">
          <p class="mt-0 text-xs text-ink-muted">
            分级把工作区与请求一起打分，决定本轮的循环形状与预算；索引按
            <code class="mono rounded bg-surface-2 px-1">size@mtime</code> 增量刷新，不 shell out。
          </p>

          <div v-if="code.lastError" class="callout callout--danger mt-3">
            <Icon name="warning" size="sm" />
            <span>{{ code.lastError }}</span>
          </div>

          <div v-if="code.loading && !view" class="py-10 text-center text-sm text-ink-muted">
            正在读取工作区…
          </div>

          <template v-if="view">
            <!-- 分级 + 路径 -->
            <div class="ws-card mt-3">
              <span class="cm-mark h-10 w-10 text-lg">
                <Icon name="code" />
              </span>
              <div class="ws-card__text">
                <div class="flex flex-wrap items-center gap-2">
                  <b class="text-base font-semibold text-ink">{{ view.workspace }}</b>
                  <span class="badge" :class="classBadge.cls">{{ classBadge.text }}</span>
                  <span v-if="view.profile?.branch" class="badge badge--muted">
                    <Icon name="swap" size="sm" />{{ view.profile.branch }}
                  </span>
                </div>
                <div class="ws-card__facts">
                  <span>评分<b class="num">{{ view.score }}</b></span>
                  <span>循环<b>{{ view.loop === "graph" ? "graph" : "linear" }}</b></span>
                  <span>已扫描<b class="num">{{ view.profile?.scanned ?? "—" }}</b></span>
                  <span>测试文件<b class="num">{{ view.profile?.testFiles ?? 0 }}</b></span>
                  <span>CI<b>{{ view.profile?.hasCI ? "有" : "无" }}</b></span>
                </div>
              </div>
            </div>

            <!-- 分级理由 -->
            <h3 class="mt-5 text-sm font-semibold text-ink">分级理由</h3>
            <ul class="mt-2 flex flex-wrap gap-1.5">
              <li v-for="r in view.reasons" :key="r" class="badge badge--muted">{{ r }}</li>
              <li v-if="!view.reasons.length" class="text-xs text-ink-muted">无</li>
            </ul>

            <!-- 预算 -->
            <h3 class="mt-5 text-sm font-semibold text-ink">本轮预算</h3>
            <div class="stat-grid mt-2">
              <div class="stat"><b class="num">{{ view.budget.maxNodes }}</b><span>最大节点</span></div>
              <div class="stat"><b class="num">{{ view.budget.maxRounds }}</b><span>最大轮数</span></div>
              <div class="stat"><b class="num">{{ view.budget.reentry }}</b><span>回边次数上限</span></div>
              <div class="stat"><b class="num">{{ view.budget.fanout }}</b><span>扇出上限</span></div>
              <div class="stat"><b class="num">{{ view.budget.searchFiles }}</b><span>搜索文件上限</span></div>
              <div class="stat"><b class="num">{{ view.budget.searchResults }}</b><span>搜索结果上限</span></div>
              <div class="stat"><b class="num">{{ view.budget.contextTokens }}</b><span>上下文 token</span></div>
              <div class="stat"><b class="num">{{ view.budget.promptChars }}</b><span>提示字符</span></div>
            </div>
            <div class="mt-2 flex flex-wrap gap-1.5">
              <span class="badge" :class="view.budget.graph ? 'badge--accent' : 'badge--muted'">
                符号图 {{ view.budget.graph ? "开" : "关" }}
              </span>
              <span class="badge" :class="view.budget.verify ? 'badge--ok' : 'badge--muted'">
                改动后验证 {{ view.budget.verify ? "开" : "关" }}
              </span>
              <span class="badge" :class="view.budget.review ? 'badge--info' : 'badge--muted'">
                评审节点 {{ view.budget.review ? "开" : "关" }}
              </span>
            </div>

            <!-- 语言分布 -->
            <template v-if="view.profile">
              <h3 class="mt-5 text-sm font-semibold text-ink">语言分布</h3>
              <ul class="mt-2 space-y-1.5">
                <li v-for="l in langRows" :key="l.name" class="flex items-center gap-3 text-xs">
                  <span class="w-24 shrink-0 font-medium text-ink">{{ l.name }}</span>
                  <span class="meter-bar flex-1"><span :style="{ width: l.pct + '%' }" /></span>
                  <span class="w-28 shrink-0 text-right num text-ink-muted">
                    {{ l.files }} 文件 · {{ fmtBytes(l.bytes) }}
                  </span>
                </li>
                <li v-if="!langRows.length" class="text-xs text-ink-muted">未检测到语言。</li>
              </ul>

              <!-- 推断命令 -->
              <h3 class="mt-5 text-sm font-semibold text-ink">推断的验证命令</h3>
              <p class="mt-1 text-xs text-ink-muted">
                任何改动文件的轮次后自动执行 test；高危命令仍需人工确认。
              </p>
              <ul class="mt-2 space-y-1.5">
                <li
                  v-for="r in commands"
                  :key="r.key"
                  class="flex items-center gap-2 rounded-lg bg-surface-2 px-3 py-2"
                >
                  <span class="badge badge--accent shrink-0">{{ r.label }}</span>
                  <code class="mono min-w-0 flex-1 truncate text-xs text-ink" :title="r.cmd">{{
                    r.cmd
                  }}</code>
                  <button
                    type="button"
                    class="icon-btn icon-btn--sm"
                    title="复制命令"
                    aria-label="复制命令"
                    @click="copyCmd(r.cmd!)"
                  >
                    <Icon name="copy" size="sm" />
                  </button>
                </li>
                <li v-if="!commands.length" class="text-xs text-ink-muted">没有可推断的命令。</li>
              </ul>

              <!-- Manifest 与目录树 -->
              <div class="mt-4 grid gap-4 md:grid-cols-2">
                <div>
                  <h3 class="text-sm font-semibold text-ink">Manifest</h3>
                  <ul class="mt-2 flex flex-wrap gap-1.5">
                    <li v-for="m in view.profile.manifests" :key="m" class="tag">{{ m }}</li>
                    <li v-if="!view.profile.manifests.length" class="text-xs text-ink-muted">无</li>
                  </ul>
                </div>
                <div>
                  <h3 class="text-sm font-semibold text-ink">
                    目录树
                    <span v-if="view.profile.truncated" class="text-xs font-normal text-warn-text"
                      >（有截断）</span
                    >
                  </h3>
                  <pre
                    class="mono mt-2 max-h-40 overflow-auto rounded-lg bg-code-bg p-3 text-[11.5px] leading-relaxed text-code-ink"
                    >{{ view.profile.tree.join("\n") || "—" }}</pre
                  >
                </div>
              </div>
            </template>

            <!-- 符号图 -->
            <template v-if="view.graph">
              <h3 class="mt-5 text-sm font-semibold text-ink">
                符号图
                <span class="ml-1 text-xs font-normal text-ink-muted">
                  {{ view.graph.enabled ? "已启用" : "已禁用" }}
                  <template v-if="view.graph.builtAt"> · 构建于 {{ view.graph.builtAt }}</template>
                  <template v-if="view.graph.dirty"> · 待刷新</template>
                </span>
              </h3>
              <div class="stat-grid mt-2">
                <div class="stat"><b class="num">{{ view.graph.shape.files }}</b><span>文件</span></div>
                <div class="stat"><b class="num">{{ view.graph.shape.symbols }}</b><span>符号</span></div>
                <div class="stat"><b class="num">{{ view.graph.shape.edges }}</b><span>调用边</span></div>
                <div class="stat"><b class="num">{{ view.graph.shape.modules }}</b><span>模块</span></div>
                <div class="stat"><b class="num">{{ view.graph.shape.maxFanIn }}</b><span>最大入度</span></div>
                <div class="stat">
                  <b class="num" :class="view.graph.shape.cycles > 0 ? 'text-warn-text' : ''">{{
                    view.graph.shape.cycles
                  }}</b
                  ><span>包级循环</span>
                </div>
                <div class="stat"><b class="num">{{ view.graph.parsed }}</b><span>已解析</span></div>
                <div class="stat"><b class="num">{{ view.graph.reused }}</b><span>缓存复用</span></div>
                <div class="stat"><b class="num">{{ view.graph.skipped }}</b><span>跳过</span></div>
              </div>
              <p class="mt-2 text-xs text-ink-muted">
                启发式解析（不做类型检查）：调用点是候选而非确认，影响面用于排序而不是结论。
              </p>
            </template>
          </template>
        </div>

        <div class="dialog__actions">
          <button type="button" class="btn btn--secondary" @click="open = false">关闭</button>
          <button
            type="button"
            class="btn btn--primary"
            :disabled="code.refreshing || code.loading"
            @click="code.refresh()"
          >
            <Icon name="refresh" size="sm" />
            {{ code.refreshing ? "重建中…" : "重新读取" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
