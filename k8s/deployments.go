package k8s

import (
	"context"
	"fmt"

	av1 "k8s.io/api/apps/v1"
	autov1 "k8s.io/api/autoscaling/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) ScaleDeployment(ctx context.Context, o Controller, replicas int) error {
	name := o.GetObjectMeta().GetName()
	if _, ok := o.Controller().(*av1.Deployment); !ok {
		return fmt.Errorf("controller %s not a deployment", name)
	}

	_, err := c.AppsV1().Deployments(o.GetObjectMeta().GetNamespace()).UpdateScale(
		ctx, name,
		&autov1.Scale{
			ObjectMeta: meta.ObjectMeta{Namespace: o.GetObjectMeta().GetNamespace(), Name: name},
			Spec:       autov1.ScaleSpec{Replicas: int32(replicas)},
		},
		meta.UpdateOptions{},
	)

	if err != nil {
		return fmt.Errorf("scaling deployment %s: %w", name, NormalizeError(err))
	}

	return nil
}
