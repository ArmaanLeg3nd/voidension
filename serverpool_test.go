package voidension

import (
	"testing"
)

func TestFindAvailableServer(t *testing.T) {
	serverPool = []*serverStruct{
		{URL: "http://server1.com", Locked: false, Alive: true},
		{URL: "http://server2.com", Locked: false, Alive: true},
	}

	server := findAvailableServer()
	if server == nil {
		t.Fatal("findAvailableServer failed to find an available server")
	}

	if !server.Locked {
		t.Fatal("findAvailableServer should lock the selected server")
	}
}

func TestUnlockServer(t *testing.T) {
	server := &serverStruct{URL: "http://server1.com", Locked: true}
	unlockServer(server)

	if server.Locked {
		t.Fatal("unlockServer should release the lock")
	}
}

func TestInitServerPool(t *testing.T) {
	configInstance = &configStruct{
		Outgoing: struct {
			ServerPostURLs []string `yaml:"serverPostURLs"`
		}{
			ServerPostURLs: []string{"http://server1.com", "http://server2.com"},
		},
	}

	secureInstance := &secure{config: configInstance}
	secureInstance.initServerPool()

	if len(serverPool) != 2 {
		t.Fatalf("Expected 2 servers in pool, got %d", len(serverPool))
	}
}
