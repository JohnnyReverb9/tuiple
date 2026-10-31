return {
  {
    "LazyVim/LazyVim",
    opts = {
      colorscheme = require("config.ide").settings.colorscheme,
    },
  },

  -- Project tree on the left, like the IDE's Project tool window
  {
    "folke/snacks.nvim",
    opts = {
      explorer = { replace_netrw = true },
      picker = {
        sources = {
          explorer = { hidden = true },
          files = { hidden = true },
          grep = { hidden = true },
        },
      },
      terminal = { win = { position = "bottom", height = 0.3 } },
    },
  },

  -- Tabs show all open files, with diagnostics
  {
    "akinsho/bufferline.nvim",
    opts = {
      options = {
        always_show_bufferline = true,
        diagnostics = "nvim_lsp",
        separator_style = "slant",
      },
    },
  },

  -- Sticky scroll of the enclosing function / class
  {
    "nvim-treesitter/nvim-treesitter-context",
    opts = { max_lines = 3 },
  },
}
