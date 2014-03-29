package main

import (
	"errors"
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

	projectPath, err := makeProjectDirectory(config, event.Change)
	if err != nil {
		log.Fatal(err)
	}

	projectUrl, err := config.GetValue(projectKey, "url")
	if err != nil {
		log.Fatalf("No url specified for project: %s", event.Change.Project)
	}

	log.Printf("initializing empty git repository: %s", projectPath)
	git(projectPath, "init")
	log.Printf("fetching change ref: %s", event.PatchSet.Ref)
	git(projectPath, "fetch", projectUrl, event.PatchSet.Ref)
	git(projectPath, "checkout", "FETCH_HEAD")
	git(projectPath, "fetch", projectUrl, event.Change.Branch)

	mergeErr := git(projectPath, "merge", "FETCH_HEAD")
	if mergeErr != nil {
		server.ReviewGerrit(event.PatchSet.Revision, "-1", "gosh darn it - we can't do the dang merge!")
	}

	if isMavenProject(projectPath) == true {
		buildMaven(projectPath, server, event)
	}
}

func getWorkspace(config *goconfig.ConfigFile) (string, error) {
	tempDir, err := config.GetValue("gowest", "workspace")
	if err != nil {
		return "", errors.New("No workspace field defined under [gowest] in gowest.ini!")
	}
	workSpace := tempDir + "/workspace"
	err = os.MkdirAll(workSpace, 0777)
	if err != nil {
		if !os.IsExist(err) {
			return "", err
		}
	}
	return workSpace, nil
}

func getProjectDirectory(config *goconfig.ConfigFile, change *Change) (string, error) {
	workspace, err := getWorkspace(config)
	if err != nil {
		return "", err
	}
	projectPath := workspace + "/" + getProjectSubDirectory(change)
	return projectPath, nil
}

func makeProjectDirectory(config *goconfig.ConfigFile, change *Change) (string, error) {
	projectPath, err := getProjectDirectory(config, change)
	if err != nil {
		return "", err
	}

	log.Printf("creating workspace dir: %s", projectPath)

	err = os.RemoveAll(projectPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("Unable to remove %s: %s", projectPath, err)
		}
	}
	os.MkdirAll(projectPath, 0777)
	if err != nil {
		return "", fmt.Errorf("Unable to create %s: %s", projectPath, err)
	}
	return projectPath, nil
}

func getProjectSubDirectory(change *Change) string {
	projectPath := change.Project
	if change.Branch != "" {
		projectPath = projectPath + "/" + change.Branch
	} else {
		projectPath = projectPath + "/" + "HEAD"
	}
	if change.Topic != "" {
		projectPath = projectPath + "-" + change.Topic
	}
	return projectPath
}

func git(projectPath string, args ...string) error {
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
