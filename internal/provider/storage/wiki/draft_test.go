package wiki_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/armckinney/gyrus/internal/provider/storage/wiki"
	"github.com/armckinney/gyrus/pkg/gyrus"
)

// -----------------------------------------------------------------------------
// [Test Level]: Unit Test
// [Purpose]: Verifies that a drafted wiki document is valid C3 with its plans, and that the reader indexes plans.
// [Execution Surface]: Real Local Filesystem
// [Assertions]: Draft renders frontmatter (plans from NORATICS_PLAN when none are given, plan from the folder under
// plans/), a drafted document filed into the store is read back with its plans, and invalid drafts are refused.
// -----------------------------------------------------------------------------
func TestDraftRoundTripsPlans(t *testing.T) {
	root, store, _ := newWiki(t)
	if err := os.MkdirAll(filepath.Join(root, "plans", "buildings"), 0o755); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	t.Setenv("NORATICS_PLAN", "noratics-ai")
	doc, err := store.Draft(wiki.Draft{ID: "repos/noratics-hq/guide-kits", Title: "Kits: how they load", Type: "guide", Tags: []string{"hq"}, Plans: wiki.ParsePlans(""), Body: "# Kits\n\nThey load from the CDN."}, today)
	if err != nil {
		t.Fatalf("Draft failed: %v", err)
	}
	want := "---\nid: repos/noratics-hq/guide-kits\ntitle: \"Kits: how they load\"\ntype: guide\nrepo: noratics-hq\nplans: [noratics-ai]\ntags: [hq]\nupdated: 2026-10-20\n---\n\n# Kits\n\nThey load from the CDN.\n"
	if doc != want {
		t.Fatalf("unexpected document:\n%s\nwant:\n%s", doc, want)
	}
	writeDoc(t, root, "repos/noratics-hq/guide-kits.md", doc)

	inPlan, err := store.Draft(wiki.Draft{ID: "plans/buildings/research", Title: "Buildings Research", Type: "research", Plans: []string{"noratics-ai"}}, today)
	if err != nil {
		t.Fatalf("Draft in a plan folder failed: %v", err)
	}
	if !strings.Contains(inPlan, "\nplan: buildings\n") || strings.Contains(inPlan, "plans:") || !strings.HasSuffix(inPlan, "# Buildings Research\n") {
		t.Errorf("a document under plans/<name>/ carries plan, not plans:\n%s", inPlan)
	}
	writeDoc(t, root, "plans/buildings/research.md", inPlan)

	plans := map[string][]string{}
	if _, err := store.Scan(context.Background(), func(c gyrus.Concept) error { plans[c.Name] = c.Plans; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := plans["root/projects/noratics-hq/guide-kits"]; !slices.Equal(got, []string{"noratics-ai"}) {
		t.Errorf("drafted document read back with plans %v", got)
	}
	if got := plans["root/reference/plans--buildings--research"]; !slices.Equal(got, []string{"buildings"}) {
		t.Errorf("plan-folder document read back with plans %v", got)
	}

	for name, bad := range map[string]wiki.Draft{
		"unknown plan":     {ID: "shared/x", Title: "X", Type: "guide", Plans: []string{"nope"}},
		"bad id":           {ID: "Shared/X", Title: "X", Type: "guide"},
		"plan-only type":   {ID: "shared/x", Title: "X", Type: "roadmap"},
		"shared-only type": {ID: "plans/buildings/x", Title: "X", Type: "guide"},
		"exists":           {ID: "shared/guide-agent-ops-bus", Title: "X", Type: "guide"},
		"no title":         {ID: "shared/x", Type: "guide"},
	} {
		if _, err := store.Draft(bad, today); err == nil {
			t.Errorf("%s: expected Draft to refuse %+v", name, bad)
		}
	}
	if got := wiki.ParsePlans(" a, b ,a"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("ParsePlans = %v", got)
	}
}
