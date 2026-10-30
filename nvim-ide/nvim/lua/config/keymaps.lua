-- Keymaps are automatically loaded on the VeryLazy event
-- Default keymaps that are always set: https://github.com/LazyVim/LazyVim/blob/main/lua/lazyvim/config/keymaps.lua
--
-- PhpStorm (macOS keymap) layer on top of LazyVim. Vim motions, <leader>
-- menus and LazyVim defaults stay as they are; IDE shortcuts are added on
-- Cmd (<D-…>), Option (<M-…>) and F-keys, which vim itself does not use.
-- Cmd keys reach Neovim only through the iTerm2 profile that install.sh sets up.

local map = vim.keymap.set
local n, i, v = "n", "i", "x"
local ni, niv = { n, i }, { n, i, v }

vim.keymap.set("n", "U", "<C-r>", { desc = "Redo" })

-- Files and buffers -----------------------------------------------------------

map(niv, "<D-s>", "<Cmd>wall<CR>", { desc = "Save all" })
map(ni, "<D-z>", "<Cmd>undo<CR>", { desc = "Undo" })
map(ni, "<D-Z>", "<Cmd>redo<CR>", { desc = "Redo" })
map(n, "<D-a>", "ggVG", { desc = "Select all" })
map(i, "<D-a>", "<Esc>ggVG", { desc = "Select all" })

-- Navigation (Search Everywhere family) ---------------------------------------

map(niv, "<D-O>", LazyVim.pick("files"), { desc = "Go to file" })
map(niv, "<D-e>", function()
  Snacks.picker.recent({ filter = { cwd = true } })
end, { desc = "Recent files" })
map(niv, "<D-o>", function()
  Snacks.picker.lsp_workspace_symbols({
    filter = { default = { "Class", "Struct", "Interface", "Enum", "Module", "Namespace", "TypeParameter" } },
  })
end, { desc = "Go to class" })
map(niv, "<M-D-o>", function()
  Snacks.picker.lsp_workspace_symbols({ filter = LazyVim.config.kind_filter })
end, { desc = "Go to symbol" })
map(niv, "<D-A>", function()
  Snacks.picker.commands()
end, { desc = "Find action" })
map(niv, "<D-F12>", function()
  Snacks.picker.lsp_symbols({ filter = LazyVim.config.kind_filter })
end, { desc = "File structure" })
map(n, "<M-7>", "<Cmd>Outline<CR>", { desc = "Structure panel" })
map(n, "<M-1>", function()
  Snacks.explorer({ cwd = LazyVim.root() })
end, { desc = "Project tree" })
map({ n, "t" }, "<M-F12>", function()
  Snacks.terminal(nil, { cwd = LazyVim.root() })
end, { desc = "Terminal" })
map(n, "<M-9>", function()
  Snacks.lazygit({ cwd = LazyVim.root.git() })
end, { desc = "Git (lazygit)" })

map(n, "<D-[>", "<C-o>", { desc = "Back" })
map(n, "<D-]>", "<C-i>", { desc = "Forward" })
map(n, "<M-D-Left>", "<C-o>", { desc = "Back" })
map(n, "<M-D-Right>", "<C-i>", { desc = "Forward" })

-- Find / replace ----------------------------------------------------------------

map(n, "<D-f>", "/", { desc = "Find in file" })
map(i, "<D-f>", "<Esc>/", { desc = "Find in file" })
map(n, "<D-F>", LazyVim.pick("live_grep"), { desc = "Find in path" })
map(v, "<D-F>", LazyVim.pick("grep_word"), { desc = "Find selection in path" })
map(niv, "<D-r>", function()
  require("grug-far").open({ prefills = { paths = vim.fn.expand("%") } })
end, { desc = "Replace in file" })
map(niv, "<D-R>", function()
  require("grug-far").open({ transient = true })
end, { desc = "Replace in path" })

-- Code intelligence -------------------------------------------------------------

map(niv, "<D-b>", function()
  Snacks.picker.lsp_definitions()
end, { desc = "Go to declaration" })
map(niv, "<M-D-b>", function()
  Snacks.picker.lsp_implementations()
end, { desc = "Go to implementation" })
map(niv, "<D-B>", function()
  Snacks.picker.lsp_type_definitions()
end, { desc = "Go to type declaration" })
map(niv, "<M-F7>", function()
  Snacks.picker.lsp_references()
end, { desc = "Find usages" })
map(n, "<S-F6>", function()
  return ":IncRename " .. vim.fn.expand("<cword>")
end, { expr = true, desc = "Rename" })
map(i, "<S-F6>", function()
  return "<Esc>:IncRename " .. vim.fn.expand("<cword>")
end, { expr = true, desc = "Rename" })
map(niv, "<M-CR>", vim.lsp.buf.code_action, { desc = "Show intention actions" })
map(niv, "<M-D-l>", function()
  LazyVim.format({ force = true })
end, { desc = "Reformat code" })
map(ni, "<M-C-O>", LazyVim.lsp.action["source.organizeImports"], { desc = "Optimize imports" })
map(ni, "<F1>", vim.lsp.buf.hover, { desc = "Quick documentation" })
map(n, "<D-p>", vim.lsp.buf.signature_help, { desc = "Parameter info" })

local function diag(count, severity)
  return function()
    vim.diagnostic.jump({ count = count, float = true, severity = severity })
  end
end
map(ni, "<F2>", diag(1), { desc = "Next problem" })
map(ni, "<S-F2>", diag(-1), { desc = "Previous problem" })

-- Editing -----------------------------------------------------------------------

map(n, "<D-/>", "gcc", { remap = true, desc = "Comment line" })
map(v, "<D-/>", "gc", { remap = true, desc = "Comment selection" })
map(i, "<D-/>", "<C-o>gcc", { remap = true, desc = "Comment line" })

map(n, "<D-d>", "<Cmd>t.<CR>", { desc = "Duplicate line" })
map(i, "<D-d>", "<Cmd>t.<CR>", { desc = "Duplicate line" })
map(v, "<D-d>", ":t'><CR>gv", { silent = true, desc = "Duplicate selection" })

map(n, "<D-BS>", '"_dd', { desc = "Delete line" })
map(i, "<D-BS>", "<Cmd>delete _<CR>", { desc = "Delete line" })
map(v, "<D-BS>", '"_d', { desc = "Delete selection" })

local function move(dir)
  local down = dir > 0
  return {
    [n] = down and "<Cmd>execute 'move .+' . v:count1<CR>==" or "<Cmd>execute 'move .-' . (v:count1 + 1)<CR>==",
    [i] = down and "<Esc><Cmd>m .+1<CR>==gi" or "<Esc><Cmd>m .-2<CR>==gi",
    [v] = down and ":<C-u>execute \"'<,'>move '>+\" . v:count1<CR>gv=gv"
      or ":<C-u>execute \"'<,'>move '<-\" . (v:count1 + 1)<CR>gv=gv",
  }
end
for _, lhs in ipairs({ "<M-S-Up>", "<S-D-Up>" }) do
  for mode, rhs in pairs(move(-1)) do
    map(mode, lhs, rhs, { silent = true, desc = "Move line up" })
  end
end
for _, lhs in ipairs({ "<M-S-Down>", "<S-D-Down>" }) do
  for mode, rhs in pairs(move(1)) do
    map(mode, lhs, rhs, { silent = true, desc = "Move line down" })
  end
end

-- Extend / shrink selection (treesitter / LSP nodes, built into Neovim 0.12)
map(n, "<M-Up>", "van", { remap = true, desc = "Extend selection" })
map(v, "<M-Up>", "an", { remap = true, desc = "Extend selection" })
map(v, "<M-Down>", "in", { remap = true, desc = "Shrink selection" })

-- Debugger ------------------------------------------------------------------------

local function dap(fn)
  return function()
    require("dap")[fn]()
  end
end
map(n, "<F5>", dap("continue"), { desc = "Debug: start / resume" })
map(n, "<F9>", dap("continue"), { desc = "Debug: resume" })
map(n, "<F8>", dap("step_over"), { desc = "Debug: step over" })
map(n, "<F7>", dap("step_into"), { desc = "Debug: step into" })
map(n, "<S-F8>", dap("step_out"), { desc = "Debug: step out" })
map(n, "<D-F8>", dap("toggle_breakpoint"), { desc = "Debug: toggle breakpoint" })
