package usage

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

type firstCallBlockingPlugin struct {
	started chan struct{}
	release chan struct{}

	mu          sync.Mutex
	records     []Record
	blockOnce   sync.Once
	releaseOnce sync.Once
}

func newFirstCallBlockingPlugin() *firstCallBlockingPlugin {
	return &firstCallBlockingPlugin{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (p *firstCallBlockingPlugin) HandleUsage(_ context.Context, record Record) {
	shouldBlock := false
	p.blockOnce.Do(func() {
		shouldBlock = true
		close(p.started)
	})
	if shouldBlock {
		<-p.release
	}

	p.mu.Lock()
	p.records = append(p.records, record)
	p.mu.Unlock()
}

func (p *firstCallBlockingPlugin) waitStarted(t *testing.T) {
	t.Helper()

	select {
	case <-p.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first dispatch")
	}
}

func (p *firstCallBlockingPlugin) releaseFirst() {
	p.releaseOnce.Do(func() {
		close(p.release)
	})
}

func (p *firstCallBlockingPlugin) waitRecords(t *testing.T, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(p.providers()) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d records, got %d", want, len(p.providers()))
}

func (p *firstCallBlockingPlugin) providers() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	providers := make([]string, 0, len(p.records))
	for _, record := range p.records {
		providers = append(providers, record.Provider)
	}
	return providers
}

func snapshotQueue(manager *Manager) (size int, dropped uint64, providers []string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	providers = make([]string, 0, manager.size)
	for offset := 0; offset < manager.size; offset++ {
		index := (manager.head + offset) % manager.buffer
		providers = append(providers, manager.queue[index].record.Provider)
	}
	return manager.size, manager.dropped, providers
}

func TestManagerPublishDropsWhenQueueFull(t *testing.T) {
	manager := NewManager(2)
	plugin := newFirstCallBlockingPlugin()
	manager.Register(plugin)
	t.Cleanup(func() {
		plugin.releaseFirst()
		manager.Stop()
	})

	manager.Publish(context.Background(), Record{Provider: "first"})
	plugin.waitStarted(t)

	manager.Publish(context.Background(), Record{Provider: "second"})
	manager.Publish(context.Background(), Record{Provider: "third"})
	manager.Publish(context.Background(), Record{Provider: "fourth"})

	size, dropped, queuedProviders := snapshotQueue(manager)
	if size != 2 {
		t.Fatalf("expected queue size 2, got %d", size)
	}
	if dropped != 1 {
		t.Fatalf("expected dropped count 1, got %d", dropped)
	}

	wantQueued := []string{"second", "third"}
	if !reflect.DeepEqual(queuedProviders, wantQueued) {
		t.Fatalf("expected queued providers %v, got %v", wantQueued, queuedProviders)
	}

	plugin.releaseFirst()
	plugin.waitRecords(t, 3)
	manager.Stop()

	wantDelivered := []string{"first", "second", "third"}
	if got := plugin.providers(); !reflect.DeepEqual(got, wantDelivered) {
		t.Fatalf("expected delivered providers %v, got %v", wantDelivered, got)
	}
}

func TestManagerPublishWarnsWhenDroppingRecords(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()

	previousLevel := log.GetLevel()
	log.SetLevel(log.WarnLevel)
	defer log.SetLevel(previousLevel)

	manager := NewManager(1)
	plugin := newFirstCallBlockingPlugin()
	manager.Register(plugin)
	t.Cleanup(func() {
		plugin.releaseFirst()
		manager.Stop()
	})

	manager.Publish(context.Background(), Record{Provider: "first"})
	plugin.waitStarted(t)

	manager.Publish(context.Background(), Record{Provider: "second"})
	manager.Publish(context.Background(), Record{Provider: "third"})

	entries := hook.AllEntries()
	if len(entries) == 0 {
		t.Fatal("expected at least one warning log entry")
	}

	entry := entries[len(entries)-1]
	if entry.Level != log.WarnLevel {
		t.Fatalf("expected warning level, got %s", entry.Level)
	}
	if entry.Message == "" {
		t.Fatal("expected warning message to be populated")
	}
}

func waitClosedAndEmpty(t *testing.T, manager *Manager) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		closed := manager.closed
		size := manager.size
		manager.mu.Unlock()
		if closed && size == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for manager to close and drain queue")
}

func TestManagerStopsWhenStartContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := NewManager(2)
	plugin := newFirstCallBlockingPlugin()
	manager.Register(plugin)
	manager.Start(ctx)
	t.Cleanup(func() {
		plugin.releaseFirst()
		manager.Stop()
	})

	manager.Publish(context.Background(), Record{Provider: "first"})
	plugin.waitStarted(t)
	manager.Publish(context.Background(), Record{Provider: "second"})

	cancel()
	plugin.releaseFirst()
	plugin.waitRecords(t, 2)
	waitClosedAndEmpty(t, manager)

	manager.Publish(context.Background(), Record{Provider: "third"})
	time.Sleep(50 * time.Millisecond)

	wantDelivered := []string{"first", "second"}
	if got := plugin.providers(); !reflect.DeepEqual(got, wantDelivered) {
		t.Fatalf("expected delivered providers %v, got %v", wantDelivered, got)
	}
}
