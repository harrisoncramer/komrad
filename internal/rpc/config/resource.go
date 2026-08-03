package config

import (
	"encoding/json"
	"fmt"

	"weave/internal/appconfig"
	"weave/internal/dsn"

	"github.com/go-viper/mapstructure/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// renderConfig renders the loaded config into the blob a caller sees. It encodes
// through the mapstructure tags the loader already decodes by, so every key is
// named as it is in the user's file, and a setting added to Config appears here
// without anything else being touched. The trip through JSON is what turns the Go
// types the encoder hands back into ones a Struct will hold.
//
// The two connection strings are redacted on the way out. This is the one place the config
// becomes something a caller sees, and since the file can hold a database password, that
// password would otherwise reach every reader of this endpoint, the MCP surface included.
// Redacting here rather than in the loader is deliberate: the loader's output is what opens
// the connection, so a redacted value there would be a connection that does not work.
func renderConfig(cfg *appconfig.Config) (*structpb.Struct, error) {
	shown := *cfg
	shown.Connection.DatabaseURL = dsn.Redact(shown.Connection.DatabaseURL)
	shown.Connection.ReadOnlyDatabaseURL = dsn.Redact(shown.Connection.ReadOnlyDatabaseURL)

	var encoded map[string]any
	if err := mapstructure.Decode(&shown, &encoded); err != nil {
		return nil, fmt.Errorf("failed to encode the config: %w", err)
	}

	raw, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the config: %w", err)
	}

	blob := &structpb.Struct{}
	if err := protojson.Unmarshal(raw, blob); err != nil {
		return nil, fmt.Errorf("failed to read the config back: %w", err)
	}

	return blob, nil
}
