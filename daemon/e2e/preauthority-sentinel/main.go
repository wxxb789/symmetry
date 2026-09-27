package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type readyRecord struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

func main() {
	readyPath := flag.String("ready-file", "", "file written after the process is resumed")
	releasePath := flag.String("release-file", "", "optional file that requests a graceful exit")
	flag.Parse()
	if *readyPath == "" {
		fmt.Fprintln(os.Stderr, "-ready-file is required")
		os.Exit(2)
	}

	record := readyRecord{PID: os.Getpid(), StartedAt: time.Now().UTC()}
	contents, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal ready record: %v\n", err)
		os.Exit(1)
	}
	if err := writeReadyFile(*readyPath, contents); err != nil {
		fmt.Fprintf(os.Stderr, "write ready record: %v\n", err)
		os.Exit(1)
	}

	for {
		if *releasePath != "" {
			if _, err := os.Stat(*releasePath); err == nil {
				return
			} else if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "inspect release file: %v\n", err)
				os.Exit(1)
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func writeReadyFile(path string, contents []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".preauthority-ready-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
