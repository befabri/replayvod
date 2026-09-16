package contracttest

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testServerSettingsFreshInsertHasEmptyURLs(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	saved, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"webhook": saved.EventSubWebhookCallbackURL, "ingest": saved.EventSubRelayIngestURL,
		"subscribe": saved.EventSubRelaySubscribeURL, "local": saved.EventSubRelayLocalCallbackURL,
	} {
		if got != "" {
			t.Fatalf("fresh insert returned a %s URL of %q", name, got)
		}
	}
	reloaded, err := repo.GetServerSettings(ctx)
	if err != nil || reloaded.ServerMode != "off" || reloaded.EventSubRelayIngestURL != "" {
		t.Fatalf("reloaded = %+v, %v", reloaded, err)
	}
}
