package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/heliantheon/aegis-go/guard"
	chaosconfig "github.com/heliantheon/chaos/config"
	chaos "github.com/heliantheon/chaos/internal"
	"github.com/heliantheon/common/config"
	commonlog "github.com/heliantheon/common/log"
	"github.com/heliantheon/common/metric"
)

func main() {
	config.LoadChaos()
	logger, err := commonlog.New(commonlog.Config{
		Service:     "chaos",
		Version:     config.GetAppVersion(),
		Environment: chaosconfig.Cfg().GetString("app.environment"),
		Level:       config.GetLogLevel(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(logger); err != nil {
		logger.Error("chaos stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := chaosconfig.Validate(); err != nil {
		return fmt.Errorf("validate Chaos configuration: %w", err)
	}
	if err := initTokenManager(); err != nil {
		return err
	}
	db, err := chaosconfig.InitDB(logger)
	if err != nil {
		return err
	}
	metrics, err := metric.New(metric.Config{
		Service:     "chaos",
		Version:     config.GetAppVersion(),
		Environment: chaosconfig.Cfg().GetString("app.environment"),
	})
	if err != nil {
		return fmt.Errorf("initialize metrics: %w", err)
	}

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	app, err := chaos.New(workerCtx, db, logger, metrics)
	if err != nil {
		return fmt.Errorf("initialize Chaos: %w", err)
	}

	if !config.IsDebug() {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.ErrorContext(c.Request.Context(), "HTTP handler panic", "panic_type", fmt.Sprintf("%T", recovered))
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/metrics", gin.WrapH(metrics.Handler()))
	app.Handler().RegisterRoutes(router)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.GetServerPort()),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("chaos started", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var serveErr error
	select {
	case <-signalCtx.Done():
		logger.Info("chaos shutdown requested")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve HTTP: %w", err)
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelShutdown()
	shutdownErrors := []error{serveErr}
	if err := server.Shutdown(shutdownCtx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("shutdown HTTP server: %w", err))
	}
	if err := app.Close(shutdownCtx); err != nil {
		shutdownErrors = append(shutdownErrors, err)
	}
	cancelWorkers()
	return errors.Join(shutdownErrors...)
}

func initTokenManager() error {
	seed, err := chaosconfig.GetAegisSecretKeyBytes()
	if err != nil {
		return fmt.Errorf("initialize Chaos token manager: %w", err)
	}
	if err := guard.NewServiceTokenManager(chaosconfig.GetAegisIssuer(), chaosconfig.GetAegisAudience(), seed); err != nil {
		return fmt.Errorf("initialize Chaos token manager: %w", err)
	}
	return nil
}
