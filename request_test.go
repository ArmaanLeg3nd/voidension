package voidension

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsIPAllowed(t *testing.T) {
	allowedIPs := []string{"192.168.1.1", "10.0.0.1"}

	if !isIPAllowed("192.168.1.1", allowedIPs) {
		t.Fatal("isIPAllowed should allow 192.168.1.1")
	}

	if isIPAllowed("192.168.2.1", allowedIPs) {
		t.Fatal("isIPAllowed should deny 192.168.2.1")
	}
}

func TestEmptyRequestBuffer(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("Expected method POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	loadTestConfig([]string{backend.URL})

	secureInstance := &secure{config: copyConfig(*getConfig())}
	if secureInstance.config == nil {
		t.Fatal("secureInstance is nil")
	}

	secureInstance.initDir()
	secureInstance.initLoggers()
	secureInstance.initServerPool()

	// Create a mock request
	req, err := http.NewRequest(http.MethodPost, "http://localhost", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Use a response recorder
	w := httptest.NewRecorder()

	secureInstance.proxyHandler(w, req)

	// Verify response
	resp := w.Result()
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Expected status code 500, got %d", resp.StatusCode)
	}

	// Check if "Failed to buffer request: request body is nil" has been logged
	logPath := filepath.Join(getConfig().App.DirPath, "VLogs.txt")
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file %s: %v", logPath, err)
	}

	logLines := strings.Split(strings.TrimSpace(string(logs)), "\n")
	if len(logLines) == 0 || !strings.HasSuffix(logLines[len(logLines)-1], "Failed to buffer request: request body is nil") {
		t.Fatalf("Expected last log to end with 'Failed to buffer request: request body is nil', but got %s", logLines[len(logLines)-1])
	}

}

func TestRequestForwarding(t *testing.T) {
	// Create a mock backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("Expected method POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	loadTestConfig([]string{backend.URL})

	secureInstance := &secure{config: copyConfig(*getConfig())}
	if secureInstance.config == nil {
		t.Fatal("secureInstance is nil")
	}

	secureInstance.initDir()
	secureInstance.initLoggers()
	secureInstance.initServerPool()

	// Create a mock request
	req, err := http.NewRequest(http.MethodPost, "http://localhost", bytes.NewBuffer([]byte(`{"data": "test"}`)))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Use a response recorder
	w := httptest.NewRecorder()

	secureInstance.proxyHandler(w, req)

	// Verify response
	resp := w.Result()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	if string(body) != "OK" {
		t.Fatalf("Expected body 'OK', got '%s'", string(body))
	}
}

func TestProxyHandlerMethodValidation(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://localhost", nil)
	w := &mockResponseWriter{header: http.Header{}}

	secureInstance := &secure{config: &configStruct{}}
	secureInstance.proxyHandler(w, req)

	if w.status != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status 405, got %d", w.status)
	}
}

// Mock response writer for testing.
type mockResponseWriter struct {
	header http.Header
	status int
}

func (m *mockResponseWriter) Header() http.Header         { return m.header }
func (m *mockResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (m *mockResponseWriter) WriteHeader(statusCode int)  { m.status = statusCode }
