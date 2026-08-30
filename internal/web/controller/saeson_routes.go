package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/firewall"
	"github.com/mhsanaei/3x-ui/v3/internal/netstats"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficlog"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
	"gorm.io/gorm"
)

// requireLogin rejects requests without a valid panel session.
// Firewall + traffic stats are sensitive, so unlike clash-link they are
// strictly authenticated.
func requireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !session.IsLogin(c) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"msg":     "unauthorized",
				"obj":     nil,
			})
			return
		}
		c.Next()
	}
}

// RegisterSaesonRoutes registers the saeson mod routes (netstats + firewall)
// on the authenticated API group. Called from web.go after NewAPIController.
func RegisterSaesonRoutes(apiGroup *gin.RouterGroup, db *gorm.DB) {
	g := apiGroup.Group("", requireLogin())

	// --- Historical traffic sampler (per inbound / per client, bucketed) ---
	trafficlog.Start(db)

	// --- Traffic breakdown: NIC vs Xray vs system ---
	g.GET("/netstats", func(c *gin.Context) {
		stats, err := netstats.GetStats(db)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"msg":     "failed to collect net stats: " + err.Error(),
				"obj":     nil,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": stats})
	})

	// --- Firewall management ---
	fw := g.Group("/firewall")

	fw.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": firewall.GetStatus(db)})
	})

	fw.POST("/enable", func(c *gin.Context) {
		var req struct {
			ExtraPorts []int `json:"extraPorts"`
		}
		_ = c.ShouldBindJSON(&req)
		st, err := firewall.Enable(db, req.ExtraPorts)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "防火墙已开启，仅放行 SSH / 面板 / 节点 / 自定义端口", "obj": st})
	})

	fw.POST("/disable", func(c *gin.Context) {
		st, err := firewall.Disable()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "防火墙已关闭", "obj": st})
	})

	fw.POST("/sync", func(c *gin.Context) {
		st, err := firewall.Sync(db)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "端口列表已同步", "obj": st})
	})

	fw.POST("/ports", func(c *gin.Context) {
		// 前端 axios 全局默认 Content-Type 是 x-www-form-urlencoded（见上游
		// axios-init.ts），未显式声明 JSON 的 POST 会被 qs.stringify 编码成
		// "port=8284&action=add"。这里同时接受 JSON 与表单两种编码，双保险。
		body, _ := io.ReadAll(c.Request.Body)
		var (
			portStr string
			action  string
		)
		var req struct {
			Port   json.Number `json:"port"`
			Action string      `json:"action"` // "add" | "remove"
		}
		if err := json.Unmarshal(body, &req); err == nil && req.Port != "" {
			portStr = req.Port.String()
			action = req.Action
		} else if vals, err := url.ParseQuery(string(body)); err == nil {
			portStr = vals.Get("port")
			action = vals.Get("action")
		}
		p, err := strconv.ParseInt(strings.TrimSpace(portStr), 10, 64)
		if err != nil || p <= 0 || p > 65535 {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid port", "obj": nil})
			return
		}
		var st *firewall.Status
		if action == "remove" {
			st, err = firewall.RemoveExtraPort(db, int(p))
		} else {
			st, err = firewall.AddExtraPort(db, int(p))
		}
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "ok", "obj": st})
	})

	// --- Historical traffic: per inbound (node) / per client (user) ---
	tg := g.Group("/traffic")
	tg.GET("/targets", func(c *gin.Context) {
		t, err := trafficlog.GetTargets(db)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": t})
	})

	tg.GET("/history", func(c *gin.Context) {
		scope := c.Query("scope")
		ref := c.Query("ref")
		rng := c.Query("range")
		if scope != "inbound" && scope != "client" {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid scope (want inbound|client)", "obj": nil})
			return
		}
		if ref == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "missing ref", "obj": nil})
			return
		}
		var span, bk int
		switch rng {
		case "24h":
			span, bk = 24, 1
		case "7d":
			span, bk = 168, 6
		case "1mo":
			span, bk = 720, 24
		case "1yr":
			span, bk = 8760, 720
		default:
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid range (want 24h|7d|1mo|1yr)", "obj": nil})
			return
		}
		s, err := trafficlog.Query(db, scope, ref, span, bk)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": s})
	})
}
