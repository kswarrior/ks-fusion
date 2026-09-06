package main

import (
	"os"
	"runtime/pprof"
)

var profileFile *os.File

func startCPUProfile(f *os.File) error {
	profileFile = f
	return pprof.StartCPUProfile(f)
}

func stopCPUProfile() {
	pprof.StopCPUProfile()
	if profileFile != nil {
		_ = profileFile.Close()
		profileFile = nil
	}
}
