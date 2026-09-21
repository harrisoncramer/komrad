package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteCast covers the header, the marker per shot and the screen a repeated frame drops.
func TestWriteCast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params writeCastParams
		want   string
	}{
		{
			name: "no shots",
			params: writeCastParams{
				title: "empty",
				cols:  10,
			},
			want: `{"version":2,"width":10,"height":0,"title":"empty"}` + "\n",
		},
		{
			name: "a repeated frame keeps its marker and drops its screen",
			params: writeCastParams{
				title: "tour",
				cols:  3,
				pause: 1500 * time.Millisecond,
				shots: []shot{
					{
						label: "key j",
						frame: frame{lines: []string{"a", "b"}},
					},
					{
						label: "sleep 1s",
						frame: frame{lines: []string{"a", "b"}},
					},
					{
						label: "key k",
						frame: frame{lines: []string{"c"}},
					},
				},
			},
			want: `{"version":2,"width":3,"height":2,"title":"tour"}` + "\n" +
				`[0,"m","key j"]` + "\n" +
				`[0,"o","\u001b[2J\u001b[Ha\r\nb"]` + "\n" +
				`[1.5,"m","sleep 1s"]` + "\n" +
				`[3,"m","key k"]` + "\n" +
				`[3,"o","\u001b[2J\u001b[Hc"]` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buffer bytes.Buffer
			require.NoError(t, writeCast(&buffer, tt.params))
			assert.Equal(t, tt.want, buffer.String())
		})
	}
}
