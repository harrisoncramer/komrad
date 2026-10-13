# The video under "Task schedules" on the schedules guide: a schedule that starts a task in
# the neovim workflow every minute, then the writer it launched. The wait for the
# first firing is out of shot. schedules-implementer.before makes the workflow.
Source recordings/panel.tape
Tab
Space
Type "c"
Wait+Screen@10s /start a new task/
Tab
Type "news-digest"
Tab
Type "* * * * *"
Tab
Tab
Type "Summarize the newest entries in runtime/doc/news.txt in three bullets."
Tab
Enter
Wait+Screen@10s /news-digest/
Sleep 2s
Shift+Tab
Hide
Wait+Screen@3m /news-digest/
Show
Type "j"
Wait+Screen@4m /done \d+:\d+ [AP]M/
