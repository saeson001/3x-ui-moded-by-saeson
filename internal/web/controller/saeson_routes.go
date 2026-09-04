package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/clientreset"
	"github.com/mhsanaei/3x-ui/v3/internal/firewall"
	"github.com/mhsanaei/3x-ui/v3/internal/inboundassoc"
	"github.com/mhsanaei/3x-ui/v3/internal/netstats"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficlog"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficreset"
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

// RegisterSaesonRoutes registers the saeson mod routes (netstats + firewall
// + client traffic scheduled reset) on the authenticated API group. Called
// from web.go after NewAPIController. The *APIController is passed so we can
// reach its private inboundController and reuse the upstream ClientService /
// InboundService for the actual traffic reset (clears DB + Xray in-memory).
func RegisterSaesonRoutes(apiGroup *gin.RouterGroup, db *gorm.DB, api *APIController) {
	g := apiGroup.Group("", requireLogin())

	// --- Historical traffic sampler (per inbound / per client, bucketed) ---
	trafficlog.Start(db)

	// --- Per-client scheduled traffic reset (calendar day) ---
	clientreset.Start(db, api.inboundController.clientService, api.inboundController.inboundService)

	// --- Batch set inbound association (replace) for clients ---
	inboundassoc.Start(api.inboundController.clientService, api.inboundController.inboundService)

	// --- Global traffic counter reset (manual + scheduled calendar day) ---
	trafficreset.Start(db, api.inboundController.clientService, api.inboundController.inboundService)

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

	// --- Per-client scheduled traffic reset (calendar day) ---
	cg := g.Group("/client-reset")
	cg.GET("/list", func(c *gin.Context) {
		rows, err := clientreset.List(db)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": rows})
	})
	cg.POST("/set", func(c *gin.Context) {
		// Accept both JSON and the upstream form-urlencoded default so the
		// client page can post either way.
		body, _ := io.ReadAll(c.Request.Body)
		req := struct {
			Email       string `json:"email"`
			ResetDay    int    `json:"resetDay"`
			ResetHour   int    `json:"resetHour"`
			ResetMinute int    `json:"resetMinute"`
			Enable      bool   `json:"enable"`
		}{}
		if err := json.Unmarshal(body, &req); err != nil {
			if vals, e2 := url.ParseQuery(string(body)); e2 == nil {
				req.Email = vals.Get("email")
				req.ResetDay, _ = strconv.Atoi(vals.Get("resetDay"))
				req.ResetHour, _ = strconv.Atoi(vals.Get("resetHour"))
				req.ResetMinute, _ = strconv.Atoi(vals.Get("resetMinute"))
				req.Enable = vals.Get("enable") == "true" || vals.Get("enable") == "1"
			}
		}
		if req.Email == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "email required", "obj": nil})
			return
		}
		if err := clientreset.Set(db, req.Email, req.ResetDay, req.ResetHour, req.ResetMinute, req.Enable); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已保存定时重置规则", "obj": nil})
	})
	cg.POST("/remove", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if b, _ := io.ReadAll(c.Request.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &req); err != nil {
				if v, e2 := url.ParseQuery(string(b)); e2 == nil {
					req.Email = v.Get("email")
				}
			}
		}
		if req.Email == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "email required", "obj": nil})
			return
		}
		if err := clientreset.Remove(db, req.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已删除定时重置规则", "obj": nil})
	})
	cg.POST("/reset-now", func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		if b, _ := io.ReadAll(c.Request.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &req); err != nil {
				if v, e2 := url.ParseQuery(string(b)); e2 == nil {
					req.Email = v.Get("email")
				}
			}
		}
		if req.Email == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "email required", "obj": nil})
			return
		}
		if err := clientreset.ResetNow(db, req.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已立即重置流量", "obj": nil})
	})

	// --- Batch set inbound association (replace): move selected clients onto
	//     exactly the chosen inbounds, detaching any others they were on. ---
	ig := g.Group("/client-inbounds")
	ig.POST("/bulk-set", func(c *gin.Context) {
		var req struct {
			Emails     []string `json:"emails"`
			InboundIds []int    `json:"inboundIds"`
		}
		if b, _ := io.ReadAll(c.Request.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &req); err != nil {
				c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid body", "obj": nil})
				return
			}
		}
		if len(req.Emails) == 0 || len(req.InboundIds) == 0 {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "emails 与 inboundIds 均不能为空", "obj": nil})
			return
		}
		changed, _, err := inboundassoc.Set(db, req.Emails, req.InboundIds)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已设置关联入站", "obj": gin.H{"changed": changed}})
	})

	// --- Global traffic counter reset (manual button + scheduled calendar day) ---
	trg := g.Group("/traffic-reset")
	trg.GET("/status", func(c *gin.Context) {
		row, err := trafficreset.Status(db)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": row})
	})
	trg.POST("/set", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		req := struct {
			ResetDay    int  `json:"resetDay"`
			ResetHour   int  `json:"resetHour"`
			ResetMinute int  `json:"resetMinute"`
			Enable      bool `json:"enable"`
		}{}
		if err := json.Unmarshal(body, &req); err != nil {
			if vals, e2 := url.ParseQuery(string(body)); e2 == nil {
				req.ResetDay, _ = strconv.Atoi(vals.Get("resetDay"))
				req.ResetHour, _ = strconv.Atoi(vals.Get("resetHour"))
				req.ResetMinute, _ = strconv.Atoi(vals.Get("resetMinute"))
				req.Enable = vals.Get("enable") == "true" || vals.Get("enable") == "1"
			}
		}
		if err := trafficreset.Set(db, req.ResetDay, req.ResetHour, req.ResetMinute, req.Enable); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已保存定时重置规则", "obj": nil})
	})
	trg.POST("/reset-now", func(c *gin.Context) {
		if err := trafficreset.ResetNow(db); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "已立即重置全部流量计数", "obj": nil})
	})

	// --- Traffic calibration: rewrite the currently displayed cumulative
	//     counters (upper/lower independently) without touching real traffic.
	//     The NIC counter is corrected via a baseline offset; the Xray card is
	//     rescaled proportionally when xrayUp / xrayDown are provided. ---
	trg.POST("/calibrate", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		req := struct {
			Sent     uint64  `json:"sent"`
			Recv     uint64  `json:"recv"`
			XrayUp   *uint64 `json:"xrayUp"`
			XrayDown *uint64 `json:"xrayDown"`
		}{}
		if err := json.Unmarshal(body, &req); err != nil {
			// Fall back to form-urlencoded (global axios default is form-encoded).
			if vals, e2 := url.ParseQuery(string(body)); e2 == nil {
				req.Sent, _ = strconv.ParseUint(vals.Get("sent"), 10, 64)
				req.Recv, _ = strconv.ParseUint(vals.Get("recv"), 10, 64)
				if v := vals.Get("xrayUp"); v != "" {
					if n, e3 := strconv.ParseUint(v, 10, 64); e3 == nil {
						req.XrayUp = &n
					}
				}
				if v := vals.Get("xrayDown"); v != "" {
					if n, e3 := strconv.ParseUint(v, 10, 64); e3 == nil {
						req.XrayDown = &n
					}
				}
			}
		}
		if err := trafficreset.Calibrate(db, req.Sent, req.Recv, req.XrayUp, req.XrayDown); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"msg":     "已校准流量计数，将从新数值继续累计",
			"obj":     gin.H{"sent": req.Sent, "recv": req.Recv},
		})
	})
}
