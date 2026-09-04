// Package trafficreset implements a global, scheduled traffic-counter reset for
// the saeson 3x-ui fork dashboard.
//
// The dashboard shows two cumulative cards:
//   - "Xray 代理流量"  : sum of every inbound's up/down (the inbounds DB columns)
//   - "总数据"          : whole-NIC traffic (what the VPS provider bills)
//
// "Reset counters" should zero BOTH from the operator's point of view (e.g. a
// monthly billing-cycle reset). The inbounds columns can be zeroed directly and
// Xray's in-memory counters reset per client. The NIC total, however, is a kernel
// counter that cannot be zeroed — so we record a baseline at reset time and the
// netstats module reports NIC totals relative to that baseline, making the
// "总数据" card restart from zero after each reset.
package trafficreset

import (
	"log"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/netstats"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"gorm.io/gorm"
)

// SaesonTrafficReset is a single global row (id = 1) holding the schedule and
// the persisted NIC baseline.
type SaesonTrafficReset struct {
	Id          uint   `gorm:"primaryKey;autoIncrement"`
	Enable      bool   `gorm:"not null;default:true"`
	ResetDay    int    `gorm:"not null;default:1"`  // day of month 1..28 (clamped to month length)
	ResetHour   int    `gorm:"not null;default:0"`  // 0..23
	ResetMinute int    `gorm:"not null;default:0"`  // 0..59
	LastReset   int64  `gorm:"not null;default:0"`  // unix seconds
	NicBaseSent uint64 `gorm:"not null;default:0"`  // NIC baseline at last reset
	NicBaseRecv uint64 `gorm:"not null;default:0"`
}

func (SaesonTrafficReset) TableName() string { return "saeson_traffic_reset" }

var (
	mu         sync.Mutex
	clientSvc  service.ClientService
	inboundSvc service.InboundService
)

// Start migrates the table, captures the upstream services, loads the persisted
// NIC baseline into netstats, and launches the per-minute scheduler.
// Services are passed by value to match the upstream clientreset.Start
// convention (api.inboundController.clientService is a value, not a pointer).
// ClientService is an empty struct upstream, so copying is safe.
func Start(db *gorm.DB, cs service.ClientService, is service.InboundService) {
	if db == nil {
		return
	}
	if err := db.AutoMigrate(&SaesonTrafficReset{}); err != nil {
		log.Println("[trafficreset] automigrate failed:", err)
		return
	}
	mu.Lock()
	clientSvc = cs
	inboundSvc = is
	mu.Unlock()

	var row SaesonTrafficReset
	if err := db.First(&row).Error; err == nil {
		netstats.SetBaseline(row.NicBaseSent, row.NicBaseRecv)
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			run(db)
		}
	}()
	log.Println("[trafficreset] scheduler started (interval = 1m)")
}

func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func startOfToday(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

func clampedDay(t time.Time, want int) int {
	if want <= 0 {
		return 1
	}
	if want > daysInMonth(t) {
		return daysInMonth(t)
	}
	return want
}

func matches(now time.Time, r SaesonTrafficReset) bool {
	if !r.Enable {
		return false
	}
	if now.Day() != clampedDay(now, r.ResetDay) {
		return false
	}
	minuteOfDay := now.Hour()*60 + now.Minute()
	scheduled := r.ResetHour*60 + r.ResetMinute
	if minuteOfDay < scheduled {
		return false
	}
	if r.LastReset >= startOfToday(now) {
		return false
	}
	return true
}

func run(db *gorm.DB) {
	var row SaesonTrafficReset
	if err := db.First(&row).Error; err != nil {
		return
	}
	if !matches(time.Now(), row) {
		return
	}
	if err := ResetNow(db); err != nil {
		log.Println("[trafficreset] reset failed:", err)
		return
	}
	log.Println("[trafficreset] global traffic reset (scheduled)")
}

// ResetNow zeroes all cumulative traffic counters and records a NIC baseline so
// the "总数据" card restarts from zero. It reuses the upstream per-client reset
// (ClientService.ResetTrafficByEmail) which clears DB counters AND the Xray
// in-memory counter; inbounds columns are zeroed directly because the upstream
// reset does not touch them.
func ResetNow(db *gorm.DB) error {
	mu.Lock()
	cs := clientSvc
	is := inboundSvc
	mu.Unlock()

	// Zero the panel's per-inbound cumulative counters (what the "Xray 代理流量"
	// dashboard card sums). The per-client client_traffics rows and Xray's
	// in-memory counters are zeroed client-by-client by ResetTrafficByEmail below.
	if err := db.Exec("UPDATE inbounds SET up = 0, down = 0").Error; err != nil {
		return err
	}

	var emails []string
	if err := db.Table("clients").Pluck("email", &emails).Error; err != nil {
		return err
	}
	for _, e := range emails {
		if _, err := cs.ResetTrafficByEmail(&is, e); err != nil {
			log.Println("[trafficreset] reset client", e, ":", err)
		}
	}

	sent, recv, err := netstats.RawNIC()
	if err != nil {
		log.Println("[trafficreset] read NIC:", err)
	}
	netstats.SetBaseline(sent, recv)

	// Persist the baseline with a load-or-create upsert. The previous
	// implementation did `UPDATE ... WHERE id = 1`, which silently matched ZERO
	// rows when the saeson_traffic_reset table had no row yet (the row is only
	// created by saving a reset schedule). The baseline then lived in memory
	// only, and the next panel restart reloaded no baseline (=> 0), making the
	// "总数据" card jump back to the raw since-boot NIC totals (last month's
	// traffic "reappearing").
	now := time.Now().Unix()
	var row SaesonTrafficReset
	if err := db.First(&row).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
		row = SaesonTrafficReset{Enable: true, ResetDay: 1}
	}
	row.LastReset = now
	row.NicBaseSent = sent
	row.NicBaseRecv = recv
	return db.Save(&row).Error
}

// Calibrate rewrites the CURRENTLY DISPLAYED cumulative counters without
// touching any real counter. The kernel NIC counters cannot be rewritten, so
// the "总数据" card is corrected by moving the baseline:
//
//	displayed = raw - baseline   =>   newBaseline = raw - target
//
// After calibration the card keeps counting up from the corrected value, which
// is what operators need when the panel's number drifts away from the VPS
// provider's billing figure (e.g. panel says 200G, provider says 30G).
//
// Both directions (sent/recv) are handled independently.
//
// The "Xray 代理流量" card sums the inbounds up/down columns, which ARE regular
// DB counters. When xrayUp / xrayDown are non-nil they are applied by scaling
// every inbound proportionally, so the new total matches the target exactly
// while the relative distribution between inbounds is preserved. Passing nil
// leaves the Xray counters untouched.
func Calibrate(db *gorm.DB, targetSent, targetRecv uint64, xrayUp, xrayDown *uint64) error {
	sent, recv, err := netstats.RawNIC()
	if err != nil {
		return err
	}

	baseSent := clampSub(sent, targetSent)
	baseRecv := clampSub(recv, targetRecv)
	netstats.SetBaseline(baseSent, baseRecv)

	if xrayUp != nil || xrayDown != nil {
		if err := scaleInbounds(db, xrayUp, xrayDown); err != nil {
			return err
		}
	}

	// Persist with a load-or-create upsert (same pattern as ResetNow): a bare
	// UPDATE ... WHERE id = 1 silently matches 0 rows when the row does not
	// exist yet, which used to make the correction vanish on restart.
	var row SaesonTrafficReset
	if err := db.First(&row).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
		row = SaesonTrafficReset{Enable: true, ResetDay: 1}
	}
	row.NicBaseSent = baseSent
	row.NicBaseRecv = baseRecv
	return db.Save(&row).Error
}

// clampSub subtracts b from a and floors the result at zero, guarding against
// an underflow when the requested target exceeds the raw kernel counter (the
// baseline then simply ends up at 0).
func clampSub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}

// scaleInbounds rescales every inbound's up/down columns so their SUMS match
// the requested targets. Inbounds that are already zero stay zero; when the
// current total is zero the target is assigned to nothing (nothing to scale).
func scaleInbounds(db *gorm.DB, targetUp, targetDown *uint64) error {
	var t struct {
		Up   *int64
		Down *int64
	}
	if err := db.Raw("SELECT COALESCE(SUM(up),0) AS up, COALESCE(SUM(down),0) AS down FROM inbounds").Scan(&t).Error; err != nil {
		return err
	}
	var curUp, curDown int64
	if t.Up != nil {
		curUp = *t.Up
	}
	if t.Down != nil {
		curDown = *t.Down
	}

	if targetUp != nil && curUp > 0 {
		if err := db.Exec("UPDATE inbounds SET up = CAST(up * ? AS INTEGER)",
			float64(*targetUp)/float64(curUp)).Error; err != nil {
			return err
		}
	}
	if targetDown != nil && curDown > 0 {
		if err := db.Exec("UPDATE inbounds SET down = CAST(down * ? AS INTEGER)",
			float64(*targetDown)/float64(curDown)).Error; err != nil {
			return err
		}
	}
	return nil
}

// Status returns the current schedule (for the frontend).
func Status(db *gorm.DB) (*SaesonTrafficReset, error) {
	var row SaesonTrafficReset
	if err := db.First(&row).Error; err != nil {
		return &SaesonTrafficReset{ResetDay: 1}, nil
	}
	return &row, nil
}

// Set upserts the schedule.
func Set(db *gorm.DB, day, hour, minute int, enable bool) error {
	day, hour, minute = normalize(day, hour, minute)
	var row SaesonTrafficReset
	err := db.First(&row).Error
	if err == gorm.ErrRecordNotFound {
		r := SaesonTrafficReset{ResetDay: day, ResetHour: hour, ResetMinute: minute, Enable: enable}
		return db.Create(&r).Error
	}
	if err != nil {
		return err
	}
	return db.Model(&row).Updates(map[string]any{
		"reset_day":    day,
		"reset_hour":   hour,
		"reset_minute": minute,
		"enable":       enable,
	}).Error
}

func normalize(day, hour, minute int) (int, int, int) {
	if day < 1 {
		day = 1
	}
	if day > 31 {
		day = 31
	}
	if hour < 0 {
		hour = 0
	}
	if hour > 23 {
		hour = 23
	}
	if minute < 0 {
		minute = 0
	}
	if minute > 59 {
		minute = 59
	}
	return day, hour, minute
}
