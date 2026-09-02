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
	"github.com/suryanshu-09/holy_grail/internal/extraction"
	"github.com/suryanshu-09/holy_grail/internal/llm"
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

	extractor, err := extraction.NewService(cfg.DataDir)
	if err != nil {
		logger.Error("failed to initialise extraction service", "error", err)
		os.Exit(1)
	}
	if ocr, ocrErr := extraction.NewTesseractOCR(); ocrErr == nil {
		extractor = extractor.WithOCRer(ocr)
	} else {
		logger.Warn("OCR fallback disabled", "reason", ocrErr)
	}

	extractionSvc, err := extraction.NewExtractionService(cfg.DataDir, extractor, documentRepo, questionRepo)
	if err != nil {
		logger.Error("failed to initialise extraction pipeline", "error", err)
		os.Exit(1)
	}
	// Wire topic classifier: LLM when OPENAI_API_KEY set, otherwise heuristic fallback.
	// Also wire LLM fallback for extraction when key is present (reuses same client).
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		openai, oErr := llm.NewOpenAIClient(key, "gpt-3.5-turbo", "")
		if oErr != nil {
			logger.Warn("failed to create OpenAI client", "error", oErr)
			heuristic := topics.NewHeuristicClassifier()
			extractionSvc = extractionSvc.WithTopicClassifier(heuristic, topicRepo)
			logger.Info("heuristic topic classifier enabled (OpenAI client creation failed)")
		} else {
			fallback := &extraction.LLMFallback{Client: openai, MaxPages: 3}
			extractionSvc = extractionSvc.WithLLMFallback(fallback)
			logger.Info("LLM fallback enabled for extraction")
			classifier := topics.NewClassifier(openai, 3, 100*time.Millisecond)
			extractionSvc = extractionSvc.WithTopicClassifier(classifier, topicRepo)
			logger.Info("LLM topic classifier enabled")
		}
	} else {
		heuristic := topics.NewHeuristicClassifier()
		extractionSvc = extractionSvc.WithTopicClassifier(heuristic, topicRepo)
		logger.Info("heuristic topic classifier enabled (no OPENAI_API_KEY)")
	}

	deps := apihttp.RouterDeps{
		DB:         db,
		Documents:  documents.NewService(documentRepo, store),
		Extraction: extractionSvc,
		Questions:  questions.NewService(questionRepo),
		Topics:     topics.NewService(topicRepo),
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
