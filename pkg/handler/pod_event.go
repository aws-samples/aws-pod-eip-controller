package handler

import (
	"strings"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	v1 "k8s.io/api/core/v1"
)

type PodEvent struct {
	Key             string
	Name            string
	Namespace       string
	Annotations     map[string]string
	Labels          map[string]string
	IP              string
	HostIP          string
	ResourceVersion string
}

func (p PodEvent) GetPECTypeAnnotation() (string, bool) {
	if v, ok := p.Annotations[pkg.PodEIPAnnotationKey]; ok {
		if pkg.ValidPECType(v) {
			return v, true
		}
	}
	return "", false
}

func (p PodEvent) GetPECTypeLabel() (string, bool) {
	if v, ok := p.Labels[pkg.PodEIPAnnotationKeyLabel]; ok {
		return v, true
	}
	return "", false
}

func (p PodEvent) GetAddressPoolIdAnnotation() (string, bool) {
	if v, ok := p.Annotations[pkg.PodAddressPoolAnnotationKey]; ok {
		return v, true
	}
	return "", false
}

func (p PodEvent) GetAddressPoolIdLabel() (string, bool) {
	v, ok := p.Labels[pkg.PodAddressPoolIDLabel]
	if ok {
		val := strings.Clone(v)
		return val, true
	}
	return "", false
}

func (p PodEvent) GetFixedTagAnnotation() (string, bool) {
	if v, ok := p.Annotations[pkg.PodAddressFixedTagAnnotationKey]; ok {
		return v, true
	}
	return "", false
}

func (p PodEvent) GetFixedTagLabel() (string, bool) {
	v, ok := p.Labels[pkg.PodFixedTagLabel]
	if ok {
		val := strings.Clone(v)
		return val, true
	}
	return "", false
}

func (p PodEvent) GetFixedTagValueAnnotation() (string, bool) {
	if v, ok := p.Annotations[pkg.PodAddressFixedTagValueAnnotationKey]; ok {
		return v, true
	}
	return "", false
}

func (p PodEvent) GetFixedTagValueLabel() (string, bool) {
	v, ok := p.Labels[pkg.PodFixedTagValueLabel]
	if ok {
		val := strings.Clone(v)
		return val, true
	}
	return "", false
}

func (p PodEvent) GetPublicIPLabel() (string, bool) {
	if v, ok := p.Labels[pkg.PodPublicIPLabel]; ok {
		return v, true
	}
	return "", false
}

func NewPodEvent(key string, pod v1.Pod) PodEvent {
	podEvent := PodEvent{
		Key:             key,
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Annotations:     pod.Annotations,
		Labels:          pod.Labels,
		IP:              pod.Status.PodIP,
		HostIP:          pod.Status.HostIP,
		ResourceVersion: pod.ResourceVersion,
	}
	return podEvent
}
