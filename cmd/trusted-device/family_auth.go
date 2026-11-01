package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
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

func runInteractiveFamilyHelper(ctx context.Context, panel *control.Server, organizer, helper string, args, env []string) (string, error, func()) {
	deadline, _ := ctx.Deadline()
	if promptDeadline := time.Now().Add(160 * time.Second); deadline.IsZero() || deadline.After(promptDeadline) {
		deadline = promptDeadline
	}
	id, input, finish := panel.BeginFamilyAuthentication(organizer, deadline)
	if id == "" {
		return "", errors.New("family challenge routing unavailable"), finish
	}
	command := exec.CommandContext(ctx, helper, append(args, "-family-auth-stdin")...)
	command.Env = append(os.Environ(), env...)
	writer := &familyProgressWriter{progress: func(stage string) { panel.SetFamilyAuthenticationStage(id, stage) }}
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
		panel.SetFamilyAuthenticationStage(id, "verifying-membership")
	}
	return writer.output.String(), err, finish
}

// Keep enough time for the user to receive and submit a code, then for both
// membership reads. The control HTTP response remains below its write deadline.
const interactiveFamilyTimeout = 205 * time.Second
