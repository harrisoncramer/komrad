# The video under "Visual selection" on the navigation guide: take one agent into the selection
# with v, extend it over two more with V, and archive all three with z.
# navigation-selecting.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Type "v"
Type "j"
Type "j"
Type "V"
Type "z"
Wait+Screen@10s /(?i)archive/
Sleep 1.5s
Enter
