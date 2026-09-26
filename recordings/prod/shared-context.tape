# The video under "Shared context" on the workflows guide. shared-context.before clones neovim.
# This makes a workflow whose context says how Lua changes are checked, spawns an agent in it
# and asks, which runs real Claude sessions.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /Welcome, komrad!/
Sleep 1s
Type "w"
Type "neovim"
Enter
Type "Run make lint before committing any Lua change."
Sleep 500ms
Ctrl+S
Sleep 2s
Ctrl+Q
Sleep 500ms
Type "c"
Type "I changed runtime/lua/vim/lsp.lua. What should I run before committing?"
Enter
Type "before-commit"
Sleep 500ms
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
