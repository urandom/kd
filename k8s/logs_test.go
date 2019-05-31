package k8s_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/urandom/kd/k8s"
	rest "k8s.io/client-go/rest"
)

func TestClient_Logs(t *testing.T) {
	type args struct {
		ctx       context.Context
		object    k8s.ObjectMetaGetter
		container string
		colors    []string
	}
	tests := []struct {
		name    string
		args    args
		want    <-chan []byte
		wantErr bool
	}{
		// TODO: Add test cases.
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
			got, err := c.Logs(tt.args.ctx, tt.args.object, tt.args.container, tt.args.colors)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Logs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Client.Logs() = %v, want %v", got, tt.want)
			}
		})
	}
}
