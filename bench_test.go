package sqlog

import (
	"io"
	"log/slog"
	"testing"
)

//  go test -v -cpu=4 -run=none -bench=. -benchtime=10s -benchmem .

const msg = "The quick brown fox jumps over the lazy dog"

func BenchmarkSlogSimple(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for i := 0; i < b.N; i++ {
		logger.Info(msg, "rate", "15", "low", 16, "high", 123.2)
	}
}

func BenchmarkSQLogSimple(b *testing.B) {
	l, err := New(&Config{})
	if err != nil {
		b.Error(err)
		return
	}
	logger := slog.New(l.Handler())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		logger.Info(msg, "rate", "15", "low", 16, "high", 123.2)
	}

	l.Close()
}
