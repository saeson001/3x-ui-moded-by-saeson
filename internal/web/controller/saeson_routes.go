package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/clientreset"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/firewall"
	"github.com/mhsanaei/3x-ui/v3/internal/inboundassoc"
	"github.com/mhsanaei/3x-ui/v3/internal/netstats"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficlog"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficreset"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
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

	// ---- Client sync: export / import / sorted-by-email ----
	// Enables cross-VPS client migration and consistent client ordering
	// across multiple 3x-ui instances (sort by email, not by DB id).
	//
	// Field names follow the upstream v3.4.2 model exactly: model.ClientRecord
	// carries Id/Email/UUID/Flow/Enable/SubID/ExpiryTime/TotalGB directly
	// (no "Total" — the column is TotalGB). model.Inbound's human-readable
	// label is Remark, NOT Name — there is no Name field on Inbound.
	// ClientRecord.ToClient() maps UUID → Client.ID, so both spellings compile;
	// we read the record fields directly to avoid a needless allocation.

	// clientInboundNames loads every client's inbound name list (email → []name).
	// Shared by /clients/export and /clients/sorted.
	loadClientInboundNames := func(db *gorm.DB) map[string][]string {
		names := map[string][]string{}
		var mappings []model.ClientInbound
		if err := db.Find(&mappings).Error; err != nil {
			return names
		}
		var inbounds []model.Inbound
		if err := db.Find(&inbounds).Error; err != nil {
			return names
		}
		ibNameByID := make(map[int]string, len(inbounds))
		for _, ib := range inbounds {
			ibNameByID[ib.Id] = ib.Remark
		}
		var records []model.ClientRecord
		if err := db.Select("id", "email").Find(&records).Error; err != nil {
			return names
		}
		idByEmail := make(map[int]string, len(records))
		for _, r := range records {
			idByEmail[r.Id] = r.Email
		}
		for _, m := range mappings {
			email, ok := idByEmail[m.ClientId]
			if !ok {
				continue
			}
			if name, ok := ibNameByID[m.InboundId]; ok {
				names[email] = append(names[email], name)
			}
		}
		// Emit [] rather than null for clients with no matching inbound.
		for _, r := range records {
			if names[r.Email] == nil {
				names[r.Email] = []string{}
			}
		}
		return names
	}

	// clientSyncEntry is the wire shape shared by /clients/export,
	// /clients/import and /clients/diff. Export emits "id"; import/diff accept
	// either "id" or "uuid" so a hand-trimmed file still works.
	type clientSyncEntry struct {
		Email        string   `json:"email"`
		ID           string   `json:"id"`
		UUID         string   `json:"uuid"`
		Flow         string   `json:"flow"`
		Total        int64    `json:"total"`
		ExpiryTime   int64    `json:"expiryTime"`
		Enable       bool     `json:"enable"`
		InboundNames []string `json:"inboundNames"`
	}

	// clientSyncID returns the UUID carried by an entry, tolerating either key.
	clientSyncID := func(e clientSyncEntry) string {
		if e.ID != "" {
			return e.ID
		}
		return e.UUID
	}

	// unmarshalClientSyncList accepts either {clients:[...]} (this export
	// format) or a bare [...] so a hand-trimmed file still works.
	unmarshalClientSyncList := func(body []byte) ([]clientSyncEntry, error) {
		wrapped := struct {
			Clients []clientSyncEntry `json:"clients"`
		}{}
		if err := json.Unmarshal(body, &wrapped); err == nil {
			return wrapped.Clients, nil
		}
		var bare []clientSyncEntry
		if err := json.Unmarshal(body, &bare); err != nil {
			return nil, err
		}
		return bare, nil
	}

	// GET /clients/export — export all clients sorted by email, with the
	// inbound names each one is attached to. The resulting JSON can be
	// imported into another 3x-ui instance via POST /clients/import.
	g.GET("/clients/export", func(c *gin.Context) {
		var records []model.ClientRecord
		if err := db.Order("email ASC").Find(&records).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		namesByEmail := loadClientInboundNames(db)

		type ExportClient struct {
			Email        string   `json:"email"`
			ID           string   `json:"id,omitempty"`
			Flow         string   `json:"flow,omitempty"`
			Total        int64    `json:"total"`
			ExpiryTime   int64    `json:"expiryTime"`
			Enable       bool     `json:"enable"`
			InboundNames []string `json:"inboundNames"`
		}
		clients := make([]ExportClient, 0, len(records))
		for i := range records {
			r := &records[i]
			clients = append(clients, ExportClient{
				Email:        r.Email,
				ID:           r.UUID,
				Flow:         r.Flow,
				Total:        r.TotalGB,
				ExpiryTime:   r.ExpiryTime,
				Enable:       r.Enable,
				InboundNames: namesByEmail[r.Email],
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"msg":     "",
			"obj": gin.H{
				"version":     1,
				"exportedAt":  time.Now().Unix(),
				"clientCount": len(clients),
				"clients":     clients,
			},
		})
	})

	// POST /clients/import — import clients from an exported JSON.
	// Existing clients (matched by email) are skipped; new ones are created
	// via the upstream BulkCreate and attached to inbounds matched by name.
	// Accepts both {clients:[...]} (this export format) and a bare [...] so a
	// hand-trimmed file still works.
	g.POST("/clients/import", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		raw, err := unmarshalClientSyncList(body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid JSON: " + err.Error(), "obj": nil})
			return
		}

		// Remark → id for inbounds on this instance. Inbound has no Name field.
		var inbounds []model.Inbound
		db.Find(&inbounds)
		ibIDByName := make(map[string]int, len(inbounds))
		for _, ib := range inbounds {
			ibIDByName[ib.Remark] = ib.Id
		}

		// Existing emails in one query (not one round-trip per client).
		emailList := make([]string, 0, len(raw))
		for _, cli := range raw {
			if e := strings.TrimSpace(cli.Email); e != "" {
				emailList = append(emailList, e)
			}
		}
		exists := make(map[string]bool, len(emailList))
		if len(emailList) > 0 {
			var existing []model.ClientRecord
			db.Where("email IN ?", emailList).Pluck("email", &existing)
			for _, e := range existing {
				exists[e.Email] = true
			}
		}

		// Build payloads for BulkCreate, skipping existing clients and those
		// whose inbound names don't resolve on this instance (a client needs
		// at least one inbound or BulkCreate refuses it).
		var payloads []service.ClientCreatePayload
		var skipped, noInbound, existingSkipped int
		for _, cli := range raw {
			email := strings.TrimSpace(cli.Email)
			if email == "" {
				skipped++
				continue
			}
			if exists[email] {
				skipped++
				existingSkipped++
				continue
			}
			var ibIDs []int
			for _, name := range cli.InboundNames {
				if id, ok := ibIDByName[name]; ok {
					ibIDs = append(ibIDs, id)
				}
			}
			if len(ibIDs) == 0 {
				skipped++
				noInbound++
				continue
			}
			payloads = append(payloads, service.ClientCreatePayload{
				Client: model.Client{
					// Empty UUID → BulkCreate/fillProtocolDefaults generates one.
					Email:      email,
					ID:         clientSyncID(cli),
					Flow:       cli.Flow,
					TotalGB:    cli.Total,
					ExpiryTime: cli.ExpiryTime,
					Enable:     true, // BulkCreate forces Enable=true anyway
				},
				InboundIds: ibIDs,
			})
		}

		var created int
		if len(payloads) > 0 {
			result, _, err := api.inboundController.clientService.BulkCreate(
				&api.inboundController.inboundService, payloads)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"success": false, "msg": "bulk create failed: " + err.Error(), "obj": nil})
				return
			}
			created = result.Created
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"msg":     "导入完成",
			"obj": gin.H{
				"created":   created,
				"skipped":   skipped,
				"existing":  existingSkipped,
				"noInbound": noInbound,
			},
		})
	})

	// GET /client-sync — standalone client-sync UI page.
	g.GET("/client-sync", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(clientSyncHTML))
	})

	// GET /clients/sorted — every client sorted by email, with inbound names.
	// Lets two instances be compared side by side in the same order.
	g.GET("/clients/sorted", func(c *gin.Context) {
		var records []model.ClientRecord
		if err := db.Order("email ASC").Find(&records).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		namesByEmail := loadClientInboundNames(db)

		type SortedClient struct {
			Email        string   `json:"email"`
			ID           int      `json:"id"`
			UUID         string   `json:"uuid"`
			Enable       bool     `json:"enable"`
			InboundNames []string `json:"inboundNames"`
		}
		clients := make([]SortedClient, 0, len(records))
		for i := range records {
			r := &records[i]
			clients = append(clients, SortedClient{
				Email:        r.Email,
				ID:           r.Id,
				UUID:         r.UUID,
				Enable:       r.Enable,
				InboundNames: namesByEmail[r.Email],
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": clients})
	})

	// POST /clients/diff — compare this instance against another instance's
	// export JSON (same shape as /clients/export, or a bare array). Returns the
	// three-way split so two panels can be reconciled without deleting anything:
	//   onlyLocal  clients here that the other side lacks
	//   onlyRemote clients on the other side that can be imported here
	//   both       present on both, with uuidMatch flagging identity drift
	// uuidMatch=false means the same email is a DIFFERENT credential on the two
	// sides (import would skip it as existing), so the client's actual link
	// differs — worth surfacing before anyone "fixes" it blindly.
	g.POST("/clients/diff", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		raw, err := unmarshalClientSyncList(body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": "invalid JSON: " + err.Error(), "obj": nil})
			return
		}

		// Order local records by id ASC — this is the panel's default listing
		// order (client_link.go:186 hardcodes ORDER BY clients.id ASC). The
		// user's "对齐" requirement means "left side order wins"; using email
		// order here would silently reorder the base side.
		var records []model.ClientRecord
		if err := db.Order("id ASC").Find(&records).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error(), "obj": nil})
			return
		}
		namesByEmail := loadClientInboundNames(db)

		local := make(map[string]*model.ClientRecord, len(records))
		for i := range records {
			local[records[i].Email] = &records[i]
		}

		// Remark → id for inbounds on this instance.
		var inbounds []model.Inbound
		db.Find(&inbounds)
		ibKnown := make(map[string]bool, len(inbounds))
		for _, ib := range inbounds {
			ibKnown[ib.Remark] = true
		}

		type diffOnly struct {
			Email        string   `json:"email"`
			UUID         string   `json:"uuid"`
			Enable       bool     `json:"enable"`
			InboundNames []string `json:"inboundNames"`
		}
		type diffBoth struct {
			Email               string   `json:"email"`
			LocalUUID           string   `json:"localUuid"`
			RemoteUUID          string   `json:"remoteUuid"`
			UUIDMatch           bool     `json:"uuidMatch"`
			LocalEnable         bool     `json:"localEnable"`
			RemoteEnable        bool     `json:"remoteEnable"`
			EnableMatch         bool     `json:"enableMatch"`
			InboundNamesLocal   []string `json:"inboundNamesLocal"`
			InboundNamesRemote  []string `json:"inboundNamesRemote"`
			MissingInboundNames []string `json:"missingInboundNames"`
		}

		onlyLocal := []diffOnly{}
		onlyRemote := []diffOnly{}
		both := []diffBoth{}
		missingByName := map[string]bool{}

		// Pass 1: walk the remote export (preserving its own panel order) and
		// bucket entries into remoteByEmail (matched) or onlyRemote (unmatched).
		remoteByEmail := make(map[string]clientSyncEntry, len(raw))
		for _, cli := range raw {
			email := strings.TrimSpace(cli.Email)
			if email == "" {
				continue
			}
			remoteByEmail[email] = cli
			for _, name := range cli.InboundNames {
				if !ibKnown[name] {
					missingByName[name] = true
				}
			}
		}

		// Pass 2: walk local records in DB order. For each one either append
		// to `both` (if the remote side has the same email) or to `onlyLocal`.
		// This is what gives the "left side order wins" property the user asked
		// for — both[] and onlyLocal[] both inherit the local panel ordering.
		seenLocal := map[string]bool{}
		for i := range records {
			r := &records[i]
			cli, ok := remoteByEmail[r.Email]
			seenLocal[r.Email] = true
			if !ok {
				onlyLocal = append(onlyLocal, diffOnly{
					Email:        r.Email,
					UUID:         r.UUID,
					Enable:       r.Enable,
					InboundNames: namesByEmail[r.Email],
				})
				continue
			}
			remoteUUID := clientSyncID(cli)
			var missing []string
			for _, name := range cli.InboundNames {
				if !ibKnown[name] {
					missing = append(missing, name)
				}
			}
			both = append(both, diffBoth{
				Email:               r.Email,
				LocalUUID:           r.UUID,
				RemoteUUID:          remoteUUID,
				UUIDMatch:           r.UUID == remoteUUID,
				LocalEnable:         r.Enable,
				RemoteEnable:        cli.Enable,
				EnableMatch:         r.Enable == cli.Enable,
				InboundNamesLocal:   namesByEmail[r.Email],
				InboundNamesRemote:  cli.InboundNames,
				MissingInboundNames: missing,
			})
		}

		// Pass 3: any remote entry not seen on the local side goes to onlyRemote,
		// in the remote panel's original order (the order the file was exported in).
		for _, cli := range raw {
			email := strings.TrimSpace(cli.Email)
			if email == "" || seenLocal[email] {
				continue
			}
			onlyRemote = append(onlyRemote, diffOnly{
				Email:        email,
				UUID:         clientSyncID(cli),
				Enable:       cli.Enable,
				InboundNames: cli.InboundNames,
			})
		}

		missing := []string{}
		for name := range missingByName {
			missing = append(missing, name)
		}
		sort.Strings(missing)

		uuidMismatches := 0
		for _, b := range both {
			if !b.UUIDMatch {
				uuidMismatches++
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"msg":     "",
			"obj": gin.H{
				"onlyLocal":           onlyLocal,
				"onlyRemote":          onlyRemote,
				"both":                both,
				"missingInboundNames": missing,
				"uuidMismatches":      uuidMismatches,
				"localCount":          len(records),
				"remoteCount":         len(raw),
			},
		})
	})
}
