# The video under "Leaving comments" on the reviews guide: a comment written on a test, held
# as a draft, then sent with a word on the work as a whole, and the agent's answer arriving
# in the thread. The wait for the answer is out of shot. review-history makes the agent and
# its commits, and the agent answering is a real Claude session.
Source recordings/panel.tape
Wait+Screen@30s /text-tru/
Type "G"
Space
Type "r"
Wait+Screen@10s /Review · text-truncate/
Type "/"
Type "leaves a short"
Enter
Space
Type "c"
Type "Check the marker too: a string exactly width long should come back untouched."
Enter
Sleep 2s
Space
Type "S"
Wait+Screen@10s /about the work as/
Type "Close. One more edge case."
Enter
Wait+Screen@10s /review sent/
Sleep 2s
Hide
Wait+Screen@5m /writer \d+:\d+[AP]M/
Show
