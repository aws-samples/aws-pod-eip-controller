package informer

import (
	"log"
	"sync"
	"time"

	"github.com/aws-samples/aws-pod-eip-controller/pkg/handler"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

type PECInformer struct {
	mux           *sync.RWMutex
	synced        bool
	handler       *handler.Handler
	clusterClient *dynamic.DynamicClient
	informer      cache.SharedIndexInformer
}

func NewPECInformer(clusterClient *dynamic.DynamicClient, resyncPeriod int, watchNamespace string, handler *handler.Handler) *PECInformer {
	pecInformer := &PECInformer{
		mux:           &sync.RWMutex{},
		synced:        false,
		handler:       handler,
		clusterClient: clusterClient,
	}
	resource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clusterClient, time.Duration(resyncPeriod)*time.Second, watchNamespace, nil)
	pecInformer.informer = factory.ForResource(resource).Informer()
	return pecInformer
}

func (p *PECInformer) AddEventHandler() error {
	_, err := p.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logrus.WithFields(logrus.Fields{
				"action": "add-pod",
				"obj":    obj,
			}).Debug()
			// Handler logic:
			err := p.handler.HandleEvent(obj.(*unstructured.Unstructured), nil, "add")
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"action": "add-pod",
					"obj":    obj,
				}).Warn(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.mux.RLock()
			defer p.mux.RUnlock()
			if !p.synced {
				return
			}
			logrus.WithFields(logrus.Fields{
				"action":  "update-pod",
				"old-obj": oldObj,
				"new-obj": newObj,
			}).Debug()
			// Handler logic
			err := p.handler.HandleEvent(newObj.(*unstructured.Unstructured), oldObj.(*unstructured.Unstructured), "update")
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"action": "update-pod",
					"obj":    newObj,
				}).Warn(err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			p.mux.RLock()
			defer p.mux.RUnlock()
			if !p.synced {
				return
			}
			logrus.WithFields(logrus.Fields{
				"action": "delete-pod",
				"obj":    obj,
			}).Debug()
			// Handler logic
			err := p.handler.HandleEvent(obj.(*unstructured.Unstructured), nil, "delete")
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"action": "delete-pod",
					"obj":    obj,
				}).Warn(err)
			}
		},
	})
	return err
}

func (p *PECInformer) Run(stopCh <-chan struct{}) {
	if err := p.AddEventHandler(); err != nil {
		log.Fatal(err)
	}
	go p.informer.Run(stopCh)
	isSynced := cache.WaitForCacheSync(stopCh, p.informer.HasSynced)
	p.mux.Lock()
	p.synced = isSynced
	p.mux.Unlock()
	if !isSynced {
		log.Fatal("failed to sync")
	}
}
