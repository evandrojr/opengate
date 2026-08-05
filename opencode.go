package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// OpenCodeEvent is a single newline-delimited JSON event emitted by
// `opencode run --format json`.
type OpenCodeEvent struct {
	Type string `json:"type"`
	Part struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Reason string `json:"reason"`
		Tokens struct {
			Total     int `json:"total"`
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
		} `json:"tokens"`
	} `json:"part"`
	Error string `json:"error"`
}

func (e OpenCodeEvent) IsText() bool {
	return e.Type == "text" && e.Part.Type == "text"
}

func (e OpenCodeEvent) IsFinish() bool {
	return e.Type == "step_finish"
}

// RunOptions configures how the opencode CLI is invoked.
type RunOptions struct {
	Model     string // provider/model passed to `-m`
	Prompt    string
	Directory string // working directory for the opencode session
	Auto      bool   // auto-approve tool permissions
	Continue  bool   // continue the last session (`-c`)
	Session   string // continue a specific session (`-s`)
}

// runOpenCode executes `opencode run --format json` and delivers every
// parsed event to onEvent (called synchronously). It returns an error if
// the process fails to start or exits non-zero.
func runOpenCode(opts RunOptions, onEvent func(OpenCodeEvent) error) error {
	args := []string{"run", "--format", "json"}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.Auto {
		args = append(args, "--auto")
	}
	if opts.Continue {
		args = append(args, "-c")
	}
	if opts.Session != "" {
		args = append(args, "-s", opts.Session)
	}
	if opts.Directory != "" {
		args = append(args, "--dir", opts.Directory)
	}
	args = append(args, opts.Prompt)

	cmd := exec.Command(OpenCodeBinary, args...)
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+OpenCodePath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opencode stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("opencode stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opencode start failed: %w", err)
	}

	// Drain stderr concurrently so the process never blocks on a full pipe.
	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			stderrBuf.WriteString(sc.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev OpenCodeEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if onEvent != nil {
			if err := onEvent(ev); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				wg.Wait()
				return err
			}
		}
	}
	scanErr := sc.Err()

	runErr := cmd.Wait()
	wg.Wait()

	if runErr != nil {
		return fmt.Errorf("opencode execution failed: %w, stderr: %s",
			runErr, strings.TrimSpace(stderrBuf.String()))
	}
	if scanErr != nil {
		return fmt.Errorf("opencode stdout scan failed: %w, stderr: %s",
			scanErr, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}
