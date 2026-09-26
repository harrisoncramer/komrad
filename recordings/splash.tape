# The splash banner on the docs site's home page (docs/public/splash), recorded from the
# throwaway instance so a visitor sees komrad busy rather than an empty list.
#
# To remake it, seed komrad-test with six workflows over small git repos under
# /private/tmp/komrad-demo, about ten haiku agents across them with one reviewer child, marks p
# (payout-reconciliation) and c (cost-review), and a piece of mail, then run
#   MISE_ENV=test mise run record recordings/splash.tape
# then take the frame after the follow-up lands as the still, and scale the webm and mp4 to
# 1920 wide. It is wider and taller than the other recordings, so it sets its own size.
#
# The tour widens the sidebar so names are not cut off, sends a marked agent a follow-up so it
# is working on camera, jumps to another by its mark and opens its details, archives an agent,
# then searches every transcript and jumps to what the search found.
Set Width 2400
Set Height 1160
Source recordings/panel.tape
Escape
Escape
Escape
Ctrl+G
Sleep 300ms
Escape
Type "}"
Type "}"
Type "}"
Type "}"
Type "}"
Type "}"
Sleep 700ms
Type "'"
Type "p"
Enter
Sleep 300ms
Type "Now add a rollback step for payouts that fail halfway, and list what it must undo."
Enter
Sleep 1500ms
Ctrl+Q
Type "'"
Type "c"
Sleep 700ms
Ctrl+Q
Sleep 900ms
Ctrl+A
Type "g"
Type "j"
Sleep 300ms
Type "z"
Sleep 400ms
Ctrl+S
Sleep 900ms
Type "s"
Type "refund"
Sleep 1000ms
Enter
Sleep 1500ms
