package k8s

import (
	"testing"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testController = PodController{logger: noOpLogger}
)

func Test_addAddEvent(t *testing.T) {
	t.Run("given pod when it does not have ip then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		assert.False(t, testController.addAddEvent(testKey, getPod("", annotations, nil)))
	})

	t.Run("given pod when it does not have eip annotation then false is returned", func(t *testing.T) {
		assert.False(t, testController.addAddEvent(testKey, getPod("10.0.0.1", nil, nil)))
	})

	t.Run("given pod when it has eip annotation with non 'auto' value then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: "test"}
		assert.False(t, testController.addAddEvent(testKey, getPod("10.0.0.1", annotations, nil)))
	})

	t.Run("given pod when it has ip and eip annotation then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		assert.True(t, testController.addAddEvent(testKey, getPod("10.0.0.1", annotations, nil)))
	})
}

func Test_addUpdateEvent(t *testing.T) {
	t.Run("given new and old pods when they do not have ip then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", annotations, nil)
		newPod := getPod("", annotations, nil)
		assert.False(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	/*
		t.Run("given new and old pods when the ip was removed then true is returned", func(t *testing.T) {
			annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
			oldPod := getPod("10.0.0.1", annotations, nil)
			newPod := getPod("", annotations, nil)
			assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
		})*/

	t.Run("given new and old pods when the ip was added then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", annotations, nil)
		newPod := getPod("10.0.0.1", annotations, nil)
		assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was removed then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("10.0.0.1", annotations, nil)
		newPod := getPod("10.0.0.1", nil, nil)
		assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was added then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("10.0.0.1", nil, nil)
		newPod := getPod("10.0.0.1", annotations, nil)
		assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the annotation was added but the ip is missing then false is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		oldPod := getPod("", nil, nil)
		newPod := getPod("", annotations, nil)
		assert.False(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the label was removed then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		labels := map[string]string{pkg.PodPublicIPLabel: "9.9.9.9"}
		oldPod := getPod("10.0.0.1", annotations, labels)
		newPod := getPod("10.0.0.1", annotations, nil)
		assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})

	t.Run("given new and old pods when the label was added then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		labels := map[string]string{pkg.PodPublicIPLabel: "9.9.9.9"}
		oldPod := getPod("10.0.0.1", annotations, nil)
		newPod := getPod("10.0.0.1", annotations, labels)
		assert.True(t, testController.addUpdateEvent(testKey, oldPod, newPod))
	})
}

func Test_addDeleteEvent(t *testing.T) {
	t.Run("given pod when it has annotation then true is returned", func(t *testing.T) {
		annotations := map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValue}
		assert.True(t, testController.addDeleteEvent(testKey, getPod("", annotations, nil)))
	})

	/*
		t.Run("given pod when it has annotation with non auto value then false is returned", func(t *testing.T) {
			annotations := map[string]string{pkg.PodEIPAnnotationKey: "test"}
			assert.False(t, testController.addDeleteEvent(testKey, getPod("", annotations, nil)))
		})*/

	t.Run("given pod when it does not have annotation then false is returned", func(t *testing.T) {
		assert.False(t, testController.addAddEvent(testKey, getPod("10.0.0.1", nil, nil)))
	})
}

func getPod(ip string, annotations, labels map[string]string) interface{} {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: v1.PodSpec{},
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}
