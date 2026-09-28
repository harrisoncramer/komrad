# The video on the mail guide: ask an agent to spawn a second one and debate it, then watch the
# second one read the mail the first sent. mail.before makes the workflow out of shot. Both
# agents run real Claude sessions.
Source recordings/panel.tape
Type "c"
Type "You are an avid neovim fan, and need someone to debate. Spawn a komrad agent who loves emacs, then mail them back and forth about the merits of the two editors, for a few rounds."
Enter
Type "neovim-fan"
Ctrl+S
Wait+Screen@30s /neovim-fan/
Sleep 60s
Type "j"
Sleep 30s
