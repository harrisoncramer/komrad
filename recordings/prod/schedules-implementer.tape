# The video under "Implementer schedules" on the schedules guide: a schedule that launches an
# agent into the neovim workflow every minute, then the agent it launched. The wait for the
# first firing is out of shot. schedules-implementer.before makes the workflow.
Source recordings/panel.tape
Type "o"
Type "s"
Type "c"
Wait+Screen@10s /launch a new agent/
Tab
Type "news-digest"
Enter
Type "* * * * *"
Enter
Enter
Type "Summarize the newest entries in runtime/doc/news.txt in three bullets."
Tab
Ctrl+S
Wait+Screen@10s /news-digest/
Sleep 2s
Type "o"
Type "a"
Hide
Wait+Screen@3m /news-digest/
Show
Type "j"
Wait+Screen@4m /done \d+:\d+ [AP]M/
