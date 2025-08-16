package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"

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
	xml, err := os.ReadFile(fullPomFile)
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

func (project *BuildProject) buildMaven(event Event) {
	ctx := context.Background()

	msg := "Building " + event.Project + " change " + event.Change.ID + "@" + event.PatchSet.Revision
	beeep.Notify("GoWest", msg, "assets/information.png")

	project.Server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "0", "Verifying build...")

	processChannel := make(chan BuildProcess)

	go func() {
		if err := buildWith(ctx, project, event); err != nil {
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
			//			out, _ := mvnCmd.CombinedOutput()
			//			message = message + "\n\n\n" + string(out)
			project.Server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "-1", message)
		}
		break
		//	case gerritEvent := <-eventChannel:
		//		if isUpdatedPatchset(event, gerritEvent) {
		//			log.Printf("Cancelling build of patchset %d of change %s on project %s.", event.PatchSet.Number, event.Change.ID, event.Change.Project)
		//			// server.ReviewGerrit(event.PatchSet.Revision, "0", "0", "-1", "such sadness - much building aborted :(!")
		//		}
	}
}

func buildWith(ctx context.Context, project *BuildProject, event Event) error {

	localM2Repo := fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", event.Change.ID)

	mvnCmd := exec.Command("mvn", localM2Repo, "clean", "verify")
	mvnCmd.Dir = project.ProjectPath
	mvnCmd.Stderr = os.Stderr
	mvnCmd.Stdout = os.Stdout

	if err := mvnCmd.Start(); err != nil {
		return err
	}

	if err := mvnCmd.Wait(); err != nil {
		return err
	} else {
		return nil
	}
}
