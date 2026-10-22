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
	"k8s.io/client-go/kubernetes/fake"
)

type okENI struct{}

func (okENI) AssociateAddress(localaws.AssociateAddressOptions) (string, error) {
	return "198.51.100.10", nil
}
func (okENI) DisassociateAddress(localaws.DisassociateAddressOptions) error { return nil }

func newPod(labels map[string]string) v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "ns", Labels: labels,
			Annotations: map[string]string{pkg.PodEIPAnnotationKey: pkg.PodEIPAnnotationValueAuto},
		},
		Status: v1.PodStatus{PodIP: "10.0.0.10", HostIP: "10.0.0.1"},
	}
}

func TestAssociateLabelsPodThatHasNoLabels(t *testing.T) {
	cases := map[string]map[string]string{
		"nil labels":   nil,
		"empty labels": {},
		"other labels": {"app": "x"},
	}
	for name, labels := range cases {
		t.Run(name, func(t *testing.T) {
			pod := newPod(labels)
			client := fake.NewSimpleClientset(pod.DeepCopy())
			h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), client.CoreV1(), okENI{}, nil)
			if err := h.AddOrUpdate("ns/p", pod); err != nil {
				t.Fatalf("AddOrUpdate: %v", err)
			}
			got, err := client.CoreV1().Pods("ns").Get(context.Background(), "p", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Labels[pkg.PodPublicIPLabel] != "198.51.100.10" || got.Labels[pkg.PodEIPAnnotationKeyLabel] != pkg.PodEIPAnnotationValueAuto {
				t.Fatalf("labels not applied: %#v", got.Labels)
			}
			for k, v := range labels {
				if got.Labels[k] != v {
					t.Fatalf("existing label %s lost: %#v", k, got.Labels)
				}
			}
		})
	}
}

// The UID guard added for replacement pods must still apply on the
// "pod has no labels" retry path: a stale reconciliation must not stamp a
// replacement pod, even when that pod has no labels.
func TestUIDGuardStillAppliesWhenPodHasNoLabels(t *testing.T) {
	old := newPod(nil)
	old.UID = "old-uid"
	replacement := newPod(nil)
	replacement.UID = "new-uid"
	client := fake.NewSimpleClientset(replacement.DeepCopy())
	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), client.CoreV1(), okENI{}, nil)
	if err := h.AddOrUpdate("ns/p", old); err == nil {
		t.Fatal("expected UID precondition failure for stale reconciliation")
	}
	got, err := client.CoreV1().Pods("ns").Get(context.Background(), "p", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Labels) != 0 {
		t.Fatalf("stale result was stamped onto replacement pod: %#v", got.Labels)
	}
}
