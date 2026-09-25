package config

import (
	"encoding/json"
	"fmt"

	komradconfig "komrad/config"
	"komrad/internal/dsn"

	"github.com/go-viper/mapstructure/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func renderConfig(cfg *komradconfig.Config) (*structpb.Struct, error) {
	shown := *cfg
	shown.Connection.DatabaseURL = dsn.Redact(shown.Connection.DatabaseURL)
	shown.Connection.ReadOnlyDatabaseURL = dsn.Redact(shown.Connection.ReadOnlyDatabaseURL)
	shown.Connection.OwnerDatabaseURL = dsn.Redact(shown.Connection.OwnerDatabaseURL)

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
