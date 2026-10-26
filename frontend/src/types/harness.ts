/** 单次改动的结论：与后端 domain.Verdict 对齐。 */
export type HarnessVerdict = "helped" | "hurt" | "no observed effect" | "confounded";

export interface HarnessSkill {
  name: string;
  description: string;
  path: string;
  body: string;
}

export interface HarnessEntry {
  slug: string;
  inventory: string;
  cue?: string;
  lesson?: string;
  createdAt: string;
}

export interface HarnessLesson {
  id: string;
  projectId: string;
  roundId?: string;
  cue: string;
  lesson: string;
  evidence?: string;
  confidence: string;
  createdAt: string;
}

export interface HarnessEdit {
  id: string;
  roundId: string;
  component: string;
  candidates?: string;
  change: string;
  observed?: string;
  verdict: HarnessVerdict;
  createdAt: string;
}

export interface HarnessRound {
  id: string;
  projectId: string;
  sessionId?: string;
  mode: string;
  k: number;
  adopted?: string;
  keptBase: boolean;
  notes?: string;
  inventory?: string;
  alwaysOnChars: number;
  errors?: string[];
  createdAt: string;
}

/** 当前 harness：可编辑面（文件树）+ 常驻文本成本 + 校验问题 */
export interface HarnessView {
  projectId: string;
  dir: string;
  files: Record<string, string>;
  alwaysOn: string[];
  alwaysOnChars: number;
  skills: HarnessSkill[];
  scripts: string[];
  problems: string[];
  lessons: HarnessLesson[];
  entries: HarnessEntry[];
}

export interface HarnessProbe {
  label: string;
  score: number;
  observed?: string;
  confounded: boolean;
}

export interface HarnessCandidate {
  label: string;
  dim: string;
  tree: Record<string, string>;
  notes?: string;
  keptBase: boolean;
  probe: HarnessProbe;
}

export interface HarnessRoundResult {
  round: HarnessRound;
  candidates: HarnessCandidate[];
  edits: HarnessEdit[];
  lesson?: HarnessLesson;
  final: Record<string, string>;
  chosen: string;
}
