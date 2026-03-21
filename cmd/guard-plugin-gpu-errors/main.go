package main

import (
	"os"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/checks/gpuerrors"
)

func main() {
	os.Exit(checkplugin.Run(gpuerrors.CheckName, os.Stdin, os.Stdout, gpuerrors.Checker{}.Check))
}
