package twitch

import "context"

// EventSubQuotaPage is GetEventSubSubscriptions plus the Helix envelope's
// total / total_cost / max_total_cost fields that the generated method drops.
// The EventSub dashboard card needs all three to render cost-vs-quota.
type EventSubQuotaPage struct {
	Data       []EventSubSubscription
	Pagination Pagination
	Total      int
	TotalCost  int
	MaxCost    int
}

// GetEventSubSubscriptionsWithQuota is a quota-exposing variant of the
// generated GetEventSubSubscriptions. The scraper generator truncates
// helixResponse to Data+Pagination, which loses the quota signals we need
// for the Snapshot polling path.
func (c *Client) GetEventSubSubscriptionsWithQuota(ctx context.Context, params *GetEventSubSubscriptionsParams) (*EventSubQuotaPage, error) {
	var result helixResponse[EventSubSubscription]
	if err := c.get(ctx, "/eventsub/subscriptions", params, &result); err != nil {
		return nil, err
	}
	return &EventSubQuotaPage{
		Data:       result.Data,
		Pagination: result.Pagination,
		Total:      result.Total,
		TotalCost:  result.TotalCost,
		MaxCost:    result.MaxCost,
	}, nil
}

// GetEventSubSubscriptionsAllWithQuota paginates through all subscriptions
// and returns the final page's quota fields. Twitch returns the same
// total/total_cost/max_total_cost on every page, so reading them once at
// the end is correct — not a per-page sum.
func (c *Client) GetEventSubSubscriptionsAllWithQuota(ctx context.Context, params *GetEventSubSubscriptionsParams) (*EventSubQuotaPage, error) {
	if params == nil {
		params = &GetEventSubSubscriptionsParams{}
	}
	var (
		all      []EventSubSubscription
		lastPage *EventSubQuotaPage
	)
	for {
		page, err := c.GetEventSubSubscriptionsWithQuota(ctx, params)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		lastPage = page
		if page.Pagination.Cursor == "" {
			break
		}
		params.After = page.Pagination.Cursor
	}
	if lastPage == nil {
		lastPage = &EventSubQuotaPage{}
	}
	lastPage.Data = all
	lastPage.Pagination = Pagination{}
	return lastPage, nil
}
