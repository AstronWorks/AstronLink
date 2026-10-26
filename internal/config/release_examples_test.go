package config_test

import (
	"testing"

	"github.com/EcoKG/reversproxy/internal/config"
)

// The shipped example configs must stay loadable (KnownFields is strict).
func TestReleaseExampleConfigsLoad(t *testing.T) {
	if _, err := config.LoadServerConfig("../../release/server.yaml"); err != nil {
		t.Fatalf("release/server.yaml: %v", err)
	}
	if _, err := config.LoadClientConfig("../../release/client.yaml"); err != nil {
		t.Fatalf("release/client.yaml: %v", err)
	}
}
