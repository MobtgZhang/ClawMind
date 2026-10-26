package memory

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Budget caps retrieval so memory cannot overwhelm the context window. Three limits matter:
// how many items, how many characters, and how long retrieval may take.
type Budget struct {
	MaxItems   int
	CharBudget int
	Timeout    time.Duration
}

func DefaultBudget() Budget {
	return Budget{MaxItems: 24, CharBudget: 2000, Timeout: 3 * time.Second}
}

func (b Budget) normalized() Budget {
	out := b
	if out.MaxItems <= 0 {
		out.MaxItems = DefaultBudget().MaxItems
	}
	if out.MaxItems > 100 {
		out.MaxItems = 100
	}
	if out.CharBudget <= 0 {
		out.CharBudget = DefaultBudget().CharBudget
	}
	if out.Timeout <= 0 {
		out.Timeout = DefaultBudget().Timeout
	}
	return out
}

// RRFConstant is the Reciprocal Rank Fusion damping constant. A rank-based fuse is used instead
// of adding scores because BM25 and cosine similarity are not on the same scale.
const RRFConstant = 60

// Fuse combines ranked lists with Reciprocal Rank Fusion.
func Fuse(lists [][]string, k int) map[string]float64 {
	if k <= 0 {
		k = RRFConstant
	}
	out := map[string]float64{}
	for _, list := range lists {
		for i, item := range list {
			out[item] += 1 / float64(k+i+1)
		}
	}
	return out
}

// Retriever performs layered retrieval: bootstrap layers give quick context, fallback layers are
// recalled with BM25 + vector + RRF when specific facts are needed.
type Retriever struct {
	Embed  EmbedFunc
	Budget Budget
}

func NewRetriever(embed EmbedFunc) *Retriever {
	return &Retriever{Embed: embed, Budget: DefaultBudget()}
}

// Search returns lines for a query over a candidate set. The budget is applied here, at the
// point where memory would otherwise flood the prompt.
func (r *Retriever) Search(ctx context.Context, rows []Row, query string) ([]string, error) {
	budget := r.Budget.normalized()
	if len(rows) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, budget.Timeout)
	defer cancel()

	bootstrap := r.bootstrap(rows)
	fallback, err := r.fallback(ctx, rows, query)
	if err != nil {
		// A retrieval budget overrun or embedding failure degrades to BM25, not to nothing.
		fallback = nil
	}

	var out []string
	seen := map[string]struct{}{}
	used := 0
	add := func(line string) bool {
		if _, ok := seen[line]; ok {
			return true
		}
		if len(out) >= budget.MaxItems || used+len(line) > budget.CharBudget {
			return false
		}
		seen[line] = struct{}{}
		out = append(out, line)
		used += len(line)
		return true
	}
	for _, line := range bootstrap {
		if !add(line) {
			break
		}
	}
	for _, line := range fallback {
		if !add(line) {
			break
		}
	}
	return out, nil
}

// bootstrap takes the quick-context layers in recency order.
func (r *Retriever) bootstrap(rows []Row) []string {
	bootstrap := map[Layer]struct{}{}
	for _, l := range BootstrapLayers() {
		bootstrap[l] = struct{}{}
	}
	var out []string
	for _, row := range rows {
		if _, ok := bootstrap[row.Layer]; ok {
			out = append(out, row.Line())
		}
	}
	return out
}

// fallback ranks the fact layers with BM25 and, when embeddings exist, with vector similarity,
// then fuses the two rankings.
func (r *Retriever) fallback(ctx context.Context, rows []Row, query string) ([]string, error) {
	fallback := map[Layer]struct{}{}
	for _, l := range FallbackLayers() {
		fallback[l] = struct{}{}
	}
	var pool []Row
	for _, row := range rows {
		if _, ok := fallback[row.Layer]; ok {
			pool = append(pool, row)
		}
	}
	if len(pool) == 0 {
		return nil, nil
	}
	docs := make([]string, len(pool))
	for i, row := range pool {
		docs[i] = row.Content
	}
	var lists [][]string
	if scores := BM25Scores(query, docs); scores != nil {
		lists = append(lists, order(pool, scores))
	}
	if r.Embed != nil {
		qvec, err := r.Embed(ctx, query)
		if err != nil {
			return lists[0], err
		}
		if len(qvec) > 0 {
			scores := make([]float64, len(pool))
			for i, row := range pool {
				scores[i] = cosineSim(qvec, row.Embedding)
			}
			if ranked := order(pool, scores); len(ranked) > 0 {
				lists = append(lists, ranked)
			}
		}
	}
	if len(lists) == 0 {
		return nil, nil
	}
	fused := Fuse(lists, RRFConstant)
	type item struct {
		line  string
		score float64
	}
	var items []item
	for line, score := range fused {
		items = append(items, item{line, score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].line < items[j].line
	})
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.line)
	}
	return out, nil
}

func order(rows []Row, scores []float64) []string {
	type item struct {
		line  string
		score float64
	}
	var items []item
	for i, row := range rows {
		if i >= len(scores) || scores[i] <= 0 {
			continue
		}
		items = append(items, item{row.Line(), scores[i]})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].line < items[j].line
	})
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.line)
	}
	return out
}

// SplitSentences splits a raw turn into candidate atoms without a model call.
func SplitSentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, part := range strings.FieldsFunc(line, func(r rune) bool {
			return strings.ContainsRune("。；;！!？?", r)
		}) {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
