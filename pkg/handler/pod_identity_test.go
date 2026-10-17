package handler

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/aws-samples/aws-pod-eip-controller/pkg"
	localaws "github.com/aws-samples/aws-pod-eip-controller/pkg/aws"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type identityENI struct {
	associate func(localaws.AssociateAddressOptions) (string, error)
	calls     int
	ip        string
}

func (e *identityENI) AssociateAddress(o localaws.AssociateAddressOptions) (string, error) {
	e.calls++
	e.ip = o.PodIP
	return e.associate(o)
}
func (e *identityENI) DisassociateAddress(localaws.DisassociateAddressOptions) error { return nil }

func TestStaleReconciliationDoesNotStampReplacementPod(t *testing.T) {
	ctx := context.Background()
	old := v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "bastion-0", Namespace: "bastion", UID: types.UID("old-uid"), Labels: map[string]string{"app": "bastion"}, Annotations: map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValueFixedTagValue, pkg.PodAddressFixedTagValueAnnotationKey: "bastion-pool"}}, Status: v1.PodStatus{PodIP: "10.0.0.10", HostIP: "10.0.0.1"}}
	replacement := old.DeepCopy()
	replacement.UID = types.UID("new-uid")
	replacement.Status.PodIP = "10.0.0.20"
	client := fake.NewSimpleClientset(old.DeepCopy())
	eni := &identityENI{}
	eni.associate = func(localaws.AssociateAddressOptions) (string, error) {
		if eni.calls > 1 {
			return "198.51.100.10", nil
		}
		if err := client.CoreV1().Pods("bastion").Delete(ctx, "bastion-0", metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.CoreV1().Pods("bastion").Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		return "198.51.100.10", nil
	}
	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), client.CoreV1(), eni, nil)
	if err := h.AddOrUpdate("bastion/bastion-0", old); err == nil {
		t.Fatal("expected a UID precondition failure after pod replacement")
	}
	current, err := client.CoreV1().Pods("bastion").Get(ctx, "bastion-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if current.UID != replacement.UID {
		t.Fatal("did not retain replacement UID")
	}
	if current.Labels[pkg.PodPublicIPLabel] != "" {
		t.Fatalf("stale result was stamped onto replacement: %#v", current.Labels)
	}
	if err := h.AddOrUpdate("bastion/bastion-0", *current); err != nil {
		t.Fatal(err)
	}
	if eni.calls != 2 || eni.ip != "10.0.0.20" {
		t.Fatalf("replacement must be reconciled at its new IP; calls=%d ip=%s", eni.calls, eni.ip)
	}
}
