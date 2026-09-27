# The video on the schedules guide: an implementer schedule that launches an agent each night,
# then an orchestrator schedule that mails the neovim workflow's orchestrator each weekday.
# schedules.before makes the workflow and starts its orchestrator out of shot.
Source recordings/panel.tape
Type "S"
Wait+Screen@10s /Schedules/
Type "c"
Wait+Screen@10s /launch a new agent/
Tab
Type "nightly-lint"
Enter
Type "0 2 * * *"
Enter
Enter
Type "Run make lint and fix what it reports."
Tab
Ctrl+S
Wait+Screen@10s /nightly-lint/
Type "c"
Wait+Screen@10s /mail one already running/
Down
Wait+Screen@10s /orchestrator  neovim/
Tab
Type "morning-brief"
Enter
Type "0 9 * * 1-5"
Enter
Enter
Type "Summarise what the workflow did overnight."
Tab
Ctrl+S
Wait+Screen@10s /morning-brief/
Type "k"
Wait+Screen@10s /mails +orchestrator/
Sleep 3s
