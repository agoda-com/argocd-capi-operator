package cluster

import (
	"time"
)

type Config struct {
	Instance           string
	Namespace          string
	ServiceAccountName string
	TokenTTL           time.Duration
}
