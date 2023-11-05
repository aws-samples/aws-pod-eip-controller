// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"testing"
)

func TestPodController_addFunc(t *testing.T) {
	annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}

	t.Run("given pod when it is added then it will be on the queue", func(t *testing.T) {
		controller := newTestController(50, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given multiple different pods when they are added then they will be on the queue", func(t *testing.T) {
		controller := newTestController(50, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(addPodName(pod, "test1"))
		controller.addFunc(addPodName(pod, "test2"))
		controller.addFunc(addPodName(pod, "test3"))

		assert.Equal(t, 3, controller.queue.Len())
		assert.Equal(t, "default/test1", getQueueItem(controller.queue))
		assert.Equal(t, "default/test2", getQueueItem(controller.queue))
		assert.Equal(t, "default/test3", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it is added multiple times then they it will appear on the queue only once", func(t *testing.T) {
		controller := newTestController(50, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(pod)
		controller.addFunc(pod)
		controller.addFunc(pod)

		// same pod, will be on the queue only once
		assert.Equal(t, 1, controller.queue.Len())
		item1, _ := controller.queue.Get()
		assert.Equal(t, 0, controller.queue.Len())
		controller.queue.Forget(item1)
		controller.queue.Done(item1)
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it is added while first is being processed then they it will appear on the queue after done is called", func(t *testing.T) {
		controller := newTestController(50, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		item1, _ := controller.queue.Get()

		// adding same pod, but first one has not been marked as done
		controller.addFunc(pod)
		assert.Equal(t, 0, controller.queue.Len())

		// finished work with first one
		controller.queue.Forget(item1)
		controller.queue.Done(item1)

		assert.Equal(t, 1, controller.queue.Len())
	})

	t.Run("given pod when it is added multiple times then it will be on the queue only once", func(t *testing.T) {
		controller := newTestController(50, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(pod)
		controller.addFunc(pod)
		controller.addFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())

		controller.addFunc(pod)
		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it does not have ip then it is not added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("", annotations)
		controller.addFunc(pod)

		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it does not have eip annotation then it is not added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", nil)
		controller.addFunc(pod)

		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it has eip annotation with non 'auto' value then it is not added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", map[string]string{pkg.PodEIPAnnotationKey: "test"})
		controller.addFunc(pod)

		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it has ip and eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.addFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})
}

func TestPodController_addUpdateEvent(t *testing.T) {
	annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}

	t.Run("given pod when it has ip and no eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", nil)
		controller.updateFunc(pod, pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it has ip and eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.updateFunc(pod, pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it does not have ip but has eip annotation then it is not added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("", annotations)
		controller.updateFunc(pod, pod)

		assert.Equal(t, 0, controller.queue.Len())
	})
}

func TestPodController_addDeleteEvent(t *testing.T) {
	annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}

	t.Run("given pod when it has ip and no eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", nil)
		controller.deleteFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it has ip and eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("10.0.0.1", annotations)
		controller.deleteFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it does not have ip but has eip annotation then it is added to the queue", func(t *testing.T) {
		controller := newTestController(5, 500)
		pod := getPod("", annotations)
		controller.deleteFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})
}

// --- helpers ---

func newTestController(queueBaseMs, queueMaxDelayMs int) *PodController {
	return &PodController{logger: noOpLogger, queue: newTestQueue(queueBaseMs, queueMaxDelayMs)}
}

func getQueueItem(queue workqueue.RateLimitingInterface) string {
	item, _ := queue.Get()
	queue.Done(item)
	return item.(string)
}

func addPodName(pod interface{}, name string) interface{} {
	v := *pod.(*v1.Pod)
	v.Name = name
	return &v
}

func getPod(ip string, annotations map[string]string) interface{} {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: v1.PodSpec{},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}
