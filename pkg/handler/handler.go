// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package handler

import (
	"github.com/aws-samples/aws-pod-eip-controller/pkg/service"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

type Handler struct {
	ChannelSize    int32
	EC2Service     *service.EC2Service
	ShiedService   *service.ShiedService
	K8sClient      *kubernetes.Clientset
	ProcessChannel []chan event
	EipStatusMap   []map[string]event
}

func (h *Handler) init() {
	h.ProcessChannel = make([]chan event, h.ChannelSize)
	for i := 0; i < int(h.ChannelSize); i++ {
		h.ProcessChannel[i] = make(chan event, 100)
	}
	h.EipStatusMap = make([]map[string]event, h.ChannelSize)
	for i := 0; i < int(h.ChannelSize); i++ {
		h.EipStatusMap[i] = make(map[string]event)
		go h.process(i)
	}
}

func (h *Handler) process(i int) {
	var e event
	for e = range h.ProcessChannel[i] {
		logrus.WithFields(logrus.Fields{
			"event": e,
		}).Info("process event")
		val, ok := h.EipStatusMap[i][e.PodIP]
		if ok && val.ResourceVersion == e.ResourceVersion {
			logrus.Info("same resource version")
			continue
		}
		if !ok {
			err := e.Process(nil, h.EC2Service, h.ShiedService, h.K8sClient)
			if err != nil {
				logrus.Error(err)
				continue
			}
		} else {
			e.Process(&val, h.EC2Service, h.ShiedService, h.K8sClient)
		}
		h.EipStatusMap[i][e.PodIP] = e
	}
}

func (h *Handler) insert2Queue(event event) {
	hash := event.PodIP2Int() % uint32(h.ChannelSize)
	h.ProcessChannel[hash] <- event
	logrus.WithFields(logrus.Fields{
		"event": event,
		"has":   hash,
	}).Info("insert event to queue")
}

func (h *Handler) HandleEvent(obj *unstructured.Unstructured, oldObj *unstructured.Unstructured, action string) (err error) {
	phase, exist, err := unstructured.NestedString(obj.Object, "status", "phase")
	if err != nil || !exist {
		return
	}
	podIP, exist, err := unstructured.NestedString(obj.Object, "status", "podIP")
	if err != nil {
		return
	}
	if exist && len(podIP) == 0 {
		logrus.Info("podIP is empty")
		return
	}
	logrus.WithFields(logrus.Fields{
		"name":             obj.GetName(),
		"uid":              obj.GetUID(),
		"resource_version": obj.GetResourceVersion(),
		"annotions":        obj.GetAnnotations(),
		"phase":            phase,
		"podIP":            podIP,
		"action":           action,
	}).Info()
	event := event{
		Namespace:       obj.GetNamespace(),
		PodName:         obj.GetName(),
		PodIP:           podIP,
		ResourceVersion: obj.GetResourceVersion(),
		AttachIP:        false,
		ShiedAdv:        false,
	}
	switch action {
	case "add":
		annotations := obj.GetAnnotations()
		if val, ok := annotations["aws-samples.github.com/aws-pod-eip-controller-type"]; ok && val == "auto" {
			event.AttachIP = true
			if val, ok := annotations["aws-samples.github.com/aws-pod-eip-controller-shield"]; ok && val == "advanced" {
				event.ShiedAdv = true
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"obj": obj,
			}).Info("ignore add event with no annotation")
			return
		}
	case "update":
		annotations := obj.GetAnnotations()
		oldAnnotations := oldObj.GetAnnotations()
		if val, ok := annotations["aws-samples.github.com/aws-pod-eip-controller-type"]; ok && val == "auto" {
			event.AttachIP = true
			if val, ok := annotations["aws-samples.github.com/aws-pod-eip-controller-shield"]; ok && val == "advanced" {
				event.ShiedAdv = true
			}
		} else {
			if val, ok := oldAnnotations["aws-samples.github.com/aws-pod-eip-controller-type"]; ok && val == "auto" {
				event.AttachIP = false
			} else {
				logrus.WithFields(logrus.Fields{
					"newobj": obj,
					"oldobj": oldObj,
				}).Info("ignore update event with no annotation")
				return
			}
		}
	case "delete":
		event.ShiedAdv = false
		event.AttachIP = false
	}
	h.insert2Queue(event)
	return
}

func NewHandler(channelSize int32, vpcid string, region string, clusterName string, client *kubernetes.Clientset) (handler *Handler, err error) {
	ec2Service, err := service.NewEC2Service(vpcid, region, clusterName)
	if err != nil {
		return nil, err
	}
	shieldService, err := service.NewShieldService(vpcid, region)
	if err != nil {
		return nil, err
	}
	handler = &Handler{
		ChannelSize:  channelSize,
		EC2Service:   ec2Service,
		ShiedService: shieldService,
		K8sClient:    client,
	}
	handler.init()
	return handler, nil
}
