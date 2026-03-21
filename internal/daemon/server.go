package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
)

type Evaluator interface {
	Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error)
}

type Server struct {
	socketPath string
	evaluator  Evaluator
}

type Client struct {
	socketPath string
	httpClient *http.Client
}

func NewServer(socketPath string, evaluator Evaluator) *Server {
	return &Server{
		socketPath: socketPath,
		evaluator:  evaluator,
	}
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport},
	}
}

func (s *Server) Run(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	_ = os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/evaluate", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var input model.EvaluationInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		decision, err := s.evaluator.Evaluate(r.Context(), input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(decision)
	})

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return nil
}

func (c *Client) Evaluate(ctx context.Context, input model.EvaluationInput) (model.EvaluationDecision, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return model.EvaluationDecision{}, fmt.Errorf("marshal evaluate request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/evaluate", bytes.NewReader(payload))
	if err != nil {
		return model.EvaluationDecision{}, fmt.Errorf("build evaluate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.EvaluationDecision{}, fmt.Errorf("%w: %v", model.ErrDaemonUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return model.EvaluationDecision{}, fmt.Errorf("daemon evaluate failed with status %d", resp.StatusCode)
	}
	var decision model.EvaluationDecision
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		return model.EvaluationDecision{}, fmt.Errorf("decode daemon response: %w", err)
	}
	return decision, nil
}
