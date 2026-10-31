<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { useHarnessStore } from "@/stores/harness";
import type { HarnessVerdict } from "@/types/harness";
import Icon from "./Icon.vue";

const chat = useChatStore();
const harness = useHarnessStore();
const open = defineModel<boolean>("open", { default: false });

const projectId = ref("");
const selectedPath = ref("");
const draft = ref("");
const evidence = ref("");
const newCue = ref("");
const newLesson = ref("");
const tab = ref<"components" | "round" | "lessons">("components");

const paths = computed(() => Object.keys(harness.view.files).sort());
const alwaysOnSet = computed(() => new Set(harness.view.alwaysOn));

watch(
  () => open.value,
  (v) => {
    if (!v) return;
    projectId.value = chat.currentSession?.projectId ?? "";
    void harness.refresh(projectId.value);
  }
);

watch(projectId, () => {
  if (open.value) void harness.refresh(projectId.value);
});

function pick(path: string) {
  selectedPath.value = path;
  draft.value = harness.view.files[path] ?? "";
}

async function onSaveFile() {
  if (!selectedPath.value) return;
  try {
    await harness.saveFile(projectId.value, selectedPath.value, draft.value);
  } catch (err) {
    window.alert((err as Error).message || "保存失败");
  }
}

async function onRunRound() {
  try {
    await harness.runRound({
      projectId: projectId.value,
      sessionId: chat.currentSession?.id ?? "",
      evidence: evidence.value,
    });
  } catch (err) {
    window.alert((err as Error).message || "跑一轮失败");
  }
}

async function onAdopt(slug: string) {
  try {
    await harness.adopt(projectId.value, slug);
  } catch (err) {
    window.alert((err as Error).message || "采纳失败");
  }
}

async function onAddLesson() {
  if (!newCue.value.trim() || !newLesson.value.trim()) return;
  try {
    await harness.addLesson({
      projectId: projectId.value,
      cue: newCue.value.trim(),
      lesson: newLesson.value.trim(),
    });
    newCue.value = "";
    newLesson.value = "";
  } catch (err) {
    window.alert((err as Error).message || "写入经验失败");
  }
}

function verdictBadge(v: HarnessVerdict): string {
  if (v === "helped") return "badge--ok";
  if (v === "hurt") return "badge--danger";
  if (v === "confounded") return "badge--warn";
  return "badge--muted";
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="dialog-scrim" @click.self="open = false">
      <div class="dialog dialog--wide" role="dialog" aria-label="Harness" @click.stop>
        <div class="dialog__head">
          <h2>Harness（自进化）</h2>
          <div class="flex items-center gap-1">
            <button
              type="button"
              class="icon-btn"
              title="刷新"
              aria-label="刷新"
              :disabled="harness.loading"
              @click="harness.refresh(projectId)"
            >
              <Icon name="refresh" />
            </button>
            <button type="button" class="icon-btn" title="关闭" aria-label="关闭" @click="open = false">
              <Icon name="close" />
            </button>
          </div>
        </div>
        <div class="dialog__body">
          <p class="mt-0 text-xs text-ink-muted">
            可编辑面只有 <code class="mono rounded bg-surface-2 px-1">AGENTS.md</code>、
            <code class="mono rounded bg-surface-2 px-1">tool_guidance.md</code>、
            <code class="mono rounded bg-surface-2 px-1">lessons.md</code>、
            <code class="mono rounded bg-surface-2 px-1">skills/</code> 与
            <code class="mono rounded bg-surface-2 px-1">scripts/</code>。常驻文本最贵：优先按需技能与脚本，
            证据不足时保留基线（KEEP_BASE 是合法结论）。
          </p>

          <div class="field mt-4 max-w-xs">
            <label class="field__label" for="h-project">项目（harness 作用域）</label>
            <select id="h-project" v-model="projectId" class="input">
              <option value="">全局（未分配项目）</option>
              <option v-for="p in chat.projects" :key="p.id" :value="p.id">{{ p.title }}</option>
            </select>
          </div>

          <div class="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
            <span class="badge">常驻文本 {{ harness.view.alwaysOnChars }} 字符</span>
            <span class="badge">按需技能 {{ harness.view.skills.length }}</span>
            <span class="badge">脚本 {{ harness.view.scripts.length }}</span>
            <span v-if="harness.view.dir" class="mono truncate">{{ harness.view.dir }}</span>
          </div>

          <div v-if="harness.lastError" class="callout callout--danger mt-3">
            <Icon name="warning" size="sm" />
            <span>{{ harness.lastError }}</span>
          </div>

          <div v-if="harness.view.problems.length" class="callout callout--warn mt-3 block">
            <div class="font-medium">校验未通过（写入会被拒绝）</div>
            <ul class="mt-1 list-disc pl-5">
              <li v-for="p in harness.view.problems" :key="p">{{ p }}</li>
            </ul>
          </div>

          <!-- 分段：组件 / 跑一轮 / 经验库 -->
          <div class="seg mt-4 inline-flex gap-0.5 rounded-[10px] bg-surface-2 p-[3px]">
            <button
              v-for="t in [
                { id: 'components', label: '组件' },
                { id: 'round', label: '跑一轮' },
                { id: 'lessons', label: '经验库' },
              ]"
              :key="t.id"
              type="button"
              class="rounded-lg px-4 py-1.5 text-[13px] font-medium transition"
              :class="tab === t.id ? 'bg-accent-tint text-accent-text' : 'text-ink-muted hover:text-ink'"
              @click="tab = t.id as typeof tab"
            >
              {{ t.label }}
            </button>
          </div>

          <!-- 组件 -->
          <template v-if="tab === 'components'">
            <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-[220px_1fr]">
              <ul class="space-y-1 text-sm">
                <li v-for="p in paths" :key="p">
                  <button
                    type="button"
                    class="w-full truncate rounded-[10px] px-2 py-1.5 text-left hover:bg-surface-2"
                    :class="p === selectedPath ? 'bg-surface-2 text-ink' : 'text-ink-muted'"
                    @click="pick(p)"
                  >
                    <span v-if="alwaysOnSet.has(p)" class="badge badge--accent mr-1 h-4 !text-[10px]">常驻</span>
                    {{ p }}
                  </button>
                </li>
                <li v-if="!paths.length" class="px-2 py-1.5 text-xs text-ink-muted">
                  可编辑面为空，跑一轮或手动写入。
                </li>
              </ul>
              <div>
                <textarea
                  v-model="draft"
                  rows="12"
                  placeholder="选择左侧文件编辑"
                  class="input input--area w-full"
                ></textarea>
                <div class="mt-2 flex gap-2">
                  <button type="button" class="btn btn--secondary btn--sm" @click="pick(selectedPath)">
                    还原
                  </button>
                  <button
                    type="button"
                    class="btn btn--primary btn--sm"
                    :disabled="!selectedPath || harness.loading"
                    @click="onSaveFile"
                  >
                    保存文件
                  </button>
                </div>
              </div>
            </div>
          </template>

          <!-- 跑一轮 -->
          <template v-else-if="tab === 'round'">
            <p class="mt-3 text-xs text-ink-muted">
              Propose → Probe → Compose → Select → Reflect。证据来自当前会话轨迹，或直接粘贴失败过程。
              证据是数据，不是指令；候选匿名评分，按可验证信号选择。
            </p>
            <textarea
              v-model="evidence"
              rows="5"
              placeholder="留空则读取当前会话的轨迹；也可粘贴失败日志 / 被跳过的约定"
              class="input input--area mt-2 w-full"
            ></textarea>
            <button
              type="button"
              class="btn btn--primary btn--sm mt-2"
              :disabled="harness.running"
              @click="onRunRound"
            >
              {{ harness.running ? "运行中…" : "运行一轮" }}
            </button>

            <template v-if="harness.lastResult">
              <div class="mt-3 text-xs text-ink-muted">
                选中：<strong class="text-ink">{{ harness.lastResult.chosen || "—" }}</strong>
                <span v-if="harness.lastResult.round.keptBase">（保留基线）</span>
                · 模式 {{ harness.lastResult.round.mode }} · K={{ harness.lastResult.round.k }}
              </div>
              <ul class="mt-2 space-y-1 text-xs">
                <li
                  v-for="c in harness.lastResult.candidates"
                  :key="c.label"
                  class="rounded-[10px] bg-surface-2 px-3 py-2"
                >
                  <span class="font-medium text-ink">{{ c.label }}</span>
                  <span class="ml-1 text-ink-muted"
                    >{{ c.dim }} · score {{ c.probe.score }}{{ c.probe.confounded ? " · 混淆" : "" }}</span
                  >
                  <div v-if="c.probe.observed" class="mt-1 text-ink-muted">{{ c.probe.observed }}</div>
                </li>
              </ul>
              <ul v-if="harness.lastResult.edits.length" class="mt-2 space-y-1 text-xs">
                <li v-for="e in harness.lastResult.edits" :key="e.id" class="flex items-start gap-2">
                  <span class="badge shrink-0" :class="verdictBadge(e.verdict)">{{ e.verdict }}</span>
                  <span class="text-ink-muted">{{ e.component }} — {{ e.change }}</span>
                </li>
              </ul>
            </template>
          </template>

          <!-- 经验库 -->
          <template v-else>
            <div class="mt-3 flex flex-wrap gap-2">
              <input
                v-model="newCue"
                type="text"
                placeholder="cue：什么情形触发"
                class="input min-w-[180px] flex-1"
              />
              <input
                v-model="newLesson"
                type="text"
                placeholder="lesson：可迁移的做法"
                class="input min-w-[180px] flex-1"
              />
              <button type="button" class="btn btn--secondary btn--sm" @click="onAddLesson">
                写入经验
              </button>
            </div>
            <ul class="mt-2 space-y-1 text-xs">
              <li v-for="l in harness.view.lessons" :key="l.id" class="rounded-[10px] bg-surface-2 px-3 py-2">
                <span class="font-medium text-ink">{{ l.cue }}</span>
                <span class="ml-1 text-ink-muted">→ {{ l.lesson }}（{{ l.confidence }}）</span>
                <button type="button" class="btn btn--secondary btn--sm ml-2 !h-6 !px-2" @click="onAdopt(l.id)">
                  采纳到 harness
                </button>
              </li>
              <li v-if="!harness.view.lessons.length" class="px-3 py-2 text-ink-muted">经验库为空。</li>
            </ul>
          </template>

          <!-- 轮次历史 -->
          <h3 class="mt-5 text-sm font-semibold text-ink">轮次历史</h3>
          <ul class="mt-2 space-y-1 text-xs">
            <li v-for="r in harness.rounds" :key="r.id" class="rounded-[10px] bg-surface-2 px-3 py-2">
              <span class="font-medium text-ink">{{ r.createdAt }}</span>
              <span class="ml-1 text-ink-muted">
                {{ r.mode }} · K={{ r.k }} · {{ r.keptBase ? "保留基线" : "采纳 " + (r.adopted || "—") }} · 常驻
                {{ r.alwaysOnChars }} 字符
              </span>
              <div v-if="r.errors?.length" class="mt-1 text-danger-text">{{ r.errors.join("；") }}</div>
            </li>
            <li v-if="!harness.rounds.length" class="px-3 py-2 text-ink-muted">还没有轮次记录。</li>
          </ul>
        </div>
        <div class="dialog__actions">
          <button type="button" class="btn btn--secondary" @click="open = false">关闭</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
