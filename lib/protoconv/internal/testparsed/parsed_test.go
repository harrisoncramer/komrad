package testparsed_test

import (
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"komrad/lib/protoconv"
	"komrad/lib/protoconv/internal/testparsed"
	"komrad/lib/protoconv/internal/testpb"
)

const (
	first   = "0192d2a1-7b3c-7def-8a12-3456789abcde"
	second  = "0192d2a1-7b3c-7def-8a12-3456789abcdf"
	trimmed = "0192d2a17b3c7def8a123456789abcde"
)

var (
	firstID  = uuid.MustParse(first)
	secondID = uuid.MustParse(second)
	moment   = time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC)
)

// parseTest is one message, and either the parsed form it converts to or the path of the field
// that refuses to convert.
type parseTest[M proto.Message, P any] struct {
	name     string
	input    M
	want     *P
	wantPath string
	wantErr  error
	// beyondRules marks a message protovalidate accepts that still does not convert.
	beyondRules bool
}

// runParseTests also holds each case to protovalidate, which must refuse a message at the same
// path the conversion does and accept every message that converts.
func runParseTests[M proto.Message, P any](t *testing.T, parse func(M) (*P, error), tests []parseTest[M, P]) {
	t.Helper()

	validator, err := protovalidate.New()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.input)
			validation := validator.Validate(tt.input)
			if tt.wantPath == "" {
				require.NoError(t, validation)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)

				return
			}

			var failure *protoconv.Error
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, tt.wantPath, failure.Path)
			assert.Nil(t, got)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			if tt.beyondRules {
				assert.NoError(t, validation)

				return
			}

			var invalid *protovalidate.ValidationError
			require.ErrorAs(t, validation, &invalid)
			paths := make([]string, 0, len(invalid.Violations))
			for _, violation := range invalid.Violations {
				paths = append(paths, protovalidate.FieldPathString(violation.Proto.GetField()))
			}
			assert.Equal(t, []string{tt.wantPath}, paths)
		})
	}
}

// TestParseSingle covers each way a single id field can hold its value.
func TestParseSingle(t *testing.T) {
	runParseTests(t, testparsed.ParseSingle, []parseTest[*testpb.Single, testparsed.Single]{
		{
			name:     "an empty id is refused",
			input:    single(func(m *testpb.Single) { m.Id = "" }),
			wantPath: "id",
		},
		{
			name:     "an id without hyphens is refused",
			input:    single(func(m *testpb.Single) { m.Id = trimmed }),
			wantPath: "id",
		},
		{
			name:     "a trimmed id with hyphens is refused",
			input:    single(func(m *testpb.Single) { m.Trimmed = first }),
			wantPath: "trimmed",
		},
		{
			name:     "an optional id set to nothing is refused",
			input:    single(func(m *testpb.Single) { m.Maybe = pointer("") }),
			wantPath: "maybe",
		},
		{
			name:     "an optional id set to a bad id is refused",
			input:    single(func(m *testpb.Single) { m.Maybe = pointer("bad") }),
			wantPath: "maybe",
		},
		{
			name:     "a bad id where empty is allowed is refused",
			input:    single(func(m *testpb.Single) { m.BlankOk = "bad" }),
			wantPath: "blank_ok",
		},
		{
			name:     "fifteen bytes are refused as a binary id",
			input:    single(func(m *testpb.Single) { m.Binary = firstID[:15] }),
			wantPath: "binary",
		},
		{
			name:     "a short binary id where empty is allowed is refused",
			input:    single(func(m *testpb.Single) { m.BinaryOk = []byte{1} }),
			wantPath: "binary_ok",
		},
		{
			name:     "a required optional id left unset is missing",
			input:    single(func(m *testpb.Single) { m.Must = nil }),
			wantPath: "must",
			wantErr:  protoconv.ErrMissing,
		},
		{
			name:     "a required optional id set to a bad id is refused",
			input:    single(func(m *testpb.Single) { m.Must = pointer("bad") }),
			wantPath: "must",
		},
		{
			name:  "unset optional values convert to nil and ignored rules keep the raw value",
			input: single(func(m *testpb.Single) { m.Skipped = "not an id" }),
			want: &testparsed.Single{
				Id:      firstID,
				Trimmed: firstID,
				Binary:  firstID,
				Must:    firstID,
				Skipped: "not an id",
			},
		},
		{
			name: "every value converts",
			input: single(func(m *testpb.Single) {
				m.Maybe = pointer(second)
				m.BlankOk = second
				m.BinaryOk = secondID[:]
				m.Name = "name"
				m.Count = pointer[int32](3)
				m.Colour = testpb.Colour_COLOUR_RED
				m.Payload = []byte("payload")
			}),
			want: &testparsed.Single{
				Id:       firstID,
				Trimmed:  firstID,
				Maybe:    &secondID,
				BlankOk:  &secondID,
				Binary:   firstID,
				BinaryOk: &secondID,
				Must:     firstID,
				Name:     "name",
				Count:    pointer[int32](3),
				Colour:   testpb.Colour_COLOUR_RED,
				Payload:  []byte("payload"),
			},
		},
	})
}

// TestParseTimes covers times that are required, optional, listed and mapped.
func TestParseTimes(t *testing.T) {
	invalid := &timestamppb.Timestamp{Nanos: -1}
	runParseTests(t, testparsed.ParseTimes, []parseTest[*testpb.Times, testparsed.Times]{
		{
			name:     "a required time left unset is missing",
			input:    &testpb.Times{},
			wantPath: "at",
			wantErr:  protoconv.ErrMissing,
		},
		{
			name:        "a required time out of range is refused",
			input:       &testpb.Times{At: invalid},
			wantPath:    "at",
			beyondRules: true,
		},
		{
			name:        "an optional time out of range is refused",
			input:       &testpb.Times{At: timestamppb.New(moment), Maybe: invalid},
			wantPath:    "maybe",
			beyondRules: true,
		},
		{
			name:        "a listed time out of range is refused at its index",
			input:       &testpb.Times{At: timestamppb.New(moment), Each: []*timestamppb.Timestamp{timestamppb.New(moment), invalid}},
			wantPath:    "each[1]",
			beyondRules: true,
		},
		{
			name:        "a mapped time out of range is refused at its key",
			input:       &testpb.Times{At: timestamppb.New(moment), ByName: map[string]*timestamppb.Timestamp{"late": invalid}},
			wantPath:    `by_name["late"]`,
			beyondRules: true,
		},
		{
			name:  "an unset optional time converts to nil rather than the epoch",
			input: &testpb.Times{At: timestamppb.New(moment), Skipped: invalid},
			want:  &testparsed.Times{At: moment, Skipped: invalid},
		},
		{
			name: "every time converts",
			input: &testpb.Times{
				At:     timestamppb.New(moment),
				Maybe:  timestamppb.New(moment.Add(time.Hour)),
				Each:   []*timestamppb.Timestamp{timestamppb.New(moment)},
				ByName: map[string]*timestamppb.Timestamp{"start": timestamppb.New(moment)},
			},
			want: &testparsed.Times{
				At:     moment,
				Maybe:  pointer(moment.Add(time.Hour)),
				Each:   []time.Time{moment},
				ByName: map[string]time.Time{"start": moment},
			},
		},
	})
}

// TestParseLists covers ids as list elements, map keys and map values.
func TestParseLists(t *testing.T) {
	plains := []*testpb.Plain{{Name: "plain"}}
	runParseTests(t, testparsed.ParseLists, []parseTest[*testpb.Lists, testparsed.Lists]{
		{
			name:     "a bad id in a list is refused at its index",
			input:    &testpb.Lists{Ids: []string{first, "bad"}},
			wantPath: "ids[1]",
		},
		{
			name:     "a bad id as a map value is refused at its key",
			input:    &testpb.Lists{ByName: map[string]string{"owner": "bad"}},
			wantPath: `by_name["owner"]`,
		},
		{
			name:     "a bad id as a map key is refused at that key",
			input:    &testpb.Lists{ById: map[string]string{"bad": "value"}},
			wantPath: `by_id["bad"]`,
		},
		{
			name:     "a bad id as a value under an id key is refused at its key",
			input:    &testpb.Lists{Both: map[string]string{first: "bad"}},
			wantPath: `both["` + first + `"]`,
		},
		{
			name:     "a short binary id in a list is refused at its index",
			input:    &testpb.Lists{Binaries: [][]byte{{1}}},
			wantPath: "binaries[0]",
		},
		{
			name:  "empty lists and maps stay nil",
			input: &testpb.Lists{},
			want:  &testparsed.Lists{},
		},
		{
			name: "every list and map converts",
			input: &testpb.Lists{
				Ids:      []string{first, second},
				Names:    []string{"name"},
				ByName:   map[string]string{"owner": first},
				ById:     map[string]string{first: "value"},
				Both:     map[string]string{first: second},
				Plain:    map[string]int32{"count": 1},
				Binaries: [][]byte{firstID[:]},
				Plains:   plains,
			},
			want: &testparsed.Lists{
				Ids:      []uuid.UUID{firstID, secondID},
				Names:    []string{"name"},
				ByName:   map[string]uuid.UUID{"owner": firstID},
				ById:     map[uuid.UUID]string{firstID: "value"},
				Both:     map[uuid.UUID]uuid.UUID{firstID: secondID},
				Plain:    map[string]int32{"count": 1},
				Binaries: []uuid.UUID{firstID},
				Plains:   plains,
			},
		},
	})
}

// TestParseListsRefusesKeysThatConvertAlike covers two keys differing only in case.
func TestParseListsRefusesKeysThatConvertAlike(t *testing.T) {
	_, err := testparsed.ParseLists(&testpb.Lists{
		ById: map[string]string{first: "lower", "0192D2A1-7B3C-7DEF-8A12-3456789ABCDE": "upper"},
	})

	assert.ErrorIs(t, err, protoconv.ErrDuplicateKey)
}

// TestParseNested covers messages converted inside other messages, lists and maps.
func TestParseNested(t *testing.T) {
	bad := single(func(m *testpb.Single) { m.Id = "bad" })
	plain := &testpb.Plain{Name: "plain"}
	runParseTests(t, testparsed.ParseNested, []parseTest[*testpb.Nested, testparsed.Nested]{
		{
			name:     "a required message left unset is missing",
			input:    &testpb.Nested{},
			wantPath: "must",
			wantErr:  protoconv.ErrMissing,
		},
		{
			name:     "a bad id in a required message is refused under it",
			input:    &testpb.Nested{Must: bad},
			wantPath: "must.id",
		},
		{
			name:     "a bad id in an optional message is refused under it",
			input:    &testpb.Nested{Must: single(nil), Single: bad},
			wantPath: "single.id",
		},
		{
			name:     "a bad id in a listed message is refused under its index",
			input:    &testpb.Nested{Must: single(nil), Many: []*testpb.Single{single(nil), bad}},
			wantPath: "many[1].id",
		},
		{
			name:     "a bad id in a mapped message is refused under its key",
			input:    &testpb.Nested{Must: single(nil), ByNumber: map[int32]*testpb.Single{7: bad}},
			wantPath: "by_number[7].id",
		},
		{
			name:     "a bad id two levels down is refused under both",
			input:    &testpb.Nested{Must: single(nil), Child: &testpb.Nested{Must: single(nil), Single: bad}},
			wantPath: "child.single.id",
		},
		{
			name: "a message under an ignored field keeps its raw value",
			input: &testpb.Nested{
				Must:     single(nil),
				Single:   single(nil),
				Many:     []*testpb.Single{single(nil)},
				ByNumber: map[int32]*testpb.Single{7: single(nil)},
				Plain:    plain,
				Child:    &testpb.Nested{Must: single(nil)},
				Ignored:  bad,
			},
			want: &testparsed.Nested{
				Must:     parsedSingle(),
				Single:   parsedSingle(),
				Many:     []*testparsed.Single{parsedSingle()},
				ByNumber: map[int32]*testparsed.Single{7: parsedSingle()},
				Plain:    plain,
				Child:    &testparsed.Nested{Must: parsedSingle()},
				Ignored:  bad,
			},
		},
	})
}

// TestParseChoice covers each kind of oneof member.
func TestParseChoice(t *testing.T) {
	plain := &testpb.Plain{Name: "plain"}
	runParseTests(t, testparsed.ParseChoice, []parseTest[*testpb.Choice, testparsed.Choice]{
		{
			name:     "a bad id in a oneof is refused",
			input:    &testpb.Choice{Target: &testpb.Choice_Id{Id: "bad"}},
			wantPath: "id",
		},
		{
			name:     "a bad id in a message in a oneof is refused under it",
			input:    &testpb.Choice{Target: &testpb.Choice_Single{Single: single(func(m *testpb.Single) { m.Id = "bad" })}},
			wantPath: "single.id",
		},
		{
			name:        "a time out of range in a oneof is refused",
			input:       &testpb.Choice{Target: &testpb.Choice_At{At: &timestamppb.Timestamp{Nanos: -1}}},
			wantPath:    "at",
			beyondRules: true,
		},
		{
			name:  "an unset oneof converts to nil",
			input: &testpb.Choice{},
			want:  &testparsed.Choice{},
		},
		{
			name:  "an id in a oneof converts",
			input: &testpb.Choice{Target: &testpb.Choice_Id{Id: first}},
			want:  &testparsed.Choice{Target: &testparsed.Choice_Id{Id: firstID}},
		},
		{
			name:  "a raw value in a oneof is kept",
			input: &testpb.Choice{Target: &testpb.Choice_Name{Name: "name"}},
			want:  &testparsed.Choice{Target: &testparsed.Choice_Name{Name: "name"}},
		},
		{
			name:  "a message in a oneof converts",
			input: &testpb.Choice{Target: &testpb.Choice_Single{Single: single(nil)}},
			want:  &testparsed.Choice{Target: &testparsed.Choice_Single{Single: parsedSingle()}},
		},
		{
			name:  "a time in a oneof converts",
			input: &testpb.Choice{Target: &testpb.Choice_At{At: timestamppb.New(moment)}},
			want:  &testparsed.Choice{Target: &testparsed.Choice_At{At: moment}},
		},
		{
			name:  "a raw message in a oneof is kept",
			input: &testpb.Choice{Target: &testpb.Choice_Plain{Plain: plain}},
			want:  &testparsed.Choice{Target: &testparsed.Choice_Plain{Plain: plain}},
		},
	})
}

// TestParseEither covers fields a message-level oneof rule lets stay empty.
func TestParseEither(t *testing.T) {
	runParseTests(t, testparsed.ParseEither, []parseTest[*testpb.Either, testparsed.Either]{
		{
			name:     "a bad id named by a oneof rule is refused",
			input:    &testpb.Either{Id: "bad"},
			wantPath: "id",
		},
		{
			name:  "an id named by a oneof rule converts",
			input: &testpb.Either{Id: first},
			want:  &testparsed.Either{Id: &firstID},
		},
		{
			name:  "an empty id named by a oneof rule converts to nil",
			input: &testpb.Either{Name: "name"},
			want:  &testparsed.Either{Name: "name"},
		},
	})
}

// TestParsePick covers a oneof written as a type it shares with oneofs in other messages.
func TestParsePick(t *testing.T) {
	name := "name"
	runParseTests(t, testparsed.ParsePick, []parseTest[*testpb.Pick, testparsed.Pick]{
		{
			name:     "a bad id in a shared oneof is refused",
			input:    &testpb.Pick{Ref: &testpb.Pick_RefId{RefId: "bad"}},
			wantPath: "ref_id",
		},
		{
			name:  "an unset shared oneof converts to no member",
			input: &testpb.Pick{},
			want:  &testparsed.Pick{},
		},
		{
			name:  "an id in a shared oneof converts",
			input: &testpb.Pick{Ref: &testpb.Pick_RefId{RefId: first}},
			want:  &testparsed.Pick{Ref: testparsed.Ref{RefId: &firstID}},
		},
		{
			name:  "a raw value in a shared oneof is kept",
			input: &testpb.Pick{Ref: &testpb.Pick_RefName{RefName: name}},
			want:  &testparsed.Pick{Ref: testparsed.Ref{RefName: &name}},
		},
	})
}

// TestParsePoint covers a wrapped id in a shared oneof, which converts to the type the other members share.
func TestParsePoint(t *testing.T) {
	runParseTests(t, testparsed.ParsePoint, []parseTest[*testpb.Point, testparsed.Point]{
		{
			name:     "a bad wrapped id in a shared oneof is refused",
			input:    &testpb.Point{Ref: &testpb.Point_RefId{RefId: wrapperspb.String("bad")}},
			wantPath: "ref_id",
		},
		{
			name:  "a wrapped id in a shared oneof converts",
			input: &testpb.Point{Ref: &testpb.Point_RefId{RefId: wrapperspb.String(first)}},
			want:  &testparsed.Point{Ref: testparsed.Ref{RefId: &firstID}},
		},
	})
}

// TestParseWrapped covers ids held in string and bytes wrappers.
func TestParseWrapped(t *testing.T) {
	name := wrapperspb.String("name")
	runParseTests(t, testparsed.ParseWrapped, []parseTest[*testpb.Wrapped, testparsed.Wrapped]{
		{
			name:     "a required wrapped id left unset is missing",
			input:    &testpb.Wrapped{},
			wantPath: "must",
			wantErr:  protoconv.ErrMissing,
		},
		{
			name:     "a required wrapped id set to nothing is refused",
			input:    &testpb.Wrapped{Must: wrapperspb.String("")},
			wantPath: "must",
		},
		{
			name:     "an optional wrapped id set to a bad id is refused",
			input:    &testpb.Wrapped{Must: wrapperspb.String(first), Owner: wrapperspb.String("bad")},
			wantPath: "owner",
		},
		{
			name:     "a wrapped trimmed id with hyphens is refused",
			input:    &testpb.Wrapped{Must: wrapperspb.String(first), Trimmed: wrapperspb.String(first)},
			wantPath: "trimmed",
		},
		{
			name:     "a wrapped binary id of fifteen bytes is refused",
			input:    &testpb.Wrapped{Must: wrapperspb.String(first), Binary: wrapperspb.Bytes(firstID[:15])},
			wantPath: "binary",
		},
		{
			name:     "a bad wrapped id in a oneof is refused",
			input:    &testpb.Wrapped{Must: wrapperspb.String(first), Target: &testpb.Wrapped_Id{Id: wrapperspb.String("bad")}},
			wantPath: "id",
		},
		{
			name:  "unset wrapped ids convert to nil",
			input: &testpb.Wrapped{Must: wrapperspb.String(first)},
			want:  &testparsed.Wrapped{Must: firstID},
		},
		{
			name: "every wrapped id converts",
			input: &testpb.Wrapped{
				Owner:   wrapperspb.String(second),
				Must:    wrapperspb.String(first),
				Trimmed: wrapperspb.String(trimmed),
				Binary:  wrapperspb.Bytes(secondID[:]),
				Name:    name,
				Target:  &testpb.Wrapped_Id{Id: wrapperspb.String(second)},
			},
			want: &testparsed.Wrapped{
				Owner:   &secondID,
				Must:    firstID,
				Trimmed: &firstID,
				Binary:  &secondID,
				Name:    name,
				Target:  testparsed.Target{Id: &secondID},
			},
		},
	})
}

// TestParsePing covers messages that reach each other.
func TestParsePing(t *testing.T) {
	runParseTests(t, testparsed.ParsePing, []parseTest[*testpb.Ping, testparsed.Ping]{
		{
			name:     "a bad id reached through mutual recursion is refused",
			input:    &testpb.Ping{Pong: &testpb.Pong{Id: first, Ping: &testpb.Ping{Pong: &testpb.Pong{Id: "bad"}}}},
			wantPath: "pong.ping.pong.id",
		},
		{
			name:  "a message with no rules of its own converts through the one it reaches",
			input: &testpb.Ping{Name: "name", Pong: &testpb.Pong{Id: first, Ping: &testpb.Ping{}}},
			want:  &testparsed.Ping{Name: "name", Pong: &testparsed.Pong{Id: firstID, Ping: &testparsed.Ping{}}},
		},
	})
}

// TestParseOuter covers a message type declared inside another.
func TestParseOuter(t *testing.T) {
	runParseTests(t, testparsed.ParseOuter, []parseTest[*testpb.Outer, testparsed.Outer]{
		{
			name:     "a bad id in a nested message type is refused",
			input:    &testpb.Outer{Inner: &testpb.Outer_Inner{Id: "bad"}},
			wantPath: "inner.id",
		},
		{
			name:  "a nested message type converts",
			input: &testpb.Outer{Inner: &testpb.Outer_Inner{Id: first}},
			want:  &testparsed.Outer{Inner: &testparsed.Outer_Inner{Id: firstID}},
		},
	})
}

// TestParseExplicit covers fields of an edition file, which track presence by default.
func TestParseExplicit(t *testing.T) {
	runParseTests(t, testparsed.ParseExplicit, []parseTest[*testpb.Explicit, testparsed.Explicit]{
		{
			name:     "a required id left unset is missing",
			input:    &testpb.Explicit{ImplicitId: first},
			wantPath: "must",
			wantErr:  protoconv.ErrMissing,
		},
		{
			name:     "an id set to a bad id is refused",
			input:    &testpb.Explicit{Must: pointer(first), ImplicitId: first, Id: pointer("bad")},
			wantPath: "id",
		},
		{
			name:     "an empty id without presence is refused",
			input:    &testpb.Explicit{Must: pointer(first)},
			wantPath: "implicit_id",
		},
		{
			name: "every value converts",
			input: &testpb.Explicit{
				Id:         pointer(second),
				Must:       pointer(first),
				ImplicitId: first,
				Name:       pointer("name"),
				Count:      pointer[int32](2),
				Single:     single(nil),
			},
			want: &testparsed.Explicit{
				Id:         &secondID,
				Must:       firstID,
				ImplicitId: firstID,
				Name:       pointer("name"),
				Count:      pointer[int32](2),
				Single:     parsedSingle(),
			},
		},
		{
			name:  "an unset id converts to nil",
			input: &testpb.Explicit{Must: pointer(first), ImplicitId: first},
			want:  &testparsed.Explicit{Must: firstID, ImplicitId: firstID},
		},
	})
}

// TestParseRefusesNil covers a message that is not there at all.
func TestParseRefusesNil(t *testing.T) {
	_, err := testparsed.ParseSingle(nil)

	assert.ErrorIs(t, err, protoconv.ErrMissing)
}

// single is a Single that converts, changed by change.
func single(change func(*testpb.Single)) *testpb.Single {
	message := &testpb.Single{
		Id:      first,
		Trimmed: trimmed,
		Binary:  firstID[:],
		Must:    pointer(first),
	}
	if change != nil {
		change(message)
	}

	return message
}

func parsedSingle() *testparsed.Single {
	return &testparsed.Single{
		Id:      firstID,
		Trimmed: firstID,
		Binary:  firstID,
		Must:    firstID,
	}
}

func pointer[T any](value T) *T {
	return &value
}
