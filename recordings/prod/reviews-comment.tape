# The video under "Leaving comments" on the reviews guide: a comment written on a test, held
# as a draft, then sent with a word on the work as a whole, and the agent's answer arriving
# in the thread. The wait for the answer is out of shot. review-history makes the agent and
# its commits, and the agent answering is a real Claude session.
Source recordings/panel.tape
Wait+Screen@30s /text-tru/
Type "j"
Type "d"
Wait+Screen@10s /Review · text-truncate/
Type "/"
Type "leaves a short"
Enter
Type "c"
Type "Check the marker too: a string exactly width long should come back untouched."
Ctrl+S
Sleep 2s
Ctrl+S
Wait+Screen@10s /about the work as/
Type "Close. One more edge case."
Ctrl+S
Wait+Screen@10s /1 comment sent/
Sleep 2s
Hide
Wait+Screen@5m /text-truncate \d+:\d+[AP]M/
Show
Sleep 4s
