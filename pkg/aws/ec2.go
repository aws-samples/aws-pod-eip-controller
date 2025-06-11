// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package aws

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
)

var keyLocks *KeyLock

func init() {
	keyLocks = NewKeyLock()
}

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

type AssociateAddressOptions struct {
	PodKey        string
	PodIP         string
	HostIP        string
	AddressPoolId string
	PECType       string
	TagKey        string
	TagValueKey   string
}

func (c EC2Client) AssociateAddress(options AssociateAddressOptions) (string, error) {
	ni, err := c.getNetworkInterface(options.PodIP, options.HostIP)
	if err != nil {
		return "", err
	}
	var allocationID, publicIP string
	switch options.PECType {
	case pkg.PodEIPAnnotationValueAuto:
		allocationID, publicIP, err = c.allocateAddress(options.PodKey, options.AddressPoolId)
		if err != nil {
			return "", err
		}
	case pkg.PodEIPAnnotationValueFixedTag:
		keyLocks.Lock(options.TagKey)
		defer keyLocks.Unlock(options.TagKey)
		allocationID, publicIP, err = c.getTagAddress(options.TagKey)
		if err != nil {
			return "", err
		}
		if err := c.createTag(allocationID, map[string]string{
			pkg.TagPodKey:         options.PodKey,
			pkg.TagClusterNameKey: c.clusterName,
			pkg.TagTypeKey:        pkg.PodEIPAnnotationValueFixedTag,
		}); err != nil {
			return "", err
		}
	case pkg.PodEIPAnnotationValueFixedTagValue:
		allocationID, publicIP, err = c.getTagValueAddress(options.TagValueKey, options.PodKey)
		if err != nil {
			return "", err
		}
		if err := c.createTag(allocationID, map[string]string{
			pkg.TagPodKey:         options.PodKey,
			pkg.TagClusterNameKey: c.clusterName,
			pkg.TagTypeKey:        pkg.PodEIPAnnotationValueFixedTagValue,
		}); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported PEC type %s", options.PECType)
	}
	if err := c.associateAddress(allocationID, ni.id, options.PodIP); err != nil {
		return "", err
	}
	return publicIP, nil
}

type DisassociateAddressOptions struct {
	PodKey string
}

func (c EC2Client) DisassociateAddress(options DisassociateAddressOptions) error {
	addrs, err := c.describePodAddresses(options.PodKey)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		c.logger.Info(fmt.Sprintf("no address found for %s pod", options.PodKey))
		return nil
	}
	if err := c.disassociateAddress(addrs[0].associationID); err != nil {
		c.logger.Error(fmt.Sprint())
		return err
	}
	tagType, ok := addrs[0].tags[pkg.TagTypeKey]
	if !ok {
		return nil
	}
	switch tagType {
	case pkg.PodEIPAnnotationValueAuto: // auto mode release address
		return c.releaseAddress(addrs[0].allocationID)
	case pkg.PodEIPAnnotationValueFixedTag: // fixed-tag mode delete eip tag
		if err := c.deleteTag(addrs[0].allocationID, []string{pkg.TagPodKey, pkg.TagTypeKey, pkg.TagClusterNameKey}); err != nil {
			return err
		}
	case pkg.PodEIPAnnotationValueFixedTagValue: // fixed-tag-value mode delete eip tag
		if err := c.deleteTag(addrs[0].allocationID, []string{pkg.TagPodKey, pkg.TagTypeKey, pkg.TagClusterNameKey}); err != nil {
			return err
		}
	}
	return nil
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

func (c EC2Client) getNetworkInterface(privateIP string, hostIP string) (networkInterface, error) {
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
		return networkInterface{}, fmt.Errorf("describe-network-interfaces private-ip-address %s vpc-id %s error: %w", privateIP, c.vpcID, err)
	}
	if len(result.NetworkInterfaces) > 0 {
		return toNetworkInterface(result.NetworkInterfaces[0]), nil
	}

	// ip prefix mode
	// aws ec2 describe-network-interfaces --filters Name=vpc-id,Values=vpc-06918bf4ad51c9d09 Name=addresses.private-ip-address,Values=192.168.5.21 --region us-east-1
	result, err = c.client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{c.vpcID},
			},
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: []string{hostIP},
			},
		},
	})
	if err != nil {
		return networkInterface{}, fmt.Errorf("describe-network-interfaces host-ip %s vpc-id %s error: %w", hostIP, c.vpcID, err)
	}
	if len(result.NetworkInterfaces) == 0 {
		return networkInterface{}, fmt.Errorf("no network interface found for %s private IP host IP %s in %s vpc on ipv4prefixes", privateIP, hostIP, c.vpcID)
	}

	attachment := result.NetworkInterfaces[0].Attachment
	if attachment == nil || attachment.InstanceId == nil {
		return networkInterface{}, fmt.Errorf("network interface for host IP %s in vpc %s does not have an attached instance", hostIP, c.vpcID)
	}
	instanceId := aws.ToString(attachment.InstanceId)

	// aws ec2 describe-network-interfaces --filters Name=vpc-id,Values=vpc-06918bf4ad51c9d09 Name=attachment.instance-id,Values=i-0d828397cc4f56df5 --region us-east-1
	result, err = c.client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{c.vpcID},
			},
			{
				Name:   aws.String("attachment.instance-id"),
				Values: []string{instanceId},
			},
		},
	})
	if err != nil {
		return networkInterface{}, fmt.Errorf("describe-network-interfaces instance-id %s vpc-id %s error: %w", instanceId, c.vpcID, err)
	}
	if len(result.NetworkInterfaces) == 0 {
		return networkInterface{}, fmt.Errorf("no network interface found for instance id %s in %s vpc on ipv4prefixes", instanceId, c.vpcID)
	}
	ip := net.ParseIP(privateIP)
	for _, ni := range result.NetworkInterfaces {
		for _, prefix := range ni.Ipv4Prefixes {
			c.logger.Debug(fmt.Sprintf("checking %s prefix %s", aws.ToString(ni.NetworkInterfaceId), aws.ToString(prefix.Ipv4Prefix)))
			_, ipnet, _ := net.ParseCIDR(aws.ToString(prefix.Ipv4Prefix))
			if ipnet != nil && ipnet.Contains(ip) {
				return toNetworkInterface(ni), nil
			}
		}
	}
	return networkInterface{}, fmt.Errorf("no id found for %s private IP host IP %s in %s vpc on ipv4prefixes", privateIP, hostIP, c.vpcID)
}

func (c EC2Client) createTag(resource string, kv map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tags := make([]types.Tag, 0, len(kv))
	for k, v := range kv {
		tags = append(tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	if _, err := c.client.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{resource},
		Tags:      tags,
	}); err != nil {
		return fmt.Errorf("create-tags resource %s tags %v", resource, kv)
	}
	return nil
}

func (c EC2Client) deleteTag(resource string, keys []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tags := make([]types.Tag, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, types.Tag{Key: aws.String(key)})
	}
	if _, err := c.client.DeleteTags(ctx, &ec2.DeleteTagsInput{
		Resources: []string{resource},
		Tags:      tags,
	}); err != nil {
		return fmt.Errorf("delete-tags resource %s tag Keys=%v", resource, keys)
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
		Filters: []types.Filter{
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", pkg.TagPodKey)),
				Values: []string{podKey},
			},
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", pkg.TagClusterNameKey)),
				Values: []string{c.clusterName},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe address pod %s: %v", podKey, err)
	}
	var out []address
	for _, v := range result.Addresses {
		if v.AssociationId != nil {
			out = append(out, toAddress(v))
		}
	}
	return out, nil
}

func (c EC2Client) allocateAddress(podKey, addressPoolId string) (allocationID string, publicIP string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 allocate-address
	allocatedResult, err := c.client.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		PublicIpv4Pool: aws.String(addressPoolId),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeElasticIp,
				Tags: []types.Tag{
					{Key: aws.String(pkg.TagTypeKey), Value: aws.String(pkg.PodEIPAnnotationValueAuto)},
					{Key: aws.String(pkg.TagClusterNameKey), Value: aws.String(c.clusterName)},
					{Key: aws.String(pkg.TagPodKey), Value: aws.String(podKey)},
				},
			},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("allocate address: %w", err)
	}
	return *allocatedResult.AllocationId, *allocatedResult.PublicIp, nil
}

func (c EC2Client) getTagAddress(tagKey string) (allocationID string, publicIP string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 describe-addresses --filters Name=tag-key,Values=aws-pod-eip-controller --query 'Addresses[?AssociationId==null]'
	describeResult, err := c.client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{Name: aws.String("tag-key"), Values: []string{tagKey}},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("get tag address fail: %w", err)
	}
	if len(describeResult.Addresses) == 0 {
		return "", "", fmt.Errorf("no address found for tag key %s", tagKey)
	}
	for _, addr := range describeResult.Addresses {
		if addr.AssociationId == nil {
			return *addr.AllocationId, *addr.PublicIp, nil
		}
	}
	return "", "", fmt.Errorf("no address found for tag key %s and not attached", tagKey)
}

func (c EC2Client) getTagValueAddress(tagKey, value string) (allocationID string, publicIP string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 describe-addresses --filters Name=tag:%,Values=demo/demo-0
	describeResult, err := c.client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{Name: aws.String(fmt.Sprintf("tag:%s", tagKey)), Values: []string{value}},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("get tag-value address fail: %w", err)
	}
	if len(describeResult.Addresses) == 0 {
		return "", "", fmt.Errorf("no address found for tag-value key %s", tagKey)
	}
	return *describeResult.Addresses[0].AllocationId, *describeResult.Addresses[0].PublicIp, nil
}

func (c EC2Client) associateAddress(allocationId, eniID, privateIP string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 associate-address --allocation-id eipalloc-64d5890a --network-interface-id eni-1a2b3c4d --private-ip-address
	if _, err := c.client.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId:       aws.String(allocationId),
		NetworkInterfaceId: aws.String(eniID),
		PrivateIpAddress:   aws.String(privateIP),
	}); err != nil {
		return fmt.Errorf("associate address allocation-id %s network-interface-id %s private-ip-address %s",
			allocationId, eniID, privateIP)
	}
	return nil
}

func (c EC2Client) disassociateAddress(associationID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 disassociate-address --association-id eipassoc-2bebb745
	if _, err := c.client.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{
		AssociationId: aws.String(associationID),
	}); err != nil {
		return fmt.Errorf("disassociate address association-id %s", associationID)
	}
	return nil
}

func (c EC2Client) releaseAddress(allocationID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// aws ec2 release-address --allocation-id eipalloc-64d5890a
	if _, err := c.client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
		AllocationId: aws.String(allocationID),
	}); err != nil {
		return fmt.Errorf("release address allocation-id %s", allocationID)
	}
	return nil
}
