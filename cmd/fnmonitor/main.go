package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fnmonitor/internal/config"
	"fnmonitor/internal/monitor"
	"fnmonitor/internal/notify"
	"fnmonitor/internal/server"
)

func main() {
	var (
		configPath = flag.String("config", "config.example.yaml", "path to YAML config")
		once       = flag.Bool("once", false, "collect once and print JSON")
		testNotify = flag.Bool("test-notify", false, "send a test notification and exit")
		logJSON    = flag.Bool("log-json", false, "write logs as JSON")
	)
	flag.Parse()

	if *logJSON {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fail("load config", err)
	}
	mon, err := monitor.New(cfg)
	if err != nil {
		fail("create monitor", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *testNotify {
		results := mon.SendTest(ctx)
		writeJSON(results)
		if hasFailedNotifyResult(results) {
			os.Exit(1)
		}
		return
	}
	if *once {
		if err := mon.CollectOnce(ctx); err != nil {
			slog.Warn("collection completed with errors", "error", err)
		}
		writeJSON(mon.State())
		return
	}

	go mon.Run(ctx)

	testResultPath := os.Getenv("FNMONITOR_TEST_RESULT_FILE")
	if testResultPath == "" && *configPath != "" {
		testResultPath = filepath.Join(filepath.Dir(*configPath), "last-notification-test.log")
	}
	srv := server.New(cfg.Server, mon, testResultPath)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server shutdown failed", "error", err)
		}
	}()

	slog.Info("fnmonitor listening", "addr", cfg.Server.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fail("run server", err)
	}
}

func writeJSON(value any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fail("write json", err)
	}
}

func hasFailedNotifyResult(results []notify.Result) bool {
	for _, result := range results {
		if !result.OK {
			return true
		}
	}
	return false
}

func fail(action string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	os.Exit(1)
}
