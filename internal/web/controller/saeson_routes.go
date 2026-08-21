package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/firewall"
	"github.com/mhsanaei/3x-ui/v3/internal/netstats"
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
		var req struct {
			Port   int    `json:"port"`
			Action string `json:"action"` // "add" | "remove"
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Port <= 0 {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid request", "obj": nil})
			return
		}
		var st *firewall.Status
		var err error
		if req.Action == "remove" {
			st, err = firewall.RemoveExtraPort(db, req.Port)
		} else {
			st, err = firewall.AddExtraPort(db, req.Port)
		}
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "ok", "obj": st})
	})
}
