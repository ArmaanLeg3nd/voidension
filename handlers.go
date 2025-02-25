package voidension

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"time"
)

func forwardRequest(req *http.Request, server *Server) {
	client := &http.Client{Timeout: 10 * time.Second}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		errorLog.Printf("Failed to read request body: %v", err)
		http.Error(nil, "Failed to read request body", http.StatusInternalServerError)
		unlockServer(server)
		return
	}

	newReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		errorLog.Printf("Failed to create new request to %s: %v", server.URL, err)
		http.Error(nil, "Failed to create request", http.StatusInternalServerError)
		unlockServer(server)
		return
	}

	newReq.Header = req.Header

	resp, err := client.Do(newReq)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			warnLog.Printf("Server %s timed out: %v", server.URL, err)
		} else {
			errorLog.Printf("Server %s error: %v", server.URL, err)
		}
		http.Error(nil, "Server error", http.StatusBadGateway)
		unlockServer(server)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		errorLog.Printf("Server %s returned error status: %d", server.URL, resp.StatusCode)
		http.Error(nil, "Server error", http.StatusBadGateway)
		unlockServer(server)
		return
	}

	responseQueue <- resp
}

func handleRequests() {
	for req := range requestQueue {
		go func(req *http.Request) {
			for {
				server := findAvailableServer()
				if server != nil {
					forwardRequest(req, server)
					break
				} else {
					time.Sleep(100 * time.Millisecond)
				}
			}
		}(req)
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	remoteIP := r.Header.Get("X-Real-IP")
	currentIP := r.RemoteAddr
	if remoteIP == "" {
		remoteIP, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	if !isIPAllowed(remoteIP) {
		warnLog.Printf("Denied request from IP: %s", remoteIP)
		http.Error(w, "Access Denied", http.StatusForbidden)
		return
	}

	accessLog.Printf("Received request from %s to %s", remoteIP, r.URL.String())

	server := findAvailableServer()
	if server == nil {
		requestQueue <- r
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		errorLog.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	newReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		errorLog.Printf("Failed to create new request to %s: %v", server.URL, err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	newReq.Header = make(http.Header)
	for key, values := range r.Header {
		newReq.Header[key] = values
	}

	currentXFF := r.Header.Get("X-Forwarded-For")
	if currentXFF == "" {
		currentXFF = remoteIP
	}

	newReq.Header.Del("X-Forwarded-For")
	newReq.Header.Add("X-Forwarded-For", currentXFF+","+currentIP)
	newReq.Header.Add("X-Real-IP", remoteIP)

	resp, err := client.Do(newReq)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			warnLog.Printf("Server %s timed out: %v", server.URL, err)
		} else {
			errorLog.Printf("Server %s error: %v", server.URL, err)
		}
		http.Error(w, "Server error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	unlockServer(server)

	if resp.StatusCode >= 500 {
		errorLog.Printf("Server %s returned error status: %d", server.URL, resp.StatusCode)
		http.Error(w, "Server error", http.StatusBadGateway)
		return
	}

	accessLog.Printf("Forwarded request to %s returned status %d", server.URL, resp.StatusCode)

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
