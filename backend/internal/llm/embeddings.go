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

// EmbedText calls POST /embeddings (OpenAI-compatible) and returns float32 vector.
func (c *Client) EmbedText(ctx context.Context, baseURL, apiKey, model, text string) ([]float32, error) {
	base := strings.TrimSpace(baseURL)
	if strings.HasPrefix(base, "ttps://") {
		base = "h" + base
	} else if strings.HasPrefix(base, "ttp://") {
		base = "h" + base
	}
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	m := strings.TrimSpace(model)
	if m == "" {
		return nil, fmt.Errorf("embedding model is empty")
	}
	body, err := json.Marshal(map[string]any{
		"model": m,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(base, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
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
		return nil, fmt.Errorf("embeddings http %d: %s", resp.StatusCode, string(b))
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	v := parsed.Data[0].Embedding
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out, nil
}
