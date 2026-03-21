package main

import (
	"os"

	"github.com/yuuki/slurm-gpu-node-guard/internal/checkplugin"
	"github.com/yuuki/slurm-gpu-node-guard/internal/checks/rdma"
)

func main() {
	os.Exit(checkplugin.Run(rdma.CheckName, os.Stdin, os.Stdout, rdma.Checker{}.Check))
}
