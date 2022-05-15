package controller

import (
	"context"

	"github.com/aserto-dev/go-lib/grpc-clients/client"
	"github.com/aserto-dev/runtime"
	"github.com/rs/zerolog"
)

type Factory struct {
	logger *zerolog.Logger
	cfg    *Config
	dop    client.DialOptionsProvider
}

func NewFactory(logger *zerolog.Logger, cfg *Config, dop client.DialOptionsProvider) *Factory {
	if !cfg.Enabled {
		return nil
	}

	newLogger := logger.With().Str("component", "controller").Logger()
	return &Factory{
		logger: &newLogger,
		cfg:    cfg,
		dop:    dop,
	}
}

func (f *Factory) OnRuntimeStarted(ctx context.Context, tenantID, policyID, host string, r *runtime.Runtime) (func(), error) {
	if f == nil {
		return func() {}, nil
	}

	return startController(ctx, f, tenantID, policyID, host, r)
}
