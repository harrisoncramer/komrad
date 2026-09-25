---
name: record-komrad
description: Record the panel for a reviewer - drive the throwaway instance by keypresses from a script, take one frame of every window after each step, render the frames to a gif, and attach it to the pull request. Use when a change alters what the panel draws or does on a key, and a person reading the pull request could not otherwise see it.
---

# Recording komrad for a reviewer

A change to the panel lands as Go and goldens, and a diff of a screen does not show what the
screen does. `mise run record` drives the throwaway instance from a script of keypresses and
takes one frame after every step, so a reviewer opens the pull request and watches the change
under the description instead of checking the branch out.

The recorder only ever runs against the throwaway instance on tmux socket `komrad-test`. Every
command here carries `MISE_ENV=test`; without it the task refuses to run. The live panel on
socket `komrad` is never recorded.

## Stand the instance up

    MISE_ENV=test mise run restart

That builds this checkout and brings the throwaway server up with the panel and nothing else,
which is the state a script should start from. Every agent there is dormant after a restart,
so a script that presses enter on one starts a real CLI in the recording: that is slow, costs
tokens, and draws differently every time. Prefer steps that stay in the panel unless the change
is about what an agent's pane looks like.

## Write a script

Scripts live under `recordings/` and are committed, so the recording can be remade after the
next push. One step per line; blank lines and `#` comments are skipped.

    # move down the list, open what the cursor is on, come back, search, close
    key j
    key j
    key Enter
    wait Enter to confirm
    key C-a
    key s
    type probe
    sleep 500ms
    key Escape

`key` presses tmux key names, so `Enter`, `Escape`, `C-c`, `Space` and `j` all work. A key the
instance binds in its root table, such as the panel's `C-a` focus key, runs its binding the way
it would from a keyboard, and any other key goes to the pane. `type` sends its text to the pane
as typed. `sleep` waits. `wait` polls every window until one shows the text, giving up after
thirty seconds. The keys the panel answers to are in `config.toml` under `[keybindings]`.

## Record

    MISE_ENV=test mise run record recordings/tour.rec

The panel's window is drawn by a client of the recorder's own at 140 by 40, splits and the
agent pane the panel has opened included, with the step written at the top right. That is the
frame: what a person sees in the popup, and nothing beside it. The output lands under
`local/recordings/tour/`, which is not tracked:

    frames/NNN.txt   each frame, escapes and all, so `cat -v` shows what a step did
    build.txt        the panel's build, from @komrad_panel_build
    recording.cast   an asciicast with a marker per step, for `asciinema play`
    recording.gif    what goes on the pull request

`--width` and `--height` change the window size, `--pause` how long each frame shows (1.5s),
`--theme` an agg theme name when the colours should not come from `~/.alacritty.toml`, and
`--out` moves the output. When a step did not
do what it meant, read its frame before changing the script, and read the one before it, since
a frame is taken after the panel settles and a key that arrived too early shows there.

## Attach it to the pull request

Keep the description in a file of your own, ending with a reference to the gif by its local
path, and hand gh both every time. gh rewrites that reference to the upload, so the live body
holds an asset URL and a rerun from the same file replaces the gif rather than adding one:

    cat > local/recordings/tour/body.md <<'END'
    The description of the pull request, as it is.

    ![tour](./local/recordings/tour/recording.gif)
    END
    gh pr edit --body-file local/recordings/tour/body.md --attach ./local/recordings/tour/recording.gif

Run that whole block again after every push, writing the file whole each time. Never build
the file from `gh pr view` or append to it, since either carries the earlier reference along
and a second gif appears under the first.

The gh pinned in `mise.toml` carries `--attach`; a gh older than 2.99.0 does not. GitHub takes a
gif up to 10 MB, and the recorder says when one is over that, in which case the script is too
long. The recording is never committed; the script that made it is.
