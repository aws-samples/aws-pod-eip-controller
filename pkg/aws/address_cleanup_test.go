package aws

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestFailedAssociationReleasesAutoAllocations(t *testing.T) {
	allocations, releases := 0, 0
	pending := map[int]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		action := r.Form.Get("Action")
		w.Header().Set("Content-Type", "text/xml")
		switch action {
		case "DescribeNetworkInterfaces":
			fmt.Fprint(w, `<DescribeNetworkInterfacesResponse><networkInterfaceSet><item><networkInterfaceId>eni-test</networkInterfaceId><status>in-use</status><privateIpAddress>10.0.0.10</privateIpAddress></item></networkInterfaceSet></DescribeNetworkInterfacesResponse>`)
		case "AllocateAddress":
			allocations++
			pending[allocations] = true
			fmt.Fprintf(w, `<AllocateAddressResponse><allocationId>eipalloc-test%d</allocationId><publicIp>198.51.100.%d</publicIp></AllocateAddressResponse>`, allocations, allocations)
		case "AssociateAddress":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>simulated IAM denial</Message></Error></Errors><RequestID>test</RequestID></Response>`)
		case "DescribeAddresses":
			var rows strings.Builder
			for i := 1; i <= allocations; i++ {
				if !pending[i] {
					continue
				}
				fmt.Fprintf(&rows, `<item><allocationId>eipalloc-test%d</allocationId><publicIp>198.51.100.%d</publicIp><tagSet><item><key>%s</key><value>auto</value></item></tagSet></item>`, i, i, pkg.TagTypeKey)
			}
			fmt.Fprintf(w, `<DescribeAddressesResponse><addressesSet>%s</addressesSet></DescribeAddressesResponse>`, rows.String())
		case "ReleaseAddress":
			releases++
			var id int
			fmt.Sscanf(r.Form.Get("AllocationId"), "eipalloc-test%d", &id)
			delete(pending, id)
			fmt.Fprint(w, `<ReleaseAddressResponse><return>true</return></ReleaseAddressResponse>`)
		default:
			t.Errorf("unexpected API action %s", action)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	c := EC2Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), vpcID: "vpc-test", clusterName: "test", client: ec2.New(ec2.Options{Region: "eu-central-1", Credentials: sdkaws.AnonymousCredentials{}, BaseEndpoint: sdkaws.String(server.URL), Retryer: sdkaws.NopRetryer{}})}
	for attempt := 0; attempt < 4; attempt++ {
		if err := c.DisassociateAddress(DisassociateAddressOptions{PodKey: "bastion/test"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.AssociateAddress(AssociateAddressOptions{PECType: pkg.PodEIPAnnotationValueAuto, PodKey: "bastion/test", PodIP: "10.0.0.10", HostIP: "10.0.0.1", AddressPoolId: "amazon"}); err == nil {
			t.Fatal("expected association error")
		}
	}
	if err := c.DisassociateAddress(DisassociateAddressOptions{PodKey: "bastion/test"}); err != nil {
		t.Fatal(err)
	}
	if allocations != 4 || releases != 4 || len(pending) != 0 {
		t.Fatalf("allocations=%d releases=%d", allocations, releases)
	}
	t.Log("each failed association rolled back its auto allocation")
}

func TestOrphanedAllocationReleaseCanBeRetried(t *testing.T) {
	attempts := 0
	released := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/xml")
		switch r.Form.Get("Action") {
		case "DescribeAddresses":
			if released {
				fmt.Fprint(w, `<DescribeAddressesResponse><addressesSet/></DescribeAddressesResponse>`)
				return
			}
			fmt.Fprintf(w, `<DescribeAddressesResponse><addressesSet><item><allocationId>eipalloc-orphan</allocationId><publicIp>198.51.100.10</publicIp><tagSet><item><key>%s</key><value>auto</value></item></tagSet></item></addressesSet></DescribeAddressesResponse>`, pkg.TagTypeKey)
		case "ReleaseAddress":
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>simulated transient denial</Message></Error></Errors></Response>`)
				return
			}
			released = true
			fmt.Fprint(w, `<ReleaseAddressResponse><return>true</return></ReleaseAddressResponse>`)
		default:
			t.Errorf("unexpected action %s", r.Form.Get("Action"))
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	c := EC2Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), vpcID: "vpc-test", clusterName: "test", client: ec2.New(ec2.Options{Region: "eu-central-1", Credentials: sdkaws.AnonymousCredentials{}, BaseEndpoint: sdkaws.String(server.URL), Retryer: sdkaws.NopRetryer{}})}
	if err := c.DisassociateAddress(DisassociateAddressOptions{PodKey: "bastion/test"}); err == nil {
		t.Fatal("expected first release failure")
	}
	if err := c.DisassociateAddress(DisassociateAddressOptions{PodKey: "bastion/test"}); err != nil {
		t.Fatal(err)
	}
	if !released || attempts != 2 {
		t.Fatalf("orphan release not retried: attempts=%d released=%v", attempts, released)
	}
}
