package k8s

import (
	"golang.org/x/xerrors"
	autov1 "k8s.io/api/autoscaling/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c Client) ScaleDeployment(o *Deployment, replicas int) error {
	name := o.GetName()
	_, err := c.AppsV1().Deployments(o.GetNamespace()).UpdateScale(
		name,
		&autov1.Scale{
			ObjectMeta: meta.ObjectMeta{Namespace: o.GetNamespace(), Name: name},
			Spec:       autov1.ScaleSpec{Replicas: int32(replicas)},
		},
	)

	if err != nil {
		return xerrors.Errorf("scaling deployment %s: %w", name, err)
	}

	return nil
}
