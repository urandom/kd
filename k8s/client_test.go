package k8s_test

import (
	"errors"
	"reflect"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/urandom/kd/k8s"
	cv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			clientset := NewMockClientSet(ctrl)
			corev1 := NewMockCoreV1Interface(ctrl)
			ns := NewMockNamespaceInterface(ctrl)

			clientset.EXPECT().CoreV1().Return(corev1)
			corev1.EXPECT().Namespaces().Return(ns)
			ns.EXPECT().List(gomock.Any()).Return(tt.nsList, tt.err)

			c := &k8s.Client{
				ClientSet: clientset,
			}
			got, err := c.Namespaces()
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
