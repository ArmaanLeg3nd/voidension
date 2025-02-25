package voidension

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
