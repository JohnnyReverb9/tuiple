-- Options are automatically loaded before lazy.nvim startup
-- Default options that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/options.lua

-- Values marked (settings) come from ~/.config/nvim-ide/settings.json,
-- which tuiple's settings screen edits — see config/ide.lua.
local ide = require("config.ide").settings

-- LazyVim language choices
vim.g.lazyvim_picker = "snacks"
vim.g.lazyvim_php_lsp = ide.php_lsp -- (settings) intelephense is closest to PhpStorm
vim.g.lazyvim_python_lsp = ide.python_lsp -- (settings)
vim.g.ai_cmp = false -- no AI provider: Tab belongs to regular completion
vim.g.autoformat = ide.format_on_save -- (settings)

local opt = vim.opt

-- Indentation (settings); formatters still win per language
opt.expandtab = ide.expand_tab
opt.tabstop = ide.tab_width
opt.shiftwidth = ide.tab_width
opt.softtabstop = ide.tab_width

opt.termguicolors = true
opt.guicursor = ide.insert_cursor == "bar" and "n-v-c:block,i-ci-ve:ver25" or "n-v-c-i:block"

-- IDE feel
opt.scrolloff = 8
opt.sidescrolloff = 8
opt.number = ide.line_numbers ~= "off" -- (settings)
opt.relativenumber = ide.line_numbers == "relative"
opt.cursorline = ide.cursorline
opt.colorcolumn = ide.margin > 0 and tostring(ide.margin) or ""
opt.wrap = ide.wrap
opt.mouse = "a"
opt.mousemoveevent = true
opt.clipboard = ide.clipboard and "unnamedplus" or "" -- (settings) macOS clipboard
opt.confirm = true
opt.updatetime = 200
opt.timeoutlen = 400
opt.completeopt = "menu,menuone,noselect"
opt.pumheight = 12
opt.undofile = true
opt.swapfile = false
opt.autowriteall = ide.autosave -- (settings) like the IDE's autosave on switching buffers
