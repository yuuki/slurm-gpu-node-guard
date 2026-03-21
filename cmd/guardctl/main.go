package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/yuuki/slurm-gpu-node-guard/internal/app"
	"github.com/yuuki/slurm-gpu-node-guard/internal/config"
	"github.com/yuuki/slurm-gpu-node-guard/internal/daemon"
	"github.com/yuuki/slurm-gpu-node-guard/internal/engine"
	"github.com/yuuki/slurm-gpu-node-guard/internal/model"
	"github.com/yuuki/slurm-gpu-node-guard/internal/notify"
	"github.com/yuuki/slurm-gpu-node-guard/internal/slurm"
	"github.com/yuuki/slurm-gpu-node-guard/internal/telemetry"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}

	ctx := context.Background()
	shutdown, err := telemetry.Setup(ctx, "slurm-gpu-node-guard/guardctl")
	if err == nil {
		defer func() {
			timeoutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = shutdown(timeoutCtx)
		}()
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	switch args[0] {
	case "prolog":
		return runLifecycle(logger, model.PhaseProlog, args[1:])
	case "epilog":
		return runLifecycle(logger, model.PhaseEpilog, args[1:])
	case "check":
		if len(args) > 1 && args[1] == "run" {
			return runChecks(logger, args[2:])
		}
	case "report":
		if len(args) > 1 && args[1] == "event" {
			return reportEvent(args[2:])
		}
	}
	usage()
	return 2
}

func runLifecycle(logger *slog.Logger, phase model.Phase, args []string) int {
	fs := flag.NewFlagSet(string(phase), flag.ContinueOnError)
	configPath := fs.String("config", "configs/policy.example.yaml", "path to config YAML")
	scontrolPath := fs.String("scontrol", "scontrol", "path to scontrol")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config failed", "error", err)
		return 1
	}
	eng, err := engine.New(&cfg.Policy, cfg.Plugins)
	if err != nil {
		logger.Error("create engine failed", "error", err)
		return 1
	}
	notifier := notify.NewManager(cfg.Notifications, nil)
	input := lifecycleInput(phase)
	decision, err := app.EvaluateWithFallback(context.Background(), app.Dependencies{
		Client:         daemon.NewClient(cfg.SocketPath),
		LocalEvaluator: eng,
	}, input)
	if err != nil {
		logger.Error("evaluation failed, falling open", "error", err)
		decision = model.EvaluationDecision{
			Verdict: model.VerdictAllowAlert,
			Source:  "fail-open",
			Summary: err.Error(),
		}
	}

	action := decision.ToActionDecision(input.Job)
	if err := slurm.ApplyDecision(context.Background(), slurm.CommandExecutor{CommandPath: *scontrolPath}, action); err != nil {
		logger.Error("apply decision failed", "error", err, "phase", phase)
		if phase == model.PhaseProlog && (decision.Verdict == model.VerdictBlockDrain || decision.Verdict == model.VerdictBlockDrainRequeue) {
			return 1
		}
	}

	event := model.NotificationEvent{
		ReceiverNames: decision.NotificationReceivers,
		NodeName:      input.Job.NodeName,
		JobID:         input.Job.ID,
		Verdict:       decision.Verdict,
		Summary:       decision.Summary,
		DrainReason:   decision.DrainReason,
		Source:        decision.Source,
	}
	if err := notifier.Notify(context.Background(), event); err != nil {
		logger.Error("notification failed", "error", err)
	}

	if phase == model.PhaseProlog && (decision.Verdict == model.VerdictBlockDrain || decision.Verdict == model.VerdictBlockDrainRequeue) {
		return 1
	}
	return 0
}

func runChecks(logger *slog.Logger, args []string) int {
	fs := flag.NewFlagSet("check run", flag.ContinueOnError)
	configPath := fs.String("config", "configs/policy.example.yaml", "path to config YAML")
	phase := fs.String("phase", string(model.PhaseProlog), "phase to execute")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config failed", "error", err)
		return 1
	}
	eng, err := engine.New(&cfg.Policy, cfg.Plugins)
	if err != nil {
		logger.Error("create engine failed", "error", err)
		return 1
	}
	results, err := eng.RunChecks(context.Background(), model.Phase(*phase), lifecycleJobContext(), model.NodeContext{Name: nodeName()})
	if err != nil {
		logger.Error("run checks failed", "error", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		logger.Error("encode checks failed", "error", err)
		return 1
	}
	return 0
}

func reportEvent(args []string) int {
	fs := flag.NewFlagSet("report event", flag.ContinueOnError)
	configPath := fs.String("config", "configs/policy.example.yaml", "path to config YAML")
	receivers := fs.String("receivers", "default", "comma-separated receiver names")
	node := fs.String("node", nodeName(), "node name")
	jobID := fs.String("job-id", os.Getenv("SLURM_JOB_ID"), "job id")
	verdict := fs.String("verdict", string(model.VerdictAllowAlert), "verdict")
	summary := fs.String("summary", "", "summary")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	notifier := notify.NewManager(cfg.Notifications, nil)
	event := model.NotificationEvent{
		ReceiverNames: splitCSV(*receivers),
		NodeName:      *node,
		JobID:         *jobID,
		Verdict:       model.Verdict(*verdict),
		Summary:       *summary,
	}
	if err := notifier.Notify(context.Background(), event); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func lifecycleInput(phase model.Phase) model.EvaluationInput {
	return model.EvaluationInput{
		Phase: phase,
		Job:   lifecycleJobContext(),
		Node:  model.NodeContext{Name: nodeName()},
	}
}

func lifecycleJobContext() model.JobContext {
	exitCode := 0
	if raw := os.Getenv("SLURM_JOB_EXIT_CODE"); raw != "" {
		fmt.Sscanf(raw, "%d", &exitCode)
	}
	return model.JobContext{
		ID:         os.Getenv("SLURM_JOB_ID"),
		NodeName:   nodeName(),
		Cluster:    os.Getenv("SLURM_CLUSTER_NAME"),
		User:       os.Getenv("SLURM_JOB_USER"),
		ExitCode:   exitCode,
		SignalName: os.Getenv("SLURM_JOB_SIGNAL"),
	}
}

func nodeName() string {
	if value := os.Getenv("SLURMD_NODENAME"); value != "" {
		return value
	}
	if value := os.Getenv("SLURM_NODELIST"); value != "" {
		return value
	}
	hostname, _ := os.Hostname()
	return hostname
}

func splitCSV(value string) []string {
	items := strings.Split(value, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: guardctl [prolog|epilog|check run|report event] [flags]")
}
