package service

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/sirupsen/logrus"
)

func GetVpcIDAndRegion(vpcid string, region string) (vpcidRet string, regionRet string, err error) {
	if len(vpcid) > 0 && len(region) > 0 {
		return vpcid, region, nil
	}
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		logrus.Fatal()
		return
	}

	client := imds.NewFromConfig(cfg)
	macData, err := client.GetMetadata(context.TODO(), &imds.GetMetadataInput{
		Path: "mac",
	})
	if err != nil {
		return
	}
	macByte, err := io.ReadAll(macData.Content)
	if err != nil {
		return
	}

	vpcidData, err := client.GetMetadata(context.TODO(), &imds.GetMetadataInput{
		Path: fmt.Sprintf("network/interfaces/macs/%s/vpc-id", string(macByte)),
	})
	if err != nil {
		return
	}
	vpcidByte, err := io.ReadAll(vpcidData.Content)
	if err != nil {
		return
	}
	vpcidRet = string(vpcidByte)

	regionData, err := client.GetMetadata(context.TODO(), &imds.GetMetadataInput{
		Path: "placement/region",
	})
	if err != nil {
		return
	}
	regionByte, err := io.ReadAll(regionData.Content)
	if err != nil {
		return
	}
	regionRet = string(regionByte)

	logrus.WithFields(logrus.Fields{
		"vpcid":  vpcidRet,
		"region": regionRet,
	}).Info("get info from imds")
	return
}

func getString(stringPoint *string) string {
	if stringPoint == nil {
		return ""
	} else {
		return *stringPoint
	}
}
