// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package main

import (
	"context"
	"fmt"
	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/aws"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/handler"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/k8s"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	flags := pkg.ParseFlags()
	logger := pkg.NewLogger(flags.SlogLevel())
	logger.Info(fmt.Sprintf("starting controller with flags: %+v", flags))

	flags, err := setVpcIdAndRegion(logger, flags)
	if err != nil {
		logger.Error(fmt.Sprintf("set vpc id and region: %v", err))
		os.Exit(1)
	}

	restConfig, err := getRestConfig(logger, flags.Kubeconfig)
	if err != nil {
		logger.Error(fmt.Sprintf("get rest config: %v", err))
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logger.Error(fmt.Sprintf("new clientset: %v", err))
		os.Exit(1)
	}

	ec2Client, err := aws.NewEC2Client(logger, flags.Region, flags.VpcID, flags.ClusterName)
	if err != nil {
		logger.Error(fmt.Sprintf("new ec2 client: %v", err))
		os.Exit(1)
	}

	//if err := run(logger, clientset, ec2Client, k8s.PodControllerConfig{
	//	Namespace:    flags.WatchNamespace,
	//	ResyncPeriod: time.Duration(flags.ResyncPeriod) * time.Second,
	//}); err != nil {
	//	logger.Error(fmt.Sprintf("controller run: %v", err))
	//	os.Exit(1)
	//}

	// One stopCh for all controllers
	stopCh := getStopCh(logger)
	// watchNamespace supports: "ns1,ns2,ns3". If empty -> default.
	namespaces := splitNamespaces(flags.WatchNamespace)

	g, _ := errgroup.WithContext(context.Background())

	for _, ns := range namespaces {
		ns := ns // capture loop var
		g.Go(func() error {
			nsLogger := logger.With("namespace", ns)
			return runWithStopCh(nsLogger, clientset, ec2Client, k8s.PodControllerConfig{
				Namespace:    ns,
				ResyncPeriod: time.Duration(flags.ResyncPeriod) * time.Second,
			}, stopCh)
		})
	}

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("controller run: %v", err))
		os.Exit(1)
	}

}

//	func run(logger *slog.Logger, clientset *kubernetes.Clientset, eniClient handler.ENIClient, config k8s.PodControllerConfig) error {
//		// Create event broadcaster and recorder
//		eventBroadcaster := record.NewBroadcaster()
//		eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
//		eventRecorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "aws-pod-eip-controller"})
//		defer eventBroadcaster.Shutdown()
//
//		podHandler := handler.NewHandler(logger, clientset.CoreV1(), eniClient, eventRecorder)
//		podController, err := k8s.NewPodController(logger, clientset, podHandler, config)
//		if err != nil {
//			return fmt.Errorf("new pod informer: %v", err)
//		}
//
//		podController.Run(getStopCh(logger))
//		logger.Info("controller stopped")
//		return nil
//	}
func runWithStopCh(logger *slog.Logger, clientset *kubernetes.Clientset, eniClient handler.ENIClient, config k8s.PodControllerConfig, stopCh <-chan struct{}) error {
	// Create event broadcaster and recorder
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})
	eventRecorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		v1.EventSource{Component: "aws-pod-eip-controller"},
	)
	defer eventBroadcaster.Shutdown()

	podHandler := handler.NewHandler(logger, clientset.CoreV1(), eniClient, eventRecorder)

	podController, err := k8s.NewPodController(logger, clientset, podHandler, config)
	if err != nil {
		return fmt.Errorf("new pod informer: %v", err)
	}

	podController.Run(stopCh)
	logger.Info("controller stopped")
	return nil
}

func getStopCh(logger *slog.Logger) <-chan struct{} {
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		logger.Debug("listening for SIGINT and SIGTERM")
		s := <-sigCh
		logger.Info(fmt.Sprintf("received %s signal, stopping", s))
		close(stopCh)
	}()
	return stopCh
}

// setVpcIdAndRegion checks if the vpc id and region are set in flags, if not, it will retrieve it from imds service
func setVpcIdAndRegion(logger *slog.Logger, flags pkg.Flags) (pkg.Flags, error) {
	if flags.VpcID == "" || flags.Region == "" {
		logger.Info("vpc id and/or region is not set, starting new imds service")
		imds, err := aws.NewIMDS()
		if err != nil {
			return flags, err
		}
		if flags.VpcID == "" {
			logger.Info("vpc id is not set, loading from imds service")
			vpcId, err := imds.GetVpcID()
			if err != nil {
				return flags, err
			}
			logger.Info(fmt.Sprintf("vpc id set to %s", vpcId))
			flags.VpcID = vpcId
		}
		if flags.Region == "" {
			logger.Info("region is not set, loading from imds service")
			region, err := imds.GetRegion()
			if err != nil {
				return flags, err
			}
			logger.Info(fmt.Sprintf("region set to %s", region))
			flags.Region = region
		}
	}
	return flags, nil
}

func getRestConfig(logger *slog.Logger, kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		logger.Info("kubeconfig is not set, creating in cluster config")
		return rest.InClusterConfig()
	}
	logger.Info("kubeconfig is set, creating default config")
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func splitNamespaces(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{"default"}
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}

	if len(out) == 0 {
		return []string{"default"}
	}
	return out
}
