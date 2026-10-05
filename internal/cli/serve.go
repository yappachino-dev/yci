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
	"github.com/yappachino-dev/yci/internal/app"
	"github.com/yappachino-dev/yci/internal/config"
	"github.com/yappachino-dev/yci/internal/download"
)

const (
	flagAddr         = "addr"
	flagPollInterval = "poll-interval"
	envToken         = "YCI_SERVE_TOKEN"
)

// defaultAddr binds loopback rather than every interface: a bearer token is
// not a reason to be reachable from the network by default.
const defaultAddr = "127.0.0.1:8737"

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the library over HTTP behind a bearer token, and drain the download queue",
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

		worker, err := newQueueWorker(cmd, c)
		if err != nil {
			return err
		}

		// Cancelled with the serve context, so Ctrl-C stops the drain between
		// cycles rather than killing a download mid-flight.
		workerCtx, stopWorker := context.WithCancel(ctx)
		defer stopWorker()
		go worker.Run(workerCtx, newConsoleReporter(cmd))

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

// newQueueWorker builds the background drain from the same config `download
// resume` uses, so a server downloads at the same pace a terminal does.
func newQueueWorker(cmd *cobra.Command, c *app.Container) (*download.Worker, error) {
	delay := resolveFlagOrConfig(cmd, flagDelay, c.Config.DownloadDelay)

	minSleep, maxSleep, err := config.ParseDelay(delay)
	if err != nil {
		return nil, err
	}

	cookiesFile := resolveFlagOrConfig(cmd, flagCookies, c.Config.CookiesFile)
	if cookiesFile != "" {
		if _, err := os.Stat(cookiesFile); err != nil {
			return nil, fmt.Errorf("cookies file %q: %w", cookiesFile, err)
		}
	}

	interval := download.DefaultWorkerInterval
	if v, _ := cmd.Flags().GetDuration(flagPollInterval); v > 0 {
		interval = v
	}

	return download.NewWorker(c.DownloadService, download.WorkerConfig{
		Run: download.QueueRunConfig{
			MinSleep:    minSleep,
			MaxSleep:    maxSleep,
			CookiesFile: cookiesFile,
		},
		Interval: interval,
		OnCycle:  reportCycle(cmd),
	}), nil
}

// reportCycle turns each drain into one stderr line. A worker runs for as
// long as the server is up, so per-track narration would be noise; the
// failure text is kept because it is the only record when something breaks.
func reportCycle(cmd *cobra.Command) func(download.DrainResult) {
	return func(r download.DrainResult) {
		switch {
		case r.Err != nil:
			cmd.PrintErrf("Queue drain failed: %v\n", r.Err)
		case r.Success > 0 || r.Failed > 0:
			cmd.PrintErrf("Queue drained: %d downloaded, %d failed.\n", r.Success, r.Failed)
		}
	}
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().String(flagAddr, defaultAddr, "Address to listen on (loopback by default)")
	serveCmd.Flags().Duration(flagPollInterval, 0, "How often to drain the download queue (default 30s)")
	serveCmd.Flags().String(flagDelay, "", "Override the configured download delay")
	serveCmd.Flags().String(flagCookies, "", "Override the configured cookies file")
}
