package wiki

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/armckinney/gyrus/internal/domain/okf"
	"github.com/armckinney/gyrus/pkg/gyrus"
	"gopkg.in/yaml.v3"
)

// ErrReadOnly is returned by every mutation: the wiki store is changed by pull request, never by gyrus.
var ErrReadOnly = errors.New("wiki storage is read-only")

// frontmatter is the C3 document form.
type frontmatter struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Type    string   `yaml:"type"`
	Repo    string   `yaml:"repo"`
	Plan    string   `yaml:"plan"`
	Tags    []string `yaml:"tags"`
	Updated string   `yaml:"updated"`
}

// categories gives each C3 type the nearest OKF category; anything unlisted is technical.
var categories = map[string]gyrus.Category{
	"decision": gyrus.CategoryArchitecture,
	"spec":     gyrus.CategoryTechnical,
	"guide":    gyrus.CategoryOperations,
	"runbook":  gyrus.CategoryOperations,
	"plan":     gyrus.CategoryProduct,
	"roadmap":  gyrus.CategoryProduct,
	"progress": gyrus.CategoryProduct,
	"phases":   gyrus.CategoryProduct,
}

// Store reads a wiki store. Documents under repos/<repo>/ become project concepts of <repo>; every
// other document is a reference concept. A document that is not valid C3 is skipped and reported,
// never fatal to the scan.
type Store struct {
	rootDir, tenant string
	report          io.Writer
	reportOnce      sync.Once
}

// NewStore opens the wiki store at root. tenant names the concepts (OKF requires one).
func NewStore(root, tenant string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("wiki storage root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("wiki storage root is not a directory: %s", abs)
	}
	if tenant == "" {
		tenant = "root"
	}
	if err := okf.ValidateSegment("tenant", tenant); err != nil {
		return nil, err
	}
	return &Store{rootDir: abs, tenant: tenant, report: os.Stderr}, nil
}

// SetReportWriter redirects the skipped-document report (stderr by default).
func (s *Store) SetReportWriter(w io.Writer) { s.report = w }

// Name maps a wiki id (the path without .md) onto an OKF concept name: repos/<repo>/a/b becomes
// <tenant>/projects/<repo>/a--b, anything else <tenant>/reference/<path with / as -->.
func Name(tenant, wikiID string) string {
	parts := strings.Split(strings.ToLower(wikiID), "/")
	if len(parts) >= 3 && parts[0] == "repos" {
		return tenant + "/projects/" + parts[1] + "/" + strings.Join(parts[2:], "--")
	}
	return tenant + "/reference/" + strings.Join(parts, "--")
}

// Scan visits every valid document; invalid ones are listed in the report writer once per Store.
func (s *Store) Scan(ctx context.Context, visit func(gyrus.Concept) error) (gyrus.SyncReport, error) {
	docs, skipped, report, err := s.snapshot(ctx)
	if err != nil {
		return report, err
	}
	if len(skipped) > 0 && s.report != nil {
		s.reportOnce.Do(func() {
			for _, line := range skipped {
				fmt.Fprintf(s.report, "gyrus: wiki: skipped %s\n", line)
			}
		})
	}
	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := visit(doc); err != nil {
			return report, err
		}
		report.IndexedFiles++
	}
	return report, nil
}

func (s *Store) snapshot(ctx context.Context) ([]gyrus.Concept, []string, gyrus.SyncReport, error) {
	var report gyrus.SyncReport
	var docs []gyrus.Concept
	var skipped []string
	seen := make(map[string]string)
	err := filepath.WalkDir(s.rootDir, func(name string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			if name != s.rootDir && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(name, ".md") || strings.HasPrefix(e.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(s.rootDir, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		report.ScannedFiles++
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		doc, err := s.parse(rel, data)
		if errors.Is(err, errNotDocument) {
			return nil
		}
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", rel, err))
			return nil
		}
		if previous, exists := seen[doc.Name]; exists {
			skipped = append(skipped, fmt.Sprintf("%s: same concept name %s as %s", rel, doc.Name, previous))
			return nil
		}
		seen[doc.Name] = rel
		doc.FilePath = filepath.ToSlash(name)
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, nil, report, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs, skipped, report, nil
}

// errNotDocument marks a Markdown file with no frontmatter at all (a README, a CLAUDE.md): not a
// wiki document, so it is skipped silently.
var errNotDocument = errors.New("no frontmatter")

func (s *Store) parse(rel string, data []byte) (gyrus.Concept, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return gyrus.Concept{}, errNotDocument
	}
	end := strings.Index(text[4:], "\n---")
	if end == -1 {
		return gyrus.Concept{}, errors.New("frontmatter is not closed")
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &fm); err != nil {
		return gyrus.Concept{}, fmt.Errorf("frontmatter: %w", err)
	}
	body := strings.TrimLeft(text[4+end+4:], "\n")
	wikiID := strings.TrimSuffix(rel, ".md")
	switch {
	case fm.ID == "":
		return gyrus.Concept{}, errors.New("id is missing")
	case fm.ID != wikiID:
		return gyrus.Concept{}, fmt.Errorf("id %q is not the path %q", fm.ID, wikiID)
	case strings.TrimSpace(fm.Title) == "":
		return gyrus.Concept{}, errors.New("title is missing")
	case fm.Type == "":
		return gyrus.Concept{}, errors.New("type is missing")
	}
	name := Name(s.tenant, wikiID)
	tenant, project, id, err := okf.ParseName(name)
	if err != nil {
		return gyrus.Concept{}, fmt.Errorf("concept name %s: %w", name, err)
	}
	category, ok := categories[fm.Type]
	if !ok {
		category = gyrus.CategoryTechnical
	}
	doc := gyrus.Concept{
		Name:     name,
		ID:       id,
		Title:    fm.Title,
		Category: category,
		Type:     gyrus.ConceptType(fm.Type),
		Format:   "c3",
		Tenant:   tenant,
		Version:  1,
		Status:   "published",
		Tags:     fm.Tags,
		Scope:    "reference",
		Project:  project,
		Content:  body,
	}
	if project != "" {
		doc.Scope = "project"
	}
	if updated, err := time.Parse("2006-01-02", fm.Updated); err == nil {
		doc.LastUpdated = updated
	}
	return doc, nil
}

// Get returns the concept with the given qualified name.
func (s *Store) Get(ctx context.Context, name string) (gyrus.Concept, error) {
	if err := okf.ValidateName(name); err != nil {
		return gyrus.Concept{}, err
	}
	docs, _, _, err := s.snapshot(ctx)
	if err != nil {
		return gyrus.Concept{}, err
	}
	for _, doc := range docs {
		if doc.Name == name {
			return doc, nil
		}
	}
	return gyrus.Concept{}, fmt.Errorf("concept not found: %s", name)
}

func (s *Store) Create(context.Context, gyrus.Concept) (gyrus.ConceptRef, error) {
	return gyrus.ConceptRef{}, ErrReadOnly
}
func (s *Store) Update(context.Context, string, gyrus.ConceptPatch, int) (gyrus.ConceptRef, error) {
	return gyrus.ConceptRef{}, ErrReadOnly
}
func (s *Store) Delete(context.Context, string) error  { return ErrReadOnly }
func (s *Store) Archive(context.Context, string) error { return ErrReadOnly }

var (
	_ gyrus.ConceptStore   = (*Store)(nil)
	_ gyrus.ConceptScanner = (*Store)(nil)
)
