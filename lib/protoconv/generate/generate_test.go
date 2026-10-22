package generate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/compiler/protogen"

	"komrad/lib/protoconv/generate"
	"komrad/lib/protoconv/internal/prototest"
)

const outputPath = protogen.GoImportPath("example.com/parsed")

// TestWrite covers the messages the generator refuses and the ones it has nothing to write for.
func TestWrite(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    []bool
		wantErr error
	}{
		{
			name: "a message in the output package is refused",
			sources: map[string]string{
				"same.proto": `syntax = "proto3";
package same;
import "buf/validate/validate.proto";
option go_package = "example.com/parsed";
message Thing { string id = 1 [(buf.validate.field).string.uuid = true]; }`,
			},
			wantErr: generate.ErrSamePackage,
		},
		{
			name: "a message generated with the opaque API is refused",
			sources: map[string]string{
				"opaque.proto": `edition = "2023";
package opaque;
import "buf/validate/validate.proto";
import "google/protobuf/go_features.proto";
option go_package = "example.com/opaque";
option features.(pb.go).api_level = API_OPAQUE;
message Thing { string id = 1 [(buf.validate.field).string.uuid = true]; }`,
			},
			wantErr: generate.ErrOpaqueAPI,
		},
		{
			name: "two messages wanting one Go name are refused",
			sources: map[string]string{
				"a.proto": `syntax = "proto3";
package a;
import "buf/validate/validate.proto";
option go_package = "example.com/a";
message Thing { string id = 1 [(buf.validate.field).string.uuid = true]; }`,
				"b.proto": `syntax = "proto3";
package b;
import "buf/validate/validate.proto";
option go_package = "example.com/b";
message Thing { string id = 1 [(buf.validate.field).string.uuid = true]; }`,
			},
			wantErr: generate.ErrNameTaken,
		},
		{
			name: "list elements that may be empty are refused",
			sources: map[string]string{
				"list.proto": `syntax = "proto3";
package list;
import "buf/validate/validate.proto";
option go_package = "example.com/list";
message Thing {
  repeated string ids = 1 [
    (buf.validate.field).repeated.items.string.uuid = true,
    (buf.validate.field).repeated.items.ignore = IGNORE_IF_ZERO_VALUE
  ];
}`,
			},
			wantErr: generate.ErrUnrepresentable,
		},
		{
			name: "map keys that may be empty are refused",
			sources: map[string]string{
				"keys.proto": `syntax = "proto3";
package keys;
import "buf/validate/validate.proto";
option go_package = "example.com/keys";
message Thing {
  map<string, int32> by_id = 1 [
    (buf.validate.field).map.keys.string.uuid = true,
    (buf.validate.field).map.keys.ignore = IGNORE_IF_ZERO_VALUE
  ];
}`,
			},
			wantErr: generate.ErrUnrepresentable,
		},
		{
			name: "only messages with something to convert are written",
			sources: map[string]string{
				"plain.proto": `syntax = "proto3";
package plain;
import "buf/validate/validate.proto";
import "google/protobuf/timestamp.proto";
option go_package = "example.com/plain";
message Plain { string name = 1; Plain next = 2; }
message Skipped { google.protobuf.Timestamp at = 1 [(buf.validate.field).ignore = IGNORE_ALWAYS]; }
message Timed { google.protobuf.Timestamp at = 1; }`,
			},
			want: []bool{false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin, err := prototest.Compile(t.Context(), tt.sources)
			require.NoError(t, err)

			got, err := writeAll(plugin)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// writeAll writes every message of the files plugin generates, reporting which had something to write.
func writeAll(plugin *protogen.Plugin) ([]bool, error) {
	generator := generate.New(plugin.NewGeneratedFile("parsed.go", outputPath), outputPath)
	var written []bool
	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}

		for _, message := range file.Messages {
			wrote, err := generator.Write(message)
			if err != nil {
				return nil, err
			}
			written = append(written, wrote)
		}
	}

	return written, nil
}
