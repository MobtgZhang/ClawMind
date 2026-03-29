package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

// Config holds defaults for HTTP client only (endpoints come per-request via StreamParams).
type Config struct {
	HTTPClient *http.Client
}

// StreamParams is one chat/completions call (OpenAI-compatible).
type StreamParams struct {
	BaseURL     string
	APIKey      string
	Model       string
	Messages    []ChatMessage
	Tools       json.RawMessage // optional tools JSON array
	Temperature float64
	TopP        float64
	TopK        *int
}

// ToolCallOut is an assistant message tool invocation (OpenAI shape).
type ToolCallOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatMessage is one turn for the completions API.
type ChatMessage struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"`
	ToolCalls    []ToolCallOut `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	FunctionCall *struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function_call,omitempty"`
}

// Client streams chat completions.
type Client struct {
	cfg Config
}

func NewClient(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 0}
	}
	return &Client{cfg: cfg}
}

type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature"`
	TopP        float64         `json:"top_p"`
	TopK        *int            `json:"top_k,omitempty"`
	Tools       json.RawMessage `json:"tools,omitempty"`
}

type streamResponseLine struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamChat calls POST /chat/completions with stream=true.
func (c *Client) StreamChat(ctx context.Context, p StreamParams, onChunk func(string) error) error {
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
		return fmt.Errorf("model is empty")
	}
	reqBody := chatRequest{
		Model:       model,
		Messages:    p.Messages,
		Stream:      true,
		Temperature: p.Temperature,
		TopP:        p.TopP,
		TopK:        p.TopK,
		Tools:       p.Tools,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	url := strings.TrimRight(base, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("llm http %d: %s", resp.StatusCode, string(b))
	}
	return readSSEStream(ctx, resp.Body, onChunk)
}

func readSSEStream(ctx context.Context, r io.Reader, onChunk func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var pending string
	flush := func() error {
		if pending == "" {
			return nil
		}
		payload := pending
		pending = ""
		if payload == "[DONE]" {
			return io.EOF
		}
		var ev streamResponseLine
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil
		}
		if len(ev.Choices) == 0 {
			return nil
		}
		d := ev.Choices[0].Delta
		if d.Content != "" {
			return onChunk(d.Content)
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !sc.Scan() {
			_ = flush()
			return sc.Err()
		}
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			pending = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
}

// ToChatMessages converts domain messages to API format (text parts only for MVP).
func ToChatMessages(systemPrompt string, history []domain.Message) []ChatMessage {
	var out []ChatMessage
	if systemPrompt != "" {
		out = append(out, ChatMessage{Role: "system", Content: systemPrompt})
	}
	for _, m := range history {
		var b strings.Builder
		for _, p := range m.Parts {
			switch p.Type {
			case domain.PartText, domain.PartReasoning, domain.PartTaskFlow, domain.PartThinking:
				b.WriteString(p.Text)
			case domain.PartCode:
				b.WriteString("```" + p.Language + "\n" + p.Text + "\n```\n")
			case domain.PartToolResult:
				b.WriteString("[tool " + p.ToolCallID + "] " + p.Result + "\n")
			default:
				b.WriteString(p.Text)
			}
		}
		if b.Len() == 0 {
			continue
		}
		out = append(out, ChatMessage{Role: string(m.Role), Content: b.String()})
	}
	return out
}

// DefaultHTTPClient returns a client suitable for long-lived streams.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}
