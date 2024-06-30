package main

import (
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

func initdir(config *Config) {
	if _, err := os.Stat(config.App.DirPath); os.IsNotExist(err) {
		err := os.MkdirAll(config.App.DirPath, 0755)
		if err != nil {
			log.Fatal("Failed to create directory:", err)
		}
	}
}

func initLoggers(config *Config) {
	logfilepath := filepath.Join(config.App.DirPath, "Vlogs.txt")
	logfile, err := os.OpenFile(logfilepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}

	accessLogFilePath := filepath.Join(config.App.DirPath, "Vaccess.txt")
	accessLogFile, err := os.OpenFile(accessLogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal("Failed to open access log file:", err)
	}

	logwriter := io.MultiWriter(logfile, os.Stdout)
	accessLogWriter := io.MultiWriter(accessLogFile, os.Stdout)

	InfoLog = log.New(logwriter, "V: INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	WarnLog = log.New(logwriter, "V: WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLog = log.New(logwriter, "V: ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
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
	req.RequestURI = ""
	req.URL.Scheme = "http"
	req.URL.Host = server.URL

	resp, err := client.Do(req)
	if err != nil {
		if netErr, ok := err.(net.Error); ok {
			if netErr.Timeout() || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "connection refused") {
				WarnLog.Printf("Server %s unreachable or timed out: %v", server.URL, err)
				requestQueue <- req
			} else {
				ErrorLog.Printf("Server %s error: %v", server.URL, err)
			}
		} else {
			ErrorLog.Printf("Server %s error: %v", server.URL, err)
		}
		unlockServer(server)
		return
	}

	if resp.StatusCode >= 500 {
		ErrorLog.Printf("Server %s returned error status: %d", server.URL, resp.StatusCode)
		resp.Body.Close()
		unlockServer(server)
		return
	}

	responseQueue <- resp
	unlockServer(server)
}

func handleRequests() {
	for req := range requestQueue {
		go func(req *http.Request) {
			server := findAvailableServer()
			if server != nil {
				forwardRequest(req, server)
			} else {
				requestQueue <- req
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
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "Unable to parse remote address", http.StatusInternalServerError)
		return
	}

	if !isIPAllowed(remoteIP) {
		WarnLog.Printf("Denied request from IP: %s", remoteIP)
		http.Error(w, "Access Denied", http.StatusForbidden)
		return
	}

	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP == "" || xRealIP != remoteIP {
		r.Header.Set("X-Real-IP", remoteIP)
	}

	req, err := http.NewRequest(r.Method, "", r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header

	AccessLog.Printf("Received request from %s to %s", remoteIP, req.URL.String())

	requestQueue <- req

	resp := <-responseQueue
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	resp.Body.Close()
}

func checkServerAvailability() {
	for {
		mu.Lock()
		for _, server := range serverPool {
			go func(s *Server) {
				conn, err := net.DialTimeout("tcp", s.URL, 5*time.Second)
				if err != nil {
					s.Alive = false
				} else {
					s.Alive = true
					conn.Close()
				}
			}(server)
		}
		mu.Unlock()
		time.Sleep(time.Duration(config.App.CheckAvailabilityTimeout) * time.Second)
	}
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to the YAML configuration file")
	flag.Parse()

	loadConfig(configPath)
	initdir(&config)
	initLoggers(&config)
	initServerPool()

	http.HandleFunc(config.App.ReceivePath, proxyHandler)
	go handleRequests()
	go checkServerAvailability()

	InfoLog.Printf("Starting the load balancer on port %d", config.App.Port)
	ErrorLog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.App.Port), nil))
}
