package main

import (
	"log"
	"os"
	"os/exec"
)

func isMavenProject(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/pom.xml"); err == nil {
		return true
	} else {
		return false
	}
}

func buildMaven(projectPath string, server ServerDetails, event Event, eventChannel chan Event) {
	binary, lookErr := exec.LookPath("mvn")
	if lookErr != nil {
		panic(lookErr)
	}

	log.Printf("Found %s - building", binary)
	// run mvn
	mvnCmd := exec.Command(binary, "clean", "install")
	mvnCmd.Dir = projectPath

	if err := mvnCmd.Start(); err != nil {
		log.Fatal(err)
		return
	}

	processChannel := make(chan bool)

	go func() {
		if err := mvnCmd.Wait(); err != nil {
			processChannel <- false
		} else {
			processChannel <- true
		}
	}()

	select {
	case processStatus := <-processChannel:
		if processStatus {
			server.ReviewGerrit(event.PatchSet.Revision, "-1", "oh noes - you did broke it!")
		} else {
			server.ReviewGerrit(event.PatchSet.Revision, "+1", "oh hai - you much legend!")
		}
		break
	case gerritEvent := <-eventChannel:
		if isUpdatedPatchset(event, gerritEvent) {
			log.Printf("Cancelling build of patchset %s of change %s on project %s.", event.PatchSet.Number, event.Change.Id, event.Change.Project)
			mvnCmd.Process.Kill()
			server.ReviewGerrit(event.PatchSet.Revision, "0", "such sadness - much building aborted :(!")
		}

	}

}
