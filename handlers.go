package voidension

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// forwards an HTTP request to an available server from the server pool.
// It retries the forwarding process a specified number of times if a server is unavailable
// or if an error occurs. The function handles the request and response headers,
// manages server locking, and logs the outcomes. If all attempts fail, it sends an
// appropriate error response to the client.
func forwardRequest(w http.ResponseWriter, reqBuffer *requestBuffer) {
	maxRetries := getConfig().App.MaxRetries
	var lastErr error
	var lastStatusCode int
	baseBackoffTime := time.Duration(getConfig().App.BaseBackoffTime) * time.Millisecond // Starting with 100ms
	maxBackoffTime := 30 * time.Second                                                   // Cap at 30 seconds
	jitterFactor := 0.2                                                                  // 20% random jitter

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Find an available server
		server := findAvailableServer()
		if server == nil {
			// Calculate exponential backoff with jitter
			backoffDuration := time.Duration(float64(baseBackoffTime) * math.Pow(2, float64(attempt)))
			if backoffDuration > maxBackoffTime {
				backoffDuration = maxBackoffTime
			}

			// Add jitter to prevent thundering herd problem
			jitter := time.Duration(float64(backoffDuration) * jitterFactor * (1 - 2*rand.Float64()))
			backoffDuration = backoffDuration + jitter

			infoLog.Printf("No available servers. Retrying in %v (attempt %d/%d)", backoffDuration, attempt+1, maxRetries)
			time.Sleep(backoffDuration)
			continue
		}

		infoLog.Printf("Attempt %d: Forwarding request to %s", attempt+1, server.URL)

		// Get the body from the buffer
		bodyBytes, err := reqBuffer.getBody()
		if err != nil {
			errorLog.Printf("Failed to get request body: %v", err)
			unlockServer(server)
			lastErr = err

			// Calculate exponential backoff for next attempt
			backoffDuration := time.Duration(float64(baseBackoffTime) * math.Pow(2, float64(attempt)))
			if backoffDuration > maxBackoffTime {
				backoffDuration = maxBackoffTime
			}

			// Add jitter
			jitter := time.Duration(float64(backoffDuration) * jitterFactor * (1 - 2*rand.Float64()))
			backoffDuration = backoffDuration + jitter

			infoLog.Printf("Request failed. Retrying in %v (attempt %d/%d)", backoffDuration, attempt+1, maxRetries)
			time.Sleep(backoffDuration)
			continue
		}

		// Create a new request
		proxyReq, err := http.NewRequest(reqBuffer.method, server.URL, bytes.NewReader(bodyBytes))
		if err != nil {
			errorLog.Printf("Failed to create request: %v", err)
			unlockServer(server)
			lastErr = err

			// Calculate exponential backoff for next attempt
			backoffDuration := time.Duration(float64(baseBackoffTime) * math.Pow(2, float64(attempt)))
			if backoffDuration > maxBackoffTime {
				backoffDuration = maxBackoffTime
			}

			// Add jitter
			jitter := time.Duration(float64(backoffDuration) * jitterFactor * (1 - 2*rand.Float64()))
			backoffDuration = backoffDuration + jitter

			infoLog.Printf("Request creation failed. Retrying in %v (attempt %d/%d)", backoffDuration, attempt+1, maxRetries)
			time.Sleep(backoffDuration)
			continue
		}

		for key, values := range reqBuffer.headers {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		// Add or update forwarded headers
		currentXFF := proxyReq.Header.Get("X-Forwarded-For")
		if currentXFF == "" {
			currentXFF = reqBuffer.remoteIP
		}

		proxyReq.Header.Del("X-Forwarded-For")
		proxyReq.Header.Add("X-Forwarded-For", currentXFF+","+reqBuffer.currentIP)
		proxyReq.Header.Add("X-Real-IP", reqBuffer.remoteIP)

		// Set timeout for the request
		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		// Send the request
		resp, err := client.Do(proxyReq)

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				warnLog.Printf("Server %s timed out: %v", server.URL, err)
			} else {
				errorLog.Printf("Server %s error: %v", server.URL, err)
			}
			unlockServer(server)
			lastErr = err

			// Calculate exponential backoff for next attempt
			backoffDuration := time.Duration(float64(baseBackoffTime) * math.Pow(2, float64(attempt)))
			if backoffDuration > maxBackoffTime {
				backoffDuration = maxBackoffTime
			}

			// Add jitter
			jitter := time.Duration(float64(backoffDuration) * jitterFactor * (1 - 2*rand.Float64()))
			backoffDuration = backoffDuration + jitter

			infoLog.Printf("Request failed. Retrying in %v (attempt %d/%d)", backoffDuration, attempt+1, maxRetries)
			time.Sleep(backoffDuration)
			continue
		}

		// Check if the server returned an error status
		if resp.StatusCode >= 500 {
			errorLog.Printf("Server %s returned error status: %d - Passing through to client", server.URL, resp.StatusCode)
			lastStatusCode = resp.StatusCode

			// Pass the 5xx error response directly back to the client without retrying
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			resp.Body.Close()
			unlockServer(server)
			return
		}

		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)

		accessLog.Printf("Successfully forwarded request to %s, returned status %d", server.URL, resp.StatusCode)
		unlockServer(server)
		return
	}

	errorLog.Printf("All retry attempts failed")

	if lastStatusCode >= 500 {
		http.Error(w, "Upstream server error", lastStatusCode)
	} else if lastErr != nil {
		errMsg := fmt.Sprintf("Service unavailable: %v", lastErr)
		http.Error(w, errMsg, http.StatusServiceUnavailable)
	} else {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
	}
}

// handles incoming HTTP requests to the proxy endpoint. It enforces that only
// POST requests are allowed, verifies if the request's originating IP is
// permitted, logs the request, buffers it, and forwards it to an available
// backend server.
func (s *secure) proxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	remoteIP := r.Header.Get("X-Real-IP")
	currentIP := r.RemoteAddr
	if remoteIP == "" {
		remoteIP, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	if !isIPAllowed(remoteIP, s.config.Incoming.AllowedIPs) {
		warnLog.Printf("Denied request from IP: %s", remoteIP)
		http.Error(w, "Access Denied", http.StatusForbidden)
		return
	}

	accessLog.Printf("Received request from %s to %s", remoteIP, r.URL.String())

	reqBuffer, err := createRequestBuffer(r, remoteIP, currentIP)
	if err != nil {
		errorLog.Printf("Failed to buffer request: %v", err)
		http.Error(w, "Failed to process request", http.StatusInternalServerError)
		return
	}
	defer reqBuffer.cleanup() // Clean up temporary files when done

	forwardRequest(w, reqBuffer)
}
