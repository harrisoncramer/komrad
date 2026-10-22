package protoconv_test

import (
	"errors"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"komrad/lib/protoconv"
	"komrad/lib/protoconv/internal/testpb"
)

const sample = "0192d2a1-7b3c-7def-8a12-3456789abcde"

// TestUUID covers the hyphenated form, the only one string.uuid accepts.
func TestUUID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    uuid.UUID
		wantErr bool
	}{
		{
			name:    "an empty string is refused",
			value:   "",
			wantErr: true,
		},
		{
			name:    "the form without hyphens is refused",
			value:   "0192d2a17b3c7def8a123456789abcde",
			wantErr: true,
		},
		{
			name:    "a braced UUID is refused",
			value:   "{" + sample + "}",
			wantErr: true,
		},
		{
			name:    "a urn UUID is refused",
			value:   "urn:uuid:" + sample,
			wantErr: true,
		},
		{
			name:    "a hyphen out of place is refused",
			value:   "0192d2a17-b3c-7def-8a12-3456789abcde",
			wantErr: true,
		},
		{
			name:    "a character outside hex is refused",
			value:   "0192d2a1-7b3c-7def-8a12-3456789abcdg",
			wantErr: true,
		},
		{
			name:  "upper case hex converts",
			value: "0192D2A1-7B3C-7DEF-8A12-3456789ABCDE",
			want:  uuid.MustParse(sample),
		},
		{
			name:  "a hyphenated UUID converts",
			value: sample,
			want:  uuid.MustParse(sample),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protoconv.UUID(tt.value)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTrimmedUUID covers the form without hyphens, the only one string.tuuid accepts.
func TestTrimmedUUID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    uuid.UUID
		wantErr bool
	}{
		{
			name:    "an empty string is refused",
			value:   "",
			wantErr: true,
		},
		{
			name:    "the hyphenated form is refused",
			value:   sample,
			wantErr: true,
		},
		{
			name:    "a character outside hex is refused",
			value:   "0192d2a17b3c7def8a123456789abcdg",
			wantErr: true,
		},
		{
			name:  "a UUID without hyphens converts",
			value: "0192d2a17b3c7def8a123456789abcde",
			want:  uuid.MustParse(sample),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protoconv.TrimmedUUID(tt.value)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBytesUUID covers the sixteen bytes bytes.uuid accepts.
func TestBytesUUID(t *testing.T) {
	want := uuid.MustParse(sample)
	tests := []struct {
		name    string
		value   []byte
		wantErr bool
	}{
		{
			name:    "no bytes are refused",
			value:   nil,
			wantErr: true,
		},
		{
			name:    "fifteen bytes are refused",
			value:   want[:15],
			wantErr: true,
		},
		{
			name:    "the text of a UUID is refused",
			value:   []byte(sample),
			wantErr: true,
		},
		{
			name:  "sixteen bytes convert",
			value: want[:],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protoconv.BytesUUID(tt.value)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestTime covers a timestamp that is missing, out of range, or converts.
func TestTime(t *testing.T) {
	moment := time.Date(2026, 10, 22, 12, 30, 0, 500, time.UTC)
	tests := []struct {
		name        string
		timestamp   *timestamppb.Timestamp
		want        time.Time
		wantMissing bool
		wantErr     bool
	}{
		{
			name:        "a missing timestamp is refused as missing",
			timestamp:   nil,
			wantMissing: true,
			wantErr:     true,
		},
		{
			name:      "nanos past a second are refused",
			timestamp: &timestamppb.Timestamp{Seconds: 1, Nanos: 1_000_000_000},
			wantErr:   true,
		},
		{
			name:      "a time past the year 9999 is refused",
			timestamp: &timestamppb.Timestamp{Seconds: 253402300800},
			wantErr:   true,
		},
		{
			name:      "the epoch converts",
			timestamp: &timestamppb.Timestamp{},
			want:      time.Unix(0, 0).UTC(),
		},
		{
			name:      "a timestamp converts in UTC",
			timestamp: timestamppb.New(moment),
			want:      moment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protoconv.Time(tt.timestamp)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, tt.wantMissing, errors.Is(err, protoconv.ErrMissing))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestPath covers the path an error builds as it passes up through fields, list elements and map entries.
func TestPath(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a field names itself",
			err:  protoconv.Field("id", protoconv.ErrMissing),
			want: "id",
		},
		{
			name: "a nested field joins with a dot",
			err:  protoconv.Field("single", protoconv.Field("id", protoconv.ErrMissing)),
			want: "single.id",
		},
		{
			name: "a list element follows its field",
			err:  protoconv.Field("many", protoconv.Index(2, protoconv.Field("id", protoconv.ErrMissing))),
			want: "many[2].id",
		},
		{
			name: "a string key is quoted",
			err:  protoconv.Field("by_id", protoconv.Key("a\"b", protoconv.ErrMissing)),
			want: `by_id["a\"b"]`,
		},
		{
			name: "a number key is bare",
			err:  protoconv.Field("by_number", protoconv.Key(int32(-3), protoconv.ErrMissing)),
			want: "by_number[-3]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var failure *protoconv.Error
			require.ErrorAs(t, tt.err, &failure)
			assert.Equal(t, tt.want, failure.Path)
			require.ErrorIs(t, tt.err, protoconv.ErrMissing)
			assert.EqualError(t, tt.err, tt.want+": value is required")
		})
	}
}

// FuzzUUID holds UUID to accepting exactly what protovalidate's string.uuid rule accepts.
func FuzzUUID(f *testing.F) {
	for _, seed := range []string{"", sample, "0192d2a17b3c7def8a123456789abcde", "{" + sample + "}"} {
		f.Add(seed)
	}
	validator := newValidator(f)

	f.Fuzz(func(t *testing.T, value string) {
		_, err := protoconv.UUID(value)
		valid := validator.Validate(&testpb.HyphenatedRule{Value: value}) == nil
		assert.Equal(t, valid, err == nil, "value %q", value)
	})
}

// FuzzTrimmedUUID holds TrimmedUUID to accepting exactly what protovalidate's string.tuuid rule accepts.
func FuzzTrimmedUUID(f *testing.F) {
	for _, seed := range []string{"", sample, "0192d2a17b3c7def8a123456789abcde", "0192d2a17b3c7def8a123456789abcdg"} {
		f.Add(seed)
	}
	validator := newValidator(f)

	f.Fuzz(func(t *testing.T, value string) {
		_, err := protoconv.TrimmedUUID(value)
		valid := validator.Validate(&testpb.TrimmedRule{Value: value}) == nil
		assert.Equal(t, valid, err == nil, "value %q", value)
	})
}

// FuzzBytesUUID holds BytesUUID to accepting exactly what protovalidate's bytes.uuid rule accepts.
func FuzzBytesUUID(f *testing.F) {
	parsed := uuid.MustParse(sample)
	for _, seed := range [][]byte{nil, parsed[:], parsed[:15], []byte(sample)} {
		f.Add(seed)
	}
	validator := newValidator(f)

	f.Fuzz(func(t *testing.T, value []byte) {
		_, err := protoconv.BytesUUID(value)
		valid := validator.Validate(&testpb.BinaryRule{Value: value}) == nil
		assert.Equal(t, valid, err == nil, "value %x", value)
	})
}

func newValidator(f *testing.F) protovalidate.Validator {
	f.Helper()

	validator, err := protovalidate.New()
	require.NoError(f, err)

	return validator
}
