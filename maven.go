package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	// "github.com/gen2brain/beeep"
	"github.com/gen2brain/beeep"
	log "github.com/sirupsen/logrus"
)

type BuildProject struct {
	Name        string
	Version     string
	ProjectPath string
	Server      ServerDetails
}

type MavenProject struct {
	BuildProject
}

type PomProject struct {
	XMLName        xml.Name `xml:"project"`
	Text           string   `xml:",chardata"`
	Xmlns          string   `xml:"xmlns,attr"`
	Xsi            string   `xml:"xsi,attr"`
	SchemaLocation string   `xml:"schemaLocation,attr"`
	ModelVersion   string   `xml:"modelVersion"`
	GroupId        string   `xml:"groupId"`
	ArtifactId     string   `xml:"artifactId"`
	Version        string   `xml:"version"`
	Packaging      string   `xml:"packaging"`
	Name           string   `xml:"name"`
}

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

func resolveProject(server ServerDetails, projectPath string) (*BuildProject, error) {
	if isMavenProject(projectPath) {
		pomProject, err := resolvePomProject(projectPath)
		return &BuildProject{
			Name: pomProject.Name, Version: pomProject.Version, ProjectPath: projectPath, Server: server,
		}, err
	}
	return nil, errors.New("unknown project type")
}

func isMavenProject(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/pom.xml"); err == nil {
		return true
	}
	return false
}

func resolvePomProject(projectPath string) (*PomProject, error) {
	fullPomFile := projectPath + "/pom.xml"
	_, err := os.Stat(fullPomFile)
	if err != nil {
		log.Error("Missing pom.xml")
		return nil, err
	}
	xml, err := ioutil.ReadFile(fullPomFile)
	if err != nil {
		log.Error("Unable to read pom.xml")
		return nil, err
	}
	return resolvePomProjectFromString(string(xml))
}

func resolvePomProjectFromString(pom string) (*PomProject, error) {
	var pomProject PomProject
	data := []byte(pom)
	err := xml.Unmarshal(data, &pomProject)
	if err != nil {
		log.Error("Unable to parse pom.xml")
		return nil, err
	}
	return &pomProject, nil

}

func (project *BuildProject) buildMaven(event Event, eventChannel chan Event) {
	binary, lookErr := exec.LookPath("mvn")
	if lookErr != nil {
		log.Warn("Unable to find maven...")
		return
	}

	msg := "Building " + event.Project + " change " + event.Change.ID + "@" + event.PatchSet.Revision
	beeep.Notify("GoWest", msg, "assets/information.png")

	localM2Repo := fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s-%s", event.Change.ID, event.PatchSet.Revision)

	log.Printf("Found %s - building", binary)

	mvnCmd := exec.Command(binary, localM2Repo, "clean", "install")
	mvnCmd.Dir = project.ProjectPath

	if err := mvnCmd.Start(); err != nil {
		project.Server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "0", "Unable to start build: "+fmt.Sprint(err))
		log.Warn(err)
		return
	}

	project.Server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "0", "Verifying build...")

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
			project.Server.ReviewGerrit(event.PatchSet.Revision, "+1", "+1", "+1", "oh hai - you much legend!")
		case Failure:
			message := "oh noes - you did broke it!"
			out, _ := mvnCmd.CombinedOutput()
			message = message + "\n\n\n" + string(out)
			project.Server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "-1", message)
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
