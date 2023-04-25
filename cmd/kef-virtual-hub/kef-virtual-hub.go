package main

import (
	"hash/fnv"
	"log"
	"os"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
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

	// switchInfo := accessory.Info{
	// 	Name:         "KEF Virtual Hub",
	// 	Manufacturer: "Jens Hilligsøe <jens@hilli.dk>",
	// 	Model:        "1.0.0",
	// }
	// Create a new file store
	fs := hap.NewFsStore("kef-virtual-hub")

	// Create speaker
	mySpeaker, err := kefw2.NewSpeaker(os.Getenv("KEFW2_IP"))

	// Create Speaker Service
	// s := service.NewSpeaker()

	// Create Accessory
	a := accessory.New(accessory.Info{
		Name:         "KEF W2",
		Manufacturer: "KEF",
		Model:        mySpeaker.Model,
	}, byte(34))
	// Add service to Accessory
	// a.AddS(service.S(s))
	
	// hash the speaker id to a uint64
	// so the speaker remains the same
	h := fnv.New64a()
	h.Write([]byte(mySpeaker.Id))
	a.Id = h.Sum64()
	
	hap.NewServer(fs, a)
}