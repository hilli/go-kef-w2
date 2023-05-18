package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	haplog "github.com/brutella/hap/log"
	"github.com/brutella/hap/service"
	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/joho/godotenv"
)

// type Speaker struct {
// 	ip string
// 	accessory *accessory.Accessory
// 	kef *kefw2.Speaker
// }

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	if os.Getenv("DEBUG") == "true" {
		log.SetLevel(log.DebugLevel)
		log.SetReportCaller(true)
		// dnslog.Debug.Enable() // Very noisy
		haplog.Debug.Enable()
	}

	bridgeInfo := accessory.Info{
		Name:         "KEF Virtual Hub",
		Manufacturer: "Jens Hilligsøe <jens@hilli.dk>",
		Model:        "1.0.0",
	}
	myBridge := accessory.NewBridge(bridgeInfo)

	// Create a new file store
	fs := hap.NewFsStore("kef-virtual-hub")

	// Create speaker
	mySpeaker, err := kefw2.NewSpeaker(os.Getenv("KEFW2_IP"))
	if err != nil {
		panic(err)
	}
	log.Debug(mySpeaker)

	// Create Accessory
	speakerInfo := accessory.Info{
		Name:         "Virtual " + mySpeaker.Name,
		Manufacturer: "KEF",
		Model:        mySpeaker.Model,
		Firmware:     mySpeaker.FirmwareVersion,
		SerialNumber: mySpeaker.MacAddress,
	}
	a := accessory.New(speakerInfo, byte(34))

	// Create Speaker Service
	s := service.NewSpeaker()

	// Add service to Accessory
	a.AddS(s.S)
	vol := characteristic.NewVolume()
	vol.Description = "Volume"
	s.AddC(vol.C)
	mute := characteristic.NewMute()
	mute.Description = "Mute"
	s.AddC(mute.C)
	fmt.Printf("Service: %+v\n", s.Cs[1])

	// updateCurrentState := func() {
	// 	state, err := mySpeaker.Source()
	// 	if err != nil {
	// 		log.Error(err)
	// 	}
	// 	// switch st
	// 	log.Debug(state)
	// }

	s.Mute.OnValueRemoteUpdate(func(state bool) {
		if state {
			err := mySpeaker.Mute()
			if err != nil {
				log.Error(err)
			}
		} else {
			err := mySpeaker.Unmute()
			if err != nil {
				log.Error(err)
			}
		}
	})

	// s.Volume.OnValueRemoteUpdate(func(state int) {
	// 	err := mySpeaker.SetVolume(state)
	// 	if err != nil {
	// 		log.Error(err)
	// 	}
	// })
	// cap := characteristic.NewVolumeSelector()

	// hash the speaker id to a uint64 so the speaker remains the same across cold restarts
	h := fnv.New64a()
	h.Write([]byte(mySpeaker.Id))
	a.Id = h.Sum64()

	log.Debug(fmt.Sprintf("%+v", a))

	server, err := hap.NewServer(fs, myBridge.A, a)
	if err != nil {
		panic(err)
	}
	server.Pin = os.Getenv("HOMEKIT_PIN")

	// Setup a listener for interrupts and SIGTERM signals
	// to stop the server.
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		// Stop delivering signals.
		signal.Stop(c)
		// Cancel the context to stop the server.
		cancel()
	}()

	server.ListenAndServe(ctx)
}
