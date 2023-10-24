// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package k8s

import (
	"context"
	"fmt"
	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"log/slog"
	"time"
)

const (
	reSyncInformer time.Duration = 0
)

type PodController struct {
	logger   *slog.Logger
	queue    workqueue.RateLimitingInterface
	informer cache.SharedIndexInformer
	worker   *podWorker
}

func NewPodController(logger *slog.Logger, clientset *kubernetes.Clientset, namespace string, handler PodHandler) (*PodController, error) {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	informer := newPodInformer(clientset, namespace)
	processor := newPodWorker(logger, queue, informer.GetIndexer(), handler)

	controller := &PodController{
		logger:   logger.With("component", "controller"),
		queue:    queue,
		informer: informer,
		worker:   processor,
	}
	if err := controller.addEventHandlers(); err != nil {
		return nil, fmt.Errorf("add event handlers: %w", err)
	}
	return controller, nil
}

func newPodInformer(clientset *kubernetes.Clientset, namespace string) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return clientset.CoreV1().Pods(namespace).Watch(context.Background(), metav1.ListOptions{})
			},
		},
		&v1.Pod{},
		reSyncInformer,
		cache.Indexers{},
	)
}

func (c *PodController) addEventHandlers() error {
	_, err := c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				c.logger.Error(fmt.Sprintf("handle add event: meta namespace key func: %v", err))
				return
			}

			if !c.addAddEvent(key, obj) {
				return
			}

			c.logger.Debug(fmt.Sprintf("add event %s added to queue", key))
			c.queue.Add(key)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err != nil {
				c.logger.Error(fmt.Sprintf("handle update event: meta namespace key func: %v", err))
				return
			}

			if !c.addUpdateEvent(key, oldObj, newObj) {
				return
			}

			c.logger.Debug(fmt.Sprintf("update event %s added to queue", key))
			c.queue.Add(key)
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				c.logger.Error(fmt.Sprintf("handle delete event: meta namespace key func: %v", err))
				return
			}

			if !c.addDeleteEvent(key, obj) {
				return
			}

			c.logger.Debug(fmt.Sprintf("delete event %s added to queue", key))
			c.queue.Add(key)
		},
	})
	return err
}

func (c *PodController) Run(stopCh <-chan struct{}) {
	c.logger.Info("starting controller")
	go func() {
		c.informer.Run(stopCh)
		c.logger.Info("informer stopped")
		c.queue.ShutDown()
		c.logger.Info("queue shut down")
	}()

	c.logger.Info("waiting for cache sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		c.logger.Error("failed to sync")
		return
	}
	c.logger.Info("cache synced")
	c.logger.Info("starting controller worker")
	c.worker.run()
	c.logger.Info("controller worker stopped")
}

func (c *PodController) addAddEvent(key string, obj interface{}) bool {
	pod := c.toPod(key, obj)

	// pod has annotation
	if v, ok := pod.Annotations[pkg.PodEIPAnnotationKey]; ok && v == pkg.PodEIPAnnotationValue {
		// and has IP assigned
		ip := pod.Status.PodIP
		if ip != "" {
			c.logger.Info(fmt.Sprintf("add add event %s ip is set %s", key, ip))
			return true
		}
	}
	c.logger.Debug(fmt.Sprintf("skipping add add event %s", key))
	return false
}

func (c *PodController) addUpdateEvent(key string, oldObj, newObj interface{}) bool {
	oldPod := c.toPod(key, oldObj)
	newPod := c.toPod(key, newObj)

	// annotation changed
	oldEIPAnnotation := oldPod.Annotations[pkg.PodEIPAnnotationKey]
	newEIPAnnotation := newPod.Annotations[pkg.PodEIPAnnotationKey]
	if oldEIPAnnotation != newEIPAnnotation {
		c.logger.Info(fmt.Sprintf("add update event %s annotation %s changed from %s to %s", key, pkg.PodEIPAnnotationKey, oldEIPAnnotation, newEIPAnnotation))
		return true
	}
	// new pod has annotation
	if v, ok := newPod.Annotations[pkg.PodEIPAnnotationKey]; ok && v == pkg.PodEIPAnnotationValue {
		// and IP changed
		oldIP := oldPod.Status.PodIP
		newIP := newPod.Status.PodIP
		if oldIP != newIP {
			c.logger.Info(fmt.Sprintf("add update event %s ip changed from %s to %s", key, oldIP, newIP))
			return true
		}
		// label changed
		oldIPLabel := oldPod.Labels[pkg.PodPublicIPLabel]
		newIPLabel := newPod.Labels[pkg.PodPublicIPLabel]
		if oldIPLabel != newIPLabel {
			c.logger.Info(fmt.Sprintf("add update event %s ip label %s changed from %s to %s", key, pkg.PodPublicIPLabel, oldIPLabel, newIPLabel))
			return true
		}
	}
	c.logger.Debug(fmt.Sprintf("skipping add update event %s", key))
	return false
}

func (c *PodController) addDeleteEvent(key string, obj interface{}) bool {
	if v, ok := c.toPod(key, obj).Annotations[pkg.PodEIPAnnotationKey]; ok && v == pkg.PodEIPAnnotationValue {
		c.logger.Info(fmt.Sprintf("add delete event %s annotation %s=%s is present", key, pkg.PodEIPAnnotationKey, pkg.PodEIPAnnotationValue))
		return true
	}
	c.logger.Debug(fmt.Sprintf("delete event %s pod does not have %s annotation, skipping", key, pkg.PodEIPAnnotationKey))
	return false
}

func (c *PodController) toPod(key string, obj interface{}) v1.Pod {
	if obj == nil {
		c.logger.Error(fmt.Sprintf("%s cannot convert nil to pod", key))
		return v1.Pod{}
	}
	return *obj.(*v1.Pod)
}
