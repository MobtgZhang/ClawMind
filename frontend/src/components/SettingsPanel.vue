<script setup lang="ts">
import { reactive, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { SAMPLING_PRESETS } from "@/types/chat";

const PRESET_IDS = ["precise", "balanced", "creative"] as const;

const store = useChatStore();
const open = defineModel<boolean>("open", { default: false });

const form = reactive({
  openaiBaseUrl: "",
  openaiApiKey: "",
  openaiModel: "",
  temperature: 0.7,
  topP: 1,
  topK: 0 as number,
  maxAgentRounds: 16,
});

function syncFromStore() {
  const s = store.serverSettings;
  form.openaiBaseUrl = s.openaiBaseUrl;
  form.openaiApiKey = s.openaiApiKey;
  form.openaiModel = s.openaiModel;
  form.temperature = s.temperature;
  form.topP = s.topP;
  form.topK = s.topK ?? 0;
  form.maxAgentRounds = s.maxAgentRounds;
}

watch(
  () => open.value,
  (v) => {
    if (v) {
      void store.loadServerSettings().then(syncFromStore);
    }
  }
);

function applyPreset(id: keyof typeof SAMPLING_PRESETS) {
  const p = SAMPLING_PRESETS[id];
  form.temperature = p.temperature;
  form.topP = p.topP;
  form.topK = p.topK ?? 0;
}

async function onSave() {
  await store.saveServerSettings({
    openaiBaseUrl: form.openaiBaseUrl.trim(),
    openaiApiKey: form.openaiApiKey,
    openaiModel: form.openaiModel.trim(),
    temperature: Number(form.temperature),
    topP: Number(form.topP),
    topK: form.topK > 0 ? Math.floor(form.topK) : 0,
    maxAgentRounds: Math.min(256, Math.max(1, Math.floor(form.maxAgentRounds))),
  });
  syncFromStore();
  open.value = false;
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
        class="max-h-[92vh] w-full max-w-lg overflow-y-auto rounded-2xl border border-ink/15 bg-surface p-6 shadow-xl"
        @click.stop
      >
        <h2 class="text-lg font-semibold text-ink">设置</h2>
        <p class="mt-1 text-xs text-ink-muted">
          模型、密钥与采样参数写入服务端 <code class="rounded bg-surface-muted px-1">.clawmind/config.json</code>（可用环境变量
          <code class="rounded bg-surface-muted px-1">CLAWMIND_DIR</code> 指定目录）。未填 API Key 时仍可使用
          <code class="rounded bg-surface-muted px-1">OPENAI_API_KEY</code> 等环境变量作为后备。
        </p>

        <h3 class="mt-5 text-sm font-semibold text-ink">模型与鉴权</h3>
        <label class="mt-2 block text-xs font-medium text-ink-muted">API Base URL</label>
        <input
          v-model="form.openaiBaseUrl"
          type="text"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />
        <label class="mt-3 block text-xs font-medium text-ink-muted">模型名称</label>
        <input
          v-model="form.openaiModel"
          type="text"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />
        <label class="mt-3 block text-xs font-medium text-ink-muted">API Key</label>
        <input
          v-model="form.openaiApiKey"
          type="password"
          autocomplete="off"
          placeholder="sk-..."
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />

        <h3 class="mt-5 text-sm font-semibold text-ink">采样参数</h3>
        <p class="mt-1 text-xs text-ink-muted">
          精确 / 平衡 / 创意仅调整温度、Top P、Top K；点「保存设置」后写入 <code class="rounded bg-surface-muted px-1">config.json</code>。
        </p>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-for="id in PRESET_IDS"
            :key="id"
            type="button"
            class="rounded-lg border border-ink/15 px-3 py-1.5 text-xs text-ink hover:bg-ink/5"
            @click="applyPreset(id)"
          >
            {{ SAMPLING_PRESETS[id].label }} (T={{ SAMPLING_PRESETS[id].temperature }}, p={{
              SAMPLING_PRESETS[id].topP
            }}, k={{ SAMPLING_PRESETS[id].topK }})
          </button>
        </div>
        <label class="mt-3 block text-xs font-medium text-ink-muted">温度 temperature</label>
        <input
          v-model.number="form.temperature"
          type="number"
          step="0.1"
          min="0"
          max="2"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />
        <label class="mt-3 block text-xs font-medium text-ink-muted">Top P</label>
        <input
          v-model.number="form.topP"
          type="number"
          step="0.05"
          min="0"
          max="1"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />
        <label class="mt-3 block text-xs font-medium text-ink-muted">Top K（0 表示不传该字段）</label>
        <input
          v-model.number="form.topK"
          type="number"
          min="0"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />

        <h3 class="mt-5 text-sm font-semibold text-ink">Agent</h3>
        <label class="mt-2 block text-xs font-medium text-ink-muted">上下文最大助手轮次（参与请求的历史条数裁剪）</label>
        <input
          v-model.number="form.maxAgentRounds"
          type="number"
          min="1"
          max="256"
          class="mt-1 w-full rounded-lg border border-ink/15 bg-surface-muted px-3 py-2 text-sm text-ink outline-none focus:border-accent"
        />

        <div class="mt-6 flex gap-2">
          <button
            type="button"
            class="flex-1 rounded-lg border border-ink/20 py-2.5 text-sm text-ink hover:bg-ink/5"
            @click="open = false"
          >
            取消
          </button>
          <button
            type="button"
            class="flex-1 rounded-lg bg-accent py-2.5 text-sm font-medium text-white hover:opacity-90"
            @click="onSave"
          >
            保存设置
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
