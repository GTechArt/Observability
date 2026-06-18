package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/linkoerr"
	"boot.dev/linko/internal/store"
	pkgerr "github.com/pkg/errors"
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

	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialized logger: %v\n", err)
		return 1
	}

	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
	)

	defer func() {
		if err := closeLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store",
			slog.Any("error", err),
		)
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
		s.logger.Error("failed to shutdown server",
			slog.Any("error", err),
		)
		return 1
	}
	if serverErr != nil {
		s.logger.Error("server error",
			slog.Any("error", err),
		)
		return 1
	}
	return 0
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}
type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {

	handlers := []slog.Handler{}
	handlers = append(handlers, slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	}))

	closers := []closeFunc{}

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE /*|os.O_APPEND*/, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file %w", err)
		}
		bufferFile := bufio.NewWriterSize(f, 8192)

		close := func() error {
			err = bufferFile.Flush()
			if err != nil {
				return fmt.Errorf("faild to flush log file: %w", err)
			}
			err = f.Close()
			if err != nil {
				return fmt.Errorf("faild to close log file: %w", err)
			}
			return nil
		}
		handlers = append(handlers, slog.NewJSONHandler(bufferFile, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		}))
		closers = append(closers, close)
	}

	closer := func() error {
		var errs []error
		for _, close := range closers {
			if err := close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	return slog.New(slog.NewMultiHandler(handlers...)), closer, nil
}

type multiError interface {
	error
	Unwrap() []error
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}
		var attrs []slog.Attr

		fmt.Printf( /*os.Stderr,*/ "DEBUG: err.Error() = %v\n", err.Error())
		if multiErr, ok := errors.AsType[multiError](err); ok {
			for i, errUnwraped := range multiErr.Unwrap() {
				attrs = append(attrs, slog.Any(fmt.Sprintf("error_%d", i+1), linkoerr.Attrs(errUnwraped)))
			}
		} else {
			attrs = append(attrs, slog.Any("error", linkoerr.Attrs(err)))
		}

		if stackErr, ok := errors.AsType[stackTracer](err); ok {
			attrs = append(attrs, slog.Attr{
				Key:   "stack_trace",
				Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
			})

		}
		return slog.GroupAttrs("errors", attrs...)
	}
	return a
}
