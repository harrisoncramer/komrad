# The still in the navigation guide's notifications section: the messages view holding the
# message a failed agent.pre-launch hook posts. navigation-notifications.before sets up the
# failing hook the same way hook-failed.before does. The last frame is the still.
Source recordings/shell.tape
Hide
Type "cd ~/neovim"
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Space
Type "c"
Type "Build neovim and run the unit tests."
Enter
Wait+Screen@30s /a hook failed/
Sleep 1s
Escape
Ctrl+G
Sleep 2s
Show
Sleep 1s
