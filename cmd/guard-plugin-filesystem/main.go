package main

import (
	"os"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/checks/filesystem"
)

func main() {
	os.Exit(checkplugin.Run(filesystem.CheckName, os.Stdin, os.Stdout, filesystem.Checker{}.Check))
}
