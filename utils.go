package voidension

import (
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func loadConfig(configData []byte) error {
	err := yaml.Unmarshal(configData, &config)
	return err
}

func initDir(config *Config) error {
	if _, err := os.Stat(config.App.DirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(config.App.DirPath, 0755); err != nil {
			return err
		}
	}
	return nil
}

func initLoggers(config *Config) error {
	logFilePath := filepath.Join(config.App.DirPath, "Vlogs.txt")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}

	accessLogFilePath := filepath.Join(config.App.DirPath, "Vaccess.txt")
	accessLogFile, err := os.OpenFile(accessLogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}

	logWriter := io.MultiWriter(logFile, os.Stdout)
	accessLogWriter := io.MultiWriter(accessLogFile, os.Stdout)

	infoLog = log.New(logWriter, "V: INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	warnLog = log.New(logWriter, "V: WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	errorLog = log.New(logWriter, "V: ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	accessLog = log.New(accessLogWriter, "V: ACCESS: ", log.Ldate|log.Ltime|log.Lshortfile)

	infoLog.Println("Voidension started")

	return nil
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

func checkServerAvailability() {
	for {
		mu.Lock()
		for _, server := range serverPool {
			go func(s *Server) {
				hostPort := extractHostPort(s.URL)

				conn, err := net.DialTimeout("tcp", hostPort, 5*time.Second)
				if err != nil {
					s.Alive = false
					warnLog.Printf("Server %s is down: %v", s.URL, err)
				} else {
					s.Alive = true
					conn.Close()
					infoLog.Printf("Server %s is up", s.URL)
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
