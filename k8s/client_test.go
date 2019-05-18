package k8s_test

import (
	"errors"
	"reflect"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/k8s/mock"
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

			clientset := mock.NewMockClientSet(ctrl)
			corev1 := mock.NewMockCoreV1Interface(ctrl)
			ns := mock.NewMockNamespaceInterface(ctrl)

			clientset.EXPECT().CoreV1().Return(corev1)
			corev1.EXPECT().Namespaces().Return(ns)
			ns.EXPECT().List(gomock.Any()).Return(tt.nsList, tt.err)

			c := k8s.NewFromClientSet(clientset)
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
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			obj := mock.NewMockObjectMetaGetter(ctrl)
			clientset := mock.NewMockClientSet(ctrl)
			corev1 := mock.NewMockCoreV1Interface(ctrl)
			event := mock.NewMockEventInterface(ctrl)
			selector := mock.NewMockSelector(ctrl)

			obj.EXPECT().GetObjectMeta().AnyTimes().Return(&metav1.ObjectMeta{})
			clientset.EXPECT().CoreV1().Return(corev1)
			corev1.EXPECT().Events(gomock.Any()).Return(event)
			event.EXPECT().GetFieldSelector(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(selector)
			selector.EXPECT().String()
			event.EXPECT().List(gomock.Any()).Return(tt.eventList, tt.err)

			c := k8s.NewFromClientSet(clientset)
			got, err := c.Events(obj)
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
