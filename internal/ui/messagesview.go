package ui

import (
	"strconv"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/reader"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
)

func (m *model) renderMessagesFlash(width int) (string, bool) {
	if !m.isMessagesFlashing() || width < 3 {
		return "", false
	}

	held := m.messages.items[0]
	words := lipgloss.NewStyle()
	if m.isMessagesPulseOn() {
		words = words.Foreground(paint.Panel.Color(pulseTone(held.kind)))
	}

	return paint.Panel.Style(kindTone(held.kind)).Render(kindIcon(m.glyphs, held.kind)) + " " + words.Render(paint.Clip(held.summary, width-2)), true
}

func (m *model) renderMessagesTally() string {
	worst, count := screen.Info, 0
	for _, held := range m.messages.items {
		if held.kind < screen.Warning {
			continue
		}

		worst = max(worst, held.kind)
		count++
	}

	if count == 0 {
		return ""
	}

	tally := paint.Panel.Style(kindTone(worst)).Render(kindIcon(m.glyphs, worst) + " " + strconv.Itoa(count))

	return tally + "  " + paint.Panel.Style(paint.ToneMuted).Render(keys.HeadLabel(m.keys.Messages.Open))
}

func (m *model) renderMessagesWindow(width, rows int) reader.Page {
	body := max(width-messagesBodyIndent, 1)
	count := len(m.messages.items)
	at := m.messages.clampedCursor()

	rendered := []string{m.renderMessagesHeading(width, count, at), ""}
	if count == 0 {
		rendered = append(rendered, paint.Panel.Style(paint.ToneMuted).Render(strings.Repeat(" ", messagesIndent)+"Komrad is running normally"))
	}

	start, end := 0, 0
	for i := range m.messages.items {
		if i > 0 {
			rendered = append(rendered, "")
		}

		if i == at {
			start = len(rendered)
		}

		rendered = append(rendered, xslice.Map(m.renderMessageRows(i, body), func(row string) string {
			return m.markRow(zoneMessage, width, strconv.Itoa(i), row)
		})...)

		if i == at {
			end = len(rendered)
		}
	}

	doc := reader.Page{Rows: rendered, Markers: true}
	shape := doc.Shape()
	shape.Top = shape.Reveal(max(end-1, 0), rows)
	doc.Top = shape.Reveal(start, rows)

	return doc
}

func (m *model) renderMessagesHeading(width, count, at int) string {
	title := paint.Panel.Header().Render("messages")
	if count == 0 {
		return title
	}

	tally := label.OfCount(at+1, count)
	pad := max(width-lipgloss.Width(title)-len(tally), 1)

	return title + strings.Repeat(" ", pad) + paint.Panel.Style(paint.ToneMuted).Render(tally)
}

func (m *model) renderMessageRows(at, width int) []string {
	held := m.messages.items[at]
	hovered := at == m.messages.clampedCursor()
	flashed := held.landed == m.messages.flashed
	summary := lipgloss.NewStyle()
	switch {
	case flashed:
		summary = flashFieldStyle(paint.Panel)
	case hovered:
		summary = summary.Bold(true)
	}

	indent := strings.Repeat(" ", messagesBodyIndent)
	icon := paint.Panel.Style(kindTone(held.kind)).Render(kindIcon(m.glyphs, held.kind))

	var rows []string
	for i, line := range paint.WrapRows(held.summary, width) {
		if i == 0 {
			rows = append(rows, m.messagesCursorPrefix(hovered)+icon+" "+summary.Render(line))

			continue
		}

		rows = append(rows, indent+summary.Render(line))
	}

	meta := []string{kindLabel(held.kind), label.Friendly(held.at)}
	if held.trace != "" {
		meta = append(meta, "komrad log "+held.trace)
	}

	for _, piece := range []struct {
		text  string
		style lipgloss.Style
	}{
		{text: held.detail, style: lipgloss.NewStyle()},
		{text: held.hint, style: paint.Panel.Style(paint.ToneMuted)},
		{text: strings.Join(meta, separated), style: paint.Panel.Style(paint.ToneMuted)},
	} {
		if piece.text == "" {
			continue
		}

		style := piece.style
		if flashed {
			style = flashFieldStyle(paint.Panel)
		}

		rows = append(rows, xslice.Map(paint.WrapRows(piece.text, width), func(line string) string {
			return indent + style.Render(line)
		})...)
	}

	return rows
}

// messagesCursorPrefix is the gutter the cursor stands in, which is the record's.
func (m *model) messagesCursorPrefix(hovered bool) string {
	if !hovered {
		return strings.Repeat(" ", messagesIndent)
	}

	return list.RenderArrow()
}

func kindIcon(glyphs glyph.Set, kind screen.Kind) string {
	switch kind {
	case screen.Error:
		return glyphs.Spell(glyph.MessageError)
	case screen.Warning:
		return glyphs.Spell(glyph.MessageWarning)
	case screen.Success:
		return glyphs.Spell(glyph.MessageSuccess)
	case screen.Info:
		return glyphs.Spell(glyph.MessageInfo)
	}

	return glyphs.Spell(glyph.MessageInfo)
}

func kindLabel(kind screen.Kind) string {
	switch kind {
	case screen.Error:
		return "error"
	case screen.Warning:
		return "warning"
	case screen.Success:
		return "done"
	case screen.Info:
		return "info"
	}

	return "info"
}

func kindTone(kind screen.Kind) paint.Tone {
	switch kind {
	case screen.Error:
		return paint.ToneDanger
	case screen.Warning:
		return paint.ToneWarning
	case screen.Success:
		return paint.ToneSuccess
	case screen.Info:
		return paint.ToneMuted
	}

	return paint.ToneMuted
}

// pulseTone is the colour a landing message's words blink in.
func pulseTone(kind screen.Kind) paint.Tone {
	if kind == screen.Info {
		return paint.TonePrimary
	}

	return kindTone(kind)
}
