package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CompleteParams is a non-streaming chat/completions request (OpenAI-compatible).
type CompleteParams struct {
	BaseURL     string
	APIKey      string
	Model       string
	Messages    []ChatMessage
	Tools       json.RawMessage // JSON array of tool definitions; empty omits
	ToolChoice  string          // e.g. "auto"; empty omits
	Temperature float64
	TopP        float64
	TopK        *int
}

// ToolCallResult is one function call returned by the model.
type ToolCallResult struct {
	ID        string
	Name      string
	Arguments string
}

// CompleteResult is the assistant turn from a non-streaming completion.
type CompleteResult struct {
	Content   string
	ToolCalls []ToolCallResult
	// Usage from provider (optional).
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type completeResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Complete performs POST /chat/completions with stream=false.
func (c *Client) Complete(ctx context.Context, p CompleteParams) (*CompleteResult, error) {
	base := strings.TrimSpace(p.BaseURL)
	if strings.HasPrefix(base, "ttps://") {
		base = "h" + base
	} else if strings.HasPrefix(base, "ttp://") {
		base = "h" + base
	}
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		return nil, fmt.Errorf("model is empty")
	}
	body := map[string]any{
		"model":       model,
		"messages":    p.Messages,
		"stream":      false,
		"temperature": p.Temperature,
		"top_p":       p.TopP,
	}
	if p.TopK != nil {
		body["top_k"] = *p.TopK
	}
	if len(p.Tools) > 0 && string(p.Tools) != "null" {
		var toolsArr []any
		if err := json.Unmarshal(p.Tools, &toolsArr); err == nil && len(toolsArr) > 0 {
			body["tools"] = toolsArr
			tc := strings.TrimSpace(p.ToolChoice)
			if tc == "" {
				tc = "auto"
			}
			body["tool_choice"] = tc
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(base, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, string(b))
	}
	var cr completeResponse
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}
	if len(cr.Choices) == 0 {
		return &CompleteResult{}, nil
	}
	msg := cr.Choices[0].Message
	out := &CompleteResult{Content: messageContentString(msg.Content)}
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "" {
			continue
		}
		out.ToolCalls = append(out.ToolCalls, ToolCallResult{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	if cr.Usage != nil {
		out.PromptTokens = cr.Usage.PromptTokens
		out.CompletionTokens = cr.Usage.CompletionTokens
		out.TotalTokens = cr.Usage.TotalTokens
	}
	return out, nil
}

func messageContentString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := m["type"].(string); typ == "text" {
				if txt, ok := m["text"].(string); ok {
					b.WriteString(txt)
				}
			}
		}
		return b.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
