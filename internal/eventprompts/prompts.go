package eventprompts

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/internal/fail"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/protoenum"
	eventv1 "komrad/proto/gen/event/v1"
)

const promptsHeading = "Your workflow asks the following. Where it differs from what komrad says above, do what your workflow asks."

const promptsCut = "\n[the rest of your workflow's prompts did not fit]"

var ErrUnknownEventPrompt = fail.InvalidArgument("a prompt must name an event komrad sends")

var ErrDuplicateEventPrompt = fail.InvalidArgument("a workflow holds one prompt per event")

var ErrPromptTooLong = fail.InvalidArgument(fmt.Sprintf("a prompt runs to at most %d characters", MaxPrompt))

// Prompts is every prompt a workflow attaches to an event.
type Prompts []workflowstore.EventPrompt

// Normalize trims what a caller sent, drops the entries that neither prompt nor mute, and
// refuses an event komrad does not send or one named twice.
func Normalize(prompts Prompts) (Prompts, error) {
	kept := make(Prompts, 0, len(prompts))
	for _, prompt := range prompts {
		prompt.Prompt = strings.TrimSpace(prompt.Prompt)
		if prompt.Prompt == "" && !prompt.Muted {
			continue
		}
		if !slices.Contains(protoenum.Values[eventv1.Event](), prompt.Event) {
			return nil, fmt.Errorf("%w: %s is not one of %s", ErrUnknownEventPrompt, protoenum.Name(prompt.Event), strings.Join(protoenum.Names(protoenum.Values[eventv1.Event]()), ", "))
		}
		if slices.ContainsFunc(kept, func(k workflowstore.EventPrompt) bool { return k.Event == prompt.Event }) {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateEventPrompt, protoenum.Name(prompt.Event))
		}
		if utf8.RuneCountInString(prompt.Prompt) > MaxPrompt {
			return nil, fmt.Errorf("%w: %s runs to %d characters", ErrPromptTooLong, protoenum.Name(prompt.Event), utf8.RuneCountInString(prompt.Prompt))
		}

		kept = append(kept, prompt)
	}

	return kept, nil
}

// Get is the prompt attached to an event, and the empty string when there is none.
func (p Prompts) Get(event eventv1.Event) string {
	for _, prompt := range p {
		if prompt.Event == event {
			return prompt.Prompt
		}
	}

	return ""
}

// isMuted says whether an event is kept from opening mail of its own.
func (p Prompts) isMuted(event eventv1.Event) bool {
	return slices.ContainsFunc(p, func(prompt workflowstore.EventPrompt) bool { return prompt.Event == event && prompt.Muted })
}

// AreAllMuted says whether every one of events is muted, which is never true of no events.
func (p Prompts) AreAllMuted(events []eventv1.Event) bool {
	if len(events) == 0 {
		return false
	}

	return !slices.ContainsFunc(events, func(event eventv1.Event) bool { return !p.isMuted(event) })
}

// Append adds the prompts of the unmuted events to body, cutting them short to keep the whole within limit characters.
func (p Prompts) Append(body string, events []eventv1.Event, limit int) string {
	var lines []string
	for _, event := range events {
		if p.isMuted(event) {
			continue
		}
		if prompt := p.Get(event); prompt != "" {
			lines = append(lines, protoenum.Name(event)+": "+prompt)
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
