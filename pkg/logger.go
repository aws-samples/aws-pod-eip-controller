// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package pkg

import (
	"log/slog"
	"os"
)

func NewLogger(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
