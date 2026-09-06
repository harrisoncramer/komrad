# Reviewing what an agent wrote

Weave has its own code review. It looks like a pull request review, but nothing leaves your machine: the diff, the comments, the replies and the verdict all live in the panel, and the agent reads them in its own pane.

This guide walks the whole loop with pictures of what you see at each step.

## Three separate things

The screen holds three things that look like one thing.

The round is a frozen photograph of the agent's worktree. Weave takes it the moment you open the diff: everything in that directory, committed or not, ignored files aside, written as one commit nothing else points at. It builds that commit in an index of its own, so the agent's own index, worktree and branch are left exactly as they were and nothing lands on the branch it is working on. The agent goes on typing; the round does not move. Every comment is pinned to a line of the round, not to a line of the file on disk.

Your draft is the comments you have written but not sent. They are yours alone. The agent cannot see them, cannot be told they exist, and will never read them until you release them. They sit in the panel marked unsent.

The released review is the whole batch let go of at once: one markdown file written to disk, one piece of mail to the agent saying where that file is, and one verdict. That is the only moment anything reaches the agent.

So: open the diff, write as many comments as you like, then send them all together with a verdict. Nothing is sent one comment at a time.

## Opening the diff

From the agents list, put the cursor on an agent and press `d`.

The key only answers for agents that have a checkout of their own. A child agent shares its parent's directory, and an agent you told weave not to provision is sitting in a directory somebody else owns, so neither has a diff that belongs to it. The workflow also has to have worktrees turned on.

Here is the screen you land on. The sidebar on the left is the conversation. The stage on the right is the diff.

```
┌──────────────────────────────┬────────────────────────────────────────────────────────┐
│ Review · api-refactor        │ internal/store/user.go                                 │
│ 3 files changed  +42 -8      │ @@ -14,6 +14,9 @@ func (s *Store) Get(id string) {     │
│ nothing written yet          │    14   14      return nil, err                        │
│                              │    15   15    }                                        │
│ internal/store/user.go:17    │         16 +  if user.Deleted {                        │
│                              │         17 +    return nil, ErrGone                    │
│ no thread here yet           │         18 +  }                                        │
│                              │    16   19    return user, nil                         │
│ 2 files changed since you    │    17   20  }                                          │
│ last reviewed                │                                                        │
│                              │ internal/store/user_test.go                            │
│ ── Agent ──────────────────  │ @@ -3,0 +3,8 @@                                        │
│ name      api-refactor       │          3 +func TestGetDeleted(t *testing.T) {        │
│ state     working            │          4 +  store := newStore(t)                     │
│ base      a1b2c3d4e5f6       │          5 +  _, err := store.Get("gone")              │
│ snapshot  9f8e7d6c5b4a       │          6 +  require.ErrorIs(t, err, ErrGone)         │
│                              │          7 +}                                          │
│ j/k move · c start a thread  │                                                        │
└──────────────────────────────┴────────────────────────────────────────────────────────┘
```

Top left is the agent whose work this is. Under it, how big the change is. Under that, how your comments stand, which says nothing written yet until you write one.

The line about files changed since you last reviewed only shows once you have actually released a review on an earlier round of this agent, so a first look does not have it.

The line `internal/store/user.go:17` is the place the cursor is standing. That is where a comment would be pinned if you wrote one right now. It is the single most useful line on the screen: if it says nothing, a comment cannot be started there.

Base and snapshot in the record at the bottom are the two commits the diff is between. Base is where the agent's branch left the branch it was cut from. Snapshot is the frozen photograph. `git diff <base> <snapshot>` in the agent's directory shows you exactly what the screen is showing.

If the workflow lists paths to keep out of the diff, generated protobuf output say, the record shows them as excludes and those files are not on screen at all. An agent whose every changed file is excluded opens nothing, and weave says so rather than showing you an empty diff.

## Reading a line

Every line of the diff carries the same gutter.

```
    16   19    return user, nil             an untouched line
         17 +●   return nil, ErrGone        a new line, with a thread on it
    22      -  return nil, err              a line that was deleted
    │    │  ││
    │    │  │└─── thread mark
    │    │  └──── what the diff did to the line
    │    └─────── its line number in the new file
    └──────────── its line number in the old file
```

The two numbers are the line's place in the old file and in the new one. A deleted line has no new number, a new line has no old number. Then `+` or `-` or nothing. Then the thread mark, which is the whole state of the conversation on that line in one character:

```
  ●  a thread, open, nothing owed to you
  ◇  a thread holding a comment that has not been sent yet
  ✓  a thread you have settled
  ◆  the agent has said something you have not read
  ○  the code under this thread has changed since it was written
```

The marks are ranked, so a thread shows the one that matters most. An unread reply beats everything else.

There is no mark for a comment you have sent and the agent has not picked up yet. It goes back to `●`, and how far a comment has got is answered in the sidebar rather than in the gutter. Watch out for `✓`, which appears in both places meaning different things: in the gutter it means you settled the thread, in the sidebar badge it means the agent picked the mail up.

## Moving around

`j` and `k` move a line. `ctrl+n` and `ctrl+p` scroll. `g` and `G` go to the top and the bottom.

`]` and `[` jump to the next and previous file, and put that file's header at the top of the pane so you see the start of it rather than the last three lines of the one before.

`t` and `T` jump to the next and previous thread, which is how you walk a review that came back with fifteen comments in it.

`/` searches the diff, `n` and `N` step through the matches. The count under the search box says which match you are on, and tells you separately how many matches are inside files you folded away.

`h` and `l` pan sideways for a line too wide for the pane, `0` snaps back to the start.

## Folding away a file you have read

Press `v` to mark the file under the cursor read. It folds to one line and the cursor steps on to the next file, which makes a long review a walk down the list.

```
  before                                 after v
  ────────────────────────────────       ──────────────────────────────────────────
  internal/store/user.go                 internal/store/user.go  ✓ viewed  +9 −1  ● 2
  @@ -14,6 +14,9 @@ func Get(            internal/store/user_test.go
     14   14      return nil, err        @@ -3,0 +3,8 @@
     15   15    }                                 3 +func TestGetDeleted(t *testing.T)
          16 +  if user.Deleted {
```

The folded line tells you what is hidden under it: how many lines changed, and how many conversations in that file are still open, so folding a file cannot bury a comment you have not dealt with.

The count of files you have read is in the sidebar header, next to the size of the change. It is your record and yours alone: the agent is never told which parts of its work you looked at. Press `v` again to open the file back up. A file the agent writes to afterwards comes back on its own.

## Writing a comment

Put the cursor on a line and press `c`. The composer opens in the sidebar.

```
┌──────────────────────────────┬────────────────────────────────────────────────────────┐
│ Review · api-refactor        │ internal/store/user.go                                 │
│ 3 files changed  +42 -8      │ @@ -14,6 +14,9 @@ func (s *Store) Get(id string) {     │
│ nothing written yet          │    14   14      return nil, err                        │
│                              │    15   15    }                                        │
│ internal/store/user.go:17    │         16 +  if user.Deleted {                        │
│                              │         17 +    return nil, ErrGone   ← cursor is here │
│ ┌──────────────────────────┐ │         18 +  }                                        │
│ │ ErrGone is not wrapped,  │ │    16   19    return user, nil                         │
│ │ so callers cannot tell   │ │    17   20  }                                          │
│ │ which id was missing.    │ │                                                        │
│ │                          │ │                                                        │
│ └──────────────────────────┘ │                                                        │
│   ctrl+s save · esc cancel   │                                                        │
│                              │                                                        │
└──────────────────────────────┴────────────────────────────────────────────────────────┘
```

`ctrl+s` saves it. `esc` throws it away.

If the cursor is on a line that already has a thread, `c` replies in that thread instead of opening a new one. The help strip at the bottom of the sidebar says which of the two the key would do before you press it.

Saved, the comment looks like this, and the gutter mark on line 17 turns into `◇`:

```
│ Review · api-refactor        │
│ 3 files changed  +42 -8      │
│ 1 unsent                     │
│                              │
│ internal/store/user.go:17    │
│                              │
│ you 3:04PM                   │
│ ErrGone is not wrapped, so   │
│ callers cannot tell which id │
│ was missing.                 │
│ ◇ unsent                     │
```

Unsent is the important word. The agent knows nothing about this. You can write ten more, go and make coffee, come back and reword them. Nothing has happened yet.

Leaving the screen with `esc` does not throw drafts away either. They are kept in the panel, and they are still there, still unsent, the next time you open the diff.

## Sending the review

Press `ctrl+s` while the keys are on the diff, not in the composer. Weave asks what to send.

```
│ Review · api-refactor        │
│ 3 files changed  +42 -8      │
│ 3 unsent                     │
│                              │
│   anything to say about the  │
│   work as a whole? (optional)│
│ ┌──────────────────────────┐ │
│ │ Close on the error       │ │
│ │ wrapping and this is     │ │
│ │ good to go.              │ │
│ └──────────────────────────┘ │
│                              │
│   ▸ request changes          │
│     approve                  │
│     comment only             │
│                              │
│   tab to change · ctrl+s     │
│   send · esc cancel          │
```

`tab` walks the verdict, `ctrl+s` sends, `esc` backs out with everything still saved as drafts.

The verdict is the part worth choosing on purpose, because it is what the agent acts on and there may be no person in the loop to interpret it. Request changes means keep working. Approve means this round is done. Comment only means neither, and is the one verdict that refuses to go out empty, since mail carrying no comment and no conclusion just costs the agent a turn.

Approving with no comments at all is a complete review and works fine.

When it goes, three things happen at once. The whole batch is written to one markdown file. One piece of ordinary mail is sent to the agent saying where that file is. Every comment in the batch flips from unsent to waiting.

The mail is one line:

```
weave: review of round 2, changes requested, 3 comment(s), read
/Users/you/.local/state/weave/reviews/01a0…/round-2-9f3c….md
```

The file it points at carries the whole review, with the code around each comment as the round froze it:

```markdown
# Review of round 2

Changes requested. Keep working on this.

On the work of api-refactor (01a077f2…), reviewed by the person running weave.

This review was written against the snapshot 9f8e7d6c5b4a, taken from a1b2c3d4e5f6.
To see exactly what was reviewed, run git diff a1b2c3d4e5f6 9f8e7d6c5b4a.

## What the reviewer said

Close on the error wrapping and this is good to go.

## internal/store/user.go:17

Thread abc123

   11    func (s *Store) Get(id string) (*User, error) {
   12      user, err := s.rows.Get(id)
   13      if err != nil {
   14        return nil, err
   15      }
   16      if user.Deleted {
>> 17        return nil, ErrGone
   18      }

ErrGone is not wrapped, so callers cannot tell which id was missing.

Answer by writing in each thread, naming its id.
```

Sending from the panel does not interrupt the agent. Mail is handed over when the agent finishes its turn, so a busy agent reads the review at the end of whatever it is doing.

## Watching a comment land

The badge under each comment, and the counts at the top of the sidebar, track one comment at a time rather than the review as a whole.

```
  ◇ unsent            you have written it, nobody else can see it
  → waiting 3:04PM    released, the mail is sitting in the agent's box
  ✓ delivered 3:06PM  the agent has picked that mail up
```

Delivered means the agent claimed the mail, which is as far as weave can honestly see. It does not mean the agent agreed, or acted, or even read past the first line.

## When the agent answers

The agent replies in the thread. Its answer reaches you the moment it writes it, with no batching and no verdict, and the gutter mark on that line turns into `◆`.

```
┌──────────────────────────────┬────────────────────────────────────────────────────────┐
│ Review · api-refactor        │     14   14      return nil, err                       │
│ 3 files changed  +42 -8      │     15   15    }                                       │
│ 3 delivered                  │          16 +  if user.Deleted {                       │
│                              │          17 ◆+    return nil, fmt.Errorf("%w: %s"…     │
│ internal/store/user.go:17    │          18 +  }                                       │
│                              │     16   19    return user, nil                        │
│ you 3:04PM                   │                                                        │
│ ErrGone is not wrapped, so   │                                                        │
│ callers cannot tell which id │                                                        │
│ was missing.                 │                                                        │
│ ✓ delivered 3:06PM           │                                                        │
│                              │                                                        │
│ api-refactor 3:41PM          │                                                        │
│ Wrapped it with the id and   │                                                        │
│ added a test for the message.│                                                        │
│                              │                                                        │
└──────────────────────────────┴────────────────────────────────────────────────────────┘
```

Moving the cursor onto the thread is what marks it read, so the `◆` clears as you walk down the review.

Nothing pages you about the reply itself. You have no mailbox of your own, and the agents list has no badge that means a review was answered. What it does have is the green dot on a row, which means that agent finished a turn you have not looked at since, and an agent that has just answered your review is an agent that has just finished a turn. That dot is the cue; press `d` to see what was said.

Press `r` on a thread you are finished with. It marks it settled, `✓`, without deleting anything, and `r` again reopens it. Settling is your own bookkeeping: the agent is not told.

## Suggesting an exact change by editing the code

Telling an agent what to write is often slower than writing it. Press `e` on a line and weave opens your editor on that file, at that line.

What it opens is not the agent's file. Weave checks the frozen round out into a scratch directory of its own, one per agent, under your cache directory. So the agent goes on editing its worktree while you type into a copy of a photograph. Neither of you can write over the other, and nothing you type there ever reaches the agent by itself.

```
┌──────────────────────────────┬────────────────────────────────────────────────────────┐
│ Review · api-refactor        │ internal/store/user.go                                 │
│ 3 files changed  +42 -8      │ @@ -14,6 +14,9 @@ func (s *Store) Get(id string) {     │
│                              │         17 +    return nil, ErrGone                    │
│ internal/store/user.go:17    │         18 +  }                                        │
│                              ├────────────────────────────────────────────────────────┤
│                              │ ~/Library/Caches/weave/weave-rounds/01a0…/store/user.go│
│                              │                                                        │
│                              │  16   if user.Deleted {                                │
│                              │  17     return nil, fmt.Errorf("%w: %s", ErrGone, id)  │
│                              │  18   }                                                │
│                              │                                          your editor   │
└──────────────────────────────┴────────────────────────────────────────────────────────┘
```

Quit the editor. Weave reads the scratch copy back, works out which runs of lines you changed, and holds each one as a proposal pinned where you made it.

```
│ internal/store/user.go:17    │
│                              │
│ no thread here yet           │
│                              │
│   edit held for              │
│   internal/store/user.go:17  │
│   (1 line)                   │
│   c to comment with it       │
```

An edit is held, not sent. It needs a comment to carry it. Press `c`, say why, and `ctrl+s`: the thread goes out with your replacement lines attached, and the sidebar says the thread was opened with your edit as a suggestion.

Note the order of precedence. When an edit is held, `c` composes for that edit wherever the cursor happens to be standing, because the edit is pinned where you made it. It is not a comment on the line under the cursor.

The agent reads the suggestion as replacement lines it can apply rather than a description it has to interpret:

```markdown
## internal/store/user.go:17

Wrap the error so callers can tell which id was missing.

Suggested replacement for those lines:

    return nil, fmt.Errorf("%w: %s", ErrGone, id)
```

`esc` in the composer turns the edit down. The sidebar says so, and that edit is not offered to you again. The text is still sitting in the scratch checkout if you want it.

Two edits in one file are two proposals, not one span swallowing the code between them. They queue, and the sidebar says how many are behind the one you are being asked about.

If you edit a line the diff does not draw at all, weave has nowhere to pin it, and says so plainly:

```
┌──────────────────────────────────────────────────────────┐
│   internal/store/user.go:92 is not in the diff,          │
│   so that edit stays in the round's checkout             │
└──────────────────────────────────────────────────────────┘
```

Leaving the review screen throws the scratch checkout away, unless you left something in it that no comment carried, in which case weave keeps it. Next time you press `e` on a newer round, weave notices and asks before checking out over the top of it:

```
┌──────────────────────────────────────────────────────────┐
│   editing this round replaces the edit of an             │
│   earlier one in ~/Library/Caches/weave/weave-           │
│   rounds/01a0…, so press again to go on                  │
└──────────────────────────────────────────────────────────┘
```

## When an AI writes the review instead of you

An agent can ask for its own work to be read. It calls `request_review`, and weave spawns a reviewer as its child: same directory, no worktree of its own, named `<agent>-reviewer` in the panel, and briefed to read rather than to write.

You can ask for one yourself from a shell, without going through an agent:

```
weave rpc request-review --agent-id 01a077f2… --prompt "Read the error wrapping on the store, I am unsure the id belongs in the message."
```

That prompt is the whole briefing the reviewer gets, so say what the change is meant to do and where you are unsure of it. Naming a branch and nothing else buys you a review of whatever the reviewer guessed you meant.

The reviewer freezes a round, reads `git diff base snapshot`, writes threads with `create_review_thread`, and releases once with `release_review` and a verdict. The agent it reviewed gets the same file and the same mail you would have sent.

A reviewer gets one review per round. It cannot review again until the agent has actually written something, which is what stops two agents answering each other forever. It also cannot review its own work, or any agent other than the one it is a child of.

You see all of it. Open the diff with `d` and the AI's threads are there in the gutter alongside yours, in the same conversation, and you can reply in them.

```
┌──────────────────────────────┬────────────────────────────────────────────────────────┐
│ Review · api-refactor        │          17 +    return nil, ErrGone                   │
│ 3 files changed  +42 -8      │          18 +  }                                       │
│ 2 waiting                    │     16   19    return user, nil                        │
│                              │                                                        │
│ internal/store/user.go:17    │                                                        │
│                              │                                                        │
│ api-refactor 2:58PM          │                                                        │
│ ErrGone is returned bare, so │                                                        │
│ the caller loses the id.     │                                                        │
│ → waiting 2:58PM             │                                                        │
└──────────────────────────────┴────────────────────────────────────────────────────────┘
```

Four things about this are worth knowing.

The name on an AI reviewer's comment is the name of the agent being reviewed, not the name of the reviewer that wrote it. The panel labels every comment that came from any agent with the name at the top of the screen. So a comment from `api-refactor-reviewer` reads as `api-refactor`. Read the direction of the sentence to tell them apart: a review says what is wrong, a reply says what was done.

You see the AI's comments before it has released them. Everything is visible to you, drafts included, because you are the person and the panel holds nothing back from you. Those unreleased comments show as `◇ unsent` in the gutter, exactly like your own drafts.

They also count in the number at the top. `3 unsent` can be two of yours and one of a reviewer's still being written.

Your `ctrl+s` does not send them. Releasing sends your own batch and nothing else. An AI reviewer's drafts stay unsent until that reviewer releases them itself.

## When the agent keeps working while you read

The round does not move, on purpose: code cannot shift under a comment you are half way through writing. So when the agent writes something, weave tells you rather than redrawing.

```
│ Review · api-refactor                                    │
│ 3 files changed  +42 -8                                  │
│▓THE WORKTREE HAS MOVED ON · R to catch up▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓│
│                                                          │
│ internal/store/user.go:17                                │
```

Press `R` to take a new round and read the new diff. Your cursor comes back where it was. The bar is a reversed block rather than a dim line because writing comments against a diff that has already moved is how you end up with a handful of threads pinned to code that is gone.

Catching up costs you one thing. Comments and threads survive it, drafts included, and get re-placed against the new diff. An edit you are still holding does not: the lines it named belong to a snapshot that is no longer on screen, so the proposal is dropped and weave says so. The text is still in the scratch checkout, but you will not be offered it again for this round. If you have quit the editor and not yet written the comment to carry the edit, write it before pressing `R`.

If the agent edits the worktree back to exactly what the round holds, the bar goes away on its own.

## Threads whose code moved

When you catch up, some threads no longer have the line they were written against.

A thread whose surrounding code changed but which still has somewhere to sit gets a hollow `○`, so you do not read the line next to it as the line it is about. The sidebar then shows the lines it was written against, above the conversation:

```
│ internal/store/user.go:17    │
│                              │
│   written against            │
│   +    return nil, ErrGone   │
│                              │
│ you 3:04PM                   │
│ ErrGone is not wrapped…      │
```

A thread with no line left at all is collected at the very bottom of the diff, under its own heading, rather than dropped:

```
   ── threads with no line left to stand on ──
              ● internal/store/user.go:31  this branch is unreachable
```

You can still reply in those and settle them. You cannot start a new thread there, because there is no line to pin it to.

## An agent that is not running

The diff opens for a dormant or archived agent, because reading old work is worth doing. Writing to it is not, so those keys stand down and say why.

```
│ Review · api-refactor                                    │
│ 3 files changed  +42 -8                                  │
│▒this agent is dormant · resume it before writing to it▒▒▒│
```

The compose and send keys drop out of the help strip while that bar is up. Everything that only records your own progress, settling a thread and folding a file away, still works.

## Where things end up

Released reviews are markdown files under weave's state directory, in `reviews/<agent id>/round-<n>-<review id>.md`. They are kept for good, deliberately: the moment you want to read an old review is usually the moment the agent is gone.

Round checkouts for the editor are scratch, under your cache directory in `weave-rounds/<agent id>`. They are thrown away when you leave the review, unless you left uncarried work in one.

Neither is written inside the agent's worktree, which would put weave's own files into the diff you are reviewing.

## Every key on this screen

```
  d           open the review (from the agents list)
  j k         move a line
  ctrl+n p    scroll
  g G         top, bottom
  ] [         next, previous file
  t T         next, previous thread
  /           search, then n and N to step through matches
  h l 0       pan left, pan right, back to the start
  v           fold the file away as read, or open it again
  c           comment on this line, or reply in the thread here
  e           open this file in your editor, to suggest an exact change
  r           settle the thread here, or reopen it
  R           take a new round and read the diff again
  ctrl+s      send the review
  esc         back to the agents list
  ?           the full list, with what each key would do right now
```

In the composer and the send panel every other key is a character you are typing, so `esc` and `ctrl+c` are fixed there. The save and send key is not a review key at all: it is `keys.form.submit`, shared with every form in the panel, and rebinding it moves it here too.
