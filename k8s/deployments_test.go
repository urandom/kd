package k8s_test

import (
	"errors"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/k8s/mock"
	av1 "k8s.io/api/apps/v1"
)

func TestClient_ScaleDeployment(t *testing.T) {
	tests := []struct {
		name     string
		obj      k8s.ObjectMetaGetter
		replicas int
		err      error
		wantErr  bool
	}{
		{name: "not a deployment", obj: &av1.StatefulSet{}, wantErr: true},
		{name: "scale err", obj: &av1.Deployment{}, err: errors.New("err"), wantErr: true},
		{name: "scale", obj: &av1.Deployment{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			clientset := mock.NewMockClientSet(ctrl)
			apps := mock.NewMockAppsV1Interface(ctrl)
			deploy := mock.NewMockDeploymentInterface(ctrl)

			clientset.EXPECT().AppsV1().AnyTimes().Return(apps)
			apps.EXPECT().Deployments(gomock.Any()).AnyTimes().Return(deploy)
			deploy.EXPECT().UpdateScale(gomock.Any(), gomock.Any()).AnyTimes().Return(nil, tt.err)

			c := k8s.NewFromClientSet(clientset)
			if err := c.ScaleDeployment(
				k8s.NewGenericCtrl(tt.obj, "", nil, k8s.PodTree{}), tt.replicas,
			); (err != nil) != tt.wantErr {
				t.Errorf("Client.ScaleDeployment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
