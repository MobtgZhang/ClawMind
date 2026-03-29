<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Message, Part, PartType } from "@/types/chat";
import MarkdownMessage from "./MarkdownMessage.vue";
import { useChatStore } from "@/stores/chat";
import {
  parentKeyForMessage,
  userTurnVersions,
  userVersionIndex,
} from "@/lib/thread";

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

const assistantAvatar = "/clawmind-assistant.png";
const userAvatar = "/clawmind-user.png";

function partLabel(t: PartType): string {
  switch (t) {
    case "task_flow":
      return "任务流程";
    case "thinking":
      return "思考过程";
    case "text":
      return "回答";
    default:
      return t;
  }
}

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

/** 任务流程流式：仅在 partIndex 0 输出时展开，进入思考后折叠 */
function taskFlowStreamOpen(): boolean {
  if (!isStreamingThis.value || !liveSections.value) return false;
  return !liveSections.value.some((s) => s.index > 0);
}

/** 思考流式：思考段开始后、回答/工具前展开 */
function thinkingStreamOpen(): boolean {
  if (!isStreamingThis.value || !liveSections.value) return false;
  if (liveTools.value.length > 0) return false;
  const textSec = liveSections.value.find((s) => s.type === "text");
  if (textSec && textSec.text.length > 0) return false;
  return liveSections.value.some((s) => s.type === "thinking");
}

function sectionTaskFlowOpen(sec: { type: PartType }): boolean | undefined {
  if (sec.type !== "task_flow") return undefined;
  if (isStreamingThis.value) return taskFlowStreamOpen();
  return undefined;
}

function sectionThinkingOpen(sec: { type: PartType }): boolean | undefined {
  if (sec.type !== "thinking") return undefined;
  if (isStreamingThis.value) return thinkingStreamOpen();
  return undefined;
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

const versionList = computed(() =>
  userTurnVersions([...store.messages], props.message)
);
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

function assistantExportMarkdown(m: Message): string {
  const chunks: string[] = [];
  for (const p of m.parts) {
    if (p.type === "text" || p.type === "thinking" || p.type === "task_flow") {
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

const iconBtn =
  "inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-ink-muted transition hover:bg-ink/[0.08] hover:text-ink disabled:pointer-events-none disabled:opacity-30 dark:hover:bg-ink/10";

const fallbackStreamText = computed(() => {
  if (!isStreamingThis.value) return "";
  const buf = store.streamingBuffers[props.message.id];
  if (buf !== undefined && buf !== "") return buf;
  return "";
});

const summaryCls =
  "flex cursor-pointer list-none items-center gap-2 py-2 pl-2 pr-3 text-[11px] font-semibold uppercase tracking-wide text-ink-muted hover:bg-ink/[0.04] [&::-webkit-details-marker]:hidden";
</script>

<template>
  <!-- 用户：气泡 + 外侧右下复制/编辑；编辑态框内取消/发送；多版本时 < X/Y > -->
  <div v-if="message.role === 'user'" class="flex w-full items-end justify-end gap-2.5 py-2.5">
    <div class="flex min-w-0 max-w-[min(100%,85%)] flex-col items-end gap-1.5">
      <div
        class="w-full rounded-[1.2rem] bg-ink/[0.08] px-4 py-2.5 text-[15px] leading-relaxed text-ink dark:bg-ink/[0.14]"
      >
        <template v-if="editing">
          <textarea
            v-model="draft"
            rows="3"
            class="w-full resize-y rounded-lg border border-ink/15 bg-surface px-3 py-2 text-[15px] text-ink outline-none focus:border-accent/50 dark:border-ink/25"
          />
          <div class="mt-2 flex justify-end gap-2">
            <button
              type="button"
              class="rounded-lg px-3 py-1.5 text-xs font-medium text-ink-muted hover:bg-ink/10"
              @click="cancelEditUser"
            >
              取消
            </button>
            <button
              type="button"
              class="rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white hover:opacity-90 disabled:opacity-40"
              :disabled="store.sending"
              @click="submitEditUser"
            >
              发送
            </button>
          </div>
        </template>
        <div v-else class="whitespace-pre-wrap break-words">{{ userText }}</div>
      </div>
      <div class="flex flex-wrap items-center justify-end gap-0.5 pr-0.5">
        <button
          v-if="!editing"
          type="button"
          :class="iconBtn"
          title="复制"
          aria-label="复制提问"
          @click="copyUserText"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <rect x="9" y="9" width="11" height="11" rx="2" />
            <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
          </svg>
        </button>
        <button
          v-if="!editing"
          type="button"
          :class="iconBtn"
          title="修改"
          aria-label="修改提问"
          @click="startEditUser"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path d="M12 20h9M16.5 3.5a2.12 2.12 0 013 3L8 18l-4 1 1-4L16.5 3.5z" />
          </svg>
        </button>
        <template v-if="!editing && versionCount > 1">
          <button
            type="button"
            :class="iconBtn"
            :disabled="versionDisplayX <= 1"
            aria-label="上一版本"
            @click="prevUserVersion"
          >
            <span class="text-sm font-medium">&lt;</span>
          </button>
          <span class="min-w-[3rem] select-none text-center text-xs tabular-nums text-ink-muted">
            {{ versionDisplayX }} / {{ versionCount }}
          </span>
          <button
            type="button"
            :class="iconBtn"
            :disabled="versionDisplayX >= versionCount"
            aria-label="下一版本"
            @click="nextUserVersion"
          >
            <span class="text-sm font-medium">&gt;</span>
          </button>
        </template>
      </div>
    </div>
    <img
      :src="userAvatar"
      width="32"
      height="32"
      class="mb-0.5 h-8 w-8 shrink-0 rounded-full object-cover ring-1 ring-ink/10"
      alt=""
    />
  </div>

  <!-- 助手 -->
  <div v-else class="w-full py-2.5">
    <div class="flex gap-3">
      <img
        :src="assistantAvatar"
        width="32"
        height="32"
        class="mt-0.5 h-8 w-8 shrink-0 rounded-full object-cover ring-1 ring-ink/10"
        alt="ClawMind"
      />
      <div class="min-w-0 flex-1 space-y-2.5 text-[15px] leading-relaxed text-ink">
        <template v-if="isStreamingThis && liveSections && liveSections.length > 0">
          <template v-for="sec in liveSections" :key="'s' + sec.index">
            <details
              v-if="sec.type === 'task_flow'"
              class="group/tf rounded-lg border border-ink/10 bg-surface-muted/50 dark:bg-surface-muted/25"
              :open="sectionTaskFlowOpen(sec)"
            >
              <summary :class="summaryCls">
                <svg
                  class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/tf:rotate-90"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                </svg>
                {{ partLabel(sec.type) }}
              </summary>
              <div class="border-t border-ink/10 px-3 pb-3 pt-2">
                <MarkdownMessage :source="sec.text" />
              </div>
            </details>
            <details
              v-else-if="sec.type === 'thinking'"
              class="group/th rounded-lg border border-ink/10 bg-surface-muted/40 dark:bg-surface-muted/20"
              :open="sectionThinkingOpen(sec)"
            >
              <summary :class="summaryCls">
                <svg
                  class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/th:rotate-90"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                </svg>
                {{ partLabel(sec.type) }}
              </summary>
              <div class="border-t border-ink/10 px-3 pb-3 pt-2">
                <MarkdownMessage :source="sec.text" />
              </div>
            </details>
            <div v-else-if="sec.type === 'text'" class="min-w-0">
              <div class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-ink-muted">
                {{ partLabel(sec.type) }}
              </div>
              <MarkdownMessage :source="sec.text" />
            </div>
            <div
              v-else
              class="rounded-lg border border-ink/10 bg-surface-muted/30 px-3 py-2 font-mono text-xs text-ink-muted"
            >
              {{ sec.text }}
            </div>
          </template>
          <details
            v-if="liveTools.length > 0"
            class="group/to rounded-lg border border-ink/10 bg-surface-muted/40 dark:bg-surface-muted/20"
            :open="isStreamingThis"
          >
            <summary :class="summaryCls">
              <svg
                class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/to:rotate-90"
                viewBox="0 0 24 24"
                fill="currentColor"
              >
                <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
              </svg>
              工具执行
            </summary>
            <div class="border-t border-ink/10 px-3 pb-3 pt-2">
              <div
                v-for="t in liveTools"
                :key="t.id"
                class="mb-2 border-l-2 border-accent/35 pl-2 last:mb-0"
              >
                <div class="font-mono text-xs text-accent">{{ t.name }}</div>
                <pre class="mt-1 max-h-28 overflow-auto text-[11px] text-ink-muted">{{ t.args }}</pre>
                <pre
                  v-if="t.result !== undefined"
                  class="mt-1 max-h-36 overflow-auto rounded-md bg-ink/[0.06] p-2 text-[11px] text-ink dark:bg-ink/10"
                  >{{ t.result }}</pre
                >
              </div>
            </div>
          </details>
        </template>

        <MarkdownMessage
          v-else-if="isStreamingThis && fallbackStreamText"
          :source="fallbackStreamText"
        />

        <template v-else-if="message.role === 'assistant'">
          <template v-for="(block, bi) in persistedBlocks" :key="bi">
            <div
              v-if="block.kind === 'part' && block.part.type === 'code'"
              class="rounded-lg border border-ink/10 bg-surface-muted/40 px-3 py-2 dark:bg-surface-muted/25"
            >
              <div class="mb-1 text-[11px] font-semibold text-ink-muted">代码</div>
              <MarkdownMessage
                :source="'```' + (block.part.language || '') + '\n' + (block.part.text || '') + '\n```'"
              />
            </div>
            <details
              v-else-if="block.kind === 'part' && block.part.type === 'task_flow'"
              class="group/tf rounded-lg border border-ink/10 bg-surface-muted/50 dark:bg-surface-muted/25"
            >
              <summary :class="summaryCls">
                <svg
                  class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/tf:rotate-90"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                >
                  <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                </svg>
                {{ partLabel(block.part.type) }}
              </summary>
              <div class="border-t border-ink/10 px-3 pb-3 pt-2">
                <MarkdownMessage :source="block.part.text || ''" />
              </div>
            </details>
            <details
              v-else-if="block.kind === 'part' && block.part.type === 'thinking'"
              class="group/th rounded-lg border border-ink/10 bg-surface-muted/40 dark:bg-surface-muted/20"
            >
              <summary :class="summaryCls">
                <svg
                  class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/th:rotate-90"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                >
                  <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                </svg>
                {{ partLabel(block.part.type) }}
              </summary>
              <div class="border-t border-ink/10 px-3 pb-3 pt-2">
                <MarkdownMessage :source="block.part.text || ''" />
              </div>
            </details>
            <div v-else-if="block.kind === 'part' && block.part.type === 'text'" class="min-w-0">
              <MarkdownMessage :source="block.part.text || ''" />
            </div>
            <div
              v-else-if="block.kind === 'part'"
              class="rounded-lg border border-ink/10 px-3 py-2 font-mono text-xs text-ink-muted"
            >
              {{ block.part.name }}({{ block.part.arguments }})
            </div>
            <details v-else class="group/to rounded-lg border border-ink/10 bg-surface-muted/40 dark:bg-surface-muted/20">
              <summary :class="summaryCls">
                <svg
                  class="h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform group-open/to:rotate-90"
                  viewBox="0 0 24 24"
                  fill="currentColor"
                >
                  <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z" />
                </svg>
                工具执行
              </summary>
              <div class="border-t border-ink/10 px-3 pb-3 pt-2">
                <div
                  v-for="t in block.steps"
                  :key="t.id"
                  class="mb-2 border-l-2 border-accent/35 pl-2 last:mb-0"
                >
                  <div class="font-mono text-xs text-accent">{{ t.name }}</div>
                  <pre class="mt-1 max-h-28 overflow-auto text-[11px] text-ink-muted">{{ t.args }}</pre>
                  <pre
                    v-if="t.result"
                    class="mt-1 max-h-36 overflow-auto rounded-md bg-ink/[0.06] p-2 text-[11px] text-ink dark:bg-ink/10"
                    >{{ t.result }}</pre
                  >
                </div>
              </div>
            </details>
          </template>
        </template>
        <div
          v-if="!isStreamingThis"
          class="flex flex-wrap items-center gap-0.5 border-t border-ink/10 pt-2 dark:border-ink/15"
        >
          <button
            type="button"
            :class="iconBtn"
            title="复制"
            aria-label="复制回复"
            @click="copyAssistantMarkdown"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <rect x="9" y="9" width="11" height="11" rx="2" />
              <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
            </svg>
          </button>
          <button
            type="button"
            :class="iconBtn"
            title="重新生成"
            aria-label="重新生成"
            :disabled="store.sending"
            @click="onRegenerateAssistant"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path d="M21 12a9 9 0 11-3-7.5M21 3v6h-6" />
            </svg>
          </button>
          <button
            type="button"
            :class="iconBtn"
            title="导出为 Markdown"
            aria-label="导出"
            @click="exportAssistantFile"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path d="M12 3v12M8 11l4 4 4-4M5 21h14" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
