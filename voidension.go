package voidension

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	configInstance *configStruct
	serverPool     []*serverStruct
	mu             sync.Mutex
	infoLog        *log.Logger
	warnLog        *log.Logger
	errorLog       *log.Logger
	accessLog      *log.Logger
	tempDir        string
)

// Launch initializes and starts the Voidension load balancer with the given
// configuration. It configures the directory, loggers, server pool, and starts
// the HTTP server. It also starts a goroutine to periodically check the
// availability of the servers and logs statistics about the load balancer every
// 10 seconds.
func Launch() {
	if !isConfigInitialized() {
		log.Fatal("Config not set!")
	}

	secure := &secure{config: copyConfig(*getConfig())}

	printASCIIArt()

	err := secure.initDir()
	if err != nil {
		log.Fatalf("Error creating directory: %v", err)
	}

	err = secure.initLoggers()
	if err != nil {
		log.Fatalf("Error initializing loggers: %v", err)
	}

	secure.initServerPool()
	http.HandleFunc(secure.config.App.ReceivePath, secure.proxyHandler)
	go checkServerAvailability(secure.config.App.CheckAvailabilityTimeout)
	startStatsLogger(10 * time.Second)
	setupShutdown()

	infoLog.Printf("Starting the load balancer on port %d", secure.config.App.Port)
	infoLog.Printf("Attempting to forward requests at most %d times before failing", secure.config.App.MaxRetries)
	infoLog.Printf("Base backoff time: %d ms", secure.config.App.BaseBackoffTime)
	infoLog.Printf("Large request threshold: %d bytes", secure.config.App.LargeBodyThreshold)

	errorLog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", secure.config.App.Port), nil))
}
