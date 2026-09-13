package mediastore

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

func growingScratch(t *testing.T) (*Scratch, *Workspace, *Workspace, string) {
	t.Helper()
	return reservedScratch(t, 400_000, 140_000)
}

func reservedScratch(t *testing.T, footprint, peerFootprint int64) (*Scratch, *Workspace, *Workspace, string) {
	t.Helper()
	scratch := NewScratch(t.TempDir())
	scratch.stat = func(string) (int64, int64, error) {
		used, err := diskUsage(scratch.Root())
		return 1_000_000, 600_000 - used, err
	}
	writer, err := scratch.New("remux", footprint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(true) })
	if err := os.WriteFile(filepath.Join(writer.Dir, "input.mp4"), make([]byte, 200_000), 0600); err != nil {
		t.Fatal(err)
	}
	peer, err := scratch.New("peer", peerFootprint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close(true) })
	return scratch, writer, peer, filepath.Join(writer.Dir, "output.part")
}

func continuallyGrowBeforeScratchStat(scratch *Scratch, path string) (*int, func()) {
	stat := scratch.stat
	calls, active := 0, true
	scratch.stat = func(root string) (int64, int64, error) {
		if active {
			calls++
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return 0, 0, err
			}
			err = errors.Join(file.Truncate(int64(calls)*1_000), file.Close())
			if err != nil {
				return 0, 0, err
			}
		}
		return stat(root)
	}
	return &calls, func() { active = false }
}

func copyScratchUsage(scratch *Scratch) map[*Workspace]scratchUsage {
	copy := make(map[*Workspace]scratchUsage, len(scratch.work))
	for workspace, usage := range scratch.work {
		copy[workspace] = scratchUsage{bytes: usage.bytes, files: maps.Clone(usage.files)}
	}
	return copy
}

func growBeforeScratchStat(scratch *Scratch, path string) {
	stat := scratch.stat
	grown := false
	scratch.stat = func(root string) (int64, int64, error) {
		if !grown {
			grown = true
			if err := os.WriteFile(path, make([]byte, 100_000), 0600); err != nil {
				return 0, 0, err
			}
		}
		return stat(root)
	}
}

func TestScratchReconcilesGrowthBetweenScanAndStat(t *testing.T) {
	for _, operation := range []string{"reserve", "reserve additional", "open", "write", "truncate"} {
		t.Run(operation, func(t *testing.T) {
			scratch, writer, peer, partial := growingScratch(t)
			growBeforeScratchStat(scratch, partial)
			var err error
			switch operation {
			case "reserve":
				err = peer.Reserve(140_000)
			case "reserve additional":
				err = writer.ReserveAdditional(t.Context(), 200_000)
			case "open":
				var next *Workspace
				next, err = scratch.New("next", 0)
				if next != nil {
					t.Cleanup(func() { _ = next.Close(true) })
				}
			default:
				file, openErr := os.Create(filepath.Join(peer.Dir, "capture.ts"))
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer file.Close()
				if operation == "write" {
					var n int
					n, err = peer.WriteFile(t.Context(), file, make([]byte, 140_000))
					if err == nil && n != 140_000 {
						t.Fatalf("reserved write was incomplete: %d", n)
					}
				} else {
					err = peer.Truncate(file, 140_000)
				}
			}
			if err != nil {
				t.Fatalf("growth within a reserved footprint was counted twice: %v", err)
			}
		})
	}
}

func TestScratchMonitorKeepsReservedExternalWriter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scratch, writer, _, partial := growingScratch(t)
		growBeforeScratchStat(scratch, partial)
		ctx, stop := writer.Monitor(t.Context())
		defer stop()
		time.Sleep(time.Second)
		synctest.Wait()
		if err := context.Cause(ctx); err != nil {
			t.Fatalf("monitor canceled output that fit its reservation: %v", err)
		}
		if info, err := os.Stat(partial); err != nil || info.Size() != 100_000 {
			t.Fatalf("monitor did not inspect external growth: %v, %v", info, err)
		}
	})
}

func TestScratchReconciliationStillRejectsRealCapacityFailure(t *testing.T) {
	scratch, _, peer, partial := growingScratch(t)
	growBeforeScratchStat(scratch, partial)
	if err := peer.Reserve(160_000); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("growth reconciliation admitted an overcommitted reservation: %v", err)
	}
}

func TestScratchContinualGrowthDefersCapacityFailure(t *testing.T) {
	for _, operation := range []string{"reserve", "open", "truncate"} {
		t.Run(operation, func(t *testing.T) {
			scratch, _, peer, partial := reservedScratch(t, 400_000, 150_000)
			calls, _ := continuallyGrowBeforeScratchStat(scratch, partial)
			before := copyScratchUsage(scratch)
			ctx, stop := peer.Monitor(t.Context())
			defer stop()
			var err error
			switch operation {
			case "reserve":
				err = peer.Reserve(150_000)
			case "open":
				var next *Workspace
				next, err = scratch.New("next", 0)
				if next != nil {
					t.Cleanup(func() { _ = next.Close(true) })
				}
			case "truncate":
				file, openErr := os.Create(filepath.Join(peer.Dir, "capture.ts"))
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer file.Close()
				err = peer.Truncate(file, 150_000)
				if info, statErr := file.Stat(); statErr != nil || info.Size() != 0 {
					t.Fatalf("uncertain capacity changed the file: %v, %v", info, statErr)
				}
			}
			if !errors.Is(err, errScratchScanChanged) || errors.Is(err, storage.ErrFull) {
				t.Fatalf("continual reserved growth became a capacity failure: %v", err)
			}
			if *calls != scratchCapacityAttempts {
				t.Fatalf("capacity reconciliation escaped its bound: %d checks", *calls)
			}
			if !reflect.DeepEqual(scratch.work, before) {
				t.Fatal("uncertain reconciliation published a speculative snapshot")
			}
			if cause := context.Cause(ctx); cause != nil {
				t.Fatalf("uncertain capacity canceled an existing owner: %v", cause)
			}
		})
	}
}

func TestScratchMonitorDefersUncertainGrowthAndStillStopsForFullDisk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scratch, writer, _, partial := reservedScratch(t, 400_000, 150_000)
		calls, settle := continuallyGrowBeforeScratchStat(scratch, partial)
		ctx, stop := writer.Monitor(t.Context())
		defer stop()
		time.Sleep(time.Second)
		synctest.Wait()
		if cause := context.Cause(ctx); cause != nil || *calls != scratchCapacityAttempts {
			t.Fatalf("monitor failed during uncertain growth: %v, checks=%d", cause, *calls)
		}
		scratch.mu.Lock()
		settle()
		scratch.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		if cause := context.Cause(ctx); cause != nil {
			t.Fatalf("monitor did not recover after growth settled: %v", cause)
		}
		scratch.mu.Lock()
		scratch.stat = func(string) (int64, int64, error) { return 1_000_000, 0, nil }
		scratch.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		if cause := context.Cause(ctx); !errors.Is(cause, storage.ErrFull) {
			t.Fatalf("monitor ignored actual disk exhaustion: %v", cause)
		}
	})
}

func TestScratchActiveOperationsWaitForCapacityReconciliation(t *testing.T) {
	for _, operation := range []string{"write", "reserve additional"} {
		for _, outcome := range []string{"settled", "canceled"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					footprint := int64(400_000)
					if operation == "reserve additional" {
						footprint = 300_000
					}
					scratch, writer, peer, partial := reservedScratch(t, footprint, 150_000)
					calls, settle := continuallyGrowBeforeScratchStat(scratch, partial)
					monitorCtx, stop := peer.Monitor(t.Context())
					defer stop()
					requestCtx, cancel := context.WithCancel(t.Context())
					defer cancel()
					file, err := os.Create(filepath.Join(peer.Dir, "capture.ts"))
					if err != nil {
						t.Fatal(err)
					}
					defer file.Close()
					done := make(chan error, 1)
					go func() {
						if operation == "write" {
							_, err := peer.WriteFile(requestCtx, file, make([]byte, 150_000))
							done <- err
						} else {
							done <- writer.ReserveAdditional(requestCtx, 200_000)
						}
					}()
					synctest.Wait()
					select {
					case err := <-done:
						t.Fatalf("active operation surfaced temporary uncertainty: %v", err)
					default:
					}
					if !scratch.mu.TryLock() {
						t.Fatal("capacity retry held the accounting lock while waiting")
					}
					reserved := writer.reserved
					settle()
					scratch.mu.Unlock()
					if reserved != footprint || *calls != scratchCapacityAttempts {
						t.Fatalf("retry leaked a tentative reservation or escaped its bound: reserved=%d checks=%d", reserved, *calls)
					}
					if info, err := file.Stat(); err != nil || info.Size() != 0 {
						t.Fatalf("capacity retry wrote before admission: %v, %v", info, err)
					}
					if cause := context.Cause(monitorCtx); cause != nil {
						t.Fatalf("temporary capacity uncertainty canceled the monitor: %v", cause)
					}
					if outcome == "canceled" {
						cancel()
					} else {
						time.Sleep(scratchRetryDelay)
					}
					synctest.Wait()
					select {
					case err := <-done:
						if outcome == "canceled" && !errors.Is(err, context.Canceled) || outcome == "settled" && err != nil {
							t.Fatalf("capacity retry result: %v", err)
						}
					default:
						t.Fatal("capacity retry did not finish after settlement or cancellation")
					}
					wantSize, wantReserved := int64(0), footprint
					if outcome == "settled" {
						if operation == "write" {
							wantSize = 150_000
						} else {
							wantReserved = 400_000
						}
					}
					if info, err := file.Stat(); err != nil || info.Size() != wantSize || writer.reserved != wantReserved {
						t.Fatalf("retry changed output or reservation more than once: file=%v err=%v reserved=%d", info, err, writer.reserved)
					}
				})
			})
		}
	}
}

func TestScratchReconciliationPreservesFilesystemFailures(t *testing.T) {
	for _, operation := range []string{"reserve", "write"} {
		for _, phase := range []string{"scan", "statfs"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				scratch, _, peer, partial := growingScratch(t)
				growBeforeScratchStat(scratch, partial)
				before := copyScratchUsage(scratch)
				if phase == "statfs" {
					stat, calls := scratch.stat, 0
					scratch.stat = func(root string) (int64, int64, error) {
						calls++
						if calls == 2 {
							return 0, 0, os.ErrPermission
						}
						return stat(root)
					}
				} else {
					initialScans := 0
					if operation == "reserve" {
						initialScans = len(scratch.work)
					}
					calls := 0
					scratch.usage = func(dir string) (scratchUsage, error) {
						calls++
						if calls > initialScans {
							return scratchUsage{}, os.ErrPermission
						}
						return scanDiskUsage(dir)
					}
				}
				monitorCtx, stop := peer.Monitor(t.Context())
				defer stop()
				var err error
				if operation == "write" {
					file, openErr := os.Create(filepath.Join(peer.Dir, "capture.ts"))
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer file.Close()
					var n int
					n, err = peer.WriteFile(t.Context(), file, make([]byte, 140_000))
					if n != 0 || !errors.Is(context.Cause(monitorCtx), os.ErrPermission) {
						t.Fatalf("filesystem failure wrote bytes or failed to stop its owner: wrote=%d cause=%v", n, context.Cause(monitorCtx))
					}
				} else {
					err = peer.Reserve(140_000)
				}
				if !errors.Is(err, os.ErrPermission) || errors.Is(err, storage.ErrFull) || errors.Is(err, errScratchScanChanged) {
					t.Fatalf("reconciliation hid or relabeled a filesystem error: %v", err)
				}
				if !reflect.DeepEqual(scratch.work, before) {
					t.Fatal("failed reconciliation replaced the last complete accounting snapshot")
				}
			})
		}
	}
}
