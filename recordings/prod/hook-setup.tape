# The video on the hooks guide: write a pre-launch hook in the shell, hang it on the workflow,
# and spawn an agent to see it ran. hook-setup.before makes the workflow out of shot, and
# hook-setup.after removes the hook. The agent answering runs a real Claude session.
Source recordings/shell.tape
Type "cd ~/neovim"
Enter
Type "mkdir -p ~/.config/komrad/hooks"
Enter
Type "cat > ~/.config/komrad/hooks/link-deps <<'EOF'"
Enter
Type "#!/usr/bin/env bash"
Enter
Type `#KOMRAD description="share the main clone's built dependencies"`
Enter
Type "ln -sfn ~/neovim/.deps .deps"
Enter
Type "EOF"
Enter
Type "chmod +x ~/.config/komrad/hooks/link-deps"
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
Wait+Screen@30s /link-deps/
Down
Sleep 1s
Ctrl+S
Sleep 2s
Type "c"
Type "Is .deps in this worktree a link, and to where? Answer in one line."
Enter
Type "check-deps"
Sleep 500ms
Ctrl+S
Sleep 2s
Enter
Wait+Screen@4m /done \d+:\d+ [AP]M/
