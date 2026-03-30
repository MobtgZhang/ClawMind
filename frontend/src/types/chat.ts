export type MessageRole = "user" | "assistant" | "system" | "tool";

export type PartType =
  | "text"
  | "code"
  | "tool_call"
  | "tool_result"
  | "reasoning"
  | "task_flow"
  | "thinking";

export interface Part {
  type: PartType;
  text?: string;
  language?: string;
  tool_call_id?: string;
  name?: string;
  arguments?: string;
  result?: string;
}

export interface Project {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
}

export interface SkillItem {
  name: string;
  description: string;
}

/** 与后端 GET/PUT /api/settings 及 .clawmind/config.json 对齐（系统提示仅由服务端 config / 环境变量决定，不在 UI 编辑） */
export interface ServerSettings {
  openaiBaseUrl: string;
  openaiApiKey: string;
  openaiModel: string;
  temperature: number;
  topP: number;
  topK?: number;
  maxAgentRounds: number;
}

export interface Session {
  id: string;
  title: string;
  model: string;
  projectId?: string;
  createdAt: string;
  updatedAt: string;
  /** 含 siblingChoice（JSON 字符串）等，与后端 sessions.metadata_json 对齐 */
  metadata?: Record<string, string>;
}

export interface Message {
  id: string;
  sessionId: string;
  role: MessageRole;
  createdAt: string;
  parts: Part[];
  parentMessageId?: string;
  branchId?: string;
}

export type StreamEventType =
  | "delta"
  | "part_start"
  | "part_end"
  | "tool_call"
  | "tool_result"
  | "done"
  | "error";

export interface StreamEvent {
  type: StreamEventType;
  messageId?: string;
  partIndex?: number;
  partType?: PartType;
  text?: string;
  toolCallId?: string;
  toolName?: string;
  arguments?: string;
  result?: string;
  error?: string;
}

/** 采样预设（与设置页按钮一致） */
export const SAMPLING_PRESETS = {
  precise: { label: "精确", temperature: 0.2, topP: 0.5, topK: 5 },
  balanced: { label: "平衡", temperature: 0.7, topP: 0.9, topK: 40 },
  creative: { label: "创意", temperature: 1.0, topP: 0.95, topK: 200 },
} as const;

export type ProjectFilter = "all" | "unassigned" | string;
