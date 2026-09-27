# The video under "Creating one" on the workflows guide: clone neovim, open komrad in it, and
# make a workflow, which starts its orchestrator. first-workflow.before marks komrad's folders
# trusted, so Claude does not ask. The orchestrator's first look runs a real Claude session.
Source recordings/shell.tape
Type "git clone --depth 1 https://github.com/neovim/neovim ~/neovim"
Enter
Wait+Line@3m /\$ ?$/
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /Welcome, komrad!/
Sleep 1s
Type "w"
Type "neovim"
Ctrl+S
Wait+Screen@4m /done \d+:\d+ [AP]M/
