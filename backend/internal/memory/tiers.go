package memory

import "strconv"

// Layer is one level of the memory hierarchy. Memory is not flat records: raw conversation is
// distilled layer by layer, and each layer answers a different question.
type Layer int

const (
	// LayerConversation keeps raw turns: exact wording, timestamps and sources.
	LayerConversation Layer = 0
	// LayerAtom keeps facts, preferences, constraints and events — precise recall.
	LayerAtom Layer = 1
	// LayerScenario keeps knowledge blocks organised around a project — quick context restore.
	LayerScenario Layer = 2
	// LayerPersona keeps long-term profiles and stable patterns — fast entry into context.
	LayerPersona Layer = 3
)

// Layers returns the hierarchy in distillation order.
func Layers() []Layer {
	return []Layer{LayerConversation, LayerAtom, LayerScenario, LayerPersona}
}

// BootstrapLayers are the layers that give quick context; the rest are recalled on demand.
func BootstrapLayers() []Layer { return []Layer{LayerScenario, LayerPersona} }

// FallbackLayers are the layers BM25 + vector + RRF fall back to when specific facts are needed.
func FallbackLayers() []Layer { return []Layer{LayerConversation, LayerAtom} }

func (l Layer) Name() string {
	switch l {
	case LayerConversation:
		return "conversation"
	case LayerAtom:
		return "atom"
	case LayerScenario:
		return "scenario"
	case LayerPersona:
		return "persona"
	}
	return "atom"
}

func (l Layer) Label() string { return "[L" + strconv.Itoa(int(l)) + "]" }

// Row is one memory entry on one layer.
type Row struct {
	ID        string    `json:"id"`
	Layer     Layer     `json:"layer"`
	SessionID string    `json:"sessionId,omitempty"`
	ProjectID string    `json:"projectId,omitempty"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	CreatedAt int64     `json:"createdAt"`
	Embedding []float32 `json:"-"`
}

// Line renders a row the way the agent sees it.
func (r Row) Line() string { return r.Layer.Label() + " [" + r.Kind + "] " + r.Content }
