// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package handler

import (
	"bytes"
	"encoding/binary"
	"net"

	"github.com/aws-samples/aws-pod-eip-controller/pkg/service"
	"github.com/sirupsen/logrus"
)

type event struct {
	PodIP           string
	ResourceVersion string
	AttachIP        bool
	ShiedAdv        bool
}

func (e *event) PodIP2Int() uint32 {
	var long uint32
	binary.Read(bytes.NewBuffer(net.ParseIP(e.PodIP).To4()), binary.BigEndian, &long)
	return long
}

func (e *event) Process(saveEvent *event, ec2Service *service.EC2Service, shieldService *service.ShiedService) error {
	// EIP attach
	var allocationID, associationID, eni string
	var isAllocated bool
	var err error
	if saveEvent == nil || e.AttachIP != saveEvent.AttachIP {
		// process eip attach
		eni, err = ec2Service.DescribeNetworkInterfaces(e.PodIP)
		if err != nil {
			return err
		}
		associationID, allocationID, isAllocated, err = ec2Service.DescribeAddresses(e.PodIP, eni)
		if err != nil {
			return err
		}
		if isAllocated != e.AttachIP {
			if e.AttachIP {
				allocationID, err = ec2Service.AllocateAddress()
				if err != nil {
					return err
				}
				err = ec2Service.AssociateAddress(e.PodIP, eni, allocationID)
				if err != nil {
					return err
				}
			} else {
				err = ec2Service.DisassociateAddress(associationID)
				if err != nil {
					return err
				}
				err = ec2Service.ReleaseAddress(allocationID)
				if err != nil {
					return err
				}
			}
		}
	}
	// Shied Attach
	if saveEvent == nil || e.ShiedAdv != saveEvent.ShiedAdv {
		// process shield attach
		account, isSubscription := shieldService.DescribeSubscription()
		if !isSubscription {
			return nil
		}
		eipARN := "arn:aws:ec2:" + ec2Service.Region + ":" + account + ":eip-allocation/" + allocationID
		logrus.Infof("eipARN:%s", eipARN)
		protectID, isProected := shieldService.DiscribeProtection(eipARN)
		if isProected != e.ShiedAdv {
			if e.ShiedAdv {
				err = shieldService.CreateProtection("EIP-"+allocationID, eipARN)
			} else {
				err = shieldService.DeleteProtection(protectID)
			}
			return err
		}
	}
	return nil
}
