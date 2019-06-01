package k8s_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/urandom/kd/k8s"
	av1 "k8s.io/api/apps/v1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

func TestClient_Logs(t *testing.T) {
	tests := []struct {
		name      string
		timeout   time.Duration
		object    k8s.ObjectMetaGetter
		container string
		colors    []string
		want      [][]byte
		wantErr   bool
	}{
		{name: "no valid object"},
		{name: "no pods in object", object: k8s.NewCtrlWithPods(&av1.Deployment{}, k8s.CategoryDeployment, nil, nil)},
		{name: "pod in object - timeout", object: k8s.NewCtrlWithPods(&av1.Deployment{}, k8s.CategoryDeployment, nil, []*cv1.Pod{
			{ObjectMeta: meta.ObjectMeta{Name: "pod1"}, Status: cv1.PodStatus{
				ContainerStatuses: []cv1.ContainerStatus{{Name: "con1"}},
			}},
		}), colors: []string{"red", "blue"}, timeout: 100 * time.Millisecond},
		{name: "pod - timeout", object: &cv1.Pod{
			ObjectMeta: meta.ObjectMeta{Name: "pod1"}, Status: cv1.PodStatus{
				ContainerStatuses: []cv1.ContainerStatus{{Name: "con1"}},
			},
		}, colors: []string{"red", "blue"}, timeout: 100 * time.Millisecond},
		{name: "multi container - timeout", object: &cv1.Pod{
			ObjectMeta: meta.ObjectMeta{Name: "pod1"}, Status: cv1.PodStatus{
				ContainerStatuses: []cv1.ContainerStatus{{Name: "con1"}, {Name: "con2", LastTerminationState: cv1.ContainerState{Terminated: &cv1.ContainerStateTerminated{}}}},
			},
		}, colors: []string{"red", "blue"}, wantErr: true},
		{name: "multi container pod specified - timeout", object: &cv1.Pod{
			ObjectMeta: meta.ObjectMeta{Name: "pod1"}, Status: cv1.PodStatus{
				ContainerStatuses: []cv1.ContainerStatus{{Name: "con1"}, {Name: "con2", LastTerminationState: cv1.ContainerState{Terminated: &cv1.ContainerStateTerminated{}}}},
			},
		}, container: "con2", colors: []string{"red", "blue"}, timeout: 100 * time.Millisecond},
		{name: "multi container pod specified previous - timeout", object: &cv1.Pod{
			ObjectMeta: meta.ObjectMeta{Name: "pod1"}, Status: cv1.PodStatus{
				ContainerStatuses: []cv1.ContainerStatus{{Name: "con1"}, {Name: "con2", LastTerminationState: cv1.ContainerState{Terminated: &cv1.ContainerStateTerminated{}}}},
			},
		}, container: "previous:con2", colors: []string{"red", "blue"}, timeout: 100 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Content-Type", "application/json")
				if b, err := json.Marshal(nil); err == nil {
					w.Write(b)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(&rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Logs(timeoutAfter(tt.timeout), tt.object, tt.container, tt.colors)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Logs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil && tt.want == nil {
				return
			}
			for data := range got {
				var found bool
				for i := range tt.want {
					if bytes.Equal(data, tt.want[i]) {
						found = true
					}
				}

				if !found {
					t.Errorf("Client.Logs() = %v, want %v", data, tt.want)
				}
			}
		})
	}
}

func timeoutAfter(d time.Duration) context.Context {
	if d == 0 {
		return context.Background()
	}
	ctx, cancel := context.WithCancel(context.Background())

	time.AfterFunc(d, cancel)

	return ctx
}
