package voidension

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// takes a YAML byte slice and loads it into the configStruct, and it is then
// stored in the package global variable configInstance.
func LoadConfig(configData []byte) {
	var loadedConfig configStruct
	err := yaml.Unmarshal(configData, &loadedConfig)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	err = validateConfig(loadedConfig)
	if err != nil {
		log.Fatalf("Error validating config: %v", err)
	}

	configInstance = &loadedConfig
}

func validateConfig(loadedConfig configStruct) error {
	if loadedConfig.App.Port < 1 || loadedConfig.App.Port > 65535 {
		return fmt.Errorf("invalid port: The port must be within valid range")
	}

	if loadedConfig.App.DirPath == "" {
		return fmt.Errorf("the dirPath value must not be empty")
	}

	if loadedConfig.App.ReceivePath == "" {
		return fmt.Errorf("the receivePath value must not be empty")
	}

	if loadedConfig.App.CheckAvailabilityTimeout < 0 {
		return fmt.Errorf("the checkAvailabilityTimeout value cannot be negative or 0")
	} else if loadedConfig.App.CheckAvailabilityTimeout == 0 {
		loadedConfig.App.CheckAvailabilityTimeout = 1000
	}

	if loadedConfig.App.MaxRetries < 0 {
		return fmt.Errorf("the maxRetries value cannot be less than or equal to 0")
	} else if loadedConfig.App.MaxRetries == 0 {
		loadedConfig.App.MaxRetries = 3
	}

	if loadedConfig.App.LargeBodyThreshold < 0 {
		return fmt.Errorf("the largeBodyThreshold value cannot be less than or equal to 0")
	} else if loadedConfig.App.LargeBodyThreshold == 0 {
		loadedConfig.App.LargeBodyThreshold = 1024 * 1024 // 1MB default
	}

	if loadedConfig.App.BaseBackoffTime < 0 {
		return fmt.Errorf("the baseBackoffTime value cannot be negative or 0")
	} else if loadedConfig.App.BaseBackoffTime == 0 {
		loadedConfig.App.BaseBackoffTime = 100
	}

	if len(loadedConfig.Outgoing.ServerPostURLs) == 0 {
		return fmt.Errorf("the serverPostURLs list must have at least one item")
	}

	return nil
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

// initDir creates the directory specified by App.DirPath if it does not exist.
func (s *secure) initDir() error {
	if _, err := os.Stat(s.config.App.DirPath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.config.App.DirPath, 0755); err != nil {
			return err
		}
	}

	var err error
	tempDir, err = os.MkdirTemp(s.config.App.DirPath, "request_buffer")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}

	return nil
}

// initLoggers sets up the loggers for voidension.
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

// initServerPool creates a pool of server structs from the serverPostURLs
// specified in the configuration.
func (s *secure) initServerPool() {
	serverPool = nil
	for _, url := range s.config.Outgoing.ServerPostURLs {
		serverPool = append(serverPool, &serverStruct{URL: url, Locked: false, Alive: true})
	}
}

// setupShutdown sets up a goroutine to handle SIGINT and SIGTERM. When
// received, it logs a message, cleans up temporary files, and exits the program.
func setupShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		infoLog.Println("Shutting down voidension...")

		if tempDir != "" {
			infoLog.Println("Cleaning up temporary files...")
			os.RemoveAll(tempDir)
		}

		infoLog.Println("Shutdown complete")
		os.Exit(0)
	}()
}

// startStatsLogger starts a goroutine to log the number of active servers in
// the serverPool at the given interval.
func startStatsLogger(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			mu.Lock()
			activeServers := 0
			for _, server := range serverPool {
				if server.Alive {
					activeServers++
				}
			}
			mu.Unlock()

			infoLog.Printf("Stats: %d/%d servers active", activeServers, len(serverPool))
		}
	}()
}

// createRequestBuffer creates a requestBuffer from the given http.Request,
// remoteIP and currentIP. It copies the headers and body of the request to the
// requestBuffer, and if the body is larger than the LargeBodyThreshold
// configuration, it writes the body to a temporary file and stores the file
// path in the requestBuffer.
func createRequestBuffer(r *http.Request, remoteIP, currentIP string) (*requestBuffer, error) {
	var reqBuffer requestBuffer
	reqBuffer.headers = make(http.Header)
	reqBuffer.method = r.Method
	reqBuffer.remoteIP = remoteIP
	reqBuffer.currentIP = currentIP

	for k, v := range r.Header {
		reqBuffer.headers[k] = v
	}

	if r.Body == nil {
		return &reqBuffer, fmt.Errorf("request body is nil")
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body.Close()

	if int64(len(bodyBytes)) > getConfig().App.LargeBodyThreshold {
		tempFile, err := os.CreateTemp(tempDir, "req_body_*.tmp")
		if err != nil {
			return nil, err
		}

		_, err = tempFile.Write(bodyBytes)
		if err != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
			return nil, err
		}

		reqBuffer.isLarge = true
		reqBuffer.filePath = tempFile.Name()
		tempFile.Close()
	} else {
		reqBuffer.isLarge = false
		reqBuffer.body = bodyBytes
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return &reqBuffer, nil
}

// Get the body from a request buffer (either from memory or from file)
func (rb *requestBuffer) getBody() ([]byte, error) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	if rb.isLarge {
		body, err := os.ReadFile(rb.filePath)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	return rb.body, nil
}

func (rb *requestBuffer) cleanup() {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	if rb.isLarge && rb.filePath != "" {
		os.Remove(rb.filePath)
		rb.filePath = ""
	}
}

// Finds an available server in the pool. An available server is one that is
// alive and not currently locked.
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

func extractHostPort(url string) string {
	parts := strings.Split(url, "://")
	if len(parts) > 1 {
		url = parts[1]
	}

	parts = strings.Split(url, "/")
	return parts[0]
}

// checkServerAvailability starts a goroutine for each server in the serverPool
// to check if the server is up or down.
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
