-- Run by install.sh inside `nvim --headless` after plugins are synced:
-- installs every Mason package and treesitter parser the config asks for and
-- waits until they are done, so the first real start is ready to work.

local function log(msg)
  io.stdout:write(msg .. "\n")
end

require("lazy").load({ plugins = { "mason.nvim", "mason-lspconfig.nvim", "nvim-lspconfig", "nvim-treesitter" } })

-- Mason: tools + language servers ------------------------------------------------

local registry = require("mason-registry")
local refreshed = false
registry.refresh(function()
  refreshed = true
end)
vim.wait(120000, function()
  return refreshed
end, 200)

local wanted = {}
for _, name in ipairs(LazyVim.opts("mason.nvim").ensure_installed or {}) do
  wanted[name] = true
end

local to_package = require("mason-lspconfig").get_mappings().lspconfig_to_package
for server, conf in pairs(LazyVim.opts("nvim-lspconfig").servers or {}) do
  if type(conf) == "table" and conf.enabled ~= false and conf.mason ~= false and to_package[server] then
    wanted[to_package[server]] = true
  end
end

local pending, failed = {}, {}
for name in pairs(wanted) do
  local ok, pkg = pcall(registry.get_package, name)
  if not ok then
    table.insert(failed, name .. " (not in registry)")
  elseif not pkg:is_installed() then
    log("mason: installing " .. name)
    pending[name] = pkg
    if not pkg:is_installing() then
      pkg:install()
    end
  end
end

vim.wait(1800000, function()
  for name, pkg in pairs(pending) do
    if not pkg:is_installing() then
      if not pkg:is_installed() then
        table.insert(failed, name)
      end
      pending[name] = nil
    end
  end
  return next(pending) == nil
end, 1000)

-- Treesitter parsers ---------------------------------------------------------------

local parsers = LazyVim.opts("nvim-treesitter").ensure_installed or {}
log("treesitter: installing " .. #parsers .. " parsers")
local ok, err = pcall(function()
  require("nvim-treesitter").install(parsers):wait(1800000)
end)
if not ok then
  table.insert(failed, "treesitter: " .. tostring(err))
end

if #failed > 0 then
  log("FAILED: " .. table.concat(failed, ", "))
  vim.cmd("cquit 1")
end
log("all language tools installed")
vim.cmd("qall!")
