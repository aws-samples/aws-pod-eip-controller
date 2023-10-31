// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package pkg

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Flags struct {
	LogLevel       string
	Kubeconfig     string
	ClusterName    string
	VpcID          string
	Region         string
	WatchNamespace string
	ResyncPeriod   int
}

func (f Flags) SlogLevel() slog.Level {
	switch strings.ToUpper(f.LogLevel) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	}
	return slog.LevelInfo
}

func ParseFlags() Flags {
	var flags Flags
	f := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	f.StringVar(&flags.LogLevel, "log-level", getStringEnv("PEC_LOG_LEVEL", "DEBUG"), "controller log level")
	f.StringVar(&flags.Kubeconfig, "kubeconfig", getStringEnv("PEC_KUBECONFIG", ""), "kubeconfig path, set only if controller should NOT be in the cluster")
	f.StringVar(&flags.ClusterName, "cluster-name", getStringEnv("PEC_CLUSTER_NAME", ""), "cluster name")
	f.StringVar(&flags.VpcID, "vpc-id", getStringEnv("PEC_VPC_ID", ""), "AWS vpc id")
	f.StringVar(&flags.Region, "region", getStringEnv("PEC_REGION", ""), "AWS region")
	f.StringVar(&flags.WatchNamespace, "watch-namespace", getStringEnv("PEC_WATCH_NAMESPACE", ""), "namespace to watch, empty will watch all namespaces")
	f.IntVar(&flags.ResyncPeriod, "resync-period", getIntEnv("PEC_RESYNC_PERIOD", 0), "resync period in seconds, 0 means no resync")

	if err := f.Parse(os.Args[1:]); err != nil {
		fmt.Printf("parse flags: %v", err)
		os.Exit(1)
	}
	if _, ok := map[string]struct{}{"DEBUG": {}, "INFO": {}, "WARN": {}, "ERROR": {}}[strings.ToUpper(flags.LogLevel)]; !ok {
		fmt.Printf("invalid log level %s", flags.LogLevel)
		os.Exit(1)
	}
	if flags.ClusterName == "" {
		fmt.Println("cluster name is not set")
		os.Exit(1)
	}
	return flags
}

func getStringEnv(envName string, defaultValue string) string {
	if env, ok := os.LookupEnv(envName); ok {
		return env
	}
	return defaultValue
}

func getIntEnv(envName string, defaultValue int) int {
	if env, ok := os.LookupEnv(envName); ok {
		if IntVar, err := strconv.Atoi(env); err == nil {
			return IntVar
		}
	}
	return defaultValue
}
