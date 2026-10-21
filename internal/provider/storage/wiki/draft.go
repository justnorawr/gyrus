package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	draftID   = regexp.MustCompile(`^(shared|repos/[a-z0-9-_]+|plans/[a-z0-9-_]+)/[a-z0-9-_]+(/[a-z0-9-_]+)*$`)
	draftName = regexp.MustCompile(`^[a-z0-9-_]+$`)
	// C3 types: a plan's folder holds its own documents; everything else holds the shared kinds.
	planTypes  = []string{"plan", "progress", "mockups", "research", "roadmap", "diagrams", "phases"}
	otherTypes = []string{"decision", "guide", "spec", "runbook", "glossary", "record"}
)

// Draft is a new wiki document. The store never writes it: Draft renders it for the caller to file by pull request.
type Draft struct {
	ID    string   // the wiki id: the path without .md, e.g. shared/guide-x or repos/<repo>/x
	Title string   // required
	Type  string   // a C3 type
	Tags  []string // optional
	Plans []string // plan names; ignored under plans/<name>/, whose plan is the folder
	Body  string   // Markdown; a heading from the title when empty
}

// ParsePlans splits a comma-separated plan list; an empty list falls back to NORATICS_PLAN, the creating agent's plan.
func ParsePlans(list string) []string {
	if strings.TrimSpace(list) == "" {
		list = os.Getenv("NORATICS_PLAN")
	}
	var plans []string
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); p != "" && !slices.Contains(plans, p) {
			plans = append(plans, p)
		}
	}
	return plans
}

// Draft validates d against C3 and this store (every plan must be a folder under plans/) and returns the document.
func (s *Store) Draft(d Draft, today time.Time) (string, error) {
	if !draftID.MatchString(d.ID) {
		return "", fmt.Errorf("id %q must be shared/<name>, repos/<repo>/<name> or plans/<plan>/<name>, lower case", d.ID)
	}
	if strings.TrimSpace(d.Title) == "" {
		return "", fmt.Errorf("title is required")
	}
	parts := strings.Split(d.ID, "/")
	repo, plan, plans := "shared", "", d.Plans
	types := otherTypes
	switch parts[0] {
	case "repos":
		repo = parts[1]
	case "plans":
		plan, plans, types = parts[1], nil, planTypes
	}
	if !slices.Contains(types, d.Type) {
		return "", fmt.Errorf("type %q is not one of %s for %s/", d.Type, strings.Join(types, ", "), parts[0])
	}
	for _, p := range append(slices.Clone(plans), plan) {
		if p == "" {
			continue
		}
		if !draftName.MatchString(p) {
			return "", fmt.Errorf("plan %q is not a plan name", p)
		}
		if info, err := os.Stat(filepath.Join(s.rootDir, "plans", p)); err != nil || !info.IsDir() {
			return "", fmt.Errorf("plan %q has no folder plans/%s/ in the wiki store", p, p)
		}
	}
	for _, t := range d.Tags {
		if !draftName.MatchString(t) {
			return "", fmt.Errorf("tag %q must be lower case letters, digits, - or _", t)
		}
	}
	if _, err := os.Stat(filepath.Join(s.rootDir, filepath.FromSlash(d.ID)+".md")); err == nil {
		return "", fmt.Errorf("%s.md already exists in the wiki store", d.ID)
	}

	title, _ := json.Marshal(d.Title)
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\ntitle: %s\ntype: %s\nrepo: %s\n", d.ID, title, d.Type, repo)
	if plan != "" {
		fmt.Fprintf(&b, "plan: %s\n", plan)
	} else {
		fmt.Fprintf(&b, "plans: [%s]\n", strings.Join(plans, ", "))
	}
	fmt.Fprintf(&b, "tags: [%s]\nupdated: %s\n---\n\n", strings.Join(d.Tags, ", "), today.Format("2006-01-02"))
	body := strings.TrimSpace(strings.ReplaceAll(d.Body, "\r\n", "\n"))
	if body == "" {
		body = "# " + d.Title
	}
	b.WriteString(body + "\n")
	return b.String(), nil
}
