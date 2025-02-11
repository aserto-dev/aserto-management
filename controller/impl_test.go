package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/aserto-dev/go-aserto"
	api "github.com/aserto-dev/go-grpc/aserto/api/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNoConnectionOptionsController(t *testing.T) {
	logger := zerolog.Nop()
	factory := NewFactory(&logger, &Config{Enabled: true, Server: &aserto.Config{}}, nil)
	cleanup, err := factory.startController(context.Background(), "test-tenant-id", "", "test-policy", "", "", func(ctx context.Context, cmd *api.Command) error {
		switch cmd.Data.(type) {
		case *api.Command_Discovery:
			t.Log("received discovery command")

		case *api.Command_SyncEdgeDirectory:
			t.Log("received edge sync command")
		default:
			return errors.New("not implemented")
		}
		return nil
	})
	defer cleanup()
	assert.Error(t, err, "invalid connection options")
}
