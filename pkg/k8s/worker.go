// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"fmt"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"log/slog"
)

const maxQueueRetries = 3

type PodHandler interface {
	AddOrUpdate(key string, pod v1.Pod) error
	Delete(key string) error
}

type podWorker struct {
	logger          *slog.Logger
	queue           workqueue.RateLimitingInterface
	maxQueueRetries int
	indexer         cache.KeyGetter
	handler         PodHandler
}

func newPodWorker(logger *slog.Logger, queue workqueue.RateLimitingInterface, indexer cache.KeyGetter, handler PodHandler) *podWorker {
	return &podWorker{
		logger:          logger.With("component", "worker"),
		queue:           queue,
		maxQueueRetries: maxQueueRetries,
		indexer:         indexer,
		handler:         handler,
	}
}

func (w *podWorker) run() {
	for w.processNextItem() {
	}
	// TODO - send shutdown signal to the handler
}

func (w *podWorker) processNextItem() bool {
	// rate limiting queue life cycle
	// https://docs.bitnami.com/tutorials/_next/static/images/key-lifecicle-workqueue-4e3c30ed8a09c28cceb2247fb776b548.png.webp
	key, shutdown := w.queue.Get()
	if shutdown {
		w.logger.Info("received queue shut down")
		return false
	}

	// done has to be called when we finished processing the item
	defer w.queue.Done(key)

	retries := w.queue.NumRequeues(key)
	if err := w.processItem(key.(string)); err != nil {
		w.logger.Error(fmt.Sprintf("process item: %v", err))
		if retries < maxQueueRetries {
			// calling done in defer, but not forget, we still can retry
			w.logger.Error(fmt.Sprintf("process item retry %d out of %d, retrying: %v", retries, maxQueueRetries, err))
			w.queue.AddRateLimited(key)
			return true
		}
		w.logger.Error(fmt.Sprintf("process item retries exceeded, retried %d out of %d: %v", retries, maxQueueRetries, err))
	}

	// if no error occurs, or number of retries exceeded we forget this item, so it does not have any delay when another change happens
	w.queue.Forget(key)
	return true
}

func (w *podWorker) processItem(key string) error {
	var pod v1.Pod
	obj, exists, err := w.indexer.GetByKey(key)
	if err != nil {
		return fmt.Errorf("get object by key %s from store: %w", key, err)
	}
	if !exists {
		w.logger.Debug(fmt.Sprintf("key %s not found in store, calling handler delete", key))
		return w.handler.Delete(key)
	}
	if obj != nil {
		pod = *obj.(*v1.Pod)
	}
	w.logger.Debug(fmt.Sprintf("key %s found in store, calling handler add/update", key))
	return w.handler.AddOrUpdate(key, pod)
}
