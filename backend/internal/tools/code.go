package tools

// CodeTools are the code-project tools: locate, edit, verify, and load on-demand skills.
func CodeTools() []Definition {
	return []Definition{
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "code_search",
				Description: "Search the workspace for a literal or regex pattern. Returns file:line matches, bounded. Use this before reading or editing: never assume file contents.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":            map[string]any{"type": "string", "description": "Pattern to search for"},
						"path":             map[string]any{"type": "string", "description": "Optional relative subdirectory"},
						"glob":             map[string]any{"type": "string", "description": "Optional filename filter, e.g. *.go"},
						"regex":            map[string]any{"type": "boolean", "description": "Treat query as a regular expression"},
						"case_insensitive": map[string]any{"type": "boolean"},
						"max_results":      map[string]any{"type": "integer", "description": "Max matches, default 60"},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "code_graph",
				Description: "Query the workspace symbol graph. This is the only tool that answers who depends on a symbol: definition, callers (impact), callees, module deps, import cycles, stats. The index is heuristic (parsed, not type-checked), so report a hit as a candidate rather than a confirmed call site.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"mode":  map[string]any{"type": "string", "enum": []string{"definition", "callers", "impact", "callees", "module", "cycles", "stats"}, "description": "Query mode"},
						"query": map[string]any{"type": "string", "description": "Symbol, file, or package name. Required except for stats"},
						"depth": map[string]any{"type": "integer", "description": "Traversal depth for callers/impact/callees, default 2, max 4"},
						"limit": map[string]any{"type": "integer", "description": "Max rows, default 40"},
					},
					"required": []string{"mode"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "repo_tree",
				Description: "List the workspace structure (bounded depth and entry count), skipping dependency and build directories.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":        map[string]any{"type": "string", "description": "Relative directory, default workspace root"},
						"depth":       map[string]any{"type": "integer", "description": "Max depth, default 3"},
						"max_entries": map[string]any{"type": "integer", "description": "Max entries, default 240"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "file_edit",
				Description: "Replace an exact string in a file under the workspace. Fails if the old text is absent or ambiguous, so the model must re-read rather than guess.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":        map[string]any{"type": "string", "description": "Relative path under workspace"},
						"old":         map[string]any{"type": "string", "description": "Exact text to replace, with enough surrounding context to be unique"},
						"new":         map[string]any{"type": "string", "description": "Replacement text"},
						"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence"},
					},
					"required": []string{"path", "old", "new"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "run_tests",
				Description: "Run the project's verification command (inferred from go.mod / package.json / pyproject.toml / Cargo.toml / Makefile, or given explicitly). Run after every edit; read the full failure output.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command":         map[string]any{"type": "string", "description": "Optional explicit command; otherwise the inferred test command is used"},
						"timeout_seconds": map[string]any{"type": "integer", "description": "Default 120"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "load_skill",
				Description: "Load the body of one harness skill by name. Skills listed in the system prompt are on demand: they cost nothing until invoked.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string", "description": "Skill name as listed in the available-skills index"},
					},
					"required": []string{"name"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "delegate",
				Description: "Run one harness subagent in its own context and return only its answer. Use for bounded investigations with large input and small output: the subagent's working context is not paid on every turn.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"agent": map[string]any{"type": "string", "description": "Subagent name as listed in the delegatable-subagents index"},
						"task":  map[string]any{"type": "string", "description": "The question or task handed to the subagent"},
					},
					"required": []string{"agent", "task"},
				},
			},
		},
	}
}
