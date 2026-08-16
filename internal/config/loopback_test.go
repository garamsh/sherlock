package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/gagent/internal/config"
)

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"ipv4 loopback", "127.0.0.1:8080", true},
		{"ipv4 loopback range", "127.1.2.3:8080", true},
		{"localhost", "localhost:8080", true},
		{"localhost case-insensitive", "LOCALHOST:8080", true},
		{"ipv6 loopback", "[::1]:8080", true},
		{"wildcard empty host", ":8080", false},
		{"wildcard ipv4", "0.0.0.0:8080", false},
		{"wildcard ipv6", "[::]:8080", false},
		{"lan address", "192.168.1.10:8080", false},
		{"hostname", "gateway.example.com:8080", false},
		{"unparseable fails safe", "not-an-address", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, config.IsLoopbackAddr(tt.addr))
		})
	}
}
