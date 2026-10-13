// Command emd is the command-line client for easymarkdown (https://easymarkdown.io).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/imgerson/emd/internal/api"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const defaultURL = "https://easymarkdown.io/api/v1"

// usageError exits 2 (bad flags, missing arguments, an ambiguous path).
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// app is what every command gets: the client, output streams and the --json switch.
type app struct {
	client *api.Client
	out    io.Writer
	errOut io.Writer
	in     io.Reader
	json   bool
	getenv func(string) string
}

type command struct {
	usage string
	help  string
	run   func(a *app, args []string) error
	noKey bool // runs without an API key (login, version, help)
}

var commands map[string]command

func init() {
	commands = map[string]command{
		"login":    {"login [--url URL]", "save an API key (read from stdin) after checking it", cmdLogin, true},
		"ls":       {"ls [--folder ID|PATH|none] [--status S] [--archived]", "list documents", cmdLs, false},
		"tree":     {"tree [--archived]", "projects, folders and documents, with IDs and statuses", cmdTree, false},
		"search":   {"search QUERY", "search folder names and document titles and content", cmdSearch, false},
		"get":      {"get ID [-o FILE]", "print a document's Markdown (or write it to FILE)", cmdGet, false},
		"show":     {"show ID", "a document's details, with its link", cmdShow, false},
		"open":     {"open ID [--print]", "open a document in the browser and print its link (--print: only print)", cmdOpen, false},
		"new":      {"new --title T [--folder ID|PATH] [--status S] [FILE|-]", "create a document: its ID on stdout, its link on stderr", cmdNew, false},
		"put":      {"put ID [FILE|-] [--if-unchanged-since T]", "replace a document's content (never overwrites newer edits); its link on stderr", cmdPut, false},
		"status":   {"status ID STATUS", "set a status: draft, review, approved, done, archived", cmdStatus, false},
		"rename":   {"rename ID TITLE", "rename a document", cmdRename, false},
		"mv":       {"mv ID --folder ID|PATH|none [--top|--bottom|--before ID|--after ID]", "move or reorder a document", cmdMv, false},
		"rm":       {"rm ID --yes", "delete a document and its history", cmdRm, false},
		"mkdir":    {"mkdir PATH [--status S]", "create a folder, and any missing parents (\"Project/Sub\")", cmdMkdir, false},
		"rmdir":    {"rmdir ID|PATH", "delete an empty folder", cmdRmdir, false},
		"log":      {"log ID [--snapshots]", "history, newest first (--snapshots: named snapshots only)", cmdLog, false},
		"cat":      {"cat ID REV", "print one past version", cmdCat, false},
		"diff":     {"diff ID [REV [REV]]", "unified diff: latest change; REV vs now; or REV vs REV", cmdDiff, false},
		"snapshot": {"snapshot ID [--name N]", "save a named snapshot", cmdSnapshot, false},
		"restore":  {"restore ID REV", "restore a past version (the server keeps a backup snapshot)", cmdRestore, false},
		"export":   {"export DIR", "write every document to DIR as Markdown files, plus manifest.json", cmdExport, false},
		"skill":    {"skill [install [--dir DIR]]", "print the agent skill, or install it for Claude Code (~/.claude/skills)", cmdSkill, true},
		"version":  {"version", "print the version", func(a *app, _ []string) error { fmt.Fprintln(a.out, "emd", version); return nil }, true},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, in io.Reader, out, errOut io.Writer, getenv func(string) string) int {
	// --json can go anywhere before a literal "--"; after it, it's an ordinary argument.
	jsonOut := false
	var rest []string
	for i, a := range args {
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if a == "--json" {
			jsonOut = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		printHelp(out)
		return api.ExitOK
	}
	cmd, ok := commands[rest[0]]
	if !ok {
		fmt.Fprintf(errOut, "emd: unknown command %q (see emd help)\n", rest[0])
		return api.ExitUsage
	}

	a := &app{out: out, errOut: errOut, in: in, json: jsonOut, getenv: getenv}
	if !cmd.noKey {
		cfg, err := loadConfig(getenv)
		if err != nil {
			fmt.Fprintln(errOut, "emd:", err)
			return api.ExitAuth
		}
		a.client = api.New(cfg.URL, cfg.Token, version)
	}

	err := cmd.run(a, rest[1:])
	if err == nil {
		return api.ExitOK
	}
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(errOut, "emd: %s\nusage: emd %s\n", ue.msg, cmd.usage)
		return api.ExitUsage
	}
	var ae *api.Error
	if jsonOut && errors.As(err, &ae) && len(ae.Body) > 0 {
		errOut.Write(ae.Body)
		fmt.Fprintln(errOut)
	} else {
		fmt.Fprintln(errOut, "emd:", hint(err))
	}
	return api.ExitCode(err)
}

// hint adds what to do next for the errors people actually hit.
func hint(err error) string {
	var e *api.Error
	if !errors.As(err, &e) {
		return err.Error()
	}
	switch e.Code {
	case "unauthenticated":
		return e.Error() + " (create a key in Settings -> API keys, then run: emd login)"
	case "missing_ability":
		return e.Error() + " (this key can't do that; create one with documents:write)"
	case "conflict":
		return e.Error() + " (changed since you read it: get it again, merge, then put)"
	case "subscription_ended":
		return e.Error() + " (reads still work; resubscribe in Settings to write)"
	}
	return e.Error()
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, "emd %s: the command line for easymarkdown\n\nusage: emd COMMAND [ARGS] [--json]\n\n", version)
	names := []string{"login", "ls", "tree", "search", "get", "show", "open", "new", "put", "status", "rename", "mv", "rm",
		"mkdir", "rmdir", "log", "cat", "diff", "snapshot", "restore", "export", "skill", "version"}
	for _, n := range names {
		fmt.Fprintf(w, "  %-60s %s\n", commands[n].usage, commands[n].help)
	}
	fmt.Fprint(w, `
--json prints the API's JSON (errors go to stderr as the API sent them).
Links (show, open, and new/put on stderr) are clickable in terminals that support it.
Config: EMD_TOKEN and EMD_URL, else ~/.config/emd/config.json (written by emd login).
Exit codes: 0 ok, 1 unexpected, 2 usage, 3 conflict, 4 auth, 5 payment, 6 not found,
7 rate limited, 8 validation or limits.
Docs: https://easymarkdown.io/docs/api
`)
}

// ---- Flags ----

// parse parses flags anywhere among the arguments (Go's flag package stops at the first
// positional), so `emd put 42 plan.md --if-unchanged-since T` works.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		before := len(args)
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		rest := fs.Args()
		// fs.Parse swallowed a "--": everything after it is positional, even "--weird".
		if consumed := before - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// ---- Config ----

type config struct {
	Token string `json:"token"`
	URL   string `json:"api_url"`
}

func configPath(getenv func(string) string) string {
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "emd", "config.json")
}

// loadConfig: EMD_TOKEN / EMD_URL win over the config file; the URL defaults to easymarkdown.io.
func loadConfig(getenv func(string) string) (config, error) {
	var cfg config
	if b, err := os.ReadFile(configPath(getenv)); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if t := getenv("EMD_TOKEN"); t != "" {
		cfg.Token = t
	}
	if u := getenv("EMD_URL"); u != "" {
		cfg.URL = u
	}
	if cfg.URL == "" {
		cfg.URL = defaultURL
	}
	if cfg.Token == "" {
		return cfg, errors.New("no API key: run `emd login` or set EMD_TOKEN (create a key in Settings -> API keys)")
	}
	if err := checkURL(cfg.URL); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// checkURL refuses to send the key anywhere but https (or a local server).
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("bad API URL %q", raw)
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fmt.Errorf("refusing to send your key to %s: use https", raw)
	}
	return nil
}

func saveConfig(getenv func(string) string, cfg config) error {
	p := configPath(getenv)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// ---- Small helpers ----

func (a *app) printJSON(raw []byte) {
	a.out.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		fmt.Fprintln(a.out)
	}
}

func (a *app) emitJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	a.printJSON(b)
	return nil
}

// readInput reads FILE, or stdin for "-" or no argument. It refuses to wait on a terminal.
func (a *app) readInput(arg string) (string, error) {
	if arg == "" || arg == "-" {
		if f, ok := a.in.(*os.File); ok {
			if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
				return "", usagef("pass a FILE, or pipe the content in (-)")
			}
		}
		b, err := io.ReadAll(a.in)
		return string(b), err
	}
	b, err := os.ReadFile(arg)
	return string(b), err
}

var statuses = []string{"draft", "review", "approved", "done", "archived"}

func checkStatus(s string) error {
	for _, v := range statuses {
		if s == v {
			return nil
		}
	}
	return usagef("unknown status %q (one of %s)", s, strings.Join(statuses, ", "))
}

// when renders an API timestamp as "2006-01-02 15:04" local time.
func when(ts string) string {
	t, err := parseTime(ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04")
}
