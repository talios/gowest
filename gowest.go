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
	userName, err := cfg.GetValue("gerrit", "username")
	keyFile, err := cfg.GetValue("gerrit", "keyfile")
	server, err := cfg.GetValue("gerrit", "server")

	events := ListenToGerrit(userName, keyFile, server)

	for {
		event := <-events
		switch event.Type {
		case "comment-added":
			//log.Printf("Commented added by %s: %s", event.Author.Email, event.Comment)
			RebuildProject(event)
		case "patchset-created":
			RebuildProject(event)
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

func RebuildProject(event Event) {
	cfg, err := LoadConfig()
	if err != nil {
		panic("No config")
	}

	projectPath := GetProjectDirectory(event.Change.Project)
	projectUrl, err := cfg.GetValue(event.Change.Project, "url")

	log.Printf("initializing empty git repository: %s", projectPath)
	Git(projectPath, "init")
	log.Printf("fetching change ref: %s", event.PatchSet.Ref)
	Git(projectPath, "fetch", projectUrl, event.PatchSet.Ref)
	Git(projectPath, "checkout", "FETCH_HEAD")

	Build(projectPath)
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

func Git(projectPath string, args ...string) {
	// Check Git is installed and on the path
	binary, lookErr := exec.LookPath("git")
	if lookErr != nil {
		panic(lookErr)
	}

	// run git specifying the project directory to use
	gitCmd := exec.Command(binary, args...)
	gitCmd.Dir = projectPath
	gitOut, err := gitCmd.Output()

	if err != nil {
		panic(err)
	}

	fmt.Println(string(gitOut))
}

func Build(projectPath string) {
	binary, lookErr := exec.LookPath("mvn")
	if lookErr != nil {
		panic(lookErr)
	}

	log.Printf("Found %s - building", binary)
	// run mvn
	mvnCmd := exec.Command(binary, "clean", "install")
	mvnCmd.Dir = projectPath
	mvnOut, err := mvnCmd.Output()
	if err != nil {
		panic(err)
	}

	fmt.Println(string(mvnOut))
}
