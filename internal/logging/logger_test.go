package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:      LevelDebug,
		Output:     &buf,
		JSON:       true,
		AddSource:  false,
		TimeFormat: time.RFC3339,
	}

	logger := New(cfg)
	if logger == nil {
		t.Fatal("New logger should not be nil")
	}

	t.Run("Levels", func(t *testing.T) {
		buf.Reset()
		logger.Debug("debug msg")
		if !strings.Contains(buf.String(), "debug msg") {
			t.Error("fast debug logging failed")
		}

		buf.Reset()
		logger.Info("info msg")
		if !strings.Contains(buf.String(), "info msg") {
			t.Error("fast info logging failed")
		}

		buf.Reset()
		logger.Warn("warn msg")
		if !strings.Contains(buf.String(), "warn msg") {
			t.Error("fast warn logging failed")
		}

		buf.Reset()
		logger.Error("error msg")
		if !strings.Contains(buf.String(), "error msg") {
			t.Error("fast error logging failed")
		}
	})

	t.Run("DynamicLevel", func(t *testing.T) {
		logger.SetLevel(LevelError)
		if logger.GetLevel() != LevelError {
			t.Error("SetLevel failed")
		}

		buf.Reset()
		logger.Info("should not appear")
		if buf.Len() > 0 {
			t.Error("Logged info message when level was Error")
		}

		logger.SetLevel(LevelDebug)
	})

	t.Run("WithComponent", func(t *testing.T) {
		buf.Reset()
		l := logger.WithComponent("test-comp")
		l.Info("msg")
		if !strings.Contains(buf.String(), "test-comp") {
			t.Error("WithComponent missing component field")
		}
	})

	t.Run("WithFields", func(t *testing.T) {
		buf.Reset()
		l := logger.WithFields(map[string]any{"foo": "bar"})
		l.Info("msg")
		if !strings.Contains(buf.String(), "foo") || !strings.Contains(buf.String(), "bar") {
			t.Error("WithFields missing fields")
		}
	})

	t.Run("Audit", func(t *testing.T) {
		buf.Reset()
		logger.Audit("login", "user:123", map[string]any{"ip": "1.2.3.4"})
		logStr := buf.String()
		if !strings.Contains(logStr, "AUDIT") {
			t.Error("Audit log missing AUDIT message")
		}
		if !strings.Contains(logStr, "user:123") {
			t.Error("Audit log missing resource")
		}
	})

	t.Run("Metric", func(t *testing.T) {
		buf.Reset()
		logger.Metric("cpu_usage", 12.5, map[string]string{"h": "h1"})
		logStr := buf.String()
		if !strings.Contains(logStr, "METRIC") {
			t.Error("Metric log missing METRIC message")
		}
		if !strings.Contains(logStr, "12.5") {
			t.Error("Metric log missing value")
		}
	})
}

func TestDefaultLogger(t *testing.T) {
	// Just cover the default logger functions to ensure no panics
	// We can't easily capture stdout/stderr without piping,
	// so we'll just execute them for coverage.

	// Ensure default is initialized
	l := Default()
	if l == nil {
		t.Fatal("Default logger is nil")
	}

	// Create a buffer logger and set it as default to capture output
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Output = &buf
	newDefault := New(cfg)
	SetDefault(newDefault)

	Debug("debug")
	Info("info")
	Warn("warn")
	Error("error")
	Errorf("error %s", "formatted")
	Audit("test", "res", nil)

	WithComponent("comp").Info("comp msg")

	if buf.Len() == 0 {
		t.Error("Default logger captured no output")
	}
}

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(5)

	t.Run("AddAndGet", func(t *testing.T) {
		rb.Clear()
		ent := AppLogEntry{Message: "msg1", Source: "src1"}
		rb.Add(ent)

		if rb.Count() != 1 {
			t.Errorf("Count expected 1, got %d", rb.Count())
		}

		all := rb.GetAll()
		if len(all) != 1 || all[0].Message != "msg1" {
			t.Error("GetAll returned incorrect data")
		}
	})

	t.Run("Overflow", func(t *testing.T) {
		rb.Clear()
		for i := 0; i < 7; i++ {
			rb.Add(AppLogEntry{Message: "msg", Level: "info"})
		}

		if rb.Count() != 5 {
			t.Errorf("Count should be capped at size 5, got %d", rb.Count())
		}

		// Circular check - head should have wrapped
	})

	t.Run("GetLast", func(t *testing.T) {
		rb.Clear()
		rb.Add(AppLogEntry{Message: "1"})
		rb.Add(AppLogEntry{Message: "2"})
		rb.Add(AppLogEntry{Message: "3"})

		last2 := rb.GetLast(2)
		if len(last2) != 2 {
			t.Errorf("GetLast(2) returned %d items", len(last2))
		}
		if last2[0].Message != "2" || last2[1].Message != "3" {
			t.Error("GetLast returned wrong items")
		}

		lastEmpty := rb.GetLast(0)
		if len(lastEmpty) != 0 {
			t.Error("GetLast(0) should return empty")
		}

		lastTooMany := rb.GetLast(10)
		if len(lastTooMany) != 3 {
			t.Error("GetLast(>count) should return all items")
		}
	})

	t.Run("GetBySource", func(t *testing.T) {
		rb.Clear()
		rb.Add(AppLogEntry{Source: "A", Message: "1"})
		rb.Add(AppLogEntry{Source: "B", Message: "2"})
		rb.Add(AppLogEntry{Source: "A", Message: "3"})

		as := rb.GetBySource("A", 0)
		if len(as) != 2 {
			t.Errorf("GetBySource(A) expected 2, got %d", len(as))
		}
		if as[0].Message != "1" || as[1].Message != "3" {
			t.Error("GetBySource returned wrong items")
		}

		limit := rb.GetBySource("A", 1)
		if len(limit) != 1 {
			t.Errorf("GetBySource limit failed")
		}
	})

	t.Run("GlobalHelpers", func(t *testing.T) {
		// Just ensure they don't panic
		GetAppLogBuffer().Clear() // clear global buffer

		APILog("info", "test")
		CtlLog("info", "test")
		GatewayLog("info", "test")
		AuthLog("info", "test")
		FirewallLog("info", "test")
		LogWithExtra("src", "info", map[string]string{"k": "v"}, "msg")

		if GetAppLogBuffer().Count() == 0 {
			t.Error("Global helpers did not add to global buffer")
		}
	})

}

func TestJSONLogParsing(t *testing.T) {
	// Verify that our JSON structure is correct
	var buf bytes.Buffer
	cfg := Config{Level: LevelInfo, Output: &buf, JSON: true}
	l := New(cfg)

	l.Info("json test", "key", "value")

	var data map[string]any
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}

	if data["msg"] != "json test" {
		t.Error("JSON msg field incorrect")
	}
	if data["key"] != "value" {
		t.Error("JSON extra field incorrect")
	}
	if data["level"] != "INFO" {
		t.Error("JSON level incorrect")
	}
}
