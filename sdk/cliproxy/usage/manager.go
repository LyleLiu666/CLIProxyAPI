package usage

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	defaultQueueBuffer = 256
	dropLogInterval    = 5 * time.Second
)

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	Provider    string
	Model       string
	APIKey      string
	AuthID      string
	AuthIndex   string
	Source      string
	RequestedAt time.Time
	Latency     time.Duration
	Failed      bool
	Detail      Detail
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
}

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

type queueItem struct {
	ctx    context.Context
	record Record
}

// Manager maintains a bounded queue of usage records and delivers them to registered plugins.
type Manager struct {
	once     sync.Once
	stopOnce sync.Once
	cancel   context.CancelFunc

	mu          sync.Mutex
	cond        *sync.Cond
	buffer      int
	queue       []queueItem
	head        int
	size        int
	dropped     uint64
	lastDropLog time.Time
	closed      bool

	pluginsMu sync.RWMutex
	plugins   []Plugin
}

// NewManager constructs a manager with a bounded queue.
func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = defaultQueueBuffer
	}

	m := &Manager{
		buffer: buffer,
		queue:  make([]queueItem, buffer),
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

// Start launches the background dispatcher. Calling Start multiple times is safe.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		var workerCtx context.Context
		workerCtx, m.cancel = context.WithCancel(ctx)
		go func() {
			<-workerCtx.Done()
			m.close(false)
		}()
		go m.run()
	})
}

// Stop stops the dispatcher and drains the queue.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.close(true)
}

func (m *Manager) close(cancel bool) {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		if cancel && m.cancel != nil {
			m.cancel()
		}
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.cond.Broadcast()
	})
}

// Register appends a plugin to the delivery list.
func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.pluginsMu.Lock()
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}

// Publish enqueues a usage record for processing. If the queue is full the
// record is dropped to keep ingress memory bounded.
func (m *Manager) Publish(ctx context.Context, record Record) {
	if m == nil {
		return
	}
	// ensure worker is running even if Start was not called explicitly
	m.Start(context.Background())

	shouldLogDrop := false
	droppedCount := uint64(0)
	buffer := 0
	provider := ""

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if m.size == m.buffer {
		m.dropped++
		droppedCount = m.dropped
		buffer = m.buffer
		provider = record.Provider
		now := time.Now()
		if m.lastDropLog.IsZero() || now.Sub(m.lastDropLog) >= dropLogInterval {
			m.lastDropLog = now
			shouldLogDrop = true
		}
		m.mu.Unlock()
		if shouldLogDrop {
			log.Warnf("usage: queue full (buffer=%d, dropped=%d), dropping record for provider %s", buffer, droppedCount, provider)
		}
		return
	}

	index := (m.head + m.size) % m.buffer
	m.queue[index] = queueItem{ctx: ctx, record: record}
	m.size++
	m.mu.Unlock()
	m.cond.Signal()
}

// DroppedCount returns the number of records that were dropped because the queue was full.
func (m *Manager) DroppedCount() uint64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dropped
}

func (m *Manager) run() {
	for {
		m.mu.Lock()
		for !m.closed && m.size == 0 {
			m.cond.Wait()
		}
		if m.size == 0 && m.closed {
			m.mu.Unlock()
			return
		}

		item := m.queue[m.head]
		m.queue[m.head] = queueItem{}
		m.head = (m.head + 1) % m.buffer
		m.size--
		m.mu.Unlock()

		m.dispatch(item)
	}
}

func (m *Manager) dispatch(item queueItem) {
	m.pluginsMu.RLock()
	plugins := make([]Plugin, len(m.plugins))
	copy(plugins, m.plugins)
	m.pluginsMu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		safeInvoke(plugin, item.ctx, item.record)
	}
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("usage: plugin panic recovered: %v", r)
		}
	}()
	plugin.HandleUsage(ctx, record)
}

var defaultManager = NewManager(512)

// DefaultManager returns the global usage manager instance.
func DefaultManager() *Manager { return defaultManager }

// RegisterPlugin registers a plugin on the default manager.
func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }

// PublishRecord publishes a record using the default manager.
func PublishRecord(ctx context.Context, record Record) { DefaultManager().Publish(ctx, record) }

// StartDefault starts the default manager's dispatcher.
func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }

// StopDefault stops the default manager's dispatcher.
func StopDefault() { DefaultManager().Stop() }
