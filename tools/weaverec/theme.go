package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

var themeSlots = []string{
	"primary.background", "primary.foreground",
	"normal.black", "normal.red", "normal.green", "normal.yellow",
	"normal.blue", "normal.magenta", "normal.cyan", "normal.white",
	"bright.black", "bright.red", "bright.green", "bright.yellow",
	"bright.blue", "bright.magenta", "bright.cyan", "bright.white",
}

func parseAlacrittyTheme(reader io.Reader) (string, error) {
	colours := map[string]string{}
	section := ""

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")

			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found || !strings.HasPrefix(section, "colors.") {
			continue
		}

		value = strings.Trim(strings.TrimSpace(value), `"'`)
		value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "#")
		colours[strings.TrimPrefix(section, "colors.")+"."+strings.TrimSpace(key)] = value
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read the alacritty config: %w", err)
	}

	parts := make([]string, 0, len(themeSlots))
	for _, slot := range themeSlots {
		colour, ok := colours[slot]
		if !ok || len(colour) != 6 {
			return "", fmt.Errorf("the alacritty config has no usable colors.%s", slot)
		}

		parts = append(parts, colour)
	}

	return strings.Join(parts, ","), nil
}
