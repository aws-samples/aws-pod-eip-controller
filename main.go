// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package main

import (
	"fmt"
	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/aws"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/handler"
	"github.com/aws-samples/aws-pod-eip-controller/pkg/k8s"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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

	if err := run(logger, clientset, ec2Client); err != nil {
		logger.Error(fmt.Sprintf("controller run: %v", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger, clientset *kubernetes.Clientset, eniClient handler.ENIClient) error {
	podHandler := handler.NewHandler(logger, clientset.CoreV1(), eniClient)
	podController, err := k8s.NewPodController(logger, clientset, "", podHandler)
	if err != nil {
		return fmt.Errorf("new pod informer: %v", err)
	}

	podController.Run(getStopCh(logger))
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
