// Package inboundassoc implements "batch set inbound association" (批量设置关联入站)
// for the saeson 3x-ui fork.
//
// Upstream 3x-ui v3.4.2 only offers BulkAttach (add a client to more inbounds)
// and BulkDetach (remove a client from inbounds). There is no "replace" — yet the
// common operational need is "these users were on the JP inbound, move them all to
// the HK inbound in one shot". This module provides exactly that: for each selected
// client it detaches every currently-associated inbound that is NOT in the target
// set, then attaches the client to every target inbound it is not already on.
package inboundassoc

import (
	"errors"
	"log"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"gorm.io/gorm"
)

var errEmpty = errors.New("emails and inboundIds are required")

var (
	clientSvc  *service.ClientService
	inboundSvc service.InboundService
)

// Start captures the upstream services. Safe to call once at boot.
func Start(cs *service.ClientService, is *service.InboundService) {
	clientSvc = cs
	if is != nil {
		inboundSvc = *is
	}
}

// Set replaces each client's associated inbounds with exactly inboundIds.
// It returns the number of clients processed and whether an Xray restart is
// needed (per the underlying BulkAttach/BulkDetach calls).
func Set(db *gorm.DB, emails []string, inboundIds []int) (int, bool, error) {
	clean := dedupe(emails)
	if len(clean) == 0 || len(inboundIds) == 0 {
		return 0, false, errEmpty
	}
	targetSet := make(map[int]struct{}, len(inboundIds))
	for _, id := range inboundIds {
		targetSet[id] = struct{}{}
	}

	// Collect every inbound each selected client is currently on.
	currentSet := make(map[int]struct{})
	for _, email := range clean {
		rec, err := clientSvc.GetRecordByEmail(nil, email)
		if err != nil {
			log.Println("[inboundassoc] GetRecordByEmail", email, ":", err)
			continue
		}
		ids, err := clientSvc.GetInboundIdsForRecord(rec.Id)
		if err != nil {
			log.Println("[inboundassoc] GetInboundIdsForRecord", email, ":", err)
			continue
		}
		for _, id := range ids {
			currentSet[id] = struct{}{}
		}
	}

	// Inbounds to detach = currently on but not in the target set.
	removeIds := make([]int, 0)
	for id := range currentSet {
		if _, ok := targetSet[id]; !ok {
			removeIds = append(removeIds, id)
		}
	}

	needRestart := false
	if len(removeIds) > 0 {
		if _, nr, err := clientSvc.BulkDetach(&inboundSvc, clean, removeIds); err != nil {
			log.Println("[inboundassoc] BulkDetach:", err)
		} else {
			needRestart = needRestart || nr
		}
	}
	if _, nr, err := clientSvc.BulkAttach(&inboundSvc, clean, inboundIds); err != nil {
		log.Println("[inboundassoc] BulkAttach:", err)
		return len(clean), needRestart, err
	} else {
		needRestart = needRestart || nr
	}

	return len(clean), needRestart, nil
}

func dedupe(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		t := strings.TrimSpace(e)
		if t == "" {
			continue
		}
		k := strings.ToLower(t)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, t)
	}
	return out
}
