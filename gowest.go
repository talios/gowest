package main

import (
	"fmt"
	"github.com/Unknwon/goconfig"
	"log"
	"os"
	"os/exec"
)

func main() {

	events := ListenToGerrit("amrk", "/Users/amrk/.ssh/id_rsa", "127.0.0.1:29418")

	for {
		event := <-events
		switch event.Type {
		case "comment-added":
			{
				log.Printf("Commented added by %s: %s", event.Author.Email, event.Comment)

				cfg, err := goconfig.LoadConfigFile("gowest.ini")
				if err != nil {
					panic("No config")
				}

				projectPath := GetProjectDirectory(event.Change.Project)
				projectUrl, err := cfg.GetValue(event.Change.Project, "url")

				log.Printf("initializing empty git repository: %s", projectPath)
				Git(projectPath, "init")
				log.Printf("fetching change ref: %s", event.PatchSet.Ref)
				Git(projectPath, "fetch", projectUrl, event.PatchSet.Ref)
				log.Printf("switching to change ref: %s", event.PatchSet.Ref)
				Git(projectPath, "checkout", "FETCH_HEAD")

			}
		}
	}

}

func GetWorkspace() string {
	tempDir := os.TempDir()
	workSpace := tempDir + "workspace"
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
	err := os.MkdirAll(projectPath, 0777)
	if err != nil {
		if !os.IsExist(err) {
			os.RemoveAll(projectPath)
			os.MkdirAll(projectPath, 0777)
		}
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
	gitArgs := append([]string{"-C", projectPath}, args...)
	gitCmd := exec.Command(binary, gitArgs...)
	gitOut, err := gitCmd.Output()

	if err != nil {
		panic(err)
	}

	fmt.Println(string(gitOut))

}
