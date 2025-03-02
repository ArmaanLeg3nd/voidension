<br />
<div align="center">
  <h3 align="center">voidension</h3>

  <p align="center">
    A fast, simple and easy to configure Load Balancer
    <br />
    <br />
  </p>
</div>
<br />

### About Voidension

This project is designed to provide a simple, fast and easy to configure load balancer. It checks if a server is alive by a simple tcp dial. It forwards the request to the any server among the server list that is available. Since it aims for simplicity, it only supports load balancing based on the server availabilty. It locks the server that is being used to prevent multiple requests to the same server. It also supports a request queue to handle the requests when none of the servers are available.

### Instructions for Setup

#### 1. Install

Run the following command in the command line to install voidension 

```shell
go get github.com/armaanleg3nd/voidension
```

#### 2. Import voidension

Import voidension in your code by adding the following package to your imports:

```go
import "github.com/armaanleg3nd/voidension"
```

#### 3. Configure and Launch  

Create a YAML configuration file and define the following parameters under the app, incoming and outgoing sections: 

###### `app` Parameters

```yaml
app:
  port: integer                      # Specify the port number for the application (integer).
  dirPath: string                    # Specify the directory path for the application (string).
  receivePath: string                # Specify the path where the application will receive the POST requests (string).
  checkAvailabilityTimeout: integer  # Specify the timeout in milliseconds for checking if the servers are alive (integer).

```

###### `incoming` Parameters

```yaml
incoming:
  allowedIPs: []string               # Specify the allowed host IPs in an array of strings. Can be left empty to allow all incoming request IPs.

```

###### `outgoing` Parameters

```yaml
outgoing:
  serverPostURLs: []string           # Specify the server POST URLs in an array of strings

```

One such example ```config.yaml``` file is as follows:

```yaml
app:
  port: 3030
  dirPath: "./voidension"
  receivePath: "/receive"
  checkAvailabilityTimeout: 10000
incoming:
  allowedIPs: []
outgoing:
  serverPostURLs: ["http://localhost:8080/receive"]
```

Then, load the configuration and launch `voidension`:  

```go
package main

import (
	"log"
	"os"

	"github.com/armaanleg3nd/voidension"
)

func main() {
	configData, err := os.ReadFile("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	voidension.LoadConfig(configData) // Load the configuration from file
	voidension.Launch()
}
```