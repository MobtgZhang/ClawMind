package memory

import (
	"math"
	"strings"
	"unicode"
)

// BM25 parameters. These are the standard Okapi values; the point of exposing them is that the
// retrieval budget, not the formula, is what keeps memory from overwhelming the context window.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Tokenize splits ASCII words and single CJK runes. A Chinese query has no spaces, so a term
// list built on whitespace alone never matches anything.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, string(word))
			word = nil
		}
	}
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// BM25Scores scores each document against the query terms. Documents with no query term score 0
// and are never returned, so retrieval stays selective instead of "everything recent".
func BM25Scores(query string, docs []string) []float64 {
	terms := Tokenize(query)
	n := len(docs)
	if len(terms) == 0 || n == 0 {
		return nil
	}
	docTerms := make([][]string, n)
	df := map[string]int{}
	total := 0
	for i, doc := range docs {
		t := Tokenize(doc)
		docTerms[i] = t
		total += len(t)
		seen := map[string]struct{}{}
		for _, w := range t {
			if _, ok := seen[w]; !ok {
				seen[w] = struct{}{}
				df[w]++
			}
		}
	}
	if total == 0 {
		return nil
	}
	avgdl := float64(total) / float64(n)
	scores := make([]float64, n)
	for i, t := range docTerms {
		tf := map[string]int{}
		for _, w := range t {
			tf[w]++
		}
		for _, q := range terms {
			f := tf[q]
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (float64(n)-float64(df[q])+0.5)/(float64(df[q])+0.5))
			denom := float64(f) + bm25K1*(1-bm25B+bm25B*float64(len(t))/avgdl)
			scores[i] += idf * float64(f) * (bm25K1 + 1) / denom
		}
	}
	return scores
}
