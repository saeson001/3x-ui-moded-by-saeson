// Package trafficlog records time-bucketed traffic deltas for every inbound
// (node) and every client (user) so the panel can show historical usage over
// 24h / 7d / 1mo / 1yr.
//
// Upstream 3x-ui only keeps *cumulative* counters (inbounds.up/down and the
// per-client up/down inside inbounds.settings). To get a history we sample
// those counters on a fixed interval, store the per-interval delta into an
// hourly bucket, and aggregate buckets on query.
//
// Counters reset to 0 whenever Xray restarts (StatsService is in-memory), so a
// negative delta is clamped to 0 — a restart simply shows a quiet window
// instead of a bogus negative spike. The in-memory baseline is re-established
// on first sighting after a panel restart, so the first tick after a restart
// writes 0 rather than the whole cumulative total.
package trafficlog

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

// TrafficLog is one hourly bucket for a (scope, ref) pair.
type TrafficLog struct {
	Id         uint   `gorm:"primaryKey;autoIncrement"`
	Scope      string `gorm:"size:16;uniqueIndex:idx_sl_uniq;not null"` // "inbound" | "client"
	Ref        string `gorm:"size:160;uniqueIndex:idx_sl_uniq;not null"` // inbound id or email
	BucketStart int64 `gorm:"uniqueIndex:idx_sl_uniq;not null"`         // unix seconds, aligned to hour
	Up         int64  `gorm:"not null;default:0"`
	Down       int64  `gorm:"not null;default:0"`
}

func (TrafficLog) TableName() string { return "saeson_traffic_log" }

type clientCfg struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

type inboundSettings struct {
	Clients []clientCfg `json:"clients"`
}

type inboundRow struct {
	Id       int64  `gorm:"column:id"`
	Remark   string `gorm:"column:remark"`
	Up       int64  `gorm:"column:up"`
	Down     int64  `gorm:"column:down"`
	Settings string `gorm:"column:settings"`
}

var (
	mu          sync.Mutex
	baselineUp  = map[string]int64{}
	baselineDn  = map[string]int64{}
	started     bool
	startOnce   sync.Once
)

const sampleInterval = 5 * time.Minute

// Start migrates the table and launches the background sampler. Safe to call
// once at boot (it no-ops if already running).
func Start(db *gorm.DB) {
	if db == nil {
		return
	}
	if err := db.AutoMigrate(&TrafficLog{}); err != nil {
		log.Println("[trafficlog] automigrate failed:", err)
		return
	}
	startOnce.Do(func() {
		go func() {
			// prime baseline shortly after boot, then tick
			time.Sleep(3 * time.Second)
			sample(db)
			ticker := time.NewTicker(sampleInterval)
			defer ticker.Stop()
			for range ticker.C {
				sample(db)
			}
		}()
		log.Println("[trafficlog] sampler started (interval =", sampleInterval, ")")
	})
}

// diff returns the delta since the last sample (clamped to >=0) and records the
// new cumulative as the baseline for next time. First sighting returns 0.
func diff(scope, ref string, curUp, curDn int64) (int64, int64) {
	mu.Lock()
	defer mu.Unlock()
	key := scope + "|" + ref
	bu, okU := baselineUp[key]
	bd, okD := baselineDn[key]
	baselineUp[key] = curUp
	baselineDn[key] = curDn
	if !okU || !okD {
		return 0, 0
	}
	u := curUp - bu
	d := curDn - bd
	if u < 0 {
		u = 0
	}
	if d < 0 {
		d = 0
	}
	return u, d
}

func sample(db *gorm.DB) {
	var rows []inboundRow
	if err := db.Table("inbounds").Select("id, remark, up, down, settings").Find(&rows).Error; err != nil {
		log.Println("[trafficlog] sample query failed:", err)
		return
	}
	bucket := (time.Now().Unix() / 3600) * 3600

	type pair struct{ up, down int64 }
	deltas := map[string]pair{}
	for _, r := range rows {
		iu, id := diff("inbound", fmt.Sprintf("%d", r.Id), r.Up, r.Down)
		deltas["inbound|"+fmt.Sprintf("%d", r.Id)] = pair{iu, id}
		var s inboundSettings
		if err := json.Unmarshal([]byte(r.Settings), &s); err == nil {
			for _, c := range s.Clients {
				if c.Email == "" {
					continue
				}
				cu, cd := diff("client", c.Email, c.Up, c.Down)
				deltas["client|"+c.Email] = pair{cu, cd}
			}
		}
	}
	for k, v := range deltas {
		scope, ref, _ := splitKey(k)
		upsert(db, scope, ref, bucket, v.up, v.down)
	}
}

func splitKey(k string) (scope, ref string, rest string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '|' {
			return k[:i], k[i+1:], k[i+1:]
		}
	}
	return k, "", ""
}

func upsert(db *gorm.DB, scope, ref string, bucket, up, down int64) {
	if up == 0 && down == 0 {
		return
	}
	err := db.Exec(
		`INSERT INTO saeson_traffic_log (scope, ref, bucket_start, up, down)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(scope, ref, bucket_start) DO UPDATE SET up = up + excluded.up, down = down + excluded.down`,
		scope, ref, bucket, up, down,
	).Error
	if err != nil {
		log.Println("[trafficlog] upsert failed:", err)
	}
}

// Series is the aggregated result for a (scope, ref) over a time span.
type Series struct {
	Labels     []string `json:"labels"`
	Up         []int64  `json:"up"`
	Down       []int64  `json:"down"`
	TotalUp    int64    `json:"totalUp"`
	TotalDown  int64    `json:"totalDown"`
}

// Query aggregates hourly buckets into `bucketHours` windows covering
// `spanHours`, returning one Series.
func Query(db *gorm.DB, scope, ref string, spanHours, bucketHours int) (*Series, error) {
	now := time.Now().Unix()
	start := now - int64(spanHours)*3600
	bucketSec := int64(bucketHours) * 3600
	startAligned := (start / bucketSec) * bucketSec

	n := int((float64(spanHours)/float64(bucketHours)) + 0.999)
	if n < 1 {
		n = 1
	}

	var logs []TrafficLog
	if err := db.Where("scope = ? AND ref = ? AND bucket_start >= ?",
		scope, ref, startAligned).Find(&logs).Error; err != nil {
		return nil, err
	}

	up := make([]int64, n)
	down := make([]int64, n)
	labels := make([]string, n)
	for i := 0; i < n; i++ {
		bs := startAligned + int64(i)*bucketSec
		labels[i] = formatLabel(bs, bucketHours)
	}
	for _, l := range logs {
		idx := (l.BucketStart - startAligned) / bucketSec
		if idx < 0 || int(idx) >= n {
			continue
		}
		up[idx] += l.Up
		down[idx] += l.Down
	}
	var tUp, tDn int64
	for _, l := range logs {
		tUp += l.Up
		tDn += l.Down
	}
	return &Series{Labels: labels, Up: up, Down: down, TotalUp: tUp, TotalDown: tDn}, nil
}

func formatLabel(bucketStart int64, bucketHours int) string {
	t := time.Unix(bucketStart, 0)
	switch {
	case bucketHours <= 1:
		return t.Format("15:00")
	case bucketHours <= 24:
		return t.Format("01-02")
	default:
		return t.Format("2006-01")
	}
}

// Targets lists every inbound (node) and every unique client (user) so the
// frontend can build the scope/ref selectors.
type InboundTarget struct {
	Id     int64  `json:"id"`
	Remark string `json:"remark"`
}
type ClientTarget struct {
	Email string `json:"email"`
}
type Targets struct {
	Inbounds []InboundTarget `json:"inbounds"`
	Clients  []ClientTarget  `json:"clients"`
}

func GetTargets(db *gorm.DB) (*Targets, error) {
	var rows []inboundRow
	if err := db.Table("inbounds").Select("id, remark, settings").Find(&rows).Error; err != nil {
		return nil, err
	}
	t := &Targets{}
	seen := map[string]bool{}
	for _, r := range rows {
		t.Inbounds = append(t.Inbounds, InboundTarget{Id: r.Id, Remark: r.Remark})
		var s inboundSettings
		if json.Unmarshal([]byte(r.Settings), &s) == nil {
			for _, c := range s.Clients {
				if c.Email != "" && !seen[c.Email] {
					seen[c.Email] = true
					t.Clients = append(t.Clients, ClientTarget{Email: c.Email})
				}
			}
		}
	}
	return t, nil
}
