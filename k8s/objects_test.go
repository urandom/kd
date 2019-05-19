package k8s_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/k8s/mock"
	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
)

func TestClient_PodTreeWatcher(t *testing.T) {
	type args struct {
		ctx    context.Context
		nsName string
	}
	tests := []struct {
		name    string
		args    args
		want    <-chan k8s.PodWatcherEvent
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			clientset := mock.NewMockClientSet(ctrl)
			c := k8s.NewFromClientSet(clientset)
			got, err := c.PodTreeWatcher(tt.args.ctx, tt.args.nsName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.PodTreeWatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Client.PodTreeWatcher() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_PodTree(t *testing.T) {
	tests := []struct {
		name            string
		nsName          string
		podList         cv1.PodList
		statefulSetList av1.StatefulSetList
		statefulSetErr  error
		deploymentList  av1.DeploymentList
		deploymentErr   error
		daemonSetList   av1.DaemonSetList
		daemonSetErr    error
		jobList         bv1.JobList
		jobErr          error
		cronJobList     bv1b1.CronJobList
		cronJobErr      error
		serviceList     cv1.ServiceList
		serviceErr      error
		podErr          error
		want            k8s.Controllers
		wantErr         bool
	}{
		{name: "post list error", podErr: errors.New("pod err"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			clientset := mock.NewMockClientSet(ctrl)
			corev1 := mock.NewMockCoreV1Interface(ctrl)
			appsv1 := mock.NewMockAppsV1Interface(ctrl)
			batchv1 := mock.NewMockBatchV1Interface(ctrl)
			batchBeta := mock.NewMockBatchV1beta1Interface(ctrl)
			pods := mock.NewMockPodInterface(ctrl)
			statefulset := mock.NewMockStatefulSetInterface(ctrl)
			deployment := mock.NewMockDeploymentInterface(ctrl)
			daemonset := mock.NewMockDaemonSetInterface(ctrl)
			job := mock.NewMockJobInterface(ctrl)
			cronjob := mock.NewMockCronJobInterface(ctrl)
			service := mock.NewMockServiceInterface(ctrl)

			clientset.EXPECT().CoreV1().AnyTimes().Return(corev1)
			clientset.EXPECT().AppsV1().AnyTimes().Return(appsv1)
			clientset.EXPECT().BatchV1().AnyTimes().Return(batchv1)
			clientset.EXPECT().BatchV1beta1().AnyTimes().Return(batchBeta)

			corev1.EXPECT().Pods(gomock.Any()).AnyTimes().Return(pods)

			appsv1.EXPECT().StatefulSets(gomock.Any()).AnyTimes().Return(statefulset)
			appsv1.EXPECT().Deployments(gomock.Any()).AnyTimes().Return(deployment)
			appsv1.EXPECT().DaemonSets(gomock.Any()).AnyTimes().Return(daemonset)

			statefulset.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.statefulSetList, tt.statefulSetErr)
			deployment.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.deploymentList, tt.deploymentErr)
			daemonset.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.daemonSetList, tt.daemonSetErr)

			batchv1.EXPECT().Jobs(gomock.Any()).AnyTimes().Return(job)
			job.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.jobList, tt.jobErr)

			batchBeta.EXPECT().CronJobs(gomock.Any()).AnyTimes().Return(cronjob)
			cronjob.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.cronJobList, tt.cronJobErr)

			corev1.EXPECT().Services(gomock.Any()).AnyTimes().Return(service)
			service.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.serviceList, tt.serviceErr)

			pods.EXPECT().List(gomock.Any()).AnyTimes().Return(&tt.podList, tt.podErr)

			c := k8s.NewFromClientSet(clientset)
			got, err := c.PodTree(tt.nsName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.PodTree() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Controllers, tt.want) {
				t.Errorf("Client.PodTree() = %v, want %v", got, tt.want)
			}
		})
	}
}
