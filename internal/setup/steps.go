package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"komrad/config"
	"komrad/internal/doctor"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
)

// ConfigStep reports which file setup writes answers into, and stops on a config that will not
// parse.
type ConfigStep struct {
	Path string
}

func (s *ConfigStep) name() string {
	return "config"
}

func (s *ConfigStep) observe(ctx context.Context, check Checker) Observation {
	report := check(ctx, doctor.CheckConfig)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	if _, err := os.Stat(s.Path); errors.Is(err, fs.ErrNotExist) {
		return Observation{Verdict: VerdictDone, Detail: shortenHome(s.Path) + " will be created"}
	}

	return Observation{Verdict: VerdictDone, Detail: shortenHome(s.Path)}
}

func (s *ConfigStep) question() *huh.Form {
	return nil
}

func (s *ConfigStep) answerUnattended() error {
	return nil
}

func (s *ConfigStep) repair(context.Context) error {
	return nil
}

// TmuxStep stops when tmux is missing or too old, since installing it is not komrad's to do.
type TmuxStep struct{}

func (s *TmuxStep) name() string {
	return "tmux"
}

func (s *TmuxStep) observe(ctx context.Context, check Checker) Observation {
	report := check(ctx, doctor.CheckTmux, doctor.CheckTmuxVersion, doctor.CheckTmuxShell)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	version := findingNamed(report, doctor.CheckTmuxVersion)

	return Observation{Verdict: VerdictDone, Detail: version.Detail}
}

func (s *TmuxStep) question() *huh.Form {
	return nil
}

func (s *TmuxStep) answerUnattended() error {
	return nil
}

func (s *TmuxStep) repair(context.Context) error {
	return nil
}

// HarnessStep makes the default harness one whose CLI is on PATH.
type HarnessStep struct {
	Path      string
	Stack     config.Stack
	Harnesses []string
	Flag      string

	installed []string
	chosen    string
}

func (s *HarnessStep) name() string {
	return "harness"
}

func (s *HarnessStep) observe(ctx context.Context, check Checker) Observation {
	report := check(ctx, append([]string{doctor.CheckConfig, doctor.CheckDefaultHarness}, s.Harnesses...)...)

	s.installed = s.installed[:0]
	var missing *doctor.Finding
	for _, id := range s.Harnesses {
		finding := findingNamed(report, id)
		switch finding.Status {
		case doctor.StatusOK:
			s.installed = append(s.installed, id)
		case doctor.StatusFail:
			missing = &finding
		case doctor.StatusWarn, doctor.StatusSkip:
		}
	}

	defaulted := findingNamed(report, doctor.CheckDefaultHarness)
	if defaulted.Status == doctor.StatusOK && missing == nil {
		return Observation{Verdict: VerdictDone, Detail: describeHarnesses(defaulted.Detail, s.installed)}
	}

	if len(s.installed) > 0 {
		return Observation{Verdict: VerdictAsk, Detail: "the default harness is not on PATH"}
	}
	if missing != nil {
		return stopOn(missing)
	}

	return stopOn(&defaulted)
}

func (s *HarnessStep) question() *huh.Form {
	options := xslice.Map(s.installed, func(id string) huh.Option[string] { return huh.NewOption(id, id) })

	return huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Which harness should new agents run?").
			Options(options...).
			Value(&s.chosen),
	))
}

func (s *HarnessStep) answerUnattended() error {
	if s.Flag == "" {
		return fmt.Errorf("%w: pass --harness, one of %s", ErrNeedsAnswer, strings.Join(s.installed, ", "))
	}

	s.chosen = s.Flag

	return nil
}

func (s *HarnessStep) repair(context.Context) error {
	if err := SetTopLevelKey(s.Path, "default_harness", s.chosen); err != nil {
		return err
	}

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return err
	}
	if cfg.DefaultHarness != s.chosen {
		return fmt.Errorf("wrote default_harness to %s, but a later config layer sets it to %s", shortenHome(s.Path), cfg.DefaultHarness)
	}

	return nil
}

func describeHarnesses(chosen string, installed []string) string {
	if len(installed) <= 1 {
		return chosen
	}

	return fmt.Sprintf("%s, of %s", chosen, strings.Join(installed, " and "))
}

// ContextStep offers to write the standing context every agent starts with, and never touches
// one that is already there.
type ContextStep struct {
	Stack config.Stack

	path string
	text string
}

func (s *ContextStep) name() string {
	return "context"
}

func (s *ContextStep) observe(ctx context.Context, check Checker) Observation {
	report := check(ctx, doctor.CheckConfig, doctor.CheckContext)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	found := findingNamed(report, doctor.CheckContext)

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return Observation{Verdict: VerdictStop, Detail: err.Error()}
	}
	s.path, err = cfg.GetContextFile()
	if err != nil {
		return Observation{Verdict: VerdictStop, Detail: err.Error()}
	}

	if _, err := os.Stat(s.path); err == nil {
		return Observation{Verdict: VerdictDone, Detail: found.Detail}
	}

	return Observation{Verdict: VerdictAsk, Detail: "no standing context yet"}
}

func (s *ContextStep) question() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewText().
			Title("Standing context for every agent").
			Description("Saved to " + shortenHome(s.path) + ". Leave it empty to skip.").
			Placeholder("What is true of this machine?\nHow do you want to be worked with?").
			Lines(8).
			Value(&s.text),
	))
}

func (s *ContextStep) answerUnattended() error {
	return nil
}

func (s *ContextStep) repair(context.Context) error {
	if strings.TrimSpace(s.text) == "" {
		return ErrSkipped
	}

	return writeAtomically(s.path, []byte(strings.TrimRight(s.text, "\n")+"\n"))
}
