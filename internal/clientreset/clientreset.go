// Package clientreset implements per-client scheduled traffic reset on a
// fixed calendar schedule (e.g. "reset traffic on day 1 of every month at
// 03:00"), on top of the upstream 3x-ui v3.4.2 base.
//
// Upstream already supports a *rolling* reset via the client "renewDays"
// field (reset every N days from creation). This module adds a *fixed-date*
// reset ("定时重置") that upstream only gained in 3.7.0, so we implement it
// here to stay on the 3.4.2 base.
//
// The actual reset reuses the upstream ClientService.ResetTrafficByEmail,
// which zeroes the client_traffics row AND resets the Xray in-memory counter
// for that email (otherwise the next stats flush would silently restore the
// old cumulative total). It also auto-re-enables a disabled client.
package clientreset

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"gorm.io/gorm"
)

var errEmptyEmail = errors.New("client email is required")

// SaesonClientReset is one scheduled-reset rule per client email.
type SaesonClientReset struct {
	Id          uint   `gorm:"primaryKey;autoIncrement"`
	Email       string `gorm:"size:160;uniqueIndex;not null"`
	ResetDay    int    `gorm:"not null;default:1"`  // day of month 1..28 (clamped to month length)
	ResetHour   int    `gorm:"not null;default:0"`  // 0..23
	ResetMinute int    `gorm:"not null;default:0"`  // 0..59
	Enable      bool   `gorm:"not null;default:true"`
	LastReset   int64  `gorm:"not null;default:0"` // unix seconds of last reset
}

func (SaesonClientReset) TableName() string { return "saeson_client_reset" }

var (
	mu         sync.Mutex
	clientSvc  service.ClientService
	inboundSvc service.InboundService
	started    bool
	startOnce  sync.Once
)

// Start migrates the table, captures the upstream services and launches the
// per-minute checker. Safe to call once at boot.
func Start(db *gorm.DB, cs service.ClientService, is service.InboundService) {
	if db == nil {
		return
	}
	if err := db.AutoMigrate(&SaesonClientReset{}); err != nil {
		log.Println("[clientreset] automigrate failed:", err)
		return
	}
	mu.Lock()
	clientSvc = cs
	inboundSvc = is
	mu.Unlock()
	startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				run(db)
			}
		}()
		log.Println("[clientreset] scheduler started (interval = 1m)")
	})
}

func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func startOfToday(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

// clampedDay returns the effective reset day, clamped to the current month
// length so e.g. day 31 on February becomes the 28th (or 29th).
func clampedDay(t time.Time, want int) int {
	if want <= 0 {
		return 1
	}
	dim := daysInMonth(t)
	if want > dim {
		return dim
	}
	return want
}

// matches reports whether the schedule should fire now: same calendar day
// (clamped), and the current time-of-day is at or past the scheduled time, and
// it has not already fired today. This also catches up a missed minute if the
// panel was briefly down.
func matches(now time.Time, r SaesonClientReset) bool {
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
	var rows []SaesonClientReset
	if err := db.Where("enable = ?", true).Find(&rows).Error; err != nil {
		log.Println("[clientreset] query failed:", err)
		return
	}
	now := time.Now()
	for _, r := range rows {
		if !matches(now, r) {
			continue
		}
		if _, err := clientSvc.ResetTrafficByEmail(&inboundSvc, r.Email); err != nil {
			log.Println("[clientreset] reset failed for", r.Email, ":", err)
			continue
		}
		db.Model(&SaesonClientReset{}).Where("email = ?", r.Email).
			Update("last_reset", now.Unix())
		log.Println("[clientreset] reset traffic for", r.Email, "(scheduled", r.ResetDay, r.ResetHour, ":", r.ResetMinute, ")")
	}
}

// Set upserts a schedule for an email.
func Set(db *gorm.DB, email string, day, hour, minute int, enable bool) error {
	if email == "" {
		return errEmptyEmail
	}
	day, hour, minute = normalize(day, hour, minute)
	var existing SaesonClientReset
	err := db.Where("email = ?", email).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		row := SaesonClientReset{Email: email, ResetDay: day, ResetHour: hour, ResetMinute: minute, Enable: enable}
		return db.Create(&row).Error
	}
	if err != nil {
		return err
	}
	return db.Model(&existing).Updates(map[string]any{
		"reset_day":    day,
		"reset_hour":   hour,
		"reset_minute": minute,
		"enable":       enable,
	}).Error
}

// Remove deletes a schedule.
func Remove(db *gorm.DB, email string) error {
	if email == "" {
		return errEmptyEmail
	}
	return db.Where("email = ?", email).Delete(&SaesonClientReset{}).Error
}

// List returns all schedules.
func List(db *gorm.DB) ([]SaesonClientReset, error) {
	var rows []SaesonClientReset
	if err := db.Order("email asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ResetNow triggers an immediate reset for one client and records the time.
func ResetNow(db *gorm.DB, email string) error {
	if email == "" {
		return errEmptyEmail
	}
	if _, err := clientSvc.ResetTrafficByEmail(&inboundSvc, email); err != nil {
		return err
	}
	now := time.Now().Unix()
	db.Model(&SaesonClientReset{}).Where("email = ?", email).
		Update("last_reset", now)
	return nil
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
