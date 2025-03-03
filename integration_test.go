package voidension

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Mock server to simulate backend responses
func mockServer(responseBody string, statusCode int) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(responseBody))
	})
	return httptest.NewServer(handler)
}

// Helper function to load test config
func loadTestConfig(serverURLs []string) {
	testConfig := fmt.Sprintf(`
app:
  port: 8080
  dirPath: "./test_logs"
  receivePath: "/proxy"
  checkAvailabilityTimeout: 1000
  maxRetries: 3
  baseBackoffTime: 1000
  largeBodyThreshold: 1048576
incoming:
  allowedIPs: []
outgoing:
  serverPostURLs:
%s
`, formatServerURLs(serverURLs))

	LoadConfig([]byte(testConfig))
}

// Helper function to format server URLs for YAML
func formatServerURLs(urls []string) string {
	result := ""
	for _, url := range urls {
		result += fmt.Sprintf("    - \"%s\"\n", url)
	}
	return result
}

const (
	numRequests = 1000
	targetURL   = "http://localhost:8080/proxy"
)

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sendPostRequest(wg *sync.WaitGroup, id int, results map[string]int, mu *sync.Mutex) {
	defer wg.Done()
	message := randomString(10)
	jsonPayload := fmt.Sprintf(`{"data":"%s"}`, message)

	body := bytes.NewBuffer([]byte(jsonPayload))
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Post(targetURL, "application/json", body)
	if err != nil {
		fmt.Printf("Request %d failed: %v\n", id, err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	mu.Lock()
	results[string(respBody)]++
	mu.Unlock()

	fmt.Printf("Request %d completed with status: %s\n", id, resp.Status)
}

// Helper test function to launch the server in a separate process
func TestLaunchProcess(t *testing.T) {
	testCase := os.Getenv("TEST_CASE") // Read test case from env variable

	var backend *httptest.Server

	switch testCase {
	case "proxy-request-flow":
		backend = mockServer(`{"message": "success"}`, http.StatusOK)
	case "failover-test":
		backend1 := mockServer(`{"message": "backend1"}`, http.StatusInternalServerError)
		backend2 := mockServer(`{"message": "backend2"}`, http.StatusOK)
		loadTestConfig([]string{backend1.URL, backend2.URL})
		Launch()
		return
	case "load-balancing-test":
		backend1 := mockServer(`{"message": "backend1"}`, http.StatusOK)
		backend2 := mockServer(`{"message": "backend2"}`, http.StatusOK)
		loadTestConfig([]string{backend1.URL, backend2.URL})
		Launch()
		return
	default:
		t.Skip("Skipping test")
	}

	defer backend.Close()
	loadTestConfig([]string{backend.URL})

	Launch()
}

// Test End-to-End Request Flow
func TestProxyRequestFlow(t *testing.T) {
	os.Setenv("TEST_CASE", "proxy-request-flow")
	defer os.Unsetenv("TEST_CASE") // Clean up after the test

	// Start the server in a separate process
	cmd := exec.Command(os.Args[0], "-test.run=TestLaunchProcess")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start server process: %v", err)
	}

	// Ensure process is killed when the test exits
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	})

	// Give server time to start
	time.Sleep(7 * time.Second)

	client := &http.Client{}
	req, _ := http.NewRequest("POST", "http://localhost:8080/proxy", bytes.NewBuffer([]byte(`{"data": "test"}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	expected := `{"message": "success"}`
	if string(body) != expected {
		t.Fatalf("Expected %s, got %s", expected, string(body))
	}
}

// Test Server Failover
func TestServerFailover(t *testing.T) {
	os.Setenv("TEST_CASE", "failover-test")
	defer os.Unsetenv("TEST_CASE") // Clean up after the test

	// Start the server in a separate process
	cmd := exec.Command(os.Args[0], "-test.run=TestLaunchProcess")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start server process: %v", err)
	}

	// Ensure process is killed when the test exits
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	})

	// Give server time to start
	time.Sleep(7 * time.Second)

	client := &http.Client{}
	req, _ := http.NewRequest("POST", "http://localhost:8080/proxy", bytes.NewBuffer([]byte(`{"data": "test"}`)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	expected := `{"message": "backend1"}`
	if string(body) != expected {
		t.Errorf("Expected %s, got %s", expected, string(body))
	}
}

// Test Load Balancing
func TestLoadBalancing(t *testing.T) {
	os.Setenv("TEST_CASE", "load-balancing-test")
	defer os.Unsetenv("TEST_CASE") // Clean up after the test

	cmd := exec.Command(os.Args[0], "-test.run=TestLaunchProcess")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server process: %v", err)
	}

	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	})

	time.Sleep(7 * time.Second) // Give server time to start

	var wg sync.WaitGroup
	results := make(map[string]int)
	var mu sync.Mutex

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go sendPostRequest(&wg, i, results, &mu)
	}

	wg.Wait()

	if len(results) < 2 {
		t.Errorf("Expected load balancing across multiple servers, but got responses: %v", results)
	}
}

// Test Log Generation
func TestLogGeneration(t *testing.T) {
	os.Setenv("TEST_CASE", "proxy-request-flow")
	defer os.Unsetenv("TEST_CASE") // Clean up after the test

	// Start the server in a separate process
	cmd := exec.Command(os.Args[0], "-test.run=TestLaunchProcess")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start server process: %v", err)
	}

	// Ensure process is killed when the test exits
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	})

	// Give server time to start
	time.Sleep(7 * time.Second)

	client := &http.Client{}
	req, _ := http.NewRequest("POST", "http://localhost:8080/proxy", bytes.NewBuffer([]byte(`{"data": "test"}`)))
	req.Header.Set("Content-Type", "application/json")

	_, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	time.Sleep(1 * time.Second) // Wait for logs to be written

	logData, err := os.ReadFile("./test_logs/Vaccess.txt")
	if err != nil {
		t.Fatalf("Failed to read access log: %v", err)
	}

	if !bytes.Contains(logData, []byte("Received request")) {
		t.Errorf("Access log does not contain expected log entry")
	}
}
