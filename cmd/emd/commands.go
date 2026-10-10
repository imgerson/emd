package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/imgerson/emd/internal/api"
	"github.com/imgerson/emd/internal/diff"
)

func id(arg string) (int64, error) {
	n, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || n < 1 {
		return 0, usagef("%q is not an ID (IDs are numbers; find them with emd ls or emd tree)", arg)
	}
	return n, nil
}

func (a *app) folders() (*tree, error) {
	f, err := a.client.Folders()
	if err != nil {
		return nil, err
	}
	return newTree(f), nil
}

func (a *app) table() *tabwriter.Writer { return tabwriter.NewWriter(a.out, 0, 2, 2, ' ', 0) }

// ---- login ----

func cmdLogin(a *app, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	base := fs.String("url", "", "API base URL (default "+defaultURL+")")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	u := *base
	if u == "" {
		u = os.Getenv("EMD_URL")
	}
	if u == "" {
		u = defaultURL
	}
	if err := checkURL(u); err != nil {
		return usageError{err.Error()}
	}
	// From stdin, never argv: keeps the key out of shell history and `ps`.
	if f, ok := a.in.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			fmt.Fprint(a.errOut, "Paste your API key (Settings -> API keys) and press Enter: ")
		}
	}
	line, _ := bufio.NewReader(a.in).ReadString('\n')
	token := strings.TrimSpace(line)
	if token == "" {
		return usagef("no key on stdin")
	}

	c := api.New(u, token, version)
	if _, err := c.JSON("GET", "/documents", url.Values{"limit": {"1"}}, nil, nil); err != nil {
		return err
	}
	if err := saveConfig(os.Getenv, config{Token: token, URL: u}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Logged in. Saved to %s\n", configPath(os.Getenv))
	return nil
}

// ---- ls / tree / search ----

func cmdLs(a *app, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	folder := fs.String("folder", "", "folder ID or path, or none for unfiled")
	status := fs.String("status", "", "only this status")
	archived := fs.Bool("archived", false, "include archived documents")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	q := url.Values{}
	if *folder != "" {
		fid, err := t.resolve(*folder)
		if err != nil {
			return err
		}
		if fid == nil {
			q.Set("folder_id", "")
		} else {
			q.Set("folder_id", strconv.FormatInt(*fid, 10))
		}
	}
	if *status != "" {
		if err := checkStatus(*status); err != nil {
			return err
		}
		q.Set("status", *status)
	}
	docs, err := a.client.Documents(q)
	if err != nil {
		return err
	}
	if !*archived && *status != "archived" {
		kept := docs[:0]
		for _, d := range docs {
			if d.Status != "archived" {
				kept = append(kept, d)
			}
		}
		docs = kept
	}
	if a.json {
		return a.emitJSON(map[string]any{"data": docs})
	}
	w := a.table()
	fmt.Fprintln(w, "ID\tSTATUS\tUPDATED\tTITLE\tFOLDER")
	for _, d := range docs {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", d.ID, d.Status, when(d.UpdatedAt), d.Title, t.path(parentKey(d.FolderID)))
	}
	return w.Flush()
}

func cmdTree(a *app, args []string) error {
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	archived := fs.Bool("archived", false, "include archived folders and documents")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	docs, err := a.client.Documents(nil)
	if err != nil {
		return err
	}
	inFolder := map[int64][]api.Document{}
	for _, d := range docs {
		if *archived || d.Status != "archived" {
			inFolder[parentKey(d.FolderID)] = append(inFolder[parentKey(d.FolderID)], d)
		}
	}
	for k := range inFolder {
		sort.SliceStable(inFolder[k], func(i, j int) bool { return inFolder[k][i].Position < inFolder[k][j].Position })
	}
	if a.json {
		return a.emitJSON(map[string]any{"folders": t.folders, "documents": docs})
	}

	var walk func(parent int64, depth int)
	walk = func(parent int64, depth int) {
		pad := strings.Repeat("  ", depth)
		for _, f := range t.children[parent] {
			if !*archived && f.Status == "archived" {
				continue
			}
			fmt.Fprintf(a.out, "%s%s/  [%d] %s\n", pad, f.Name, f.ID, f.Status)
			walk(f.ID, depth+1)
		}
		for _, d := range inFolder[parent] {
			fmt.Fprintf(a.out, "%s%s  [%d] %s\n", pad, d.Title, d.ID, d.Status)
		}
	}
	walk(0, 0)
	return nil
}

func cmdSearch(a *app, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("what should I search for?")
	}
	var res api.SearchResults
	raw, err := a.client.JSON("GET", "/search", url.Values{"q": {strings.Join(pos, " ")}}, nil, &res)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	w := a.table()
	for _, f := range res.Folders {
		fmt.Fprintf(w, "folder %d\t%s\t%s\n", f.ID, f.Status, strings.TrimPrefix(f.Path+" / "+f.Name, " / "))
	}
	for _, d := range res.Documents {
		snippet := ""
		if d.Snippet != nil {
			snippet = "  " + *d.Snippet
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s%s\n", d.ID, d.Status, d.Path, d.Title, snippet)
	}
	if len(res.Folders)+len(res.Documents) == 0 {
		fmt.Fprintln(a.errOut, "no matches")
	}
	return w.Flush()
}

// ---- get / show ----

func cmdGet(a *app, args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	outFile := fs.String("o", "", "write to FILE instead of stdout")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which document?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	if a.json {
		raw, err := a.client.JSON("GET", fmt.Sprintf("/documents/%d", n), nil, nil, nil)
		if err == nil {
			a.printJSON(raw)
		}
		return err
	}
	content, _, err := a.client.Content(n)
	if err != nil {
		return err
	}
	if *outFile != "" {
		return os.WriteFile(*outFile, []byte(content), 0o644)
	}
	_, err = fmt.Fprint(a.out, content)
	return err
}

func cmdShow(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("show", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which document?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	var d api.Document
	raw, err := a.client.JSON("GET", fmt.Sprintf("/documents/%d", n), nil, nil, &d)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	w := a.table()
	fmt.Fprintf(w, "id\t%d\ntitle\t%s\nfolder\t%s\nstatus\t%s\nwords\t%d\nupdated_at\t%s\n",
		d.ID, d.Title, t.path(parentKey(d.FolderID)), d.Status, d.WordCount, d.UpdatedAt)
	return w.Flush()
}

// ---- new / put / status / rename / mv / rm ----

func cmdNew(a *app, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	title := fs.String("title", "", "title (required)")
	folder := fs.String("folder", "", "folder ID or path")
	status := fs.String("status", "", "status (default draft)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *title == "" {
		return usagef("--title is required")
	}
	body := map[string]any{"title": *title}
	if *status != "" {
		if err := checkStatus(*status); err != nil {
			return err
		}
		body["status"] = *status
	}
	if *folder != "" {
		t, err := a.folders()
		if err != nil {
			return err
		}
		fid, err := t.resolve(*folder)
		if err != nil {
			return err
		}
		body["folder_id"] = fid
	}
	if len(pos) > 0 {
		content, err := a.readInput(pos[0])
		if err != nil {
			return err
		}
		body["content"] = content
	}
	var d api.Document
	raw, err := a.client.JSON("POST", "/documents", nil, body, &d)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	fmt.Fprintln(a.out, d.ID)
	return nil
}

func cmdPut(a *app, args []string) error {
	fs := flag.NewFlagSet("put", flag.ContinueOnError)
	since := fs.String("if-unchanged-since", "", "updated_at you read earlier (default: read it now)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return usagef("which document, and which file?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	src := ""
	if len(pos) == 2 {
		src = pos[1]
	}
	content, err := a.readInput(src)
	if err != nil {
		return err
	}
	lock := *since
	if lock == "" { // read the current lock so a browser edit made meanwhile is never overwritten
		if _, lock, err = a.client.Content(n); err != nil {
			return err
		}
	}
	raw, err := a.client.PutContent(n, content, lock)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
	}
	return nil
}

func patchDoc(a *app, n int64, body map[string]any) error {
	raw, err := a.client.JSON("PATCH", fmt.Sprintf("/documents/%d", n), nil, body, nil)
	if err == nil && a.json {
		a.printJSON(raw)
	}
	return err
}

func cmdStatus(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("status", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("which document, and which status?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	if err := checkStatus(pos[1]); err != nil {
		return err
	}
	return patchDoc(a, n, map[string]any{"status": pos[1]})
}

func cmdRename(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("rename", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return usagef("which document, and what title?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	return patchDoc(a, n, map[string]any{"title": strings.Join(pos[1:], " ")})
}

func cmdMv(a *app, args []string) error {
	fs := flag.NewFlagSet("mv", flag.ContinueOnError)
	folder := fs.String("folder", "", "destination folder ID or path, or none (required)")
	top := fs.Bool("top", false, "first in the folder (default)")
	bottom := fs.Bool("bottom", false, "last in the folder")
	before := fs.String("before", "", "just before this document ID")
	after := fs.String("after", "", "just after this document ID")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *folder == "" {
		return usagef("which document, and --folder where?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	fid, err := t.resolve(*folder)
	if err != nil {
		return err
	}
	body := map[string]any{"folder_id": fid, "place": "top"}
	switch {
	case *before != "" || *after != "":
		anchor, place := *before, "before"
		if *after != "" {
			anchor, place = *after, "after"
		}
		aid, err := id(anchor)
		if err != nil {
			return err
		}
		body["place"], body["anchor_id"] = place, aid
	case *bottom:
		body["place"] = "bottom"
	case *top:
	}
	raw, err := a.client.JSON("POST", fmt.Sprintf("/documents/%d/move", n), nil, body, nil)
	if err == nil && a.json {
		a.printJSON(raw)
	}
	return err
}

func cmdRm(a *app, args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm: the document and its history are deleted")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which document?")
	}
	if !*yes {
		return usagef("deleting can't be undone: add --yes")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	_, err = a.client.JSON("DELETE", fmt.Sprintf("/documents/%d", n), nil, nil, nil)
	return err
}

// ---- folders ----

func cmdMkdir(a *app, args []string) error {
	fs := flag.NewFlagSet("mkdir", flag.ContinueOnError)
	status := fs.String("status", "", "status for folders this creates")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || len(splitPath(pos[0])) == 0 {
		return usagef("which path?")
	}
	if *status != "" {
		if err := checkStatus(*status); err != nil {
			return err
		}
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	var parent *int64
	var last api.Folder
	for _, seg := range splitPath(pos[0]) {
		var match []api.Folder
		for _, f := range t.children[parentKey(parent)] {
			if f.Name == seg {
				match = append(match, f)
			}
		}
		switch len(match) {
		case 1:
			last = match[0]
		case 0:
			body := map[string]any{"name": seg, "parent_id": parent}
			if *status != "" {
				body["status"] = *status
			}
			if _, err := a.client.JSON("POST", "/folders", nil, body, &last); err != nil {
				return err
			}
		default:
			ids := make([]int64, len(match))
			for i, f := range match {
				ids[i] = f.ID
			}
			return usagef("%q matches folders %v at that level: create it by ID instead", seg, ids)
		}
		fid := last.ID
		parent = &fid
	}
	if a.json {
		return a.emitJSON(last)
	}
	fmt.Fprintln(a.out, last.ID)
	return nil
}

func cmdRmdir(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("rmdir", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which folder?")
	}
	t, err := a.folders()
	if err != nil {
		return err
	}
	fid, err := t.resolve(pos[0])
	if err != nil {
		return err
	}
	if fid == nil {
		return usagef("which folder?")
	}
	_, err = a.client.JSON("DELETE", fmt.Sprintf("/folders/%d", *fid), nil, nil, nil)
	return err
}

// ---- history ----

func (a *app) snapshots(n int64, kind string) ([]api.Snapshot, []byte, error) {
	var res struct {
		Data []api.Snapshot `json:"data"`
	}
	raw, err := a.client.JSON("GET", fmt.Sprintf("/documents/%d/snapshots", n), url.Values{"kind": {kind}}, nil, &res)
	return res.Data, raw, err
}

func cmdLog(a *app, args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	named := fs.Bool("snapshots", false, "named snapshots instead of the automatic history")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which document?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	kind := "auto"
	if *named {
		kind = "manual"
	}
	snaps, raw, err := a.snapshots(n, kind)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	w := a.table()
	fmt.Fprintln(w, "REV\tWHEN\tBY\tWORDS\tNAME")
	for _, s := range snaps {
		by := "web"
		if s.Source != nil && *s.Source == "api" {
			by = "api"
			if s.TokenName != nil {
				by = "api:" + *s.TokenName
			}
		}
		name := s.Name
		if kind == "auto" {
			name = s.Title
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", s.ID, when(s.CreatedAt), by, s.WordCount, name)
	}
	return w.Flush()
}

func (a *app) revision(n int64, rev string) (api.Snapshot, []byte, error) {
	r, err := id(rev)
	if err != nil {
		return api.Snapshot{}, nil, err
	}
	var s api.Snapshot
	raw, err := a.client.JSON("GET", fmt.Sprintf("/documents/%d/snapshots/%d", n, r), nil, nil, &s)
	return s, raw, err
}

func cmdCat(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("cat", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("which document, and which version (REV from emd log)?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	s, raw, err := a.revision(n, pos[1])
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	if s.Content != nil {
		fmt.Fprint(a.out, *s.Content)
	}
	return nil
}

func cmdDiff(a *app, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	ctx := fs.Int("U", 3, "lines of context")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 3 {
		return usagef("which document?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	var from, to, fromName, toName string
	switch len(pos) {
	case 1: // the latest change: the newest history entry vs the one before it
		snaps, _, err := a.snapshots(n, "auto")
		if err != nil {
			return err
		}
		if len(snaps) == 0 {
			return nil
		}
		latest, _, err := a.revision(n, strconv.FormatInt(snaps[0].ID, 10))
		if err != nil {
			return err
		}
		to, toName = deref(latest.Content), fmt.Sprintf("rev %d", latest.ID)
		fromName = "(empty)"
		if len(snaps) > 1 {
			prev, _, err := a.revision(n, strconv.FormatInt(snaps[1].ID, 10))
			if err != nil {
				return err
			}
			from, fromName = deref(prev.Content), fmt.Sprintf("rev %d", prev.ID)
		}
	case 2: // REV vs now
		s, _, err := a.revision(n, pos[1])
		if err != nil {
			return err
		}
		if to, _, err = a.client.Content(n); err != nil {
			return err
		}
		from, fromName, toName = deref(s.Content), "rev "+pos[1], "current"
	case 3:
		s1, _, err := a.revision(n, pos[1])
		if err != nil {
			return err
		}
		s2, _, err := a.revision(n, pos[2])
		if err != nil {
			return err
		}
		from, to, fromName, toName = deref(s1.Content), deref(s2.Content), "rev "+pos[1], "rev "+pos[2]
	}
	fmt.Fprint(a.out, diff.Unified(diff.Lines(from, to), fromName, toName, *ctx))
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func cmdSnapshot(a *app, args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	name := fs.String("name", "", "snapshot name (default: the time)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("which document?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	body := map[string]any{}
	if *name != "" {
		body["name"] = *name
	}
	var s api.Snapshot
	raw, err := a.client.JSON("POST", fmt.Sprintf("/documents/%d/snapshots", n), nil, body, &s)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
		return nil
	}
	fmt.Fprintln(a.out, s.ID)
	return nil
}

func cmdRestore(a *app, args []string) error {
	pos, err := parse(flag.NewFlagSet("restore", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usagef("which document, and which version (REV from emd log)?")
	}
	n, err := id(pos[0])
	if err != nil {
		return err
	}
	r, err := id(pos[1])
	if err != nil {
		return err
	}
	raw, err := a.client.JSON("POST", fmt.Sprintf("/documents/%d/snapshots/%d/restore", n, r), nil, nil, nil)
	if err == nil && a.json {
		a.printJSON(raw)
	}
	return err
}
