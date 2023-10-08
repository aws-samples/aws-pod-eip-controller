package recycle

import (
	"context"
	"time"

	"github.com/aws-samples/aws-pod-eip-controller/pkg/service"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type Recycle struct {
	period        int
	vpcID         string
	region        string
	EC2Service    *service.EC2Service
	ShiedService  *service.ShiedService
	clusterClient *dynamic.DynamicClient
}

func NewRecycle(clusterClient *dynamic.DynamicClient, clusterName string, period int, vpcID string, region string) (*Recycle, error) {
	ec2Service, err := service.NewEC2Service(vpcID, region, clusterName)
	if err != nil {
		return nil, err
	}
	shieldService, err := service.NewShieldService(vpcID, region)
	if err != nil {
		return nil, err
	}
	return &Recycle{
		period:        period,
		clusterClient: clusterClient,
		vpcID:         vpcID,
		region:        region,
		EC2Service:    ec2Service,
		ShiedService:  shieldService,
	}, nil
}

func (r *Recycle) Run() {
	account, isSubscription := r.ShiedService.DescribeSubscription()
	for {
		list, err := r.clusterClient.Resource(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}).List(context.TODO(), v1.ListOptions{})
		if err != nil {
			logrus.Error(err)
			time.Sleep(10 * time.Second)
			continue
		}
		IPList := make(map[string]bool, len(list.Items))
		for _, item := range list.Items {
			podIP, exist, err := unstructured.NestedString(item.Object, "status", "podIP")
			if err != nil || !exist {
				continue
			}
			IPList[podIP] = true
		}
		addresses, err := r.EC2Service.DescribeUsedAddresses()
		if err != nil {
			logrus.Error(err)
		}
		for _, address := range addresses {
			logrus.Debug("process: ", address)
			// only process association eip
			if address.PrivateIpAddress == "" || address.AssociationID == "" {
				continue
			}
			if _, ok := IPList[address.PrivateIpAddress]; ok {
				continue
			}
			if isSubscription {
				eipARN := "arn:aws:ec2:" + r.region + ":" + account + ":eip-allocation/" + address.AllocationID
				logrus.Infof("delete protection eipARN:%s", eipARN)
				protectionID, isProtected := r.ShiedService.DiscribeProtection(eipARN)
				if isProtected {
					r.ShiedService.DeleteProtection(protectionID)
				}
			}
			err = r.EC2Service.DisassociateAddress(address.AssociationID)
			if err != nil {
				logrus.Error(err)
			}
			err = r.EC2Service.ReleaseAddress(address.AllocationID)
			if err != nil {
				logrus.Error(err)
			}
			time.Sleep(5 * time.Second)
		}
		if r.period == 0 {
			break
		}
		time.Sleep(time.Duration(r.period) * time.Second)
	}
}
