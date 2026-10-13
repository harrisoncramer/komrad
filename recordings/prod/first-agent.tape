# The video on the agents guide: start a first task straight from the workflow's row with a
# brief, and watch its writer answer. first-agent.before makes the workflow out of shot. The
# agent answering runs a real Claude session.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Space
Type "c"
Type "Where is :checkhealth implemented?"
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
