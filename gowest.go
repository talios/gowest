package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"gopkg.in/alecthomas/kingpin.v2"

	log "github.com/sirupsen/logrus"

	"github.com/Unknwon/goconfig"
)

// projectQueueSize bounds how many pending patchsets we hold per project before dropping.
const projectQueueSize = 16

var (
	verboseMode = kingpin.Flag("verbose", "Run in verbose mode.").Short('v').Bool()
)

func main() {
	kingpin.Parse()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("No config")
		panic("No config")
	}

	var server ServerDetails

	server.Username, _ = cfg.GetValue("gerrit", "username")
	server.Keyfile, _ = cfg.GetValue("gerrit", "keyfile")
	server.Location, _ = cfg.GetValue("gerrit", "server")
	server.startReviewWorker()

	events := server.listenToGerrit(*verboseMode)

	changeEventChannelMap := make(map[string]chan Event)

	for {
		event, ok := <-events
		if !ok {
			log.Fatal("Gerrit event stream closed")
		}

		switch event.Type {
		// case "comment-added":
		// 	log.Printf("Commented added by %s: %s", event.Uploader.Email, event.Comment)
		case "patchset-created":
			changeEventChannel := findOrCreateProjectEventChannel(changeEventChannelMap, cfg, server, event)

			select {
			case changeEventChannel <- event:
				log.WithFields(log.Fields{
					"changeID": event.Change.ID,
					"email":    event.Change.Owner.Email,
					"username": event.Change.Owner.Username,
					"project":  event.Change.Project,
				}).Info("Queued build")
			default:
				log.WithFields(log.Fields{
					"changeID": event.Change.ID,
					"project":  event.Change.Project,
				}).Warn("Build queue full, dropping event")
			}
		}
	}
}

/*
 * Lookup an event channel based on the Event's project, if not existing in the map - create one,
 * insert it, and start the worker goroutine that builds that project's events one at a time.
 */
func findOrCreateProjectEventChannel(projectEventChannelMap map[string]chan Event, config *goconfig.ConfigFile, server ServerDetails, event Event) chan Event {
	projectEventChannel, exists := projectEventChannelMap[event.Change.Project]
	if !exists {
		projectEventChannel = make(chan Event, projectQueueSize)
		projectEventChannelMap[event.Change.Project] = projectEventChannel
		go buildProjectEvents(config, server, projectEventChannel)
	}
	return projectEventChannel
}

/*
 * Serially build every event for a single project - one worker per project means builds of the
 * same project never overlap, while different projects still build concurrently.
 */
func buildProjectEvents(config *goconfig.ConfigFile, server ServerDetails, events chan Event) {
	for event := range events {
		rebuildProject(config, server, event)
	}
}

func loadConfig() (*goconfig.ConfigFile, error) {
	cfg, err := goconfig.LoadConfigFile("gowest.ini")
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func rebuildProject(config *goconfig.ConfigFile, server ServerDetails, event Event) {
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

	project, err := resolveProject(config, projectKey, server, projectPath)
	if err != nil {
		projectPathLogger.WithError(err).WithField("changeID", event.Change.ID).Error("Unable to resolve project, skipping build")
		return
	}
	project.buildMaven(event)

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
