package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"moitoring/internal/models"
)

type SnapshotReader interface {
	Snapshot(context.Context) (models.MonitoringSnapshot, error)
}

type view int

const (
	overviewView view = iota
	queuesView
	workersView
	viewCount
)

var (
	brandStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#14583F"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#68756D"))
	activeTab    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#14583F")).Padding(0, 1)
	inactiveTab  = lipgloss.NewStyle().Foreground(lipgloss.Color("#46534B")).Padding(0, 1)
	goodStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#167447")).Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9A6A00")).Bold(true)
	badStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#B42318")).Bold(true)
)

type model struct {
	ctx          context.Context
	reader       SnapshotReader
	pollInterval time.Duration
	activeView   view
	snapshot     models.MonitoringSnapshot
	queueSearch  string
	workerSearch string
	queueStatus  string
	workerStatus string
	queueVHost   string
	searching    bool
	searchBefore string
	hasSnapshot  bool
	loading      bool
	err          error
	width        int
	height       int
	scroll       int
	updatedAt    time.Time
}

type snapshotMsg struct {
	snapshot models.MonitoringSnapshot
	err      error
}

type refreshMsg struct{}

func Run(ctx context.Context, reader SnapshotReader, pollInterval time.Duration) error {
	if reader == nil {
		return fmt.Errorf("monitoring snapshot reader cannot be nil")
	}
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	program := tea.NewProgram(newModel(ctx, reader, pollInterval), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newModel(ctx context.Context, reader SnapshotReader, pollInterval time.Duration) model {
	return model{
		ctx:          ctx,
		reader:       reader,
		pollInterval: pollInterval,
		queueStatus:  "all",
		workerStatus: "all",
		queueVHost:   "all",
		loading:      true,
		width:        80,
		height:       24,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadSnapshot(), m.waitForRefresh())
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case tea.KeyMsg:
		if message.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searching {
			m.handleSearchKey(message)
			return m, nil
		}
		switch message.String() {
		case "q", "esc":
			return m, tea.Quit
		case "left", "h":
			m.activeView = (m.activeView + viewCount - 1) % viewCount
			m.scroll = 0
		case "right", "l", "tab":
			m.activeView = (m.activeView + 1) % viewCount
			m.scroll = 0
		case "1":
			m.activeView = overviewView
			m.scroll = 0
		case "2":
			m.activeView = queuesView
			m.scroll = 0
		case "3":
			m.activeView = workersView
			m.scroll = 0
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			m.scroll++
		case "r":
			m.loading = true
			m.err = nil
			return m, m.loadSnapshot()
		case "/":
			if m.activeView == queuesView || m.activeView == workersView {
				m.searching = true
				m.searchBefore = m.searchQuery()
			}
		case "s":
			if m.activeView == queuesView {
				m.queueStatus = cycleFilter(m.queueStatus, []string{"all", "critical", "warning", "healthy", "stale", "unknown"})
			} else if m.activeView == workersView {
				m.workerStatus = cycleFilter(m.workerStatus, []string{"all", "healthy", "unexpected", "stale", "unknown"})
			}
		case "v":
			if m.activeView == queuesView {
				m.queueVHost = cycleFilter(m.queueVHost, vhostFilterOptions(m.snapshot.Queues))
			}
		case "c":
			m.clearFilters()
		}
	case snapshotMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.snapshot = message.snapshot
			m.hasSnapshot = true
			m.updatedAt = time.Now()
			if !contains(vhostFilterOptions(m.snapshot.Queues), m.queueVHost) {
				m.queueVHost = "all"
			}
		}
	case refreshMsg:
		return m, tea.Batch(m.loadSnapshot(), m.waitForRefresh())
	}
	return m, nil
}

func (m model) View() string {
	parts := []string{
		brandStyle.Render("MOITORING"),
		mutedStyle.Render("Read-only monitoring"),
		m.renderTabs(),
		strings.Repeat("─", max(1, m.width-2)),
	}
	if m.err != nil {
		parts = append(parts, m.fitRow(badStyle.Render("Could not load monitoring status: "+m.err.Error())))
	} else if m.loading && !m.hasSnapshot {
		parts = append(parts, mutedStyle.Render("Loading monitoring status…"))
	} else if m.hasSnapshot {
		parts = append(parts, m.fitRows(m.renderPage())...)
	}
	if m.loading && m.hasSnapshot {
		parts = append(parts, mutedStyle.Render("Refreshing…"))
	}
	if !m.updatedAt.IsZero() {
		parts = append(parts, mutedStyle.Render("Updated "+formatAge(m.updatedAt)))
	}
	parts = append(parts, "", mutedStyle.Render(m.helpText()))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderTabs() string {
	labels := [...]string{"1 Overview", "2 RabbitMQ queues", "3 Systemd workers"}
	if m.width < 52 {
		labels = [...]string{"1 Overview", "2 Queues", "3 Services"}
	}
	tabs := make([]string, 0, len(labels))
	for index, label := range labels {
		style := inactiveTab
		if view(index) == m.activeView {
			style = activeTab
		}
		tabs = append(tabs, style.Render(label))
	}
	if m.width < 40 {
		return lipgloss.JoinVertical(lipgloss.Left, tabs...)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m model) renderPage() []string {
	switch m.activeView {
	case queuesView:
		return m.renderQueues()
	case workersView:
		return m.renderWorkers()
	default:
		return m.renderOverview()
	}
}

func (m model) renderOverview() []string {
	queuesStale, workersStale := 0, 0
	for _, queue := range m.snapshot.Queues {
		if queue.IsStale {
			queuesStale++
		}
	}
	for _, worker := range m.snapshot.Workers {
		if worker.IsStale {
			workersStale++
		}
	}
	rows := []string{
		"Sources",
	}
	rows = append(rows, sourceLines("RabbitMQ", m.snapshot.Source)...)
	rows = append(rows, sourceLines("Systemd", m.snapshot.WorkerSource)...)
	rows = append(rows, "", "Inventory", fmt.Sprintf("%d queues (%d stale)  ·  %d workers (%d stale)", len(m.snapshot.Queues), queuesStale, len(m.snapshot.Workers), workersStale))
	if m.snapshot.Source.FailedEmails > 0 {
		rows = append(rows, warningStyle.Render(fmt.Sprintf("Failed alert emails: %d", m.snapshot.Source.FailedEmails)))
		if m.snapshot.Source.LastEmailError != "" {
			rows = append(rows, mutedStyle.Render("  "+singleLine(m.snapshot.Source.LastEmailError)))
		}
	}
	return rows
}

func (m model) renderQueues() []string {
	rows := sourceLines("RabbitMQ", m.snapshot.Source)
	queues := filterQueues(m.snapshot.Queues, queueFilter{status: m.queueStatus, vhost: m.queueVHost, name: m.queueSearch})
	rows = append(rows, mutedStyle.Render(m.queueFilterText()))
	rows = append(rows, mutedStyle.Render(fmt.Sprintf("%d of %d queues", len(queues), len(m.snapshot.Queues))))
	if len(queues) == 0 {
		message := "No queues match these filters."
		if len(m.snapshot.Queues) == 0 {
			message = "No queue observations yet."
		}
		return append(rows, mutedStyle.Render(message))
	}
	queueRows := make([]string, len(queues))
	for index, queue := range queues {
		status := styledStatus(queue.Status, queue.IsStale)
		queueRows[index] = fmt.Sprintf("%s  %s/%s · %s messages · %s ready · %s unacked · %s consumers · %s",
			status, queue.VHost, queue.Name, formatCount(queue.Messages), formatCount(queue.MessagesReady),
			formatCount(queue.MessagesUnacked), formatCount(queue.Consumers), formatAge(queue.ObservedAt))
	}
	return append(rows, m.visibleRows(queueRows)...)
}

func (m model) renderWorkers() []string {
	rows := sourceLines("Systemd", m.snapshot.WorkerSource)
	workers := filterWorkers(m.snapshot.Workers, m.workerStatus, m.workerSearch)
	rows = append(rows, mutedStyle.Render(m.workerFilterText()))
	rows = append(rows, mutedStyle.Render(fmt.Sprintf("%d of %d workers", len(workers), len(m.snapshot.Workers))))
	if len(workers) == 0 {
		message := "No workers match these filters."
		if len(m.snapshot.Workers) == 0 {
			message = "No systemd worker observations yet."
		}
		return append(rows, mutedStyle.Render(message))
	}
	workerRows := make([]string, len(workers))
	for index, worker := range workers {
		state := worker.ActiveState
		if worker.SubState != "" {
			state += "/" + worker.SubState
		}
		workerRows[index] = fmt.Sprintf("%s  %s · %s · expected %s · %s · %s",
			styledStatus(worker.Status, worker.IsStale), worker.Host, worker.Unit,
			unknownIfEmpty(worker.ExpectedState), unknownIfEmpty(state), formatAge(worker.ObservedAt))
	}
	return append(rows, m.visibleRows(workerRows)...)
}

func (m model) visibleRows(rows []string) []string {
	limit := max(1, m.height-11)
	start := min(m.scroll, max(0, len(rows)-limit))
	end := min(len(rows), start+limit)
	visible := append([]string(nil), rows[start:end]...)
	if end < len(rows) {
		visible = append(visible, mutedStyle.Render(fmt.Sprintf("… %d more; scroll down", len(rows)-end)))
	}
	return visible
}

func (m model) searchQuery() string {
	if m.activeView == queuesView {
		return m.queueSearch
	}
	return m.workerSearch
}

func (m *model) setSearchQuery(query string) {
	if m.activeView == queuesView {
		m.queueSearch = query
	} else {
		m.workerSearch = query
	}
}

func (m *model) handleSearchKey(message tea.KeyMsg) {
	query := m.searchQuery()
	switch message.String() {
	case "esc":
		m.setSearchQuery(m.searchBefore)
		m.searching = false
		m.searchBefore = ""
	case "enter":
		m.searching = false
		m.searchBefore = ""
	case "backspace", "ctrl+h":
		runes := []rune(query)
		if len(runes) > 0 {
			m.setSearchQuery(string(runes[:len(runes)-1]))
		}
	case "ctrl+u":
		m.setSearchQuery("")
	default:
		if message.Type == tea.KeyRunes {
			m.setSearchQuery(query + string(message.Runes))
		} else if message.String() == " " {
			m.setSearchQuery(query + " ")
		}
	}
}

func (m *model) clearFilters() {
	switch m.activeView {
	case queuesView:
		m.queueSearch = ""
		m.queueStatus = "all"
		m.queueVHost = "all"
	case workersView:
		m.workerSearch = ""
		m.workerStatus = "all"
	}
}

func (m model) queueFilterText() string {
	query := searchLabel(m.queueSearch, m.searching && m.activeView == queuesView)
	return fmt.Sprintf("Search: %s  ·  Status: %s  ·  VHost: %s", query, queueStatusLabel(m.queueStatus), vhostLabel(m.queueVHost))
}

func (m model) workerFilterText() string {
	query := searchLabel(m.workerSearch, m.searching && m.activeView == workersView)
	return fmt.Sprintf("Search: %s  ·  Status: %s", query, workerStatusLabel(m.workerStatus))
}

func searchLabel(query string, editing bool) string {
	if query == "" && !editing {
		query = "all"
	}
	if editing {
		query += "▏"
	}
	return query
}

func (m model) helpText() string {
	if m.searching {
		return "Type to search   Enter apply   Esc cancel   Ctrl+U clear"
	}
	switch m.activeView {
	case queuesView:
		return "/ search   s status   v vhost   c clear filters   r refresh   q quit"
	case workersView:
		return "/ search   s status   c clear filters   r refresh   q quit"
	default:
		return "←/→ or 1–3 navigate   ↑/↓ scroll   r refresh   q quit"
	}
}

func (m model) fitRows(rows []string) []string {
	for index, row := range rows {
		rows[index] = m.fitRow(row)
	}
	return rows
}

func (m model) fitRow(row string) string {
	return lipgloss.NewStyle().MaxWidth(max(1, m.width-2)).Render(row)
}

func (m model) loadSnapshot() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		defer cancel()
		snapshot, err := m.reader.Snapshot(ctx)
		return snapshotMsg{snapshot: snapshot, err: err}
	}
}

func (m model) waitForRefresh() tea.Cmd {
	return tea.Tick(m.pollInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func sourceStatus(status string) string {
	if status == "" {
		status = "unknown"
	}
	return styledStatus(status, false)
}

func sourceCheckedAt(source models.SourceStatus) string {
	if source.CheckedAt != nil {
		return formatAge(*source.CheckedAt)
	}
	if source.LastSuccessAt != nil {
		return formatAge(*source.LastSuccessAt)
	}
	return "not collected"
}

func sourceLines(name string, source models.SourceStatus) []string {
	rows := []string{fmt.Sprintf("%s  %s  · checked %s", name, sourceStatus(source.Status), sourceCheckedAt(source))}
	if source.LastError != "" {
		rows = append(rows, mutedStyle.Render("  "+singleLine(source.LastError)))
	}
	return rows
}

func vhostFilterOptions(queues []models.QueueStatus) []string {
	options := []string{"all"}
	options = append(options, queueVHosts(queues)...)
	if hasUnknownVHost(queues) {
		options = append(options, "__unknown__")
	}
	return options
}

func vhostLabel(vhost string) string {
	switch vhost {
	case "", "all":
		return "All"
	case "__unknown__":
		return "Unknown"
	default:
		return vhost
	}
}

func queueStatusLabel(status string) string {
	switch status {
	case "critical":
		return "Critical"
	case "warning":
		return "Warning"
	case "healthy":
		return "Healthy"
	case "stale":
		return "Stale"
	case "unknown":
		return "Unknown"
	default:
		return "All"
	}
}

func workerStatusLabel(status string) string {
	switch status {
	case "healthy":
		return "Healthy"
	case "unexpected":
		return "Unexpected"
	case "stale":
		return "Stale"
	case "unknown":
		return "Unknown"
	default:
		return "All"
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func styledStatus(status string, stale bool) string {
	if status == "" {
		status = "unknown"
	}
	label := strings.ToUpper(status)
	style := mutedStyle
	if stale {
		label = "STALE · " + label
		style = warningStyle
	} else {
		switch status {
		case "up", "healthy":
			style = goodStyle
		case "warning", "not_configured":
			style = warningStyle
		case "down":
			style = badStyle
		}
	}
	return style.Render(label)
}

func formatCount(value *int64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprint(*value)
}

func formatAge(value time.Time) string {
	if value.IsZero() {
		return "not collected"
	}
	age := time.Since(value)
	if age < 0 {
		return "just now"
	}
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
	}
}

func unknownIfEmpty(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
