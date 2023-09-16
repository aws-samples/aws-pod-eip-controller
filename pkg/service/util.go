package service

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

func GetVpcIDAndRegion(vpcid string, region string) (vpcidRet string, regionRet string, err error) {
	if len(vpcid) > 0 && len(region) > 0 {
		return vpcid, region, nil
	}
	// get vpcid from instance meta url
	url := "http://instance-data/latest/meta-data/network/interfaces/macs/"
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	res, err := client.Get(url)
	if err != nil {
		return
	}
	macs, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}
	mac := strings.Split(string(macs), "\n")[0]
	url = url + string(mac) + "/vpc-id"
	client.Get(url)
	if err != nil {
		return
	}
	res, err = client.Get(url)
	if err != nil {
		return
	}
	vpcID, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}
	vpcid = string(vpcID)
	// get region from instance meta url
	url = "http://instance-data/latest/dynamic/instance-identity/document"
	client = &http.Client{
		Timeout: time.Second * 5,
	}
	res, err = client.Get(url)
	if err != nil {
		return
	}
	document, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}
	region = gjson.Get(string(document), "region").String()

	logrus.WithFields(logrus.Fields{
		"vpcid":  vpcid,
		"region": region,
	}).Info("get info from imds")
	return vpcid, region, nil
}
