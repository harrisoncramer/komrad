# The video under "The details" on the navigation guide: move the keyboard into an agent's
# details with ctrl+a pressed twice, move down to its directory, and copy it with y.
# navigation-details.before fills the panel with three workflows and six haiku agents.
Source recordings/panel.tape
Wait+Screen@60s /help-tags/
Type "j"
Sleep 700ms
Ctrl+A
Sleep 700ms
Ctrl+A
Sleep 1s
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 700ms
Type "y"
Sleep 1.5s
