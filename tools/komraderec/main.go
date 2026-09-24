// Package main records the throwaway komrade instance one frame per script step, and renders the frames to a gif.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const gifLimit = 10 << 20

var refusedSockets = map[string]string{
	"":               "no tmux socket named, so this would record whatever tmux server is default",
	"komrade":        "that is the live panel",
	"komrade-remote": "that is the remote engine's panel",
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := run(ctx, os.Args[1:], os.Stdout)
	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "komraderec:", err)
		os.Exit(1)
	}
}

type options struct {
	socket    string
	session   string
	cols      int
	rows      int
	out       string
	theme     string
	alacritty string
	pause     time.Duration
	script    string
}

func parseOptions(args []string) (*options, error) {
	flags := flag.NewFlagSet("komraderec", flag.ContinueOnError)
	opts := &options{}
	flags.StringVar(&opts.socket, "socket", "", "the tmux socket of the instance to record")
	flags.StringVar(&opts.session, "session", "komrade", "the session on that socket whose windows are recorded")
	flags.IntVar(&opts.cols, "width", 140, "columns of every window")
	flags.IntVar(&opts.rows, "height", 40, "rows of every window")
	flags.StringVar(&opts.out, "out", "", "the directory the recording is written under")
	flags.StringVar(&opts.theme, "theme", "", "an agg theme name or hex palette for the gif")
	flags.StringVar(&opts.alacritty, "alacritty", "", "an alacritty config whose colours the gif takes when --theme is empty")
	flags.DurationVar(&opts.pause, "pause", 1500*time.Millisecond, "how long each frame shows in the gif")

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if flags.NArg() != 1 {
		return nil, fmt.Errorf("usage: komraderec --socket komrade-test [--width 140] [--height 40] [--out dir] <script>")
	}

	if reason, refused := refusedSockets[opts.socket]; refused {
		return nil, fmt.Errorf("refusing to record tmux socket %q: %s", opts.socket, reason)
	}

	if opts.pause <= 0 {
		return nil, fmt.Errorf("a pause of %s leaves no time to read a frame", opts.pause)
	}

	if opts.cols < 20 || opts.rows < 5 {
		return nil, fmt.Errorf("a window of %dx%d is too small to record", opts.cols, opts.rows)
	}

	opts.script = flags.Arg(0)
	if opts.out == "" {
		opts.out = filepath.Join("local", "recordings", strings.TrimSuffix(filepath.Base(opts.script), filepath.Ext(opts.script)))
	}

	return opts, nil
}

func resolveTheme(opts *options) (string, error) {
	if opts.theme != "" || opts.alacritty == "" {
		return opts.theme, nil
	}

	file, err := os.Open(opts.alacritty)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", opts.alacritty, err)
	}
	defer file.Close()

	theme, err := parseAlacrittyTheme(file)
	if err != nil {
		return "", fmt.Errorf("%w in %s", err, opts.alacritty)
	}

	return theme, nil
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	file, err := os.Open(opts.script)
	if err != nil {
		return fmt.Errorf("failed to open script: %w", err)
	}
	defer file.Close()

	steps, err := parseScript(file)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", opts.script, err)
	}

	theme, err := resolveTheme(opts)
	if err != nil {
		return err
	}

	framesDir := filepath.Join(opts.out, "frames")
	for _, stale := range []string{framesDir, filepath.Join(opts.out, "build.txt"), filepath.Join(opts.out, "recording.cast"), filepath.Join(opts.out, "recording.gif")} {
		if err := os.RemoveAll(stale); err != nil {
			return fmt.Errorf("failed to clear %s: %w", stale, err)
		}
	}

	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", framesDir, err)
	}

	recorded := &instance{
		inner: tmuxServer{socket: opts.socket},
		outer: tmuxServer{
			socket: "komrade-record",
			args:   []string{"-f", "/dev/null"},
		},
		session: opts.session,
		cols:    opts.cols,
		rows:    opts.rows,
	}

	build, err := recorded.requirePanel(ctx)
	if err != nil {
		return err
	}
	recorded.outer.forget(ctx, "kill-server")
	defer recorded.close()

	buildPath := filepath.Join(opts.out, "build.txt")
	if err := os.WriteFile(buildPath, []byte(build+"\n"), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", buildPath, err)
	}

	shots, err := record(ctx, recordParams{
		recorded:  recorded,
		steps:     steps,
		framesDir: framesDir,
		stdout:    stdout,
	})
	if err != nil {
		return err
	}

	return render(ctx, renderParams{
		out:    opts.out,
		title:  filepath.Base(opts.out) + " on " + build,
		cols:   opts.cols,
		theme:  theme,
		pause:  opts.pause,
		shots:  shots,
		stdout: stdout,
	})
}

type recordParams struct {
	recorded  *instance
	steps     []step
	framesDir string
	stdout    io.Writer
}

func record(ctx context.Context, params recordParams) ([]shot, error) {
	recorded, steps, framesDir, stdout := params.recorded, params.steps, params.framesDir, params.stdout
	shots := make([]shot, 0, len(steps)+1)

	take := func(label string) error {
		current, err := recorded.settle(ctx, label)
		if err != nil {
			return err
		}

		taken := shot{
			label: label,
			frame: current,
		}

		name := filepath.Join(framesDir, fmt.Sprintf("%03d.txt", len(shots)))
		if err := os.WriteFile(name, []byte(current.file()), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}

		shots = append(shots, taken)

		if _, err := fmt.Fprintf(stdout, "%03d  %s\n", len(shots)-1, label); err != nil {
			return fmt.Errorf("failed to write progress: %w", err)
		}

		return nil
	}

	if err := take("start"); err != nil {
		return nil, err
	}

	for _, current := range steps {
		if err := perform(ctx, recorded, current); err != nil {
			return nil, fmt.Errorf("%w at line %d, %s", err, current.line, current.text)
		}

		if err := take(current.text); err != nil {
			return nil, err
		}
	}

	return shots, nil
}

func perform(ctx context.Context, recorded *instance, current step) error {
	switch current.kind {
	case stepKey:
		return recorded.sendKeys(ctx, strings.Fields(current.arg)...)
	case stepType:
		return recorded.typeText(ctx, current.arg)
	case stepSleep:
		return sleep(ctx, current.pause)
	case stepWait:
		return recorded.await(ctx, current.arg)
	default:
		return fmt.Errorf("unknown step kind %d", current.kind)
	}
}

type renderParams struct {
	out    string
	title  string
	cols   int
	theme  string
	pause  time.Duration
	shots  []shot
	stdout io.Writer
}

func render(ctx context.Context, params renderParams) error {
	castPath := filepath.Join(params.out, "recording.cast")
	gifPath := filepath.Join(params.out, "recording.gif")

	castFile, err := os.Create(castPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", castPath, err)
	}

	if err := writeCast(castFile, writeCastParams{
		title: params.title,
		cols:  params.cols,
		pause: params.pause,
		shots: params.shots,
	}); err != nil {
		castFile.Close()

		return err
	}

	if err := castFile.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", castPath, err)
	}

	args := []string{"--idle-time-limit", fmt.Sprint(params.pause.Seconds()), "--last-frame-duration", "3"}
	if params.theme != "" {
		args = append(args, "--theme", params.theme)
	}

	agg := exec.CommandContext(ctx, "agg", append(args, castPath, gifPath)...)
	agg.Stderr = os.Stderr
	if err := agg.Run(); err != nil {
		return fmt.Errorf("failed to render %s with agg, which mise installs: %w", castPath, err)
	}

	info, err := os.Stat(gifPath)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", gifPath, err)
	}

	summary := fmt.Sprintf("%d frames of %s, %s (%d KB)", len(params.shots), params.title, gifPath, info.Size()>>10)
	if info.Size() > gifLimit {
		summary += "\nthe gif is over 10 MB, which gh will not attach; shorten the script"
	}

	if _, err := fmt.Fprintln(params.stdout, summary); err != nil {
		return fmt.Errorf("failed to write summary: %w", err)
	}

	return nil
}
