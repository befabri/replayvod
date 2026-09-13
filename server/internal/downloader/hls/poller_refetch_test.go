package hls

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestPollerRetainsFutureRefetchThroughEmptyAndShortPolls(t *testing.T) {
	empty := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n"
	short := empty + "#EXTINF:1,\n100.ts\n"
	complete := short + "#EXTINF:1,\n101.ts\n#EXTINF:1,\n102.ts\n#EXT-X-ENDLIST\n"
	srv := sequencePlaylistServer(t, empty, short, complete)
	skips := make(chan SkipEvent, 8)
	p := &Poller{
		URL: srv.URL, HTTPClient: srv.Client(), MinTick: time.Millisecond,
		StartMediaSeq: 103, RefetchSeqs: map[int64]bool{101: true}, SkipEvents: skips,
	}
	first, ok, jobs, err := runPollerCollect(t.Context(), p)
	if err != nil || !ok || len(first.ExpiredRefetchSeqs) != 0 {
		t.Fatalf("future refetch expired before appearing: first=%+v err=%v", first, err)
	}
	if len(jobs) != 1 || jobs[0].Segment.MediaSeq != 101 || len(p.RefetchSeqs) != 0 {
		t.Fatalf("future refetch was lost or repeated: jobs=%+v pending=%v", jobs, p.RefetchSeqs)
	}
	close(skips)
	for ev := range skips {
		t.Fatalf("future refetch was reported as loss: %+v", ev)
	}
}

func TestPollerReportsRefetchLossOnLaterWindow(t *testing.T) {
	initial := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:100\n#EXTINF:1,\n100.ts\n"
	rolled := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:120\n#EXTINF:1,\n120.ts\n#EXT-X-ENDLIST\n"
	srv := sequencePlaylistServer(t, initial, rolled)
	skips := make(chan SkipEvent, 8)
	p := &Poller{
		URL: srv.URL, HTTPClient: srv.Client(), MinTick: time.Millisecond,
		StartMediaSeq: 101, RefetchSeqs: map[int64]bool{110: true}, SkipEvents: skips,
	}
	first, _, jobs, err := runPollerCollect(t.Context(), p)
	if err != nil || len(first.ExpiredRefetchSeqs) != 0 || len(jobs) != 1 || jobs[0].Segment.MediaSeq != 120 {
		t.Fatalf("later refetch loss: first=%+v jobs=%+v err=%v", first, jobs, err)
	}
	close(skips)
	var got []SkipEvent
	for ev := range skips {
		got = append(got, ev)
	}
	want := []SkipEvent{
		{MediaSeq: 101, EndMediaSeq: 119, Reason: SkipReasonWindowRolled},
		{MediaSeq: 110, Reason: SkipReasonRefetchExpired},
	}
	if !slices.Equal(got, want) || len(p.RefetchSeqs) != 0 {
		t.Fatalf("later loss events=%+v pending=%v, want %+v", got, p.RefetchSeqs, want)
	}
}

func TestPollerReportsBootstrapRefetchLossWithoutBlockingSkips(t *testing.T) {
	body := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:1000\n#EXT-X-ENDLIST\n"
	srv := sequencePlaylistServer(t, body)
	refetch := make(map[int64]bool)
	for seq := range int64(1000) {
		refetch[seq] = true
	}
	p := &Poller{URL: srv.URL, HTTPClient: srv.Client(), StartMediaSeq: 900, RefetchSeqs: refetch, SkipEvents: make(chan SkipEvent)}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	first, ok, jobs, err := runPollerCollect(ctx, p)
	if err != nil || !ok || len(jobs) != 0 || len(first.ExpiredRefetchSeqs) != 1000 {
		t.Fatalf("bootstrap loss blocked on an undrained skip channel: first=%+v jobs=%d err=%v", first, len(jobs), err)
	}
	if first.WindowRollFrom != 900 || first.WindowRollTo != 999 {
		t.Fatalf("empty final playlist lost the initial window: %+v", first)
	}
	for i, seq := range first.ExpiredRefetchSeqs {
		if seq != int64(i) {
			t.Fatalf("expired sequences are not unique and ordered: index=%d seq=%d", i, seq)
		}
	}
}
