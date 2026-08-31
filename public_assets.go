package main

import (
	"os"
	"path/filepath"
)

const (
	bundledPublicDir = "./public"
	runtimePublicDir = "/app/runtime/public"
)

func activePublicDir() string {
	return selectPublicDir(runtimePublicDir, bundledPublicDir)
}

func selectPublicDir(updatedDir, bundledDir string) string {
	info, err := os.Stat(filepath.Join(updatedDir, "index.html"))
	if err == nil && !info.IsDir() {
		return updatedDir
	}
	return bundledDir
}

func publicAssetPath(name string) string {
	return filepath.Join(activePublicDir(), name)
}
