# The review screen following a worktree that moves: open the greeter's diff, then let the
# agent's checkout change underneath it, and watch cwd sync report the move and the diff catch up.
Source recordings/panel.tape
Type "k"
Type "d"
Wait+Screen@30s /hello, reviewer/
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Sleep 3s
Escape
