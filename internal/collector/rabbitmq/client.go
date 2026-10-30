package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"moitoring/internal/models"
)

const pageSize = 100
const maxQueueResponseBytes = 32 << 20

type Client struct {
	baseURL  *url.URL
	username string
	password string
	vhosts   []string
	http     *http.Client
}

func NewClient(baseURL, username, password string, vhosts []string) (*Client, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, errors.New("RabbitMQ URL must be an absolute HTTP or HTTPS URL")
	}
	if len(vhosts) == 0 {
		return nil, errors.New("at least one RabbitMQ virtual host is required")
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	return &Client{
		baseURL:  parsedURL,
		username: username,
		password: password,
		vhosts:   append([]string(nil), vhosts...),
		http:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *Client) Collect(ctx context.Context) ([]models.QueueObservation, error) {
	queues := make([]models.QueueObservation, 0)
	seen := make(map[string]struct{})

	for _, vhost := range c.vhosts {
		for page := 1; ; page++ {
			items, err := c.fetchPage(ctx, vhost, page)
			if err != nil {
				return nil, err
			}
			newItems := 0
			for _, item := range items {
				item.VHost = vhost
				key := vhost + "\x00" + item.Name
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				queues = append(queues, item)
				newItems++
			}
			if len(items) < pageSize {
				break
			}
			if newItems == 0 {
				return nil, errors.New("RabbitMQ queue listing did not advance to a new page")
			}
		}
	}
	return queues, nil
}

func (c *Client) fetchPage(ctx context.Context, vhost string, page int) ([]models.QueueObservation, error) {
	basePath := strings.TrimRight(c.baseURL.EscapedPath(), "/")
	rawPath := basePath + "/api/queues/" + url.PathEscape(vhost)
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, errors.New("invalid RabbitMQ virtual host path")
	}
	endpoint := *c.baseURL
	endpoint.Path = decodedPath
	endpoint.RawPath = rawPath
	query := endpoint.Query()
	query.Set("page", fmt.Sprint(page))
	query.Set("page_size", fmt.Sprint(pageSize))
	query.Set("pagination", "true")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("could not create RabbitMQ request")
	}
	request.SetBasicAuth(c.username, c.password)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, errors.New("RabbitMQ management API request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RabbitMQ management API returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxQueueResponseBytes+1))
	if err != nil {
		return nil, errors.New("could not read RabbitMQ queue response")
	}
	if len(body) > maxQueueResponseBytes {
		return nil, fmt.Errorf("RabbitMQ queue page exceeded the %d MiB response limit", maxQueueResponseBytes>>20)
	}
	var items []queueResponse
	if err := decodeQueuePage(body, &items); err != nil {
		return nil, err
	}
	queues := make([]models.QueueObservation, 0, len(items))
	for _, item := range items {
		if item.Name == "" {
			return nil, errors.New("RabbitMQ management API returned a queue without a name")
		}
		queues = append(queues, models.QueueObservation{
			QueueKey:        models.QueueKey{Name: item.Name},
			Node:            item.Node,
			State:           strings.ToLower(strings.TrimSpace(item.State)),
			Messages:        item.Messages,
			MessagesReady:   item.MessagesReady,
			MessagesUnacked: item.MessagesUnacked,
			Consumers:       item.Consumers,
			PublishRate:     item.MessageStats.PublishDetails.rate(),
			DeliveryRate:    item.MessageStats.DeliverGetDetails.rate(),
			ObservedAt:      time.Now().UTC(),
		})
	}
	return queues, nil
}

func decodeQueuePage(body []byte, items *[]queueResponse) error {
	trimmedBody := bytes.TrimSpace(body)
	if len(trimmedBody) == 0 {
		return errors.New("RabbitMQ management API returned an empty queue response")
	}
	queueList := trimmedBody
	switch trimmedBody[0] {
	case '[':
	case '{':
		var page struct {
			Items json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(trimmedBody, &page); err != nil {
			return invalidQueueJSON(err)
		}
		if len(page.Items) == 0 {
			return errors.New("RabbitMQ management API JSON page did not contain an items array")
		}
		queueList = bytes.TrimSpace(page.Items)
		if len(queueList) == 0 || queueList[0] != '[' {
			return errors.New("RabbitMQ management API JSON page items were not an array")
		}
	case '<':
		return errors.New("RabbitMQ management API returned HTML instead of a queue list; verify the API URL or proxy")
	default:
		return errors.New("RabbitMQ management API returned an unexpected queue response format")
	}
	if err := json.Unmarshal(queueList, items); err != nil {
		return invalidQueueJSON(err)
	}
	return nil
}

func invalidQueueJSON(err error) error {
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		if strings.Contains(syntaxError.Error(), "unexpected end") {
			return errors.New("RabbitMQ management API queue response ended before the data was complete")
		}
		return fmt.Errorf("RabbitMQ management API queue response has invalid JSON near byte %d", syntaxError.Offset)
	}
	return errors.New("RabbitMQ management API queue response did not match the expected format")
}

type queueResponse struct {
	Name            string       `json:"name"`
	Node            string       `json:"node"`
	State           string       `json:"state"`
	Messages        *int64       `json:"messages"`
	MessagesReady   *int64       `json:"messages_ready"`
	MessagesUnacked *int64       `json:"messages_unacknowledged"`
	Consumers       *int64       `json:"consumers"`
	MessageStats    messageStats `json:"message_stats"`
}

type messageStats struct {
	PublishDetails    *rateDetails `json:"publish_details"`
	DeliverGetDetails *rateDetails `json:"deliver_get_details"`
}

type rateDetails struct {
	Rate *float64 `json:"rate"`
}

func (r *rateDetails) rate() *float64 {
	if r == nil {
		return nil
	}
	return r.Rate
}
