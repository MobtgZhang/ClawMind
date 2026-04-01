package llm

import "context"

// Provider is the chat surface the agent needs. The default *Client implements this via OpenAI-compatible HTTP.
// Other backends (e.g. native Anthropic) can be wired by implementing this interface.
type Provider interface {
	Complete(ctx context.Context, p CompleteParams) (*CompleteResult, error)
	StreamChat(ctx context.Context, p StreamParams, onChunk func(string) error) error
}

var _ Provider = (*Client)(nil)
