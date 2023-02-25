package main

import (
	"dagger.io/dagger"
	"dagger.io/dagger/core"
	"universe.dagger.io/go"
)

dagger.#Plan & {
	client: filesystem: "~/golang/bin": write: contents: actions.build.goBuild.output

	actions: {
		build: {
			code: core.#Source & {
				path: "./"
			}
			test: go.#Test & {
				source:  code.output
				packages: ["./..."]
			}
			goBuild: go.#Build & {
				source: code.output
				os:     "darwin"
				arch:   "amd64"
			}
		}
	}
}
