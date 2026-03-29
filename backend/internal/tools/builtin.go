package tools

// AtomicTools returns built-in ClawMind agent tools (OpenAI function schema).
func AtomicTools() []Definition {
	return []Definition{
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "file_read",
				Description: "Read UTF-8 text from a file under the agent workspace (relative path only).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "Relative path under workspace"},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "file_write",
				Description: "Write UTF-8 text to a file under the agent workspace (creates parent dirs).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string", "description": "Relative path under workspace"},
						"content": map[string]any{"type": "string", "description": "Full file content"},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "shell_exec",
				Description: "Run a shell command (Windows: cmd /C; macOS/Linux: sh -c). Use with care.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string", "description": "Command string"},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "web_fetch",
				Description: "HTTP GET a public URL and return truncated plain text (HTML stripped loosely). Not a structured API client.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{"type": "string", "description": "http or https URL"},
					},
					"required": []string{"url"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "task_plan",
				Description: "Generate a Markdown todo list for sub-agents from a goal (uses a short internal completion).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"goal": map[string]any{"type": "string", "description": "What to accomplish"},
					},
					"required": []string{"goal"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSpec{
				Name:        "task_summary",
				Description: "Summarize long text into concise bullets (uses a short internal completion).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string", "description": "Text to summarize"},
					},
					"required": []string{"text"},
				},
			},
		},
	}
}

// MergeDefinitions concatenates tool lists (atomic first, then file, then user).
func MergeDefinitions(parts ...[]Definition) []Definition {
	var out []Definition
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
