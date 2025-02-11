package controller

import (
	"context"
	"math"
	"time"

	client "github.com/aserto-dev/go-aserto"
	api "github.com/aserto-dev/go-grpc/aserto/api/v2"
	management "github.com/aserto-dev/go-grpc/aserto/management/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type SleepResult bool

const (
	timeout                     = 1 * time.Second
	maxBackoff                  = 600 * time.Second // max waits between retries.
	Canceled        SleepResult = true
	DurationReached SleepResult = false
)

func (f *Factory) startController(ctx context.Context, tenantID, policyID, policyName, instanceLabel, host string, commandFunc CommandFunc) (func(), error) {
	logger := f.logger.With().Fields(map[string]interface{}{
		"tenant-id":      tenantID,
		"policy-id":      policyID,
		"policy-name":    policyName,
		"instance-label": instanceLabel,
		"host":           host,
	}).Logger()

	errGroup := errgroup.Group{}

	conn, err := f.cfg.Server.Connect(client.WithTenantID(tenantID), client.WithDialOptions(f.dopts...))
	if err != nil {
		return func() {}, errors.Wrap(err, "failed to initialize new connection")
	}

	remoteCli := management.NewControllerClient(conn)
	ctx, cancel := context.WithCancel(ctx)

	errGroup.Go(func() error {
		for retry := 0; ; retry++ {
			retry++
			err = f.runCommandLoop(ctx, &logger, policyID, policyName, instanceLabel, host, commandFunc, remoteCli)
			if err == nil { // graceful shutdown on context canceled.
				return nil
			}
			logger.Info().Err(err).Msg("command loop exited with error, restarting")
			backoff := timeout * time.Duration(math.Pow(2, float64(retry)))
			if sleepWithContext(ctx, min(backoff, maxBackoff)) == Canceled {
				break
			}
		}

		return nil
	})

	return func() {
		cancel()
		if err = errGroup.Wait(); err != nil {
			logger.Error().Err(err).Msg("error cleanup")
		}
	}, nil
}

func (f *Factory) runCommandLoop(ctx context.Context, logger *zerolog.Logger, policyID, policyName, instanceLabel, host string, commandFunc CommandFunc, remoteCli management.ControllerClient) error {
	stream, err := remoteCli.CommandStream(ctx, &management.CommandStreamRequest{
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
		for {
			cmd, errRcv := stream.Recv()
			if errRcv != nil {
				errCh <- errRcv
				return
			}

			logger.Trace().Msg("processing remote command")
			err := commandFunc(ctx, cmd.Command)
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
	case <-stream.Context().Done():
		logger.Trace().Msg("stream context done")
		return stream.Context().Err()
	case <-ctx.Done():
		logger.Trace().Msg("context done")
		return nil
	}
}

func sleepWithContext(ctx context.Context, duration time.Duration) SleepResult {
	select {
	case <-ctx.Done():
		return Canceled
	case <-time.After(duration):
		return DurationReached
	}
}
