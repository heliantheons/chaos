package chaos

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/heliantheon/aegis-go/guard"
	reqr "github.com/heliantheon/aegis-go/guard/requirement"
	"github.com/heliantheon/aegis-go/utilities/relation"
	"github.com/heliantheon/chaos/internal/logquery"
	"github.com/heliantheon/chaos/internal/mail"
	"github.com/heliantheon/chaos/internal/models"
	"github.com/heliantheon/chaos/internal/storage"
	"github.com/heliantheon/chaos/internal/template"
)

// Handler Chaos API Handler
type Handler struct {
	guard           *guard.Gin
	audience        string
	mailService     *mail.Service
	templateService *template.Service
	storageService  *storage.Service
	logService      *logquery.Service
	logger          *slog.Logger
}

// NewHandler 创建 Handler
func NewHandler(g *guard.Gin, audience string, mailSvc *mail.Service, templateSvc *template.Service, storageSvc *storage.Service, logSvc *logquery.Service, logger *slog.Logger) *Handler {
	return &Handler{
		guard:           g,
		audience:        audience,
		mailService:     mailSvc,
		templateService: templateSvc,
		storageService:  storageSvc,
		logService:      logSvc,
		logger:          logger,
	}
}

// QueryLogs returns a bounded historical log page. The client cannot provide
// raw LogQL; ParseOptions accepts only the supported filters.
func (h *Handler) QueryLogs(c *gin.Context) {
	options, err := logquery.ParseOptions(c.Request.URL.Query(), time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.logService.Query(c.Request.Context(), options)
	if err != nil {
		h.logger.ErrorContext(c.Request.Context(), "query application logs failed", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "日志服务暂时不可用"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// StreamLogs streams normalized log entries as authenticated server-sent
// events. The write deadline is refreshed for every event and heartbeat.
func (h *Handler) StreamLogs(c *gin.Context) {
	options, err := logquery.ParseOptions(c.Request.URL.Query(), time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	options.Direction = "forward"
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	controller := http.NewResponseController(c.Writer)
	write := func(event string, payload []byte) error {
		if err := controller.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
			return fmt.Errorf("set log stream write deadline: %w", err)
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	err = h.logService.Tail(c.Request.Context(), options, func(entry logquery.Entry) error {
		payload, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return write("log", payload)
	}, func() error {
		return write("heartbeat", []byte(strconv.Quote(time.Now().UTC().Format(time.RFC3339))))
	})
	if err != nil && c.Request.Context().Err() == nil {
		h.logger.WarnContext(c.Request.Context(), "stream application logs stopped", "error", err)
		if writeErr := write("error", []byte(`{"message":"日志流已中断"}`)); writeErr != nil {
			h.logger.DebugContext(c.Request.Context(), "write log stream error event failed", "error", writeErr)
		}
	}
}

// SendMail 发送邮件 POST /api/mail
func (h *Handler) SendMail(c *gin.Context) {
	var req mail.SendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deliveryID, err := h.mailService.Enqueue(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"code":    "MAIL_QUEUE_UNAVAILABLE",
				"message": "邮件队列暂时不可用",
			},
		})
		return
	}

	c.JSON(http.StatusAccepted, mail.SendResponse{DeliveryID: deliveryID})
}

// CreateTemplate 创建模板 POST /api/templates
func (h *Handler) CreateTemplate(c *gin.Context) {
	var req template.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tpl, err := h.templateService.Create(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, tpl)
}

// GetTemplate 获取模板 GET /api/templates/:id
func (h *Handler) GetTemplate(c *gin.Context) {
	templateID := c.Param("id")

	tpl, err := h.templateService.Get(c.Request.Context(), templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// ListTemplates 列出模板 GET /api/templates
func (h *Handler) ListTemplates(c *gin.Context) {
	var serviceID *string
	if sid := c.Query("service_id"); sid != "" {
		serviceID = &sid
	}

	templates, err := h.templateService.List(c.Request.Context(), serviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, templates)
}

// UpdateTemplate 更新模板 PATCH /api/templates/:id
func (h *Handler) UpdateTemplate(c *gin.Context) {
	templateID := c.Param("id")

	var req template.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.templateService.Update(c.Request.Context(), templateID, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "模板更新成功"})
}

// DeleteTemplate 删除模板 DELETE /api/templates/:id
func (h *Handler) DeleteTemplate(c *gin.Context) {
	templateID := c.Param("id")

	if err := h.templateService.Delete(c.Request.Context(), templateID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// RenderTemplate 预览渲染模板 POST /api/templates/:id/render
func (h *Handler) RenderTemplate(c *gin.Context) {
	templateID := c.Param("id")

	var req struct {
		Data map[string]any `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	subject, body, err := h.templateService.Render(c.Request.Context(), templateID, req.Data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, template.RenderResponse{
		Subject: subject,
		Body:    body,
	})
}

// PresignUpload 生成 Presigned URL POST /api/presign
func (h *Handler) PresignUpload(c *gin.Context) {
	var req storage.PresignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("参数错误: %v", err)})
		return
	}

	resp, err := h.storageService.GeneratePresignedURL(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成上传链接失败"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	svc := "service:" + h.audience

	api := r.Group("/api")
	api.Use(h.guard.Require())
	{
		api.POST("/mail", h.SendMail)

		logs := api.Group("/logs")
		logs.Use(h.guard.Require(reqr.Relation(relation.Qualify("admin", svc))))
		{
			logs.GET("", h.QueryLogs)
			logs.GET("/stream", h.StreamLogs)
		}

		templates := api.Group("/templates")
		templates.Use(h.guard.Require(reqr.Relation(relation.Qualify("admin", svc))))
		{
			templates.POST("", h.CreateTemplate)
			templates.GET("", h.ListTemplates)
			templates.GET("/:id", h.GetTemplate)
			templates.PATCH("/:id", h.UpdateTemplate)
			templates.DELETE("/:id", h.DeleteTemplate)
			templates.POST("/:id/render", h.RenderTemplate)
		}

		api.POST("/presign", h.guard.Require(reqr.User(), reqr.Relation(relation.Qualify("editor", svc))), h.PresignUpload)
	}
}

// MailService 获取邮件服务（供 Aegis 等内部调用）
func (h *Handler) MailService() *mail.Service {
	return h.mailService
}

// TemplateService 获取模板服务（供内部调用）
func (h *Handler) TemplateService() *template.Service {
	return h.templateService
}

// StorageService 获取存储服务（供内部调用）
func (h *Handler) StorageService() *storage.Service {
	return h.storageService
}

// Re-export types for external use
type (
	EmailTemplate  = models.EmailTemplate
	SendMailReq    = mail.SendRequest
	TemplateCreate = template.CreateRequest
	TemplateUpdate = template.UpdateRequest
)
