package main

import (
	"log"
)

func main() {

	events, err := listenToGerrit("amrk", "/Users/amrk/.ssh/id_rsa", "127.0.0.1:29418")
	if err != nil {
		log.Fatalf("Unable to stream events: %s", err)
	}

	for {
		event := <-events
		log.Print(event)
	}

}
