#!/usr/bin/env bash
# Installs the LazyVim "IDE" config (PhpStorm-style keys, Tab completion) on macOS.
#
#   ./install.sh                 copy the config to ~/.config/nvim
#   ./install.sh --link          symlink it instead (edit here, both stay in sync)
#   ./install.sh --iterm-default also make "nvim IDE" the default iTerm2 profile
#   ./install.sh --no-brew       skip Homebrew packages
#
# The previous ~/.config/nvim is moved to ~/.config/nvim.bak.<timestamp>.
# Set NVIM_APPNAME to install under another name (e.g. to try it side by side).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP="${NVIM_APPNAME:-nvim}"
TARGET="$HOME/.config/$APP"
STAMP="$(date +%Y%m%d-%H%M%S)"
LINK=0 ITERM_DEFAULT=0 BREW=1

for arg in "$@"; do
	case "$arg" in
	--link) LINK=1 ;;
	--iterm-default) ITERM_DEFAULT=1 ;;
	--no-brew) BREW=0 ;;
	-h | --help)
		sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown option: $arg" >&2
		exit 2
		;;
	esac
done

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m!   %s\033[0m\n' "$*"; }

[[ "$(uname)" == "Darwin" ]] || {
	echo "macOS only" >&2
	exit 1
}

# 1. Tools -------------------------------------------------------------------------

if ((BREW)); then
	step "Homebrew packages"
	command -v brew >/dev/null || {
		echo "Homebrew is required: https://brew.sh" >&2
		exit 1
	}
	brew install neovim git ripgrep fd fzf lazygit tree-sitter-cli node python
	brew install --cask font-jetbrains-mono-nerd-font || warn "Nerd Font not installed; icons may look broken"
fi

command -v nvim >/dev/null || {
	echo "nvim not found" >&2
	exit 1
}
nvim --headless -c 'if !has("nvim-0.11") | cquit 1 | endif' -c q ||
	{
		echo "Neovim 0.11+ required (have $(nvim --version | head -1))" >&2
		exit 1
	}

for tool in go php; do
	command -v "$tool" >/dev/null || warn "$tool not found: its language server won't install (brew install $tool)"
done

# 2. Config ------------------------------------------------------------------------

step "Neovim config -> $TARGET"
if [[ -L "$TARGET" ]]; then
	rm "$TARGET"
elif [[ -e "$TARGET" ]]; then
	mv "$TARGET" "$TARGET.bak.$STAMP"
	echo "old config saved to $TARGET.bak.$STAMP"
fi
mkdir -p "$(dirname "$TARGET")"
if ((LINK)); then
	ln -s "$HERE/nvim" "$TARGET"
else
	cp -R "$HERE/nvim" "$TARGET"
fi

# 3. Plugins, language servers, parsers -------------------------------------------

step "Plugins (lazy.nvim sync)"
nvim --headless "+Lazy! sync" +qa

step "Language servers, formatters, linters, treesitter parsers"
nvim --headless -c "luafile $HERE/bootstrap.lua" || warn "some tools failed to install; run :Mason / :checkhealth inside nvim"

# 4. iTerm2 profile: lets Cmd/Option/F-keys reach Neovim --------------------------

step "iTerm2 profile 'nvim IDE'"
PROFILE="$HOME/Library/Application Support/iTerm2/DynamicProfiles/nvim-ide.json"
ZSH_KEYS="$HOME/.config/nvim-ide/cmd-keys.zsh"
mkdir -p "$(dirname "$ZSH_KEYS")"
GUID="$(python3 "$HERE/iterm/make_profile.py" "$HERE/iterm/keys.tsv" "$PROFILE" "$ZSH_KEYS" | tail -1)"
if ((ITERM_DEFAULT)); then
	defaults write com.googlecode.iterm2 "Default Bookmark Guid" -string "$GUID"
	echo "set as the default profile (applies to new windows)"
fi

# 5. zsh: EDITOR and key bindings ---------------------------------------------------

step "~/.zshrc"
ZSHRC="$HOME/.zshrc"
BEGIN="# >>> nvim-ide >>>"
END="# <<< nvim-ide <<<"
touch "$ZSHRC"
if grep -qF "$BEGIN" "$ZSHRC"; then
	sed -i '' "/$BEGIN/,/$END/d" "$ZSHRC"
fi
cat >>"$ZSHRC" <<EOF
$BEGIN
export EDITOR=nvim VISUAL=nvim
[[ -r "$ZSH_KEYS" ]] && source "$ZSH_KEYS"
$END
EOF
echo "EDITOR=nvim and Cmd-key bindings added"

step "Done"
cat <<EOF
Next:
  * iTerm2 -> Settings -> Profiles -> "nvim IDE" -> Other Actions -> Set as Default
    (or rerun with --iterm-default), then open a new window. Without that
    profile Cmd shortcuts don't reach Neovim; Option/F-keys still work.
  * Font: JetBrainsMono Nerd Font (inherited from your default profile).
  * Cheat sheet: $HERE/README.md
EOF
