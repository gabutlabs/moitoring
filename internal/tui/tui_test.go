package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"moitoring/internal/models"
)

type snapshotReaderFunc func(context.Context) (models.MonitoringSnapshot, error)

func (reader snapshotReaderFunc) Snapshot(ctx context.Context) (models.MonitoringSnapshot, error) {
	return reader(ctx)
}

func TestModelNavigatesDashboardViews(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)

	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	app = updated.(model)
	if app.activeView != workersView {
		t.Fatalf("view after left = %d, want workers view", app.activeView)
	}

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = updated.(model)
	if app.activeView != queuesView {
		t.Fatalf("view after pressing 2 = %d, want queues view", app.activeView)
	}
}

func TestQueueViewShowsUnavailableMetricsAndStaleState(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.hasSnapshot = true
	app.activeView = queuesView
	app.snapshot.Queues = []models.QueueStatus{{
		QueueKey:   models.QueueKey{VHost: "/", Name: "jobs"},
		Status:     "down",
		IsStale:    true,
		ObservedAt: time.Now().Add(-2 * time.Minute),
	}}

	view := app.View()
	for _, want := range []string{"STALE", "jobs", "n/a"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q:\n%s", want, view)
		}
	}
}

func TestQueueViewScrollsThroughTargets(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.hasSnapshot = true
	app.activeView = queuesView
	app.height = 9
	app.snapshot.Queues = []models.QueueStatus{
		{QueueKey: models.QueueKey{VHost: "/", Name: "first"}, Status: "healthy"},
		{QueueKey: models.QueueKey{VHost: "/", Name: "second"}, Status: "healthy"},
	}

	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = updated.(model)
	if !strings.Contains(app.View(), "second") {
		t.Fatalf("view after scrolling = %q, want second queue", app.View())
	}
}

func TestEmptyQueueViewExplainsMissingObservations(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.hasSnapshot = true
	app.activeView = queuesView

	if view := app.View(); !strings.Contains(view, "No queue observations yet.") {
		t.Fatalf("view = %q, want empty queue message", view)
	}
}

func TestModelRefreshesAndQuitsFromKeyboard(t *testing.T) {
	reads := 0
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		reads++
		return models.MonitoringSnapshot{Source: models.SourceStatus{Name: "rabbitmq", Status: "up"}}, nil
	}), time.Minute)

	updated, command := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app = updated.(model)
	if !app.loading || command == nil {
		t.Fatal("r should start a refresh")
	}
	updated, _ = app.Update(command())
	app = updated.(model)
	if !app.hasSnapshot || app.loading || reads != 1 {
		t.Fatalf("after refresh: hasSnapshot=%v loading=%v reads=%d", app.hasSnapshot, app.loading, reads)
	}

	_, command = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command == nil {
		t.Fatal("q should quit the TUI")
	}
}

func TestOverviewShowsSourceCollectionError(t *testing.T) {
	app := newModel(context.Background(), snapshotReaderFunc(func(context.Context) (models.MonitoringSnapshot, error) {
		return models.MonitoringSnapshot{}, nil
	}), time.Minute)
	app.hasSnapshot = true
	app.snapshot.Source = models.SourceStatus{Status: "down", LastError: "connection refused"}

	if rendered := app.View(); !strings.Contains(rendered, "connection refused") {
		t.Fatalf("overview = %q, want collection error", rendered)
	}
}
