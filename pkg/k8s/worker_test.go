package k8s

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/api/core/v1"
	"testing"
	"time"
)

func TestPodWorker_processNextItem(t *testing.T) {
	t.Run("given pod worker when queue is shut down then no item is processed and false is returned", func(t *testing.T) {
		// indexer and handler are not set, they should not be called on queue shutdown
		worker := newTestWorker(nil)
		queue := newTestQueue(5, 500)
		queue.ShutDown()
		assert.False(t, worker.processNextItem(queue, nil))
	})

	t.Run("given pod worker when handler returns error then queue is retried only max times", func(t *testing.T) {
		indexer := new(KeyGetterMock)
		indexer.On("GetByKey", testKey).Return(nil, false, nil)
		handler := new(HandlerMock)
		handler.On("Delete", testKey).Return(errors.New("test delete failure"))

		worker := newTestWorker(handler)
		queue := newTestQueue(5, 500)
		queue.Add(testKey)

		// run process next item max retries and check if queue is empty after (item is forgotten and removed)
		for i := 0; i <= maxQueueRetries; i++ {
			assert.True(t, worker.processNextItem(queue, indexer))
		}

		// wait longer than rate limiter for the queue and check if the item has been removed
		time.Sleep(600 * time.Millisecond)
		assert.Equal(t, 0, queue.Len())
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
