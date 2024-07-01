package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App struct {
		Port                     int    `yaml:"port"`
		DirPath                  string `yaml:"dirPath"`
		ReceivePath              string `yaml:"receivePath"`
		CheckAvailabilityTimeout int    `yaml:"checkAvailabilityTimeout"`
	} `yaml:"app"`
	Incoming struct {
		AllowedIPs []string `yaml:"allowedIPs"`
	} `yaml:"incoming"`
	Outgoing struct {
		ServerPostURLs []string `yaml:"serverPostURLs"`
	} `yaml:"outgoing"`
}

type Server struct {
	URL    string
	Locked bool
	Alive  bool
}

var (
	config        Config
	serverPool    []*Server
	mu            sync.Mutex
	requestQueue  = make(chan *http.Request, 100)
	responseQueue = make(chan *http.Response, 100)
	InfoLog       *log.Logger
	WarnLog       *log.Logger
	ErrorLog      *log.Logger
	AccessLog     *log.Logger
)

func initDir(config *Config) {
	if _, err := os.Stat(config.App.DirPath); os.IsNotExist(err) {
		err := os.MkdirAll(config.App.DirPath, 0755)
		if err != nil {
			log.Fatal("Failed to create directory:", err)
		}
	}
}

func initLoggers(config *Config) {
	logFilePath := filepath.Join(config.App.DirPath, "Vlogs.txt")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}

	accessLogFilePath := filepath.Join(config.App.DirPath, "Vaccess.txt")
	accessLogFile, err := os.OpenFile(accessLogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open access log file:", err)
	}

	logWriter := io.MultiWriter(logFile, os.Stdout)
	accessLogWriter := io.MultiWriter(accessLogFile, os.Stdout)

	InfoLog = log.New(logWriter, "V: INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	WarnLog = log.New(logWriter, "V: WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLog = log.New(logWriter, "V: ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	AccessLog = log.New(accessLogWriter, "V: ACCESS: ", log.Ldate|log.Ltime|log.Lshortfile)

	InfoLog.Println("Voidension started")
}

func loadConfig(configPath string) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		ErrorLog.Fatalf("Error reading configuration file: %v", err)
	}

	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		ErrorLog.Fatalf("Error parsing YAML configuration: %v", err)
	}
}

func initServerPool() {
	for _, url := range config.Outgoing.ServerPostURLs {
		serverPool = append(serverPool, &Server{URL: url, Locked: false, Alive: true})
	}
}

func findAvailableServer() *Server {
	mu.Lock()
	defer mu.Unlock()
	for _, server := range serverPool {
		if server.Alive && !server.Locked {
			server.Locked = true
			return server
		}
	}
	return nil
}

func unlockServer(server *Server) {
	mu.Lock()
	defer mu.Unlock()
	server.Locked = false
}

func forwardRequest(req *http.Request, server *Server) {
	client := &http.Client{Timeout: 10 * time.Second}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		ErrorLog.Printf("Failed to read request body: %v", err)
		http.Error(nil, "Failed to read request body", http.StatusInternalServerError)
		unlockServer(server)
		return
	}

	newReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		ErrorLog.Printf("Failed to create new request to %s: %v", server.URL, err)
		http.Error(nil, "Failed to create request", http.StatusInternalServerError)
		unlockServer(server)
		return
	}

	newReq.Header = req.Header

	resp, err := client.Do(newReq)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			WarnLog.Printf("Server %s timed out: %v", server.URL, err)
		} else {
			ErrorLog.Printf("Server %s error: %v", server.URL, err)
		}
		http.Error(nil, "Server error", http.StatusBadGateway)
		unlockServer(server)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		ErrorLog.Printf("Server %s returned error status: %d", server.URL, resp.StatusCode)
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

func isIPAllowed(ip string) bool {
	if len(config.Incoming.AllowedIPs) == 0 {
		return true
	}

	for _, allowedIP := range config.Incoming.AllowedIPs {
		if ip == allowedIP {
			return true
		}
	}
	return false
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
		WarnLog.Printf("Denied request from IP: %s", remoteIP)
		http.Error(w, "Access Denied", http.StatusForbidden)
		return
	}

	AccessLog.Printf("Received request from %s to %s", remoteIP, r.URL.String())

	server := findAvailableServer()
	if server == nil {
		requestQueue <- r
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		ErrorLog.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	newReq, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		ErrorLog.Printf("Failed to create new request to %s: %v", server.URL, err)
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
			WarnLog.Printf("Server %s timed out: %v", server.URL, err)
		} else {
			ErrorLog.Printf("Server %s error: %v", server.URL, err)
		}
		http.Error(w, "Server error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	unlockServer(server)

	if resp.StatusCode >= 500 {
		ErrorLog.Printf("Server %s returned error status: %d", server.URL, resp.StatusCode)
		http.Error(w, "Server error", http.StatusBadGateway)
		return
	}

	AccessLog.Printf("Forwarded request to %s returned status %d", server.URL, resp.StatusCode)

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func checkServerAvailability() {
	for {
		mu.Lock()
		for _, server := range serverPool {
			go func(s *Server) {
				hostPort := extractHostPort(s.URL)

				conn, err := net.DialTimeout("tcp", hostPort, 5*time.Second)
				if err != nil {
					s.Alive = false
					WarnLog.Printf("Server %s is down: %v", s.URL, err)
				} else {
					s.Alive = true
					conn.Close()
					InfoLog.Printf("Server %s is up", s.URL)
				}
			}(server)
		}
		mu.Unlock()
		time.Sleep(time.Duration(config.App.CheckAvailabilityTimeout) * time.Millisecond)
	}
}

func extractHostPort(url string) string {
	parts := strings.Split(url, "://")
	if len(parts) > 1 {
		url = parts[1]
	}

	parts = strings.Split(url, "/")
	return parts[0]
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to the YAML configuration file")
	flag.Parse()

	loadConfig(configPath)
	initDir(&config)
	initLoggers(&config)
	initServerPool()

	http.HandleFunc(config.App.ReceivePath, proxyHandler)
	go handleRequests()
	go checkServerAvailability()

	InfoLog.Printf("Starting the load balancer on port %d", config.App.Port)
	ErrorLog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.App.Port), nil))
}
