package models

import "time"

type QueueKey struct {
	VHost string `json:"vhost"`
	Name  string `json:"name"`
}

type QueueObservation struct {
	QueueKey
	Node            string    `json:"node"`
	State           string    `json:"state"`
	Status          string    `json:"-"`
	Messages        *int64    `json:"messages"`
	MessagesReady   *int64    `json:"messages_ready"`
	MessagesUnacked *int64    `json:"messages_unacknowledged"`
	Consumers       *int64    `json:"consumers"`
	PublishRate     *float64  `json:"publish_rate"`
	DeliveryRate    *float64  `json:"delivery_rate"`
	ObservedAt      time.Time `json:"observed_at"`
}

type QueueStatus struct {
	QueueKey
	Node            string    `json:"node"`
	State           string    `json:"state"`
	Status          string    `json:"status"`
	Messages        *int64    `json:"messages"`
	MessagesReady   *int64    `json:"messages_ready"`
	MessagesUnacked *int64    `json:"messages_unacknowledged"`
	Consumers       *int64    `json:"consumers"`
	PublishRate     *float64  `json:"publish_rate"`
	DeliveryRate    *float64  `json:"delivery_rate"`
	ObservedAt      time.Time `json:"observed_at"`
	IsStale         bool      `json:"is_stale"`
}

type SourceStatus struct {
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	FailedEmails   int        `json:"failed_emails"`
	LastEmailError string     `json:"last_email_error,omitempty"`
}

type MonitoringSnapshot struct {
	Source       SourceStatus   `json:"source"`
	WorkerSource SourceStatus   `json:"worker_source"`
	Queues       []QueueStatus  `json:"queues"`
	Workers      []WorkerStatus `json:"workers"`
}

type WorkerTarget struct {
	Host          string `json:"host"`
	Unit          string `json:"unit"`
	ExpectedState string `json:"expected_state"`
}

type WorkerObservation struct {
	Host            string    `json:"host"`
	Unit            string    `json:"unit"`
	CollectionError string    `json:"-"`
	LoadState       string    `json:"load_state"`
	ExpectedState   string    `json:"expected_state"`
	ActiveState     string    `json:"active_state"`
	SubState        string    `json:"sub_state"`
	Result          string    `json:"result"`
	ExecMainCode    string    `json:"exec_main_code"`
	ExecMainStatus  string    `json:"exec_main_status"`
	StatusText      string    `json:"status_text"`
	ObservedAt      time.Time `json:"observed_at"`
	Status          string    `json:"-"`
}

type WorkerStatus struct {
	WorkerObservation
	Status  string `json:"status"`
	IsStale bool   `json:"is_stale"`
}

type Notification struct {
	ID         int64
	Recipients []string
	Subject    string
	Body       string
	Attempts   int
	ClaimToken string
}
