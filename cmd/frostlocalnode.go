//go:build frosttest

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/keep-network/keep-core/pkg/frost/node"
	"github.com/spf13/cobra"
)

// This explicit local rehearsal path does not enable the production start flag.
var FrostLocalNodeCommand = &cobra.Command{Use: "frost-local-node <private-config>", Short: "Run a bounded local-regtest FROST node", Args: cobra.ExactArgs(1), RunE: runFrostLocalNode}
var frostLocalListen string

func init() {
	RootCmd.AddCommand(FrostLocalNodeCommand)
	FrostLocalNodeCommand.Flags().StringVar(&frostLocalListen, "listen", "127.0.0.1:0", "Loopback address serving public node status")
}
func runFrostLocalNode(command *cobra.Command, args []string) error {
	file, err := os.Open(args[0])
	if err != nil {
		return errors.New("cannot open private local node configuration")
	}
	defer file.Close()
	config, err := node.ReadConfig(file)
	if err != nil {
		return errors.New("invalid local node configuration")
	}
	ctx, cancel := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 15*time.Minute)
	defer timeout()
	host, _, err := net.SplitHostPort(frostLocalListen)
	if err != nil || host != "127.0.0.1" {
		return errors.New("local status listener required")
	}
	runtime, err := node.New(ctx, config)
	if err != nil {
		return errors.New("local FROST node initialization failed")
	}
	defer runtime.Close()
	listener, err := net.Listen("tcp", frostLocalListen)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(runtime.Status())
	})
	runtime.RegisterVerification(mux)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); done <- runtime.Run(ctx) }()
	select {
	case err = <-done:
	case err = <-served:
	case <-ctx.Done():
		err = ctx.Err()
	}
	err = localNodeOutcome(ctx, err)
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		return errors.New("local node did not stop within its shutdown bound")
	}
	stop, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	_ = server.Shutdown(stop)
	if errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "private local node phase %s: %v\n", runtime.Status().State, err)
		return errors.New("local FROST node runtime stopped; inspect private diagnostics")
	}
	return nil
}
