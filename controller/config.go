package controller

import "github.com/aserto-dev/go-utils/grpcclient"

type Config struct {
	Enabled bool              `json:"enabled"`
	Server  grpcclient.Config `json:"server"`
}
