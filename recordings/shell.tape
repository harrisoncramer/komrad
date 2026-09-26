# A clean shell in the home directory, set up out of shot: a short prompt, no history written,
# and no history expansion, so a typed #! line is left alone.
Hide
Type "unset HISTFILE; set +H; export PS1='\[\e[34m\]\w\[\e[0m\] $ '; cd ~; clear"
Enter
Sleep 500ms
Show
