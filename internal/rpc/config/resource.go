package config

import (
	"encoding/json"
	"fmt"

	"weave/internal/appconfig"

	"github.com/go-viper/mapstructure/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// renderConfig renders the loaded config into the blob a caller sees. It encodes
// through the mapstructure tags the loader already decodes by, so every key is
// named as it is in the user's file, and a setting added to Config appears here
// without anything else being touched. The trip through JSON is what turns the Go
// types the encoder hands back into ones a Struct will hold.
func renderConfig(cfg *appconfig.Config) (*structpb.Struct, error) {
	var encoded map[string]any
	if err := mapstructure.Decode(cfg, &encoded); err != nil {
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
