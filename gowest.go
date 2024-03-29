package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/Unknwon/goconfig"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("No config")
		panic("No config")
	}

	var server ServerDetails

	server.Username, _ = cfg.GetValue("gerrit", "username")
	server.Keyfile, _ = cfg.GetValue("gerrit", "keyfile")
	server.Location, _ = cfg.GetValue("gerrit", "server")

	events := server.listenToGerrit()

	changeEventChannelMap := make(map[string]chan Event)

	for {
		event := <-events
		changeEventChannel := findOrCreateProjectEventChannel(changeEventChannelMap, event)

		switch event.Type {
		// case "comment-added":
		// 	log.Printf("Commented added by %s: %s", event.Uploader.Email, event.Comment)
		case "patchset-created":
			select {
			case changeEventChannel <- event:
				log.WithFields(log.Fields{
					"changeID": event.Change.ID,
				}).Info("Triggering build")
				// time.Sleep(5 * time.Second)
				// case <-time.After(5 * time.Second):
				// 	log.Info("No previous builds... building!")
			}

			go rebuildProject(cfg, server, event, changeEventChannel)
		}
	}
}

/*
 * Lookup an event channel based on the Event's project, if not existing in the map - create one and insert
 */
func findOrCreateProjectEventChannel(projectEventChannelMap map[string]chan Event, event Event) chan Event {
	projectEventChannel, exists := projectEventChannelMap[event.Change.Project]
	if !exists {
		projectEventChannel = make(chan Event)
		projectEventChannelMap[event.Change.Project] = projectEventChannel
	}
	return projectEventChannel
}

func loadConfig() (*goconfig.ConfigFile, error) {
	cfg, err := goconfig.LoadConfigFile("gowest.ini")
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func rebuildProject(config *goconfig.ConfigFile, server ServerDetails, event Event, eventChannel chan Event) {

	projectKey := fmt.Sprintf("project.%s", event.Change.Project)

	projectLogger := log.WithFields(log.Fields{
		"project": projectKey,
	})

	// skip the project build if there's no section in the config for it.
	_, err := config.GetSection(projectKey)
	if err != nil {
		projectLogger.Warn("Skipping undefined project")
		return
	}

	projectPath, err := makeProjectDirectory(config, &event)
	if err != nil {
		projectLogger.Warn(err)
		return
	}

	projectPathLogger := projectLogger.WithFields(log.Fields{
		"path": projectPath,
	})

	projectURL, err := config.GetValue(projectKey, "url")
	if err != nil {
		projectLogger.Warn("No url specified for project")
	}

	if !isGitRepo(projectPath) {
		projectPathLogger.Print("initializing empty git repository")
		git(projectPath, "init")
	}

	projectPathLogger.Printf("fetching change ref: %s", event.PatchSet.Ref)
	git(projectPath, "fetch", projectURL, event.PatchSet.Ref)
	git(projectPath, "checkout", "FETCH_HEAD")
	git(projectPath, "fetch", projectURL, event.Change.Branch)

	mergeErr := git(projectPath, "merge", "FETCH_HEAD")
	if mergeErr != nil {
		server.ReviewGerrit(event.PatchSet.Revision, "0", "-1", "-1", "gosh darn it - we can't do the dang merge!")
		git(projectPath, "reset", "--hard", "HEAD")
	}

	if isMavenProject(projectPath) == true {
		buildMaven(projectPath, server, event, eventChannel)
	}
}

func getWorkspace(config *goconfig.ConfigFile) (string, error) {
	tempDir, err := config.GetValue("gowest", "workspace")
	if err != nil {
		return "", errors.New("no workspace field defined under [gowest] in gowest.ini")
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

func getProjectDirectory(config *goconfig.ConfigFile, event *Event) (string, error) {
	workspace, err := getWorkspace(config)
	if err != nil {
		return "", err
	}
	projectPath := workspace + "/" + getProjectSubDirectory(&event.Change, &event.PatchSet)
	return projectPath, nil
}

func makeProjectDirectory(config *goconfig.ConfigFile, event *Event) (string, error) {
	projectPath, err := getProjectDirectory(config, event)
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

func getProjectSubDirectory(change *Change, patchSet *PatchSet) string {
	projectPath := change.Project
	if change.Branch != "" {
		projectPath = projectPath + "/" + change.Branch
	} else {
		projectPath = projectPath + "/" + "HEAD"
	}
	if change.Topic != "" {
		projectPath = projectPath + "-" + change.Topic
	}
	projectPath = projectPath + "-" + change.ID
	projectPath = projectPath + "-" + strconv.Itoa(patchSet.Number)
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
