package main

import (
	"fmt"
	"log"

	"github.com/hilli/go-kef-w2/kefw2"
)

func main() {
	speaker, err := kefw2.NewSpeaker("10.0.0.93")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Name:", speaker.Name)
	fmt.Println("Model:", speaker.Model)
	fmt.Println("IP Address:", speaker.IPAddress)
	fmt.Println("MAC Address:", speaker.MacAddress)
	volume, _ := speaker.GetVolume()
	fmt.Println("Volume:", volume)
	source, _ := speaker.GetSource()
	fmt.Println("Source:", source)
	powerstate, _ := speaker.GetPowerState()
	fmt.Println("Powered on:", powerstate)
}
