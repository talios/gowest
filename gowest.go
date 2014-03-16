package main

import (
	"bytes"
	"fmt"
	"github.com/Unknwon/goconfig"
	"log"
	"os"
	"os/exec"
)

func main() {

	tempDir := os.TempDir()
	workSpace := tempDir + "workspace"
	err := os.MkdirAll(workSpace, 0777)
	if err != nil {
		if !os.IsExist(err) {
			panic(err)
		}
	}

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
				projectUrl, err := cfg.GetValue(event.Change.Project, "url")

				// create workspace dir
				projectPath := workSpace + "/" + event.Change.Project
				log.Printf("creating workspace dir: %s", projectPath)
				err = os.MkdirAll(projectPath, 0777)
				if err != nil {
					if !os.IsExist(err) {
						os.RemoveAll(projectPath)
						os.MkdirAll(projectPath, 0777)
					}
				}

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

func Git(projectPath string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Path = projectPath
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatalf("ERR: %s", err.Error())
	}
	fmt.Print(out.String())

}
