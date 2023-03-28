package controller

import (
	"github.com/aserto-dev/go-aserto/client"
)

type Config struct {
	Enabled bool           `json:"enabled"`
	Server  *client.Config `json:"server"`
}
