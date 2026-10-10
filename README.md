# emd

The command line for [easymarkdown](https://easymarkdown.io): list, read, write, version and
export your Markdown documents from a terminal, a script or an AI agent.

```bash
brew install imgerson/emd/emd
emd login            # paste an API key from easymarkdown → Settings → API keys
emd tree
```

Without Homebrew, download the archive for your system from
[Releases](https://github.com/imgerson/emd/releases), check it against `SHA256SUMS`, and put
`emd` on your `PATH`. One static binary, no dependencies.

## Commands

```
emd ls      [--folder ID|PATH|none] [--status S] [--archived]
emd tree    [--archived]                     projects > folders > documents, with IDs
emd search  QUERY
emd get     ID [-o FILE]                     the Markdown, byte for byte
emd show    ID
emd new     --title T [--folder ID|PATH] [--status S] [FILE|-]      prints the new ID
emd put     ID [FILE|-] [--if-unchanged-since T]
emd status  ID draft|review|approved|done|archived
emd rename  ID TITLE
emd mv      ID --folder ID|PATH|none [--top|--bottom|--before ID|--after ID]
emd rm      ID --yes
emd mkdir   PATH [--status S]                creates missing levels: "Project/Plans"
emd rmdir   ID|PATH                          empty folders only
emd log     ID [--snapshots]                 history, newest first
emd cat     ID REV
emd diff    ID [REV [REV]]                   latest change; REV vs now; REV vs REV
emd snapshot ID [--name N]
emd restore ID REV                           the server keeps a "Before restore" backup
emd export  DIR                              every document as DIR/Project/Folder/Title.md + manifest.json
```

Add `--json` to any command to get the API's JSON on stdout (errors go to stderr, as the API
sent them). Folders can be named by ID or by path; a path that matches more than one folder is
an error that lists their IDs.

`put` never overwrites edits made in the browser: it reads the document's version first and
sends it back, and if the document changed in between it stops with exit code 3.

## Configuration

`EMD_TOKEN` and `EMD_URL` win over `~/.config/emd/config.json` (written by `emd login`, mode
0600). `EMD_URL` defaults to `https://easymarkdown.io/api/v1`; the key is only ever sent over
https (or to a local server). `emd login` reads the key from stdin, so it stays out of your shell
history:

```bash
emd login < key.txt
EMD_TOKEN='12|emd_…' emd ls        # CI and agents
```

Keys contain a `|`, so always quote them in a shell.

Each key is limited to 60 requests a minute; `emd` slows down as it gets close and waits once
on `429`. Your browser has its own limit, so a script never slows the editor down.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Unexpected (network, server error) |
| 2 | Usage: bad arguments, ambiguous folder path |
| 3 | Conflict: changed since you read it, or folder not empty |
| 4 | Auth: missing, invalid or expired key, or the key lacks write access |
| 5 | Payment: the plan has ended (reads still work) |
| 6 | Not found |
| 7 | Rate limited, even after waiting once |
| 8 | Validation or a limit (512 KB per document, 500 MB per account) |

## For agents

```bash
# Keep the plan for this repo in easymarkdown, create or update
P=$(emd mkdir "$(basename "$PWD")")
ID=$(emd ls --folder "$P" --json | jq -r '.data[] | select(.title=="Plan") | .id')
if [ -n "$ID" ]; then emd put "$ID" plan.md; else emd new --title Plan --folder "$P" --status review plan.md; fi

emd get "$ID" > plan.md     # next session
emd diff "$ID"              # what changed last
```

API reference: <https://easymarkdown.io/docs/api> · OpenAPI:
<https://easymarkdown.io/docs/api/openapi.json>
