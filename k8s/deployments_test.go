package k8s_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/urandom/kd/k8s"
	av1 "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

func TestClient_ScaleDeployment(t *testing.T) {
	tests := []struct {
		name    string
		obj     k8s.ObjectMetaGetter
		wantErr bool
	}{
		{name: "not a deployment", obj: &av1.StatefulSet{}, wantErr: true},
		{name: "invalid object", obj: &av1.Deployment{}, wantErr: true},
		{name: "scale", obj: &av1.Deployment{ObjectMeta: meta.ObjectMeta{Name: "test1", Namespace: "default"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]interface{}{}

				w.Header().Add("Content-Type", "application/json")
				if b, err := json.Marshal(resp); err == nil {
					w.Write(b)
				} else {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer ts.Close()

			config := &rest.Config{Host: ts.URL}

			c, err := k8s.NewForConfig(config)
			if err != nil {
				t.Errorf("Client.ScaleDeployment() NewForConfig error = %v", err)
			}
			if err := c.ScaleDeployment(
				k8s.NewGenericCtrl(tt.obj, "", nil, k8s.PodTree{}), 2,
			); (err != nil) != tt.wantErr {
				t.Errorf("Client.ScaleDeployment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
