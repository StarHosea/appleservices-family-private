package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/appleservices/internal/control"
)

const familyAuthPrefix = "FAMILY_AUTH_JSON="

type familyProgressWriter struct {
	mu       sync.Mutex
	output   bytes.Buffer
	pending  string
	progress func(string)
}

func (w *familyProgressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.output.Len()+len(p) > 256<<10 {
		return 0, errors.New("family helper output exceeded limit")
	}
	w.output.Write(p)
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		if !strings.HasPrefix(line, familyAuthPrefix) {
			continue
		}
		var state struct {
			Stage string `json:"stage"`
		}
		decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, familyAuthPrefix)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&state) == nil && decoder.Decode(&struct{}{}) == io.EOF && (state.Stage == "repair-consent" || state.Stage == "sms-code") {
			w.progress(state.Stage)
		}
	}
	return len(p), nil
}

func runInteractiveFamilyHelper(ctx context.Context, panel *control.Server, organizer, targetDir, helper string, args, env []string) (string, error, func()) {
	deadline, _ := ctx.Deadline()
	if promptDeadline := time.Now().Add(160 * time.Second); deadline.IsZero() || deadline.After(promptDeadline) {
		deadline = promptDeadline
	}
	id, input, finish := panel.BeginFamilyAuthentication(organizer, deadline)
	if id == "" {
		return "", errors.New("family challenge routing unavailable"), finish
	}
	diagnostics, diagnosticErr := os.MkdirTemp(targetDir, ".family-auth-diagnostics-")
	if diagnosticErr != nil {
		return "", errors.New("cannot create private family authentication diagnostics"), finish
	}
	attempt := filepath.Base(diagnostics)
	fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s started; organizer-bound=true; private-diagnostics=true\n", attempt)
	command := exec.CommandContext(ctx, helper, append(args, "-family-auth-stdin")...)
	command.Env = append(os.Environ(), env...)
	command.Env = append(command.Env, "APPLESERVICES_2FA_DIAGNOSTIC_DIR="+diagnostics)
	writer := &familyProgressWriter{progress: func(stage string) {
		fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s stage=%s\n", attempt, stage)
		panel.SetFamilyAuthenticationStage(id, stage)
	}}
	command.Stdout, command.Stderr = writer, writer
	pipe, err := command.StdinPipe()
	if err != nil {
		return "", err, finish
	}
	defer pipe.Close()
	err = command.Start()
	command.Env = nil
	for i := range env {
		env[i] = ""
	}
	if err != nil {
		return "", err, finish
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		encoder := json.NewEncoder(pipe)
		for {
			select {
			case <-finished:
				return
			case <-ctx.Done():
				return
			case value := <-input:
				err := encoder.Encode(value)
				fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s input=%s forwarded=%t\n", attempt, value.Action, err == nil)
				value.Code = ""
				if err != nil {
					return
				}
			}
		}
	}()
	err = command.Wait()
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err == nil {
		fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s helper-completed; verifying-membership=true\n", attempt)
		os.RemoveAll(diagnostics)
		panel.SetFamilyAuthenticationStage(id, "verifying-membership")
	} else {
		fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s failed; private-diagnostics-retained=true\n", attempt)
		result := map[string]any{"error": err.Error(), "output": writer.output.String()}
		if ctx.Err() != nil {
			result["context_error"] = ctx.Err().Error()
		}
		if exit, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exit.ExitCode()
		}
		data, encodeErr := json.Marshal(result)
		if encodeErr == nil {
			// Preserve failures that bypass the helper's ordinary error return,
			// including cancellation, process termination and native crashes.
			if writeErr := os.WriteFile(filepath.Join(diagnostics, "helper-result.json"), data, 0600); writeErr != nil {
				fmt.Fprintf(os.Stderr, "FamilyKit attempt=%s helper-result-retained=false\n", attempt)
			}
		}
	}
	return writer.output.String(), err, finish
}

// Keep enough time for the user to receive and submit a code, then for both
// membership reads. The control HTTP response remains below its write deadline.
const interactiveFamilyTimeout = 205 * time.Second
