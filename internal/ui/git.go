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
	gitChoiceCancel
)

var (
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
		rebaseable: m.isRebaseable(agent),
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
			huh.NewOption("Cancel", gitChoiceCancel),
		)

	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(true).WithKeyMap(gitMenuKeyMap(form)).WithTheme(paint.Panel.FormTheme()), &choice
}

// gitMenuKeyMap is the form's own keys without the select's filter.
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
	default:
		return m.updateForm(msg)
	}
}

// runGitChoice does what the entry asked for, or says why this agent is not one to ask it of.
func (m model) runGitChoice(agentID string, choice gitChoice) (model, tea.Cmd) {
	// The list may have moved under the open menu.
	item, ok := m.liveAgent(agentID)
	if !ok {
		m.report("Could not act on the agent", errAgentGone)

		return m, nil
	}

	agent := item.agent

	switch choice {
	case gitChoiceRebase:
		if !m.isRebaseable(agent) {
			m.report("Could not rebase the worktree of "+agent.Name, errNoWorktree)

			return m, nil
		}

		return m, m.planRefreshCmd(agent.ID, agent.Name)
	case gitChoiceCancel:
		return m, nil
	}

	// The switch carries every entry and no default, so this is what an entry added without an arm
	// compiles to until the exhaustive check refuses it.
	return m, nil
}
