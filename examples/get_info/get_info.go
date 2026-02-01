package main

import (
	"context"
	"fmt"
	"log"

	"github.com/hilli/go-kef-w2/kefw2"
)

func main() {
	speaker, err := kefw2.NewSpeaker("10.0.0.149")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("Name:", speaker.Name)
	fmt.Println("Model:", speaker.Model)
	fmt.Println("Firmware:", speaker.FirmwareVersion)
	fmt.Println("IP Address:", speaker.IPAddress)
	fmt.Println("MAC Address:", speaker.MacAddress)
	networkOpMode, _ := speaker.NetworkOperationMode(ctx)
	fmt.Println("Network operation mode:", networkOpMode)
	volume, _ := speaker.GetVolume(ctx)
	fmt.Println("Volume:", volume)
	source, _ := speaker.Source(ctx)
	fmt.Println("Source:", source)
	fmt.Println("Max Volume:", speaker.MaxVolume)
	muted, _ := speaker.IsMuted(ctx)
	fmt.Println("Muted:", muted)
	powerstate, _ := speaker.IsPoweredOn(ctx)
	fmt.Println("Powered on:", powerstate)
	pd, _ := speaker.PlayerData(ctx)
	fmt.Printf("Player data: %+v\n", pd)
	// Are we currently playing?
	isPlaying, _ := speaker.IsPlaying(ctx)
	fmt.Println("Playing:", isPlaying)
}
