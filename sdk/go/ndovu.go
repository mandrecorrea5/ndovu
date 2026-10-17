package ndovu

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const sdkVersion = "1.0.0-go"

var redactKeys = regexp.MustCompile(`(?i)password|senha|token|secret|authorization|cvv|card|cart[aã]o`)

// Config configures a Ndovu ingestion client.
type Config struct {
	Endpoint        string
	APIKey          string
	App             string
	HTTPClient      *http.Client
	FlushInterval   time.Duration
	RequestTimeout  time.Duration
	MaxBatch        int
	Release         string
	ServiceInstance string
	OnError         func(error)
}

// Session identifies the user/session associated with an event.
type Session struct {
	SessionID  string         `json:"sessionId"`
	UserID     string         `json:"userId,omitempty"`
	UserAgent  string         `json:"userAgent,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// EventOptions contains optional metadata shared by event methods.
type EventOptions struct {
	Feature  string
	Screen   string
	Duration time.Duration
	Metadata map[string]any
	Session  *Session
}

// ErrorOptions describes an error event.
type ErrorOptions struct {
	EventOptions
	Code    string
	Message string
	Body    any
}

// HTTPRequestOptions describes an HTTP request event.
type HTTPRequestOptions struct {
	EventOptions
	Method       string
	URL          string
	StatusCode   int
	RequestBody  any
	ResponseBody any
}

// Event is one Ndovu v1 event.
type Event struct {
	EventID    string         `json:"eventId"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Feature    string         `json:"feature,omitempty"`
	Screen     string         `json:"screen,omitempty"`
	Timestamp  string         `json:"timestamp"`
	DurationMs int64          `json:"durationMs,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	HTTP       *HTTPDetails   `json:"http,omitempty"`
	Error      *ErrorDetails  `json:"error,omitempty"`
}

// HTTPDetails is the HTTP payload attached to an http_request event.
type HTTPDetails struct {
	Method       string `json:"method,omitempty"`
	URL          string `json:"url,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	RequestBody  any    `json:"requestBody,omitempty"`
	ResponseBody any    `json:"responseBody,omitempty"`
}

// ErrorDetails is the error payload attached to an error event.
type ErrorDetails struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Body    any    `json:"body,omitempty"`
}

type envelope struct {
	App        string  `json:"app"`
	SDKVersion string  `json:"sdkVersion"`
	Session    Session `json:"session"`
	Events     []Event `json:"events"`
}

type queuedEvent struct {
	session Session
	event   Event
}

// Client sends batched events to Ndovu.
type Client struct {
	endpoint        string
	apiKey          string
	app             string
	httpClient      *http.Client
	flushInterval   time.Duration
	requestTimeout  time.Duration
	maxBatch        int
	release         string
	serviceInstance string
	onError         func(error)
	processSession  Session

	mu         sync.Mutex
	flushMu    sync.Mutex
	queue      []queuedEvent
	closed     bool
	cancel     context.CancelFunc
	workerDone chan struct{}
	closeOnce  sync.Once
}

// New creates a client and starts its periodic batch flusher.
func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.Endpoint) == "" || config.APIKey == "" || config.App == "" {
		return nil, errors.New("ndovu: endpoint, APIKey e App são obrigatórios")
	}
	u, err := url.Parse(config.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("ndovu: Endpoint deve ser uma URL http(s) válida, sem query ou fragment")
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 10 * time.Second
	}
	if config.MaxBatch <= 0 {
		config.MaxBatch = 20
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.ServiceInstance == "" {
		config.ServiceInstance, _ = os.Hostname()
	}
	if config.OnError == nil {
		config.OnError = func(err error) { log.Printf("ndovu: background flush failed: %v", err) }
	}
	sessionID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("ndovu: generate process session id: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		endpoint:        strings.TrimRight(config.Endpoint, "/"),
		apiKey:          config.APIKey,
		app:             config.App,
		httpClient:      config.HTTPClient,
		flushInterval:   config.FlushInterval,
		requestTimeout:  config.RequestTimeout,
		maxBatch:        config.MaxBatch,
		release:         config.Release,
		serviceInstance: config.ServiceInstance,
		onError:         config.OnError,
		processSession: Session{
			SessionID: sessionID,
			UserAgent: "go",
			Attributes: map[string]any{
				"serviceInstance": config.ServiceInstance,
			},
		},
		cancel:     cancel,
		workerDone: make(chan struct{}),
	}
	if config.Release != "" {
		client.processSession.Attributes["release"] = config.Release
	}
	go client.run(ctx)
	return client, nil
}

// PageView records a page_view event.
func (c *Client) PageView(screen string, options EventOptions) error {
	if options.Screen == "" {
		options.Screen = screen
	}
	return c.record("page_view", "tela_"+screen, options, nil, nil)
}

// Action records an action event.
func (c *Client) Action(name string, options EventOptions) error {
	return c.record("action", name, options, nil, nil)
}

// Custom records a custom event.
func (c *Client) Custom(name string, options EventOptions) error {
	return c.record("custom", name, options, nil, nil)
}

// Error records an error event.
func (c *Client) Error(name string, options ErrorOptions) error {
	body, err := redactJSON(options.Body)
	if err != nil {
		return fmt.Errorf("ndovu: sanitize error body: %w", err)
	}
	details := &ErrorDetails{
		Code:    options.Code,
		Message: options.Message,
		Body:    body,
	}
	return c.record("error", name, options.EventOptions, nil, details)
}

// HTTPRequest records an http_request event.
func (c *Client) HTTPRequest(name string, options HTTPRequestOptions) error {
	requestBody, err := redactJSON(options.RequestBody)
	if err != nil {
		return fmt.Errorf("ndovu: sanitize HTTP request body: %w", err)
	}
	responseBody, err := redactJSON(options.ResponseBody)
	if err != nil {
		return fmt.Errorf("ndovu: sanitize HTTP response body: %w", err)
	}
	details := &HTTPDetails{
		Method:       options.Method,
		URL:          options.URL,
		StatusCode:   options.StatusCode,
		RequestBody:  requestBody,
		ResponseBody: responseBody,
	}
	return c.record("http_request", name, options.EventOptions, details, nil)
}

// Flush synchronously sends queued events. On failure, unsent events remain queued.
func (c *Client) Flush(ctx context.Context) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()

	c.mu.Lock()
	pending := c.queue
	c.queue = nil
	c.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}

	for start := 0; start < len(pending); {
		session := pending[start].session
		end := start
		for end < len(pending) && end-start < c.maxBatch && sameSession(session, pending[end].session) {
			end++
		}
		events := make([]Event, 0, end-start)
		for _, item := range pending[start:end] {
			events = append(events, item.event)
		}
		if err := c.send(ctx, session, events); err != nil {
			c.mu.Lock()
			c.queue = append(pending[start:], c.queue...)
			c.mu.Unlock()
			return err
		}
		start = end
	}
	return nil
}

// Close stops periodic flushing and attempts a final synchronous flush.
func (c *Client) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.cancel()
	})
	select {
	case <-c.workerDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.Flush(ctx)
}

func (c *Client) record(eventType, name string, options EventOptions, httpDetails *HTTPDetails, errorDetails *ErrorDetails) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("ndovu: event name is required")
	}
	eventID, err := newUUID()
	if err != nil {
		return fmt.Errorf("ndovu: generate event id: %w", err)
	}
	session := c.processSession
	if options.Session != nil {
		session = *options.Session
	}
	if session.SessionID == "" {
		return errors.New("ndovu: session id is required")
	}
	if session.Attributes != nil {
		attributes, err := redactJSON(session.Attributes)
		if err != nil {
			return fmt.Errorf("ndovu: sanitize session attributes: %w", err)
		}
		session.Attributes, _ = attributes.(map[string]any)
	}
	if c.serviceInstance != "" && session.Attributes == nil {
		session.Attributes = map[string]any{"serviceInstance": c.serviceInstance}
	}
	if c.release != "" {
		if session.Attributes == nil {
			session.Attributes = make(map[string]any)
		}
		if _, exists := session.Attributes["release"]; !exists {
			session.Attributes["release"] = c.release
		}
	}
	metadata, err := redactJSON(options.Metadata)
	if err != nil {
		return fmt.Errorf("ndovu: sanitize metadata: %w", err)
	}
	redactedMetadata, _ := metadata.(map[string]any)

	var durationMs int64
	if options.Duration > 0 {
		durationMs = options.Duration.Milliseconds()
	}
	event := Event{
		EventID:    eventID,
		Type:       eventType,
		Name:       name,
		Feature:    options.Feature,
		Screen:     options.Screen,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		DurationMs: durationMs,
		Metadata:   redactedMetadata,
		HTTP:       httpDetails,
		Error:      errorDetails,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("ndovu: client is closed")
	}
	c.queue = append(c.queue, queuedEvent{session: session, event: event})
	return nil
}

func (c *Client) send(ctx context.Context, session Session, events []Event) error {
	env := envelope{App: c.app, SDKVersion: sdkVersion, Session: session, Events: events}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("ndovu: encode events: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ndovu: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ndovu: send events: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ndovu: ingestion rejected (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (c *Client) run(ctx context.Context) {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()
	defer close(c.workerDone)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
			err := c.Flush(ctx)
			cancel()
			if err != nil {
				c.onError(err)
			}
		}
	}
}

func sameSession(a, b Session) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func redactJSON(input any) (any, error) {
	if input == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return redactJSONValue(value), nil
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if redactKeys.MatchString(key) {
				result[key] = "***"
			} else {
				result[key] = redactJSONValue(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = redactJSONValue(child)
		}
		return result
	default:
		return value
	}
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded), nil
}
