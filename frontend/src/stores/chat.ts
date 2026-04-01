import { defineStore } from "pinia";
import { ref, computed } from "vue";
import type {
  Message,
  Session,
  Project,
  SkillItem,
  ServerSettings,
  ProjectFilter,
  PartType,
} from "@/types/chat";
import { parseSSEChunk } from "@/lib/sse";
import { buildPathForSession, parseSiblingChoiceFromMeta } from "@/lib/thread";

const LS_UI = "clawmind_ui";

function loadDark(): boolean {
  try {
    const raw = localStorage.getItem(LS_UI);
    if (raw) return JSON.parse(raw).dark !== false;
  } catch {
    /* ignore */
  }
  return true;
}

function saveDark(dark: boolean) {
  localStorage.setItem(LS_UI, JSON.stringify({ dark }));
}

function defaultServerSettings(): ServerSettings {
  return {
    openaiBaseUrl: "https://api.openai.com/v1",
    openaiApiKey: "",
    openaiModel: "gpt-4o-mini",
    temperature: 0.7,
    topP: 1,
    maxAgentRounds: 16,
  };
}

export type BootState = "loading" | "ready" | "error";

/** 防止接口返回 null / 非数组导致 sessions.value.length 报错 */
function asArray<T>(data: unknown): T[] {
  return Array.isArray(data) ? data : [];
}

export const useChatStore = defineStore("chat", () => {
  const sessions = ref<Session[]>([]);
  const projects = ref<Project[]>([]);
  const skills = ref<SkillItem[]>([]);
  const projectFilter = ref<ProjectFilter>("all");
  const currentSessionId = ref<string | null>(null);
  const messages = ref<Message[]>([]);
  const streamingBuffers = ref<Record<string, string>>({});
  /** 流式阶段：按 partIndex 累积（Cursor 式任务 / 思考 / 正文等） */
  const streamingPartTexts = ref<Record<string, Record<number, string>>>({});
  const streamingPartTypes = ref<Record<string, Record<number, PartType>>>({});
  const streamingToolSteps = ref<
    Record<string, Array<{ id: string; name: string; args: string; result?: string }>>
  >({});
  const sending = ref(false);
  const streamingMessageId = ref<string | null>(null);
  /** 在收到 SSE done 或 error 后为 true，直到 fetch 完全结束 */
  const streamFinished = ref(false);
  const serverSettings = ref<ServerSettings>(defaultServerSettings());
  const darkMode = ref(loadDark());
  const bootState = ref<BootState>("loading");
  const bootError = ref<string | null>(null);
  const lastError = ref<string | null>(null);
  let abortCtrl: AbortController | null = null;

  const currentSession = computed(() =>
    asArray<Session>(sessions.value).find((s) => s.id === currentSessionId.value) ?? null
  );

  const siblingChoiceParsed = computed(() =>
    parseSiblingChoiceFromMeta(currentSession.value?.metadata)
  );

  const visiblePathMessages = computed(() =>
    buildPathForSession(asArray<Message>(messages.value), siblingChoiceParsed.value)
  );

  async function mergeSessionFromServer(sessionId: string) {
    try {
      const sess = await api<Session>(`/api/sessions/${sessionId}`);
      const list = asArray<Session>(sessions.value);
      const idx = list.findIndex((s) => s.id === sessionId);
      if (idx >= 0) {
        const next = [...list];
        next[idx] = sess;
        sessions.value = next;
      }
    } catch {
      /* ignore */
    }
  }

  function applyTheme() {
    if (darkMode.value) document.documentElement.classList.add("dark");
    else document.documentElement.classList.remove("dark");
    saveDark(darkMode.value);
  }
  applyTheme();

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

  async function loadServerSettings() {
    try {
      const data = await api<ServerSettings>("/api/settings");
      if (data && typeof data === "object") {
        serverSettings.value = { ...defaultServerSettings(), ...data };
      }
    } catch {
      serverSettings.value = defaultServerSettings();
    }
  }

  async function saveServerSettings(patch: Partial<ServerSettings>) {
    const merged = { ...serverSettings.value, ...patch };
    const body: Record<string, unknown> = {
      openaiBaseUrl: merged.openaiBaseUrl,
      openaiApiKey: merged.openaiApiKey,
      openaiModel: merged.openaiModel,
      temperature: merged.temperature,
      topP: merged.topP,
      maxAgentRounds: merged.maxAgentRounds,
    };
    if (merged.topK != null && merged.topK > 0) {
      body.topK = Math.floor(merged.topK);
    }
    await api("/api/settings", {
      method: "PUT",
      body: JSON.stringify(body),
    });
    await loadServerSettings();
  }

  async function refreshProjects() {
    const data = await api<Project[]>("/api/projects");
    projects.value = asArray<Project>(data);
  }

  async function refreshSkills() {
    const data = await api<SkillItem[]>("/api/skills");
    skills.value = asArray<SkillItem>(data);
  }

  async function refreshSessions() {
    let url = "/api/sessions";
    if (projectFilter.value === "unassigned") {
      url += "?projectId=unassigned";
    } else if (projectFilter.value !== "all") {
      url += `?projectId=${encodeURIComponent(projectFilter.value)}`;
    }
    const data = await api<Session[]>(url);
    sessions.value = asArray<Session>(data);
  }

  async function setProjectFilter(f: ProjectFilter) {
    projectFilter.value = f;
    await refreshSessions();
  }

  async function createProject(title: string) {
    await api<Project>("/api/projects", {
      method: "POST",
      body: JSON.stringify({ title: title.trim() || "新项目" }),
    });
    await refreshProjects();
  }

  async function importSkillsFile(file: File) {
    const fd = new FormData();
    fd.append("file", file);
    const res = await fetch("/api/skills/import", { method: "POST", body: fd });
    if (!res.ok) {
      const t = await res.text();
      throw new Error(t || res.statusText);
    }
    await refreshSkills();
  }

  async function createSkill(name: string, description: string) {
    await api("/api/skills", {
      method: "POST",
      body: JSON.stringify({
        name: name.trim(),
        description: description.trim(),
        parameters: {
          type: "object",
          properties: {
            input: { type: "string", description: "调用参数" },
          },
        },
      }),
    });
    await refreshSkills();
  }

  /** 首次进入或重试 */
  async function bootstrap() {
    bootState.value = "loading";
    bootError.value = null;
    currentSessionId.value = null;
    messages.value = [];
    try {
      await Promise.all([loadServerSettings(), refreshProjects(), refreshSkills(), refreshSessions()]);
      if (asArray<Session>(sessions.value).length > 0) {
        await selectSession(asArray<Session>(sessions.value)[0].id);
      } else {
        await newSession();
      }
      bootState.value = "ready";
    } catch (e) {
      const err = e as Error;
      let msg = err.message || "初始化失败";
      if (err instanceof TypeError && err.message === "Failed to fetch") {
        msg =
          "无法连接后端（请确认已运行 make run，且通过 http://127.0.0.1:5173 访问）。";
      }
      bootError.value = msg;
      bootState.value = "error";
    }
  }

  async function selectSession(id: string) {
    currentSessionId.value = id;
    streamingBuffers.value = {};
    streamingPartTexts.value = {};
    streamingPartTypes.value = {};
    streamingToolSteps.value = {};
    streamingMessageId.value = null;
    streamFinished.value = false;
    messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${id}/messages`));
    await mergeSessionFromServer(id);
    bootState.value = "ready";
    bootError.value = null;
  }

  async function newSession() {
    const body: Record<string, unknown> = {};
    if (serverSettings.value.openaiModel) {
      body.model = serverSettings.value.openaiModel;
    }
    if (projectFilter.value !== "all" && projectFilter.value !== "unassigned") {
      body.projectId = projectFilter.value;
    }
    const s = await api<Session>("/api/sessions", {
      method: "POST",
      body: JSON.stringify(body),
    });
    await refreshSessions();
    await selectSession(s.id);
    bootState.value = "ready";
    bootError.value = null;
  }

  async function deleteSession(id: string) {
    try {
      await api(`/api/sessions/${id}`, { method: "DELETE" });
    } catch (e) {
      lastError.value =
        e instanceof TypeError && e.message === "Failed to fetch"
          ? "无法连接后端，删除未生效。"
          : (e as Error).message || "删除会话失败";
      return;
    }
    if (currentSessionId.value === id) {
      currentSessionId.value = null;
      messages.value = [];
    }
    await refreshSessions();
    const sessList = asArray<Session>(sessions.value);
    if (currentSessionId.value === null && sessList.length > 0) {
      await selectSession(sessList[0].id);
    }
    if (currentSessionId.value === null && sessList.length === 0) {
      try {
        await newSession();
      } catch {
        bootState.value = "error";
        bootError.value = bootError.value ?? "无法创建新会话";
      }
    }
  }

  async function sendMessage(text: string, opts?: { editOfUserMessageId?: string }) {
    const sid = currentSessionId.value;
    if (!sid || !text.trim() || sending.value) return;
    lastError.value = null;
    sending.value = true;
    streamFinished.value = false;
    abortCtrl = new AbortController();
    try {
      const body: Record<string, unknown> = { content: text.trim() };
      if (opts?.editOfUserMessageId) {
        body.editOfUserMessageId = opts.editOfUserMessageId;
      } else {
        const path = buildPathForSession(asArray<Message>(messages.value), siblingChoiceParsed.value);
        if (path.length > 0) {
          body.parentMessageId = path[path.length - 1]!.id;
        }
      }
      const resp = await api<{ userMessageId: string; assistantMessageId: string }>(
        `/api/sessions/${sid}/messages`,
        {
          method: "POST",
          body: JSON.stringify(body),
          signal: abortCtrl.signal,
        }
      );
      messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sid}/messages`));
      await mergeSessionFromServer(sid);
      streamingMessageId.value = resp.assistantMessageId;
      streamingBuffers.value[resp.assistantMessageId] = "";
      streamingPartTexts.value[resp.assistantMessageId] = {};
      streamingPartTypes.value[resp.assistantMessageId] = {};
      streamingToolSteps.value[resp.assistantMessageId] = [];
      await consumeStream(sid, resp.assistantMessageId, abortCtrl.signal);
    } catch (e) {
      if ((e as Error).name === "AbortError") return;
      lastError.value =
        e instanceof TypeError && e.message === "Failed to fetch"
          ? "无法连接后端，请检查服务是否已启动。"
          : (e as Error).message || "发送失败";
      console.error(e);
      if (sid) {
        try {
          messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sid}/messages`));
        } catch {
          /* ignore */
        }
      }
    } finally {
      sending.value = false;
      streamingMessageId.value = null;
      streamFinished.value = false;
      abortCtrl = null;
      await refreshSessions().catch(() => {});
    }
  }

  async function regenerateAssistant(assistantMessageId: string) {
    const sid = currentSessionId.value;
    if (!sid || sending.value) return;
    lastError.value = null;
    sending.value = true;
    streamFinished.value = false;
    abortCtrl = new AbortController();
    try {
      const res = await fetch(
        `/api/sessions/${sid}/messages/${encodeURIComponent(assistantMessageId)}/regenerate`,
        { method: "POST", signal: abortCtrl.signal }
      );
      if (!res.ok) {
        const t = await res.text();
        throw new Error(t || res.statusText);
      }
      messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sid}/messages`));
      streamingMessageId.value = assistantMessageId;
      streamingBuffers.value[assistantMessageId] = "";
      streamingPartTexts.value[assistantMessageId] = {};
      streamingPartTypes.value[assistantMessageId] = {};
      streamingToolSteps.value[assistantMessageId] = [];
      await consumeStream(sid, assistantMessageId, abortCtrl.signal);
    } catch (e) {
      if ((e as Error).name === "AbortError") return;
      lastError.value =
        e instanceof TypeError && e.message === "Failed to fetch"
          ? "无法连接后端，请检查服务是否已启动。"
          : (e as Error).message || "重新生成失败";
      console.error(e);
      if (sid) {
        try {
          messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sid}/messages`));
        } catch {
          /* ignore */
        }
      }
    } finally {
      sending.value = false;
      streamingMessageId.value = null;
      streamFinished.value = false;
      abortCtrl = null;
      await refreshSessions().catch(() => {});
    }
  }

  async function selectBranchUserVersion(parentKey: string, userMessageId: string) {
    const sid = currentSessionId.value;
    if (!sid) return;
    try {
      const next = { ...siblingChoiceParsed.value, [parentKey]: userMessageId };
      await api(`/api/sessions/${sid}`, {
        method: "PATCH",
        body: JSON.stringify({ siblingChoice: next }),
      });
      await mergeSessionFromServer(sid);
      messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sid}/messages`));
    } catch (e) {
      lastError.value =
        e instanceof TypeError && e.message === "Failed to fetch"
          ? "无法连接后端。"
          : (e as Error).message || "切换版本失败";
    }
  }

  async function consumeStream(sessionId: string, assistantMessageId: string, signal: AbortSignal) {
    streamFinished.value = false;
    const res = await fetch(
      `/api/sessions/${sessionId}/stream?messageId=${encodeURIComponent(assistantMessageId)}`,
      {
        signal,
        cache: "no-store",
        headers: { Accept: "text/event-stream" },
      }
    );
    if (!res.ok || !res.body) {
      throw new Error("stream failed");
    }
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let carry = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      carry = parseSSEChunk(carry, dec.decode(value, { stream: true }), (ev) => {
        const mid = ev.messageId;
        if (!mid) return;
        if (ev.type === "part_start" && ev.partIndex !== undefined && ev.partType) {
          const prev = streamingPartTexts.value[mid] ?? {};
          const prevT = streamingPartTypes.value[mid] ?? {};
          streamingPartTexts.value = {
            ...streamingPartTexts.value,
            [mid]: { ...prev, [ev.partIndex]: "" },
          };
          streamingPartTypes.value = {
            ...streamingPartTypes.value,
            [mid]: { ...prevT, [ev.partIndex]: ev.partType },
          };
        }
        if (ev.type === "delta" && ev.text !== undefined && ev.partIndex !== undefined) {
          const bag = streamingPartTexts.value[mid];
          if (bag && bag[ev.partIndex] !== undefined) {
            streamingPartTexts.value = {
              ...streamingPartTexts.value,
              [mid]: { ...bag, [ev.partIndex]: (bag[ev.partIndex] ?? "") + ev.text },
            };
          }
          streamingBuffers.value[mid] = (streamingBuffers.value[mid] ?? "") + ev.text;
        }
        if (ev.type === "tool_approval_request" && ev.approvalId && ev.sessionId) {
          const preview = (ev.arguments ?? "").trim() || ev.toolName || "shell 命令";
          const ok = window.confirm(`高危 shell 需确认（来自 Agent）：\n\n${preview.slice(0, 800)}`);
          void fetch(
            `/api/sessions/${encodeURIComponent(ev.sessionId)}/tool-approval`,
            {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ approvalId: ev.approvalId, approve: ok }),
            }
          ).catch(() => {});
        }
        if (ev.type === "tool_call" && ev.toolCallId) {
          const cur = [...(streamingToolSteps.value[mid] ?? [])];
          cur.push({
            id: ev.toolCallId,
            name: ev.toolName ?? "?",
            args: ev.arguments ?? "",
          });
          streamingToolSteps.value = { ...streamingToolSteps.value, [mid]: cur };
        }
        if (ev.type === "tool_result" && ev.toolCallId) {
          const steps = streamingToolSteps.value[mid];
          if (steps) {
            const next = steps.map((s) =>
              s.id === ev.toolCallId ? { ...s, result: ev.result ?? "" } : s
            );
            streamingToolSteps.value = { ...streamingToolSteps.value, [mid]: next };
          }
        }
        if (ev.type === "done" || ev.type === "error") {
          streamFinished.value = true;
        }
        if (ev.type === "error") {
          console.error(ev.error);
        }
      });
    }
    streamFinished.value = true;
    messages.value = asArray<Message>(await api<Message[]>(`/api/sessions/${sessionId}/messages`));
    const next = { ...streamingBuffers.value };
    delete next[assistantMessageId];
    streamingBuffers.value = next;
    const nextP = { ...streamingPartTexts.value };
    delete nextP[assistantMessageId];
    streamingPartTexts.value = nextP;
    const nextTypes = { ...streamingPartTypes.value };
    delete nextTypes[assistantMessageId];
    streamingPartTypes.value = nextTypes;
    const nextTools = { ...streamingToolSteps.value };
    delete nextTools[assistantMessageId];
    streamingToolSteps.value = nextTools;
  }

  async function stopGeneration() {
    const sid = currentSessionId.value;
    const mid = streamingMessageId.value;
    if (!sid || !mid) {
      abortCtrl?.abort();
      return;
    }
    try {
      await fetch(`/api/sessions/${sid}/messages/${mid}/cancel`, { method: "POST" });
    } catch {
      /* ignore */
    }
    abortCtrl?.abort();
  }

  function setDarkMode(v: boolean) {
    darkMode.value = v;
    applyTheme();
  }

  function clearLastError() {
    lastError.value = null;
  }

  return {
    sessions,
    projects,
    skills,
    projectFilter,
    currentSessionId,
    messages,
    streamingBuffers,
    streamingPartTexts,
    streamingPartTypes,
    streamingToolSteps,
    sending,
    streamingMessageId,
    streamFinished,
    serverSettings,
    darkMode,
    bootState,
    bootError,
    lastError,
    currentSession,
    siblingChoiceParsed,
    visiblePathMessages,
    mergeSessionFromServer,
    bootstrap,
    loadServerSettings,
    saveServerSettings,
    refreshProjects,
    refreshSkills,
    refreshSessions,
    setProjectFilter,
    createProject,
    importSkillsFile,
    createSkill,
    selectSession,
    newSession,
    deleteSession,
    sendMessage,
    regenerateAssistant,
    selectBranchUserVersion,
    stopGeneration,
    setDarkMode,
    clearLastError,
  };
});
