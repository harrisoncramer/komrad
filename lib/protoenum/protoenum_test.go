package protoenum

import (
	"testing"

	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    eventv1.Moment
		wantErr string
	}{
		{
			name:    "an undefined name fails",
			in:      "agent.open-pr",
			wantErr: `event.v1.Moment defines no value named "agent.open-pr"`,
		},
		{
			name:    "the constant name is not a JSON name",
			in:      "MOMENT_AGENT_PRE_LAUNCH",
			wantErr: `event.v1.Moment defines no value named "MOMENT_AGENT_PRE_LAUNCH"`,
		},
		{
			name:    "the zero value has no name to parse",
			in:      "MOMENT_UNSPECIFIED",
			wantErr: `event.v1.Moment defines no value named "MOMENT_UNSPECIFIED"`,
		},
		{
			name: "a dotted name is its value",
			in:   "agent.pre-launch",
			want: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse[eventv1.Moment](tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAll(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []eventv1.Event
		wantErr string
	}{
		{
			name:    "one undefined name fails the whole list",
			in:      []string{"pull_request.behind", "review.forge_reply_requested"},
			wantErr: `event.v1.Event defines no value named "review.forge_reply_requested"`,
		},
		{
			name: "no names are no values",
			in:   nil,
			want: []eventv1.Event{},
		},
		{
			name: "dotted names keep their order",
			in:   []string{"pull_request.checks_failing", "pull_request.behind"},
			want: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, eventv1.Event_EVENT_PULL_REQUEST_BEHIND},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAll[eventv1.Event](tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		name string
		in   eventv1.Moment
		want string
	}{
		{name: "an undefined number is its number", in: eventv1.Moment(99), want: "99"},
		{name: "the zero value is its constant name", in: eventv1.Moment_MOMENT_UNSPECIFIED, want: "MOMENT_UNSPECIFIED"},
		{name: "a value is its dotted name", in: eventv1.Moment_MOMENT_TASK_PRE_OPEN, want: "task.pre-open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Name(tt.in))
		})
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		name      string
		in        eventv1.Moment
		wantTitle string
	}{
		{name: "an undefined number has an empty label", in: eventv1.Moment(99)},
		{name: "the zero value has an empty label", in: eventv1.Moment_MOMENT_UNSPECIFIED},
		{name: "a value has its title", in: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, wantTitle: "Agent pre-launch hook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := Label(tt.in)
			require.NotNil(t, label)
			assert.Equal(t, tt.wantTitle, label.GetTitle())
		})
	}
}

func TestAcceptedOf(t *testing.T) {
	tests := []struct {
		name  string
		field protoreflect.FieldDescriptor
		want  []string
	}{
		{
			name:  "not_in leaves out the numbers it names",
			field: (&workflowv1.WorkflowHook{}).ProtoReflect().Descriptor().Fields().ByName("event"),
			want: []string{
				"agent.pre-launch", "agent.post-launch", "agent.pre-resume", "agent.post-resume",
				"agent.pre-archive", "agent.post-archive", "agent.pre-restore", "agent.post-restore",
				"agent.pre-delete", "agent.post-delete", "task.pre-open", "task.post-archive", "task.pre-delete",
			},
		},
		{
			name:  "a rule refusing only the zero accepts every declared value",
			field: (&workflowv1.WorkflowEventPrompt{}).ProtoReflect().Descriptor().Fields().ByName("event"),
			want:  Names(Values[eventv1.Event]()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, xslice.Map(AcceptedOf(tt.field), NameOf))
			assert.False(t, IsAccepted(tt.field, 0))
		})
	}
}
