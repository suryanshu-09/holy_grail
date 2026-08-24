package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apihttp "github.com/suryanshu-09/holy_grail/internal/http"

	"github.com/suryanshu-09/holy_grail/internal/config"
	"github.com/suryanshu-09/holy_grail/internal/database"
	"github.com/suryanshu-09/holy_grail/internal/documents"
	"github.com/suryanshu-09/holy_grail/internal/logging"
	"github.com/suryanshu-09/holy_grail/internal/questions"
	"github.com/suryanshu-09/holy_grail/internal/storage"
	"github.com/suryanshu-09/holy_grail/internal/topics"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg := config.NewAppConfig()

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	db, err := database.NewConnection(&cfg)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	documentRepo := documents.NewRepository(db)
	questionRepo := questions.NewRepository(db)
	topicRepo := topics.NewRepository(db)

	store, err := storage.NewLocalStore(cfg.DataDir)
	if err != nil {
		logger.Error("failed to initialise local storage", "error", err)
		os.Exit(1)
	}

	deps := apihttp.RouterDeps{
		DB:        db,
		Documents: documents.NewService(documentRepo, store),
		Questions: questions.NewService(questionRepo),
		Topics:    topics.NewService(topicRepo),
	}

	server := &http.Server{
		Addr:         cfg.Host + ":" + cfg.Port,
		Handler:      apihttp.NewRouter(&cfg, deps),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("starting server", "addr", server.Addr, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Warn("server forced to shutdown", "error", err)
	} else {
		logger.Info("server exited properly")
	}
}
