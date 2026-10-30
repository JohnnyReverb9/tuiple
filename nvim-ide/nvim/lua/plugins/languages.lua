-- Languages LazyVim has no extra for: HTML, CSS, Emmet, shell.
-- Go, Python, PHP, JS/TS, C/C++ and friends come from lazyvim.json extras.
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
        html = { filetypes = { "html", "templ", "blade" } },
        cssls = {},
        emmet_language_server = {
          filetypes = { "html", "css", "scss", "javascriptreact", "typescriptreact", "php", "blade", "vue" },
        },
        bashls = {},
      },
    },
  },

  {
    "mason-org/mason.nvim",
    opts = { ensure_installed = { "shfmt", "shellcheck" } },
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
}
