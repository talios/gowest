package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Unknwon/goconfig"
	"github.com/gen2brain/beeep"
	log "github.com/sirupsen/logrus"
)

type BuildProject struct {
	Name         string
	Version      string
	ProjectPath  string
	RepoType     string
	BuildCommand string
	Server       ServerDetails
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

// resolveProject reads the project's build settings from its config section, e.g. [project.foo].
func resolveProject(config *goconfig.ConfigFile, projectKey string, server ServerDetails, projectPath string) (*BuildProject, error) {
	if isMavenProject(projectPath) {

		pomProject, err := resolvePomProject(projectPath)
		if err != nil {
			return nil, err
		}

		defaultType := "local"
		repoType, err := config.GetValue(projectKey, "repo")

		if err != nil {
			repoType = defaultType
		}

		defaultBuildCommand := "mvnd clean verify"
		buildCommand, err := config.GetValue(projectKey, "build-command")

		if err != nil {
			buildCommand = defaultBuildCommand
		}

		return &BuildProject{
			Name: pomProject.Name, Version: pomProject.Version, ProjectPath: projectPath,
			RepoType: repoType, BuildCommand: buildCommand, Server: server,
		}, nil
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

	commandParts := strings.Fields(project.BuildCommand)
	if len(commandParts) == 0 {
		return errors.New("empty build command")
	}

	// The "local" repo type passes no flag, leaving maven to use its own default (or whatever
	// localRepository settings.xml specifies). The command runs without a shell, so a "~" path
	// here would never be expanded.
	var args []string
	if project.RepoType == "project" {
		args = append(args, fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", project.Name))
	}
	if project.RepoType == "change" {
		args = append(args, fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", event.Change.ID))
	}
	args = append(args, commandParts[1:]...)
	mvnCmd := exec.Command(commandParts[0], args...)
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
