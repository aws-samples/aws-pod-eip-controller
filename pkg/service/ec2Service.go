// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package service

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/sirupsen/logrus"
)

type EC2Service struct {
	VPCID       string
	Region      string
	ClusterName string
	EC2Client   *ec2.Client
}

func (s *EC2Service) DescribeNetworkInterfaces(ip string) (eni string, err error) {
	// aws ec2 describe-network-interfaces --filters Name=addresses.private-ip-address,Values=10.2.21.154 Name=vpc-id,Values=vpc-0d46053e21e3a2cf9
	result, err := s.EC2Client.DescribeNetworkInterfaces(context.TODO(), &ec2.DescribeNetworkInterfacesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("addresses.private-ip-address"),
				Values: []string{ip},
			},
			{
				Name:   aws.String("vpc-id"),
				Values: []string{s.VPCID},
			},
		},
	})
	if err != nil {
		return
	}
	if len(result.NetworkInterfaces) == 0 {
		return "", errors.New("no eni found")
	}
	eni = *(result.NetworkInterfaces[0].NetworkInterfaceId)
	logrus.Infof("describe eni: %s", eni)
	return
}

func (s *EC2Service) DescribeAddresses(ip string, eni string) (associationID string, allocationID string, isAllocated bool, err error) {
	// aws ec2 describe-addresses --filters Name=private-ip-address,Values=10.2.21.154 Name=network-interface-id,Values=eni-1a2b3c4d
	result, err := s.EC2Client.DescribeAddresses(context.TODO(), &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("private-ip-address"),
				Values: []string{ip},
			},
			{
				Name:   aws.String("network-interface-id"),
				Values: []string{eni},
			},
		},
	})
	if err != nil {
		return
	}
	if len(result.Addresses) == 0 {
		isAllocated = false
		return
	}
	isAllocated = true
	associationID = *(result.Addresses[0].AssociationId)
	allocationID = *(result.Addresses[0].AllocationId)
	logrus.Infof("describe address associationID: %s", associationID)
	return
}

type Address struct {
	AllocationID     string
	AssociationID    string
	PrivateIpAddress string
}

func (s *EC2Service) DescribeUsedAddresses() (addresses []Address, err error) {
	result, err := s.EC2Client.DescribeAddresses(context.TODO(), &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:service.beta.kubernetes.io/aws-pod-eip-controller-type"),
				Values: []string{"auto"},
			},
			{
				Name:   aws.String("tag:service.beta.kubernetes.io/aws-pod-eip-controller-cluster-name"),
				Values: []string{s.ClusterName},
			},
		},
	})
	if err != nil {
		return
	}
	addresses = make([]Address, 0, 10)
	for _, address := range result.Addresses {
		addresses = append(addresses, Address{
			AllocationID:     *(address.AllocationId),
			AssociationID:    *(address.AssociationId),
			PrivateIpAddress: *(address.PrivateIpAddress),
		})
	}
	logrus.Infof("used address length: %d", len(addresses))
	return addresses, nil
}

func (s *EC2Service) AllocateAddress() (allocationID string, err error) {
	// aws ec2 allocate-address
	result, err := s.EC2Client.AllocateAddress(context.TODO(), &ec2.AllocateAddressInput{
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeElasticIp,
				Tags: []types.Tag{
					{
						Key:   aws.String("service.beta.kubernetes.io/aws-pod-eip-controller-type"),
						Value: aws.String("auto"),
					},
					{
						Key:   aws.String("service.beta.kubernetes.io/aws-pod-eip-controller-cluster-name"),
						Value: aws.String(s.ClusterName),
					},
				},
			},
		},
	})
	if err != nil {
		return
	}
	allocationID = *(result.AllocationId)
	logrus.Infof("allocate address allocationID: %s", allocationID)
	return
}

func (s *EC2Service) AssociateAddress(ip string, eni string, allocationID string) (err error) {
	//aws ec2 associate-address --allocation-id eipalloc-64d5890a --network-interface-id eni-1a2b3c4d --private-ip-address 10.0.0.85
	result, err := s.EC2Client.AssociateAddress(context.TODO(), &ec2.AssociateAddressInput{
		AllocationId:       aws.String(allocationID),
		NetworkInterfaceId: aws.String(eni),
		PrivateIpAddress:   aws.String(ip),
	})
	if err != nil {
		return
	}
	logrus.Infof("associate address result: %s", *result.AssociationId)
	return
}

func (s *EC2Service) DisassociateAddress(associationID string) (err error) {
	//aws ec2 disassociate-address --association-id eipassoc-2bebb745
	_, err = s.EC2Client.DisassociateAddress(context.TODO(), &ec2.DisassociateAddressInput{
		AssociationId: aws.String(associationID),
	})
	if err != nil {
		return
	}
	logrus.Infof("disassociate address result: %s", associationID)
	return
}

func (s *EC2Service) ReleaseAddress(allocationID string) (err error) {
	//aws ec2 release-address --allocation-id eipalloc-64d5890a
	_, err = s.EC2Client.ReleaseAddress(context.TODO(), &ec2.ReleaseAddressInput{
		AllocationId: aws.String(allocationID),
	})
	if err != nil {
		return
	}
	logrus.Infof("release address result: %s", allocationID)
	return
}

func NewEC2Service(vpcid string, region string, clusterName string) (service *EC2Service, err error) {
	service = &EC2Service{
		VPCID:       vpcid,
		Region:      region,
		ClusterName: clusterName,
	}
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(service.Region))
	if err != nil {
		return
	}
	service.EC2Client = ec2.NewFromConfig(cfg)

	return service, nil
}
