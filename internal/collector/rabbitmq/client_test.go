package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectReadsQueueMetricsFromEncodedVHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/api/queues/%2F" {
			t.Errorf("path = %q, want encoded default vhost", request.URL.EscapedPath())
		}
		if request.URL.Query().Get("page") != "1" || request.URL.Query().Get("page_size") != fmt.Sprint(pageSize) {
			t.Errorf("pagination query = %v", request.URL.Query())
		}
		if request.URL.Query().Get("pagination") != "true" {
			t.Errorf("pagination = %q, want true", request.URL.Query().Get("pagination"))
		}
		username, password, ok := request.BasicAuth()
		if !ok || username != "monitor" || password != "secret" {
			t.Errorf("basic auth = (%q, %q, %t), want configured credentials", username, password, ok)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"name":"jobs","node":"rabbit@dev05","state":"idle","messages":4,"messages_ready":3,"messages_unacknowledged":1,"consumers":0,"message_stats":{"publish_details":{"rate":1.5},"deliver_get_details":{"rate":0.5}}}]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "monitor", "secret", []string{"/"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	queues, err := client.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(queues) != 1 {
		t.Fatalf("queue count = %d, want 1", len(queues))
	}
	queue := queues[0]
	if queue.VHost != "/" || queue.Name != "jobs" || queue.Node != "rabbit@dev05" || queue.State != "idle" {
		t.Fatalf("queue identity/node/state = (%q, %q, %q, %q)", queue.VHost, queue.Name, queue.Node, queue.State)
	}
	if queue.Messages == nil || *queue.Messages != 4 || queue.Consumers == nil || *queue.Consumers != 0 {
		t.Fatalf("queue metrics = (%v, %v), want 4 messages and 0 consumers", queue.Messages, queue.Consumers)
	}
	if queue.PublishRate == nil || *queue.PublishRate != 1.5 || queue.DeliveryRate == nil || *queue.DeliveryRate != 0.5 {
		t.Fatalf("queue rates = (%v, %v), want 1.5 and 0.5", queue.PublishRate, queue.DeliveryRate)
	}
}

func TestCollectPaginatesWithoutPartialResults(t *testing.T) {
	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var page int
		if _, err := fmt.Sscanf(request.URL.Query().Get("page"), "%d", &page); err != nil {
			t.Errorf("parse page: %v", err)
		}
		pages = append(pages, page)
		count := 1
		if page == 1 {
			count = pageSize
		}
		items := make([]map[string]any, count)
		for index := range items {
			items[index] = map[string]any{"name": fmt.Sprintf("queue-%d-%d", page, index), "state": "running"}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"page": page, "page_size": pageSize, "page_count": 2,
			"item_count": len(items), "total_count": pageSize + 1, "items": items,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "monitor", "secret", []string{"/"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	queues, err := client.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(queues) != pageSize+1 {
		t.Fatalf("queue count = %d, want %d", len(queues), pageSize+1)
	}
	if strings.TrimSpace(fmt.Sprint(pages)) != "[1 2]" {
		t.Fatalf("requested pages = %v, want [1 2]", pages)
	}
}

func TestCollectReportsHTTPFailureWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte("sensitive response body"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "monitor", "secret", []string{"/"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("Collect() error = %v, want HTTP 401", err)
	}
	if strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error exposed response or credential data: %v", err)
	}
}

func TestCollectClassifiesMalformedSuccessfulResponsesSafely(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     string
		wantText string
	}{
		{name: "html page", body: "<html>private proxy page</html>", wantText: "returned HTML"},
		{name: "json object without queue items", body: `{"error":"private response"}`, wantText: "did not contain an items array"},
		{name: "incomplete json", body: `[{"name":"jobs"`, wantText: "ended before the data was complete"},
		{name: "invalid json", body: `[not-json]`, wantText: "invalid JSON near byte"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "monitor", "secret", []string{"/"})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.Collect(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Collect() error = %v, want %q", err, test.wantText)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "not-json") {
				t.Fatalf("error exposed response content: %v", err)
			}
		})
	}
}
