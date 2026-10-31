-- Languages LazyVim has no extra for: HTML, CSS, Emmet, shell.
-- Go, Python, PHP, JS/TS, C/C++ and friends are LazyVim extras, imported
-- in config/lazy.lua for the languages switched on in settings.json.
local langs = require("config.ide").settings.languages

return {
  {
    "nvim-treesitter/nvim-treesitter",
    opts = {
      ensure_installed = {
        "bash",
        "c",
        "cpp",
        "css",
        "scss",
        "html",
        "javascript",
        "typescript",
        "tsx",
        "php",
        "phpdoc",
        "python",
        "go",
        "sql",
        "make",
        "dockerfile",
      },
    },
  },

  {
    "neovim/nvim-lspconfig",
    opts = {
      servers = {
        html = { enabled = langs.web, filetypes = { "html", "templ", "blade" } },
        cssls = { enabled = langs.web },
        emmet_language_server = {
          enabled = langs.web,
          filetypes = { "html", "css", "scss", "javascriptreact", "typescriptreact", "php", "blade", "vue" },
        },
        bashls = { enabled = langs.shell },
      },
    },
  },

  {
    "mason-org/mason.nvim",
    opts = { ensure_installed = langs.shell and { "shfmt", "shellcheck" } or {} },
  },

  {
    "stevearc/conform.nvim",
    opts = {
      formatters_by_ft = {
        sh = { "shfmt" },
        zsh = { "shfmt" },
      },
    },
  },

  -- Markdown: no line-length warnings (MD013); see .markdownlint.yaml
  {
    "mfussenegger/nvim-lint",
    opts = {
      linters = {
        ["markdownlint-cli2"] = {
          prepend_args = { "--config", vim.fn.stdpath("config") .. "/.markdownlint.yaml" },
        },
      },
    },
  },
}
