import { defineStore } from "pinia";
import { ref } from "vue";
import type {
  HarnessEntry,
  HarnessLesson,
  HarnessRound,
  HarnessRoundResult,
  HarnessView,
} from "@/types/harness";

/** 接口可能返回 null，先做形状防御，避免 UI 直接访问 undefined */
function asArray<T>(data: unknown): T[] {
  return Array.isArray(data) ? data : [];
}

function emptyView(projectId: string): HarnessView {
  return {
    projectId,
    dir: "",
    files: {},
    alwaysOn: [],
    alwaysOnChars: 0,
    skills: [],
    scripts: [],
    problems: [],
    lessons: [],
    entries: [],
  };
}

export const useHarnessStore = defineStore("harness", () => {
  const view = ref<HarnessView>(emptyView(""));
  const rounds = ref<HarnessRound[]>([]);
  const lastResult = ref<HarnessRoundResult | null>(null);
  const loading = ref(false);
  const running = ref(false);
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

  function clearLastError() {
    lastError.value = null;
  }

  async function refresh(projectId: string) {
    loading.value = true;
    try {
      const data = await api<HarnessView>(
        `/api/harness?projectId=${encodeURIComponent(projectId)}`
      );
      view.value = data && typeof data === "object" ? { ...emptyView(projectId), ...data } : emptyView(projectId);
      rounds.value = asArray<HarnessRound>(
        await api<HarnessRound[]>(`/api/harness/rounds?projectId=${encodeURIComponent(projectId)}`)
      );
    } catch (err) {
      lastError.value = (err as Error).message || "读取 harness 失败";
    } finally {
      loading.value = false;
    }
  }

  async function runRound(input: { projectId: string; sessionId?: string; evidence?: string }) {
    running.value = true;
    try {
      const res = await api<HarnessRoundResult>("/api/harness/round", {
        method: "POST",
        body: JSON.stringify({
          projectId: input.projectId,
          sessionId: input.sessionId ?? "",
          evidence: input.evidence ?? "",
        }),
      });
      lastResult.value = res;
      await refresh(input.projectId);
      return res;
    } catch (err) {
      lastError.value = (err as Error).message || "跑一轮失败";
      throw err;
    } finally {
      running.value = false;
    }
  }

  async function adopt(projectId: string, entry: HarnessEntry | string) {
    const slug = typeof entry === "string" ? entry : entry.slug;
    try {
      await api("/api/harness/adopt", {
        method: "POST",
        body: JSON.stringify({ projectId, entry: slug }),
      });
      await refresh(projectId);
    } catch (err) {
      lastError.value = (err as Error).message || "采纳失败";
      throw err;
    }
  }

  async function addLesson(input: {
    projectId: string;
    cue: string;
    lesson: string;
    evidence?: string;
    confidence?: string;
  }) {
    try {
      await api<HarnessLesson>("/api/harness/lessons", {
        method: "POST",
        body: JSON.stringify({
          projectId: input.projectId,
          cue: input.cue,
          lesson: input.lesson,
          evidence: input.evidence ?? "",
          confidence: input.confidence ?? "low",
        }),
      });
      await refresh(input.projectId);
    } catch (err) {
      lastError.value = (err as Error).message || "写入经验失败";
      throw err;
    }
  }

  async function saveFile(projectId: string, path: string, content: string) {
    try {
      await api("/api/harness/file", {
        method: "POST",
        body: JSON.stringify({ projectId, path, content }),
      });
      await refresh(projectId);
    } catch (err) {
      lastError.value = (err as Error).message || "保存失败";
      throw err;
    }
  }

  return {
    view,
    rounds,
    lastResult,
    loading,
    running,
    lastError,
    refresh,
    runRound,
    adopt,
    addLesson,
    saveFile,
    clearLastError,
  };
});
