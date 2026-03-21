package main

import (
	"os"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/checks/gpu"
)

func main() {
	os.Exit(checkplugin.Run(gpu.CheckName, os.Stdin, os.Stdout, gpu.Checker{}.Check))
}
