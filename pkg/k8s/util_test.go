// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"io"
	"k8s.io/client-go/util/workqueue"
	"log/slog"
	"time"
)

var (
	testKey    = "default/test-pod"
	noOpLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
)

func newTestQueue(baseMs, maxDelayMs int) workqueue.RateLimitingInterface {
	return workqueue.NewRateLimitingQueue(
		workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(time.Duration(baseMs)*time.Millisecond, time.Duration(maxDelayMs)*time.Millisecond),
		))
}
