package controller

import (
	"context"
	"io"
	"time"

	client "github.com/aserto-dev/go-aserto"
	api "github.com/aserto-dev/go-grpc/aserto/api/v2"
	management "github.com/aserto-dev/go-grpc/aserto/management/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

func (f *Factory) startController(ctx context.Context, tenantID, policyID, policyName, instanceLabel, host string, commandFunc CommandFunc) (func(), error) {
	logger := f.logger.With().Fields(map[string]interface{}{
		"tenant-id":      tenantID,
		"policy-id":      policyID,
		"policy-name":    policyName,
		"instance-label": instanceLabel,
		"host":           host,
	}).Logger()

	options, err := f.cfg.Server.ToConnectionOptions()
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup grpc dial options for the remote service")
	}

	options = append(options, client.WithTenantID(tenantID), client.WithDialOptions(f.dopts...))

	stop := make(chan bool)
	cleanup := func() {
		stop <- true
	}

	go func() {
		for {
			err = f.runCommandLoop(ctx, &logger, policyID, policyName, instanceLabel, host, commandFunc, stop, options)
			if err == nil || err == io.EOF {
				return
			}

			logger.Info().Err(err).Msg("command loop exited with error, restarting")
			time.Sleep(5 * time.Second)
		}
	}()

	return cleanup, nil
}

func (f *Factory) runCommandLoop(ctx context.Context, logger *zerolog.Logger, policyID, policyName, instanceLabel, host string, commandFunc CommandFunc, stop <-chan bool, opts []client.ConnectionOption) error {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := client.NewConnection(opts...)
	if err != nil {
		return errors.Wrap(err, "failed to connect to the control plane")
	}

	remoteCli := management.NewControllerClient(conn)
	stream, err := remoteCli.CommandStream(callCtx, &management.CommandStreamRequest{
		Info: &api.InstanceInfo{
			PolicyId:    policyID,
			PolicyName:  policyName,
			PolicyLabel: instanceLabel,
			RemoteHost:  host,
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
			err := commandFunc(bgCtx, cmd.Command)
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
