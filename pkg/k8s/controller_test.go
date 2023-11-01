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
	pod := getPod("10.0.0.1", annotations)

	t.Run("given pod when it is added then it will be on the queue", func(t *testing.T) {
		controller := newTestController(50, 500)
		controller.addFunc(pod)

		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given multiple different pods when they are added then they will be on the queue", func(t *testing.T) {
		controller := newTestController(50, 500)
		controller.addFunc(addPodName(pod, "test1"))
		controller.addFunc(addPodName(pod, "test2"))
		controller.addFunc(addPodName(pod, "test3"))

		assert.Equal(t, 3, controller.queue.Len())
		assert.Equal(t, "default/test1", getQueueItem(controller.queue))
		assert.Equal(t, "default/test2", getQueueItem(controller.queue))
		assert.Equal(t, "default/test3", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})

	t.Run("given pod when it is added multiple times then it will be on the queue only once", func(t *testing.T) {
		controller := newTestController(50, 500)
		controller.addFunc(pod)
		controller.addFunc(pod)
		controller.addFunc(pod)

		// we are using add rate limited, keys should be available only after 'base' time
		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())

		controller.addFunc(pod)
		assert.Equal(t, 1, controller.queue.Len())
		assert.Equal(t, "default/test", getQueueItem(controller.queue))
		assert.Equal(t, 0, controller.queue.Len())
	})
}

func TestPodController_addAddEvent(t *testing.T) {
	t.Run("given pod when it does not have ip then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		controller := newTestController(5, 500)
		assert.False(t, controller.addAddEvent(testKey, getPod("", annotations)))
	})

	t.Run("given pod when it does not have eip annotation then false is returned", func(t *testing.T) {
		controller := newTestController(5, 500)
		assert.False(t, controller.addAddEvent(testKey, getPod("10.0.0.1", nil)))
	})

	t.Run("given pod when it has eip annotation with non 'auto' value then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: "test"}
		controller := newTestController(5, 500)
		assert.False(t, controller.addAddEvent(testKey, getPod("10.0.0.1", annotations)))
	})

	t.Run("given pod when it has ip and eip annotation then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		controller := newTestController(5, 500)
		assert.True(t, controller.addAddEvent(testKey, getPod("10.0.0.1", annotations)))
	})
}

func TestPodController_addUpdateEvent(t *testing.T) {
	t.Run("given new and old pods when they do not have ip then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", annotations)
		newPod := getPod("", annotations)
		controller := newTestController(5, 500)
		assert.False(t, controller.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the ip was added then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", annotations)
		newPod := getPod("10.0.0.1", annotations)
		controller := newTestController(5, 500)
		assert.True(t, controller.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was removed then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("10.0.0.1", annotations)
		newPod := getPod("10.0.0.1", nil)
		controller := newTestController(5, 500)
		assert.True(t, controller.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was added then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("10.0.0.1", nil)
		newPod := getPod("10.0.0.1", annotations)
		controller := newTestController(5, 500)
		assert.True(t, controller.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was added but the ip is missing then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", nil)
		newPod := getPod("", annotations)
		controller := newTestController(5, 500)
		assert.False(t, controller.addUpdateEvent(testKey, oldPod, newPod))
	})
}

func TestPodController_addDeleteEvent(t *testing.T) {
	t.Run("given pod when it has annotation then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		controller := newTestController(5, 500)
		assert.True(t, controller.addDeleteEvent(testKey, getPod("", annotations)))
	})

	t.Run("given pod when it does not have annotation then true is returned", func(t *testing.T) {
		controller := newTestController(5, 500)
		assert.True(t, controller.addDeleteEvent(testKey, getPod("10.0.0.1", nil)))
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
