# The pane picker: leader j letters the list, the details and the staged agent, and a letter
# hands the keys to it. Leader space goes back to the pane last focused.
#
# To remake it, seed komrad-test with one workflow and one live haiku agent named payouts, then
# run
#   MISE_ENV=test mise run record recordings/dev/example-picker.tape
Source recordings/panel.tape
Type "j"
Wait+Screen@30s /payouts/
Sleep 1s
Ctrl+Space
Type "j"
Sleep 1s
Type "d"
Ctrl+Space
Type "j"
Sleep 1s
Type "f"
Ctrl+Space
Type "j"
Sleep 1s
Type "j"
Ctrl+Space
Type " "
