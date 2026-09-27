# The still of an agent's details on the agents guide: an agent that commits a change, so its
# details show a branch and commits. agent-details.before makes the workflow out of shot. The
# agent's work runs a real Claude session.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Type "c"
Type "Add a sentence to the end of CONTRIBUTING.md reminding contributors to run make lint, then commit it. Do not ask for a review."
Enter
Type "lint-reminder"
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
Ctrl+A
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
