package main

import (
	"os"
	"path/filepath"
)

type PathType string

const (
	AWSDIR      PathType = "awsDir"
	CONFIG      PathType = "config"
	CREDENTIALS PathType = "credentials"
	CHROMIUM    PathType = "chromium"
)

var userHomeDir = getUserHomeDir()
var awsDir = filepath.Join(userHomeDir, ".aws")

var paths = map[PathType]string{
	AWSDIR:      awsDir,
	CONFIG:      ifThenElse(os.Getenv("AWS_CONFIG_FILE") != "", os.Getenv("AWS_CONFIG_FILE"), filepath.Join(awsDir, string(CONFIG))),
	CREDENTIALS: ifThenElse(os.Getenv("AWS_SHARED_CREDENTIALS_FILE") != "", os.Getenv("AWS_SHARED_CREDENTIALS_FILE"), filepath.Join(awsDir, string(CREDENTIALS))),
	CHROMIUM:    filepath.Join(awsDir, string(CHROMIUM)),
}

func getUserHomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		// Can't use zerolog here as it may not be initialized yet
		// Use panic for init-time errors
		panic("Failed to get user home directory: " + err.Error())
	}
	return dir
}

func ifThenElse(condition bool, a string, b string) string {
	if condition {
		return a
	}
	return b
}
