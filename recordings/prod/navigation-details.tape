# The video under "The details" on the navigation guide: move the keyboard into an agent's
# details with the pane picker, move down to its directory, and copy it with y.
# navigation-details.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Ctrl+Space
Type "j"
Sleep 1s
Type "f"
Type "j"
Type "j"
Type "j"
Type "y"
