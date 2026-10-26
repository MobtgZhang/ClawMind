import { defineStore } from "pinia";
import { ref } from "vue";
import type { MemoryCensus, PluginConfig, PluginView } from "@/types/plugins";

function asArray<T>(data: unknown): T[] {
  return Array.isArray(data) ? data : [];
}

export const usePluginStore = defineStore("plugins", () => {
  const plugins = ref<PluginView[]>([]);
  const census = ref<MemoryCensus | null>(null);
  const config = ref<PluginConfig | null>(null);
  const loading = ref(false);
  const lastError = ref<string | null>(null);

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(path, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    });
    if (!res.ok) {
      const t = await res.text();
      throw new Error(t || res.statusText);
    }
    if (res.status === 204) return undefined as T;
    return res.json() as Promise<T>;
  }

  async function refresh(projectId = "") {
    loading.value = true;
    try {
      plugins.value = asArray<PluginView>(await api<PluginView[]>("/api/plugins"));
      config.value = await api<PluginConfig>("/api/plugins/config");
      const query = projectId ? `?projectId=${encodeURIComponent(projectId)}` : "";
      const data = await api<MemoryCensus>(`/api/memory/layers${query}`);
      census.value = data && typeof data === "object" ? data : null;
    } catch (err) {
      lastError.value = (err as Error).message || "读取插件与记忆失败";
    } finally {
      loading.value = false;
    }
  }

  async function setEnabled(id: string, enabled: boolean) {
    try {
      await api(`/api/plugins/${encodeURIComponent(id)}/${enabled ? "enable" : "disable"}`, {
        method: "POST",
      });
      await refresh();
    } catch (err) {
      lastError.value = (err as Error).message || "切换插件失败";
      throw err;
    }
  }

  return { plugins, census, config, loading, lastError, refresh, setEnabled };
});
