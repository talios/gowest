package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
)

// BuildProcess Enum type for build status
type BuildProcess int

const (
	// Success The build was successful
	Success BuildProcess = iota
	// Failure The build failed
	Failure
)

func isGitRepo(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/.git"); err == nil {
		return true
	}
	return false
}

func isMavenProject(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/pom.xml"); err == nil {
		return true
	}
	return false
}

func buildMaven(projectPath string, server ServerDetails, event Event, eventChannel chan Event) {
	binary, lookErr := exec.LookPath("mvn")
	if lookErr != nil {
		log.Warn("Unable to find maven...")
		return
	}

	log.Printf("Found %s - building", binary)
	// run mvn
	mvnCmd := exec.Command(binary, "clean", "install")
	mvnCmd.Dir = projectPath

	if err := mvnCmd.Start(); err != nil {
		server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "0", "Unable to start build: "+fmt.Sprint(err))
		log.Warn(err)
		return
	}

	server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "0", "Verifying build...")

	processChannel := make(chan BuildProcess)

	go func() {
		if err := mvnCmd.Wait(); err != nil {
			processChannel <- Failure
		} else {
			processChannel <- Success
		}
	}()

	select {
	case processStatus := <-processChannel:
		switch processStatus {
		case Success:
			server.ReviewGerrit(event.PatchSet.Revision, "+1", "+1", "+1", "oh hai - you much legend!")
		case Failure:
			message := "oh noes - you did broke it!"
			out, _ := mvnCmd.CombinedOutput()
			message = message + "\n\n\n" + string(out)
			server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "-1", message)
		}
		break
	case gerritEvent := <-eventChannel:
		if isUpdatedPatchset(event, gerritEvent) {
			log.Printf("Cancelling build of patchset %d of change %s on project %s.", event.PatchSet.Number, event.Change.ID, event.Change.Project)
			mvnCmd.Process.Kill()
			// server.ReviewGerrit(event.PatchSet.Revision, "0", "0", "-1", "such sadness - much building aborted :(!")
		}

	}

}
