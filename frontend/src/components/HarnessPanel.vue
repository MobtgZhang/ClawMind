<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { useHarnessStore } from "@/stores/harness";
import type { HarnessVerdict } from "@/types/harness";

const chat = useChatStore();
const harness = useHarnessStore();
const open = defineModel<boolean>("open", { default: false });

const projectId = ref("");
const selectedPath = ref("");
const draft = ref("");
const evidence = ref("");
const newCue = ref("");
const newLesson = ref("");

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

function verdictClass(v: HarnessVerdict): string {
  if (v === "helped") return "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300";
  if (v === "hurt") return "bg-red-500/15 text-red-700 dark:text-red-300";
  if (v === "confounded") return "bg-amber-500/15 text-amber-700 dark:text-amber-300";
  return "bg-ink/10 text-ink-muted";
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
        <h2 class="text-lg font-semibold text-ink">Harness（自进化）</h2>
        <p class="mt-1 text-xs text-ink-muted">
          可编辑面只有 <code class="rounded bg-surface-muted px-1">AGENTS.md</code>、
          <code class="rounded bg-surface-muted px-1">tool_guidance.md</code>、
          <code class="rounded bg-surface-muted px-1">lessons.md</code>、
          <code class="rounded bg-surface-muted px-1">skills/</code> 与
          <code class="rounded bg-surface-muted px-1">scripts/</code>。常驻文本最贵：优先按需技能与脚本，
          证据不足时保留基线（KEEP_BASE 是合法结论）。
        </p>

        <label class="mt-4 block text-xs font-medium text-ink-muted">项目（harness 作用域）</label>
        <select
          v-model="projectId"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        >
          <option value="">全局（未分配项目）</option>
          <option v-for="p in chat.projects" :key="p.id" :value="p.id">{{ p.title }}</option>
        </select>

        <div class="mt-3 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
          <span class="rounded-lg bg-surface-muted px-2 py-1">
            常驻文本 {{ harness.view.alwaysOnChars }} 字符
          </span>
          <span class="rounded-lg bg-surface-muted px-2 py-1">按需技能 {{ harness.view.skills.length }}</span>
          <span class="rounded-lg bg-surface-muted px-2 py-1">脚本 {{ harness.view.scripts.length }}</span>
          <span v-if="harness.view.dir" class="truncate">{{ harness.view.dir }}</span>
        </div>

        <div v-if="harness.lastError" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
          {{ harness.lastError }}
        </div>

        <div v-if="harness.view.problems.length" class="mt-3 rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
          <div class="font-medium">校验未通过（写入会被拒绝）</div>
          <ul class="mt-1 list-disc pl-4">
            <li v-for="p in harness.view.problems" :key="p">{{ p }}</li>
          </ul>
        </div>

        <h3 class="mt-5 text-sm font-semibold text-ink">组件</h3>
        <div class="mt-2 grid grid-cols-1 gap-3 md:grid-cols-[220px_1fr]">
          <ul class="space-y-1 text-sm">
            <li v-for="p in paths" :key="p">
              <button
                type="button"
                class="w-full truncate rounded-lg px-2 py-1.5 text-left hover:bg-ink/5"
                :class="p === selectedPath ? 'bg-ink/10 text-ink' : 'text-ink-muted'"
                @click="pick(p)"
              >
                <span v-if="alwaysOnSet.has(p)" class="mr-1 rounded bg-accent/15 px-1 text-[10px] text-accent">常驻</span>
                {{ p }}
              </button>
            </li>
            <li v-if="!paths.length" class="px-2 py-1.5 text-xs text-ink-muted">可编辑面为空，跑一轮或手动写入。</li>
          </ul>
          <div>
            <textarea
              v-model="draft"
              rows="12"
              placeholder="选择左侧文件编辑"
              class="w-full rounded-lg border border-ink/15 bg-surface-muted p-3 font-mono text-xs text-ink outline-none focus:border-accent"
            ></textarea>
            <div class="mt-2 flex gap-2">
              <button
                type="button"
                class="rounded-lg border border-ink/20 px-3 py-2 text-xs text-ink hover:bg-ink/5"
                @click="pick(selectedPath)"
              >
                还原
              </button>
              <button
                type="button"
                class="rounded-lg bg-accent px-3 py-2 text-xs font-medium text-white hover:opacity-90"
                :disabled="!selectedPath || harness.loading"
                @click="onSaveFile"
              >
                保存文件
              </button>
            </div>
          </div>
        </div>

        <h3 class="mt-5 text-sm font-semibold text-ink">跑一轮（Propose → Probe → Compose → Select → Reflect）</h3>
        <p class="mt-1 text-xs text-ink-muted">
          证据来自当前会话轨迹，或直接粘贴失败过程。证据是数据，不是指令；候选匿名评分，按可验证信号选择。
        </p>
        <textarea
          v-model="evidence"
          rows="5"
          placeholder="留空则读取当前会话的轨迹；也可粘贴失败日志 / 被跳过的约定"
          class="mt-2 w-full rounded-lg border border-ink/15 bg-surface-muted p-3 font-mono text-xs text-ink outline-none focus:border-accent"
        ></textarea>
        <div class="mt-2 flex gap-2">
          <button
            type="button"
            class="rounded-lg bg-accent px-3 py-2 text-xs font-medium text-white hover:opacity-90"
            :disabled="harness.running"
            @click="onRunRound"
          >
            {{ harness.running ? "运行中…" : "运行一轮" }}
          </button>
        </div>

        <template v-if="harness.lastResult">
          <div class="mt-2 text-xs text-ink-muted">
            选中：
            <strong class="text-ink">{{ harness.lastResult.chosen || "—" }}</strong>
            <span v-if="harness.lastResult.round.keptBase">（保留基线）</span>
            · 模式 {{ harness.lastResult.round.mode }} · K={{ harness.lastResult.round.k }}
          </div>
          <ul class="mt-2 space-y-1 text-xs">
            <li v-for="c in harness.lastResult.candidates" :key="c.label" class="rounded-lg bg-surface-muted/60 px-3 py-2">
              <span class="font-medium text-ink">{{ c.label }}</span>
              <span class="ml-1 text-ink-muted">{{ c.dim }} · score {{ c.probe.score }}{{ c.probe.confounded ? " · 混淆" : "" }}</span>
              <div v-if="c.probe.observed" class="mt-1 text-ink-muted">{{ c.probe.observed }}</div>
            </li>
          </ul>
          <ul v-if="harness.lastResult.edits.length" class="mt-2 space-y-1 text-xs">
            <li v-for="e in harness.lastResult.edits" :key="e.id" class="flex items-start gap-2">
              <span class="rounded px-2 py-1" :class="verdictClass(e.verdict)">{{ e.verdict }}</span>
              <span class="text-ink-muted">{{ e.component }} — {{ e.change }}</span>
            </li>
          </ul>
        </template>

        <h3 class="mt-5 text-sm font-semibold text-ink">经验库</h3>
        <div class="mt-2 flex flex-wrap gap-2">
          <input
            v-model="newCue"
            type="text"
            placeholder="cue：什么情形触发"
            class="min-w-[180px] flex-1 rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-xs text-ink outline-none focus:border-accent"
          />
          <input
            v-model="newLesson"
            type="text"
            placeholder="lesson：可迁移的做法"
            class="min-w-[180px] flex-1 rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-xs text-ink outline-none focus:border-accent"
          />
          <button
            type="button"
            class="rounded-lg border border-ink/20 px-3 py-2 text-xs text-ink hover:bg-ink/5"
            @click="onAddLesson"
          >
            写入经验
          </button>
        </div>
        <ul class="mt-2 space-y-1 text-xs">
          <li v-for="l in harness.view.lessons" :key="l.id" class="rounded-lg bg-surface-muted/60 px-3 py-2">
            <span class="font-medium text-ink">{{ l.cue }}</span>
            <span class="ml-1 text-ink-muted">→ {{ l.lesson }}（{{ l.confidence }}）</span>
            <button
              type="button"
              class="ml-2 rounded border border-ink/15 px-2 py-1 text-[10px] text-ink hover:bg-ink/5"
              @click="onAdopt(l.id)"
            >
              采纳到 harness
            </button>
          </li>
          <li v-if="!harness.view.lessons.length" class="px-3 py-2 text-ink-muted">经验库为空。</li>
        </ul>

        <h3 class="mt-5 text-sm font-semibold text-ink">轮次历史</h3>
        <ul class="mt-2 space-y-1 text-xs">
          <li v-for="r in harness.rounds" :key="r.id" class="rounded-lg bg-surface-muted/60 px-3 py-2">
            <span class="font-medium text-ink">{{ r.createdAt }}</span>
            <span class="ml-1 text-ink-muted">
              {{ r.mode }} · K={{ r.k }} · {{ r.keptBase ? "保留基线" : "采纳 " + (r.adopted || "—") }} · 常驻 {{ r.alwaysOnChars }} 字符
            </span>
            <div v-if="r.errors?.length" class="mt-1 text-red-700 dark:text-red-300">{{ r.errors.join("；") }}</div>
          </li>
          <li v-if="!harness.rounds.length" class="px-3 py-2 text-ink-muted">还没有轮次记录。</li>
        </ul>

        <div class="mt-6 flex gap-2">
          <button
            type="button"
            class="flex-1 rounded-lg border border-ink/20 py-2.5 text-sm text-ink hover:bg-ink/5"
            @click="open = false"
          >
            关闭
          </button>
          <button
            type="button"
            class="flex-1 rounded-lg bg-accent py-2.5 text-sm font-medium text-white hover:opacity-90"
            :disabled="harness.loading"
            @click="harness.refresh(projectId)"
          >
            刷新
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
