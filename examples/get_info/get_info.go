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
	source, _ := speaker.Source()
	fmt.Println("Source:", source)
	powerstate, _ := speaker.IsPoweredOn()
	fmt.Println("Powered on:", powerstate)
	// speaker.PowerOff()
	// err = speaker.Unmute()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	speaker.PlayPause()
	err = speaker.SetSource(kefw2.SourceTV)
	if err != nil {
		fmt.Println(err)
	}
}
