// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type ENIClient interface {
	AssociateAddress(podKey, podIP string, addressPoolId *string) (string, error)
	AssociateFixedAddress(podKey, podIP string) (string, error)
	DisassociateAddress(podKey string) error
	HasAssociatedAddress(podIP string) (bool, error)
	HasAssociatedPodAddress(podIP string) (bool, error)
}

type PodEvent struct {
	Key             string
	Name            string
	Namespace       string
	Annotations     map[string]string
	Labels          map[string]string
	IP              string
	ResourceVersion string
}

func (p PodEvent) HasEIPAnnotation() bool {
	// pod annotations["aws-samples.github.com/aws-pod-eip-controller-type"]: "auto"
	if v, ok := p.Annotations[pkg.PodEIPAnnotationKey]; ok {
		return v == pkg.PodEIPAnnotationValue
	}
	return false
}

func (p PodEvent) IsEIPReclaimDisabled() bool {
	// pod annotations["aws-samples.github.com/aws-pod-eip-controller-reclaim"]: "false"
	if v, ok := p.Annotations[pkg.PodEIPReclaimAnnotationKey]; ok {
		return v == pkg.PodEIPReclaimAnnotationVal
	}
	// if pod eip mode in fixed mode, disable EIP reclaim
	if v, ok := p.Annotations[pkg.PodEIPModeAnnotationKey]; ok {
		return v == pkg.PodEIPModeAnnotationVal
	}
	return false
}

func (p PodEvent) GetEIPMode() string {
	// pod annotations["aws-samples.github.com/aws-pod-eip-controller-mode"]: "fixed"
	if v, ok := p.Annotations[pkg.PodEIPModeAnnotationKey]; ok {
		return v
	}
	return ""
}

func (p PodEvent) GetPublicIPLabel() (string, bool) {
	v, ok := p.Labels[pkg.PodPublicIPLabel]
	return v, ok
}

func (p PodEvent) GetAddressPoolId() *string {
	v, ok := p.Labels[pkg.PodAddressPoolAnnotationKey]
	if ok {
		val := strings.Clone(v)
		return &val
	}
	return nil
}

func NewPodEvent(key string, pod v1.Pod) PodEvent {
	return PodEvent{
		Key:             key,
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Annotations:     pod.Annotations,
		Labels:          pod.Labels,
		IP:              pod.Status.PodIP,
		ResourceVersion: pod.ResourceVersion,
	}
}

type Handler struct {
	logger        *slog.Logger
	coreClient    clientv1.CoreV1Interface
	eniClient     ENIClient
	cacheEventMap sync.Map
}

func NewHandler(logger *slog.Logger, coreClient clientv1.CoreV1Interface, eniClient ENIClient) *Handler {
	h := &Handler{
		logger:        logger.With("component", "handler"),
		coreClient:    coreClient,
		eniClient:     eniClient,
		cacheEventMap: sync.Map{},
	}
	return h
}

func (h *Handler) AddOrUpdate(key string, pod v1.Pod) error {
	if pod.Status.PodIP == "" {
		h.logger.Debug(fmt.Sprintf("pod %s in phase %s does not have IP, skipping", key, pod.Status.Phase))
		return nil
	}

	event := NewPodEvent(key, pod)
	if h.sameStatusInCache(event.Key, event) {
		h.logger.Debug(fmt.Sprintf("pod %s has same status in cache, skipping", event.Key))
		return nil
	}
	h.logger.Info(fmt.Sprintf("received pod add/update %s phase %s IP %s", key, pod.Status.Phase, pod.Status.PodIP))
	if err := h.addOrUpdateEvent(event); err != nil {
		return err
	}

	h.cacheEventMap.Store(event.Key, event)
	return nil
}

func (h *Handler) Delete(key string) error {
	h.logger.Info(fmt.Sprintf("received pod delete %s", key))

	if h.isEnableEIPReclaim(key) {
		h.logger.Info("pod eip reclaim is set to true, reclaiming eip")
		// reclaim pod eip
		if err := h.eniClient.DisassociateAddress(key); err != nil {
			return err
		}
	} else {
		h.logger.Info("pod eip reclaim is set to false, skipping reclaiming eip")
	}

	h.cacheEventMap.Delete(key)
	return nil
}

// sameStatusInCache checks if the pod event is the same as the one in cache
func (h *Handler) sameStatusInCache(key string, event PodEvent) bool {
	if v, ok := h.cacheEventMap.Load(key); ok {
		// resource version is the same, no need to process
		if v.(PodEvent).ResourceVersion == event.ResourceVersion {
			return true
		}
		// annotation is the same, no need to process
		if v.(PodEvent).Annotations[pkg.PodEIPAnnotationKey] == event.Annotations[pkg.PodEIPAnnotationKey] {
			return true
		}
		// annotation is not the same, but both are not set to auto, no need to process
		if v.(PodEvent).Annotations[pkg.PodEIPAnnotationKey] != pkg.PodEIPAnnotationValue && event.Annotations[pkg.PodEIPAnnotationKey] != pkg.PodEIPAnnotationValue {
			return true
		}
	}
	return false
}

// isEnableEIPReclaim checks if the pod annotation has enabled eip reclaim policy
func (h *Handler) isEnableEIPReclaim(key string) bool {
	if v, ok := h.cacheEventMap.Load(key); ok {
		// annotation is not the same, but both are not set, no need to process
		if v.(PodEvent).IsEIPReclaimDisabled() {
			return false
		}
	}
	// default enable EIP reclaim
	return true
}

func (h *Handler) addOrUpdateEvent(event PodEvent) error {
	isAssociated, err := h.eniClient.HasAssociatedAddress(event.IP)
	if err != nil {
		return fmt.Errorf("check if pod %s ip %s is associated: %w", event.Key, event.IP, err)
	}

	// pod does not have EIP annotation
	if !event.HasEIPAnnotation() {
		if isAssociated {
			if err := h.eniClient.DisassociateAddress(event.Key); err != nil {
				return fmt.Errorf("pod %s does not have eip annotation, disassocate address: %w", event.Key, err)
			}
			h.logger.Info(fmt.Sprintf("pod %s does not have eip annotation, address has been disassociated", event.Key))
		}
		if _, ok := event.GetPublicIPLabel(); ok {
			if err := h.patchPublicIPLabel(event, "remove", "None"); err != nil {
				return fmt.Errorf("pod %s does not have eip annotation, remove label: %w", event.Key, err)
			}
			h.logger.Info(fmt.Sprintf("pod %s does not have address associated, label has been removed", event.Key))
		}
		return nil
	}

	// pod has EIP annotation
	var publicIP string

	// check if the pod has fixed EIP mode, if yes, associate fixed address
	// otherwise, associate random address
	eipMode := event.GetEIPMode()
	switch eipMode {
	case pkg.PodEIPModeAnnotationVal:
		publicIP, err = h.eniClient.AssociateFixedAddress(event.Key, event.IP)
		if err != nil {
			return fmt.Errorf("associate fixed address %s: %v", event.Key, err)
		}
	default:
		publicIP, err = h.eniClient.AssociateAddress(event.Key, event.IP, event.GetAddressPoolId())
		if err != nil {
			return fmt.Errorf("associate address %s: %v", event.Key, err)
		}
	}

	// pod has already public IP label, check if the public IP matches
	if v, ok := event.GetPublicIPLabel(); ok {
		if v == publicIP {
			h.logger.Info(fmt.Sprintf("pod %s has already label %s=%s with correct IP", event.Key, pkg.PodPublicIPLabel, publicIP))
			return nil
		}
		// public IP label does not match the pod public IP
		h.logger.Warn(fmt.Sprintf("pod %s label %s=%s does not match its public ip is %s", event.Key, pkg.PodPublicIPLabel, v, publicIP))
	}
	if err := h.patchPublicIPLabel(event, "add", publicIP); err != nil {
		return err
	}
	h.logger.Info(fmt.Sprintf("pod %s patched with %s=%s label", event.Key, pkg.PodPublicIPLabel, publicIP))
	return nil
}

func (h *Handler) patchPublicIPLabel(event PodEvent, op, value string) error {
	patch := []byte(fmt.Sprintf(`[{"op":"%s","path":"/metadata/labels/%s","value":"%s"}]`, op, pkg.PodPublicIPLabel, value))
	if op == "remove" {
		patch = []byte(fmt.Sprintf(`[{"op":"%s","path":"/metadata/labels/%s"}]`, op, pkg.PodPublicIPLabel))
	}
	if _, err := h.coreClient.Pods(event.Namespace).Patch(context.Background(), event.Name, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch pod %s, %s label: %w", event.Key, op, err)
	}
	return nil
}
