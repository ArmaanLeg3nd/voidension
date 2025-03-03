package voidension

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	yamlData := `
app:
  port: 8080
  dirPath: "./logs"
  receivePath: "/receive"
  checkAvailabilityTimeout: 5000
  maxRetries: 3
  baseBackoffTime: 1000
  largeBodyThreshold: 1048576
incoming:
  allowedIPs: ["192.168.1.1", "10.0.0.1"]
outgoing:
  serverPostURLs: ["http://server1.com", "http://server2.com"]
`
	LoadConfig([]byte(yamlData))

	if !isConfigInitialized() {
		t.Fatal("Config should be initialized after LoadConfig")
	}

	config := getConfig()
	if config.App.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", config.App.Port)
	}
}

func TestIsConfigInitialized(t *testing.T) {
	if !isConfigInitialized() {
		t.Fatal("isConfigInitialized should return true after loading config")
	}
}

func TestCopyConfig(t *testing.T) {
	original := *getConfig()
	copied := copyConfig(original)

	if copied.App.Port != original.App.Port {
		t.Errorf("Expected copied config port %d, got %d", original.App.Port, copied.App.Port)
	}

	copied.App.Port = 9090
	if copied.App.Port == original.App.Port {
		t.Errorf("Copy should be independent, but modifying copied changed original")
	}
}
