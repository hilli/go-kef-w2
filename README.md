# go-kef-w2

Library for controlling KEFs W2 platform based speakers over the network.

## Usage

```go
package main

import (
  "fmt"
  "log"

  "github.com/hilli/go-kef-w2/kefw2"
)

func main() {
  speaker, err := kef.NewSpeaker("10.0.0.93")
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(speaker.Name)
  fmt.Println(speaker.Model)
  fmt.Println(speaker.MacAddress)
  fmt.Println(speaker.IPAddress)
  fmt.Println(speaker.Version)
  fmt.Println(speaker.SerialNumber)
  fmt.Println(speaker.MacAddress)
}
```

## License

MIT License
