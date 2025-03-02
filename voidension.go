package voidension

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

var (
	configInstance *configStruct
	serverPool     []*serverStruct
	mu             sync.Mutex
	requestQueue   = make(chan *http.Request, 100)
	responseQueue  = make(chan *http.Response, 100)
	infoLog        *log.Logger
	warnLog        *log.Logger
	errorLog       *log.Logger
	accessLog      *log.Logger
)

func Launch() {
	if !isConfigInitialized() {
		log.Fatal("Config not set!")
	}

	secure := &secure{config: copyConfig(*getConfig())}

	printASCIIArt()

	err := secure.initDir()
	if err != nil {
		errorLog.Fatalf("Error creating directory: %v", err)
	}

	err = secure.initLoggers()
	if err != nil {
		log.Fatalf("Error initializing loggers: %v", err)
	}

	secure.initServerPool()
	http.HandleFunc(secure.config.App.ReceivePath, secure.proxyHandler)
	go handleRequests()
	go checkServerAvailability(secure.config.App.CheckAvailabilityTimeout)
	infoLog.Printf("Starting the load balancer on port %d", secure.config.App.Port)
	errorLog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", secure.config.App.Port), nil))
}
