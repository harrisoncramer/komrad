package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type shot struct {
	label string
	frame frame
}

type castHeader struct {
	Version int    `json:"version"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Title   string `json:"title"`
}

type writeCastParams struct {
	title string
	cols  int
	pause time.Duration
	shots []shot
}

func writeCast(writer io.Writer, params writeCastParams) error {
	rows := 0
	for _, shot := range params.shots {
		rows = max(rows, len(shot.frame.lines))
	}

	encoder := json.NewEncoder(writer)
	if err := encoder.Encode(castHeader{
		Version: 2,
		Width:   params.cols,
		Height:  rows,
		Title:   params.title,
	}); err != nil {
		return fmt.Errorf("failed to write cast header: %w", err)
	}

	var last frame
	for i, shot := range params.shots {
		seconds := (time.Duration(i) * params.pause).Seconds()

		if err := encoder.Encode([]any{seconds, "m", shot.label}); err != nil {
			return fmt.Errorf("failed to write cast marker: %w", err)
		}

		if i > 0 && shot.frame.equals(last) {
			continue
		}

		if err := encoder.Encode([]any{seconds, "o", shot.frame.screen()}); err != nil {
			return fmt.Errorf("failed to write cast frame: %w", err)
		}

		last = shot.frame
	}

	return nil
}
