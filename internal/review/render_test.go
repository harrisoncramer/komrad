package review

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestAHarvestedThreadIsNamedByItsEdit is what keeps a sidebar full of harvested comments legible,
// since every one of them carries the same body.
func TestAHarvestedThreadIsNamedByItsEdit(t *testing.T) {
	t.Parallel()

	harvested := &agentv1.ReviewThread{
		Harvested: true,
		Messages: []*agentv1.ReviewMessage{{
			Body:       "The person reviewing wrote this edit themselves.",
			Suggestion: []string{"\treturn nil, err", "}"},
		}},
	}

	said := threadSummary(harvested)

	assert.Contains(t, said, "return nil, err", "the edit is what tells one apart from the next")
	assert.Contains(t, said, "+1 more")
	assert.NotContains(t, said, "The person reviewing")
}

// TestAThreadSomebodyTypedIsNamedByItsBody pins that the harvested case did not take over the
// ordinary one.
func TestAThreadSomebodyTypedIsNamedByItsBody(t *testing.T) {
	t.Parallel()

	typed := &agentv1.ReviewThread{
		Messages: []*agentv1.ReviewMessage{{Body: "this needs a nil check"}},
	}

	assert.Equal(t, "this needs a nil check", threadSummary(typed))
}
