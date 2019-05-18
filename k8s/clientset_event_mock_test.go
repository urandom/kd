// Code generated by MockGen. DO NOT EDIT.
// Source: k8s.io/client-go/kubernetes/typed/core/v1 (interfaces: EventInterface)

// Package k8s_test is a generated GoMock package.
package k8s_test

import (
	gomock "github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"
	v10 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fields "k8s.io/apimachinery/pkg/fields"
	runtime "k8s.io/apimachinery/pkg/runtime"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	reflect "reflect"
)

// MockEventInterface is a mock of EventInterface interface
type MockEventInterface struct {
	ctrl     *gomock.Controller
	recorder *MockEventInterfaceMockRecorder
}

// MockEventInterfaceMockRecorder is the mock recorder for MockEventInterface
type MockEventInterfaceMockRecorder struct {
	mock *MockEventInterface
}

// NewMockEventInterface creates a new mock instance
func NewMockEventInterface(ctrl *gomock.Controller) *MockEventInterface {
	mock := &MockEventInterface{ctrl: ctrl}
	mock.recorder = &MockEventInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockEventInterface) EXPECT() *MockEventInterfaceMockRecorder {
	return m.recorder
}

// Create mocks base method
func (m *MockEventInterface) Create(arg0 *v1.Event) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Create", arg0)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Create indicates an expected call of Create
func (mr *MockEventInterfaceMockRecorder) Create(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Create", reflect.TypeOf((*MockEventInterface)(nil).Create), arg0)
}

// CreateWithEventNamespace mocks base method
func (m *MockEventInterface) CreateWithEventNamespace(arg0 *v1.Event) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateWithEventNamespace", arg0)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateWithEventNamespace indicates an expected call of CreateWithEventNamespace
func (mr *MockEventInterfaceMockRecorder) CreateWithEventNamespace(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateWithEventNamespace", reflect.TypeOf((*MockEventInterface)(nil).CreateWithEventNamespace), arg0)
}

// Delete mocks base method
func (m *MockEventInterface) Delete(arg0 string, arg1 *v10.DeleteOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Delete", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Delete indicates an expected call of Delete
func (mr *MockEventInterfaceMockRecorder) Delete(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Delete", reflect.TypeOf((*MockEventInterface)(nil).Delete), arg0, arg1)
}

// DeleteCollection mocks base method
func (m *MockEventInterface) DeleteCollection(arg0 *v10.DeleteOptions, arg1 v10.ListOptions) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteCollection", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteCollection indicates an expected call of DeleteCollection
func (mr *MockEventInterfaceMockRecorder) DeleteCollection(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteCollection", reflect.TypeOf((*MockEventInterface)(nil).DeleteCollection), arg0, arg1)
}

// Get mocks base method
func (m *MockEventInterface) Get(arg0 string, arg1 v10.GetOptions) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0, arg1)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get
func (mr *MockEventInterfaceMockRecorder) Get(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockEventInterface)(nil).Get), arg0, arg1)
}

// GetFieldSelector mocks base method
func (m *MockEventInterface) GetFieldSelector(arg0, arg1, arg2, arg3 *string) fields.Selector {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFieldSelector", arg0, arg1, arg2, arg3)
	ret0, _ := ret[0].(fields.Selector)
	return ret0
}

// GetFieldSelector indicates an expected call of GetFieldSelector
func (mr *MockEventInterfaceMockRecorder) GetFieldSelector(arg0, arg1, arg2, arg3 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFieldSelector", reflect.TypeOf((*MockEventInterface)(nil).GetFieldSelector), arg0, arg1, arg2, arg3)
}

// List mocks base method
func (m *MockEventInterface) List(arg0 v10.ListOptions) (*v1.EventList, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "List", arg0)
	ret0, _ := ret[0].(*v1.EventList)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// List indicates an expected call of List
func (mr *MockEventInterfaceMockRecorder) List(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "List", reflect.TypeOf((*MockEventInterface)(nil).List), arg0)
}

// Patch mocks base method
func (m *MockEventInterface) Patch(arg0 string, arg1 types.PatchType, arg2 []byte, arg3 ...string) (*v1.Event, error) {
	m.ctrl.T.Helper()
	varargs := []interface{}{arg0, arg1, arg2}
	for _, a := range arg3 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "Patch", varargs...)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Patch indicates an expected call of Patch
func (mr *MockEventInterfaceMockRecorder) Patch(arg0, arg1, arg2 interface{}, arg3 ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{arg0, arg1, arg2}, arg3...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Patch", reflect.TypeOf((*MockEventInterface)(nil).Patch), varargs...)
}

// PatchWithEventNamespace mocks base method
func (m *MockEventInterface) PatchWithEventNamespace(arg0 *v1.Event, arg1 []byte) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PatchWithEventNamespace", arg0, arg1)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PatchWithEventNamespace indicates an expected call of PatchWithEventNamespace
func (mr *MockEventInterfaceMockRecorder) PatchWithEventNamespace(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PatchWithEventNamespace", reflect.TypeOf((*MockEventInterface)(nil).PatchWithEventNamespace), arg0, arg1)
}

// Search mocks base method
func (m *MockEventInterface) Search(arg0 *runtime.Scheme, arg1 runtime.Object) (*v1.EventList, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Search", arg0, arg1)
	ret0, _ := ret[0].(*v1.EventList)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Search indicates an expected call of Search
func (mr *MockEventInterfaceMockRecorder) Search(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Search", reflect.TypeOf((*MockEventInterface)(nil).Search), arg0, arg1)
}

// Update mocks base method
func (m *MockEventInterface) Update(arg0 *v1.Event) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Update", arg0)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Update indicates an expected call of Update
func (mr *MockEventInterfaceMockRecorder) Update(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Update", reflect.TypeOf((*MockEventInterface)(nil).Update), arg0)
}

// UpdateWithEventNamespace mocks base method
func (m *MockEventInterface) UpdateWithEventNamespace(arg0 *v1.Event) (*v1.Event, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateWithEventNamespace", arg0)
	ret0, _ := ret[0].(*v1.Event)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UpdateWithEventNamespace indicates an expected call of UpdateWithEventNamespace
func (mr *MockEventInterfaceMockRecorder) UpdateWithEventNamespace(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateWithEventNamespace", reflect.TypeOf((*MockEventInterface)(nil).UpdateWithEventNamespace), arg0)
}

// Watch mocks base method
func (m *MockEventInterface) Watch(arg0 v10.ListOptions) (watch.Interface, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Watch", arg0)
	ret0, _ := ret[0].(watch.Interface)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Watch indicates an expected call of Watch
func (mr *MockEventInterfaceMockRecorder) Watch(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Watch", reflect.TypeOf((*MockEventInterface)(nil).Watch), arg0)
}
