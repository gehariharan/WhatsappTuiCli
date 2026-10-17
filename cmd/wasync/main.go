package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"whatsappsync/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cli.New().Run(ctx, os.Args[1:])
	stop()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			os.Exit(130)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintln(os.Stderr, "Command timed out; retry or increase --timeout.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wasync:", err)
		os.Exit(1)
	}
}
