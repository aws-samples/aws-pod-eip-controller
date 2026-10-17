package k8s

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestWatchPreservesListedResourceVersion(t *testing.T) {
	watchRV := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("watch") == "true" {
			watchRV <- r.URL.Query().Get("resourceVersion")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"kind":"PodList","apiVersion":"v1","metadata":{"resourceVersion":"12345"},"items":[]}`)
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	informer := newPodInformer(client, "bastion", 0)
	stop := make(chan struct{})
	defer close(stop)
	go informer.Run(stop)
	select {
	case rv := <-watchRV:
		if rv != "12345" {
			t.Fatalf("watch must resume from listed resourceVersion 12345, got %q", rv)
		}
		t.Log("watch resumes from the listed resourceVersion")
	case <-time.After(5 * time.Second):
		t.Fatal("no watch request")
	}
}
