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
	"komrad/internal/forge"
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

func (s *ConfigStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckConfig)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	if _, err := os.Stat(s.Path); errors.Is(err, fs.ErrNotExist) {
		return observation{Verdict: verdictDone, Detail: shortenHome(s.Path) + " will be created"}
	}

	return observation{Verdict: verdictDone, Detail: shortenHome(s.Path)}
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

func (s *TmuxStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckTmux, doctor.CheckTmuxVersion, doctor.CheckTmuxShell)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	version := findingNamed(report, doctor.CheckTmuxVersion)

	return observation{Verdict: verdictDone, Detail: version.Detail}
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

func (s *HarnessStep) observe(ctx context.Context, check checker) observation {
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
		return observation{Verdict: verdictDone, Detail: describeHarnesses(defaulted.Detail, s.installed)}
	}

	if len(s.installed) > 0 {
		return observation{Verdict: verdictAsk, Detail: "the default harness is not on PATH"}
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
		return fmt.Errorf("%w: pass --harness, one of %s", errNeedsAnswer, strings.Join(s.installed, ", "))
	}

	s.chosen = s.Flag

	return nil
}

func (s *HarnessStep) repair(context.Context) error {
	if err := setTopLevelKey(s.Path, "default_harness", s.chosen); err != nil {
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
	path  string
	text  string
}

func (s *ContextStep) name() string {
	return "context"
}

func (s *ContextStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckConfig, doctor.CheckContext)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	found := findingNamed(report, doctor.CheckContext)

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return observation{Verdict: verdictStop, Detail: err.Error()}
	}
	s.path, err = cfg.GetContextFile()
	if err != nil {
		return observation{Verdict: verdictStop, Detail: err.Error()}
	}

	if _, err := os.Stat(s.path); err == nil {
		return observation{Verdict: verdictDone, Detail: found.Detail}
	}

	return observation{Verdict: verdictAsk, Detail: "no standing context yet"}
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
		return errSkipped
	}

	return writeAtomically(s.path, []byte(strings.TrimRight(s.text, "\n")+"\n"))
}

// AutoSetupStep offers to have `komrad open` set up a workflow for the checkout it is run in.
type AutoSetupStep struct {
	Path   string
	Stack  config.Stack
	chosen bool
}

func (s *AutoSetupStep) name() string {
	return "workflow"
}

func (s *AutoSetupStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckConfig)
	if failed := firstFailing(report); failed != nil {
		return stopOn(failed)
	}

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return observation{Verdict: verdictStop, Detail: err.Error()}
	}
	if cfg.AutoSetup {
		return observation{Verdict: verdictDone, Detail: "set up wherever komrad is opened"}
	}

	return observation{Verdict: verdictAsk, Detail: "set up by hand"}
}

func (s *AutoSetupStep) question() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Set up a workflow wherever you open komrad?").
			Description("Opening komrad in a git checkout with no workflow creates one there, with a worktree per agent and an orchestrator standing by.").
			Value(&s.chosen),
	))
}

func (s *AutoSetupStep) answerUnattended() error {
	return nil
}

func (s *AutoSetupStep) repair(context.Context) error {
	if !s.chosen {
		return errSkipped
	}

	if err := setTopLevelBool(s.Path, "auto_setup", true); err != nil {
		return err
	}

	cfg, err := config.LoadStack(s.Stack)
	if err != nil {
		return err
	}
	if !cfg.AutoSetup {
		return fmt.Errorf("wrote auto_setup to %s, but a later config layer turns it off", shortenHome(s.Path))
	}

	return nil
}

const (
	choiceGHToken = "gh"
	githubHost    = "github.com"
	gitlabHost    = "gitlab.com"
	choiceLater   = "later"
)

// ForgeStep offers to connect github.com or gitlab.com when the engine has no forge, and skips
// while the engine is not answering.
type ForgeStep struct {
	Connect   func(ctx context.Context, host string, token forge.Secret) error
	GHToken   func(ctx context.Context) forge.Secret
	Flag      string
	TokenFile string
	ghToken   forge.Secret
	choice    string
	pasted    string
}

func (s *ForgeStep) name() string {
	return "forge"
}

func (s *ForgeStep) observe(ctx context.Context, check checker) observation {
	report := check(ctx, doctor.CheckServer, doctor.CheckForges)
	server := findingNamed(report, doctor.CheckServer)
	forges := findingNamed(report, doctor.CheckForges)

	switch {
	case server.Status == doctor.StatusFail:
		return observation{Verdict: verdictSkip, Detail: server.Error, Finding: &server}
	case server.Status != doctor.StatusOK, forges.Code == doctor.CodeForgesUnreadable:
		return observation{Verdict: verdictSkip, Detail: "the engine is not running; open the panel, then run setup again"}
	case forges.Status == doctor.StatusOK:
		return observation{Verdict: verdictDone, Detail: forges.Detail}
	case forges.Code != doctor.CodeNoForges:
		return observation{Verdict: verdictSkip, Detail: forges.Error, Finding: &forges}
	}

	s.ghToken = s.GHToken(ctx)

	return observation{Verdict: verdictAsk, Detail: "no forge is connected"}
}

func (s *ForgeStep) question() *huh.Form {
	var options []huh.Option[string]
	if !s.ghToken.IsZero() {
		options = append(options, huh.NewOption("GitHub, with gh's token", choiceGHToken))
	}
	options = append(options,
		huh.NewOption("GitHub, paste a token", githubHost),
		huh.NewOption("GitLab, paste a token with the api scope", gitlabHost),
		huh.NewOption("Not now", choiceLater),
	)
	s.choice = options[0].Value

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Connect a forge so komrad can read pull requests").
				Description("For a self-hosted forge, run komrad forge connect after setup.").
				Options(options...).
				Value(&s.choice),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Token").
				Description("The forge checks it before anything is stored.").
				EchoMode(huh.EchoModePassword).
				Validate(requireToken).
				Value(&s.pasted),
		).WithHideFunc(func() bool { return s.choice != githubHost && s.choice != gitlabHost }),
	)
}

func requireToken(pasted string) error {
	if forge.NewSecret(pasted).IsZero() {
		return errors.New("paste a token, or go back and choose not now")
	}

	return nil
}

func (s *ForgeStep) answerUnattended() error {
	if s.Flag == "" && s.TokenFile != "" {
		return fmt.Errorf("%w: --forge-token-file needs --forge to name the host", errNeedsAnswer)
	}
	if s.Flag == "" {
		s.choice = choiceLater

		return nil
	}
	if s.Flag != githubHost && s.Flag != gitlabHost {
		return fmt.Errorf("%w: --forge is github.com or gitlab.com, and komrad forge connect connects any other host", errNeedsAnswer)
	}

	if s.TokenFile != "" {
		raw, err := os.ReadFile(s.TokenFile)
		if err != nil {
			return fmt.Errorf("failed to read the token file: %w", err)
		}
		s.choice, s.pasted = s.Flag, string(raw)

		return nil
	}

	if s.Flag == gitlabHost {
		return fmt.Errorf("%w: pass --forge-token-file naming a personal access token with the api scope", errNeedsAnswer)
	}
	if s.ghToken.IsZero() {
		return fmt.Errorf("%w: gh has no token for github.com, so pass --forge-token-file", errNeedsAnswer)
	}
	s.choice = choiceGHToken

	return nil
}

func (s *ForgeStep) repair(ctx context.Context) error {
	switch s.choice {
	case choiceGHToken:
		return s.Connect(ctx, githubHost, s.ghToken)
	case githubHost, gitlabHost:
		token := forge.NewSecret(s.pasted)
		if token.IsZero() {
			return errors.New("no token was given")
		}

		return s.Connect(ctx, s.choice, token)
	}

	return errSkipped
}
