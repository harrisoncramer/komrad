# The view cycle: C-a steps forward through the list, the staged agent and its record, and
# C-q steps back, from whichever of them has the keyboard.
#
# To remake it, seed komrad-test with one workflow and one live haiku agent named payouts, then
# run
#   MISE_ENV=test mise run record recordings/dev/example-cycle.tape
Source recordings/panel.tape
Type "j"
Wait+Screen@30s /payouts/
Sleep 1s
Ctrl+A
Ctrl+A
Ctrl+A
Ctrl+Q
Ctrl+Q
Ctrl+Q
