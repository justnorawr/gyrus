package wiki_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/armckinney/gyrus/internal/domain/lifecycle"
	searchokf "github.com/armckinney/gyrus/internal/provider/search/okf"
	"github.com/armckinney/gyrus/internal/provider/storage/wiki"
	"github.com/armckinney/gyrus/pkg/gyrus"
)

func writeDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func c3(id, title, typ, repo string) string {
	return "---\nid: " + id + "\ntitle: \"" + title + "\"\ntype: " + typ + "\nrepo: " + repo +
		"\ntags: [bus, wiki]\nupdated: 2026-10-20\n---\n\n# " + title + "\n\nBody of " + id + ".\n"
}

func newWiki(t *testing.T) (string, *wiki.Store, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	writeDoc(t, root, "README.md", "# noratics-wiki\n\nNo frontmatter: not a document.\n")
	writeDoc(t, root, "shared/guide-agent-ops-bus.md", c3("shared/guide-agent-ops-bus", "The agent-ops bus: tasks, replies", "guide", "shared"))
	writeDoc(t, root, "plans/noratics-ai/plan.md", c3("plans/noratics-ai/plan", "noratics-ai Plan", "plan", "shared"))
	writeDoc(t, root, "repos/noratics-code/build.md", strings.ReplaceAll(c3("repos/noratics-code/build", "Building the bus client", "guide", "noratics-code"), "\n", "\r\n"))
	writeDoc(t, root, "shared/bad-yaml.md", "---\nid: shared/bad-yaml\ntitle: Accounts: one per org\ntype: decision\n---\n\nBody.\n")
	writeDoc(t, root, "shared/wrong-id.md", c3("shared/other", "Wrong id", "decision", "shared"))
	writeDoc(t, root, ".github/notes.md", c3(".github/notes", "Hidden", "guide", "shared"))

	store, err := wiki.NewStore(root, "root")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	var report bytes.Buffer
	store.SetReportWriter(&report)
	return root, store, &report
}

// -----------------------------------------------------------------------------
// [Test Level]: Unit Test
// [Purpose]: Verifies that the wiki store maps C3 documents onto OKF concepts and skips, rather than fails on, invalid ones.
// [Execution Surface]: Real Local Filesystem
// [Assertions]: Valid documents are visited with OKF names, repos/<repo>/ documents are project-scoped, CRLF files parse,
// documents with broken YAML or a mismatched id are reported once and skipped, files without frontmatter are ignored.
// -----------------------------------------------------------------------------
func TestWikiStoreScanMapsC3AndSkipsInvalid(t *testing.T) {
	_, store, report := newWiki(t)

	got := map[string]gyrus.Concept{}
	scanReport, err := store.Scan(context.Background(), func(doc gyrus.Concept) error {
		got[doc.Name] = doc
		return nil
	})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(scanReport.Errors) != 0 {
		t.Fatalf("Scan must not fail the collection, got errors: %v", scanReport.Errors)
	}
	if len(got) != 3 || scanReport.IndexedFiles != 3 {
		t.Fatalf("expected 3 documents, got %d: %v", len(got), got)
	}

	guide, ok := got["root/reference/shared--guide-agent-ops-bus"]
	if !ok {
		t.Fatalf("shared guide missing; names: %v", got)
	}
	if guide.Scope != "reference" || guide.Type != "guide" || guide.Category != gyrus.CategoryOperations || guide.Title != "The agent-ops bus: tasks, replies" {
		t.Errorf("unexpected shared guide mapping: %+v", guide)
	}
	if !strings.HasPrefix(guide.Content, "# The agent-ops bus") || guide.LastUpdated.Format("2006-01-02") != "2026-10-20" {
		t.Errorf("unexpected content or date: %q %v", guide.Content, guide.LastUpdated)
	}
	if _, ok := got["root/reference/plans--noratics-ai--plan"]; !ok {
		t.Errorf("plan document missing; names: %v", got)
	}
	build, ok := got["root/projects/noratics-code/build"]
	if !ok || build.Scope != "project" || build.Project != "noratics-code" {
		t.Errorf("repo document not project-scoped: %+v", build)
	}
	if strings.Contains(build.Content, "\r") {
		t.Errorf("CRLF not normalised: %q", build.Content)
	}

	out := report.String()
	for _, want := range []string{"shared/bad-yaml.md: frontmatter", `shared/wrong-id.md: id "shared/other" is not the path`} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "README.md") || strings.Contains(out, ".github") {
		t.Errorf("non-documents must not be reported:\n%s", out)
	}

	if _, err := store.Scan(context.Background(), func(gyrus.Concept) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Count(report.String(), "bad-yaml") != 1 {
		t.Errorf("skipped documents must be reported once per store:\n%s", report.String())
	}
}

// -----------------------------------------------------------------------------
// [Test Level]: Integration Test
// [Purpose]: Verifies that the OKF search and context engine run over the wiki store, with the session's repo as the project.
// [Execution Surface]: Real Local Filesystem + OKF search provider + lifecycle engine
// [Assertions]: Search finds wiki documents, Get resolves an OKF name, suggest-context includes the repo's own document,
// and every mutation is refused as read-only.
// -----------------------------------------------------------------------------
func TestWikiStoreServesSearchAndSuggest(t *testing.T) {
	ctx := context.Background()
	_, store, _ := newWiki(t)
	search, err := searchokf.NewSearchProvider(store)
	if err != nil {
		t.Fatalf("search provider: %v", err)
	}
	engine := lifecycle.NewEngineWithProject(store, search, nil, nil, "", "noratics-code")

	results, err := engine.SearchQuery(ctx, gyrus.SearchQuery{Query: "bus", MaxResults: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected wiki documents to match 'bus'")
	}

	doc, err := engine.Get(ctx, "root/reference/shared--guide-agent-ops-bus")
	if err != nil || doc.Title != "The agent-ops bus: tasks, replies" {
		t.Fatalf("Get failed: %v %+v", err, doc)
	}

	layer, err := engine.SuggestContext(ctx, "build the bus client", "", 5)
	if err != nil {
		t.Fatalf("suggest failed: %v", err)
	}
	if !strings.Contains(layer, "root/projects/noratics-code/build") {
		t.Errorf("expected the repo's own document in the context layer:\n%s", layer)
	}

	if _, err := store.Create(ctx, gyrus.Concept{}); !errors.Is(err, wiki.ErrReadOnly) {
		t.Errorf("Create must be refused, got %v", err)
	}
	if err := store.Delete(ctx, doc.Name); !errors.Is(err, wiki.ErrReadOnly) {
		t.Errorf("Delete must be refused, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// [Test Level]: Unit Test
// [Purpose]: Verifies the wiki id to OKF concept name mapping.
// [Execution Surface]: Pure function
// [Assertions]: repos/<repo>/ paths become project names; other paths become reference names with / as --.
// -----------------------------------------------------------------------------
func TestWikiName(t *testing.T) {
	cases := map[string]string{
		"shared/glossary":          "root/reference/shared--glossary",
		"plans/noratics-ai/plan":   "root/reference/plans--noratics-ai--plan",
		"repos/noratics-hq/a/b":    "root/projects/noratics-hq/a--b",
		"repos/noratics-hq":        "root/reference/repos--noratics-hq",
		"shared/Spec-Plan-Formats": "root/reference/shared--spec-plan-formats",
	}
	for in, want := range cases {
		if got := wiki.Name("root", in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}
