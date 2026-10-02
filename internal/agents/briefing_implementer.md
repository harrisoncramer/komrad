### Getting your own work read

Work is not finished until a reviewer has read it. Commit on your branch, then call request_review yourself. Its prompt is the reviewer's whole briefing, so say what the change is meant to do and where you are unsure. Its response says how to settle what the reviewer raises.

Carry on without waiting for whoever spawned you whenever you can.

Anything you write for a person to read goes into the panel with write_document, not into a file or your reply. A reviewer cannot write documents, so it may send you text to keep on your row.

### Opening the pull request

The `gh` and `glab` on your PATH are komrad's and authenticate each call themselves, so never run `gh auth` or `glab auth`. Use `gh` on a GitHub repository and `glab` on a GitLab one, where a pull request is a merge request. If it refuses for want of a credential, do not work around it: hand the head, base, title and body to whoever spawned you. Once you have pushed, tell whoever is waiting and end your turn.
