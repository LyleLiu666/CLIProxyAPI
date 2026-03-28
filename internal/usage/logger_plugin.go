// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const (
	httpStatusBadRequest          = 400
	defaultMaxDetailsPerModel     = 1000
	statisticsPersistenceVersion  = 1
	statisticsPersistenceWaitTime = 25 * time.Millisecond
)

var (
	statisticsEnabled atomic.Bool
	eventIDCounter    atomic.Uint64
)

func init() {
	statisticsEnabled.Store(true)
	coreusage.RegisterPlugin(NewLoggerPlugin())
}

// LoggerPlugin collects in-memory request statistics for usage analysis.
// It implements coreusage.Plugin to receive usage records emitted by the runtime.
type LoggerPlugin struct {
	stats *RequestStatistics
}

// NewLoggerPlugin constructs a new logger plugin instance.
func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{stats: defaultRequestStatistics} }

// HandleUsage implements coreusage.Plugin.
func (p *LoggerPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if !statisticsEnabled.Load() {
		return
	}
	if p == nil || p.stats == nil {
		return
	}
	p.stats.Record(ctx, record)
}

// SetStatisticsEnabled toggles whether in-memory statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports the current recording state.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// RequestStatistics maintains aggregated request metrics in memory.
type RequestStatistics struct {
	mu sync.RWMutex

	totalRequests int64
	successCount  int64
	failureCount  int64
	totalTokens   int64

	apis map[string]*apiStats

	requestsByDay  map[string]int64
	requestsByHour map[int]int64
	tokensByDay    map[string]int64
	tokensByHour   map[int]int64

	maxDetailsPerModel int

	persistMu      sync.Mutex
	persistPath    string
	persistPending bool
	persistRunning bool
}

// apiStats holds aggregated metrics for a single API key.
type apiStats struct {
	TotalRequests int64
	TotalTokens   int64
	Models        map[string]*modelStats
}

// modelStats holds aggregated metrics for a specific model within an API.
type modelStats struct {
	TotalRequests int64
	TotalTokens   int64
	Details       []RequestDetail
}

// RequestDetail stores the timestamp, latency, and token usage for a single request.
type RequestDetail struct {
	EventID   string     `json:"event_id,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
	LatencyMs int64      `json:"latency_ms"`
	Source    string     `json:"source"`
	AuthIndex string     `json:"auth_index"`
	Tokens    TokenStats `json:"tokens"`
	Failed    bool       `json:"failed"`
}

// TokenStats captures the token usage breakdown for a request.
type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

// StatisticsSnapshot represents an immutable view of the aggregated metrics.
type StatisticsSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`

	APIs map[string]APISnapshot `json:"apis"`

	RequestsByDay  map[string]int64 `json:"requests_by_day"`
	RequestsByHour map[string]int64 `json:"requests_by_hour"`
	TokensByDay    map[string]int64 `json:"tokens_by_day"`
	TokensByHour   map[string]int64 `json:"tokens_by_hour"`
}

// APISnapshot summarises metrics for a single API key.
type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

// ModelSnapshot summarises metrics for a specific model.
type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

// SnapshotOptions controls how much request detail is copied into a snapshot.
type SnapshotOptions struct {
	IncludeDetails bool
	DetailsLimit   int
}

type statisticsPersistencePayload struct {
	Version int                `json:"version"`
	Usage   StatisticsSnapshot `json:"usage"`
}

var defaultRequestStatistics = NewRequestStatistics()

// GetRequestStatistics returns the shared statistics store.
func GetRequestStatistics() *RequestStatistics { return defaultRequestStatistics }

// NewRequestStatistics constructs an empty statistics store.
func NewRequestStatistics() *RequestStatistics {
	return &RequestStatistics{
		apis:               make(map[string]*apiStats),
		requestsByDay:      make(map[string]int64),
		requestsByHour:     make(map[int]int64),
		tokensByDay:        make(map[string]int64),
		tokensByHour:       make(map[int]int64),
		maxDetailsPerModel: defaultMaxDetailsPerModel,
	}
}

// ConfigurePersistence enables snapshot persistence at the provided file path and restores any existing snapshot.
func (s *RequestStatistics) ConfigurePersistence(path string) error {
	if s == nil {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		s.persistMu.Lock()
		s.persistPath = ""
		s.persistPending = false
		s.persistRunning = false
		s.persistMu.Unlock()
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("usage: create persistence directory: %w", err)
	}

	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		payload := statisticsPersistencePayload{}
		if err := json.Unmarshal(data, &payload); err == nil && payload.Usage.APIs != nil {
			s.RestoreSnapshot(payload.Usage)
		} else {
			legacy := StatisticsSnapshot{}
			if err := json.Unmarshal(data, &legacy); err != nil {
				return fmt.Errorf("usage: decode persisted statistics: %w", err)
			}
			s.RestoreSnapshot(legacy)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("usage: read persisted statistics: %w", err)
	}

	s.persistMu.Lock()
	s.persistPath = path
	s.persistPending = false
	s.persistRunning = false
	s.persistMu.Unlock()
	return nil
}

// RestoreSnapshot replaces in-memory state with the provided snapshot.
func (s *RequestStatistics) RestoreSnapshot(snapshot StatisticsSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restoreSnapshotLocked(snapshot)
}

// Record ingests a new usage record and updates the aggregates.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	if !statisticsEnabled.Load() {
		return
	}
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	detail := normaliseDetail(record.Detail)
	totalTokens := detail.TotalTokens
	statsKey := resolveBucketKey(ctx, record)
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	success := !failed
	modelName := record.Model
	if modelName == "" {
		modelName = "unknown"
	}
	dayKey := timestamp.Format("2006-01-02")
	hourKey := timestamp.Hour()
	requestDetail := RequestDetail{
		EventID:   newEventID(timestamp),
		Timestamp: timestamp,
		LatencyMs: normaliseLatency(record.Latency),
		Source:    record.Source,
		AuthIndex: record.AuthIndex,
		Tokens:    detail,
		Failed:    failed,
	}

	s.mu.Lock()
	s.totalRequests++
	if success {
		s.successCount++
	} else {
		s.failureCount++
	}
	s.totalTokens += totalTokens

	stats, ok := s.apis[statsKey]
	if !ok {
		stats = &apiStats{Models: make(map[string]*modelStats)}
		s.apis[statsKey] = stats
	}
	s.updateAPIStats(stats, modelName, requestDetail)

	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
	s.mu.Unlock()

	s.schedulePersist()
}

func (s *RequestStatistics) updateAPIStats(stats *apiStats, model string, detail RequestDetail) {
	stats.TotalRequests++
	stats.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue, ok := stats.Models[model]
	if !ok {
		modelStatsValue = &modelStats{}
		stats.Models[model] = modelStatsValue
	}
	modelStatsValue.TotalRequests++
	modelStatsValue.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue.Details = append(modelStatsValue.Details, detail)
	if s.maxDetailsPerModel > 0 && len(modelStatsValue.Details) > s.maxDetailsPerModel {
		overflow := len(modelStatsValue.Details) - s.maxDetailsPerModel
		copy(modelStatsValue.Details, modelStatsValue.Details[overflow:])
		modelStatsValue.Details = modelStatsValue.Details[:s.maxDetailsPerModel]
	}
}

// Snapshot returns a copy of the aggregated metrics for external consumption.
func (s *RequestStatistics) Snapshot() StatisticsSnapshot {
	return s.SnapshotWithOptions(SnapshotOptions{IncludeDetails: true})
}

// SnapshotWithOptions returns a copy of the aggregated metrics with controllable detail copying.
func (s *RequestStatistics) SnapshotWithOptions(opts SnapshotOptions) StatisticsSnapshot {
	result := StatisticsSnapshot{}
	if s == nil {
		return result
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result.TotalRequests = s.totalRequests
	result.SuccessCount = s.successCount
	result.FailureCount = s.failureCount
	result.TotalTokens = s.totalTokens

	result.APIs = make(map[string]APISnapshot, len(s.apis))
	for apiName, stats := range s.apis {
		apiSnapshot := APISnapshot{
			TotalRequests: stats.TotalRequests,
			TotalTokens:   stats.TotalTokens,
			Models:        make(map[string]ModelSnapshot, len(stats.Models)),
		}
		for modelName, modelStatsValue := range stats.Models {
			modelSnapshot := ModelSnapshot{
				TotalRequests: modelStatsValue.TotalRequests,
				TotalTokens:   modelStatsValue.TotalTokens,
			}
			if opts.IncludeDetails {
				requestDetails := cloneRequestDetails(modelStatsValue.Details, opts.DetailsLimit)
				modelSnapshot.Details = requestDetails
			}
			apiSnapshot.Models[modelName] = modelSnapshot
		}
		result.APIs[apiName] = apiSnapshot
	}

	result.RequestsByDay = make(map[string]int64, len(s.requestsByDay))
	for k, v := range s.requestsByDay {
		result.RequestsByDay[k] = v
	}

	result.RequestsByHour = make(map[string]int64, len(s.requestsByHour))
	for hour, v := range s.requestsByHour {
		key := formatHour(hour)
		result.RequestsByHour[key] = v
	}

	result.TokensByDay = make(map[string]int64, len(s.tokensByDay))
	for k, v := range s.tokensByDay {
		result.TokensByDay[k] = v
	}

	result.TokensByHour = make(map[string]int64, len(s.tokensByHour))
	for hour, v := range s.tokensByHour {
		key := formatHour(hour)
		result.TokensByHour[key] = v
	}

	return result
}

type MergeResult struct {
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}

// MergeSnapshot merges an exported statistics snapshot into the current store.
// Existing data is preserved and duplicate request details are skipped.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
	result := MergeResult{}
	if s == nil {
		return result
	}

	s.mu.Lock()

	seen := make(map[string]struct{})
	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for _, detail := range modelStatsValue.Details {
				seen[dedupKey(apiName, modelName, detail)] = struct{}{}
			}
		}
	}

	for apiName, apiSnapshot := range snapshot.APIs {
		apiName = strings.TrimSpace(apiName)
		if apiName == "" {
			continue
		}
		stats, ok := s.apis[apiName]
		if !ok || stats == nil {
			stats = &apiStats{Models: make(map[string]*modelStats)}
			s.apis[apiName] = stats
		} else if stats.Models == nil {
			stats.Models = make(map[string]*modelStats)
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				detail = normaliseRequestDetail(detail)
				key := dedupKey(apiName, modelName, detail)
				if _, exists := seen[key]; exists {
					result.Skipped++
					continue
				}
				seen[key] = struct{}{}
				s.recordImported(apiName, modelName, stats, detail)
				result.Added++
			}
		}
	}

	s.mu.Unlock()
	if result.Added > 0 {
		s.schedulePersist()
	}
	return result
}

func (s *RequestStatistics) recordImported(apiName, modelName string, stats *apiStats, detail RequestDetail) {
	totalTokens := detail.Tokens.TotalTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	s.totalRequests++
	if detail.Failed {
		s.failureCount++
	} else {
		s.successCount++
	}
	s.totalTokens += totalTokens

	s.updateAPIStats(stats, modelName, detail)

	dayKey := detail.Timestamp.Format("2006-01-02")
	hourKey := detail.Timestamp.Hour()

	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
}

func (s *RequestStatistics) restoreSnapshotLocked(snapshot StatisticsSnapshot) {
	s.totalRequests = snapshot.TotalRequests
	s.successCount = snapshot.SuccessCount
	s.failureCount = snapshot.FailureCount
	s.totalTokens = snapshot.TotalTokens

	s.apis = make(map[string]*apiStats, len(snapshot.APIs))
	for apiName, apiSnapshot := range snapshot.APIs {
		stats := &apiStats{
			TotalRequests: apiSnapshot.TotalRequests,
			TotalTokens:   apiSnapshot.TotalTokens,
			Models:        make(map[string]*modelStats, len(apiSnapshot.Models)),
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			stats.Models[modelName] = &modelStats{
				TotalRequests: modelSnapshot.TotalRequests,
				TotalTokens:   modelSnapshot.TotalTokens,
				Details:       cloneRequestDetails(modelSnapshot.Details, s.maxDetailsPerModel),
			}
		}
		s.apis[apiName] = stats
	}

	s.requestsByDay = cloneStringInt64Map(snapshot.RequestsByDay)
	s.requestsByHour = make(map[int]int64, len(snapshot.RequestsByHour))
	for hour, count := range snapshot.RequestsByHour {
		if parsed, ok := parseHour(hour); ok {
			s.requestsByHour[parsed] = count
		}
	}
	s.tokensByDay = cloneStringInt64Map(snapshot.TokensByDay)
	s.tokensByHour = make(map[int]int64, len(snapshot.TokensByHour))
	for hour, count := range snapshot.TokensByHour {
		if parsed, ok := parseHour(hour); ok {
			s.tokensByHour[parsed] = count
		}
	}
}

func (s *RequestStatistics) schedulePersist() {
	if s == nil {
		return
	}
	s.persistMu.Lock()
	if s.persistPath == "" {
		s.persistMu.Unlock()
		return
	}
	s.persistPending = true
	if s.persistRunning {
		s.persistMu.Unlock()
		return
	}
	s.persistRunning = true
	s.persistMu.Unlock()

	go func() {
		for {
			time.Sleep(statisticsPersistenceWaitTime)

			s.persistMu.Lock()
			if !s.persistPending {
				s.persistRunning = false
				s.persistMu.Unlock()
				return
			}
			s.persistPending = false
			path := s.persistPath
			s.persistMu.Unlock()

			if path == "" {
				continue
			}
			if err := writeStatisticsSnapshot(path, s.Snapshot()); err != nil {
				log.Warnf("usage: persist statistics: %v", err)
			}
		}
	}()
}

func writeStatisticsSnapshot(path string, snapshot StatisticsSnapshot) error {
	payload := statisticsPersistencePayload{
		Version: statisticsPersistenceVersion,
		Usage:   snapshot,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal statistics snapshot: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp statistics snapshot: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename statistics snapshot: %w", err)
	}
	return nil
}

func cloneRequestDetails(details []RequestDetail, limit int) []RequestDetail {
	if len(details) == 0 {
		return nil
	}
	if limit > 0 && len(details) > limit {
		details = details[len(details)-limit:]
	}
	out := make([]RequestDetail, len(details))
	copy(out, details)
	for index := range out {
		out[index] = normaliseRequestDetail(out[index])
	}
	return out
}

func cloneStringInt64Map(input map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func normaliseRequestDetail(detail RequestDetail) RequestDetail {
	detail.Tokens = normaliseTokenStats(detail.Tokens)
	if detail.LatencyMs < 0 {
		detail.LatencyMs = 0
	}
	if detail.Timestamp.IsZero() {
		detail.Timestamp = time.Now()
	}
	return detail
}

func newEventID(timestamp time.Time) string {
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	sequence := eventIDCounter.Add(1)
	return fmt.Sprintf("evt_%d_%d", timestamp.UTC().UnixNano(), sequence)
}

func dedupKey(apiName, modelName string, detail RequestDetail) string {
	if eventID := strings.TrimSpace(detail.EventID); eventID != "" {
		return eventID
	}
	timestamp := detail.Timestamp.UTC().Format(time.RFC3339Nano)
	tokens := normaliseTokenStats(detail.Tokens)
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%d|%d|%d|%d|%d",
		apiName,
		modelName,
		timestamp,
		detail.Source,
		detail.AuthIndex,
		detail.Failed,
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.TotalTokens,
	)
}

func resolveAPIIdentifier(ctx context.Context, record coreusage.Record) string {
	if ctx != nil {
		if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil {
			path := ginCtx.FullPath()
			if path == "" && ginCtx.Request != nil {
				path = ginCtx.Request.URL.Path
			}
			method := ""
			if ginCtx.Request != nil {
				method = ginCtx.Request.Method
			}
			if path != "" {
				if method != "" {
					return method + " " + path
				}
				return path
			}
		}
	}
	if record.Provider != "" {
		return record.Provider
	}
	return "unknown"
}

func resolveBucketKey(ctx context.Context, record coreusage.Record) string {
	base := strings.TrimSpace(record.APIKey)
	if base == "" {
		base = resolveAPIIdentifier(ctx, record)
	}
	route := resolveRequestRoute(ctx)
	if route == "" {
		return base
	}
	if base == "" {
		return route
	}
	return base + " @ " + route
}

func resolveRequestRoute(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return ""
	}
	path := strings.TrimSpace(ginCtx.FullPath())
	if path == "" && ginCtx.Request != nil && ginCtx.Request.URL != nil {
		path = strings.TrimSpace(ginCtx.Request.URL.Path)
	}
	if path == "" {
		return ""
	}
	method := ""
	if ginCtx.Request != nil {
		method = strings.TrimSpace(ginCtx.Request.Method)
	}
	if method == "" {
		return path
	}
	return method + " " + path
}

func resolveSuccess(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return true
	}
	status := ginCtx.Writer.Status()
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

func normaliseDetail(detail coreusage.Detail) TokenStats {
	tokens := TokenStats{
		InputTokens:     detail.InputTokens,
		OutputTokens:    detail.OutputTokens,
		ReasoningTokens: detail.ReasoningTokens,
		CachedTokens:    detail.CachedTokens,
		TotalTokens:     detail.TotalTokens,
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens + detail.CachedTokens
	}
	return tokens
}

func normaliseTokenStats(tokens TokenStats) TokenStats {
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	return tokens
}

func normaliseLatency(latency time.Duration) int64 {
	if latency <= 0 {
		return 0
	}
	return latency.Milliseconds()
}

func formatHour(hour int) string {
	if hour < 0 {
		hour = 0
	}
	hour = hour % 24
	return fmt.Sprintf("%02d", hour)
}

func parseHour(value string) (int, bool) {
	if len(value) != 2 {
		return 0, false
	}
	var hour int
	_, err := fmt.Sscanf(value, "%02d", &hour)
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	return hour, true
}
