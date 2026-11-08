# Glose

Review a Markdown text with your AI agent, in the margins.

Glose shows a Markdown text, rendered, in your browser. The author comments on it and suggests changes; the AI agent (Claude Code, Codex, OpenCode…) replies and suggests its own; the author accepts or rejects each one with a click. Everything is written into the Markdown file itself, as [CriticMarkup](https://github.com/CriticMarkup/CriticMarkup-toolkit), at the exact place concerned and nowhere else. The history of the exchange is kept in a journal.

> A *gloss* (French *glose*) is a note written in the margin of a text to explain or discuss it; the glossators of Bologna annotated the *Digest* this way.

**Status:** experimental (version 0.x). Reading, commenting, suggesting and editing on screen, running and following the review rounds, keeping drafts; durable writes; large books displayed part by part. Tested on Linux and WSL; native Windows builds but has not been tried in real use yet.

> [!NOTE]
> **Glose is currently in French only**: the interface, the command-line messages, the instructions written for the agent and the documentation in `docs/`. An English version is on the TODO list.

## Principles

- **The file is never regenerated:** Glose only touches the ranges that are annotated or decided upon. Every other byte stays as it was (line endings, byte order mark, formatting).
- **The author decides:** the agent suggests, it never applies.
- **Everything stays local:** the server listens on `127.0.0.1` only, with a session token, and makes no outgoing request.
- **One program** (`glose`, written in Go), plus pandoc.

## Installation

Requirements: Go 1.27 (to build) and pandoc 3.1 or later (3.1.3 is the only version tested).

```bash
go install github.com/ptitan/glose/cmd/glose@latest
```

or, from a clone of the repository:

```bash
./scripts/build.sh          # builds bin/glose
cp bin/glose ~/.local/bin/  # or any directory in your PATH
```

`glose doctor` checks the installation (pandoc, Git, browser) and the project, and says how to fix each problem.

## Usage

```bash
glose open                  # the current directory: starts the server in the background, opens the browser
glose open notes.md         # a single file
glose init                  # glose.json profile and a "Navette Glose" section for the agent (AGENTS.md or CLAUDE.md)
glose set                   # agent settings, for all projects: installed agents, short names (agent + model), default agent
glose set --list            # the same, printed, without the browser
glose home                  # your projects: whose hand, annotations, progress, deadline; a click opens one
glose home --list           # the same, printed (--json: stable keys, never a token)
glose project --status review --deadline 2026-12-31  # says something of the project, in glose.json, keeping every other byte
glose init --type article   # starts from your template gabarits/article/ (configuration directory), overwriting nothing
glose help profile          # the glose.json profile: files, directories, patterns, display filter
glose status                # whose turn it is, how many annotations, which problems
glose list                  # the annotations, as file:line:column; --json for an agent
glose log                   # the history: turns, operations, archived decisions
glose doctor                # installation and project checks, with fixes
glose check                 # profile, pandoc, marks, identifiers, journal (exit code 1 on error; warnings leave 0)
glose render > page.html    # the rendered HTML, to check a filter
glose stop                  # stops the server
```

### In the browser

A sticky top bar holds the title, the hand-over banner (who has the hand, the round, what you are about to hand over; a click hands over or takes the hand back), then, on the right, the three views, undo, redo and display settings, and the search field.

Select a passage, then **Commenter** (comment, `C`) or **Suggérer** (suggest, `S`); the handle in the left margin acts on a whole paragraph. Each annotation is written to the file at once, as CriticMarkup, with its own identifier. The cards in the margin show the thread (author and agent) and the decisions: accept, reject, resolve, or accept everything in a file. Undo and redo (`Ctrl+Z`, `Ctrl+Y`) apply to your latest actions.

Keyboard: `J` and `K` move between annotations; on the active card, `A` accepts, `X` rejects, `R` replies, `E` edits your own suggestion; `1`, `2`, `3` switch views (changes, current text, proposed text). The left column has four tabs (`Alt+1` to `Alt+4`): table of contents (with review progress), files (choose the part displayed), annotations (with filters), and the timeline of turns; `Alt+0` folds the column away.

**Search** (`Ctrl+Shift+F`) looks through every file, even those not displayed, in the current or the proposed text. Markdown links between chapters open the target part. Archived discussions can be read again from the timeline: suggestion, decision and the whole thread.

### Taking turns with the agent

When the author is done, they click **Remettre à l'agent** (hand over to the agent). The agent, guided by the section written by `glose init`, then works with:

```bash
glose wait                  # waits for the author to hand over (returns at once if they already have)
glose list --json           # the annotations to deal with
glose reply --id ID --text "…"                                          # reply in a thread
glose propose --file a.md --find "text" --new "other text" --note "…"   # suggest a change, with an explanation (--whole: as one block)
# … or edit the file freely: its changes will become suggestions
glose done --note "…"       # hands the turn back; free edits become suggestions
```

The author accepts or rejects; the review ends when no annotation is left. The agent never decides. If the agent does not hand its turn back, the author can take it back from the top bar.

### Agents

Glose works with a command-line agent (Claude Code, Codex, OpenCode); it never calls a model itself and stores no key: each agent keeps its own login and tools (MCP servers, skills).

```bash
glose set                   # opens the agent settings page (shared by all projects)
glose join CODE --agent Sol # binds an agent session already open elsewhere, then waits for the hand
```

- **`glose set`** lists the installed agents, lets you name agent + model pairs with short names (« Sol » = Codex with a given model) and choose the default one. **Essayer** (try) runs an agent dry, without writing anything. Until an agent is configured, the interface and `glose open`, `status`, `doctor` say so.
- **Launch at hand-over.** When you hand over, Glose can launch the agent itself: non-interactive, in the project directory, with reduced permissions (run `glose`, read and write the text), one at a time, with a time limit. The capsule menu shows its output, stops it, or launches it again.
- **Bind a session.** « Lier un agent » (menu of the hand-over banner) gives an instruction ready to paste into an agent session, with a short code. `glose join` binds the session and waits for your hand-over; when it ends with « délai écoulé » (timeout), the agent runs the same command again, and again for every following turn. The capsule shows who listens, with the model when known.
- **One agent per annotation.** An annotation may name the agent that must answer (« Réponse par », or `@Name` at the start of the text). Glose then runs one pass per agent, in sequence, and hands the turn back itself; every reply is signed with the agent's name and model.
- A text may ask for its own agent, instructions and required tools in its `glose.json` (key `agent`, see `glose help profile`).

### Projects

Without a profile, Glose takes the `.md` files of the directory and its subdirectories, in table-of-contents order (skipping hidden directories and agent instruction files). A `glose.json` file can choose the files, directories or patterns that make up the text, exclude others, set a title, and adapt the display with a Lua filter and a stylesheet of the text's own (`glose help profile`).

**Drafts.** Comments, replies and rewrites are saved as you type, apart from the manuscript, in `.glose/brouillons/`. Closing a panel keeps what you typed; the **Brouillons** (drafts) button lets you resume or discard it. These files hold your private input: back them up if needed, but do not publish them by default.

**Backups.** Track your `.md` files, `.glose/journal.jsonl` and `.glose/snapshots/` with Git; the rest of `.glose/` is local.

**Limits.** UTF-8 only. What Glose cannot locate to the exact character can be annotated at paragraph level only, never by guesswork. A very large single file is re-rendered section by section (2 to 3 s for 240,000 words in one file).

## Documentation (in French)

- `docs/GUIDE.md`: preparing a text, running a review, backing up, recovering when something goes wrong, what Glose can read.
- `docs/TESTS-NAVIGATEUR.md`: reproducible browser tests in Chrome/Chromium (`./scripts/test-browser.sh`), without any npm package.
- `docs/UNICODE.md`: maintaining the Unicode tables.

Development checks: `./scripts/check.sh` (formatting, static analysis, tests, Windows cross-build).

## TODO

- English version of the interface, command-line messages, agent instructions and documentation.
- Real-use testing on native Windows.

## License

Glose is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License, version 3 or (at your option) any later version. See `LICENSE`.

Icons come from [Lucide](https://lucide.dev) (ISC license, notice reproduced in `web/icones.js`). The fonts Inter and Source Serif 4 are embedded in the executable (SIL Open Font License 1.1, texts in `web/polices/`).
