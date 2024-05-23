// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/aws"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type ENIClient interface {
	AssociateAddress(aws.AssociateAddressOptions) (string, error)
	DisassociateAddress(aws.DisassociateAddressOptions) error
}

type Handler struct {
	logger     *slog.Logger
	coreClient clientv1.CoreV1Interface
	eniClient  ENIClient
}

func NewHandler(logger *slog.Logger, coreClient clientv1.CoreV1Interface, eniClient ENIClient) *Handler {
	h := &Handler{
		logger:     logger.With("component", "handler"),
		coreClient: coreClient,
		eniClient:  eniClient,
	}
	return h
}

func (h *Handler) AddOrUpdate(key string, pod v1.Pod) error {
	if pod.Status.PodIP == "" {
		h.logger.Debug(fmt.Sprintf("pod %s in phase %s does not have IP, skipping", key, pod.Status.Phase))
		return nil
	}

	event := NewPodEvent(key, pod)
	if !h.hasChange(event) {
		h.logger.Debug(fmt.Sprintf("pod %s has not change", event.Key))
		return nil
	}
	h.logger.Info(fmt.Sprintf("received pod add/update %s phase %s IP %s", key, pod.Status.Phase, pod.Status.PodIP))
	if err := h.addOrUpdateEvent(event); err != nil {
		return err
	}
	return nil
}

func (h *Handler) Delete(key string) error {
	h.logger.Info(fmt.Sprintf("received pod delete %s", key))
	if err := h.DisassociateAddress(NewPodEvent(key, v1.Pod{})); err != nil {
		return err
	}
	return nil
}

// hasChange checks if the pod event is the same
func (h *Handler) hasChange(event PodEvent) bool {
	pecAnnotation, _ := event.GetPECTypeAnnotation()
	pecLabel, _ := event.GetPECTypeLabel()
	if pecAnnotation != pecLabel {
		h.logger.Debug(fmt.Sprintf("pec type annotation %s and label %s are different", pecAnnotation, pecLabel))
		return true
	}
	switch pecAnnotation {
	// if the pod has auto annotation, check if the address pool id or fixed tag has changed
	case pkg.PodEIPAnnotationValueAuto:
		addressPoolIDAnnotation, _ := event.GetAddressPoolIdAnnotation()
		addressPoolIDLabel, _ := event.GetAddressPoolIdLabel()
		if addressPoolIDAnnotation != addressPoolIDLabel {
			h.logger.Debug(fmt.Sprintf("address pool id annotation %s and label %s are different", addressPoolIDAnnotation, addressPoolIDLabel))
			return true
		}
	// if the pod has fixed tag annotation, check if the fixed tag has changed
	case pkg.PodEIPAnnotationValueFixedTag:
		fixedTagAnnotation, _ := event.GetFixedTagAnnotation()
		fixedTagLabel, _ := event.GetFixedTagLabel()
		if fixedTagAnnotation != fixedTagLabel {
			h.logger.Debug(fmt.Sprintf("fixed tag annotation %s and label %s are different", fixedTagAnnotation, fixedTagLabel))
			return true
		}
	case pkg.PodEIPAnnotationValueFixedTagValue:
		fixedTagValueAnnotation, _ := event.GetFixedTagValueAnnotation()
		fixedTagValueLabel, _ := event.GetFixedTagValueLabel()
		if fixedTagValueAnnotation != fixedTagValueLabel {
			h.logger.Debug(fmt.Sprintf("fixed tag value annotation %s and label %s are different", fixedTagValueAnnotation, fixedTagValueLabel))
			return true
		}
	}
	return false
}

func (h *Handler) addOrUpdateEvent(event PodEvent) error {
	// DisassociateAddress
	if err := h.DisassociateAddress(event); err != nil {
		h.logger.Error(fmt.Sprintf("disassociate address for pod: %s fail: %v", event.Key, err))
		return err
	}

	// AssociateAddress
	err := h.AssociateAddress(event)
	if err != nil {
		h.logger.Error(fmt.Sprintf("associate address for pod: %s fail: %v", event.Key, err))
		return err
	}
	return nil
}

func (h *Handler) DisassociateAddress(event PodEvent) error {
	if err := h.eniClient.DisassociateAddress(aws.DisassociateAddressOptions{
		PodKey: event.Key,
	}); err != nil {
		return fmt.Errorf("disassociate address %s: %w", event.Key, err)
	}
	h.logger.Debug(fmt.Sprintf("disassociate address from pod %s", event.Key))
	// remove all relate labels
	labelPatches := make([]labelPatch, 0)
	if _, exist := event.GetPECTypeLabel(); exist {
		labelPatches = append(labelPatches, labelPatch{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", pkg.PodEIPAnnotationKeyLabel),
		})
	}
	if _, exist := event.GetAddressPoolIdLabel(); exist {
		labelPatches = append(labelPatches, labelPatch{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", pkg.PodAddressPoolIDLabel),
		})
	}
	if _, exist := event.GetPublicIPLabel(); exist {
		labelPatches = append(labelPatches, labelPatch{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", pkg.PodPublicIPLabel),
		})
	}
	if _, exist := event.GetFixedTagLabel(); exist {
		labelPatches = append(labelPatches, labelPatch{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", pkg.PodFixedTagLabel),
		})
	}
	if _, exist := event.GetFixedTagValueLabel(); exist {
		labelPatches = append(labelPatches, labelPatch{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", pkg.PodFixedTagValueLabel),
		})
	}
	if len(labelPatches) == 0 {
		return nil
	}
	if err := h.patchPodLabel(event, labelPatches); err != nil {
		return fmt.Errorf("patch pod %s: %w", event.Key, err)
	}
	return nil
}

func (h *Handler) AssociateAddress(event PodEvent) error {
	pecType, _ := event.GetPECTypeAnnotation()
	if !pkg.ValidPECType(pecType) {
		h.logger.Info(fmt.Sprintf("invalid pec type %s for pod %s", pecType, event.Key))
		return nil
	}

	addressPoolID, _ := event.GetAddressPoolIdAnnotation()
	addressPoolIDTmp := addressPoolID
	if addressPoolIDTmp == "" {
		addressPoolIDTmp = "amazon"
	}
	tagKey, _ := event.GetFixedTagAnnotation()
	tagValueKey, _ := event.GetFixedTagValueAnnotation()
	publicIP, err := h.eniClient.AssociateAddress(aws.AssociateAddressOptions{
		PodKey:        event.Key,
		PodIP:         event.IP,
		HostIP:        event.HostIP,
		AddressPoolId: addressPoolIDTmp,
		PECType:       pecType,
		TagKey:        tagKey,
		TagValueKey:   tagValueKey,
	})
	if err != nil {
		return fmt.Errorf("associate address %s: %w", event.Key, err)
	}
	h.logger.Debug(fmt.Sprintf("associate address %s to pod %s", publicIP, event.Key))

	// add labels
	labelPatches := make([]labelPatch, 0)
	if pecType == pkg.PodEIPAnnotationValueAuto {
		if addressPoolID > "" {
			labelPatches = append(labelPatches, labelPatch{
				Op:    "add",
				Path:  fmt.Sprintf("/metadata/labels/%s", pkg.PodAddressPoolIDLabel),
				Value: addressPoolID,
			})
		}
	}
	if pecType == pkg.PodEIPAnnotationValueFixedTag {
		if tagKey > "" {
			labelPatches = append(labelPatches, labelPatch{
				Op:    "add",
				Path:  fmt.Sprintf("/metadata/labels/%s", pkg.PodFixedTagLabel),
				Value: tagKey,
			})
		}
	}
	if pecType == pkg.PodEIPAnnotationValueFixedTagValue {
		if tagValueKey > "" {
			labelPatches = append(labelPatches, labelPatch{
				Op:    "add",
				Path:  fmt.Sprintf("/metadata/labels/%s", pkg.PodFixedTagValueLabel),
				Value: tagValueKey,
			})
		}
	}
	if pecType > "" {
		labelPatches = append(labelPatches, labelPatch{
			Op:    "add",
			Path:  fmt.Sprintf("/metadata/labels/%s", pkg.PodEIPAnnotationKeyLabel),
			Value: pecType,
		})
	}
	if publicIP > "" {
		labelPatches = append(labelPatches, labelPatch{
			Op:    "add",
			Path:  fmt.Sprintf("/metadata/labels/%s", pkg.PodPublicIPLabel),
			Value: publicIP,
		})
	}
	if len(labelPatches) == 0 {
		return nil
	}
	if err := h.patchPodLabel(event, labelPatches); err != nil {
		return fmt.Errorf("patch pod %s: %w", event.Key, err)
	}
	return nil
}

type labelPatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

func (h *Handler) patchPodLabel(event PodEvent, lables []labelPatch) error {
	patch, err := json.Marshal(lables)
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	if _, err := h.coreClient.Pods(event.Namespace).Patch(context.Background(), event.Name, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch pod %s, %s error: %w", event.Key, patch, err)
	}
	return nil
}
