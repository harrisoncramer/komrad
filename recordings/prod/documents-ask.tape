# The video under "Asking an agent for one" on the documents guide: an agent launched to write
# an RFC rather than code, then its documents opened once it has. The wait for it to finish is
# out of shot. documents-ask.before makes the workflow, and the agent writing is a real Claude
# session.
Source recordings/panel.tape
Type "c"
Type "Write an RFC for adding vim.text.truncate(), which shortens a string to a width in display columns. Save it as a komrad document named truncate-rfc, and change no code."
Enter
Type "truncate-rfc"
Ctrl+S
Wait+Screen@10s /truncate-rfc/
Sleep 2s
Hide
Wait+Screen@4m /done \d+:\d+ [AP]M/
Show
Sleep 2s
Type "D"
Wait+Screen@10s /Documents  truncate-rfc/
Sleep 4s
