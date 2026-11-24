package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	client, err := NewAPIClient(os.Getenv("SED_API_URL"), os.Getenv("SED_TOKEN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv := NewMCPServer(NewRegistry(client))
	if err := srv.Serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
