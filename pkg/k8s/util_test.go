package k8s

import (
	"io"
	"log/slog"
)

var (
	testKey    = "default/test-pod"
	noOpLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
)
