package voidension

type configStruct struct {
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

type secure struct {
	config *configStruct
}

type serverStruct struct {
	URL    string
	Locked bool
	Alive  bool
}
