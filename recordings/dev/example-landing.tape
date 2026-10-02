# Create a workflow, an agent in it, and a schedule, and see where the cursor goes after each.
Source recordings/panel.tape
Type "w"
Type "landing"
Tab
Tab
Type "/tmp"
Tab
Up
Enter
Sleep 2s
Ctrl+Space
Type "j"
Sleep 1s
Type "d"
Type "c"
Type "reply with the single word ok"
Enter
Sleep 3s
Type "o"
Type "s"
Type "c"
Type "landing-probe"
Tab
Type "0 9 * * 1-5"
Tab
Tab
Type "say ok"
Enter
Sleep 2s
