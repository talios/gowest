package main

import (
	"log"
)

func main() {

	events := ListenToGerrit("amrk", "/Users/amrk/.ssh/id_rsa", "127.0.0.1:29418")

	for {
		event := <-events
		log.Print(event)
	}

}
