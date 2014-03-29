package main

import (
	"fmt"
	"github.com/Unknwon/goconfig"
	"log"
	"os"
	"os/exec"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		panic("No config")
	}

	var server ServerDetails

	server.Username, _ = cfg.GetValue("gerrit", "username")
	server.Keyfile, _ = cfg.GetValue("gerrit", "keyfile")
	server.Location, _ = cfg.GetValue("gerrit", "server")

	events := server.ListenToGerrit()

	for {
		event := <-events
		switch event.Type {
		case "comment-added":
			log.Printf("Commented added by %s: %s", event.Author.Email, event.Comment)
		case "patchset-created":
			go RebuildProject(cfg, server, event)
		}
	}
}

func LoadConfig() (*goconfig.ConfigFile, error) {
	cfg, err := goconfig.LoadConfigFile("gowest.ini")
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func RebuildProject(config *goconfig.ConfigFile, server ServerDetails, event Event) {

	projectKey := fmt.Sprintf("project.%s", event.Change.Project)

	// skip the project build if there's no section in the config for it.
	_, err := config.GetSection(projectKey)
	if err != nil {
		log.Printf("Skipping undefined project: %s", event.Change.Project)
		return
	}

	projectPath := GetProjectDirectory(projectKey)
	projectUrl, err := config.GetValue(projectKey, "url")
	if err != nil {
		log.Fatalf("No url specified for project: %s", event.Change.Project)
	}

	log.Printf("initializing empty git repository: %s", projectPath)
	Git(projectPath, "init")
	log.Printf("fetching change ref: %s", event.PatchSet.Ref)
	Git(projectPath, "fetch", projectUrl, event.PatchSet.Ref)
	Git(projectPath, "checkout", "FETCH_HEAD")
	Git(projectPath, "fetch", projectUrl, event.Change.Branch)

	mergeErr := Git(projectPath, "merge", "FETCH_HEAD")
	if mergeErr != nil {
		server.ReviewGerrit(event.PatchSet.Revision, "-1", "gosh darn it - we can't do the dang merge!")
	}

   if (isMavenProject(projectPath) {
		buildMaven(projectPath, server, event)
	}
}

func GetWorkspace() string {
	tempDir, _ := os.Getwd()
	workSpace := tempDir + "/workspace"
	err := os.MkdirAll(workSpace, 0777)
	if err != nil {
		if !os.IsExist(err) {
			panic(err)
		}
	}
	return workSpace
}

func GetProjectDirectory(projectName string) string {
	workSpace := GetWorkspace()
	// create workspace dir
	projectPath := workSpace + "/" + projectName
	log.Printf("creating workspace dir: %s", projectPath)

	err := os.RemoveAll(projectPath)
	if err != nil {
		if !os.IsNotExist(err) {
			panic("Unable to remove " + projectPath)
		}
	}
	os.MkdirAll(projectPath, 0777)
	if err != nil {
		panic("Unable to create " + projectPath)
	}
	return projectPath
}

func Git(projectPath string, args ...string) error {
	// Check Git is installed and on the path
	binary, lookErr := exec.LookPath("git")
	if lookErr != nil {
		return lookErr
	}

	// run git specifying the project directory to use
	gitCmd := exec.Command(binary, args...)
	gitCmd.Dir = projectPath
	gitOut, err := gitCmd.Output()

	if err != nil {
		return err
	}

	fmt.Println(string(gitOut))

	return nil
}

