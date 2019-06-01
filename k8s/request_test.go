package k8s_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/urandom/kd/k8s"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

func TestClient_RestClient(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		err     error
		timeout time.Duration
		data    []byte
		want    k8s.ObjectMetaGetter
		wantErr bool
	}{
		{name: "request error", err: errors.New("err"), wantErr: true},
		{name: "not json", data: []byte("{non-json>data"), wantErr: true},
		{name: "custom object", uri: "/apis/custom.surg.org/v1/custom/obj1", data: []byte(customObjData), want: &k8s.GenericObj{
			TypeMeta: meta.TypeMeta{"CustomObject", "custom.sugr.org/v1"},
			ObjectMeta: meta.ObjectMeta{
				Generation:      1,
				Name:            "obj1",
				ResourceVersion: "957447",
				SelfLink:        "/apis/custom.surg.org/v1/custom/obj1",
				UID:             "7287f784-83c2-11e9-8d6d-d402815ed0bf",
			},
			Spec: map[string]interface{}{
				"extraData": float64(42),
				"arr":       []interface{}{1.0, 2.0, 3.0},
				"obj":       map[string]interface{}{"foo": 1.0},
			},
			Other: map[string]interface{}{
				"custom": 42.0,
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.err != nil {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Add("Content-Type", "application/json")
				w.Write(tt.data)
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(&rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			rc, err := c.RestClient(tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ResultToObject(rc.Get().Timeout(tt.timeout).Do())
			t.Log(err)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.RestClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("Client.RestClient() diff = %v", diff)
			}
		})
	}
}

func TestClient_RestClient_List(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		err     error
		timeout time.Duration
		data    []byte
		want    []k8s.GenericObj
		wantErr bool
	}{
		{name: "not json", data: []byte("{non-json>data"), wantErr: true},
		{name: "custom object", uri: "/apis/custom.surg.org/v1/custom", data: []byte(customObjDataList), want: []k8s.GenericObj{
			{ObjectMeta: meta.ObjectMeta{Name: "obj1"}, Spec: map[string]interface{}{"data": "foo"}},
			{ObjectMeta: meta.ObjectMeta{Name: "obj2"}, Spec: map[string]interface{}{"data": "bar"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.err != nil {
					http.Error(w, tt.err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Add("Content-Type", "application/json")
				w.Write(tt.data)
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(&rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}
			rc, err := c.RestClient(tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ResultToObjectList(rc.Get().Timeout(tt.timeout).Do())
			t.Log(err)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.RestClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("Client.RestClient() diff = %v", diff)
			}
		})
	}
}

var (
	customObjData = `
{
    "apiVersion": "custom.sugr.org/v1",
    "kind": "CustomObject",
    "metadata": {
        "generation": 1,
        "name": "obj1",
        "resourceVersion": "957447",
        "selfLink": "/apis/custom.surg.org/v1/custom/obj1",
        "uid": "7287f784-83c2-11e9-8d6d-d402815ed0bf"
    },
    "spec": {
        "extraData": 42,
        "arr": [1,2,3],
        "obj": {"foo": 1}
    },
    "custom": 42
}
`
	customObjDataList = `
{
    "apiVersion": "custom.sugr.org/v1",
	"items": [
		{
			"metadata": {"name": "obj1"},
			"spec": {"data": "foo"}
		},
		{
			"metadata": {"name": "obj2"},
			"spec": {"data": "bar"}
		}
	]
}
`
)
