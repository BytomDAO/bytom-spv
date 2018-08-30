package main

import (
	"runtime"

	cmd "github.com/bytom-spv/cmd/bytomcli/commands"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	cmd.Execute()
}
