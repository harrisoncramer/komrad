# The video under "Visual selection" on the navigation guide: take one agent into the selection
# with v, extend it over two more with V, and archive all three with z.
# navigation-selecting.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Sleep 500ms
Type "v"
Sleep 700ms
Type "j"
Sleep 400ms
Type "j"
Sleep 400ms
Type "V"
Sleep 1s
Type "z"
Wait+Screen@10s /(?i)archive/
Sleep 1.5s
Enter
Sleep 2s
