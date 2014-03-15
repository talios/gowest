package main

import (
	"bufio"
	"code.google.com/p/go.crypto/ssh"
	"log"
)

func listenToGerrit(username string, keyfile string, server string) chan string {
	streamChannel := make(chan string)

	go func() {
		k := new(keychain)
		// Add path to id_rsa file
		k.loadPEM(keyfile)

		config := &ssh.ClientConfig{
			User: username,
			Auth: []ssh.ClientAuth{
				ssh.ClientAuthKeyring(k),
			},
		}
		client, err := ssh.Dial("tcp", server, config)
		if err != nil {
			panic("Failed to dial: " + err.Error())

		}
		defer client.Close()

		session, err := client.NewSession()
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
