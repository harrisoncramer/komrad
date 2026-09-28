# The video under "Orchestrator schedules" on the schedules guide: a schedule that mails the
# neovim workflow's orchestrator every minute, then the orchestrator reading the mail and
# answering it. The wait for the first firing is out of shot. schedules-orchestrator.before
# makes the workflow, two agents in it, and starts its orchestrator.
Source recordings/panel.tape
Hide
Type "k"
Type "k"
Wait+Screen@4m /done \d+:\d+ [AP]M/
Show
Type "o"
Type "s"
Type "c"
Wait+Screen@10s /mail one already running/
Down
Wait+Screen@10s /orchestrator  neovim/
Tab
Type "standup"
Enter
Type "* * * * *"
Enter
Enter
Type "List the agents in your workflow and say in one sentence what each is doing."
Tab
Ctrl+S
Wait+Screen@10s /standup/
Sleep 2s
Type "o"
Type "a"
Type "k"
Type "k"
Hide
Wait+Screen@3m /unread mail/
Show
Wait+Screen@4m /unread mail[\s\S]*done \d+:\d+ [AP]M/
