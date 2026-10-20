package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/gehariharan/wasync/internal/cli"
	"os"
	"os/signal"
	"syscall"
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
		var reported *cli.ReportedError
		if errors.As(err, &reported) { // Already written to stdout as a v1 envelope.
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "wasync:", err)
		os.Exit(1)
	}
}
