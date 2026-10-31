-- Light themes (tokyonight-day, catppuccin-latte) draw the current line,
-- inline code and the other uses of the word under the cursor on fairly
-- dark backgrounds, under which blue text gets hard to read. Repaint those
-- as a light tint of the text colour over the background.
local function blend(bg, fg, alpha)
  local function ch(c, shift)
    return math.floor(c / 2 ^ shift) % 256
  end
  local out = 0
  for _, shift in ipairs({ 16, 8, 0 }) do
    local v = ch(bg, shift) * (1 - alpha) + ch(fg, shift) * alpha
    out = out + math.floor(v + 0.5) * 2 ^ shift
  end
  return string.format("#%06x", out)
end

vim.api.nvim_create_autocmd("ColorScheme", {
  group = vim.api.nvim_create_augroup("ide_light_tints", { clear = true }),
  callback = function()
    if vim.o.background ~= "light" then
      return
    end
    local normal = vim.api.nvim_get_hl(0, { name = "Normal", link = false })
    if not (normal.bg and normal.fg) then
      return
    end
    local tint = function(alpha)
      return blend(normal.bg, normal.fg, alpha)
    end
    local set = function(name, val)
      local cur = vim.api.nvim_get_hl(0, { name = name, link = false })
      vim.api.nvim_set_hl(0, name, vim.tbl_extend("force", cur, val))
    end
    set("CursorLine", { bg = tint(0.05) })
    set("ColorColumn", { bg = tint(0.06) })
    for _, name in ipairs({ "RenderMarkdownCodeInline", "@markup.raw.markdown_inline" }) do
      set(name, { bg = tint(0.08) })
    end
    for _, name in ipairs({
      "IlluminatedWordText",
      "IlluminatedWordRead",
      "IlluminatedWordWrite",
      "LspReferenceText",
      "LspReferenceRead",
      "LspReferenceWrite",
    }) do
      set(name, { bg = tint(0.10) })
    end
  end,
})

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
