package voidension

import (
	"net/http"
	"sync"
)

type configStruct struct {
	App struct {
		Port                     int    `yaml:"port"`
		DirPath                  string `yaml:"dirPath"`
		ReceivePath              string `yaml:"receivePath"`
		CheckAvailabilityTimeout int    `yaml:"checkAvailabilityTimeout"`
		MaxRetries               int    `yaml:"maxRetries"`
		BaseBackoffTime          int    `yaml:"baseBackoffTime"`
		LargeBodyThreshold       int64  `yaml:"largeBodyThreshold"`
	} `yaml:"app"`
	Incoming struct {
		AllowedIPs []string `yaml:"allowedIPs"`
	} `yaml:"incoming"`
	Outgoing struct {
		ServerPostURLs []string `yaml:"serverPostURLs"`
	} `yaml:"outgoing"`
}

type requestBuffer struct {
	body      []byte // For small request bodies (in-memory)
	filePath  string // For large request bodies (file-based)
	headers   http.Header
	method    string
	isLarge   bool
	remoteIP  string
	currentIP string
	mutex     sync.Mutex
}

type secure struct {
	config *configStruct
}

type serverStruct struct {
	URL    string
	Locked bool
	Alive  bool
}
