# nvim IDE

LazyVim with PhpStorm-style controls (macOS keymap) and Tab completion.
Vim motions, the `<leader>` menus and every LazyVim default stay as they are:
the IDE shortcuts live on Cmd, Option and the F-keys, which vim itself leaves alone.

## Install

```bash
./install.sh                  # copy the config to ~/.config/nvim
./install.sh --link           # or symlink it — edit here, it works everywhere
./install.sh --iterm-default  # also make the "nvim IDE" iTerm2 profile the default
./install.sh --no-brew        # skip Homebrew packages
```

What the script does:

1. `brew install`s neovim, ripgrep, fd, fzf, lazygit, tree-sitter-cli, node, python
   and the JetBrainsMono Nerd Font.
2. Moves the old `~/.config/nvim` to `~/.config/nvim.bak.<timestamp>` and puts this one in place.
3. Syncs plugins, then installs through Mason every language server, formatter, linter
   and debugger (gopls, basedpyright + ruff, intelephense, vtsls, clangd, html/css/emmet,
   tailwind, eslint, prettier, delve, codelldb…) plus the treesitter parsers. It waits
   for all of them, so the first real start is ready to work.
4. Creates the iTerm2 dynamic profile **"nvim IDE"**, which inherits your default
   profile and passes Cmd shortcuts through to Neovim.
5. Adds a `# >>> nvim-ide >>>` block to `~/.zshrc`: `EDITOR=nvim` (tuiple picks it up)
   and key bindings so the same keys don't print garbage in the shell.

Go and PHP need `go` and `php` themselves (`brew install go php`), otherwise their
language servers won't install.

> Cmd shortcuts only work in the "nvim IDE" profile. Turn it on in
> iTerm2 → Settings → Profiles → nvim IDE → Other Actions → Set as Default, then open a new window.
> In that profile Cmd+D / Cmd+F / Cmd+R / Cmd+O / Cmd+[ ] no longer do their iTerm thing
> (split, find and so on) — they go to Neovim. Cmd+C/V/T/W/N and Cmd+digits are untouched.

## Completion

| Key | |
| --- | --- |
| appears on its own | the first item is selected and previewed in grey right in the text |
| `Tab` | accept; with no menu — next snippet field, otherwise a plain Tab |
| `Shift+Tab` | previous snippet field |
| `↑` `↓` / `Ctrl+N` `Ctrl+P` | pick an item |
| `Enter` | accept, **only after you picked an item with the arrows**; otherwise just a new line |
| `Esc` | close the menu and leave insert mode in one press |
| `Ctrl+Space` | open the menu / documentation |
| `Cmd+J` | snippets (live templates) |
| `Cmd+P` / `Ctrl+K` | parameter info |
| `Ctrl+B` / `Ctrl+F` | scroll documentation |

## PhpStorm keys

| Key | Action |
| --- | --- |
| `Cmd+S` | save all (buffers also autosave when you switch away) |
| `Cmd+Z` / `Cmd+Shift+Z` | undo / redo |
| `Cmd+Shift+O` | go to file |
| `Cmd+O` / `Cmd+Option+O` | go to class / symbol |
| `Cmd+E` | recent files |
| `Cmd+Shift+A` | find action (all commands) |
| `Cmd+F12` / `Option+7` | file structure / Structure panel |
| `Option+1` | project tree |
| `Option+F12` | terminal |
| `Option+9` | git (lazygit) |
| `Cmd+[` `Cmd+]`, `Cmd+Option+←/→` | back / forward |
| `Cmd+F` / `Cmd+Shift+F` | find in file / in path |
| `Cmd+R` / `Cmd+Shift+R` | replace in file / in path |
| `Cmd+B` | go to declaration |
| `Cmd+Option+B` | go to implementation |
| `Cmd+Shift+B` | go to type declaration |
| `Option+F7` | find usages |
| `Shift+F6` | rename (with live preview) |
| `Option+Enter` | intention actions / quick fix |
| `Cmd+Option+L` | reformat code |
| `Ctrl+Option+O` | optimize imports |
| `F1` | quick documentation |
| `F2` / `Shift+F2` | next / previous problem |
| `Cmd+/` | comment |
| `Cmd+D` | duplicate line / selection |
| `Cmd+Backspace` | delete line |
| `Option+Shift+↑/↓`, `Cmd+Shift+↑/↓` | move line |
| `Option+↑` / `Option+↓` | extend / shrink selection |
| `Cmd+A` | select all |
| `F5` / `F9` | debug: start / resume |
| `F8` / `F7` / `Shift+F8` | step over / into / out |
| `Cmd+F8` | toggle breakpoint |

On a Mac the F-keys need `Fn` unless "Use F1, F2, etc. keys as standard function keys" is on.

Everything else is stock LazyVim: `Space` opens the which-key menu
(`Space Space` files, `Space /` grep, `Space c` code, `Space g` git, `Space d` debug,
`Space t` tests), `gd`/`gr`/`K`, `s` for a flash jump, `U` for redo.

## Where things are

- `nvim/lua/config/keymaps.lua` — the PhpStorm key layer
- `nvim/lua/plugins/completion.lua` — completion behaviour (blink.cmp)
- `nvim/lua/plugins/languages.lua` — HTML/CSS/Emmet/bash; other languages are extras in `nvim/lazyvim.json`
- `nvim/lua/config/options.lua` — options (4-wide tabs, block cursor, margin at 120…)
- `iterm/keys.tsv` — which keys iTerm2 sends to Neovim; rerun `install.sh` after editing it
