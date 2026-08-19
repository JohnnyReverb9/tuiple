# Tuiple

A three-panel file manager for the terminal, written in Go and built on
[Bubble Tea](https://github.com/charmbracelet/bubbletea). Sidebar, file list,
preview — it looks like a file manager, it opens instantly, and it stays out of
the way.

![Tuiple](./assets/preview.png)

It is macOS-first on purpose: previews go through Quick Look, audio through
`afplay`, deletion through Finder's Trash. Think of it as a Finder that lives in
the terminal.

## What it does

Navigation is the usual arrow keys or `hjkl`, with `f` for fuzzy name search
(four directories deep), `F` for full-text search inside files, and `'` to
bookmark the current directory into the sidebar.

Files can be marked with `Space` and copied, moved or deleted in bulk. `E` opens
the marked names in your editor for a bulk rename — search and replace across
fifty files, save, done — and `z` / `Z` zip and unzip. Directory sizes are a
walk away with `s`. Every action goes on an undo stack (`u` / `U`) and into a
persistent log you can browse with `Y`. Deleting is reversible by default: files
wait out a countdown before they are really gone, or go to the macOS Trash if
you prefer — your choice in the settings.

The preview panel renders text with per-extension colouring, images and video
thumbnails as Unicode half-blocks, PDF covers via Quick Look, and audio files as
a player with seeking and metadata. `chafa` gives you a full-quality image view,
`bookokrat` opens e-books, and `S` drops you into a shell in the current
directory and picks you back up when you exit.

Inside a git repository the list gains status markers, the status bar shows the
branch, and `Ctrl+G` opens a panel for staging, committing, log, stashes, blame
and file history. `b` handles branches: checkout, create, rename, merge, rebase,
push, pull.

Shortcuts keep working on a Cyrillic keyboard layout — `ф` acts as `a`, `Ж` as
`:` — while typing (rename, search, commit messages) is never translated. Every
binding can be changed from the settings file, and the help screen always shows
what is actually in force.

## Install

Go 1.21+.

```bash
git clone https://github.com/JohnnyReverb9/tuiple.git
cd tuiple
make install          # builds to ~/.local/bin/tuiple
```

`make` alone builds `./tuiple` in place; `make install BIN=/usr/local/bin/tuiple`
puts it somewhere else. Then run `tuiple` or `tuiple /some/path`.

## Keys

Press `?` in the app for the full list, `,` for settings.

Navigation

| Key | |
| --- | --- |
| `↑` `↓` / `k` `j` | Move the cursor |
| `Enter` `→` `l` | Enter directory, open file |
| `Backspace` `←` `h` | Up to the parent |
| `g` / `G` | Top / bottom |
| `Ctrl+U` / `Ctrl+D` | Half page up / down |
| `~` | Home directory |
| `:` | Go to path (Tab completes) |
| `Tab` / `Shift+Tab` | Switch panel |

Files

| Key | |
| --- | --- |
| `Space` | Mark / unmark |
| `Esc` | Clear marks |
| `c` / `x` / `p` | Copy / cut / paste |
| `d` | Delete |
| `r` | Rename |
| `E` | Bulk rename marked (or all) in $EDITOR |
| `n` / `N` | New file / new directory |
| `z` / `Z` | Zip marked entries / extract the archive |
| `s` | Measure the size of marked directories |
| `u` / `U` | Undo / redo |
| `Y` | Action history |

Search, view, system

| Key | |
| --- | --- |
| `f` / `F` | Search by name / in contents |
| `/` | Filter the current list |
| `'` | Bookmark this directory |
| `.` | Show hidden files |
| `o n` `o s` `o d` | Sort by name / size / date |
| `l` `-` `=` `_` `+` | Audio: play-pause, seek ±5s, seek ±30s |
| `S` | Shell here |
| `,` | Settings |
| `?` | Help |
| `q` | Quit |

Git

| Key | |
| --- | --- |
| `Ctrl+G` | Git panel (commit, log, stashes) |
| `b` | Branches |
| `a` / `R` | Stage / unstage the file under the cursor |
| `D` | Discard its changes |
| `i` | Add it to .gitignore |
| `H` / `L` | Its history / blame |

## Following you back to the shell

A program cannot change its parent shell's directory, so exiting Tuiple normally
leaves you where you started. The wrapper fixes that:

```bash
eval "$(tuiple --shell-init zsh)"     # or bash; fish uses: tuiple --shell-init fish | source
```

That defines `tp`, which runs Tuiple and cds to wherever you exited. Rename the
function to taste — it is four lines of shell. Under the hood it is
`tuiple --cwd-file PATH`, which writes the final directory to a file on exit.

If you want Tuiple to open in the shell's directory rather than your home, set
`files.start_dir` to `cwd` in the settings.

## Optional tools

Everything below is optional — Tuiple degrades to a simpler view without them.

| Tool | For |
| --- | --- |
| `chafa` | Full-quality image and video viewing |
| `ffmpeg` | Video thumbnails, audio seeking |
| `pdftotext` | Text extraction from PDFs |
| `bookokrat` | Reading PDF, EPUB and DJVU |

```bash
brew install chafa ffmpeg poppler
```

`afplay`, `afinfo` and `qlmanage` ship with macOS.

## Settings

Press `,` to open the settings screen — six sections, arrow keys change values,
`e` edits the text ones, `r` resets the highlighted one. Changes are written
immediately to `~/.config/tuiple/config.json`; delete that file and the defaults
come back. Bookmarks live next to it in `bookmarks.json`, the action log in
`audit.log`. Set `TUIPLE_CONFIG` to run against a different settings file.

Deleting

| | Values | |
| --- | --- | --- |
| `delete.mode` | `timer` `trash` `permanent` | `timer` hides the file beside itself and purges it after the countdown; `trash` hands it to the macOS Trash, where Put Back keeps working; `permanent` removes it at once |
| `delete.timer_seconds` | 3–600 | How long `u` can still bring a timer-deleted file back |
| `delete.confirm` | `true` `false` | Whether `d` asks first |

One caveat about `trash`: macOS reserves taking things *out* of the Trash for
Finder. Undo works if your terminal has Full Disk Access (System Settings →
Privacy & Security); without it Tuiple says so, opens Finder on the item, and
leaves the action on the undo stack so `u` works once you have sorted it out.
The `timer` mode has no such problem — the file never leaves the directory.

The file list

| | Values | |
| --- | --- | --- |
| `files.start_dir` | `home` `cwd` | Where `tuiple` opens when you give it no path |
| `files.show_hidden` | `true` `false` | Dotfiles visible on start (`.` still toggles per session) |
| `files.sort` | `name` `size` `date` `type` | Starting sort order |
| `files.reverse_sort` | `true` `false` | Flip it — Z to A, smallest first, oldest first |
| `files.dirs_first` | `true` `false` | Group folders above files whatever the sort |
| `files.time_format` | `short` `iso` `relative` | `Jan 02 15:04`, `2026-01-02 15:04`, or `5m ago` |
| `files.binary_units` | `true` `false` | KiB of 1024 bytes, or kB of 1000 |
| `files.paste_conflict` | `rename` `skip` `overwrite` | What `p` does when the name is taken |
| `files.icons` | `true` `false` | The per-type symbol in front of each row |

The preview panel

| | Values | |
| --- | --- | --- |
| `preview.media` | `true` `false` | Image, video and PDF rendering |
| `preview.line_numbers` | `true` `false` | The gutter in front of text |
| `preview.hex_dump` | `true` `false` | Binary files as hex, or just a note |
| `preview.dir_sizes` | `true` `false` | Walk a previewed directory to total it up |
| `preview.max_text_kb` | 4–65536 | Text files above this are not previewed |
| `preview.max_media_mb` | 1–4096 | Media files above this are skipped |

Layout and git

| | Values | |
| --- | --- | --- |
| `ui.sidebar` | `true` `false` | The favourites column on the left |
| `ui.preview` | `true` `false` | The preview panel on the right |
| `ui.sidebar_width_percent` | 10–40 | Share of the terminal it takes |
| `ui.preview_width_percent` | 15–60 | Same for the preview |
| `git.enabled` | `true` `false` | Status markers, branch, `Ctrl+G`, `b`. Off means no background git calls at all |
| `git.poll_seconds` | 1–60 | How often `git status` is re-checked — raise it in huge repositories |
| `keys.cyrillic_layout` | `true` `false` | Cyrillic keystrokes act as the QWERTY key in the same position |

Rebinding

Every shortcut has an action name — the ones `?` lists, lower case with dashes:
`delete`, `bulk-rename`, `search-name`, `git-panel`, and so on. Name one in
`keys.bindings` and it takes those keys instead; give it an empty list and it is
unbound. Anything you leave out keeps its default, and the help screen follows
whatever is in force.

```json
{
  "keys": {
    "bindings": {
      "delete": ["d", "x"],
      "bulk-rename": ["ctrl+r"],
      "quit": []
    }
  }
}
```

Opening files

`open.editor` and `open.shell` override `$EDITOR` and `$SHELL`. Beyond that,
opening is decided in three layers — a rule for the exact extension, then the
handler for that kind of file, then Tuiple's own behaviour.

`open.handlers` is the coarse one, and it is what the Opening tab edits: point a
whole kind of file at a command and be done. The kinds are `image`, `video`,
`pdf`, `ebook`, `audio`, `archive` and `default`; leave one out and the built-in
stays (chafa for pictures, bookokrat for books, the internal player for audio,
your editor for the rest).

```json
{
  "open": {
    "editor": "nvim",
    "handlers": {
      "pdf": "open -a Preview {}",
      "image": "open -a Preview {}",
      "audio": "mpv --no-video {}"
    },
    "rules": [
      { "extensions": [".md"], "command": "glow -p {}", "terminal": true },
      { "extensions": [".psd"], "command": "open -a Photoshop {}", "terminal": false }
    ]
  }
}
```

`{}` becomes the file path (appended if you leave it out). A handler starting
with `open` is treated as a GUI app and launched in the background; anything
else gets the terminal, and Tuiple comes back when it exits. `rules` says it
explicitly with `terminal`, and is there for the cases a whole kind is too broad
— one extension routed somewhere of its own.

## License

MIT. Built on the Charm stack.
