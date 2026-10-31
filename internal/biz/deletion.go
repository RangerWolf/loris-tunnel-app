package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"loris-tunnel/internal/cloudflared"
	"loris-tunnel/internal/conf"
	"loris-tunnel/internal/devtunnel"
	"loris-tunnel/internal/model"
)

const (
	DeletionCompleted = "completed"
	DeletionBlocked   = "blocked"
	DeletionFailed    = "failed"
	DeletionPartial   = "partial"

	CleanupDeleted   = "deleted"
	CleanupAbsent    = "already_absent"
	CleanupPreserved = "preserved"
	CleanupFailed    = "failed"
)

// DeletionPreview describes one delete before it runs.
type DeletionPreview struct {
	Kind             string   `json:"kind"`
	Provider         string   `json:"provider"`
	RecordID         int      `json:"recordId"`
	Name             string   `json:"name"`
	TunnelID         string   `json:"tunnelId"`
	LocalService     string   `json:"localService"`
	RemotePort       int      `json:"remotePort"`
	Hostname         string   `json:"hostname"`
	CleanupAvailable bool     `json:"cleanupAvailable"`
	Blocked          bool     `json:"blocked"`
	BlockReason      string   `json:"blockReason"`
	LinkedNames      []string `json:"linkedNames"`
	DNSNames         []string `json:"dnsNames"`
	DNSQueryError    string   `json:"dnsQueryError"`
	ScopeNote        string   `json:"scopeNote"`
}

// DeletionCleanupItem is one port or DNS result.
type DeletionCleanupItem struct {
	Target string `json:"target"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// DeletionResult is the outcome of a delete or a cleanup retry.
type DeletionResult struct {
	OperationID     string                `json:"operationId"`
	Status          string                `json:"status"`
	LocalDeleted    bool                  `json:"localDeleted"`
	RemoteIDDeleted bool                  `json:"remoteIdDeleted"`
	Items           []DeletionCleanupItem `json:"items"`
	ErrorCategory   string                `json:"errorCategory"`
	Detail          string                `json:"detail"`
	CompletedSteps  []string              `json:"completedSteps"`
}

type deletionTask struct {
	OperationID     string                  `json:"operationId"`
	Account         string                  `json:"account"`
	Provider        string                  `json:"provider"`
	Kind            string                  `json:"kind"`
	TunnelID        string                  `json:"tunnelId"`
	RecordID        int                     `json:"recordId"`
	Port            int                     `json:"port,omitempty"`
	DNS             []cloudflared.DNSRecord `json:"dns,omitempty"`
	LocalPending    bool                    `json:"localPending,omitempty"`
	RemoteIDDeleted bool                    `json:"remoteIdDeleted,omitempty"`
	CleanupDNS      bool                    `json:"cleanupDns,omitempty"`
	ErrorCategory   string                  `json:"errorCategory"`
	Detail          string                  `json:"detail"`
	CompletedSteps  []string                `json:"completedSteps"`
}

func (b *TunnelBiz) PreviewTunnelDeletion(ctx context.Context, id int) DeletionPreview {
	preview := DeletionPreview{Kind: "service", RecordID: id, LinkedNames: []string{}, DNSNames: []string{}}
	tunnel, ok := b.tunnelByID(id)
	if !ok {
		preview.Blocked = true
		preview.BlockReason = "not_found"
		return preview
	}
	b.fillServicePreview(ctx, &preview, tunnel)
	return preview
}

func (b *TunnelBiz) DeleteTunnelWithOptions(ctx context.Context, id int, cleanupRemote bool) DeletionResult {
	if !b.beginDeletion(fmt.Sprintf("record:%d", id)) {
		return deletionBlocked("in_progress", "a delete for this service is already running")
	}
	defer b.endDeletion(fmt.Sprintf("record:%d", id))
	tunnel, ok := b.tunnelByID(id)
	if !ok {
		return deletionFailed("not_found", "service record was not found")
	}
	if tunnel.ProviderName() == model.ProviderSSH || !cleanupRemote {
		return b.deleteLocalOnly(id)
	}
	switch tunnel.ProviderName() {
	case model.ProviderDevTunnel:
		return b.deleteDevService(ctx, tunnel)
	case model.ProviderCloudflare:
		return b.deleteCloudflareService(ctx, tunnel)
	default:
		return b.deleteLocalOnly(id)
	}
}

func (b *TunnelBiz) PreviewProviderResourceDeletion(ctx context.Context, provider, tunnelID string) DeletionPreview {
	preview := DeletionPreview{
		Kind: "provider", Provider: provider, TunnelID: strings.TrimSpace(tunnelID),
		LinkedNames: []string{}, DNSNames: []string{},
	}
	names := b.linkedServiceNames(provider, tunnelID)
	if len(names) > 0 {
		preview.Blocked = true
		preview.BlockReason = "linked"
		preview.LinkedNames = names
		return preview
	}
	switch provider {
	case model.ProviderDevTunnel:
		preview.CleanupAvailable = false
	case model.ProviderCloudflare:
		preview.CleanupAvailable = true
		preview.ScopeNote = "managed_zones_only"
		records, err := b.cloudflare.ListTunnelCNAMEs(ctx, tunnelID)
		if err != nil {
			preview.DNSQueryError = dnsErrorCategory(err)
			return preview
		}
		for _, record := range records {
			preview.DNSNames = append(preview.DNSNames, record.Name)
		}
	default:
		preview.Blocked = true
		preview.BlockReason = "unsupported"
	}
	return preview
}

func (b *TunnelBiz) DeleteProviderResourceWithOptions(ctx context.Context, provider, tunnelID string, cleanupDNS bool) DeletionResult {
	key := "provider:" + resourceLockKey(provider, tunnelID)
	if !b.beginDeletion(key) {
		return deletionBlocked("in_progress", "a delete for this tunnel id is already running")
	}
	defer b.endDeletion(key)
	names := b.linkedServiceNames(provider, tunnelID)
	if len(names) > 0 {
		result := deletionBlocked("linked", "local services still reference this tunnel id")
		result.Items = []DeletionCleanupItem{}
		return result
	}
	unlock := b.lockResource(resourceLockKey(provider, tunnelID))
	defer unlock()
	names = b.linkedServiceNames(provider, tunnelID)
	if len(names) > 0 {
		return deletionBlocked("linked", "local services still reference this tunnel id")
	}
	switch provider {
	case model.ProviderDevTunnel:
		return b.deleteDevTunnelID(ctx, tunnelID)
	case model.ProviderCloudflare:
		return b.deleteCloudflareTunnelID(ctx, tunnelID, cleanupDNS)
	default:
		return deletionFailed("unsupported", "provider delete is not available")
	}
}

func (b *TunnelBiz) RetryDeletionCleanup(ctx context.Context, operationID string) DeletionResult {
	task, err := b.readDeletionTask(operationID)
	if err != nil {
		return deletionFailed("not_found", "cleanup task was not found")
	}
	accountMatches := task.Account == "" || task.Account == b.deletionAccount(ctx, task.Provider)
	if task.Provider == model.ProviderCloudflare && task.Account != "" {
		accountMatches = b.cloudflare.MatchesAccount(task.Account)
	}
	if !accountMatches {
		return deletionFailed("account_mismatch", "cleanup must be retried with the same account")
	}
	key := task.OperationID
	if !b.beginDeletion("task:" + key) {
		return deletionBlocked("in_progress", "this cleanup is already running")
	}
	defer b.endDeletion("task:" + key)
	if task.Kind == "provider" && !task.RemoteIDDeleted {
		if task.Provider == model.ProviderCloudflare {
			return b.continueCloudflareIDDelete(ctx, task)
		}
		result := b.deleteDevTunnelID(ctx, task.TunnelID)
		if result.Status == DeletionCompleted {
			b.removeDeletionTask(task.OperationID)
		}
		result.OperationID = task.OperationID
		return result
	}
	if task.Port > 0 {
		if preserved, _ := b.portUsedByOthers(task.RecordID, task.Provider, task.TunnelID, task.Port); preserved {
			task.Port = 0
			task.CompletedSteps = append(task.CompletedSteps, "port_preserved")
		} else if err := b.dev.DeletePort(ctx, task.TunnelID, task.Port); err != nil {
			task.ErrorCategory = "stopped_cleanup_failed"
			task.Detail = err.Error()
			_ = b.writeDeletionTask(task)
			return taskResult(task, DeletionFailed)
		} else {
			task.Port = 0
			task.CompletedSteps = append(task.CompletedSteps, "port_deleted")
		}
	}
	var pending []cloudflared.DNSRecord
	for _, record := range task.DNS {
		item, keep := b.deleteOneDNS(ctx, task.TunnelID, record)
		if keep {
			pending = append(pending, record)
		}
		_ = item
	}
	task.DNS = pending
	if len(pending) > 0 {
		task.ErrorCategory = "tunnel_deleted_dns_partial"
		task.Detail = "some dns records remain"
		_ = b.writeDeletionTask(task)
		result := taskResult(task, DeletionPartial)
		result.RemoteIDDeleted = task.RemoteIDDeleted
		return result
	}
	if task.LocalPending && task.RecordID > 0 {
		if err := b.deleteTunnelRecord(task.RecordID); err != nil {
			task.ErrorCategory = "local_write_failed"
			task.Detail = err.Error()
			_ = b.writeDeletionTask(task)
			return taskResult(task, DeletionFailed)
		}
		task.LocalPending = false
		task.CompletedSteps = append(task.CompletedSteps, "local_deleted")
	}
	b.removeDeletionTask(task.OperationID)
	result := taskResult(task, DeletionCompleted)
	result.LocalDeleted = task.RecordID == 0 || !task.LocalPending
	result.RemoteIDDeleted = task.RemoteIDDeleted
	if task.RecordID > 0 && !task.LocalPending {
		result.LocalDeleted = true
	}
	return result
}

func (b *TunnelBiz) ListDeletionTasks() []DeletionResult {
	dir := b.deletionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []DeletionResult{}
	}
	var items []DeletionResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		task, err := b.readDeletionTask(id)
		if err != nil {
			continue
		}
		result := taskResult(task, DeletionFailed)
		if len(task.DNS) > 0 && task.RemoteIDDeleted {
			result.Status = DeletionPartial
		}
		items = append(items, result)
	}
	if items == nil {
		items = []DeletionResult{}
	}
	return items
}

func (b *TunnelBiz) deleteLocalOnly(id int) DeletionResult {
	if err := b.stopRuntime(id); err != nil {
		return deletionFailed("stop_failed", err.Error())
	}
	return b.finishLocal(id, nil)
}

func (b *TunnelBiz) deleteDevService(ctx context.Context, tunnel model.Tunnel) DeletionResult {
	id := tunnel.DevTunnel.TunnelID
	unlock := b.lockResource(resourceLockKey(model.ProviderDevTunnel, id))
	defer unlock()
	if err := b.stopRuntime(tunnel.ID); err != nil {
		return deletionFailed("stop_failed", err.Error())
	}
	steps := []string{"stopped"}
	port, mode := b.resolveDevPort(ctx, tunnel)
	switch mode {
	case "preserved":
		return b.finishLocal(tunnel.ID, []DeletionCleanupItem{{
			Target: fmt.Sprintf("%d", port), Status: CleanupPreserved, Detail: "another local service uses this port",
		}})
	case "absent":
		return b.finishLocal(tunnel.ID, []DeletionCleanupItem{{Target: id, Status: CleanupAbsent}})
	case "unknown":
		return DeletionResult{
			Status: DeletionFailed, ErrorCategory: "port_unknown",
			Detail: "remote port could not be confirmed", CompletedSteps: steps, Items: []DeletionCleanupItem{},
		}
	}
	if err := b.dev.DeletePort(ctx, id, port); err != nil {
		task := b.newTask(tunnel, "service")
		task.Port = port
		task.ErrorCategory = "stopped_cleanup_failed"
		task.Detail = err.Error()
		task.CompletedSteps = steps
		_ = b.writeDeletionTask(task)
		result := taskResult(task, DeletionFailed)
		result.Items = []DeletionCleanupItem{{Target: fmt.Sprintf("%d", port), Status: CleanupFailed, Detail: err.Error()}}
		return result
	}
	steps = append(steps, "port_deleted")
	result := b.finishLocal(tunnel.ID, []DeletionCleanupItem{{Target: fmt.Sprintf("%d", port), Status: CleanupDeleted}})
	result.CompletedSteps = steps
	if !result.LocalDeleted {
		task := b.newTask(tunnel, "service")
		task.LocalPending = true
		task.ErrorCategory = "local_write_failed"
		task.CompletedSteps = steps
		_ = b.writeDeletionTask(task)
		result.OperationID = task.OperationID
		result.ErrorCategory = "local_write_failed"
	}
	return result
}

func (b *TunnelBiz) deleteCloudflareService(ctx context.Context, tunnel model.Tunnel) DeletionResult {
	hostname := ""
	id := ""
	if tunnel.Cloudflare != nil {
		hostname = tunnel.Cloudflare.Hostname
		id = tunnel.Cloudflare.TunnelID
	}
	unlock := b.lockResource(resourceLockKey(model.ProviderCloudflare, id))
	defer unlock()
	if err := b.stopRuntime(tunnel.ID); err != nil {
		return deletionFailed("stop_failed", err.Error())
	}
	steps := []string{"stopped"}
	if strings.TrimSpace(hostname) == "" || !cloudflared.ValidTunnelID(id) {
		result := b.finishLocal(tunnel.ID, nil)
		result.CompletedSteps = steps
		return result
	}
	records, err := b.cloudflare.ListCNAMEsByName(ctx, hostname)
	if err != nil {
		return DeletionResult{
			Status: DeletionFailed, ErrorCategory: dnsErrorCategory(err), Detail: "dns lookup failed",
			CompletedSteps: steps, Items: []DeletionCleanupItem{},
		}
	}
	var items []DeletionCleanupItem
	var failed []cloudflared.DNSRecord
	preserved := false
	for _, record := range records {
		if !cloudflared.CNAMEMatches(record, id) {
			preserved = true
			items = append(items, DeletionCleanupItem{Target: record.Name, Status: CleanupPreserved, Detail: "dns target changed"})
			continue
		}
		item, keep := b.deleteOneDNS(ctx, id, record)
		items = append(items, item)
		if item.Status == CleanupPreserved {
			preserved = true
		}
		if keep {
			failed = append(failed, record)
		}
	}
	if len(records) == 0 {
		items = append(items, DeletionCleanupItem{Target: hostname, Status: CleanupAbsent})
	}
	if len(failed) > 0 {
		task := b.newTask(tunnel, "service")
		task.DNS = failed
		task.ErrorCategory = "stopped_cleanup_failed"
		task.Detail = "dns cleanup failed"
		task.CompletedSteps = steps
		_ = b.writeDeletionTask(task)
		result := taskResult(task, DeletionFailed)
		result.Items = items
		return result
	}
	result := b.finishLocal(tunnel.ID, items)
	result.CompletedSteps = append(steps, "dns_checked")
	if preserved {
		result.ErrorCategory = "dns_changed"
	}
	if !result.LocalDeleted {
		task := b.newTask(tunnel, "service")
		task.LocalPending = true
		task.ErrorCategory = "local_write_failed"
		task.CompletedSteps = result.CompletedSteps
		_ = b.writeDeletionTask(task)
		result.OperationID = task.OperationID
		result.ErrorCategory = "local_write_failed"
	}
	return result
}

func (b *TunnelBiz) deleteDevTunnelID(ctx context.Context, tunnelID string) DeletionResult {
	detail := b.dev.GetResource(ctx, tunnelID)
	target := tunnelID
	if detail.State == devtunnel.ResourceReady && detail.Resource.ID != "" {
		target = detail.Resource.ID
	}
	if detail.State == devtunnel.ResourceNotFound {
		b.updateDevResourceCache(devtunnel.Resource{ID: tunnelID}, true)
		return DeletionResult{Status: DeletionCompleted, RemoteIDDeleted: true, Items: []DeletionCleanupItem{{Target: tunnelID, Status: CleanupAbsent}}, CompletedSteps: []string{"remote_absent"}}
	}
	if err := b.dev.DeleteRemoteGuarded(ctx, target); err != nil {
		if devMissing(err) {
			b.updateDevResourceCache(devtunnel.Resource{ID: target}, true)
			return DeletionResult{Status: DeletionCompleted, RemoteIDDeleted: true, Items: []DeletionCleanupItem{{Target: target, Status: CleanupAbsent}}, CompletedSteps: []string{"remote_absent"}}
		}
		return deletionFailed(devTunnelFailureKindString(err), err.Error())
	}
	b.updateDevResourceCache(devtunnel.Resource{ID: target}, true)
	return DeletionResult{Status: DeletionCompleted, RemoteIDDeleted: true, Items: []DeletionCleanupItem{{Target: target, Status: CleanupDeleted}}, CompletedSteps: []string{"remote_deleted"}}
}

func (b *TunnelBiz) deleteCloudflareTunnelID(ctx context.Context, tunnelID string, cleanupDNS bool) DeletionResult {
	b.cloudflareAuthMu.Lock()
	defer b.cloudflareAuthMu.Unlock()
	task := deletionTask{
		OperationID: newOperationID(), Provider: model.ProviderCloudflare, Kind: "provider",
		TunnelID: tunnelID, CleanupDNS: cleanupDNS, Account: b.deletionAccount(ctx, model.ProviderCloudflare),
		CompletedSteps: []string{},
	}
	if cleanupDNS {
		records, err := b.cloudflare.ListTunnelCNAMEs(ctx, tunnelID)
		if err != nil {
			return deletionFailed(dnsErrorCategory(err), "dns lookup failed")
		}
		task.DNS = records
		if err := b.writeDeletionTask(task); err != nil {
			return deletionFailed("task_write_failed", err.Error())
		}
	}
	if err := b.cloudflare.DeleteRemote(ctx, tunnelID); err != nil {
		if !cloudflareMissing(err) {
			task.ErrorCategory = "remote_delete_failed"
			task.Detail = err.Error()
			if cleanupDNS {
				_ = b.writeDeletionTask(task)
			}
			result := taskResult(task, DeletionFailed)
			return result
		}
	}
	task.RemoteIDDeleted = true
	task.CompletedSteps = append(task.CompletedSteps, "remote_deleted")
	b.updateCloudflareResourceCache(cloudflared.Resource{ID: tunnelID}, true)
	if !cleanupDNS {
		b.removeDeletionTask(task.OperationID)
		return DeletionResult{Status: DeletionCompleted, RemoteIDDeleted: true, Items: []DeletionCleanupItem{}, CompletedSteps: task.CompletedSteps}
	}
	var pending []cloudflared.DNSRecord
	var items []DeletionCleanupItem
	for _, record := range task.DNS {
		item, keep := b.deleteOneDNS(ctx, tunnelID, record)
		items = append(items, item)
		if keep {
			pending = append(pending, record)
		}
	}
	task.DNS = pending
	if len(pending) > 0 {
		task.ErrorCategory = "tunnel_deleted_dns_partial"
		task.Detail = "some dns records remain"
		_ = b.writeDeletionTask(task)
		result := taskResult(task, DeletionPartial)
		result.RemoteIDDeleted = true
		result.Items = items
		return result
	}
	b.removeDeletionTask(task.OperationID)
	return DeletionResult{OperationID: task.OperationID, Status: DeletionCompleted, RemoteIDDeleted: true, Items: items, CompletedSteps: task.CompletedSteps}
}

func (b *TunnelBiz) deleteOneDNS(ctx context.Context, tunnelID string, record cloudflared.DNSRecord) (DeletionCleanupItem, bool) {
	current, err := b.cloudflare.GetDNSRecord(ctx, record.ZoneID, record.ID)
	if errors.Is(err, cloudflared.ErrDNSAbsent) {
		return DeletionCleanupItem{Target: record.Name, Status: CleanupAbsent}, false
	}
	if err != nil {
		return DeletionCleanupItem{Target: record.Name, Status: CleanupFailed, Detail: dnsErrorCategory(err)}, true
	}
	if !cloudflared.CNAMEMatches(current, tunnelID) {
		return DeletionCleanupItem{Target: current.Name, Status: CleanupPreserved, Detail: "dns target changed"}, false
	}
	if err := b.cloudflare.DeleteDNSRecord(ctx, record.ZoneID, record.ID); err != nil {
		return DeletionCleanupItem{Target: record.Name, Status: CleanupFailed, Detail: dnsErrorCategory(err)}, true
	}
	return DeletionCleanupItem{Target: record.Name, Status: CleanupDeleted}, false
}

func (b *TunnelBiz) finishLocal(id int, items []DeletionCleanupItem) DeletionResult {
	if items == nil {
		items = []DeletionCleanupItem{}
	}
	if b.beforeForgetRecord != nil {
		b.beforeForgetRecord()
	}
	if err := b.deleteTunnelRecord(id); err != nil {
		return DeletionResult{Status: DeletionFailed, ErrorCategory: "local_write_failed", Detail: err.Error(), Items: items}
	}
	return DeletionResult{Status: DeletionCompleted, LocalDeleted: true, Items: items, CompletedSteps: []string{"local_deleted"}}
}

func (b *TunnelBiz) continueCloudflareIDDelete(ctx context.Context, task deletionTask) DeletionResult {
	if err := b.cloudflare.DeleteRemote(ctx, task.TunnelID); err != nil && !cloudflareMissing(err) {
		task.ErrorCategory = "remote_delete_failed"
		task.Detail = err.Error()
		_ = b.writeDeletionTask(task)
		return taskResult(task, DeletionFailed)
	}
	task.RemoteIDDeleted = true
	task.CompletedSteps = append(task.CompletedSteps, "remote_deleted")
	b.updateCloudflareResourceCache(cloudflared.Resource{ID: task.TunnelID}, true)
	return b.finishTaskDNS(ctx, task)
}

func (b *TunnelBiz) finishTaskDNS(ctx context.Context, task deletionTask) DeletionResult {
	var pending []cloudflared.DNSRecord
	var items []DeletionCleanupItem
	for _, record := range task.DNS {
		item, keep := b.deleteOneDNS(ctx, task.TunnelID, record)
		items = append(items, item)
		if keep {
			pending = append(pending, record)
		}
	}
	task.DNS = pending
	if len(pending) > 0 {
		task.ErrorCategory = "tunnel_deleted_dns_partial"
		task.Detail = "some dns records remain"
		_ = b.writeDeletionTask(task)
		result := taskResult(task, DeletionPartial)
		result.RemoteIDDeleted = true
		result.Items = items
		return result
	}
	if task.LocalPending && task.RecordID > 0 {
		if err := b.deleteTunnelRecord(task.RecordID); err != nil {
			task.ErrorCategory = "local_write_failed"
			task.Detail = err.Error()
			_ = b.writeDeletionTask(task)
			result := taskResult(task, DeletionFailed)
			result.Items = items
			return result
		}
	}
	b.removeDeletionTask(task.OperationID)
	return DeletionResult{
		OperationID: task.OperationID, Status: DeletionCompleted, RemoteIDDeleted: task.RemoteIDDeleted,
		LocalDeleted: task.RecordID > 0, Items: items, CompletedSteps: append(task.CompletedSteps, "dns_checked"),
	}
}

func (b *TunnelBiz) deleteTunnelRecord(id int) error {
	_, err := b.storage.Update(func(cfg *conf.Config) error {
		idx := -1
		for i := range cfg.Tunnels {
			if cfg.Tunnels[i].ID == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrTunnelNotFound
		}
		cfg.Tunnels = append(cfg.Tunnels[:idx], cfg.Tunnels[idx+1:]...)
		return nil
	})
	if errors.Is(err, ErrTunnelNotFound) {
		b.forgetURL(id)
		return nil
	}
	if err == nil {
		b.forgetURL(id)
	}
	return err
}

func (b *TunnelBiz) fillServicePreview(ctx context.Context, preview *DeletionPreview, tunnel model.Tunnel) {
	preview.Name = tunnel.Name
	preview.Provider = tunnel.ProviderName()
	preview.LocalService = serviceURL(tunnel)
	switch tunnel.ProviderName() {
	case model.ProviderDevTunnel:
		if tunnel.DevTunnel == nil {
			return
		}
		preview.TunnelID = tunnel.DevTunnel.TunnelID
		port, mode := b.resolveDevPort(ctx, tunnel)
		if mode == "ready" || mode == "preserved" {
			preview.RemotePort = port
			preview.CleanupAvailable = mode == "ready"
		}
	case model.ProviderCloudflare:
		if tunnel.Cloudflare == nil {
			return
		}
		preview.TunnelID = tunnel.Cloudflare.TunnelID
		preview.Hostname = tunnel.Cloudflare.Hostname
		preview.CleanupAvailable = strings.TrimSpace(preview.Hostname) != "" && cloudflared.ValidTunnelID(preview.TunnelID)
	}
}

func (b *TunnelBiz) resolveDevPort(ctx context.Context, tunnel model.Tunnel) (int, string) {
	if tunnel.DevTunnel == nil || !devtunnel.ValidTunnelID(tunnel.DevTunnel.TunnelID) {
		return 0, "unknown"
	}
	id := tunnel.DevTunnel.TunnelID
	if tunnel.DevTunnel.TunnelPort > 0 {
		if preserved, _ := b.portUsedByOthers(tunnel.ID, model.ProviderDevTunnel, id, tunnel.DevTunnel.TunnelPort); preserved {
			return tunnel.DevTunnel.TunnelPort, "preserved"
		}
		return tunnel.DevTunnel.TunnelPort, "ready"
	}
	detail := b.dev.GetResource(ctx, id)
	if detail.State == devtunnel.ResourceNotFound {
		return 0, "absent"
	}
	if detail.State != devtunnel.ResourceReady {
		return 0, "unknown"
	}
	if len(b.linkedServiceNames(model.ProviderDevTunnel, id)) > 1 {
		return 0, "unknown"
	}
	if len(detail.Resource.Ports) != 1 {
		return 0, "unknown"
	}
	return detail.Resource.Ports[0].Number, "ready"
}

func (b *TunnelBiz) portUsedByOthers(selfID int, provider, tunnelID string, port int) (bool, string) {
	cfg, err := b.storage.Load()
	if err != nil {
		return false, ""
	}
	for _, item := range cfg.Tunnels {
		if item.ID == selfID || item.ProviderName() != provider {
			continue
		}
		switch provider {
		case model.ProviderDevTunnel:
			if item.DevTunnel == nil || !devtunnel.SameTunnelID(item.DevTunnel.TunnelID, tunnelID) {
				continue
			}
			if item.DevTunnel.TunnelPort == port {
				return true, item.Name
			}
		}
	}
	return false, ""
}

func (b *TunnelBiz) linkedServiceNames(provider, tunnelID string) []string {
	cfg, err := b.storage.Load()
	if err != nil {
		return nil
	}
	var names []string
	for _, item := range cfg.Tunnels {
		if item.ProviderName() != provider {
			continue
		}
		switch provider {
		case model.ProviderDevTunnel:
			if item.DevTunnel != nil && devtunnel.SameTunnelID(item.DevTunnel.TunnelID, tunnelID) {
				names = append(names, item.Name)
			}
		case model.ProviderCloudflare:
			if item.Cloudflare != nil && cloudflared.SameTunnelID(item.Cloudflare.TunnelID, tunnelID) {
				names = append(names, item.Name)
			}
		}
	}
	if names == nil {
		names = []string{}
	}
	return names
}

func (b *TunnelBiz) tunnelByID(id int) (model.Tunnel, bool) {
	cfg, err := b.storage.Load()
	if err != nil {
		return model.Tunnel{}, false
	}
	return findTunnelByID(cfg.Tunnels, id)
}

func (b *TunnelBiz) newTask(tunnel model.Tunnel, kind string) deletionTask {
	provider := tunnel.ProviderName()
	task := deletionTask{
		OperationID: newOperationID(), Account: b.deletionAccount(context.Background(), provider),
		Provider: provider, Kind: kind, RecordID: tunnel.ID, CompletedSteps: []string{},
	}
	if tunnel.DevTunnel != nil {
		task.TunnelID = tunnel.DevTunnel.TunnelID
	}
	if tunnel.Cloudflare != nil {
		task.TunnelID = tunnel.Cloudflare.TunnelID
	}
	return task
}

func (b *TunnelBiz) deletionAccount(ctx context.Context, provider string) string {
	switch provider {
	case model.ProviderDevTunnel:
		status, err := b.dev.Inspect(ctx)
		if err != nil || status.AccountState != "signed_in" {
			return ""
		}
		b.rememberDevAccount(status)
		if status.AccountLabel == "" {
			return ""
		}
		return "dev:" + status.AccountLabel
	case model.ProviderCloudflare:
		id := b.cloudflare.AccountIdentity()
		if id == "" {
			return ""
		}
		return "cf:" + id
	default:
		return ""
	}
}

func (b *TunnelBiz) deletionDir() string {
	if b.storage == nil || b.storage.Path() == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(b.storage.Path()), "deletion-tasks")
}

func (b *TunnelBiz) writeDeletionTask(task deletionTask) error {
	if !validOperationID(task.OperationID) {
		return fmt.Errorf("invalid cleanup id")
	}
	dir := b.deletionDir()
	if dir == "" {
		return fmt.Errorf("cleanup directory is unavailable")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".deletion-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, task.OperationID+".json"))
}

func (b *TunnelBiz) readDeletionTask(operationID string) (deletionTask, error) {
	if !validOperationID(operationID) {
		return deletionTask{}, fmt.Errorf("invalid cleanup id")
	}
	raw, err := os.ReadFile(filepath.Join(b.deletionDir(), operationID+".json"))
	if err != nil {
		return deletionTask{}, err
	}
	var task deletionTask
	if err := json.Unmarshal(raw, &task); err != nil {
		return deletionTask{}, err
	}
	return task, nil
}

func (b *TunnelBiz) removeDeletionTask(operationID string) {
	if !validOperationID(operationID) {
		return
	}
	_ = os.Remove(filepath.Join(b.deletionDir(), operationID+".json"))
}

func (b *TunnelBiz) beginDeletion(key string) bool {
	b.deletionMu.Lock()
	defer b.deletionMu.Unlock()
	if b.deletionBusy[key] {
		return false
	}
	if b.deletionBusy == nil {
		b.deletionBusy = map[string]bool{}
	}
	b.deletionBusy[key] = true
	return true
}

func (b *TunnelBiz) endDeletion(key string) {
	b.deletionMu.Lock()
	delete(b.deletionBusy, key)
	b.deletionMu.Unlock()
}

func (b *TunnelBiz) lockResource(key string) func() {
	b.resourceMu.Lock()
	if b.resourceLocks == nil {
		b.resourceLocks = map[string]*sync.Mutex{}
	}
	mu := b.resourceLocks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		b.resourceLocks[key] = mu
	}
	b.resourceMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func resourceLockKey(provider, id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if provider == model.ProviderDevTunnel || provider == "devtunnel" {
		short, _, _ := strings.Cut(id, ".")
		if short != "" {
			id = short
		}
	}
	return provider + ":" + id
}

func serviceURL(tunnel model.Tunnel) string {
	host, port, protocol := tunnel.LocalHost, tunnel.LocalPort, tunnel.Mode
	if tunnel.DevTunnel != nil {
		if tunnel.DevTunnel.LocalHost != "" {
			host = tunnel.DevTunnel.LocalHost
		}
		if tunnel.DevTunnel.LocalPort > 0 {
			port = tunnel.DevTunnel.LocalPort
		}
		if tunnel.DevTunnel.Protocol != "" {
			protocol = tunnel.DevTunnel.Protocol
		}
	}
	if tunnel.Cloudflare != nil {
		if tunnel.Cloudflare.LocalHost != "" {
			host = tunnel.Cloudflare.LocalHost
		}
		if tunnel.Cloudflare.LocalPort > 0 {
			port = tunnel.Cloudflare.LocalPort
		}
		if tunnel.Cloudflare.Protocol != "" {
			protocol = tunnel.Cloudflare.Protocol
		}
	}
	if host == "" || port <= 0 {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	scheme := "http"
	if strings.EqualFold(protocol, "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

func deletionBlocked(category, detail string) DeletionResult {
	return DeletionResult{Status: DeletionBlocked, ErrorCategory: category, Detail: detail, Items: []DeletionCleanupItem{}, CompletedSteps: []string{}}
}

func deletionFailed(category, detail string) DeletionResult {
	return DeletionResult{Status: DeletionFailed, ErrorCategory: category, Detail: detail, Items: []DeletionCleanupItem{}, CompletedSteps: []string{}}
}

func taskResult(task deletionTask, status string) DeletionResult {
	return DeletionResult{
		OperationID: task.OperationID, Status: status, ErrorCategory: task.ErrorCategory, Detail: task.Detail,
		CompletedSteps: task.CompletedSteps, Items: []DeletionCleanupItem{}, RemoteIDDeleted: task.RemoteIDDeleted,
	}
}

func dnsErrorCategory(err error) string {
	if errors.Is(err, cloudflared.ErrDNSPermission) {
		return "dns_permission"
	}
	if errors.Is(err, cloudflared.ErrDNSNetwork) {
		return "dns_network"
	}
	return "dns_failed"
}

func devMissing(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func cloudflareMissing(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") || strings.Contains(text, "does not exist")
}

func devTunnelFailureKindString(err error) string {
	state, _ := devTunnelFailureKind(err)
	if state == "" {
		return "remote_delete_failed"
	}
	return state
}

func newOperationID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(raw[:])
}

func validOperationID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, ch := range id {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}
