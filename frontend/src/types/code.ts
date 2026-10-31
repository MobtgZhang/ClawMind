/** GET /api/code 的响应形状（backend/internal/api/code.go + internal/codectx）。 */

export interface LanguageStat {
  name: string;
  files: number;
  bytes: number;
}

export interface Commands {
  test?: string;
  build?: string;
  lint?: string;
  format?: string;
}

export interface Profile {
  root: string;
  isGit: boolean;
  branch?: string;
  languages: LanguageStat[];
  manifests: string[];
  commands: Commands;
  tree: string[];
  scanned: number;
  truncated: boolean;
  testFiles: number;
  hasCI: boolean;
}

export interface Budget {
  class: "simple" | "complex";
  score: number;
  reasons: string[];
  loop: "linear" | "graph";
  maxNodes: number;
  maxRounds: number;
  reentry: number;
  fanout: number;
  searchFiles: number;
  searchResults: number;
  contextTokens: number;
  promptChars: number;
  graph: boolean;
  verify: boolean;
  review: boolean;
}

export interface GraphShape {
  files: number;
  symbols: number;
  edges: number;
  modules: number;
  maxFanIn: number;
  cycles: number;
}

export interface GraphStats {
  enabled: boolean;
  shape: GraphShape;
  parsed: number;
  reused: number;
  skipped: number;
  dirty: boolean;
  truncated: boolean;
  builtAt?: string;
}

export interface CodeWorkspace {
  workspace: string;
  class: "simple" | "complex";
  score: number;
  reasons: string[];
  loop: "linear" | "graph";
  budget: Budget;
  profile?: Profile;
  graph?: GraphStats;
}
