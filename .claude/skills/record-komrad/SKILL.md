---
name: record-komrad
description: Record komrad with VHS - drive the throwaway instance from a tape, in the docs site's colours, to a video, a gif and a still, and attach the gif to a pull request or publish the video on the docs site. Use when a change alters what the panel draws or does on a key and a reviewer could not otherwise see it, or when a docs page needs a recording.
---

# Recording komrad

A change to the panel lands as Go and goldens, and a diff of a screen does not show what the
screen does. `mise run record` runs a VHS tape against the throwaway instance: VHS starts a
terminal of its own through ttyd, types what the tape says into it, and renders what it draws
in a headless browser. Nothing is captured from your screen, so it records the same whether or
not you are at the machine, and whatever you are doing meanwhile.

The recorder only ever runs against the throwaway instance on tmux socket `komrad-test`. Every
command here carries `MISE_ENV=test`; without it the task refuses to run. The live panel on
socket `komrad` is never recorded.

mise installs VHS and the ffmpeg it uses, and leaves both out of CI. VHS also needs ttyd, which
mise cannot install on macOS, so install that with nix or Homebrew.

## Stand the instance up

    MISE_ENV=test mise run restart

That builds this checkout and brings the throwaway server up with the panel and nothing else.
To start from no workflows at all, drop its database first:

    MISE_ENV=test mise run docker:down -v
    MISE_ENV=test mise run docker:up
    MISE_ENV=test mise run restart

## Where tapes live

    recordings/
      settings.tape shell.tape panel.tape   shared, sourced by every tape
      prod/                                 tracked: what the docs site shows
      dev/                                  untracked scratch, for a pull request or a look
        example-*.tape                      tracked references to copy from

A tape the docs site shows goes in `prod/`, with its `.before` and `.after` scripts, and is
committed so it can be remade. Anything else goes in `dev/`, where git ignores it. The
`example-*.tape` files in `dev/` are the one exception: they are kept only as worked examples
of driving the panel, so start a new tape by copying one. Never add a tape anywhere else.

What a tape renders lands beside it and is never committed, in either folder. The docs
bucket's `media/` holds what `prod/` rendered and the site shows.

## Write a tape

Tapes are [VHS tapes](https://github.com/charmbracelet/vhs), with two things the task adds:

- `recordings/settings.tape` is sourced ahead of every tape. It sets the size, the font and the
  docs site's colours, so a tape does not set its own.
- A tape starts by sourcing where it begins. `Source recordings/shell.tape` puts it in a clean
  shell in the home directory, and `Source recordings/panel.tape` opens the panel out of shot.

The tape runs in a bash whose `komrad` is this checkout's build. Keys a tape presses reach tmux
the way they would from a keyboard, so the panel's `Ctrl+A` and `Ctrl+Q` cycle keys and a
tmux prefix both work.

    Source recordings/shell.tape
    Type "komrad open"
    Enter
    Wait+Screen@30s /Welcome, komrad!/
    Type "w"
    Type "neovim"
    Ctrl+S
    Wait+Screen@4m /done \d+:\d+ [AP]M/

- `Type` types text and `Enter`, `Escape`, `Down`, `Ctrl+S` and the like press keys.
- `Sleep` waits and keeps recording.
- `Wait+Screen@<timeout> /regex/` waits until the screen matches.
- `Hide` and `Show` stop and start recording, for setup a viewer should not watch.

A Claude session finishes a turn on a line like `Crunched for 4s · done 10:09 PM`, so waiting
for `/done \d+:\d+ [AP]M/` waits out a reply. When one is already on screen, match the question
first so only the new reply counts, as in `/work on it\?[\s\S]*done \d+:\d+ [AP]M/`. Keys sent
to a Claude pane go to Claude, so a bare `Enter` on its folder-trust prompt picks "No, exit".

## Record

    cp recordings/dev/example-tour.tape recordings/dev/my-change.tape
    MISE_ENV=test mise run record recordings/dev/my-change.tape

The output lands beside the tape:

    my-change.mp4    the video
    my-change.webm   the same video for browsers that prefer it
    my-change.gif    for a pull request
    my-change.png    the final frame

`--out` moves it. What VHS actually ran, the settings, the tape and the closing screenshot, is
kept in `local/recordings/my-change/run.tape`. When a step did not do what it meant, add a
`Screenshot` after it and look.

## Attach it to a pull request

Keep the description in a file of your own, ending with a reference to the gif by its local
path, and hand gh both every time. gh rewrites that reference to the upload, so the live body
holds an asset URL and a rerun from the same file replaces the gif rather than adding one:

    cat > local/recordings/my-change/body.md <<'END'
    The description of the pull request, as it is.

    ![my-change](./recordings/dev/my-change.gif)
    END
    gh pr edit --body-file local/recordings/my-change/body.md --attach ./recordings/dev/my-change.gif

Run that whole block again after every push, writing the file whole each time. Never build
the file from `gh pr view` or append to it, since either carries the earlier reference along
and a second gif appears under the first. GitHub takes a gif up to 10 MB, so keep a tape meant
for a pull request short.

## Publish one on the docs site

Recordings the docs pages show live under `recordings/prod/`, one tape per published name,
and are never committed as media. A `<name>.before` script beside one runs against the fresh
throwaway instance first, with this checkout's `komrad` on its PATH, to set up what the video
should not show, such as a workflow made with `komrad rpc create-workflow`. A `<name>.after`
script runs once the recording is done, to clear what it made.

    mise run docs:recordings                           every docs recording
    mise run docs:recordings orchestrator first-agent  just those
    mise run infra:docs:recordings upload              publish them on komrad.dev

`docs:recordings` drops the throwaway instance's database before each one and writes
`<name>.mp4`, `<name>.webm` and `<name>.png` beside each tape in `recordings/prod/`. The png is
the last frame, which is a video's poster or a still on its own. A page reaches a file with
`media("<name>.mp4")` from `docs/src/site.ts`:

    <video autoplay loop muted playsinline poster={media("first-workflow.png")}>
      <source src={media("first-workflow.webm")} type="video/webm" />
      <source src={media("first-workflow.mp4")} type="video/mp4" />
    </video>

The docs dev server answers `media(...)` from `recordings/prod/` when the file is there, and
otherwise from komrad.dev's `media/`, so a machine that has recorded nothing still shows every
video that has been published. The built site reads the docs bucket's
`media/`, which only the publish step writes, so nothing reaches komrad.dev until it is
published. The names are fixed, so re-recording and uploading again replaces what the page
shows.
