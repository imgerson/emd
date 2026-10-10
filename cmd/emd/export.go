package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type manifestEntry struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

// cmdExport writes every document (archived ones too) as DIR/Project/Folder/Title.md, plus
// DIR/manifest.json. Client-side: one list call plus one read per document, no server export.
func cmdExport(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("export", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which directory?")
	}
	dir := pos[0]
	t, err := a.folders()
	if err != nil {
		return err
	}
	docs, err := a.client.Documents(nil)
	if err != nil {
		return err
	}

	used := map[string]bool{}
	var manifest []manifestEntry
	for _, d := range docs {
		var parts []string
		for _, seg := range strings.Split(t.path(parentKey(d.FolderID)), " / ") {
			if seg != "" {
				parts = append(parts, safeName(seg))
			}
		}
		rel := uniquePath(filepath.Join(append(parts, safeName(d.Title))...), used)

		content, _, err := a.client.Content(d.ID)
		if err != nil {
			return err
		}
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
		manifest = append(manifest, manifestEntry{d.ID, filepath.ToSlash(rel), d.Title, d.Status, d.UpdatedAt})
		if !a.json {
			fmt.Fprintln(a.out, filepath.ToSlash(rel))
		}
	}

	b, _ := json.MarshalIndent(map[string]any{"documents": manifest}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	if a.json {
		a.printJSON(b)
	} else {
		fmt.Fprintf(a.errOut, "%d documents written to %s\n", len(manifest), dir)
	}
	return nil
}

// safeName makes a title usable as a file or folder name on every OS.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case strings.ContainsRune(`/\:*?"<>|`, r), unicode.IsControl(r):
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "Untitled"
	}
	if len(s) > 120 {
		s = strings.TrimSpace(string([]rune(s)[:100]))
	}
	return s
}

// uniquePath appends .md, and " (2)", " (3)"... when two documents would share a file.
// Compared case-insensitively, as on macOS and Windows.
func uniquePath(base string, used map[string]bool) string {
	p := base + ".md"
	for n := 2; used[strings.ToLower(p)]; n++ {
		p = fmt.Sprintf("%s (%d).md", base, n)
	}
	used[strings.ToLower(p)] = true
	return p
}
