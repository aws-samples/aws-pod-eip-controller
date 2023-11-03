// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"errors"
	"github.com/stretchr/testify/mock"
	"k8s.io/api/core/v1"
	"testing"
	"time"
)

func TestPodWorker_processNextItem(t *testing.T) {
	t.Run("given pod worker when queue is shut down then no item is processed", func(t *testing.T) {
		// indexer and handler are not set, they should not be called on queue shutdown
		worker := newTestWorker(nil)
		queue := newTestQueue(5, 500)
		queue.ShutDown()
		worker.run(queue, nil)
		// test is not blocking and continues
	})

	t.Run("given pod worker when handler returns error then queue is retried only max times", func(t *testing.T) {
		indexer := new(KeyGetterMock)
		// first get plus retries
		indexer.On("GetByKey", testKey).Return(nil, false, nil).Times(1 + maxQueueRetries)
		handler := new(HandlerMock)
		// first delete plus retries
		handler.On("Delete", testKey).Return(errors.New("test delete failure")).Times(1 + maxQueueRetries)

		worker := newTestWorker(handler)
		queue := newTestQueue(5, 100)
		queue.Add(testKey)

		go func() {
			// wait longer than rate limiter after that shut down the queue so run can exit
			time.Sleep(300 * time.Millisecond)
			queue.ShutDown()
		}()

		worker.run(queue, indexer)
		mock.AssertExpectationsForObjects(t)
	})
}

// --- helpers ---

func newTestWorker(handler PodHandler) *worker {
	return newWorker(noOpLogger, handler)
}

// --- mocks ---

type KeyGetterMock struct {
	mock.Mock
}

func (m *KeyGetterMock) GetByKey(key string) (item interface{}, exists bool, err error) {
	args := m.Called(key)
	return args.Get(0), args.Bool(1), args.Error(2)
}

type HandlerMock struct {
	mock.Mock
}

func (m *HandlerMock) AddOrUpdate(key string, pod v1.Pod) error {
	args := m.Called(key, pod)
	return args.Error(0)
}

func (m *HandlerMock) Delete(key string) error {
	args := m.Called(key)
	return args.Error(0)
}
