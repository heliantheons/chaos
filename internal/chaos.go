package chaos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/heliantheon/aegis-go/guard"
	"github.com/heliantheon/chaos/config"
	"github.com/heliantheon/chaos/internal/logquery"
	"github.com/heliantheon/chaos/internal/mail"
	"github.com/heliantheon/chaos/internal/models"
	"github.com/heliantheon/chaos/internal/storage"
	"github.com/heliantheon/chaos/internal/template"
	"github.com/heliantheon/common/eventbus"
	"github.com/heliantheon/common/metric"
)

// Chaos 模块实例
type Chaos struct {
	handler         *Handler
	mailService     *mail.Service
	templateService *template.Service
	storageService  *storage.Service
	logService      *logquery.Service
	eventBus        *eventbus.Bus
	mailWorker      *eventbus.Subscription
}

// New 创建 Chaos 实例
func New(ctx context.Context, db *gorm.DB, logger *slog.Logger, metrics *metric.Registry) (*Chaos, error) {
	if db == nil {
		return nil, fmt.Errorf("数据库连接未初始化")
	}
	if logger == nil || metrics == nil {
		return nil, fmt.Errorf("logger 和 metrics 未初始化")
	}

	if err := autoMigrate(db); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	bus, err := eventbus.New(ctx, eventbus.Config{
		URLs:          config.GetNATSURLs(),
		Name:          "chaos",
		Source:        "urn:heliantheon:chaos",
		Token:         config.GetNATSToken(),
		Logger:        logger,
		Registerer:    metrics.Registerer(),
		MaxReconnects: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("连接事件总线失败: %w", err)
	}

	templateSvc := template.NewService(db, logger)

	mailSvc, err := mail.NewService(templateSvc, bus, logger)
	if err != nil {
		closeBusOnInitFailure(logger, bus)
		return nil, fmt.Errorf("创建邮件服务失败: %w", err)
	}
	mailVerifyCtx, mailVerifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	verifyOptionalDependency(mailVerifyCtx, "mail", mailSvc.Verify, logger)
	mailVerifyCancel()

	mailWorker, err := bus.Consume(ctx, eventbus.ConsumerConfig{
		Stream:        config.GetNATSStream(),
		Durable:       config.GetNATSConsumer(),
		FilterSubject: config.GetNATSSubject(),
		DLQSubject:    config.GetNATSDLQSubject(),
		AckWait:       45 * time.Second,
		RetryDelay:    30 * time.Second,
		MaxDeliver:    5,
		MaxAckPending: 8,
	}, mailSvc.HandleEvent)
	if err != nil {
		mailSvc.Close()
		closeBusOnInitFailure(logger, bus)
		return nil, fmt.Errorf("启动邮件消费者失败: %w", err)
	}

	storageSvc, err := storage.NewService(logger)
	if err != nil {
		cleanupOnInitFailure(logger, mailWorker, mailSvc, bus)
		return nil, fmt.Errorf("创建存储服务失败: %w", err)
	}
	storageVerifyCtx, storageVerifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	verifyOptionalDependency(storageVerifyCtx, "storage", storageSvc.Verify, logger)
	storageVerifyCancel()

	logSvc, err := logquery.New(config.GetLokiURL(), config.GetLokiNamespace(), metrics)
	if err != nil {
		cleanupOnInitFailure(logger, mailWorker, mailSvc, bus)
		return nil, fmt.Errorf("创建日志查询服务失败: %w", err)
	}

	aud := config.GetAegisAudience()
	g, err := guard.NewGin(aud)
	if err != nil {
		cleanupOnInitFailure(logger, mailWorker, mailSvc, bus)
		return nil, fmt.Errorf("创建鉴权中间件失败: %w", err)
	}
	handler := NewHandler(g, aud, mailSvc, templateSvc, storageSvc, logSvc, logger)

	return &Chaos{
		handler:         handler,
		mailService:     mailSvc,
		templateService: templateSvc,
		storageService:  storageSvc,
		logService:      logSvc,
		eventBus:        bus,
		mailWorker:      mailWorker,
	}, nil
}

func verifyOptionalDependency(ctx context.Context, name string, verify func(context.Context) error, loggers ...*slog.Logger) bool {
	if err := verify(ctx); err != nil {
		if len(loggers) > 0 && loggers[0] != nil {
			loggers[0].WarnContext(ctx, "optional dependency preflight failed", "dependency", name, "error", err)
		}
		return false
	}
	return true
}

// autoMigrate 自动迁移数据库
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.EmailTemplate{},
	)
}

// Handler 获取 HTTP Handler
func (c *Chaos) Handler() *Handler {
	return c.handler
}

// MailService 获取邮件服务（供 Aegis 等内部包调用）
func (c *Chaos) MailService() *mail.Service {
	return c.mailService
}

// TemplateService 获取模板服务
func (c *Chaos) TemplateService() *template.Service {
	return c.templateService
}

// StorageService 获取存储服务
func (c *Chaos) StorageService() *storage.Service {
	return c.storageService
}

// Close 关闭服务
func (c *Chaos) Close(ctx context.Context) error {
	var errs []error
	if c.mailWorker != nil {
		if err := c.mailWorker.Drain(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	c.mailService.Close()
	if c.eventBus != nil {
		if err := c.eventBus.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeBus(bus *eventbus.Bus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return bus.Close(ctx)
}

func closeBusOnInitFailure(logger *slog.Logger, bus *eventbus.Bus) {
	if err := closeBus(bus); err != nil {
		logger.Warn("close event bus after initialization failure", "error", err)
	}
}

func cleanupOnInitFailure(logger *slog.Logger, subscription *eventbus.Subscription, mailService *mail.Service, bus *eventbus.Bus) {
	if err := closeSubscription(subscription); err != nil {
		logger.Warn("close mail subscription after initialization failure", "error", err)
	}
	mailService.Close()
	closeBusOnInitFailure(logger, bus)
}

func closeSubscription(subscription *eventbus.Subscription) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return subscription.Drain(ctx)
}
