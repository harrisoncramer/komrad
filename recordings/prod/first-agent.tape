# The video on the agents guide: spawn a first agent straight from the workflow's row with a
# prompt and a name, and open it. first-agent.before makes the workflow out of shot. The agent
# answering runs a real Claude session.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Type "c"
Type "Where is :checkhealth implemented?"
Enter
Type "find-checkhealth"
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
