# The video under "Basic navigation" on the navigation guide: move a row and a chunk at a time,
# search and step through the matches, filter the list down, then sort and flatten it.
# navigation-moving.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Type "j"
Type "j"
Ctrl+D
Ctrl+U
Type "G"
Type "gg"
Type "/"
Type "lsp"
Enter
Type "n"
Type "n"
Type "N"
Escape
Type "/"
Tab
Type "docs"
Enter
Sleep 1.5s
Escape
Type "f"
Tab
Tab
Type "j"
Tab
Type "j"
Sleep 1.5s
Escape
