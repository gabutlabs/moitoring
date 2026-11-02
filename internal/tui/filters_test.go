package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"moitoring/internal/models"
)

func TestQueueFiltersMatchWebStatusVHostAndName(t *testing.T) {
	queues := []models.QueueStatus{
		{QueueKey: models.QueueKey{VHost: "/", Name: "Jobs"}, Status: "healthy"},
		{QueueKey: models.QueueKey{VHost: "/", Name: "jobs-old"}, Status: "healthy", IsStale: true},
		{QueueKey: models.QueueKey{VHost: "/", Name: "jobs-retry"}, Status: "down", IsStale: true},
		{QueueKey: models.QueueKey{VHost: "events", Name: "outbox"}, Status: "warning"},
		{QueueKey: models.QueueKey{Name: "unrouted"}, Status: "unknown"},
	}

	got := filterQueues(queues, queueFilter{status: "healthy", vhost: "/", name: "job"})
	if len(got) != 2 || got[0].Name != "Jobs" || got[1].Name != "jobs-old" {
		t.Fatalf("healthy / name filter = %+v, want Jobs and stale jobs-old", got)
	}
	got = filterQueues(queues, queueFilter{status: "stale"})
	if len(got) != 2 || got[0].Name != "jobs-old" || got[1].Name != "jobs-retry" {
		t.Fatalf("stale filter = %+v, want both stale queues", got)
	}
	got = filterQueues(queues, queueFilter{vhost: "__unknown__"})
	if len(got) != 1 || got[0].Name != "unrouted" {
		t.Fatalf("unknown vhost filter = %+v, want only unrouted", got)
	}
}

func TestWorkerFiltersSearchAndStatus(t *testing.T) {
	workers := []models.WorkerStatus{
		{WorkerObservation: models.WorkerObservation{Host: "ops@worker-a", Unit: "billing.service", ActiveState: "failed"}, Status: "down"},
		{WorkerObservation: models.WorkerObservation{Host: "localhost", Unit: "jobs.service", ActiveState: "active"}, Status: "healthy"},
		{WorkerObservation: models.WorkerObservation{Host: "worker-b", Unit: "mail.service"}, Status: "healthy", IsStale: true},
	}

	got := filterWorkers(workers, "unexpected", "BILLING")
	if len(got) != 1 || got[0].Unit != "billing.service" {
		t.Fatalf("unexpected search = %+v, want billing.service", got)
	}
	got = filterWorkers(workers, "stale", "worker-b")
	if len(got) != 1 || got[0].Unit != "mail.service" {
		t.Fatalf("stale worker search = %+v, want mail.service", got)
	}
	got = filterWorkers(workers, "healthy", "")
	if len(got) != 2 || got[1].Unit != "mail.service" {
		t.Fatalf("healthy worker filter = %+v, want current and stale healthy workers", got)
	}
}

func TestSearchEditingAppliesAndEscRestoresPreviousQuery(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.activeView = queuesView
	app.queueSearch = "jobs"

	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = updated.(model)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-', 'r', 'e', 't', 'r', 'y'}})
	app = updated.(model)
	if !app.searching || app.queueSearch != "jobs-retry" {
		t.Fatalf("edited search = %q, searching=%v", app.queueSearch, app.searching)
	}
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = updated.(model)
	if app.searching || app.queueSearch != "jobs" {
		t.Fatalf("search after escape = %q, searching=%v; want previous query", app.queueSearch, app.searching)
	}

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app = updated.(model)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	app = updated.(model)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m', 'a', 'i', 'l'}})
	app = updated.(model)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = updated.(model)
	if app.searching || app.queueSearch != "mail" {
		t.Fatalf("search after enter = %q, searching=%v; want applied query", app.queueSearch, app.searching)
	}
}

func TestListFilterKeysCycleAndClear(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.activeView = queuesView
	app.snapshot.Queues = []models.QueueStatus{
		{QueueKey: models.QueueKey{VHost: "beta", Name: "b"}},
		{QueueKey: models.QueueKey{VHost: "alpha", Name: "a"}},
	}
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	app = updated.(model)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	app = updated.(model)
	if app.queueVHost != "alpha" || app.queueStatus != "critical" {
		t.Fatalf("queue filters = vhost %q, status %q; want alpha/critical", app.queueVHost, app.queueStatus)
	}
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = updated.(model)
	if app.queueVHost != "all" || app.queueStatus != "all" || app.queueSearch != "" {
		t.Fatalf("filters after clear = vhost %q, status %q, search %q", app.queueVHost, app.queueStatus, app.queueSearch)
	}
}
