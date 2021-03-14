package k8s_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/urandom/kd/k8s"
	av1 "k8s.io/api/apps/v1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

func TestClient_Namespaces(t *testing.T) {
	tests := []struct {
		name    string
		nsList  *cv1.NamespaceList
		err     error
		want    []string
		wantErr bool
	}{
		{name: "error", err: errors.New("error"), wantErr: true},
		{name: "list", nsList: &cv1.NamespaceList{Items: []cv1.Namespace{
			{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
		}}, want: []string{"", "ns1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.err != nil {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Add("Content-Type", "application/json")
				if b, err := json.Marshal(tt.nsList); err == nil {
					w.Write(b)
				} else {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
				}
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(context.Background(), &rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Namespaces(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Namespaces() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Client.Namespaces() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_Events(t *testing.T) {
	tests := []struct {
		name      string
		eventList *cv1.EventList
		err       error
		want      []cv1.Event
		wantErr   bool
	}{
		{name: "error", err: errors.New("error"), wantErr: true},
		{name: "list", eventList: &cv1.EventList{Items: []cv1.Event{
			{ObjectMeta: metav1.ObjectMeta{Name: "ev1"}},
		}}, want: []cv1.Event{{ObjectMeta: metav1.ObjectMeta{Name: "ev1"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.err != nil {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Add("Content-Type", "application/json")
				if b, err := json.Marshal(tt.eventList); err == nil {
					w.Write(b)
				} else {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
				}
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(context.Background(), &rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}

			got, err := c.Events(context.Background(), &av1.Deployment{ObjectMeta: meta.ObjectMeta{Name: "test1", Namespace: "default"}})
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Events() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Client.Events() = %v, want %v", got, tt.want)
			}
		})
	}
}
