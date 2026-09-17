-- Rewrite relative links between the Markdown docs so the rendered PDFs
-- link to each other: GUIDE.md#uninstall -> GUIDE.pdf#uninstall.
function Link(el)
  local target = el.target
  if target:match("^https?://") or target:match("^mailto:") then
    return nil
  end
  local path, anchor = target:match("^([^#]*)(#?.*)$")
  if path ~= "" and path:match("%.md$") then
    el.target = path:gsub("%.md$", ".pdf") .. anchor
    return el
  end
  return nil
end
