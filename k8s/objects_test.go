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
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClient_PodTreeWatcher(t *testing.T) {
	tests := []struct {
		name    string
		nsName  string
		want    []k8s.PodWatcherEvent
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

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gotC, err := c.PodTreeWatcher(ctx, tt.nsName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.PodTreeWatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, ev := range tt.want {
				got := <-gotC
				if !reflect.DeepEqual(got, ev) {
					t.Errorf("Client.PodTreeWatcher() = %v, want %v", got, tt.want)
				}
			}

		})
	}
}

func TestClient_PodTree(t *testing.T) {
	pods := []cv1.Pod{
		{ObjectMeta: meta.ObjectMeta{Name: "p1", Labels: map[string]string{"app": "p"}}},
		{ObjectMeta: meta.ObjectMeta{Name: "p2", Labels: map[string]string{"app": "p"}}},
		{ObjectMeta: meta.ObjectMeta{Name: "k1", Labels: map[string]string{"app": "k"}}},
	}
	statefulset := av1.StatefulSet{
		ObjectMeta: meta.ObjectMeta{Name: "p"}, Spec: av1.StatefulSetSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	deployment := av1.Deployment{
		ObjectMeta: meta.ObjectMeta{Name: "p"}, Spec: av1.DeploymentSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	daemonset := av1.DaemonSet{
		ObjectMeta: meta.ObjectMeta{Name: "p"}, Spec: av1.DaemonSetSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "k"}},
		}}
	job := bv1.Job{
		ObjectMeta: meta.ObjectMeta{Name: "p", OwnerReferences: []meta.OwnerReference{{UID: "cronjob"}}}, Spec: bv1.JobSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	cronjob := bv1b1.CronJob{ObjectMeta: meta.ObjectMeta{Name: "p", UID: "cronjob"}}
	service := cv1.Service{
		ObjectMeta: meta.ObjectMeta{Name: "p"}, Spec: cv1.ServiceSpec{
			Selector: map[string]string{"app": "k"},
		}}
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
		{name: "statefulset list error", statefulSetErr: errors.New("pod err"), wantErr: true},
		{name: "deployment list error", deploymentErr: errors.New("pod err"), wantErr: true},
		{name: "daemonset list error", daemonSetErr: errors.New("pod err"), wantErr: true},
		{name: "job list error", jobErr: errors.New("pod err"), wantErr: true},
		{name: "cronJob list error", cronJobErr: errors.New("pod err"), wantErr: true},
		{name: "service list error", serviceErr: errors.New("pod err"), wantErr: true},
		{name: "stateful set", podList: cv1.PodList{Items: pods},
			statefulSetList: av1.StatefulSetList{Items: []av1.StatefulSet{statefulset}},
			want: k8s.Controllers{k8s.NewCtrlWithPods(
				&statefulset, k8s.CategoryStatefulSet,
				statefulset.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[:2]),
			)},
		},
		{name: "deployment", podList: cv1.PodList{Items: pods},
			deploymentList: av1.DeploymentList{Items: []av1.Deployment{deployment}},
			want: k8s.Controllers{k8s.NewCtrlWithPods(
				&deployment, k8s.CategoryDeployment,
				deployment.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[:2]),
			)},
		},
		{name: "daemon set", podList: cv1.PodList{Items: pods},
			daemonSetList: av1.DaemonSetList{Items: []av1.DaemonSet{daemonset}},
			want: k8s.Controllers{k8s.NewCtrlWithPods(
				&daemonset, k8s.CategoryDaemonSet,
				daemonset.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[2:]),
			)},
		},
		{name: "job", podList: cv1.PodList{Items: pods},
			jobList: bv1.JobList{Items: []bv1.Job{job}},
			want: k8s.Controllers{k8s.NewCtrlWithPods(
				&job, k8s.CategoryJob,
				job.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[:2]),
			)},
		},
		{name: "job + cronjob", podList: cv1.PodList{Items: pods},
			jobList:     bv1.JobList{Items: []bv1.Job{job}},
			cronJobList: bv1b1.CronJobList{Items: []bv1b1.CronJob{cronjob}},
			want: k8s.Controllers{
				k8s.NewCtrlWithPods(
					&job, k8s.CategoryJob,
					job.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[:2]),
				),
				k8s.NewCtrlWithPods(
					&cronjob, k8s.CategoryCronJob,
					map[string]string{"app": "p"}, k8s.Pods{}.AddSlice(pods[:2]),
				)},
		},
		{name: "service", podList: cv1.PodList{Items: pods},
			serviceList: cv1.ServiceList{Items: []cv1.Service{service}},
			want: k8s.Controllers{k8s.NewCtrlWithPods(
				&service, k8s.CategoryService,
				service.Spec.Selector, k8s.Pods{}.AddSlice(pods[2:]),
			)},
		},
		{name: "deployment, daemonset, service", podList: cv1.PodList{Items: pods},
			deploymentList: av1.DeploymentList{Items: []av1.Deployment{deployment}},
			daemonSetList:  av1.DaemonSetList{Items: []av1.DaemonSet{daemonset}},
			serviceList:    cv1.ServiceList{Items: []cv1.Service{service}},
			want: k8s.Controllers{
				k8s.NewCtrlWithPods(
					&deployment, k8s.CategoryDeployment,
					deployment.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[:2]),
				),
				k8s.NewCtrlWithPods(
					&daemonset, k8s.CategoryDaemonSet,
					daemonset.Spec.Selector.MatchLabels, k8s.Pods{}.AddSlice(pods[2:]),
				),
				k8s.NewCtrlWithPods(
					&service, k8s.CategoryService,
					service.Spec.Selector, k8s.Pods{}.AddSlice(pods[2:]),
				)},
		},
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
