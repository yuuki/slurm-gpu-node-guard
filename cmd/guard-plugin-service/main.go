package main

import (
	"os"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/checks/service"
)

func main() {
	os.Exit(checkplugin.Run(service.CheckName, os.Stdin, os.Stdout, service.Checker{}.Check))
}
