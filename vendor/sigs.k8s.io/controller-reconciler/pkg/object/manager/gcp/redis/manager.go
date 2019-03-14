/*
Copyright 2018 Google LLC
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package redis

import (
	"context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/redis/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sigs.k8s.io/controller-reconciler/pkg/object"
	rmanager "sigs.k8s.io/controller-reconciler/pkg/object/manager"
	"sigs.k8s.io/controller-reconciler/pkg/object/manager/gcp"
)

// constants
const (
	Type      = "redis"
	UserAgent = "kcc/controller-manager"
)

// RsrcManager - complies with resource manager interface
type RsrcManager struct {
	name    string
	service *redis.Service
}

// Getter returns nil manager
func Getter(ctx context.Context) func() (string, rmanager.Manager, error) {
	return func() (string, rmanager.Manager, error) {
		rm := &RsrcManager{}
		service, err := NewService(ctx)
		if err != nil {
			return Type, nil, err
		}
		rm.WithService(service).WithName(Type + "Mgr")
		return Type, rm, nil
	}
}

// NewRsrcManager returns nil manager
func NewRsrcManager(ctx context.Context, name string) (*RsrcManager, error) {
	rm := &RsrcManager{}
	service, err := NewService(ctx)
	if err != nil {
		return nil, err
	}
	rm.WithService(service).WithName(name)
	return rm, nil
}

// WithName adds name
func (rm *RsrcManager) WithName(v string) *RsrcManager {
	rm.name = v
	return rm
}

// WithService adds storage service
func (rm *RsrcManager) WithService(s *redis.Service) *RsrcManager {
	rm.service = s
	return rm
}

// Object - PD object
type Object struct {
	Obj        *redis.Instance
	Parent     string
	InstanceID string
}

// SetOwnerReferences - return name string
func (o *Object) SetOwnerReferences(refs *metav1.OwnerReference) bool { return false }

// IsSameAs - return name string
func (o *Object) IsSameAs(a interface{}) bool {
	same := false
	e := a.(*Object)
	if e.Parent == o.Parent && e.InstanceID == o.InstanceID {
		same = true
	}
	return same
}

// GetName - return name string
func (o *Object) GetName() string {
	return o.Obj.Name + "(" + o.Obj.DisplayName + ")"
}

// Observable captures the k8s resource info and selector to fetch child resources
type Observable struct {
	// Labels list of labels
	Labels map[string]string
	// Object
	Obj        *redis.Instance
	Parent     string
	InstanceID string
}

// AsItem wraps object as resource item
func (o *Object) AsItem() *object.Item {
	return &object.Item{
		Obj:       o,
		Lifecycle: object.LifecycleManaged,
		Type:      Type,
	}
}

// NewObservable returns an observable object
func NewObservable(o *Object, labels map[string]string) object.Observable {
	return object.Observable{
		Type: Type,
		Obj: Observable{
			Labels:     labels,
			Obj:        o.Obj,
			Parent:     o.Parent,
			InstanceID: o.InstanceID,
		},
	}
}

// ObservablesFromObjects returns ObservablesFromObjects
func (rm *RsrcManager) ObservablesFromObjects(bag *object.Bag, labels map[string]string) []object.Observable {
	var observables []object.Observable
	for _, item := range bag.Items() {
		if item.Type != Type {
			continue
		}
		obj, ok := item.Obj.(*Object)
		if !ok {
			continue
		}
		observables = append(observables, NewObservable(obj, labels))

	}
	return observables
}

// CopyMutatedSpecFields - copy known mutated fields from observed to expected
func CopyMutatedSpecFields(to *object.Item, from *object.Item) {
	e := to.Obj.(*Object).Obj
	o := from.Obj.(*Object).Obj
	if e.AlternativeLocationId == "" {
		e.AlternativeLocationId = o.AlternativeLocationId
	}
	if e.AuthorizedNetwork == "" {
		e.AuthorizedNetwork = o.AuthorizedNetwork
	}
	if e.RedisVersion == "" {
		e.RedisVersion = o.RedisVersion
	}
	if e.ReservedIpRange == "" {
		e.ReservedIpRange = o.ReservedIpRange
	}
	if e.Tier == "" {
		e.Tier = o.Tier
	}
}

// SpecDiffers - check if the spec part differs
func (rm *RsrcManager) SpecDiffers(expected, observed *object.Item) bool {
	CopyMutatedSpecFields(expected, observed)
	e := expected.Obj.(*Object).Obj
	o := observed.Obj.(*Object).Obj

	return !reflect.DeepEqual(e.AlternativeLocationId, o.AlternativeLocationId) ||
		!reflect.DeepEqual(e.AuthorizedNetwork, o.AuthorizedNetwork) ||
		!reflect.DeepEqual(e.DisplayName, o.DisplayName) ||
		!reflect.DeepEqual(e.Labels, o.Labels) ||
		!reflect.DeepEqual(e.MemorySizeGb, o.MemorySizeGb) ||
		!reflect.DeepEqual(e.RedisConfigs, o.RedisConfigs) ||
		!reflect.DeepEqual(e.RedisVersion, o.RedisVersion) ||
		!reflect.DeepEqual(e.ReservedIpRange, o.ReservedIpRange) ||
		!reflect.DeepEqual(e.Tier, o.Tier)
}

// Observe - get resources
func (rm *RsrcManager) Observe(observables ...object.Observable) (*object.Bag, error) {
	var returnval *object.Bag = new(object.Bag)
	for _, item := range observables {
		obs, ok := item.Obj.(Observable)
		if !ok {
			continue
		}
		redis, err := rm.service.Projects.Locations.Instances.Get(obs.Parent + "/instances/" + obs.InstanceID).Do()
		if err != nil {
			if gcp.IsNotFound(err) {
				continue
			}
			return &object.Bag{}, nil
		}
		obj := Object{Obj: redis, Parent: obs.Parent, InstanceID: obs.InstanceID}
		returnval.Add(*obj.AsItem())
	}
	return returnval, nil
}

// Update - Generic client update
func (rm *RsrcManager) Update(item object.Item) error {
	obj := item.Obj.(*Object)
	d := obj.Obj
	_, err := rm.service.Projects.Locations.Instances.Patch(obj.Parent+"/instances/"+obj.InstanceID, d).UpdateMask("displayName,labels,memorySizeGb,redisConfigs").Do()
	return err
}

// Create - Generic client create
func (rm *RsrcManager) Create(item object.Item) error {
	obj := item.Obj.(*Object)
	d := obj.Obj
	_, err := rm.service.Projects.Locations.Instances.Create(obj.Parent, d).InstanceId(obj.InstanceID).Do()
	return err
}

// Delete - Generic client delete
func (rm *RsrcManager) Delete(item object.Item) error {
	obj := item.Obj.(*Object)
	_, err := rm.service.Projects.Locations.Instances.Delete(obj.Parent + "/instances/" + obj.InstanceID).Do()
	return err
}

// NewObject return a new object
func NewObject(parent, instanceid string) *Object {
	return &Object{
		Obj:        &redis.Instance{},
		InstanceID: instanceid,
		Parent:     parent,
	}
}

// NewService returns a new client
func NewService(ctx context.Context) (*redis.Service, error) {
	httpClient, err := google.DefaultClient(ctx, redis.CloudPlatformScope)
	if err != nil {
		return nil, err
	}
	client, err := redis.New(httpClient)
	if err != nil {
		return nil, err
	}
	client.UserAgent = UserAgent
	return client, nil
}