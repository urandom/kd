package k8s_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/urandom/kd/k8s"
	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

type event struct {
	Object k8s.ObjectMetaGetter `json:"object"`
	Type   string               `json:"type"`
}

func TestClient_PodTreeWatcher(t *testing.T) {
	podWatchEv := event{Object: &pods[0], Type: "ADDED"}
	statefulSetWatchEv := event{Object: &statefulset, Type: "ADDED"}
	deploymentWatchEv := event{Object: &deployment, Type: "ADDED"}
	daemonSetWatchEv := event{Object: &daemonset, Type: "ADDED"}
	jobWatchEv := event{Object: &job, Type: "ADDED"}
	cronJobWatchEv := event{Object: &cronjob, Type: "ADDED"}
	serviceWatchEv := event{Object: &service, Type: "ADDED"}

	tests := []struct {
		name                string
		nsName              string
		podWatchEv          *event
		podWatchErr         error
		podList             cv1.PodList
		podErr              error
		statefulSetWatchEv  *event
		statefulSetWatchErr error
		statefulSetList     av1.StatefulSetList
		statefulSetErr      error
		deploymentWatchEv   *event
		deploymentWatchErr  error
		deploymentList      av1.DeploymentList
		deploymentErr       error
		daemonSetWatchEv    *event
		daemonSetWatchErr   error
		daemonSetList       av1.DaemonSetList
		daemonSetErr        error
		jobWatchEv          *event
		jobWatchErr         error
		jobList             bv1.JobList
		jobErr              error
		cronJobWatchEv      *event
		cronJobWatchErr     error
		cronJobList         bv1b1.CronJobList
		cronJobErr          error
		serviceWatchEv      *event
		serviceWatchErr     error
		serviceList         cv1.ServiceList
		serviceErr          error
		want                []k8s.PodWatcherEvent
		wantErr             bool
	}{
		{name: " pod watch err", podWatchErr: errors.New("err"), wantErr: true},
		{name: "statefulset watch err", statefulSetWatchErr: errors.New("err"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data interface{}
				var err error
				switch r.URL.String() {
				case "/api/v1/pods?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.podWatchEv == nil {
						data = podWatchEv
					} else {
						data = tt.podWatchEv
					}
					err = tt.podWatchErr
				case "/apis/apps/v1/statefulsets?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.statefulSetWatchEv == nil {
						data = statefulSetWatchEv
					} else {
						data = tt.statefulSetWatchEv
					}
					err = tt.statefulSetWatchErr
				case "/apis/apps/v1/deployments?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.deploymentWatchEv == nil {
						data = deploymentWatchEv
					} else {
						data = tt.deploymentWatchEv
					}
					err = tt.deploymentWatchErr
				case "/apis/apps/v1/daemonsets?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.daemonSetWatchEv == nil {
						data = daemonSetWatchEv
					} else {
						data = tt.daemonSetWatchEv
					}
					err = tt.daemonSetWatchErr
				case "/apis/batch/v1/jobs?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.cronJobWatchEv == nil {
						data = jobWatchEv
					} else {
						data = tt.jobWatchEv
					}
					err = tt.jobWatchErr
				case "/apis/batch/v1beta1/cronjobs?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.cronJobWatchEv == nil {
						data = cronJobWatchEv
					} else {
						data = tt.cronJobWatchEv
					}
					err = tt.cronJobWatchErr
				case "/api/v1/services?watch=true":
					time.Sleep(100 * time.Millisecond)
					if tt.serviceWatchEv == nil {
						data = serviceWatchEv
					} else {
						data = tt.serviceWatchEv
					}
					err = tt.serviceWatchErr
				default:
					treeHandler(t,
						tt.podList, tt.podErr,
						tt.statefulSetList, tt.statefulSetErr,
						tt.deploymentList, tt.deploymentErr,
						tt.daemonSetList, tt.daemonSetErr,
						tt.jobList, tt.jobErr,
						tt.cronJobList, tt.cronJobErr,
						tt.serviceList, tt.serviceErr,
					)
					return
				}
				var b []byte
				if err == nil {
					b, err = json.Marshal(data)
				}
				w.Header().Add("Content-Type", "application/json")
				if err == nil {
					if data != nil {
						w.Write(b)
					}
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(context.Background(), &rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}

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
	tests := []struct {
		name            string
		nsName          string
		podList         cv1.PodList
		podErr          error
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
			ts := httptest.NewServer(treeHandler(t,
				tt.podList, tt.podErr,
				tt.statefulSetList, tt.statefulSetErr,
				tt.deploymentList, tt.deploymentErr,
				tt.daemonSetList, tt.daemonSetErr,
				tt.jobList, tt.jobErr,
				tt.cronJobList, tt.cronJobErr,
				tt.serviceList, tt.serviceErr,
			))
			defer ts.Close()

			c, err := k8s.NewForConfig(context.Background(), &rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}

			got, err := c.PodTree(context.Background(), tt.nsName)
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

func treeHandler(t *testing.T,
	podList cv1.PodList,
	podErr error,
	statefulSetList av1.StatefulSetList,
	statefulSetErr error,
	deploymentList av1.DeploymentList,
	deploymentErr error,
	daemonSetList av1.DaemonSetList,
	daemonSetErr error,
	jobList bv1.JobList,
	jobErr error,
	cronJobList bv1b1.CronJobList,
	cronJobErr error,
	serviceList cv1.ServiceList,
	serviceErr error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		var data interface{}
		var err error

		switch r.URL.String() {
		case "/api/v1/pods":
			data = podList
			err = podErr
		case "/apis/apps/v1/statefulsets":
			data = statefulSetList
			err = statefulSetErr
		case "/apis/apps/v1/deployments":
			data = deploymentList
			err = deploymentErr
		case "/apis/apps/v1/daemonsets":
			data = daemonSetList
			err = daemonSetErr
		case "/apis/batch/v1/jobs":
			data = jobList
			err = jobErr
		case "/apis/batch/v1beta1/cronjobs":
			data = cronJobList
			err = cronJobErr
		case "/api/v1/services":
			data = serviceList
			err = serviceErr
		default:
			t.Fatal(r.URL.String())
		}

		var b []byte
		if err == nil {
			b, err = json.Marshal(data)
		}
		if err == nil {
			w.Write(b)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

var (
	pods = []cv1.Pod{
		{TypeMeta: meta.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: meta.ObjectMeta{Name: "p1", UID: "p1", Labels: map[string]string{"app": "p"}}},
		{TypeMeta: meta.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: meta.ObjectMeta{Name: "p2", UID: "p2", Labels: map[string]string{"app": "p"}}},
		{TypeMeta: meta.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: meta.ObjectMeta{Name: "k1", UID: "k1", Labels: map[string]string{"app": "k"}}},
	}
	statefulset = av1.StatefulSet{
		ObjectMeta: meta.ObjectMeta{Name: "p", UID: "sts"}, Spec: av1.StatefulSetSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	deployment = av1.Deployment{
		ObjectMeta: meta.ObjectMeta{Name: "p", UID: "deploy"}, Spec: av1.DeploymentSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	daemonset = av1.DaemonSet{
		ObjectMeta: meta.ObjectMeta{Name: "p", UID: "ds"}, Spec: av1.DaemonSetSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "k"}},
		}}
	job = bv1.Job{
		ObjectMeta: meta.ObjectMeta{Name: "p", OwnerReferences: []meta.OwnerReference{{UID: "cronjob"}}, UID: "job"}, Spec: bv1.JobSpec{
			Selector: &meta.LabelSelector{MatchLabels: map[string]string{"app": "p"}},
		}}
	cronjob = bv1b1.CronJob{ObjectMeta: meta.ObjectMeta{Name: "p", UID: "cronjob"}}
	service = cv1.Service{
		ObjectMeta: meta.ObjectMeta{Name: "p", UID: "svc"}, Spec: cv1.ServiceSpec{
			Selector: map[string]string{"app": "k"},
		}}
)
