package main

import (
	"bufio"
	"encoding/json"
	"log"
)

type PatchSet struct {
	Number         string
	Revision       string
	Parents        []string
	Ref            string
	Uploader       *User
	CreatedOn      int64
	Author         *User
	IsDraft        bool
	SizeInsertions int64
	SizeDeletions  int64
}

type Change struct {
	Project       string
	Branch        string
	Id            string
	Number        string
	Subject       string
	Owner         *User
	Url           string
	CommitMessage string
	Status        string
}

type User struct {
	Name     string
	Email    string
	Username string
}

type Event struct {
	Type     string
	Change   *Change
	PatchSet *PatchSet
	Author   *User
	Comment  string
}

func ListenToGerrit(username string, keyfile string, server string) chan Event {
	streamChannel := make(chan Event)

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

			log.Print(line)

			var event Event
			_ = json.Unmarshal([]byte(line), &event)

			streamChannel <- event
		}
	}()

	return streamChannel
}
