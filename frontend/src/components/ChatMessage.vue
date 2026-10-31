<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Message, Part, PartType } from "@/types/chat";
import MarkdownMessage from "./MarkdownMessage.vue";
import Icon from "./Icon.vue";
import { useChatStore } from "@/stores/chat";
import { parentKeyForMessage, userTurnVersions, userVersionIndex } from "@/lib/thread";

const props = defineProps<{ message: Message }>();
const store = useChatStore();

const editing = ref(false);
const draft = ref("");

watch(
  () => props.message.id,
  () => {
    editing.value = false;
  }
);

const isStreamingThis = computed(
  () => props.message.role === "assistant" && store.streamingMessageId === props.message.id
);

const liveSections = computed(() => {
  if (!isStreamingThis.value) return null;
  const texts = store.streamingPartTexts[props.message.id];
  const types = store.streamingPartTypes[props.message.id];
  if (!texts || Object.keys(texts).length === 0) return null;
  const idxs = Object.keys(texts)
    .map(Number)
    .sort((a, b) => a - b);
  return idxs.map((i) => ({
    index: i,
    type: types?.[i] ?? ("text" as PartType),
    text: texts[i] ?? "",
  }));
});

const liveTools = computed(() => store.streamingToolSteps[props.message.id] ?? []);

/** 任务流程流式：仅在 partIndex 0 输出时展开，进入后续段后折叠 */
function taskFlowStreamOpen(): boolean {
  if (!isStreamingThis.value || !liveSections.value) return false;
  return !liveSections.value.some((s) => s.index > 0);
}

function sectionTaskFlowOpen(sec: { type: PartType }): boolean | undefined {
  if (sec.type !== "task_flow") return undefined;
  if (isStreamingThis.value) return taskFlowStreamOpen();
  return undefined;
}

/** 执行图：图循环的轨迹是本轮的主要产出，流式期间保持展开 */
function sectionGraphOpen(sec: { type: PartType }): boolean | undefined {
  if (sec.type !== "graph") return undefined;
  return isStreamingThis.value;
}

const persistedBlocks = computed(() => {
  const m = props.message;
  if (m.role !== "assistant") return [];
  const blocks: Array<{ kind: "part"; part: Part } | { kind: "tools"; steps: typeof liveTools.value }> = [];
  const parts = m.parts;
  let i = 0;
  while (i < parts.length) {
    const p = parts[i];
    if (p.type === "tool_call") {
      const steps: Array<{ id: string; name: string; args: string; result?: string }> = [];
      while (i < parts.length && parts[i].type === "tool_call") {
        const tc = parts[i];
        const tr = i + 1 < parts.length && parts[i + 1].type === "tool_result" ? parts[i + 1] : null;
        steps.push({
          id: tc.tool_call_id || tc.name || "",
          name: tc.name || "?",
          args: tc.arguments || "",
          result: tr?.result,
        });
        i += tr ? 2 : 1;
      }
      if (steps.length) blocks.push({ kind: "tools", steps });
      continue;
    }
    if (p.type === "tool_result") {
      i++;
      continue;
    }
    blocks.push({ kind: "part", part: p });
    i++;
  }
  return blocks;
});

/* ---------------------------------------------------------------- 工具步骤的展示增强（代码工程） */

/** 工具名 → sprite 图标 */
const TOOL_ICONS: Record<string, string> = {
  code_search: "search",
  repo_tree: "folder",
  file_read: "eye",
  file_write: "notebook-pen",
  file_edit: "edit",
  shell_exec: "tool",
  run_tests: "check",
  web_fetch: "external",
  task_plan: "layers",
  task_summary: "report",
  load_skill: "box",
  delegate: "experts",
  code_graph: "boxes",
};

function toolIcon(name: string): string {
  return TOOL_ICONS[name] ?? "tool";
}

/** 从工具参数里提取一个可读预览（路径 / 查询 / 命令） */
function toolPreview(name: string, args: string): string {
  let obj: Record<string, unknown> = {};
  try {
    obj = JSON.parse(args || "{}") as Record<string, unknown>;
  } catch {
    return (args || "").slice(0, 160);
  }
  const pick = (...keys: string[]) => {
    for (const k of keys) {
      const v = obj[k];
      if (typeof v === "string" && v) return v;
    }
    return "";
  };
  switch (name) {
    case "file_read":
    case "file_write":
    case "file_edit":
      return pick("path", "file", "filename") || "—";
    case "code_search":
      return pick("query", "pattern") || "—";
    case "code_graph":
      return pick("symbol", "query", "mode") || "—";
    case "repo_tree":
      return pick("path") || "仓库结构";
    case "run_tests":
      return pick("command") || "推断的测试命令";
    case "shell_exec":
      return pick("command", "cmd") || "—";
    case "web_fetch":
      return pick("url") || "—";
    case "load_skill":
      return pick("name", "skill") || "—";
    case "delegate":
      return pick("task", "prompt") || "—";
    default:
      return pick("path", "query", "command", "url", "name") || (args || "").slice(0, 120);
  }
}

type ToolState = "running" | "done" | "error";

/** 工具状态：无结果 = 运行中；结果含失败标记 = error */
function toolState(step: { result?: string }): ToolState {
  if (step.result === undefined) return "running";
  const r = step.result;
  if (/^\s*(error|失败|exit code [1-9])/i.test(r) || /\bFAIL\b/.test(r)) return "error";
  return "done";
}

const TOOL_STATE_BADGE: Record<ToolState, { cls: string; text: string }> = {
  running: { cls: "badge--accent badge--live", text: "运行中" },
  done: { cls: "badge--ok", text: "完成" },
  error: { cls: "badge--danger", text: "失败" },
};

function toolBadge(step: { name: string; result?: string }) {
  if (step.name === "run_tests" && step.result !== undefined) {
    const failed = toolState(step) === "error";
    return failed
      ? { cls: "badge--danger", text: "测试未通过" }
      : { cls: "badge--ok", text: "测试通过" };
  }
  return TOOL_STATE_BADGE[toolState(step)];
}

/* ---------------------------------------------------------------- 用户消息：复制 / 编辑 / 版本 */

function userPlainText(m: Message): string {
  return m.parts
    .filter((p) => p.type === "text" || p.type === "code")
    .map((p) => (p.type === "code" ? "```\n" + (p.text || "") + "\n```" : p.text || ""))
    .join("");
}

const userText = computed(() => {
  const m = props.message;
  if (m.role !== "user") return "";
  return userPlainText(m) || m.parts[0]?.text || "";
});

const versionList = computed(() => userTurnVersions([...store.messages], props.message));
const vIndex0 = computed(() => userVersionIndex(versionList.value, props.message));
const versionDisplayX = computed(() => vIndex0.value + 1);
const versionCount = computed(() => versionList.value.length);
const branchParentKey = computed(() => parentKeyForMessage(props.message.parentMessageId));

async function copyUserText() {
  try {
    await navigator.clipboard.writeText(userText.value);
  } catch {
    /* ignore */
  }
}

function startEditUser() {
  draft.value = userText.value;
  editing.value = true;
}

function cancelEditUser() {
  editing.value = false;
}

async function submitEditUser() {
  const t = draft.value.trim();
  if (!t) return;
  editing.value = false;
  await store.sendMessage(t, { editOfUserMessageId: props.message.id });
}

function prevUserVersion() {
  const list = versionList.value;
  const i = vIndex0.value;
  if (i <= 0) return;
  void store.selectBranchUserVersion(branchParentKey.value, list[i - 1]!.id);
}

function nextUserVersion() {
  const list = versionList.value;
  const i = vIndex0.value;
  if (i >= list.length - 1) return;
  void store.selectBranchUserVersion(branchParentKey.value, list[i + 1]!.id);
}

/* ---------------------------------------------------------------- 助手消息：复制 / 重新生成 / 导出 */

function assistantExportMarkdown(m: Message): string {
  const chunks: string[] = [];
  for (const p of m.parts) {
    if (p.type === "text" || p.type === "task_flow" || p.type === "graph") {
      if (p.text) chunks.push(p.text);
    } else if (p.type === "code") {
      chunks.push("```" + (p.language || "") + "\n" + (p.text || "") + "\n```");
    }
  }
  return chunks.join("\n\n").trim();
}

async function copyAssistantMarkdown() {
  const md = assistantExportMarkdown(props.message);
  try {
    await navigator.clipboard.writeText(md || "(空)");
  } catch {
    /* ignore */
  }
}

function exportAssistantFile() {
  const md = assistantExportMarkdown(props.message);
  const blob = new Blob([md || ""], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `clawmind-${props.message.id.slice(0, 8)}.md`;
  a.click();
  URL.revokeObjectURL(url);
}

function onRegenerateAssistant() {
  void store.regenerateAssistant(props.message.id);
}

const fallbackStreamText = computed(() => {
  if (!isStreamingThis.value) return "";
  return store.streamingBuffers[props.message.id] ?? "";
});

/** 流式正文的最后一段加打字光标 */
function isLastTextSection(idx: number): boolean {
  if (!liveSections.value) return false;
  const textIdxs = liveSections.value.filter((s) => s.type === "text").map((s) => s.index);
  return textIdxs.length > 0 && textIdxs[textIdxs.length - 1] === idx;
}

const sectionLabel: Record<string, string> = {
  task_flow: "任务流程",
  thinking: "思考",
  graph: "执行图",
  reasoning: "评审",
  text: "回答",
};
</script>

<template>
  <!-- 用户 -->
  <div v-if="message.role === 'user'" class="you">
    <div class="you__bubble">
      <template v-if="editing">
        <textarea
          v-model="draft"
          rows="3"
          class="input input--area w-full"
          @keydown.esc="cancelEditUser"
        />
        <div class="mt-2 flex justify-end gap-2">
          <button type="button" class="btn btn--ghost btn--sm" @click="cancelEditUser">取消</button>
          <button
            type="button"
            class="btn btn--primary btn--sm"
            :disabled="store.sending"
            @click="submitEditUser"
          >
            发送
          </button>
        </div>
      </template>
      <div v-else>{{ userText }}</div>
    </div>
    <div class="you__actions">
      <span v-if="!editing && versionCount > 1" class="you__time num">
        {{ versionDisplayX }} / {{ versionCount }}
      </span>
      <button
        v-if="!editing && versionCount > 1"
        type="button"
        class="icon-btn icon-btn--sm"
        :disabled="versionDisplayX <= 1"
        aria-label="上一版本"
        @click="prevUserVersion"
      >
        <Icon name="chevron" size="sm" class="rotate-90" />
      </button>
      <button
        v-if="!editing && versionCount > 1"
        type="button"
        class="icon-btn icon-btn--sm"
        :disabled="versionDisplayX >= versionCount"
        aria-label="下一版本"
        @click="nextUserVersion"
      >
        <Icon name="chevron" size="sm" class="-rotate-90" />
      </button>
      <button
        v-if="!editing"
        type="button"
        class="icon-btn icon-btn--sm"
        title="复制"
        aria-label="复制提问"
        @click="copyUserText"
      >
        <Icon name="copy" size="sm" />
      </button>
      <button
        v-if="!editing"
        type="button"
        class="icon-btn icon-btn--sm"
        title="修改"
        aria-label="修改提问"
        @click="startEditUser"
      >
        <Icon name="edit" size="sm" />
      </button>
    </div>
  </div>

  <!-- 助手 -->
  <div v-else class="answer">
    <!-- 流式 -->
    <template v-if="isStreamingThis && liveSections && liveSections.length > 0">
      <div
        class="answer__body"
        :class="{
          'is-streaming': liveSections.some((s) => s.type === 'text' && s.text.length > 0),
        }"
      >
        <template v-for="sec in liveSections" :key="'s' + sec.index">
          <details
            v-if="sec.type === 'task_flow' || sec.type === 'graph' || sec.type === 'thinking'"
            class="fold"
            :class="{ 'is-live': sec.type !== 'thinking' }"
            :open="sectionTaskFlowOpen(sec) ?? sectionGraphOpen(sec)"
          >
            <summary>
              <Icon
                name="chevron"
                size="sm"
                class="chev"
                :class="{ 'is-closed': !(sectionTaskFlowOpen(sec) ?? sectionGraphOpen(sec)) }"
              />
              <span class="fold__title">{{ sectionLabel[sec.type] ?? sec.type }}</span>
            </summary>
            <div class="fold__body">
              <MarkdownMessage :source="sec.text" />
            </div>
          </details>
          <div
            v-else-if="sec.type === 'text'"
            class="min-w-0"
            :class="{ 'answer__last': isLastTextSection(sec.index) }"
          >
            <MarkdownMessage :source="sec.text" />
          </div>
          <pre
            v-else
            class="tool__pre"
          >{{ sec.text }}</pre>
        </template>
      </div>
      <!-- 流式工具步骤 -->
      <template v-if="liveTools.length > 0">
        <details
          v-for="t in liveTools"
          :key="t.id"
          class="tool"
          :data-state="toolState(t)"
          :open="t.result === undefined"
        >
          <summary>
            <Icon :name="toolIcon(t.name)" size="sm" />
            <span class="tool__name">{{ t.name }}</span>
            <span class="tool__preview">{{ toolPreview(t.name, t.args) }}</span>
            <span class="badge" :class="toolBadge(t).cls">{{ toolBadge(t).text }}</span>
            <Icon name="chevron" size="sm" class="chev is-closed" />
          </summary>
          <div class="tool__body">
            <div class="tool__label">参数</div>
            <pre class="tool__pre">{{ t.args || "—" }}</pre>
            <template v-if="t.result !== undefined">
              <div class="tool__label">结果</div>
              <pre class="tool__pre">{{ t.result }}</pre>
            </template>
          </div>
        </details>
      </template>
      <div v-else class="answer__wait"><span class="dot-pulse" /></div>
    </template>

    <MarkdownMessage v-else-if="isStreamingThis && fallbackStreamText" :source="fallbackStreamText" />

    <!-- 持久化 -->
    <div v-else class="answer__body">
      <template v-for="(block, bi) in persistedBlocks" :key="bi">
        <template v-if="block.kind === 'part'">
          <details
            v-if="block.part.type === 'task_flow' || block.part.type === 'graph' || block.part.type === 'thinking'"
            class="fold"
          >
            <summary>
              <Icon name="chevron" size="sm" class="chev is-closed" />
              <span>{{ sectionLabel[block.part.type] ?? block.part.type }}</span>
            </summary>
            <div class="fold__body">
              <MarkdownMessage :source="block.part.text || ''" />
            </div>
          </details>
          <div v-else-if="block.part.type === 'text'" class="min-w-0">
            <MarkdownMessage :source="block.part.text || ''" />
          </div>
          <pre v-else class="tool__pre">{{ block.part.name }}({{ block.part.arguments }})</pre>
        </template>
        <!-- 工具步骤组 -->
        <template v-if="block.kind === 'tools'">
          <details v-for="t in block.steps" :key="t.id" class="tool" :data-state="toolState(t)">
            <summary>
              <Icon :name="toolIcon(t.name)" size="sm" />
              <span class="tool__name">{{ t.name }}</span>
              <span class="tool__preview">{{ toolPreview(t.name, t.args) }}</span>
              <span class="badge" :class="toolBadge(t).cls">{{ toolBadge(t).text }}</span>
              <Icon name="chevron" size="sm" class="chev is-closed" />
            </summary>
            <div class="tool__body">
              <div class="tool__label">参数</div>
              <pre class="tool__pre">{{ t.args || "—" }}</pre>
              <template v-if="t.result">
                <div class="tool__label">结果</div>
                <pre class="tool__pre">{{ t.result }}</pre>
              </template>
            </div>
          </details>
        </template>
      </template>
    </div>

    <!-- 操作条 -->
    <div v-if="!isStreamingThis" class="answer__actions">
      <button
        type="button"
        class="icon-btn icon-btn--sm"
        title="复制"
        aria-label="复制回复"
        @click="copyAssistantMarkdown"
      >
        <Icon name="copy" size="sm" />
      </button>
      <button
        type="button"
        class="icon-btn icon-btn--sm"
        title="重新生成"
        aria-label="重新生成"
        :disabled="store.sending"
        @click="onRegenerateAssistant"
      >
        <Icon name="refresh" size="sm" />
      </button>
      <button
        type="button"
        class="icon-btn icon-btn--sm"
        title="导出为 Markdown"
        aria-label="导出"
        @click="exportAssistantFile"
      >
        <Icon name="download" size="sm" />
      </button>
    </div>
  </div>
</template>
