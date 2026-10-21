# The video under "Leaving comments" on the reviews guide: a comment written on a test, the
# thread showing it released and then read, and the agent's answer arriving in it. The waits
# for the agent are out of shot. review-history makes the agent and its commits, and the agent
# answering is a real Claude session.
Source recordings/panel.tape
Wait+Screen@30s /text-tru/
Type "G"
Space
Type "r"
Wait+Screen@10s /text-truncate › review/
Type "/"
Type "leaves a short"
Enter
Space
Type "c"
Type "Check the marker too: a string exactly width long should come back untouched."
Enter
Wait+Screen@10s /→ released/
Sleep 2s
Hide
Wait+Screen@3m /✓ read/
Show
Sleep 2s
Hide
Wait+Screen@5m /writer \d+:\d+[AP]M/
Show
Sleep 3s
