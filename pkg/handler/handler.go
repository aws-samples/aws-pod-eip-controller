// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	podChannelBuff = 10
)

type ENIClient interface {
	AssociateAddress(podKey, podIP string) (string, error)
	DisassociateAddress(podKey string) error
	HasAssociatedAddress(podIP string) (bool, error)
}

type PodEvent struct {
	Key         string
	Name        string
	Namespace   string
	Annotations map[string]string
	Labels      map[string]string
	IP          string
}

func (p PodEvent) HasEIPAnnotation() bool {
	if v, ok := p.Annotations[pkg.PodEIPAnnotationKey]; ok {
		return v == pkg.PodEIPAnnotationValue
	}
	return false
}

func (p PodEvent) GetPublicIPLabel() (string, bool) {
	v, ok := p.Labels[pkg.PodPublicIPLabel]
	return v, ok
}

func NewPodEvent(key string, pod v1.Pod) PodEvent {
	return PodEvent{
		Key:         key,
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		Annotations: pod.Annotations,
		Labels:      pod.Labels,
		IP:          pod.Status.PodIP,
	}
}

type Handler struct {
	logger     *slog.Logger
	mux        *sync.RWMutex
	coreClient clientv1.CoreV1Interface
	eniClient  ENIClient
	podEvents  map[string]chan<- PodEvent
}

func NewHandler(logger *slog.Logger, coreClient clientv1.CoreV1Interface, eniClient ENIClient) *Handler {
	h := &Handler{
		logger:     logger.With("component", "handler"),
		mux:        &sync.RWMutex{},
		coreClient: coreClient,
		eniClient:  eniClient,
		podEvents:  make(map[string]chan<- PodEvent),
	}
	return h
}

func (h *Handler) AddOrUpdate(key string, pod v1.Pod) error {
	if pod.Status.PodIP == "" {
		h.logger.Debug(fmt.Sprintf("pod %s in phase %s does not have IP, skipping", key, pod.Status.Phase))
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			// recover just in case channel has been already closed. this should not happen though, events are
			// received from the queue in sequence, if the add/update is blocked (channel is full), we should not
			// be able to receive delete, hence close channel
			h.logger.Error(fmt.Sprintf("recovereed add/update %s key: %v", key, r))
		}
	}()

	h.logger.Info(fmt.Sprintf("received pod add/update %s phase %s IP %s", key, pod.Status.Phase, pod.Status.PodIP))
	h.getPodChannel(key) <- NewPodEvent(key, pod)
	return nil
}

func (h *Handler) Delete(key string) error {
	h.logger.Info(fmt.Sprintf("received pod delete %s, deleting channel", key))
	h.deletePodChannel(key)
	return nil
}

func (h *Handler) getPodChannel(key string) chan<- PodEvent {
	h.mux.Lock()
	defer h.mux.Unlock()
	if c, ok := h.podEvents[key]; ok {
		h.logger.Debug(fmt.Sprintf("using existing channel for %s", key))
		return c
	}
	h.podEvents[key] = h.newPodChannel(key)
	h.logger.Debug(fmt.Sprintf("created new channel for %s", key))
	return h.podEvents[key]
}

func (h *Handler) deletePodChannel(key string) {
	h.mux.Lock()
	defer h.mux.Unlock()
	if c, ok := h.podEvents[key]; ok {
		close(c)
		h.logger.Debug(fmt.Sprintf("%s channel closed", key))
		delete(h.podEvents, key)
		h.logger.Debug(fmt.Sprintf("key %s deleted from pod events map", key))
		return
	}
	h.logger.Info(fmt.Sprintf("channel for %s not found", key))
}

// newPodChannel creates new channel for a specific pod and return it
func (h *Handler) newPodChannel(key string) chan<- PodEvent {
	events := make(chan PodEvent, podChannelBuff)
	go func() {
		for e := range events {
			h.logger.Info(fmt.Sprintf("got pod event %s IP %s", e.Key, e.IP))
			if err := h.addOrUpdateEvent(e); err != nil {
				h.logger.Error(err.Error())
			}
		}
		// channel closed, pod has been deleted
		h.logger.Debug(fmt.Sprintf("finished processing %s events", key))
		if err := h.eniClient.DisassociateAddress(key); err != nil {
			h.logger.Error(fmt.Sprintf("disassociate address %s: %v", key, err))
			return
		}
	}()
	return events
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
	publicIP, err := h.eniClient.AssociateAddress(event.Key, event.IP)
	if err != nil {
		return fmt.Errorf("associate address %s: %v", event.Key, err)
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
	if value == "remove" {
		patch = []byte(fmt.Sprintf(`[{"op":"%s","path":"/metadata/labels/%s"}]`, op, pkg.PodPublicIPLabel))
	}
	if _, err := h.coreClient.Pods(event.Namespace).Patch(context.Background(), event.Name, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch pod %s, %s label: %w", event.Key, op, err)
	}
	return nil
}
