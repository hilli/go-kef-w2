package main

import (
	"fmt"
	"log"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Create a new speaker
	speaker, err := kefw2.NewSpeaker(os.Getenv("KEFW2_IP"))
	if err != nil {
		log.Fatal(err)
	}
	// Print the speaker name
	fmt.Printf("Speaker struct: %+v", speaker)
}
