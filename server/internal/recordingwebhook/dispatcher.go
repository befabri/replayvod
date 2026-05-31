package recordingwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/videodownload"
)

const (
	defaultAttempts     = 3
	defaultTimeout      = 10 * time.Second
	defaultBackoff      = 500 * time.Millisecond
	defaultMaxBackoff   = 5 * time.Minute
	defaultConcurrency  = 4
	defaultPollInterval = 30 * time.Second
	defaultStaleTimeout = 2 * time.Minute
	defaultDrainTimeout = 8 * time.Second
	userAgent           = "ReplayVOD-Webhook/1"
)

// DeliveryResult is the synchronous outcome of a SendTest, returned to the
// dashboard so the owner sees immediately whether their receiver answered.
type DeliveryResult struct {
	OK     bool
	Status int
	Error  string
}

type deliveryStore interface {
	payloadStore
	configStore
	CreateClaimedRecordingWebhookDelivery(ctx context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error)
	ClaimDueRecordingWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]repository.RecordingWebhookDelivery, error)
	MarkRecordingWebhookDeliveryDelivered(ctx context.Context, id int64, status int, now time.Time) error
	MarkRecordingWebhookDeliveryFinal(ctx context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt time.Time, now time.Time) error
	SetRecordingWebhookDeliveryFrozenParts(ctx context.Context, id int64, frozenParts string) error
	ResetStaleRecordingWebhookDeliveries(ctx context.Context, before time.Time, now time.Time) error
	RetryRecordingWebhookDelivery(ctx context.Context, id int64, now time.Time) (*repository.RecordingWebhookDelivery, error)
	ListRecordingWebhookDeliveries(ctx context.Context, limit int) ([]repository.RecordingWebhookDelivery, error)
}

// Dispatcher drains the durable recording_webhook_deliveries outbox and POSTs
// signed payloads to the owner-configured receiver. Terminal recording paths
// insert rows transactionally with the video's terminal state; the event bus is
// now only a wake-up hint so dropped in-process events delay delivery instead of
// losing it.
type Dispatcher struct {
	svc     *Service
	store   deliveryStore
	client  *http.Client
	log     *slog.Logger
	signURL partURLSigner

	capDownloadURLsAtRetention bool

	sem    chan struct{}
	wg     sync.WaitGroup
	wakeCh chan struct{}

	stopped       chan struct{}
	deliverCtx    context.Context
	deliverCancel context.CancelFunc

	attempts     int
	timeout      time.Duration
	backoff      time.Duration
	maxBackoff   time.Duration
	pollInterval time.Duration
	staleTimeout time.Duration
	drainTimeout time.Duration
}

// NewDispatcher builds a dispatcher with production defaults. signer mints the
// signed per-part download URLs embedded in each payload; pass nil to omit them.
func NewDispatcher(repo repository.Repository, signer *videodownload.Signer, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	var signURL partURLSigner
	if signer != nil {
		signURL = signer.PartURLUntil
	}
	return &Dispatcher{
		svc:     New(repo, log),
		store:   repo,
		client:  newDeliveryClient(),
		log:     log.With("domain", "recording-webhook"),
		signURL: signURL,
		sem:     make(chan struct{}, defaultConcurrency),

		capDownloadURLsAtRetention: true,

		attempts:     defaultAttempts,
		timeout:      defaultTimeout,
		backoff:      defaultBackoff,
		maxBackoff:   defaultMaxBackoff,
		pollInterval: defaultPollInterval,
		staleTimeout: defaultStaleTimeout,
		drainTimeout: defaultDrainTimeout,
	}
}

// SetRetentionDownloadURLCapEnabled controls whether completed recording
// payloads cap signed URL expiry to the recording's retention deadline. Main
// disables this when the retention sweep task itself is disabled, because the
// bytes will not be auto-deleted at that schedule deadline.
func (d *Dispatcher) SetRetentionDownloadURLCapEnabled(enabled bool) {
	d.capDownloadURLsAtRetention = enabled
}

// newDeliveryClient builds the HTTP client used for every delivery. It refuses
// redirects so an accepted URL cannot bounce a POST to a different host.
func newDeliveryClient() *http.Client {
	return &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// RecentDeliveries returns durable delivery history newest-first for the owner
// dashboard.
func (d *Dispatcher) RecentDeliveries(ctx context.Context) ([]DeliveryRecord, error) {
	rows, err := d.store.ListRecordingWebhookDeliveries(ctx, 50)
	if err != nil {
		return nil, err
	}
	out := make([]DeliveryRecord, len(rows))
	for i, row := range rows {
		out[i] = deliveryRecordFromRow(row)
	}
	return out, nil
}

// RetryDelivery re-queues a failed/rejected delivery (due now) and wakes the
// poller. A delivery that is missing or not in a retryable state yields
// ErrDeliveryNotRetryable, so a caller can't reset a delivered or in-flight row
// into a duplicate send.
func (d *Dispatcher) RetryDelivery(ctx context.Context, id int64) (DeliveryRecord, error) {
	row, err := d.store.RetryRecordingWebhookDelivery(ctx, id, time.Now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return DeliveryRecord{}, ErrDeliveryNotRetryable
	}
	if err != nil {
		return DeliveryRecord{}, err
	}
	d.wake()
	return deliveryRecordFromRow(*row), nil
}

// Start launches the poller and, when available, subscribes to terminal
// recording events as wake-up hints. The subscription still happens
// synchronously before Start returns, preserving the boot ordering guarantee.
func (d *Dispatcher) Start(ctx context.Context, bus *eventbus.Buses) {
	d.stopped = make(chan struct{})
	d.deliverCtx, d.deliverCancel = context.WithCancel(context.Background())
	d.wakeCh = make(chan struct{}, 1)

	var loops sync.WaitGroup
	loops.Add(1)
	go func() {
		defer loops.Done()
		d.pollLoop(ctx)
	}()

	if bus != nil && bus.RecordingTerminal != nil {
		events := bus.RecordingTerminal.Subscribe(ctx)
		loops.Add(1)
		go func() {
			defer loops.Done()
			d.wakeLoop(ctx, events)
		}()
	}

	go func() {
		loops.Wait()
		close(d.stopped)
	}()
	d.wake()
}

func (d *Dispatcher) wake() {
	if d.wakeCh == nil {
		return
	}
	select {
	case d.wakeCh <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) wakeLoop(ctx context.Context, events <-chan eventbus.RecordingTerminalEvent) {
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
			d.wake()
		case <-ctx.Done():
			return
		}
	}
}

func (d *Dispatcher) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()
	d.drainDue(ctx)
	for {
		select {
		case <-ticker.C:
			d.drainDue(ctx)
		case <-d.wakeCh:
			d.drainDue(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (d *Dispatcher) drainDue(ctx context.Context) {
	now := time.Now().UTC()
	if err := d.store.ResetStaleRecordingWebhookDeliveries(ctx, now.Add(-d.staleTimeout), now); err != nil && ctx.Err() == nil {
		d.log.Warn("reset stale recording webhook deliveries", "error", err)
	}
	for {
		select {
		case d.sem <- struct{}{}:
		default:
			return
		}

		rows, err := d.store.ClaimDueRecordingWebhookDeliveries(ctx, time.Now().UTC(), 1)
		if err != nil {
			<-d.sem
			if ctx.Err() == nil {
				d.log.Warn("claim recording webhook delivery", "error", err)
			}
			return
		}
		if len(rows) == 0 {
			<-d.sem
			return
		}
		row := rows[0]
		d.wg.Go(func() {
			defer func() { <-d.sem }()
			d.deliverClaimed(d.deliverCtx, row)
		})
	}
}

// Wait blocks until accept loops stop and in-flight deliveries finish or the
// drain timeout elapses, at which point stragglers are cancelled.
func (d *Dispatcher) Wait() {
	if d.stopped == nil {
		return
	}
	<-d.stopped
	defer d.deliverCancel()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d.drainTimeout):
		d.log.Warn("recording webhook: in-flight deliveries did not drain; cancelling", "timeout", d.drainTimeout)
		d.deliverCancel()
		<-done
	}
}

func (d *Dispatcher) deliverClaimed(ctx context.Context, row repository.RecordingWebhookDelivery) {
	// Build (and freeze) the body BEFORE the config gate. A delivery whose
	// webhook config is disabled or incomplete at this first attempt is marked
	// failed below, but the snapshot still captures its parts, so a later manual
	// retry (after the operator fixes the config) resends the real part list even
	// if retention has since deleted the parts. bodyForDelivery needs the store
	// and signer, not the config, so it is safe to run first.
	body, eventID, err := d.bodyForDelivery(ctx, row)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			d.markFinal(ctx, row, repository.RecordingWebhookDeliveryFailed, 0, describeErr(err), time.Now().UTC())
			return
		}
		d.retryOrFail(ctx, row, 0, err)
		return
	}

	cfg, err := d.svc.Get(ctx)
	if err != nil {
		d.retryOrFail(ctx, row, 0, fmt.Errorf("load webhook config: %w", err))
		return
	}
	if cfg.URL == "" || cfg.Secret == "" || (!row.Test && !cfg.Enabled) {
		d.markFinal(ctx, row, repository.RecordingWebhookDeliveryFailed, 0, "webhook disabled or incomplete before delivery", time.Now().UTC())
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	signature := sign(cfg.Secret, row.MessageID, timestamp, body)
	status, perr := d.post(ctx, cfg.URL, eventID, row.MessageID, timestamp, signature, body)
	d.finishAttempt(ctx, row, status, perr, cfg.URL)
}

func (d *Dispatcher) bodyForDelivery(ctx context.Context, row repository.RecordingWebhookDelivery) ([]byte, string, error) {
	if row.Test {
		body, err := json.Marshal(Payload{Version: PayloadVersion, Event: EventTest, Test: true, Parts: []PayloadPart{}})
		return body, EventTest, err
	}
	// Part metadata is frozen on the first build, while the video's parts still
	// exist. Later attempts rebuild from the frozen list, so a receiver that
	// recovers after retention deleted the parts still gets the real part list;
	// the signed download URLs are re-minted fresh each attempt, so a late retry
	// never ships a URL that expired at enqueue time.
	var frozen []PayloadPart
	if row.FrozenParts != "" {
		if err := json.Unmarshal([]byte(row.FrozenParts), &frozen); err != nil {
			return nil, row.Event, fmt.Errorf("decode frozen parts for delivery %d: %w", row.ID, err)
		}
	}
	payload, err := buildPayload(ctx, d.store, d.signURL, row.Event, row.VideoID, frozen, d.capDownloadURLsAtRetention)
	if err != nil {
		return nil, row.Event, err
	}
	if row.FrozenParts == "" {
		// First build: snapshot the part metadata (URLs stripped) for retries before
		// any POST can escape. If the snapshot cannot be saved, fail this attempt so
		// a later retry can freeze parts while they still exist instead of widening
		// the retention race.
		raw, merr := json.Marshal(stripPartURLs(payload.Parts))
		if merr != nil {
			return nil, row.Event, fmt.Errorf("marshal frozen parts for delivery %d: %w", row.ID, merr)
		}
		if err := d.store.SetRecordingWebhookDeliveryFrozenParts(ctx, row.ID, string(raw)); err != nil {
			return nil, row.Event, fmt.Errorf("persist frozen parts for delivery %d: %w", row.ID, err)
		}
	}
	body, err := json.Marshal(payload)
	return body, row.Event, err
}

func (d *Dispatcher) finishAttempt(ctx context.Context, row repository.RecordingWebhookDelivery, status int, err error, target string) {
	now := time.Now().UTC()
	switch {
	case err == nil && status >= 200 && status < 300:
		if markErr := d.store.MarkRecordingWebhookDeliveryDelivered(ctx, row.ID, status, now); markErr != nil {
			d.log.Warn("mark recording webhook delivered", "delivery_id", row.ID, "error", markErr)
		}
		d.log.Debug("recording webhook delivered", "delivery_id", row.ID, "event", row.Event, "video_id", row.VideoID, "status", status)
	case err == nil && !retryableStatus(status):
		if markErr := d.markFinal(ctx, row, repository.RecordingWebhookDeliveryRejected, status, fmt.Sprintf("HTTP %d", status), now); markErr == nil {
			d.log.Warn("recording webhook rejected", "delivery_id", row.ID, "event", row.Event, "video_id", row.VideoID, "status", status, "target", safeURL(target))
		}
	default:
		d.retryOrFail(ctx, row, status, err)
	}
}

func (d *Dispatcher) retryOrFail(ctx context.Context, row repository.RecordingWebhookDelivery, status int, err error) {
	now := time.Now().UTC()
	errMsg := describeDeliveryFailure(status, row.Attempts, err)
	if row.Attempts >= d.attempts {
		_ = d.markFinal(ctx, row, repository.RecordingWebhookDeliveryFailed, status, errMsg, now)
		d.log.Warn("recording webhook delivery failed", "delivery_id", row.ID, "event", row.Event, "video_id", row.VideoID, "attempts", row.Attempts, "status", status, "error", errMsg)
		return
	}
	next := now.Add(d.backoffForAttempt(row.Attempts))
	if markErr := d.markFinal(ctx, row, repository.RecordingWebhookDeliveryPending, status, errMsg, next); markErr != nil {
		d.log.Warn("schedule recording webhook retry", "delivery_id", row.ID, "error", markErr)
	}
}

func (d *Dispatcher) markFinal(ctx context.Context, row repository.RecordingWebhookDelivery, status string, httpStatus int, errMsg string, nextAttemptAt time.Time) error {
	err := d.store.MarkRecordingWebhookDeliveryFinal(ctx, row.ID, status, httpStatus, errMsg, nextAttemptAt, time.Now().UTC())
	if err != nil {
		d.log.Warn("mark recording webhook delivery", "delivery_id", row.ID, "status", status, "error", err)
	}
	return err
}

func (d *Dispatcher) backoffForAttempt(attempt int) time.Duration {
	if attempt <= 1 {
		return d.backoff
	}
	delay := d.backoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= d.maxBackoff {
			return d.maxBackoff
		}
	}
	return delay
}

// SendTest delivers a one-off signed test payload to the configured receiver in
// a single attempt and records the outcome in the durable delivery table.
func (d *Dispatcher) SendTest(ctx context.Context) DeliveryResult {
	cfg, err := d.svc.Get(ctx)
	if err != nil {
		return DeliveryResult{Error: "failed to load webhook config"}
	}
	if cfg.URL == "" {
		return DeliveryResult{Error: "no webhook URL is configured"}
	}
	if cfg.Secret == "" {
		cfg, err = d.svc.EnsureSecret(ctx)
		if err != nil {
			return DeliveryResult{Error: "failed to create signing secret"}
		}
	}
	input, err := newTestDeliveryInput(time.Now().UTC())
	if err != nil {
		return DeliveryResult{Error: "failed to mint message id"}
	}
	// Create the row already claimed ('delivering'): SendTest delivers it
	// synchronously below, so it must not also be visible to the poller's
	// claim, or a concurrent drain could double-send the test.
	row, err := d.store.CreateClaimedRecordingWebhookDelivery(ctx, input)
	if err != nil {
		d.log.Warn("record test webhook delivery", "error", err)
		return DeliveryResult{Error: "failed to record test delivery"}
	}
	body, eventID, err := d.bodyForDelivery(ctx, *row)
	if err != nil {
		return DeliveryResult{Error: "failed to build test payload"}
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	signature := sign(cfg.Secret, row.MessageID, timestamp, body)
	status, perr := d.post(ctx, cfg.URL, eventID, row.MessageID, timestamp, signature, body)

	outcome := classifyOutcome(status, perr)
	switch outcome {
	case OutcomeDelivered:
		_ = d.store.MarkRecordingWebhookDeliveryDelivered(ctx, row.ID, status, time.Now().UTC())
	case OutcomeRejected:
		_ = d.markFinal(ctx, *row, repository.RecordingWebhookDeliveryRejected, status, fmt.Sprintf("HTTP %d", status), time.Now().UTC())
	default:
		_ = d.markFinal(ctx, *row, repository.RecordingWebhookDeliveryFailed, status, describeDeliveryFailure(status, 1, perr), time.Now().UTC())
	}
	errMsg := ""
	if outcome != OutcomeDelivered {
		errMsg = describeDeliveryFailure(status, 1, perr)
		if outcome == OutcomeRejected {
			errMsg = fmt.Sprintf("HTTP %d", status)
		}
	}
	return DeliveryResult{OK: outcome == OutcomeDelivered, Status: status, Error: errMsg}
}

// post sends one delivery attempt with its own short timeout, returning the
// status code (0 on transport error).
func (d *Dispatcher) post(ctx context.Context, target, eventID, id, timestamp, signature string, body []byte) (int, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(HeaderEvent, eventID)
	req.Header.Set(HeaderID, id)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, signature)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// classifyOutcome maps a final (status, err) to a DeliveryOutcome.
func classifyOutcome(status int, err error) DeliveryOutcome {
	switch {
	case err != nil:
		return OutcomeFailed
	case status >= 200 && status < 300:
		return OutcomeDelivered
	case retryableStatus(status):
		return OutcomeFailed
	default:
		return OutcomeRejected
	}
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func describeDeliveryFailure(status, attempts int, err error) string {
	if err != nil {
		return describeErr(err)
	}
	if status > 0 {
		return fmt.Sprintf("HTTP %d after %d attempts", status, attempts)
	}
	return "delivery failed"
}

// describeErr renders a delivery error without leaking the target URL's
// credentials. A *url.Error embeds the full request URL (including any userinfo)
// in its own Error(); the wrapped Err does not, so surface that instead.
func describeErr(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

// safeURL returns just the scheme://host/path of target, dropping userinfo,
// query, and fragment before logging.
func safeURL(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.Path
}
