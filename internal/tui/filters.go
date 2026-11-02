package tui

import (
	"sort"
	"strings"

	"moitoring/internal/models"
)

type queueFilter struct {
	status string
	vhost  string
	name   string
}

func filterQueues(queues []models.QueueStatus, filter queueFilter) []models.QueueStatus {
	name := strings.ToLower(strings.TrimSpace(filter.name))
	filtered := make([]models.QueueStatus, 0, len(queues))
	for _, queue := range queues {
		if !queueMatchesStatus(queue, filter.status) || !queueMatchesVHost(queue, filter.vhost) {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(queue.Name), name) {
			continue
		}
		filtered = append(filtered, queue)
	}
	return filtered
}

func queueMatchesStatus(queue models.QueueStatus, status string) bool {
	switch status {
	case "critical":
		return queue.Status == "down"
	case "warning":
		return queue.Status == "warning"
	case "healthy":
		return queue.Status == "healthy"
	case "stale":
		return queue.IsStale
	case "unknown":
		return queue.Status == "unknown"
	default:
		return true
	}
}

func queueMatchesVHost(queue models.QueueStatus, vhost string) bool {
	switch vhost {
	case "", "all":
		return true
	case "__unknown__":
		return queue.VHost == ""
	default:
		return queue.VHost == vhost
	}
}

func queueVHosts(queues []models.QueueStatus) []string {
	seen := make(map[string]struct{}, len(queues))
	for _, queue := range queues {
		if queue.VHost != "" {
			seen[queue.VHost] = struct{}{}
		}
	}
	vhosts := make([]string, 0, len(seen))
	for vhost := range seen {
		vhosts = append(vhosts, vhost)
	}
	sort.Strings(vhosts)
	return vhosts
}

func hasUnknownVHost(queues []models.QueueStatus) bool {
	for _, queue := range queues {
		if queue.VHost == "" {
			return true
		}
	}
	return false
}

func filterWorkers(workers []models.WorkerStatus, status, query string) []models.WorkerStatus {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]models.WorkerStatus, 0, len(workers))
	for _, worker := range workers {
		if !workerMatchesStatus(worker, status) {
			continue
		}
		searchable := strings.ToLower(strings.Join([]string{
			worker.Host, worker.Unit, worker.LoadState, worker.ExpectedState,
			worker.ActiveState, worker.SubState, worker.Result, worker.Status,
		}, " "))
		if query != "" && !strings.Contains(searchable, query) {
			continue
		}
		filtered = append(filtered, worker)
	}
	return filtered
}

func workerMatchesStatus(worker models.WorkerStatus, status string) bool {
	switch status {
	case "healthy":
		return worker.Status == "healthy"
	case "unexpected":
		return worker.Status != "healthy" && worker.Status != ""
	case "stale":
		return worker.IsStale
	case "unknown":
		return worker.Status == "" || worker.Status == "unknown"
	default:
		return true
	}
}

func cycleFilter(current string, options []string) string {
	for index, option := range options {
		if option == current {
			return options[(index+1)%len(options)]
		}
	}
	if len(options) == 0 {
		return current
	}
	return options[0]
}
