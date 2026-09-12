package pane

import (
	"sync"
	"time"
)

// draft is what an agent's input box was seen holding, and when it was first seen holding it.
type draft struct {
	text   string
	since  time.Time
	warned bool
}

// drafts is when each agent's input box was first seen holding the text it holds now.
type drafts struct {
	mu    sync.Mutex
	boxes map[string]draft
}

// since is when the box was first seen holding this text, and now for text that has moved.
func (d *drafts) since(agentID, text string, now time.Time) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.boxes == nil {
		d.boxes = map[string]draft{}
	}

	held, ok := d.boxes[agentID]
	if !ok || held.text != text {
		d.boxes[agentID] = draft{text: text, since: now}

		return now
	}

	return held.since
}

// claimWarning reports whether nothing has been said yet about the text the box holds now.
func (d *drafts) claimWarning(agentID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	held, ok := d.boxes[agentID]
	if !ok || held.warned {
		return false
	}

	held.warned = true
	d.boxes[agentID] = held

	return true
}

// forget drops what was remembered of an agent's box.
func (d *drafts) forget(agentID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.boxes, agentID)
}
