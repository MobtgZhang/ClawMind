<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useChatStore } from "@/stores/chat";
import { SAMPLING_PRESETS } from "@/types/chat";
import Icon from "./Icon.vue";

const PRESET_IDS = ["precise", "balanced", "creative"] as const;

type SectionId = "model" | "sampling" | "agent" | "appearance";

const store = useChatStore();
const open = defineModel<boolean>("open", { default: false });

const section = ref<SectionId>("model");

const SECTIONS: Array<{ id: SectionId; label: string; icon: string; hint: string }> = [
  { id: "model", label: "模型与鉴权", icon: "cpu", hint: "API Base URL、模型名称与密钥" },
  { id: "sampling", label: "采样参数", icon: "sliders", hint: "温度、Top P、Top K 与采样档" },
  { id: "agent", label: "Agent", icon: "activity", hint: "上下文轮次与运行上限" },
  { id: "appearance", label: "外观", icon: "sun", hint: "深浅色主题" },
];

const form = reactive({
  openaiBaseUrl: "",
  openaiApiKey: "",
  openaiModel: "",
  temperature: 0.7,
  topP: 1,
  topK: 0 as number,
  maxAgentRounds: 16,
});

/** Keys the plugin layer wins. The form edits the file, so these are shown as overrides rather than
 * merged into the inputs: saving here would not change what the runtime uses. */
const overrides = computed(() => {
  const source = store.serverSettings.source ?? {};
  const effective = store.serverSettings.effective ?? {};
  return Object.keys(source)
    .sort()
    .map((key) => ({ key, plugin: source[key], value: String(effective[key] ?? "") }));
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
    <div v-if="open" class="dialog-scrim" @click.self="open = false">
      <div class="dialog settings" role="dialog" aria-label="设置" @click.stop>
        <div class="dialog__head">
          <h2>设置</h2>
          <button type="button" class="icon-btn" title="关闭" aria-label="关闭" @click="open = false">
            <Icon name="close" />
          </button>
        </div>

        <div class="settings__main">
          <nav class="set-nav" aria-label="设置分类">
            <button
              v-for="s in SECTIONS"
              :key="s.id"
              type="button"
              class="nav-item"
              :class="{ 'is-current': section === s.id }"
              @click="section = s.id"
            >
              <Icon :name="s.icon" />
              <span>{{ s.label }}</span>
            </button>
          </nav>

          <div class="set-body">
            <!-- ------------------------------------------------ 模型与鉴权 -->
            <template v-if="section === 'model'">
              <h3>模型与鉴权</h3>
              <p class="set-intro">
                模型、密钥与采样参数写入服务端
                <code class="mono rounded bg-surface-2 px-1">.clawmind/config.json</code
                >（可用 <code class="mono rounded bg-surface-2 px-1">CLAWMIND_DIR</code> 指定目录）。未填 API Key 时仍可用
                <code class="mono rounded bg-surface-2 px-1">OPENAI_API_KEY</code> 等环境变量。
              </p>

              <div v-if="overrides.length" class="callout callout--warn mt-3">
                <Icon name="warning" size="sm" />
                <span>
                  这些 key 由插件层决定，设置页保存的只是文件配置：
                  <template v-for="row in overrides" :key="row.key">
                    <code class="mono rounded bg-surface px-1">{{ row.key }}</code> → 插件
                    <code class="mono rounded bg-surface px-1">{{ row.plugin }}</code>（生效值 {{ row.value }}）
                  </template>
                </span>
              </div>

              <div class="set-row set-row--top">
                <span class="set-row__text">
                  <span class="set-row__label">API Base URL</span>
                  <span class="set-row__sub">OpenAI 兼容端点，留空使用默认</span>
                </span>
                <span class="set-row__ctl">
                  <input
                    id="set-base"
                    v-model="form.openaiBaseUrl"
                    type="text"
                    class="input"
                    placeholder="https://api.openai.com/v1"
                  />
                </span>
              </div>
              <div class="set-row">
                <span class="set-row__text">
                  <span class="set-row__label">模型名称</span>
                  <span class="set-row__sub">聊天页左上角显示的就是它</span>
                </span>
                <span class="set-row__ctl">
                  <input id="set-model" v-model="form.openaiModel" type="text" class="input" placeholder="gpt-4o" />
                </span>
              </div>
              <div class="set-row">
                <span class="set-row__text">
                  <span class="set-row__label">API Key</span>
                  <span class="set-row__sub">留空则回退环境变量</span>
                </span>
                <span class="set-row__ctl">
                  <input
                    id="set-key"
                    v-model="form.openaiApiKey"
                    type="password"
                    autocomplete="off"
                    placeholder="sk-..."
                    class="input"
                  />
                </span>
              </div>
            </template>

            <!-- ------------------------------------------------ 采样参数 -->
            <template v-else-if="section === 'sampling'">
              <h3>采样参数</h3>
              <p class="set-intro">
                精确 / 平衡 / 创意仅调整温度、Top P、Top K；点「保存设置」后写入 config.json。
              </p>
              <div class="mt-2 flex flex-wrap gap-2">
                <button
                  v-for="id in PRESET_IDS"
                  :key="id"
                  type="button"
                  class="btn btn--secondary btn--sm"
                  @click="applyPreset(id)"
                >
                  {{ SAMPLING_PRESETS[id].label }} (T={{ SAMPLING_PRESETS[id].temperature }}, p={{
                    SAMPLING_PRESETS[id].topP
                  }}, k={{ SAMPLING_PRESETS[id].topK }})
                </button>
              </div>

              <div class="set-slider">
                <label for="set-temp">温度</label>
                <input
                  id="set-temp"
                  v-model.number="form.temperature"
                  type="range"
                  step="0.05"
                  min="0"
                  max="2"
                  class="range"
                />
                <b>{{ Number(form.temperature).toFixed(2) }}</b>
              </div>
              <div class="set-slider">
                <label for="set-topp">Top P</label>
                <input
                  id="set-topp"
                  v-model.number="form.topP"
                  type="range"
                  step="0.05"
                  min="0"
                  max="1"
                  class="range"
                />
                <b>{{ Number(form.topP).toFixed(2) }}</b>
              </div>
              <div class="set-slider">
                <label for="set-topk">Top K（0 不传）</label>
                <input
                  id="set-topk"
                  v-model.number="form.topK"
                  type="range"
                  step="1"
                  min="0"
                  max="200"
                  class="range"
                />
                <b>{{ Math.floor(form.topK) }}</b>
              </div>
            </template>

            <!-- ------------------------------------------------ Agent -->
            <template v-else-if="section === 'agent'">
              <h3>Agent</h3>
              <p class="set-intro">上下文最大助手轮次：历史消息按轮次裁剪，避免上下文溢出。</p>
              <div class="set-slider">
                <label for="set-rounds">最大助手轮次</label>
                <input
                  id="set-rounds"
                  v-model.number="form.maxAgentRounds"
                  type="range"
                  step="1"
                  min="1"
                  max="256"
                  class="range"
                />
                <b>{{ Math.floor(form.maxAgentRounds) }}</b>
              </div>
            </template>

            <!-- ------------------------------------------------ 外观 -->
            <template v-else>
              <h3>外观</h3>
              <p class="set-intro">切换深色 / 浅色主题，设置立即保存在本地。</p>
              <div class="set-row">
                <span class="set-row__text">
                  <span class="set-row__label">{{ store.darkMode ? "深色主题" : "浅色主题" }}</span>
                  <span class="set-row__sub">切换按钮即时生效</span>
                </span>
                <span class="set-row__ctl">
                  <button
                    type="button"
                    class="btn btn--secondary btn--sm"
                    @click="store.setDarkMode(!store.darkMode)"
                  >
                    <Icon :name="store.darkMode ? 'sun' : 'moon'" size="sm" />
                    切换为{{ store.darkMode ? "浅色" : "深色" }}
                  </button>
                </span>
              </div>
            </template>
          </div>
        </div>

        <div class="dialog__actions">
          <button type="button" class="btn btn--secondary" @click="open = false">取消</button>
          <button type="button" class="btn btn--primary" @click="onSave">保存设置</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
