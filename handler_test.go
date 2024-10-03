package sqlog

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Mock do Ingester
type testMockIngester struct {
	close  func() error
	ingest func(time time.Time, level int8, data []byte) error
}

func (m *testMockIngester) Close() error {
	if m.close == nil {
		return nil
	}
	return m.close()
}

func (m *testMockIngester) Ingest(time time.Time, level int8, data []byte) error {
	if m.ingest == nil {
		return nil
	}
	return m.ingest(time, level, data)
}

func Test_Handler_Handle(t *testing.T) {

	var (
		ingested bool
	)

	ingester := &testMockIngester{
		ingest: func(time time.Time, level int8, data []byte) error {
			ingested = true
			return nil
		},
	}

	logger := slog.New(newHandler(ingester, nil))

	logger.Info("test message")

	assert.True(t, ingested)
}

func Test_Handler_Enabled(t *testing.T) {
	ingester := new(testMockIngester)
	config := &HandlerConfig{
		Options: &slog.HandlerOptions{
			Level: slog.LevelWarn,
		},
	}

	handler := newHandler(ingester, config)

	ctx := context.Background()

	assert.True(t, handler.Enabled(ctx, slog.LevelWarn))
	assert.False(t, handler.Enabled(ctx, slog.LevelInfo))
}

func Test_Handler_Fanout(t *testing.T) {

	var (
		ingested    int
		ingestCount = func(time time.Time, level int8, data []byte) error {
			ingested++
			return nil
		}
	)

	handler := newHandler(&testMockIngester{ingest: ingestCount}, &HandlerConfig{
		Options: &slog.HandlerOptions{
			Level: slog.LevelWarn, // will not ingest info msg
		},
	})
	logger := slog.New(handler)

	ctx := context.Background()
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn))
	assert.False(t, handler.Enabled(ctx, slog.LevelInfo))

	handler1 := newHandler(&testMockIngester{ingest: ingestCount}, nil)
	handler2 := newHandler(&testMockIngester{ingest: ingestCount}, nil)

	handler.fanout(handler1, handler2)
	assert.Equal(t, 2, len(handler.handlers))
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn))
	assert.True(t, handler.Enabled(ctx, slog.LevelInfo))

	logger.Info("test message")
	assert.Equal(t, 2, ingested) // only fanout

	logger.Warn("test message")
	assert.Equal(t, 5, ingested)
}
