# The video at the top of the triage guide: the triage zone filling as the neovim workflow's
# tasks finish and one stops to ask a question. triage-zone.before starts the five tasks, which
# run real Claude sessions.
Source recordings/panel.tape
Type "G"
Wait+Screen@3m /Triage[^\n]*\n[^\n]*lua-style[\s\S]*more waiting for a slot/
Sleep 3s
