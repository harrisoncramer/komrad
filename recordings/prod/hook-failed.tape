# The still on the hooks guide: an agent whose pre-launch hook failed, with the hook's log open.
# hook-failed.before hangs a build-neovim hook on the workflow that fails in a fresh worktree,
# since nothing has configured the build, and hook-failed.after removes it.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Type "c"
Type "Build neovim and run the unit tests."
Enter
Type "build"
Ctrl+S
Wait+Screen@30s /a hook failed/
Sleep 1s
Escape
Type "j"
Type "l"
Sleep 2s
