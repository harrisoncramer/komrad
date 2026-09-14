package ui

import (
	"errors"

	"weave/config"
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
	errAgentGone  = errors.New("the agent the menu was opened on has left the list")
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

	agentID := agent.ID

	return m, m.openForm(formState{
		kind:   formGit,
		form:   form,
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) { return m.updateGitMenu(msg, choice) },
		submit: confirmSubmit(func(m model) (model, tea.Cmd) { return m.runGitChoice(agentID, *choice) }),
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
		WithShowHelp(true).WithKeyMap(gitMenuKeyMap(form)).WithTheme(paint.FormTheme()), &choice
}

// gitMenuKeyMap is the form's own keys without the select's filter, which would otherwise take
// the letters the entries answer to.
func gitMenuKeyMap(keys config.FormKeys) *huh.KeyMap {
	km := multiFieldKeyMap(keys)
	km.Select.Filter.SetEnabled(false)

	return km
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
		label += " · " + entry.refusal
	}

	return label
}

// updateGitMenu answers an entry by its own letter, and hands huh every other message.
func (m model) updateGitMenu(msg tea.Msg, choice *gitChoice) (tea.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateForm(msg)
	}

	switch k := press.String(); {
	case isBound(k, m.keys.Git.Rebase):
		*choice = gitChoiceRebase

		return m.answerForm()
	case isBound(k, m.keys.Git.OpenPR):
		*choice = gitChoiceOpenPR

		return m.answerForm()
	default:
		return m.updateForm(msg)
	}
}

// runGitChoice does what the entry asked for, or says why this agent is not one to ask it of.
func (m model) runGitChoice(agentID string, choice gitChoice) (model, tea.Cmd) {
	// Read back rather than captured, so an entry acts on the agent as the list holds it now.
	item, ok := m.liveAgent(agentID)
	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m, nil
	}

	agent := item.agent

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
