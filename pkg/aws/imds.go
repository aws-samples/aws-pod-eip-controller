// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package aws

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"io"
	"time"
)

type IMDS struct {
	client *imds.Client
}

func NewIMDS() (IMDS, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return IMDS{}, err
	}
	return IMDS{client: imds.NewFromConfig(cfg)}, nil
}

func (i IMDS) GetRegion() (string, error) {
	region, err := i.getMetadata("placement/region")
	if err != nil {
		return "", fmt.Errorf("get region: %w", err)
	}
	return region, nil
}

func (i IMDS) GetVpcID() (string, error) {
	mac, err := i.getMetadata("mac")
	if err != nil {
		return "", fmt.Errorf("get vpc id: %w", err)
	}
	vpcId, err := i.getMetadata(fmt.Sprintf("network/interfaces/macs/%s/vpc-id", mac))
	if err != nil {
		return "", fmt.Errorf("get vpc id: %w", err)
	}
	return vpcId, nil
}

func (i IMDS) getMetadata(path string) (string, error) {
	data, err := i.client.GetMetadata(context.TODO(), &imds.GetMetadataInput{Path: path})
	if err != nil {
		return "", fmt.Errorf("get %s metadata: %w", path, err)
	}
	out, err := io.ReadAll(data.Content)
	if err != nil {
		return "", fmt.Errorf("read %s metadata: %w", path, err)
	}
	return string(out), nil
}
