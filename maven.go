package main

import (
	"log"
	"os"
	"os/exec"
)

func isMavenProject(projectPath string) {
	if _, err := os.Stat(projectPath + "/pom.xml"); err == nil {
    	return true
    } else {
		return false;
	}
}

func buildMaven(projectPath string, server ServerDetails, event Event) {
	binary, lookErr := exec.LookPath("mvn")
	if lookErr != nil {
		panic(lookErr)
	}

	log.Printf("Found %s - building", binary)
	// run mvn
	mvnCmd := exec.Command(binary, "clean", "install")
	mvnCmd.Dir = projectPath
	mvnCmd.Stderr = os.Stderr
	mvnCmd.Stdout = os.Stdout

	if err := mvnCmd.Start(); err != nil {
		log.Fatal(err)
		return
	}

	if err := mvnCmd.Wait(); err != nil {
		server.ReviewGerrit(event.PatchSet.Revision, "-1", "oh noes - you did broke it!")
	} else {
		server.ReviewGerrit(event.PatchSet.Revision, "+1", "oh hai - you much legend!")
	}

}
