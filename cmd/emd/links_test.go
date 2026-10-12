package main

import (
	"strings"
	"testing"

	"github.com/imgerson/emd/internal/api"
)

func TestDocURL(t *testing.T) {
	cases := []struct {
		name, base string
		doc        api.Document
		want       string
	}{
		{"the server's url wins", "https://easymarkdown.io/api/v1", api.Document{ID: 42, URL: "https://easymarkdown.io/dashboard?doc=42"}, "https://easymarkdown.io/dashboard?doc=42"},
		{"older server: built from the base", "https://easymarkdown.io/api/v1", api.Document{ID: 42}, "https://easymarkdown.io/dashboard?doc=42"},
		{"base with a trailing slash", "https://easymarkdown.io/api/v1/", api.Document{ID: 7}, "https://easymarkdown.io/dashboard?doc=7"},
		{"local server", "http://127.0.0.1:8000/api/v1", api.Document{ID: 3}, "http://127.0.0.1:8000/dashboard?doc=3"},
		{"base without /api/v1", "http://localhost:8000", api.Document{ID: 3}, "http://localhost:8000/dashboard?doc=3"},
		{"a url with escapes is ignored", "https://easymarkdown.io/api/v1", api.Document{ID: 5, URL: "https://x.io/\x1b]8;;evil\x1b\\"}, "https://easymarkdown.io/dashboard?doc=5"},
		{"a non-http url is ignored", "https://easymarkdown.io/api/v1", api.Document{ID: 5, URL: "file:///etc/passwd"}, "https://easymarkdown.io/dashboard?doc=5"},
	}
	for _, c := range cases {
		if got := docURL(c.base, c.doc); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHyperlink(t *testing.T) {
	cases := []struct {
		tty  bool
		want string
	}{
		{false, "https://e.io/dashboard?doc=1"},
		{true, "\x1b]8;;https://e.io/dashboard?doc=1\x1b\\https://e.io/dashboard?doc=1\x1b]8;;\x1b\\"},
	}
	for _, c := range cases {
		if got := hyperlink("https://e.io/dashboard?doc=1", c.tty); got != c.want {
			t.Errorf("tty=%v: got %q, want %q", c.tty, got, c.want)
		}
	}
}

func TestLinksInOutput(t *testing.T) {
	_, emd := setup(t)

	// Not a terminal (a buffer): plain URLs, never escape codes. The fake server has no `url`
	// field, so these are the built fallback.
	code, out, errOut := emd("stdin=x", "put", "10")
	if code != 0 || out != "" || !strings.HasSuffix(errOut, "/dashboard?doc=10\n") || strings.Contains(errOut, "\x1b") {
		t.Fatalf("put: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, errOut = emd("show", "10")
	if code != 0 || !strings.Contains(out, "/dashboard?doc=10\n") || strings.Contains(out, "\x1b") {
		t.Fatalf("show: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, _ = emd("show", "10", "--json")
	if code != 0 || strings.Contains(out, "dashboard") {
		t.Fatalf("show --json must stay the API's JSON: %q", out)
	}
}

func TestOpen(t *testing.T) {
	_, emd := setup(t)
	var opened []string
	orig := browse
	browse = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { browse = orig })

	code, out, _ := emd("open", "10", "--print")
	if code != 0 || !strings.HasSuffix(out, "/dashboard?doc=10\n") || len(opened) != 0 {
		t.Fatalf("--print: code %d, stdout %q, opened %v", code, out, opened)
	}
	code, out, _ = emd("open", "10")
	if code != 0 || !strings.HasSuffix(out, "/dashboard?doc=10\n") || len(opened) != 1 || !strings.HasSuffix(opened[0], "/dashboard?doc=10") {
		t.Fatalf("open: code %d, stdout %q, opened %v", code, out, opened)
	}
	if code, _, _ := emd("open", "99"); code != api.ExitNotFound || len(opened) != 1 {
		t.Fatalf("a missing document: exit %d, opened %v", code, opened)
	}
}
