package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gen2brain/beeep"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type GerritEvent struct {
	Type           string `json:"type"`
	EventCreatedOn int    `json:"eventCreatedOn"`
}

type Update struct {
	Submitter User      `json:"submitter"`
	RefUpdate RefUpdate `json:"refUpdate"`
	GerritEvent
}

type RefUpdate struct {
	OldRev  string `json:"oldRev"`
	NewRev  string `json:"newRev"`
	RefName string `json:"refName"`
	Project string `json:"project"`
}

type User struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type Event struct {
	Uploader  User      `json:"uploader"`
	PatchSet  PatchSet  `json:"patchSet"`
	Change    Change    `json:"change"`
	Project   string    `json:"project"`
	RefName   string    `json:"refName"`
	ChangeKey ChangeKey `json:"changeKey"`
	GerritEvent
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

	// reviews queues reviews for the review worker; nil until startReviewWorker is called.
	reviews chan reviewRequest
}

type reviewRequest struct {
	revision string
	message  string
	command  string
}

const (
	reviewAttempts     = 5
	initialReviewDelay = 1 * time.Second
	reviewQueueSize    = 64
)

/*
 * Start the goroutine that posts queued reviews. A single worker posts reviews in the order they
 * were queued, so a "Verifying build..." review can never land after that build's result.
 * Must be called before the ServerDetails is copied into other goroutines.
 */
func (s *ServerDetails) startReviewWorker() {
	s.reviews = make(chan reviewRequest, reviewQueueSize)
	go func() {
		for request := range s.reviews {
			s.sendReview(request)
		}
	}()
}

// ReviewGerrit queues a score and message for a given gerrit revision. It only blocks if the
// review queue is full; without a review worker it posts the review synchronously.
func (s *ServerDetails) ReviewGerrit(revision string, reviewScore string, verifiedScore string, mergeScore string, message string) {
	request := reviewRequest{
		revision: revision,
		message:  message,
		command: fmt.Sprintf("gerrit review -t gowest -m \"%s\"  --verified %s --merge %s --code-review %s %s",
			message, verifiedScore, mergeScore, reviewScore, revision),
	}

	if s.reviews == nil {
		s.sendReview(request)
		return
	}
	s.reviews <- request
}

// sendReview posts a review, retrying with exponential backoff if gerrit cannot be reached.
func (s *ServerDetails) sendReview(request reviewRequest) {
	reviewLogger := log.WithFields(log.Fields{
		"revision": request.revision,
		"server":   s.Location,
	})

	delay := initialReviewDelay
	for attempt := 1; ; attempt++ {
		output, err := s.postReview(request.command)
		if err == nil {
			log.Printf("%s: %s", string(output), request.message)
			return
		}

		// The command ran and gerrit rejected it - retrying the same review won't help.
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			reviewLogger.WithError(err).WithField("output", string(output)).Error("gerrit review command failed")
			return
		}

		if attempt >= reviewAttempts {
			reviewLogger.WithError(err).WithField("attempts", attempt).Error("Giving up posting review")
			return
		}

		reviewLogger.WithError(err).WithFields(log.Fields{
			"attempt": attempt,
			"retryIn": delay,
		}).Warn("Unable to post review, retrying")
		time.Sleep(delay)
		delay *= 2
	}
}

// postReview makes a single attempt at running the gerrit review command.
func (s *ServerDetails) postReview(reviewCommand string) ([]byte, error) {
	client, session, err := connectToSSH(s.Username, s.Keyfile, s.Location)
	if err != nil {
		return nil, fmt.Errorf("unable to create session: %w", err)
	}

	defer client.Close()
	defer session.Close()

	return session.CombinedOutput(reviewCommand)
}

const (
	initialReconnectDelay = 1 * time.Second
	maxReconnectDelay     = 2 * time.Minute
	// A stream that stays up at least this long counts as healthy, so the backoff resets.
	stableStreamDuration = 1 * time.Minute
)

/*
 * Listen to gerrit stream-events, reconnecting with exponential backoff whenever the stream
 * drops or cannot be established. The returned channel is never closed.
 */
func (s *ServerDetails) listenToGerrit(verboseMode bool) chan Event {
	streamChannel := make(chan Event)

	go func() {
		delay := initialReconnectDelay
		for {
			started := time.Now()
			err := s.streamEvents(verboseMode, streamChannel)

			if time.Since(started) >= stableStreamDuration {
				delay = initialReconnectDelay
			}

			log.WithError(err).WithField("retryIn", delay).Warn("Gerrit event stream disconnected")
			time.Sleep(delay)

			delay *= 2
			if delay > maxReconnectDelay {
				delay = maxReconnectDelay
			}
		}
	}()

	return streamChannel
}

/*
 * Run a single gerrit stream-events session, forwarding parsed events until the stream ends.
 * Always returns a non-nil error describing why the stream stopped.
 */
func (s *ServerDetails) streamEvents(verboseMode bool, streamChannel chan<- Event) error {
	client, session, err := connectToSSH(s.Username, s.Keyfile, s.Location)
	if err != nil {
		return fmt.Errorf("unable to create session: %w", err)
	}

	defer client.Close()
	defer session.Close()

	reader, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("unable to open stdout for gerrit stream-events: %w", err)
	}
	session.Stderr = os.Stderr

	if err := session.Start("gerrit stream-events"); err != nil {
		return fmt.Errorf("unable to start gerrit stream-events: %w", err)
	}

	log.Printf("Connected to %s - listening for stream events...", s.Location)
	beeep.Notify("GoWest", "Connected to "+s.Location, "assets/information.png")

	scanner := bufio.NewScanner(reader)
	// Gerrit events can include large commit messages, so allow lines well beyond the 64KB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		if verboseMode {
			log.Println(line)
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			streamChannel <- event
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading gerrit stream-events: %w", err)
	}
	if err := session.Wait(); err != nil {
		return fmt.Errorf("gerrit stream-events exited: %w", err)
	}
	return errors.New("gerrit stream-events ended")
}

func isUpdatedPatchset(left Event, right Event) bool {
	leftPatchSet := left.PatchSet.Number
	rightPatchSet := right.PatchSet.Number
	return left.Change.ID == right.Change.ID && leftPatchSet < rightPatchSet
}
