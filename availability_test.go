package voidension

import "testing"

func TestExtractHostPort(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"http://example.com:8080/path", "example.com:8080"},
		{"https://example.com", "example.com"},
		{"example.com:3000", "example.com:3000"},
	}

	for _, test := range tests {
		if got := extractHostPort(test.url); got != test.expected {
			t.Fatalf("extractHostPort(%s) = %s; want %s", test.url, got, test.expected)
		}
	}
}

func TestMockCheckServerAvailability(t *testing.T) {
	serverPool = []*serverStruct{
		{URL: "http://server1.com", Alive: false},
	}

	mockCheckServerAvailability()

	if !serverPool[0].Alive {
		t.Fatal("Server should be marked as alive in mock test")
	}
}

func mockCheckServerAvailability() {
	for _, server := range serverPool {
		server.Alive = true
	}
}
