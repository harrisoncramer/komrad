# The video under "Children" on the agents guide: ask the orchestrator to split some reading
# into three agents and watch them appear under the workflow. agent-children.before makes the
# workflow out of shot. The orchestrator and its three agents run real Claude sessions, which
# the recording leaves working.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
Type "Spawn three agents that only read: one to find where :checkhealth is implemented, one to explain how runtime/lua/vim/lsp is organized, and one to list the Lua modules under runtime/lua/vim. Do not wait for them to finish."
Enter
Sleep 15s
Sleep 15s
Sleep 15s
Sleep 15s
