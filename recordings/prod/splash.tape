# The still on the docs site's home page, which is this recording's last frame. splash.before
# makes three workflows on neovim with two haiku agents each, and this waits out of shot for
# them to get going, then moves around the list, opens an agent, and searches. The agents run
# real Claude sessions.
Set Width 1920
Set Height 930
Source recordings/panel.tape
Hide
Sleep 20s
Show
Sleep 2s
Type "j"
Type "j"
Type "j"
Sleep 1.5s
Enter
Sleep 4s
Ctrl+Q
Sleep 1.5s
Type "j"
Type "j"
Enter
Sleep 4s
Ctrl+Q
Sleep 1.5s
Type "O"
Type "q"
Type "lsp"
Sleep 3s
Escape
Sleep 1.5s
Type "k"
Type "k"
Sleep 3s
