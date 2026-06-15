package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {

	logger, err := initialiezLogger(os.Getenv("LINKO_LOG_FILE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialized logger: %v\n", err)
		return 1
	}

	st, err := store.New(dataDir, logger)
	if err != nil {
		fmt.Printf("failed to create store: %v", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		s.logger.Printf("failed to shutdown server: %v", err)
		return 1
	}
	if serverErr != nil {
		s.logger.Printf("server error: %v", serverErr)
		return 1
	}
	return 0
}

func initialiezLogger(logFile string) (*log.Logger, error) {
	if logFile == "" {
		return log.New(os.Stderr, "", log.LstdFlags), nil
	}
	f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o664)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %w", err)
	}
	multiWriter := io.MultiWriter(f, os.Stderr)
	return log.New(multiWriter, "", log.LstdFlags), nil
}
