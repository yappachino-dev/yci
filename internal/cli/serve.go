package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/api"
)

const (
	flagAddr = "addr"
	envToken = "YCI_SERVE_TOKEN"
)

// defaultAddr binds loopback rather than every interface: a bearer token is
// not a reason to be reachable from the network by default.
const defaultAddr = "127.0.0.1:8737"

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the library over HTTP, read-only, behind a bearer token",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		c := Container(ctx)

		token := os.Getenv(envToken)
		server, err := api.New(c, token)
		if err != nil {
			return err
		}

		addr, _ := cmd.Flags().GetString(flagAddr)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", addr, err)
		}

		httpServer := &http.Server{
			Handler:           server.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		cmd.Printf("Serving %s on http://%s\n", c.Config.LibraryDir, listener.Addr())

		errs := make(chan error, 1)
		go func() { errs <- httpServer.Serve(listener) }()

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		select {
		case err := <-errs:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case sig := <-stop:
			cmd.Printf("\nReceived %s, shutting down.\n", sig)

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			return httpServer.Shutdown(shutdownCtx)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().String(flagAddr, defaultAddr, "Address to listen on (loopback by default)")
}
