package controller

import (
	"context"
	"io"
	"time"

	gosdk "github.com/aserto-dev/aserto-go/client"
	"github.com/aserto-dev/aserto-management/api/management/v2"
	"github.com/aserto-dev/go-grpc/aserto/api/v2"
	"github.com/aserto-dev/go-lib/grpc-clients/client"
	"github.com/aserto-dev/runtime"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

func (f *Factory) startController(ctx context.Context, tenantID, policyID, host string, r *runtime.Runtime) (func(), error) {
	logger := f.logger.With().Fields(map[string]interface{}{
		"tenant-id": tenantID,
		"policy-id": policyID,
		"host":      host,
	}).Logger()

	options, err := client.ConfigToConnectionOptions(&f.cfg.Server, f.dop)
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup grpc dial options for the remote service")
	}

	options = append(options, gosdk.WithTenantID(tenantID))

	stop := make(chan bool)
	cleanup := func() {
		stop <- true
	}

	go func() {
		for {
			err = f.runCommandLoop(ctx, &logger, policyID, host, r, stop, options)
			if err == nil || err == io.EOF {
				return
			}

			logger.Info().Err(err).Msg("command loop exited with error, restarting")
			time.Sleep(5 * time.Second)
		}
	}()

	return cleanup, nil
}

func (f *Factory) runCommandLoop(ctx context.Context, logger *zerolog.Logger, policyID, host string, r *runtime.Runtime, stop <-chan bool, opts []gosdk.ConnectionOption) error {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := gosdk.NewConnection(callCtx, opts...)
	if err != nil {
		return errors.Wrap(err, "failed to connect to the control plane")
	}

	remoteCli := management.NewControllerClient(conn.Conn)
	stream, err := remoteCli.CommandStream(callCtx, &management.CommandStreamRequest{
		Info: &api.InstanceInfo{
			PolicyId:   policyID,
			RemoteHost: host,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to establish command stream with control plane")
	}

	errCh := make(chan error)

	go func() {
		bgCtx := context.Background()
		for {
			cmd, errRcv := stream.Recv()
			if errRcv != nil {
				errCh <- errRcv
				return
			}

			logger.Trace().Msg("processing remote command")
			err := processCommand(bgCtx, r, cmd.Command)
			if err != nil {
				logger.Error().Err(err).Msg("error processing command")
			}
			logger.Trace().Msg("successfully processed remote command")
		}
	}()

	logger.Trace().Msg("command loop running")
	defer func() {
		f.logger.Trace().Msg("command loop ended")
	}()

	select {
	case err = <-errCh:
		logger.Info().Err(err).Msg("error receiving command")
		return err
	case <-stop:
		logger.Trace().Msg("received stop signal")
		return nil
	case <-stream.Context().Done():
		logger.Trace().Msg("stream context done")
		return stream.Context().Err()
	case <-ctx.Done():
		logger.Trace().Msg("context done")
		return nil
	}
}

func processCommand(ctx context.Context, r *runtime.Runtime, cmd *api.Command) error {
	switch cmd.Data.(type) {
	case *api.Command_Discovery:
		plugin := r.PluginsManager.Plugin(discovery.Name)
		if plugin == nil {
			return errors.Errorf("failed to find discovery plugin")
		}

		discoveryPlugin, ok := plugin.(*discovery.Discovery)
		if !ok {
			return errors.Errorf("failed to cast discovery plugin")
		}

		err := discoveryPlugin.Trigger(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to trigger discovery")
		}
	default:
		return errors.New("not implemented")
	}

	return nil
}
