package clash_link

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Controller handles HTTP requests for clash link generation
type Controller struct {
	svc     *LinkService
	baseURL string
}

// NewController creates a new clash link controller
func NewController(db *gorm.DB, storeDir string) (*Controller, error) {
	svc, err := NewLinkService(db, storeDir)
	if err != nil {
		return nil, err
	}
	return &Controller{svc: svc}, nil
}

// RegisterRoutes registers clash link routes on the gin engine
// apiGroup: the authenticated API group (e.g., /panel/api)
// publicGroup: the public group (e.g., /) for serving YAML files
func (c *Controller) RegisterRoutes(apiGroup, publicGroup *gin.RouterGroup) {
	// Authenticated API endpoints
	clashAPI := apiGroup.Group("/clash-link")
	clashAPI.POST("/generate", c.handleGenerate)
	clashAPI.GET("/list", c.handleList)
	clashAPI.DELETE("/:token", c.handleDelete)
	clashAPI.POST("/:token/regenerate", c.handleRegenerate)
	clashAPI.GET("/backup", c.handleBackup)

	// Public YAML serving endpoint (no auth required)
	publicGroup.GET("/d/:token", c.handleServeYAML)
}

// handleGenerate handles POST /panel/api/clash-link/generate
func (c *Controller) handleGenerate(ctx *gin.Context) {
	var req GenerateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// Try form data
		req = GenerateRequest{
			ConfigName: ctx.PostForm("config_name"),
			MixedPort:  parsePort(ctx.PostForm("mixed_port")),
			AllowLan:   ctx.PostForm("allow_lan") == "true",
			Mode:       ctx.PostForm("mode"),
			LogLevel:   ctx.PostForm("log_level"),
			GroupName:  ctx.PostForm("group_name"),
		}
		// Parse inbound_ids from form
		if idsStr := ctx.PostForm("inbound_ids"); idsStr != "" {
			for _, s := range strings.Split(idsStr, ",") {
				if id, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && id > 0 {
					req.InboundIDs = append(req.InboundIDs, id)
				}
			}
		}
	}

	// Set host for URL generation
	req.Host = ctx.Request.Host

	resp, err := c.svc.Generate(req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"msg":     err.Error(),
		})
		return
	}

	// Build full URL
	scheme := "http"
	if ctx.Request.TLS != nil {
		scheme = "https"
	}
	fullURL := fmt.Sprintf("%s://%s/d/%s", scheme, req.Host, resp.Token)

	ctx.JSON(http.StatusOK, gin.H{
		"success":   true,
		"token":     resp.Token,
		"yaml_url":  resp.YAMLURL,
		"full_url":  fullURL,
		"proxy_num": resp.ProxyNum,
	})
}

// handleList handles GET /panel/api/clash-link/list
func (c *Controller) handleList(ctx *gin.Context) {
	records, err := c.svc.ListConfigs()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"msg":     err.Error(),
		})
		return
	}

	if records == nil {
		records = []ConfigRecord{}
	}

	ctx.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    records,
	})
}

// handleDelete handles DELETE /panel/api/clash-link/:token
func (c *Controller) handleDelete(ctx *gin.Context) {
	token := ctx.Param("token")
	if err := c.svc.DeleteConfig(token); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"msg":     err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"success": true,
		"msg":     "已删除",
	})
}

// handleRegenerate handles POST /panel/api/clash-link/:token/regenerate
func (c *Controller) handleRegenerate(ctx *gin.Context) {
	token := ctx.Param("token")
	host := ctx.Request.Host

	resp, err := c.svc.Regenerate(token, host)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"msg":     err.Error(),
		})
		return
	}

	scheme := "http"
	if ctx.Request.TLS != nil {
		scheme = "https"
	}
	fullURL := fmt.Sprintf("%s://%s/d/%s", scheme, host, resp.Token)

	ctx.JSON(http.StatusOK, gin.H{
		"success":   true,
		"token":     resp.Token,
		"full_url":  fullURL,
		"proxy_num": resp.ProxyNum,
	})
}

// handleBackup handles GET /panel/api/clash-link/backup
func (c *Controller) handleBackup(ctx *gin.Context) {
	data, err := c.svc.FullBackup()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"msg":     err.Error(),
		})
		return
	}

	ctx.Header("Content-Type", "application/json")
	ctx.Header("Content-Disposition", "attachment; filename=3x-ui-backup.json")
	ctx.Data(http.StatusOK, "application/json", data)
}

// handleServeYAML handles GET /d/:token (public, no auth)
func (c *Controller) handleServeYAML(ctx *gin.Context) {
	token := ctx.Param("token")

	yamlStr, configName, err := c.svc.GetYAML(token)
	if err != nil {
		ctx.String(http.StatusNotFound, "Config not found or expired")
		return
	}

	// Set headers for Clash subscription compatibility
	ctx.Header("Content-Type", "text/yaml; charset=utf-8")
	ctx.Header("Subscription-Userinfo", "upload=0; download=0; total=0; expire=0")
	ctx.Header("Profile-Update-Interval", "24")
	ctx.Header("Profile-Title", url.QueryEscape(configName))

	// Set Content-Disposition for download filename
	encodedName := url.QueryEscape(configName)
	ctx.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s.yaml\"; filename*=UTF-8''%s", configName, encodedName))

	ctx.String(http.StatusOK, yamlStr)
}

func parsePort(s string) int {
	if s == "" {
		return 0
	}
	port, err := strconv.Atoi(s)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}
