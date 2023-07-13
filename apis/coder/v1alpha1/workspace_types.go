/*
Copyright 2022 The Crossplane Authors.

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

package v1alpha1

import (
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"reflect"
)

// WorkspaceParameters are the configurable fields of a Workspace.
type WorkspaceParameters struct {
	ImageID         string `json:"image_id"`
	OrgID           string `json:"org_id"`
	ImageTag        string `json:"image_tag"`
	CPUCores        string `json:"cpu_cores"`
	MemoryGB        string `json:"memory_gb"`
	DiskGB          int    `json:"disk_gb"`
	GPUs            int    `json:"gpus"`
	UseContainerVM  bool   `json:"use_container_vm"`
	ResourcePoolID  string `json:"resource_pool_id"`
	Namespace       string `json:"namespace"`
	EnableAutoStart bool   `json:"autostart_enabled"`

	// ForUserID is an optional param to create a workspace for another user
	// other than the requester. This only works for admins and site managers.
	// +optional
	ForUserID *string `json:"for_user_id,omitempty"`

	// TemplateID comes from the parse template route on cemanager.
	// +optional
	TemplateID *string `json:"template_id,omitempty"`
}

// WorkspaceObservation are the observable fields of a Workspace.
type WorkspaceObservation struct {
	LatestStat      string `json:"latest_stat"`
	ObservableField string `json:"observableField,omitempty"`
}

// A WorkspaceSpec defines the desired state of a Workspace.
type WorkspaceSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       WorkspaceParameters `json:"forProvider"`
}

// A WorkspaceStatus represents the observed state of a Workspace.
type WorkspaceStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          WorkspaceObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true

// A Workspace is an example API type.
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,coderworkspaces}
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceList contains a list of Workspace
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}

// Workspace type metadata.
var (
	WorkspaceKind             = reflect.TypeOf(Workspace{}).Name()
	WorkspaceGroupKind        = schema.GroupKind{Group: Group, Kind: WorkspaceKind}.String()
	WorkspaceKindAPIVersion   = WorkspaceKind + "." + SchemeGroupVersion.String()
	WorkspaceGroupVersionKind = SchemeGroupVersion.WithKind(WorkspaceKind)
)

func init() {
	SchemeBuilder.Register(&Workspace{}, &WorkspaceList{})
}
