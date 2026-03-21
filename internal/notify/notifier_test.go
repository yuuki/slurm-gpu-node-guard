package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

func TestNotifierPostsWebhookAndRunsCommand(t *testing.T) {
	var (
		mu      sync.Mutex
		payload map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	output := filepath.Join(dir, "command-output.json")
	script := filepath.Join(dir, "notify.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > \"$NOTIFY_OUTPUT\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	n := NewManager(Config{
		Receivers: map[string]Receiver{
			"default": {
				Webhook: WebhookConfig{URL: srv.URL},
				Command: CommandConfig{Path: script},
			},
		},
	}, map[string]string{
		"NOTIFY_OUTPUT": output,
	})

	event := model.NotificationEvent{
		ReceiverNames: []string{"default"},
		NodeName:      "node001",
		JobID:         "123",
		Verdict:       model.VerdictBlockDrainRequeue,
		Summary:       "gpu unhealthy",
	}
	if err := n.Notify(context.Background(), event); err != nil {
		t.Fatalf("Notify returned error: %v", err)
	}

	mu.Lock()
	gotNode := payload["node_name"]
	mu.Unlock()
	if gotNode != "node001" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected command output file: %v", err)
	}
}
