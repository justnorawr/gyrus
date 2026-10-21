// Package eval measures retrieval over a wiki store against a labelled query set (queries.json).
package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/armckinney/gyrus/internal/domain/lifecycle"
	searchokf "github.com/armckinney/gyrus/internal/provider/search/okf"
	"github.com/armckinney/gyrus/internal/provider/storage/wiki"
	"github.com/armckinney/gyrus/internal/textrank"
	"github.com/armckinney/gyrus/pkg/gyrus"
)

const layerBudget = 1500 // the prompt hook's budget

type query struct {
	Source string   `json:"source"`
	Lookup int      `json:"lookup,omitempty"`
	Query  string   `json:"query"`
	Expect []string `json:"expect"`
}

type variant struct {
	name string
	opts textrank.Options
}

var variants = []variant{
	{"stopwords off, plurals folded (default)", textrank.Options{}},
	{"stopwords kept, plurals folded", textrank.Options{KeepStopwords: true}},
	{"stopwords off, plurals kept", textrank.Options{NoPluralFold: true}},
	{"stopwords kept, plurals kept", textrank.Options{KeepStopwords: true, NoPluralFold: true}},
}

// scores accumulates per-query metrics for one variant and one slice of the query set.
type scores struct {
	n                                int
	r1, r3, r5, mrr, tokens, inLayer float64
}

func (s *scores) add(ranked []string, layer string, expect []string) {
	s.n++
	recall := func(k int) float64 {
		hit := 0
		for _, id := range expect {
			for i := 0; i < k && i < len(ranked); i++ {
				if ranked[i] == id {
					hit++
					break
				}
			}
		}
		return float64(hit) / float64(len(expect))
	}
	s.r1 += recall(1)
	s.r3 += recall(3)
	s.r5 += recall(5)
	for i, id := range ranked {
		if contains(expect, id) {
			s.mrr += 1 / float64(i+1)
			break
		}
	}
	s.tokens += float64(lifecycle.EstimateTokensFromBytes(layer))
	found := 0
	for _, id := range expect {
		if strings.Contains(layer, "--- ["+wiki.Name("root", id)+"]") {
			found++
		}
	}
	s.inLayer += float64(found) / float64(len(expect))
}

func (s scores) row(name string) string {
	n := float64(s.n)
	return fmt.Sprintf("| %s | %d | %.2f | %.2f | %.2f | %.3f | %.0f | %.2f |", name, s.n, s.r1/n, s.r3/n, s.r5/n, s.mrr/n, s.tokens/n, s.inLayer/n)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// [Test Level]: Evaluation (opt-in: set GYRUS_EVAL_WIKI to a wiki store; GYRUS_EVAL_OUT to also write the table)
// [Purpose]: Measures retrieval over a real wiki store for each tokenizer variant: stopwords dropped or kept, plurals
// folded or kept.
// [Execution Surface]: wiki storage + OKF search + lifecycle engine, all queries in queries.json, scope "all"
// [Assertions]: every expected id exists in the store (stale labels fail the run). Reports recall@1/3/5 and MRR of
// the document ranking, plus the estimated tokens of the 1500-token context layer and the share of expected
// documents that reach it, overall and for real and written queries apart.
// -----------------------------------------------------------------------------
func TestRetrievalVariants(t *testing.T) {
	root := os.Getenv("GYRUS_EVAL_WIKI")
	if root == "" {
		t.Skip("set GYRUS_EVAL_WIKI to a wiki store to run the retrieval evaluation")
	}
	data, err := os.ReadFile("queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Queries []query `json:"queries"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatalf("queries.json: %v", err)
	}
	for _, q := range set.Queries {
		for _, id := range q.Expect {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(id)+".md")); err != nil {
				t.Fatalf("stale label: %q expects %s, which is not in the store", q.Query, id)
			}
		}
	}

	ctx := context.Background()
	defer textrank.Configure(textrank.Options{})
	var out strings.Builder
	fmt.Fprintf(&out, "Store %s, %d queries. Ranking: top 10 of search, scope all. Layer: suggest-context at %d tokens.\n\n", root, len(set.Queries), layerBudget)
	out.WriteString("| Variant | Queries | R@1 | R@3 | R@5 | MRR | Layer tokens | Expected in layer |\n|---|---|---|---|---|---|---|---|\n")
	for _, v := range variants {
		textrank.Configure(v.opts)
		store, err := wiki.NewStore(root, "root")
		if err != nil {
			t.Fatal(err)
		}
		store.SetReportWriter(nil)
		search, err := searchokf.NewSearchProvider(store)
		if err != nil {
			t.Fatal(err)
		}
		engine := lifecycle.NewEngine(store, search, nil, nil, "")
		all, realQ, written := scores{}, scores{}, scores{}
		filter := gyrus.SearchFilter{Scope: "all"}
		for _, q := range set.Queries {
			results, err := engine.SearchQuery(ctx, gyrus.SearchQuery{Query: q.Query, Filter: filter, MaxResults: 10})
			if err != nil {
				t.Fatal(err)
			}
			var ranked []string
			for _, r := range results {
				rel, _ := filepath.Rel(root, filepath.FromSlash(r.Concept.FilePath))
				ranked = append(ranked, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
			}
			layer, err := engine.SuggestContextWithBudget(ctx, q.Query, filter, 5, layerBudget)
			if err != nil {
				t.Fatal(err)
			}
			all.add(ranked, layer, q.Expect)
			if q.Source == "real" {
				realQ.add(ranked, layer, q.Expect)
			} else {
				written.add(ranked, layer, q.Expect)
			}
		}
		out.WriteString(all.row(v.name+", all") + "\n")
		out.WriteString(realQ.row(v.name+", real") + "\n")
		out.WriteString(written.row(v.name+", written") + "\n")
	}
	t.Log("\n" + out.String())
	if path := os.Getenv("GYRUS_EVAL_OUT"); path != "" {
		if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
