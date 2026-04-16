<div align="center">
  <h1>Tuiple 📁</h1>
  <p>A fast, modern, and beautiful Terminal User Interface (TUI) file manager written in Go.</p>
</div>

![Tuiple UI Preview](https://via.placeholder.com/800x400.png?text=Tuiple+TUI+File+Manager)

## 🌟 About Tuiple

**Tuiple** is designed for power-users who want the speed of the terminal without sacrificing aesthetics and essential quality-of-life features. Built with `Bubble Tea` and styled with `Lip Gloss`, it features a clean layout, deep integration with your OS, and native integration into modern light/dark terminal color palettes.

### ✨ Key Features

- **Modern UI:** Unobtrusive three-panel layout (Sidebar, File List, File Preview) that intelligently conforms to your terminal.
- **Smart Navigation:** Native `hjkl` bindings. 
- **Lightning Fast Search Engine:** 
  - **Fuzzy Name Search** (`f`): Rapidly find files nested up to 4 directories deep.
  - **Content Search** (`F`): RipGrep-style full text search inside your documents.
- **Vim-Style Bookmarks:** Save any directory to a character (`m` + `char`) and jump across the galaxy instantly (`'` + `char`). Bookmarks are persistently saved!
- **Multi-Select & Bulk Operations:** Mark dozens of files with `<Space>` and Copy/Cut/Delete them instantly.
- **Rich File Previews:** Look at file contents with syntax-like colors for specific extensions without opening them.
- **Instant Shell Drops:** Press `S` to pause Tuiple, drop seamlessly into a robust bash/zsh shell to execute commands, and instantly teleport right back.
- **Mouse Support:** Scroll through everything using native touchpad/mouse wheel bindings!

---

## 💻 Installation

Make sure you have [Go](https://golang.org/dl/) 1.21+ installed.

```bash
# Clone the repository
git clone https://github.com/JohnnyReverb9/tuiple.git
cd tuiple

# Build the project
go build -o tuiple .

# Run the app
./tuiple [optional/start/path]
```

---

## ⌨️ Global Keyboard Shortcuts

**Navigation**
| Key | Action |
| --- | --- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` / `l` | Enter directory |
| `Backspace` / `h` | Go back to parent directory |
| `g` / `G` | Jump to the very top / bottom |
| `Ctrl+u` / `Ctrl+d` | Page up / Page down |
| `~` | Fly to Home directory |
| `Tab` / `Shift+Tab` | Switch focus between panels |

**File Operations**
| Key | Action |
| --- | --- |
| `Space` | Toggle multi-selection on file |
| `Esc` | Clear all selections |
| `c` / `x` / `p` | Copy / Cut / Paste |
| `d` | Delete the selected file(s) |
| `r` | Rename current file |
| `n` / `N` | Create new File / new Directory |

**Search & Bookmarks**
| Key | Action |
| --- | --- |
| `f` | Open Fuzzy File Search (by name) |
| `F` | Open Full-Text Search (in contents) |
| `/` | Live list filter |
| `m` + `<char>` | Bookmark current path to `<char>` |
| `'` + `<char>` | Jump globally to bookmark `<char>` |

**System & Sorting**
| Key | Action |
| --- | --- |
| `.` | Toggle hidden files display |
| `o n` / `o s` / `o d`| Sort by: Name / Size / Date |
| `S` | Drop to Subshell (`sh`/`bash`/`zsh`) |
| `?` | Show comprehensive Help screen |
| `q` / `Ctrl+c` | Quit Tuiple |

---

## 🗃️ Configuration

Tuiple automatically saves persistent runtime logic into `~/.config/tuiple/`.
- **Bookmarks:** Found in `~/.config/tuiple/bookmarks.json`.

---

<p align="center">
  <i>Made with ❤️ by JohnnyReverb9. Built on top of Charm.sh ecosystem.</i>
</p>
