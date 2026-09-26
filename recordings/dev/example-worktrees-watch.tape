# The worktrees screen drawn from the watch. Before recording, create an empty directory
# named zz-rec-probe under ~/.local/state/komrad-test/worktrees a few seconds in. The size
# loop records it within thirty seconds and the row arrives without the screen asking. The
# delete then takes it off the screen as soon as it is queued.
Source recordings/panel.tape
Type "t"
Wait+Screen@30s /Worktrees/
Sleep 2s
Sleep 20s
Wait+Screen@30s /zz-rec-probe/
Type "/"
Type "zz-rec-probe"
Enter
Type "d"
Wait+Screen@30s /Delete/
Type "y"
Sleep 2s
Escape
