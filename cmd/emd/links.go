package main

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	"github.com/imgerson/emd/internal/api"
)

// docURL is the link that opens a document in the browser: the API's `url` field, or, from a
// server that predates it, the same link built from the API base URL. A `url` that isn't a plain
// http(s) link (or carries control characters a terminal would act on) is ignored.
func docURL(base string, d api.Document) string {
	if u, err := url.Parse(d.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" &&
		!strings.ContainsFunc(d.URL, unicode.IsControl) {
		return d.URL
	}
	return fmt.Sprintf("%s/dashboard?doc=%d", strings.TrimSuffix(strings.TrimRight(base, "/"), "/api/v1"), d.ID)
}

// noControls replaces control characters (C0 and C1, so ESC, BEL and 8-bit CSI too) with '?', so
// text from the server can't drive the terminal it is printed to.
func noControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}

// hyperlink wraps u in an OSC 8 escape so terminals that support it make it clickable (the
// others show the text as is). Only for a terminal: pipes and files get the plain URL.
func hyperlink(u string, tty bool) string {
	if !tty {
		return u
	}
	return "\x1b]8;;" + u + "\x1b\\" + u + "\x1b]8;;\x1b\\"
}

// isTerminal: w is a terminal that can take escape sequences.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// printLink writes the document's link on its own line to w, clickable when w is a terminal.
func (a *app) printLink(w io.Writer, d api.Document) {
	fmt.Fprintln(w, hyperlink(docURL(a.client.Base, d), isTerminal(w)))
}

// browse opens u in the default browser; replaced in tests.
var browse = func(u string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, u).Start()
}

func cmdOpen(a *app, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	printOnly := fs.Bool("print", false, "only print the link")
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
	var d api.Document
	raw, err := a.client.JSON("GET", fmt.Sprintf("/documents/%d", n), nil, nil, &d)
	if err != nil {
		return err
	}
	if a.json {
		a.printJSON(raw)
	} else {
		a.printLink(a.out, d)
	}
	if *printOnly {
		return nil
	}
	if err := browse(docURL(a.client.Base, d)); err != nil {
		return fmt.Errorf("couldn't open a browser (%v): use the link above", err)
	}
	return nil
}
