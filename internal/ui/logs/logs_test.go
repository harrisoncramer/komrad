package logs_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	eventv1 "komrad/proto/gen/event/v1"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// TestLogsOpen covers the screen opening from the subpages menu on the agent's log sources.
func TestLogsOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu lists the sources and opens the newest lines of the server log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())

		frame := harness.Frame()
		assert.Equal(t, screen.Logs, paneltest.Screen(harness.Frame()))
		assert.True(t, harness.Tmux().IsZoomed(), "the screen holds the whole window")
		assert.Contains(t, frame, "agents › reviewer › logs")
		assert.Contains(t, paneltest.HoveredRow(frame), "server")
		assert.Contains(t, paneltest.SidebarProse(frame), "001 agent.provision setup")
		assert.Contains(t, paneltest.StageText(frame), "first line")
		assert.Contains(t, paneltest.StageText(frame), "second line")
		harness.AssertSentTo(logv1connect.LogServiceListAgentLogSourcesProcedure, &logv1.ListAgentLogSourcesRequest{AgentId: "reviewer"})
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""))
	})

	t.Run("an orchestrator's logs are under its workflow's name", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildLogAgents()...)
		harness.MoveToRow("komrad")
		harness.Press("space", "l")

		assert.Equal(t, screen.Logs, paneltest.Screen(harness.Frame()))
		assert.Contains(t, harness.Frame(), "agents › komrad › logs")
	})

	t.Run("an agent with a failed run opens on it", func(t *testing.T) {
		t.Parallel()

		rows := buildLogAgents()
		reviewer, _ := rows[2].(*agentv1.Agent)
		reviewer.FailedRunLog = xptr.Ptr[int32](1)
		harness := openLogs(t, tailOfTwo(), rows...)

		frame := harness.Frame()
		assert.Contains(t, paneltest.HoveredRow(frame), "001 agent.provision setup \uf489")
		assert.Contains(t, paneltest.StageText(frame), "error: port 5470 already allocated")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(provisionRun, ""))
	})

	t.Run("a log with nothing about the agent says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})

		assert.Contains(t, paneltest.StageText(harness.Frame()), "Nothing about this agent in the last 64 MB of the log.")
	})

	t.Run("a run that printed nothing says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})
		harness.Press("j", paneltest.Rest)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "This run wrote nothing.")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("esc")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "reviewer")
	})
}

// TestLogsFailedRunRead covers opening the run that failed claiming the agent's unread failure.
func TestLogsFailedRunRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		failedRun *int32
		presses   []string
		want      []proto.Message
	}{
		{
			name:    "opening a run that did not fail sends nothing",
			presses: []string{"j", paneltest.Rest},
		},
		{
			name:      "opening the failed run marks the agent's runs read",
			failedRun: xptr.Ptr[int32](1),
			want:      []proto.Message{&agentv1.MarkRunsReadRequest{Id: "reviewer"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := buildLogAgents()
			reviewer, _ := rows[2].(*agentv1.Agent)
			reviewer.FailedRunLog = tt.failedRun
			harness := openLogs(t, tailOfTwo(), rows...)
			harness.Press(tt.presses...)

			harness.AssertSentTo(agentv1connect.AgentServiceMarkRunsReadProcedure, tt.want...)
		})
	}
}

// TestLogsFormat covers the server log rendered compact or raw from the format strip.
func TestLogsFormat(t *testing.T) {
	t.Parallel()

	t.Run("the server log opens compact under its strip", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())

		frame := harness.Frame()
		assert.Contains(t, frame, "raw  compact")
		assert.Contains(t, paneltest.StageText(frame), "first line")
		assert.NotContains(t, paneltest.StageText(frame), `{"body"`)
	})

	t.Run("the strip steps the server log to raw", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("F", "tab", "esc")

		assert.Contains(t, paneltest.StageText(harness.Frame()), `{"body":"first line"}`)
	})

	t.Run("a run renders no strip and its output as it was written", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", paneltest.Rest)

		frame := harness.Frame()
		assert.NotContains(t, frame, "raw  compact")
		assert.Contains(t, paneltest.StageText(frame), "error: port 5470 already allocated")
	})
}

// TestLogsSources covers moving between sources, each rendered on its own from its newest lines.
func TestLogsSources(t *testing.T) {
	t.Parallel()

	t.Run("moving to another source renders its lines alone", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", paneltest.Rest)

		stage := paneltest.StageText(harness.Frame())
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "001 agent.provision setup")
		assert.Contains(t, stage, "error: port 5470 already allocated")
		assert.NotContains(t, stage, "first line")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead(provisionRun, ""))
	})

	t.Run("moving back reads the source from its newest again", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", paneltest.Rest, "k", paneltest.Rest)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "second line")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead(provisionRun, ""), logRead("server", ""))
	})

	t.Run("a source passed on the way to another is never read", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", "k", paneltest.Rest)

		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""))
	})

	t.Run("a key that leaves the cursor where it was reads nothing again", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("g", "g")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "second line")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""))
	})

	t.Run("a newer run listed on a tick leaves the hovered source as it was", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", paneltest.Rest)
		answerSources(harness, serverSource, launchSource, provisionSource)
		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.Contains(t, paneltest.SidebarProse(frame), "002 agent.pre-launch setup")
		assert.Contains(t, paneltest.StageText(frame), "error: port 5470 already allocated")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead(provisionRun, ""), logRead(provisionRun, "35"))
	})

	t.Run("a hovered run gone from the listing moves to its neighbour and reads it", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("j", paneltest.Rest)
		answerSources(harness, serverSource)
		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.NotContains(t, paneltest.SidebarProse(frame), "001 agent.provision setup")
		assert.Contains(t, paneltest.StageText(frame), "second line")
		assert.NotContains(t, paneltest.StageText(frame), "error: port")
		sent := harness.Sent(logv1connect.LogServiceReadAgentLogsProcedure)
		assert.True(t, proto.Equal(logRead("server", ""), sent[len(sent)-1]), "the neighbour is read from its newest")
	})

	t.Run("a hovered run gone from the listing hands the keys back to the list", func(t *testing.T) {
		t.Parallel()

		reads := tailOfTwo()
		reads[launchSource.GetId()+"@"] = &logv1.ReadAgentLogsResponse{Lines: []string{"launching"}, Cursor: "9"}
		harness := openLogs(t, reads)
		harness.Press("j", paneltest.Rest, "pick:block")
		answerSources(harness, serverSource, launchSource)
		harness.Advance(time.Second)
		harness.Press("k", paneltest.Rest)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "second line")
	})

	t.Run("a read of the source moved off is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := paneltest.OpenAgents(t, buildLogAgents()...)
		tailOfTwo().answer(harness)
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")
		harness.Hold(read)
		harness.Advance(time.Second)
		harness.Press("j", paneltest.Rest)

		harness.Release(read)

		assert.NotContains(t, paneltest.StageText(harness.Frame()), "third line")
	})
}

// TestLogsFilter covers searching and filtering the sources, and the log following the cursor to
// the source it lands on.
func TestLogsFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		presses     []string
		wantStage   string
		notSidebar  string
		wantSidebar string
	}{
		{
			name:      "a search lands on the source it matches and reads it",
			presses:   []string{"/", "p", "r", "o", "v", "enter"},
			wantStage: "error: port 5470 already allocated",
		},
		{
			name:       "a filter leaves only the sources it matches",
			presses:    []string{"/", "tab", "p", "r", "o", "v", "enter"},
			wantStage:  "error: port 5470 already allocated",
			notSidebar: "server",
		},
		{
			name:        "a filter nothing matches says so",
			presses:     []string{"/", "tab", "z", "z", "z", "enter"},
			wantSidebar: "nothing matches",
		},
		{
			name:        "clearing the filter goes back to the source it opened on",
			presses:     []string{"/", "tab", "p", "r", "o", "v", "enter", "esc"},
			wantStage:   "second line",
			wantSidebar: "001 agent.provision setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, tailOfTwo())
			harness.Hold(logv1connect.LogServiceListAgentLogSourcesProcedure)
			harness.Press(tt.presses...)
			harness.Advance(time.Second)

			frame := harness.Frame()
			if tt.wantStage != "" {
				assert.Contains(t, paneltest.StageText(frame), tt.wantStage)
			}
			if tt.wantSidebar != "" {
				assert.Contains(t, paneltest.SidebarProse(frame), tt.wantSidebar)
			}
			if tt.notSidebar != "" {
				assert.NotContains(t, paneltest.SidebarProse(frame), tt.notSidebar)
			}
		})
	}

	t.Run("a filtered run gone from the listing moves to the neighbour the filter shows", func(t *testing.T) {
		t.Parallel()

		reads := tailOfTwo()
		reads[launchSource.GetId()+"@"] = &logv1.ReadAgentLogsResponse{Lines: []string{"launching"}, Cursor: "9"}
		harness := openLogs(t, reads)
		answerSources(harness, serverSource, launchSource, provisionSource)
		harness.Advance(time.Second)
		harness.Press("/", "tab", "s", "e", "t", "u", "p", "enter", "j")
		harness.Advance(time.Second)
		answerSources(harness, serverSource, launchSource)
		harness.Advance(time.Second)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "launching")
	})
}

// TestLogsTail covers the reads after the first, each from where the last left off.
func TestLogsTail(t *testing.T) {
	t.Parallel()

	t.Run("the next read carries the cursor and appends what is new", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Advance(time.Second)

		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead("server", "10"))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "third line")
	})

	t.Run("a log that started over clears what was held", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"server@": {Lines: []string{`{"body":"first line"}`}, Cursor: "10"},
			"server@10": {
				Lines:  []string{`{"body":"fresh line"}`},
				Cursor: "4",
				Reset_: true,
			},
		})
		harness.Advance(time.Second)

		stage := paneltest.StageText(harness.Frame())
		assert.Contains(t, stage, "fresh line")
		assert.NotContains(t, stage, "first line")
	})

	t.Run("a read with more behind it reads again at once", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"server@": {
				Lines:  []string{`{"body":"first line"}`},
				Cursor: "10",
				More:   true,
			},
			"server@10": {Lines: []string{`{"body":"second line"}`}, Cursor: "20"},
		})

		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead("server", "10"))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "second line")
	})

	t.Run("a read answered after the screen was left and opened again is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := paneltest.OpenAgents(t, buildLogAgents()...)
		logReads{}.answer(harness)
		harness.Answer(read, func(req connect.AnyRequest) (connect.AnyResponse, error) {
			sent, _ := req.Any().(*logv1.ReadAgentLogsRequest)
			if slices.Contains(sent.GetAgentIds(), "reviewer") {
				return connect.NewResponse(&logv1.ReadAgentLogsResponse{
					Lines:  []string{`{"body":"first line"}`},
					Cursor: "10",
					More:   true,
				}), nil
			}

			return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: "30"}), nil
		})
		harness.Hold(read)
		harness.MoveToRow("reviewer")
		harness.Press("space", "l", "esc")
		harness.MoveToRow("quiet")
		harness.Press("space", "l")

		harness.Release(read)
		harness.Release(read)

		quiet := &logv1.ReadAgentLogsRequest{
			AgentIds: []string{"quiet"},
			Source:   "server",
			Limit:    500,
		}
		assert.NotContains(t, paneltest.StageText(harness.Frame()), "first line")
		harness.AssertSentTo(read, logRead("server", ""), quiet)
	})

	t.Run("leaving the screen ends the reads", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.Press("esc")
		harness.Advance(time.Second)

		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""))
	})

	t.Run("a failed read says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildLogAgents()...)
		logReads{}.answer(harness)
		harness.Refuse(logv1connect.LogServiceReadAgentLogsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the log is gone")))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")
		harness.Advance(time.Second)

		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("server", ""), logRead("server", ""))
		assert.Contains(t, paneltest.StageText(harness.Frame()), "could not read: the log is gone")
	})

	t.Run("a failed listing says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildLogAgents()...)
		harness.Refuse(logv1connect.LogServiceListAgentLogSourcesProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the engine is gone")))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")
		harness.Advance(time.Second)

		assert.Len(t, harness.Sent(logv1connect.LogServiceListAgentLogSourcesProcedure), 2)
		assert.Contains(t, paneltest.SidebarProse(harness.Frame()), "could not list: the engine is gone")
	})
}

// TestLogsScroll covers the picker handing the keys to the log, its cursor moving through it,
// and the wheel scrolling it.
func TestLogsScroll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    string
		notWant string
	}{
		{
			name:    "the list holding the keys renders no cursor in the log",
			want:    "  entry 100",
			notWant: "›",
		},
		{
			name:    "the picker puts the cursor on the newest line",
			presses: []string{"pick:block"},
			want:    "› entry 100",
		},
		{
			name:    "up in the log moves the cursor back a line",
			presses: []string{"pick:block", "k"},
			want:    "› entry 099",
			notWant: "› entry 100",
		},
		{
			name:    "top in the log moves the cursor to its oldest line",
			presses: []string{"pick:block", "g", "g"},
			want:    "› entry 001",
			notWant: "entry 100",
		},
		{
			name:    "bottom in the log moves the cursor to the newest line",
			presses: []string{"pick:block", "g", "g", "G"},
			want:    "› entry 100",
			notWant: "entry 001",
		},
		{
			name:    "back from the log hands the keys to the list",
			presses: []string{"pick:block", "esc", "j", paneltest.Rest},
			want:    "error: port 5470 already allocated",
		},
		{
			name:    "the list's letter from the log hands the keys to the list",
			presses: []string{"pick:block", "pick:list", "j", paneltest.Rest},
			want:    "error: port 5470 already allocated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, longLog())
			harness.Press(tt.presses...)

			stage := paneltest.StageText(harness.Frame())
			assert.Contains(t, stage, tt.want)
			if tt.notWant != "" {
				assert.NotContains(t, stage, tt.notWant)
			}
		})
	}

	t.Run("the wheel over the log scrolls it and leaves the list alone", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, longLog())
		harness.Send(tea.MouseWheelMsg{
			X:      paneltest.DefaultWidth - 1,
			Y:      5,
			Button: tea.MouseWheelUp,
		})

		frame := harness.Frame()
		assert.NotContains(t, paneltest.StageText(frame), "entry 100")
		assert.Contains(t, paneltest.HoveredRow(frame), "server")
	})
}

// TestLogsLineFilter covers the pattern typed at the foot of the log narrowing the lines the
// server reads, and the keys moving between the prompt, the lines and the list.
func TestLogsLineFilter(t *testing.T) {
	t.Parallel()

	typed := []string{"pick:block", "/", "s", "e", "c", "o", "n", "d"}
	settled := append(slices.Clone(typed), settle)

	tests := []struct {
		name     string
		presses  []string
		want     []string
		notWant  []string
		wantLast *logv1.ReadAgentLogsRequest
	}{
		{
			name:     "typing narrows the log to the lines the pattern matches",
			presses:  settled,
			want:     []string{"second line"},
			notWant:  []string{"first line"},
			wantLast: serverReadMatching("(?i)second", ""),
		},
		{
			name:     "an upper-case letter makes the pattern match case",
			presses:  []string{"pick:block", "/", "S", settle},
			wantLast: serverReadMatching("S", ""),
		},
		{
			name:     "a broken pattern reads nothing and keeps the lines",
			presses:  append(slices.Clone(settled), "(", settle),
			want:     []string{"second line", "▽ second("},
			notWant:  []string{"first line"},
			wantLast: serverReadMatching("(?i)second", "10"),
		},
		{
			name:     "esc on the lines clears a standing filter",
			presses:  append(slices.Clone(typed), "enter", "esc"),
			want:     []string{"first line", "second line"},
			notWant:  []string{"▽ second"},
			wantLast: logRead("server", ""),
		},
		{
			name:    "esc on the lines with no filter standing goes back to the list",
			presses: append(slices.Clone(typed), "enter", "esc", "esc", "j", paneltest.Rest),
			want:    []string{"error: port 5470 already allocated"},
		},
		{
			name:     "esc while typing drops the query and keeps the keys on the lines",
			presses:  []string{"pick:block", "/", "s", "esc", "esc", "j", paneltest.Rest},
			want:     []string{"error: port 5470 already allocated"},
			wantLast: logRead(provisionRun, ""),
		},
		{
			name:    "a pattern matching nothing says so",
			presses: []string{"pick:block", "/", "z", "z", "z", "enter"},
			want:    []string{"Nothing matching in the last 64 MB.", "▽ zzz"},
		},
		{
			name:    "a pattern matching nothing keeps the keys on the lines",
			presses: []string{"pick:block", "/", "z", "z", "z", "enter", "esc"},
			want:    []string{"first line", "second line"},
		},
		{
			name:     "moving to another source drops the filter",
			presses:  append(slices.Clone(typed), "enter", "pick:list", "j", paneltest.Rest, "k", paneltest.Rest),
			want:     []string{"first line", "second line"},
			notWant:  []string{"▽ second"},
			wantLast: logRead("server", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, filteredTailOfTwo())
			pressSettling(harness, tt.presses...)

			stage := paneltest.StageText(harness.Frame())
			assert.Equal(t, screen.Logs, paneltest.Screen(harness.Frame()))
			for _, want := range tt.want {
				assert.Contains(t, stage, want)
			}
			for _, notWant := range tt.notWant {
				assert.NotContains(t, stage, notWant)
			}
			if tt.wantLast != nil {
				sent := harness.Sent(logv1connect.LogServiceReadAgentLogsProcedure)
				assert.True(t, proto.Equal(tt.wantLast, sent[len(sent)-1]), "the last read was %v", sent[len(sent)-1])
			}
		})
	}

	t.Run("the prompt is rendered on the lowest row of the log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.Press(typed...)

		assert.Contains(t, lastStageRow(harness.Frame()), "▽ second")
	})

	t.Run("the prompt stands at the foot of the log before anything is typed", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())

		assert.Equal(t, "  ▽", lastStageRow(harness.Frame()))
	})

	t.Run("the lines sit on the prompt rather than at the top of the log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.Press(append(slices.Clone(typed), "enter")...)

		rows := paneltest.Stage(harness.Frame())
		foot := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "▽ second") })
		assert.Positive(t, foot)
		assert.Contains(t, rows[foot-1], "second line")
	})

	t.Run("the next read carries the pattern", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.Press(append(slices.Clone(typed), "enter")...)
		harness.Advance(time.Second)

		sent := harness.Sent(logv1connect.LogServiceReadAgentLogsProcedure)
		assert.True(t, proto.Equal(serverReadMatching("(?i)second", "10"), sent[len(sent)-1]), "the last read was %v", sent[len(sent)-1])
	})

	t.Run("a read of the pattern typed over is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		reads := filteredTailOfTwo()
		reads["server~(?i)s@"] = &logv1.ReadAgentLogsResponse{Lines: []string{`{"body":"stale line"}`}, Cursor: "10"}
		harness := openLogs(t, reads)
		harness.Hold(read)
		harness.Press("pick:block", "/", "s")
		harness.Advance(time.Second)
		harness.Press("e", "c", "o", "n", "d")
		harness.Advance(time.Second)
		for harness.Held(read) > 0 {
			harness.Release(read)
		}

		stage := paneltest.StageText(harness.Frame())
		assert.NotContains(t, stage, "stale line")
		assert.Contains(t, stage, "second line")
	})

	t.Run("a pattern typed in one go is read once", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		pressSettling(harness, settled...)

		narrowed := slices.DeleteFunc(harness.Sent(logv1connect.LogServiceReadAgentLogsProcedure), func(sent proto.Message) bool {
			read, _ := sent.(*logv1.ReadAgentLogsRequest)

			return read.GetPattern() == ""
		})
		assert.Len(t, narrowed, 1)
	})

	t.Run("keys pressed while a cleared filter is read again stay on the lines", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := openLogs(t, filteredTailOfTwo())
		pressSettling(harness, append(slices.Clone(settled), "enter")...)
		harness.Hold(read)
		harness.Press("esc", "j")
		harness.Release(read)

		stage := paneltest.StageText(harness.Frame())
		assert.Contains(t, stage, "first line")
		assert.NotContains(t, stage, "error: port 5470 already allocated")
		assert.False(t, slices.ContainsFunc(harness.Sent(read), func(sent proto.Message) bool {
			return proto.Equal(logRead(provisionRun, ""), sent)
		}), "the source under the list stays the one being read")
	})
}

// TestLogsCopy covers the copy key putting the log line under the cursor on the clipboard.
func TestLogsCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		presses []string
		want    []string
	}{
		{
			name:    "the newest line is copied from where the cursor starts",
			presses: []string{"pick:block", "y"},
			want:    []string{`{"body":"second line"}`},
		},
		{
			name:    "the line the cursor moved to is copied",
			presses: []string{"pick:block", "k", "y"},
			want:    []string{`{"body":"first line"}`},
		},
		{
			name:    "the list holding the keys copies nothing",
			presses: []string{"y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, tailOfTwo())
			harness.Press(tt.presses...)

			assert.Equal(t, tt.want, harness.Copied())
		})
	}
}

const provisionRun = "001-agent.provision-setup.log"

// logReads answers each read of an agent's logs by the source, the pattern after a ~ when there is one, and
// the cursor it was sent with.
type logReads map[string]*logv1.ReadAgentLogsResponse

var (
	serverSource    = &logv1.LogSource{Id: "server"}
	provisionSource = &logv1.LogSource{
		Id:      provisionRun,
		Run:     xptr.Ptr[int32](1),
		Trigger: eventv1.Moment_MOMENT_AGENT_PROVISION,
		Script:  "setup",
	}
	launchSource = &logv1.LogSource{
		Id:      "002-agent.pre-launch-setup.log",
		Run:     xptr.Ptr[int32](2),
		Trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
		Script:  "setup",
	}
)

// answerSources lists the sources given for every agent.
func answerSources(harness *paneltest.Panel, sources ...*logv1.LogSource) {
	harness.Answer(logv1connect.LogServiceListAgentLogSourcesProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&logv1.ListAgentLogSourcesResponse{Sources: sources}), nil
	})
}

func (r logReads) answer(harness *paneltest.Panel) {
	answerSources(harness, serverSource, provisionSource)
	harness.Answer(logv1connect.LogServiceReadAgentLogsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*logv1.ReadAgentLogsRequest)
		source := read.GetSource()
		if read.GetPattern() != "" {
			source += "~" + read.GetPattern()
		}
		if resp, ok := r[source+"@"+read.GetCursor()]; ok {
			return connect.NewResponse(resp), nil
		}

		return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: read.GetCursor()}), nil
	})
}

// tailOfTwo is a log holding two lines about the agent, a third written after the first read, and
// a provision run that printed one line.
func tailOfTwo() logReads {
	return logReads{
		"server@":          {Lines: []string{`{"body":"first line"}`, `{"body":"second line"}`}, Cursor: "10"},
		"server@10":        {Lines: []string{`{"body":"third line"}`}, Cursor: "20"},
		provisionRun + "@": {Lines: []string{"error: port 5470 already allocated"}, Cursor: "35"},
	}
}

// openLogs opens the logs of the reviewer from the subpages menu, over a log answering reads.
func openLogs(t *testing.T, reads logReads, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	if len(rows) == 0 {
		rows = buildLogAgents()
	}
	harness := paneltest.OpenAgents(t, rows...)
	reads.answer(harness)
	harness.MoveToRow("reviewer")
	harness.Press("space", "l")

	return harness
}

func logRead(source, cursor string) *logv1.ReadAgentLogsRequest {
	return &logv1.ReadAgentLogsRequest{
		AgentIds: []string{"reviewer"},
		Source:   source,
		Cursor:   cursor,
		Limit:    500,
	}
}

func serverReadMatching(pattern, cursor string) *logv1.ReadAgentLogsRequest {
	read := logRead("server", cursor)
	read.Pattern = pattern

	return read
}

// longLog is a server log of a hundred numbered entries, taller than the block renders.
func longLog() logReads {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("entry %03d", i))
	}

	return logReads{
		"server@":          {Lines: lines, Cursor: "10"},
		provisionRun + "@": {Lines: []string{"error: port 5470 already allocated"}, Cursor: "35"},
	}
}

// filteredTailOfTwo is tailOfTwo with the log narrowed to its second line by a pattern for it.
func filteredTailOfTwo() logReads {
	reads := tailOfTwo()
	reads["server~(?i)second@"] = &logv1.ReadAgentLogsResponse{Lines: []string{`{"body":"second line"}`}, Cursor: "10"}
	reads["server~(?i)second@10"] = &logv1.ReadAgentLogsResponse{Cursor: "20"}

	return reads
}

// lastStageRow is the lowest row of the stage that renders anything.
func lastStageRow(frame string) string {
	rows := paneltest.Stage(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) != "" {
			return rows[i]
		}
	}

	return ""
}

const settle = "<settle>"

func pressSettling(harness *paneltest.Panel, presses ...string) {
	for _, press := range presses {
		if press == settle {
			harness.Advance(time.Second)

			continue
		}
		harness.Press(press)
	}
}

// TestLogsTask covers a task's logs listing the runs of every agent on it from the task's row.
func TestLogsTask(t *testing.T) {
	t.Parallel()

	t.Run("a task with a failed run nobody opened marks its row", func(t *testing.T) {
		t.Parallel()

		rows := buildLogTask()
		task, _ := rows[1].(*taskv1.Task)
		task.FailedRun = true
		harness := paneltest.OpenAgents(t, rows...)

		harness.MoveToRow("fix-login")

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "fix-login ")
	})

	t.Run("opening a task's logs marks its runs read", func(t *testing.T) {
		t.Parallel()

		harness := openTaskLogs(t, taskRunSources())

		harness.AssertSentTo(taskv1connect.TaskServiceMarkTaskRunsReadProcedure, &taskv1.MarkTaskRunsReadRequest{Id: "t1"})
	})

	t.Run("a task's logs list the runs of each agent on it under its slug", func(t *testing.T) {
		t.Parallel()

		harness := openTaskLogs(t, taskRunSources())

		frame := harness.Frame()
		assert.Equal(t, screen.Logs, paneltest.Screen(harness.Frame()))
		assert.Contains(t, frame, "agents › fix-login › logs")
		assert.Contains(t, paneltest.HoveredRow(frame), "follow-up 001 task.pre-open")
		assert.Contains(t, paneltest.SidebarProse(frame), "author 001 agent.pre-launch")
		assert.Contains(t, paneltest.StageText(frame), "opened the checkout")
		harness.AssertSentTo(logv1connect.LogServiceListTaskLogSourcesProcedure, &logv1.ListTaskLogSourcesRequest{TaskId: "t1"})
	})

	t.Run("a task's logs land on the run that failed without claiming the agent's runs", func(t *testing.T) {
		t.Parallel()

		sources := taskRunSources()
		sources[1].Failed = true
		harness := openTaskLogs(t, sources)

		row := paneltest.HoveredRow(harness.Frame())
		assert.Contains(t, row, "author 001 agent.pre-launch")
		assert.Contains(t, row, "\uf489")
		harness.AssertSentTo(agentv1connect.AgentServiceMarkRunsReadProcedure)
	})

	t.Run("a run of the same name is read from the agent that ran it", func(t *testing.T) {
		t.Parallel()

		harness := openTaskLogs(t, taskRunSources())
		harness.Press("j", paneltest.Rest)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "launched the author")
		harness.AssertSentTo(logv1connect.LogServiceReadAgentLogsProcedure,
			&logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a2"},
				Source:   "001-task.pre-open-setup.log",
				Limit:    500,
			},
			&logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a1"},
				Source:   "001-agent.pre-launch-setup.log",
				Limit:    500,
			},
		)
	})
}

// taskRunSources is a run of each agent on task fix-login, the follow-up's newest.
func taskRunSources() []*logv1.LogSource {
	return []*logv1.LogSource{
		{
			Id:      "001-task.pre-open-setup.log",
			Run:     xptr.Ptr[int32](1),
			Trigger: eventv1.Moment_MOMENT_TASK_PRE_OPEN,
			Script:  "setup",
			AgentId: "a2",
		},
		{
			Id:      "001-agent.pre-launch-setup.log",
			Run:     xptr.Ptr[int32](1),
			Trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			Script:  "setup",
			AgentId: "a1",
		},
	}
}

// openTaskLogs opens the logs of task fix-login from its row's menu, over the sources given.
func openTaskLogs(t *testing.T, sources []*logv1.LogSource) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, buildLogTask()...)
	harness.Answer(logv1connect.LogServiceListTaskLogSourcesProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&logv1.ListTaskLogSourcesResponse{Sources: sources}), nil
	})
	harness.Answer(logv1connect.LogServiceReadAgentLogsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*logv1.ReadAgentLogsRequest)
		lines := map[string]string{"a1": "launched the author", "a2": "opened the checkout"}

		return connect.NewResponse(&logv1.ReadAgentLogsResponse{Lines: []string{lines[read.GetAgentIds()[0]]}}), nil
	})
	harness.MoveToRow("fix-login")
	harness.Press("space", "l")

	return harness
}

func buildLogAgents() []proto.Message {
	writer := paneltest.Agent("writer", "writer", "w1")
	task := paneltest.TaskOf(writer)

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		paneltest.Orchestrator(),
		paneltest.Reviewer("reviewer", "reviewer", "writer"),
		writer,
		task,
		paneltest.Reviewer("quiet", "quiet", "writer"),
	}
}

// buildLogTask is task fix-login, written by author with follow-up reviewing.
func buildLogTask() []proto.Message {
	author := paneltest.Agent("a1", "author", "w1")
	author.TaskId = xptr.Ptr("t1")
	author.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	followUp := paneltest.Agent("a2", "follow-up", "w1")
	followUp.TaskId = xptr.Ptr("t1")
	followUp.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		&taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "fix-login",
		},
		author,
		followUp,
	}
}
