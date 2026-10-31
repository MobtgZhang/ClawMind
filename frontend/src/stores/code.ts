import { defineStore } from "pinia";
import { ref } from "vue";
import type { CodeWorkspace } from "@/types/code";

/** 代码工程视图（GET /api/code）：工作区画像 + 项目分级 + 符号图统计，只读。 */
export const useCodeStore = defineStore("code", () => {
  const view = ref<CodeWorkspace | null>(null);
  const loading = ref(false);
  const refreshing = ref(false);
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
    return res.json() as Promise<T>;
  }

  async function refresh(path?: string) {
    loading.value = true;
    lastError.value = null;
    try {
      const q = path ? `?path=${encodeURIComponent(path)}` : "";
      view.value = await api<CodeWorkspace>(`/api/code${q}`);
    } catch (e) {
      lastError.value =
        e instanceof TypeError && e.message === "Failed to fetch"
          ? "无法连接后端。"
          : (e as Error).message || "加载代码工作区失败";
    } finally {
      loading.value = false;
    }
  }

  /** 重建画像与符号图索引（POST /api/code/refresh）。 */
  async function reindex(path?: string) {
    refreshing.value = true;
    lastError.value = null;
    try {
      const q = path ? `?path=${encodeURIComponent(path)}` : "";
      await api<{ ok: boolean }>(`/api/code/refresh${q}`, { method: "POST" });
      await refresh(path);
    } catch (e) {
      lastError.value = (e as Error).message || "重建索引失败";
    } finally {
      refreshing.value = false;
    }
  }

  return { view, loading, refreshing, lastError, refresh, reindex };
});
