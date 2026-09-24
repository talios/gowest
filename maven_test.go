package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {

	server := ServerDetails{Username: "markd", Keyfile: "/Users/amrk/.ssh/id_rsa", Location: "gerrit.smxemail.com:29418"}
	project := BuildProject{Name: "dev/smx3/smx3-monitoring", Version: "1.1", ProjectPath: "/tmp/gowest/workspace/dev/smx3/smx3-monitoring/develop-I1ed4922f4183d71442024e1c645709db36a15d1e-5", Server: server}

	event := Event{
		PatchSet: PatchSet{Number: 29201, Revision: "3"},
	}
	project.buildMaven(event)

}

func TestPomParsing(t *testing.T) {

	var xmlStr = `
	<?xml version="1.0" encoding="UTF-8"?>
	<project
			xmlns="http://maven.apache.org/POM/4.0.0"
			xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
			xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">

		<modelVersion>4.0.0</modelVersion>

		<groupId>com.smxemail</groupId>
		<artifactId>selfrelease</artifactId>
		<version>1.0.1-SNAPSHOT</version>
		<packaging>jar</packaging>
		<name>Azure Java Functions</name>
	</project>`

	pomProject, _ := resolvePomProjectFromString(xmlStr)
	assert.Equal(t, pomProject.ArtifactId, "selfrelease", "Invalid artifact id")

}
