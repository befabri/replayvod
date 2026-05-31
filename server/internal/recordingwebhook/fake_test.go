package recordingwebhook

import (
	"context"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// fakeRepo implements both configStore and payloadStore for the package tests,
// holding canned rows and capturing the last webhook upsert. A typed fake keeps
// the tests from standing up a real database for logic that does not need one.
type fakeRepo struct {
	mu sync.Mutex

	settings    *repository.ServerSettings
	settingsErr error

	video    *repository.Video
	videoErr error

	parts    []repository.VideoPart
	partsErr error

	channel    *repository.Channel
	channelErr error

	categories map[int64]repository.Category

	// upsertCalls records every UpsertRecordingWebhookConfig argument set so a
	// test can assert what config was persisted.
	upsertCalls []upsertCall
	// ensureCalls / setCalls count the two secret writes so a test can assert
	// the right one fired (CAS-seed vs unconditional-rotate).
	ensureCalls int
	setCalls    int

	nextDeliveryID int64
	deliveries     []repository.RecordingWebhookDelivery
}

type upsertCall struct {
	enabled bool
	url     string
	events  string
}

func (f *fakeRepo) GetServerSettings(_ context.Context) (*repository.ServerSettings, error) {
	if f.settingsErr != nil {
		return nil, f.settingsErr
	}
	return f.settings, nil
}

func (f *fakeRepo) UpsertRecordingWebhookConfig(_ context.Context, enabled bool, url, events string) (*repository.ServerSettings, error) {
	f.upsertCalls = append(f.upsertCalls, upsertCall{enabled: enabled, url: url, events: events})
	// Mirror the real query: only the webhook config columns change, the secret
	// and everything else on the row are preserved.
	f.ensureRow()
	f.settings.RecordingWebhookEnabled = enabled
	f.settings.RecordingWebhookURL = url
	f.settings.RecordingWebhookEvents = events
	return f.settings, nil
}

// EnsureRecordingWebhookSecret models the CAS query: the secret is written only
// when the slot is still empty.
func (f *fakeRepo) EnsureRecordingWebhookSecret(_ context.Context, secret string) error {
	f.ensureCalls++
	f.ensureRow()
	if f.settings.RecordingWebhookSecret == "" {
		f.settings.RecordingWebhookSecret = secret
	}
	return nil
}

// SetRecordingWebhookSecret models the unconditional rotate query.
func (f *fakeRepo) SetRecordingWebhookSecret(_ context.Context, secret string) error {
	f.setCalls++
	f.ensureRow()
	f.settings.RecordingWebhookSecret = secret
	return nil
}

// ensureRow lazily materializes the single settings row the way the upsert
// queries create id=1 on first write.
func (f *fakeRepo) ensureRow() {
	if f.settings == nil {
		f.settings = &repository.ServerSettings{}
	}
}

func (f *fakeRepo) GetVideo(_ context.Context, _ int64) (*repository.Video, error) {
	if f.videoErr != nil {
		return nil, f.videoErr
	}
	return f.video, nil
}

func (f *fakeRepo) ListVideoParts(_ context.Context, _ int64) ([]repository.VideoPart, error) {
	if f.partsErr != nil {
		return nil, f.partsErr
	}
	return f.parts, nil
}

func (f *fakeRepo) GetChannel(_ context.Context, _ string) (*repository.Channel, error) {
	if f.channelErr != nil {
		return nil, f.channelErr
	}
	return f.channel, nil
}

func (f *fakeRepo) ListPrimaryCategoriesForVideos(_ context.Context, _ []int64) (map[int64]repository.Category, error) {
	return f.categories, nil
}

func (f *fakeRepo) lastUpsert() (upsertCall, bool) {
	if len(f.upsertCalls) == 0 {
		return upsertCall{}, false
	}
	return f.upsertCalls[len(f.upsertCalls)-1], true
}

func (f *fakeRepo) CreateRecordingWebhookDelivery(_ context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deliveries {
		if f.deliveries[i].DedupeKey == input.DedupeKey {
			return cloneDelivery(f.deliveries[i]), nil
		}
	}
	f.nextDeliveryID++
	now := input.NextAttemptAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := repository.RecordingWebhookDelivery{
		ID:            f.nextDeliveryID,
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		Status:        repository.RecordingWebhookDeliveryPending,
		Test:          input.Test,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	f.deliveries = append(f.deliveries, row)
	return cloneDelivery(row), nil
}

// CreateClaimedRecordingWebhookDelivery models the SQL that inserts a row
// already in 'delivering' (one attempt counted), so the poller's claim — which
// only picks 'pending' — never also delivers it.
func (f *fakeRepo) CreateClaimedRecordingWebhookDelivery(_ context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deliveries {
		if f.deliveries[i].DedupeKey == input.DedupeKey {
			return cloneDelivery(f.deliveries[i]), nil
		}
	}
	f.nextDeliveryID++
	now := input.NextAttemptAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := repository.RecordingWebhookDelivery{
		ID:            f.nextDeliveryID,
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		Status:        repository.RecordingWebhookDeliveryDelivering,
		Attempts:      1,
		Test:          input.Test,
		NextAttemptAt: now,
		LastAttemptAt: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	f.deliveries = append(f.deliveries, row)
	return cloneDelivery(row), nil
}

func (f *fakeRepo) ClaimDueRecordingWebhookDeliveries(_ context.Context, now time.Time, limit int) ([]repository.RecordingWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]repository.RecordingWebhookDelivery, 0, limit)
	for i := range f.deliveries {
		if len(out) >= limit {
			break
		}
		if f.deliveries[i].Status != repository.RecordingWebhookDeliveryPending || f.deliveries[i].NextAttemptAt.After(now) {
			continue
		}
		f.deliveries[i].Status = repository.RecordingWebhookDeliveryDelivering
		f.deliveries[i].Attempts++
		f.deliveries[i].LastAttemptAt = &now
		f.deliveries[i].UpdatedAt = now
		out = append(out, f.deliveries[i])
	}
	return out, nil
}

func (f *fakeRepo) MarkRecordingWebhookDeliveryDelivered(_ context.Context, id int64, status int, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.deliveryIndex(id)
	if err != nil {
		return err
	}
	f.deliveries[i].Status = repository.RecordingWebhookDeliveryDelivered
	f.deliveries[i].LastStatus = status
	f.deliveries[i].LastError = ""
	f.deliveries[i].DeliveredAt = &now
	f.deliveries[i].UpdatedAt = now
	return nil
}

func (f *fakeRepo) MarkRecordingWebhookDeliveryFinal(_ context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt time.Time, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.deliveryIndex(id)
	if err != nil {
		return err
	}
	f.deliveries[i].Status = status
	f.deliveries[i].LastStatus = httpStatus
	f.deliveries[i].LastError = errMsg
	f.deliveries[i].NextAttemptAt = nextAttemptAt
	f.deliveries[i].UpdatedAt = now
	return nil
}

func (f *fakeRepo) ResetStaleRecordingWebhookDeliveries(_ context.Context, before time.Time, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.deliveries {
		if f.deliveries[i].Status == repository.RecordingWebhookDeliveryDelivering && f.deliveries[i].UpdatedAt.Before(before) {
			f.deliveries[i].Status = repository.RecordingWebhookDeliveryPending
			f.deliveries[i].NextAttemptAt = now
			f.deliveries[i].UpdatedAt = now
		}
	}
	return nil
}

func (f *fakeRepo) RetryRecordingWebhookDelivery(_ context.Context, id int64, now time.Time) (*repository.RecordingWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.deliveryIndex(id)
	if err != nil {
		return nil, err
	}
	status := f.deliveries[i].Status
	if status != repository.RecordingWebhookDeliveryFailed && status != repository.RecordingWebhookDeliveryRejected {
		return nil, repository.ErrNotFound
	}
	f.deliveries[i].Status = repository.RecordingWebhookDeliveryPending
	f.deliveries[i].Attempts = 0
	f.deliveries[i].LastStatus = 0
	f.deliveries[i].LastError = ""
	f.deliveries[i].NextAttemptAt = now
	f.deliveries[i].DeliveredAt = nil
	f.deliveries[i].UpdatedAt = now
	return cloneDelivery(f.deliveries[i]), nil
}

func (f *fakeRepo) ListRecordingWebhookDeliveries(_ context.Context, limit int) ([]repository.RecordingWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 || limit > len(f.deliveries) {
		limit = len(f.deliveries)
	}
	out := make([]repository.RecordingWebhookDelivery, 0, limit)
	for i := len(f.deliveries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, f.deliveries[i])
	}
	return out, nil
}

func (f *fakeRepo) deliveryIndex(id int64) (int, error) {
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			return i, nil
		}
	}
	return -1, repository.ErrNotFound
}

func cloneDelivery(d repository.RecordingWebhookDelivery) *repository.RecordingWebhookDelivery {
	if d.LastAttemptAt != nil {
		t := *d.LastAttemptAt
		d.LastAttemptAt = &t
	}
	if d.DeliveredAt != nil {
		t := *d.DeliveredAt
		d.DeliveredAt = &t
	}
	return &d
}
