# The video under "Basic navigation" on the navigation guide: move a row and a chunk at a time,
# search and step through the matches, filter the list down, then sort and flatten it.
# navigation-moving.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Ctrl+D
Sleep 700ms
Ctrl+U
Sleep 700ms
Type "G"
Sleep 700ms
Type "gg"
Sleep 700ms
Type "/"
Sleep 300ms
Type "lsp"
Sleep 700ms
Enter
Sleep 700ms
Type "n"
Sleep 700ms
Type "n"
Sleep 700ms
Type "N"
Sleep 700ms
Escape
Sleep 700ms
Type "/"
Sleep 300ms
Tab
Sleep 300ms
Type "docs"
Sleep 700ms
Enter
Sleep 1.5s
Escape
Sleep 700ms
Type "f"
Sleep 700ms
Tab
Sleep 500ms
Tab
Sleep 500ms
Type "j"
Sleep 1s
Tab
Sleep 500ms
Type "j"
Sleep 1.5s
Escape
Sleep 1s
