# Tuiple 📁✨

**Tuiple** is a lightning-fast, highly aesthetic Terminal User Interface (TUI) file manager written in Go. Inspired by the clean and intuitive layout of the macOS Finder, Tuiple brings a modern 3-panel file browsing experience straight into your terminal. 

Built on top of the awesome [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) frameworks, Tuiple is designed out-of-the-box to look beautiful using standard terminal fonts (like SF Mono) without requiring patched Nerd Fonts.

![Tokyo Night Theme](https://img.shields.io/badge/Theme-Tokyo_Night-blue?style=flat-square)
![Go Version](https://img.shields.io/badge/Go-%3E%3D1.22-00ADD8?style=flat-square&logo=go)

---

## 🎨 Features

*   **Three-Panel UI:** Effortlessly navigate between Locations/Favorites (Sidebar), File List, and a live File/Directory Preview.
*   **Built-in Previews:** Instantly view text files, see directory statistics (with child items), or safely inspect binary files via Hex Dumps.
*   **Vim-like & Native Keybindings:** Navigate your file system smoothly using arrows or standard `hjkl` bindings.
*   **Native File Operations:** Create, rename, delete (with safety confirmation), cut, copy, and paste files and directories cleanly through an inline terminal UI.
*   **Adaptive Filtering & Sorting:** Quickly find what you need by fuzzy filtering names or sorting by size and title.
*   **No Nerd Font Required:** Specially curated Unicode-based icons mean Tuiple looks premium in standard terminal environments.

---

## 🚀 Installation

Ensure you have **Go 1.22+** installed on your system.

1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/tuiple.git
   cd tuiple
   ```

2. Build the binary (an `output/` directory is created for the builds):
   ```bash
   mkdir -p output
   go build -o output/tuiple .
   ```

3. Move the binary to a directory in your PATH (optional):
   ```bash
   sudo mv output/tuiple /usr/local/bin/
   ```

---

## 📖 Usage

Launch Tuiple in your current directory:
```bash
./output/tuiple
```

Or open a specific directory:
```bash
./output/tuiple /path/to/directory
```

### ⌨️ Keyboard Shortcuts

Press `?` at any time inside the app to bring up the quick help menu.

#### **Navigation**
| Key | Action |
| :--- | :--- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Enter` / `→` / `l` | Enter directory / Select |
| `Backspace` / `←` / `h` | Go up one directory (Go Back) |
| `Tab` / `Shift+Tab` | Cycle focus between Sidebar, File List, and Preview |
| `g` / `G` | Jump to the Top / Bottom of the list |
| `Ctrl+U` / `Ctrl+D`| Page Up / Page Down |
| `~` | Go to Home directory |

#### **File Operations**
| Key | Action |
| :--- | :--- |
| `n` | Create a new file |
| `N` (*Shift+n*) | Create a new directory |
| `r` | Rename the selected item |
| `d` | Delete the selected item (Prompts for `y/N` confirmation) |
| `c` | Copy item to Tuiple's internal clipboard |
| `x` | Cut item to Tuiple's internal clipboard |
| `p` | Paste item from Tuiple's internal clipboard |

#### **View & Filters**
| Key | Action |
| :--- | :--- |
| `/` | Start typing to filter files by name. (Press `Esc` to clear) |
| `.` | Toggle visibility of hidden (`.dot`) files |
| `s` | Sort list by Name |
| `S` *(Shift+s)* | Sort list by Size |

#### **Global**
| Key | Action |
| :--- | :--- |
| `?` | Toggle Help Menu overlay |
| `q` / `Ctrl+c` | Quit Tuiple |

---

## 🛠 Architecture

Tuiple follows the strict [Elm architecture](https://guide.elm-lang.org/architecture/) enforced by Bubble Tea. 
*   `app`: Main root model routing messages and maintaining layout constraints.
*   `components/`: Sub-models (`sidebar`, `filelist`, `preview`) maintaining their own logic and views.
*   `clipboard/`: Internal copy/cut buffer implementation.
*   `filesystem/`: File wrappers, safety abstractions, sorting, and formatters.
*   `theme/` & `icons/`: Unified UI tokens providing the beautiful Tokyo Night color palette.

---
*Created as part of an advanced TUI engineering project.*
