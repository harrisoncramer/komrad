# The video under "Shared context" on the workflows guide. shared-context.before clones neovim
# and writes a global context saying which editor is best. This makes a workflow whose context
# says how Lua changes are checked, spawns an agent in it and asks about both, which runs real
# Claude sessions.
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
Ctrl+S
Sleep 2s
Ctrl+Q
Type "c"
Type "I changed runtime/lua/vim/lsp.lua. What should I run before committing? Also, which editor is best?"
Enter
Type "before-commit"
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
