# The video under "Visual selection" on the navigation guide: take two tasks into the selection
# with v and V, add a third with v, and archive all three from the menu.
# navigation-selecting.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "G"
Type "v"
Type "k"
Type "V"
Type "k"
Type "k"
Type "v"
Space
Wait+Screen@10s /archive ›/
Type "a"
Wait+Screen@10s /archive all/
Sleep 1.5s
Type "a"
