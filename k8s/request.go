package k8s

import (
	"encoding/json"
	"fmt"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	rest "k8s.io/client-go/rest"
)

func (c *Client) RestClient(uri string) (*rest.RESTClient, error) {
	conf := *c.config

	conf.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	if conf.UserAgent == "" {
		conf.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	conf.APIPath = uri
	conf.GroupVersion = &schema.GroupVersion{}

	return rest.RESTClientFor(&conf)
}

type GenericObj struct {
	meta.TypeMeta   `json:",inline"`
	meta.ObjectMeta `json:"metadata,omitempty"`

	Spec   map[string]interface{} `json:"spec,omitempty"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Status map[string]interface{} `json:"status,omitempty"`
	Other  map[string]interface{} `json:"other-unknown,omitempty"`
}

func (o *GenericObj) UnmarshalJSON(data []byte) error {
	obj := struct {
		meta.TypeMeta   `json:",inline"`
		meta.ObjectMeta `json:"metadata,omitempty"`

		Spec   map[string]interface{} `json:"spec,omitempty"`
		Data   map[string]interface{} `json:"data,omitempty"`
		Status map[string]interface{} `json:"status,omitempty"`
	}{}

	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}

	o.TypeMeta = obj.TypeMeta
	o.ObjectMeta = obj.ObjectMeta
	o.Spec = obj.Spec
	o.Data = obj.Data
	o.Status = obj.Status

	m := map[string]interface{}{}
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing result data - other: %w", err)
	}

	delete(m, "apiVersion")
	delete(m, "kind")
	delete(m, "metadata")
	delete(m, "spec")
	delete(m, "data")
	delete(m, "status")

	for k, v := range m {
		if o.Other == nil {
			o.Other = map[string]interface{}{}
		}
		o.Other[k] = v
	}

	return nil
}

type GenericObjList struct {
	Items []GenericObj `json:"items"`
}

func (c *Client) ResultToObject(result rest.Result) (ObjectMetaGetter, error) {
	b, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("obtaining result data: %w", err)
	}

	var o GenericObj

	if err := json.Unmarshal(b, &o); err != nil {
		return nil, fmt.Errorf("parsing result data: %w", err)
	}

	return &o, nil
}

func (c *Client) ResultToObjectList(result rest.Result) ([]GenericObj, error) {
	b, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("obtaining result data: %w", err)
	}

	var o GenericObjList

	if err := json.Unmarshal(b, &o); err != nil {
		return nil, fmt.Errorf("parsing result data: %w", err)
	}

	return o.Items, nil
}
