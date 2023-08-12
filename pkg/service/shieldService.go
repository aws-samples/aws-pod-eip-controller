// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package service

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/shield"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/sirupsen/logrus"
)

type ShiedService struct {
	VPCID        string
	Region       string
	ShieldClient *shield.Client
}

func (s *ShiedService) DescribeSubscription() (account string, isSubscription bool) {
	// aws shield describe-subscription
	result, err := s.ShieldClient.DescribeSubscription(context.TODO(), &shield.DescribeSubscriptionInput{})
	if err != nil {
		isSubscription = false
		logrus.Warnf("describe subscription error:%s", err)
		return
	}
	arn := *(result.Subscription.SubscriptionArn)
	account = strings.Split(arn, ":")[4]
	isSubscription = true
	logrus.Infof("account:%s", account)
	return
}

func (s *ShiedService) DiscribeProtection(resourceARN string) (protectionID string, isProtected bool) {
	// aws shield describe-protection --resource-arn arn:aws:ec2:ap-southeast-1:900212707297:eip-allocation/eipalloc-054338b22a63a7aff
	result, err := s.ShieldClient.DescribeProtection(context.TODO(), &shield.DescribeProtectionInput{
		ResourceArn: aws.String(resourceARN),
	})
	if err != nil {
		isProtected = false
		logrus.Warnf("describe protection error:%s", err)
		return
	}
	isProtected = true
	protectionID = *result.Protection.Id
	logrus.Infof("describe protection id:%s", protectionID)
	return
}

func (s *ShiedService) CreateProtection(name string, resourceARN string) (err error) {
	// aws shield create-protection --name test --resource-arn arn:aws:ec2:ap-southeast-1:900212707297:eip-allocation/eipalloc-0d9c2c0d160169a27
	result, err := s.ShieldClient.CreateProtection(context.TODO(), &shield.CreateProtectionInput{
		Name:        aws.String(name),
		ResourceArn: aws.String(resourceARN),
	})
	if err != nil {
		return
	}
	logrus.Infof("create protection id:%s", *result.ProtectionId)
	return
}

func (s *ShiedService) DeleteProtection(protectionID string) (err error) {
	// aws shield delete-protection --protection-id e7d8a7c6-b7c8-4b4d-9c8b-f6e7d8a7c6b7
	_, err = s.ShieldClient.DeleteProtection(context.TODO(), &shield.DeleteProtectionInput{
		ProtectionId: aws.String(protectionID),
	})
	if err != nil {
		return
	}
	logrus.Infof("delete protection id:%s", protectionID)
	return
}

func NewShieldService(vpcid string, region string) (service *ShiedService, err error) {
	service = &ShiedService{
		VPCID:  vpcid,
		Region: region,
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(service.Region))
	if err != nil {
		return
	}
	service.ShieldClient = shield.NewFromConfig(cfg)
	return service, nil
}
