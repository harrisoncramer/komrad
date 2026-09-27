# The video on the hooks guide: write a pre-launch hook in the shell, hang it on the workflow,
# and spawn an agent to see it ran. The hook goes in the throwaway instance's own hooks
# directory, which the slot names in its config, so the picker offers nothing else.
# hook-setup.before makes the workflow out of shot, and hook-setup.after removes the hook. The
# agent answering runs a real Claude session.
Source recordings/shell.tape
Hide
Type `HOOKS="$(dirname "$KOMRAD_CONFIG")/hooks"; clear`
Enter
Show
Type "cd ~/neovim"
Enter
Type `mkdir -p "$HOOKS"`
Enter
Type `cat > "$HOOKS/ignore-revs" <<'EOF'`
Enter
Type "#!/usr/bin/env bash"
Enter
Type `#KOMRAD description="make git blame skip neovim's reformatting commits"`
Enter
Type "git config blame.ignoreRevsFile .git-blame-ignore-revs"
Enter
Type "EOF"
Enter
Type `chmod +x "$HOOKS/ignore-revs"`
Enter
Type "komrad open"
Enter
Wait+Screen@30s /no shared context set/
Sleep 1s
Type "e"
Enter
Enter
Enter
Enter
Enter
Enter
Enter
Enter
Enter
Enter
Enter
Wait+Screen@30s /ignore-revs/
Down
Sleep 1s
Ctrl+S
Sleep 2s
Type "c"
Type "Does git blame here skip the commits in .git-blame-ignore-revs? Answer in one line."
Enter
Type "check-blame"
Sleep 500ms
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
