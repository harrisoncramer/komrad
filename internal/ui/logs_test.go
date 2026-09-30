package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

const logsScreen = "the logs"

const provisionRun = "001-provision-setup.log"

// logReads answers each read of an agent's logs by the source, the pattern after a ~ when there is one, and
// the cursor it was sent with.
type logReads map[string]*logv1.ReadAgentLogsResponse

var (
	serviceSource   = &logv1.LogSource{Id: "service", Name: "service"}
	provisionSource = &logv1.LogSource{
		Id:   provisionRun,
		Name: "001 provision setup",
		Run:  xptr.Ptr[int32](1),
	}
	launchSource = &logv1.LogSource{
		Id:   "002-pre-launch-setup.log",
		Name: "002 pre-launch setup",
		Run:  xptr.Ptr[int32](2),
	}
)

// answerSources lists the sources given for every agent.
func answerSources(harness *panelHarness, sources ...*logv1.LogSource) {
	harness.answer(logv1connect.LogServiceListAgentLogSourcesProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&logv1.ListAgentLogSourcesResponse{Sources: sources}), nil
	})
}

func (r logReads) answer(harness *panelHarness) {
	answerSources(harness, serviceSource, provisionSource)
	harness.answer(logv1connect.LogServiceReadAgentLogsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
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
		"service@":         {Lines: []string{`{"body":"first line"}`, `{"body":"second line"}`}, Cursor: "10"},
		"service@10":       {Lines: []string{`{"body":"third line"}`}, Cursor: "20"},
		provisionRun + "@": {Lines: []string{"error: port 5470 already allocated"}, Cursor: "35"},
	}
}

// openLogs opens the logs of the author from the subpages menu, over a log answering reads.
func openLogs(t *testing.T, reads logReads, rows ...proto.Message) *panelHarness {
	t.Helper()

	if len(rows) == 0 {
		rows = buildSubpagesAgents()
	}
	harness := openAgents(t, rows...)
	reads.answer(harness)
	moveToAgentRow(t, harness, "author")
	harness.press("space", "l")

	return harness
}

func logRead(source, cursor string) *logv1.ReadAgentLogsRequest {
	return &logv1.ReadAgentLogsRequest{
		AgentIds: []string{"author"},
		Source:   source,
		Cursor:   cursor,
		Limit:    500,
	}
}

func serviceReadMatching(pattern, cursor string) *logv1.ReadAgentLogsRequest {
	read := logRead("service", cursor)
	read.Pattern = pattern

	return read
}

// TestLogsOpen covers the screen opening from the subpages menu on the agent's log sources.
func TestLogsOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu lists the sources and opens the newest lines of the service log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())

		frame := harness.readFrame()
		assert.Equal(t, logsScreen, harness.readScreen())
		assert.True(t, harness.panel.IsZoomed(), "the screen holds the whole window")
		assert.Contains(t, frame, "agents › author › logs")
		assert.Contains(t, readHoveredRow(frame), "service")
		assert.Contains(t, readSidebarProse(frame), "001 provision setup")
		assert.Contains(t, readStageText(frame), `{"body":"first line"}`)
		assert.Contains(t, readStageText(frame), `{"body":"second line"}`)
		harness.assertSentTo(logv1connect.LogServiceListAgentLogSourcesProcedure, &logv1.ListAgentLogSourcesRequest{AgentId: "author"})
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""))
	})

	t.Run("an orchestrator's logs are under its workflow's name", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "komrad")
		harness.press("l")

		assert.Equal(t, logsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "agents › komrad › logs")
	})

	t.Run("an agent with a failed run opens on it", func(t *testing.T) {
		t.Parallel()

		rows := buildSubpagesAgents()
		author, _ := rows[2].(*agentv1.Agent)
		author.FailedRunLog = xptr.Ptr[int32](1)
		harness := openLogs(t, tailOfTwo(), rows...)

		frame := harness.readFrame()
		assert.Contains(t, readHoveredRow(frame), "001 provision setup \uf489")
		assert.Contains(t, readStageText(frame), "error: port 5470 already allocated")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(provisionRun, ""))
	})

	t.Run("a log with nothing about the agent says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})

		assert.Contains(t, readStageText(harness.readFrame()), "Nothing about this agent in the last 64 MB of the log.")
	})

	t.Run("a run that printed nothing says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})
		harness.press("j")

		assert.Contains(t, readStageText(harness.readFrame()), "This run wrote nothing.")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})
}

// TestLogsSources covers moving between sources, each drawn on its own from its newest lines.
func TestLogsSources(t *testing.T) {
	t.Parallel()

	t.Run("moving to another source draws its lines alone", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j")

		stage := readStageText(harness.readFrame())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "001 provision setup")
		assert.Contains(t, stage, "error: port 5470 already allocated")
		assert.NotContains(t, stage, "first line")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead(provisionRun, ""))
	})

	t.Run("moving back reads the source from its newest again", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j", "k")

		assert.Contains(t, readStageText(harness.readFrame()), "second line")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead(provisionRun, ""), logRead("service", ""))
	})

	t.Run("a key that leaves the cursor where it was reads nothing again", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("g", "g")

		assert.Contains(t, readStageText(harness.readFrame()), "second line")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""))
	})

	t.Run("a newer run listed on a tick leaves the hovered source as it was", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j")
		answerSources(harness, serviceSource, launchSource, provisionSource)
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "002 pre-launch setup")
		assert.Contains(t, readStageText(frame), "error: port 5470 already allocated")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead(provisionRun, ""), logRead(provisionRun, "35"))
	})

	t.Run("a hovered run gone from the listing moves to its neighbour and reads it", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j")
		answerSources(harness, serviceSource)
		harness.fireTimers()

		frame := harness.readFrame()
		assert.NotContains(t, readSidebarProse(frame), "001 provision setup")
		assert.Contains(t, readStageText(frame), "second line")
		assert.NotContains(t, readStageText(frame), "error: port")
		sent := harness.readSent(logv1connect.LogServiceReadAgentLogsProcedure)
		assert.True(t, proto.Equal(logRead("service", ""), sent[len(sent)-1]), "the neighbour is read from its newest")
	})

	t.Run("a hovered run gone from the listing hands the keys back to the list", func(t *testing.T) {
		t.Parallel()

		reads := tailOfTwo()
		reads[launchSource.GetId()+"@"] = &logv1.ReadAgentLogsResponse{Lines: []string{"launching"}, Cursor: "9"}
		harness := openLogs(t, reads)
		harness.press("j", "pick:block")
		answerSources(harness, serviceSource, launchSource)
		harness.fireTimers()
		harness.press("k")

		assert.Contains(t, readStageText(harness.readFrame()), "second line")
	})

	t.Run("a read of the source moved off is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := openAgents(t, buildSubpagesAgents()...)
		tailOfTwo().answer(harness)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.hold(read)
		harness.fireTimers()
		harness.press("j")

		harness.release(read)

		assert.NotContains(t, readStageText(harness.readFrame()), "third line")
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
			notSidebar: "service",
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
			wantSidebar: "001 provision setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, tailOfTwo())
			harness.hold(logv1connect.LogServiceListAgentLogSourcesProcedure)
			harness.press(tt.presses...)
			harness.fireTimers()

			frame := harness.readFrame()
			if tt.wantStage != "" {
				assert.Contains(t, readStageText(frame), tt.wantStage)
			}
			if tt.wantSidebar != "" {
				assert.Contains(t, readSidebarProse(frame), tt.wantSidebar)
			}
			if tt.notSidebar != "" {
				assert.NotContains(t, readSidebarProse(frame), tt.notSidebar)
			}
		})
	}

	t.Run("a filtered run gone from the listing moves to the neighbour the filter shows", func(t *testing.T) {
		t.Parallel()

		reads := tailOfTwo()
		reads[launchSource.GetId()+"@"] = &logv1.ReadAgentLogsResponse{Lines: []string{"launching"}, Cursor: "9"}
		harness := openLogs(t, reads)
		answerSources(harness, serviceSource, launchSource, provisionSource)
		harness.fireTimers()
		harness.press("/", "tab", "s", "e", "t", "u", "p", "enter", "j")
		harness.fireTimers()
		answerSources(harness, serviceSource, launchSource)
		harness.fireTimers()

		assert.Contains(t, readStageText(harness.readFrame()), "launching")
	})
}

// TestLogsTail covers the reads after the first, each from where the last left off.
func TestLogsTail(t *testing.T) {
	t.Parallel()

	t.Run("the next read carries the cursor and appends what is new", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", "10"))
		assert.Contains(t, readStageText(harness.readFrame()), `{"body":"third line"}`)
	})

	t.Run("a log that started over clears what was held", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"service@": {Lines: []string{`{"body":"first line"}`}, Cursor: "10"},
			"service@10": {
				Lines:  []string{`{"body":"fresh line"}`},
				Cursor: "4",
				Reset_: true,
			},
		})
		harness.fireTimers()

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "fresh line")
		assert.NotContains(t, stage, "first line")
	})

	t.Run("a read with more behind it reads again at once", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"service@": {
				Lines:  []string{`{"body":"first line"}`},
				Cursor: "10",
				More:   true,
			},
			"service@10": {Lines: []string{`{"body":"second line"}`}, Cursor: "20"},
		})

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", "10"))
		assert.Contains(t, readStageText(harness.readFrame()), `{"body":"second line"}`)
	})

	t.Run("a read answered after the screen was left and opened again is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := openAgents(t, buildSubpagesAgents()...)
		logReads{}.answer(harness)
		harness.answer(read, func(req connect.AnyRequest) (connect.AnyResponse, error) {
			sent, _ := req.Any().(*logv1.ReadAgentLogsRequest)
			if slices.Contains(sent.GetAgentIds(), "author") {
				return connect.NewResponse(&logv1.ReadAgentLogsResponse{
					Lines:  []string{`{"body":"first line"}`},
					Cursor: "10",
					More:   true,
				}), nil
			}

			return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: "30"}), nil
		})
		harness.hold(read)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l", "esc")
		moveToAgentRow(t, harness, "quiet")
		harness.press("space", "l")

		harness.release(read)
		harness.release(read)

		quiet := &logv1.ReadAgentLogsRequest{
			AgentIds: []string{"quiet"},
			Source:   "service",
			Limit:    500,
		}
		assert.NotContains(t, readStageText(harness.readFrame()), "first line")
		harness.assertSentTo(read, logRead("service", ""), quiet)
	})

	t.Run("leaving the screen ends the reads", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""))
	})

	t.Run("a failed read says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		logReads{}.answer(harness)
		harness.refuse(logv1connect.LogServiceReadAgentLogsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the log is gone")))
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", ""))
		assert.Contains(t, readStageText(harness.readFrame()), "could not read: the log is gone")
	})

	t.Run("a failed listing says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		harness.refuse(logv1connect.LogServiceListAgentLogSourcesProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the engine is gone")))
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.fireTimers()

		assert.Len(t, harness.readSent(logv1connect.LogServiceListAgentLogSourcesProcedure), 2)
		assert.Contains(t, readSidebarProse(harness.readFrame()), "could not list: the engine is gone")
	})
}

// longLog is a service log of a hundred numbered entries, taller than the block draws.
func longLog() logReads {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("entry %03d", i))
	}

	return logReads{
		"service@":         {Lines: lines, Cursor: "10"},
		provisionRun + "@": {Lines: []string{"error: port 5470 already allocated"}, Cursor: "35"},
	}
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
			name:    "the list holding the keys draws no cursor in the log",
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
			presses: []string{"pick:block", "esc", "j"},
			want:    "error: port 5470 already allocated",
		},
		{
			name:    "the list's letter from the log hands the keys to the list",
			presses: []string{"pick:block", "pick:list", "j"},
			want:    "error: port 5470 already allocated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, longLog())
			harness.press(tt.presses...)

			stage := readStageText(harness.readFrame())
			assert.Contains(t, stage, tt.want)
			if tt.notWant != "" {
				assert.NotContains(t, stage, tt.notWant)
			}
		})
	}

	t.Run("the wheel over the log scrolls it and leaves the list alone", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, longLog())
		harness.send(tea.MouseWheelMsg{
			X:      defaultHarnessWidth - 1,
			Y:      5,
			Button: tea.MouseWheelUp,
		})

		frame := harness.readFrame()
		assert.NotContains(t, readStageText(frame), "entry 100")
		assert.Contains(t, readHoveredRow(frame), "service")
	})
}

// filteredTailOfTwo is tailOfTwo with the log narrowed to its second line by a pattern for it.
func filteredTailOfTwo() logReads {
	reads := tailOfTwo()
	reads["service~(?i)second@"] = &logv1.ReadAgentLogsResponse{Lines: []string{`{"body":"second line"}`}, Cursor: "10"}
	reads["service~(?i)second@10"] = &logv1.ReadAgentLogsResponse{Cursor: "20"}

	return reads
}

// lastStageRow is the lowest row of the stage that draws anything.
func lastStageRow(frame string) string {
	rows := readStage(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) != "" {
			return rows[i]
		}
	}

	return ""
}

const settle = "<settle>"

func pressSettling(harness *panelHarness, presses ...string) {
	for _, press := range presses {
		if press == settle {
			harness.fireTimers()

			continue
		}
		harness.press(press)
	}
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
			wantLast: serviceReadMatching("(?i)second", ""),
		},
		{
			name:     "an upper-case letter makes the pattern match case",
			presses:  []string{"pick:block", "/", "S", settle},
			wantLast: serviceReadMatching("S", ""),
		},
		{
			name:     "a broken pattern reads nothing and keeps the lines",
			presses:  append(slices.Clone(settled), "(", settle),
			want:     []string{"second line", "▽ second("},
			notWant:  []string{"first line"},
			wantLast: serviceReadMatching("(?i)second", "10"),
		},
		{
			name:     "esc on the lines clears a standing filter",
			presses:  append(slices.Clone(typed), "enter", "esc"),
			want:     []string{"first line", "second line"},
			notWant:  []string{"▽ second"},
			wantLast: logRead("service", ""),
		},
		{
			name:    "esc on the lines with no filter standing goes back to the list",
			presses: append(slices.Clone(typed), "enter", "esc", "esc", "j"),
			want:    []string{"error: port 5470 already allocated"},
		},
		{
			name:     "esc while typing drops the query and keeps the keys on the lines",
			presses:  []string{"pick:block", "/", "s", "esc", "esc", "j"},
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
			presses:  append(slices.Clone(typed), "enter", "pick:list", "j", "k"),
			want:     []string{"first line", "second line"},
			notWant:  []string{"▽ second"},
			wantLast: logRead("service", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openLogs(t, filteredTailOfTwo())
			pressSettling(harness, tt.presses...)

			stage := readStageText(harness.readFrame())
			assert.Equal(t, logsScreen, harness.readScreen())
			for _, want := range tt.want {
				assert.Contains(t, stage, want)
			}
			for _, notWant := range tt.notWant {
				assert.NotContains(t, stage, notWant)
			}
			if tt.wantLast != nil {
				sent := harness.readSent(logv1connect.LogServiceReadAgentLogsProcedure)
				assert.True(t, proto.Equal(tt.wantLast, sent[len(sent)-1]), "the last read was %v", sent[len(sent)-1])
			}
		})
	}

	t.Run("the prompt is drawn on the lowest row of the log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.press(typed...)

		assert.Contains(t, lastStageRow(harness.readFrame()), "▽ second")
	})

	t.Run("the prompt stands at the foot of the log before anything is typed", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())

		assert.Equal(t, "  ▽", lastStageRow(harness.readFrame()))
	})

	t.Run("the lines sit on the prompt rather than at the top of the log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.press(append(slices.Clone(typed), "enter")...)

		rows := readStage(harness.readFrame())
		foot := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, "▽ second") })
		assert.Positive(t, foot)
		assert.Contains(t, rows[foot-1], "second line")
	})

	t.Run("the next read carries the pattern", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		harness.press(append(slices.Clone(typed), "enter")...)
		harness.fireTimers()

		sent := harness.readSent(logv1connect.LogServiceReadAgentLogsProcedure)
		assert.True(t, proto.Equal(serviceReadMatching("(?i)second", "10"), sent[len(sent)-1]), "the last read was %v", sent[len(sent)-1])
	})

	t.Run("a read of the pattern typed over is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		reads := filteredTailOfTwo()
		reads["service~(?i)s@"] = &logv1.ReadAgentLogsResponse{Lines: []string{`{"body":"stale line"}`}, Cursor: "10"}
		harness := openLogs(t, reads)
		harness.hold(read)
		harness.press("pick:block", "/", "s")
		harness.fireTimers()
		harness.press("e", "c", "o", "n", "d")
		harness.fireTimers()
		for len(harness.parked) > 0 {
			harness.release(read)
		}

		stage := readStageText(harness.readFrame())
		assert.NotContains(t, stage, "stale line")
		assert.Contains(t, stage, "second line")
	})

	t.Run("a pattern typed in one go is read once", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, filteredTailOfTwo())
		pressSettling(harness, settled...)

		narrowed := slices.DeleteFunc(harness.readSent(logv1connect.LogServiceReadAgentLogsProcedure), func(sent proto.Message) bool {
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
		harness.hold(read)
		harness.press("esc", "j")
		harness.release(read)

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "first line")
		assert.NotContains(t, stage, "error: port 5470 already allocated")
		assert.False(t, slices.ContainsFunc(harness.readSent(read), func(sent proto.Message) bool {
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
			harness.press(tt.presses...)

			assert.Equal(t, tt.want, harness.panel.ReadCopied())
		})
	}
}
