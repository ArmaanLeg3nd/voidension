package voidension

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func LoadConfig(configData []byte) {
	var loadedConfig configStruct
	err := yaml.Unmarshal(configData, &loadedConfig)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	configInstance = &loadedConfig
}

func isConfigInitialized() bool {
	return configInstance != nil
}

func getConfig() *configStruct {
	if configInstance == nil {
		log.Fatal("Config not initialized")
	}
	return configInstance
}

func copyConfig(original configStruct) *configStruct {
	var copied configStruct
	data, err := yaml.Marshal(original)
	if err != nil {
		log.Fatalf("Failed to copy global config: %v", err)
	}
	err = yaml.Unmarshal(data, &copied)
	if err != nil {
		log.Fatalf("Failed to copy global config: %v", err)
	}
	return &copied
}

func printASCIIArt() {
	asciiArt := `
             _     _                _             
 /\   /\___ (_) __| | ___ _ __  ___(_) ___  _ __  
 \ \ / /\_/\| |/ _` + "`" + ` |/ _ \ '_ \/ __| |/ _ \| '_ \ 
  \ V /--•--| | (_| |  __/ | | \__ \ | (_) | | | |
   \_/ \/_\/|_|\__,_|\___|_| |_|___/_|\___/|_| |_|
                                                  
`
	fmt.Println(asciiArt)
}

func (s *secure) initDir() error {
	if _, err := os.Stat(s.config.App.DirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.config.App.DirPath, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (s *secure) initLoggers() error {
	logFilePath := filepath.Join(s.config.App.DirPath, "Vlogs.txt")
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}

	accessLogFilePath := filepath.Join(s.config.App.DirPath, "Vaccess.txt")
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

func (s *secure) initServerPool() {
	for _, url := range s.config.Outgoing.ServerPostURLs {
		serverPool = append(serverPool, &serverStruct{URL: url, Locked: false, Alive: true})
	}
}

func findAvailableServer() *serverStruct {
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

func unlockServer(server *serverStruct) {
	mu.Lock()
	defer mu.Unlock()
	server.Locked = false
}

func isIPAllowed(ip string, allowedIPs []string) bool {
	if len(allowedIPs) == 0 {
		return true
	}

	for _, allowedIP := range allowedIPs {
		if ip == allowedIP {
			return true
		}
	}
	return false
}

func checkServerAvailability(checkAvailabilityTimeout int) {
	for {
		mu.Lock()
		for _, server := range serverPool {
			go func(s *serverStruct) {
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
		time.Sleep(time.Duration(checkAvailabilityTimeout) * time.Millisecond)
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
