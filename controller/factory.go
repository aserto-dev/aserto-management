package controller

import (
	"context"

	api "github.com/aserto-dev/go-grpc/aserto/api/v2"
	"github.com/aserto-dev/go-utils/grpcclient"
	"github.com/rs/zerolog"
)

type CommandFunc func(context.Context, *api.Command) error

type Factory struct {
	logger *zerolog.Logger
	cfg    *Config
	dop    grpcclient.DialOptionsProvider
}

func NewFactory(logger *zerolog.Logger, cfg *Config, dop grpcclient.DialOptionsProvider) *Factory {
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

func (f *Factory) OnRuntimeStarted(ctx context.Context, tenantID, policyID, host string, commandFunc CommandFunc) (func(), error) {
	if f == nil {
		return func() {}, nil
	}

	return f.startController(ctx, tenantID, policyID, host, commandFunc)
}
