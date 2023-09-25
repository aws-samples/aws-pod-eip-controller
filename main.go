// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/aws-samples/aws-pod-eip-controller/pkg/handler"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/informer"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/recycle"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/service"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type Config struct {
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
	ClusterName    string `yaml:"clusterName"`
	ChannelSize    int    `yaml:"channelSize"`
	VpcID          string `yaml:"vpcID"`
	Region         string `yaml:"region"`
	KubeConfigPath string `yaml:"kubeConfigPath"`
	WatchNamespace string `yaml:"watchNamespace"`
	ResyncPeriod   int    `yaml:"resyncPeriod"`
	RecycleOption  struct {
		Enable bool `yaml:"enable"`
		Period int  `yaml:"period"`
	} `yaml:"recycleOption"`
}

func main() {
	// config parse
	configFileName := flag.String("f", "config.yaml", "config file name")
	flag.Parse()
	config := Config{}
	configYaml, err := os.ReadFile(*configFileName)
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(configYaml, &config)
	if err != nil {
		panic(err)
	}
	if config.ChannelSize <= 0 || config.ChannelSize > 100 {
		config.ChannelSize = 20
	}
	vpcID, region, err := service.GetVpcIDAndRegion(config.VpcID, config.Region)
	if err != nil {
		logrus.Fatalln("get vpc-id and region fail", err)
	}
	// logrus setting
	level, err := logrus.ParseLevel(config.Log.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	if config.Log.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{})
	}
	logrus.SetOutput(os.Stdout)
	logrus.Debugf("%+v", config)

	// create cluster client
	var clusterConfig *rest.Config
	if config.KubeConfigPath != "" {
		clusterConfig, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigPath)
	} else {
		clusterConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		logrus.Fatalln(err)
	}
	clusterClient, err := dynamic.NewForConfig(clusterConfig)
	if err != nil {
		logrus.Fatalln(err)
	}

	// acquired lease lock
	client := kubernetes.NewForConfigOrDie(clusterConfig)
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      "aws-pod-eip-controller-lock",
			Namespace: "kube-system",
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: uuid.New().String(),
		},
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   30 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logrus.Infof("start leading")
				// init handler
				handler, err := handler.NewHandler(int32(config.ChannelSize), vpcID, region, config.ClusterName)
				if err != nil {
					logrus.Fatalln(err)
				}

				// create informer
				pecInformer := informer.NewPECInformer(clusterClient, config.ResyncPeriod, config.WatchNamespace, handler)
				go pecInformer.Run(ctx.Done())

				// recycle elatic ip
				if config.RecycleOption.Enable {
					recycle, err := recycle.NewRecycle(clusterClient, config.ClusterName, config.RecycleOption.Period, vpcID, region)
					if err != nil {
						logrus.Fatalln(err)
					}
					go recycle.Run()
				}
			},
			OnStoppedLeading: func() {
				logrus.Infof("stop leading")
				os.Exit(0)
			},
			OnNewLeader: func(identity string) {
				logrus.Infof("new leader: %s", identity)
			},
		},
	})

}
