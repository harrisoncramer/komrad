# The video under "Editing a document" on the documents guide: the agent's RFC opened in the
# editor beside the list, a line added to it, and the document drawn again with the edit. The
# panel opens with a plain Neovim as its editor. document-history makes the agent and its RFC.
Source recordings/shell.tape
Hide
Type "export EDITOR='nvim --clean'; komrad open"
Enter
Wait+Screen@30s /agents\s+query\s+schedules/
Sleep 1s
Show
Type "j"
Type "D"
Wait+Screen@10s /RFC: vim\.text/
Sleep 2s
Type "e"
Wait+Screen@10s /komrad-document-/
Type "/wide characters"
Enter
Type "o"
Type "Also a case where the width is narrower than the marker."
Escape
Type ":wq"
Enter
Wait+Screen@10s /narrower than the marker/
Sleep 4s
