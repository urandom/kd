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
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
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
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Content-Type", "application/json")
				var data interface{}
				var err error

				switch r.URL.String() {
				case "/api/v1/pods":
					data = tt.podList
					err = tt.podErr
				case "/apis/apps/v1/statefulsets":
					data = tt.statefulSetList
					err = tt.statefulSetErr
				case "/apis/apps/v1/deployments":
					data = tt.deploymentList
					err = tt.deploymentErr
				case "/apis/apps/v1/daemonsets":
					data = tt.daemonSetList
					err = tt.daemonSetErr
				case "/apis/batch/v1/jobs":
					data = tt.jobList
					err = tt.jobErr
				case "/apis/batch/v1beta1/cronjobs":
					data = tt.cronJobList
					err = tt.cronJobErr
				case "/api/v1/services":
					data = tt.serviceList
					err = tt.serviceErr
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
			}))
			defer ts.Close()

			c, err := k8s.NewForConfig(&rest.Config{Host: ts.URL})
			if err != nil {
				t.Fatal(err)
			}

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
