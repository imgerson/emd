package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assets "github.com/imgerson/emd"
	"github.com/imgerson/emd/internal/api"
)

// fake is a tiny stand-in for /api/v1 with just what the tests touch.
type fake struct {
	t        *testing.T
	content  string
	lock     string
	puts     []string // X-Updated-At of each PUT
	listHits int
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer k" {
		w.WriteHeader(401)
		io.WriteString(w, `{"message":"Unauthenticated.","code":"unauthenticated"}`)
		return
	}
	w.Header().Set("X-RateLimit-Remaining", "50")
	switch {
	case r.URL.Path == "/folders":
		io.WriteString(w, `{"data":[
			{"id":1,"parent_id":null,"name":"Site","status":"draft","position":0},
			{"id":2,"parent_id":1,"name":"Plans","status":"draft","position":0},
			{"id":3,"parent_id":null,"name":"Dup","status":"draft","position":1},
			{"id":4,"parent_id":null,"name":"Dup","status":"draft","position":2}]}`)
	case r.URL.Path == "/documents" && r.Method == "GET":
		f.listHits++
		if r.URL.Query().Get("cursor") == "" {
			io.WriteString(w, `{"data":[{"id":10,"folder_id":2,"title":"A","status":"draft","updated_at":"2026-10-09T10:00:00.000000Z"}],"meta":{"next_cursor":"c2"}}`)
		} else {
			io.WriteString(w, `{"data":[{"id":11,"folder_id":null,"title":"B","status":"archived","updated_at":"2026-10-09T09:00:00.000000Z"}],"meta":{"next_cursor":null}}`)
		}
	case r.URL.Path == "/documents/10" && r.Method == "GET":
		io.WriteString(w, `{"id":10,"folder_id":2,"title":"A","status":"draft","word_count":1,"updated_at":"2026-10-09T10:00:00.000000Z"}`)
	case r.URL.Path == "/documents/10/content" && r.Method == "GET":
		w.Header().Set("X-Updated-At", f.lock)
		io.WriteString(w, f.content)
	case r.URL.Path == "/documents/10/content" && r.Method == "PUT":
		got := r.Header.Get("X-Updated-At")
		f.puts = append(f.puts, got)
		if got != f.lock {
			w.WriteHeader(409)
			io.WriteString(w, `{"message":"Document was modified elsewhere.","code":"conflict"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.content, f.lock = string(b), "2026-10-09T12:00:00.000000Z"
		io.WriteString(w, `{"id":10}`)
	case r.URL.Path == "/search":
		// A snippet is cut from content, which may hold anything, terminal escapes included.
		io.WriteString(w, `{"folders":[],"documents":[{"id":10,"status":"draft","path":"Site","title":"A","snippet":"x \u001b]52;c;ZXZpbA==\u0007 \u009b2J y"}]}`)
	default:
		w.WriteHeader(404)
		io.WriteString(w, `{"message":"Not found.","code":"not_found"}`)
	}
}

func setup(t *testing.T) (*fake, func(args ...string) (int, string, string)) {
	f := &fake{t: t, content: "old", lock: "2026-10-09T11:00:00.000000Z"}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	cfgDir := t.TempDir()
	env := map[string]string{"EMD_TOKEN": "k", "EMD_URL": srv.URL, "XDG_CONFIG_HOME": cfgDir}
	return f, func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		in := strings.NewReader("")
		if len(args) > 0 && strings.HasPrefix(args[0], "stdin=") {
			in = strings.NewReader(strings.TrimPrefix(args[0], "stdin="))
			args = args[1:]
		}
		code := run(args, in, &out, &errOut, func(k string) string { return env[k] })
		return code, out.String(), errOut.String()
	}
}

func TestPutSendsTheLockItReadAndNeverOverwritesNewerEdits(t *testing.T) {
	f, emd := setup(t)

	if code, _, errOut := emd("stdin=# new\n", "put", "10", "-"); code != 0 {
		t.Fatalf("put failed: %d %s", code, errOut)
	}
	if f.content != "# new\n" || f.puts[0] != "2026-10-09T11:00:00.000000Z" {
		t.Fatalf("content %q, lock sent %q", f.content, f.puts[0])
	}

	// A stale --if-unchanged-since gets the server's 409 and exit 3, with a hint.
	code, _, errOut := emd("stdin=clobber", "put", "10", "--if-unchanged-since", "2026-01-01T00:00:00.000000Z")
	if code != api.ExitConflict || !strings.Contains(errOut, "conflict") || f.content != "# new\n" {
		t.Fatalf("want exit 3 and unchanged content, got %d %q content %q", code, errOut, f.content)
	}
}

func TestLsFollowsCursorsAndHidesArchived(t *testing.T) {
	f, emd := setup(t)
	code, out, _ := emd("ls")
	if code != 0 || f.listHits != 2 {
		t.Fatalf("code %d, list calls %d", code, f.listHits)
	}
	if !strings.Contains(out, "Site / Plans") || strings.Contains(out, "\n11 ") {
		t.Fatalf("unexpected ls output:\n%s", out)
	}
	_, out, _ = emd("ls", "--archived")
	if !strings.Contains(out, "\n11 ") {
		t.Fatalf("--archived should list B:\n%s", out)
	}
}

func TestFolderPaths(t *testing.T) {
	tr := newTree([]api.Folder{
		{ID: 1, Name: "Site"}, {ID: 2, Name: "Plans", ParentID: ptr(1)},
		{ID: 3, Name: "Dup"}, {ID: 4, Name: "Dup"},
	})
	if id, err := tr.resolve("Site / Plans"); err != nil || *id != 2 {
		t.Fatalf("Site / Plans: %v %v", id, err)
	}
	if id, err := tr.resolve("none"); err != nil || id != nil {
		t.Fatalf("none should be unfiled: %v %v", id, err)
	}
	if _, err := tr.resolve("Dup"); err == nil || !strings.Contains(err.Error(), "[3 4]") {
		t.Fatalf("ambiguous path should list the ids, got %v", err)
	}
	if _, err := tr.resolve("Nope"); api.ExitCode(err) != api.ExitNotFound {
		t.Fatalf("missing path should be not found, got %v", err)
	}
	if got := tr.path(2); got != "Site / Plans" {
		t.Fatalf("path(2) = %q", got)
	}
}

func TestAmbiguousFolderIsAUsageError(t *testing.T) {
	_, emd := setup(t)
	if code, _, errOut := emd("ls", "--folder", "Dup"); code != api.ExitUsage || !strings.Contains(errOut, "use an ID") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestExitCodes(t *testing.T) {
	cases := map[string]int{
		"conflict": 3, "folder_not_empty": 3, "unauthenticated": 4, "missing_ability": 4,
		"subscription_required": 5, "subscription_ended": 5, "not_found": 6, "rate_limited": 7,
		"validation_failed": 8, "storage_limit": 8, "row_limit": 8, "content_too_large": 8,
	}
	for code, want := range cases {
		if got := api.ExitCode(&api.Error{Code: code}); got != want {
			t.Errorf("%s: exit %d, want %d", code, got, want)
		}
	}
	if got := api.ExitCode(&api.Error{Status: 502}); got != api.ExitUnexpected {
		t.Errorf("502 without a code: exit %d", got)
	}
}

func TestBadKeyExits4AndJSONErrorsGoToStderr(t *testing.T) {
	f := &fake{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	env := map[string]string{"EMD_TOKEN": "wrong", "EMD_URL": srv.URL, "XDG_CONFIG_HOME": t.TempDir()}
	var out, errOut bytes.Buffer
	code := run([]string{"ls", "--json"}, strings.NewReader(""), &out, &errOut, func(k string) string { return env[k] })
	var body map[string]string
	if code != api.ExitAuth || out.Len() != 0 || json.Unmarshal(errOut.Bytes(), &body) != nil || body["code"] != "unauthenticated" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"XDG_CONFIG_HOME": dir}
	get := func(k string) string { return env[k] }

	if _, err := loadConfig(get); err == nil {
		t.Fatal("no key anywhere should be an error")
	}
	if err := saveConfig(get, config{Token: "file", URL: "https://easymarkdown.io/api/v1"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(dir, "emd", "config.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode %v, want 0600", st.Mode().Perm())
	}
	if cfg, _ := loadConfig(get); cfg.Token != "file" {
		t.Fatalf("file token not used: %+v", cfg)
	}
	env["EMD_TOKEN"] = "env"
	if cfg, _ := loadConfig(get); cfg.Token != "env" {
		t.Fatalf("EMD_TOKEN should win: %+v", cfg)
	}
	env["EMD_URL"] = "http://evil.example/api/v1"
	if _, err := loadConfig(get); err == nil {
		t.Fatal("plain http to a remote host must be refused")
	}
	env["EMD_URL"] = "http://localhost:8000/api/v1"
	if _, err := loadConfig(get); err != nil {
		t.Fatalf("localhost over http is fine: %v", err)
	}
}

func TestDoubleDashEndsFlagsAndJSON(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	title := fs.String("title", "", "")
	pos, err := parse(fs, []string{"--title", "x", "--", "--weird-file", "--json"})
	if err != nil || *title != "x" || strings.Join(pos, ",") != "--weird-file,--json" {
		t.Fatalf("pos %v title %q err %v", pos, *title, err)
	}
}

func TestParseTakesFlagsAnywhere(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	since := fs.String("if-unchanged-since", "", "")
	pos, err := parse(fs, []string{"42", "plan.md", "--if-unchanged-since", "T"})
	if err != nil || strings.Join(pos, ",") != "42,plan.md" || *since != "T" {
		t.Fatalf("pos %v since %q err %v", pos, *since, err)
	}
}

func TestExportNames(t *testing.T) {
	if got := safeName(` a/b:c*? `); got != "a-b-c--" {
		t.Errorf("safeName = %q", got)
	}
	if got := safeName(strings.Repeat("😀", 50)); got != strings.Repeat("😀", 50) {
		t.Errorf("multibyte titles must not panic or be cut under 100 runes: %q", got)
	}
	if got := []rune(safeName(strings.Repeat("漢", 150))); len(got) != 100 {
		t.Errorf("long titles are cut to 100 runes, got %d", len(got))
	}
	if got := safeName(" .. "); got != "Untitled" {
		t.Errorf("empty name = %q", got)
	}
	used := map[string]bool{}
	got := []string{uniquePath("Plans/Notes", used), uniquePath("Plans/notes", used), uniquePath("Plans/Notes", used)}
	want := []string{"Plans/Notes.md", "Plans/notes (2).md", "Plans/Notes (3).md"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniquePath #%d = %q, want %q", i, got[i], want[i])
		}
	}
}

func ptr(n int64) *int64 { return &n }

func TestSearchSnippetCannotDriveTheTerminal(t *testing.T) {
	_, emd := setup(t)
	code, out, _ := emd("search", "x")
	if code != 0 || !strings.Contains(out, "x ?]52;c;ZXZpbA==? ?2J y") {
		t.Fatalf("code %d, want the snippet with its control characters replaced:\n%q", code, out)
	}
	if strings.ContainsAny(out, "\x1b\x07\u009b") {
		t.Fatalf("control characters reached the terminal: %q", out)
	}
}

func TestSkillPrintsAndInstalls(t *testing.T) {
	home := t.TempDir()
	env := func(k string) string { return map[string]string{"HOME": home}[k] }
	var out, errOut bytes.Buffer
	if code := run([]string{"skill"}, strings.NewReader(""), &out, &errOut, env); code != 0 || out.String() != string(assets.Skill) {
		t.Fatalf("emd skill: code %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "---\nname: easymarkdown-plans\n") {
		t.Fatalf("not a skill: %q", out.String()[:40])
	}
	out.Reset()
	if code := run([]string{"skill", "install"}, strings.NewReader(""), &out, &errOut, env); code != 0 {
		t.Fatalf("install: code %d, stderr %q", code, errOut.String())
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "easymarkdown-plans", "SKILL.md"))
	if err != nil || string(got) != string(assets.Skill) {
		t.Fatalf("installed file: %v", err)
	}
	if code := run([]string{"skill", "install"}, strings.NewReader(""), &out, &errOut, func(string) string { return "" }); code == 0 {
		t.Fatal("install without HOME or --dir should fail")
	}
}
