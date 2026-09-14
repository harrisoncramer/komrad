package ui

import (
	"errors"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/ui/paint"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// gitChoice is one entry of the git menu.
type gitChoice int

const (
	gitChoiceRebase gitChoice = iota
	gitChoiceOpenPR
	gitChoiceCancel
)

var (
	errNoPR       = errors.New("nothing has recorded a pull request on this agent yet")
	errNoWorktree = errors.New("this agent has no worktree of its own, so there is no base to move it onto")
)

// openGitMenu draws the git actions beside the list, for the agent the cursor is on.
func (m model) openGitMenu() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	agent := item.agent
	form, choice := gitMenuForm(gitMenu{
		name:       agent.Name,
		rebaseable: m.isReviewable(agent),
		hasPR:      hasPR(agent),
	}, m.keys.Git, formKeys())

	return m, m.openForm(formState{
		kind:   formGit,
		form:   form,
		update: model.updateGitMenu,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) { return m.runGitChoice(agent, *choice) }),
	})
}

// gitMenu is what the menu is drawn from: who it is about, and which of its entries would do
// anything.
type gitMenu struct {
	name       string
	rebaseable bool
	hasPR      bool
}

func gitMenuForm(menu gitMenu, keys config.GitKeys, form config.FormKeys) (*huh.Form, *gitChoice) {
	// huh scrolls its list to whichever entry the cursor starts on.
	choice := gitChoiceRebase

	field := huh.NewSelect[gitChoice]().
		Key("git").
		Title("Git on "+menu.name).
		Value(&choice).
		Options(
			huh.NewOption(gitEntryLabel(gitEntry{
				name:    "Rebase onto the base",
				keys:    keys.Rebase,
				refusal: "no worktree of its own",
				held:    menu.rebaseable,
			}), gitChoiceRebase),
			huh.NewOption(gitEntryLabel(gitEntry{
				name:    "Open the pull request",
				keys:    keys.OpenPR,
				refusal: "none recorded yet",
				held:    menu.hasPR,
			}), gitChoiceOpenPR),
			huh.NewOption("Cancel", gitChoiceCancel),
		)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(true).WithKeyMap(multiFieldKeyMap(form)).WithTheme(paint.FormTheme()), &choice
}

// gitEntry is one row of the menu: what it does, the key that picks it, and its refusal.
type gitEntry struct {
	name    string
	keys    []string
	refusal string
	held    bool
}

// gitEntryLabel names an entry, the key that picks it, and why it would do nothing here.
func gitEntryLabel(entry gitEntry) string {
	label := entry.name

	if key := headLabel(entry.keys); key != "" {
		label += " (" + key + ")"
	}

	if !entry.held {
		label += ", " + entry.refusal
	}

	return label
}

// updateGitMenu reads the mnemonic of each entry before huh is given the message.
func (m model) updateGitMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateForm(msg)
	}

	switch k := press.String(); {
	case isBound(k, m.keys.Git.Rebase):
		return m.pickGit(gitChoiceRebase)
	case isBound(k, m.keys.Git.OpenPR):
		return m.pickGit(gitChoiceOpenPR)
	default:
		return m.updateForm(msg)
	}
}

// pickGit answers the menu with an entry the cursor never had to reach.
func (m model) pickGit(choice gitChoice) (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m.closeForm()
	}

	return m.closeAfter(func(m model) (model, tea.Cmd) { return m.runGitChoice(item.agent, choice) })
}

// closeAfter runs what an entry asked for and takes the menu down as it goes.
func (m model) closeAfter(write func(model) (model, tea.Cmd)) (tea.Model, tea.Cmd) {
	return confirmSubmit(write)(m)
}

// runGitChoice does what the entry asked for, or says why this agent is not one to ask it of.
func (m model) runGitChoice(agent agentstore.Agent, choice gitChoice) (model, tea.Cmd) {
	switch choice {
	case gitChoiceRebase:
		if !m.isReviewable(agent) {
			m.report("Could not rebase the worktree of "+agent.Name, errNoWorktree)

			return m, nil
		}

		return m, m.planRefreshCmd(agent.ID, agent.Name)
	case gitChoiceOpenPR:
		if !hasPR(agent) {
			m.report("Could not open the pull request of "+agent.Name, errNoPR)

			return m, nil
		}

		agentID := agent.ID

		return m, m.writeCmd(
			"Could not open the pull request",
			func(a api) error { return a.openPullRequest(agentID) },
		)
	case gitChoiceCancel:
		return m, nil
	}

	// The switch carries every entry and no default, so this is what an entry added without an arm
	// compiles to until the exhaustive check refuses it.
	return m, nil
}
