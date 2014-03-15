package main

import (
	"bufio"
	"log"
)

func ListenToGerrit(username string, keyfile string, server string) chan string {
	streamChannel := make(chan string)

	go func() {
		session, err := ConnectToSsh(username, keyfile, server)
		if err != nil {
			panic("unable to create session: " + err.Error())
		}

		defer session.Close()

		log.Printf("Connected to %s - listening for stream events...", server)

		reader, _ := session.StdoutPipe()

		session.Start("gerrit stream-events")
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			streamChannel <- line
		}
	}()

	return streamChannel
}
