-- Completion that behaves like PhpStorm:
--   * the first item is preselected and previewed inline as ghost text
--   * Tab accepts it; with no menu open Tab jumps through snippet fields
--   * Enter accepts only after you picked an item with the arrows,
--     so a plain Enter at the end of a word is still just a new line
--   * Ctrl+Space / Cmd+J open the list (Cmd+J: snippets only, "live templates")

-- Set once the user moves through the list; cleared whenever the menu closes.
local navigated = false

vim.api.nvim_create_autocmd("User", {
  pattern = { "BlinkCmpMenuOpen", "BlinkCmpMenuClose" },
  callback = function()
    navigated = false
  end,
})

local function select(dir)
  return function(cmp)
    if cmp.is_menu_visible() then
      navigated = true
      return dir > 0 and cmp.select_next() or cmp.select_prev()
    end
  end
end

return {
  {
    "saghen/blink.cmp",
    opts = function(_, opts)
      opts.keymap = {
        preset = "none",
        ["<Tab>"] = {
          "select_and_accept",
          LazyVim.cmp.map({ "snippet_forward", "ai_nes", "ai_accept" }),
          "fallback",
        },
        ["<S-Tab>"] = { "snippet_backward", "fallback" },
        ["<CR>"] = {
          function(cmp)
            if navigated and cmp.get_selected_item() then
              return cmp.accept()
            end
          end,
          "fallback",
        },
        ["<Down>"] = { select(1), "fallback" },
        ["<Up>"] = { select(-1), "fallback" },
        ["<C-n>"] = { select(1), "show" },
        ["<C-p>"] = { select(-1), "show" },
        ["<C-Space>"] = { "show", "show_documentation", "hide_documentation" },
        ["<D-j>"] = {
          function(cmp)
            return cmp.show({ providers = { "snippets" } })
          end,
        },
        -- one Esc both closes the list and leaves insert mode
        ["<Esc>"] = {
          function(cmp)
            cmp.hide()
          end,
          "fallback",
        },
        ["<C-e>"] = { "cancel", "fallback" },
        ["<C-b>"] = { "scroll_documentation_up", "fallback" },
        ["<C-f>"] = { "scroll_documentation_down", "fallback" },
        ["<D-p>"] = { "show_signature", "hide_signature", "fallback" },
        ["<C-k>"] = { "show_signature", "hide_signature", "fallback" },
      }

      opts.completion = vim.tbl_deep_extend("force", opts.completion or {}, {
        list = { selection = { preselect = true, auto_insert = false } },
        menu = {
          border = "rounded",
          scrollbar = false,
          draw = {
            treesitter = { "lsp" },
            columns = {
              { "kind_icon" },
              { "label", "label_description", gap = 1 },
            },
          },
        },
        documentation = {
          auto_show = true,
          auto_show_delay_ms = 150,
          window = { border = "rounded" },
        },
        ghost_text = { enabled = true },
        accept = { auto_brackets = { enabled = true } },
      })

      opts.signature = { enabled = true, window = { border = "rounded" } }

      -- Words from open buffers are a fallback, not the main act.
      opts.sources.providers = vim.tbl_deep_extend("force", opts.sources.providers or {}, {
        lsp = { score_offset = 10 },
        path = { score_offset = 5 },
        buffer = { min_keyword_length = 3, max_items = 5, score_offset = -5 },
        snippets = { score_offset = 0 },
      })
    end,
  },
}
