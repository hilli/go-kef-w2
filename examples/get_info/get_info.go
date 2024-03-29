package main

import (
	"fmt"
	"log"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/k0kubun/pp"
)

func main() {
	speaker, err := kefw2.NewSpeaker("10.0.0.143")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Name:", speaker.Name)
	fmt.Println("Model:", speaker.Model)
	fmt.Println("Firmware:", speaker.FirmwareVersion)
	fmt.Println("IP Address:", speaker.IPAddress)
	fmt.Println("MAC Address:", speaker.MacAddress)
	networkOpMode, _ := speaker.NetworkOperationMode()
	fmt.Println("Network operation mode:", networkOpMode)
	volume, _ := speaker.GetVolume()
	fmt.Println("Volume:", volume)
	source, _ := speaker.Source()
	fmt.Println("Source:", source)
	fmt.Println("Max Volume:", speaker.MaxVolume)
	muted, _ := speaker.IsMuted()
	fmt.Println("Muted:", muted)
	powerstate, _ := speaker.IsPoweredOn()
	fmt.Println("Powered on:", powerstate)
	pd, _ := speaker.PlayerData()
	pp.Printf("Player data: %+v", pd)
	// Are we currently playing?
	isPlaying, _ := speaker.IsPlaying()
	fmt.Print("Playing:", isPlaying)
	// speaker.PowerOff()
	// err = speaker.Unmute()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	//speaker.PlayPause()
	//err = speaker.SetSource(kefw2.SourceUSB)
	//if err != nil {
	//	fmt.Println(err)
	//}
}
