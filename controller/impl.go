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
)

func startController(ctx context.Context, factory *Factory, tenantID, policyID, host string, r *runtime.Runtime) (func(), error) {
	options, err := client.ConfigToConnectionOptions(&factory.cfg.Server, factory.dop)
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
			err = runCommandLoop(ctx, factory, policyID, host, r, stop, options)
			if err == nil || err == io.EOF {
				return
			}

			factory.logger.Info().Err(err).Msg("command loop exited with error, restarting")
			time.Sleep(5 * time.Second)
		}
	}()

	return cleanup, nil
}

func runCommandLoop(ctx context.Context, factory *Factory, policyID, host string, r *runtime.Runtime, stop <-chan bool, opts []gosdk.ConnectionOption) error {
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

			factory.logger.Trace().Msg("processing remote command")
			err := processCommand(bgCtx, r, cmd.Command)
			if err != nil {
				factory.logger.Error().Err(err).Msg("error processing command")
			}
			factory.logger.Trace().Msg("successfully processed remote command")
		}
	}()

	factory.logger.Trace().Msgf("running command loop for %s", r.Config.InstanceID)
	defer func() {
		factory.logger.Trace().Msgf("ended command loop for %s", r.Config.InstanceID)
	}()

	select {
	case err = <-errCh:
		factory.logger.Info().Err(err).Msg("error receiving command")
		return err
	case <-stop:
		return nil
	case <-stream.Context().Done():
		return stream.Context().Err()
	case <-ctx.Done():
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
