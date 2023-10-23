// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package aws

import (
	"context"
	"fmt"
	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"log/slog"
	"time"
)

type EC2Client struct {
	logger      *slog.Logger
	vpcID       string
	client      *ec2.Client
	clusterName string
}

func NewEC2Client(logger *slog.Logger, region, vpcID, clusterName string) (EC2Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return EC2Client{}, err
	}

	return EC2Client{
		logger:      logger.With("component", "ec2"),
		vpcID:       vpcID,
		client:      ec2.NewFromConfig(cfg),
		clusterName: clusterName,
	}, nil
}

func (c EC2Client) AssociateAddress(podKey, podIP string) (string, error) {
	ni, err := c.getNetworkInterface(podIP)
	if err != nil {
		return "", err
	}
	addrs, err := c.describeAddresses(podIP, ni.id)
	if err != nil {
		return "", err
	}
	if len(addrs) != 0 {
		c.logger.Info(fmt.Sprintf("pod ip %s is already associated with %s ip", podIP, addrs[0].publicIP))
		return addrs[0].publicIP, nil
	}
	return c.allocatedAndAssociateAddress(podKey, podIP, ni.id)
}

func (c EC2Client) HasAssociatedAddress(podIP string) (bool, error) {
	ni, err := c.getNetworkInterface(podIP)
	if err != nil {
		return false, err
	}
	addrs, err := c.describeAddresses(podIP, ni.id)
	if err != nil {
		return false, err
	}
	return len(addrs) != 0, nil
}

func (c EC2Client) DisassociateAddress(podKey string) error {
	addrs, err := c.describePodAddresses(podKey)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		c.logger.Info("no address found for %s pod", podKey)
		return nil
	}
	if err := c.deleteTag(addrs[0].allocationID, pkg.TagPodKey); err != nil {
		c.logger.Error(fmt.Sprintf("disassociate address %s: %v", podKey, err))
	}
	return c.disassociateAndReleaseAddress(addrs[0].associationID, addrs[0].allocationID)
}

type networkInterface struct {
	id     string
	status string
}

func toNetworkInterface(ni types.NetworkInterface) networkInterface {
	return networkInterface{
		id:     aws.ToString(ni.NetworkInterfaceId),
		status: string(ni.Status),
	}
}

func (c EC2Client) getNetworkInterface(privateIP string) (networkInterface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 describe-network-interfaces --filters Name=addresses.private-ip-address,Values=10.2.21.154 Name=vpc-id,Values=vpc-0d46053e21e3a2cf9
	result, err := c.client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: []string{privateIP},
			},
			{
				Name:   aws.String("vpc-id"),
				Values: []string{c.vpcID},
			},
		},
	})
	if err != nil {
		return networkInterface{}, fmt.Errorf("describe-network-interfaces private-ip-address %s vpc-id %s", privateIP, c.vpcID)
	}
	if len(result.NetworkInterfaces) == 0 {
		return networkInterface{}, fmt.Errorf("no id found for %s private IP in %s vpc", privateIP, c.vpcID)
	}
	return toNetworkInterface(result.NetworkInterfaces[0]), nil
}

func (c EC2Client) deleteTag(resource, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.client.DeleteTags(ctx, &ec2.DeleteTagsInput{
		Resources: []string{resource},
		Tags:      []types.Tag{{Key: aws.String(key)}},
	}); err != nil {
		return fmt.Errorf("delete-tags resource %s tag Key=%s", resource, key)
	}
	return nil
}

type address struct {
	associationID string
	allocationID  string
	privateIP     string
	publicIP      string
	tags          map[string]string
}

func toAddress(addr types.Address) address {
	tags := make(map[string]string)
	for _, t := range addr.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return address{
		associationID: aws.ToString(addr.AssociationId),
		allocationID:  aws.ToString(addr.AllocationId),
		privateIP:     aws.ToString(addr.PrivateIpAddress),
		publicIP:      aws.ToString(addr.PublicIp),
		tags:          tags,
	}
}

func (c EC2Client) describeAddresses(privateIP string, eniID string) ([]address, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 describe-addresses --filters Name=private-ip-address,Values=10.2.21.154 Name=network-interface-id,Values=id-1a2b3c4d
	result, err := c.client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("private-ip-address"),
				Values: []string{privateIP},
			},
			{
				Name:   aws.String("network-interface-id"),
				Values: []string{eniID},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe address private-ip-address %s network-interface-id %s", privateIP, eniID)
	}
	var out []address
	for _, v := range result.Addresses {
		out = append(out, toAddress(v))
	}
	return out, nil
}

func (c EC2Client) describePodAddresses(podKey string) ([]address, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: c.getAWSTagsFilter(podKey),
	})
	if err != nil {
		return nil, fmt.Errorf("describe address pod %s", podKey)
	}
	var out []address
	for _, v := range result.Addresses {
		out = append(out, toAddress(v))
	}
	return out, nil
}

func (c EC2Client) allocatedAndAssociateAddress(podKey, privateIP, eniID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 allocate-address
	allocatedResult, err := c.client.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeElasticIp,
				Tags:         c.getAWSTags(podKey),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("allocate address: %w", err)
	}

	//aws ec2 associate-address --allocation-id eipalloc-64d5890a --network-interface-id eni-1a2b3c4d --private-ip-address 10.0.0.85
	if _, err := c.client.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId:       allocatedResult.AllocationId,
		NetworkInterfaceId: aws.String(eniID),
		PrivateIpAddress:   aws.String(privateIP),
	}); err != nil {
		return "", fmt.Errorf("associate address allocation-id %s network-interface-id %s private-ip-address %s",
			aws.ToString(allocatedResult.AllocationId), eniID, privateIP)
	}
	return aws.ToString(allocatedResult.PublicIp), nil
}

func (c EC2Client) disassociateAndReleaseAddress(associationID, allocationID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	//aws ec2 disassociate-address --association-id eipassoc-2bebb745
	if _, err := c.client.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{
		AssociationId: aws.String(associationID),
	}); err != nil {
		return fmt.Errorf("disassociate address association-id %s", associationID)
	}

	//aws ec2 release-address --allocation-id eipalloc-64d5890a
	if _, err := c.client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
		AllocationId: aws.String(allocationID),
	}); err != nil {
		return fmt.Errorf("release address allocation-id %s", allocationID)
	}
	return nil
}

func (c EC2Client) getAWSTags(podKey string) []types.Tag {
	var out []types.Tag
	for k, v := range c.getTags(podKey) {
		out = append(out, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return out
}

func (c EC2Client) getAWSTagsFilter(podKey string) []types.Filter {
	var out []types.Filter
	for k, v := range c.getTags(podKey) {
		out = append(out, types.Filter{Name: aws.String(fmt.Sprintf("tag:%s", k)), Values: []string{v}})
	}
	return out
}

func (c EC2Client) getTags(podKey string) map[string]string {
	return map[string]string{
		pkg.TagTypeKey:        "auto",
		pkg.TagClusterNameKey: c.clusterName,
		pkg.TagPodKey:         podKey,
	}
}
