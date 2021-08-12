package controllers

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type reconcileType struct {
	ObjectType            client.Object
	GroupVersion          schema.GroupVersion
	GroupVersionSupported bool
	TypeWatchDisable      bool
}

func (rt reconcileType) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   rt.GroupVersion.Group,
		Version: rt.GroupVersion.Version,
		Kind:    rt.Kind(),
	}
}

func (rt reconcileType) Kind() string {
	return reflect.TypeOf(rt.ObjectType).Elem().Name()
}
