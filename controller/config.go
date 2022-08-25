package controller

import "github.com/aserto-dev/aserto-grpc/grpcclient"

type Config struct {
	Enabled bool              `json:"enabled"`
	Server  grpcclient.Config `json:"server"`
}
