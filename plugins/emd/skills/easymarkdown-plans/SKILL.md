---
name: easymarkdown-plans
description: Store the MAIN plan for a piece of work in the user's easymarkdown workspace (easymarkdown.io), so they can review it by hand and pick it up next session, with every revision kept. Use only when the user asks to save/store/put a plan in easymarkdown, or right after the user approves a plan they will need again (then offer once). Save a new revision only when the plan is approved or materially changes, never per step or in a loop. Never for scratch notes, research dumps, drafts, todo or progress logs, code, or anything with secrets.
---

# easymarkdown plans

easymarkdown keeps Markdown documents in projects and folders, each with a status and a full
revision history. This skill puts the **few plans that matter** there: the plan the user must
read and approve, and that they (or you) need on hand next time. Every save is a new revision,
so the user can see how the plan changed over time.

## When to store (all three must hold)

1. **It is the main plan** for a piece of work: the one the user is asked to approve, or
   will act on by hand. Not a sub-plan, not a step list you keep for yourself.
2. **It is needed later**: work that spans sessions, a hand-off, or a decision record.
3. **The user asked**, or the plan was **just approved** and you offer once:
   *"Save this plan to easymarkdown so it's there next session?"* A yes counts; silence or
   no does not. Don't offer again for the same plan.

If you are unsure, don't store. Ask only if the plan is clearly long-lived.

## How often to save

Every save is a permanent revision the user scrolls through, so make each one count:

- Save when the plan is **approved**, or when it **materially changes** (scope, approach or
  order of work), and then once, with the whole plan.
- Never save after each step, each small wording fix, on a timer, or in a loop. Batch your edits
  and save once at the end.
- One plan per piece of work: update it in place (a new revision) rather than creating
  "Plan v2" or a second document.
- Progress belongs in the status (`approved` -> `done`), not in new revisions.

## Never store

- Scratchpad notes, research dumps, brainstorms, intermediate drafts.
- Todo lists, progress or status logs, per-step updates, test output, command output.
- Code, diffs, config files.
- Anything containing secrets, tokens, passwords, customer data, personal data or `.env`
  values. Strip them out or don't store.
- A plan just because you wrote a plan file to disk.

## Setup

The user creates an API key in easymarkdown under **Settings -> API keys** with the
`documents:write` ability (it expires; 90 days by default) and exports it:

```bash
export EMD_TOKEN=...                                # never echo or log it
export EMD_URL=https://easymarkdown.io/api/v1       # optional, this is the default
```

These are the same variables the `emd` CLI reads. The old names `EASYMARKDOWN_TOKEN` /
`EASYMARKDOWN_URL` still work as a fallback for now.

No token? Tell the user how to make one and stop. Never ask them to paste it into chat.

```bash
EMD_TOKEN="${EMD_TOKEN:-$EASYMARKDOWN_TOKEN}"
EM="${EMD_URL:-${EASYMARKDOWN_URL:-https://easymarkdown.io/api/v1}}"
case "$EM" in https://*) ;; *) echo "EMD_URL must be https://" >&2; EM= ;; esac
# The token goes in a header read from a file descriptor, never on the command line (ps).
em() { curl -sS -w '\n%{http_code}\n' -H @<(printf 'Authorization: Bearer %s\n' "$EMD_TOKEN") \
            -H 'Accept: application/json' -H 'Content-Type: application/json' "$@"; }
```

`em` prints the JSON body, then the HTTP status on the last line. If `EMD_URL` points
anywhere other than `https://easymarkdown.io`, confirm with the user before the first call: the
token is sent there.

### If `emd` is installed

`command -v emd` finds it? Prefer it over `curl`: it reads the same `EMD_TOKEN` / `EMD_URL`,
never overwrites browser edits (`put` sends the lock itself; exit 3 on a conflict) and reports
errors as exit codes (4 auth, 5 plan ended, 6 not found, 8 validation or limits).

```bash
P=$(emd mkdir "<project>")                                   # finds or creates it, prints the ID
ID=$(emd ls --folder "$P" --json | jq -r '.data[] | select(.title=="<title>") | .id')
if [ -n "$ID" ]; then emd put "$ID" plan.md; else ID=$(emd new --title "<title>" --folder "$P" --status review plan.md); fi
emd open "$ID" --print                                       # the link to give the user
```

Without `emd`, use the `curl` steps below.

## How to store

**Where:** one project per repository. The project is a top-level folder named after the
repo (`basename "$(git rev-parse --show-toplevel)"`, or the working directory's name). A plan
that isn't about a repository goes in a project named after its topic.

Folders are for finding plans again, not for structure for its own sake:

- Plans go directly in the project. Most projects never need a subfolder.
- Once a project holds **more than about 8 plans**, group them by area (`Billing`, `Infra`)
  in subfolders, one level deep. Move the existing plans with them (`emd mv ID --folder PATH`).
- Use a subfolder the user names, always.
- Never create a folder for a single document, per date, per session or per agent run.
- Before creating any folder, list the existing ones and reuse a match.

**Title:** the plan's own H1 (`# Title`). **Content:** the plan exactly as approved, as
Markdown, with no chat wrapper around it.

1. **Find or create the project.**
   ```bash
   em "$EM/folders"                       # data[]: {id, parent_id, name, status, position}
   ```
   Use the folder with `parent_id: null` and `name` equal to the repo name. If there is
   none, create it (always build JSON with python3, never by pasting values into a string):
   ```bash
   python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1]}))' "<repo>" \
     | em -X POST "$EM/folders" --data-binary @-
   ```

2. **Find the plan: update, don't duplicate.** If the user gave an ID, or you saved this
   plan earlier in the conversation, use that ID. Otherwise list the project's documents
   and match on the exact title:
   ```bash
   em "$EM/documents?folder_id=<project_id>"
   ```

3. **Write it.** Build the JSON with python3 so quotes and newlines survive (`jq -Rs` works too).
   - New plan, status `review` because it is waiting for the user:
     ```bash
     python3 -c 'import json,sys; print(json.dumps({"title": sys.argv[1], "content": open(sys.argv[2]).read(), "folder_id": int(sys.argv[3]), "status": "review"}))' \
       "<title>" plan.md <project_id> | em -X POST "$EM/documents" --data-binary @-
     ```
   - Existing plan: a new revision. Send `updated_at` from your last read so you don't
     overwrite edits the user made in the browser:
     ```bash
     python3 -c 'import json,sys; print(json.dumps({"content": open(sys.argv[1]).read(), "updated_at": sys.argv[2]}))' \
       plan.md "<updated_at>" | em -X PATCH "$EM/documents/<id>" --data-binary @-
     ```

4. **Status.** Use `draft` while it is still being shaped and `review` when it is waiting
   for the user. Set `approved` only when the user says so, and `done` when the work the
   plan describes has shipped (the user says so, or you merged it with them), so it stays
   useful as a record. Set `archived` only when asked:
   ```bash
   python3 -c 'import json,sys; print(json.dumps({"status": sys.argv[1]}))' approved \
     | em -X PATCH "$EM/documents/<id>" --data-binary @-
   ```
   A status change is not a revision and never conflicts with edits.

5. **Report back** in one line: title, ID, status and the link (the `url` field of the
   response, or `emd open ID --print`), for example
   *Saved "Relaunch plan" to easymarkdown (id 42, review): https://easymarkdown.io/dashboard?doc=42*
   Never print the token.

## Reading a plan next session

```bash
em "$EM/documents/<id>"                           # content, status, updated_at
em "$EM/search?q=<words>"                         # search folders, titles and content (URL-encode the words)
em "$EM/documents/<id>/snapshots?kind=auto"       # revision history, newest first
em "$EM/documents/<id>/snapshots/<snapshot_id>"   # one past version's content
```

Your API saves are fixed revisions. The user's browser saves within 10 minutes fold into one
row, so the newest `web` revision can still change for a few minutes.

If the user edited the plan in the browser, their version wins. Read it before changing it.

## Errors

| Status | Meaning | Do |
|---|---|---|
| 401 | Token missing, wrong or expired | Ask the user to create a new key in Settings -> API keys |
| 402 | `subscription_ended` (reads still work) or `subscription_required` | Tell the user; don't retry |
| 403 | Key lacks `documents:write` | Ask for a key with write access |
| 404 | Deleted, or not in this account | Search again by title; don't guess IDs |
| 409 | Changed in the browser since you read it | Re-read, merge with the user's edits, show them the result, then save |
| 422 | Validation (unknown status, folder not theirs, ...) or a storage limit | Fix the request; on a storage limit, tell the user |
| 429 | Rate limited (60 requests a minute per key) | Wait `Retry-After` seconds; never loop |

Full reference: https://easymarkdown.io/docs/api
