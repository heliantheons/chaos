package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	baseconfig "github.com/heliantheon/common/config"
	pkgdb "github.com/heliantheon/common/database"
	commonlog "github.com/heliantheon/common/log"
)

// Cfg 返回 Chaos 配置单例
func Cfg() *baseconfig.Cfg {
	return baseconfig.Chaos()
}

// Validate 校验 Chaos 所有必需模块的启动配置。
func Validate() error {
	var errs []error
	for _, key := range []string{
		"db.url", "aegis.audience", "aegis.issuer", "aegis.secret-key",
		"smtp.host", "smtp.port", "smtp.username", "smtp.password", "smtp.from",
		"r2.account-id", "r2.access-key-id", "r2.access-key-secret", "r2.bucket", "r2.domain",
		"eventing.broker-url", "eventing.mail-event-type", "eventing.mail-event-source",
		"loki.url", "loki.namespace",
	} {
		if strings.TrimSpace(Cfg().GetString(key)) == "" {
			errs = append(errs, fmt.Errorf("必需配置 %s 未设置", key))
		}
	}
	if _, err := GetAegisSecretKeyBytes(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// GetAegisAudience 获取 Chaos 服务 audience（用于 token 验证）
func GetAegisAudience() string {
	audience := Cfg().GetString("aegis.audience")
	if audience == "" {
		return "chaos"
	}
	return audience
}

// GetAegisIssuer 获取 Aegis API/issuer 端点。
func GetAegisIssuer() string {
	issuer := strings.TrimRight(Cfg().GetString("aegis.issuer"), "/")
	if issuer == "" {
		return "https://aegis.heliannuuthus.com/api"
	}
	return issuer
}

// GetAegisSecretKeyBytes 获取 Chaos 服务的 48 字节 token seed。
func GetAegisSecretKeyBytes() ([]byte, error) {
	secret := Cfg().GetString("aegis.secret-key")
	if secret == "" {
		return nil, fmt.Errorf("chaos aegis.secret-key 未配置")
	}
	seed, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("解码 chaos aegis.secret-key 失败: %w", err)
	}
	if len(seed) != 48 {
		return nil, fmt.Errorf("chaos aegis.secret-key 长度错误: 期望 48 字节 seed, 实际 %d 字节", len(seed))
	}
	return seed, nil
}

// GetSMTPHost 获取 SMTP 主机
func GetSMTPHost() string {
	return Cfg().GetString("smtp.host")
}

// GetSMTPPort 获取 SMTP 端口
func GetSMTPPort() int {
	port := Cfg().GetInt("smtp.port")
	if port == 0 {
		return 587
	}
	return port
}

// GetSMTPUsername 获取 SMTP 用户名
func GetSMTPUsername() string {
	return Cfg().GetString("smtp.username")
}

// GetSMTPPassword 获取 SMTP 密码
func GetSMTPPassword() string {
	return Cfg().GetString("smtp.password")
}

// GetSMTPFrom 获取发件人地址
func GetSMTPFrom() string {
	return Cfg().GetString("smtp.from")
}

// GetSMTPFromName 获取发件人名称
func GetSMTPFromName() string {
	name := Cfg().GetString("smtp.from-name")
	if name == "" {
		return "Helios"
	}
	return name
}

// GetEventingBrokerURL returns the fixed Knative Broker ingress URL.
func GetEventingBrokerURL() string { return Cfg().GetString("eventing.broker-url") }

// GetMailEventType returns the CloudEvent type selected by the Knative Trigger.
func GetMailEventType() string { return Cfg().GetString("eventing.mail-event-type") }

// GetMailEventSource returns the trusted CloudEvent source for Chaos mail events.
func GetMailEventSource() string { return Cfg().GetString("eventing.mail-event-source") }

// GetLokiURL returns the cluster-internal Loki gateway used by the controlled
// Chaos log-query proxy. It is never exposed to browser clients.
func GetLokiURL() string { return Cfg().GetString("loki.url") }

// GetLokiNamespace returns the Kubernetes namespace visible through the
// controlled Chaos log-query proxy.
func GetLokiNamespace() string { return Cfg().GetString("loki.namespace") }

// GetCloudflareR2AccessKeyID 获取 R2 Access Key ID
func GetCloudflareR2AccessKeyID() string {
	return Cfg().GetString("r2.access-key-id")
}

// GetCloudflareR2AccessKeySecret 获取 R2 Access Key Secret
func GetCloudflareR2AccessKeySecret() string {
	return Cfg().GetString("r2.access-key-secret")
}

// GetCloudflareR2Bucket 获取 R2 Bucket 名称
func GetCloudflareR2Bucket() string {
	return Cfg().GetString("r2.bucket")
}

// GetCloudflareR2Endpoint 获取 R2 Endpoint（根据 Account ID 构建）
func GetCloudflareR2Endpoint() string {
	accountID := Cfg().GetString("r2.account-id")
	if accountID == "" {
		return ""
	}
	return "https://" + accountID + ".r2.cloudflarestorage.com"
}

// GetCloudflareR2PublicURL 获取 R2 公开访问 URL
func GetCloudflareR2PublicURL() string {
	return Cfg().GetString("r2.domain")
}

// InitDB initializes the Chaos database connection.
func InitDB(logger *slog.Logger) (*gorm.DB, error) {
	dsn := Cfg().GetString("db.url")
	db, err := pkgdb.Connect(dsn, pkgdb.WithLogWriter(commonlog.GormWriter(logger)))
	if err != nil {
		return nil, fmt.Errorf("连接 Chaos 数据库失败: %w", err)
	}
	logger.Info("数据库连接成功", "database", "chaos")
	return db, nil
}
