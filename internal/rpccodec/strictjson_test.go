package rpccodec

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestStrictJSONUnmarshal pins that a payload naming something the schema does not have is
// refused rather than read as what is left once the unknown parts are dropped.
func TestStrictJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    agentv1.MailDirection
		wantErr bool
	}{
		{
			name:    "the name of an enum value",
			payload: `{"direction":"MAIL_DIRECTION_SENT"}`,
			want:    agentv1.MailDirection_MAIL_DIRECTION_SENT,
		},
		{
			name:    "the number of an enum value",
			payload: `{"direction":2}`,
			want:    agentv1.MailDirection_MAIL_DIRECTION_SENT,
		},
		{
			name:    "an omitted field keeps its zero value",
			payload: `{}`,
			want:    agentv1.MailDirection_MAIL_DIRECTION_UNSPECIFIED,
		},
		{
			name:    "an enum value the schema does not have",
			payload: `{"direction":"SENT"}`,
			wantErr: true,
		},
		{
			name:    "a field the schema does not have",
			payload: `{"directoin":"MAIL_DIRECTION_SENT"}`,
			wantErr: true,
		},
		{
			name:    "an empty payload",
			payload: ``,
			wantErr: true,
		},
	}

	codec := strictJSON{name: nameJSON}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request agentv1.ListMailRequest

			err := codec.Unmarshal([]byte(tt.payload), &request)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, request.GetDirection())
		})
	}
}

// TestStrictJSONMarshal pins that the codec still writes what connect's own JSON codec writes,
// only the reading side of it being meant to differ.
func TestStrictJSONMarshal(t *testing.T) {
	codec := strictJSON{name: nameJSON}
	message := &agentv1.ListMailRequest{Direction: agentv1.MailDirection_MAIL_DIRECTION_SENT}

	encoded, err := codec.Marshal(message)
	require.NoError(t, err)

	appended, err := codec.MarshalAppend([]byte("head:"), message)
	require.NoError(t, err)
	assert.Equal(t, "head:"+string(encoded), string(appended))

	assert.False(t, codec.IsBinary())

	var read agentv1.ListMailRequest
	require.NoError(t, codec.Unmarshal(encoded, &read))
	assert.True(t, proto.Equal(message, &read))
}

// TestStrictJSONRejectsNonProto pins that a value the codec cannot speak for is refused on both
// sides.
func TestStrictJSONRejectsNonProto(t *testing.T) {
	codec := strictJSON{name: nameJSON}

	_, err := codec.Marshal("not a message")
	require.Error(t, err)

	_, err = codec.MarshalAppend(nil, "not a message")
	require.Error(t, err)

	require.Error(t, codec.Unmarshal([]byte(`{}`), "not a message"))
}

// TestStrictJSONNames pins the two content types connect looks the JSON codec up by, which a
// caller spelling out the charset has to reach it through.
func TestStrictJSONNames(t *testing.T) {
	assert.Equal(t, "json", strictJSON{name: nameJSON}.Name())
	assert.Equal(t, "json; charset=utf-8", strictJSON{name: nameJSONCharsetUTF8}.Name())
}
