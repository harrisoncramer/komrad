package eventprompts

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const promptsHeading = "Your workflow asks the following. Where it differs from what komrad says above, do what your workflow asks."

const promptsCut = "\n[the rest of your workflow's prompts did not fit]"

// Prompt is what a workflow asks of its agents when komrad sends them one event.
type Prompt struct {
	Event  string
	Prompt string
	Muted  bool
}

// Prompts is every prompt a workflow attaches to an event.
type Prompts []Prompt

// For is the prompt attached to an event, and the empty string when there is none.
func (p Prompts) For(event string) string {
	for _, prompt := range p {
		if prompt.Event == event {
			return prompt.Prompt
		}
	}

	return ""
}

// IsMuted says whether an event is kept from opening mail of its own.
func (p Prompts) IsMuted(event string) bool {
	return slices.ContainsFunc(p, func(prompt Prompt) bool { return prompt.Event == event && prompt.Muted })
}

// AreAllMuted says whether every one of events is muted, which is never true of no events.
func (p Prompts) AreAllMuted(events []string) bool {
	if len(events) == 0 {
		return false
	}

	return !slices.ContainsFunc(events, func(event string) bool { return !p.IsMuted(event) })
}

// Append adds the prompts of the unmuted events to body, cutting them short to keep the whole within limit characters.
func (p Prompts) Append(body string, events []string, limit int) string {
	var lines []string
	for _, event := range events {
		if p.IsMuted(event) {
			continue
		}
		if prompt := p.For(event); prompt != "" {
			lines = append(lines, event+": "+prompt)
		}
	}
	if len(lines) == 0 {
		return body
	}

	section := "\n\n" + promptsHeading + "\n\n" + strings.Join(lines, "\n\n")

	room := limit - utf8.RuneCountInString(body)
	if utf8.RuneCountInString(section) <= room {
		return body + section
	}

	kept := room - utf8.RuneCountInString(promptsCut)
	if kept <= 0 {
		return body
	}

	return body + string([]rune(section)[:kept]) + promptsCut
}
