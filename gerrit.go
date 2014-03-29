package main

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	Topic         string
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

type ServerDetails struct {
	Username string
	Keyfile  string
	Location string
}

func (s *ServerDetails) ReviewGerrit(revision string, score string, message string) {
	session, err := ConnectToSsh(s.Username, s.Keyfile, s.Location)
	if err != nil {
		panic("unable to create session: " + err.Error())
	}

	defer session.Close()

	reviewCommand := fmt.Sprintf("gerrit review -m \"%s\" --code-review %s %s", message, score, revision)

	output, err := session.Output(reviewCommand)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))
}

func (s *ServerDetails) ListenToGerrit() chan Event {
	streamChannel := make(chan Event)

	go func() {
		session, err := ConnectToSsh(s.Username, s.Keyfile, s.Location)
		if err != nil {
			panic("unable to create session: " + err.Error())
		}

		defer session.Close()

		log.Printf("Connected to %s - listening for stream events...", s.Location)

		reader, _ := session.StdoutPipe()

		session.Start("gerrit stream-events")
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			var event Event
			_ = json.Unmarshal([]byte(line), &event)

			streamChannel <- event
		}
	}()

	return streamChannel
}
