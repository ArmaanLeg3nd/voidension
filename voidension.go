package voidension

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

var (
	config        Config
	serverPool    []*Server
	mu            sync.Mutex
	requestQueue  = make(chan *http.Request, 100)
	responseQueue = make(chan *http.Response, 100)
	infoLog       *log.Logger
	warnLog       *log.Logger
	errorLog      *log.Logger
	accessLog     *log.Logger
)

func Launch(configData []byte) {

	printASCIIArt()
	err := loadConfig(configData)

	if err != nil {
		errorLog.Fatalf("Error parsing YAML configuration: %v", err)
	}

	err = initDir(&config)

	if err != nil {
		errorLog.Fatalf("Error creating directory: %v", err)
	}

	err = initLoggers(&config)

	if err != nil {
		log.Fatalf("Error initializing loggers: %v", err)
	}

	initServerPool()

	http.HandleFunc(config.App.ReceivePath, proxyHandler)
	go handleRequests()
	go checkServerAvailability()

	infoLog.Printf("Starting the load balancer on port %d", config.App.Port)
	errorLog.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.App.Port), nil))
}
