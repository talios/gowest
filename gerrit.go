package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
)

type User struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type Event struct {
	Uploader       User      `json:"uploader"`
	PatchSet       PatchSet  `json:"patchSet"`
	Change         Change    `json:"change"`
	Project        string    `json:"project"`
	RefName        string    `json:"refName"`
	ChangeKey      ChangeKey `json:"changeKey"`
	Type           string    `json:"type"`
	EventCreatedOn int       `json:"eventCreatedOn"`
}

type PatchSet struct {
	Number         int      `json:"number"`
	Revision       string   `json:"revision"`
	Parents        []string `json:"parents"`
	Ref            string   `json:"ref"`
	Uploader       User     `json:"uploader"`
	CreatedOn      int      `json:"createdOn"`
	Author         User     `json:"author"`
	Kind           string   `json:"kind"`
	SizeInsertions int      `json:"sizeInsertions"`
	SizeDeletions  int      `json:"sizeDeletions"`
}

type Change struct {
	Project       string   `json:"project"`
	Branch        string   `json:"branch"`
	ID            string   `json:"id"`
	Number        int      `json:"number"`
	Subject       string   `json:"subject"`
	Owner         User     `json:"owner"`
	URL           string   `json:"url"`
	CommitMessage string   `json:"commitMessage"`
	Hashtags      []string `json:"hashtags"`
	CreatedOn     int      `json:"createdOn"`
	Status        string   `json:"status"`
	Topic         string   `json:"topic"`
}
type ChangeKey struct {
	ID string `json:"id"`
}

type ServerDetails struct {
	Username string
	Keyfile  string
	Location string
}

// ReviewGerrit updates a given gerrut revision with a score and message
func (s *ServerDetails) ReviewGerrit(revision string, reviewScore string, verifiedScore string, mergeScore string, message string) {
	session, err := connectToSSH(s.Username, s.Keyfile, s.Location)
	if err != nil {
		panic("unable to create session: " + err.Error())
	}

	defer session.Close()

	reviewCommand := fmt.Sprintf("gerrit review -t gowest -m \"%s\"  --verified %s --merge %s --code-review %s %s",
		message, verifiedScore, mergeScore, reviewScore, revision)

	output, err := session.Output(reviewCommand)
	if err != nil {
		panic(err)
	}
	log.Printf("%s: %s", string(output), message)
}

func (s *ServerDetails) ListenToGerrit() chan Event {
	streamChannel := make(chan Event)

	go func() {
		session, err := connectToSSH(s.Username, s.Keyfile, s.Location)
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

			log.Println("---")
			log.Println(line)
			log.Println("---")

			var event Event
			err = json.Unmarshal([]byte(line), &event)

			if err == nil {
				streamChannel <- event
			}
		}
	}()

	return streamChannel
}

func isUpdatedPatchset(left Event, right Event) bool {
	leftPatchSet := left.PatchSet.Number
	rightPatchSet := right.PatchSet.Number
	return left.Change.ID == right.Change.ID && leftPatchSet < rightPatchSet
}
