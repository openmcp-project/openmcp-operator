package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/openmcp-operator/cmd/openmcp-operator/app"
)

func main() {
	ctx := context.Background()
	defer ctx.Done()
	cmd := app.NewOpenMCPOperatorCommand(ctx)

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
