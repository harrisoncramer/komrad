package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

type stepKind int

const (
	stepKey stepKind = iota
	stepType
	stepSleep
	stepWait
)

type step struct {
	kind  stepKind
	arg   string
	pause time.Duration
	line  int
	text  string
}

func parseScript(reader io.Reader) ([]step, error) {
	var steps []step

	scanner := bufio.NewScanner(reader)
	for number := 1; scanner.Scan(); number++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		parsed, err := parseStep(text)
		if err != nil {
			return nil, fmt.Errorf("%w at line %d", err, number)
		}

		parsed.line = number
		parsed.text = text
		steps = append(steps, parsed)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read script: %w", err)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("the script has no steps")
	}

	return steps, nil
}

func parseStep(text string) (step, error) {
	word, rest, _ := strings.Cut(text, " ")
	rest = strings.TrimSpace(rest)

	switch word {
	case "key":
		if rest == "" {
			return step{}, fmt.Errorf("key needs a tmux key name")
		}

		return step{kind: stepKey, arg: rest}, nil
	case "type":
		if rest == "" {
			return step{}, fmt.Errorf("type needs text")
		}

		return step{kind: stepType, arg: rest}, nil
	case "wait":
		if rest == "" {
			return step{}, fmt.Errorf("wait needs text to wait for")
		}

		return step{kind: stepWait, arg: rest}, nil
	case "sleep":
		pause, err := time.ParseDuration(rest)
		if err != nil {
			return step{}, fmt.Errorf("%w: sleep needs a duration such as 500ms", err)
		}

		if pause <= 0 {
			return step{}, fmt.Errorf("sleep needs a duration above zero")
		}

		return step{kind: stepSleep, pause: pause}, nil
	default:
		return step{}, fmt.Errorf("unknown step %q, expected key, type, sleep or wait", word)
	}
}
