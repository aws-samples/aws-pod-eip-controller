// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/aws-samples/aws-pod-eip-controller/pkg/handler"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	// logrus setting
	logLevel := os.Getenv("LOG_LEVEL")
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.InfoLevel
	}
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(level)
	log.SetOutput(os.Stdout)

	// init handler
	channelSize := os.Getenv("CHANNEL_SIZE")
	vpcID := os.Getenv("VPC_ID")
	region := os.Getenv("REGION")
	if channelSize == "" {
		channelSize = "20"
	}
	size, err := strconv.Atoi(channelSize)
	if err != nil {
		log.Fatalln(err)
	}
	handler, err := handler.NewHandler(int32(size), vpcID, region)
	if err != nil {
		log.Fatalln(err)
	}

	// kube config
	kubeConfig := os.Getenv("KUBECONFIG")
	var clusterConfig *rest.Config
	if kubeConfig != "" {
		clusterConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
	} else {
		clusterConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalln(err)
	}
	clusterClient, err := dynamic.NewForConfig(clusterConfig)
	if err != nil {
		log.Fatalln(err)
	}
	resource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	NameSpace := os.Getenv("NAMESPACE")
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clusterClient, 60*time.Second, NameSpace, nil)
	informer := factory.ForResource(resource).Informer()

	mux := &sync.RWMutex{}
	synced := false

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logrus.WithFields(logrus.Fields{
				"action": "add-pod",
				"obj":    obj,
			}).Debug()
			// Handler logic: add event dismiss
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			mux.RLock()
			defer mux.RUnlock()
			if !synced {
				return
			}
			logrus.WithFields(logrus.Fields{
				"action":  "update-pod",
				"old-obj": oldObj,
				"new-obj": newObj,
			}).Debug()
			// Handler logic
			err := handler.HandleEvent(newObj.(*unstructured.Unstructured), oldObj.(*unstructured.Unstructured), "update")
			if err != nil {
				log.WithFields(logrus.Fields{
					"action": "delete-pod",
					"obj":    newObj,
				}).Warn(err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			mux.RLock()
			defer mux.RUnlock()
			if !synced {
				return
			}
			logrus.WithFields(logrus.Fields{
				"action": "delete-pod",
				"obj":    obj,
			}).Debug()
			// Handler logic
			handler.HandleEvent(obj.(*unstructured.Unstructured), nil, "delete")
			if err != nil {
				log.WithFields(logrus.Fields{
					"action": "delete-pod",
					"obj":    obj,
				}).Warn(err)
			}
		},
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go informer.Run(ctx.Done())

	isSynced := cache.WaitForCacheSync(ctx.Done(), informer.HasSynced)
	mux.Lock()
	synced = isSynced
	mux.Unlock()

	if !isSynced {
		log.Fatal("failed to sync")
	}

	<-ctx.Done()
}
