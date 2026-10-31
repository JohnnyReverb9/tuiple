-- User settings for this config, kept outside it so the config can stay a
-- symlink into a repository. tuiple's settings screen (the Neovim tab)
-- edits the same file; anything missing falls back to the defaults below.
--
--   ~/.config/nvim-ide/settings.json   (override with $NVIM_IDE_SETTINGS)
--
-- Changes apply on the next start of Neovim (or :restart).

local M = {}

M.defaults = {
  -- Look
  colorscheme = "tokyonight-day",
  line_numbers = "absolute", -- "absolute" | "relative" | "off"
  insert_cursor = "block", -- "block" | "bar"
  cursorline = true,
  margin = 0, -- column of the right-margin line; 0 = none

  -- Editing
  tab_width = 4,
  expand_tab = false, -- false: real tabs
  wrap = false,
  autosave = true, -- write buffers when switching away, like the IDE
  clipboard = true, -- yank / paste through the macOS clipboard
  format_on_save = true,

  -- Completion
  enter_accepts = "after_arrows", -- "after_arrows" | "always" | "never"
  ghost_text = true,
  auto_docs = true,
  signature_help = true,

  -- Languages (LazyVim extras, plus a few servers it has no extra for)
  languages = {
    go = true,
    python = true,
    php = true,
    typescript = true, -- JavaScript / TypeScript
    web = true, -- HTML, CSS, Emmet
    tailwind = true,
    c = true, -- C / C++ (clangd), CMake
    shell = true,
    sql = true,
    docker = true,
    json = true,
    yaml = true,
    toml = true,
    markdown = true,
    git = true,
  },
  php_lsp = "intelephense", -- "intelephense" | "phpactor"
  python_lsp = "basedpyright", -- "basedpyright" | "pyright"
}

M.path = vim.env.NVIM_IDE_SETTINGS or vim.fn.expand("~/.config/nvim-ide/settings.json")

local function load()
  local settings = vim.deepcopy(M.defaults)
  local f = io.open(M.path, "r")
  if not f then
    return settings
  end
  local raw = f:read("*a")
  f:close()
  local ok, parsed = pcall(vim.json.decode, raw, { luanil = { object = true, array = true } })
  if not ok or type(parsed) ~= "table" then
    vim.schedule(function()
      vim.notify("nvim-ide: cannot read " .. M.path .. ", using defaults", vim.log.levels.WARN)
    end)
    return settings
  end
  return vim.tbl_deep_extend("force", settings, parsed)
end

M.settings = load()

-- LazyVim extras each language turns on.
M.language_extras = {
  go = { "lang.go" },
  python = { "lang.python" },
  php = { "lang.php" },
  typescript = { "lang.typescript", "linting.eslint" },
  tailwind = { "lang.tailwind" },
  c = { "lang.clangd", "lang.cmake" },
  sql = { "lang.sql" },
  docker = { "lang.docker" },
  json = { "lang.json" },
  yaml = { "lang.yaml" },
  toml = { "lang.toml" },
  markdown = { "lang.markdown" },
  git = { "lang.git" },
}

-- Lazy specs importing the extras of the enabled languages.
function M.extra_specs()
  local specs = {}
  local names = vim.tbl_keys(M.language_extras)
  table.sort(names)
  for _, lang in ipairs(names) do
    if M.settings.languages[lang] then
      for _, extra in ipairs(M.language_extras[lang]) do
        table.insert(specs, { import = "lazyvim.plugins.extras." .. extra })
      end
    end
  end
  return specs
end

return M
