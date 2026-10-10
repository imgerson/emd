package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imgerson/emd/internal/api"
)

// tree is the folder list indexed for path lookups.
type tree struct {
	folders  []api.Folder
	byID     map[int64]api.Folder
	children map[int64][]api.Folder // key 0 = top level (projects)
}

func newTree(folders []api.Folder) *tree {
	t := &tree{folders: folders, byID: map[int64]api.Folder{}, children: map[int64][]api.Folder{}}
	for _, f := range folders {
		t.byID[f.ID] = f
		t.children[parentKey(f.ParentID)] = append(t.children[parentKey(f.ParentID)], f)
	}
	for k := range t.children {
		sort.SliceStable(t.children[k], func(i, j int) bool { return t.children[k][i].Position < t.children[k][j].Position })
	}
	return t
}

func parentKey(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// path is "Project / Sub" for a folder id (0 = unfiled → "").
func (t *tree) path(id int64) string {
	var parts []string
	seen := map[int64]bool{}
	for id != 0 && !seen[id] {
		seen[id] = true
		f, ok := t.byID[id]
		if !ok {
			break
		}
		parts = append([]string{f.Name}, parts...)
		id = parentKey(f.ParentID)
	}
	return strings.Join(parts, " / ")
}

// resolve turns "12", "none" or "Project/Sub" into a folder id (nil = unfiled).
// A path that matches more than one folder is a usage error listing the ids.
func (t *tree) resolve(arg string) (*int64, error) {
	if arg == "none" || arg == "" {
		return nil, nil
	}
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		if _, ok := t.byID[id]; !ok {
			return nil, &api.Error{Status: 404, Code: "not_found", Message: fmt.Sprintf("no folder %d", id)}
		}
		return &id, nil
	}
	var parent int64
	var found int64
	for _, seg := range splitPath(arg) {
		var matches []int64
		for _, f := range t.children[parent] {
			if f.Name == seg {
				matches = append(matches, f.ID)
			}
		}
		switch len(matches) {
		case 0:
			return nil, &api.Error{Status: 404, Code: "not_found", Message: fmt.Sprintf("no folder %q", arg)}
		case 1:
			parent, found = matches[0], matches[0]
		default:
			return nil, usagef("%q matches folders %v: use an ID", arg, matches)
		}
	}
	return &found, nil
}

// splitPath splits "A/B" or "A / B" into trimmed, non-empty segments.
func splitPath(p string) []string {
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

func parseTime(ts string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, ts)
}
