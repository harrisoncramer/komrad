# The still at the top of the triage guide: the triage zone after the neovim workflow's tasks
# finish and one stops to ask a question. The last frame is the still. triage-zone.before starts
# the five tasks, which run real Claude sessions. The cursor waits on the workflow row, since the
# zone holds still while the cursor is in it.
# It records taller than the rest, so the zone and the whole tree fit in the list at once.
Set Height 1200
Source recordings/panel.tape
Hide
Type "G"
Type "kkkkk"
Show
Wait+Screen@3m /Triage[^\n]*\n[^\n]*lua-style[\s\S]*more waiting for a slot/
Sleep 3s
