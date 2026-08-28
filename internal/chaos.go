package chaos

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/heliantheon/aegis-go/guard"
	"github.com/heliantheon/chaos/config"
	"github.com/heliantheon/chaos/internal/eventauth"
	chaoseventing "github.com/heliantheon/chaos/internal/eventing"
	"github.com/heliantheon/chaos/internal/logquery"
	"github.com/heliantheon/chaos/internal/mail"
	"github.com/heliantheon/chaos/internal/models"
	"github.com/heliantheon/chaos/internal/storage"
	"github.com/heliantheon/chaos/internal/template"
	"github.com/heliantheon/common/metric"
)

// Chaos 模块实例
type Chaos struct {
	handler         *Handler
	mailService     *mail.Service
	templateService *template.Service
	storageService  *storage.Service
	logService      *logquery.Service
}

// New 创建 Chaos 实例
func New(_ context.Context, db *gorm.DB, logger *slog.Logger, metrics *metric.Registry) (*Chaos, error) {
	if db == nil {
		return nil, fmt.Errorf("数据库连接未初始化")
	}
	if logger == nil || metrics == nil {
		return nil, fmt.Errorf("logger 和 metrics 未初始化")
	}

	if err := autoMigrate(db); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	brokerPublisher, err := chaoseventing.NewPublisher(config.GetEventingBrokerURL())
	if err != nil {
		return nil, fmt.Errorf("创建 Knative Broker 发布器失败: %w", err)
	}
	serviceSeed, err := config.GetAegisSecretKeyBytes()
	if err != nil {
		return nil, fmt.Errorf("加载事件签名密钥失败: %w", err)
	}
	signer, err := eventauth.NewSigner(serviceSeed)
	if err != nil {
		return nil, fmt.Errorf("创建事件签名器失败: %w", err)
	}
	templateSvc := template.NewService(db, logger)
	idempotencyStore := mail.NewGORMIdempotencyStore(db)

	mailSvc, err := mail.NewService(templateSvc, signer, config.GetMailEventType(), config.GetMailEventSource(), logger)
	if err != nil {
		return nil, fmt.Errorf("创建邮件服务失败: %w", err)
	}
	mailPublisher, err := mail.NewPublisher(brokerPublisher, signer, idempotencyStore, mail.PublisherConfig{
		EventType: config.GetMailEventType(),
		Source:    config.GetMailEventSource(),
	}, logger)
	if err != nil {
		mailSvc.Close()
		return nil, fmt.Errorf("创建邮件事件发布器失败: %w", err)
	}
	mailVerifyCtx, mailVerifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	verifyOptionalDependency(mailVerifyCtx, "mail", mailSvc.Verify, logger)
	mailVerifyCancel()

	storageSvc, err := storage.NewService(logger)
	if err != nil {
		mailSvc.Close()
		return nil, fmt.Errorf("创建存储服务失败: %w", err)
	}
	storageVerifyCtx, storageVerifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	verifyOptionalDependency(storageVerifyCtx, "storage", storageSvc.Verify, logger)
	storageVerifyCancel()

	logSvc, err := logquery.New(config.GetLokiURL(), config.GetLokiNamespace(), metrics)
	if err != nil {
		mailSvc.Close()
		return nil, fmt.Errorf("创建日志查询服务失败: %w", err)
	}

	aud := config.GetAegisAudience()
	g, err := guard.NewGin(aud)
	if err != nil {
		mailSvc.Close()
		return nil, fmt.Errorf("创建鉴权中间件失败: %w", err)
	}
	handler := NewHandler(g, aud, mailPublisher, mailSvc, templateSvc, storageSvc, logSvc, logger)

	return &Chaos{
		handler:         handler,
		mailService:     mailSvc,
		templateService: templateSvc,
		storageService:  storageSvc,
		logService:      logSvc,
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
		&models.MailDeliveryRequest{},
	)
}

// Handler 获取 HTTP Handler
func (c *Chaos) Handler() *Handler {
	return c.handler
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
	_ = ctx
	c.mailService.Close()
	return nil
}
