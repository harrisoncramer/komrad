# The video under "The orchestrator" on the workflows guide. orchestrator.before clones neovim
# and makes its workflow out of shot. This opens the orchestrator and asks it about the
# repository, which runs a real Claude session.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
Type "What is this repository, and how would you help me work on it? Answer in two short sentences."
Enter
Wait+Screen@4m /work on it\?[\s\S]*done \d+:\d+ [AP]M/
