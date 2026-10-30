-- Options are automatically loaded before lazy.nvim startup
-- Default options that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/options.lua

-- LazyVim language choices
vim.g.lazyvim_picker = "snacks"
vim.g.lazyvim_php_lsp = "intelephense" -- closest to PhpStorm's own analysis
vim.g.lazyvim_python_lsp = "basedpyright"
vim.g.ai_cmp = false -- no AI provider: Tab belongs to regular completion

local opt = vim.opt

-- Indentation: real tabs, 4 wide (formatters still win per language)
opt.expandtab = false
opt.tabstop = 4
opt.shiftwidth = 4
opt.softtabstop = 4

opt.termguicolors = true
opt.guicursor = "n-v-c-i:block"

-- IDE feel
opt.scrolloff = 8
opt.sidescrolloff = 8
opt.relativenumber = false -- absolute numbers, like the IDE gutter
opt.colorcolumn = "120" -- PhpStorm's right margin
opt.cursorline = true
opt.wrap = false
opt.mouse = "a"
opt.mousemoveevent = true
opt.clipboard = "unnamedplus" -- yank/paste through the macOS clipboard
opt.confirm = true
opt.updatetime = 200
opt.timeoutlen = 400
opt.completeopt = "menu,menuone,noselect"
opt.pumheight = 12
opt.undofile = true
opt.swapfile = false
opt.autowriteall = true -- like the IDE's autosave on switching buffers
