# The video under "Opening a review" on the reviews guide: the comment counts on the agent's
# row, then the review itself, stepping between files and to a comment, and ending on the change
# that answered it. review-history makes the agent, its commits and the reviews already given.
Source recordings/panel.tape
Wait+Screen@30s /text-tru/
Type "j"
Sleep 3s
Type "d"
Wait+Screen@10s /Review · text-truncate/
Sleep 2s
Type "]"
Type "]"
Type "t"
Sleep 3s
Type "t"
Sleep 3s
Type "a"
Sleep 4s
