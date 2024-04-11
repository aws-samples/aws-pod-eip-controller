// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"fmt"
	"log/slog"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const maxQueueRetries = 3

type PodHandler interface {
	AddOrUpdate(key string, pod v1.Pod) error
	Delete(key string) error
}

type worker struct {
	logger          *slog.Logger
	maxQueueRetries int
	handler         PodHandler
}

func newWorker(logger *slog.Logger, handler PodHandler) *worker {
	return &worker{
		logger:          logger.With("component", "worker"),
		maxQueueRetries: maxQueueRetries,
		handler:         handler,
	}
}

// run starts processing items from the queue, this call is blocking until queue is shut down
func (w *worker) run(queue workqueue.RateLimitingInterface, indexer cache.KeyGetter) {
	var wg sync.WaitGroup
	for {
		item, shutdown := queue.Get()
		if shutdown {
			w.logger.Info("received queue shut down")
			w.logger.Info("waiting for items to be processed")
			wg.Wait()
			w.logger.Info("all items processed")
			return
		}

		wg.Add(1)
		go func(key interface{}) {
			// done has to be called when we finished processing the item
			defer queue.Done(key)
			defer wg.Done()

			retries := queue.NumRequeues(key)
			if err := w.processItem(indexer, key.(string)); err != nil {
				w.logger.Error(fmt.Sprintf("process item: %v", err))
				if retries < maxQueueRetries {
					// calling done in defer, but not forget, we still can retry
					w.logger.Error(fmt.Sprintf("process item retry %d out of %d, retrying: %v", retries, maxQueueRetries, err))
					queue.AddRateLimited(key)
					return
				}
				w.logger.Error(fmt.Sprintf("process item retries exceeded, retried %d out of %d: %v", retries, maxQueueRetries, err))
			}

			// if no error occurs, or number of retries exceeded we forget this item, so it does not have any delay when another change happens
			queue.Forget(key)
		}(item)
	}
}

// processItem retrieves object by key from indexer and sends it to handler for processing
func (w *worker) processItem(indexer cache.KeyGetter, key string) error {
	var pod v1.Pod
	obj, exists, err := indexer.GetByKey(key)
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
