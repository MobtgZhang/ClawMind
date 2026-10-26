/** 插件目录解析后的视图：声明了什么、贡献了什么、哪一层赢了。 */
export interface PluginView {
  id: string;
  name: string;
  version: string;
  description: string;
  seams: string[];
  enabled: boolean;
  layer: number;
  tools: string[];
  skills: string[];
  subagents: string[];
  hooks: Record<string, number>;
  patch: string[];
  dir: string;
  problems?: string[];
}

export interface MemoryLayerRow {
  layer: number;
  name: string;
  label: string;
  count: number;
  cap: number;
}

/** 后端 Budget 没有 json tag，字段名按 Go 字段序列化；Timeout 是纳秒。 */
export interface MemoryBudget {
  MaxItems: number;
  CharBudget: number;
  Timeout: number;
}

export interface MemoryCensus {
  layers: MemoryLayerRow[];
  budget: MemoryBudget;
  note?: string;
}

/** 按层解析后的配置：这是看到哪一层真正生效的唯一途径。 */
export interface PluginConfig {
  home: string;
  layers: number;
  config: Record<string, Record<string, unknown>>;
}
