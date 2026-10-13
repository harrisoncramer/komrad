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
Type "G"
Type "k"
Type "k"
Type "k"
Type "k"
Sleep 1.5s
Enter
Sleep 4s
Ctrl+Space
Ctrl+Space
Sleep 1.5s
Type "k"
Type "k"
Type "k"
Enter
Sleep 4s
Ctrl+Space
Ctrl+Space
Sleep 1.5s
Type "/"
Type "lsp"
Enter
Sleep 1.5s
Type "j"
Sleep 3s
