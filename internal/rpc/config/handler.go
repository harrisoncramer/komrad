// Package config serves the configuration RPC, which reports the settings komrade is running on
// as they were resolved rather than as they were written.
package config

import (
	"context"

	komradeconfig "komrade/config"
	"komrade/internal/fail"
	"komrade/internal/rpcerr"
	"komrade/internal/version"
	configv1 "komrade/proto/gen/config/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type loader func() (*komradeconfig.Config, error)

type Handler struct {
	load loader
}

// NewHandler constructs the handler from the loader it reads the config through.
func NewHandler(load loader) *Handler {
	return &Handler{load: load}
}

// GetConfig returns the configuration in force and the build answering, falling back to the
// built-in defaults and the reason when the user's file will not parse.
func (s *Handler) GetConfig(_ context.Context, _ *emptypb.Empty) (*configv1.GetConfigResponse, *fail.Error) {
	var configError *string

	cfg, err := s.load()
	if err != nil {
		cfg = komradeconfig.Defaults()
		configError = proto.String(err.Error())
	}

	blob, err := renderConfig(cfg)
	if err != nil {
		return nil, rpcerr.From(err, "failed to render the config")
	}

	return &configv1.GetConfigResponse{
		Config:      blob,
		ConfigError: configError,
		Version:     version.Version(),
	}, nil
}
