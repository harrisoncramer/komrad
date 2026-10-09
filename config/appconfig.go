// Package config loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the tags an agent can be given, the panel keybindings, and which database, address
// and tmux server this komrad is.
package config

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// defaultLogLevel is how much the service writes when the config says nothing. Failures
// are written at error and everything else at debug, so the file stays empty while the
// service works.
const defaultLogLevel = "info"

// defaultDeliveryDeadline is how long mail stays deliverable when the config names no
// duration. Long enough that an agent left alone over lunch still gets its mail, short
// enough that nothing chases a recipient nobody is coming back to.
const defaultDeliveryDeadline = "3h"

// DefaultMaxExchange is how much mail two agents may write each other when the config names no
// limit, set at more than twice the longest exchange the panel has recorded finishing, since a
// cap set too high costs one long loop and a cap set too low severs a review as it concludes.
const DefaultMaxExchange = 40

// defaultMaxCheckLogBytes is the largest check log komrad keeps when the config names no size, set
// at more than three times the largest log sampled across real repositories.
const defaultMaxCheckLogBytes = 2 << 20

// DefaultFrameHistory is how many frames a recording keeps for one agent when the config names
// no number. Since komrad captures about one frame every two seconds, this equates roughly to 15 minutes
// of frames captured, which should be more than enough to replicate most issues related to state transitions
// or reading data off of an agent panel.
const DefaultFrameHistory = 500

// defaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const defaultMaxAgents = 50

// defaultMaxChildren is how many children one agent may hold at once when the config names no
// number. It is far lower than the panel's own ceiling because they all work in the one
// checkout, so a third is usually a sign an orchestrator is fanning out where it wanted agents
// of their own.
const defaultMaxChildren = 3

// defaultImplementerAutoCompaction is the conversation size an implementer compacts at.
const defaultImplementerAutoCompaction = 250_000

// defaultReviewerAutoCompaction is the conversation size a reviewer compacts at.
const defaultReviewerAutoCompaction = 250_000

// defaultOrchestratorAutoCompaction is the conversation size an orchestrator compacts at.
const defaultOrchestratorAutoCompaction = 150_000

// DefaultHarness is the CLI a new agent runs when the config names none, spelled here
// rather than taken from the registry since this is a default a user overrides.
const DefaultHarness = "claude"

// The layout the panel lays itself out at when the config names none. The info pane opens
// tall enough to read a log in without pushing the agent above it off the screen.
const (
	defaultSidebarSide   = SidebarSideLeft
	DefaultSidebarWidth  = 47
	defaultInfoSide      = infoSideBelow
	DefaultInfoHeight    = 0.4
	defaultDetailsHeight = 9
	defaultTriageRows    = 3
)

// Where each region of the panel's layout can sit.
const (
	SidebarSideLeft  = "left"
	SidebarSideRight = "right"
	infoSideBelow    = "below"
	InfoSideAbove    = "above"
)

// Hooks is where komrad finds the scripts a workflow hangs on a moment in its agents' lives.
type Hooks struct {
	// Directory for hooks: one executable file per hook, named after the hook.
	// A hook is a script or program komrad runs at a moment a workflow names it against, in the
	// agent's own directory, with agent info in KOMRAD_AGENT as JSON and the moment in
	// KOMRAD_TRIGGER. Default is ~/.config/komrad/hooks.
	Dir string `mapstructure:"dir"`
}

// Worktrees is where a workflow's generated git worktree provisioner puts what it creates.
type Worktrees struct {
	// Directory generated worktrees are created under, one subdirectory per agent.
	// Left empty this is ~/.local/state/komrad/worktrees.
	Dir string `mapstructure:"dir"`
}

// Git is how komrad reads the checkouts its agents work in.
type Git struct {
	// SSH hosts that stand in for a forge's web host, each written as "ssh_host=web_host", such
	// as "ssh.gitlab.example.com=gitlab.example.com". A remote on the SSH host is then matched to
	// the forge connected for the web host. Write host names only, with no port, scheme or
	// user. Public aliases such as ssh.github.com and altssh.gitlab.com are already known.
	SSHHosts []string `mapstructure:"ssh_hosts"`
}

// GetSSHHosts is the SSH hosts keyed to the web host each stands in for, lowercased.
func (g Git) GetSSHHosts() (map[string]string, error) {
	hosts := make(map[string]string, len(g.SSHHosts))
	for _, entry := range g.SSHHosts {
		ssh, web, ok := strings.Cut(entry, "=")
		ssh, web = strings.ToLower(strings.TrimSpace(ssh)), strings.ToLower(strings.TrimSpace(web))
		if !ok || !isBareHost(ssh) || !isBareHost(web) {
			return nil, fmt.Errorf("%q is not written as ssh_host=web_host, with host names only", entry)
		}
		if _, seen := hosts[ssh]; seen {
			return nil, fmt.Errorf("%q names %s a second time", entry, ssh)
		}

		hosts[ssh] = web
	}

	return hosts, nil
}

// Forge is how komrad reads the review threads on the pull requests its agents open.
type Forge struct {
	// Logins whose review comments are left out of the threads mirrored onto an agent, such as a
	// CI bot or a code review App. A thread whose every comment is from one of them is not mirrored
	// at all. Names are matched without regard to case.
	BotNames []string `mapstructure:"bot_names"`
	// The largest check log komrad keeps, in bytes. A log past it is not kept, and the panel links to it
	// on the forge instead. A negative size is refused.
	MaxCheckLogBytes int64 `mapstructure:"max_check_log_bytes"`
}

// GetMaxCheckLogBytes is the largest check log komrad keeps. An unset size takes the default, and a
// negative one is an error rather than the default.
func (f Forge) GetMaxCheckLogBytes() (int64, error) {
	if f.MaxCheckLogBytes == 0 {
		f.MaxCheckLogBytes = defaultMaxCheckLogBytes
	}
	if f.MaxCheckLogBytes < 0 {
		return 0, fmt.Errorf("the check log size limit must be positive, not %d", f.MaxCheckLogBytes)
	}

	return f.MaxCheckLogBytes, nil
}

// isBareHost says whether a host carries no port, scheme, user or path.
func isBareHost(host string) bool {
	return host != "" && !strings.ContainsAny(host, ":/@= \t")
}

// GitHubApp is the App an engine fetches and pushes as.
type GitHubApp struct {
	// The App's own id, from its settings page. Left empty the engine has no GitHub identity
	// and git is left to whatever credentials the account already has, which on a machine
	// nobody logs into is none.
	AppID string `mapstructure:"app_id"`
	// The installation the token is minted for, which is what scopes it to the repositories the
	// App was installed on rather than to everything it could reach. GitHub writes it as a
	// number, and it is in the URL of the installation's own settings page.
	InstallationID string `mapstructure:"installation_id"`
	// The PEM the App was created with. On a deployed engine this is fetched into a tmpfs at
	// boot rather than written to disk, so the key does not outlive the machine.
	PrivateKeyPath string `mapstructure:"private_key_path"`
}

// Context is the standing text every agent is started with, above whatever its workflow
// adds. It is where what is true of your machine and how you want to be worked with goes,
// so it does not have to be pasted into every workflow.
type Context struct {
	// The file holding global context, read at each launch.
	// Left empty this is ~/.config/komrad/context.md, but will not trigger a failure if left blank.
	File string `mapstructure:"file"`
}

// Layout is how the panel divides its window between its own list, the details under that
// list, the stage beside them, and the info pane that opens under the stage. A size below
// one is read as a share of what it divides, and one at or above it as a count of cells.
type Layout struct {
	// Which side of the window the panel's sidebar sits on, "left" or "right".
	SidebarSide string `mapstructure:"sidebar_side"`
	// How many columns wide the panel's sidebar opens at. Dragging the divider afterwards moves it, and the panel follows.
	SidebarWidth int `mapstructure:"sidebar_width"`
	// Which side of the stage the info pane opens on, "below" or "above". That pane is where the server log and an editor are drawn.
	InfoSide string `mapstructure:"info_side"`
	// How tall the info pane opens, either a share of the stage's height below one or a whole number of rows.
	InfoHeight float64 `mapstructure:"info_height"`
	// How many lines the details under the list draw, either a share of the sidebar's height below one or a whole number of lines. The marks the hovered row is carrying are spelled out in that same room, ahead of the fields. The chrome around them is added on top, and the two together never take more than half the sidebar's height. Once the details hold the keys they grow past this to show the whole record, as far as the sidebar allows.
	DetailsHeight float64 `mapstructure:"details_height"`
	// How many families of agents the triage zone at the top of the agents list draws, the agents most in need of you, the most urgent first. A family is an agent with all of its children, and the zone never draws fewer rows than it has slots. Zero turns the zone off.
	TriageRows int `mapstructure:"triage_rows"`
}

// Harness is how one agent CLI is configured.
type Harness struct {
	// Flags appended to every agent's command line for this CLI, ahead of the flags that agent carries itself.
	Flags string `mapstructure:"flags"`
	// Whether to record each agent's directory as trusted in this CLI's own config
	// before launching it. The CLI asks whether a directory is trusted the first
	// time it runs there and waits for an answer, which it reads only from that
	// file, so an agent in a fresh worktree otherwise starts blocked and shows
	// here as waiting. Turning this on edits that file on your behalf.
	TrustLaunchDirs bool `mapstructure:"trust_launch_dirs"`
}

// Harnesses is the per-CLI configuration. There is a field per harness the binary
// implements rather than an open table, so a misspelled name is refused by the same
// unknown-key check as everything else here, and this file lists what can be run.
type Harnesses struct {
	Claude Harness `mapstructure:"claude"`
	Codex  Harness `mapstructure:"codex"`
}

// Usage is how much agents may run and how large their conversations may grow.
type Usage struct {
	// How many unarchived agents this engine may run at once, past which a create is refused.
	MaxAgents int `mapstructure:"max_agents"`
	// What an implementer, the agent that lands work on a branch of its own, may use.
	Implementer ImplementerUsage `mapstructure:"implementer"`
	// What a reviewer, the child that reads another agent's work, may use.
	Reviewer ReviewerUsage `mapstructure:"reviewer"`
	// What a workflow's orchestrator may use.
	Orchestrator OrchestratorUsage `mapstructure:"orchestrator"`
}

// ImplementerUsage is what one implementer may use.
type ImplementerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many children, such as its reviewer, one implementer may hold at once. Children share
	// their parent's checkout, so this is deliberately far lower than max_agents.
	MaxChildren int `mapstructure:"max_children"`
}

// ReviewerUsage is what one reviewer may use.
type ReviewerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
}

// OrchestratorUsage is what one orchestrator may use.
type OrchestratorUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many agents one orchestrator may have spawned and running at once, not counting their
	// children. 0 sets no limit of its own, leaving max_agents as the cap.
	MaxChildren int `mapstructure:"max_children"`
}

// newUsage is the usage limits a config that names none runs on.
func newUsage() Usage {
	return Usage{
		MaxAgents: defaultMaxAgents,
		Implementer: ImplementerUsage{
			AutoCompaction: defaultImplementerAutoCompaction,
			MaxChildren:    defaultMaxChildren,
		},
		Reviewer:     ReviewerUsage{AutoCompaction: defaultReviewerAutoCompaction},
		Orchestrator: OrchestratorUsage{AutoCompaction: defaultOrchestratorAutoCompaction},
	}
}

func (u *Usage) normalize() {
	if u.MaxAgents == 0 {
		u.MaxAgents = defaultMaxAgents
	}
	if u.Implementer.MaxChildren == 0 {
		u.Implementer.MaxChildren = defaultMaxChildren
	}
}

func (u Usage) validate() error {
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	limits := []struct {
		key   string
		value int
		unit  string
	}{
		{"usage.max_agents", u.MaxAgents, "a number of agents"},
		{"usage.implementer.max_children", u.Implementer.MaxChildren, "a number of agents"},
		{"usage.orchestrator.max_children", u.Orchestrator.MaxChildren, "a number of agents"},
		{"usage.implementer.auto_compaction", u.Implementer.AutoCompaction, "a number of tokens"},
		{"usage.reviewer.auto_compaction", u.Reviewer.AutoCompaction, "a number of tokens"},
		{"usage.orchestrator.auto_compaction", u.Orchestrator.AutoCompaction, "a number of tokens"},
	}
	for _, limit := range limits {
		if err := nonNegative(limit.key, limit.value, limit.unit); err != nil {
			return err
		}
	}

	return nil
}

func nonNegative(key string, value int, unit string) error {
	if value < 0 {
		return fmt.Errorf("%s: %d is not %s", key, value, unit)
	}

	return nil
}

// Mail is how long komrad keeps trying to hand mail to the agent it was addressed
// to. Delivery waits on the recipient: an agent that is busy gets its mail as its turn
// ends, and one that has stopped is nudged, so an agent nobody is running holds mail until
// it comes back or the deadline passes.
type Mail struct {
	// How long mail stays deliverable, as a Go duration such as "3h" or "45m". Mail past
	// it is neither delivered nor readable, and the row is kept as a record that it never
	// arrived. A duration komrad cannot read, or one that is not positive, is refused.
	DeliveryDeadline string `mapstructure:"delivery_deadline"`
	// How much mail two agents may write each other before a send between them is
	// refused. It counts the pair in both directions over every thread, so opening a new
	// subject is not a way around it. When a send is refused komrad marks a row for
	// attention and tells the recipient its mail was stopped, so an exchange that is cut
	// off does not leave the other side waiting on a reply that will never arrive. A
	// negative limit is refused.
	MaxExchange int `mapstructure:"max_exchange"`
}

// GetDeliveryDeadline is how long mail stays deliverable, parsed. A duration that will
// not parse is an error rather than the default, since a deadline somebody wrote and komrad
// quietly ignored is worse than one it refuses.
func (m Mail) GetDeliveryDeadline() (time.Duration, error) {
	if m.DeliveryDeadline == "" {
		m.DeliveryDeadline = defaultDeliveryDeadline
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(m.DeliveryDeadline))
	if err != nil {
		return 0, fmt.Errorf("unreadable delivery deadline %q: %w", m.DeliveryDeadline, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("the delivery deadline must be positive, not %q", m.DeliveryDeadline)
	}

	return parsed, nil
}

// GetMaxExchange is how much mail two agents may write each other. An unset limit takes the
// default; a negative one is an error rather than the default, for the same reason an
// unparseable deadline is: a config somebody wrote and komrad quietly ignored is worse than one
// it refuses.
func (m Mail) GetMaxExchange() (int, error) {
	if m.MaxExchange == 0 {
		m.MaxExchange = DefaultMaxExchange
	}
	if m.MaxExchange < 0 {
		return 0, fmt.Errorf("the mail exchange limit must be positive, not %d", m.MaxExchange)
	}

	return m.MaxExchange, nil
}

// Diff is how the panel draws a diff.
type Diff struct {
	// Wrap a line wider than the pane onto the lines under it rather than cutting it off. The wrap
	// key turns this off and on while reading.
	Wrap bool `mapstructure:"wrap"`
}

// Debug is what komrad records about its own reading of an agent.
type Debug struct {
	// Whether the pane capture behind each reading of an agent is kept under the cache
	// directory.
	Frames bool `mapstructure:"frames"`
	// How many frames are kept for one agent before the oldest is dropped. A number at or
	// below zero takes the default.
	FrameHistory int `mapstructure:"frame_history"`
}

// Log is how much the background service writes about itself. Where it writes is not
// configurable.
type Log struct {
	// How much is written, one of debug, info, warn, or error.
	Level string `mapstructure:"level"`
}

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow
	// names one. It has to be a harness this binary implements, so it is one of
	// the names under [harness] below.
	DefaultHarness string `mapstructure:"default_harness"`
	// Whether `komrad open` sets up a workflow for the git checkout it is run in, with a
	// worktree for each agent and an orchestrator standing by, when no workflow covers that
	// directory yet.
	AutoSetup bool      `mapstructure:"auto_setup"`
	Harness   Harnesses `mapstructure:"harness"`
	// How much agents may run and how large their conversations may grow.
	Usage Usage `mapstructure:"usage"`
	// Where the panel's sidebar, the stage beside it, and the info pane under the stage sit, and how much of the window each takes.
	Layout Layout `mapstructure:"layout"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Edit the panel's text fields the way vim does. A form's field opens in insert mode, esc goes
	// to normal mode, and a second esc closes it. The filter, search and rename lines close on esc,
	// and ctrl+f puts one in normal mode, after which esc works as it does in a form.
	VimMode bool `mapstructure:"vim_mode"`
	// How the panel draws a diff.
	Diff Diff `mapstructure:"diff"`
	// The icons the panel draws, grouped by where each one is drawn. Every icon is one character
	// from the approved set, the Nerd Fonts octicons and a few named characters. The chrome the
	// layout is drawn with, such as the status bars, dividers and rules, is fixed and not
	// configured here.
	Glyphs Glyphs `mapstructure:"glyphs"`
	// The colours the panel draws in, one for each part a colour plays. Each is a terminal colour
	// number from 0 to 255, which your terminal's theme decides the look of, or a hex code like
	// "#1e3a5f".
	Palette Palette `mapstructure:"palette"`
	// Where komrad finds the scripts a workflow hangs on a moment in its agents' lives.
	Hooks Hooks `mapstructure:"hooks"`
	// Where a workflow's generated worktree provisioner creates its worktrees.
	Worktrees Worktrees `mapstructure:"worktrees"`
	// How komrad reads the git checkouts its agents work in.
	Git Git `mapstructure:"git"`
	// How komrad reads the review threads on the pull requests its agents open.
	Forge Forge `mapstructure:"forge"`
	// The GitHub App an engine fetches and pushes as.
	GitHubApp GitHubApp `mapstructure:"github_app"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// How mail between agents is delivered.
	Mail Mail `mapstructure:"mail"`
	// The log the background service writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Puts komrad into debug mode which records additional information used to help diagnose issues.
	Debug Debug `mapstructure:"debug"`
	// Which komrad this is: the database, the address, the tmux socket, the cache.
	Connection Connection `mapstructure:"connection"`
}

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		Usage:          newUsage(),
		Layout:         DefaultLayout(),
		Keybindings:    DefaultKeybindings(),
		Glyphs:         defaultGlyphs(),
		Palette:        defaultPalette(),
		Forge:          Forge{MaxCheckLogBytes: defaultMaxCheckLogBytes},
		Mail:           Mail{DeliveryDeadline: defaultDeliveryDeadline, MaxExchange: DefaultMaxExchange},
		Log:            Log{Level: defaultLogLevel},
		Debug:          Debug{FrameHistory: DefaultFrameHistory},
		Diff:           Diff{Wrap: true},
		Connection:     defaultConnection(),
	}
}

// DefaultLayout is the layout used when the config omits it.
func DefaultLayout() Layout {
	return Layout{
		SidebarSide:   defaultSidebarSide,
		SidebarWidth:  DefaultSidebarWidth,
		InfoSide:      defaultInfoSide,
		InfoHeight:    DefaultInfoHeight,
		DetailsHeight: defaultDetailsHeight,
		TriageRows:    defaultTriageRows,
	}
}

func (l *Layout) normalize() {
	if l.SidebarWidth == 0 {
		l.SidebarWidth = DefaultSidebarWidth
	}
	if l.InfoHeight == 0 {
		l.InfoHeight = DefaultInfoHeight
	}
	if l.DetailsHeight == 0 {
		l.DetailsHeight = defaultDetailsHeight
	}
	if strings.TrimSpace(l.SidebarSide) == "" {
		l.SidebarSide = defaultSidebarSide
	}
	if strings.TrimSpace(l.InfoSide) == "" {
		l.InfoSide = defaultInfoSide
	}
}

// validate rejects a layout tmux could not lay the panes out at. Zero is how an omitted
// size reads, so it falls back to the default rather than being called a mistake.
func (l Layout) validate() error {
	if err := nonNegative("layout.sidebar_width", l.SidebarWidth, "a width"); err != nil {
		return err
	}
	if err := nonNegative("layout.triage_rows", l.TriageRows, "a number of families"); err != nil {
		return err
	}
	if err := validatePaneSize("layout.info_height", l.InfoHeight); err != nil {
		return err
	}
	if err := validatePaneSize("layout.details_height", l.DetailsHeight); err != nil {
		return err
	}
	if err := validateSide("layout.sidebar_side", l.SidebarSide, SidebarSideLeft, SidebarSideRight); err != nil {
		return err
	}

	return validateSide("layout.info_side", l.InfoSide, infoSideBelow, InfoSideAbove)
}

// validatePaneSize rejects a size that is neither a share of what it divides nor a whole
// number of cells, since tmux is given one or the other and 1.5 columns is neither.
func validatePaneSize(key string, size float64) error {
	switch {
	case size < 0:
		return fmt.Errorf("%s: %v is not a size", key, size)
	case size >= 1 && size != math.Trunc(size):
		return fmt.Errorf("%s: %v is neither a share below one nor a whole number of cells", key, size)
	}

	return nil
}

// validateSide rejects a side that names neither of the two a region can sit on.
func validateSide(key, side string, allowed ...string) error {
	trimmed := strings.TrimSpace(side)
	if trimmed == "" || slices.Contains(allowed, trimmed) {
		return nil
	}

	return fmt.Errorf("%s: %q is not one of %s", key, side, strings.Join(allowed, ", "))
}

// validate rejects the settings the panel could not run on, so a config that cannot work
// says so when it is read rather than when something tries to draw with it.
func (c Config) validate() error {
	sections := []func() error{
		c.Log.validate,
		c.Usage.validate,
		c.Keybindings.validate,
		c.Mail.validate,
		c.Git.validate,
		c.Glyphs.validate,
		c.Palette.validate,
		c.Connection.validate,
		c.Layout.validate,
	}
	for _, validate := range sections {
		if err := validate(); err != nil {
			return err
		}
	}

	return nil
}

// logLevels are the levels the service understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

// validate rejects the two mail settings the service reads at send time.
func (m Mail) validate() error {
	if _, err := m.GetDeliveryDeadline(); err != nil {
		return fmt.Errorf("mail.delivery_deadline: %w", err)
	}
	if _, err := m.GetMaxExchange(); err != nil {
		return fmt.Errorf("mail.max_exchange: %w", err)
	}

	return nil
}

func (g Git) validate() error {
	if _, err := g.GetSSHHosts(); err != nil {
		return fmt.Errorf("git.ssh_hosts: %w", err)
	}

	return nil
}

func (l *Log) normalize() {
	if strings.TrimSpace(l.Level) == "" {
		l.Level = defaultLogLevel
	}
}

// validate rejects a level the service could not act on.
func (l Log) validate() error {
	level := strings.TrimSpace(l.Level)
	if level == "" || slices.Contains(logLevels, strings.ToLower(level)) {
		return nil
	}

	return fmt.Errorf("log.level: %q is not one of %s", l.Level, strings.Join(logLevels, ", "))
}

func (d *Debug) normalize() {
	if d.FrameHistory <= 0 {
		d.FrameHistory = DefaultFrameHistory
	}
}

// normalize keeps the settings whose zero has historically meant "unset" on
// their built-in values. Collections are deliberately excluded: an explicit empty list
// clears a list supplied by a default or lower layer.
func (c *Config) normalize() {
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}

	c.Usage.normalize()
	c.Layout.normalize()
	c.Connection.normalize()
	c.Log.normalize()
	c.Debug.normalize()
}
